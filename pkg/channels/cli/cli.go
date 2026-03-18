package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/core/bus"
	"github.com/sipeed/picoclaw/pkg/core/identity"
	"github.com/sipeed/picoclaw/pkg/infra/config"
	"github.com/sipeed/picoclaw/pkg/infra/logger"
)

// cliConn represents a single WebSocket connection.
type cliConn struct {
	id        string
	conn      *websocket.Conn
	sessionID string
	writeMu   sync.Mutex
	closed    atomic.Bool
}

func (cc *cliConn) writeJSON(v any) error {
	if cc.closed.Load() {
		return fmt.Errorf("connection closed")
	}
	cc.writeMu.Lock()
	defer cc.writeMu.Unlock()
	return cc.conn.WriteJSON(v)
}

func (cc *cliConn) close() {
	if cc.closed.CompareAndSwap(false, true) {
		cc.conn.Close()
	}
}

// CLIChannel implements the WebSocket CLI channel.
type CLIChannel struct {
	*channels.BaseChannel
	config      config.CLIChannelConfig
	upgrader    websocket.Upgrader
	connections sync.Map // connID → *cliConn
	connCount   atomic.Int32
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewCLIChannel creates a new CLI WebSocket channel.
func NewCLIChannel(cfg config.CLIChannelConfig, messageBus *bus.MessageBus) (*CLIChannel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("cli token is required")
	}

	base := channels.NewBaseChannel("cli", cfg, messageBus, cfg.AllowFrom)

	allowOrigins := cfg.AllowOrigins
	checkOrigin := func(r *http.Request) bool {
		if len(allowOrigins) == 0 {
			return true
		}
		origin := r.Header.Get("Origin")
		for _, allowed := range allowOrigins {
			if allowed == "*" || allowed == origin {
				return true
			}
		}
		return false
	}

	return &CLIChannel{
		BaseChannel: base,
		config:      cfg,
		upgrader: websocket.Upgrader{
			CheckOrigin:     checkOrigin,
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}, nil
}

func (c *CLIChannel) Start(ctx context.Context) error {
	logger.InfoC("cli", "Starting CLI WebSocket channel")
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.SetRunning(true)
	logger.InfoC("cli", "CLI WebSocket channel started")
	return nil
}

func (c *CLIChannel) Stop(ctx context.Context) error {
	logger.InfoC("cli", "Stopping CLI WebSocket channel")
	c.SetRunning(false)
	c.connections.Range(func(key, value any) bool {
		if cc, ok := value.(*cliConn); ok {
			cc.close()
		}
		c.connections.Delete(key)
		return true
	})
	if c.cancel != nil {
		c.cancel()
	}
	logger.InfoC("cli", "CLI WebSocket channel stopped")
	return nil
}

func (c *CLIChannel) WebhookPath() string { return "/cli/" }

func (c *CLIChannel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/cli")
	switch {
	case path == "/ws" || path == "/ws/":
		c.handleWebSocket(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (c *CLIChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}
	outMsg := newMessage(TypeMessageCreate, map[string]any{"content": msg.Content})
	return c.broadcastToSession(msg.ChatID, outMsg)
}

func (c *CLIChannel) EditMessage(ctx context.Context, chatID string, messageID string, content string) error {
	outMsg := newMessage(TypeMessageUpdate, map[string]any{
		"message_id": messageID,
		"content":    content,
	})
	return c.broadcastToSession(chatID, outMsg)
}

func (c *CLIChannel) StartTyping(ctx context.Context, chatID string) (func(), error) {
	startMsg := newMessage(TypeTypingStart, nil)
	if err := c.broadcastToSession(chatID, startMsg); err != nil {
		return func() {}, err
	}
	return func() {
		stopMsg := newMessage(TypeTypingStop, nil)
		c.broadcastToSession(chatID, stopMsg)
	}, nil
}

func (c *CLIChannel) SendPlaceholder(ctx context.Context, chatID string) (string, error) {
	if !c.config.Placeholder.Enabled {
		return "", nil
	}
	text := c.config.Placeholder.Text
	if text == "" {
		text = "Thinking... 💭"
	}
	msgID := uuid.New().String()
	outMsg := newMessage(TypeMessageCreate, map[string]any{
		"content":    text,
		"message_id": msgID,
	})
	if err := c.broadcastToSession(chatID, outMsg); err != nil {
		return "", err
	}
	return msgID, nil
}

func (c *CLIChannel) broadcastToSession(chatID string, msg CLIMessage) error {
	sessionID := strings.TrimPrefix(chatID, "cli:")
	msg.SessionID = sessionID
	var sent bool
	c.connections.Range(func(key, value any) bool {
		cc, ok := value.(*cliConn)
		if !ok {
			return true
		}
		if cc.sessionID == sessionID {
			if err := cc.writeJSON(msg); err != nil {
				logger.DebugCF("cli", "Write to connection failed", map[string]any{
					"conn_id": cc.id, "error": err.Error(),
				})
			} else {
				sent = true
			}
		}
		return true
	})
	if !sent {
		return fmt.Errorf("no active connections for session %s: %w", sessionID, channels.ErrSendFailed)
	}
	return nil
}

func (c *CLIChannel) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !c.IsRunning() {
		http.Error(w, "channel not running", http.StatusServiceUnavailable)
		return
	}
	if !c.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	maxConns := c.config.MaxConnections
	if maxConns <= 0 {
		maxConns = 100
	}
	if int(c.connCount.Load()) >= maxConns {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.ErrorCF("cli", "WebSocket upgrade failed", map[string]any{"error": err.Error()})
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	cc := &cliConn{id: uuid.New().String(), conn: conn, sessionID: sessionID}
	c.connections.Store(cc.id, cc)
	c.connCount.Add(1)
	logger.InfoCF("cli", "WebSocket client connected", map[string]any{
		"conn_id": cc.id, "session_id": sessionID,
	})
	go c.readLoop(cc)
}

func (c *CLIChannel) authenticate(r *http.Request) bool {
	token := c.config.Token
	if token == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
		if after == token {
			return true
		}
	}
	if c.config.AllowTokenQuery {
		if r.URL.Query().Get("token") == token {
			return true
		}
	}
	return false
}

func (c *CLIChannel) readLoop(cc *cliConn) {
	defer func() {
		cc.close()
		c.connections.Delete(cc.id)
		c.connCount.Add(-1)
		logger.InfoCF("cli", "WebSocket client disconnected", map[string]any{
			"conn_id": cc.id, "session_id": cc.sessionID,
		})
	}()
	readTimeout := time.Duration(c.config.ReadTimeout) * time.Second
	if readTimeout <= 0 {
		readTimeout = 60 * time.Second
	}
	_ = cc.conn.SetReadDeadline(time.Now().Add(readTimeout))
	cc.conn.SetPongHandler(func(appData string) error {
		_ = cc.conn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})
	pingInterval := time.Duration(c.config.PingInterval) * time.Second
	if pingInterval <= 0 {
		pingInterval = 30 * time.Second
	}
	go c.pingLoop(cc, pingInterval)
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}
		_, rawMsg, err := cc.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logger.DebugCF("cli", "WebSocket read error", map[string]any{
					"conn_id": cc.id, "error": err.Error(),
				})
			}
			return
		}
		_ = cc.conn.SetReadDeadline(time.Now().Add(readTimeout))
		var msg CLIMessage
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			errMsg := newError("invalid_message", "failed to parse message")
			cc.writeJSON(errMsg)
			continue
		}
		c.handleMessage(cc, msg)
	}
}

