// Package voice provides Speech-to-Text (STT) as a lifecycle extension.
//
// Configuration (config.json):
//
//	agents.defaults.stt_model: "groq-whisper"
//
//	model_list:
//	  - model_name: "groq-whisper"
//	    api_base:   "https://api.groq.com/openai/v1"
//	    api_key:    "gsk_..."
//	    model:      "whisper-large-v3"
//
// Any OpenAI-compatible /v1/audio/transcriptions endpoint works.
package voice

import (
	"context"
	"fmt"

	"github.com/sipeed/picoclaw/pkg/extension"
	"github.com/sipeed/picoclaw/pkg/infra/logger"
)

// Ext implements extension.Extension for speech-to-text transcription.
type Ext struct {
	transcriber *Transcriber
}

// New creates a new voice extension.
func New() *Ext { return &Ext{} }

func (e *Ext) Name() string { return "voice" }

// Init resolves the stt_model from ExtensionContext config and creates the Transcriber.
// Expected config keys:
//
//	"stt_model"  string  — model alias (looked up in model_list)
//	"api_base"   string  — provider base URL
//	"api_key"    string  — auth token (empty = local server)
//	"model"      string  — model identifier sent to the API
//	"language"   string  — ISO 639-1 hint, optional
func (e *Ext) Init(ctx extension.ExtensionContext) error {
	sttModel, _ := ctx.Config["stt_model"].(string)
	if sttModel == "" {
		logger.DebugCF("voice", "STT not configured (stt_model is empty)", nil)
		return nil
	}

	apiBase, _ := ctx.Config["api_base"].(string)
	apiKey, _ := ctx.Config["api_key"].(string)
	model, _ := ctx.Config["model"].(string)
	language, _ := ctx.Config["language"].(string)

	if apiBase == "" {
		return fmt.Errorf("voice: stt_model=%q configured but api_base is missing — add it to model_list", sttModel)
	}

	t, err := NewTranscriber(TranscriberConfig{
		APIBase:  apiBase,
		APIKey:   apiKey,
		Model:    model,
		Language: language,
	})
	if err != nil {
		return fmt.Errorf("voice: init transcriber: %w", err)
	}

	e.transcriber = t
	logger.InfoCF("voice", "STT transcriber initialised", map[string]any{
		"stt_model": sttModel,
		"provider":  t.Provider(),
	})
	return nil
}

func (e *Ext) Start(_ context.Context) error { return nil }
func (e *Ext) Stop() error                   { return nil }

// Transcriber returns the configured STT transcriber, or nil if STT is disabled.
func (e *Ext) Transcriber() *Transcriber {
	return e.transcriber
}

// IsAvailable reports whether STT is configured and ready.
func (e *Ext) IsAvailable() bool {
	return e.transcriber != nil && e.transcriber.IsAvailable()
}
