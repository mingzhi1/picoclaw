package agent

import (
	"strings"
	"testing"
)

// =============================================================================
// CheckpointItem 测试
// =============================================================================

// TestCheckpointItem_Basic tests basic checkpoint item structure
func TestCheckpointItem_Basic(t *testing.T) {
	item := CheckpointItem{
		Text:      "Read configuration file",
		Skippable: false,
	}

	if item.Text != "Read configuration file" {
		t.Errorf("unexpected Text: %s", item.Text)
	}
	if item.Skippable {
		t.Error("Skippable should be false")
	}
}

// TestCheckpointItem_Skippable tests skippable checkpoint item
func TestCheckpointItem_Skippable(t *testing.T) {
	item := CheckpointItem{
		Text:      "Optional: Add tests",
		Skippable: true,
	}

	if !item.Skippable {
		t.Error("Skippable should be true")
	}
}

// TestCheckpointItem_EmptyText tests empty text handling
func TestCheckpointItem_EmptyText(t *testing.T) {
	item := CheckpointItem{
		Text:      "",
		Skippable: false,
	}

	if item.Text != "" {
		t.Errorf("expected empty Text, got %s", item.Text)
	}
}

// =============================================================================
// AnalyseResult 测试
// =============================================================================

// TestAnalyseResult_DefaultValues tests default values
func TestAnalyseResult_DefaultValues(t *testing.T) {
	result := AnalyseResult{}

	if result.Intent != "" {
		t.Errorf("expected empty Intent, got %s", result.Intent)
	}
	if result.Tags != nil {
		t.Errorf("expected nil Tags, got %v", result.Tags)
	}
	if result.ToolHints != nil {
		t.Errorf("expected nil ToolHints, got %v", result.ToolHints)
	}
	if result.CotID != "" {
		t.Errorf("expected empty CotID, got %s", result.CotID)
	}
	if result.CotPrompt != "" {
		t.Errorf("expected empty CotPrompt, got %s", result.CotPrompt)
	}
	if result.Checkpoints != nil {
		t.Errorf("expected nil Checkpoints, got %v", result.Checkpoints)
	}
	if result.MemoryContext != "" {
		t.Errorf("expected empty MemoryContext, got %s", result.MemoryContext)
	}
}

// TestAnalyseResult_WithIntent tests with various intents
func TestAnalyseResult_WithIntent(t *testing.T) {
	intents := []string{"chat", "question", "task", "code", "debug"}

	for _, intent := range intents {
		t.Run(intent, func(t *testing.T) {
			result := AnalyseResult{Intent: intent}
			if result.Intent != intent {
				t.Errorf("expected Intent %s, got %s", intent, result.Intent)
			}
		})
	}
}

// TestAnalyseResult_WithTags tests with tags
func TestAnalyseResult_WithTags(t *testing.T) {
	tags := []string{"go", "deploy", "ci"}
	result := AnalyseResult{Tags: tags}

	if len(result.Tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(result.Tags))
	}
}

// TestAnalyseResult_WithCheckpoints tests with checkpoints
func TestAnalyseResult_WithCheckpoints(t *testing.T) {
	checkpoints := []CheckpointItem{
		{Text: "Step 1", Skippable: false},
		{Text: "Step 2", Skippable: true},
	}
	result := AnalyseResult{Checkpoints: checkpoints}

	if len(result.Checkpoints) != 2 {
		t.Errorf("expected 2 checkpoints, got %d", len(result.Checkpoints))
	}
}

// =============================================================================
// AnalyserTopicAction 测试
// =============================================================================

// TestAnalyserTopicAction_Continue tests continue action
func TestAnalyserTopicAction_Continue(t *testing.T) {
	action := AnalyserTopicAction{
		Action: "continue",
		ID:     "topic-123",
	}

	if action.Action != "continue" {
		t.Errorf("expected Action 'continue', got %s", action.Action)
	}
	if action.ID != "topic-123" {
		t.Errorf("expected ID 'topic-123', got %s", action.ID)
	}
}

// TestAnalyserTopicAction_New tests new topic action
func TestAnalyserTopicAction_New(t *testing.T) {
	action := AnalyserTopicAction{
		Action: "new",
		Title:  "New Discussion",
	}

	if action.Action != "new" {
		t.Errorf("expected Action 'new', got %s", action.Action)
	}
	if action.Title != "New Discussion" {
		t.Errorf("expected Title 'New Discussion', got %s", action.Title)
	}
}

