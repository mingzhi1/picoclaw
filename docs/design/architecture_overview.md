# PicoClaw Architecture Overview

> Status: Living Document | Updated: 2026-03-25

## Philosophy

PicoClaw started as an ultra-lightweight personal AI agent inspired by [nanobot](https://github.com/HKUDS/nanobot). It has since evolved into a **lightweight agent framework** — still minimal in dependencies, but richer in architecture.

**Core principles:**
- **Go-native**: Single binary, no external runtime, cross-platform (RISC-V / ARM / x86)
- **Layered, not monolithic**: Each phase is a separate, testable component
- **Convention over configuration**: Sensible defaults, deep customization when needed
- **Channel-agnostic**: Same agent logic serves CLI, Telegram, Feishu, Discord, and more
- **Extension-first**: Optional capabilities (voice, devices) plug in via a unified lifecycle framework
- **Security-first**: 4-layer filesystem sandbox + command guard with allow/deny patterns

## System Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                       Entry Points                                │
│  Telegram · Feishu · CLI · Discord · LINE · DingTalk · WhatsApp  │
└────────────────────────┬──────────────────────────────────────────┘
                         │  channels.Manager (two-phase StopAll)
                         │  bus.MessageBus (pub/sub)
                         ▼
┌──────────────────────────────────────────────────────────────────┐
│                    AgentLoop (loop.go)                           │
│                                                                  │
│  ┌──────────┐  ┌─────────────────┐  ┌──────────────────┐       │
│  │ Phase 1  │  │    Phase 2      │  │     Phase 3      │       │
│  │ Analyse  │→ │    Execute      │→ │     Reflect      │       │
│  │          │  │                 │  │                  │       │
│  │ intent   │  │  LLM ↔ Tools   │  │ score + persist  │       │
│  │ tags     │  │  iterations     │  │ TurnStore        │       │
│  │ CoT      │  │  multi-agent    │  │ ActiveContext    │       │
│  └──────────┘  └─────────────────┘  └──────────────────┘       │
│                                                                  │
│  ┌───────────────────┐  ┌──────────────────────────────┐       │
│  │  AgentRegistry    │  │   Extension Manager          │       │
│  │  multi-agent pool │  │   voice, devices, ...        │       │
│  └───────────────────┘  └──────────────────────────────┘       │
│                                                                  │
│  ┌───────────────────┐  ┌──────────────────────────────┐       │
│  │  TopicTracker     │  │   CheckpointTracker          │       │
│  │  topic lifecycle  │  │   LLM execution checkpoints  │       │
│  └───────────────────┘  └──────────────────────────────┘       │
└──────────────────────────────────────────────────────────────────┘
          │                      │                  │
  ┌───────────┐         ┌──────────────┐    ┌────────────┐
  │   Tools   │         │    Memory    │    │   Skills   │
  │ Registry  │         │    System    │    │   Loader   │
  │ (+ MCP)   │         │ TurnStore    │    │ 3-layer    │
  └───────────┘         │ KVCache      │    └────────────┘
                        │ MemoryDigest │
                        │ FactStore    │
                        └──────────────┘
```

## Package Map

| Package | Responsibility | Key Files |
|---------|---------------|-----------|
| `pkg/agent` | Runtime loop, phases, routing, TurnStore, topic tracking | `loop.go`, `analyser.go`, `executor.go`, `reflector.go`, `context.go`, `instance.go`, `turn_store.go`, `topic/`, `routing/`, `checkpoint_tracker.go` |
| `pkg/tools` | Tool interface + builtin implementations | `registry.go`, `toolloop.go`, `filesystem.go`, `exec.go`, `shell_instance.go`, `subagent.go`, `mcp_tool.go` |
| `pkg/channels` | Channel adapters + outbound workers | `manager.go`, `telegram/`, `feishu/`, `cli/`, `webhook.go` |
| `pkg/extension` | Optional capability lifecycle framework | `extension.go` (Manager), `voice/` |
| `pkg/infra/config` | Config loading, model/provider resolution | `config.go`, `load.go` |
| `pkg/infra/media` | Media file ref management (always-on) | `store.go` (MediaStore interface + FileMediaStore) |
| `pkg/infra/kvcache` | Write-through KV cache (memory + SQLite) | `kvcache.go` |
| `pkg/infra/logger` | Structured context-tagged logging | `logger.go` |
| `pkg/infra/store` | SQLite store abstraction + migrations | `store.go` |
| `pkg/infra/vectorstore` | In-memory vector store for RAG | `vectorstore.go` |
| `pkg/infra/cron` | Scheduled task execution | `cron.go` |
| `pkg/llm` | LLM provider abstraction + implementations | `providers/openai_compat/`, `providers/anthropic/`, `providers/factory.go`, `providers/fallback.go`, `auth/`, `mcp/` |
| `pkg/skills` | Skill loading + keyword matching | `loader.go`, `clawhub_registry.go`, `search_cache.go` |
| `pkg/core` | Shared bus types, constants, session, state | `bus/`, `session/`, `state/`, `identity/`, `constants.go` |

### `pkg/infra` Sub-packages

```
pkg/infra/
├── config/          Config + model list resolution
├── cron/            Scheduled task service
├── health/          HTTP health check server
├── heartbeat/       Periodic alive-ping to gateway
├── httpclient/      Shared HTTP client with timeouts
├── kvcache/         Write-through KV (memory + SQLite)
├── logger/          Structured CF logger
├── media/           MediaStore: ref ↔ local path + TTL cleanup
├── store/           SQLite store abstraction + migrations
├── utils/           String truncation, misc helpers
└── vectorstore/     In-memory vector store for RAG
```

## Phase Details

### Phase 1: Analyse (`analyser.go`)

Fast/cheap auxiliary LLM extracts structured metadata before the main LLM runs.

```
Input:  user message + available tags + CoT learning data
Output: { intent, tags[], tool_hints[], cot_id, cot_prompt, checkpoints[], topic_action }
```

- Uses `auxiliary_model` (configurable; fallback chain: `auxiliary_model` → `analyser_model` (deprecated) → `pre_llm_model` (deprecated) → `primary_model`)
- Keyword-based skill matching → injects Tool Execution Plan into system prompt
- CoT prompt injection from learning history (`cot_id` selects template, `cot_prompt` is task-specific)
- Generates `checkpoints[]` — an execution plan for Phase 2 (skippable items for flexibility)
- Topic action decision: `continue` | `new` | `resolve` for long conversation lifecycle

### Phase 2: Execute (`loop.go` + `executor.go`)

Main LLM iteration loop with tool calling.

```
repeat:
  LLM(system_prompt + messages + tools) → response
  if response.has_tool_calls:
    execute tools → append results
    update checkpoints (CheckpointTracker)
  else:
    break → final answer
```

- `ContextBuilder` assembles messages with Instant Memory from TurnStore
- Retry on context overflow with automatic message compression
- Multi-agent routing via `AgentRegistry`
- `SubagentManager`: concurrent-safe (value-copy snapshots, no pointer leaks)
- Checkpoint tracking: LLM reports progress, enables resumable long tasks
- Topic tracking: maintains topic state across turns, auto-resolve stale topics

### Phase 3: Reflect (`reflector.go`)

Post-LLM processing, split into sync and async:

- **SyncPhase3** (<2ms): Turn scoring + Active Context update + Checkpoint summary
- **AsyncPhase3**: `TurnStore.Insert()` → SQLite persistence (with tag inverted index)
- Slash commands: `/memory`, `/shell`, `/show`, `/switch`, `/help`, `/cot`, `/runtime`, `/rag`, `/tokens`
- Memory extraction: background `MemoryDigestWorker` distills durable facts

## Tools Architecture

```
pkg/tools/
├── Core Framework
│   base.go           Tool / AsyncTool / ContextualTool interfaces
│   types.go          Message, ToolCall, LLMResponse types
│   result.go         ToolResult with ForLLM / ForUser / IsError
│   registry.go       ToolRegistry (register, execute, schema)
│   toolloop.go       RunToolLoop (reusable LLM+tool iteration)
│
├── File Operations
│   filesystem.go     read_file, write_file, list_dir
│                     fileSystem interface: hostFs / sandboxFs / whitelistFs / tildeExpandFs
│   edit.go           edit_file, append_file
│
├── Command Execution
│   exec.go            ExecTool with guardCommand security
│   exec_llm_review.go LLM semantic review (optional risk explanation)
│   exec_process_*.go  Platform-specific process management
│   exec_security.go   Command guard: deny/allow patterns, workspace restriction
│
├── Shell Instance       ← Pure Go, no process spawn
│   shell_builtins.go    ls, cat, head, tail, grep, wc, find, diff, tree, etc.
│   shell_instance.go    ShellInstance: unified entry for /shell + other callers
│
├── Subagent
│   subagent.go          SubagentManager (orchestrator → worker pattern)
│   spawn.go             SpawnTool (async) + SubagentTool (sync)
│
├── Communication
│   message.go           MessageTool (send message to user, dedup-aware)
│   web.go               web_search, fetch_url
│   mcp_tool.go          MCP server tool adapter
│
├── Skills
│   skills_install.go    Install skills from registry
│   skills_search.go     Search skill registry
│
├── Cron
│   cron.go              Scheduled task execution
│
├── RAG
│   rag_search.go        RAG search with vector store
│
└── Checkpoint
    checkpoint.go        Checkpoint tool for LLM progress reporting
```

### Security Model

```
File path validation:
  tildeExpandFs          ~ → $HOME expansion (all file tools)
    → whitelistFs        Config allow_read_paths / allow_write_paths
      → sandboxFs        os.Root workspace restriction
        → hostFs         Direct filesystem access

ExecTool command guard:
  denyPatterns           Block rm -rf, format, dd, shutdown, fork bombs
  allowPatterns          Custom overrides (e.g. git push origin)
  customAllowPatterns    Exempt specific commands from deny checks
  workspace restriction  Path validation for working_dir
  safePaths              /dev/null, /dev/zero, /dev/urandom always allowed
```

## Memory Hierarchy

| Layer | Scope | Storage | Purpose |
|-------|-------|---------|---------|
| **Instant Memory** | Per-turn | In-memory | Dynamic window: TurnStore (score ≥ threshold + tag match, channel-isolated) |
| **Active Context** | Per-session | In-memory | CurrentFiles + RecentErrors, injected as user message |
| **Long-term Memory** | Persistent | SQLite `memory.db` | MemoryDigest batch extraction, searchable by tags |
| **KV Cache** | Persistent | SQLite `cache.db` | Write-through (memory + SQLite), O(1) read, lazy TTL eviction |
| **Fact Store** | Persistent | SQLite `turns.db` | Entity-Attribute-Value facts with versioning |

### TurnStore Tag Index

Tags are stored in a separate **inverted index** table — no more `LIKE '%tag%'` full scans:

```sql
-- turn_tags: one row per (tag, channel, turn)
CREATE TABLE turn_tags (
    tag         TEXT,
    channel_key TEXT,
    turn_id     TEXT,
    ts          INTEGER,
    PRIMARY KEY (tag, channel_key, turn_id)
);
CREATE INDEX idx_turn_tags_lookup ON turn_tags(tag, channel_key, ts);
```

`QueryByTags` does an indexed JOIN instead of a LIKE scan — O(k) where k = matching rows, exact match, deduplication built-in.

`Insert()` wraps both `turns` and `turn_tags` writes in a single transaction for consistency.

### Turn ID Format

Channel-aware, time-sortable, compact: `{channel_hash}_{timestamp_base62}_{counter}`

Example: `12tdkW_VCrYNEP_1`

### Topic Tracker

Long conversations are organized into **topics** with lifecycle management:

- **New topic**: Analyser detects topic shift → creates new topic record
- **Continue**: Same topic continues → accumulated context
- **Resolve**: Topic completed → marked resolved, excluded from instant memory

### Checkpoint Tracker

LLM execution checkpoints for long-running tasks:

- Analyser generates `checkpoints[]` execution plan
- LLM reports progress after each checkpoint
- Enables resumable tasks and progress visibility

## Extension Framework

Optional capabilities are managed by a unified `extension.Manager`:

```
pkg/extension/
├── extension.go     Manager (Register, InitOne, StartAll, StopAll)
└── voice/
    ├── voice.go     Ext lifecycle + Transcriber() accessor
    └── transcriber.go  OpenAI-compatible /v1/audio/transcriptions client
```

**Rule**: only truly optional capabilities live here. `pkg/infra/media` stays as infra (always-on, shared by channels + agent).

### Extension Lifecycle

```
NewAgentLoop()
  Register(voiceExt)    ← only if stt_model configured
  InitAll()             ← called once
  StartAll()            ← on gateway start
  StopAll()             ← on gateway shutdown (reverse order)
```

### Voice / STT Extension

```
config.json:
  agents.defaults.stt_model: "groq-whisper"

model_list:
  - model_name: "groq-whisper"
    api_base:   "https://api.groq.com/openai/v1"
    api_key:    "gsk_..."
    model:      "whisper-large-v3"
```

Any OpenAI-compatible `/v1/audio/transcriptions` provider works (Groq, OpenAI, local faster-whisper).

---

## Channel Manager Concurrency

`channels.Manager.StopAll` uses a **two-phase shutdown** to avoid holding `mu` during blocking I/O:

```
Phase 1 (lock held, µs):
  snapshot workers/channels/httpServer → local vars
  clear m.workers, m.channels maps
  unlock

Phase 2 (no lock, blocking):
  httpServer.Shutdown()    ← max 5s
  dispatchTask.cancel()
  close(queue); <-done     ← drain workers
  channel.Stop()           ← per-channel teardown
```

Concurrent readers (`GetChannel`, `dispatchLoop`) see empty maps immediately after Phase 1.

---

## Multi-Agent

```json
{
  "agents": {
    "list": [
      { "id": "coder", "model": { "primary": "deepseek-coder" }, "skills": ["code"] },
      { "id": "reviewer", "model": { "primary": "gpt-4" }, "skills": ["review"] }
    ]
  }
}
```

- `AgentRegistry` manages instances, each with own LLM/tools/skills
- Message routing: pattern match → specific agent, default → main agent
- Duplicate message prevention: checks ALL agents' MessageTool before outbound
- Spawn: main agent can delegate tasks to other agents (async or sync)
- `SubagentManager` (`pkg/tools/subagent.go`): `.GetTask` / `.ListTasks` return **value copies** — no shared pointer leaks
- `AgentRegistry.CanSpawnSubagent()` enforces parent → child permissions

---

## Skill System

3-layer loading hierarchy (shadowing prevention):

```
Workspace skills  (~/.picoclaw/workspace/skills/)
  ↓ override
Global skills     (~/.picoclaw/skills/)
  ↓ override
Built-in skills   (embedded in binary)
```

Skills declare `tool_steps` for structured execution plans:

```yaml
tool_steps:
  - step: "Read config"
    mode: parallel
    tools: ["read_file SKILL.md", "read_file config.json"]
  - step: "Apply changes"
    mode: serial
    tools: ["edit_file config.json"]
```

Analyser matches skills by keywords and injects the plan into system prompt.

---

## Media Store

`pkg/infra/media.FileMediaStore` is **always-on infrastructure** (not an extension):

```
Channel receives file
  → download to /tmp/picoclaw_media/
  → store.Store(path, meta, scope) → ref = "media://uuid"

Agent / Tool uses file
  → store.Resolve(ref) → local path

Conversation ends
  → store.ReleaseAll(scope) → delete files + clear mappings

Background goroutine (optional TTL cleanup)
  → CleanExpired() removes stale entries
```

Injected into both `channels.Manager` and `AgentLoop.SetMediaStore` at gateway startup.

