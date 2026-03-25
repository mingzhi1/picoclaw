# Runtime Commands

> Updated: 2026-03-25

## Slash Commands

All commands start with `/`, handled synchronously by `Reflector.HandleCommand()`.

| Command | Usage | Description |
|---------|-------|-------------|
| `/help` | `/help` | List all commands |
| `/memory list` | `/memory list [N]` | Show recent N memories (default 10) |
| `/memory add` | `/memory add <text> #tags` | Add a memory with tags |
| `/memory delete` | `/memory delete <id>` | Delete memory by ID |
| `/memory edit` | `/memory edit <id> <text> #tags` | Edit existing memory content and tags |
| `/memory search` | `/memory search <tags>` | Search memories by tags |
| `/memory stats` | `/memory stats` | Memory statistics (count, tags distribution) |
| `/cot feedback` | `/cot feedback <1\|0\|-1>` | Rate last CoT strategy (positive/neutral/negative) |
| `/cot stats` | `/cot stats` | CoT performance statistics |
| `/cot history` | `/cot history [N]` | Show recent N CoT usages |
| `/shell` | `/shell <cmd> [args]` | Execute shell command (Go built-in or system) |
| `/show model` | `/show model` | Show current model configuration |
| `/show agents` | `/show agents` | List all configured agents |
| `/list agents` | `/list agents` | List available agents |
| `/list channels` | `/list channels` | List active channels |
| `/switch model to` | `/switch model to <name>` | Switch to a different model |
| `/switch channel to` | `/switch channel to <name>` | Switch to a different channel |
| `/runtime status` | `/runtime status` | Runtime diagnostics (memory, topics, checkpoints) |
| `/rag search` | `/rag search <query>` | RAG vector search |
| `/rag add` | `/rag add <text>` | Add text to RAG store |
| `/tokens` | `/tokens [today\|week\|all\|channel]` | Show token usage statistics |

## Shell Architecture

```
/shell <cmd> <args>
  → Reflector.cmdShell() delegates to ShellInstance.Execute()

ShellInstance (pkg/tools/shell_instance.go)
  ├─ Go Built-in (shell_builtins.go, pure Go, cross-platform)
  │   ls, cat, head, tail, grep, wc, find, diff, tree,
  │   stat, pwd, echo, touch, mkdir, cp, mv, rm
  └─ Dev Tool Passthrough → ExecTool.Execute()
      go, git, node, python, npm, cargo, make, jq, rg, etc.

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

4. **Output capping**: All shell output capped at 4000 chars to prevent context overflow.

## Security

### File Operations

- **tildeExpandFs**: `~` → `$HOME` expansion (all file tools)
- **whitelistFs**: Config `allow_read_paths` / `allow_write_paths`
- **sandboxFs**: `os.Root` workspace restriction (kernel-level, Go 1.23+)
- **hostFs**: Direct filesystem access (last resort)

### Command Execution

- **denyPatterns**: Block `rm -rf`, `format`, `dd`, `shutdown`, fork bombs
- **allowPatterns**: Custom overrides (e.g., `git push origin`)
- **customAllowPatterns**: Exempt specific commands from deny checks
- **workspace restriction**: Path validation for `working_dir`
- **safePaths**: `/dev/null`, `/dev/zero`, `/dev/urandom` always allowed

### Go Built-in Commands

Pure Go implementations, auto-skip `.git`/`node_modules`, output capped at 4000 chars:

| Command | Description |
|---------|-------------|
| `ls` | List directory with tree view |
| `cat` | Concatenate and print files |
| `head` / `tail` | First/last N lines |
| `grep` | Pattern search (basic regex) |
| `wc` | Word/line/byte count |
| `find` | File search by name pattern |
| `diff` | File comparison |
| `tree` | Directory tree visualization |
| `stat` | File statistics |
| `pwd` | Print working directory |
| `echo` | Print text |
| `touch` | Create empty file |
| `mkdir` | Create directory |
| `cp` / `mv` | Copy/move files |
| `rm` | Remove files (safe, no recursive by default) |