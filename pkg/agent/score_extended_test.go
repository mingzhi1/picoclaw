package agent

import (
	"strings"
	"testing"
)

// =============================================================================
// CalcTurnScore 测试
// =============================================================================

// TestCalcTurnScore_EmptyInput tests scoring with empty input
func TestCalcTurnScore_EmptyInput(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "",
		AssistantReply:   "",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Empty input should get negative score due to short content penalty
	if score != -2 {
		t.Errorf("expected score -2 for empty input, got %d", score)
	}
}

// TestCalcTurnScore_ChatOnly tests scoring for simple chat
func TestCalcTurnScore_ChatOnly(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "hello",
		AssistantReply:   "hi there!",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "chat",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Short chat should get negative score
	if score > 0 {
		t.Errorf("expected negative score for short chat, got %d", score)
	}
}

// TestCalcTurnScore_TaskWithTools tests scoring for task with tool calls
func TestCalcTurnScore_TaskWithTools(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "deploy to staging",
		AssistantReply:   "deployed successfully",
		ToolCalls:        []ToolCallRecord{{Name: "exec", Error: ""}},
		Intent:           "task",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Task (+3) + tool (+3) = 6
	if score < 4 {
		t.Errorf("expected score >= 4 for task+tool, got %d", score)
	}
}

// TestCalcTurnScore_WriteTool tests scoring with write tool
func TestCalcTurnScore_WriteTool(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "create a file",
		AssistantReply:   "created",
		ToolCalls:        []ToolCallRecord{{Name: "write_file", Error: ""}},
		Intent:           "code",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Code (+3) + tool (+3) + write (+2) = 8, minus short content (-2) = 6
	if score < 5 {
		t.Errorf("expected score >= 5 for write tool, got %d", score)
	}
}

// TestCalcTurnScore_ManyTools tests scoring with many tool calls
func TestCalcTurnScore_ManyTools(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "complex task",
		AssistantReply:   strings.Repeat("done ", 100),
		ToolCalls: []ToolCallRecord{
			{Name: "read_file"},
			{Name: "write_file"},
			{Name: "exec"},
			{Name: "web_search"},
			{Name: "grep_search"},
		},
		Intent:           "task",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Task (+3) + tools (+3) + write (+2) + many tools (+2) + long reply (+2) = 12
	if score < 10 {
		t.Errorf("expected score >= 10 for many tools, got %d", score)
	}
}

// TestCalcTurnScore_LongReply tests scoring with long reply
func TestCalcTurnScore_LongReply(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "explain",
		AssistantReply:   strings.Repeat("explanation ", 100), // > 500 chars
		ToolCalls:        []ToolCallRecord{},
		Intent:           "question",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Long reply should get +2
	if score < 3 {
		t.Errorf("expected score >= 3 for long reply, got %d", score)
	}
}

// TestCalcTurnScore_RememberKeyword tests scoring with remember keyword
func TestCalcTurnScore_RememberKeyword(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "请记住这个配置",
		AssistantReply:   "好的，已记住",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "chat",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Remember keyword should add +3
	if score < 1 {
		t.Errorf("expected score >= 1 for remember keyword, got %d", score)
	}
}

// TestCalcTurnScore_ImportantKeyword tests scoring with important keyword
func TestCalcTurnScore_ImportantKeyword(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "这是重要的信息",
		AssistantReply:   "明白了",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "chat",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Important keyword should add +3
	if score < 1 {
		t.Errorf("expected score >= 1 for important keyword, got %d", score)
	}
}

// TestCalcTurnScore_EnglishRemember tests scoring with English remember keyword
func TestCalcTurnScore_EnglishRemember(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "Please remember this",
		AssistantReply:   "Got it",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "chat",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	if score < 1 {
		t.Errorf("expected score >= 1 for English remember, got %d", score)
	}
}

// TestCalcTurnScore_CheckpointActivity tests scoring with checkpoint summary
func TestCalcTurnScore_CheckpointActivity(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "complete the task",
		AssistantReply:   "done",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "task",
		CheckpointSummary: "3/3 passed",
	}

	score := CalcTurnScore(input)
	// Task (+3) + checkpoint (+2) + completed (+3) = 8, minus short content (-2) = 6
	if score < 5 {
		t.Errorf("expected score >= 5 for completed checkpoints, got %d", score)
	}
}

// TestCalcTurnScore_CheckpointFailed tests scoring with failed checkpoints
func TestCalcTurnScore_CheckpointFailed(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "try the task",
		AssistantReply:   "failed at step 2",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "task",
		CheckpointSummary: "1/3 passed, 1 failed",
	}

	score := CalcTurnScore(input)
	// Failed checkpoint should add +1
	if score < 4 {
		t.Errorf("expected score >= 4 for failed checkpoint, got %d", score)
	}
}

// TestCalcTurnScore_DebugIntent tests scoring with debug intent
func TestCalcTurnScore_DebugIntent(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "fix the bug",
		AssistantReply:   "fixed",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "debug",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Debug intent should add +3
	if score < 1 {
		t.Errorf("expected score >= 1 for debug intent, got %d", score)
	}
}