func (c *CLIChannel) pingLoop(cc *cliConn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if cc.closed.Load() {
				return
			}
			cc.writeMu.Lock()
			err := cc.conn.WriteMessage(websocket.PingMessage, nil)
			cc.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (c *CLIChannel) handleMessage(cc *cliConn, msg CLIMessage) {
	switch msg.Type {
	case TypePing:
		pong := newMessage(TypePong, nil)
		pong.ID = msg.ID
		cc.writeJSON(pong)
	case TypeMessageSend:
		c.handleMessageSend(cc, msg)
	default:
		errMsg := newError("unknown_type", fmt.Sprintf("unknown message type: %s", msg.Type))
		cc.writeJSON(errMsg)
	}
}

func (c *CLIChannel) handleMessageSend(cc *cliConn, msg CLIMessage) {
	content, _ := msg.Payload["content"].(string)
	if strings.TrimSpace(content) == "" {
		errMsg := newError("empty_content", "message content is empty")
		cc.writeJSON(errMsg)
		return
	}
	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = cc.sessionID
	}
	chatID := "cli:" + sessionID
	senderID := "cli-user"
	peer := bus.Peer{Kind: "direct", ID: "cli:" + sessionID}
	metadata := map[string]string{
		"platform":   "cli",
		"session_id": sessionID,
		"conn_id":    cc.id,
	}
	logger.DebugCF("cli", "Received message", map[string]any{
		"session_id": sessionID,
		"preview":    truncate(content, 50),
	})
	sender := bus.SenderInfo{
		Platform:    "cli",
		PlatformID:  senderID,
		CanonicalID: identity.BuildCanonicalID("cli", senderID),
	}
	if !c.IsAllowedSender(sender) {
		return
	}
	c.HandleMessage(c.ctx, peer, msg.ID, senderID, chatID, content, nil, metadata, sender)
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
