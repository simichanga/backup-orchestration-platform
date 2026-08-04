package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newHealthCmd checks whether a plugin can currently reach its target host,
// via the same BackupPlugin.Health check the plugin contract already
// defines (each built-in plugin just confirms its SSH connection works) -
// cheap and fast, not a full backup dry-run. Lets an operator verify
// inventory/SSH/known_hosts setup before finding out it's broken from a
// failed scheduled backup instead.
func newHealthCmd(configPath *string) *cobra.Command {
	var host, pluginName string

	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check whether a plugin can reach its target host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" || pluginName == "" {
				return fmt.Errorf("--host and --plugin are required")
			}

			a, err := buildApp(*configPath)
			if err != nil {
				return err
			}
			defer a.Close()

			p, err := a.Controller.BuildPlugin(host, pluginName)
			if err != nil {
				return err
			}

			if err := p.Health(cmd.Context()); err != nil {
				return fmt.Errorf("%s/%s: %w", host, pluginName, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s/%s: ok\n", host, pluginName)
			return nil
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "inventory host to check")
	cmd.Flags().StringVar(&pluginName, "plugin", "", "plugin to check")
	return cmd
}
