// Live integration tests for Topic + Fact context management.
//
// These tests require a real config at ~/.picoclaw/config.json with valid model credentials.
//
// Run:
//
//	go test ./pkg/agent/... -run TestLive_Topic -v -timeout 120s
//
// Skip in CI:
//
//	go test -short ./pkg/agent/...
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

// ---------------------------------------------------------------------------
// TestLive_TopicAction — Analyser outputs topic_action with real LLM
// ---------------------------------------------------------------------------

func TestLive_TopicAction_NewTopic(t *testing.T) {
	cfg := liveConfig(t)
	auxModel := cfg.Agents.Defaults.GetAuxiliaryModel()
	p, resolved := liveProvider(t, cfg, auxModel)
	analyser := NewAnalyser(p, resolved, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	r := analyser.Analyse(ctx, "帮我写一个Go HTTP服务器", nil, nil, "")
	t.Logf("intent=%q tags=%v topic_action=%+v", r.Intent, r.Tags, r.TopicAction)

	if r.TopicAction == nil {
		t.Log("SOFT: TopicAction is nil (LLM may not have returned it)")
	} else {
		t.Logf("topic_action: action=%q id=%q title=%q resolve=%v",
			r.TopicAction.Action, r.TopicAction.ID, r.TopicAction.Title, r.TopicAction.Resolve)
		// Without existing topics, LLM should suggest "new"
		if r.TopicAction.Action != "new" && r.TopicAction.Action != "continue" {
			t.Logf("SOFT: expected new/continue, got %q", r.TopicAction.Action)
		}
	}
}

func TestLive_TopicAction_ContinueExisting(t *testing.T) {
	cfg := liveConfig(t)
	auxModel := cfg.Agents.Defaults.GetAuxiliaryModel()
	p, resolved := liveProvider(t, cfg, auxModel)
	analyser := NewAnalyser(p, resolved, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	topicCtx := "当前话题列表:\n- [topic_abc] Go性能优化 (active, 2min ago)\n- [topic_def] API设计 (idle, 1h ago)\n"

	r := analyser.Analyse(ctx, "刚才的性能优化继续，sync.Pool的效果怎样", nil, nil, topicCtx)
	t.Logf("intent=%q tags=%v topic_action=%+v", r.Intent, r.Tags, r.TopicAction)

	if r.TopicAction == nil {
		t.Log("SOFT: TopicAction is nil")
	} else {
		t.Logf("topic_action: action=%q id=%q title=%q",
			r.TopicAction.Action, r.TopicAction.ID, r.TopicAction.Title)
		if r.TopicAction.Action == "continue" && r.TopicAction.ID == "topic_abc" {
			t.Log("✓ LLM correctly identified continue + topic_abc")
		} else {
			t.Logf("SOFT: expected continue+topic_abc, got action=%q id=%q",
				r.TopicAction.Action, r.TopicAction.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// TestLive_TopicTracker_E2E — Full topic lifecycle with real LLM
// ---------------------------------------------------------------------------

func TestLive_TopicTracker_E2E(t *testing.T) {
	cfg := liveConfig(t)
	auxModel := cfg.Agents.Defaults.GetAuxiliaryModel()
	p, resolved := liveProvider(t, cfg, auxModel)
	analyser := NewAnalyser(p, resolved, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Set up TopicTracker with temp file DB.
	tmpDir := t.TempDir()
	store, err := topic.NewStore(tmpDir + "/test_topics.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tracker, err := topic.NewTracker(store)
	if err != nil {
		t.Fatal(err)
	}

	// Turn 1: New topic (no existing topics).
	topicCtx1 := tracker.FormatForAnalyser()
	t.Logf("Turn 1 topicCtx: %q", topicCtx1)
	r1 := analyser.Analyse(ctx, "帮我写一个 Go HTTP 服务器", nil, nil, topicCtx1)
	t.Logf("Turn 1: intent=%q tags=%v topic_action=%+v", r1.Intent, r1.Tags, r1.TopicAction)

	// Apply topic action.
	action1 := topic.Action{Type: topic.ActionNew, Title: "Go HTTP"}
	if r1.TopicAction != nil && r1.TopicAction.Action == "new" && r1.TopicAction.Title != "" {
		action1.Title = r1.TopicAction.Title
	}
	tp1, err := tracker.Apply(action1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Turn 1 topic: id=%s title=%q", tp1.ID, tp1.Title)
	_ = tracker.RecordTurnTokens(500)

	liveSleep(t)

	// Turn 2: Continue same topic.
	topicCtx2 := tracker.FormatForAnalyser()
	t.Logf("Turn 2 topicCtx: %s", topicCtx2)
	r2 := analyser.Analyse(ctx, "再加一个 /health 端点", nil, nil, topicCtx2)
	t.Logf("Turn 2: intent=%q tags=%v topic_action=%+v", r2.Intent, r2.Tags, r2.TopicAction)

	action2 := topic.Action{Type: topic.ActionContinue, Primary: tp1.ID}
	if r2.TopicAction != nil {
		if r2.TopicAction.Action == "continue" && r2.TopicAction.ID != "" {
			action2.Primary = r2.TopicAction.ID
		}
	}
	tp2, err := tracker.Apply(action2)
	if err != nil {
		t.Fatal(err)
	}
	if tp2.ID != tp1.ID {
		t.Logf("SOFT: expected same topic, got different: %s vs %s", tp2.ID, tp1.ID)
	} else {
		t.Log("✓ Continue matched same topic")
	}
	_ = tracker.RecordTurnTokens(300)

	liveSleep(t)

	// Turn 3: Switch to new topic.
	topicCtx3 := tracker.FormatForAnalyser()
	r3 := analyser.Analyse(ctx, "另外帮我配置一下 Docker compose", nil, nil, topicCtx3)
	t.Logf("Turn 3: intent=%q tags=%v topic_action=%+v", r3.Intent, r3.Tags, r3.TopicAction)

	action3 := topic.Action{Type: topic.ActionNew, Title: "Docker配置"}
	if r3.TopicAction != nil && r3.TopicAction.Action == "new" && r3.TopicAction.Title != "" {
		action3.Title = r3.TopicAction.Title
	}
	tp3, err := tracker.Apply(action3)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Turn 3 topic: id=%s title=%q", tp3.ID, tp3.Title)
	if tp3.ID == tp1.ID {
		t.Log("SOFT: expected new topic for Docker, but LLM continued same")
	} else {
		t.Log("✓ New topic created for Docker")
	}

	// Verify tracker state.
	t.Logf("Current topic: %s", tracker.CurrentID())
	t.Logf("Final topic list:\n%s", tracker.FormatForAnalyser())
}

// ---------------------------------------------------------------------------
// TestLive_FactStore_Integration — FactStore with DigestWorker
// ---------------------------------------------------------------------------

func TestLive_FactStore_FormatForContext(t *testing.T) {
	// This is a pure unit test (no LLM needed) to verify context formatting.
	db, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(wal)")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := NewFactStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	// Simulate facts that DigestWorker would produce.
	fs.Upsert("parseConfig", "latency", "380ns", FactState, "")
	fs.Upsert("parseConfig", "approach", "sync.Pool", FactAppend, "")
	fs.Upsert("parseConfig", "approach", "type switch", FactAppend, "")
	fs.Upsert("deploy", "status", "running", FactEvent, "topic_abc")

	// Global context (no topic filter).
	globalCtx := fs.FormatForContext("")
	t.Logf("Global context:\n%s", globalCtx)
	if !strings.Contains(globalCtx, "parseConfig") {
		t.Error("expected parseConfig in global context")
	}
	if !strings.Contains(globalCtx, "380ns") {
		t.Error("expected 380ns in global context")
	}

	// Topic-scoped context.
	topicCtx := fs.FormatForContext("topic_abc")
	t.Logf("Topic context:\n%s", topicCtx)
	if !strings.Contains(topicCtx, "deploy") {
		t.Error("expected deploy in topic context")
	}
	if !strings.Contains(topicCtx, "parseConfig") {
		t.Error("expected global facts in topic context too")
	}
}
