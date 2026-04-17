package telegram

import (
	"github.com/mingzhi1/metaclaw/pkg/core/bus"
	"github.com/mingzhi1/metaclaw/pkg/channels"
	"github.com/mingzhi1/metaclaw/pkg/infra/config"
)

func init() {
	channels.RegisterFactory("telegram", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewTelegramChannel(cfg, b)
	})
}
