package agent

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mingzhi1/metaclaw/pkg/agent/routing"
	"github.com/mingzhi1/metaclaw/pkg/core/session"
	"github.com/mingzhi1/metaclaw/pkg/infra/config"
	"github.com/mingzhi1/metaclaw/pkg/infra/logger"
	"github.com/mingzhi1/metaclaw/pkg/infra/store"
	"github.com/mingzhi1/metaclaw/pkg/infra/vectorstore"
	"github.com/mingzhi1/metaclaw/pkg/llm/providers"
	"github.com/mingzhi1/metaclaw/pkg/tools"
)

// AgentInstance represents a fully configured agent with its own workspace,
// session manager, context builder, and tool registry.
type AgentInstance struct {
	ID             string
	Name           string
	Model          string
	Fallbacks      []string
	Workspace      string
	MaxIterations  int
	MaxTokens      int
	Temperature    float64
	ContextWindow  int
	Provider       providers.LLMProvider
	Sessions       *session.SessionManager
	ContextBuilder *ContextBuilder
	Tools          *tools.ToolRegistry
	Subagents      *config.SubagentsConfig
	SkillsFilter   []string
	Candidates     []providers.FallbackCandidate
	ThinkingLevel  ThinkingLevel // Extended thinking budget level
	Analyser       *Analyser     // Phase 1: intent/tag analysis
	Reflector      *Reflector    // Phase 3: post-LLM processing + slash commands
	RAGStore       *RAGStore     // Optional RAG store for /rag and knowledge_search
}

// NewAgentInstance creates an agent instance from config.
func NewAgentInstance(
	agentCfg *config.AgentConfig,
	defaults *config.AgentDefaults,
	cfg *config.Config,
	provider providers.LLMProvider,
) *AgentInstance {
	workspace := resolveAgentWorkspace(agentCfg, defaults)
	os.MkdirAll(workspace, 0o755)

	model := resolveAgentModel(agentCfg, defaults)
	fallbacks := resolveAgentFallbacks(agentCfg, defaults)

	restrict := defaults.RestrictToWorkspace
	readRestrict := restrict && !defaults.AllowReadOutsideWorkspace

	// Compile path whitelist patterns from config.
	allowReadPaths := compilePatterns(cfg.Tools.AllowReadPaths)
	allowWritePaths := compilePatterns(cfg.Tools.AllowWritePaths)

	toolsRegistry := tools.NewToolRegistry()
	toolsRegistry.Register(tools.NewReadFileTool(workspace, readRestrict, allowReadPaths))
	toolsRegistry.Register(tools.NewWriteFileTool(workspace, restrict, allowWritePaths))
	toolsRegistry.Register(tools.NewListDirTool(workspace, readRestrict, allowReadPaths))
	execTool, err := tools.NewExecToolWithConfig(workspace, restrict, cfg)
	if err != nil {
		log.Fatalf("Critical error: unable to initialize exec tool: %v", err)
	}
	toolsRegistry.Register(execTool)

	toolsRegistry.Register(tools.NewEditFileTool(workspace, restrict, allowWritePaths))
	toolsRegistry.Register(tools.NewAppendFileTool(workspace, restrict, allowWritePaths))

	db, err := store.Open(workspace)
	if err != nil {
		log.Printf("[WARN] agent: failed to open store for sessions: %v", err)
	}
	sessionsManager := session.NewSessionManager(db)

	contextBuilder := NewContextBuilder(workspace)

	agentID := routing.DefaultAgentID
	agentName := ""
	var subagents *config.SubagentsConfig
	var skillsFilter []string

	if agentCfg != nil {
		agentID = routing.NormalizeAgentID(agentCfg.ID)
		agentName = agentCfg.Name
		subagents = agentCfg.Subagents
		skillsFilter = agentCfg.Skills
	}

	maxIter := defaults.MaxToolIterations
	if maxIter == 0 {
		maxIter = 20
	}

	maxTokens := defaults.MaxTokens
	if maxTokens == 0 {
		maxTokens = 8192
	}

	temperature := 0.7
	if defaults.Temperature != nil {
		temperature = *defaults.Temperature
	}

	// Resolve fallback candidates
	modelCfg := providers.ModelConfig{
		Primary:   model,
		Fallbacks: fallbacks,
	}
	resolveFromModelList := func(raw string) (string, bool) {
		ensureProtocol := func(model string) string {
			model = strings.TrimSpace(model)
			if model == "" {
				return ""
			}
			if strings.Contains(model, "/") {
				return model
			}
			return "openai/" + model
		}

		raw = strings.TrimSpace(raw)
		if raw == "" {
			return "", false
		}

		if cfg != nil {
			if mc, err := cfg.GetModelConfig(raw); err == nil && mc != nil && strings.TrimSpace(mc.Model) != "" {
				return ensureProtocol(mc.Model), true
			}

			for i := range cfg.ModelList {
				fullModel := strings.TrimSpace(cfg.ModelList[i].Model)
				if fullModel == "" {
					continue
				}
				if fullModel == raw {
					return ensureProtocol(fullModel), true
				}
				_, modelID := providers.ExtractProtocol(fullModel)
				if modelID == raw {
					return ensureProtocol(fullModel), true
				}
			}
		}

		return "", false
	}

	candidates := providers.ResolveCandidatesWithLookup(modelCfg, defaults.Provider, resolveFromModelList)

	// Initialise optional Phase 1 analyser + reflector using auxiliary model.
	// Uses GetAuxiliaryModel() which resolves: auxiliary_model → analyser_model → pre_llm_model → primary_model.
	// If auxiliary model differs from primary, try to create its own provider from model_list.
	var analyser *Analyser
	var rt *Reflector
	auxModel := defaults.GetAuxiliaryModel()
	if auxModel != "" {
		auxProvider := provider // default: share primary provider
		auxModelID := auxModel

		// If auxiliary model differs from primary, resolve its own provider.
		if auxModel != model && cfg != nil {
			if mc, err := cfg.GetModelConfig(auxModel); err == nil && mc != nil {
				if ap, mid, err := providers.CreateProviderFromConfig(mc); err == nil {
					auxProvider = ap
					auxModelID = mid
					log.Printf("Auxiliary model uses its own provider (model: %s)", mc.Model)
				} else {
					log.Printf("Warning: failed to create auxiliary provider for %q: %v, falling back to primary provider", auxModel, err)
				}
			}
			// If not in model_list, the raw auxModel string is used as model ID with the primary provider.
		}

		cotRegistry := NewCotRegistry(workspace)
		analyser = NewAnalyser(auxProvider, auxModelID, cotRegistry)
		rt = NewReflector(auxProvider, auxModelID)
		log.Printf("Analyser + Reflector enabled for agent %s (auxiliary_model: %s)", agentID, auxModelID)
	} else {
		// Reflector without LLM processors (just commands + error tracker).
		rt = NewReflector(nil, "")
	}
	rt.SetTools(toolsRegistry)

	ragStore, err := initRAGStore(cfg, workspace)
	if err != nil {
		logger.WarnCF("agent", "Failed to initialize RAG store",
			map[string]any{"agent_id": agentID, "error": err.Error()})
	} else if ragStore != nil {
		rt.SetRAGStore(ragStore)
	}

	return &AgentInstance{
		ID:             agentID,
		Name:           agentName,
		Model:          model,
		Fallbacks:      fallbacks,
		Workspace:      workspace,
		MaxIterations:  maxIter,
		MaxTokens:      maxTokens,
		Temperature:    temperature,
		ContextWindow:  maxTokens,
		Provider:       provider,
		Sessions:       sessionsManager,
		ContextBuilder: contextBuilder,
		Tools:          toolsRegistry,
		Subagents:      subagents,
		SkillsFilter:   skillsFilter,
		Candidates:     candidates,
		ThinkingLevel:  parseThinkingLevel(defaults.ThinkingLevel),
		Analyser:       analyser,
		Reflector:      rt,
		RAGStore:       ragStore,
	}
}

