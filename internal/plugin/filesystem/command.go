package filesystem

import (
	"path"
	"strings"

	"bop/internal/plugin/shellcmd"
)

// tarCommand builds the remote command to stream a gzip-compressed tar
// archive of sourcePath to stdout. -C changes into sourcePath's parent
// directory first, so the archive's single top-level entry is just the
// base name - matching untarCommand's --strip-components=1 on the way
// back in.
func tarCommand(cfg filesystemConfig, sourcePath string) string {
	dir, base := path.Split(strings.TrimSuffix(sourcePath, "/"))
	if dir == "" {
		dir = "."
	}
	args := []string{"tar", "czf", "-", "-C", dir}
	for _, ex := range cfg.Excludes {
		args = append(args, "--exclude="+ex)
	}
	args = append(args, base)
	return shellcmd.Build(args)
}

// untarCommand builds the remote command to extract a tar.gz stream
// (received on stdin) into targetPath, creating it first if needed.
// --strip-components=1 drops the top-level entry tarCommand wrapped the
// archive in, so content lands directly inside targetPath regardless of
// what the source path's base name was - this is what lets Restore target
// an arbitrary directory (notably a scratch-suffixed one during a
// restore-test) rather than requiring an exact match with the source path.
//
// mkdir and tar are joined with a real (unquoted) "&&", not built as a
// single shellcmd.Build call: shellcmd quotes every token as a literal
// argument, which would turn "&&" into a literal string instead of a shell
// operator.
func untarCommand(targetPath string) string {
	mkdir := shellcmd.Build([]string{"mkdir", "-p", targetPath})
	extract := shellcmd.Build([]string{"tar", "xzf", "-", "-C", targetPath, "--strip-components=1"})
	return mkdir + " && " + extract
}
