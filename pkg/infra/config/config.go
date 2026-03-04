package config

import (
	"encoding/json"
	"fmt"
)

// --- Core config struct and agent types ---

// FlexibleStringSlice is a []string that also accepts JSON numbers,
// so allow_from can contain both "123" and 123.
type FlexibleStringSlice []string

func (f *FlexibleStringSlice) UnmarshalJSON(data []byte) error {
	// Try []string first
	var ss []string
	if err := json.Unmarshal(data, &ss); err == nil {
		*f = ss
		return nil
	}

	// Try []interface{} to handle mixed types
	var raw []any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	result := make([]string, 0, len(raw))
	for _, v := range raw {
		switch val := v.(type) {
		case string:
			result = append(result, val)
		case float64:
			result = append(result, fmt.Sprintf("%.0f", val))
		default:
			result = append(result, fmt.Sprintf("%v", val))
		}
	}
	*f = result
	return nil
}

// Config is the top-level configuration struct for PicoClaw.
type Config struct {
	Proxy     string          `json:"proxy,omitempty"    env:"PICOCLAW_PROXY"` // Global HTTP proxy (overridden by per-model/per-tool proxy)
	Agents    AgentsConfig    `json:"agents"`
	Bindings  []AgentBinding  `json:"bindings,omitempty"`
	Session   SessionConfig   `json:"session,omitempty"`
	Channels  ChannelsConfig  `json:"channels,omitempty"`
	Providers ProvidersConfig `json:"providers,omitempty"`
	ModelList []ModelConfig   `json:"model_list,omitempty"`
	Gateway   GatewayConfig   `json:"gateway,omitempty"`
	Tools     ToolsConfig     `json:"tools,omitempty"`
	Heartbeat HeartbeatConfig `json:"heartbeat,omitempty"`
	Devices   DevicesConfig   `json:"devices,omitempty"`
	Logging   LoggingConfig   `json:"logging,omitempty"`
}

