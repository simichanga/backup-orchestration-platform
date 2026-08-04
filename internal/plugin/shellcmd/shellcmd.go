// Package shellcmd builds POSIX shell command lines from argv-style token
// lists, shared by every SSH-based plugin (postgres, filesystem, ...) so
// shell-quoting correctness lives in exactly one place.
package shellcmd

import "strings"

// Quote wraps s in single quotes for a POSIX shell, escaping any embedded
// single quotes. The remote SSH server runs the command line through the
// user's shell, so every token that isn't a fixed keyword must be quoted -
// database names, passwords, and paths are all operator-controlled
// inventory data, not free-form user input, but quoting is cheap and
// correct regardless.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Build quotes each argv-style token and joins them into a single shell
// command line. Building as an argv slice first, rather than interpolating
// into a format string, avoids the nested-quoting bugs that come from
// mixing a wrapper command's own arguments (e.g. docker exec) with the
// command it runs.
func Build(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = Quote(a)
	}
	return strings.Join(quoted, " ")
}
