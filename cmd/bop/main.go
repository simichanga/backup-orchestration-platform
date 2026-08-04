// Command bop is the Backup Orchestration Platform entrypoint.
package main

import (
	"fmt"
	"os"

	"bop/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
