// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/sipeed/picoclaw/pkg/infra/logger"
	"github.com/sipeed/picoclaw/pkg/tools"
)

const (
	// maxCheckpoints is the maximum number of checkpoints allowed per plan.
	// This limit applies to both Analyser-generated and dynamically added checkpoints.
	maxCheckpoints = 7
	// maxCheckpointTextLen is the maximum length of checkpoint text in runes.
	maxCheckpointTextLen = 120
)

// checkpointDangerousPatterns are substrings that should never appear in checkpoint text.
// If the LLM is tricked into echoing dangerous commands, they are rejected.
var checkpointDangerousPatterns = []string{
	"rm -rf", "rm -r", "rmdir", "del /", "format c:",
	"sudo ", "DROP TABLE", "DROP DATABASE", "DELETE FROM",
	"shutdown", "mkfs", "dd if=", "> /dev/",
	"删除所有", "删除全部", "格式化",
}

// Checkpoint represents a single verification point in the LLM's execution plan.
type Checkpoint struct {
	Text       string
	Skippable  bool // set by Phase 1 Analyser — true for optional steps
	Completed  bool
	Failed     bool
	FailReason string
	Skipped    bool
	SkipReason string
}

// CheckpointPlan is the execution plan for a single task.
type CheckpointPlan struct {
	Steps          []Checkpoint
	CompletedCount int
	FailedCount    int
	SkippedCount   int
}

// CheckpointTracker manages execution checkpoints across turns.
//
// Flow:
//  1. Phase 1 Analyser → tracker.Begin(items)
//  2. Phase 2 prompt   ← tracker.FormatChecklist()
//  3. Phase 2 runtime  → LLM calls checkpoint tool (done/fail/skip)
//  4. Phase 3 sync     → tracker.Evaluate(input) auto-detects
//  5. Next turn        ← tracker.FormatProgress()
type CheckpointTracker struct {
	plans map[string]*CheckpointPlan
	mu    sync.RWMutex
}

// NewCheckpointTracker creates a new tracker.
func NewCheckpointTracker() *CheckpointTracker {
	return &CheckpointTracker{plans: make(map[string]*CheckpointPlan)}
}

// Begin starts a new plan from Phase 1 analysis output.
func (ct *CheckpointTracker) Begin(channelKey string, items []CheckpointItem) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if len(items) == 0 {
		delete(ct.plans, channelKey)
		return
	}
	plan := &CheckpointPlan{Steps: make([]Checkpoint, len(items))}
	for i, item := range items {
		plan.Steps[i] = Checkpoint{Text: item.Text, Skippable: item.Skippable}
	}
	ct.plans[channelKey] = plan
}

// FormatChecklist for Phase 2 system prompt injection.
func (ct *CheckpointTracker) FormatChecklist(key string) string {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	plan := ct.plans[key]
	if plan == nil || len(plan.Steps) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Execution Checkpoints\n\n")
	sb.WriteString("Work through these checkpoints in order.\n")
	sb.WriteString("Use the checkpoint tool to mark each done/fail/skip.\n\n")
	for i, cp := range plan.Steps {
		mark := "[ ]"
		suffix := ""
		if cp.Skipped {
			mark = "[-]"
			if cp.SkipReason != "" {
				suffix = " — " + cp.SkipReason
			}
		} else if cp.Failed {
			mark = "[!]"
			if cp.FailReason != "" {
				suffix = " ⚠ " + cp.FailReason
			}
		} else if cp.Completed {
			mark = "[x]"
		}
		opt := ""
		if cp.Skippable {
			opt = " (optional)"
		}
		fmt.Fprintf(&sb, "%d. %s %s%s%s\n", i+1, mark, cp.Text, opt, suffix)
	}
	return sb.String()
}

