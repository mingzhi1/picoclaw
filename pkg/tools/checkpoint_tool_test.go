package tools

import (
	"context"
	"testing"
)

// mockCheckpointUpdater is a mock implementation for testing
type mockCheckpointUpdater struct {
	addStepCalled    bool
	markDoneCalled   bool
	markFailedCalled bool
	markSkipCalled   bool
	getStatusCalled  bool
	lastChannel      string
	lastText         string
	lastIdx          int
	lastReason       string
	steps            []CheckpointStepInfo
	completed        int
	total            int
}

func (m *mockCheckpointUpdater) AddStep(channelKey, text string, priority int) {
	m.addStepCalled = true
	m.lastChannel = channelKey
	m.lastText = text
}

func (m *mockCheckpointUpdater) MarkStepDone(channelKey string, index int) error {
	m.markDoneCalled = true
	m.lastChannel = channelKey
	m.lastIdx = index
	return nil
}

func (m *mockCheckpointUpdater) MarkStepFailed(channelKey string, index int, reason string) error {
	m.markFailedCalled = true
	m.lastChannel = channelKey
	m.lastIdx = index
	m.lastReason = reason
	return nil
}

func (m *mockCheckpointUpdater) MarkStepSkipped(channelKey string, index int, reason string) error {
	m.markSkipCalled = true
	m.lastChannel = channelKey
	m.lastIdx = index
	m.lastReason = reason
	return nil
}

func (m *mockCheckpointUpdater) GetStatus(channelKey string) ([]CheckpointStepInfo, int, int) {
	m.getStatusCalled = true
	m.lastChannel = channelKey
	return m.steps, m.completed, m.total
}

// TestCheckpointTool_Basic tests basic checkpoint tool functionality
func TestCheckpointTool_Basic(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	
	if tool.Name() != "checkpoint" {
		t.Errorf("expected name 'checkpoint', got %s", tool.Name())
	}
	
	desc := tool.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
}

// TestCheckpointTool_Parameters tests parameter schema
func TestCheckpointTool_Parameters(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	
	params := tool.Parameters()
	if params == nil {
		t.Fatal("Parameters returned nil")
	}
	
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	
	if _, ok := props["action"]; !ok {
		t.Error("action parameter should be defined")
	}
	if _, ok := props["text"]; !ok {
		t.Error("text parameter should be defined")
	}
	if _, ok := props["step_number"]; !ok {
		t.Error("step_number parameter should be defined")
	}
	
	required, ok := params["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "action" {
		t.Error("only action should be required")
	}
}

// TestCheckpointTool_SetContext tests context setting
func TestCheckpointTool_SetContext(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	
	tool.SetContext("telegram", "user123")
	
	if tool.channelKey != "telegram:user123" {
		t.Errorf("expected channelKey 'telegram:user123', got %s", tool.channelKey)
	}
}

// TestCheckpointTool_Execute_NoUpdater tests execution without updater
func TestCheckpointTool_Execute_NoUpdater(t *testing.T) {
	tool := &CheckpointTool{}
	
	result := tool.Execute(context.Background(), map[string]any{
		"action": "status",
	})
	
	if !result.IsError {
		t.Error("should return error without updater")
	}
}

// TestCheckpointTool_Execute_UnknownAction tests unknown action handling
func TestCheckpointTool_Execute_UnknownAction(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action": "unknown",
	})
	
	if !result.IsError {
		t.Error("unknown action should return error")
	}
}

// TestCheckpointTool_Execute_Add tests add action
func TestCheckpointTool_Execute_Add(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action": "add",
		"text":   "Test checkpoint",
	})
	
	if !updater.addStepCalled {
		t.Error("AddStep should be called")
	}
	if updater.lastChannel != "telegram:user123" {
		t.Errorf("expected channel 'telegram:user123', got %s", updater.lastChannel)
	}
	if updater.lastText != "Test checkpoint" {
		t.Errorf("expected text 'Test checkpoint', got %s", updater.lastText)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.ForLLM)
	}
}

// TestCheckpointTool_Execute_Add_EmptyText tests add with empty text
func TestCheckpointTool_Execute_Add_EmptyText(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action": "add",
		"text":   "",
	})
	
	if !result.IsError {
		t.Error("empty text should return error")
	}
}

