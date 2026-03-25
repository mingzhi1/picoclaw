# Multi-Agent System Design

> Status: Implemented | Updated: 2026-03-25

## Overview

PicoClaw supports **multiple agent instances** running concurrently, each with its own configuration, LLM, tools, and workspace. This enables specialized agents for different tasks (coding, reviewing, ops) and parallel task execution.

```
┌─────────────────────────────────────────────────────────────┐
│                    User Message                              │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  AgentRegistry.ResolveRoute()                               │
│  - Pattern matching on message content                      │
│  - Default fallback to main agent                           │
└────────────────────────┬────────────────────────────────────┘
                         │
         ┌───────────────┼───────────────┐
         │               │               │
         ▼               ▼               ▼
┌───────────────┐ ┌───────────────┐ ┌───────────────┐
│  Agent: main  │ │  Agent: coder │ │ Agent: ops    │
│  (general)    │ │  (coding)     │ │ (devops)      │
│               │ │               │ │               │
│  LLM: gpt-4   │ │ LLM: claude   │ │ LLM: gemini   │
│  Tools: all   │ │ Tools: file   │ │ Tools: exec   │
│  Workspace: ~/│ │ Workspace: ~/ │ │ Workspace: ~/ │
└───────────────┘ └───────────────┘ └───────────────┘
```

---

## Architecture

### AgentRegistry

**Purpose:** Central management of all agent instances.

**Responsibilities:**
- Instantiate agents from configuration
- Route incoming messages to appropriate agent
- Enforce subagent spawn permissions
- Prevent duplicate outbound messages

**Implementation:**
```go
// pkg/agent/registry.go
type AgentRegistry struct {
    agents   map[string]*AgentInstance
    resolver *routing.RouteResolver
    mu       sync.RWMutex
}

func (r *AgentRegistry) ResolveRoute(input routing.RouteInput) routing.ResolvedRoute {
    return r.resolver.ResolveRoute(input)
}

func (r *AgentRegistry) CanSpawnSubagent(parentID, targetID string) bool {
    // Checks parent's subagents.allow_agents list
}
```

---

### AgentInstance

**Purpose:** Self-contained agent with isolated state.

**Components:**
- **LLM Provider**: Dedicated provider instance (can be different model/provider)
- **ToolRegistry**: Per-agent tool registration (can filter by skills)
- **Workspace**: Isolated filesystem boundary
- **TurnStore**: Per-agent conversation history (separate `turns.db`)
- **ActiveContext**: Per-session context (files, errors)
- **KVCache**: Per-agent cache (separate `cache.db`)

**Implementation:**
```go
// pkg/agent/instance.go
type AgentInstance struct {
    ID            string
    Name          string
    Workspace     string
    Model         string
    Provider      providers.LLMProvider
    Tools         *tools.ToolRegistry
    TurnStore     *TurnStore
    ActiveContext *ActiveContextStore
    // ...
}
```

---

## Configuration

### Basic Multi-Agent Setup

```json
{
  "agents": {
    "list": [
      {
        "id": "main",
        "name": "General Assistant",
        "default": true,
        "workspace": "~/.picoclaw/workspace/main"
      },
      {
        "id": "coder",
        "name": "Code Specialist",
        "workspace": "~/projects/myapp",
        "model_name": "anthropic/claude-sonnet-4.5"
      },
      {
        "id": "reviewer",
        "name": "Code Reviewer",
        "workspace": "~/projects/myapp",
        "model_name": "openai/gpt-4-turbo"
      }
    ],
    "defaults": {
      "model_name": "gemini/gemini-2.0-flash-exp"
    }
  }
}
```

### Routing Rules

```json
{
  "subagents": {
    "routing": {
      "rules": [
        {
          "pattern": "review|check|audit",
          "agent_id": "reviewer"
        },
        {
          "pattern": "implement|create|fix|refactor",
          "agent_id": "coder"
        },
        {
          "pattern": "deploy|restart|kubectl|docker",
          "agent_id": "ops"
        }
      ]
    }
  }
}
```

**Pattern Matching:** Case-insensitive regex on message content.

### Skills Filter

```json
{
  "subagents": {
    "agents": {
      "coder": {
        "skills_filter": ["code", "debug", "test"]
      },
      "reviewer": {
        "skills_filter": ["review", "security"]
      }
    }
  }
}
```

**Effect:** Only matching skills are loaded into agent's ToolRegistry.

### Spawn Permissions

```json
{
  "subagents": {
    "agents": {
      "main": {
        "allow_agents": ["coder", "reviewer", "ops"]
      },
      "coder": {
        "allow_agents": ["reviewer"]
      }
    }
  }
}
```

**Effect:** `main` can spawn any agent; `coder` can only spawn `reviewer`.

---

## Message Routing

### Flow

```
User Message
    │
    ▼
AgentRegistry.ResolveRoute(input)
    │
    │ RouteInput: {
    │   channel: "telegram",
    │   chat_id: "123456",
    │   message: "Review this PR",
    │   agent_hint: ""
    │ }
    ▼
RouteResolver checks routing rules (in order):
    1. Match "review" → agent_id: "reviewer"
    2. If no match → default agent

Returns: ResolvedRoute { AgentID: "reviewer" }
    │
    ▼
AgentLoop.processMessage() routes to AgentInstance "reviewer"
```

