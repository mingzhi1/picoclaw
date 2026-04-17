// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package config

import (
	"os"
	"path/filepath"
)

// DefaultConfig returns the default configuration for PicoClaw.
func DefaultConfig() *Config {
	homePath := defaultHomePath()
	workspacePath := filepath.Join(homePath, "workspace")

	return &Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:           workspacePath,
				RestrictToWorkspace: true,
				Provider:            "",
				Model:               "",
				MaxTokens:           32768,
				Temperature:         nil, // nil means use provider default
				MaxToolIterations:   50,
			},
		},
		Bindings: []AgentBinding{},
		Session: SessionConfig{
			DMScope: "per-channel-peer",
		},
		Channels: ChannelsConfig{
			Telegram: TelegramConfig{
				Enabled:   false,
				Token:     "",
				AllowFrom: FlexibleStringSlice{},
				Typing:    TypingConfig{Enabled: true},
				Placeholder: PlaceholderConfig{
					Enabled: true,
					Text:    "Thinking... 💭",
				},
			},
			Feishu: FeishuConfig{
				Enabled:           false,
				AppID:             "",
				AppSecret:         "",
				EncryptKey:        "",
				VerificationToken: "",
				AllowFrom:         FlexibleStringSlice{},
			},
		},
		Providers: ProvidersConfig{
			OpenAI: OpenAIProviderConfig{WebSearch: true},
		},
		// ModelList defaults to empty. Users add only what they need.
		// See config.example.jsonc for provider templates.
		ModelList: []ModelConfig{},
		Gateway: GatewayConfig{
			Host: "127.0.0.1",
			Port: 18790,
		},
		Tools: ToolsConfig{
			MediaCleanup: MediaCleanupConfig{
				Enabled:  true,
				MaxAge:   30,
				Interval: 5,
			},
			Web: WebToolsConfig{
				Proxy:           "",
				FetchLimitBytes: 10 * 1024 * 1024, // 10MB by default
				Brave: BraveConfig{
					Enabled:    false,
					APIKey:     "",
					MaxResults: 5,
				},
				DuckDuckGo: DuckDuckGoConfig{
					Enabled:    true,
					MaxResults: 5,
				},
				Perplexity: PerplexityConfig{
					Enabled:    false,
					APIKey:     "",
					MaxResults: 5,
				},
			},
			RAG: RAGToolsConfig{
				Enabled:        false,
				EmbeddingModel: "",
			},
			Cron: CronToolsConfig{
				ExecTimeoutMinutes: 5,
			},
			Exec: ExecConfig{
				EnableDenyPatterns: true,
			},
			Skills: SkillsToolsConfig{
				Registries: SkillsRegistriesConfig{
					ClawHub: ClawHubRegistryConfig{
						Enabled: true,
						BaseURL: "https://clawhub.ai",
					},
				},
				MaxConcurrentSearches: 2,
				SearchCache: SearchCacheConfig{
					MaxSize:    50,
					TTLSeconds: 300,
				},
			},
			MCP: MCPConfig{
				Enabled: false,
				Servers: map[string]MCPServerConfig{},
			},
		},
		Heartbeat: HeartbeatConfig{
			Enabled:  true,
			Interval: 30,
		},
	}
}

func defaultHomePath() string {
	if metaclawHome := os.Getenv("METACLAW_HOME"); metaclawHome != "" {
		return metaclawHome
	}
	if picoclawHome := os.Getenv("PICOCLAW_HOME"); picoclawHome != "" {
		return picoclawHome
	}

	userHome, _ := os.UserHomeDir()
	metaHome := filepath.Join(userHome, ".metaclaw")
	legacyHome := filepath.Join(userHome, ".picoclaw")
	if pathExists(metaHome) {
		return metaHome
	}
	if pathExists(legacyHome) {
		return legacyHome
	}
	return metaHome
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
