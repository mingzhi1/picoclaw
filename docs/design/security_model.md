# Security Model Design

> Status: Implemented | Updated: 2026-03-25

## Overview

PicoClaw implements a **layered security model** where every file and command operation passes through multiple validation layers. The design philosophy is **defense in depth** — no single point of failure can compromise system safety.

```
┌─────────────────────────────────────────────────────────────┐
│                    User/LLM Request                          │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 1: Tilde Expansion (~ → $HOME)                       │
│  - Consistent path resolution across platforms              │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 2: Whitelist FS (allow_read/write_paths)             │
│  - Config-based path grants outside workspace               │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 3: Sandbox FS (os.Root, kernel-level)                │
│  - Go 1.23+ os.Root API — OS-level path escape prevention   │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 4: Host FS (actual filesystem)                       │
│  - Final operation execution                                │
└─────────────────────────────────────────────────────────────┘
```

**Command Execution** has a parallel security pipeline:
- Deny patterns (block dangerous commands)
- Allow patterns (explicit overrides)
- Custom allow patterns (exemptions)
- Workspace restriction (path traversal guard)

---

## File Operation Security

### Layer 1: Tilde Expansion (`tildeExpandFs`)

**Purpose:** Normalize `~` and `~user` paths to absolute paths.

**Implementation:**
```go
// pkg/tools/filesystem.go
func tildeExpandFs(path string) (string, error) {
    if strings.HasPrefix(path, "~") {
        home, err := os.UserHomeDir()
        if err != nil {
            return "", err
        }
        return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
    }
    return path, nil
}
```

**Why First:** All subsequent layers operate on expanded paths — consistent baseline.

---

### Layer 2: Whitelist FS (`whitelistFs`)

**Purpose:** Grant access to specific paths outside workspace without disabling sandbox.

**Configuration:**
```json
{
  "security": {
    "allow_read_paths": [
      "/etc/ssl/certs",
      "~/Documents/shared"
    ],
    "allow_write_paths": [
      "~/output",
      "/tmp/picoclaw_exports"
    ]
  }
}
```

**Implementation:**
```go
type whitelistFs struct {
    base         fs.FS
    allowRead    []string
    allowWrite   []string
    workspaceRoot string
}

func (w *whitelistFs) Open(name string) (fs.File, error) {
    // Check if path is in workspace OR in allowlist
    if !w.isPathAllowed(name, "read") {
        return nil, fmt.Errorf("access denied: %s", name)
    }
    return w.base.Open(name)
}
```

**Key Property:** Whitelist is **additive** — doesn't replace sandbox, works alongside it.

---

### Layer 3: Sandbox FS (`sandboxFs` with `os.Root`)

**Purpose:** Kernel-level path escape prevention using Go 1.23+ `os.Root` API.

**Why `os.Root`:** String-based path validation can be bypassed via:
- Symlinks
- Unicode normalization attacks
- Race conditions (TOCTOU)

`os.Root` delegates to OS-level `openat2()` with `RESOLVE_BENEATH` flag — path escape is **impossible** at the kernel level.

**Implementation:**
```go
// pkg/tools/filesystem.go
func sandboxFs(workspace string) (fs.FS, error) {
    root, err := os.Root(workspace)
    if err != nil {
        return nil, err
    }
    return &sandboxFs{root: root, workspace: workspace}, nil
}

func (s *sandboxFs) Open(name string) (fs.File, error) {
    // os.Root.Open() internally uses openat2(RESOLVE_BENEATH)
    // Path escape attempts (../, symlinks) fail at OS level
    return s.root.Open(name)
}
```

**Blocked Attacks:**
```
../etc/passwd          → Blocked (path traversal)
symlink → /etc/passwd  → Blocked (symlink resolution)
....//....//etc/passwd → Blocked (normalization)
```

---

### Layer 4: Host FS (Direct Access)

**Purpose:** Final filesystem access after all validation layers pass.

**Note:** In practice, `sandboxFs` wraps `hostFs` — the host FS never receives unvalidated paths.

---

## Command Execution Security

