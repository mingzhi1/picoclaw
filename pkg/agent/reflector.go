// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/infra/logger"
	"github.com/sipeed/picoclaw/pkg/llm/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// ---------------------------------------------------------------------------
// Runtime 鈥?unified execution engine
//
// The Runtime serves two purposes:
//
//  1. Post-LLM processing: runs async processors after the main LLM responds
//     (memory extraction, CoT feedback, error tracking).
//
//  2. Slash commands: handles /{cmd} {args} from users, executed synchronously.
//
// Both share the same MemoryStore and lightweight LLM provider.
// ---------------------------------------------------------------------------

// --- Post-LLM Processing ---------------------------------------------------

// RuntimeInput captures everything that happened during a single agent turn.
type RuntimeInput struct {
	UserMessage    string   // Original user message
	AssistantReply string   // Main LLM's final response
	Intent         string   // Pre-LLM detected intent
	Tags           []string // Pre-LLM extracted tags
	CotPrompt      string   // Generated thinking strategy
	ToolCalls      []ToolCallRecord
	Iterations     int // Number of LLM iterations used
	Score          int // Phase 3 CalcTurnScore result (set by SyncPhase3)
	ChannelKey     string // "channel:chatID" (set by runAgentLoop)
}

// ToolCallRecord captures one tool invocation and its outcome.
type ToolCallRecord struct {
	Name     string
	Error    string        // Empty if success
	Duration time.Duration // How long the tool took
}

// RuntimeProcessor is a single post-LLM processing step.
type RuntimeProcessor interface {
	Name() string
	Process(ctx context.Context, input RuntimeInput, memory *MemoryStore) error
}

// --- Slash Commands ---------------------------------------------------------

// CommandHandler handles a single /{cmd} invocation.
type CommandHandler func(args []string, memory *MemoryStore) string

// CommandDef defines a registered slash command.
type CommandDef struct {
	Name        string // e.g. "memory"
	Usage       string // e.g. "/memory [list|add|search] ..."
	Description string
	Handler     CommandHandler
}

// --- Reflector (Phase 3) ----------------------------------------------------

// Reflector manages post-LLM processors and slash commands.
// This is Phase 3 (Reflect) of the Runtime Loop.
type Reflector struct {
	provider       providers.LLMProvider
	model          string
	processors     []RuntimeProcessor
	commands       map[string]CommandDef
	mu             sync.RWMutex
	timeout        time.Duration
	shellInstance  *tools.ShellInstance   // Consolidated shell execution
	agentRegistry  *AgentRegistry         // For /show, /list, /switch
	channelManager *channels.Manager      // For /list channels, /switch channel
}


// NewReflector creates a new Reflector (Phase 3) with built-in processors and commands.
func NewReflector(provider providers.LLMProvider, model string) *Reflector {
	r := &Reflector{
		provider: provider,
		model:    model,
		timeout:  30 * time.Second,
		commands: make(map[string]CommandDef),
	}

	// Built-in processors (post-LLM, async).
	// Note: CotEvaluator and MemoryExtractor are intentionally removed from the
	// default pipeline 鈥?memory extraction is now handled by MemoryDigestWorker
	// (batch, background) rather than per-turn inline LLM calls.
	r.RegisterProcessor(&ErrorTracker{})

	// Built-in slash commands.
	r.RegisterCommand(CommandDef{
		Name:        "help",
		Usage:       "/help",
		Description: "Show all available commands",
		Handler:     r.cmdHelp,
	})
	r.RegisterCommand(CommandDef{
		Name:        "memory",
		Usage:       "/memory [list|add|delete|edit|search|stats] ...",
		Description: "Manage long-term memory",
		Handler:     cmdMemory,
	})
	r.RegisterCommand(CommandDef{
		Name:        "cot",
		Usage:       "/cot [feedback|stats|history] ...",
		Description: "Manage CoT learning",
		Handler:     cmdCot,
	})
	r.RegisterCommand(CommandDef{
		Name:        "runtime",
		Usage:       "/runtime [status|processors]",
		Description: "Runtime status and diagnostics",
		Handler:     r.cmdRuntimeStatus,
	})
	r.RegisterCommand(CommandDef{
		Name:        "shell",
		Usage:       "/shell <cmd> [args...]",
		Description: "Execute shell command in workspace",
		Handler:     r.cmdShell,
	})

	// System commands (migrated from handleCommand).
	r.RegisterCommand(CommandDef{
		Name:        "show",
		Usage:       "/show [model|channel|agents]",
		Description: "Show current settings",
		Handler:     r.cmdShow,
	})
	r.RegisterCommand(CommandDef{
		Name:        "list",
		Usage:       "/list [models|channels|agents]",
		Description: "List available resources",
		Handler:     r.cmdList,
	})
	r.RegisterCommand(CommandDef{
		Name:        "switch",
		Usage:       "/switch [model|channel] to <name>",
		Description: "Switch model or channel",
		Handler:     r.cmdSwitch,
	})

	return r
}


