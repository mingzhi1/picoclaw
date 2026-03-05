// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"fmt"
	"strings"
)

// --- CoT usage tracking (learning) ------------------------------------------

// CotUsageRecord represents a single CoT usage entry.
type CotUsageRecord struct {
	ID        int64
	Intent    string
	Tags      []string // Tags from the message analysis
	CotPrompt string   // LLM-generated thinking strategy
	Message   string
	Feedback  int // -1=bad, 0=neutral, 1=good
	CreatedAt string
}

// CotStats holds aggregated statistics for an intent.
type CotStats struct {
	Intent    string
	TotalUses int
	AvgScore  float64 // Average feedback score
	LastUsed  string
}

// RecordCotUsage logs a CoT usage event with the LLM-generated prompt and tags.
// messagePreview is truncated to 200 characters.
func (ms *MemoryStore) RecordCotUsage(intent string, tags []string, cotPrompt, message string) (int64, error) {
	if ms.db == nil {
		return 0, fmt.Errorf("memory DB not available")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Truncate message preview.
	if len(message) > 200 {
		message = message[:200]
	}

	tagStr := strings.Join(tags, ",")
	res, err := ms.db.Exec(
		"INSERT INTO cot_usage (intent, tags, cot_prompt, message) VALUES (?, ?, ?, ?)",
		intent, tagStr, cotPrompt, message,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateCotFeedback updates the feedback score for a CoT usage record.
// score: -1=bad, 0=neutral, 1=good.
func (ms *MemoryStore) UpdateCotFeedback(id int64, score int) error {
	if ms.db == nil {
		return fmt.Errorf("memory DB not available")
	}
	if score < -1 || score > 1 {
		return fmt.Errorf("feedback score must be -1, 0, or 1")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()

	_, err := ms.db.Exec("UPDATE cot_usage SET feedback = ? WHERE id = ?", score, id)
	return err
}

// UpdateLatestCotFeedback updates the feedback score for the most recent
// CoT usage record. This is useful when the user provides feedback after
// the main LLM has responded (at which point the usage ID may not be tracked).
func (ms *MemoryStore) UpdateLatestCotFeedback(score int) error {
	if ms.db == nil {
		return fmt.Errorf("memory DB not available")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()

	_, err := ms.db.Exec(
		"UPDATE cot_usage SET feedback = ? WHERE id = (SELECT MAX(id) FROM cot_usage)",
		score,
	)
	return err
}

// GetCotStats returns aggregated statistics per intent,
// based on usage in the last N days. Ordered by total uses descending.
func (ms *MemoryStore) GetCotStats(days int) ([]CotStats, error) {
	if ms.db == nil {
		return nil, fmt.Errorf("memory DB not available")
	}
	if days <= 0 {
		days = 30
	}

	rows, err := ms.db.Query(`
		SELECT
			intent,
			COUNT(*) as total_uses,
			COALESCE(AVG(CASE WHEN feedback != 0 THEN CAST(feedback AS REAL) END), 0.0) as avg_score,
			MAX(created_at) as last_used
		FROM cot_usage
		WHERE created_at >= datetime('now', ? || ' days')
		GROUP BY intent
		ORDER BY total_uses DESC
	`, fmt.Sprintf("-%d", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []CotStats
	for rows.Next() {
		var s CotStats
		if err := rows.Scan(&s.Intent, &s.TotalUses, &s.AvgScore, &s.LastUsed); err != nil {
			continue
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// GetCotIntentStats returns usage stats per intent.
// This is a simpler version that just counts per intent.
func (ms *MemoryStore) GetCotIntentStats(days int) ([]CotStats, error) {
	return ms.GetCotStats(days)
}

// GetTopRatedCotPrompts returns the highest-rated generated CoT prompts.
// If filterTags is non-empty, prioritises prompts that share tags with the query.
// These serve as proven examples for future LLM generation.
func (ms *MemoryStore) GetTopRatedCotPrompts(days, limit int, filterTags []string) ([]CotUsageRecord, error) {
	if ms.db == nil {
		return nil, fmt.Errorf("memory DB not available")
	}
	if days <= 0 {
		days = 30
	}
	if limit <= 0 {
		limit = 5
	}

	rows, err := ms.db.Query(`
		SELECT id, intent, tags, cot_prompt, message, feedback, created_at
		FROM cot_usage
		WHERE feedback > 0
		  AND cot_prompt != ''
		  AND created_at >= datetime('now', ? || ' days')
		ORDER BY feedback DESC, created_at DESC
		LIMIT ?
	`, fmt.Sprintf("-%d", days), limit*3) // Over-fetch to filter by tags later.
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []CotUsageRecord
	for rows.Next() {
		var r CotUsageRecord
		var tagStr string
		if err := rows.Scan(&r.ID, &r.Intent, &tagStr, &r.CotPrompt, &r.Message, &r.Feedback, &r.CreatedAt); err != nil {
			continue
		}
		if tagStr != "" {
			r.Tags = strings.Split(tagStr, ",")
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// If filter tags provided, sort by tag overlap (most relevant first).
	if len(filterTags) > 0 && len(all) > 0 {
		tagSet := make(map[string]bool, len(filterTags))
		for _, t := range filterTags {
			tagSet[strings.ToLower(t)] = true
		}

		// Partition: matching first, then non-matching.
		var matching, rest []CotUsageRecord
		for _, r := range all {
			hasOverlap := false
			for _, t := range r.Tags {
				if tagSet[strings.ToLower(t)] {
					hasOverlap = true
					break
				}
			}
			if hasOverlap {
				matching = append(matching, r)
			} else {
				rest = append(rest, r)
			}
		}
		all = append(matching, rest...)
	}

	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// GetRecentCotUsage returns the N most recent CoT usage records.
func (ms *MemoryStore) GetRecentCotUsage(limit int) ([]CotUsageRecord, error) {
	if ms.db == nil {
		return nil, fmt.Errorf("memory DB not available")
	}
	if limit <= 0 {
		limit = 20
	}

	rows, err := ms.db.Query(
		"SELECT id, intent, tags, cot_prompt, message, feedback, created_at FROM cot_usage ORDER BY id DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []CotUsageRecord
	for rows.Next() {
		var r CotUsageRecord
		var tagStr string
		if err := rows.Scan(&r.ID, &r.Intent, &tagStr, &r.CotPrompt, &r.Message, &r.Feedback, &r.CreatedAt); err != nil {
			continue
		}
		if tagStr != "" {
			r.Tags = strings.Split(tagStr, ",")
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// FormatCotLearningContext formats CoT usage history and top-rated prompts
// into a string for the pre-LLM to learn from past generations.
// currentTags are the tags extracted from the current message, used to
// prioritise relevant proven strategies.
func (ms *MemoryStore) FormatCotLearningContext(days int, currentTags []string) string {
	var sb strings.Builder
	hasContent := false

	// 1. Usage stats per intent.
	stats, err := ms.GetCotStats(days)
	if err == nil && len(stats) > 0 {
		sb.WriteString("## Historical Usage Stats\n\n")
		for _, s := range stats {
			scoreLabel := "neutral"
			if s.AvgScore > 0.3 {
				scoreLabel = "good"
			} else if s.AvgScore < -0.3 {
				scoreLabel = "poor"
			}
			fmt.Fprintf(&sb, "- Intent '%s': %d uses, avg feedback=%s (%.1f)\n",
				s.Intent, s.TotalUses, scoreLabel, s.AvgScore)
		}
		sb.WriteString("\n")
		hasContent = true
	}

	// 2. Top-rated generated prompts as proven examples (filtered by current tags).
	topPrompts, err := ms.GetTopRatedCotPrompts(days, 3, currentTags)
	if err == nil && len(topPrompts) > 0 {
		sb.WriteString("## Proven Strategies (from past sessions with positive feedback)\n\n")
		sb.WriteString("These generated strategies received positive feedback. Use similar approaches for similar intents.\n\n")
		for i, r := range topPrompts {
			msgPreview := r.Message
			if len(msgPreview) > 80 {
				msgPreview = msgPreview[:80] + "..."
			}
			tagLabel := ""
			if len(r.Tags) > 0 {
				tagLabel = fmt.Sprintf(", tags: [%s]", strings.Join(r.Tags, ", "))
			}
			fmt.Fprintf(&sb, "### Proven #%d (intent: %s%s, message: \"%s\")\n%s\n\n",
				i+1, r.Intent, tagLabel, msgPreview, r.CotPrompt)
		}
		hasContent = true
	}

	if !hasContent {
		return ""
	}
	return sb.String()
}
