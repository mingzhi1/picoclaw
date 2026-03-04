package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/llm/auth"
	"github.com/sipeed/picoclaw/pkg/infra/config"
)

// ── Model identification helpers ─────────────────────────────────

func TestIsOpenAIModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"openai", true},
		{"openai/gpt-4o", true},
		{"openai/gpt-5.2", true},
		{"anthropic", false},
		{"anthropic/claude-sonnet-4.6", false},
		{"openai-compatible", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isOpenAIModel(tt.model); got != tt.want {
			t.Errorf("isOpenAIModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestIsAnthropicModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"anthropic", true},
		{"anthropic/claude-sonnet-4.6", true},
		{"openai", false},
		{"openai/gpt-4o", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isAnthropicModel(tt.model); got != tt.want {
			t.Errorf("isAnthropicModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestIsAntigravityModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"antigravity", true},
		{"google-antigravity", true},
		{"antigravity/gemini-3-flash", true},
		{"google-antigravity/gemini-3-flash", true},
		{"openai", false},
		{"antigravity-custom", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isAntigravityModel(tt.model); got != tt.want {
			t.Errorf("isAntigravityModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

// ── Config update helpers ────────────────────────────────────────

func writeTempConfigViaSave(t *testing.T, cfg *config.Config) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := config.SaveConfig(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return path
}

func loadTempConfig(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestUpdateConfigAfterLogin_OpenAI_ExistingModel(t *testing.T) {
	cfg := &config.Config{
		ModelList: []config.ModelConfig{
			{ModelName: "gpt-4o", Model: "openai/gpt-4o"},
		},
	}
	path := writeTempConfigViaSave(t, cfg)

	cred := &auth.AuthCredential{AuthMethod: "oauth"}
	updateConfigAfterLogin(path, "openai", cred)

	result := loadTempConfig(t, path)

	// Model-level auth_method persists through serialization
	if len(result.ModelList) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result.ModelList))
	}
	if result.ModelList[0].AuthMethod != "oauth" {
		t.Errorf("expected model auth_method=oauth, got %q", result.ModelList[0].AuthMethod)
	}
}

func TestUpdateConfigAfterLogin_OpenAI_NoExistingModel(t *testing.T) {
	// Start with a config that has only anthropic, no openai model
	// Use DefaultConfig so there's a model_list (non-empty → hasUserModelList=true)
	cfg := config.DefaultConfig()
	// Remove all OpenAI models from the list
	var filtered []config.ModelConfig
	for _, m := range cfg.ModelList {
		if !strings.HasPrefix(m.Model, "openai/") {
			filtered = append(filtered, m)
		}
	}
	cfg.ModelList = filtered
	path := writeTempConfigViaSave(t, cfg)

	cred := &auth.AuthCredential{AuthMethod: "oauth"}
	updateConfigAfterLogin(path, "openai", cred)

	result := loadTempConfig(t, path)

	// One OpenAI model should have been added
	var openAIModels []config.ModelConfig
	for _, m := range result.ModelList {
		if strings.HasPrefix(m.Model, "openai/") {
			openAIModels = append(openAIModels, m)
		}
	}
	if len(openAIModels) != 1 {
		t.Fatalf("expected exactly 1 openai model added, got %d", len(openAIModels))
	}
	if openAIModels[0].Model != "openai/gpt-5.2" {
		t.Errorf("expected added model openai/gpt-5.2, got %q", openAIModels[0].Model)
	}
	if result.Agents.Defaults.PrimaryModel != "gpt-5.2" {
		t.Errorf("expected default primary_model=gpt-5.2, got %q", result.Agents.Defaults.PrimaryModel)
	}
}

func TestUpdateConfigAfterLogin_Anthropic(t *testing.T) {
	// Use DefaultConfig so LoadConfig doesn't re-inject defaults.
	// The default model list already contains an anthropic model;
	// updateConfigAfterLogin should find it and update auth_method.
	cfg := config.DefaultConfig()
	path := writeTempConfigViaSave(t, cfg)

	cred := &auth.AuthCredential{AuthMethod: "token"}
	updateConfigAfterLogin(path, "anthropic", cred)

	result := loadTempConfig(t, path)

	// At least one anthropic model should have auth_method="token"
	var found bool
	for _, m := range result.ModelList {
		if strings.HasPrefix(m.Model, "anthropic/") && m.AuthMethod == "token" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one anthropic model with auth_method=token")
	}
}

func TestUpdateConfigAfterLogin_GoogleAntigravity(t *testing.T) {
	// Use DefaultConfig so LoadConfig doesn't re-inject defaults.
	// The default model list already contains an antigravity model;
	// updateConfigAfterLogin should find it and update auth_method.
	cfg := config.DefaultConfig()
	path := writeTempConfigViaSave(t, cfg)

	cred := &auth.AuthCredential{AuthMethod: "oauth"}
	updateConfigAfterLogin(path, "google-antigravity", cred)

	result := loadTempConfig(t, path)

	// At least one antigravity model should have auth_method="oauth"
	var found bool
	for _, m := range result.ModelList {
		if strings.HasPrefix(m.Model, "antigravity/") && m.AuthMethod == "oauth" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one antigravity model with auth_method=oauth")
	}
}

func TestClearAuthMethodInConfig(t *testing.T) {
	cfg := &config.Config{
		ModelList: []config.ModelConfig{
			{ModelName: "gpt-4o", Model: "openai/gpt-4o", AuthMethod: "oauth"},
			{ModelName: "claude", Model: "anthropic/claude-sonnet-4.6", AuthMethod: "token"},
		},
	}
	path := writeTempConfigViaSave(t, cfg)

	clearAuthMethodInConfig(path, "openai")

	result := loadTempConfig(t, path)

	// Openai model auth_method should be cleared
	if result.ModelList[0].AuthMethod != "" {
		t.Errorf("expected openai model auth_method cleared, got %q", result.ModelList[0].AuthMethod)
	}
	// Anthropic model should be unchanged
	if result.ModelList[1].AuthMethod != "token" {
		t.Errorf("expected anthropic model auth_method unchanged, got %q", result.ModelList[1].AuthMethod)
	}
}

func TestClearAllAuthMethodsInConfig(t *testing.T) {
	cfg := &config.Config{
		ModelList: []config.ModelConfig{
			{ModelName: "gpt-4o", Model: "openai/gpt-4o", AuthMethod: "oauth"},
			{ModelName: "claude", Model: "anthropic/claude-sonnet-4.6", AuthMethod: "token"},
			{ModelName: "gemini", Model: "antigravity/gemini-3-flash", AuthMethod: "oauth"},
		},
	}
	path := writeTempConfigViaSave(t, cfg)

	clearAllAuthMethodsInConfig(path)

	result := loadTempConfig(t, path)

	for i, m := range result.ModelList {
		if m.AuthMethod != "" {
			t.Errorf("model[%d] auth_method not cleared, got %q", i, m.AuthMethod)
		}
	}
}
