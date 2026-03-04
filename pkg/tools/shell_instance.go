package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// ShellInstance consolidates all shell execution: Go built-in commands
// and dev-tool passthrough via ExecTool. This is the single source of truth
// for shell execution, used by both /shell slash command and any other callers.
type ShellInstance struct {
	execTool  *ExecTool
	workspace string
	maxOutput int
}

// NewShellInstance creates a ShellInstance backed by the given ExecTool.
func NewShellInstance(execTool *ExecTool, workspace string) *ShellInstance {
	return &ShellInstance{
		execTool:  execTool,
		workspace: workspace,
		maxOutput: 4000,
	}
}

// Execute runs a shell command: first tries Go built-in, then dev-tool passthrough.
// Returns formatted output suitable for display.
func (s *ShellInstance) Execute(args []string) string {
	if len(args) == 0 {
		return "Usage: <command> [args...]\n" +
			"  Built-in: ls, cat, head, tail, grep, wc, find, diff, tree, stat, pwd, echo\n" +
			"  Dev tools: go, git, node, python, npm, cargo, make\n" +
			"  File ops: touch, mkdir, cp, mv"
	}

	baseCmd := strings.ToLower(args[0])
	cmdArgs := args[1:]

	// 1. Try Go built-in (cross-platform, no system shell needed).
	if handler, ok := BuiltinCmds[baseCmd]; ok {
		cwd := s.workspace
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		return s.formatOutput(handler(cmdArgs, cwd))
	}

	// 2. Try dev-tool passthrough via ExecTool (reuses its guardCommand security).
	if DevToolPassthrough[baseCmd] {
		return s.execPassthrough(args)
	}

	return fmt.Sprintf("❌ Unknown command '%s'. Supported: built-in (ls, cat, grep...) + dev tools (go, git, python...)", baseCmd)
}

// execPassthrough delegates to ExecTool, reusing its guardCommand security checks.
// No separate deny list needed — ExecTool's guards are the single source of truth.
func (s *ShellInstance) execPassthrough(args []string) string {
	if s.execTool == nil {
		return "⚠️ Dev tool passthrough not available (no exec tool)"
	}

	command := strings.Join(args, " ")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := s.execTool.Execute(ctx, map[string]any{
		"command": command,
	})

	if result.IsError || result.Err != nil {
		errMsg := result.ForLLM
		if errMsg == "" && result.Err != nil {
			errMsg = result.Err.Error()
		}
		return fmt.Sprintf("❌ %s", errMsg)
	}
	return s.formatOutput(result.ForLLM)
}

func (s *ShellInstance) formatOutput(output string) string {
	if output == "" {
		return "✅ (no output)"
	}
	if len(output) > s.maxOutput {
		output = output[:s.maxOutput] + fmt.Sprintf("\n... (truncated, %d chars total)", len(output))
	}
	return "```\n" + output + "\n```"
}
