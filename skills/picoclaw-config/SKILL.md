---
name: picoclaw-config
description: Read and modify ~/.picoclaw/config.json to configure picoclaw — set models, API keys, and enable/disable messaging channels (feishu 飞书, telegram, discord, slack, qq, wecom, whatsapp, line, dingtalk, maixcam, pico) or tool settings.
---

# PicoClaw Configuration

## ⚠️ CRITICAL: Single Config File — No Other Config Files Exist

**ALL** picoclaw configuration lives in **one single file**: `~/.picoclaw/config.json`

- There is **NO** `feishu.yaml`, `telegram.yaml`, `channels.yaml`, or any other config file.
- There is **NO** `.env` file, `.toml` file, or separate per-channel config file.
- **Every** setting — models, channels (feishu, telegram, discord, etc.), tools, logging — is inside this one JSON file.
- Do **NOT** search for, create, or reference any config file other than `~/.picoclaw/config.json`.

Path: `~/.picoclaw/config.json` (or the path in `PICOCLAW_CONFIG` env var).

## Mandatory Rules

1. **Read `~/.picoclaw/config.json` first** — always read the actual file before making any changes.
2. **Never guess file paths** — do not assume separate config files exist for individual channels or features.
3. **All channel config is under `channels.<name>`** in config.json — e.g. `channels.feishu`, `channels.telegram`.
4. **Minimal edits only** — do not rewrite the entire file; use surgical edits on the specific section.

## Tool Steps

1. [parallel] read_file {skill_path} | read_file ~/.picoclaw/config.json
2. [serial] edit_file ~/.picoclaw/config.json

## When to use

Use this skill when the user asks to:
- Configure or enable a messaging channel: **feishu 飞书、telegram、discord、slack、qq、wecom、whatsapp、line、dingtalk、maixcam、pico**
- "配置飞书机器人" / "设置 Telegram" / "开启 Discord"
- View or change the current model / auxiliary model
- Enable or disable a channel
- Add or modify a model in `model_list`
- Change API keys or endpoints
- Adjust tool settings (web search, skills, MCP, etc.)
- View current configuration

## Pre-check: verify config exists

Before reading or modifying the configuration, **you MUST first check whether `~/.picoclaw/config.json` exists**.

Use the `read_file` tool (or shell `cat`) on the config path. If the file does **not** exist (file not found / error), do NOT proceed with modifications. Instead:

1. Inform the user that no configuration file was found.
2. Ask the user what they want to configure (model, channel, etc.).
3. Create a minimal config file with only the sections they need. Use this template as a starting point:

```json
{
  "agents": {
    "defaults": {
      "primary_model": "<model_name>",
      "max_tokens": 32768,
      "max_tool_iterations": 50
    }
  },
  "model_list": [
    {
      "model_name": "<alias>",
      "model": "<protocol/model-id>",
      "api_base": "<endpoint>",
      "api_key": "<key>"
    }
  ]
}
```

4. Only add sections (`channels`, `tools`, `gateway`, `logging`) that the user explicitly requests. Do NOT create a full config with all disabled channels — keep it minimal.

> ⚠️ Always read the current config.json first before modifying it. If the file doesn't exist, create it first.

## Config structure

```json
{
  "agents": {
    "defaults": {
      "workspace": "~/.picoclaw/workspace",
      "primary_model": "<model_name from model_list>",
      "auxiliary_model": "<model_name from model_list>",
      "max_tokens": 32768,
      "max_tool_iterations": 50
    }
  },
  "model_list": [
    {
      "model_name": "my-model",
      "model": "openai/gpt-4o",
      "api_base": "https://api.openai.com/v1",
      "api_key": "sk-..."
    }
  ],
  "channels": {
    "telegram": { "enabled": true, "token": "BOT_TOKEN" },
    "discord":  { "enabled": false, "token": "" }
  },
  "gateway": { "host": "127.0.0.1", "port": 18790 },
  "tools": {
    "web": {
      "duckduckgo": { "enabled": true, "max_results": 5 },
      "brave":      { "enabled": false, "api_key": "" }
    },
    "skills": {
      "registries": {
        "clawhub": { "enabled": true }
      }
    },
    "mcp": { "enabled": false }
  },
  "logging": {
    "level": "warn",
    "file_dir": ""
  }
}
```

## Key fields

### `agents.defaults`

| Field | Description |
|-------|-------------|
| `primary_model` | Main LLM model name (must match a `model_name` in `model_list`) |
| `auxiliary_model` | Fast/cheap model for Analyser & Reflector (falls back to primary if empty) |
| `max_tokens` | Max output tokens per LLM call |
| `max_tool_iterations` | Max tool-call rounds per message |

### `model_list`

Each entry:

| Field | Required | Description |
|-------|----------|-------------|
| `model_name` | ✓ | Alias used in `primary_model` / `auxiliary_model` |
| `model` | ✓ | Protocol-prefixed model ID, e.g. `openai/gpt-4o`, `anthropic/claude-sonnet-4.6`, `gemini/gemini-2.0-flash` |
| `api_key` | usually | Provider API key |
| `api_base` | sometimes | Custom endpoint URL |
| `auth_method` | OAuth only | `"oauth"` for GitHub Copilot / Antigravity |

### `channels`

Only enabled channels need to be in the file. Each channel has at minimum `"enabled": true` plus its auth token/secret.

Common channels: `telegram`, `discord`, `feishu`, `slack`, `qq`, `onebot`, `wecom`, `line`, `whatsapp`, `maixcam`, `pico`

### `logging`

| Field | Values | Description |
|-------|--------|-------------|
| `level` | `debug` / `info` / `warn` / `error` | Log verbosity |
| `file_dir` | path | Directory for `picoclaw.log`; empty = stderr only |

## How to read the current config

```bash
cat ~/.picoclaw/config.json
```

Or use the `read_file` tool on `~/.picoclaw/config.json`.

## How to edit

1. Read the current file with `read_file`
2. Make the minimal change needed (do NOT rewrite the whole file)
3. Write back with `write_file` or use `str_replace` for surgical edits
4. Confirm the change is correct by reading the file again

> **Important**: After changing the model, gateway or channels, the picoclaw process must be restarted for changes to take effect.

## Common tasks

### Change primary model

```json
"agents": {
  "defaults": {
    "primary_model": "my-new-model"
  }
}
```

`my-new-model` must exist in `model_list`.

### Add a new model

Append to `model_list`:

```json
{
  "model_name": "deepseek-chat",
  "model": "deepseek/deepseek-chat",
  "api_base": "https://api.deepseek.com/v1",
  "api_key": "sk-..."
}
```

### Enable Feishu 飞书

Feishu webhook bot (最简配置):

```json
"channels": {
  "feishu": {
    "enabled": true,
    "app_id": "cli_xxx",
    "app_secret": "your-app-secret",
    "verification_token": "your-token",
    "encrypt_key": ""
  }
}
```

Feishu webhook (群机器人，仅发送消息):

```json
"channels": {
  "feishu": {
    "enabled": true,
    "webhook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/your-id"
  }
}
```

### Enable Telegram

```json
"channels": {
  "telegram": {
    "enabled": true,
    "token": "123456789:AAExampleToken"
  }
}
```

### Enable file logging

```json
"logging": {
  "level": "info",
  "file_dir": "~/.picoclaw/logs"
}
```

Log file: `~/.picoclaw/logs/picoclaw.log` (JSON lines format)