// TestCalcTurnScore_CodeIntent tests scoring with code intent
func TestCalcTurnScore_CodeIntent(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "write code",
		AssistantReply:   "here is the code",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "code",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Code intent should add +3
	if score < 1 {
		t.Errorf("expected score >= 1 for code intent, got %d", score)
	}
}

// TestCalcTurnScore_QuestionIntent tests scoring with question intent
func TestCalcTurnScore_QuestionIntent(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "what is Go?",
		AssistantReply:   "Go is a programming language",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "question",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Question intent should add +1
	if score < -1 {
		t.Errorf("expected score >= -1 for question intent, got %d", score)
	}
}

// TestCalcTurnScore_AlwaysKeepThreshold tests the always_keep threshold
func TestCalcTurnScore_AlwaysKeepThreshold(t *testing.T) {
	// This turn should reach always_keep threshold (>= 7)
	input := RuntimeInput{
		UserMessage:      "deploy to production",
		AssistantReply:   strings.Repeat("done ", 100),
		ToolCalls: []ToolCallRecord{
			{Name: "write_file"},
			{Name: "exec"},
			{Name: "exec"},
			{Name: "exec"},
		},
		Intent:           "task",
		CheckpointSummary: "all passed",
	}

	score := CalcTurnScore(input)
	if score < alwaysKeepThreshold {
		t.Errorf("expected score >= %d for always_keep, got %d", alwaysKeepThreshold, score)
	}
}

// TestCalcTurnScore_CaseInsensitiveKeywords tests case insensitive keyword detection
func TestCalcTurnScore_CaseInsensitiveKeywords(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected int
	}{
		{"lowercase remember", "please remember this", 1},
		{"uppercase REMEMBER", "REMEMBER this", 1},
		{"mixed case Remember", "Remember this", 1},
		{"lowercase important", "this is important", 1},
		{"uppercase IMPORTANT", "this is IMPORTANT", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := RuntimeInput{
				UserMessage:      tt.message,
				AssistantReply:   "ok",
				ToolCalls:        []ToolCallRecord{},
				Intent:           "chat",
				CheckpointSummary: "",
			}
			score := CalcTurnScore(input)
			if score < tt.expected {
				t.Errorf("expected score >= %d for %q, got %d", tt.expected, tt.message, score)
			}
		})
	}
}

// =============================================================================
// RuntimeInput 测试
// =============================================================================

// TestRuntimeInput_EmptyToolCalls tests behavior with empty tool calls
func TestRuntimeInput_EmptyToolCalls(t *testing.T) {
	input := RuntimeInput{
		UserMessage:    "hello",
		AssistantReply: "hi",
		ToolCalls:      []ToolCallRecord{},
	}

	if len(input.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(input.ToolCalls))
	}
}

// TestRuntimeInput_NilToolCalls tests behavior with nil tool calls
func TestRuntimeInput_NilToolCalls(t *testing.T) {
	input := RuntimeInput{
		UserMessage:    "hello",
		AssistantReply: "hi",
		ToolCalls:      nil,
	}

	if input.ToolCalls != nil {
		t.Errorf("expected nil tool calls, got %v", input.ToolCalls)
	}
}

// =============================================================================
// ContextBuilder 测试
// =============================================================================

// TestContextBuilder_New tests ContextBuilder creation
func TestContextBuilder_New(t *testing.T) {
	dir := t.TempDir()
	cb := NewContextBuilder(dir)
	defer cb.memory.Close()

	if cb == nil {
		t.Fatal("NewContextBuilder returned nil")
	}
	if cb.workspace != dir {
		t.Errorf("expected workspace %s, got %s", dir, cb.workspace)
	}
	if cb.skillsLoader == nil {
		t.Error("skillsLoader should not be nil")
	}
	if cb.memory == nil {
		t.Error("memory should not be nil")
	}
}

// TestContextBuilder_GetIdentity tests identity generation
func TestContextBuilder_GetIdentity(t *testing.T) {
	dir := t.TempDir()
	cb := NewContextBuilder(dir)
	defer cb.memory.Close()

	identity := cb.getIdentity()

	if identity == "" {
		t.Error("getIdentity returned empty string")
	}
	if !strings.Contains(identity, "picoclaw") {
		t.Error("identity should contain 'picoclaw'")
	}
	if !strings.Contains(identity, dir) {
		t.Error("identity should contain workspace path")
	}
}

// TestContextBuilder_GetIdentity_Consistency tests identity consistency
func TestContextBuilder_GetIdentity_Consistency(t *testing.T) {
	dir := t.TempDir()
	cb := NewContextBuilder(dir)
	defer cb.memory.Close()

	identity1 := cb.getIdentity()
	identity2 := cb.getIdentity()

	if identity1 != identity2 {
		t.Error("getIdentity should return consistent results")
	}
}

// =============================================================================
// ActiveContext 测试
// =============================================================================

// TestActiveContext_New tests ActiveContext creation
func TestActiveContext_New(t *testing.T) {
	// ActiveContextStore requires a SQL DB connection
	// This is a placeholder test - actual testing done in integration tests
	t.Skip("ActiveContextStore requires SQL DB connection")
}

