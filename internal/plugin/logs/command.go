package logs

import (
	"time"

	"bop/internal/plugin/shellcmd"
)

// journalCommand dumps the systemd journal from since onward as text, one
// line per entry with a microsecond ISO timestamp. -q drops the "-- No
// entries --" marker an empty window would otherwise print. since is
// passed in UTC, explicitly suffixed, so the target's local time zone
// doesn't shift the window.
func journalCommand(since time.Time) string {
	return shellcmd.Build([]string{"journalctl", "-q", "--no-pager", "-o", "short-iso-precise",
		"--since", since.UTC().Format("2006-01-02 15:04:05") + " UTC"})
}

// containerLogsCommand dumps one container's logs from since onward,
// timestamped. docker logs replays the container's stderr on its own
// stderr, so 2>&1 is needed to keep those lines in the artifact (and out
// of sshexec's in-memory stderr buffer). The window and name reach the
// inner shell as positional parameters, never interpolated into the
// script itself.
func containerLogsCommand(name string, since time.Time) string {
	return shellcmd.Build([]string{"sh", "-c", `exec docker logs --timestamps --since "$1" "$2" 2>&1`, "sh",
		since.UTC().Format(time.RFC3339), name})
}

// listContainersCommand lists every container name, running or stopped:
// a stopped container's logs are still worth keeping.
func listContainersCommand() string {
	return shellcmd.Build([]string{"docker", "ps", "-a", "--format", "{{.Names}}"})
}
