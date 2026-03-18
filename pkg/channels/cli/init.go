package cli

import (
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/core/bus"
	"github.com/sipeed/picoclaw/pkg/infra/config"
)

func init() {
	channels.RegisterFactory("cli", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewCLIChannel(cfg.Channels.CLI, b)
	})
}
