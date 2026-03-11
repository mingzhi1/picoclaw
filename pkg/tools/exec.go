package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/infra/config"
	"github.com/sipeed/picoclaw/pkg/infra/logger"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type ExecTool struct {
	workingDir          string
	timeout             time.Duration
	denyPatterns        []*regexp.Regexp
	allowPatterns       []*regexp.Regexp
	customAllowPatterns []*regexp.Regexp
	restrictToWorkspace bool
	llmReviewer         *LLMCommandReviewer // optional LLM-based semantic review
}

var (
	defaultDenyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\brm\s+-[rf]{1,2}\b`),
		regexp.MustCompile(`\bdel\s+/[fq]\b`),
		regexp.MustCompile(`\brmdir\s+/s\b`),
		// Match disk wiping commands (must be followed by space/args)
		regexp.MustCompile(
			`\b(format|mkfs|diskpart)\b\s`,
		),
		regexp.MustCompile(`\bdd\s+if=`),
		// Block writes to block devices (all common naming schemes).
		regexp.MustCompile(
			`>\s*/dev/(sd[a-z]|hd[a-z]|vd[a-z]|xvd[a-z]|nvme\d|mmcblk\d|loop\d|dm-\d|md\d|sr\d|nbd\d)`,
		),
		regexp.MustCompile(`\b(shutdown|reboot|poweroff)\b`),
		regexp.MustCompile(`:\(\)\s*\{.*\};\s*:`),
		regexp.MustCompile(`\$\([^)]+\)`),
		regexp.MustCompile(`\$\{[^}]+\}`),
		regexp.MustCompile("`[^`]+`"),
		regexp.MustCompile(`\|\s*sh\b`),
		regexp.MustCompile(`\|\s*bash\b`),
		regexp.MustCompile(`;\s*rm\s+-[rf]`),
		regexp.MustCompile(`&&\s*rm\s+-[rf]`),
		regexp.MustCompile(`\|\|\s*rm\s+-[rf]`),
		regexp.MustCompile(`<<\s*EOF`),
		regexp.MustCompile(`\$\(\s*cat\s+`),
		regexp.MustCompile(`\$\(\s*curl\s+`),
		regexp.MustCompile(`\$\(\s*wget\s+`),
		regexp.MustCompile(`\$\(\s*which\s+`),
		regexp.MustCompile(`\bsudo\b`),
		regexp.MustCompile(`\bchmod\s+[0-7]{3,4}\b`),
		regexp.MustCompile(`\bchown\b`),
		regexp.MustCompile(`\bpkill\b`),
		regexp.MustCompile(`\bkillall\b`),
		regexp.MustCompile(`\bkill\s+-[9]\b`),
		regexp.MustCompile(`\bcurl\b.*\|\s*(sh|bash)`),
		regexp.MustCompile(`\bwget\b.*\|\s*(sh|bash)`),
		regexp.MustCompile(`\bnpm\s+install\s+-g\b`),
		regexp.MustCompile(`\bpip\s+install\s+--user\b`),
		regexp.MustCompile(`\bapt\s+(install|remove|purge)\b`),
		regexp.MustCompile(`\byum\s+(install|remove)\b`),
		regexp.MustCompile(`\bdnf\s+(install|remove)\b`),
		regexp.MustCompile(`\bdocker\s+run\b`),
		regexp.MustCompile(`\bdocker\s+exec\b`),
		regexp.MustCompile(`\bgit\s+push\b`),
		regexp.MustCompile(`\bgit\s+force\b`),
		regexp.MustCompile(`\bssh\b.*@`),
		regexp.MustCompile(`\beval\b`),
		regexp.MustCompile(`\bsource\s+.*\.sh\b`),
	}

	// absolutePathPattern matches absolute file paths in commands (Unix and Windows).
	absolutePathPattern = regexp.MustCompile(`[A-Za-z]:\\[^\\\"']+|/[^\s\"']+`)

	// safePaths are kernel pseudo-devices that are always safe to reference in
	// commands, regardless of workspace restriction. They contain no user data
	// and cannot cause destructive writes.
	safePaths = map[string]bool{
		"/dev/null":    true,
		"/dev/zero":    true,
		"/dev/random":  true,
		"/dev/urandom": true,
		"/dev/stdin":   true,
		"/dev/stdout":  true,
		"/dev/stderr":  true,
	}
)

