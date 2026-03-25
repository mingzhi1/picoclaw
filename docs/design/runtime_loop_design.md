# Runtime Loop Design

> Status: Implemented | Updated: 2026-03-25

## Architecture

```
Message ──→ Phase 1 (Analyse) ──→ Phase 2 (Execute) ──→ Phase 3 (Reflect)
```

| Phase | File | Responsibility |
|-------|------|----------------|
| **Analyse** | `analyser.go` | Lightweight LLM → intent, tags, tool_hints, CoT prompt, checkpoints, topic_action |
| **Execute** | `loop.go` + `executor.go` | LLM iteration loop + tool calling + checkpoint tracking |
| **Reflect** | `reflector.go` | Turn scoring, TurnRecord persistence, slash commands, memory extraction |

## Turn Definition

One Turn = user message + Phase 1 result + all Phase 2 iterations + Phase 3 output.
Multiple tool-call iterations within Phase 2 count as **one Turn**.

Each turn is assigned a unique ID: `{channel_hash}_{timestamp_base62}_{counter}`

## Phase 1: Analyse

- Uses configurable `auxiliary_model` (fallback chain: `auxiliary_model` → `analyser_model` (deprecated) → `pre_llm_model` (deprecated) → primary model)
- Inputs: user message, Active Context, available tags, CoT learning data
- Outputs:
  - `intent`: classification label (chat/task/question/code/debug/greeting)
  - `tags[]`: extracted tags for memory retrieval
  - `tool_hints[]`: tool category hints (file/exec/web/skill/spawn/message/device/mcp)
  - `cot_id`: built-in CoT template selector (code/debug/analytical/direct)
  - `cot_prompt`: task-specific thinking strategy
  - `checkpoints[]`: execution plan items (skippable for flexibility)
  - `topic_action`: topic lifecycle decision (continue/new/resolve)
- After analysis: memory retrieval by tags, CoT injection into system prompt
- Topic tracking: creates/continues/resolves topics based on conversation flow

## Phase 2: Execute

- LLM → tool call → tool result loop until no more tool calls
- Retry logic for context window overflow with automatic compression
- Reasoning output forwarded to dedicated channels (if configured)
- Checkpoint tracking: LLM reports progress after each checkpoint item
- Topic tracking: updates topic state, accumulates turn context
- Cost cap: aborts if cumulative tokens exceed 2x agent's context window
- Multi-agent routing: AgentRegistry resolves target agent per message

## Phase 3: Reflect

- **SyncPhase3** (< 2ms, before response sent):
  - Turn scoring (determines if turn is "always kept" in future contexts)
  - Active Context update (CurrentFiles + RecentErrors)
  - Checkpoint summary generation
- **AsyncPhase3** (after response):
  - `go turnStore.Insert(record)` → SQLite with tag inverted index
  - Topic state persistence
  - Fact extraction (Entity-Attribute-Value)
- Slash commands: `/memory`, `/cot`, `/shell`, `/show`, `/list`, `/switch`, `/help`, `/runtime`, `/rag`, `/tokens`

## Scoring Rules

| Condition | Score |
|-----------|-------|
| Has tool calls | +3 |
| Write/edit tools | +2 |
| Tool count > 3 | +2 |
| intent = task/code/debug | +3 |
| Reply > 500 chars | +2 |
| Short exchange < 80 chars | -2 |
| "remember"/"记住"/"important"/"重要" keyword | +3 |
| Checkpoint progress (basic) | +2 |
| All checkpoints resolved | +3 |
| Checkpoint failure | +1 |

**Thresholds:**
- Score ≥ 7: "always keep" turn (injected into every context)
- Score ≥ 4: retained in rolling window
- Score < 3: eligible for early eviction

## Memory Hierarchy

| Layer | Lifetime | Purpose |
|-------|----------|---------|
| **Instant Memory** | Per-turn | Dynamic window from TurnStore (score ≥ threshold + tag match, channel-isolated) |
| **Active Context** | Per-session | `CurrentFiles` + `RecentErrors`, injected into user prompt |
| **Long-term Memory** | Persistent | MemoryDigest batch extraction → `memory.db`, searchable by tags |
| **KV Cache** | Persistent | Write-through (memory + SQLite), O(1) read, lazy TTL eviction |
| **Fact Store** | Persistent | Entity-Attribute-Value facts with versioning |

## Multi-Model Support

| Config Field | Phase | Fallback |
|-------------|-------|----------|
| `primary_model` | Phase 2 (Execute) | — |
| `auxiliary_model` | Phase 1 (Analyse) | → `analyser_model` (deprecated) → `primary_model` |
| `digest_model` | MemoryDigest | → `auxiliary_model` |
| `stt_model` | Voice/STT Extension | — |

## Message Ordering (KV Cache Friendly)

```
[system_prompt]              → always cached
[long_term_memory by tags]   → cached when same tags
[always_keep turns (score≥7)] → fixed position, append-only
[recent turns]               → rolling window
[current user message]       → new each turn
```

Active Context injected as **user message** (not system prompt) to keep system prompt prefix stable.

## Topic Tracking

Topics organize long conversations into coherent units:

```
User: "Help me fix this bug..."  → New topic: "Debug foo.go nil pointer"
User: "Also check the tests"     → Continue same topic
User: "Great, it works!"         → Resolve topic
User: "New task: add feature X"  → New topic: "Feature X implementation"
```

**Topic Actions:**
- `new`: Create new topic with title
- `continue`: Append turn to current topic
- `resolve`: Mark topic as completed

**Storage:** Topics stored in `turns.db` with foreign key to turns.
