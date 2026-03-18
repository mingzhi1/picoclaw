// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sipeed/picoclaw/pkg/core/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/infra/config"
	"github.com/sipeed/picoclaw/pkg/core"
	"github.com/sipeed/picoclaw/pkg/extension"

	"github.com/sipeed/picoclaw/pkg/extension/voice"
	"github.com/sipeed/picoclaw/pkg/infra/kvcache"
	"github.com/sipeed/picoclaw/pkg/infra/logger"
	"github.com/sipeed/picoclaw/pkg/infra/store"
	"github.com/sipeed/picoclaw/pkg/llm/mcp"
	"github.com/sipeed/picoclaw/pkg/infra/media"
	"github.com/sipeed/picoclaw/pkg/llm/providers"
	"github.com/sipeed/picoclaw/pkg/agent/routing"
	"github.com/sipeed/picoclaw/pkg/agent/topic"
	"github.com/sipeed/picoclaw/pkg/skills"
	"github.com/sipeed/picoclaw/pkg/core/state"
	"github.com/sipeed/picoclaw/pkg/tools"
	"github.com/sipeed/picoclaw/pkg/infra/utils"
)

type AgentLoop struct {
	bus            *bus.MessageBus
	cfg            *config.Config
	registry       *AgentRegistry
	state          *state.Manager
	running        atomic.Bool
	msgSeqId       atomic.Uint64
	summarizing    sync.Map
	fallback       *providers.FallbackChain
	channelManager *channels.Manager
	mediaStore     media.MediaStore
	// Phase 3 infrastructure (M1-M4)
	turnStore      *TurnStore             // per-workspace turns.db
	activeCtx      *ActiveContextStore    // per channel:chatID context
	memoryDigest   *MemoryDigestWorker    // background memory distillation
	cache          *kvcache.Store         // persistent KV cache (workspace/cache.db)
	extensions     *extension.Manager     // optional extension modules (devices, media, voice)
	topicTracker   *topic.Tracker         // topic lifecycle management for long conversations
	factStore      *FactStore             // entity-attribute-value facts with versioning
	// Concurrency control
	msgSem         chan struct{}           // limits concurrent message processing
	sessionLocks   sync.Map               // per-session mutex to serialize same-session messages
}

// processOptions configures how a message is processed
type processOptions struct {
	SessionKey      string   // Session identifier for history/context
	Channel         string   // Target channel for tool execution
	ChatID          string   // Target chat ID for tool execution
	UserMessage     string   // User message content (may include prefix)
	DefaultResponse string   // Response when LLM returns empty
	EnableSummary   bool     // Whether to trigger summarization
	SendResponse    bool     // Whether to send response via bus
	NoHistory       bool     // If true, don't load session history (for heartbeat)
	MsgSeqId        uint64   // Global message sequence number
	Intent          string   // Phase 1 intent (chat/task/question)
	Tags            []string // Phase 1 tags 鈥?used for memory retrieval
	ToolHints       []string // Phase 1 tool_hints 鈥?categories of tools to include (e.g. ["file","web"])
}

const defaultResponse = "I've completed processing but have no response to give. Increase `max_tool_iterations` in config.json."

