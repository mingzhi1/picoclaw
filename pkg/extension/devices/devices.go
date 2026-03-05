// Package devices provides hardware peripheral access (I2C, SPI, GPIO, USB).
// This extension wraps pkg/infra/devices.Service (USB monitor) and provides
// hardware tools (I2C, SPI) to the agent.
//
// On embedded Linux: USB hotplug monitor + I2C/SPI tools.
// On other platforms: degrades gracefully (tools return "unsupported").
package devices

import (
	"context"

	"github.com/sipeed/picoclaw/pkg/core/bus"
	infradevices "github.com/sipeed/picoclaw/pkg/infra/devices"
	"github.com/sipeed/picoclaw/pkg/infra/store"
	"github.com/sipeed/picoclaw/pkg/core/state"
	"github.com/sipeed/picoclaw/pkg/extension"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// Ext implements extension.Extension for hardware device interaction.
type Ext struct {
	enabled    bool
	monitorUSB bool
	service    *infradevices.Service
	msgBus     *bus.MessageBus
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
	if bus, ok := ctx.Config["bus"].(*bus.MessageBus); ok {
		e.msgBus = bus
	}

	if e.enabled {
		db, err := store.Open(ctx.Workspace)
		var stateMgr *state.Manager
		if err == nil {
			stateMgr = state.NewManager(db)
		}
		e.service = infradevices.NewService(infradevices.Config{
			Enabled:    e.enabled,
			MonitorUSB: e.monitorUSB,
		}, stateMgr)
		if e.msgBus != nil {
			e.service.SetBus(e.msgBus)
		}
	}

	return nil
}

func (e *Ext) Start(ctx context.Context) error {
	if e.service != nil {
		return e.service.Start(ctx)
	}
	return nil
}

func (e *Ext) Stop() error {
	if e.service != nil {
		e.service.Stop()
	}
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
