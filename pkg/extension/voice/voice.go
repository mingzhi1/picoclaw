// Package voice provides text-to-speech (TTS) and speech-to-text (STT) capabilities.
// This is a scaffold for future implementation.
package voice

import (
	"context"

	"github.com/sipeed/picoclaw/pkg/extension"
	"github.com/sipeed/picoclaw/pkg/infra/logger"
)

// Ext implements extension.Extension for voice interaction.
type Ext struct {
	enabled   bool
	ttsEngine string // "local", "openai", "azure", etc.
	sttEngine string // "whisper", "azure", etc.
}

func New() *Ext { return &Ext{} }

func (e *Ext) Name() string { return "voice" }

func (e *Ext) Init(ctx extension.ExtensionContext) error {
	if v, ok := ctx.Config["enabled"].(bool); ok {
		e.enabled = v
	}
	if v, ok := ctx.Config["tts_engine"].(string); ok {
		e.ttsEngine = v
	}
	if v, ok := ctx.Config["stt_engine"].(string); ok {
		e.sttEngine = v
	}
	return nil
}

func (e *Ext) Start(_ context.Context) error {
	if !e.enabled {
		return nil
	}
	logger.InfoCF("voice", "Voice extension started", map[string]any{
		"tts": e.ttsEngine,
		"stt": e.sttEngine,
	})
	// TODO: initialise TTS/STT engines
	return nil
}

func (e *Ext) Stop() error {
	if !e.enabled {
		return nil
	}
	logger.InfoCF("voice", "Voice extension stopped", nil)
	return nil
}

// Synthesize converts text to audio bytes. Returns (audioData, format, error).
// Placeholder — will be implemented when a TTS engine is integrated.
func (e *Ext) Synthesize(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}

// Transcribe converts audio bytes to text. Returns (text, error).
// Placeholder — will be implemented when an STT engine is integrated.
func (e *Ext) Transcribe(_ context.Context, _ []byte, _ string) (string, error) {
	return "", nil
}