func NewExecTool(workingDir string, restrict bool) (*ExecTool, error) {
	return NewExecToolWithConfig(workingDir, restrict, nil)
}

func NewExecToolWithConfig(workingDir string, restrict bool, config *config.Config) (*ExecTool, error) {
	denyPatterns := make([]*regexp.Regexp, 0)
	customAllowPatterns := make([]*regexp.Regexp, 0)

	if config != nil {
		execConfig := config.Tools.Exec
		enableDenyPatterns := execConfig.EnableDenyPatterns
		if enableDenyPatterns {
			denyPatterns = append(denyPatterns, defaultDenyPatterns...)
			denyPatterns = append(denyPatterns, windowsDenyPatterns...)
			if len(execConfig.CustomDenyPatterns) > 0 {
				fmt.Printf("Using custom deny patterns: %v\n", execConfig.CustomDenyPatterns)
				for _, pattern := range execConfig.CustomDenyPatterns {
					re, err := regexp.Compile(pattern)
					if err != nil {
						return nil, fmt.Errorf("invalid custom deny pattern %q: %w", pattern, err)
					}
					denyPatterns = append(denyPatterns, re)
				}
			}
		} else {
			// If deny patterns are disabled, we won't add any patterns, allowing all commands.
			validateDenyConfig(false)
		}
		for _, pattern := range execConfig.CustomAllowPatterns {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid custom allow pattern %q: %w", pattern, err)
			}
			customAllowPatterns = append(customAllowPatterns, re)
		}
	} else {
		denyPatterns = append(denyPatterns, defaultDenyPatterns...)
		denyPatterns = append(denyPatterns, windowsDenyPatterns...)
	}

	return &ExecTool{
		workingDir:          workingDir,
		timeout:             60 * time.Second,
		denyPatterns:        denyPatterns,
		allowPatterns:       nil,
		customAllowPatterns: customAllowPatterns,
		restrictToWorkspace: restrict,
	}, nil
}

func (t *ExecTool) Name() string {
	return "exec"
}

func (t *ExecTool) Description() string {
	return "Execute a shell command and return its output. Use with caution."
}

