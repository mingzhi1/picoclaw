// Config & Skill conversation live tests.
//
// These tests simulate realistic user conversations about:
//   - Querying and modifying Telegram channel config (uses a sample file in workspace)
//   - Discovering and installing skills via natural dialogue
//   - Multi-turn skill usage after installation
//
// Run (individual):
//
//	$env:LIVE_SLEEP="6s"; go test ./pkg/agent/... -run TestLive_Config_Telegram -v -timeout 120s
//	$env:LIVE_SLEEP="6s"; go test ./pkg/agent/... -run TestLive_Skill_ -v -timeout 300s
//
// Run all:
//
//	$env:LIVE_SLEEP="6s"; go test ./pkg/agent/... -run "TestLive_Config_|TestLive_Skill_" -v -timeout 900s -p 1
package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleTelegramConfig is a minimal picoclaw config JSON used in Telegram tests.
// We write it into the test workspace so the agent can read it without
// violating the restrict_to_workspace=true policy.
const sampleTelegramConfig = `{
  "channels": {
    "telegram": {
      "enabled": true,
      "token": "8764406301:AAEvXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
    }
  }
}`

// writeSampleConfig writes sampleTelegramConfig to a temp .json file inside
// the agent loop's workspace and returns its absolute path.
func writeSampleConfig(t *testing.T, al *AgentLoop) string {
	t.Helper()
	agent := al.registry.GetDefaultAgent()
	if agent == nil {
		t.Skip("no default agent")
	}
	workspace := agent.Workspace
	if workspace == "" {
		t.Skip("agent has no workspace")
	}
	p := filepath.Join(workspace, "sample_config.json")
	if err := os.WriteFile(p, []byte(sampleTelegramConfig), 0o600); err != nil {
		t.Fatalf("writeSampleConfig: %v", err)
	}
	t.Cleanup(func() { os.Remove(p) })
	return p
}

// cfgSleep is longer than liveSleep to avoid 429 across config+skill tests.
func cfgSleep(t *testing.T) {
	t.Helper()
	d := 6 * time.Second
	t.Logf("sleeping %s (config test buffer)...", d)
	time.Sleep(d)
}

// ---------------------------------------------------------------------------
// Telegram config conversations (uses sample file in workspace)
// ---------------------------------------------------------------------------

// TestLive_Config_Telegram_Query asks the agent to read a sample config and
// report on the Telegram section. Uses a workspace-safe temp file.
func TestLive_Config_Telegram_Query(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	cfgPath := writeSampleConfig(t, al)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := al.ProcessDirect(ctx,
		"Read the file "+cfgPath+` and tell me: is the Telegram channel enabled? What are the first 10 characters of the token?`,
		newSession("tg-query"))
	if err != nil {
		t.Fatalf("ProcessDirect: %v", err)
	}
	t.Logf("Response (%d chars):\n%s", len(resp), resp)

	lower := strings.ToLower(resp)
	if !strings.Contains(lower, "telegram") {
		t.Errorf("expected 'telegram' in response; got: %s", resp)
	}
	if !strings.Contains(lower, "enabled") && !strings.Contains(lower, "true") && !strings.Contains(lower, "active") {
		t.Logf("SOFT: expected enabled status mention; got: %s", resp)
	}
}

// TestLive_Config_Telegram_Disable reads, disables, verifies, then re-enables
// Telegram in a workspace-safe sample config file.
func TestLive_Config_Telegram_Disable(t *testing.T) {
	cfgSleep(t) // buffer after Query test's API calls

	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	cfgPath := writeSampleConfig(t, al)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	sess := newSession("tg-disable")

	// Turn 1: ask to disable
	r1, err := al.ProcessDirect(ctx,
		"Read the file "+cfgPath+`. Change the telegram "enabled" field from true to false, write the file back, then confirm.`,
		sess)
	if err != nil {
		t.Fatalf("T1 disable: %v", err)
	}
	t.Logf("T1 DISABLE: %s", r1)
	lower1 := strings.ToLower(r1)
	if !strings.Contains(lower1, "false") && !strings.Contains(lower1, "disabled") && !strings.Contains(lower1, "done") {
		t.Logf("SOFT T1: expected disable confirmation; got: %s", r1)
	}

	cfgSleep(t)

	// Turn 2: verify
	r2, err := al.ProcessDirect(ctx,
		"Read "+cfgPath+` and confirm: is the Telegram channel now disabled? Answer yes or no.`,
		sess)
	if err != nil {
		t.Fatalf("T2 verify: %v", err)
	}
	t.Logf("T2 VERIFY: %s", r2)
	lower2 := strings.ToLower(r2)
	if !strings.Contains(lower2, "false") && !strings.Contains(lower2, "disabled") && !strings.Contains(lower2, "yes") {
		t.Errorf("T2: expected disabled; got: %s", r2)
	}

	cfgSleep(t)

	// Turn 3: re-enable
	r3, err := al.ProcessDirect(ctx,
		"Re-enable Telegram (set enabled: true) in "+cfgPath+" and write the file back. Confirm when done.",
		sess)
	if err != nil {
		t.Fatalf("T3 restore: %v", err)
	}
	t.Logf("T3 RESTORE: %s", r3)
	lower3 := strings.ToLower(r3)
	if !strings.Contains(lower3, "true") && !strings.Contains(lower3, "enabled") && !strings.Contains(lower3, "done") {
		t.Logf("SOFT T3: expected re-enable confirmation; got: %s", r3)
	}
}

