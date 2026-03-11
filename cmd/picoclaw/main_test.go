package main

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
)

func TestNewPicoclawCommand(t *testing.T) {
	cmd := NewPicoclawCommand()

	require.NotNil(t, cmd)

	short := fmt.Sprintf("%s picoclaw - Personal AI Assistant v%s\n\n", internal.Logo, internal.GetVersion())

	assert.Equal(t, "picoclaw", cmd.Use)
	assert.Equal(t, short, cmd.Short)

	assert.True(t, cmd.HasSubCommands())
	assert.True(t, cmd.HasAvailableSubCommands())

	assert.False(t, cmd.HasFlags())

	assert.Nil(t, cmd.Run)
	assert.Nil(t, cmd.RunE)

	assert.Nil(t, cmd.PersistentPreRun)
	assert.Nil(t, cmd.PersistentPostRun)

	// Visible commands (shown in --help)
	visibleCommands := []string{
		"chat",
		"config",
		"cron",
		"doctor",
		"gateway",
		"onboard",
		"skills",
		"status",
		"version",
	}

	// Hidden commands (still work, just not in --help)
	hiddenCommands := []string{
		"agent",
		"auth",
		"migrate",
	}

	allExpected := append(visibleCommands, hiddenCommands...)

	// cobra's Commands() returns ALL commands including hidden ones
	allCommands := cmd.Commands()
	assert.Len(t, allCommands, len(allExpected))

	for _, subcmd := range allCommands {
		found := slices.Contains(allExpected, subcmd.Name())
		assert.True(t, found, "unexpected subcommand %q", subcmd.Name())

		if slices.Contains(hiddenCommands, subcmd.Name()) {
			assert.True(t, subcmd.Hidden, "command %q should be hidden", subcmd.Name())
		} else {
			assert.False(t, subcmd.Hidden, "command %q should not be hidden", subcmd.Name())
		}
	}

	// Check hidden commands are still callable
	for _, name := range hiddenCommands {
		_, _, err := cmd.Find([]string{name})
		assert.NoError(t, err, "hidden command %q should still be findable", name)
	}
}