func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"working_dir": map[string]any{
				"type":        "string",
				"description": "Optional working directory for the command",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	command, ok := args["command"].(string)
	if !ok {
		return ErrorResult("command is required")
	}

	cwd := t.workingDir
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		if t.restrictToWorkspace && t.workingDir != "" {
			resolvedWD, err := validatePath(wd, t.workingDir, true)
			if err != nil {
				return ErrorResult("Command blocked by safety guard (" + err.Error() + ")")
			}
			cwd = resolvedWD
		} else {
			cwd = wd
		}
	}

	if cwd == "" {
		wd, err := os.Getwd()
		if err == nil {
			cwd = wd
		}
	}

	// Fast path: try Go built-in commands before spawning a system shell.
	// Only when workspace restriction is off — restricted mode needs guardCommand
	// to enforce path security on command arguments.
	if !t.restrictToWorkspace {
		if baseCmd, cmdArgs, ok := parseSimpleCommand(command); ok {
			if handler, found := BuiltinCmds[strings.ToLower(baseCmd)]; found {
				output := handler(cmdArgs, cwd)
				if output == "" {
					output = "(no output)"
				}
				// Built-in commands signal errors by prefixing output with "cmdname: "
				isErr := strings.HasPrefix(output, strings.ToLower(baseCmd)+":")
				return &ToolResult{ForLLM: output, ForUser: output, IsError: isErr}
			}
		}
	}

	if guardError := t.guardCommand(command, cwd); guardError != "" {
		// Anti-social-engineering: do NOT include the blocked command in ForLLM.
		// If LLM sees the rejected command text, it may ask the user to run it manually,
		// turning the deny list into a social engineering vector (reject-then-retry attack).
		return &ToolResult{
			ForLLM: "Command blocked by security policy. " +
				"Do NOT ask the user to run this command manually. " +
				"Do NOT suggest alternative ways to execute the same operation. " +
				"Explain to the user that this operation is not permitted and suggest a safe alternative approach.",
			ForUser: fmt.Sprintf("⛔ 命令因安全策略被阻止: %s\n请勿手动执行此命令。", guardError),
			IsError: true,
		}
	}

	// LLM semantic review — advisory mode.
	// When the reviewer flags a command, we do NOT block it. Instead we:
	//   1. Show the command + risk reason to the user (ForUser)
	//   2. Tell the LLM it was not auto-executed (ForLLM)
	// The user can then decide to copy-paste and run it themselves.
	if t.llmReviewer != nil {
		review := t.llmReviewer.Review(ctx, command)
		if !review.Allowed {
			logger.InfoCF("exec", "Command flagged by LLM review — showing to user", map[string]any{
				"command": command,
				"reason":  review.Reason,
			})
			return &ToolResult{
				ForLLM: fmt.Sprintf(
					"Command was NOT auto-executed due to security review: %s\n"+
						"The command has been shown to the user. "+
						"If the user wants to run it, they will do so manually.",
					review.Reason,
				),
				ForUser: fmt.Sprintf(
					"⚠️ 风险提示: %s\n\n"+
						"命令未自动执行，请检查后手动运行:\n```\n%s\n```",
					review.Reason, command,
				),
				IsError: false,
			}
		}
	}

	// timeout == 0 means no timeout
	var cmdCtx context.Context
	var cancel context.CancelFunc
	if t.timeout > 0 {
		cmdCtx, cancel = context.WithTimeout(ctx, t.timeout)
	} else {
		cmdCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(cmdCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(cmdCtx, "sh", "-c", command)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}

	prepareCommandForTermination(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return ErrorResult(fmt.Sprintf("failed to start command: %v", err))
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var err error
	select {
	case err = <-done:
	case <-cmdCtx.Done():
		_ = terminateProcessTree(cmd)
		select {
		case err = <-done:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			err = <-done
		}
	}

	output := stdout.String()
	stderrStr := stderr.String()

	// On Windows, PowerShell outputs CJK text in GBK/CP936 encoding.
	// Decode to UTF-8 so the LLM receives readable error messages.
	if runtime.GOOS == "windows" {
		if !utf8.ValidString(output) {
			if decoded, err := simplifiedchinese.GBK.NewDecoder().String(output); err == nil {
				output = decoded
			}
		}
		if !utf8.ValidString(stderrStr) {
			if decoded, err := simplifiedchinese.GBK.NewDecoder().String(stderrStr); err == nil {
				stderrStr = decoded
			}
		}
	}

	if stderrStr != "" {
		output += "\nSTDERR:\n" + stderrStr
	}

	if err != nil {
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			msg := fmt.Sprintf("Command timed out after %v", t.timeout)
			return &ToolResult{
				ForLLM:  msg,
				ForUser: msg,
				IsError: true,
			}
		}

		// Detect "command not found" to give the LLM a clear, actionable error.
		if strings.Contains(stderrStr, "CommandNotFoundException") ||
			strings.Contains(stderrStr, "not recognized") ||
			strings.Contains(stderrStr, "command not found") ||
			strings.Contains(stderrStr, "not found") {
			// Extract the command name from the original command string
			cmdName := command
			if parts := strings.Fields(command); len(parts) > 0 {
				cmdName = parts[0]
			}
			msg := fmt.Sprintf("Command '%s' is not installed or not found in PATH. "+
				"Try an alternative approach (e.g. use web_fetch tool instead of browser CLI, "+
				"or suggest the user to install the missing command).\nOriginal error: %s",
				cmdName, stderrStr)
			return &ToolResult{
				ForLLM:  msg,
				ForUser: fmt.Sprintf("Command '%s' not found. It may need to be installed first.", cmdName),
				IsError: true,
			}
		}

		output += fmt.Sprintf("\nExit code: %v", err)
	}

	if output == "" {
		output = "(no output)"
	}

	maxLen := 10000
	if len(output) > maxLen {
		output = output[:maxLen] + fmt.Sprintf("\n... (truncated, %d more chars)", len(output)-maxLen)
	}

	if err != nil {
		return &ToolResult{
			ForLLM:  output,
			ForUser: output,
			IsError: true,
		}
	}

	return &ToolResult{
		ForLLM:  output,
		ForUser: output,
		IsError: false,
	}
}

