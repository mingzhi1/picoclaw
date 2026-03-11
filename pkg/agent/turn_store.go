// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sipeed/picoclaw/pkg/infra/logger"

	_ "modernc.org/sqlite"
)

// TurnRecord captures everything that happened during a single completed turn.
// It is persisted to turns.db for use by MemoryDigest and instant-memory assembly.
type TurnRecord struct {
	ID         string          // ULID or time-based unique ID
	Ts         int64           // Unix timestamp (seconds)
	ChannelKey string          // "channel:chatID"
	Score      int             // Phase 3 CalcTurnScore result
	Intent     string          // Phase 1 detected intent
	Tags       []string        // Phase 1 detected tags
	Tokens     int             // rough token estimate (chars / 3)
	Status     string          // "pending" | "processed" | "archived"
	UserMsg    string          // original user message
	Reply      string          // assistant final response
	ToolCalls  []ToolCallRecord // serialised as JSON in DB
}

// TurnStore manages persistent Turn storage in SQLite.
// The DB lives at {workspace}/turns.db, mirroring the memory.db pattern.
type TurnStore struct {
	db *sql.DB
}

const turnsDDL = `
CREATE TABLE IF NOT EXISTS turns (
    id          TEXT    PRIMARY KEY,
    ts          INTEGER NOT NULL,
    channel_key TEXT    NOT NULL DEFAULT '',
    score       INTEGER NOT NULL DEFAULT 0,
    intent      TEXT    NOT NULL DEFAULT '',
    tags        TEXT    NOT NULL DEFAULT '[]',
    tokens      INTEGER NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL DEFAULT 'pending',
    user_msg    TEXT    NOT NULL DEFAULT '',
    reply       TEXT    NOT NULL DEFAULT '',
    tool_calls  TEXT    NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_turns_status  ON turns(status);
CREATE INDEX IF NOT EXISTS idx_turns_ts      ON turns(ts);
CREATE INDEX IF NOT EXISTS idx_turns_channel ON turns(channel_key);
CREATE INDEX IF NOT EXISTS idx_turns_score   ON turns(score);

-- Inverted index: tag × channel → turn IDs.
-- Replaces the slow JSON-LIKE scan in QueryByTags.
CREATE TABLE IF NOT EXISTS turn_tags (
    tag         TEXT    NOT NULL,
    channel_key TEXT    NOT NULL,
    turn_id     TEXT    NOT NULL,
    ts          INTEGER NOT NULL,
    PRIMARY KEY (tag, channel_key, turn_id)
);
CREATE INDEX IF NOT EXISTS idx_turn_tags_lookup ON turn_tags(tag, channel_key, ts);
`

// NewTurnStore creates (or opens) turns.db in the given workspace directory.
func NewTurnStore(workspace string) (*TurnStore, error) {
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return nil, fmt.Errorf("turn_store: mkdir %s: %w", workspace, err)
	}
	dbPath := filepath.Join(workspace, "turns.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("turn_store: open %s: %w", dbPath, err)
	}
	if _, err := db.Exec(turnsDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("turn_store: init schema: %w", err)
	}
	return &TurnStore{db: db}, nil
}

// Close shuts down the underlying DB connection.
func (s *TurnStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func unmarshalTags(raw string) []string {
	var tags []string
	_ = json.Unmarshal([]byte(raw), &tags)
	return tags
}

func unmarshalToolCalls(raw string) []ToolCallRecord {
	var tcs []ToolCallRecord
	_ = json.Unmarshal([]byte(raw), &tcs)
	return tcs
}

// estimateTokens gives a cheap estimate: characters / 3.
func estimateTokens(r TurnRecord) int {
	chars := len(r.UserMsg) + len(r.Reply)
	for _, tc := range r.ToolCalls {
		chars += len(tc.Name) + len(tc.Error)
	}
	if chars < 3 {
		return 1
	}
	return chars / 3
}

// ---------------------------------------------------------------------------
// Turn ID generation — channel-aware, time-sortable, compact
// ---------------------------------------------------------------------------

// base62Chars is the alphabet for base62 encoding (0-9, A-Z, a-z).
const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// encodeBase62 encodes a uint64 value to a base62 string.
func encodeBase62(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte // max 11 chars for uint64 in base62
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = base62Chars[n%62]
		n /= 62
	}
	return string(buf[i:])
}

