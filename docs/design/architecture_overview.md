# PicoClaw Architecture Overview

> Status: Living Document | Updated: 2026-03-04

## Philosophy

PicoClaw started as an ultra-lightweight personal AI agent inspired by [nanobot](https://github.com/HKUDS/nanobot). It has since evolved into a **lightweight agent framework** — still minimal in dependencies, but richer in architecture.

**Core principles:**
- **Go-native**: Single binary, no external runtime
- **Layered, not monolithic**: Each phase is a separate, testable component
- **Convention over configuration**: Sensible defaults, deep customization when needed
- **Channel-agnostic**: Same agent logic serves CLI, Feishu, Telegram, Discord, WebSocket, etc.
- **Extension-first**: Optional capabilities (devices, voice) plug in via a unified lifecycle framework

## System Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                        Entry Points                          │
│   Telegram · Discord · DingTalk · Pico WS · CLI · MaixCam   │
└────────────────────────┬─────────────────────────────────────┘
                         │  channels.Manager (two-phase StopAll)
                         │  bus.MessageBus (pub/sub)
                         ▼
┌──────────────────────────────────────────────────────────────┐
│                    AgentLoop (loop.go)                       │
│                                                              │
│  ┌──────────┐  ┌─────────────────┐  ┌──────────────────┐   │
│  │ Phase 1  │  │    Phase 2      │  │     Phase 3      │   │
│  │ Analyse  │→ │    Execute      │→ │     Reflect      │   │
│  │          │  │                 │  │                  │   │
│  │ intent   │  │  LLM ↔ Tools   │  │ score + persist  │   │
│  │ tags     │  │  iterations     │  │ TurnStore        │   │
│  │ CoT      │  │  multi-agent    │  │ ActiveContext     │   │
│  └──────────┘  └─────────────────┘  └──────────────────┘   │
│                                                              │
│  ┌───────────────────┐  ┌──────────────────────────────┐   │
│  │  AgentRegistry    │  │   Extension Manager          │   │
│  │  multi-agent pool │  │   devices · voice            │   │
│  └───────────────────┘  └──────────────────────────────┘   │
└──────────────────────────────────────────────────────────────┘
          │                      │                  │
  ┌───────────┐         ┌──────────────┐    ┌────────────┐
  │   Tools   │         │    Memory    │    │   Skills   │
  │ Registry  │         │    System    │    │   Loader   │
  │ (+ MCP)   │         │ TurnStore    │    │ 3-layer    │
  └───────────┘         │ KVCache      │    └────────────┘
                        │ MemoryDigest │
                        └──────────────┘
```

## Package Map

| Package | Responsibility | Key Files |
|---------|---------------|-----------|
| `pkg/agent` | Runtime loop, phases, routing, TurnStore | `loop.go`, `analyser.go`, `reflector.go`, `context.go`, `turn_store.go` |
| `pkg/tools` | Tool interface + builtin implementations | `registry.go`, `toolloop.go`, `filesystem.go`, `exec.go`, `subagent.go` |
| `pkg/channels` | Channel adapters + outbound workers | `manager.go`, `telegram/`, `discord/`, `pico/`, `cli/` |
| `pkg/extension` | Optional capability lifecycle framework | `extension.go` (Manager), `devices/`, `voice/` |
| `pkg/infra/config` | Config loading, model/provider resolution | `config.go`, `model.go`, `load.go` |
| `pkg/infra/media` | Media file ref management (always-on) | `store.go` (MediaStore interface + FileMediaStore) |
| `pkg/infra/kvcache` | Write-through KV cache (memory + SQLite) | `kvcache.go` |
| `pkg/infra/devices` | I2C/SPI/USB hardware service | `service.go`, `source.go` |
| `pkg/infra/logger` | Structured context-tagged logging | `logger.go` |
| `pkg/llm` | LLM provider abstraction + implementations | `providers/openai.go`, `providers/anthropic.go`, ... |
| `pkg/skills` | Skill loading + keyword matching | `loader.go` |
| `pkg/core` | Shared bus types, constants | `bus/`, `routing/`, `constants.go` |

### `pkg/infra` Sub-packages

```
pkg/infra/
├── config/          Config + model list resolution
├── cron/            Scheduled task service
├── devices/         Hardware abstraction (I2C, SPI, USB)
├── health/          HTTP health check server
├── heartbeat/       Periodic alive-ping to gateway
├── kvcache/         Write-through KV (memory + SQLite)
├── logger/          Structured CF logger
├── media/           MediaStore: ref ↔ local path + TTL cleanup
└── utils/           String truncation, misc helpers
```

## Phase Details

### Phase 1: Analyse (`analyser.go`)

Fast/cheap auxiliary LLM extracts structured metadata before the main LLM runs.

```
Input:  user message + available tags
Output: { intent, tags[], cot_prompt }
```

- Uses `auxiliary_model` (configurable, falls back to `primary_model`)
- Keyword-based skill matching → injects Tool Execution Plan into system prompt
- CoT prompt injection from learning history

### Phase 2: Execute (`loop.go` + `executor.go`)

Main LLM iteration loop with tool calling.

```
repeat:
  LLM(system_prompt + messages + tools) → response
  if response.has_tool_calls:
    execute tools → append results
  else:
    break → final answer
```

- `ContextBuilder` assembles messages with Instant Memory from TurnStore
- Retry on context overflow with automatic message compression
- Multi-agent routing via `AgentRegistry`
- `SubagentManager`: concurrent-safe (value-copy snapshots, no pointer leaks)

### Phase 3: Reflect (`reflector.go`)

Post-LLM processing, split into sync and async:

- **SyncPhase3** (<2ms): Turn scoring + Active Context update
- **AsyncPhase3**: `TurnStore.Insert()` → SQLite persistence (with tag inverted index)
- Slash commands: `/memory`, `/shell`, `/show`, `/switch`, `/help`

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
├── Command Execution    ← ExecTool spawns system processes
│   exec.go              ExecTool with guardCommand security
│                        Fast path: Go built-in for simple commands (unrestricted mode)
│   exec_process_*.go    Platform-specific process management
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
└── Hardware (embedded)
    spi*.go, i2c*.go     SPI/I2C for embedded devices
```

### Security Model

```
Tool path validation:
  tildeExpandFs          ~ → $HOME expansion (all file tools)
    → whitelistFs        Config allow_read_paths / allow_write_paths
      → sandboxFs        os.Root workspace restriction
        → hostFs         Direct filesystem access

ExecTool command guard:
  denyPatterns           Block rm -rf, format, etc.
  allowPatterns          Custom overrides (e.g. git push origin)
  workspace restriction  Path validation for working_dir
  safePaths              /dev/null, /dev/zero, /dev/urandom always allowed
```

## Memory Hierarchy

| Layer | Scope | Storage | Purpose |
|-------|-------|---------|---------|
| **Instant Memory** | Per-turn | In-memory | Dynamic window from TurnStore (score ≥ threshold + tag match, channel-isolated) |
| **Active Context** | Per-session | In-memory | CurrentFiles + RecentErrors, injected as user message |
| **Long-term Memory** | Persistent | SQLite `memory.db` | MemoryDigest batch extraction, searchable by tags |

### Turn ID Format

Channel-aware, time-sortable, compact: `{channel_hash}_{timestamp_base62}_{counter}`

Example: `12tdkW_VCrYNEP_1`

## Extension Framework

Optional capabilities are managed by a unified `extension.Manager`:

```
pkg/extension/
├── extension.go     Manager (Register, InitOne, StartAll, StopAll)
├── devices/         I2C/SPI tools + USB monitoring (cfg.Devices.Enabled)
└── voice/
    ├── voice.go     Ext lifecycle + Transcriber() accessor
    └── transcriber.go  OpenAI-compatible /v1/audio/transcriptions client
```

**Rule**: only truly optional capabilities live here. `pkg/infra/media` stays as infra (always-on, shared by channels + agent).

### Extension Lifecycle

```
NewAgentLoop()
  Register(devicesExt)  ← only if cfg.Devices.Enabled
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

## Memory System

| Layer | Scope | Storage | Purpose |
|-------|-------|---------|---------| 
| **Instant Memory** | Per-turn | In-memory | Dynamic window: TurnStore (score ≥ threshold + tag match) |
| **Active Context** | Per-session | In-memory | CurrentFiles + RecentErrors, injected as user message |
| **Long-term Memory** | Persistent | SQLite `memory.db` | MemoryDigest batch extraction, searchable by tags |
| **KV Cache** | Persistent | SQLite + map | Write-through, O(1) read, lazy TTL eviction |

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

---

## Multi-Agent

```json
{
  "subagents": {
    "agents": {
      "coder":    { "model": "deepseek-coder", "skills_filter": ["code"] },
      "reviewer": { "model": "gpt-4", "skills_filter": ["review"] }
    },
    "routing": {
      "rules": [{ "pattern": "review|check", "agent_id": "reviewer" }]
    }
  }
}
```

- `AgentRegistry` manages instances, each with own LLM/tools/skills
- Message routing: pattern match → specific agent, default → main agent
- Duplicate message prevention: checks ALL agents' MessageTool before outbound
- Spawn: main agent can delegate tasks to other agents (async or sync)
- `SubagentManager.GetTask` / `ListTasks` return **value copies** — no shared pointer leaks

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

