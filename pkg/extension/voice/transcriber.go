package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/infra/logger"
	"github.com/sipeed/picoclaw/pkg/infra/utils"
)

// TranscriptionResponse is the parsed result from /v1/audio/transcriptions.
type TranscriptionResponse struct {
	Text     string  `json:"text"`
	Language string  `json:"language,omitempty"`
	Duration float64 `json:"duration,omitempty"`
}

// TranscriberConfig configures an OpenAI-compatible STT transcriber.
// Any provider implementing /v1/audio/transcriptions is supported:
//
//	Groq:   APIBase="https://api.groq.com/openai/v1",  Model="whisper-large-v3"
//	OpenAI: APIBase="https://api.openai.com/v1",       Model="whisper-1"
//	Local:  APIBase="http://localhost:8080/v1",         Model="faster-whisper"
type TranscriberConfig struct {
	APIBase        string        // base URL, no trailing slash
	APIKey         string        // Bearer token; empty = no auth (local servers)
	Model          string        // model identifier sent to the API
	Timeout        time.Duration // default 60s
	ResponseFormat string        // "json" (default) or "text"
	Language       string        // ISO 639-1 hint, e.g. "en", "zh"; empty = auto
}

// Transcriber transcribes audio via any OpenAI-compatible endpoint.
type Transcriber struct {
	cfg        TranscriberConfig
	httpClient *http.Client
}

// NewTranscriber creates a Transcriber. Returns error if APIBase is empty.
func NewTranscriber(cfg TranscriberConfig) (*Transcriber, error) {
	if cfg.APIBase == "" {
		return nil, fmt.Errorf("voice: APIBase is required")
	}
	if cfg.Model == "" {
		cfg.Model = "whisper-large-v3"
	}
	if cfg.ResponseFormat == "" {
		cfg.ResponseFormat = "json"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	cfg.APIBase = strings.TrimRight(cfg.APIBase, "/")

	return &Transcriber{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// IsAvailable returns true if the transcriber can make requests.
// Local servers (localhost/127.0.0.1) don't require an API key.
func (t *Transcriber) IsAvailable() bool {
	local := strings.Contains(t.cfg.APIBase, "localhost") ||
		strings.Contains(t.cfg.APIBase, "127.0.0.1")
	return local || t.cfg.APIKey != ""
}

// Provider returns a human-readable description for logging.
func (t *Transcriber) Provider() string {
	return fmt.Sprintf("%s | model=%s", t.cfg.APIBase, t.cfg.Model)
}

// Transcribe converts an audio file to text.
// Supported formats: mp3, mp4, mpeg, mpga, m4a, wav, webm, ogg.
func (t *Transcriber) Transcribe(ctx context.Context, audioFilePath string) (*TranscriptionResponse, error) {
	logger.InfoCF("voice", "Starting transcription", map[string]any{
		"file":     audioFilePath,
		"provider": t.Provider(),
	})

	audioFile, err := os.Open(audioFilePath)
	if err != nil {
		return nil, fmt.Errorf("voice: open audio file: %w", err)
	}
	defer audioFile.Close()

	fi, err := audioFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("voice: stat audio file: %w", err)
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	part, err := w.CreateFormFile("file", filepath.Base(audioFilePath))
	if err != nil {
		return nil, fmt.Errorf("voice: create form file: %w", err)
	}
	if _, err = io.Copy(part, audioFile); err != nil {
		return nil, fmt.Errorf("voice: copy audio data: %w", err)
	}
	if err = w.WriteField("model", t.cfg.Model); err != nil {
		return nil, fmt.Errorf("voice: write model field: %w", err)
	}
	if err = w.WriteField("response_format", t.cfg.ResponseFormat); err != nil {
		return nil, fmt.Errorf("voice: write response_format field: %w", err)
	}
	if t.cfg.Language != "" {
		if err = w.WriteField("language", t.cfg.Language); err != nil {
			return nil, fmt.Errorf("voice: write language field: %w", err)
		}
	}
	if err = w.Close(); err != nil {
		return nil, fmt.Errorf("voice: close multipart writer: %w", err)
	}

	url := t.cfg.APIBase + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, &body)
	if err != nil {
		return nil, fmt.Errorf("voice: create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if t.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.cfg.APIKey)
	}

	logger.DebugCF("voice", "Sending transcription request", map[string]any{
		"url":        url,
		"model":      t.cfg.Model,
		"file_bytes": fi.Size(),
	})

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice: send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("voice: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logger.ErrorCF("voice", "Transcription API error", map[string]any{
			"status":   resp.StatusCode,
			"response": string(respBody),
		})
		return nil, fmt.Errorf("voice: API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if t.cfg.ResponseFormat == "text" {
		result := &TranscriptionResponse{Text: strings.TrimSpace(string(respBody))}
		logger.InfoCF("voice", "Transcription complete", map[string]any{
			"text_len": len(result.Text),
			"preview":  utils.Truncate(result.Text, 50),
		})
		return result, nil
	}

	var result TranscriptionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("voice: unmarshal response: %w", err)
	}

	logger.InfoCF("voice", "Transcription complete", map[string]any{
		"text_len":     len(result.Text),
		"language":     result.Language,
		"duration_sec": result.Duration,
		"preview":      utils.Truncate(result.Text, 50),
	})

	return &result, nil
}
