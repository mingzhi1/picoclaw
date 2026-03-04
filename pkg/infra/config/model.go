package config

import (
	"fmt"
	"sync/atomic"
)

// rrCounter is a global counter for round-robin load balancing across models.
var rrCounter atomic.Uint64

// GetModelConfig returns the ModelConfig for the given model name.
// If multiple configs exist with the same model_name, it uses round-robin
// selection for load balancing. Returns an error if the model is not found.
func (c *Config) GetModelConfig(modelName string) (*ModelConfig, error) {
	matches := c.findMatches(modelName)
	if len(matches) == 0 {
		return nil, fmt.Errorf("model %q not found in model_list or providers", modelName)
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}

	// Multiple configs - use round-robin for load balancing
	idx := rrCounter.Add(1) % uint64(len(matches))
	return &matches[idx], nil
}

// findMatches finds all ModelConfig entries with the given model_name.
func (c *Config) findMatches(modelName string) []ModelConfig {
	var matches []ModelConfig
	for i := range c.ModelList {
		if c.ModelList[i].ModelName == modelName {
			matches = append(matches, c.ModelList[i])
		}
	}
	return matches
}

// HasProvidersConfig checks if any provider in the old providers config has configuration.
func (c *Config) HasProvidersConfig() bool {
	return !c.Providers.IsEmpty()
}

// ValidateModelList validates all ModelConfig entries in the model_list.
func (c *Config) ValidateModelList() error {
	for i := range c.ModelList {
		if err := c.ModelList[i].Validate(); err != nil {
			return fmt.Errorf("model_list[%d]: %w", i, err)
		}
	}
	return nil
}

// WorkspacePath returns the expanded workspace path from agent defaults.
func (c *Config) WorkspacePath() string {
	return expandHome(c.Agents.Defaults.Workspace)
}

// --- Legacy helpers (deprecated, prefer using model_list) ---

// GetAPIKey returns the first non-empty API key found across providers.
func (c *Config) GetAPIKey() string {
	if c.Providers.OpenRouter.APIKey != "" {
		return c.Providers.OpenRouter.APIKey
	}
	if c.Providers.Anthropic.APIKey != "" {
		return c.Providers.Anthropic.APIKey
	}
	if c.Providers.OpenAI.APIKey != "" {
		return c.Providers.OpenAI.APIKey
	}
	if c.Providers.Gemini.APIKey != "" {
		return c.Providers.Gemini.APIKey
	}
	if c.Providers.Zhipu.APIKey != "" {
		return c.Providers.Zhipu.APIKey
	}
	if c.Providers.Groq.APIKey != "" {
		return c.Providers.Groq.APIKey
	}
	if c.Providers.VLLM.APIKey != "" {
		return c.Providers.VLLM.APIKey
	}
	if c.Providers.ShengSuanYun.APIKey != "" {
		return c.Providers.ShengSuanYun.APIKey
	}
	if c.Providers.Cerebras.APIKey != "" {
		return c.Providers.Cerebras.APIKey
	}
	return ""
}

// GetAPIBase returns the API base URL for the first configured provider.
func (c *Config) GetAPIBase() string {
	if c.Providers.OpenRouter.APIKey != "" {
		if c.Providers.OpenRouter.APIBase != "" {
			return c.Providers.OpenRouter.APIBase
		}
		return "https://openrouter.ai/api/v1"
	}
	if c.Providers.Zhipu.APIKey != "" {
		return c.Providers.Zhipu.APIBase
	}
	if c.Providers.VLLM.APIKey != "" && c.Providers.VLLM.APIBase != "" {
		return c.Providers.VLLM.APIBase
	}
	return ""
}