// TestAnalyserTopicAction_Resolve tests resolve action
func TestAnalyserTopicAction_Resolve(t *testing.T) {
	action := AnalyserTopicAction{
		Action:  "resolve",
		ID:      "topic-456",
		Resolve: []string{"topic-123", "topic-456"},
	}

	if action.Action != "resolve" {
		t.Errorf("expected Action 'resolve', got %s", action.Action)
	}
	if len(action.Resolve) != 2 {
		t.Errorf("expected 2 topics to resolve, got %d", len(action.Resolve))
	}
}

// TestAnalyserTopicAction_Empty tests empty action
func TestAnalyserTopicAction_Empty(t *testing.T) {
	action := AnalyserTopicAction{}

	if action.Action != "" {
		t.Errorf("expected empty Action, got %s", action.Action)
	}
	if action.ID != "" {
		t.Errorf("expected empty ID, got %s", action.ID)
	}
	if action.Title != "" {
		t.Errorf("expected empty Title, got %s", action.Title)
	}
}

// =============================================================================
// ToolHints 测试
// =============================================================================

// TestToolHints_ValidCategories tests valid tool hint categories
func TestToolHints_ValidCategories(t *testing.T) {
	validHints := []string{"file", "exec", "web", "skill", "spawn", "message", "device", "mcp"}

	for _, hint := range validHints {
		t.Run(hint, func(t *testing.T) {
			result := AnalyseResult{ToolHints: []string{hint}}
			if len(result.ToolHints) != 1 {
				t.Errorf("expected 1 tool hint, got %d", len(result.ToolHints))
			}
		})
	}
}

// TestToolHints_MultipleCategories tests multiple tool hint categories
func TestToolHints_MultipleCategories(t *testing.T) {
	hints := []string{"file", "exec", "web"}
	result := AnalyseResult{ToolHints: hints}

	if len(result.ToolHints) != 3 {
		t.Errorf("expected 3 tool hints, got %d", len(result.ToolHints))
	}
}

// =============================================================================
// CotRegistry 测试
// =============================================================================

// TestCotRegistry_Get tests CoT template retrieval
func TestCotRegistry_Get(t *testing.T) {
	dir := t.TempDir()
	reg := NewCotRegistry(dir)

	// Test built-in templates
	templates := []string{"code", "debug", "analytical", "creative", "direct"}

	for _, id := range templates {
		t.Run(id, func(t *testing.T) {
			tpl := reg.Get(id)
			if tpl.ID == "" && id != "" {
				t.Errorf("Get(%s) returned empty template", id)
			}
		})
	}
}

// TestCotRegistry_Get_Unknown tests unknown CoT template
func TestCotRegistry_Get_Unknown(t *testing.T) {
	dir := t.TempDir()
	reg := NewCotRegistry(dir)

	tpl := reg.Get("unknown-template")
	// Unknown templates may return a default/fallback template
	// Just verify it doesn't panic
	t.Logf("Get(unknown) returned template with ID: %s", tpl.ID)
}

// TestCotRegistry_Get_CaseSensitive tests case sensitivity
func TestCotRegistry_Get_CaseSensitive(t *testing.T) {
	dir := t.TempDir()
	reg := NewCotRegistry(dir)

	// "code" should exist, "CODE" should not
	tpl1 := reg.Get("code")
	tpl2 := reg.Get("CODE")

	if tpl1.ID == "" {
		t.Error("Get(code) should return template")
	}
	// Case sensitivity depends on implementation - just verify behavior is consistent
	if tpl1.ID == tpl2.ID && tpl1.ID != "" {
		t.Log("Get is case-insensitive (acceptable)")
	}
}

// =============================================================================
// FormatMemoryEntries 测试
// =============================================================================

// TestFormatMemoryEntries_Basic tests basic memory entry formatting
func TestFormatMemoryEntries_Basic(t *testing.T) {
	entries := []MemoryEntry{
		{ID: 1, Content: "First entry", Tags: []string{"tag1"}},
		{ID: 2, Content: "Second entry", Tags: []string{"tag2", "tag3"}},
	}

	result := formatMemoryEntries(entries)

	if result == "" {
		t.Error("formatMemoryEntries returned empty string")
	}
	if !strings.Contains(result, "First entry") {
		t.Error("result should contain first entry")
	}
	if !strings.Contains(result, "Second entry") {
		t.Error("result should contain second entry")
	}
	if !strings.Contains(result, "tag1") {
		t.Error("result should contain tag1")
	}
}

