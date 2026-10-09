package informix

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "bop/internal/plugin/informix/ifxagentpb"
)

// agentClient is everything Backup/Restore/Health need from the ifx-agent
// gRPC subprocess, narrowed to an interface so tests can substitute a fake
// instead of spawning a real process - the same role sshexec.Executor plays
// for the postgres/filesystem plugins.
type agentClient interface {
	Connect(ctx context.Context, req *pb.ConnectRequest) (connID string, err error)
	ExecuteQuery(ctx context.Context, connID, sql string, params []*pb.Param) (*queryResult, error)
	BeginTransaction(ctx context.Context, connID string) error
	CommitTransaction(ctx context.Context, connID string) error
	RollbackTransaction(ctx context.Context, connID string) error
	Disconnect(ctx context.Context, connID string) error
	// Close shuts the agent down and releases the subprocess. Best-effort -
	// see spawnedAgentClient.Close's doc comment - never returns an error
	// that should fail whatever Backup/Restore/Health call is cleaning up.
	Close() error
}

// queryResult collects one ExecuteQuery RPC's three event kinds, mirroring
// agent/testclient/main.go's executeQuery helper in the informix-odbc repo.
type queryResult struct {
	Columns  []*pb.ColumnDesc
	Rows     [][]*pb.Cell
	RowCount uint32
}

// spawnedAgentClient is the real agentClient: spawns ifx-agent, dials it
// over gRPC, and tracks the subprocess for a clean Close.
type spawnedAgentClient struct {
	cmd    *exec.Cmd
	conn   *grpc.ClientConn
	client pb.IfxAgentClient
}

// dialAgent spawns agentPath (ifx-agent) bound to an ephemeral port, reads
// its required "LISTENING <port>" first stdout line (see
// agent/server_main.cpp's doc comment in informix-odbc for why this is the
// readiness signal, not a separate side channel), and dials it over gRPC -
// the same sequence agent/testclient/main.go proves end to end against a
// real Informix server. ctx governs the subprocess's lifetime: cancelling it
// (e.g. controller.job_timeout firing) kills ifx-agent, not just abandons
// the gRPC call.
func dialAgent(ctx context.Context, agentPath string) (*spawnedAgentClient, error) {
	cmd := exec.CommandContext(ctx, agentPath, "127.0.0.1:0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("informix: ifx-agent stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("informix: start ifx-agent: %w", err)
	}

	port, err := readListeningPort(stdout)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("informix: read ifx-agent listening port: %w", err)
	}

	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("informix: dial ifx-agent: %w", err)
	}

	return &spawnedAgentClient{cmd: cmd, conn: conn, client: pb.NewIfxAgentClient(conn)}, nil
}

func readListeningPort(r io.Reader) (int, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("ifx-agent closed stdout before printing a listening port")
	}
	line := scanner.Text()
	const prefix = "LISTENING "
	if !strings.HasPrefix(line, prefix) {
		return 0, fmt.Errorf("unexpected first line from ifx-agent: %q", line)
	}
	return strconv.Atoi(strings.TrimPrefix(line, prefix))
}

func (a *spawnedAgentClient) Connect(ctx context.Context, req *pb.ConnectRequest) (string, error) {
	resp, err := a.client.Connect(ctx, req)
	if err != nil {
		return "", fmt.Errorf("informix: Connect RPC: %w", err)
	}
	return resp.GetConnectionId(), nil
}

func (a *spawnedAgentClient) ExecuteQuery(ctx context.Context, connID, sql string, params []*pb.Param) (*queryResult, error) {
	stream, err := a.client.ExecuteQuery(ctx, &pb.ExecuteQueryRequest{ConnectionId: connID, Sql: sql, Params: params})
	if err != nil {
		return nil, fmt.Errorf("informix: ExecuteQuery RPC: %w", err)
	}
	result := &queryResult{}
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("informix: ExecuteQuery stream: %w", err)
		}
		switch e := event.GetEvent().(type) {
		case *pb.QueryEvent_Columns:
			result.Columns = e.Columns.GetColumns()
		case *pb.QueryEvent_Row:
			result.Rows = append(result.Rows, e.Row.GetCells())
		case *pb.QueryEvent_Done:
			result.RowCount = e.Done.GetRowCount()
		}
	}
	return result, nil
}

func (a *spawnedAgentClient) BeginTransaction(ctx context.Context, connID string) error {
	if _, err := a.client.BeginTransaction(ctx, &pb.TransactionRequest{ConnectionId: connID}); err != nil {
		return fmt.Errorf("informix: BeginTransaction RPC: %w", err)
	}
	return nil
}

func (a *spawnedAgentClient) CommitTransaction(ctx context.Context, connID string) error {
	if _, err := a.client.CommitTransaction(ctx, &pb.TransactionRequest{ConnectionId: connID}); err != nil {
		return fmt.Errorf("informix: CommitTransaction RPC: %w", err)
	}
	return nil
}

func (a *spawnedAgentClient) RollbackTransaction(ctx context.Context, connID string) error {
	if _, err := a.client.RollbackTransaction(ctx, &pb.TransactionRequest{ConnectionId: connID}); err != nil {
		return fmt.Errorf("informix: RollbackTransaction RPC: %w", err)
	}
	return nil
}

func (a *spawnedAgentClient) Disconnect(ctx context.Context, connID string) error {
	if _, err := a.client.Disconnect(ctx, &pb.DisconnectRequest{ConnectionId: connID}); err != nil {
		return fmt.Errorf("informix: Disconnect RPC: %w", err)
	}
	return nil
}

// Close asks ifx-agent to shut down gracefully (draining any still-in-flight
// RPC rather than cutting it off - see informix-odbc's STATUS.md) and waits
// for the subprocess to exit. Always returns nil: Close runs during
// defer-style cleanup where the caller has already gotten what it needed
// from this connection, so a cleanup failure must not fail the overall
// Backup/Restore/Health call - same best-effort convention postgres's
// scratch-database drop follows. Failures are logged, not swallowed silently.
func (a *spawnedAgentClient) Close() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.client.Shutdown(shutdownCtx, &pb.ShutdownRequest{}); err != nil {
		slog.Default().Warn("informix: Shutdown RPC failed, killing ifx-agent", "error", err)
		_ = a.cmd.Process.Kill()
	}
	_ = a.conn.Close()
	if err := a.cmd.Wait(); err != nil {
		slog.Default().Warn("informix: ifx-agent exited with an error", "error", err)
	}
	return nil
}
