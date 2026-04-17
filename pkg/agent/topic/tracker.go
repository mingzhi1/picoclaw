// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT

package topic

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mingzhi1/metaclaw/pkg/infra/logger"
)

// Tracker is the application-level service that coordinates topic lifecycle.
// It wraps Store with in-memory caching and validation logic.
type Tracker struct {
	store   *Store
	current string // cached current active topic ID
	mu      sync.Mutex
}

// NewTracker creates a Tracker backed by the given Store and attempts to restore
// the last active topic from the database (crash recovery).
func NewTracker(store *Store) (*Tracker, error) {
	t := &Tracker{store: store}
	if err := t.restore(); err != nil {
		return nil, fmt.Errorf("topic tracker restore: %w", err)
	}
	return t, nil
}

// CurrentID returns the cached active topic ID (empty if none).
func (t *Tracker) CurrentID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current
}

// Current returns the full Topic object for the active topic, or nil if none.
func (t *Tracker) Current() *Topic {
	t.mu.Lock()
	id := t.current
	t.mu.Unlock()
	if id == "" {
		return nil
	}
	tp, err := t.store.Get(id)
	if err != nil {
		return nil
	}
	return tp
}

// Apply processes an Action from the Analyser and returns the active Topic.
// This is the single entry point called each turn.
func (t *Tracker) Apply(action Action) (*Topic, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 1. Resolve any topics the user closed.
	for _, id := range action.Resolve {
		if err := t.store.SetStatus(id, StatusResolved); err != nil {
			logger.WarnCF("topic", "Failed to resolve topic",
				map[string]any{"id": id, "error": err.Error()})
		}
	}

	// 2. Determine primary topic.
	switch action.Type {
	case ActionNew:
		title := action.Title
		if title == "" {
			title = "未命名话题"
		}
		tp, err := t.store.Create(title)
		if err != nil {
			return nil, fmt.Errorf("create topic: %w", err)
		}
		t.current = tp.ID
		return tp, nil

	case ActionContinue, ActionMulti:
		primary := action.Primary
		if primary == "" {
			primary = t.current
		}
		// Validate: does this topic exist?
		if primary != "" {
			tp, err := t.store.Get(primary)
			if err != nil || tp == nil {
				// Degradation: topic ID invalid → create new
				logger.WarnCF("topic", "Invalid topic ID, creating new",
					map[string]any{"id": primary})
				return t.createFallback()
			}
			if err := t.store.Activate(primary); err != nil {
				return nil, fmt.Errorf("activate topic: %w", err)
			}
			t.current = primary
			return tp, nil
		}
		// No current topic → create new
		return t.createFallback()

	case ActionResolve:
		// Resolve the primary and create a new one
		if action.Primary != "" {
			_ = t.store.SetStatus(action.Primary, StatusResolved)
		}
		return t.createFallback()

	default:
		// Unknown action → continue current or create new
		if t.current != "" {
			tp, err := t.store.Get(t.current)
			if err == nil && tp != nil {
				return tp, nil
			}
		}
		return t.createFallback()
	}
}

// RecordTurnTokens increments the active topic's counters after a turn completes.
func (t *Tracker) RecordTurnTokens(tokens int) error {
	t.mu.Lock()
	id := t.current
	t.mu.Unlock()
	if id == "" {
		return nil
	}
	return t.store.AddTokens(id, tokens)
}

// CheckCompact returns true if the current topic needs compaction.
func (t *Tracker) CheckCompact(contextWindowTokens int) bool {
	t.mu.Lock()
	id := t.current
	t.mu.Unlock()
	if id == "" {
		return false
	}
	tp, err := t.store.Get(id)
	if err != nil || tp == nil {
		return false
	}
	return tp.ShouldCompact(contextWindowTokens)
}

// FormatForAnalyser returns a compact text representation of recent topics
// for injection into the Analyser prompt.
func (t *Tracker) FormatForAnalyser() string {
	topics, err := t.store.RecentTopics(5)
	if err != nil || len(topics) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("当前话题列表:\n")
	for _, tp := range topics {
		ago := time.Since(tp.UpdatedAt).Truncate(time.Minute)
		sb.WriteString(fmt.Sprintf("- [%s] %s (%s, %s ago)\n",
			tp.ID, tp.Title, tp.Status, ago))
	}
	return sb.String()
}

// restore loads the last active topic from DB on startup.
func (t *Tracker) restore() error {
	tp, err := t.store.ActiveTopic()
	if err != nil {
		return err
	}
	if tp != nil {
		t.current = tp.ID
		logger.InfoCF("topic", "Restored active topic",
			map[string]any{"id": tp.ID, "title": tp.Title})
	}
	return nil
}

func (t *Tracker) createFallback() (*Topic, error) {
	tp, err := t.store.Create("未命名话题")
	if err != nil {
		return nil, err
	}
	t.current = tp.ID
	return tp, nil
}
