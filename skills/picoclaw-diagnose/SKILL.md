---
name: picoclaw-diagnose
description: Diagnose and troubleshoot PicoClaw issues — check config validity, test model API connectivity, verify channel status, and review logs. Use when picoclaw 出错 error debug 排查 diagn断 troubleshoot not working 连不上 无法启动 API failed.
---

# PicoClaw Diagnostics

Use this skill when PicoClaw is malfunctioning or the user reports errors.

## Diagnostic checklist

Run checks in this order — stop when you find the issue:

### 1. Config file exists and is valid JSON

```bash
cat ~/.picoclaw/config.json | python3 -c "import json,sys; json.load(sys.stdin); print('✅ Valid JSON')" 2>&1 || echo "❌ Invalid JSON"
```

If this fails, use `read_file` on `~/.picoclaw/config.json` and fix the JSON syntax errors.

### 2. Model is configured

Check that `primary_model` in `agents.defaults` matches a `model_name` in `model_list`:

```bash
cat ~/.picoclaw/config.json
```

Look for:
- `agents.defaults.primary_model` → must match one `model_list[].model_name`
- `model_list` has at least one entry with `model`, `api_base`, and `api_key`

### 3. API endpoint is reachable

Test connectivity to the model's API base:

```bash
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer <api_key>" <api_base>/models
```

Common issues:
| HTTP code | Problem |
|-----------|---------|
| 000 | DNS / network failure — check URL and proxy |
| 401 | Invalid API key |
| 403 | Account suspended or rate limited |
| 404 | Wrong API base URL or model path |
| 429 | Rate limited — wait or switch model |

### 4. Channel tokens are valid

For each enabled channel, verify the token format:
- **Telegram**: token format `<numbers>:<alphanumeric>`, test with `curl https://api.telegram.org/bot<token>/getMe`
- **Discord**: test with `curl -H "Authorization: Bot <token>" https://discord.com/api/v10/users/@me`
- **Feishu**: verify `app_id` starts with `cli_` and `app_secret` is non-empty

### 5. Gateway port is available

Check if the gateway port is already in use:

```bash
# Windows
netstat -ano | findstr :18790

# Linux/Mac
lsof -i :18790
```

If occupied, either stop the other process or change `gateway.port` in config.

### 6. Check logs

If `logging.file_dir` is set:
```bash
tail -50 ~/.picoclaw/logs/picoclaw.log
```

If logging to stderr (default), check the terminal output.

Look for:
- `"level":"error"` entries
- `connection refused` — API unreachable
- `401 Unauthorized` — bad API key
- `model not found` — model name mismatch

## Common problems and fixes

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| "model not found" | `primary_model` doesn't match `model_list` | Fix naming in config |
| Connection timeout | API endpoint unreachable | Check network/proxy settings |
| "Start failed: undefined" | Gateway port conflict | Change port or kill conflicting process |
| No response from bot | Channel token invalid | Re-generate token from platform |
| Skills not loading | Wrong skills directory | Check `workspace` path in config |
| "Memory store not available" | Workspace directory not writable | Fix permissions on workspace dir |

## Quick health check

Run all checks with one command:

```bash
echo "=== Config ===" && cat ~/.picoclaw/config.json | python3 -c "import json,sys; c=json.load(sys.stdin); print(f'Model: {c[\"agents\"][\"defaults\"].get(\"primary_model\",\"NOT SET\")}'); print(f'Models: {len(c.get(\"model_list\",[]))}'); print(f'Gateway: {c[\"gateway\"][\"host\"]}:{c[\"gateway\"][\"port\"]}')" 2>&1
```