// MarshalJSON implements custom JSON marshaling for Config
// to omit empty/default sections and keep the saved file minimal.
func (c Config) MarshalJSON() ([]byte, error) {
	type Alias Config
	aux := &struct {
		Providers *ProvidersConfig `json:"providers,omitempty"`
		Session   *SessionConfig   `json:"session,omitempty"`
		Channels  *ChannelsConfig  `json:"channels,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(&c),
	}

	if !c.Providers.IsEmpty() {
		aux.Providers = &c.Providers
	}
	if c.Session.DMScope != "" || len(c.Session.IdentityLinks) > 0 {
		aux.Session = &c.Session
	}
	if c.Channels.HasEnabled() {
		aux.Channels = &c.Channels
	}

	return json.Marshal(aux)
}

// --- Agent types ---

type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
	List     []AgentConfig `json:"list,omitempty"`
}

// AgentModelConfig supports both string and structured model config.
// String format: "gpt-4" (just primary, no fallbacks)
// Object format: {"primary": "gpt-4", "fallbacks": ["claude-haiku"]}
type AgentModelConfig struct {
	Primary   string   `json:"primary,omitempty"`
	Fallbacks []string `json:"fallbacks,omitempty"`
}

func (m *AgentModelConfig) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.Primary = s
		m.Fallbacks = nil
		return nil
	}
	type raw struct {
		Primary   string   `json:"primary"`
		Fallbacks []string `json:"fallbacks"`
	}
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	m.Primary = r.Primary
	m.Fallbacks = r.Fallbacks
	return nil
}

func (m AgentModelConfig) MarshalJSON() ([]byte, error) {
	if len(m.Fallbacks) == 0 && m.Primary != "" {
		return json.Marshal(m.Primary)
	}
	type raw struct {
		Primary   string   `json:"primary,omitempty"`
		Fallbacks []string `json:"fallbacks,omitempty"`
	}
	return json.Marshal(raw{Primary: m.Primary, Fallbacks: m.Fallbacks})
}

type AgentConfig struct {
	ID        string            `json:"id"`
	Default   bool              `json:"default,omitempty"`
	Name      string            `json:"name,omitempty"`
	Workspace string            `json:"workspace,omitempty"`
	Model     *AgentModelConfig `json:"model,omitempty"`
	Skills    []string          `json:"skills,omitempty"`
	Subagents *SubagentsConfig  `json:"subagents,omitempty"`
}

type SubagentsConfig struct {
	AllowAgents []string          `json:"allow_agents,omitempty"`
	Model       *AgentModelConfig `json:"model,omitempty"`
}

type PeerMatch struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type BindingMatch struct {
	Channel   string     `json:"channel"`
	AccountID string     `json:"account_id,omitempty"`
	Peer      *PeerMatch `json:"peer,omitempty"`
	GuildID   string     `json:"guild_id,omitempty"`
	TeamID    string     `json:"team_id,omitempty"`
}

type AgentBinding struct {
	AgentID string       `json:"agent_id"`
	Match   BindingMatch `json:"match"`
}

type SessionConfig struct {
	DMScope       string              `json:"dm_scope,omitempty"`
	IdentityLinks map[string][]string `json:"identity_links,omitempty"`
}

type AgentDefaults struct {
	Workspace                 string   `json:"workspace"                          env:"PICOCLAW_AGENTS_DEFAULTS_WORKSPACE"`
	RestrictToWorkspace       bool     `json:"restrict_to_workspace,omitempty"    env:"PICOCLAW_AGENTS_DEFAULTS_RESTRICT_TO_WORKSPACE"`
	AllowReadOutsideWorkspace bool     `json:"allow_read_outside_workspace,omitempty" env:"PICOCLAW_AGENTS_DEFAULTS_ALLOW_READ_OUTSIDE_WORKSPACE"`
	Provider                  string   `json:"provider,omitempty"                 env:"PICOCLAW_AGENTS_DEFAULTS_PROVIDER"`

	// Primary model (主 LLM) — the main model for the agent loop.
	PrimaryModel   string   `json:"primary_model,omitempty"   env:"PICOCLAW_AGENTS_DEFAULTS_PRIMARY_MODEL"`
	ModelFallbacks []string `json:"model_fallbacks,omitempty"`

	// Auxiliary model (副 LLM) — lightweight model for Analyser, Reflector, and Digest.
	AuxiliaryModel string `json:"auxiliary_model,omitempty" env:"PICOCLAW_AGENTS_DEFAULTS_AUXILIARY_MODEL"`

	// Deprecated: kept for backward compatibility — use primary_model / auxiliary_model instead.
	ModelName     string `json:"model_name,omitempty"      env:"PICOCLAW_AGENTS_DEFAULTS_MODEL_NAME"`
	Model         string `json:"model,omitempty"           env:"PICOCLAW_AGENTS_DEFAULTS_MODEL"`
	AnalyserModel string `json:"analyser_model,omitempty"  env:"PICOCLAW_AGENTS_DEFAULTS_ANALYSER_MODEL"`
	PreLLMModel   string `json:"pre_llm_model,omitempty"   env:"PICOCLAW_AGENTS_DEFAULTS_PRE_LLM_MODEL"`
	DigestModel   string `json:"digest_model,omitempty"    env:"PICOCLAW_AGENTS_DEFAULTS_DIGEST_MODEL"`

	// STT (Speech-to-Text) model — OpenAI-compatible /v1/audio/transcriptions.
	// Examples:
	//   "groq/whisper"           → Groq Whisper-large-v3 (fast, free tier)
	//   "openai/whisper-1"       → OpenAI Whisper
	//   "local/faster-whisper"   → local server at api_base in model_list
	// Falls back to a no-op (disabled) if empty.
	STTModel string `json:"stt_model,omitempty" env:"PICOCLAW_AGENTS_DEFAULTS_STT_MODEL"`

	ImageModel                string   `json:"image_model,omitempty"              env:"PICOCLAW_AGENTS_DEFAULTS_IMAGE_MODEL"`
	ImageModelFallbacks       []string `json:"image_model_fallbacks,omitempty"`
	MaxTokens                 int      `json:"max_tokens,omitempty"               env:"PICOCLAW_AGENTS_DEFAULTS_MAX_TOKENS"`
	Temperature               *float64 `json:"temperature,omitempty"              env:"PICOCLAW_AGENTS_DEFAULTS_TEMPERATURE"`
	MaxToolIterations         int      `json:"max_tool_iterations,omitempty"      env:"PICOCLAW_AGENTS_DEFAULTS_MAX_TOOL_ITERATIONS"`
}

// GetPrimaryModel returns the primary (main) model for the agent loop.
// Priority: primary_model → model_name → model.
func (d *AgentDefaults) GetPrimaryModel() string {
	if d.PrimaryModel != "" {
		return d.PrimaryModel
	}
	if d.ModelName != "" {
		return d.ModelName
	}
	return d.Model
}

// GetModelName is an alias for GetPrimaryModel (backward compatibility).
func (d *AgentDefaults) GetModelName() string {
	return d.GetPrimaryModel()
}

// GetAuxiliaryModel returns the auxiliary (lightweight) model.
// Used by Analyser (Phase 1), Reflector, and Digest.
// Priority: auxiliary_model → analyser_model → pre_llm_model → primary model.
func (d *AgentDefaults) GetAuxiliaryModel() string {
	if d.AuxiliaryModel != "" {
		return d.AuxiliaryModel
	}
	if d.AnalyserModel != "" {
		return d.AnalyserModel
	}
	if d.PreLLMModel != "" {
		return d.PreLLMModel
	}
	return d.GetPrimaryModel()
}

// GetAnalyserModel is an alias for GetAuxiliaryModel (backward compatibility).
func (d *AgentDefaults) GetAnalyserModel() string {
	return d.GetAuxiliaryModel()
}

// GetDigestModel returns the model for Phase 3 (MemoryDigest).
// Priority: digest_model → auxiliary model.
func (d *AgentDefaults) GetDigestModel() string {
	if d.DigestModel != "" {
		return d.DigestModel
	}
	return d.GetAuxiliaryModel()
}

// GetSTTModel returns the model name for Speech-to-Text transcription.
// Returns empty string if not configured (STT disabled).
func (d *AgentDefaults) GetSTTModel() string {
	return d.STTModel
}
