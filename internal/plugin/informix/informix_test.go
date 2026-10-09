package informix

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"bop/internal/core"
	"bop/internal/plugin"
	pb "bop/internal/plugin/informix/ifxagentpb"
)

// fakeAgentClient is an agentClient test double: no real ifx-agent
// subprocess, no real gRPC call.
type fakeAgentClient struct {
	calls []string

	connectErr error

	queryResults []*queryResult // consumed in order, one per ExecuteQuery call
	queryErr     error
	// queryErrFor, if set, overrides queryErr on a per-call basis - lets a
	// test simulate one INSERT in a sequence failing while others succeed.
	queryErrFor func(sql string) error

	beginErr, commitErr, rollbackErr, disconnectErr error
}

func (f *fakeAgentClient) Connect(context.Context, *pb.ConnectRequest) (string, error) {
	f.calls = append(f.calls, "Connect")
	if f.connectErr != nil {
		return "", f.connectErr
	}
	return "conn-1", nil
}

func (f *fakeAgentClient) ExecuteQuery(_ context.Context, _, sql string, _ []*pb.Param) (*queryResult, error) {
	f.calls = append(f.calls, "ExecuteQuery:"+sql)
	if f.queryErrFor != nil {
		if err := f.queryErrFor(sql); err != nil {
			return nil, err
		}
	} else if f.queryErr != nil {
		return nil, f.queryErr
	}
	if len(f.queryResults) > 0 {
		r := f.queryResults[0]
		f.queryResults = f.queryResults[1:]
		return r, nil
	}
	return &queryResult{}, nil
}

func (f *fakeAgentClient) BeginTransaction(context.Context, string) error {
	f.calls = append(f.calls, "BeginTransaction")
	return f.beginErr
}

func (f *fakeAgentClient) CommitTransaction(context.Context, string) error {
	f.calls = append(f.calls, "CommitTransaction")
	return f.commitErr
}

func (f *fakeAgentClient) RollbackTransaction(context.Context, string) error {
	f.calls = append(f.calls, "RollbackTransaction")
	return f.rollbackErr
}

func (f *fakeAgentClient) Disconnect(context.Context, string) error {
	f.calls = append(f.calls, "Disconnect")
	return f.disconnectErr
}

func (f *fakeAgentClient) Close() error {
	f.calls = append(f.calls, "Close")
	return nil
}

func newTestPlugin(cfg Config, tempDir string, fake *fakeAgentClient) *Plugin {
	return &Plugin{
		cfg:     cfg,
		tempDir: tempDir,
		dial: func(context.Context, string) (agentClient, error) {
			return fake, nil
		},
	}
}

func textCell(sqlType int32, text string) *pb.Cell {
	return &pb.Cell{SqlType: sqlType, Value: &pb.Value{Kind: &pb.Value_TextValue{TextValue: text}}}
}

func nullCell(sqlType int32) *pb.Cell {
	return &pb.Cell{IsNull: true, SqlType: sqlType}
}

func TestDiscoverReturnsConfiguredTables(t *testing.T) {
	p := &Plugin{cfg: Config{Tables: []string{"customers", "orders"}}}
	resources, err := p.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(resources) != 2 || resources[0].ID != "customers" || resources[1].ID != "orders" {
		t.Errorf("Discover() = %+v, want [customers orders]", resources)
	}
}