// TestLive_Config_Telegram_AddField reads the sample config and lists its fields.
func TestLive_Config_Telegram_AddField(t *testing.T) {
	cfgSleep(t) // buffer after previous test's API calls

	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	cfgPath := writeSampleConfig(t, al)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sess := newSession("tg-addfield")

	// Turn 1: read current telegram section
	r1, err := al.ProcessDirect(ctx,
		"Read the file "+cfgPath+` and show me just the "telegram" section.`,
		sess)
	if err != nil {
		t.Fatalf("T1: %v", err)
	}
	t.Logf("T1 CONFIG SECTION:\n%s", r1)
	if !strings.Contains(strings.ToLower(r1), "telegram") {
		t.Errorf("expected telegram section; got: %s", r1)
	}

	cfgSleep(t)

	// Turn 2: list field names
	r2, err := al.ProcessDirect(ctx,
		"Based on what you just read, what fields are in the telegram section? List them.",
		sess)
	if err != nil {
		t.Fatalf("T2: %v", err)
	}
	t.Logf("T2 FIELDS: %s", r2)
	lower := strings.ToLower(r2)
	if !strings.Contains(lower, "enabled") && !strings.Contains(lower, "token") {
		t.Logf("SOFT T2: expected field list; got: %s", r2)
	}
}

// ---------------------------------------------------------------------------
// Skill installation via natural dialogue
// ---------------------------------------------------------------------------

// TestLive_Skill_SearchAndInstall 通过对话搜索并安装 skill（weather）
func TestLive_Skill_SearchAndInstall(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	sess := newSession("skill-install")

	// Turn 1: user asks for capability, AI should discover it needs a skill
	r1, err := al.ProcessDirect(ctx,
		"I want you to be able to tell me the weather. Do you have that capability right now? If not, can you find and install a skill for it?",
		sess)
	if err != nil {
		t.Fatalf("T1: %v", err)
	}
	t.Logf("T1 CAPABILITY CHECK (%d chars):\n%s", len(r1), r1)

	liveSleep(t)

	// Turn 2: explicitly ask to install if not already done
	r2, err := al.ProcessDirect(ctx,
		"Please search the skill registry for weather and install the best result. Tell me which skill you installed.",
		sess)
	if err != nil {
		t.Fatalf("T2: %v", err)
	}
	t.Logf("T2 INSTALL (%d chars):\n%s", len(r2), r2)

	lower2 := strings.ToLower(r2)
	if !strings.Contains(lower2, "weather") {
		t.Errorf("expected weather skill mention; got: %s", r2)
	}
	if !strings.Contains(lower2, "install") && !strings.Contains(lower2, "installed") {
		t.Logf("SOFT T2: expected install confirmation; got: %s", r2)
	}

	liveSleep(t)

	// Turn 3: use the installed skill
	r3, err := al.ProcessDirect(ctx,
		"Now use the weather skill to get the current weather for Tokyo, Japan.",
		sess)
	if err != nil {
		t.Fatalf("T3: %v", err)
	}
	t.Logf("T3 USE SKILL (%d chars):\n%s", len(r3), r3)

	lower3 := strings.ToLower(r3)
	// After install, skill SKILL.md is loaded — may or may not succeed depending on external API
	if !strings.Contains(lower3, "tokyo") && !strings.Contains(lower3, "weather") &&
		!strings.Contains(lower3, "temperature") && !strings.Contains(lower3, "celsius") {
		t.Logf("SOFT T3: expected weather result or attempt; got: %s", r3)
	}
}

// TestLive_Skill_ListInstalled 查询已安装的 skill 列表
func TestLive_Skill_ListInstalled(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := al.ProcessDirect(ctx,
		"What skills are currently installed in your workspace? List them with their names and what they do.",
		newSession("skill-list"))
	if err != nil {
		t.Fatalf("ProcessDirect: %v", err)
	}
	t.Logf("Response (%d chars):\n%s", len(resp), resp)

	// Should either list skills or say none are installed
	lower := strings.ToLower(resp)
	hasSkillMention := strings.Contains(lower, "skill") ||
		strings.Contains(lower, "installed") ||
		strings.Contains(lower, "no skill") ||
		strings.Contains(lower, "none")
	if !hasSkillMention {
		t.Errorf("expected skill status report; got: %s", resp)
	}
}

// TestLive_Skill_MultiTurnSetup simulates a multi-turn GitHub skill setup.
func TestLive_Skill_MultiTurnSetup(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	// 4 turns × ~6s + 3 sleeps × 6s = 42s nominal, allow 240s
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	sess := newSession("skill-github-setup")

	turns := []struct {
		prompt  string
		wantAny []string // at least one must be present (case-insensitive)
	}{
		{
			"Do you have any GitHub-related skills installed?",
			[]string{"github", "skill", "installed", "no"},
		},
		{
			"Search the skill registry for 'github' and tell me the top 3 results with their descriptions.",
			[]string{"github", "score", "registry", "clawhub", "description", "1.", "2.", "3."},
		},
		{
			"Install the top result from that search. What is the full name and version of the skill you installed?",
			[]string{"install", "version", "github", "skill"},
		},
		{
			"Now that the github skill is installed, what new capabilities do I have? Summarize in bullet points.",
			[]string{"github", "•", "-", "can", "you", "ability"},
		},
	}

	for i, turn := range turns {
		if i > 0 {
			liveSleep(t)
		}
		resp, err := al.ProcessDirect(ctx, turn.prompt, sess)
		if err != nil {
			t.Fatalf("T%d: %v", i+1, err)
		}
		t.Logf("T%d (%d chars): %.500s", i+1, len(resp), resp)

		lower := strings.ToLower(resp)
		found := false
		for _, kw := range turn.wantAny {
			if strings.Contains(lower, strings.ToLower(kw)) {
				found = true
				break
			}
		}
		if !found {
			t.Logf("SOFT T%d: none of %v found in: %s", i+1, turn.wantAny, resp)
		}
	}
}
