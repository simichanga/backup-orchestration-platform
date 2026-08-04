package cli

import (
	"bytes"
	"testing"
)

func TestNewRootCmdHasExpectedSubcommands(t *testing.T) {
	root := NewRootCmd()

	want := []string{"version", "controller", "backup", "snapshot", "restore"}
	for _, name := range want {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd == root {
			t.Errorf("subcommand %q not found on root", name)
		}
	}
}

func TestVersionCmdPrintsVersion(t *testing.T) {
	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if buf.Len() == 0 {
		t.Errorf("version command produced no output")
	}
}
