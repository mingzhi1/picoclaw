# Memory System Design

> Status: Implemented | Updated: 2026-03-25

## Overview

PicoClaw implements a **five-layer memory hierarchy** that balances speed, relevance, and persistence. Each layer serves a specific purpose in the agent's cognitive process.

```
┌─────────────────────────────────────────────────────────────┐
│                    User Message                              │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 1: Instant Memory (Per-turn, dynamic window)         │
│  - TurnStore query by tags + score threshold                │
│  - Channel-isolated (Telegram turns ≠ CLI turns)            │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 2: Active Context (Per-session)                      │
│  - CurrentFiles: files being worked on                      │
│  - RecentErrors: last 3 errors for debugging continuity     │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 3: Long-term Memory (Persistent, distilled facts)    │
│  - MemoryDigest: background worker extracts durable facts   │
│  - Searchable by tags via inverted index                    │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 4: KV Cache (Persistent, O(1) access)                │
│  - Write-through (memory + SQLite)                          │
│  - Lazy TTL eviction                                        │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 5: Fact Store (Persistent, structured knowledge)     │
│  - Entity-Attribute-Value with versioning                   │
│  - Tracks code entities, configs, user preferences          │
└─────────────────────────────────────────────────────────────┘
```

## Layer 1: Instant Memory

**Purpose:** Provide immediate context for the current turn.

**Lifetime:** Per-turn (rebuilt each message).

**Storage:** In-memory slice, sourced from TurnStore (SQLite).

