// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mingzhi1/metaclaw/pkg/agent/routing"
	"github.com/mingzhi1/metaclaw/pkg/channels"
	"github.com/mingzhi1/metaclaw/pkg/core/bus"
	"github.com/mingzhi1/metaclaw/pkg/infra/config"
	"github.com/mingzhi1/metaclaw/pkg/infra/kvcache"
	"github.com/mingzhi1/metaclaw/pkg/infra/logger"
	"github.com/mingzhi1/metaclaw/pkg/infra/media"
	"github.com/mingzhi1/metaclaw/pkg/tools"
)

// ---------------------------------------------------------------------------
// Public API & utility methods
// ---------------------------------------------------------------------------

// Cache returns the persistent KV cache, or nil if unavailable.
func (al *AgentLoop) Cache() *kvcache.Store {
	return al.cache
}

func (al *AgentLoop) RegisterTool(tool tools.Tool) {
	for _, agentID := range al.registry.ListAgentIDs() {
		if agent, ok := al.registry.GetAgent(agentID); ok {
			agent.Tools.Register(tool)
		}
	}
}

func (al *AgentLoop) SetChannelManager(cm *channels.Manager) {
	al.channelManager = cm
	// Wire agent info into all agent Runtimes so /show, /list, /switch work.
	for _, id := range al.registry.ListAgentIDs() {
		if agent, ok := al.registry.GetAgent(id); ok && agent != nil && agent.Reflector != nil {
			agent.Reflector.SetAgentInfo(al.registry, cm)
			// Wire TurnStore so /tokens can query historical token usage.
			if al.turnStore != nil {
				agent.Reflector.SetTurnStore(al.turnStore)
			}
		}
	}
}


// SetMediaStore injects a MediaStore for media lifecycle management.
func (al *AgentLoop) SetMediaStore(s media.MediaStore) {
	al.mediaStore = s
}

// inferMediaType determines the media type ("image", "audio", "video", "file")
// from a filename and MIME content type.
func inferMediaType(filename, contentType string) string {
	ct := strings.ToLower(contentType)
	fn := strings.ToLower(filename)

	if strings.HasPrefix(ct, "image/") {
		return "image"
	}
	if strings.HasPrefix(ct, "audio/") || ct == "application/ogg" {
		return "audio"
	}
	if strings.HasPrefix(ct, "video/") {
		return "video"
	}

	// Fallback: infer from extension
	ext := filepath.Ext(fn)
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg":
		return "image"
	case ".mp3", ".wav", ".ogg", ".m4a", ".flac", ".aac", ".wma", ".opus":
		return "audio"
	case ".mp4", ".avi", ".mov", ".webm", ".mkv":
		return "video"
	}

	return "file"
}

// RecordLastChannel records the last active channel for this workspace.
// This uses the atomic state save mechanism to prevent data loss on crash.
func (al *AgentLoop) RecordLastChannel(channel string) error {
	if al.state == nil {
		return nil
	}
	return al.state.SetLastChannel(channel)
}

// RecordLastChatID records the last active chat ID for this workspace.
// This uses the atomic state save mechanism to prevent data loss on crash.
func (al *AgentLoop) RecordLastChatID(chatID string) error {
	if al.state == nil {
		return nil
	}
	return al.state.SetLastChatID(chatID)
}

func (al *AgentLoop) ProcessDirect(
	ctx context.Context,
	content, sessionKey string,
) (string, error) {
	return al.ProcessDirectWithChannel(ctx, content, sessionKey, "cli", "direct")
}

func (al *AgentLoop) ProcessDirectWithChannel(
	ctx context.Context,
	content, sessionKey, channel, chatID string,
) (string, error) {
	msg := bus.InboundMessage{
		Channel:    channel,
		SenderID:   "cron",
		ChatID:     chatID,
		Content:    content,
		SessionKey: sessionKey,
	}

	return al.processMessage(ctx, msg)
}

// ProcessHeartbeat processes a heartbeat request without session history.
// Each heartbeat is independent and doesn't accumulate context.
func (al *AgentLoop) ProcessHeartbeat(
	ctx context.Context,
	content, channel, chatID string,
) (string, error) {
	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		return "", fmt.Errorf("no default agent for heartbeat")
	}
	return al.runAgentLoop(ctx, agent, processOptions{
		SessionKey:      "heartbeat",
		Channel:         channel,
		ChatID:          chatID,
		UserMessage:     content,
		DefaultResponse: defaultResponse,
		EnableSummary:   false,
		SendResponse:    false,
		NoHistory:       true, // Don't load session history for heartbeat
	})
}

// ---------------------------------------------------------------------------
// Routing & config helpers
// ---------------------------------------------------------------------------

