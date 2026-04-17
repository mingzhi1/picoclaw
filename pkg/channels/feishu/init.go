package feishu

import (
	"github.com/mingzhi1/metaclaw/pkg/core/bus"
	"github.com/mingzhi1/metaclaw/pkg/channels"
	"github.com/mingzhi1/metaclaw/pkg/infra/config"
)

func init() {
	channels.RegisterFactory("feishu", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewFeishuChannel(cfg.Channels.Feishu, b)
	})
}