func (t *ExecTool) guardCommand(command, cwd string) string {
	cmd := strings.TrimSpace(command)
	lower := strings.ToLower(cmd)

	// Custom allow patterns exempt a command from deny checks.
	explicitlyAllowed := false
	for _, pattern := range t.customAllowPatterns {
		if pattern.MatchString(lower) {
			explicitlyAllowed = true
			break
		}
	}

	if !explicitlyAllowed {
		for _, pattern := range t.denyPatterns {
			if pattern.MatchString(lower) {
				return "Command blocked by safety guard (dangerous pattern detected)"
			}
		}
	}

	if len(t.allowPatterns) > 0 {
		allowed := false
		for _, pattern := range t.allowPatterns {
			if pattern.MatchString(lower) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "Command blocked by safety guard (not in allowlist)"
		}
	}

	if t.restrictToWorkspace {
		if strings.Contains(cmd, "..\\") || strings.Contains(cmd, "../") {
			return "Command blocked by safety guard (path traversal detected)"
		}

		cwdPath, err := filepath.Abs(cwd)
		if err != nil {
			return ""
		}

		matches := absolutePathPattern.FindAllString(cmd, -1)

		for _, raw := range matches {
			p, err := filepath.Abs(raw)
			if err != nil {
				continue
			}

			if safePaths[p] {
				continue
			}

			rel, err := filepath.Rel(cwdPath, p)
			if err != nil {
				continue
			}

			if strings.HasPrefix(rel, "..") {
				return "Command blocked by safety guard (path outside working dir)"
			}
		}
	}

	return ""
}

func (t *ExecTool) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}

// SetLLMReviewer enables LLM-based semantic command review.
// When set, commands that pass the regex deny list are additionally
// reviewed by a lightweight LLM before execution.
func (t *ExecTool) SetLLMReviewer(reviewer *LLMCommandReviewer) {
	t.llmReviewer = reviewer
}

func (t *ExecTool) SetRestrictToWorkspace(restrict bool) {
	t.restrictToWorkspace = restrict
}

// WorkingDir returns the configured working directory.
func (t *ExecTool) WorkingDir() string {
	return t.workingDir
}

// RestrictToWorkspace returns whether workspace restriction is enabled.
func (t *ExecTool) RestrictToWorkspace() bool {
	return t.restrictToWorkspace
}

func (t *ExecTool) SetAllowPatterns(patterns []string) error {
	t.allowPatterns = make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("invalid allow pattern %q: %w", p, err)
		}
		t.allowPatterns = append(t.allowPatterns, re)
	}
	return nil
}

// parseSimpleCommand tries to parse a command string into baseCmd and args.
// Returns false if the command contains shell metacharacters (pipes, redirects,
// chains, subshells) that require a real shell to handle.
func parseSimpleCommand(command string) (baseCmd string, args []string, ok bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", nil, false
	}

	// Bail out for anything that needs a real shell.
	for _, meta := range []string{"|", "&&", "||", ";", ">", "<", "$(", "${", "`"} {
		if strings.Contains(command, meta) {
			return "", nil, false
		}
	}

	// Simple tokenizer: split on whitespace, respecting single/double quotes.
	var tokens []string
	var current strings.Builder
	inSingle, inDouble := false, false

	for i := 0; i < len(command); i++ {
		ch := command[i]
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case (ch == ' ' || ch == '\t') && !inSingle && !inDouble:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	if len(tokens) == 0 {
		return "", nil, false
	}

	return tokens[0], tokens[1:], true
}
