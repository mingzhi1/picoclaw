package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PromptLogger writes LLM prompt/response pairs to separate log files,
// keeping them isolated from system logs for debugging and auditing.
//
// Each call creates a JSON file: {prompt_dir}/{timestamp}_{seq}_{model}.json
type PromptLogger struct {
	dir string
	seq uint64
	mu  sync.Mutex
}

var (
	promptLogger *PromptLogger
	promptMu     sync.RWMutex
)

// PromptEntry is a single LLM call record.
type PromptEntry struct {
	Timestamp string         `json:"timestamp"`
	Model     string         `json:"model"`
	MsgSeq    int            `json:"msg_seq,omitempty"`    // message sequence from agent loop
	Phase     string         `json:"phase,omitempty"`      // "main", "analyser", "reflector", "digest"
	Messages  any            `json:"messages"`             // the messages array sent to LLM
	Tools     any            `json:"tools,omitempty"`      // tool definitions if any
	Options   map[string]any `json:"options,omitempty"`    // temperature, max_tokens, etc.
	Response  any            `json:"response,omitempty"`   // LLM response (filled after call)
	Error     string         `json:"error,omitempty"`      // error message if call failed
	Latency   string         `json:"latency_ms,omitempty"` // call duration
}

// EnablePromptLogging starts recording LLM calls to the given directory.
func EnablePromptLogging(dir string) error {
	promptMu.Lock()
	defer promptMu.Unlock()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create prompt log dir: %w", err)
	}

	promptLogger = &PromptLogger{dir: dir}
	Info("Prompt logging enabled: " + dir)
	return nil
}

// DisablePromptLogging stops recording LLM calls.
func DisablePromptLogging() {
	promptMu.Lock()
	defer promptMu.Unlock()
	promptLogger = nil
}

// IsPromptLoggingEnabled returns true if prompt logging is active.
func IsPromptLoggingEnabled() bool {
	promptMu.RLock()
	defer promptMu.RUnlock()
	return promptLogger != nil
}

// LogPrompt writes a prompt/response entry to the prompt log directory.
// Safe to call even if prompt logging is disabled (no-op).
func LogPrompt(entry *PromptEntry) {
	promptMu.RLock()
	pl := promptLogger
	promptMu.RUnlock()

	if pl == nil {
		return
	}

	pl.mu.Lock()
	pl.seq++
	seq := pl.seq
	pl.mu.Unlock()

	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		ErrorF("Failed to marshal prompt entry", map[string]any{"error": err.Error()})
		return
	}

	// Filename: 20260304T130000Z_0001_gpt-4o.json
	ts := time.Now().UTC().Format("20060102T150405Z")
	model := sanitizeFilename(entry.Model)
	if model == "" {
		model = "unknown"
	}
	filename := fmt.Sprintf("%s_%04d_%s.json", ts, seq, model)

	path := filepath.Join(pl.dir, filename)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		ErrorF("Failed to write prompt log", map[string]any{
			"path":  path,
			"error": err.Error(),
		})
	}
}

// sanitizeFilename replaces characters that are not safe for filenames.
func sanitizeFilename(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
