package cron

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
)

func NewCronCommand() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:     "cron",
		Aliases: []string{"c"},
		Short:   "Manage scheduled tasks",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		// Resolve workspace at execution time so it reflects the current config
		// and is shared across all subcommands.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := internal.LoadConfig()
			if err != nil {
				return fmt.Errorf("error loading config: %w", err)
			}
			workspace = cfg.WorkspacePath()
			return nil
		},
	}

	getWorkspace := func() string { return workspace }

	cmd.AddCommand(
		newListCommand(getWorkspace),
		newAddCommand(getWorkspace),
		newRemoveCommand(getWorkspace),
		newEnableCommand(getWorkspace),
		newDisableCommand(getWorkspace),
	)

	return cmd
}
