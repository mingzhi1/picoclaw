// Full pipeline E2E test: TurnStore → InstantMemory → DigestWorker → FactStore → Context injection
//
// Run:
//   LIVE_CONFIG=testdata/config.json go test ./pkg/agent/... -run TestLive_FullPipeline -v -timeout 300s
package agent

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/agent/topic"
	_ "modernc.org/sqlite"
)

func TestLive_FullPipeline_MemoryAndFacts(t *testing.T) {
	cfg := liveConfig(t)
	auxModel := cfg.Agents.Defaults.GetAuxiliaryModel()
	p, resolved := liveProvider(t, cfg, auxModel)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	tmpDir := t.TempDir()

	// --- Infrastructure setup ---
	turnStore, err := NewTurnStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer turnStore.Close()

	memStore := NewMemoryStore(tmpDir)
	defer memStore.Close()

	topicStore, err := topic.NewStore(tmpDir + "/topics.db")
	if err != nil {
		t.Fatal(err)
	}
	defer topicStore.Close()
	tracker, err := topic.NewTracker(topicStore)
	if err != nil {
		t.Fatal(err)
	}

	factDB, err := sql.Open("sqlite", tmpDir+"/facts.db?_pragma=journal_mode(wal)")
	if err != nil {
		t.Fatal(err)
	}
	factStore, err := NewFactStore(factDB)
	if err != nil {
		t.Fatal(err)
	}
	defer factStore.Close()

	analyser := NewAnalyser(p, resolved, nil)

	digestWorker := NewMemoryDigestWorker(turnStore, memStore, p, resolved)
	digestWorker.SetFactStore(factStore)

	channelKey := "test:e2e"

	// ==========================================================
	// Phase 1: Simulate 6 turns of conversation with persistence
	// ==========================================================
	conversations := []struct {
		user  string
		reply string
		score int
		tags  []string
	}{
		{"我的Go服务parseConfig延迟850ns，怎么优化",
			"建议从三个方向入手：1)对象池复用 2)减少反射 3)缓存结果", 6, []string{"go", "performance"}},
		{"试了sync.Pool，降到420ns了",
			"很好的进展。sync.Pool避免了频繁GC，建议继续看反射部分", 5, []string{"go", "performance"}},
		{"type switch替代反射，现在380ns",
			"380ns已经很优秀了。如果还要压榨可以考虑unsafe或代码生成", 7, []string{"go", "performance", "optimization"}},
		{"帮我写Docker Compose，Go+Redis+Postgres",
			"好的，这是一个典型的三件套部署...\n```yaml\nservices:\n  app:\n    build: .\n  redis:\n    image: redis:7\n  postgres:\n    image: postgres:16\n```", 5, []string{"docker", "deploy"}},
		{"线上session丢了，用户登录后被踢出",
			"紧急排查：1)检查Redis连接状态 2)看session TTL 3)检查负载均衡sticky session", 8, []string{"bug", "redis", "urgent"}},
		{"查到了，Redis maxconn配成10太小，改成100后恢复",
			"根因是连接池饱和导致新连接被拒绝。建议加监控告警", 7, []string{"bug", "redis", "resolved"}},
	}

	t.Log("=== Phase 1: Storing turns ===")
	for i, c := range conversations {
		record := TurnRecord{
			Ts:         time.Now().Unix() - int64(len(conversations)-i)*60, // 每轮间隔1分钟
			ChannelKey: channelKey,
			Score:      c.score,
			Intent:     "task",
			Tags:       c.tags,
			Status:     "pending",
			UserMsg:    sanitizeUserMsg(c.user),
			Reply:      sanitizeReply(c.reply),
		}
		if err := turnStore.Insert(record); err != nil {
			t.Fatalf("Insert turn %d: %v", i, err)
		}
		t.Logf("  Stored turn %d: score=%d tags=%v msg=%q",
			i+1, c.score, c.tags, c.user[:min(len(c.user), 30)])
	}

	// ==========================================================
	// Phase 2: Verify InstantMemory selection
	// ==========================================================
	t.Log("\n=== Phase 2: InstantMemory selection ===")
	imCfg := DefaultInstantMemoryCfg(32000)
	selected := BuildInstantMemory(turnStore, []string{"go", "performance"}, channelKey, imCfg)
	t.Logf("  Selected %d turns (tags=[go,performance])", len(selected))
	for _, s := range selected {
		t.Logf("    ts=%d score=%d tags=%v msg=%q", s.Ts, s.Score, s.Tags, s.UserMsg[:min(len(s.UserMsg), 40)])
	}
	if len(selected) == 0 {
		t.Error("InstantMemory returned 0 turns")
	}

	// High-score turns (>=7) should always be included
	highCount := 0
	for _, s := range selected {
		if s.Score >= 7 {
			highCount++
		}
	}
	t.Logf("  High-score turns (>=7): %d", highCount)
	if highCount == 0 {
		t.Error("Expected at least 1 high-score turn in selection")
	}

	// ==========================================================
	// Phase 3: Run DigestWorker to extract memories + facts
	// ==========================================================
	t.Log("\n=== Phase 3: DigestWorker extraction ===")
	liveSleep(t) // rate limit
	if err := digestWorker.RunOnceNow(ctx); err != nil {
		t.Logf("  DigestWorker error (non-fatal): %v", err)
	}

	// Check what memories were extracted
	allTags, _ := memStore.ListAllTags()
	t.Logf("  Memory tags after digest: %v", allTags)

	allMems, _ := memStore.ListEntries(20)
	t.Logf("  Total memories extracted: %d", len(allMems))
	for i, m := range allMems {
		content := m.Content
		if len(content) > 80 {
			content = content[:80] + "..."
		}
		t.Logf("    [%d] %s (tags=%v)", i+1, content, m.Tags)
	}

	// Check pending → processed
	pending, _ := turnStore.QueryPending(50)
	t.Logf("  Remaining pending turns: %d (should be 0)", len(pending))
	if len(pending) != 0 {
		t.Errorf("Expected 0 pending turns after digest, got %d", len(pending))
	}

	// ==========================================================
	// Phase 4: Verify FactStore state
	// ==========================================================
	t.Log("\n=== Phase 4: FactStore verification ===")
	activeFacts, _ := factStore.GetGlobalFacts()
	t.Logf("  Global active facts: %d", len(activeFacts))
	for _, f := range activeFacts {
		t.Logf("    %s.%s = %q (%s)", f.Entity, f.Key, f.Value, f.Type)
	}

	factsCtx := factStore.FormatForContext("")
	if factsCtx != "" {
		t.Logf("  Fact context:\n%s", factsCtx)
	} else {
		t.Log("  (no structured facts extracted — LLM may not have produced entity/key/value)")
	}

	// ==========================================================
	// Phase 5: Build Phase2Messages with all context layers
	// ==========================================================
	t.Log("\n=== Phase 5: Full context assembly ===")

	// Simulate Analyser call
	liveSleep(t)
	topicCtx := tracker.FormatForAnalyser()
	ar := analyser.Analyse(ctx, "帮我总结一下今天做了什么", memStore, nil, topicCtx)
	t.Logf("  Analyser: intent=%q tags=%v", ar.Intent, ar.Tags)

	// Build memory context from matched tags
	longTermMem := memStore.GetMemoryContext()
	t.Logf("  Long-term memory: %d chars", len(longTermMem))

	// Re-select instant memory with analyser tags
	turns2 := BuildInstantMemory(turnStore, ar.Tags, channelKey, imCfg)
	if len(turns2) == 0 {
		turns2 = selected // fallback to previous selection
	}

	systemPrompt := "你是一个AI助手。"
	if factsCtx != "" {
		systemPrompt += "\n\n# Active Facts\n" + factsCtx
	}

	msgs := BuildPhase2Messages(systemPrompt, longTermMem, turns2, "帮我总结一下今天做了什么", 7)
	t.Logf("  Phase2 messages: %d total", len(msgs))
	for i, m := range msgs {
		preview := m.Content
		if len(preview) > 60 {
			preview = preview[:60] + "..."
		}
		t.Logf("    [%d] %s: %s", i, m.Role, preview)
	}

	// ==========================================================
	// Phase 6: Call LLM with full context and verify it uses memory
	// ==========================================================
	t.Log("\n=== Phase 6: LLM response with full context ===")
	liveSleep(t)

	llmOpts := map[string]any{"max_tokens": 1024, "temperature": 0.5}
	resp, err := p.Chat(ctx, msgs, nil, resolved, llmOpts)
	if err != nil {
		t.Fatalf("LLM Chat: %v", err)
	}

	t.Logf("  LLM response (%d chars):\n%s", len(resp.Content), resp.Content)

	// Verify response references conversation topics
	response := strings.ToLower(resp.Content)
	checks := []struct {
		keyword string
		desc    string
	}{
		{"parseconfig", "应该提到 parseConfig"},
		{"380", "应该提到 380ns 延迟"},
		{"docker", "应该提到 Docker"},
		{"redis", "应该提到 Redis"},
		{"session", "应该提到 session 问题"},
	}
	matched := 0
	for _, c := range checks {
		if strings.Contains(response, c.keyword) {
			t.Logf("  ✓ %s", c.desc)
			matched++
		} else {
			t.Logf("  ✗ %s (keyword=%q not found)", c.desc, c.keyword)
		}
	}
	t.Logf("  内容覆盖率: %d/%d", matched, len(checks))
	if matched < 3 {
		t.Errorf("LLM未能充分利用上下文: 只提到 %d/%d 个关键信息", matched, len(checks))
	}

	if resp.Usage != nil {
		t.Logf("  Token usage: prompt=%d completion=%d total=%d",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	}
}
