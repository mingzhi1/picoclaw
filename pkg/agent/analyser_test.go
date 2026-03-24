package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/llm/providers"
)

func TestPreLLM_parseResponse(t *testing.T) {
	p := &Analyser{}

	tests := []struct {
		name       string
		input      string
		wantIntent string
		wantTags   []string
		wantCot    string
	}{
		{
			name:       "valid JSON with cot_prompt",
			input:      `{"intent":"question","tags":["golang","testing"],"cot_prompt":"1. Understand the question\n2. Research the answer"}`,
			wantIntent: "question",
			wantTags:   []string{"golang", "testing"},
			wantCot:    "1. Understand the question\n2. Research the answer",
		},
		{
			name:       "JSON with markdown fences",
			input:      "```json\n{\"intent\":\"task\",\"tags\":[\"deploy\"],\"cot_prompt\":\"1. Plan\\n2. Execute\"}\n```",
			wantIntent: "task",
			wantTags:   []string{"deploy"},
			wantCot:    "1. Plan\n2. Execute",
		},
		{
			name:       "empty cot_prompt for chat",
			input:      `{"intent":"chat","tags":[],"cot_prompt":""}`,
			wantIntent: "chat",
			wantTags:   []string{},
			wantCot:    "",
		},
		{
			name:       "invalid JSON no braces",
			input:      "this is not json at all",
			wantIntent: "",
			wantTags:   nil,
			wantCot:    "",
		},
		{
			name:       "tags trimmed and lowered",
			input:      `{"intent":"code","tags":["  GoLang  ","  API  "],"cot_prompt":"think"}`,
			wantIntent: "code",
			wantTags:   []string{"golang", "api"},
			wantCot:    "think",
		},
		{
			name:       "tags limited to 5",
			input:      `{"intent":"search","tags":["a","b","c","d","e","f","g"],"cot_prompt":"search"}`,
			wantIntent: "search",
			wantTags:   []string{"a", "b", "c", "d", "e"},
			wantCot:    "search",
		},
		{
			name:       "missing cot_prompt field",
			input:      `{"intent":"question","tags":["golang"]}`,
			wantIntent: "question",
			wantTags:   []string{"golang"},
			wantCot:    "",
		},
		// --- New cases for enhanced parsing ---
		{
			name:       "JSON with surrounding text (prefix)",
			input:      "Here is the analysis:\n{\"intent\":\"task\",\"tags\":[\"deploy\"],\"cot_prompt\":\"plan\"}",
			wantIntent: "task",
			wantTags:   []string{"deploy"},
			wantCot:    "plan",
		},
		{
			name:       "JSON with surrounding text (suffix)",
			input:      "{\"intent\":\"code\",\"tags\":[\"go\"],\"cot_prompt\":\"\"}\nI hope this helps!",
			wantIntent: "code",
			wantTags:   []string{"go"},
			wantCot:    "",
		},
		{
			name:       "JSON with ```json fence (uppercase)",
			input:      "```JSON\n{\"intent\":\"debug\",\"tags\":[\"error\"],\"cot_prompt\":\"trace\"}\n```",
			wantIntent: "debug",
			wantTags:   []string{"error"},
			wantCot:    "trace",
		},
		{
			name:       "JSON with nested objects extracted",
			input:      "The result is: {\"intent\":\"question\",\"tags\":[\"api\"],\"cot_prompt\":\"check docs\"} as requested.",
			wantIntent: "question",
			wantTags:   []string{"api"},
			wantCot:    "check docs",
		},
		{
			name:       "text with braces but invalid inner JSON",
			input:      "Use {curly braces} for grouping",
			wantIntent: "",
			wantTags:   nil,
			wantCot:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.parseResponse(tt.input)
			if result.Intent != tt.wantIntent {
				t.Errorf("intent = %q, want %q", result.Intent, tt.wantIntent)
			}
			if result.CotPrompt != tt.wantCot {
				t.Errorf("cot_prompt = %q, want %q", result.CotPrompt, tt.wantCot)
			}

			if tt.wantTags == nil {
				if result.Tags != nil {
					t.Errorf("tags = %v, want nil", result.Tags)
				}
				return
			}

			if len(result.Tags) != len(tt.wantTags) {
				t.Errorf("tags len = %d, want %d (tags=%v)", len(result.Tags), len(tt.wantTags), result.Tags)
				return
			}
			for i, tag := range result.Tags {
				if tag != tt.wantTags[i] {
					t.Errorf("tag[%d] = %q, want %q", i, tag, tt.wantTags[i])
				}
			}
		})
	}
}