func NewAgentLoop(
	cfg *config.Config,
	msgBus *bus.MessageBus,
	provider providers.LLMProvider,
) *AgentLoop {
	registry := NewAgentRegistry(cfg, provider)

	// Register shared tools to all agents
	registerSharedTools(cfg, msgBus, registry, provider)

	// Set up shared fallback chain
	cooldown := providers.NewCooldownTracker()
	fallbackChain := providers.NewFallbackChain(cooldown)

	// Create state manager using default agent's workspace for channel recording
	defaultAgent := registry.GetDefaultAgent()
	var stateManager *state.Manager
	if defaultAgent != nil {
		stateDB, err := store.Open(defaultAgent.Workspace)
		if err != nil {
			logger.ErrorCF("agent", "Failed to open store for state", map[string]any{"error": err.Error()})
		} else {
			stateManager = state.NewManager(stateDB)
		}
	}

	// Initialise Phase 3 infrastructure.
	var activeCtxDB *sql.DB
	if defaultAgent != nil {
		activeCtxDB, _ = store.Open(defaultAgent.Workspace)
	}
	activeCtxStore := NewActiveContextStore(activeCtxDB)
	var ts *TurnStore
	var digestWorker *MemoryDigestWorker
	if defaultAgent != nil {
		var tsErr error
		ts, tsErr = NewTurnStore(defaultAgent.Workspace)
		if tsErr != nil {
			logger.ErrorCF("agent", "Failed to create TurnStore", map[string]any{"error": tsErr.Error()})
		} else {
			mem := defaultAgent.ContextBuilder.GetMemory()
			digestModel := cfg.Agents.Defaults.GetDigestModel()
			digestProvider := provider
			digestModelID := digestModel

			// If digest model differs from primary, resolve its own provider.
			primaryModel := cfg.Agents.Defaults.GetPrimaryModel()
			if digestModel != primaryModel {
				if mc, err := cfg.GetModelConfig(digestModel); err == nil && mc != nil {
					if dp, mid, err := providers.CreateProviderFromConfig(mc); err == nil {
						digestProvider = dp
						digestModelID = mid
					}
				}
			}

			digestWorker = NewMemoryDigestWorker(ts, mem, digestProvider, digestModelID)
		}
	}

	// Topic Tracker: DB-backed topic lifecycle management for long conversations.
	var topicTracker *topic.Tracker
	if defaultAgent != nil {
		topicDBPath := filepath.Join(defaultAgent.Workspace, "turns.db")
		topicStore, err := topic.NewStore(topicDBPath)
		if err != nil {
			logger.WarnCF("agent", "Failed to create TopicStore", map[string]any{"error": err.Error()})
		} else {
			topicTracker, err = topic.NewTracker(topicStore)
			if err != nil {
				logger.WarnCF("agent", "Failed to create TopicTracker", map[string]any{"error": err.Error()})
				topicStore.Close()
			}
		}
	}

	// Fact Store: structured entity-attribute-value facts with versioning.
	// Shares the same turns.db file (separate table).
	var factStoreInstance *FactStore
	if defaultAgent != nil {
		factDB, err := store.Open(defaultAgent.Workspace)
		if err != nil {
			logger.WarnCF("agent", "Failed to open DB for FactStore", map[string]any{"error": err.Error()})
		} else {
			factStoreInstance, err = NewFactStore(factDB)
			if err != nil {
				logger.WarnCF("agent", "Failed to create FactStore", map[string]any{"error": err.Error()})
			}
		}
	}
	// Wire FactStore to DigestWorker so extracted facts are persisted.
	if digestWorker != nil && factStoreInstance != nil {
		digestWorker.SetFactStore(factStoreInstance)
	}

	// Persistent KV cache for general-purpose caching (web results, analysis, etc.)
	var kvCache *kvcache.Store
	if defaultAgent != nil {
		var cacheErr error
		kvCache, cacheErr = kvcache.New(filepath.Join(defaultAgent.Workspace, "cache.db"))
		if cacheErr != nil {
			logger.WarnCF("agent", "Failed to open KV cache", map[string]any{"error": cacheErr.Error()})
		}
	}

	// Extension manager: register optional modules based on config.
	extMgr := extension.NewManager()

	extMgr.Register(voice.New())

	// Init extensions 鈥?each gets its own config map.
	if defaultAgent != nil {
		sttCfg := resolveSttConfig(cfg)


		voiceCtx := extension.ExtensionContext{
			Workspace: defaultAgent.Workspace,
			Config:    sttCfg,
		}
		extCtxByName := map[string]extension.ExtensionContext{
			"voice":   voiceCtx,
		}

		for _, ext := range extMgr.List() {
			ctx, ok := extCtxByName[ext]
			if !ok {
				ctx = extension.ExtensionContext{Workspace: defaultAgent.Workspace, Config: map[string]any{}}
			}
			if err := extMgr.InitOne(ext, ctx); err != nil {
				logger.WarnCF("agent", "Extension init error", map[string]any{"name": ext, "error": err.Error()})
			}
		}

		for _, tool := range extMgr.CollectTools() {
			for _, aid := range registry.ListAgentIDs() {
				if a, ok := registry.GetAgent(aid); ok {
					a.Tools.Register(tool)
				}
			}
		}
	}

	return &AgentLoop{
		bus:            msgBus,
		cfg:            cfg,
		registry:       registry,
		state:          stateManager,
		summarizing:    sync.Map{},
		fallback:       fallbackChain,
		turnStore:      ts,
		activeCtx:      activeCtxStore,
		memoryDigest:   digestWorker,
		cache:          kvCache,
		extensions:     extMgr,
		topicTracker:   topicTracker,
		factStore:      factStoreInstance,
		msgSem:         make(chan struct{}, 4), // max 4 concurrent message handlers
	}
}

