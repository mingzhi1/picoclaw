package agent

import (
	"testing"
	"time"
)

func TestCheckpointTracker_Begin_FormatChecklist(t *testing.T) {
	ct := NewCheckpointTracker()
	key := "test:chat1"

	if got := ct.FormatChecklist(key); got != "" {
		t.Errorf("expected empty before Begin, got %q", got)
	}

	ct.Begin(key, []CheckpointItem{
		{Text: "Read config file", Skippable: false},
		{Text: "Write handler", Skippable: false},
		{Text: "Add tests", Skippable: true},
	})
	checklist := ct.FormatChecklist(key)
	if checklist == "" {
		t.Fatal("expected non-empty checklist")
	}
	if !contains(checklist, "Execution Checkpoints") {
		t.Error("missing header")
	}
	if !contains(checklist, "[ ] Read config file") {
		t.Error("missing step 1")
	}
	if !contains(checklist, "(optional)") {
		t.Error("should show (optional) for skippable step")
	}
}

func TestCheckpointTracker_Evaluate(t *testing.T) {
	ct := NewCheckpointTracker()
	key := "test:eval"

	ct.Begin(key, []CheckpointItem{
		{Text: "Read the handler code"},
		{Text: "Write the new endpoint"},
		{Text: "Run the unit tests"},
	})

	input := RuntimeInput{
		AssistantReply: "I've implemented the changes.",
		ToolCalls: []ToolCallRecord{
			{Name: "read_file", Duration: time.Millisecond},
			{Name: "write_file", Duration: time.Millisecond},
		},
	}
	ct.Evaluate(key, input)

	if !ct.HasPending(key) {
		t.Error("should still have pending checkpoints")
	}

	progress := ct.FormatProgress(key)
	if !contains(progress, "✅") {
		t.Error("should contain passed markers")
	}
	if !contains(progress, "⏳") {
		t.Error("should contain pending markers")
	}
	if !contains(progress, "2/3") {
		t.Error("should show 2/3 passed")
	}
}

func TestCheckpointTracker_MarkSkipped_RequiresSkippable(t *testing.T) {
	ct := NewCheckpointTracker()
	key := "test:skip"

	ct.Begin(key, []CheckpointItem{
		{Text: "Required step", Skippable: false},
		{Text: "Optional step", Skippable: true},
	})

	// Cannot skip required step.
	err := ct.MarkStepSkipped(key, 0, "not needed")
	if err == nil {
		t.Error("expected error when skipping required step")
	}
	if !contains(err.Error(), "required") {
		t.Error("error should mention 'required'")
	}

	// Can skip optional step.
	if err := ct.MarkStepSkipped(key, 1, "not applicable"); err != nil {
		t.Fatalf("should allow skipping optional step: %v", err)
	}

	progress := ct.FormatProgress(key)
	if !contains(progress, "⏭") {
		t.Error("should show skip marker")
	}
	if !contains(progress, "1 skipped") {
		t.Error("should mention skipped count")
	}
}

func TestCheckpointTracker_MarkFailed(t *testing.T) {
	ct := NewCheckpointTracker()
	key := "test:fail"

	ct.Begin(key, []CheckpointItem{
		{Text: "Step A"},
		{Text: "Step B"},
	})

	if err := ct.MarkStepFailed(key, 0, "permission denied"); err != nil {
		t.Fatal(err)
	}

	progress := ct.FormatProgress(key)
	if !contains(progress, "⛔") {
		t.Error("should show failed marker")
	}
	if !contains(progress, "1 failed") {
		t.Error("should mention failed count")
	}
}

func TestCheckpointTracker_AllResolved(t *testing.T) {
	ct := NewCheckpointTracker()
	key := "test:all"

	ct.Begin(key, []CheckpointItem{
		{Text: "Search docs"},
		{Text: "Write summary"},
	})

	input := RuntimeInput{
		AssistantReply: "Here is the summary.",
		ToolCalls: []ToolCallRecord{
			{Name: "web_search", Duration: time.Millisecond},
			{Name: "write_file", Duration: time.Millisecond},
		},
	}
	ct.Evaluate(key, input)

	if ct.HasPending(key) {
		t.Error("all checkpoints should be resolved")
	}
	progress := ct.FormatProgress(key)
	if !contains(progress, "All checkpoints resolved") {
		t.Error("should show all resolved message")
	}
}

func TestCheckpointTracker_MultiChannel(t *testing.T) {
	ct := NewCheckpointTracker()

	ct.Begin("ch1", []CheckpointItem{{Text: "task A"}})
	ct.Begin("ch2", []CheckpointItem{{Text: "task B"}, {Text: "task C"}})

	if ct.FormatChecklist("ch1") == ct.FormatChecklist("ch2") {
		t.Error("different channels should have different plans")
	}

	ct.Clear("ch1")
	if ct.FormatChecklist("ch1") != "" {
		t.Error("ch1 should be cleared")
	}
	if ct.FormatChecklist("ch2") == "" {
		t.Error("ch2 should still exist")
	}
}
