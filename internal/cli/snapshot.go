package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newSnapshotCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage snapshots",
	}
	cmd.AddCommand(newSnapshotListCmd(configPath))
	return cmd
}

func newSnapshotListCmd(configPath *string) *cobra.Command {
	var host string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List snapshots for a host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" {
				return fmt.Errorf("--host is required")
			}

			a, err := buildApp(*configPath)
			if err != nil {
				return err
			}
			defer a.Close()

			snaps, err := a.Metadata.ListSnapshots(cmd.Context(), host)
			if err != nil {
				return fmt.Errorf("list snapshots: %w", err)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tHost\tPlugin\tTime\tSize")
			for _, s := range snaps {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", s.ID, s.Host, s.Plugin, s.CreatedAt.Format("2006-01-02 15:04:05"), s.Size)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "host to list snapshots for")
	return cmd
}
