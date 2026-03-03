# Runtime Loop Design

> Status: Implemented | Date: 2026-03-02

## Architecture

```
Message ──→ Phase 1 (Analyse) ──→ Phase 2 (Execute) ──→ Phase 3 (Reflect)
```

| Phase | File | Responsibility |
|-------|------|----------------|
| **Analyse** | `analyser.go` | Lightweight LLM → intent, tags, CoT prompt |
| **Execute** | `executor.go` | LLM iteration loop + tool calling |
| **Reflect** | `reflector.go` | Turn scoring, TurnRecord persistence, slash commands |

## Turn Definition

One Turn = user message + Phase 1 result + all Phase 2 iterations + Phase 3 output.
Multiple tool-call iterations within Phase 2 count as **one Turn**.

## Phase 1: Analyse

- Uses configurable `analyser_model` (fast/cheap model, falls back to main model)
- Inputs: user message, Active Context, available tags, CoT learning data
- Outputs: `intent`, `tags[]`, `cot_prompt`
- After analysis: memory retrieval by tags, CoT injection into system prompt

## Phase 2: Execute

- LLM → tool call → tool result loop until no more tool calls
- Retry logic for context window overflow with automatic compression
- Reasoning output forwarded to dedicated channels

## Phase 3: Reflect

- **SyncPhase3** (< 2ms, before response sent): turn scoring + Active Context update
- **AsyncPhase3** (after response): `go turnStore.Insert(record)` → SQLite
- Slash commands: `/memory`, `/cot`, `/shell`, `/show`, `/list`, `/switch`, `/help`

## Scoring Rules

| Condition | Score |
|-----------|-------|
| Has tool calls | +3 |
| Write/edit tools | +2 |
| intent = task/code/debug | +3 |
| Reply > 500 chars | +2 |
| Short exchange < 80 chars | -2 |

## Memory Hierarchy

| Layer | Lifetime | Purpose |
|-------|----------|---------|
| Instant Memory | Per-turn | Dynamic window from TurnStore (score + tag filtering) |
| Active Context | Per-session | `CurrentFiles` + `RecentErrors`, injected into user prompt |
| Long-term Memory | Persistent | MemoryDigest batch extraction → `memory.db` |

## Multi-Model Support

| Config Field | Phase | Fallback |
|-------------|-------|----------|
| `model_name` | Phase 2 | — |
| `analyser_model` | Phase 1 | → `model_name` |
| `digest_model` | MemoryDigest | → `model_name` |

## Message Ordering (KV Cache Friendly)

```
[system_prompt]              → always cached
[long_term_memory by tags]   → cached when same tags
[always_keep turns (score≥7)] → fixed position, append-only
[recent turns]               → rolling window
[current user message]       → new each turn
```

Active Context injected as **user message** (not system prompt) to keep system prompt prefix stable.
