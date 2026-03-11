package agent

import (
	"strings"
	"testing"
)

func TestSanitizeReply_Short(t *testing.T) {
	input := "Hello, world!"
	got := sanitizeReply(input)
	if got != input {
		t.Errorf("short reply should be unchanged, got %q", got)
	}
}

func TestSanitizeReply_ExactLimit(t *testing.T) {
	input := strings.Repeat("x", maxReplyLen)
	got := sanitizeReply(input)
	if got != input {
		t.Errorf("exact-limit reply should be unchanged, len=%d", len(got))
	}
}

func TestSanitizeReply_Long(t *testing.T) {
	input := strings.Repeat("x", maxReplyLen+500)
	got := sanitizeReply(input)
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Errorf("long reply should be truncated, got suffix %q", got[len(got)-20:])
	}
	if len(got) > maxReplyLen+50 {
		t.Errorf("truncated reply too long: %d", len(got))
	}
}

func TestSanitizeUserMsg_Short(t *testing.T) {
	input := "How do I test Go code?"
	got := sanitizeUserMsg(input)
	if got != input {
		t.Errorf("short msg should be unchanged, got %q", got)
	}
}

func TestSanitizeUserMsg_Long(t *testing.T) {
	input := strings.Repeat("a", maxUserMsgLen+200)
	got := sanitizeUserMsg(input)
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Errorf("long msg should be truncated, got suffix %q", got[len(got)-20:])
	}
}

func TestSanitizeReply_Empty(t *testing.T) {
	got := sanitizeReply("")
	if got != "" {
		t.Errorf("empty reply should stay empty, got %q", got)
	}
}