// Close releases resources held by the AgentLoop (database connections, etc.).
// Call this in tests or on graceful shutdown to avoid file-lock errors on Windows.
func (al *AgentLoop) Close() {
	if al.cache != nil {
		al.cache.Close()
	}
	if al.turnStore != nil {
		al.turnStore.Close()
	}
	if al.factStore != nil {
		al.factStore.Close()
	}
	// Close each agent's MemoryStore to release SQLite file locks.
	if al.registry != nil {
		for _, id := range al.registry.ListAgentIDs() {
			if agent, ok := al.registry.GetAgent(id); ok && agent.ContextBuilder != nil {
				if mem := agent.ContextBuilder.GetMemory(); mem != nil {
					mem.Close()
				}
			}
		}
	}
}

// registerSharedTools registers tools that are shared across all agents (web, message, spawn).
func registerSharedTools(
	cfg *config.Config,
	msgBus *bus.MessageBus,
	registry *AgentRegistry,
	provider providers.LLMProvider,
) {
	for _, agentID := range registry.ListAgentIDs() {
		agent, ok := registry.GetAgent(agentID)
		if !ok {
			continue
		}

		// Web tools
		searchTool, err := tools.NewWebSearchTool(tools.WebSearchToolOptions{
			BraveAPIKey:          cfg.Tools.Web.Brave.APIKey,
			BraveMaxResults:      cfg.Tools.Web.Brave.MaxResults,
			BraveEnabled:         cfg.Tools.Web.Brave.Enabled,
			TavilyAPIKey:         cfg.Tools.Web.Tavily.APIKey,
			TavilyBaseURL:        cfg.Tools.Web.Tavily.BaseURL,
			TavilyMaxResults:     cfg.Tools.Web.Tavily.MaxResults,
			TavilyEnabled:        cfg.Tools.Web.Tavily.Enabled,
			DuckDuckGoMaxResults: cfg.Tools.Web.DuckDuckGo.MaxResults,
			DuckDuckGoEnabled:    cfg.Tools.Web.DuckDuckGo.Enabled,
			PerplexityAPIKey:     cfg.Tools.Web.Perplexity.APIKey,
			PerplexityMaxResults: cfg.Tools.Web.Perplexity.MaxResults,
			PerplexityEnabled:    cfg.Tools.Web.Perplexity.Enabled,
			Proxy:                cfg.Tools.Web.Proxy,
		})
		if err != nil {
			logger.ErrorCF("agent", "Failed to create web search tool", map[string]any{"error": err.Error()})
		} else if searchTool != nil {
			agent.Tools.Register(searchTool)
		}
		fetchTool, err := tools.NewWebFetchToolWithProxy(50000, cfg.Tools.Web.Proxy, cfg.Tools.Web.FetchLimitBytes)
		if err != nil {
			logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
		} else {
			agent.Tools.Register(fetchTool)
		}



		// Message tool
		messageTool := tools.NewMessageTool()
		messageTool.SetSendCallback(func(channel, chatID, content string) error {
			pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer pubCancel()
			return msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
				Channel: channel,
				ChatID:  chatID,
				Content: content,
			})
		})
		agent.Tools.Register(messageTool)

		// Skill discovery and installation tools
		registryMgr := skills.NewRegistryManagerFromConfig(skills.RegistryConfig{
			MaxConcurrentSearches: cfg.Tools.Skills.MaxConcurrentSearches,
			ClawHub:               skills.ClawHubConfig(cfg.Tools.Skills.Registries.ClawHub),
		})
		searchCache := skills.NewSearchCache(
			cfg.Tools.Skills.SearchCache.MaxSize,
			time.Duration(cfg.Tools.Skills.SearchCache.TTLSeconds)*time.Second,
		)
		agent.Tools.Register(tools.NewFindSkillsTool(registryMgr, searchCache))
		agent.Tools.Register(tools.NewInstallSkillTool(registryMgr, agent.Workspace))

		// Spawn tool with allowlist checker
		subagentManager := tools.NewSubagentManager(provider, agent.Model, agent.Workspace, msgBus)
		subagentManager.SetLLMOptions(agent.MaxTokens, agent.Temperature)
		spawnTool := tools.NewSpawnTool(subagentManager)
		currentAgentID := agentID
		spawnTool.SetAllowlistChecker(func(targetAgentID string) bool {
			return registry.CanSpawnSubagent(currentAgentID, targetAgentID)
		})
		agent.Tools.Register(spawnTool)
	}
}

