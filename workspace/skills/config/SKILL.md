---
name: config
description: Configure picoclaw settings including channels (telegram, discord, feishu, dingtalk, slack, line, qq, wecom, whatsapp, maixcam), LLM providers (deepseek, openai, anthropic, gemini, qwen, groq, ollama, moonshot, zhipu), proxy, and other settings. Use this skill when the user asks to set up, configure, modify, or troubleshoot any picoclaw configuration. Keywords: 配置, 设置, config, setup, channel, provider, model, api key, token, proxy.
---

# Config Skill — picoclaw Configuration Assistant

You help users configure picoclaw by reading and editing the config file.

## Config File Location

The config file is at: `~/.picoclaw/config.json` (JSONC format, supports comments)

## Tool Execution Plan

1. [parallel] read_file ~/.picoclaw/config.json | read_file {skill_path}/SKILL.md
2. [serial] edit_file ~/.picoclaw/config.json

## How to Configure

### Step 1: Read the current config
Always read `~/.picoclaw/config.json` first to understand what's already configured.

### Step 2: Identify what the user wants
Common requests and their config sections:

#### Channels (消息渠道)
| Channel | Key fields | Where to get credentials |
|---------|-----------|------------------------|
| Telegram | `channels.telegram.enabled`, `.token` | @BotFather on Telegram |
| Discord | `channels.discord.enabled`, `.token` | Discord Developer Portal |
| Feishu (飞书) | `channels.feishu.enabled`, `.app_id`, `.app_secret`, `.encrypt_key`, `.verification_token` | Feishu Open Platform |
| DingTalk (钉钉) | `channels.dingtalk.enabled`, `.client_id`, `.client_secret` | DingTalk Developer |
| Slack | `channels.slack.enabled`, `.bot_token`, `.app_token` | Slack API |
| LINE | `channels.line.enabled`, `.channel_secret`, `.channel_access_token` | LINE Developers |
| QQ | `channels.qq.enabled`, `.app_id`, `.app_secret` | QQ Open Platform |
| WeCom (企业微信) | `channels.wecom.enabled`, `.token`, `.encoding_aes_key`, `.webhook_url` | WeCom Admin |
| WeComApp | `channels.wecom_app.enabled`, `.corp_id`, `.corp_secret`, `.agent_id`, `.token`, `.encoding_aes_key` | WeCom Admin |
| WhatsApp | `channels.whatsapp.enabled`, `.bridge_url` | WhatsApp Bridge |

#### LLM Providers (model_list)
Add entries to the `model_list` array:

```jsonc
{
  "model_list": [
    {
      "model_name": "user-friendly-name",  // used in primary_model
      "model": "protocol/model-id",        // e.g. "deepseek/deepseek-chat"
      "api_base": "https://...",           // API endpoint
      "api_key": "sk-xxx"                  // your API key
    }
  ]
}
```

Common providers:

| Provider | model | api_base |
|----------|-------|----------|
| DeepSeek | `deepseek/deepseek-chat` | `https://api.deepseek.com/v1` |
| OpenAI | `openai/gpt-5.2` | `https://api.openai.com/v1` |
| Anthropic | `anthropic/claude-sonnet-4.6` | `https://api.anthropic.com/v1` |
| Gemini | `gemini/gemini-2.0-flash-exp` | `https://generativelanguage.googleapis.com/v1beta` |
| Qwen (通义千问) | `qwen/qwen-plus` | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| Zhipu (智谱) | `zhipu/glm-4.7` | `https://open.bigmodel.cn/api/paas/v4` |
| Groq | `groq/llama-3.3-70b-versatile` | `https://api.groq.com/openai/v1` |
| Moonshot (月之暗面) | `moonshot/moonshot-v1-8k` | `https://api.moonshot.cn/v1` |
| Ollama (local) | `ollama/llama3` | `http://localhost:11434/v1` |
| OpenRouter | `openrouter/auto` | `https://openrouter.ai/api/v1` |

#### Proxy (代理)
```jsonc
{ "proxy": "http://127.0.0.1:7890" }
```

#### Model Selection
```jsonc
{
  "agents": {
    "defaults": {
      "primary_model": "model-name-from-model_list",
      "auxiliary_model": "cheaper-model-name"
    }
  }
}
```

### Step 3: Edit the config
- Use `edit_file` to modify specific sections
- If starting from scratch, use `write_file` to create the full config
- Always ask the user for sensitive values (API keys, tokens) — never guess them
- After editing, show the user what was changed

### Step 4: Validate
- Remind the user to restart picoclaw for changes to take effect
- For channels: confirm the token/key format looks reasonable
- For providers: suggest testing with `picoclaw chat "hello"`

## Important Rules
1. NEVER invent or guess API keys, tokens, or secrets
2. Always ask the user for credentials they haven't provided
3. When enabling a channel, set `"enabled": true` AND fill in required credentials
4. Keep the config minimal — only include sections the user needs
5. Preserve existing config — use edit_file, not write_file, when config already exists
