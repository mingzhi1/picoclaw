// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

// Package topic implements the Topic domain layer for MetaClaw's context management.
// A Topic groups related conversation turns under a shared theme, enabling
// precise context retrieval and automatic compaction for long conversations.
package topic

import "time"

// Status represents the lifecycle state of a Topic.
type Status string

const (
	StatusActive    Status = "active"    // Currently being discussed
	StatusIdle      Status = "idle"      // No new turns for >5 minutes
	StatusCompacted Status = "compacted" // Summary generated, old turns archived
	StatusResolved  Status = "resolved"  // User explicitly closed this topic
)

// ActionType classifies what the Analyser believes the user is doing w.r.t. topics.
type ActionType string

const (
	ActionContinue ActionType = "continue" // Resume an existing topic
	ActionNew      ActionType = "new"      // Start a fresh topic
	ActionMulti    ActionType = "multi"    // Message spans multiple topics
	ActionResolve  ActionType = "resolve"  // Close one topic, start another
)

// Action is the Analyser's topic decision for a single turn.
type Action struct {
	Type    ActionType
	Primary string   // Topic ID to activate (empty → create new)
	Title   string   // Title for a new topic (used when Type==ActionNew)
	Refs    []string // Topic IDs to reference (summary only, max 1)
	Resolve []string // Topic IDs to close (no context needed)
}

// Topic is the aggregate root for a conversation thread.
type Topic struct {
	ID          string
	Title       string
	Status      Status
	Summary     string
	TotalTokens int
	TurnCount   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ShouldCompact returns true when this topic is large enough to require compaction.
// Uses hard metrics only — no LLM involved in this decision.
func (t *Topic) ShouldCompact(contextWindowTokens int) bool {
	tokenThreshold := contextWindowTokens * 40 / 100
	return t.TotalTokens > tokenThreshold ||
		t.TurnCount > 12 ||
		time.Since(t.CreatedAt) > 4*time.Hour
}

// IsStale returns true when the topic has been idle long enough for background digest.
func (t *Topic) IsStale(idleThreshold time.Duration) bool {
	return t.Status == StatusIdle && time.Since(t.UpdatedAt) > idleThreshold
}
