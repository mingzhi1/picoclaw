package openai_compat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mingzhi1/metaclaw/pkg/infra/httpclient"
	"github.com/mingzhi1/metaclaw/pkg/infra/logger"
)

// EmbeddingProvider calls an OpenAI-compatible /v1/embeddings endpoint.
type EmbeddingProvider struct {
	apiKey     string
	apiBase    string
	model      string
	dimensions int
	httpClient *http.Client
}

// NewEmbeddingProvider creates an embedding provider for OpenAI-compatible APIs.
func NewEmbeddingProvider(apiKey, apiBase, proxy, model string, dimensions int) *EmbeddingProvider {
	client, err := httpclient.NewWithProxy(60*time.Second, proxy)
	if err != nil {
		client = httpclient.New(60 * time.Second)
	}
	return &EmbeddingProvider{
		apiKey:     apiKey,
		apiBase:    apiBase,
		model:      model,
		dimensions: dimensions,
		httpClient: client,
	}
}

func (e *EmbeddingProvider) Dimensions() int { return e.dimensions }

// Embed converts texts into float32 vectors via the /v1/embeddings API.
func (e *EmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := map[string]any{
		"model": e.model,
		"input": texts,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	url := e.apiBase + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	start := time.Now()
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API error %d: %s", resp.StatusCode, string(body))
	}

	var apiResp embeddingResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal embedding response: %w", err)
	}

	result := make([][]float32, len(apiResp.Data))
	for i, d := range apiResp.Data {
		result[i] = d.Embedding
	}

	logger.DebugCF("embedding", "Embedding completed",
		map[string]any{
			"texts":    len(texts),
			"model":    e.model,
			"duration": time.Since(start).Milliseconds(),
		})

	return result, nil
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}
