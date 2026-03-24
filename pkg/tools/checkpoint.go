package tools

import (
	"context"
	"fmt"
	"strings"
)

// CheckpointUpdater is the interface for runtime checkpoint management.
type CheckpointUpdater interface {
	AddStep(channelKey, text string, priority int)
	MarkStepDone(channelKey string, index int) error
	MarkStepFailed(channelKey string, index int, reason string) error
	MarkStepSkipped(channelKey string, index int, reason string) error
	GetStatus(channelKey string) ([]CheckpointStepInfo, int, int)
}

// CheckpointStepInfo is a read-only view of a checkpoint.
type CheckpointStepInfo struct {
	Text       string
	Skippable  bool
	Completed  bool
	Failed     bool
	FailReason string
	Skipped    bool
	SkipReason string
}

// CheckpointTool lets the LLM manage execution checkpoints at runtime.
type CheckpointTool struct {
	updater    CheckpointUpdater
	channelKey string
}

// NewCheckpointTool creates a new checkpoint management tool.
func NewCheckpointTool(updater CheckpointUpdater) *CheckpointTool {
	return &CheckpointTool{updater: updater}
}

func (t *CheckpointTool) Name() string { return "checkpoint" }

func (t *CheckpointTool) Description() string {
	return "Manage execution checkpoints. " +
		"'done' = passed, 'fail' = failed, " +
		"'skip' = skip (optional only), " +
		"'add' = new checkpoint, 'status' = review."
}

func (t *CheckpointTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"add", "done", "fail", "skip", "status"},
				"description": "Action to perform",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text (add) or reason (fail/skip)",
			},
			"step_number": map[string]any{
				"type":        "integer",
				"description": "1-based checkpoint number",
			},
		},
		"required": []string{"action"},
	}
}

func (t *CheckpointTool) SetContext(channel, chatID string) {
	t.channelKey = channel + ":" + chatID
}

func (t *CheckpointTool) Execute(_ context.Context, args map[string]any) *ToolResult {
	if t.updater == nil {
		return ErrorResult("checkpoint tracking not available")
	}
	action, _ := args["action"].(string)
	switch action {
	case "add":
		text, _ := args["text"].(string)
		if text == "" {
			return ErrorResult("text required for 'add'")
		}
		t.updater.AddStep(t.channelKey, text, 0)
		return SilentResult(fmt.Sprintf("Added checkpoint: %s", text))
	case "done":
		return t.execMark(args, "done")
	case "fail":
		return t.execMark(args, "fail")
	case "skip":
		return t.execMark(args, "skip")
	case "status":
		return t.execStatus()
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *CheckpointTool) execMark(args map[string]any, kind string) *ToolResult {
	num, ok := args["step_number"].(float64)
	if !ok || num < 1 {
		return ErrorResult("step_number (>= 1) required")
	}
	idx := int(num) - 1
	reason, _ := args["text"].(string)
	var err error
	switch kind {
	case "done":
		err = t.updater.MarkStepDone(t.channelKey, idx)
	case "fail":
		if reason == "" {
			reason = "execution failed"
		}
		err = t.updater.MarkStepFailed(t.channelKey, idx, reason)
	case "skip":
		if reason == "" {
			reason = "not applicable"
		}
		err = t.updater.MarkStepSkipped(t.channelKey, idx, reason)
	}
	if err != nil {
		return ErrorResult(err.Error())
	}
	return SilentResult(fmt.Sprintf("Checkpoint %d: %s", int(num), kind))
}

func (t *CheckpointTool) execStatus() *ToolResult {
	steps, completed, total := t.updater.GetStatus(t.channelKey)
	if total == 0 {
		return SilentResult("No active checkpoints.")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Checkpoints: %d/%d passed\n", completed, total)
	for i, s := range steps {
		mark := "[ ]"
		suffix := ""
		opt := ""
		if s.Skippable {
			opt = " (optional)"
		}
		if s.Skipped {
			mark = "[-]"
			if s.SkipReason != "" {
				suffix = " — " + s.SkipReason
			}
		} else if s.Failed {
			mark = "[!]"
			if s.FailReason != "" {
				suffix = " ⚠ " + s.FailReason
			}
		} else if s.Completed {
			mark = "[x]"
		}
		fmt.Fprintf(&sb, "%d. %s %s%s%s\n", i+1, mark, s.Text, opt, suffix)
	}
	return SilentResult(sb.String())
}
