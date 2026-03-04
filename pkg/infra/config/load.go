package config

import (
	"encoding/json"
	"os"

	"github.com/caarlos0/env/v11"
	"github.com/tidwall/jsonc"

	"github.com/sipeed/picoclaw/pkg/infra/utils"
)

// LoadConfig reads and parses a config file (supports JSONC comments).
func LoadConfig(path string) (*Config, error) {
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

	if err := cfg.ValidateModelList(); err != nil {
		return nil, err
	}

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
