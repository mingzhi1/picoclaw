// Live integration tests for all three layers of the LLM architecture.
//
// These tests require a real config at ~/.picoclaw/config.json with valid model credentials.
//
// Run:
//
//	go test ./pkg/agent/... -run TestLive -v -timeout 120s
//
// Skip in CI:
//
//	go test -short ./pkg/agent/...   # all TestLive_* tests are skipped
package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/core/bus"
	"github.com/sipeed/picoclaw/pkg/infra/config"
	"github.com/sipeed/picoclaw/pkg/infra/logger"
	"github.com/sipeed/picoclaw/pkg/llm/providers"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// liveSleep pauses between API calls to avoid rate-limit (429) errors.
// Default: 3 seconds. Override with env LIVE_SLEEP (e.g. LIVE_SLEEP=5s).
func liveSleep(t *testing.T) {
	t.Helper()
	d := 3 * time.Second
	if s := os.Getenv("LIVE_SLEEP"); s != "" {
		if parsed, err := time.ParseDuration(s); err == nil {
			d = parsed
		}
	}
	t.Logf("sleeping %s (rate-limit buffer)...", d)
	time.Sleep(d)
}

func liveConfig(t *testing.T) *config.Config {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping live API test in -short mode")
	}
	if os.Getenv("RUN_LIVE_TESTS") != "1" && os.Getenv("LIVE_CONFIG") == "" {
		t.Skip("skipping live API test; set RUN_LIVE_TESTS=1 or LIVE_CONFIG to run")
	}
	candidates := []string{
		filepath.Join(os.Getenv("USERPROFILE"), ".picoclaw", "config.json"),
		filepath.Join(os.Getenv("HOME"), ".picoclaw", "config.json"),
		filepath.Join("testdata", "config.json"),
	}
	// Allow explicit config path via LIVE_CONFIG env var.
	if envCfg := os.Getenv("LIVE_CONFIG"); envCfg != "" {
		candidates = []string{envCfg}
	}
	var cfgPath string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			cfgPath = p
			break
		}
	}
	if cfgPath == "" {
		t.Skip("no config.json found; expected at ~/.picoclaw/config.json")
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Agents.Defaults.GetPrimaryModel() == "" {
		t.Skip("no primary_model configured")
	}
	// Allow overriding auxiliary model via env (e.g. to avoid different-provider rate limits).
	// Default: fall back to primary model so both phases use the same provider.
	auxOverride := os.Getenv("LIVE_AUX_MODEL")
	if auxOverride == "" {
		auxOverride = cfg.Agents.Defaults.GetPrimaryModel()
	}
	cfg.Agents.Defaults.AuxiliaryModel = auxOverride

	t.Logf("Config: %s  primary=%s  aux=%s (overridden)",
		cfgPath, cfg.Agents.Defaults.GetPrimaryModel(), cfg.Agents.Defaults.GetAuxiliaryModel())
	return cfg
}

func liveProvider(t *testing.T, cfg *config.Config, modelName string) (providers.LLMProvider, string) {
	t.Helper()
	if modelName == "" {
		t.Skip("empty model name")
	}
	mc, err := cfg.GetModelConfig(modelName)
	if err != nil {
		t.Skipf("model %q not in model_list: %v", modelName, err)
	}
	p, resolved, err := providers.CreateProviderFromConfig(mc)
	if err != nil {
		t.Skipf("CreateProviderFromConfig(%q): %v", modelName, err)
	}
	return p, resolved
}

func liveLoop(t *testing.T, cfg *config.Config) *AgentLoop {
	t.Helper()
	tmpDir := t.TempDir()
	cfg.Agents.Defaults.Workspace = tmpDir
	if cfg.Agents.Defaults.MaxTokens == 0 {
		cfg.Agents.Defaults.MaxTokens = 4096
	}
	if cfg.Agents.Defaults.MaxToolIterations == 0 {
		cfg.Agents.Defaults.MaxToolIterations = 5
	}
	p, resolved, err := providers.CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if resolved != "" {
		cfg.Agents.Defaults.ModelName = resolved
	}
	msgBus := bus.NewMessageBus()
	t.Cleanup(func() { msgBus.Close() })
	al := NewAgentLoop(cfg, msgBus, p)
	t.Cleanup(func() { al.Close() }) // release SQLite locks before TempDir cleanup

	// Write prompt logs to a fixed location for post-test inspection.
	// Falls back gracefully if the directory can't be created.
	promptDir := filepath.Join("testdata", "prompt_logs", t.Name())
	_ = os.MkdirAll(promptDir, 0o755)
	if err := logger.EnablePromptLogging(promptDir); err == nil {
		t.Logf("Prompt logs → %s", promptDir)
	}
	t.Cleanup(func() { logger.DisablePromptLogging() })
	return al
}

