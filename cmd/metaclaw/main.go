// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal"
	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal/agent"
	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal/auth"
	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal/cfgcmd"
	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal/cron"
	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal/doctor"
	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal/gateway"
	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal/onboard"
	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal/skills"
	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal/status"
	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal/version"
)

func NewPicoclawCommand() *cobra.Command {
	short := fmt.Sprintf("%s metaclaw - Personal AI Assistant v%s\n\n", internal.Logo, internal.GetVersion())

	cmd := &cobra.Command{
		Use:   "metaclaw",
		Short: short,
		Example: `  metaclaw init                  # first-time setup
	metaclaw chat "What is Go?"     # quick one-shot chat
	metaclaw start                  # launch gateway
	metaclaw doctor                 # check config & environment
	metaclaw config list            # show all settings
	metaclaw config set <key> <val> # modify config (git-style)`,
	}

	// --- Core commands with git-style aliases ---

	// init = onboard (git init style)
	initCmd := onboard.NewOnboardCommand()
	initCmd.Aliases = append(initCmd.Aliases, "init", "setup")

	// start = gateway (git-daemon style)
	startCmd := gateway.NewGatewayCommand()
	startCmd.Aliases = append(startCmd.Aliases, "start", "serve", "up")

	// chat = agent shortcut for quick messages
	chatCmd := newChatCommand()

	// --- Hidden commands (still work, just don't clutter --help) ---
	agentCmd := agent.NewAgentCommand()
	agentCmd.Aliases = append(agentCmd.Aliases, "a")
	agentCmd.Hidden = true // use "chat" instead

	authCmd := auth.NewAuthCommand()
	authCmd.Hidden = true

	cmd.AddCommand(
		initCmd,
		chatCmd,
		agentCmd,
		authCmd,
		startCmd,
		doctor.NewDoctorCommand(),
		cfgcmd.NewConfigCommand(),
		status.NewStatusCommand(),
		cron.NewCronCommand(),
		skills.NewSkillsCommand(),
		version.NewVersionCommand(),
	)

	// Disable the auto-generated "completion" command
	cmd.CompletionOptions.DisableDefaultCmd = true

	return cmd
}

// newChatCommand creates a git-style "chat" shortcut.
// Usage: metaclaw chat "Hello!"  (instead of metaclaw agent -m "Hello!")
func newChatCommand() *cobra.Command {
	var (
		sessionKey string
		model      string
	)

	cmd := &cobra.Command{
		Use:     "chat [message]",
		Aliases: []string{"c", "ask"},
		Short:   "Send a message to the agent (shortcut for 'agent -m')",
		Args:    cobra.MaximumNArgs(1),
		Example: `  metaclaw chat "What is the meaning of life?"
  metaclaw ask "Translate this to Chinese"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var message string
			if len(args) > 0 {
				message = args[0]
			}
			// Delegate to the agent package's exported function
			return agent.RunAgent(message, sessionKey, model, false)
		},
	}

	cmd.Flags().StringVarP(&sessionKey, "session", "s", "cli:default", "Session key")
	cmd.Flags().StringVarP(&model, "model", "", "", "Model to use")

	return cmd
}

func main() {
	cmd := NewPicoclawCommand()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
