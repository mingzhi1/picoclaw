# Runtime Loop Implementation Tasks

> Design ref: `docs/design/picoclaw_runtime_loop_design.md`

## Design Decisions

| Decision | Conclusion |
|----------|------------|
| Turn storage | SQLite `turns.db` |
| Active Context fields | `CurrentFiles` + `RecentErrors` only |
| Phase 1 short-circuit | No — short messages need Active Context most |
| Async write | Direct `go insert()`, no channel buffer |
| Token budget check | Periodic time-based archival instead |
| Tag-gated tools | Deferred until tool count > 15 |
| KV Cache | Fixed ordering (high-score first, by ID ASC) |

## Milestones

### M1: Turn Score + Phase 3 Timing — ✅

- [x] `score.go`: `CalcTurnScore(input) int`
- [x] Split `RunPostLLM` → `SyncPhase3` (sync, < 2ms) + `AsyncPhase3` (goroutine)
- [x] Adjust `runAgentLoop` timing: score → publish → async write

### M2: Active Context — ✅

- [x] `active_context.go`: per `channel:chatID` store
- [x] Fields: `CurrentFiles` (5), `RecentErrors` (3)
- [x] Injected as user message (not system prompt) for KV cache stability
- [x] Flush to JSON on shutdown, load on startup

### M3: TurnStore — ✅

- [x] `turn_store.go`: SQLite `turns.db` with WAL mode
- [x] Methods: Insert, QueryPending, QueryByScore, QueryByTags, QueryRecent, ArchiveOldProcessed
- [x] Async insert via goroutine in AsyncPhase3

### M4: MemoryDigest — ✅

- [x] `memory_digest.go`: background worker (5min interval)
- [x] QueryPending → group by channel → LLM batch extraction → MemoryStore
- [x] Removed `MemoryExtractor` and `CotEvaluator` processors (kept `ErrorTracker`)

### M5: Instant Memory + KV Cache Ordering — ✅

- [x] `instant_memory.go`: dynamic window from TurnStore
- [x] Cache-friendly message ordering: system → memory → high-score → recent → current
- [x] Legacy SessionManager kept as fallback

## Deferred

| Item | Reason |
|------|--------|
| Tag-gated tool loading | Tool count < 10 currently |
| Summary Anchor | Fixed ordering sufficient for v1 |