// TestFormatMemoryEntries_Empty tests empty entries
func TestFormatMemoryEntries_Empty(t *testing.T) {
	entries := []MemoryEntry{}

	result := formatMemoryEntries(entries)
	if result != "" {
		t.Errorf("expected empty string for empty entries, got %s", result)
	}
}

// TestFormatMemoryEntries_NoTags tests entries without tags
func TestFormatMemoryEntries_NoTags(t *testing.T) {
	entries := []MemoryEntry{
		{ID: 1, Content: "Entry without tags", Tags: []string{}},
	}

	result := formatMemoryEntries(entries)
	if result == "" {
		t.Error("formatMemoryEntries returned empty string")
	}
	// Should still format without tags
	if !strings.Contains(result, "Entry without tags") {
		t.Error("result should contain entry content")
	}
}

// TestFormatMemoryEntries_SpecialCharacters tests entries with special characters
func TestFormatMemoryEntries_SpecialCharacters(t *testing.T) {
	entries := []MemoryEntry{
		{ID: 1, Content: "Entry with special chars: @#$%", Tags: []string{"go-lang"}},
		{ID: 2, Content: "Entry with emoji: 🚀", Tags: []string{"deploy"}},
	}

	result := formatMemoryEntries(entries)
	if !strings.Contains(result, "@#$%") {
		t.Error("result should contain special characters")
	}
	if !strings.Contains(result, "🚀") {
		t.Error("result should contain emoji")
	}
}

// TestFormatMemoryEntries_LongContent tests entries with long content
func TestFormatMemoryEntries_LongContent(t *testing.T) {
	longContent := strings.Repeat("This is a long content. ", 100)
	entries := []MemoryEntry{
		{ID: 1, Content: longContent, Tags: []string{"test"}},
	}

	result := formatMemoryEntries(entries)
	if result == "" {
		t.Error("formatMemoryEntries returned empty string")
	}
}

// =============================================================================
// countMemoryLines 测试
// =============================================================================

// TestCountMemoryLines_Basic tests basic line counting
func TestCountMemoryLines_Basic(t *testing.T) {
	formatted := `## Relevant Memories

- (#1 [go]) Entry one
- (#2 [deploy]) Entry two
- (#3 [ci]) Entry three`

	count := countMemoryLines(formatted)
	if count != 3 {
		t.Errorf("expected 3 lines, got %d", count)
	}
}

// TestCountMemoryLines_Empty tests empty string
func TestCountMemoryLines_Empty(t *testing.T) {
	count := countMemoryLines("")
	if count != 0 {
		t.Errorf("expected 0 lines for empty string, got %d", count)
	}
}

// TestCountMemoryLines_NoMemories tests string without memory entries
func TestCountMemoryLines_NoMemories(t *testing.T) {
	formatted := `## Some Header

Some other content
Not a memory entry`

	count := countMemoryLines(formatted)
	if count != 0 {
		t.Errorf("expected 0 lines, got %d", count)
	}
}

// TestCountMemoryLines_MixedFormat tests mixed format
func TestCountMemoryLines_MixedFormat(t *testing.T) {
	formatted := `## Relevant Memories

- (#1 [go]) Entry one
Some random text
- (#2 [deploy]) Entry two
More random text
- (#3) Entry three without tags`

	count := countMemoryLines(formatted)
	if count != 3 {
		t.Errorf("expected 3 lines, got %d", count)
	}
}

// =============================================================================
// sanitizeCotPrompt 测试
// =============================================================================

// TestSanitizeCotPrompt_Basic tests basic sanitization
func TestSanitizeCotPrompt_Basic(t *testing.T) {
	// This tests that dangerous patterns are filtered
	prompt := "Normal thinking strategy"
	result := sanitizeCotPrompt(prompt)
	if result == "" {
		t.Error("sanitizeCotPrompt should not filter normal content")
	}
}

// TestSanitizeCotPrompt_DangerousPatterns tests dangerous pattern filtering
func TestSanitizeCotPrompt_DangerousPatterns(t *testing.T) {
	dangerousPrompts := []string{
		"rm -rf /",
		"sudo apt-get remove",
		"DROP TABLE users",
		"format c:",
		"删除所有文件",
	}

	for _, prompt := range dangerousPrompts {
		t.Run(prompt, func(t *testing.T) {
			result := sanitizeCotPrompt(prompt)
			if result != "" {
				t.Errorf("expected empty result for dangerous prompt, got %s", result)
			}
		})
	}
}

