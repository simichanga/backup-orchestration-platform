// Package informix implements the BackupPlugin port for IBM Informix.
// Unlike postgres/filesystem (SSH to the target host, shell out to a CLI
// tool), this plugin spawns a local gRPC subprocess ("ifx-agent", from the
// companion informix-odbc project: https://github.com/simichanga/informix-odbc)
// and talks to it directly - there is no SSH involved; config.host/port
// identify the Informix server itself, and config.agent_path is a binary
// available on whatever host runs "bop controller".
//
// Restore model (current scope): re-insert rows into an already-existing
// table. Recreating a table's schema from scratch is explicitly out of
// scope for now - every table this plugin backs up/restores must already
// exist on the target server.
package informix

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"bop/internal/core"
	"bop/internal/inventory"
	"bop/internal/plugin"
	pb "bop/internal/plugin/informix/ifxagentpb"
)

// maxDumpLineSize bounds a single JSON Lines record (one row) - a smart
// LOB (BLOB/CLOB) can be large, so this is generous, not a tight limit:
// 64MB comfortably covers any single-row content this plugin is expected
// to handle without risking an unbounded read on a corrupt artifact.
const maxDumpLineSize = 64 * 1024 * 1024

// Plugin implements plugin.BackupPlugin for Informix. dial is the agent
// subprocess seam (real: dialAgent; tests: a fake), matching how
// postgres.Plugin's exec field is sshexec.Executor.
type Plugin struct {
	cfg     Config
	tempDir string
	dial    func(ctx context.Context, agentPath string) (agentClient, error)
}

// NewFactory returns a controller.PluginFactory-compatible factory. Unlike
// postgres/filesystem's factories, this one takes no knownHostsFile (or any
// other SSH-related) parameter, and doesn't validate srv.Host/SSHUser/
// SSHKey - none of that applies here. srv is accepted only to match the
// PluginFactory signature; the Informix server's own address lives in this
// plugin's config (config.host/config.port), not in the inventory host
// entry, which exists here purely as a grouping key for retention/schedule.
func NewFactory() func(srv inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
	return func(_ inventory.Server, cfg *inventory.PluginConfig, tempDir string) (plugin.BackupPlugin, error) {
		ifxCfg, err := ParseConfig(cfg)
		if err != nil {
			return nil, err
		}
		return &Plugin{
			cfg:     ifxCfg,
			tempDir: tempDir,
			dial: func(ctx context.Context, agentPath string) (agentClient, error) {
				return dialAgent(ctx, agentPath)
			},
		}, nil
	}
}

func (p *Plugin) Discover(context.Context) ([]core.Resource, error) {
	resources := make([]core.Resource, len(p.cfg.Tables))
	for i, t := range p.cfg.Tables {
		resources[i] = core.Resource{ID: t, Name: t}
	}
	return resources, nil
}

// connect spawns ifx-agent and opens one sqli connection. Every
// Backup/Restore/Health call is a fully self-contained spawn-connect-work-
// disconnect-shutdown sequence - BackupPlugin has no plugin-level Close
// hook, and the controller builds a fresh Plugin per job (see
// controller.PluginFactory's doc comment), so there is no safe place to
// hold a long-lived connection open across calls.
func (p *Plugin) connect(ctx context.Context) (agentClient, string, error) {
	client, err := p.dial(ctx, p.cfg.AgentPath)
	if err != nil {
		return nil, "", fmt.Errorf("informix: %w", err)
	}
	connID, err := client.Connect(ctx, &pb.ConnectRequest{
		Host:       p.cfg.Host,
		Port:       int32(p.cfg.Port),
		Username:   p.cfg.Username,
		Password:   p.cfg.Password,
		ServerName: p.cfg.ServerName,
		Database:   p.cfg.Database,
	})
	if err != nil {
		client.Close()
		return nil, "", fmt.Errorf("informix: %w", err)
	}
	return client, connID, nil
}

func (p *Plugin) disconnect(ctx context.Context, client agentClient, connID string) {
	if err := client.Disconnect(ctx, connID); err != nil {
		slog.Default().Warn("informix: disconnect failed", "error", err)
	}
	client.Close()
}

// --- Backup ---

// dumpHeader is the JSON Lines artifact format's first line. Columns is
// informational only (lets a human - or a future "recreate from scratch"
// feature - see what was backed up without parsing row lines); it is not
// consulted by Restore, which gets everything it needs (including each
// value's SQL type) from the row lines themselves, mirroring how
// ifx_agent.proto's Cell carries its own sql_type rather than relying on a
// separately-fetched schema.
type dumpHeader struct {
	Table   string       `json:"table"`
	Columns []dumpColumn `json:"columns"`
}

type dumpColumn struct {
	Name string `json:"name"`
}

type dumpRow struct {
	Cells []dumpCell `json:"cells"`
}

