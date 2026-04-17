package cfgcmd

import (
	"github.com/spf13/cobra"
)

// NewConfigCommand creates a git-style "config" command with list/set/get subcommands.
func NewConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"cfg"},
		Short:   "View and modify MetaClaw configuration",
		Example: `  metaclaw config list                    # show all settings
	metaclaw config get agents.defaults.primary_model
	metaclaw config set agents.defaults.primary_model deepseek-chat
	metaclaw config set channels.telegram.token "123:ABC"
	metaclaw config path                    # show config file path`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newListCommand(),
		newGetCommand(),
		newSetCommand(),
		newPathCommand(),
	)

	return cmd
}
