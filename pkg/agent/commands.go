// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"fmt"
	"strings"
)

// ===========================================================================
// Built-in slash commands
// ===========================================================================

// --- /help ------------------------------------------------------------------

func (r *Reflector) cmdHelp(_ []string, _ *MemoryStore) string {
	var sb strings.Builder
	sb.WriteString("📖 **Available Commands**\n\n")

	r.mu.RLock()
	for _, cmd := range r.commands {
		fmt.Fprintf(&sb, "• `%s` — %s\n", cmd.Usage, cmd.Description)
	}
	r.mu.RUnlock()

	return sb.String()
}

// --- /memory ----------------------------------------------------------------

func cmdMemory(args []string, memory *MemoryStore) string {
	if len(args) == 0 {
		return "Usage: /memory [list|add|delete|edit|search|stats]\n" +
			"  /memory list           — show recent memories\n" +
			"  /memory add <text> #tags — add a memory\n" +
			"  /memory delete <id>    — delete a memory\n" +
			"  /memory edit <id> <text> — edit a memory\n" +
			"  /memory search <query> — search by tags\n" +
			"  /memory stats          — memory statistics"
	}

	switch args[0] {
	case "list":
		limit := 10
		entries, err := memory.ListEntries(limit)
		if err != nil {
			return fmt.Sprintf("❌ Error: %v", err)
		}
		if len(entries) == 0 {
			return "📭 No memories stored yet."
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "📝 **Recent Memories** (%d)\n\n", len(entries))
		for _, e := range entries {
			tags := ""
			if len(e.Tags) > 0 {
				tags = " [" + strings.Join(e.Tags, ", ") + "]"
			}
			preview := e.Content
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			fmt.Fprintf(&sb, "• #%d%s: %s\n", e.ID, tags, preview)
		}
		return sb.String()

	case "add":
		if len(args) < 2 {
			return "Usage: /memory add <text> #tag1 #tag2"
		}
		// Separate content from #tags.
		var content []string
		var tags []string
		for _, a := range args[1:] {
			if strings.HasPrefix(a, "#") {
				tags = append(tags, strings.TrimPrefix(a, "#"))
			} else {
				content = append(content, a)
			}
		}
		text := strings.Join(content, " ")
		if text == "" {
			return "❌ Memory content cannot be empty"
		}
		id, err := memory.AddEntry(text, tags)
		if err != nil {
			return fmt.Sprintf("❌ Failed to add: %v", err)
		}
		return fmt.Sprintf("✅ Memory #%d saved (tags: %v)", id, tags)

	case "search":
		if len(args) < 2 {
			return "Usage: /memory search <tag1> [tag2] ..."
		}
		entries, err := memory.SearchByAnyTag(args[1:])
		if err != nil {
			return fmt.Sprintf("❌ Error: %v", err)
		}
		if len(entries) == 0 {
			return fmt.Sprintf("🔍 No memories found for tags: %v", args[1:])
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "🔍 **Found %d memories**\n\n", len(entries))
		for _, e := range entries {
			tags := ""
			if len(e.Tags) > 0 {
				tags = " [" + strings.Join(e.Tags, ", ") + "]"
			}
			preview := e.Content
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			fmt.Fprintf(&sb, "• #%d%s: %s\n", e.ID, tags, preview)
		}
		return sb.String()

	case "stats":
		tags, _ := memory.ListAllTags()
		entries, _ := memory.ListEntries(9999)
		var sb strings.Builder
		sb.WriteString("📊 **Memory Stats**\n")
		fmt.Fprintf(&sb, "• Total entries: %d\n", len(entries))
		fmt.Fprintf(&sb, "• Total tags: %d\n", len(tags))
		if len(tags) > 0 {
			preview := tags
			if len(preview) > 20 {
				preview = preview[:20]
			}
			fmt.Fprintf(&sb, "• Tags: %s", strings.Join(preview, ", "))
			if len(tags) > 20 {
				fmt.Fprintf(&sb, " ... (+%d more)", len(tags)-20)
			}
			sb.WriteString("\n")
		}
		return sb.String()

	case "delete":
		if len(args) < 2 {
			return "Usage: /memory delete <id>"
		}
		var id int64
		if _, err := fmt.Sscanf(args[1], "%d", &id); err != nil {
			return "❌ Invalid ID. Usage: /memory delete <id>"
		}
		if err := memory.DeleteEntry(id); err != nil {
			return fmt.Sprintf("❌ Failed: %v", err)
		}
		return fmt.Sprintf("✅ Memory #%d deleted", id)

	case "edit":
		if len(args) < 3 {
			return "Usage: /memory edit <id> <new content> #tags"
		}
		var id int64
		if _, err := fmt.Sscanf(args[1], "%d", &id); err != nil {
			return "❌ Invalid ID. Usage: /memory edit <id> <text>"
		}
		var content []string
		var tags []string
		for _, a := range args[2:] {
			if strings.HasPrefix(a, "#") {
				tags = append(tags, strings.TrimPrefix(a, "#"))
			} else {
				content = append(content, a)
			}
		}
		text := strings.Join(content, " ")
		if text == "" {
			return "❌ Content cannot be empty"
		}
		if err := memory.UpdateEntry(id, text, tags); err != nil {
			return fmt.Sprintf("❌ Failed: %v", err)
		}
		return fmt.Sprintf("✅ Memory #%d updated", id)

	default:
		return fmt.Sprintf("Unknown subcommand: %s. Use /memory for help.", args[0])
	}
}