func (al *AgentLoop) Run(ctx context.Context) error {
	al.running.Store(true)

	// Active context is loaded from SQLite in NewActiveContextStore — no separate Load needed.

	// Start MemoryDigest background worker.
	if al.memoryDigest != nil {
		al.memoryDigest.Start(ctx)
	}

	// Start extensions (devices, media, voice, etc.)
	if al.extensions != nil {
		if err := al.extensions.StartAll(ctx); err != nil {
			logger.WarnCF("agent", "Extension start error", map[string]any{"error": err.Error()})
		}
	}

	// Initialize MCP servers for all agents
	if al.cfg.Tools.MCP.Enabled {
		mcpManager := mcp.NewManager()
		defaultAgent := al.registry.GetDefaultAgent()
		var workspacePath string
		if defaultAgent != nil && defaultAgent.Workspace != "" {
			workspacePath = defaultAgent.Workspace
		} else {
			workspacePath = al.cfg.WorkspacePath()
		}

		if err := mcpManager.LoadFromMCPConfig(ctx, al.cfg.Tools.MCP, workspacePath); err != nil {
			logger.WarnCF("agent", "Failed to load MCP servers, MCP tools will not be available",
				map[string]any{
					"error": err.Error(),
				})
		} else {
			// Ensure MCP connections are cleaned up on exit, only if initialization succeeded
			defer func() {
				if err := mcpManager.Close(); err != nil {
					logger.ErrorCF("agent", "Failed to close MCP manager",
						map[string]any{
							"error": err.Error(),
						})
				}
			}()

			// Register MCP tools for all agents
			servers := mcpManager.GetServers()
			uniqueTools := 0
			totalRegistrations := 0
			agentIDs := al.registry.ListAgentIDs()
			agentCount := len(agentIDs)

			for serverName, conn := range servers {
				uniqueTools += len(conn.Tools)
				for _, tool := range conn.Tools {
					for _, agentID := range agentIDs {
						agent, ok := al.registry.GetAgent(agentID)
						if !ok {
							continue
						}
						mcpTool := tools.NewMCPTool(mcpManager, serverName, tool)
						agent.Tools.Register(mcpTool)
						totalRegistrations++
						logger.DebugCF("agent", "Registered MCP tool",
							map[string]any{
								"agent_id": agentID,
								"server":   serverName,
								"tool":     tool.Name,
								"name":     mcpTool.Name(),
							})
					}
				}
			}
			logger.InfoCF("agent", "MCP tools registered successfully",
				map[string]any{
					"server_count":        len(servers),
					"unique_tools":        uniqueTools,
					"total_registrations": totalRegistrations,
					"agent_count":         agentCount,
				})
		}
	}

	for al.running.Load() {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg, ok := al.bus.ConsumeInbound(ctx)
			if !ok {
				continue
			}

			// Acquire concurrency semaphore (blocks if maxConcurrent goroutines active).
			al.msgSem <- struct{}{}

			// Process message in a goroutine so other channels aren't blocked.
			go func(m bus.InboundMessage) {
				defer func() { <-al.msgSem }() // release semaphore

				// Per-session lock: messages within the same session must run serially
				// to maintain conversation coherence.
				sessionKey := m.SessionKey
				if sessionKey == "" {
					sessionKey = fmt.Sprintf("%s:%s", m.Channel, m.ChatID)
				}
				muI, _ := al.sessionLocks.LoadOrStore(sessionKey, &sync.Mutex{})
				mu := muI.(*sync.Mutex)
				mu.Lock()
				defer mu.Unlock()

				response, err := al.processMessage(ctx, m)
				if err != nil {
					response = fmt.Sprintf("Error processing message: %v", err)
				}

				if response != "" {
					// Check if ANY agent's message tool already sent a response during this round.
					alreadySent := false
					for _, agentID := range al.registry.ListAgentIDs() {
						if a, ok := al.registry.GetAgent(agentID); ok {
							if tool, ok := a.Tools.Get("message"); ok {
								if mt, ok := tool.(*tools.MessageTool); ok {
									if mt.HasSentInRound() {
										alreadySent = true
										break
									}
								}
							}
						}
					}

					if !alreadySent {
						al.bus.PublishOutbound(ctx, bus.OutboundMessage{
							Channel: m.Channel,
							ChatID:  m.ChatID,
							Content: response,
						})
						logger.InfoCF("agent", "Published outbound response",
							map[string]any{
								"channel":     m.Channel,
								"chat_id":     m.ChatID,
								"content_len": len(response),
							})
					} else {
						logger.DebugCF(
							"agent",
							"Skipped outbound (message tool already sent)",
							map[string]any{"channel": m.Channel},
						)
					}
				}
			}(msg)
		}
	}

	return nil
}