// RegisterProcessor adds a post-LLM processor.
func (r *Reflector) RegisterProcessor(p RuntimeProcessor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processors = append(r.processors, p)
}

// RegisterCommand adds a slash command.
func (r *Reflector) RegisterCommand(cmd CommandDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[cmd.Name] = cmd
}

// SetTools extracts the ExecTool from the registry and creates a ShellInstance.
func (r *Reflector) SetTools(registry *tools.ToolRegistry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var execTool *tools.ExecTool
	if tool, ok := registry.Get("exec"); ok {
		execTool, _ = tool.(*tools.ExecTool)
	}
	var workspace string
	var restrict bool
	if execTool != nil {
		workspace = execTool.WorkingDir()
		restrict = execTool.RestrictToWorkspace()
	}
	r.shellInstance = tools.NewShellInstance(execTool, workspace, restrict)
}

// SetAgentInfo provides the Runtime with agent and channel references
// needed by system commands (/show, /list, /switch).
func (r *Reflector) SetAgentInfo(reg *AgentRegistry, cm *channels.Manager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentRegistry = reg
	r.channelManager = cm
}

// ---------------------------------------------------------------------------
// Post-LLM: async execution
// ---------------------------------------------------------------------------

// SyncPhase3 runs the synchronous, low-latency part of Phase 3:
// it calculates the Turn score and returns it. The caller must invoke this
// BEFORE PublishOutbound so that Active Context is ready for the next turn.
// Execution target: < 2ms (pure CPU, no I/O).
func (r *Reflector) SyncPhase3(input RuntimeInput) int {
	score := CalcTurnScore(input)
	logger.DebugCF("reflector", "SyncPhase3 score",
		map[string]any{"score": score, "intent": input.Intent, "tools": len(input.ToolCalls)})
	return score
}

// AsyncPhase3 runs the asynchronous post-turn work: persisting TurnRecord,
// running legacy processors, etc. Call this AFTER PublishOutbound.
func (r *Reflector) AsyncPhase3(input RuntimeInput, memory *MemoryStore, turnStore *TurnStore, activeCtx *ActiveContextStore) {
	if r == nil {
		return
	}

	r.mu.RLock()
	processors := make([]RuntimeProcessor, len(r.processors))
	copy(processors, r.processors)
	r.mu.RUnlock()

	go func() {
		tctx, cancel := context.WithTimeout(context.Background(), r.timeout)
		defer cancel()

		// Run registered processors (currently: ErrorTracker).
		if memory != nil {
			for _, p := range processors {
				select {
				case <-tctx.Done():
					return
				default:
				}
				start := time.Now()
				if err := p.Process(tctx, input, memory); err != nil {
					logger.WarnCF("reflector", "Processor failed",
						map[string]any{"processor": p.Name(), "error": err.Error(),
							"ms": time.Since(start).Milliseconds()})
				}
			}
		}

		// Persist TurnRecord to turns.db.
		if turnStore != nil && input.UserMessage != "" {
			record := TurnRecord{
				Ts:         time.Now().Unix(),
				ChannelKey: input.ChannelKey,
				Score:      input.Score,
				Intent:     input.Intent,
				Tags:       input.Tags,
				Status:     "pending",
				UserMsg:    sanitizeUserMsg(input.UserMessage),
				Reply:      sanitizeReply(input.AssistantReply),
				ToolCalls:  input.ToolCalls,
			}
			if err := turnStore.Insert(record); err != nil {
				logger.WarnCF("reflector", "TurnRecord insert failed",
					map[string]any{"error": err.Error()})
			}
		}
	}()
}

// RunPostLLM is kept for backward compatibility. New code should use
// SyncPhase3 + AsyncPhase3 instead.
func (r *Reflector) RunPostLLM(input RuntimeInput, memory *MemoryStore) {
	r.AsyncPhase3(input, memory, nil, nil)
}

// ---------------------------------------------------------------------------
// Slash commands: synchronous execution
// ---------------------------------------------------------------------------

// HandleCommand tries to handle a /{cmd} message.
// Returns (response, true) if handled, ("", false) if not a known command.
func (r *Reflector) HandleCommand(content string, memory *MemoryStore) (string, bool) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "/") {
		return "", false
	}

	parts := strings.Fields(content)
	if len(parts) == 0 {
		return "", false
	}

	cmdName := strings.TrimPrefix(parts[0], "/")
	args := parts[1:]

	r.mu.RLock()
	cmd, ok := r.commands[cmdName]
	r.mu.RUnlock()

	if !ok {
		return "", false // Not our command 鈥?let AgentLoop's handleCommand try.
	}

	if memory == nil {
		return "鈿狅笍 Memory store not available", true
	}

	return cmd.Handler(args, memory), true
}

// ListCommands returns a formatted help text for all registered commands.
func (r *Reflector) ListCommands() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("**Runtime Commands**\n\n")
	for _, cmd := range r.commands {
		fmt.Fprintf(&sb, "鈥?`%s` 鈥?%s\n", cmd.Usage, cmd.Description)
	}
	return sb.String()
}
// ===========================================================================
// Built-in processors (post-LLM, async)
// ===========================================================================

