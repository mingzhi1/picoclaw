// Package mediaext wraps the media store as an extension with lifecycle management.
// It provides automatic cleanup of expired media files via a background goroutine.
package mediaext

import (
	"context"
	"time"

	"github.com/sipeed/picoclaw/pkg/extension"
	"github.com/sipeed/picoclaw/pkg/infra/media"
)

// Ext implements extension.Extension for media file management.
type Ext struct {
	store *media.FileMediaStore
	cfg   media.MediaCleanerConfig
}

func New() *Ext { return &Ext{} }

func (e *Ext) Name() string { return "media" }

func (e *Ext) Init(ctx extension.ExtensionContext) error {
	enabled, _ := ctx.Config["enabled"].(bool)
	maxAgeMin, _ := ctx.Config["max_age_minutes"].(float64)
	intervalMin, _ := ctx.Config["interval_minutes"].(float64)

	if maxAgeMin == 0 {
		maxAgeMin = 30
	}
	if intervalMin == 0 {
		intervalMin = 5
	}

	e.cfg = media.MediaCleanerConfig{
		Enabled:  enabled,
		MaxAge:   time.Duration(maxAgeMin) * time.Minute,
		Interval: time.Duration(intervalMin) * time.Minute,
	}

	if e.cfg.Enabled {
		e.store = media.NewFileMediaStoreWithCleanup(e.cfg)
	} else {
		e.store = media.NewFileMediaStore()
	}

	return nil
}

func (e *Ext) Start(_ context.Context) error {
	if e.store != nil {
		e.store.Start()
	}
	return nil
}

func (e *Ext) Stop() error {
	if e.store != nil {
		e.store.Stop()
	}
	return nil
}

// Store returns the underlying MediaStore for injection into the agent.
func (e *Ext) Store() media.MediaStore {
	return e.store
}
