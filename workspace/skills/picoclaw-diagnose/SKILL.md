---
name: picoclaw-diagnose
description: Diagnose and troubleshoot PicoClaw issues — check config validity, test model API connectivity, verify channel tokens, inspect logs, and fix common errors. Use when picoclaw is not working, errors occur, API fails, bot doesn't respond, or the user reports 出错 报错 error debug 排查 诊断 troubleshoot not working 连不上 无法启动 API failed 没反应.
---

# PicoClaw Diagnostics

Diagnose PicoClaw issues by checking config, connectivity, and logs.

## Tool Execution Plan

1. [parallel] read_file ~/.picoclaw/config.json | read_file {skill_path}/SKILL.md
2. [serial] Analyze config and report findings
3. [serial] If needed, test API connectivity with exec tool
4. [serial] If needed, fix issues with edit_file

## Quick Automated Check

Tell the user they can run `picoclaw doctor` for an automated health check.

## Environment Variable Shortcuts

PicoClaw supports short env var aliases for Docker/CI deployment:

| Short | Maps to | Example |
|-------|---------|---------|
| `PC_MODEL` | `PICOCLAW_AGENTS_DEFAULTS_PRIMARY_MODEL` | `PC_MODEL=deepseek-chat` |
| `PC_API_KEY` | Auto-injected into first `model_list` entry | `PC_API_KEY=sk-xxx` |
| `PC_AUX_MODEL` | `PICOCLAW_AGENTS_DEFAULTS_AUXILIARY_MODEL` | `PC_AUX_MODEL=gemini-flash` |
| `PC_PROXY` | `PICOCLAW_PROXY` | `PC_PROXY=http://127.0.0.1:7890` |
| `PC_TG_TOKEN` | `PICOCLAW_CHANNELS_TELEGRAM_TOKEN` | `PC_TG_TOKEN=123:ABC` |
| `PC_DC_TOKEN` | `PICOCLAW_CHANNELS_DISCORD_TOKEN` | `PC_DC_TOKEN=xxx` |
| `PC_CHANNEL` | Auto-enables that channel | `PC_CHANNEL=telegram` |
| `PC_GW_HOST` | `PICOCLAW_GATEWAY_HOST` | `PC_GW_HOST=0.0.0.0` |
| `PC_GW_PORT` | `PICOCLAW_GATEWAY_PORT` | `PC_GW_PORT=8080` |

When diagnosing issues, also check if the user has conflicting env vars set.

## Diagnostic Flow

Run checks **in order** — stop and fix when you find the issue.

### Check 1: Config file exists and is valid

Use `read_file ~/.picoclaw/config.json` (NOT cat/python).

If the file doesn't exist:
→ Tell user to run `picoclaw init` first.

If JSON is malformed:
→ Identify the syntax error, fix with `edit_file`.

### Check 2: Model is properly configured

Verify in the config:

1. `model_list` has at least one entry with non-empty `api_key`
2. `agents.defaults.primary_model` matches a `model_name` in `model_list`
3. Each model entry has `model`, `api_base`, and `api_key` (or `auth_method`)

**Common mistakes:**
- `primary_model` set but no matching `model_name` in `model_list`
- `api_key` is `""` (empty) — user forgot to fill it in
- Using deprecated `providers` section instead of `model_list`

### Check 3: API endpoint is reachable

Test the active model's API base with exec:

```
# Cross-platform: use curl if available, otherwise PowerShell
curl -s -o /dev/null -w "%{http_code}" <api_base>/models
```

On Windows without curl:
```powershell
(Invoke-WebRequest -Uri "<api_base>/models" -Method Head -UseBasicParsing).StatusCode
```

| HTTP code | Problem | Fix |
|-----------|---------|-----|
| 000 | DNS/network failure | Check URL, proxy, and internet |
| 401 | Invalid API key | Regenerate key from provider |
| 403 | Account suspended | Check provider dashboard |
| 404 | Wrong API base URL | Verify URL in provider docs |
| 429 | Rate limited | Wait or switch to another model |

### Check 4: Channel tokens are valid

For each channel with `"enabled": true`, verify:

| Channel | Validation | Test command |
|---------|-----------|-------------|
| Telegram | Token = `<digits>:<alphanumeric>` | `curl https://api.telegram.org/bot<token>/getMe` |
| Discord | Token is non-empty | `curl -H "Authorization: Bot <token>" https://discord.com/api/v10/users/@me` |
| Feishu | `app_id` starts with `cli_` | Check feishu open platform |
| DingTalk | `client_id` and `client_secret` non-empty | — |

**IMPORTANT:** Never expose full tokens in output — show only first 8 chars + `***`.

### Check 5: Gateway port is available

Use exec to check:
```
# Windows
netstat -ano | findstr :18790

# Linux/Mac  
ss -tlnp | grep :18790
```

If port is occupied → suggest changing `gateway.port` or stopping the other process.

### Check 6: Inspect logs

If `logging.file_dir` is configured, read the last 50 lines:
```
read_file <file_dir>/picoclaw.log
```

Key error patterns to look for:
- `connection refused` → API endpoint unreachable
- `401` / `Unauthorized` → bad API key
- `model not found` → model name mismatch between config sections
- `bind: address already in use` → port conflict

## Common Problems Quick Reference

| Symptom | Cause | Fix |
|---------|-------|-----|
| "model not found" | `primary_model` ≠ any `model_list[].model_name` | Align names in config |
| Connection timeout | API unreachable | Check proxy/network, verify `api_base` URL |
| Gateway won't start | Port 18790 occupied | Kill process or change `gateway.port` |
| Bot not responding | Channel token invalid or `enabled: false` | Verify token, set `enabled: true` |
| Skills not loading | Wrong workspace path | Check `agents.defaults.workspace` |
| Config warnings at startup | Using deprecated fields | Use `primary_model` not `model_name` |
| High API costs | All context loaded every request | Set `auxiliary_model` to a cheaper model |

## Output Format

Always report findings as a checklist:

```
🔍 PicoClaw Diagnostics
━━━━━━━━━━━━━━━━━━━━━━
✅ Config file: valid JSON
✅ Model: deepseek-chat → deepseek/deepseek-chat
❌ API key: empty for "deepseek-chat" — please add your key
⬚ Channel: telegram enabled, token set
⬚ Gateway: port 18790
⬚ Logs: no logging configured
━━━━━━━━━━━━━━━━━━━━━━
Action needed: Add API key to model_list entry "deepseek-chat"
```

