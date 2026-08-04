package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the bop command tree.
func NewRootCmd() *cobra.Command {
	var configPath string

	root := &cobra.Command{
		Use:   "bop",
		Short: "Backup Orchestration Platform",
	}
	root.PersistentFlags().StringVar(&configPath, "config", "/etc/bop/config.yaml", "path to config.yaml")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newControllerCmd(&configPath))
	root.AddCommand(newBackupCmd(&configPath))
	root.AddCommand(newSnapshotCmd(&configPath))
	root.AddCommand(newRestoreCmd(&configPath))
	root.AddCommand(newHealthCmd(&configPath))

	return root
}