// --- ErrorTracker (no LLM) --------------------------------------------------

type ErrorTracker struct{}

func (e *ErrorTracker) Name() string { return "error_tracker" }

func (e *ErrorTracker) Process(_ context.Context, input RuntimeInput, _ *MemoryStore) error {
	for _, tc := range input.ToolCalls {
		if tc.Error == "" {
			continue
		}
		logger.InfoCF("reflector", "Tool error recorded",
			map[string]any{"tool": tc.Name, "error": tc.Error})
	}
	return nil
}

// --- CotEvaluator (LLM) ----------------------------------------------------

type CotEvaluator struct {
	provider providers.LLMProvider
	model    string
}

func (c *CotEvaluator) Name() string { return "cot_evaluator" }

const cotEvalPrompt = `Rate how well the thinking strategy helped answer the user's question.

Question: %s
Strategy: %s
Response (first 500 chars): %s

Respond with ONLY one JSON: {"score": <-1|0|1>}
1 = good, 0 = neutral, -1 = poor`

func (c *CotEvaluator) Process(ctx context.Context, input RuntimeInput, memory *MemoryStore) error {
	if input.CotPrompt == "" {
		return nil
	}

	reply := input.AssistantReply
	if len(reply) > 500 {
		reply = reply[:500]
	}

	resp, err := c.provider.Chat(ctx, []providers.Message{
		{Role: "user", Content: fmt.Sprintf(cotEvalPrompt, input.UserMessage, input.CotPrompt, reply)},
	}, nil, c.model, map[string]any{"max_tokens": 32, "temperature": 0.1})
	if err != nil {
		return fmt.Errorf("eval LLM failed: %w", err)
	}

	// Parse JSON (strip markdown fences if present).
	raw := strings.TrimSpace(resp.Content)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) > 2 {
			raw = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	var evalResult struct {
		Score int `json:"score"`
	}
	if err := json.Unmarshal([]byte(raw), &evalResult); err != nil {
		// Fallback: string matching.
		if strings.Contains(raw, `"score": 1`) || strings.Contains(raw, `"score":1`) {
			evalResult.Score = 1
		} else if strings.Contains(raw, `"score": -1`) || strings.Contains(raw, `"score":-1`) {
			evalResult.Score = -1
		}
	}

	if evalResult.Score != 0 {
		if err := memory.UpdateLatestCotFeedback(evalResult.Score); err != nil {
			return err
		}
		logger.InfoCF("reflector", "CoT feedback auto-recorded",
			map[string]any{"score": evalResult.Score, "intent": input.Intent})
	}
	return nil
}

// --- MemoryExtractor (LLM) --------------------------------------------------

type MemoryExtractor struct {
	provider providers.LLMProvider
	model    string
}

func (m *MemoryExtractor) Name() string { return "memory_extractor" }

const memoryExtractPrompt = `Extract important facts worth remembering from this conversation.

User: %s
Assistant (first 800 chars): %s

Respond with ONLY JSON: {"memories": [{"content": "<fact>", "tags": ["tag1"]}]}
Rules: max 3 memories, max 3 tags each, lowercase tags, skip trivial chat.
If nothing worth remembering: {"memories": []}`

type memExtractResult struct {
	Memories []struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	} `json:"memories"`
}

func (m *MemoryExtractor) Process(ctx context.Context, input RuntimeInput, memory *MemoryStore) error {
	if len(input.UserMessage) < 20 || input.Intent == "chat" {
		return nil
	}

	reply := input.AssistantReply
	if len(reply) > 800 {
		reply = reply[:800]
	}

	resp, err := m.provider.Chat(ctx, []providers.Message{
		{Role: "user", Content: fmt.Sprintf(memoryExtractPrompt, input.UserMessage, reply)},
	}, nil, m.model, map[string]any{"max_tokens": 256, "temperature": 0.1})
	if err != nil {
		return fmt.Errorf("memory extract LLM failed: %w", err)
	}

	// Parse JSON (strip markdown fences if present).
	raw := strings.TrimSpace(resp.Content)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) > 2 {
			raw = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var result memExtractResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil // Parsing failed 鈥?skip silently.
	}

	for _, mem := range result.Memories {
		content := strings.TrimSpace(mem.Content)
		if content == "" {
			continue
		}
		tags := make([]string, 0, len(mem.Tags))
		for _, t := range mem.Tags {
			t = strings.ToLower(strings.TrimSpace(t))
			if t != "" {
				tags = append(tags, t)
			}
		}
		if id, err := memory.AddEntry(content, tags); err != nil {
			logger.WarnCF("reflector", "Failed to save memory",
				map[string]any{"error": err.Error()})
		} else {
			logger.InfoCF("reflector", "Memory extracted",
				map[string]any{"id": id, "tags": tags, "content": content})
		}
	}
	return nil
}
