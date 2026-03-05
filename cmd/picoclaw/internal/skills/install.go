package skills

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	"github.com/sipeed/picoclaw/pkg/skills"
)

func newInstallCommand(installerFn func() (*skills.SkillInstaller, error)) *cobra.Command {
	var registry string
	var zipPath string
	var name string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install skill from GitHub, registry, or local ZIP",
		Example: `
picoclaw skills install sipeed/picoclaw-skills/weather
picoclaw skills install --registry clawhub agent-browser
picoclaw skills install --zip ./agent-browser.zip --name agent-browser
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if zipPath != "" {
				if len(args) != 0 {
					return fmt.Errorf("no positional arguments expected when --zip is set")
				}
				return nil
			}
			if len(args) != 1 {
				if registry != "" {
					return fmt.Errorf("exactly 1 argument (slug) is required when --registry is set")
				}
				return fmt.Errorf("exactly 1 argument is required: <github-repo> or --registry <name> <slug>")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if zipPath != "" {
				cfg, err := internal.LoadConfig()
				if err != nil {
					return err
				}
				return skillsInstallFromZip(cfg, zipPath, name)
			}

			if registry != "" {
				cfg, err := internal.LoadConfig()
				if err != nil {
					return err
				}

				return skillsInstallFromRegistry(cfg, registry, args[0])
			}

			installer, err := installerFn()
			if err != nil {
				return err
			}

			return skillsInstallCmd(installer, args[0])
		},
	}

	cmd.Flags().StringVar(&registry, "registry", "", "Install from a skill registry (e.g. clawhub)")
	cmd.Flags().StringVar(&zipPath, "zip", "", "Install from a local ZIP file")
	cmd.Flags().StringVar(&name, "name", "", "Skill directory name (required with --zip)")

	return cmd
}

