// Package tools — codex_exec tool
//
// Allows picoclaw to delegate coding tasks to Codex CLI.
// Flow: picoclaw (free LLM analysis) → codex exec (GPT-5.x execution)
//
// Use cases:
//   - "分析这段代码" → picoclaw analyzes → delegates fix to Codex
//   - "bugfix" → picoclaw identifies bug → Codex applies patch
//   - "重构" → picoclaw plans → Codex executes

package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sipeed/picoclaw/pkg/infra/logger"
)

// CodexExecTool delegates coding tasks to Codex CLI subprocess.
type CodexExecTool struct {
	workspace string
}

// NewCodexExecTool creates a codex_exec tool.
func NewCodexExecTool(workspace string) *CodexExecTool {
	return &CodexExecTool{workspace: workspace}
}

func (t *CodexExecTool) Name() string { return "codex_exec" }

func (t *CodexExecTool) Description() string {
	return "Delegate a coding task to Codex CLI (GPT-5.x). " +
		"Use for code generation, bugfix, refactoring, and analysis that " +
		"requires file system access and sandboxed execution. " +
		"Codex will read/write files and run commands in the workspace."
}

func (t *CodexExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Coding task for Codex to execute",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Model to use (default: auto)",
			},
			"workspace": map[string]any{
				"type":        "string",
				"description": "Working directory (default: agent workspace)",
			},
		},
		"required": []string{"task"},
	}
}

func (t *CodexExecTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	task, _ := args["task"].(string)
	if strings.TrimSpace(task) == "" {
		return ErrorResult("task is required")
	}

	model, _ := args["model"].(string)
	workspace, _ := args["workspace"].(string)
	if workspace == "" {
		workspace = t.workspace
	}

	// Find codex binary
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return ErrorResult("codex CLI not found in PATH. Install: npm i -g @openai/codex")
	}

	logger.InfoCF("codex_exec", "Delegating task to Codex", map[string]any{
		"task_preview": truncateStr(task, 100),
		"model":        model,
		"workspace":    workspace,
	})

	// Build codex exec command
	cmdArgs := []string{
		"exec", "--json",
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
		"--color", "never",
	}
	if model != "" {
		cmdArgs = append(cmdArgs, "-m", model)
	}
	if workspace != "" {
		cmdArgs = append(cmdArgs, "-C", workspace)
	}
	cmdArgs = append(cmdArgs, "-") // read from stdin

	cmd := exec.CommandContext(ctx, codexPath, cmdArgs...)
	cmd.Stdin = bytes.NewReader([]byte(task))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Parse JSONL output
	if stdoutStr := stdout.String(); stdoutStr != "" {
		result := parseCodexOutput(stdoutStr)
		if result != "" {
			return SilentResult(result)
		}
	}

	if err != nil {
		if ctx.Err() == context.Canceled {
			return ErrorResult("codex execution canceled")
		}
		errMsg := stderr.String()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		return ErrorResult(fmt.Sprintf("codex error: %s", errMsg))
	}

	return SilentResult("Codex completed (no output)")
}

// parseCodexOutput extracts text from codex JSONL events.
func parseCodexOutput(output string) string {
	var parts []string
	var usage string

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Item  *struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Command string `json:"command"`
				Output  string `json:"output"`
				Status  string `json:"status"`
			} `json:"item"`
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}

		switch event.Type {
		case "item.completed":
			if event.Item == nil {
				continue
			}
			switch event.Item.Type {
			case "agent_message":
				if event.Item.Text != "" {
					parts = append(parts, event.Item.Text)
				}
			case "tool_call":
				if event.Item.Command != "" {
					parts = append(parts, fmt.Sprintf("```\n$ %s\n```", event.Item.Command))
				}
			case "tool_output":
				if event.Item.Output != "" {
					out := event.Item.Output
					if len(out) > 500 {
						out = out[:500] + "..."
					}
					parts = append(parts, fmt.Sprintf("Output:\n```\n%s\n```", out))
				}
			}
		case "turn.completed":
			if event.Usage != nil {
				usage = fmt.Sprintf("\n[Codex tokens: %d in, %d out]",
					event.Usage.InputTokens, event.Usage.OutputTokens)
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n") + usage
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
