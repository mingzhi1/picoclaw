// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package providers

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/mingzhi1/metaclaw/pkg/infra/logger"
)

// --- iFlow credential reader ---

// iflowSettings mirrors ~/.iflow/settings.json
type iflowSettings struct {
	APIKey    string `json:"apiKey"`
	BaseURL   string `json:"baseUrl"`
	ModelName string `json:"modelName"`
}

// iflowOAuthCreds mirrors ~/.iflow/oauth_creds.json
type iflowOAuthCreds struct {
	APIKey      string `json:"apiKey"`
	AccessToken string `json:"access_token"`
}

// ReadIFlowCredentials reads API key from ~/.iflow/ config files.
// Priority: settings.json apiKey > oauth_creds.json apiKey.
func ReadIFlowCredentials() (apiKey, apiBase, model string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", ""
	}
	iflowDir := filepath.Join(home, ".iflow")

	// Try settings.json first
	if data, err := os.ReadFile(filepath.Join(iflowDir, "settings.json")); err == nil {
		var s iflowSettings
		if json.Unmarshal(data, &s) == nil && s.APIKey != "" {
			apiBase := s.BaseURL
			if apiBase == "" {
				apiBase = "https://apis.iflow.cn/v1"
			}
			logger.InfoCF("external_creds", "Found iFlow credentials from ~/.iflow/settings.json", nil)
			return s.APIKey, apiBase, s.ModelName
		}
	}

	// Fallback to oauth_creds.json
	if data, err := os.ReadFile(filepath.Join(iflowDir, "oauth_creds.json")); err == nil {
		var o iflowOAuthCreds
		if json.Unmarshal(data, &o) == nil && o.APIKey != "" {
			logger.InfoCF("external_creds", "Found iFlow credentials from ~/.iflow/oauth_creds.json", nil)
			return o.APIKey, "https://apis.iflow.cn/v1", ""
		}
	}

	return "", "", ""
}

// --- Qwen credential reader ---

// qwenSettings mirrors ~/.qwen/settings.json
type qwenSettings struct {
	ModelProviders map[string][]qwenModelProvider `json:"modelProviders"`
	Env            map[string]string              `json:"env"`
	Security       struct {
		Auth struct {
			SelectedType string `json:"selectedType"`
		} `json:"auth"`
	} `json:"security"`
	Model struct {
		Name string `json:"name"`
	} `json:"model"`
}

// qwenModelProvider mirrors a single model entry in modelProviders.
type qwenModelProvider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	EnvKey  string `json:"envKey"`
}

// qwenOAuthCreds mirrors ~/.qwen/oauth_creds.json
type qwenOAuthCreds struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ResourceURL  string `json:"resource_url"`
	ExpiryDate   int64  `json:"expiry_date"`
}

// ReadQwenCredentials reads credentials from ~/.qwen/ config files.
// Returns apiKey, apiBase, model, authType.
// Supports both API-KEY mode (from settings.json env) and OAuth mode.
func ReadQwenCredentials() (apiKey, apiBase, model, authType string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", "", ""
	}
	qwenDir := filepath.Join(home, ".qwen")

	// Read settings.json
	var settings qwenSettings
	if data, err := os.ReadFile(filepath.Join(qwenDir, "settings.json")); err == nil {
		_ = json.Unmarshal(data, &settings)
	} else {
		return "", "", "", ""
	}

	authType = settings.Security.Auth.SelectedType
	model = settings.Model.Name

	// API-KEY mode: extract key from env section or modelProviders
	if authType != "qwen-oauth" && authType != "" {
		// Look for API keys in settings.env
		for _, providers := range settings.ModelProviders {
			for _, p := range providers {
				if envKey := p.EnvKey; envKey != "" {
					// Check settings.env first, then real env vars
					if key, ok := settings.Env[envKey]; ok && key != "" {
						base := p.BaseURL
						if base == "" {
							base = "https://dashscope.aliyuncs.com/compatible-mode/v1"
						}
						m := model
						if m == "" {
							m = p.ID
						}
						logger.InfoCF("external_creds", "Found Qwen API key credentials", nil)
						return key, base, m, authType
					}
					// Check real environment variable
					if key := os.Getenv(envKey); key != "" {
						base := p.BaseURL
						if base == "" {
							base = "https://dashscope.aliyuncs.com/compatible-mode/v1"
						}
						m := model
						if m == "" {
							m = p.ID
						}
						logger.InfoCF("external_creds", "Found Qwen API key from env var", nil)
						return key, base, m, authType
					}
				}
			}
		}
	}

	// OAuth mode: read oauth_creds.json for access token
	if data, err := os.ReadFile(filepath.Join(qwenDir, "oauth_creds.json")); err == nil {
		var creds qwenOAuthCreds
		if json.Unmarshal(data, &creds) == nil && creds.AccessToken != "" {
			logger.InfoCF("external_creds", "Found Qwen OAuth credentials", nil)
			return creds.AccessToken, "https://chat.qwen.ai/api", model, "qwen-oauth"
		}
	}

	return "", "", "", ""
}