// --- /cot -------------------------------------------------------------------

func cmdCot(args []string, memory *MemoryStore) string {
	if len(args) == 0 {
		return "Usage: /cot [feedback|stats|history]\n" +
			"  /cot feedback <1|0|-1> — rate last CoT strategy\n" +
			"  /cot stats             — show CoT performance\n" +
			"  /cot history [N]       — show recent CoT usage"
	}

	switch args[0] {
	case "feedback":
		if len(args) < 2 {
			return "Usage: /cot feedback <1|0|-1>"
		}
		var score int
		switch args[1] {
		case "1", "+1", "good":
			score = 1
		case "-1", "bad":
			score = -1
		case "0", "neutral":
			score = 0
		default:
			return "❌ Score must be 1 (good), 0 (neutral), or -1 (bad)"
		}
		if err := memory.UpdateLatestCotFeedback(score); err != nil {
			return fmt.Sprintf("❌ Failed: %v", err)
		}
		labels := map[int]string{1: "👍 good", 0: "😐 neutral", -1: "👎 bad"}
		return fmt.Sprintf("✅ CoT feedback recorded: %s", labels[score])

	case "stats":
		stats, err := memory.GetCotStats(30)
		if err != nil || len(stats) == 0 {
			return "📊 No CoT usage data yet."
		}
		var sb strings.Builder
		sb.WriteString("📊 **CoT Stats (last 30 days)**\n\n")
		for _, s := range stats {
			scoreLabel := "neutral"
			if s.AvgScore > 0.3 {
				scoreLabel = "good"
			} else if s.AvgScore < -0.3 {
				scoreLabel = "poor"
			}
			fmt.Fprintf(&sb, "• Intent '%s': %d uses, avg=%s (%.1f)\n",
				s.Intent, s.TotalUses, scoreLabel, s.AvgScore)
		}
		return sb.String()

	case "history":
		limit := 5
		if len(args) > 1 {
			fmt.Sscanf(args[1], "%d", &limit)
		}
		records, err := memory.GetRecentCotUsage(limit)
		if err != nil || len(records) == 0 {
			return "📜 No CoT history yet."
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "📜 **Recent CoT Usage** (%d)\n\n", len(records))
		for _, r := range records {
			fb := "😐"
			if r.Feedback > 0 {
				fb = "👍"
			} else if r.Feedback < 0 {
				fb = "👎"
			}
			tags := ""
			if len(r.Tags) > 0 {
				tags = " [" + strings.Join(r.Tags, ", ") + "]"
			}
			prompt := r.CotPrompt
			if len(prompt) > 80 {
				prompt = prompt[:80] + "..."
			}
			fmt.Fprintf(&sb, "• #%d %s %s%s: %s\n", r.ID, fb, r.Intent, tags, prompt)
		}
		return sb.String()

	default:
		return fmt.Sprintf("Unknown subcommand: %s. Use /cot for help.", args[0])
	}
}

// --- /runtime ---------------------------------------------------------------

