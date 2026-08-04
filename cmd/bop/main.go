// Command bop is the Backup Orchestration Platform entrypoint. The CLI
// (Cobra, subcommands) is not wired up yet; this is a build placeholder
// while the core interfaces and pipeline are scaffolded.
package main

import "fmt"

var version = "dev"

func main() {
	fmt.Printf("bop %s (scaffolding, no subcommands yet)\n", version)
}