// extractPeer extracts the routing peer from the inbound message's structured Peer field.
func extractPeer(msg bus.InboundMessage) *routing.RoutePeer {
	if msg.Peer.Kind == "" {
		return nil
	}
	peerID := msg.Peer.ID
	if peerID == "" {
		if msg.Peer.Kind == "direct" {
			peerID = msg.SenderID
		} else {
			peerID = msg.ChatID
		}
	}
	return &routing.RoutePeer{Kind: msg.Peer.Kind, ID: peerID}
}

// extractParentPeer extracts the parent peer (reply-to) from inbound message metadata.
func extractParentPeer(msg bus.InboundMessage) *routing.RoutePeer {
	parentKind := msg.Metadata["parent_peer_kind"]
	parentID := msg.Metadata["parent_peer_id"]
	if parentKind == "" || parentID == "" {
		return nil
	}
	return &routing.RoutePeer{Kind: parentKind, ID: parentID}
}

// resolveSttConfig builds the config map for the voice extension by looking up
// the stt_model entry in config.ModelList.
// Returns a map suitable for extension.ExtensionContext.Config.
// If stt_model is empty or not found, returns a map without api_base so the
// voice extension gracefully disables itself.
func resolveSttConfig(cfg *config.Config) map[string]any {
	sttModel := cfg.Agents.Defaults.GetSTTModel()
	result := map[string]any{
		"stt_model": sttModel,
	}
	if sttModel == "" {
		return result
	}

	mc, err := cfg.GetModelConfig(sttModel)
	if err != nil {
		logger.WarnCF("agent", "stt_model not found in model_list — STT disabled",
			map[string]any{"stt_model": sttModel, "error": err.Error()})
		return result
	}

	result["api_base"] = mc.APIBase
	result["api_key"] = mc.APIKey
	result["model"] = mc.Model
	return result
}

// ---------------------------------------------------------------------------
// System prompt enrichment — shared by Path A (Instant Memory) and Path B (Legacy)
// ---------------------------------------------------------------------------

// enrichSystemPrompt appends CoT template/supplement, checkpoint plan/progress,
// skill execution plan, and memory context to the base system prompt.
// Returns the enriched prompt string.
func (al *AgentLoop) enrichSystemPrompt(
	base string,
	agent *AgentInstance,
	result AnalyseResult,
	channelKey string,
	userMsg string,
) string {
	var sb strings.Builder
	sb.WriteString(base)

	// 1. CoT injection: template + task-specific supplement.
	if result.CotID != "" && agent.Analyser != nil {
		if reg := agent.Analyser.GetCotRegistry(); reg != nil {
			tpl := reg.Get(result.CotID)
			if tpl.Prompt != "" {
				sb.WriteString("\n\n---\n\n")
				sb.WriteString(tpl.Prompt)
			}
		}
		if result.CotPrompt != "" {
			sb.WriteString("\n\n### Task-Specific Notes\n\n")
			sb.WriteString(result.CotPrompt)
		}
	} else if result.CotPrompt != "" {
		sb.WriteString("\n\n---\n\n## Thinking Strategy\n\n")
		sb.WriteString(result.CotPrompt)
	}

	// 2. Checkpoint injection: new plan or previous progress.
	if al.checkpoints != nil && len(result.Checkpoints) > 0 {
		al.checkpoints.Begin(channelKey, result.Checkpoints)
		if checklist := al.checkpoints.FormatChecklist(channelKey); checklist != "" {
			sb.WriteString("\n\n---\n\n")
			sb.WriteString(checklist)
		}
	} else if al.checkpoints != nil {
		if progress := al.checkpoints.FormatProgress(channelKey); progress != "" {
			sb.WriteString("\n\n---\n\n")
			sb.WriteString(progress)
		}
	}

	// 3. Skill matching: inject tool execution plan.
	if matched := agent.ContextBuilder.MatchSkillByMessage(userMsg); matched != nil {
		if plan := FormatToolSteps(matched.ToolSteps, matched.Path); plan != "" {
			sb.WriteString("\n\n---\n\n")
			sb.WriteString(plan)
			logger.InfoCF("agent", "Injected tool execution plan from skill",
				map[string]any{
					"skill":      matched.Name,
					"steps":      len(matched.ToolSteps),
					"skill_path": matched.Path,
				})
		}
	}

	// NOTE: Memory context is NOT injected here to avoid duplication.
	// - BuildSystemPrompt() already includes full memory via GetMemoryContext().
	// - Path A injects tag-matched memory via BuildPhase2Messages (separate user message).
	// - Path B's full memory is already in the system prompt from BuildMessages().

	return sb.String()
}
