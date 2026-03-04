// Package devices provides hardware peripheral access (I2C, SPI, GPIO, USB).
// This extension is primarily useful on embedded Linux (MaixCAM, Raspberry Pi, etc.)
// and degrades gracefully on other platforms.
package devices

import (
	"context"

	"github.com/sipeed/picoclaw/pkg/extension"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// Ext implements extension.Extension for hardware device interaction.
type Ext struct {
	enabled    bool
	monitorUSB bool
}

func New() *Ext { return &Ext{} }

func (e *Ext) Name() string { return "devices" }

func (e *Ext) Init(ctx extension.ExtensionContext) error {
	if v, ok := ctx.Config["enabled"].(bool); ok {
		e.enabled = v
	}
	if v, ok := ctx.Config["monitor_usb"].(bool); ok {
		e.monitorUSB = v
	}
	return nil
}

func (e *Ext) Start(ctx context.Context) error {
	if !e.enabled {
		return nil
	}
	// TODO: start USB monitor goroutine if e.monitorUSB
	return nil
}

func (e *Ext) Stop() error {
	return nil
}

// Tools returns device-related tools (I2C, SPI).
func (e *Ext) Tools() []tools.Tool {
	if !e.enabled {
		return nil
	}
	return []tools.Tool{
		tools.NewI2CTool(),
		tools.NewSPITool(),
	}
}
