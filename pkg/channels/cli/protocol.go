package cli

import "time"

// Protocol message types.
const (
	// TypeMessageSend is sent from client to server.
	TypeMessageSend = "message.send"
	TypeMediaSend   = "media.send"
	TypePing        = "ping"

	// TypeMessageCreate is sent from server to client.
	TypeMessageCreate = "message.create"
	TypeMessageUpdate = "message.update"
	TypeMediaCreate   = "media.create"
	TypeTypingStart   = "typing.start"
	TypeTypingStop    = "typing.stop"
	TypeError         = "error"
	TypePong          = "pong"
)

// CLIMessage is the wire format for all CLI Protocol messages.
type CLIMessage struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// newMessage creates a CLIMessage with the given type and payload.
func newMessage(msgType string, payload map[string]any) CLIMessage {
	return CLIMessage{
		Type:      msgType,
		Timestamp: time.Now().UnixMilli(),
		Payload:   payload,
	}
}

// newError creates an error CLIMessage.
func newError(code, message string) CLIMessage {
	return newMessage(TypeError, map[string]any{
		"code":    code,
		"message": message,
	})
}