func (al *AgentLoop) Stop() {
	al.running.Store(false)
	// Active context is persisted per-update to SQLite — no bulk flush needed.
	// Close TurnStore.
	if al.turnStore != nil {
		if err := al.turnStore.Close(); err != nil {
			logger.WarnCF("agent", "Failed to close turn store", map[string]any{"error": err.Error()})
		}
	}
	// Close KV cache.
	if al.cache != nil {
		if err := al.cache.Close(); err != nil {
			logger.WarnCF("agent", "Failed to close KV cache", map[string]any{"error": err.Error()})
		}
	}
	// Stop extensions (reverse order).
	if al.extensions != nil {
		al.extensions.StopAll()
	}
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Add message preview to log (show full content for error messages)
	var logContent string
	if strings.Contains(msg.Content, "Error:") || strings.Contains(msg.Content, "error") {
		logContent = msg.Content // Full content for errors
	} else {
		logContent = utils.Truncate(msg.Content, 80)
	}
	logger.InfoCF(
		"agent",
		fmt.Sprintf("Processing message from %s:%s: %s", msg.Channel, msg.SenderID, logContent),
		map[string]any{
			"channel":     msg.Channel,
			"chat_id":     msg.ChatID,
			"sender_id":   msg.SenderID,
			"session_key": msg.SessionKey,
		},
	)

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}

	// Check for runtime commands (e.g. /memory, /cot, /runtime, /show, /list, /switch).
	if agent := al.registry.GetDefaultAgent(); agent != nil && agent.Reflector != nil {
		if response, handled := agent.Reflector.HandleCommand(msg.Content, agent.ContextBuilder.GetMemory()); handled {
			return response, nil
		}
	}

	// Route to determine agent and session key
	route := al.registry.ResolveRoute(routing.RouteInput{
		Channel:    msg.Channel,
		AccountID:  msg.Metadata["account_id"],
		Peer:       extractPeer(msg),
		ParentPeer: extractParentPeer(msg),
		GuildID:    msg.Metadata["guild_id"],
		TeamID:     msg.Metadata["team_id"],
	})

	agent, ok := al.registry.GetAgent(route.AgentID)
	if !ok {
		agent = al.registry.GetDefaultAgent()
	}
	if agent == nil {
		return "", fmt.Errorf("no agent available for route (agent_id=%s)", route.AgentID)
	}

	// Reset message-tool state for this round so we don't skip publishing due to a previous round.
	if tool, ok := agent.Tools.Get("message"); ok {
		if mt, ok := tool.(tools.ContextualTool); ok {
			mt.SetContext(msg.Channel, msg.ChatID)
		}
	}

	// Use routed session key, but honor pre-set agent-scoped keys (for ProcessDirect/cron)
	sessionKey := route.SessionKey
	if msg.SessionKey != "" && strings.HasPrefix(msg.SessionKey, "agent:") {
		sessionKey = msg.SessionKey
	}

	logger.InfoCF("agent", "Routed message",
		map[string]any{
			"agent_id":    agent.ID,
			"session_key": sessionKey,
			"matched_by":  route.MatchedBy,
		})

	return al.runAgentLoop(ctx, agent, processOptions{
		SessionKey:      sessionKey,
		Channel:         msg.Channel,
		ChatID:          msg.ChatID,
		UserMessage:     msg.Content,
		DefaultResponse: defaultResponse,
		EnableSummary:   true,
		SendResponse:    false,
	})
}

func (al *AgentLoop) processSystemMessage(
	ctx context.Context,
	msg bus.InboundMessage,
) (string, error) {
	if msg.Channel != "system" {
		return "", fmt.Errorf(
			"processSystemMessage called with non-system message channel: %s",
			msg.Channel,
		)
	}

	logger.InfoCF("agent", "Processing system message",
		map[string]any{
			"sender_id": msg.SenderID,
			"chat_id":   msg.ChatID,
		})

	// Parse origin channel from chat_id (format: "channel:chat_id")
	var originChannel, originChatID string
	if idx := strings.Index(msg.ChatID, ":"); idx > 0 {
		originChannel = msg.ChatID[:idx]
		originChatID = msg.ChatID[idx+1:]
	} else {
		originChannel = "cli"
		originChatID = msg.ChatID
	}

	// Extract subagent result from message content
	// Format: "Task 'label' completed.\n\nResult:\n<actual content>"
	content := msg.Content
	if idx := strings.Index(content, "Result:\n"); idx >= 0 {
		content = content[idx+8:] // Extract just the result part
	}

	// Skip internal channels - only log, don't send to user
	if core.IsInternalChannel(originChannel) {
		logger.InfoCF("agent", "Subagent completed (internal channel)",
			map[string]any{
				"sender_id":   msg.SenderID,
				"content_len": len(content),
				"channel":     originChannel,
			})
		return "", nil
	}

	// Use default agent for system messages
	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		return "", fmt.Errorf("no default agent for system message")
	}

	// Use the origin session for context
	sessionKey := routing.BuildAgentMainSessionKey(agent.ID)

	return al.runAgentLoop(ctx, agent, processOptions{
		SessionKey:      sessionKey,
		Channel:         originChannel,
		ChatID:          originChatID,
		UserMessage:     fmt.Sprintf("[System: %s] %s", msg.SenderID, msg.Content),
		DefaultResponse: "Background task completed.",
		EnableSummary:   false,
		SendResponse:    true,
	})
}