// TestCheckpointTool_Execute_Done tests done action
func TestCheckpointTool_Execute_Done(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action":      "done",
		"step_number": 1.0,
	})
	
	if !updater.markDoneCalled {
		t.Error("MarkStepDone should be called")
	}
	if updater.lastIdx != 0 {
		t.Errorf("expected index 0, got %d", updater.lastIdx)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.ForLLM)
	}
}

// TestCheckpointTool_Execute_Done_InvalidStep tests done with invalid step number
func TestCheckpointTool_Execute_Done_InvalidStep(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action": "done",
	})
	
	if !result.IsError {
		t.Error("missing step_number should return error")
	}
	
	result2 := tool.Execute(context.Background(), map[string]any{
		"action":      "done",
		"step_number": 0.0,
	})
	
	if !result2.IsError {
		t.Error("step_number < 1 should return error")
	}
}

// TestCheckpointTool_Execute_Fail tests fail action
func TestCheckpointTool_Execute_Fail(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action":      "fail",
		"step_number": 2.0,
		"text":        "Execution failed",
	})
	
	if !updater.markFailedCalled {
		t.Error("MarkStepFailed should be called")
	}
	if updater.lastIdx != 1 {
		t.Errorf("expected index 1, got %d", updater.lastIdx)
	}
	if updater.lastReason != "Execution failed" {
		t.Errorf("expected reason 'Execution failed', got %s", updater.lastReason)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.ForLLM)
	}
}

// TestCheckpointTool_Execute_Fail_DefaultReason tests fail with default reason
func TestCheckpointTool_Execute_Fail_DefaultReason(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action":      "fail",
		"step_number": 1.0,
	})
	
	if !updater.markFailedCalled {
		t.Error("MarkStepFailed should be called")
	}
	if updater.lastReason != "execution failed" {
		t.Errorf("expected default reason 'execution failed', got %s", updater.lastReason)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.ForLLM)
	}
}

// TestCheckpointTool_Execute_Skip tests skip action
func TestCheckpointTool_Execute_Skip(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action":      "skip",
		"step_number": 3.0,
		"text":        "Not applicable",
	})
	
	if !updater.markSkipCalled {
		t.Error("MarkStepSkipped should be called")
	}
	if updater.lastIdx != 2 {
		t.Errorf("expected index 2, got %d", updater.lastIdx)
	}
	if updater.lastReason != "Not applicable" {
		t.Errorf("expected reason 'Not applicable', got %s", updater.lastReason)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.ForLLM)
	}
}

// TestCheckpointTool_Execute_Skip_DefaultReason tests skip with default reason
func TestCheckpointTool_Execute_Skip_DefaultReason(t *testing.T) {
	updater := &mockCheckpointUpdater{}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action":      "skip",
		"step_number": 1.0,
	})
	
	if !updater.markSkipCalled {
		t.Error("MarkStepSkipped should be called")
	}
	if updater.lastReason != "not applicable" {
		t.Errorf("expected default reason 'not applicable', got %s", updater.lastReason)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.ForLLM)
	}
}

// TestCheckpointTool_Execute_Status tests status action
func TestCheckpointTool_Execute_Status(t *testing.T) {
	updater := &mockCheckpointUpdater{
		steps: []CheckpointStepInfo{
			{Text: "Step 1", Completed: true},
			{Text: "Step 2", Completed: false},
		},
		completed: 1,
		total:     2,
	}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action": "status",
	})
	
	if !updater.getStatusCalled {
		t.Error("GetStatus should be called")
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.ForLLM)
	}
	if result.ForLLM == "" {
		t.Error("status should return information")
	}
}

// TestCheckpointTool_Execute_Status_NoCheckpoints tests status with no checkpoints
func TestCheckpointTool_Execute_Status_NoCheckpoints(t *testing.T) {
	updater := &mockCheckpointUpdater{
		steps:     []CheckpointStepInfo{},
		completed: 0,
		total:     0,
	}
	tool := NewCheckpointTool(updater)
	tool.SetContext("telegram", "user123")
	
	result := tool.Execute(context.Background(), map[string]any{
		"action": "status",
	})
	
	if result.IsError {
		t.Errorf("unexpected error: %s", result.ForLLM)
	}
}