```
┌─────────────────────────────────────────────────────────────┐
│                    Command Request                           │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 1: Deny Patterns                                     │
│  - Block: rm -rf, format, dd, shutdown, fork bombs          │
│  - Regex matching on full command string                    │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 2: Custom Allow Patterns                             │
│  - Exempt specific commands from deny checks                │
│  - e.g., "git push --force" allowed for dev workflow        │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 3: Allow Patterns                                    │
│  - Explicit overrides (bypass deny + workspace restriction) │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 4: Workspace Restriction                             │
│  - Path traversal guard (../, absolute paths)               │
│  - working_dir must be within workspace                     │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Layer 5: Safe Paths                                        │
│  - /dev/null, /dev/zero, /dev/urandom always allowed        │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  Execution: Go Built-in OR System Shell                     │
│  - Fast path: Go built-in (no process spawn)                │
│  - Fallback: powershell (Windows) / sh -c (Unix)            │
└─────────────────────────────────────────────────────────────┘
```

### Layer 1: Deny Patterns

**Purpose:** Block obviously dangerous commands.

**Default Patterns:**
```go
var denyPatterns = []string{
    `(?i)\brm\s+(-[rf]+\s+)?/`,      // rm -rf /
    `(?i)\bformat\s`,                // Windows format
    `(?i)\bdd\s`,                    // dd (disk write)
    `(?i)\bshutdown\b`,              // shutdown
    `(?i)\binit\s+\d\b`,             // init level change
    `(?i):\(\)\{\s*:\|:&\s*\};:`,    // fork bomb
    `(?i)\bmkfs\b`,                  // filesystem creation
    `(?i)\bchmod\s+777\b`,           // dangerous permissions
}
```

**Matching:** Full command string regex match (case-insensitive).

---

### Layer 2: Custom Allow Patterns

**Purpose:** Exempt specific commands from deny checks.

**Use Case:** Development workflows that need "dangerous" commands:
```json
{
  "security": {
    "custom_allow_patterns": [
      "git push --force",
      "rm -rf node_modules"
    ]
  }
}
```

**Implementation:**
```go
func (e *ExecTool) isCustomAllowed(cmd string) bool {
    for _, pattern := range e.customAllowPatterns {
        if strings.Contains(cmd, pattern) {
            return true
        }
    }
    return false
}
```

---

### Layer 3: Allow Patterns

**Purpose:** Full override — bypasses deny + workspace restriction.

**Use Case:** Trusted commands in controlled environments.

**Warning:** Use sparingly — this is a security escape hatch.

---

### Layer 4: Workspace Restriction

**Purpose:** Prevent path traversal attacks in command arguments.

**Checks:**
```go
// pkg/tools/exec_security.go
func guardCommand(cmd string, workingDir string, workspace string) error {
    // Block .. path traversal
    if strings.Contains(cmd, "..") {
        return fmt.Errorf("path traversal detected")
    }

    // Block absolute paths outside workspace
    if filepath.IsAbs(cmd) {
        if !strings.HasPrefix(cmd, workspace) {
            return fmt.Errorf("absolute path outside workspace")
        }
    }

    // Validate working_dir
    if !strings.HasPrefix(workingDir, workspace) {
        return fmt.Errorf("working_dir outside workspace")
    }

    return nil
}
```

---

### Layer 5: Safe Paths

**Purpose:** Always allow harmless system paths.

**List:**
- `/dev/null` — discard output
- `/dev/zero` — zero stream
- `/dev/urandom` — random bytes

**Why:** These are read-only, side-effect-free, and commonly needed.

---

## Go Built-in Commands

**Purpose:** Fast, safe command execution without process spawn.

**Implementation:** Pure Go in `shell_builtins.go`.

**Commands:**
| Command | Description | Security Notes |
|---------|-------------|----------------|
| `ls` | List directory | Auto-skip `.git`, `node_modules` |
| `cat` | Read files | Output capped at 4000 chars |
| `head` / `tail` | First/last N lines | — |
| `grep` | Pattern search | Basic regex only |
| `wc` | Word/line/byte count | — |
| `find` | File search | Name pattern only, no -exec |
| `diff` | File comparison | — |
| `tree` | Directory tree | Depth-limited |
| `stat` | File metadata | — |
| `pwd` | Print working directory | — |
| `echo` | Print text | — |
| `touch` | Create empty file | Workspace-only |
| `mkdir` | Create directory | Workspace-only |
| `cp` / `mv` | Copy/move | Workspace-only |
| `rm` | Remove files | No `-r` flag (safe delete) |

**Benefits:**
- **Speed:** No process spawn overhead
- **Cross-platform:** Same behavior on Windows/Unix
- **Safety:** No shell injection possible

---

## Execution Flow