// Evaluate auto-detects completion based on Phase 2 tool calls + reply.
// It recalculates all counters (CompletedCount, FailedCount, SkippedCount) to ensure consistency.
func (ct *CheckpointTracker) Evaluate(key string, input RuntimeInput) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	plan := ct.plans[key]
	if plan == nil || len(plan.Steps) == 0 {
		return
	}

	toolNames := make(map[string]bool, len(input.ToolCalls))
	for _, tc := range input.ToolCalls {
		toolNames[strings.ToLower(tc.Name)] = true
	}
	replyLower := strings.ToLower(input.AssistantReply)

	// Recalculate all counters from scratch to ensure consistency.
	completed := 0
	failed := 0
	skipped := 0

	for i := range plan.Steps {
		if plan.Steps[i].Completed || plan.Steps[i].Failed || plan.Steps[i].Skipped {
			// Already resolved, count it.
			if plan.Steps[i].Completed {
				completed++
			} else if plan.Steps[i].Failed {
				failed++
			} else if plan.Steps[i].Skipped {
				skipped++
			}
			continue
		}
		// Auto-detect completion based on tool calls + reply.
		if matchStep(strings.ToLower(plan.Steps[i].Text), toolNames, replyLower) {
			plan.Steps[i].Completed = true
			completed++
		}
	}
	plan.CompletedCount = completed
	plan.FailedCount = failed
	plan.SkippedCount = skipped

	logger.InfoCF("checkpoint", "Evaluated plan",
		map[string]any{"channel": key, "completed": completed, "failed": failed, "skipped": skipped, "total": len(plan.Steps)})
}

// FormatProgress for next-turn context injection.
func (ct *CheckpointTracker) FormatProgress(key string) string {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	plan := ct.plans[key]
	if plan == nil || len(plan.Steps) == 0 {
		return ""
	}
	if plan.CompletedCount == 0 && plan.FailedCount == 0 && plan.SkippedCount == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Previous Checkpoints (%d/%d passed", plan.CompletedCount, len(plan.Steps))
	if plan.FailedCount > 0 {
		fmt.Fprintf(&sb, ", %d failed", plan.FailedCount)
	}
	if plan.SkippedCount > 0 {
		fmt.Fprintf(&sb, ", %d skipped", plan.SkippedCount)
	}
	sb.WriteString(")\n\n")

	for i, cp := range plan.Steps {
		mark := "⏳"
		suffix := ""
		if cp.Skipped {
			mark = "⏭"
			if cp.SkipReason != "" {
				suffix = " — " + cp.SkipReason
			}
		} else if cp.Failed {
			mark = "⛔"
			if cp.FailReason != "" {
				suffix = " — " + cp.FailReason
			}
		} else if cp.Completed {
			mark = "✅"
		}
		fmt.Fprintf(&sb, "%d. %s %s%s\n", i+1, mark, cp.Text, suffix)
	}

	resolved := plan.CompletedCount + plan.SkippedCount + plan.FailedCount
	if resolved == len(plan.Steps) {
		sb.WriteString("\n_All checkpoints resolved._\n")
	} else {
		pending := len(plan.Steps) - resolved
		if pending > 0 {
			fmt.Fprintf(&sb, "\n_%d checkpoints remaining._\n", pending)
		}
	}
	return sb.String()
}

// HasPending returns true if there are unresolved checkpoints.
func (ct *CheckpointTracker) HasPending(key string) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	plan := ct.plans[key]
	if plan == nil {
		return false
	}
	resolved := plan.CompletedCount + plan.FailedCount + plan.SkippedCount
	return resolved < len(plan.Steps)
}