// --- Kilo Code credential reader ---

// kiloCodeConfig mirrors ~/.kilocode/cli/config.json
type kiloCodeConfig struct {
	Provider  string              `json:"provider"`
	Providers []kiloCodeProvider  `json:"providers"`
}

// kiloCodeProvider mirrors a provider entry in config.json
type kiloCodeProvider struct {
	ID             string `json:"id"`
	Provider       string `json:"provider"`
	KilocodeToken  string `json:"kilocodeToken"`
	KilocodeModel  string `json:"kilocodeModel"`
	OpenRouterKey  string `json:"openRouterApiKey"`
	OpenRouterURL  string `json:"openRouterBaseUrl"`
	APIKey         string `json:"apiKey"`
	APIModelID     string `json:"apiModelId"`
}

// kiloCodeSecrets mirrors ~/.kilocode/cli/global/secrets.json
type kiloCodeSecrets struct {
	KilocodeToken string `json:"kilocodeToken"`
}

// ReadKiloCodeCredentials reads API credentials from ~/.kilocode/cli/.
// Kilo Code proxies OpenRouter: kilocodeToken is used as Bearer auth
// against https://kilocode.ai/api/openrouter (OpenAI-compatible).
func ReadKiloCodeCredentials() (apiKey, apiBase, model string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", ""
	}
	kiloDir := filepath.Join(home, ".kilocode", "cli")

	// Read config.json
	var cfg kiloCodeConfig
	if data, err := os.ReadFile(filepath.Join(kiloDir, "config.json")); err == nil {
		_ = json.Unmarshal(data, &cfg)
	} else {
		return "", "", ""
	}

	// Find the active provider
	activeID := cfg.Provider
	for _, p := range cfg.Providers {
		if p.ID != activeID && activeID != "" {
			continue
		}
		// kilocode provider — uses Kilo gateway (OpenRouter proxy)
		if p.Provider == "kilocode" && p.KilocodeToken != "" {
			m := p.KilocodeModel
			if m == "" {
				m = "kilo-auto/balanced"
			}
			logger.InfoCF("external_creds", "Found Kilo Code credentials from ~/.kilocode/cli/", nil)
			return p.KilocodeToken, "https://kilocode.ai/api/openrouter", m
		}
		// openrouter provider — direct OpenRouter key
		if p.Provider == "openrouter" && p.OpenRouterKey != "" {
			base := p.OpenRouterURL
			if base == "" {
				base = "https://openrouter.ai/api/v1"
			}
			logger.InfoCF("external_creds", "Found OpenRouter credentials from Kilo CLI", nil)
			return p.OpenRouterKey, base, p.APIModelID
		}
		// Generic provider with apiKey
		if p.APIKey != "" {
			logger.InfoCF("external_creds", "Found API key from Kilo CLI", nil)
			return p.APIKey, "", p.APIModelID
		}
	}

	// Fallback: read from secrets.json
	if data, err := os.ReadFile(filepath.Join(kiloDir, "global", "secrets.json")); err == nil {
		var s kiloCodeSecrets
		if json.Unmarshal(data, &s) == nil && s.KilocodeToken != "" {
			logger.InfoCF("external_creds", "Found Kilo Code token from secrets.json", nil)
			return s.KilocodeToken, "https://kilocode.ai/api/openrouter", "kilo-auto/balanced"
		}
	}

	return "", "", ""
}
