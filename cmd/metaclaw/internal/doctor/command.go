package doctor

import (
	"github.com/spf13/cobra"
)

func NewDoctorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "doctor",
		Aliases: []string{"check", "diag"},
		Short:   "Check MetaClaw configuration and connectivity",
		Example: "  metaclaw doctor",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor()
		},
	}

	return cmd
}
