# Runtime Commands

## Slash Commands

All commands start with `/`, handled synchronously by `Reflector.HandleCommand()`.

| Command | Usage | Description |
|---------|-------|-------------|
| `/help` | `/help` | List all commands |
| `/memory list` | `/memory list` | Show recent memories |
| `/memory add` | `/memory add <text> #tags` | Add a memory |
| `/memory delete` | `/memory delete <id>` | Delete by ID |
| `/memory search` | `/memory search <tags>` | Search by tags |
| `/memory stats` | `/memory stats` | Memory statistics |
| `/cot feedback` | `/cot feedback <1\|0\|-1>` | Rate last CoT strategy |
| `/cot stats` | `/cot stats` | CoT performance stats |
| `/cot history` | `/cot history [N]` | Recent CoT usage |
| `/shell` | `/shell <cmd> [args]` | Execute shell command |
| `/show model` | `/show model` | Current model |
| `/list agents` | `/list agents` | List agents |
| `/switch model to` | `/switch model to <name>` | Switch model |
| `/runtime status` | `/runtime status` | Runtime diagnostics |

## /shell Architecture

```
/shell <cmd> <args>
  ├─ Built-in (pure Go, cross-platform)
  │   ls, cat, head, tail, grep, wc, find, diff, tree,
  │   stat, pwd, echo, touch, mkdir, cp, mv
  └─ Dev Tool Passthrough (via exec tool)
      go, git, node, python, npm, cargo, make, jq, rg
```

## Security

- Built-in: Go stdlib only, auto-skip `.git`/`node_modules`, output capped at 4000 chars
- Passthrough: whitelist + deny patterns (`| sh`, `$()`, etc.) + ExecTool workspace restriction
- Unknown commands: rejected