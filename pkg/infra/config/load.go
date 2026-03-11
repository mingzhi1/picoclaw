package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/caarlos0/env/v11"
	"github.com/tidwall/jsonc"

	"github.com/sipeed/picoclaw/pkg/infra/utils"
)

// LoadConfig reads and parses a config file (supports JSONC comments).
func LoadConfig(path string) (*Config, error) {
	resolveShortEnvVars()

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	// Lightweight probe: check if user provides model_list so we can clear
	// default entries before unmarshal (Go reuses slice backing-array elements).
	if hasUserModelList(data) {
		cfg.ModelList = nil
	}

	if err := json.Unmarshal(jsonc.ToJSON(data), cfg); err != nil {
		return nil, err
	}

	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	cfg.migrateChannelConfigs()

	if len(cfg.ModelList) == 0 && cfg.HasProvidersConfig() {
		cfg.ModelList = ConvertProvidersToModelList(cfg)
	}

	// PC_API_KEY: inject into first model_list entry if it has no key
	if pcKey := os.Getenv("PC_API_KEY"); pcKey != "" && len(cfg.ModelList) > 0 {
		if cfg.ModelList[0].APIKey == "" {
			cfg.ModelList[0].APIKey = pcKey
		}
	}

	if err := cfg.ValidateModelList(); err != nil {
		return nil, err
	}

	warnDeprecatedFields(cfg)

	return cfg, nil
}

// hasUserModelList does a lightweight check for non-empty model_list in raw JSON,
// avoiding a full Config unmarshal just to count entries.
func hasUserModelList(data []byte) bool {
	var probe struct {
		ModelList []json.RawMessage `json:"model_list"`
	}
	_ = json.Unmarshal(jsonc.ToJSON(data), &probe)
	return len(probe.ModelList) > 0
}

// SaveConfig writes the config to disk atomically.
func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return utils.WriteFileAtomic(path, data, 0o600)
}

// migrateChannelConfigs handles backward-compatible config field migrations.
func (c *Config) migrateChannelConfigs() {
	// Discord: mention_only -> group_trigger.mention_only
	if c.Channels.Discord.MentionOnly && !c.Channels.Discord.GroupTrigger.MentionOnly {
		c.Channels.Discord.GroupTrigger.MentionOnly = true
	}

	// OneBot: group_trigger_prefix -> group_trigger.prefixes
	if len(c.Channels.OneBot.GroupTriggerPrefix) > 0 &&
		len(c.Channels.OneBot.GroupTrigger.Prefixes) == 0 {
		c.Channels.OneBot.GroupTrigger.Prefixes = c.Channels.OneBot.GroupTriggerPrefix
	}
}

// expandHome expands ~ to the user's home directory.
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

// --- Short environment variable aliases ---
// Makes Docker/CI deployment much simpler:
//   PC_MODEL=deepseek-chat PC_API_KEY=sk-xxx picoclaw gateway

// shortEnvAliases maps short env var names to their full PICOCLAW_ equivalents.
var shortEnvAliases = map[string]string{
	"PC_MODEL":     "PICOCLAW_AGENTS_DEFAULTS_PRIMARY_MODEL",
	"PC_AUX_MODEL": "PICOCLAW_AGENTS_DEFAULTS_AUXILIARY_MODEL",
	"PC_PROXY":     "PICOCLAW_PROXY",
	"PC_TG_TOKEN":  "PICOCLAW_CHANNELS_TELEGRAM_TOKEN",
	"PC_DC_TOKEN":  "PICOCLAW_CHANNELS_DISCORD_TOKEN",
	"PC_GW_HOST":   "PICOCLAW_GATEWAY_HOST",
	"PC_GW_PORT":   "PICOCLAW_GATEWAY_PORT",
}

// resolveShortEnvVars maps PC_* short aliases to their full PICOCLAW_* names.
// Short aliases do NOT override already-set full env vars.
func resolveShortEnvVars() {
	for short, full := range shortEnvAliases {
		if v := os.Getenv(short); v != "" {
			if os.Getenv(full) == "" {
				os.Setenv(full, v)
			}
		}
	}

	// PC_CHANNEL=telegram → auto-enable that channel
	if ch := os.Getenv("PC_CHANNEL"); ch != "" {
		enableEnv := fmt.Sprintf("PICOCLAW_CHANNELS_%s_ENABLED", upperCase(ch))
		if os.Getenv(enableEnv) == "" {
			os.Setenv(enableEnv, "true")
		}
	}
}

// upperCase is a simple ASCII uppercase helper (avoids importing strings).
func upperCase(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// warnDeprecatedFields prints warnings for deprecated config fields.
func warnDeprecatedFields(cfg *Config) {
	if cfg.Agents.Defaults.ModelName != "" {
		fmt.Fprintf(os.Stderr, "[WARN] config: 'model_name' is deprecated, use 'primary_model' instead\n")
	}
	if cfg.Agents.Defaults.Model != "" {
		fmt.Fprintf(os.Stderr, "[WARN] config: 'model' is deprecated, use 'primary_model' instead\n")
	}
	if cfg.Agents.Defaults.AnalyserModel != "" {
		fmt.Fprintf(os.Stderr, "[WARN] config: 'analyser_model' is deprecated, use 'auxiliary_model' instead\n")
	}
	if cfg.Agents.Defaults.PreLLMModel != "" {
		fmt.Fprintf(os.Stderr, "[WARN] config: 'pre_llm_model' is deprecated, use 'auxiliary_model' instead\n")
	}
	if !cfg.Providers.IsEmpty() {
		fmt.Fprintf(os.Stderr, "[WARN] config: 'providers' section is deprecated, use 'model_list' instead\n")
	}
}

