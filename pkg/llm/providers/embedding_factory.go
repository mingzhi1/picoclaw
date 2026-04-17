package providers

import (
	"fmt"

	"github.com/mingzhi1/metaclaw/pkg/infra/config"
	"github.com/mingzhi1/metaclaw/pkg/llm/providers/openai_compat"
)

// CreateEmbeddingProviderFromConfig creates an embedding provider from model_list config.
func CreateEmbeddingProviderFromConfig(cfg *config.ModelConfig) (EmbeddingProvider, string, error) {
	if cfg == nil {
		return nil, "", fmt.Errorf("config is nil")
	}
	if cfg.Model == "" {
		return nil, "", fmt.Errorf("model is required")
	}

	protocol, modelID := ExtractProtocol(cfg.Model)
	switch protocol {
	case "openai", "litellm", "openrouter", "groq", "zhipu", "gemini", "nvidia",
		"ollama", "moonshot", "shengsuanyun", "deepseek", "cerebras",
		"volcengine", "vllm", "qwen", "mistral", "iflow", "kilocode":
		apiBase := cfg.APIBase
		if apiBase == "" {
			apiBase = getDefaultAPIBase(protocol)
		}
		if apiBase == "" {
			return nil, "", fmt.Errorf("api_base is required for embedding protocol %q", protocol)
		}
		return openai_compat.NewEmbeddingProvider(cfg.APIKey, apiBase, cfg.Proxy, modelID, 0), modelID, nil
	default:
		return nil, "", fmt.Errorf("protocol %q does not support embeddings", protocol)
	}
}