### Duplicate Message Prevention

**Problem:** Multiple agents might try to send the same response.

**Solution:** Before sending, content hash deduplication prevents repeat messages:

```go
// pkg/tools/message.go
type MessageTool struct {
    mu         sync.RWMutex
    sentHashes map[string]time.Time // content hash → timestamp
}

// HasRecentSimilar checks if a similar message was sent recently.
func (m *MessageTool) HasRecentSimilar(content string) bool {
    hash := hashContent(content)
    if ts, ok := m.sentHashes[hash]; ok {
        if time.Since(ts) < 5*time.Minute {
            return true
        }
    }
    return false
}
```

> **Note:** Cross-agent deduplication is handled at the MessageTool level per agent. A global deduplication layer across all agents is planned but not yet implemented.

---

## Subagent System

### Spawn Tool (Async)

**Purpose:** Delegate long-running tasks to specialized agents.

**Usage:**
```
User: "Deploy the app and monitor for errors"
Main Agent: spawn("ops", "Deploy app and monitor")
    │
    │ (returns immediately with task ID)
    ▼
Main Agent: "I've delegated this to our ops specialist. Task ID: abc123"

Ops Agent (background):
    1. Deploy app
    2. Monitor logs
    3. Report via MessageTool when complete
```

**Implementation:**
```go
// pkg/tools/spawn.go
func (t *SpawnTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
    agentID := args["agent_id"].(string)
    task := args["task"].(string)

    // Get target agent from registry
    agent, ok := t.registry.GetAgent(agentID)
    if !ok {
        return ErrorResult("agent not found")
    }

    // Check spawn permission
    if !t.registry.CanSpawnSubagent(t.parentAgentID, agentID) {
        return ErrorResult("spawn not permitted")
    }

    // Spawn async goroutine
    go agent.ProcessTask(task)

    return SuccessResult(fmt.Sprintf("Task spawned: %s", taskID))
}
```

---

### Subagent Tool (Sync)

**Purpose:** Get immediate result from specialized agent.

**Usage:**
```
User: "What's the best way to fix this bug?"
Main Agent: subagent("coder", "How to fix nil pointer in foo.go?")
    │
    │ (blocks until coder responds)
    ▼
Coder Agent: "Add nil check before line 42"
    │
    ▼
Main Agent: "The coding specialist suggests adding a nil check before line 42."
```

**Implementation:**
```go
// pkg/tools/subagent.go
func (t *SubagentTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
    agentID := args["agent_id"].(string)
    query := args["query"].(string)

    agent, ok := t.registry.GetAgent(agentID)
    if !ok {
        return ErrorResult("agent not found")
    }

    // Synchronous call (with timeout)
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    response, err := agent.QuerySync(ctx, query)
    if err != nil {
        return ErrorResult(err.Error())
    }

    return SuccessResult(response)
}
```

---

### SubagentManager

**Purpose:** Track spawned tasks and provide status.

**Implementation:**
```go
// pkg/tools/subagent.go
type SubagentManager struct {
    mu       sync.RWMutex
    tasks    map[string]*TaskInfo  // task_id → info
    registry *AgentRegistry
}

type TaskInfo struct {
    ID        string
    Task      string
    Label     string
    AgentID   string
    Status    string  // "running" | "completed" | "failed" | "canceled"
    Result    string
    Created   int64
}

func (m *SubagentManager) GetTask(taskID string) (SubagentTask, bool) {
    // Returns VALUE COPY — no pointer leaks
    m.mu.RLock()
    defer m.mu.RUnlock()
    task, ok := m.tasks[taskID]
    if !ok {
        return SubagentTask{}, false
    }
    return *task, true // Deep copy via value dereference
}

func (m *SubagentManager) ListTasks() []SubagentTask {
    // Returns slice of value copies
}
```

**Key Design:** All getters return **value copies** — no shared pointer leaks between agents.

---

## Context Isolation

### Workspace Isolation

Each agent has its own filesystem sandbox:

```go
// pkg/agent/instance.go
func NewAgentInstance(cfg *config.AgentConfig, ...) *AgentInstance {
    sandbox, _ := sandboxFs(cfg.Workspace)

    return &AgentInstance{
        Workspace: cfg.Workspace,
        sandboxFs: sandbox,
        // ...
    }
}
```

**Effect:** `coder` agent in `~/projects/app` cannot access `reviewer` agent's `~/projects/reviews`.

---

### TurnStore Isolation

Each agent maintains separate conversation history:

```
~/.picoclaw/workspace/main/turns.db    ← main agent
~/.picoclaw/workspace/coder/turns.db   ← coder agent
~/.picoclaw/workspace/reviewer/turns.db ← reviewer agent
```

**Benefit:** Context pollution prevented — coder doesn't see main agent's casual chats.

---

### KVCache Isolation

Separate cache per agent:

```
~/.picoclaw/workspace/main/cache.db
~/.picoclaw/workspace/coder/cache.db
```