// dumpCell mirrors ifx_agent.proto's Cell/Value shape in JSON instead of
// protobuf wire format - chosen over reusing the wire format directly for
// the artifact file so the archive stays inspectable without the pb
// package and doesn't couple to ifx-agent's wire protocol evolving later.
type dumpCell struct {
	IsNull     bool    `json:"is_null,omitempty"`
	SQLType    int32   `json:"sql_type,omitempty"`
	TextValue  *string `json:"text_value,omitempty"`
	BytesValue []byte  `json:"bytes_value,omitempty"`
}

func cellToDump(cell *pb.Cell) dumpCell {
	sqlType := cell.GetSqlType()
	if cell.GetIsNull() {
		return dumpCell{IsNull: true, SQLType: sqlType}
	}
	switch v := cell.GetValue().GetKind().(type) {
	case *pb.Value_TextValue:
		text := v.TextValue
		return dumpCell{SQLType: sqlType, TextValue: &text}
	case *pb.Value_BytesValue:
		return dumpCell{SQLType: sqlType, BytesValue: v.BytesValue}
	default:
		// No text/bytes value populated and not NULL: a type this driver
		// has no text/value conversion for at all (see Cell's own doc
		// comment in ifx_agent.proto) - genuinely rare for the MVP scalar
		// types plus LOBs this plugin's config.tables are expected to use.
		return dumpCell{IsNull: true, SQLType: sqlType}
	}
}

func cellToParam(cell dumpCell) *pb.Param {
	if cell.IsNull {
		return &pb.Param{IsNull: true, ParameterType: cell.SQLType}
	}
	if cell.BytesValue != nil {
		return &pb.Param{ParameterType: cell.SQLType, Value: &pb.Value{Kind: &pb.Value_BytesValue{BytesValue: cell.BytesValue}}}
	}
	text := ""
	if cell.TextValue != nil {
		text = *cell.TextValue
	}
	return &pb.Param{ParameterType: cell.SQLType, Value: &pb.Value{Kind: &pb.Value_TextValue{TextValue: text}}}
}

func writeDump(w io.Writer, table string, result *queryResult) error {
	enc := json.NewEncoder(w)
	header := dumpHeader{Table: table, Columns: make([]dumpColumn, len(result.Columns))}
	for i, col := range result.Columns {
		header.Columns[i] = dumpColumn{Name: col.GetName()}
	}
	if err := enc.Encode(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	for _, row := range result.Rows {
		dr := dumpRow{Cells: make([]dumpCell, len(row))}
		for i, cell := range row {
			dr.Cells[i] = cellToDump(cell)
		}
		if err := enc.Encode(dr); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}
	return nil
}

// Backup reads every row of res.ID (a configured table name) through
// ExecuteQuery and writes it to a local temp file in the JSON Lines format
// above - streamed row by row as ExecuteQuery's own stream delivers them
// (collected into queryResult.Rows by agentclient.go's ExecuteQuery, which
// itself drains the whole gRPC stream first; a future revision could
// stream directly row-by-row into the file without buffering the whole
// result in memory first, same discipline postgres's pg_dump piping
// already has - not done yet since ExecuteQuery's current shape buffers).
func (p *Plugin) Backup(ctx context.Context, res core.Resource) (core.Artifact, error) {
	client, connID, err := p.connect(ctx)
	if err != nil {
		return core.Artifact{}, fmt.Errorf("informix: backup %s: %w", res.ID, err)
	}
	defer p.disconnect(ctx, client, connID)

	result, err := client.ExecuteQuery(ctx, connID, "select * from "+res.ID, nil)
	if err != nil {
		return core.Artifact{}, fmt.Errorf("informix: backup %s: query: %w", res.ID, err)
	}

	f, err := os.CreateTemp(p.tempDir, "informix-"+res.ID+"-*")
	if err != nil {
		return core.Artifact{}, fmt.Errorf("informix: create artifact file: %w", err)
	}
	path := f.Name()

	// Close before Remove on every error path, not after via defer: an
	// open file handle can't be removed on Windows (unlike POSIX) - same
	// discipline as postgres.Plugin.Backup.
	if err := writeDump(f, res.ID, result); err != nil {
		f.Close()
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("informix: write artifact: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("informix: stat artifact: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(path)
		return core.Artifact{}, fmt.Errorf("informix: close artifact: %w", err)
	}

	return core.Artifact{
		ResourceID: res.ID,
		Path:       path,
		Size:       info.Size(),
		CreatedAt:  time.Now(),
	}, nil
}

// --- Restore ---

func newDumpScanner(f *os.File) *bufio.Scanner {
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxDumpLineSize)
	return scanner
}

// Restore re-inserts a.Path's rows into the table the artifact was backed
// up from (parsed from the artifact's own header line, not re-derived from
// a.ResourceID - keeps the artifact self-describing). When a.ResourceID
// carries plugin.RestoreTestSuffix (the controller's restore-test step),
// every row is still inserted into the real table - there's no scratch
// table to provision; "CREATE TABLE ... (LIKE ...)" was tried and found to
// not mean table-structure-copy on this Informix version, it resolves
// LIKE's argument as a row-type name instead (sqlcode -9628, confirmed
// live) - but the whole insert sequence runs inside one transaction that
// is always rolled back, never committed, for a restore-test. This proves
// the exact INSERT/bind path a real restore would use, against the real
// schema, with no data persisted: reuses the same transaction isolation
// already confirmed live in informix-odbc's own agent/testclient (an
// uncommitted insert is invisible to any other connection). One documented
// side effect: a SERIAL/BIGSERIAL column's counter can still advance
// despite the rollback - a common cross-database gotcha, not a
// data-correctness issue, since no row is actually left behind.
func (p *Plugin) Restore(ctx context.Context, a core.Artifact) error {
	f, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("informix: restore: open artifact: %w", err)
	}
	defer f.Close()

	scanner := newDumpScanner(f)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("informix: restore: read artifact header: %w", err)
		}
		return fmt.Errorf("informix: restore: artifact has no header line")
	}
	var header dumpHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return fmt.Errorf("informix: restore: parse artifact header: %w", err)
	}
	if header.Table == "" || len(header.Columns) == 0 {
		return fmt.Errorf("informix: restore: artifact header missing table/columns")
	}
	if !validTableName.MatchString(header.Table) {
		return fmt.Errorf("informix: restore: artifact header table name %q is not a valid identifier", header.Table)
	}

	isRestoreTest := strings.HasSuffix(a.ResourceID, plugin.RestoreTestSuffix)

	client, connID, err := p.connect(ctx)
	if err != nil {
		return fmt.Errorf("informix: restore %s: %w", header.Table, err)
	}
	defer p.disconnect(ctx, client, connID)

	if err := client.BeginTransaction(ctx, connID); err != nil {
		return fmt.Errorf("informix: restore %s: begin transaction: %w", header.Table, err)
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(header.Columns)), ",")
	insertSQL := fmt.Sprintf("insert into %s values (%s)", header.Table, placeholders)

	insertErr := p.insertRows(ctx, client, connID, scanner, insertSQL, len(header.Columns))

	if insertErr != nil || isRestoreTest {
		if err := client.RollbackTransaction(ctx, connID); err != nil {
			slog.Default().Warn("informix: rollback failed", "table", header.Table, "error", err)
		}
		if insertErr != nil {
			return fmt.Errorf("informix: restore %s: %w", header.Table, insertErr)
		}
		return nil
	}

	if err := client.CommitTransaction(ctx, connID); err != nil {
		return fmt.Errorf("informix: restore %s: commit transaction: %w", header.Table, err)
	}
	return nil
}

