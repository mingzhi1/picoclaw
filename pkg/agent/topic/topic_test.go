// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT

package topic

import (
	"testing"
	"time"
)

// --- Topic.ShouldCompact ---

func TestShouldCompact_TokenThreshold(t *testing.T) {
	topic := &Topic{
		TotalTokens: 4100, // > 40% of 10000
		TurnCount:   3,
		CreatedAt:   time.Now(),
	}
	if !topic.ShouldCompact(10000) {
		t.Error("expected ShouldCompact=true when tokens exceed 40% of context window")
	}
}

func TestShouldCompact_TurnThreshold(t *testing.T) {
	topic := &Topic{
		TotalTokens: 100,
		TurnCount:   13, // > 12
		CreatedAt:   time.Now(),
	}
	if !topic.ShouldCompact(10000) {
		t.Error("expected ShouldCompact=true when turn count exceeds 12")
	}
}

func TestShouldCompact_TimeThreshold(t *testing.T) {
	topic := &Topic{
		TotalTokens: 100,
		TurnCount:   3,
		CreatedAt:   time.Now().Add(-5 * time.Hour), // > 4h
	}
	if !topic.ShouldCompact(10000) {
		t.Error("expected ShouldCompact=true when topic spans > 4 hours")
	}
}

func TestShouldCompact_BelowAllThresholds(t *testing.T) {
	topic := &Topic{
		TotalTokens: 100,
		TurnCount:   3,
		CreatedAt:   time.Now().Add(-1 * time.Hour),
	}
	if topic.ShouldCompact(10000) {
		t.Error("expected ShouldCompact=false when all metrics below thresholds")
	}
}

// --- Topic.IsStale ---

func TestIsStale_IdleLongEnough(t *testing.T) {
	topic := &Topic{
		Status:    StatusIdle,
		UpdatedAt: time.Now().Add(-6 * time.Minute),
	}
	if !topic.IsStale(5 * time.Minute) {
		t.Error("expected IsStale=true when idle > threshold")
	}
}

func TestIsStale_IdleButRecent(t *testing.T) {
	topic := &Topic{
		Status:    StatusIdle,
		UpdatedAt: time.Now().Add(-2 * time.Minute),
	}
	if topic.IsStale(5 * time.Minute) {
		t.Error("expected IsStale=false when idle < threshold")
	}
}

func TestIsStale_NotIdle(t *testing.T) {
	topic := &Topic{
		Status:    StatusActive,
		UpdatedAt: time.Now().Add(-10 * time.Minute),
	}
	if topic.IsStale(5 * time.Minute) {
		t.Error("expected IsStale=false for active topic regardless of time")
	}
}