**Selection Criteria:**
- Score ≥ threshold (configurable, default 4)
- Tags match extracted intent
- Channel-isolated (Telegram turns don't pollute CLI context)
- Recent turns weighted higher

**Implementation:**
```go
// pkg/agent/instant_memory.go
func (al *AgentLoop) BuildInstantMemory(
    ctx context.Context,
    channelKey string,
    tags []string,
    intent string,
) ([]providers.Message, error)
```

**TurnStore Schema:**
```sql
CREATE TABLE turns (
    turn_id     TEXT PRIMARY KEY,
    channel_key TEXT,
    user_msg    TEXT,
    assistant   TEXT,
    intent      TEXT,
    score       INTEGER,
    ts          INTEGER,
    topic_id    TEXT REFERENCES topics(id)
);

CREATE TABLE turn_tags (
    tag         TEXT,
    channel_key TEXT,
    turn_id     TEXT,
    ts          INTEGER,
    PRIMARY KEY (tag, channel_key, turn_id)
);
CREATE INDEX idx_turn_tags_lookup ON turn_tags(tag, channel_key, ts);
```

**Query Strategy:**
```sql
SELECT DISTINCT t.*
FROM turns t
JOIN turn_tags tt ON t.turn_id = tt.turn_id
WHERE tt.tag IN (?, ?, ...)
  AND t.channel_key = ?
  AND t.score >= ?
ORDER BY t.ts DESC
LIMIT ?
```

---

## Layer 2: Active Context

**Purpose:** Maintain session-level continuity for ongoing tasks.

**Lifetime:** Per-session (cleared on session end or explicit reset).

**Storage:** In-memory struct.

**Components:**
- **CurrentFiles**: Files being actively edited (path + last content hash)
- **RecentErrors**: Last 3 errors with stack traces for debugging continuity

**Injection Point:** Appended as **user message** (not system prompt) to keep system prompt stable for KV caching.

**Implementation:**
```go
// pkg/agent/active_context.go
type ActiveContext struct {
    CurrentFiles []string `json:"current_files"` // newest first, max 5
    RecentErrors []string `json:"recent_errors"` // newest first, max 3
}

// ActiveContextStore is a thread-safe in-memory map of channel:chatID → ActiveContext.
// Backed by SQLite for persistence (db may be nil for memory-only mode).
type ActiveContextStore struct {
    mu   sync.RWMutex
    data map[string]*ActiveContext // key = "channel:chatID"
    db   *sql.DB
}
```

---

## Layer 3: Long-term Memory

**Purpose:** Distill durable facts from conversation turns.

**Lifetime:** Persistent (until explicit deletion).

**Storage:** SQLite `memory.db`.

**Extraction Process:**
```
TurnStore (raw turns)
    │
    │ Background goroutine (MemoryDigestWorker)
    │ Runs every N minutes or after M turns
    ▼
MemoryDigest LLM call
    │
    │ Extracts facts like:
    │ - "User prefers Go over Python"
    │ - "Project uses SQLite for persistence"
    │ - "API key stored in ~/.config/app/key"
    ▼
memory_entries table (tagged, searchable)
```

**Schema:**
```sql
CREATE TABLE memory_entries (
    id         INTEGER PRIMARY KEY,
    content    TEXT,
    source_turn_id TEXT,
    ts         INTEGER,
    tags       TEXT  -- JSON array
);
CREATE INDEX idx_memory_tags ON memory_entries(tags);
```

**LLM Prompt Template:**
```
Extract durable facts from these conversation turns.
Facts should be:
- General (not turn-specific)
- Reusable across sessions
- Tagged for retrieval

Turns:
{turn_content}

Output JSON:
{
  "facts": [
    {"content": "...", "tags": ["go", "preference"]},
    ...
  ]
}
```

---

## Layer 4: KV Cache

**Purpose:** O(1) access to frequently-used data.

**Lifetime:** Persistent with TTL.

**Storage:** In-memory map + SQLite `cache.db` (write-through).

**Use Cases:**
- System prompt caching (stable prefix)
- Turn history for same tags (avoid re-querying TurnStore)
- LLM response caching (identical requests)

**Implementation:**
```go
// pkg/infra/kvcache/kvcache.go
type Store struct {
    mu    sync.RWMutex
    mem   map[string]*CacheEntry
    db    *sql.DB
    ttl   time.Duration
}

// Write-through: always writes to both memory and SQLite
func (s *Store) Set(key string, value []byte, ttl time.Duration) error
```

**Eviction Strategy:**
- Lazy TTL check on read (no background goroutine)
- Memory cap: LRU eviction when entries > N

---

## Layer 5: Fact Store

**Purpose:** Track structured, versioned knowledge about code entities and user environment.

**Lifetime:** Persistent (versioned history).

**Storage:** SQLite `turns.db` (co-located with turns for transactional consistency).

**Schema:**
```sql
CREATE TABLE facts (
    id         INTEGER PRIMARY KEY,
    entity     TEXT,      -- e.g., "file:pkg/agent/loop.go"
    attribute  TEXT,      -- e.g., "line_count", "last_modified"
    value      TEXT,      -- e.g., "967", "2026-03-25T10:30:00Z"
    turn_id    TEXT,      -- which turn created this fact
    ts         INTEGER
);
CREATE INDEX idx_facts_entity ON facts(entity, attribute);
```

**Use Cases:**
- Track file modifications (for context assembly)
- Remember user preferences (editor, shell, etc.)
- Code entity metadata (function signatures, config keys)

---

## Turn Scoring Algorithm

Turns are scored in **SyncPhase3** (<2ms) to determine retention priority:

```go
// pkg/agent/score.go
func CalcTurnScore(input RuntimeInput) int {
    score := 0

    if len(input.ToolCalls) > 0 {
        score += 3  // Tool usage indicates task-oriented turn
    }

    for _, tc := range input.ToolCalls {
        if isWriteTool(tc.Name) {
            score += 2  // Write/edit tools have lasting effect
            break
        }
    }

    if len(input.ToolCalls) > 3 {
        score += 2  // Lots of tool activity
    }

    switch record.Intent {
    case "task", "code", "debug":
        score += 3
    case "question":
        score += 1
    }

    if len(record.AssistantReply) > 500 {
        score += 2  // Long replies usually contain valuable info
    }

    if len(record.UserMessage)+len(record.AssistantReply) < 80 {
        score -= 2  // Short exchanges are low-value
    }

    // Explicit importance markers
    if containsImportanceKeyword(record.UserMessage) {
        score += 3  // "remember", "记住", "important", "重要"
    }

    // Checkpoint awareness (multi-level)
    if input.CheckpointSummary != "" {
        score += 2  // has checkpoint activity
        if allResolved {
            score += 3  // task completion turn
        }
        if hasFailed {
            score += 1  // failures are informative
        }
    }

    return score
}
```

**Score Thresholds:**
- **≥ 7**: "Always keep" — injected into every context
- **4-6**: Rolling window — retained while recent
- **< 3**: Early eviction — first to be removed under pressure

---

## Topic Tracking

**Purpose:** Organize long conversations into coherent units for better context management.

**Lifecycle:**
```
User message
    │
    ▼
Analyser extracts topic_action:
    - "new" with title → Create topic
    - "continue" → Append to current
    - "resolve" → Mark complete, start new

Turn stored with topic_id foreign key
    │
    ▼
Instant Memory query filters by:
    - Current active topic (if any)
    - Recently resolved topics (for continuity)
```

**Schema:**
```sql
CREATE TABLE topics (
    id           TEXT PRIMARY KEY,
    channel_key  TEXT,
    title        TEXT,
    status       TEXT,  -- "active" | "resolved"
    created_ts   INTEGER,
    resolved_ts  INTEGER
);

ALTER TABLE turns ADD COLUMN topic_id TEXT REFERENCES topics(id);
```

**Benefits:**
- Prevents context pollution from unrelated past conversations
- Enables "resume topic N" functionality
- Automatic cleanup: resolved topics evicted faster

---

## Checkpoint Tracking

**Purpose:** Enable resumable long-running tasks with progress visibility.

**Flow:**
```
Analyser generates checkpoints[]:
  [
    {"text": "Read config file", "skippable": false},
    {"text": "Parse YAML structure", "skippable": false},
    {"text": "Validate required fields", "skippable": true},
    {"text": "Apply defaults", "skippable": true},
    {"text": "Write updated config", "skippable": false}
  ]

Phase 2 (Execute):
  LLM completes each checkpoint → reports progress
  CheckpointTracker marks complete/incomplete

Phase 3 (Reflect):
  Store checkpoint summary in TurnRecord
  User can query: "Where did we stop?"
```

**Implementation:**
```go
// pkg/agent/checkpoint_tracker.go
type CheckpointTracker struct {
    mu         sync.RWMutex
    checkpoints []CheckpointItem
    completed  map[int]bool
}

func (ct *CheckpointTracker) ReportProgress(completedIdx []int)
func (ct *CheckpointTracker) GetSummary() string  // "3/5 complete: ..."
```

---

## Channel Isolation

**Problem:** Telegram conversations have different context than CLI sessions.

**Solution:** All memory queries include `channel_key` filter:

```go
channelKey := fmt.Sprintf("%s:%s", channelName, chatID)

// TurnStore query
SELECT * FROM turns
WHERE channel_key = ?  -- "telegram:123456789"
  AND ...
```

**Result:** Each channel maintains independent memory context.

---

## Performance Characteristics

| Operation | Latency | Frequency |
|-----------|---------|-----------|
| Instant Memory build | 50-200ms (SQLite query) | Per-turn |
| Active Context inject | <1ms (in-memory) | Per-turn |
| Turn scoring | <2ms | Per-turn (sync) |
| TurnStore insert | 5-20ms (SQLite tx) | Per-turn (async) |
| MemoryDigest extraction | 500-2000ms (LLM call) | Background (every N turns) |
| KV cache read | <1µs (memory) / 1ms (SQLite) | Per-turn |
| Fact Store update | 1-5ms | Per tool execution |

---

## Configuration

```json
{
  "memory": {
    "instant_memory": {
      "score_threshold": 4,
      "max_turns": 20,
      "channel_isolated": true
    },
    "active_context": {
      "max_files": 5,
      "max_errors": 3
    },
    "memory_digest": {
      "enabled": true,
      "interval_turns": 10,
      "model_name": "gemini/gemini-2.0-flash-exp"
    },
    "kv_cache": {
      "enabled": true,
      "ttl_hours": 24,
      "max_memory_entries": 1000
    }
  }
}
```