// runAgentLoop is the core message processing logic.
func (al *AgentLoop) runAgentLoop(
	ctx context.Context,
	agent *AgentInstance,
	opts processOptions,
) (string, error) {
	seq := al.msgSeqId.Add(1)
	opts.MsgSeqId = seq

	// 0. Record last channel for heartbeat notifications (skip internal channels)
	if opts.Channel != "" && opts.ChatID != "" {
		// Don't record internal channels (cli, system, subagent)
		if !core.IsInternalChannel(opts.Channel) {
			channelKey := fmt.Sprintf("%s:%s", opts.Channel, opts.ChatID)
			if err := al.RecordLastChannel(channelKey); err != nil {
				logger.WarnCF(
					"agent",
					"Failed to record last channel",
					map[string]any{"error": err.Error()},
				)
			}
		}
	}

	// 1. Update tool contexts
	al.updateToolContexts(agent, opts.Channel, opts.ChatID)

	// 2. Analyse intent + build Phase 2 messages.
	//
	// Two paths:
	//   A. Instant Memory (when Analyser + TurnStore are ready):
	//      - Phase 1 analyses intent/tags
	//      - BuildInstantMemory selects relevant historical turns from TurnStore
	//      - BuildPhase2Messages assembles KV cache friendly message array
	//   B. Legacy SessionManager (fallback):
	//      - Uses Session history directly via ContextBuilder.BuildMessages
	//
	var analyseResult AnalyseResult
	var messages []providers.Message
	channelKey := fmt.Sprintf("%s:%s", opts.Channel, opts.ChatID)

	useInstantMemory := agent.Analyser != nil && al.turnStore != nil && !opts.NoHistory && opts.UserMessage != ""

	if useInstantMemory {
		// --- Path A: Instant Memory ---

		// Phase 1: analyse intent + tags.
		var actCtx *ActiveContext
		if al.activeCtx != nil {
			actCtx = al.activeCtx.Get(channelKey)
		}
		// Inject topic context into Analyser prompt for topic-aware routing.
		var topicContext string
		if al.topicTracker != nil {
			topicContext = al.topicTracker.FormatForAnalyser()
		}
		analyseResult = agent.Analyser.Analyse(ctx, opts.UserMessage, agent.ContextBuilder.GetMemory(), actCtx, topicContext)

		// Apply topic action from Analyser to TopicTracker.
		if al.topicTracker != nil {
			ta := analyseResult.TopicAction
			action := topic.Action{Type: topic.ActionContinue}
			if ta != nil {
				switch ta.Action {
				case "new":
					action = topic.Action{Type: topic.ActionNew, Title: ta.Title, Resolve: ta.Resolve}
				case "resolve":
					action = topic.Action{Type: topic.ActionResolve, Primary: ta.ID, Resolve: ta.Resolve}
				default: // "continue" or unknown
					action = topic.Action{Type: topic.ActionContinue, Primary: ta.ID, Resolve: ta.Resolve}
				}
			}
			if tp, err := al.topicTracker.Apply(action); err != nil {
				logger.WarnCF("agent", "Topic apply failed", map[string]any{"error": err.Error()})
			} else if tp != nil {
				logger.DebugCF("agent", "Topic active",
					map[string]any{"id": tp.ID, "title": tp.Title, "status": string(tp.Status)})
			}
		}

		// Build system prompt from ContextBuilder (cached static + dynamic context).
		staticPrompt := agent.ContextBuilder.BuildSystemPromptWithCache()
		dynamicCtx := agent.ContextBuilder.buildDynamicContext(opts.Channel, opts.ChatID)
		systemPrompt := staticPrompt + "\n\n---\n\n" + dynamicCtx

		// Inject active facts (global + topic-scoped) into system prompt.
		// Position: after static prompt, before CoT — stable, good cache hit.
		if al.factStore != nil {
			var currentTopicID string
			if al.topicTracker != nil {
				if cur := al.topicTracker.Current(); cur != nil {
					currentTopicID = cur.ID
				}
			}
			if factsCtx := al.factStore.FormatForContext(currentTopicID); factsCtx != "" {
				systemPrompt += "\n\n---\n\n" + factsCtx
			}
		}

		// Enrich system prompt with CoT 鈥?template + task-specific supplement.
		if analyseResult.CotID != "" && agent.Analyser != nil {
			if reg := agent.Analyser.GetCotRegistry(); reg != nil {
				tpl := reg.Get(analyseResult.CotID)
				if tpl.Prompt != "" {
					systemPrompt += "\n\n---\n\n" + tpl.Prompt
				}
			}
			if analyseResult.CotPrompt != "" {
				systemPrompt += "\n\n### Task-Specific Notes\n\n" + analyseResult.CotPrompt
			}
		} else if analyseResult.CotPrompt != "" {
			// Legacy fallback: raw CoT prompt (no template selected).
			systemPrompt += "\n\n---\n\n## Thinking Strategy\n\n" + analyseResult.CotPrompt
		}

		// Match skill by keywords and inject tool execution plan (no LLM needed).
		if matched := agent.ContextBuilder.MatchSkillByMessage(opts.UserMessage); matched != nil {
			if plan := FormatToolSteps(matched.ToolSteps, matched.Path); plan != "" {
				systemPrompt += "\n\n---\n\n" + plan
				logger.InfoCF("agent", "Injected tool execution plan from skill",
					map[string]any{
						"skill":      matched.Name,
						"steps":      len(matched.ToolSteps),
						"skill_path": matched.Path,
					})
			}
		}
		// Select relevant turns from TurnStore.
		cfg := DefaultInstantMemoryCfg(agent.ContextWindow)
		instantTurns := BuildInstantMemory(al.turnStore, analyseResult.Tags, channelKey, cfg)

		// Get long-term memory by tags.
		longTermMemory := analyseResult.MemoryContext

		// Assemble Phase 2 messages in KV cache friendly order.
		messages = BuildPhase2Messages(
			systemPrompt,
			longTermMemory,
			instantTurns,
			opts.UserMessage,
			cfg.HighScoreThreshold,
		)

		logger.InfoCF("agent", "Phase 2 messages built via instant memory",
			map[string]any{
				"seq":            seq,
				"agent_id":       agent.ID,
				"intent":         analyseResult.Intent,
				"tags":           analyseResult.Tags,
				"instant_turns":  len(instantTurns),
				"total_messages": len(messages),
				"has_cot":        analyseResult.CotPrompt != "",
				"has_memories":   longTermMemory != "",
			})
	} else {
		// --- Path B: Legacy SessionManager ---
		var history []providers.Message
		var summary string
		if !opts.NoHistory {
			history = agent.Sessions.GetHistory(opts.SessionKey)
			summary = agent.Sessions.GetSummary(opts.SessionKey)
		}
		messages = agent.ContextBuilder.BuildMessages(
			history,
			summary,
			opts.UserMessage,
			nil,
			opts.Channel,
			opts.ChatID,
		)

		// Optional Phase 1 enrichment (when Analyser exists but TurnStore not ready).
		if agent.Analyser != nil && !opts.NoHistory && opts.UserMessage != "" {
			var actCtx *ActiveContext
			if al.activeCtx != nil {
				actCtx = al.activeCtx.Get(channelKey)
			}
			analyseResult = agent.Analyser.Analyse(ctx, opts.UserMessage, agent.ContextBuilder.GetMemory(), actCtx, "")

			var enrichment strings.Builder
			// CoT injection: template + task-specific supplement (hybrid mode).
			if analyseResult.CotID != "" {
				if reg := agent.Analyser.GetCotRegistry(); reg != nil {
					tpl := reg.Get(analyseResult.CotID)
					if tpl.Prompt != "" {
						enrichment.WriteString("\n\n---\n\n")
						enrichment.WriteString(tpl.Prompt)
					}
				}
				if analyseResult.CotPrompt != "" {
					enrichment.WriteString("\n\n### Task-Specific Notes\n\n")
					enrichment.WriteString(analyseResult.CotPrompt)
				}
			} else if analyseResult.CotPrompt != "" {
				// Legacy fallback: raw CoT prompt (no template selected).
				enrichment.WriteString("\n\n---\n\n## Thinking Strategy\n\n")
				enrichment.WriteString(analyseResult.CotPrompt)
			}
			if analyseResult.MemoryContext != "" {
				enrichment.WriteString("\n\n---\n\n# Contextual Memories (pre-analysed)\n\n")
				enrichment.WriteString(analyseResult.MemoryContext)
			}

			// Skill matching 鈥?inject tool execution plan (same as Path A).
			if matched := agent.ContextBuilder.MatchSkillByMessage(opts.UserMessage); matched != nil {
				if plan := FormatToolSteps(matched.ToolSteps, matched.Path); plan != "" {
					enrichment.WriteString("\n\n---\n\n")
					enrichment.WriteString(plan)
					logger.InfoCF("agent", "Injected tool execution plan from skill (legacy path)",
						map[string]any{
							"skill":      matched.Name,
							"steps":      len(matched.ToolSteps),
							"skill_path": matched.Path,
						})
				}
			}

			if enrichment.Len() > 0 && len(messages) > 0 && messages[0].Role == "system" {
				messages[0].Content += enrichment.String()
				if len(messages[0].SystemParts) > 0 {
					enrichBlock := providers.ContentBlock{
						Type: "text",
						Text: enrichment.String(),
					}
					messages[0].SystemParts = append(messages[0].SystemParts, enrichBlock)
				}
				logger.InfoCF("agent", "Pre-LLM enriched context (legacy path)",
					map[string]any{
						"seq":          seq,
						"agent_id":     agent.ID,
						"intent":       analyseResult.Intent,
						"tags":         analyseResult.Tags,
						"has_memories": analyseResult.MemoryContext != "",
						"has_cot":      analyseResult.CotPrompt != "",
					})
			}
		}
	}

	// 3. Save user message to session.
	// In Instant Memory mode, session is only kept for debug commands (/show);
	// no summarization needed since TurnStore handles context selection.
	agent.Sessions.AddMessage(opts.SessionKey, "user", opts.UserMessage)

	// Propagate Phase 1 analysis results to executor for intent-based tool filtering.
	opts.Intent = analyseResult.Intent
	opts.Tags = analyseResult.Tags
	opts.ToolHints = analyseResult.ToolHints

	// 4. Run LLM iteration loop
	finalContent, iteration, toolRecords, totalTokens, err := al.runLLMIteration(ctx, agent, messages, opts)
	if err != nil {
		return "", err
	}

	// If last tool had ForUser content and we already sent it, we might not need to send final response
	// This is controlled by the tool's Silent flag and ForUser content

	// 5. Handle empty response
	if finalContent == "" {
		finalContent = opts.DefaultResponse
	}

	// 6. Save final assistant message to session
	agent.Sessions.AddMessage(opts.SessionKey, "assistant", finalContent)
	agent.Sessions.Save(opts.SessionKey)

	// Build the runtime input used by Phase 3 stages.
	runtimeInput := RuntimeInput{
		UserMessage:    opts.UserMessage,
		AssistantReply: finalContent,
		Intent:         analyseResult.Intent,
		Tags:           analyseResult.Tags,
		CotPrompt:      analyseResult.CotPrompt,
		ToolCalls:      toolRecords,
		Iterations:     iteration,
		ChannelKey:     channelKey,
		TotalTokens:    totalTokens,
	}

	// 6.5. Phase 3 鈥?Synchronous part (< 2ms): score + Active Context update.
	// MUST run before PublishOutbound so the next turn's Phase 1 sees fresh context.
	if agent.Reflector != nil && opts.UserMessage != "" {
		score := agent.Reflector.SyncPhase3(runtimeInput)
		runtimeInput.Score = score

		// Update Active Context for this channel.
		if al.activeCtx != nil {
			al.activeCtx.Update(channelKey, runtimeInput)
		}
	}

	// 7. Optional: summarization 鈥?ONLY for legacy path.
	// Instant Memory mode uses TurnStore for context selection, so session
	// summarization is unnecessary and would waste LLM calls.
	if opts.EnableSummary && !useInstantMemory {
		al.maybeSummarize(agent, opts.SessionKey, opts.Channel, opts.ChatID)
	}

	// 8. Optional: send response via bus (user receives reply here).
	if opts.SendResponse {
		al.bus.PublishOutbound(ctx, bus.OutboundMessage{
			Channel: opts.Channel,
			ChatID:  opts.ChatID,
			Content: finalContent,
		})
	}

	// 6.6. Phase 3 鈥?Async part: persist TurnRecord, run processors.
	// Runs AFTER PublishOutbound to not delay the user response.
	if agent.Reflector != nil && opts.UserMessage != "" {
		agent.Reflector.AsyncPhase3(runtimeInput, agent.ContextBuilder.GetMemory(), al.turnStore, al.activeCtx)
	}

	// 6.7. Topic token tracking — rough estimate for compaction decisions.
	if al.topicTracker != nil && opts.UserMessage != "" {
		turnTokens := (len(opts.UserMessage) + len(finalContent)) / 4 // ~4 chars/token
		_ = al.topicTracker.RecordTurnTokens(turnTokens)
	}

	// 9. Log response
	responsePreview := utils.Truncate(finalContent, 120)
	logger.InfoCF("agent", fmt.Sprintf("Response: %s", responsePreview),
		map[string]any{
			"seq":          seq,
			"agent_id":     agent.ID,
			"session_key":  opts.SessionKey,
			"iterations":   iteration,
			"final_length": len(finalContent),
		})

	return finalContent, nil
}