// CompactSummary returns a terse one-line checkpoint status for TurnRecord injection.
// Example: "3/5 passed, 1 failed (Write handler: build error), 1 skipped"
// Returns "" if no plan exists or no progress yet.
func (ct *CheckpointTracker) CompactSummary(key string) string {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	plan := ct.plans[key]
	if plan == nil || len(plan.Steps) == 0 {
		return ""
	}
	if plan.CompletedCount == 0 && plan.FailedCount == 0 && plan.SkippedCount == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d/%d passed", plan.CompletedCount, len(plan.Steps))

	if plan.FailedCount > 0 {
		// Include first failed step detail.
		var failDetail string
		for _, s := range plan.Steps {
			if s.Failed {
				failDetail = s.Text
				if s.FailReason != "" {
					failDetail += ": " + s.FailReason
				}
				break
			}
		}
		fmt.Fprintf(&sb, ", %d failed", plan.FailedCount)
		if failDetail != "" {
			fmt.Fprintf(&sb, " (%s)", failDetail)
		}
	}
	if plan.SkippedCount > 0 {
		fmt.Fprintf(&sb, ", %d skipped", plan.SkippedCount)
	}

	pending := len(plan.Steps) - plan.CompletedCount - plan.FailedCount - plan.SkippedCount
	if pending > 0 {
		fmt.Fprintf(&sb, ", %d pending", pending)
	}
	return sb.String()
}

// Clear removes the plan for a channel.
func (ct *CheckpointTracker) Clear(key string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	delete(ct.plans, key)
}

// --- CheckpointUpdater interface (used by tools.CheckpointTool) ---

// AddStep adds a new checkpoint step at runtime.
// It enforces the same security limits as Analyser-generated checkpoints:
// - Maximum 7 checkpoints per plan
// - Maximum 120 runes per step text
// - Dangerous command patterns are filtered
func (ct *CheckpointTracker) AddStep(key, text string, _ int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	plan := ct.plans[key]
	if plan == nil {
		plan = &CheckpointPlan{}
		ct.plans[key] = plan
	}

	// Enforce maximum checkpoints limit.
	if len(plan.Steps) >= maxCheckpoints {
		logger.WarnCF("checkpoint", "Rejected AddStep: maximum limit reached",
			map[string]any{"channel": key, "limit": maxCheckpoints})
		return
	}

	// Sanitize text: trim and truncate.
	text = strings.TrimSpace(text)
	if len([]rune(text)) > maxCheckpointTextLen {
		text = string([]rune(text)[:maxCheckpointTextLen])
	}
	if text == "" {
		logger.WarnCF("checkpoint", "Rejected AddStep: empty text",
			map[string]any{"channel": key})
		return
	}

	// Defense-in-depth: reject checkpoint items containing dangerous commands.
	lower := strings.ToLower(text)
	for _, pat := range checkpointDangerousPatterns {
		if strings.Contains(lower, strings.ToLower(pat)) {
			logger.WarnCF("checkpoint", "Rejected AddStep: dangerous pattern detected",
				map[string]any{"channel": key, "pattern": pat})
			return
		}
	}

	// New steps are required (not skippable) by default.
	plan.Steps = append(plan.Steps, Checkpoint{Text: text, Skippable: false})
	logger.InfoCF("checkpoint", "Step added", map[string]any{"channel": key, "text": text})
}

func (ct *CheckpointTracker) MarkStepDone(key string, idx int) error {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	plan := ct.plans[key]
	if plan == nil {
		return fmt.Errorf("no active checkpoint plan")
	}
	if idx < 0 || idx >= len(plan.Steps) {
		return fmt.Errorf("checkpoint %d out of range (1-%d)", idx+1, len(plan.Steps))
	}
	if plan.Steps[idx].Completed {
		return nil
	}
	plan.Steps[idx].Completed = true
	plan.CompletedCount++
	return nil
}

func (ct *CheckpointTracker) MarkStepFailed(key string, idx int, reason string) error {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	plan := ct.plans[key]
	if plan == nil {
		return fmt.Errorf("no active checkpoint plan")
	}
	if idx < 0 || idx >= len(plan.Steps) {
		return fmt.Errorf("checkpoint %d out of range (1-%d)", idx+1, len(plan.Steps))
	}
	if plan.Steps[idx].Failed {
		return nil
	}
	if plan.Steps[idx].Completed {
		plan.Steps[idx].Completed = false
		plan.CompletedCount--
	}
	plan.Steps[idx].Failed = true
	plan.Steps[idx].FailReason = reason
	plan.FailedCount++
	return nil
}