// fnv32a computes a 32-bit FNV-1a hash of the string.
func fnv32a(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// turnIDCounter is a process-wide atomic counter for sub-millisecond uniqueness.
var turnIDCounter uint64

// NewTurnID generates a channel-aware, time-sortable, compact ID.
// Format: b62(channelHash)_b62(unixMilli)_b62(counter)
// Example: "1Bx_5kR9wG_3" — short, URL-safe, embeds channel identity.
//
// Falls back to timestamp-only if channelKey is empty.
func NewTurnID(channelKey string) string {
	ms := uint64(time.Now().UnixMilli())
	seq := atomic.AddUint64(&turnIDCounter, 1) - 1

	tsEnc := encodeBase62(ms)

	if channelKey == "" {
		return tsEnc + "_" + encodeBase62(seq)
	}

	chHash := encodeBase62(uint64(fnv32a(channelKey)))
	return chHash + "_" + tsEnc + "_" + encodeBase62(seq)
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

// Insert persists a TurnRecord to the DB.
// The record's ID and Ts are set if empty/zero.
func (s *TurnStore) Insert(r TurnRecord) error {
	if r.ID == "" {
		r.ID = NewTurnID(r.ChannelKey)
	}
	if r.Ts == 0 {
		r.Ts = time.Now().Unix()
	}
	if r.Status == "" {
		r.Status = "pending"
	}
	if r.Tokens == 0 {
		r.Tokens = estimateTokens(r)
	}

	tagsJSON := marshalJSON(r.Tags)
	tcJSON := marshalJSON(r.ToolCalls)

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("turn_store: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.Exec(`
		INSERT INTO turns (id, ts, channel_key, score, intent, tags, tokens, status, user_msg, reply, tool_calls)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		r.ID, r.Ts, r.ChannelKey, r.Score, r.Intent,
		tagsJSON, r.Tokens, r.Status, r.UserMsg, r.Reply, tcJSON,
	)
	if err != nil {
		return fmt.Errorf("turn_store: insert turn %s: %w", r.ID, err)
	}

	// Update inverted tag index.
	for _, tag := range r.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		_, err = tx.Exec(`
			INSERT OR IGNORE INTO turn_tags (tag, channel_key, turn_id, ts)
			VALUES (?, ?, ?, ?)`,
			tag, r.ChannelKey, r.ID, r.Ts)
		if err != nil {
			return fmt.Errorf("turn_store: insert tag index %s/%s: %w", r.ID, tag, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("turn_store: commit %s: %w", r.ID, err)
	}

	logger.DebugCF("turn_store", "Turn inserted",
		map[string]any{"id": r.ID, "score": r.Score, "tokens": r.Tokens, "status": r.Status, "tags": len(r.Tags)})
	return nil
}

// SetStatus updates the status of a turn by ID.
func (s *TurnStore) SetStatus(id, status string) error {
	_, err := s.db.Exec("UPDATE turns SET status = ? WHERE id = ?", status, id)
	return err
}

// ---------------------------------------------------------------------------
// Queries — used by MemoryDigest and instant-memory assembly
// ---------------------------------------------------------------------------

// QueryPending returns up to limit turns with status = 'pending', ordered oldest first.
func (s *TurnStore) QueryPending(limit int) ([]TurnRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, ts, channel_key, score, intent, tags, tokens, status, user_msg, reply, tool_calls
		FROM turns WHERE status = 'pending'
		ORDER BY ts ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTurns(rows)
}

// QueryByScore returns all turns with score >= highThreshold (always_keep)
// for the given channelKey, ordered by ts ASC.
func (s *TurnStore) QueryByScore(channelKey string, highThreshold int) ([]TurnRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, ts, channel_key, score, intent, tags, tokens, status, user_msg, reply, tool_calls
		FROM turns WHERE channel_key = ? AND score >= ? AND status != 'archived'
		ORDER BY ts ASC`, channelKey, highThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTurns(rows)
}

// QueryByTags returns turns matching any of the given tags for the channelKey.
// Uses the turn_tags inverted index — exact match, no full-table scan.
// Returns non-archived turns with score > 0, ordered by ts ASC.
func (s *TurnStore) QueryByTags(channelKey string, tags []string) ([]TurnRecord, error) {
	if len(tags) == 0 {
		return nil, nil
	}

	// Normalise and deduplicate tags.
	seen := make(map[string]struct{}, len(tags))
	placeholders := make([]string, 0, len(tags))
	args := make([]any, 0, len(tags)+2)
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		placeholders = append(placeholders, "?")
		args = append(args, t)
	}
	if len(placeholders) == 0 {
		return nil, nil
	}

	// Append channelKey and score filter args.
	args = append(args, channelKey, 0)

	query := fmt.Sprintf(`
		SELECT DISTINCT t.id, t.ts, t.channel_key, t.score, t.intent,
		       t.tags, t.tokens, t.status, t.user_msg, t.reply, t.tool_calls
		FROM turn_tags tt
		JOIN turns t ON t.id = tt.turn_id
		WHERE tt.tag IN (%s)
		  AND tt.channel_key = ?
		  AND t.score > ?
		  AND t.status != 'archived'
		ORDER BY t.ts ASC`,
		strings.Join(placeholders, ", "))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTurns(rows)
}

// QueryTagsForChannel returns all distinct tags that have at least one non-archived
// turn for the given channelKey. Useful for building tag clouds or auto-suggestions.
func (s *TurnStore) QueryTagsForChannel(channelKey string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT tt.tag
		FROM turn_tags tt
		JOIN turns t ON t.id = tt.turn_id
		WHERE tt.channel_key = ? AND t.status != 'archived'
		ORDER BY tt.tag ASC`, channelKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return tags, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// QueryRecent returns the n most-recent non-archived turns for a channelKey,
// ordered by ts ASC (oldest first, so they can be appended naturally).
func (s *TurnStore) QueryRecent(channelKey string, n int) ([]TurnRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, ts, channel_key, score, intent, tags, tokens, status, user_msg, reply, tool_calls
		FROM turns
		WHERE channel_key = ? AND status != 'archived'
		ORDER BY ts DESC LIMIT ?`, channelKey, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	turns, err := scanTurns(rows)
	if err != nil {
		return nil, err
	}
	// Reverse to ascending order.
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	return turns, nil
}

// ArchiveOldProcessed marks processed turns older than olderThanDays as 'archived'.
// At most 100 rows are archived per call to limit lock time.
func (s *TurnStore) ArchiveOldProcessed(olderThanDays int) error {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays).Unix()
	_, err := s.db.Exec(`
		UPDATE turns SET status = 'archived'
		WHERE id IN (
			SELECT id FROM turns
			WHERE status = 'processed' AND ts < ?
			ORDER BY ts ASC LIMIT 100
		)`, cutoff)
	return err
}

// ---------------------------------------------------------------------------
// Token statistics
// ---------------------------------------------------------------------------

// TokenStats holds aggregated token usage.
type TokenStats struct {
	Turns       int
	TotalTokens int
	AvgTokens   int
}

// QueryTokenStats returns aggregated token usage for the given time window.
// sinceUnix = 0 means all-time.
func (s *TurnStore) QueryTokenStats(sinceUnix int64) (TokenStats, error) {
	var (
		q    string
		args []any
	)
	if sinceUnix > 0 {
		q = `SELECT COUNT(*), COALESCE(SUM(tokens),0) FROM turns WHERE ts >= ? AND status != 'archived'`
		args = []any{sinceUnix}
	} else {
		q = `SELECT COUNT(*), COALESCE(SUM(tokens),0) FROM turns WHERE status != 'archived'`
	}
	var st TokenStats
	row := s.db.QueryRow(q, args...)
	if err := row.Scan(&st.Turns, &st.TotalTokens); err != nil {
		return st, err
	}
	if st.Turns > 0 {
		st.AvgTokens = st.TotalTokens / st.Turns
	}
	return st, nil
}

// ChannelTokenStats holds token usage for one channel.
type ChannelTokenStats struct {
	ChannelKey  string
	Turns       int
	TotalTokens int
}

// QueryTokenStatsByChannel returns aggregated token usage per channel for the given time window.
// Returns a slice ordered by totalTokens DESC.
func (s *TurnStore) QueryTokenStatsByChannel(sinceUnix int64, limit int) ([]ChannelTokenStats, error) {
	var (
		q    string
		args []any
	)
	if sinceUnix > 0 {
		q = `SELECT channel_key, COUNT(*), COALESCE(SUM(tokens),0)
		     FROM turns WHERE ts >= ? AND status != 'archived'
		     GROUP BY channel_key ORDER BY 3 DESC LIMIT ?`
		args = []any{sinceUnix, limit}
	} else {
		q = `SELECT channel_key, COUNT(*), COALESCE(SUM(tokens),0)
		     FROM turns WHERE status != 'archived'
		     GROUP BY channel_key ORDER BY 3 DESC LIMIT ?`
		args = []any{limit}
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelTokenStats
	for rows.Next() {
		var r ChannelTokenStats
		if err := rows.Scan(&r.ChannelKey, &r.Turns, &r.TotalTokens); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}


// ---------------------------------------------------------------------------
// Internal scanner
// ---------------------------------------------------------------------------

func scanTurns(rows *sql.Rows) ([]TurnRecord, error) {
	var out []TurnRecord
	for rows.Next() {
		var r TurnRecord
		var tagsJSON, tcJSON string
		if err := rows.Scan(
			&r.ID, &r.Ts, &r.ChannelKey, &r.Score, &r.Intent,
			&tagsJSON, &r.Tokens, &r.Status,
			&r.UserMsg, &r.Reply, &tcJSON,
		); err != nil {
			return out, err
		}
		r.Tags = unmarshalTags(tagsJSON)
		r.ToolCalls = unmarshalToolCalls(tcJSON)
		out = append(out, r)
	}
	return out, rows.Err()
}
