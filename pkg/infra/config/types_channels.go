package config

import "encoding/json"

// --- Channel configuration types ---

// ChannelsConfig holds configuration for all communication channels.
type ChannelsConfig struct {
	Telegram TelegramConfig   `json:"telegram"`
	Feishu   FeishuConfig     `json:"feishu"`
	CLI      CLIChannelConfig `json:"cli"`
}

// HasEnabled returns true if at least one channel is enabled.
func (c ChannelsConfig) HasEnabled() bool {
	return c.Telegram.Enabled || c.Feishu.Enabled || c.CLI.Enabled
}

// MarshalJSON only serializes enabled channels.
func (c ChannelsConfig) MarshalJSON() ([]byte, error) {
	m := make(map[string]any)
	if c.Telegram.Enabled {
		m["telegram"] = c.Telegram
	}
	if c.Feishu.Enabled {
		m["feishu"] = c.Feishu
	}
	if c.CLI.Enabled {
		m["cli"] = c.CLI
	}
	return json.Marshal(m)
}

// CLIChannelConfig configures the WebSocket CLI channel.
type CLIChannelConfig struct {
	Enabled        bool                `json:"enabled,omitempty"          env:"PICOCLAW_CHANNELS_CLI_ENABLED"`
	Token          string              `json:"token,omitempty"            env:"PICOCLAW_CHANNELS_CLI_TOKEN"`
	AllowFrom      FlexibleStringSlice `json:"allow_from,omitempty"       env:"PICOCLAW_CHANNELS_CLI_ALLOW_FROM"`
	AllowOrigins   []string            `json:"allow_origins,omitempty"`
	AllowTokenQuery bool               `json:"allow_token_query,omitempty"`
	MaxConnections int                 `json:"max_connections,omitempty"`
	ReadTimeout    int                 `json:"read_timeout,omitempty"`
	PingInterval   int                 `json:"ping_interval,omitempty"`
	Placeholder    PlaceholderConfig   `json:"placeholder,omitempty"`
}

// GroupTriggerConfig controls when the bot responds in group chats.
type GroupTriggerConfig struct {
	MentionOnly bool     `json:"mention_only,omitempty"`
	Prefixes    []string `json:"prefixes,omitempty"`
}

// TypingConfig controls typing indicator behavior.
type TypingConfig struct {
	Enabled bool `json:"enabled,omitempty"`
}

// PlaceholderConfig controls placeholder message behavior.
type PlaceholderConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Text    string `json:"text,omitempty"`
}

type TelegramConfig struct {
	Enabled            bool                `json:"enabled,omitempty"                 env:"PICOCLAW_CHANNELS_TELEGRAM_ENABLED"`
	Token              string              `json:"token,omitempty"                   env:"PICOCLAW_CHANNELS_TELEGRAM_TOKEN"`
	Proxy              string              `json:"proxy,omitempty"                   env:"PICOCLAW_CHANNELS_TELEGRAM_PROXY"`
	AllowFrom          FlexibleStringSlice `json:"allow_from,omitempty"              env:"PICOCLAW_CHANNELS_TELEGRAM_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig   `json:"group_trigger,omitempty"`
	Typing             TypingConfig        `json:"typing,omitempty"`
	Placeholder        PlaceholderConfig   `json:"placeholder,omitempty"`
	ReasoningChannelID string              `json:"reasoning_channel_id,omitempty"    env:"PICOCLAW_CHANNELS_TELEGRAM_REASONING_CHANNEL_ID"`
}

type FeishuConfig struct {
	Enabled            bool                `json:"enabled,omitempty"                 env:"PICOCLAW_CHANNELS_FEISHU_ENABLED"`
	AppID              string              `json:"app_id,omitempty"                  env:"PICOCLAW_CHANNELS_FEISHU_APP_ID"`
	AppSecret          string              `json:"app_secret,omitempty"              env:"PICOCLAW_CHANNELS_FEISHU_APP_SECRET"`
	EncryptKey         string              `json:"encrypt_key,omitempty"             env:"PICOCLAW_CHANNELS_FEISHU_ENCRYPT_KEY"`
	VerificationToken  string              `json:"verification_token,omitempty"      env:"PICOCLAW_CHANNELS_FEISHU_VERIFICATION_TOKEN"`
	AllowFrom          FlexibleStringSlice `json:"allow_from,omitempty"              env:"PICOCLAW_CHANNELS_FEISHU_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig   `json:"group_trigger,omitempty"`
	ReasoningChannelID string              `json:"reasoning_channel_id,omitempty"    env:"PICOCLAW_CHANNELS_FEISHU_REASONING_CHANNEL_ID"`
}
