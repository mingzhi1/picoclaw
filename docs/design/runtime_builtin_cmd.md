# Runtime Commands

> Updated: 2026-03-04

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

## Shell Architecture

```
/shell <cmd> <args>
  → Reflector.cmdShell() delegates to ShellInstance.Execute()

ShellInstance (pkg/tools/shell_instance.go)
  ├─ Go Built-in (shell_builtins.go, pure Go, cross-platform)
  │   ls, cat, head, tail, grep, wc, find, diff, tree,
  │   stat, pwd, echo, touch, mkdir, cp, mv
  └─ Dev Tool Passthrough → ExecTool.Execute()
      go, git, node, python, npm, cargo, make, jq, rg

ExecTool (pkg/tools/exec.go)
  ├─ Fast path: Go built-in for simple commands (unrestricted mode only)
  └─ System shell: powershell (Windows) / sh -c (Unix)
      with guardCommand security checks
```

### Key Design Decisions

1. **Single source of truth**: ShellInstance owns all shell execution logic.
   Reflector's `cmdShell` is a 3-line delegation.

2. **Unified security**: ExecTool's `guardCommand` is the single security gate.
   No separate deny list in Reflector (eliminated `shellDenySubstrings`).

3. **Built-in fast path**: When LLM calls `exec("ls")` in unrestricted mode,
   Go built-in runs directly — no process spawn, faster and cross-platform.
   Restricted mode always goes through `guardCommand` first.

## Security

- Built-in: Go stdlib only, auto-skip `.git`/`node_modules`, output capped at 4000 chars
- ExecTool: deny patterns + allow patterns + workspace restriction + safe paths
- Unknown commands: rejected with available command list