**Benefit:** No cache poisoning across agents.

---

## Shared Resources

### ToolRegistry (Per-Agent)

Tools are registered per-agent, but share the same underlying implementations:

```go
// pkg/agent/loop.go
func registerSharedTools(cfg *config.Config, registry *AgentRegistry, ...) {
    for _, agent := range registry.agents {
        // Register core tools
        agent.Tools.Register(filesystem.New(agent.sandboxFs))
        agent.Tools.Register(exec.New(agent.Workspace))

        // Apply skills_filter if configured
        if agent.SkillsFilter != nil {
            filterTools(agent.Tools, agent.SkillsFilter)
        }
    }
}
```

---

### MessageTool (Dedup-Aware)

Shared message history for duplicate detection:

```go
// pkg/tools/message.go
type MessageTool struct {
    mu        sync.RWMutex
    sentHashes map[string]time.Time  // content hash → timestamp
}

func (m *MessageTool) HasRecentSimilar(content string) bool {
    hash := hashContent(content)
    if ts, ok := m.sentHashes[hash]; ok {
        if time.Since(ts) < 5*time.Minute {
            return true
        }
    }
    return false
}
```

---

## Topic Tracking (Multi-Agent)

**Problem:** Topics span multiple agents (main → coder → reviewer).

**Solution:** Topics stored with `agent_id` + `channel_key`:

```sql
CREATE TABLE topics (
    id           TEXT PRIMARY KEY,
    agent_id     TEXT,
    channel_key  TEXT,
    title        TEXT,
    status       TEXT,
    created_ts   INTEGER
);
```

**Query:** Instant memory includes topics from ALL agents for same `channel_key`.

---

## Use Cases

### 1. Code Development Workflow

```
User (Telegram): "Add a new endpoint for user profile"
    │
    ▼
Route: "endpoint" → coder agent
    │
    ▼
Coder Agent:
    1. Read existing routes
    2. Create new handler
    3. Write tests
    │
    ▼
User: "Great! Now have someone review it"
    │
    ▼
Route: "review" → reviewer agent
    │
    ▼
Reviewer Agent:
    1. Read new code
    2. Check security patterns
    3. Suggest improvements
```

---

### 2. DevOps Delegation

```
User (CLI): "Deploy to staging and verify health"
    │
    ▼
Route: "deploy" → ops agent
    │
    ▼
Ops Agent:
    1. kubectl apply
    2. Wait for rollout
    3. Health check
    │
    ▼
Ops Agent: "Deployed. Health: OK"
```

---

### 3. Parallel Task Execution

```
User: "Review PR #42 and check if tests pass"
    │
    ▼
Main Agent spawns:
    - reviewer agent: "Review PR #42"
    - coder agent: "Check CI test results"
    │
    │ (both run concurrently)
    ▼
Main Agent aggregates results:
    "Reviewer found 2 issues. Tests passing."
```

---

## Configuration Best Practices

### 1. Specialize by Model Strength

```json
{
  "agents": {
    "list": [
      {
        "id": "creative",
        "model_name": "anthropic/claude-3.5-sonnet"
      },
      {
        "id": "precise",
        "model_name": "openai/gpt-4-turbo"
      },
      {
        "id": "fast",
        "model_name": "gemini/gemini-2.0-flash-exp"
      }
    ]
  }
}
```

**Use:**
- `creative`: Brainstorming, design
- `precise`: Math, logic, code review
- `fast`: Quick lookups, simple tasks

---

### 2. Workspace Separation

```json
{
  "agents": {
    "list": [
      {
        "id": "personal",
        "workspace": "~/.picoclaw/personal"
      },
      {
        "id": "work",
        "workspace": "~/company/project"
      }
    ]
  }
}
```

**Benefit:** Strict separation of personal and work contexts.

---

### 3. Hierarchical Permissions

```json
{
  "subagents": {
    "agents": {
      "main": {
        "allow_agents": "*"
      },
      "coder": {
        "allow_agents": ["reviewer"]
      },
      "intern": {
        "allow_agents": []
      }
    ]
  }
}
```

**Effect:** `main` can spawn anyone; `coder` can spawn reviewer; `intern` cannot spawn anyone.

---

## Performance Considerations

| Operation | Latency | Notes |
|-----------|---------|-------|
| Route resolution | <1ms | Regex match on short string |
| Agent instantiation | 10-50ms | Provider setup |
| Subagent spawn (async) | <5ms | Goroutine creation |
| Subagent query (sync) | 500-5000ms | LLM call + tool loop |
| Cross-agent message check | <1ms | Hash lookup |

**Recommendation:** Use async spawn for long tasks, sync for quick queries.

---

## Future Enhancements

1. **Agent-to-Agent Direct Communication:** Currently mediated by MessageTool; direct channels would reduce latency.

2. **Dynamic Agent Creation:** Spawn new agents on-the-fly for isolated task contexts.

3. **Shared Memory Pool:** Optional shared long-term memory across agents (with access control).

4. **Agent Orchestration:** Coordinator agent that manages task distribution and result aggregation.

5. **Priority Queues:** High-priority messages preempt low-priority agent tasks.