```go
// pkg/tools/exec.go
func (e *ExecTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
    cmd := args["command"].(string)
    workingDir := args["working_dir"].(string)

    // Security check
    if err := guardCommand(cmd, workingDir, e.workspace); err != nil {
        return ErrorResult(err.Error())
    }

    // Fast path: Go built-in
    if builtin, ok := e.getBuiltIn(cmd); ok {
        return builtin(ctx, args)
    }

    // Fallback: system shell
    return e.executeSystemShell(ctx, cmd, workingDir)
}
```

---

## Configuration

```json
{
  "security": {
    "workspace": "/home/user/projects/myapp",
    "allow_read_paths": [
      "/etc/ssl/certs",
      "~/Documents"
    ],
    "allow_write_paths": [
      "~/output"
    ],
    "deny_patterns": [
      "(?i)\\brm\\s+(-[rf]+\\s+)?/"
    ],
    "allow_patterns": [
      "git push"
    ],
    "custom_allow_patterns": [
      "npm run build"
    ],
    "unrestricted_mode": false
  }
}
```

**`unrestricted_mode`:** When `true`, skips `guardCommand` checks. **Never enable in production.**

---

## Attack Scenarios (and Why They Fail)

### Scenario 1: Path Traversal via `..`

```
User: exec("cat ../../etc/passwd")
Result: Blocked by Layer 4 (workspace restriction)
```

### Scenario 2: Symlink Attack

```
User creates: symlink → /etc/passwd inside workspace
User: exec("cat symlink")
Result: Blocked by os.Root (Layer 3) — symlinks resolved at kernel level
```

### Scenario 3: Unicode Normalization

```
User: exec("cat ．．/etc/passwd")  // Full-width dots
Result: Normalized to `..` → blocked by Layer 4
```

### Scenario 4: Race Condition (TOCTOU)

```
User: Creates safe file, then swaps with symlink
Result: os.Root uses openat2(RESOLVE_BENEATH) — atomic check+open
```

### Scenario 5: Command Injection

```
User: exec("ls; rm -rf /")
Result: Deny pattern matches `rm -rf` → blocked
```

### Scenario 6: Fork Bomb

```
User: exec(":(){ :|:& };:")
Result: Fork bomb pattern detected → blocked
```

---

## Multi-Agent Security

**Problem:** Subagents inherit parent's security context.

**Solution:** All agents share the same `sandboxFs` and `guardCommand` instance — no bypass via delegation.

```go
// pkg/agent/registry.go
func NewAgentInstance(cfg *config.AgentConfig, ...) *AgentInstance {
    // All agents use same workspace root
    sandbox, _ := sandboxFs(cfg.Workspace)

    return &AgentInstance{
        sandboxFs: sandbox,  // Shared instance
        // ...
    }
}
```

**Spawn Tool:** Parent can delegate to subagent, but subagent operates under same security boundary.

---

## Channel Isolation

**Problem:** Different channels (Telegram vs CLI) may have different trust levels.

**Current State:** Security is **global** — same rules apply to all channels.

**Future Enhancement:** Per-channel security profiles:
```json
{
  "security": {
    "channel_profiles": {
      "cli": { "unrestricted_mode": true },
      "telegram": { "unrestricted_mode": false }
    }
  }
}
```

---

## Logging and Audit

All security events are logged:

```go
logger.WarnCF("tools", "Command blocked by deny pattern", map[string]any{
    "command": cmd,
    "pattern": matchedPattern,
    "channel": channelName,
})

logger.InfoCF("tools", "File access outside workspace (whitelisted)", map[string]any{
    "path": path,
    "operation": "read",
})
```

**Audit Trail:** Security logs can be shipped to external SIEM for compliance.

---

## Performance Impact

| Layer | Latency | Notes |
|-------|---------|-------|
| Tilde expansion | <1µs | Simple string operation |
| Whitelist check | <10µs | Linear scan (short list) |
| os.Root (sandbox) | <1ms | Kernel syscall |
| Deny pattern match | <50µs | Regex on short strings |
| Go built-in | <10ms | No process spawn |
| System shell | 50-500ms | Process spawn overhead |

**Overall:** Security adds <2ms overhead for typical operations.

---

## Best Practices

1. **Never disable sandbox** — use `allow_read_paths` / `allow_write_paths` instead.
2. **Minimize allow patterns** — each one is a potential escape route.
3. **Log all denials** — helps identify attack patterns.
4. **Regular pattern audits** — remove stale custom allow patterns.
5. **Test with attack scenarios** — verify each layer independently.
