package cli

import (
	"github.com/mingzhi1/metaclaw/pkg/channels"
	"github.com/mingzhi1/metaclaw/pkg/core/bus"
	"github.com/mingzhi1/metaclaw/pkg/infra/config"
)

func init() {
	channels.RegisterFactory("cli", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewCLIChannel(cfg.Channels.CLI, b)
	})
}
