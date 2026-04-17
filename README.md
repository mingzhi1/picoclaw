<div align="center">

```
    __  ___     __        ________              
   /  |/  /__  / /_____ _/ ____/ /___ __      __
  / /|_/ / _ \/ __/ __ `/ /   / / __ `/ | /| / /
 / /  / /  __/ /_/ /_/ / /___/ / /_/ /| |/ |/ / 
/_/  /_/\___/\__/\__,_/\____/_/\__,_/ |__/|__/  

```

  <h1>MetaClaw: AI Assistant Powered by Go</h1>

  <h3>Go-Native · AI-Bootstrapped · Cross-Platform · 皮皮虾，我们走！</h3>

  <p>
    <img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go&logoColor=white" alt="Go">
    <img src="https://img.shields.io/badge/Arch-x86__64%2C%20ARM64%2C%20RISC--V-blue" alt="Hardware">
    <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  </p>

[中文](README.zh.md) | **English**

</div>

---

## What is MetaClaw?

MetaClaw is a personal AI Assistant built from the ground up in Go. It originated as a fork of [PicoClaw](https://github.com/sipeed/picoclaw) (which was inspired by [nanobot](https://github.com/HKUDS/nanobot)), and has since evolved into a distinct project with its own architecture and design philosophy.

**Where PicoClaw focuses on ultra-lightweight deployment, MetaClaw focuses on _meta-cognition_ — the agent's ability to reason about its own thinking.**

The name "Meta" reflects exactly what makes this project different:

- **Meta-cognition** — The 3-phase architecture (Analyse → Execute → Reflect) is fundamentally about an agent that thinks about how to think
- **Meta-data driven** — Intent, tags, CoT prompts — everything is structured metadata about the conversation itself
- **Meta-programming** — Built through AI self-bootstrapping, where the agent drove its own architectural evolution

**Core principles:**

- **Go-native** — Single self-contained binary, no external runtime, runs on RISC-V / ARM / x86
- **Layered, not monolithic** — Each phase is a separate, testable component
- **Security-first** — 4-layer filesystem sandbox + command guard with allow/deny patterns
- **Channel-agnostic** — Same agent logic serves CLI, Telegram, Feishu, and more
- **Extension-first** — Optional capabilities (voice) plug in via a unified lifecycle framework

## 🧠 How It Thinks — The 3-Phase Architecture

Every user interaction flows through three distinct phases. This separation allows specialized models, observable intermediate states, and efficient token usage.

```
┌──────────────────────────────────────────────────────────┐
│                     Entry Points                          │
│              Telegram · Feishu · CLI                        │
└────────────────────────┬─────────────────────────────────┘
                         │  MessageBus (pub/sub)
                         ▼
┌──────────────────────────────────────────────────────────┐
│                    AgentLoop                              │
│                                                          │
│  ┌──────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │ Phase 1  │  │   Phase 2    │  │     Phase 3      │   │
│  │ Analyse  │→ │   Execute    │→ │     Reflect      │   │
│  │          │  │              │  │                  │   │
│  │ intent   │  │ LLM ↔ Tools │  │ score + persist  │   │
│  │ tags     │  │ iterations   │  │ TurnStore        │   │
│  │ CoT      │  │ multi-agent  │  │ ActiveContext    │   │
│  └──────────┘  └──────────────┘  └──────────────────┘   │
└──────────────────────────────────────────────────────────┘
          │                    │                │
  ┌───────────┐       ┌──────────────┐  ┌────────────┐
  │   Tools   │       │    Memory    │  │   Skills   │
  │ Registry  │       │  TurnStore   │  │   3-layer  │
  │ (+ MCP)   │       │  KVCache     │  │   loader   │
  └───────────┘       │  Digest      │  └────────────┘
                      └──────────────┘
```

### Phase 1: Analyse

A fast, cheap **auxiliary model** classifies the user's intent and extracts tags before the main LLM runs.

- Extracts `{ intent, tags[], cot_prompt }` from the user message
- Matches skills by keywords → injects a Tool Execution Plan
- Generates a tailored Chain-of-Thought strategy for Phase 2
- Injects **historical usage stats** to keep the analyser consistent

### Phase 2: Execute

The **primary model** handles core reasoning with tool calling in an iterative loop.

- Assembles context: system prompt + Active Context + Instant Memory (relevant past turns) + user message
- History messages are annotated with `[turn intent=code tags=[go]]` for cross-turn awareness
- CoT prompt from Phase 1 is injected as a `## Thinking Strategy` section
- Multi-agent routing via `AgentRegistry` for specialized tasks

### Phase 3: Reflect

Post-turn evaluation, synchronous scoring + async persistence.

- **Sync** (<2ms): Turn scoring determines if the turn is "always kept" in future contexts
- **Async**: `TurnStore.Insert()` → SQLite with tag inverted index (no `LIKE '%tag%'` scans)
- Background `MemoryDigestWorker` extracts durable facts into long-term memory

### Memory Hierarchy

| Layer | Scope | Purpose |
|-------|-------|---------|
| **Instant Memory** | Per-turn | Dynamic window from TurnStore (score ≥ threshold + tag match) |
| **Active Context** | Per-session | CurrentFiles + RecentErrors, injected as user message |
| **Long-term Memory** | Persistent | MemoryDigest batch extraction, searchable by tags |
| **KV Cache** | Persistent | Write-through (memory + SQLite), O(1) read, lazy TTL |

## 📦 Install

### From source

```bash
git clone https://github.com/user/metaclaw.git
cd metaclaw && task deps && task build
```

### Docker

```bash
git clone https://github.com/user/metaclaw.git && cd metaclaw
docker compose -f docker/docker-compose.yml --profile gateway up   # first run: generates config
vim docker/data/config.json                                        # set API keys
docker compose -f docker/docker-compose.yml --profile gateway up -d # start
```

## 🚀 Quick Start

**1. Initialize**

```bash
metaclaw onboard
```

**2. Configure** (`~/.metaclaw/config.json`)

```json
{
  "model_list": [
    {
      "model_name": "gpt4",
      "model": "openai/gpt-5.2",
      "api_key": "your-api-key"
    }
  ],
  "agents": {
    "defaults": {
      "model_name": "gpt4"
    }
  }
}
```

> Use `vendor/model` format (e.g. `zhipu/glm-4.7`, `anthropic/claude-sonnet-4.6`) — zero code changes for new providers.

**3. Chat**

```bash
metaclaw agent -m "What is 2+2?"
```

## 💬 Supported Channels

| Channel | Setup | Notes |
|---------|-------|-------|
| **Telegram** | ⭐ Easy | Recommended. Long-polling, no public IP needed |
| **Feishu** | ⭐⭐ Medium | WebSocket/SDK mode |

All webhook channels share a single Gateway HTTP server (`127.0.0.1:18790` by default).

## ⚙️ Configuration

Config file: `~/.metaclaw/config.json`

| Variable | Description | Default |
|----------|-------------|---------|
| `METACLAW_CONFIG` | Override config file path | `~/.metaclaw/config.json` |
| `METACLAW_HOME` | Override data root directory | `~/.metaclaw` |

### Supported Providers

| Vendor | `model` Prefix | Protocol |
|--------|---------------|----------|
| OpenAI | `openai/` | OpenAI |
| Anthropic | `anthropic/` | Anthropic |
| Zhipu (GLM) | `zhipu/` | OpenAI |
| DeepSeek | `deepseek/` | OpenAI |
| Google Gemini | `gemini/` | OpenAI |
| Groq | `groq/` | OpenAI |
| Qwen | `qwen/` | OpenAI |
| Ollama | `ollama/` | OpenAI |
| OpenRouter | `openrouter/` | OpenAI |
| LiteLLM | `litellm/` | OpenAI |
| VLLM | `vllm/` | OpenAI |
| Cerebras | `cerebras/` | OpenAI |
| Moonshot | `moonshot/` | OpenAI |
| Volcengine | `volcengine/` | OpenAI |

> **Load balancing**: Configure multiple endpoints for the same `model_name` — MetaClaw round-robins automatically.

### 🔒 Security Model

MetaClaw enforces a **layered security model** where every file and command operation passes through multiple validation layers:

```
File operations:
  tildeExpandFs          ~ → $HOME expansion
    → whitelistFs        Config allow_read_paths / allow_write_paths
      → sandboxFs        os.Root workspace restriction (kernel-level)
        → hostFs         Direct filesystem access

Command execution:
  denyPatterns           Block rm -rf, format, dd, shutdown, fork bombs
  allowPatterns          Custom overrides (e.g. git push origin)
  customAllowPatterns    Exempt specific commands from deny checks
  path traversal guard   Blocks .. and absolute paths outside workspace
```

- **Kernel-level sandbox**: Uses Go 1.23+ `os.Root` API — path escape is blocked at the OS level, not just string matching
- **Whitelist paths**: `allow_read_paths` / `allow_write_paths` grant access to specific paths outside workspace without disabling the sandbox
- **Command guard**: Multi-layer pattern matching (deny → custom allow → allow → workspace restriction)
- **Consistent boundary**: Main agent, subagents, spawned tasks, and heartbeat all inherit the same restriction — no bypass via delegation

## CLI Reference

| Command | Description |
|---------|-------------|
| `metaclaw onboard` | Initialize config & workspace |
| `metaclaw agent -m "..."` | Chat with the agent |
| `metaclaw agent` | Interactive chat mode |
| `metaclaw gateway` | Start the gateway |
| `metaclaw cron list` | List scheduled jobs |

## 🔗 Relationship to PicoClaw

MetaClaw is a fork of [PicoClaw](https://github.com/sipeed/picoclaw) that has diverged in focus:

| | PicoClaw | MetaClaw |
|---|---|---|
| **Focus** | Ultra-lightweight, $10 hardware | Meta-cognition, architecture depth |
| **Architecture** | Single-loop agent | 3-phase (Analyse → Execute → Reflect) |
| **Memory** | Basic | 4-layer hierarchy with inverted-index TurnStore |
| **Security** | Basic workspace restriction | 4-layer sandbox (os.Root + whitelist + command guard) |
| **Positioning** | Embedded / IoT | General-purpose AI assistant |

We gratefully acknowledge PicoClaw and the Sipeed team for the foundation that made MetaClaw possible.

## 🤝 Contribute

PRs welcome! The codebase is intentionally small and readable. 🤗

## 📝 License

MIT
