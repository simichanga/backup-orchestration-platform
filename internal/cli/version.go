package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags; defaults to "dev" otherwise.
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the bop version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "bop "+Version)
			return nil
		},
	}
}
