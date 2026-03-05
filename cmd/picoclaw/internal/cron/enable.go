package cron

import "github.com/spf13/cobra"

func newEnableCommand(workspace func() string) *cobra.Command {
	return &cobra.Command{
		Use:     "enable",
		Short:   "Enable a job",
		Args:    cobra.ExactArgs(1),
		Example: `picoclaw cron enable 1`,
		RunE: func(_ *cobra.Command, args []string) error {
			cronSetJobEnabled(workspace(), args[0], true)
			return nil
		},
	}
}
