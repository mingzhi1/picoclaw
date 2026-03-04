package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ShellInstance consolidates all shell execution: Go built-in commands
// and dev-tool passthrough via ExecTool. This is the single source of truth
// for shell execution, used by both /shell slash command and any other callers.
type ShellInstance struct {
	execTool  *ExecTool
	workspace string
	restrict  bool
	maxOutput int
}

// NewShellInstance creates a ShellInstance backed by the given ExecTool.
// When restrict is true, builtin commands are confined to the workspace.
func NewShellInstance(execTool *ExecTool, workspace string, restrict bool) *ShellInstance {
	return &ShellInstance{
		execTool:  execTool,
		workspace: workspace,
		restrict:  restrict,
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

		// When workspace restriction is active, validate that the builtin
		// command arguments do not reference paths outside the workspace.
		if s.restrict && s.workspace != "" {
			if err := s.guardBuiltinArgs(baseCmd, cmdArgs); err != "" {
				return s.formatOutput(err)
			}
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

// guardBuiltinArgs checks that all path-like arguments of a builtin command
// resolve to paths inside the workspace. Returns an error string if any
// argument would escape the workspace; empty string means all OK.
func (s *ShellInstance) guardBuiltinArgs(cmd string, args []string) string {
	absWorkspace, err := filepath.Abs(s.workspace)
	if err != nil {
		return fmt.Sprintf("❌ %s: cannot resolve workspace: %v", cmd, err)
	}

	for _, arg := range args {
		// Skip flags
		if strings.HasPrefix(arg, "-") {
			continue
		}

		resolved := ResolvePath(arg, absWorkspace)
		if !isWithinWorkspace(resolved, absWorkspace) {
			return fmt.Sprintf("❌ %s: blocked — path '%s' is outside workspace", cmd, arg)
		}
	}
	return ""
}