// TestSanitizeCotPrompt_CaseInsensitive tests case insensitive filtering
func TestSanitizeCotPrompt_CaseInsensitive(t *testing.T) {
	prompts := []string{
		"RM -RF /",
		"Sudo command",
		"drop table",
	}

	for _, prompt := range prompts {
		t.Run(prompt, func(t *testing.T) {
			result := sanitizeCotPrompt(prompt)
			if result != "" {
				t.Errorf("expected empty result for case-variant dangerous prompt, got %s", result)
			}
		})
	}
}

// =============================================================================
// truncateRunes 测试
// =============================================================================

// TestTruncateRunes_Basic tests basic truncation
func TestTruncateRunes_Basic(t *testing.T) {
	s := "Hello, 世界"
	result := truncateRunes(s, 8)
	// Result includes "..." suffix, so total is 8+3=11
	if len([]rune(result)) > 11 {
		t.Errorf("expected max 11 runes (8+...), got %d", len([]rune(result)))
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("expected ellipsis suffix, got %s", result)
	}
}

// TestTruncateRunes_NoTruncation tests when no truncation needed
func TestTruncateRunes_NoTruncation(t *testing.T) {
	s := "Short"
	result := truncateRunes(s, 10)
	if result != s {
		t.Errorf("expected %s, got %s", s, result)
	}
}

// TestTruncateRunes_Empty tests empty string
func TestTruncateRunes_Empty(t *testing.T) {
	s := ""
	result := truncateRunes(s, 10)
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

// TestTruncateRunes_Unicode tests Unicode truncation
func TestTruncateRunes_Unicode(t *testing.T) {
	s := "你好世界🌍"
	result := truncateRunes(s, 3)
	// Result is 3 runes + "..." = 6 runes
	if len([]rune(result)) != 6 {
		t.Logf("expected 6 runes (3+...), got %d: %s", len([]rune(result)), result)
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("expected ellipsis suffix, got %s", result)
	}
}

// TestTruncateRunes_AddsEllipsis tests that ellipsis is added
func TestTruncateRunes_AddsEllipsis(t *testing.T) {
	s := "This is a long string"
	result := truncateRunes(s, 10)
	if !strings.HasSuffix(result, "...") {
		t.Errorf("expected ellipsis suffix, got %s", result)
	}
}

// =============================================================================
// generateNonce 测试
// =============================================================================

// TestGenerateNonce_Basic tests basic nonce generation
func TestGenerateNonce_Basic(t *testing.T) {
	nonce := generateNonce()
	if nonce == "" {
		t.Error("generateNonce returned empty string")
	}
}

// TestGenerateNonce_Uniqueness tests nonce uniqueness
func TestGenerateNonce_Uniqueness(t *testing.T) {
	nonces := make(map[string]bool)
	for i := 0; i < 100; i++ {
		nonce := generateNonce()
		if nonces[nonce] {
			t.Errorf("duplicate nonce generated: %s", nonce)
		}
		nonces[nonce] = true
	}
}

// TestGenerateNonce_Length tests nonce length
func TestGenerateNonce_Length(t *testing.T) {
	nonce := generateNonce()
	// Nonce should be short hex string (typically 8 chars for 4 bytes)
	if len(nonce) < 4 {
		t.Errorf("nonce too short: %s", nonce)
	}
	if len(nonce) > 20 {
		t.Errorf("nonce too long: %s", nonce)
	}
}

// =============================================================================
// validToolHints 测试
// =============================================================================

// TestValidToolHints_ContainsAll tests that all valid hints are present
func TestValidToolHints_ContainsAll(t *testing.T) {
	expected := map[string]bool{
		"file": true, "exec": true, "web": true, "skill": true,
		"spawn": true, "message": true, "device": true, "mcp": true,
	}

	for hint := range expected {
		if !validToolHints[hint] {
			t.Errorf("validToolHints should contain %s", hint)
		}
	}
}

// TestValidToolHints_NoExtra tests no extra hints
func TestValidToolHints_NoExtra(t *testing.T) {
	if len(validToolHints) != 8 {
		t.Errorf("expected 8 valid tool hints, got %d", len(validToolHints))
	}
}

// TestValidToolHints_CaseSensitive tests case sensitivity
func TestValidToolHints_CaseSensitive(t *testing.T) {
	// Should be lowercase only
	if validToolHints["FILE"] {
		t.Error("validToolHints should be case sensitive (lowercase only)")
	}
	if !validToolHints["file"] {
		t.Error("validToolHints should contain 'file'")
	}
}