func TestBackupWritesArtifact(t *testing.T) {
	fake := &fakeAgentClient{queryResults: []*queryResult{{
		Columns: []*pb.ColumnDesc{{Name: "id"}, {Name: "name"}},
		Rows: [][]*pb.Cell{
			{textCell(4, "1"), textCell(12, "alice")},
			{textCell(4, "2"), nullCell(12)},
		},
		RowCount: 0,
	}}}
	p := newTestPlugin(Config{AgentPath: "ifx-agent"}, t.TempDir(), fake)

	artifact, err := p.Backup(context.Background(), core.Resource{ID: "customers"})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if artifact.ResourceID != "customers" {
		t.Errorf("ResourceID = %q, want customers", artifact.ResourceID)
	}

	content, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("artifact has %d line(s), want 3 (1 header + 2 rows): %q", len(lines), content)
	}
	if !strings.Contains(lines[0], `"customers"`) || !strings.Contains(lines[0], `"id"`) {
		t.Errorf("header line = %q, want it to name the table and columns", lines[0])
	}
	if !strings.Contains(lines[1], `"alice"`) {
		t.Errorf("row 1 = %q, want it to contain alice", lines[1])
	}
	if !strings.Contains(lines[2], `"is_null":true`) {
		t.Errorf("row 2 = %q, want a NULL cell", lines[2])
	}

	var queries []string
	for _, c := range fake.calls {
		if strings.HasPrefix(c, "ExecuteQuery:") {
			queries = append(queries, c)
		}
	}
	if len(queries) != 1 || !strings.Contains(queries[0], "select * from customers") {
		t.Errorf("queries = %v, want exactly one select against customers", queries)
	}
	if fake.calls[len(fake.calls)-1] != "Close" {
		t.Errorf("last call = %q, want Close (agent must be shut down even on success)", fake.calls[len(fake.calls)-1])
	}
}

func TestBackupPropagatesQueryErrorAndCreatesNoFile(t *testing.T) {
	fake := &fakeAgentClient{queryErr: errors.New("connection refused")}
	dir := t.TempDir()
	p := newTestPlugin(Config{AgentPath: "ifx-agent"}, dir, fake)

	if _, err := p.Backup(context.Background(), core.Resource{ID: "customers"}); err == nil {
		t.Fatalf("Backup: expected error, got nil")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("tempDir has %d leftover file(s) after a failed query, want 0: %v", len(entries), entries)
	}
}

func TestVerifyValidDump(t *testing.T) {
	p := &Plugin{}
	path := t.TempDir() + "/dump.jsonl"
	content := `{"table":"customers","columns":[{"name":"id"}]}` + "\n" + `{"cells":[{"text_value":"1"}]}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := p.Verify(context.Background(), core.Artifact{Path: path}); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestVerifyMissingHeader(t *testing.T) {
	p := &Plugin{}
	path := t.TempDir() + "/dump.jsonl"
	if err := os.WriteFile(path, []byte("not json at all"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := p.Verify(context.Background(), core.Artifact{Path: path}); err == nil {
		t.Errorf("Verify: expected error for a non-JSON header, got nil")
	}
}

func TestVerifyEmptyFile(t *testing.T) {
	p := &Plugin{}
	path := t.TempDir() + "/dump.jsonl"
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := p.Verify(context.Background(), core.Artifact{Path: path}); err == nil {
		t.Errorf("Verify: expected error for an empty artifact, got nil")
	}
}

func writeDumpFile(t *testing.T, table string) string {
	t.Helper()
	path := t.TempDir() + "/dump.jsonl"
	content := `{"table":"` + table + `","columns":[{"name":"id"},{"name":"name"}]}` + "\n" +
		`{"cells":[{"sql_type":4,"text_value":"1"},{"sql_type":12,"text_value":"alice"}]}` + "\n" +
		`{"cells":[{"sql_type":4,"text_value":"2"},{"is_null":true,"sql_type":12}]}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write dump file: %v", err)
	}
	return path
}

