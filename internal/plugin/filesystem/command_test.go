package filesystem

import (
	"strings"
	"testing"
)

func TestTarCommandChangesToParentDirectory(t *testing.T) {
	cfg := filesystemConfig{}
	got := tarCommand(cfg, "/var/www")

	for _, want := range []string{"'tar'", "'czf'", "'-'", "'-C'", "'/var/'", "'www'"} {
		if !strings.Contains(got, want) {
			t.Errorf("tarCommand() = %q, missing %q", got, want)
		}
	}
}

func TestTarCommandTrimsTrailingSlash(t *testing.T) {
	cfg := filesystemConfig{}
	got := tarCommand(cfg, "/var/www/")

	if !strings.Contains(got, "'www'") {
		t.Errorf("tarCommand() = %q, want a bare 'www' entry, not an empty trailing entry", got)
	}
}

func TestTarCommandIncludesExcludes(t *testing.T) {
	cfg := filesystemConfig{Excludes: []string{"*.log", "node_modules"}}
	got := tarCommand(cfg, "/var/www")

	for _, want := range []string{"--exclude=*.log", "--exclude=node_modules"} {
		if !strings.Contains(got, want) {
			t.Errorf("tarCommand() = %q, missing %q", got, want)
		}
	}
}

func TestRemoveCommandDeletesRecursively(t *testing.T) {
	got := removeCommand("/tmp/bop-restore-test/var/www-bop-verify")

	if !strings.Contains(got, "'rm' '-rf' '/tmp/bop-restore-test/var/www-bop-verify'") {
		t.Errorf("removeCommand() = %q, want an rm -rf of the target", got)
	}
}

func TestUntarCommandCreatesTargetAndStripsTopLevel(t *testing.T) {
	got := untarCommand("/scratch/restore-target")

	if !strings.Contains(got, "'mkdir' '-p' '/scratch/restore-target'") {
		t.Errorf("untarCommand() = %q, want an mkdir -p for the target", got)
	}
	if !strings.Contains(got, "--strip-components=1") {
		t.Errorf("untarCommand() = %q, want --strip-components=1 so content lands directly in the target", got)
	}
	if !strings.Contains(got, " && ") {
		t.Errorf("untarCommand() = %q, want mkdir and tar joined with a real shell &&", got)
	}
}
