package informix

import (
	"context"
	"os"
	"strings"
	"testing"

	"bop/internal/inventory"
	"bop/internal/plugin"
)

// TestIntegrationBackupAndRestore exercises the real Plugin against a real
// ifx-agent subprocess and a live Informix server - skipped unless
// IFX_AGENT_PATH names a built ifx-agent binary, mirroring how
// internal/metadata's own Postgres integration test is gated behind
// BOP_TEST_POSTGRES_DSN. Build ifx-agent from the informix-odbc repo
// (IFX_BUILD_AGENT=ON) and point this at it; the target server defaults to
// the same dev container informix-odbc's own integration tests use.
func TestIntegrationBackupAndRestore(t *testing.T) {
	agentPath := os.Getenv("IFX_AGENT_PATH")
	if agentPath == "" {
		t.Skip("IFX_AGENT_PATH not set, skipping live integration test")
	}

	t.Setenv("IFX_IT_PASSWORD", envOr("IFX_PASSWORD", "in4mix"))
	rawConfig := map[string]interface{}{
		"host":         envOr("IFX_HOST", "127.0.0.1"),
		"port":         9088,
		"username":     envOr("IFX_USER", "informix"),
		"password_env": "IFX_IT_PASSWORD",
		"server_name":  envOr("IFX_SERVER", "informix"),
		"database":     envOr("IFX_DATABASE", "ifxodbc_test"),
		"tables":       []interface{}{"bop_informix_it_probe"},
		"agent_path":   agentPath,
	}

	factory := NewFactory()
	bp, err := factory(inventory.Server{}, &inventory.PluginConfig{Config: rawConfig}, t.TempDir())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	p := bp.(*Plugin)

	ctx := context.Background()
	setupSQL := []string{
		"drop table if exists bop_informix_it_probe",
		"create table bop_informix_it_probe (id integer, name varchar(50))",
	}
	runSetupSQL(t, p, ctx, setupSQL)
	t.Cleanup(func() {
		runSetupSQL(t, p, ctx, []string{"drop table if exists bop_informix_it_probe"})
	})
	runSetupSQL(t, p, ctx, []string{"insert into bop_informix_it_probe values (1, 'alice')", "insert into bop_informix_it_probe values (2, 'bob')"})

	resources, err := p.Discover(ctx)
	if err != nil || len(resources) != 1 {
		t.Fatalf("Discover: %v, %+v", err, resources)
	}

	artifact, err := p.Backup(ctx, resources[0])
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	defer os.Remove(artifact.Path)

	if err := p.Verify(ctx, artifact); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	content, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !strings.Contains(string(content), "alice") || !strings.Contains(string(content), "bob") {
		t.Fatalf("artifact missing expected content: %s", content)
	}

	// Restore-test: must insert then roll back, leaving the real table
	// exactly as it was (still just alice/bob, no third copy).
	testArtifact := artifact
	testArtifact.ResourceID = resources[0].ID + plugin.RestoreTestSuffix
	if err := p.Restore(ctx, testArtifact); err != nil {
		t.Fatalf("Restore (test): %v", err)
	}
	count := countRows(t, p, ctx, "bop_informix_it_probe")
	if count != 2 {
		t.Fatalf("after restore-test, table has %d row(s), want 2 (restore-test must not persist data)", count)
	}

	// Plain restore: clear the table, then restore from the artifact and
	// confirm the original rows come back.
	runSetupSQL(t, p, ctx, []string{"delete from bop_informix_it_probe"})
	if err := p.Restore(ctx, artifact); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	count = countRows(t, p, ctx, "bop_informix_it_probe")
	if count != 2 {
		t.Fatalf("after restore, table has %d row(s), want 2", count)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func runSetupSQL(t *testing.T, p *Plugin, ctx context.Context, statements []string) {
	t.Helper()
	client, connID, err := p.connect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer p.disconnect(ctx, client, connID)
	for _, sql := range statements {
		if _, err := client.ExecuteQuery(ctx, connID, sql, nil); err != nil {
			t.Fatalf("setup SQL %q: %v", sql, err)
		}
	}
}

func countRows(t *testing.T, p *Plugin, ctx context.Context, table string) int {
	t.Helper()
	client, connID, err := p.connect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer p.disconnect(ctx, client, connID)
	result, err := client.ExecuteQuery(ctx, connID, "select * from "+table, nil)
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return len(result.Rows)
}