func initRAGStore(cfg *config.Config, workspace string) (*RAGStore, error) {
	if cfg == nil || !cfg.Tools.RAG.Enabled {
		return nil, nil
	}

	modelName := strings.TrimSpace(cfg.Tools.RAG.EmbeddingModel)
	if modelName == "" {
		return nil, fmt.Errorf("tools.rag.embedding_model is required when tools.rag.enabled is true")
	}

	modelCfg, err := cfg.GetModelConfig(modelName)
	if err != nil {
		return nil, fmt.Errorf("resolve embedding model %q: %w", modelName, err)
	}

	embedder, _, err := providers.CreateEmbeddingProviderFromConfig(modelCfg)
	if err != nil {
		return nil, fmt.Errorf("create embedding provider for %q: %w", modelName, err)
	}

	vectors, err := vectorstore.NewSQLiteStore(filepath.Join(workspace, "rag.db"))
	if err != nil {
		return nil, fmt.Errorf("open vector store: %w", err)
	}

	return NewRAGStore(embedder, vectors), nil
}

// resolveAgentWorkspace determines the workspace directory for an agent.
func resolveAgentWorkspace(agentCfg *config.AgentConfig, defaults *config.AgentDefaults) string {
	if agentCfg != nil && strings.TrimSpace(agentCfg.Workspace) != "" {
		return expandHome(strings.TrimSpace(agentCfg.Workspace))
	}
	if agentCfg == nil || agentCfg.Default || agentCfg.ID == "" || routing.NormalizeAgentID(agentCfg.ID) == "main" {
		return expandHome(defaults.Workspace)
	}
	home, _ := os.UserHomeDir()
	id := routing.NormalizeAgentID(agentCfg.ID)
	return filepath.Join(home, ".picoclaw", "workspace-"+id)
}

// resolveAgentModel resolves the primary model for an agent.
func resolveAgentModel(agentCfg *config.AgentConfig, defaults *config.AgentDefaults) string {
	if agentCfg != nil && agentCfg.Model != nil && strings.TrimSpace(agentCfg.Model.Primary) != "" {
		return strings.TrimSpace(agentCfg.Model.Primary)
	}
	return defaults.GetModelName()
}

// resolveAgentFallbacks resolves the fallback models for an agent.
func resolveAgentFallbacks(agentCfg *config.AgentConfig, defaults *config.AgentDefaults) []string {
	if agentCfg != nil && agentCfg.Model != nil && agentCfg.Model.Fallbacks != nil {
		return agentCfg.Model.Fallbacks
	}
	return defaults.ModelFallbacks
}

func compilePatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			fmt.Printf("Warning: invalid path pattern %q: %v\n", p, err)
			continue
		}
		compiled = append(compiled, re)
	}
	return compiled
}

func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		if len(path) > 1 && path[1] == '/' {
			return home + path[1:]
		}
		return home
	}
	return path
}
