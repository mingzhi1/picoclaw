---
name: cron
description: Schedule reminders, recurring tasks, and automated commands — set timers, alarms, periodic health checks. Use when user says 定时 提醒 remind schedule cron every hour 每天 alarm timer 闹钟 定期.
---

# Cron — Scheduled Tasks

PicoClaw has a built-in `cron` tool for one-time and recurring scheduled tasks.

## Tool: `cron`

The `cron` tool supports these actions:

| Action | Description |
|--------|-------------|
| `add` | Create a new scheduled task |
| `list` | Show all scheduled tasks |
| `remove` | Delete a task by job_id |
| `enable` | Re-enable a disabled task |
| `disable` | Temporarily pause a task |

## Scheduling modes

Choose the right mode based on the user's request:

### 1. One-time reminder (`at_seconds`)

For "remind me in X minutes/hours":

```json
{
  "action": "add",
  "message": "Time to take a break!",
  "at_seconds": 600
}
```

| User says | at_seconds |
|-----------|-----------|
| "5 分钟后" / "in 5 minutes" | 300 |
| "半小时后" / "in 30 minutes" | 1800 |
| "1 小时后" / "in 1 hour" | 3600 |
| "2 小时后" / "in 2 hours" | 7200 |
| "明天" (rough, 24h) | 86400 |

### 2. Recurring interval (`every_seconds`)

For "every X hours" or "daily":

```json
{
  "action": "add",
  "message": "Drink water! 💧",
  "every_seconds": 3600
}
```

| User says | every_seconds |
|-----------|--------------|
| "每 30 分钟" / "every 30 min" | 1800 |
| "每小时" / "every hour" | 3600 |
| "每 2 小时" / "every 2 hours" | 7200 |
| "每天" / "daily" | 86400 |

### 3. Cron expression (`cron_expr`)

For complex schedules (specific time of day, weekdays only, etc.):

```json
{
  "action": "add",
  "message": "Good morning! ☀️ Here's your daily briefing.",
  "cron_expr": "0 9 * * *"
}
```

**Cron expression format**: `minute hour day-of-month month day-of-week`

| Expression | Meaning |
|-----------|---------|
| `0 9 * * *` | Every day at 9:00 AM |
| `0 9 * * 1-5` | Weekdays at 9:00 AM |
| `30 18 * * *` | Every day at 6:30 PM |
| `0 */2 * * *` | Every 2 hours on the hour |
| `0 9,18 * * *` | At 9 AM and 6 PM |
| `*/30 * * * *` | Every 30 minutes |
| `0 0 * * 0` | Every Sunday at midnight |
| `0 9 1 * *` | 1st of every month at 9 AM |

## Command execution (`command`)

Run a shell command instead of showing a message:

```json
{
  "action": "add",
  "message": "Disk usage check",
  "command": "df -h",
  "every_seconds": 3600
}
```

When `command` is set, the agent executes it and reports the output. Good for:
- Health checks (`df -h`, `free -m`)
- Git status (`git -C /path/to/repo status`)
- Custom scripts

## Delivery mode (`deliver`)

| `deliver` | Behavior |
|-----------|----------|
| `true` (default) | Send message directly to user's chat |
| `false` | Let the agent process the message (for complex tasks) |

Use `deliver: false` when the task needs the agent to think (e.g., "summarize today's news").

## Common patterns

### Morning briefing
```json
{"action": "add", "message": "Morning check: what's on my calendar today?", "cron_expr": "0 9 * * 1-5", "deliver": false}
```

### Hydration reminder
```json
{"action": "add", "message": "💧 Time to drink water!", "every_seconds": 3600}
```

### Stand-up reminder
```json
{"action": "add", "message": "🧍 Time to stand up and stretch!", "every_seconds": 1800}
```

### Check GitHub PRs
```json
{"action": "add", "message": "Check open PRs", "command": "gh pr list --repo owner/repo --state open", "cron_expr": "0 10 * * 1-5"}
```

## Managing tasks

List all:
```json
{"action": "list"}
```

Remove by ID:
```json
{"action": "remove", "job_id": "abc123"}
```

Pause/resume:
```json
{"action": "disable", "job_id": "abc123"}
{"action": "enable", "job_id": "abc123"}
```

## Important notes

- Scheduled tasks survive restarts (persisted in SQLite)
- One-time tasks (`at_seconds`) auto-delete after firing
- Times are in the server's local timezone
- If `exec_timeout_minutes` is set in config, commands are killed after that duration