func (p *Plugin) insertRows(ctx context.Context, client agentClient, connID string, scanner *bufio.Scanner, insertSQL string, columnCount int) error {
	for scanner.Scan() {
		var row dumpRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return fmt.Errorf("parse row: %w", err)
		}
		if len(row.Cells) != columnCount {
			return fmt.Errorf("row has %d cell(s), header declares %d column(s)", len(row.Cells), columnCount)
		}
		params := make([]*pb.Param, len(row.Cells))
		for i, cell := range row.Cells {
			params[i] = cellToParam(cell)
		}
		if _, err := client.ExecuteQuery(ctx, connID, insertSQL, params); err != nil {
			return fmt.Errorf("insert row: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read artifact: %w", err)
	}
	return nil
}

// --- Verify, Health, Metadata ---

// Verify is a cheap structural check (tier 1 of the three-tier
// verification model, see docs/01-introduction.md): the artifact's header
// line parses and declares at least one column. Not a live DB check -
// that's what the restore-test does.
func (p *Plugin) Verify(_ context.Context, a core.Artifact) error {
	f, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("informix: verify: open artifact: %w", err)
	}
	defer f.Close()

	scanner := newDumpScanner(f)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("informix: verify: read artifact: %w", err)
		}
		return fmt.Errorf("informix: verify: artifact is empty")
	}
	var header dumpHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return fmt.Errorf("informix: verify: artifact header is not valid JSON: %w", err)
	}
	if header.Table == "" || len(header.Columns) == 0 {
		return fmt.Errorf("informix: verify: artifact header missing table/columns")
	}
	return nil
}

// Health spawns ifx-agent and opens/closes one connection - a cheap
// reachability check, same spirit as postgres's "echo ok".
func (p *Plugin) Health(ctx context.Context) error {
	client, connID, err := p.connect(ctx)
	if err != nil {
		return fmt.Errorf("informix: health check: %w", err)
	}
	p.disconnect(ctx, client, connID)
	return nil
}

func (p *Plugin) Metadata() core.PluginMetadata {
	return core.PluginMetadata{Name: "informix", Version: "0.1.0"}
}