// TestActiveContext_Get_NonExistent tests getting non-existent context
func TestActiveContext_Get_NonExistent(t *testing.T) {
	t.Skip("Requires SQL DB connection")
}

// TestActiveContext_Update_Basic tests basic update operation
func TestActiveContext_Update_Basic(t *testing.T) {
	t.Skip("Requires SQL DB connection")
}

// TestActiveContext_Update_MultipleChannels tests isolation between channels
func TestActiveContext_Update_MultipleChannels(t *testing.T) {
	t.Skip("Requires SQL DB connection")
}

// =============================================================================
// ToolCallRecord 测试
// =============================================================================

// TestToolCallRecord_Empty tests empty tool call record
func TestToolCallRecord_Empty(t *testing.T) {
	tc := ToolCallRecord{}

	if tc.Name != "" {
		t.Errorf("expected empty Name, got %s", tc.Name)
	}
	if tc.Error != "" {
		t.Errorf("expected empty Error, got %s", tc.Error)
	}
}

// TestToolCallRecord_WithError tests tool call with error
func TestToolCallRecord_WithError(t *testing.T) {
	tc := ToolCallRecord{
		Name:  "exec",
		Error: "permission denied",
	}

	if tc.Name != "exec" {
		t.Errorf("expected Name 'exec', got %s", tc.Name)
	}
	if tc.Error != "permission denied" {
		t.Errorf("expected Error 'permission denied', got %s", tc.Error)
	}
}

// =============================================================================
// Checkpoint 集成测试
// =============================================================================

// TestCalcTurnScore_WithCheckpoints_Integration tests checkpoint integration
func TestCalcTurnScore_WithCheckpoints_Integration(t *testing.T) {
	tests := []struct {
		name              string
		checkpointSummary string
		expectedMinScore  int
	}{
		{"no checkpoints", "", 0},
		{"pending", "1/3 passed, 2 pending", 2},
		{"all passed", "3/3 passed", 5},
		{"all failed", "0/3 passed, 3 failed", 3},
		{"mixed", "2/3 passed, 1 failed", 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := RuntimeInput{
				UserMessage:       "task",
				AssistantReply:    "done",
				ToolCalls:         []ToolCallRecord{{Name: "exec"}},
				Intent:            "task",
				CheckpointSummary: tt.checkpointSummary,
			}

			score := CalcTurnScore(input)
			if score < tt.expectedMinScore {
				t.Errorf("expected score >= %d, got %d", tt.expectedMinScore, score)
			}
		})
	}
}

// =============================================================================
// 边界情况测试
// =============================================================================

// TestCalcTurnScore_VeryLongMessage tests with very long messages
func TestCalcTurnScore_VeryLongMessage(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      strings.Repeat("a", 10000),
		AssistantReply:   strings.Repeat("b", 10000),
		ToolCalls:        []ToolCallRecord{},
		Intent:           "chat",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Long content should not cause panic and should get positive score
	if score < 0 {
		t.Errorf("expected non-negative score for long content, got %d", score)
	}
}

// TestCalcTurnScore_UnicodeContent tests with Unicode content
func TestCalcTurnScore_UnicodeContent(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "请记住：你好世界🌍",
		AssistantReply:   "好的，已记住：你好世界🌍",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "chat",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Unicode should be handled correctly
	if score < 1 {
		t.Errorf("expected score >= 1 for Unicode with remember, got %d", score)
	}
}

// TestCalcTurnScore_MultipleKeywords tests with multiple score-boosting keywords
func TestCalcTurnScore_MultipleKeywords(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "请记住这个重要的配置，非常重要！",
		AssistantReply:   "好的",
		ToolCalls:        []ToolCallRecord{},
		Intent:           "chat",
		CheckpointSummary: "",
	}

	score := CalcTurnScore(input)
	// Keywords may not stack if they're in the same message
	// Just verify it gets some boost
	if score < 0 {
		t.Errorf("expected non-negative score for multiple keywords, got %d", score)
	}
}

// TestCalcTurnScore_AllRulesCombined tests combining all scoring rules
func TestCalcTurnScore_AllRulesCombined(t *testing.T) {
	input := RuntimeInput{
		UserMessage:      "请记住这个重要的部署任务",
		AssistantReply:   strings.Repeat("部署成功 ", 100),
		ToolCalls: []ToolCallRecord{
			{Name: "write_file"},
			{Name: "exec"},
			{Name: "exec"},
			{Name: "exec"},
			{Name: "exec"},
		},
		Intent:            "task",
		CheckpointSummary: "5/5 passed",
	}

	score := CalcTurnScore(input)
	// All rules combined should give high score
	if score < 20 {
		t.Errorf("expected score >= 20 for all rules, got %d", score)
	}
}

// TestAlwaysKeepThreshold_Constant tests the always_keep threshold constant
func TestAlwaysKeepThreshold_Constant(t *testing.T) {
	if alwaysKeepThreshold != 7 {
		t.Errorf("expected alwaysKeepThreshold = 7, got %d", alwaysKeepThreshold)
	}
}