func TestExtractJSONObject(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple object", `{"key":"val"}`, `{"key":"val"}`},
		{"with prefix", `prefix {"key":"val"} suffix`, `{"key":"val"}`},
		{"nested braces", `{"a":{"b":1}}`, `{"a":{"b":1}}`},
		{"braces in string", `{"a":"{not a brace}"}`, `{"a":"{not a brace}"}`},
		{"no braces", "no json here", ""},
		{"unmatched open", "{incomplete", ""},
		{"escaped quotes", `{"a":"say \"hello\""}`, `{"a":"say \"hello\""}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONObject(tt.input)
			if got != tt.want {
				t.Errorf("extractJSONObject(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPreLLM_Analyse_NoProvider(t *testing.T) {
	p := &Analyser{} // no provider, no model
	result := p.Analyse(context.Background(), "hello", nil, nil, "")
	if result.Intent != "" || len(result.Tags) != 0 {
		t.Errorf("expected empty result with no provider, got %+v", result)
	}
}

func TestPreLLM_Analyse_NoTags(t *testing.T) {
	dir := t.TempDir()
	ms := NewMemoryStore(dir)
	defer ms.Close()

	cotReg := NewCotRegistry(dir)
	mp := &mockLLMProvider{
		response: `{"intent":"chat","tags":[],"cot_prompt":""}`,
	}
	p := NewAnalyser(mp, "test-model", cotReg)

	result := p.Analyse(context.Background(), "hello there", ms, nil, "")
	if result.Intent != "chat" {
		t.Errorf("expected intent 'chat', got %q", result.Intent)
	}
	if result.CotPrompt != "" {
		t.Errorf("expected empty cot_prompt for chat, got %q", result.CotPrompt)
	}
}

func TestPreLLM_Analyse_WithMemory(t *testing.T) {
	dir := t.TempDir()
	ms := NewMemoryStore(dir)
	defer ms.Close()

	// Seed memory.
	ms.AddEntry("Go is great for concurrency", []string{"golang", "concurrency"})
	ms.AddEntry("Kubernetes cluster setup notes", []string{"k8s", "devops"})
	ms.AddEntry("Go testing best practices", []string{"golang", "testing"})

	cotReg := NewCotRegistry(dir)
	mp := &mockLLMProvider{
		response: `{"intent":"question","tags":["golang"],"cot_prompt":"1. Check Go docs\n2. Write example code\n3. Verify with tests"}`,
	}
	p := NewAnalyser(mp, "test-model", cotReg)

	result := p.Analyse(context.Background(), "How do I test Go code?", ms, nil, "")

	if result.Intent != "question" {
		t.Errorf("intent = %q, want %q", result.Intent, "question")
	}
	if result.CotPrompt == "" {
		t.Error("expected non-empty CotPrompt")
	}
	if !strings.Contains(result.CotPrompt, "Go docs") {
		t.Error("CotPrompt should contain the LLM-generated strategy")
	}
	if len(result.Tags) != 1 || result.Tags[0] != "golang" {
		t.Errorf("tags = %v, want [golang]", result.Tags)
	}
	if result.MemoryContext == "" {
		t.Error("expected non-empty MemoryContext with matching tags")
	}
	if !contains(result.MemoryContext, "Go is great for concurrency") {
		t.Error("MemoryContext missing 'Go is great for concurrency'")
	}
	if !contains(result.MemoryContext, "Go testing best practices") {
		t.Error("MemoryContext missing 'Go testing best practices'")
	}
	if contains(result.MemoryContext, "Kubernetes") {
		t.Error("MemoryContext should not contain 'Kubernetes' entry")
	}

	// Verify usage was recorded with tags.
	records, _ := ms.GetRecentCotUsage(1)
	if len(records) == 0 {
		t.Fatal("expected usage record to be recorded")
	}
	if len(records[0].Tags) != 1 || records[0].Tags[0] != "golang" {
		t.Errorf("recorded tags = %v, want [golang]", records[0].Tags)
	}
}

func TestSearchByAnyTag(t *testing.T) {
	dir := t.TempDir()
	ms := NewMemoryStore(dir)
	defer ms.Close()

	ms.AddEntry("Go concurrency", []string{"golang", "concurrency"})
	ms.AddEntry("K8s setup", []string{"k8s", "devops"})
	ms.AddEntry("Go testing", []string{"golang", "testing"})
	ms.AddEntry("Python ML", []string{"python", "ml"})

	entries, err := ms.SearchByAnyTag([]string{"golang", "k8s"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3", len(entries))
	}

	entries, err = ms.SearchByAnyTag([]string{"python"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}

	entries, err = ms.SearchByAnyTag([]string{"nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestFormatMemoryEntries(t *testing.T) {
	entries := []MemoryEntry{
		{ID: 1, Content: "Test content 1", Tags: []string{"tag1", "tag2"}},
		{ID: 2, Content: "Test content 2", Tags: []string{"tag3"}},
		{ID: 3, Content: "No tags entry", Tags: nil},
	}

	result := formatMemoryEntries(entries)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !contains(result, "Relevant Memories") {
		t.Error("missing header")
	}
	if !contains(result, "#1") {
		t.Error("missing entry #1")
	}
	if !contains(result, "[tag1, tag2]") {
		t.Error("missing tags for entry #1")
	}
}

func TestFormatMemoryEntries_Empty(t *testing.T) {
	result := formatMemoryEntries(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// --- Checkpoint parsing and sanitisation tests ---

func TestParseResponse_Checkpoints_BasicParsing(t *testing.T) {
	p := &Analyser{}

	tests := []struct {
		name     string
		input    string
		wantCP   []string // expected .Text values
		wantSkip []bool   // expected .Skippable values (nil = don't check)
	}{
		{
			name:     "with checkpoints object format",
			input:    `{"intent":"task","tags":[],"cot_id":"task","checkpoints":[{"text":"Read config","skippable":false},{"text":"Write handler","skippable":false},{"text":"Add tests","skippable":true}]}`,
			wantCP:   []string{"Read config", "Write handler", "Add tests"},
			wantSkip: []bool{false, false, true},
		},
		{
			name:   "empty checkpoints for chat",
			input:  `{"intent":"chat","tags":[],"cot_id":"direct","checkpoints":[]}`,
			wantCP: []string{},
		},
		{
			name:   "missing checkpoints field",
			input:  `{"intent":"question","tags":["go"],"cot_prompt":"think"}`,
			wantCP: nil,
		},
		{
			name:   "single checkpoint",
			input:  `{"intent":"code","tags":[],"checkpoints":[{"text":"Write the function","skippable":false}]}`,
			wantCP: []string{"Write the function"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.parseResponse(tt.input)
			if tt.wantCP == nil {
				if result.Checkpoints != nil && len(result.Checkpoints) != 0 {
					t.Errorf("checkpoints = %v, want nil/empty", result.Checkpoints)
				}
				return
			}
			if len(result.Checkpoints) != len(tt.wantCP) {
				t.Errorf("checkpoints len = %d, want %d (got %v)",
					len(result.Checkpoints), len(tt.wantCP), result.Checkpoints)
				return
			}
			for i, cp := range result.Checkpoints {
				if cp.Text != tt.wantCP[i] {
					t.Errorf("cp[%d].Text = %q, want %q", i, cp.Text, tt.wantCP[i])
				}
				if tt.wantSkip != nil && cp.Skippable != tt.wantSkip[i] {
					t.Errorf("cp[%d].Skippable = %v, want %v", i, cp.Skippable, tt.wantSkip[i])
				}
			}
		})
	}
}

func TestParseResponse_Checkpoints_Sanitisation(t *testing.T) {
	p := &Analyser{}

	t.Run("capped at 7 items", func(t *testing.T) {
		items := make([]string, 9)
		for i := range items {
			items[i] = fmt.Sprintf(`{"text":"s%d","skippable":false}`, i+1)
		}
		input := `{"intent":"task","tags":[],"checkpoints":[` + strings.Join(items, ",") + `]}`
		result := p.parseResponse(input)
		if len(result.Checkpoints) > 7 {
			t.Errorf("checkpoints should be capped at 7, got %d", len(result.Checkpoints))
		}
	})

	t.Run("empty steps filtered", func(t *testing.T) {
		input := `{"intent":"task","tags":[],"checkpoints":[{"text":"step1"},{"text":""},{"text":"  "},{"text":"step2"}]}`
		result := p.parseResponse(input)
		if len(result.Checkpoints) != 2 {
			t.Errorf("expected 2 steps after filtering empties, got %d: %v",
				len(result.Checkpoints), result.Checkpoints)
		}
	})

	t.Run("dangerous patterns stripped", func(t *testing.T) {
		input := `{"intent":"task","tags":[],"checkpoints":[{"text":"Read the file"},{"text":"Run rm -rf /tmp/data"},{"text":"Write output"}]}`
		result := p.parseResponse(input)
		for _, cp := range result.Checkpoints {
			if contains(cp.Text, "rm -rf") {
				t.Errorf("dangerous pattern should be filtered: %q", cp.Text)
			}
		}
		if len(result.Checkpoints) != 2 {
			t.Errorf("expected 2 safe steps, got %d: %v",
				len(result.Checkpoints), result.Checkpoints)
		}
	})

	t.Run("long steps truncated", func(t *testing.T) {
		longStep := strings.Repeat("x", 200)
		input := `{"intent":"task","tags":[],"checkpoints":[{"text":"` + longStep + `"}]}`
		result := p.parseResponse(input)
		if len(result.Checkpoints) != 1 {
			t.Fatalf("expected 1 step, got %d", len(result.Checkpoints))
		}
		if len([]rune(result.Checkpoints[0].Text)) > 120 {
			t.Errorf("step should be truncated to 120 runes, got %d",
				len([]rune(result.Checkpoints[0].Text)))
		}
	})
}

func TestPreLLM_Analyse_WithCheckpoints(t *testing.T) {
	dir := t.TempDir()
	ms := NewMemoryStore(dir)
	defer ms.Close()

	cotReg := NewCotRegistry(dir)
	mp := &mockLLMProvider{
		response: `{"intent":"code","tags":[],"cot_id":"code","cot_prompt":"","checkpoints":[{"text":"Read existing code","skippable":false},{"text":"Add new endpoint","skippable":false},{"text":"Register route","skippable":true},{"text":"Write tests","skippable":true}]}`,
	}
	p := NewAnalyser(mp, "test-model", cotReg)

	result := p.Analyse(context.Background(), "Add a /health endpoint to the API", ms, nil, "")
	if result.Intent != "code" {
		t.Errorf("intent = %q, want 'code'", result.Intent)
	}
	if len(result.Checkpoints) != 4 {
		t.Errorf("checkpoints len = %d, want 4: %v", len(result.Checkpoints), result.Checkpoints)
	}
	if result.Checkpoints[0].Text != "Read existing code" {
		t.Errorf("cp[0].Text = %q, want 'Read existing code'", result.Checkpoints[0].Text)
	}
	if result.Checkpoints[2].Skippable != true {
		t.Error("cp[2] should be skippable")
	}
}

func TestPreLLM_Analyse_ChatNoCheckpoints(t *testing.T) {
	dir := t.TempDir()
	ms := NewMemoryStore(dir)
	defer ms.Close()

	cotReg := NewCotRegistry(dir)
	mp := &mockLLMProvider{
		response: `{"intent":"chat","tags":[],"cot_id":"direct","cot_prompt":"","checkpoints":[]}`,
	}
	p := NewAnalyser(mp, "test-model", cotReg)

	result := p.Analyse(context.Background(), "hello", ms, nil, "")
	if len(result.Checkpoints) != 0 {
		t.Errorf("chat should have empty checkpoints, got %v", result.Checkpoints)
	}
}

// --- Helpers ---

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestPreLLM_MemoryDBPath(t *testing.T) {
	dir := t.TempDir()
	ms := NewMemoryStore(dir)
	defer ms.Close()

	dbPath := filepath.Join(dir, "memory.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("memory.db not created: %v", err)
	}
}

// mockLLMProvider returns a configurable response for pre-LLM testing.
type mockLLMProvider struct {
	response string
}

func (m *mockLLMProvider) Chat(
	_ context.Context,
	_ []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{
		Content:   m.response,
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *mockLLMProvider) GetDefaultModel() string {
	return "mock-pre-llm"
}
