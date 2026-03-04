# PicoClaw Architecture Overview

> Status: Living Document | Updated: 2026-03-04

## Philosophy

PicoClaw started as an ultra-lightweight personal AI agent inspired by [nanobot](https://github.com/HKUDS/nanobot). It has since evolved into a **lightweight agent framework** — still minimal in dependencies, but richer in architecture.

**Core principles:**
- **Go-native**: Single binary, no external runtime
- **Layered, not monolithic**: Each phase is a separate, testable component
- **Convention over configuration**: Sensible defaults, deep customization when needed
- **Channel-agnostic**: Same agent logic serves CLI, Feishu, Telegram, Discord, etc.

## System Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     Entry Points                        │
│   CLI · Feishu · Telegram · Discord · DingTalk · ...    │
└──────────────────────┬──────────────────────────────────┘
                       │ MessageBus
                       ▼
┌──────────────────────────────────────────────────────────┐
│                    AgentLoop (loop.go)                    │
│                                                          │
│  ┌──────────┐  ┌──────────────┐  ┌───────────────────┐  │
│  │ Phase 1  │  │   Phase 2    │  │     Phase 3       │  │
│  │ Analyse  │→ │   Execute    │→ │     Reflect       │  │
│  │          │  │              │  │                   │  │
│  │ intent   │  │ LLM ↔ Tools │  │ scoring, memory,  │  │
│  │ tags     │  │ iterations   │  │ slash commands    │  │
│  │ CoT      │  │              │  │                   │  │
│  └──────────┘  └──────────────┘  └───────────────────┘  │
│                                                          │
│  ┌──────────────────────────────────────────────────┐    │
│  │            AgentRegistry (multi-agent)            │    │
│  │  main · coder · reviewer · ...                    │    │
│  └──────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────┘
                       │
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
   ┌─────────┐  ┌───────────┐  ┌──────────┐
   │  Tools   │  │  Memory   │  │  Skills  │
   │ Registry │  │  System   │  │  Loader  │
   └─────────┘  └───────────┘  └──────────┘
```

## Package Map

| Package | Responsibility | Key Files |
|---------|---------------|-----------|
| `pkg/agent` | Runtime loop, phases, routing | `loop.go`, `analyser.go`, `reflector.go`, `context.go`, `instance.go` |
| `pkg/tools` | Tool interface + implementations | `exec.go`, `filesystem.go`, `shell_instance.go`, `web.go`, `spawn.go` |
| `pkg/config` | Config loading, model resolution | `config.go` |
| `pkg/providers` | LLM provider abstraction | `openai.go`, `siliconflow.go`, ... |
| `pkg/channels` | Channel adapters | `feishu/`, `telegram/`, `cli/`, ... |
| `pkg/bus` | Message routing | `bus.go` |
| `pkg/skills` | Skill loading + matching | `loader.go` |
| `pkg/mcp` | MCP server management | `manager.go` |

## Phase Details

### Phase 1: Analyse (`analyser.go`)

Fast/cheap auxiliary LLM extracts structured metadata before the main LLM runs.

```
Input:  user message + available tags
Output: { intent, tags[], cot_prompt }
```

- Uses `auxiliary_model` (configurable, falls back to `primary_model`)
- Keyword-based skill matching → injects Tool Execution Plan
- CoT prompt injection from learning history

### Phase 2: Execute (`loop.go`)

Main LLM iteration loop with tool calling.

```
repeat:
  LLM(system_prompt + messages + tools) → response
  if response.has_tool_calls:
    execute tools → append results
  else:
    break → final answer
```

- ContextBuilder assembles messages with Instant Memory
- Retry on context overflow with automatic compression
- Multi-agent routing via AgentRegistry

### Phase 3: Reflect (`reflector.go`)

Post-LLM processing, split into sync and async:

- **SyncPhase3** (<2ms): Turn scoring + Active Context update
- **AsyncPhase3**: `TurnStore.Insert()` → SQLite persistence
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

- AgentRegistry manages instances, each with own LLM/tools/skills
- Message routing: pattern match → specific agent, default → main agent
- Duplicate message prevention: checks ALL agents' MessageTool before outbound
- Spawn: main agent can delegate tasks to other agents (async or sync)

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
