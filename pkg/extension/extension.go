// Package extension provides a unified lifecycle framework for optional
// PicoClaw capabilities — devices, media, voice, and future modules.
//
// Design:
//   - Each extension implements the Extension interface (Name, Init, Start, Stop).
//   - Extensions can optionally provide tools (ToolProvider) or middleware (Hook).
//   - The Manager handles ordered startup/shutdown and exposes registered tools.
//   - Extensions are registered in main/gateway code; the agent loop queries
//     the Manager for tools and capabilities at runtime.
//
// Example:
//
//	mgr := extension.NewManager()
//	mgr.Register(&devices.Extension{})
//	mgr.Register(&media.Extension{})
//	mgr.Register(&voice.Extension{})
//	mgr.InitAll(cfg)
//	mgr.StartAll(ctx)
//	defer mgr.StopAll()
package extension

import (
	"context"
	"fmt"
	"sync"

	"github.com/sipeed/picoclaw/pkg/infra/logger"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// Extension is the interface that every optional module must implement.
type Extension interface {
	// Name returns a unique identifier (e.g. "devices", "media", "voice").
	Name() string

	// Init performs one-time setup using the provided context.
	// Called synchronously in registration order. Return non-nil to abort.
	Init(ctx ExtensionContext) error

	// Start begins background work (goroutines, listeners, etc.).
	// The provided context is cancelled on shutdown.
	Start(ctx context.Context) error

	// Stop performs graceful shutdown. Called in reverse registration order.
	Stop() error
}

// ToolProvider is an optional interface for extensions that expose tools.
// If an Extension also implements ToolProvider, the Manager will collect
// its tools and make them available to the agent's ToolRegistry.
type ToolProvider interface {
	// Tools returns the tools this extension provides.
	// Called after Init, before Start.
	Tools() []tools.Tool
}

// ExtensionContext provides dependencies to extensions during Init.
type ExtensionContext struct {
	// Workspace is the agent's workspace directory.
	Workspace string

	// Config is the raw extension-specific configuration (from config.json).
	// Each extension interprets its own section.
	Config map[string]any
}

// Manager manages the lifecycle of registered extensions.
type Manager struct {
	mu         sync.RWMutex
	extensions []Extension
	started    bool
}

// NewManager creates a new extension manager.
func NewManager() *Manager {
	return &Manager{}
}

// Register adds an extension. Must be called before InitAll.
// Panics if called after StartAll.
func (m *Manager) Register(ext Extension) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		panic(fmt.Sprintf("extension: cannot register %q after StartAll", ext.Name()))
	}

	// Check for duplicates.
	for _, e := range m.extensions {
		if e.Name() == ext.Name() {
			logger.WarnCF("extension", "Duplicate extension ignored",
				map[string]any{"name": ext.Name()})
			return
		}
	}

	m.extensions = append(m.extensions, ext)
	logger.DebugCF("extension", "Registered",
		map[string]any{"name": ext.Name()})
}

// InitAll initialises all extensions in registration order with the same context.
// Stops and returns on the first error.
func (m *Manager) InitAll(ctx ExtensionContext) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, ext := range m.extensions {
		if err := ext.Init(ctx); err != nil {
			return fmt.Errorf("extension %q init: %w", ext.Name(), err)
		}
		logger.InfoCF("extension", "Initialised",
			map[string]any{"name": ext.Name()})
	}
	return nil
}

// InitOne initialises a single extension by name with the provided context.
// Returns an error if the extension is not found.
func (m *Manager) InitOne(name string, ctx ExtensionContext) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, ext := range m.extensions {
		if ext.Name() == name {
			if err := ext.Init(ctx); err != nil {
				return fmt.Errorf("extension %q init: %w", name, err)
			}
			logger.InfoCF("extension", "Initialised",
				map[string]any{"name": name})
			return nil
		}
	}
	return fmt.Errorf("extension %q not registered", name)
}

// StartAll starts all extensions in registration order.
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, ext := range m.extensions {
		if err := ext.Start(ctx); err != nil {
			return fmt.Errorf("extension %q start: %w", ext.Name(), err)
		}
		logger.InfoCF("extension", "Started",
			map[string]any{"name": ext.Name()})
	}
	return nil
}

// StopAll stops all extensions in reverse registration order.
// All extensions are stopped even if some return errors.
func (m *Manager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := len(m.extensions) - 1; i >= 0; i-- {
		ext := m.extensions[i]
		if err := ext.Stop(); err != nil {
			logger.WarnCF("extension", "Stop error",
				map[string]any{"name": ext.Name(), "error": err.Error()})
		} else {
			logger.InfoCF("extension", "Stopped",
				map[string]any{"name": ext.Name()})
		}
	}
}

// CollectTools gathers tools from all extensions that implement ToolProvider.
// Call after InitAll, before or after StartAll.
func (m *Manager) CollectTools() []tools.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []tools.Tool
	for _, ext := range m.extensions {
		if tp, ok := ext.(ToolProvider); ok {
			all = append(all, tp.Tools()...)
		}
	}
	return all
}

// Get returns a registered extension by name, or nil if not found.
func (m *Manager) Get(name string) Extension {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, ext := range m.extensions {
		if ext.Name() == name {
			return ext
		}
	}
	return nil
}

// List returns the names of all registered extensions.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, len(m.extensions))
	for i, ext := range m.extensions {
		names[i] = ext.Name()
	}
	return names
}
