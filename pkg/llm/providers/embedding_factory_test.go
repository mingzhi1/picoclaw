package providers

import (
	"testing"

	"github.com/sipeed/picoclaw/pkg/infra/config"
	"github.com/sipeed/picoclaw/pkg/llm/providers/openai_compat"
)

func TestCreateEmbeddingProviderFromConfig_OpenAICompatible(t *testing.T) {
	cfg := &config.ModelConfig{
		Model:   "openai/text-embedding-3-small",
		APIBase: "https://api.openai.com/v1",
		APIKey:  "test-key",
	}

	provider, modelID, err := CreateEmbeddingProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("CreateEmbeddingProviderFromConfig() error = %v", err)
	}
	if modelID != "text-embedding-3-small" {
		t.Fatalf("modelID = %q, want %q", modelID, "text-embedding-3-small")
	}
	if _, ok := provider.(*openai_compat.EmbeddingProvider); !ok {
		t.Fatalf("provider type = %T, want *openai_compat.EmbeddingProvider", provider)
	}
}

func TestCreateEmbeddingProviderFromConfig_UnsupportedProtocol(t *testing.T) {
	cfg := &config.ModelConfig{
		Model:  "anthropic/claude-sonnet-4.6",
		APIKey: "test-key",
	}

	if _, _, err := CreateEmbeddingProviderFromConfig(cfg); err == nil {
		t.Fatal("CreateEmbeddingProviderFromConfig() expected error for anthropic protocol")
	}
}
