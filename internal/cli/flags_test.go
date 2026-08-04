package cli

import (
	"strings"
	"testing"
)

// These commands validate required flags before buildApp is ever called,
// so they can be tested without a real config.yaml/inventory.yaml.

func TestBackupCmdRequiresHostAndPlugin(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"backup"})
	root.SilenceUsage = true
	root.SilenceErrors = true

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--host and --plugin are required") {
		t.Errorf("backup with no flags: err = %v, want a --host/--plugin required error", err)
	}
}

func TestSnapshotListCmdRequiresHost(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"snapshot", "list"})
	root.SilenceUsage = true
	root.SilenceErrors = true

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--host is required") {
		t.Errorf("snapshot list with no flags: err = %v, want a --host required error", err)
	}
}

func TestRestoreCmdRequiresSnapshotAndTarget(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"restore"})
	root.SilenceUsage = true
	root.SilenceErrors = true

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--snapshot and --target are required") {
		t.Errorf("restore with no flags: err = %v, want a --snapshot/--target required error", err)
	}
}