// ---------------------------------------------------------------------------
// Phase 1: Analyser (auxiliary LLM)
// ---------------------------------------------------------------------------

func TestLive_Phase1_Analyser(t *testing.T) {
	cfg := liveConfig(t)
	auxModel := cfg.Agents.Defaults.GetAuxiliaryModel()
	p, resolved := liveProvider(t, cfg, auxModel)
	analyser := NewAnalyser(p, resolved, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cases := []struct {
		name    string
		msg     string
		wantTag string
	}{
		{"code task zh", "帮我写一个 Go 函数来解析 JSON 配置文件", "code"},
		{"deploy task en", "Deploy the service to production and restart nginx", "deploy"},
		{"math question", "What is 17 * 23?", ""},
		{"whitespace boundary", "   ", ""},
	}
	for i, tc := range cases {
		if i > 0 {
			liveSleep(t)
		}
		t.Run(tc.name, func(t *testing.T) {
			r := analyser.Analyse(ctx, tc.msg, nil, nil, "")
			t.Logf("input=%q → intent=%q tags=%v cot_len=%d",
				tc.msg, r.Intent, r.Tags, len(r.CotPrompt))
			if tc.wantTag != "" {
				found := strings.Contains(strings.ToLower(r.Intent), tc.wantTag)
				for _, tag := range r.Tags {
					if strings.Contains(strings.ToLower(tag), tc.wantTag) {
						found = true
					}
				}
				if !found {
					t.Logf("SOFT: expected %q in intent/tags; got intent=%q tags=%v", tc.wantTag, r.Intent, r.Tags)
				}
			}
		})
	}
}

func TestLive_Phase1_Boundary(t *testing.T) {
	cfg := liveConfig(t)
	auxModel := cfg.Agents.Defaults.GetAuxiliaryModel()
	p, resolved := liveProvider(t, cfg, auxModel)
	analyser := NewAnalyser(p, resolved, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Run("very long message", func(t *testing.T) {
		msg := strings.Repeat("请帮我分析这个代码然后提供优化建议。", 100)
		r := analyser.Analyse(ctx, msg, nil, nil, "")
		t.Logf("long(%d chars) → intent=%q tags=%v", len(msg), r.Intent, r.Tags)
	})
	liveSleep(t)
	t.Run("emoji noise", func(t *testing.T) {
		r := analyser.Analyse(ctx, "🎉🚀💥🔥 !!!###$$$", nil, nil, "")
		t.Logf("noise → intent=%q tags=%v", r.Intent, r.Tags)
	})
	liveSleep(t)
	t.Run("mixed zh-en", func(t *testing.T) {
		r := analyser.Analyse(ctx, "Please 帮我 deploy to 生产环境 now", nil, nil, "")
		t.Logf("mixed → intent=%q tags=%v", r.Intent, r.Tags)
	})
	liveSleep(t)
	t.Run("slash passthrough", func(t *testing.T) {
		r := analyser.Analyse(ctx, "/help", nil, nil, "")
		t.Logf("/help → intent=%q tags=%v", r.Intent, r.Tags)
	})
}

// ---------------------------------------------------------------------------
// Phase 2: Executor (primary LLM) — full agent loop
// ---------------------------------------------------------------------------

func TestLive_Phase2_Executor(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cases := []struct {
		name        string
		msg         string
		wantContain string
	}{
		{"math", "What is 17 * 23? Reply with just the number.", "391"},
		{"code", "Write a Go function Add returning sum of two ints. Just the function.", "func"},
		{"greeting", "Say hello in 3 words.", ""},
	}
	for i, tc := range cases {
		if i > 0 {
			liveSleep(t)
		}
		t.Run(tc.name, func(t *testing.T) {
			session := "live-p2-" + tc.name + "-" + time.Now().Format("150405")
			start := time.Now()
			resp, err := al.ProcessDirect(ctx, tc.msg, session)
			if err != nil {
				t.Fatalf("ProcessDirect: %v", err)
			}
			if resp == "" {
				t.Error("empty response")
			}
			t.Logf("latency=%s len=%d: %.200s",
				time.Since(start).Round(time.Millisecond), len(resp), resp)
			if tc.wantContain != "" && !strings.Contains(resp, tc.wantContain) {
				t.Errorf("want %q in response\ngot: %s", tc.wantContain, resp)
			}
		})
	}
}

func TestLive_Phase2_MultiTurn(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	session := "live-multiturn-" + time.Now().Format("150405")

	r1, err := al.ProcessDirect(ctx, "My secret number is 42. Acknowledge.", session)
	if err != nil {
		t.Fatalf("turn1: %v", err)
	}
	t.Logf("T1: %s", r1)

	liveSleep(t)

	r2, err := al.ProcessDirect(ctx, "What is my secret number?", session)
	if err != nil {
		t.Fatalf("turn2: %v", err)
	}
	t.Logf("T2: %s", r2)
	if !strings.Contains(r2, "42") {
		t.Errorf("model forgot secret number; T2: %s", r2)
	}
}

func TestLive_Phase2_SmallMaxTokens(t *testing.T) {
	cfg := liveConfig(t)
	cfg.Agents.Defaults.MaxTokens = 50
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := al.ProcessDirect(ctx, "Say OK.", "live-smalltok")
	t.Logf("resp=%q err=%v", resp, err)
	if err == nil && resp == "" {
		t.Error("expected non-empty response or an error")
	}
}

// ---------------------------------------------------------------------------
// Phase 3: Reflector — slash commands (synchronous, no LLM)
// ---------------------------------------------------------------------------

func TestLive_Phase3_SlashCommands(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess := "live-slash-" + time.Now().Format("150405")

	cmds := []struct {
		input string
		want  string
	}{
		{"/help", "Commands"},
		{"/memory stats", "Stats"},
		{"/memory add Live test note #live #test", "saved"},
		{"/memory list", "Live test note"},
		{"/runtime status", "Runtime"},
		{"/memory search live", "Live test note"},
	}
	for _, c := range cmds {
		t.Run(c.input, func(t *testing.T) {
			resp, err := al.ProcessDirect(ctx, c.input, sess)
			if err != nil {
				t.Fatalf("ProcessDirect(%q): %v", c.input, err)
			}
			t.Logf("→ %.200s", resp)
			if c.want != "" && !strings.Contains(resp, c.want) {
				t.Errorf("want %q in response\ngot: %s", c.want, resp)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Full three-layer integration
// ---------------------------------------------------------------------------

func TestLive_ThreeLayers_FullTurn(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := al.ProcessDirect(ctx,
		"Write a Go function called Multiply that returns a*b for two ints.",
		"live-three-layers")
	if err != nil {
		t.Fatalf("ProcessDirect: %v", err)
	}
	if resp == "" {
		t.Fatal("empty response")
	}
	t.Logf("3-layer (%d chars): %.300s", len(resp), resp)
	if !strings.Contains(resp, "func") {
		t.Logf("SOFT: expected func keyword")
	}
}

// ---------------------------------------------------------------------------
// Score logic (pure CPU, no LLM)
// ---------------------------------------------------------------------------

func TestCalcTurnScore_Logic(t *testing.T) {
	cases := []struct {
		name  string
		input RuntimeInput
		check func(int) bool
		desc  string
	}{
		{
			"task+tools → always_keep",
			RuntimeInput{
				Intent: "task", UserMessage: "Deploy",
				AssistantReply: strings.Repeat("x", 600),
				ToolCalls:      []ToolCallRecord{{Name: "exec"}, {Name: "write_file"}, {Name: "read_file"}, {Name: "exec"}},
			},
			func(s int) bool { return s >= alwaysKeepThreshold },
			">= 7",
		},
		{
			"chat+short → low",
			RuntimeInput{Intent: "chat", UserMessage: "hi", AssistantReply: "Hello!"},
			func(s int) bool { return s < 3 },
			"< 3",
		},
		{
			"remember marker",
			RuntimeInput{Intent: "question", UserMessage: "remember: server is 10.0.0.1", AssistantReply: "Got it."},
			func(s int) bool { return s >= 2 }, // question(+1) + remember(+3) - short(-2) = 2
			">= 2",
		},
		{
			"重要 zh marker",
			RuntimeInput{Intent: "chat", UserMessage: "这很重要：密钥是abc123", AssistantReply: "已记录。"},
			func(s int) bool { return s >= 1 }, // chat(0) + 重要(+3) - short(-2) = 1
			">= 1",
		},
		{
			"code+long reply",
			RuntimeInput{Intent: "code", UserMessage: "write func", AssistantReply: strings.Repeat("x", 600)},
			func(s int) bool { return s >= 5 },
			">= 5",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			score := CalcTurnScore(tc.input)
			t.Logf("score=%d (want %s)", score, tc.desc)
			if !tc.check(score) {
				t.Errorf("score=%d violates: %s", score, tc.desc)
			}
		})
	}
}