func (r *Reflector) cmdRuntimeStatus(args []string, memory *MemoryStore) string {
	if len(args) == 0 {
		return "Usage: /runtime [status|processors|commands]"
	}

	switch args[0] {
	case "status":
		r.mu.RLock()
		nProc := len(r.processors)
		nCmd := len(r.commands)
		r.mu.RUnlock()

		var sb strings.Builder
		sb.WriteString("⚙️ **Runtime Status**\n")
		fmt.Fprintf(&sb, "• Processors: %d\n", nProc)
		fmt.Fprintf(&sb, "• Commands: %d\n", nCmd)
		fmt.Fprintf(&sb, "• Timeout: %s\n", r.timeout)
		if r.model != "" {
			fmt.Fprintf(&sb, "• Model: %s\n", r.model)
		}
		return sb.String()

	case "processors":
		r.mu.RLock()
		defer r.mu.RUnlock()
		var sb strings.Builder
		sb.WriteString("⚙️ **Processors**\n")
		for i, p := range r.processors {
			fmt.Fprintf(&sb, "• %d. %s\n", i+1, p.Name())
		}
		return sb.String()

	case "commands":
		return r.ListCommands()

	default:
		return fmt.Sprintf("Unknown: %s. Use /runtime for help.", args[0])
	}
}

// --- /shell -----------------------------------------------------------------

func (r *Reflector) cmdShell(args []string, _ *MemoryStore) string {
	r.mu.RLock()
	si := r.shellInstance
	r.mu.RUnlock()

	if si == nil {
		return "⚠️ Shell not available"
	}
	return si.Execute(args)
}

// --- /show ------------------------------------------------------------------

func (r *Reflector) cmdShow(args []string, _ *MemoryStore) string {
	if len(args) < 1 {
		return "Usage: /show [model|channel|agents]"
	}

	r.mu.RLock()
	reg := r.agentRegistry
	r.mu.RUnlock()

	switch args[0] {
	case "model":
		if reg == nil {
			return "⚠️ Agent registry not available"
		}
		agent := reg.GetDefaultAgent()
		if agent == nil {
			return "No default agent configured"
		}
		return fmt.Sprintf("Current model: %s", agent.Model)
	case "channel":
		return "Use /list channels to see enabled channels"
	case "agents":
		if reg == nil {
			return "⚠️ Agent registry not available"
		}
		ids := reg.ListAgentIDs()
		return fmt.Sprintf("Registered agents: %s", strings.Join(ids, ", "))
	default:
		return fmt.Sprintf("Unknown show target: %s", args[0])
	}
}

// --- /list ------------------------------------------------------------------

func (r *Reflector) cmdList(args []string, _ *MemoryStore) string {
	if len(args) < 1 {
		return "Usage: /list [models|channels|agents]"
	}

	r.mu.RLock()
	reg := r.agentRegistry
	cm := r.channelManager
	r.mu.RUnlock()

	switch args[0] {
	case "models":
		return "Available models: configured in config.json per agent"
	case "channels":
		if cm == nil {
			return "Channel manager not initialized"
		}
		chs := cm.GetEnabledChannels()
		if len(chs) == 0 {
			return "No channels enabled"
		}
		return fmt.Sprintf("Enabled channels: %s", strings.Join(chs, ", "))
	case "agents":
		if reg == nil {
			return "⚠️ Agent registry not available"
		}
		ids := reg.ListAgentIDs()
		return fmt.Sprintf("Registered agents: %s", strings.Join(ids, ", "))
	default:
		return fmt.Sprintf("Unknown list target: %s", args[0])
	}
}

// --- /switch ----------------------------------------------------------------

func (r *Reflector) cmdSwitch(args []string, _ *MemoryStore) string {
	if len(args) < 3 || args[1] != "to" {
		return "Usage: /switch [model|channel] to <name>"
	}

	target := args[0]
	value := args[2]

	r.mu.RLock()
	reg := r.agentRegistry
	cm := r.channelManager
	r.mu.RUnlock()

	switch target {
	case "model":
		if reg == nil {
			return "⚠️ Agent registry not available"
		}
		agent := reg.GetDefaultAgent()
		if agent == nil {
			return "No default agent configured"
		}
		oldModel := agent.Model
		agent.Model = value
		return fmt.Sprintf("Switched model from %s to %s", oldModel, value)
	case "channel":
		if cm == nil {
			return "Channel manager not initialized"
		}
		if _, exists := cm.GetChannel(value); !exists && value != "cli" {
			return fmt.Sprintf("Channel '%s' not found or not enabled", value)
		}
		return fmt.Sprintf("Switched target channel to %s", value)
	default:
		return fmt.Sprintf("Unknown switch target: %s", target)
	}
}