func (ct *CheckpointTracker) MarkStepSkipped(key string, idx int, reason string) error {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	plan := ct.plans[key]
	if plan == nil {
		return fmt.Errorf("no active checkpoint plan")
	}
	if idx < 0 || idx >= len(plan.Steps) {
		return fmt.Errorf("checkpoint %d out of range (1-%d)", idx+1, len(plan.Steps))
	}
	// Only skippable checkpoints can be skipped.
	if !plan.Steps[idx].Skippable {
		return fmt.Errorf("checkpoint %d (%s) is required and cannot be skipped", idx+1, plan.Steps[idx].Text)
	}
	if plan.Steps[idx].Skipped {
		return nil
	}
	if plan.Steps[idx].Completed {
		plan.Steps[idx].Completed = false
		plan.CompletedCount--
	}
	if plan.Steps[idx].Failed {
		plan.Steps[idx].Failed = false
		plan.FailedCount--
	}
	plan.Steps[idx].Skipped = true
	plan.Steps[idx].SkipReason = reason
	plan.SkippedCount++
	return nil
}

func (ct *CheckpointTracker) GetStatus(key string) ([]tools.CheckpointStepInfo, int, int) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	plan := ct.plans[key]
	if plan == nil {
		return nil, 0, 0
	}
	steps := make([]tools.CheckpointStepInfo, len(plan.Steps))
	for i, s := range plan.Steps {
		steps[i] = tools.CheckpointStepInfo{
			Text:       s.Text,
			Skippable:  s.Skippable,
			Completed:  s.Completed,
			Failed:     s.Failed,
			FailReason: s.FailReason,
			Skipped:    s.Skipped,
			SkipReason: s.SkipReason,
		}
	}
	return steps, plan.CompletedCount, len(plan.Steps)
}




// matchStep determines if a checkpoint was completed based on tool calls + reply.
func matchStep(stepLower string, toolNames map[string]bool, replyLower string) bool {
	actionTools := []struct {
		keywords []string
		tools    []string
	}{
		{[]string{"read", "查看", "检查", "分析"}, []string{"read_file", "list_directory", "web_search", "fetch_url"}},
		{[]string{"write", "create", "add", "implement", "写", "创建", "添加"}, []string{"write_file", "edit_file", "create_file"}},
		{[]string{"test", "verify", "run", "execute", "测试", "运行"}, []string{"exec"}},
		{[]string{"search", "find", "搜索", "查找"}, []string{"web_search", "codebase_search", "grep_search"}},
		{[]string{"delete", "remove", "删除"}, []string{"exec", "write_file"}},
		{[]string{"install", "deploy", "安装", "部署"}, []string{"exec"}},
	}

	for _, at := range actionTools {
		stepMatches := false
		for _, kw := range at.keywords {
			if strings.Contains(stepLower, kw) {
				stepMatches = true
				break
			}
		}
		if !stepMatches {
			continue
		}
		for _, tool := range at.tools {
			if toolNames[tool] {
				return true
			}
		}
	}

	// Fallback: significant words from step appear in reply.
	words := extractSignificantWords(stepLower)
	if len(words) == 0 {
		return false
	}
	matchCount := 0
	threshold := len(words) / 2
	if threshold < 2 {
		threshold = 2
	}
	for _, w := range words {
		if strings.Contains(replyLower, w) {
			matchCount++
		}
	}
	return matchCount >= threshold
}

func extractSignificantWords(s string) []string {
	stop := map[string]bool{
		"the": true, "and": true, "for": true, "with": true,
		"that": true, "this": true, "from": true, "into": true,
		"will": true, "should": true, "each": true, "them": true,
	}
	var result []string
	for _, w := range strings.Fields(s) {
		w = strings.Trim(w, ".,;:!?\"'`()[]{}") // strip punctuation
		if len(w) > 3 && !stop[w] {
			result = append(result, w)
		}
	}
	return result
}