func TestRestoreInsertsRowsAndCommits(t *testing.T) {
	fake := &fakeAgentClient{}
	p := newTestPlugin(Config{AgentPath: "ifx-agent"}, "", fake)

	err := p.Restore(context.Background(), core.Artifact{ResourceID: "customers", Path: writeDumpFile(t, "customers")})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	var inserts []string
	for _, c := range fake.calls {
		if strings.HasPrefix(c, "ExecuteQuery:") {
			inserts = append(inserts, c)
		}
	}
	if len(inserts) != 2 {
		t.Fatalf("insert calls = %v, want 2 (one per row)", inserts)
	}
	for _, c := range inserts {
		if !strings.Contains(c, "insert into customers values (?,?)") {
			t.Errorf("insert call = %q, want it to target customers with 2 placeholders", c)
		}
	}
	if !contains(fake.calls, "CommitTransaction") {
		t.Errorf("calls = %v, want CommitTransaction (a plain restore must commit)", fake.calls)
	}
	if contains(fake.calls, "RollbackTransaction") {
		t.Errorf("calls = %v, want no RollbackTransaction on a successful plain restore", fake.calls)
	}
}

func TestRestoreTestAlwaysRollsBackAndNeverCommits(t *testing.T) {
	fake := &fakeAgentClient{}
	p := newTestPlugin(Config{AgentPath: "ifx-agent"}, "", fake)

	resourceID := "customers" + plugin.RestoreTestSuffix
	err := p.Restore(context.Background(), core.Artifact{ResourceID: resourceID, Path: writeDumpFile(t, "customers")})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	var inserts []string
	for _, c := range fake.calls {
		if strings.HasPrefix(c, "ExecuteQuery:") {
			inserts = append(inserts, c)
		}
	}
	if len(inserts) != 2 {
		t.Fatalf("insert calls = %v, want 2 (restore-test still runs the real insert path)", inserts)
	}
	for _, c := range inserts {
		// The restore-test inserts into the REAL table name (parsed from
		// the artifact header), not a scratch-suffixed one - there is no
		// scratch table in this design (see informix.go's Restore doc
		// comment for why CREATE TABLE ... (LIKE ...) doesn't work here).
		if !strings.Contains(c, "insert into customers values") {
			t.Errorf("insert call = %q, want it to target the real table customers", c)
		}
	}
	if !contains(fake.calls, "RollbackTransaction") {
		t.Errorf("calls = %v, want RollbackTransaction (a restore-test must never persist data)", fake.calls)
	}
	if contains(fake.calls, "CommitTransaction") {
		t.Errorf("calls = %v, want no CommitTransaction on a restore-test", fake.calls)
	}
}

func TestRestoreRollsBackOnInsertError(t *testing.T) {
	fake := &fakeAgentClient{queryErrFor: func(sql string) error {
		if strings.HasPrefix(sql, "insert into") {
			return errors.New("sqlcode -206: table not found")
		}
		return nil
	}}
	p := newTestPlugin(Config{AgentPath: "ifx-agent"}, "", fake)

	err := p.Restore(context.Background(), core.Artifact{ResourceID: "customers", Path: writeDumpFile(t, "customers")})
	if err == nil {
		t.Fatalf("Restore: expected error, got nil")
	}
	if !contains(fake.calls, "RollbackTransaction") {
		t.Errorf("calls = %v, want RollbackTransaction after a failed insert", fake.calls)
	}
	if contains(fake.calls, "CommitTransaction") {
		t.Errorf("calls = %v, want no CommitTransaction after a failed insert", fake.calls)
	}
}

func TestHealthConnectsAndDisconnects(t *testing.T) {
	fake := &fakeAgentClient{}
	p := newTestPlugin(Config{AgentPath: "ifx-agent"}, "", fake)

	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
	if !contains(fake.calls, "Connect") || !contains(fake.calls, "Disconnect") || !contains(fake.calls, "Close") {
		t.Errorf("calls = %v, want Connect, Disconnect, and Close", fake.calls)
	}
}

func TestHealthPropagatesConnectError(t *testing.T) {
	fake := &fakeAgentClient{connectErr: errors.New("no route to host")}
	p := newTestPlugin(Config{AgentPath: "ifx-agent"}, "", fake)

	if err := p.Health(context.Background()); err == nil {
		t.Errorf("Health: expected error, got nil")
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
