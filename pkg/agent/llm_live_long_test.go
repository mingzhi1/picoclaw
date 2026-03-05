// Long-conversation live tests — multi-turn dialogues to stress-test
// context retention, token growth, and agent coherence under extended history.
//
// Run:
//
//	$env:LIVE_SLEEP="4s"; go test ./pkg/agent/... -run "TestLive_Long" -v -timeout 600s
package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 1. 知识积累对话 (8 turns)
//    每轮告诉 agent 一个新事实，最后测试它能否一次性回忆全部
// ---------------------------------------------------------------------------

func TestLive_Long_KnowledgeAccumulation(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	// 8 turns × (Analyser~2s + Executor~4s) + 7 sleeps × 3s + recall ≈ 90s nominal, allow 300s for slow API
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	sess := newSession("knowledge")

	facts := []struct {
		tell   string
		expect string // substring expected in final recall
	}{
		{"My project is called PicoClaw.", "picoclaw"},
		{"The primary language is Go.", "go"},
		{"The main model is qwen3-max.", "qwen"},
		{"The codebase lives at d:\\pilot2cli\\picoclaw.", "pilot2cli"},
		{"There are three agent phases: Analyse, Execute, Reflect.", "analys"},
		{"The auxiliary model handles Phase 1 (Analyser).", "auxiliar"},
		{"Token budget per turn is 32768.", "32768"},
		{"The project started in March 2026.", "march"},
	}

	const ackSleep = 3 * time.Second // shorter sleep: pure ack turns, no tool calls
	for i, f := range facts {
		if i > 0 {
			t.Logf("sleeping %s (rate-limit buffer)...", ackSleep)
			time.Sleep(ackSleep)
		}
		resp, err := al.ProcessDirect(ctx,
			fmt.Sprintf("Remember this fact #%d: %s Acknowledge with 'Got it.'", i+1, f.tell),
			sess)
		if err != nil {
			t.Fatalf("turn %d: %v", i+1, err)
		}
		t.Logf("T%d ACK: %s", i+1, resp)
	}

	liveSleep(t) // full sleep before recall (allows Reflector to run)

	// Final recall turn
	recall, err := al.ProcessDirect(ctx,
		"Please list ALL the facts I told you (facts #1 through #8). Be complete.",
		sess)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	t.Logf("RECALL (%d chars):\n%s", len(recall), recall)

	lower := strings.ToLower(recall)
	var missed []string
	for _, f := range facts {
		if !strings.Contains(lower, f.expect) {
			missed = append(missed, f.expect)
		}
	}
	if len(missed) > 0 {
		t.Errorf("recall missing keywords: %v\nfull recall: %s", missed, recall)
	}
}

// ---------------------------------------------------------------------------
// 2. 代码迭代对话 (6+1 turns)
//    第一轮写一个基础结构，后续每轮 append 一个新函数，最后验证完整文件
// ---------------------------------------------------------------------------

func TestLive_Long_CodeIteration(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	// 7 turns × (2+5)s + 6 sleeps × 4s ≈ 73s, allow 240s
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	sess := newSession("code-iter")

	turns := []struct {
		msg     string
		wantKey string
	}{
		{`Write a Go file "calc.go" in your workspace with just a package declaration: package main`, "package"},
		{`Append a function Add(a, b int) int { return a + b } to "calc.go".`, "Add"},
		{`Append a function Sub(a, b int) int { return a - b } to "calc.go".`, "Sub"},
		{`Append a function Mul(a, b int) int { return a * b } to "calc.go".`, "Mul"},
		{`Append a function Div(a, b float64) float64 { return a / b } to "calc.go".`, "Div"},
		{`Read "calc.go" and show me its full content.`, "func"},
	}

	for i, turn := range turns {
		if i > 0 {
			liveSleep(t)
		}
		resp, err := al.ProcessDirect(ctx, turn.msg, sess)
		if err != nil {
			t.Fatalf("T%d: %v", i+1, err)
		}
		t.Logf("T%d (%d chars): %.300s", i+1, len(resp), resp)
	}

	// Extra verification turn
	liveSleep(t)
	content, err := al.ProcessDirect(ctx,
		`Read "calc.go" again. Does it contain Add, Sub, Mul, and Div? Reply yes or no then show the file.`,
		sess)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	t.Logf("VERIFY (%d chars):\n%s", len(content), content)

	lower := strings.ToLower(content)
	for _, fn := range []string{"add", "sub", "mul", "div"} {
		if !strings.Contains(lower, fn) {
			t.Errorf("final calc.go missing function: %s", fn)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. 角色扮演 + 上下文记忆 (7+1 turns)
//    Agent 扮演虚构的技术顾问，每轮追问，最终验证身份一致性
// ---------------------------------------------------------------------------

func TestLive_Long_RoleplayConsistency(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	// 8 turns × 6s + 7 sleeps × 4s ≈ 76s, allow 240s
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	sess := newSession("roleplay")

	msgs := []string{
		// Turn 1 — establish persona
		"For this conversation, please act as 'Alex', a senior Go architect with 10 years of experience. " +
			"Introduce yourself briefly.",
		// Turn 2
		"Alex, what is your opinion on using goroutines for I/O-bound tasks?",
		// Turn 3
		"What about using channels vs mutexes for shared state?",
		// Turn 4
		"Alex, can you recommend a pattern for graceful shutdown in a long-running service?",
		// Turn 5
		"Now let's talk about testing. What's your preferred approach for integration tests in Go?",
		// Turn 6
		"Alex, how would you handle context cancellation propagation across multiple goroutines?",
		// Turn 7 — consistency check embedded in conversation
		"Finally: what is your name, how many years of experience do you have, and what language do you specialise in?",
	}

	for i, msg := range msgs {
		if i > 0 {
			liveSleep(t)
		}
		resp, err := al.ProcessDirect(ctx, msg, sess)
		if err != nil {
			t.Fatalf("T%d: %v", i+1, err)
		}
		t.Logf("T%d (%d chars): %.400s", i+1, len(resp), resp)
	}

	// Extra consistency check
	liveSleep(t)
	lastResp, err := al.ProcessDirect(ctx,
		"One more: remind me your name and your specialty language in one sentence.",
		sess)
	if err != nil {
		t.Fatalf("consistency check: %v", err)
	}
	t.Logf("CONSISTENCY: %s", lastResp)

	lower := strings.ToLower(lastResp)
	if !strings.Contains(lower, "alex") {
		t.Errorf("consistency: name 'Alex' missing; got: %s", lastResp)
	}
	if !strings.Contains(lower, "go") {
		t.Logf("SOFT: expected 'Go' in consistency answer; got: %s", lastResp)
	}
}

// ---------------------------------------------------------------------------
// 4. 任务规划 + 执行跟踪 (5 turns)
//    Agent 先制定计划，然后逐步执行并记录结果
// ---------------------------------------------------------------------------

func TestLive_Long_PlanAndExecute(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	// 5 turns × 6s + 4 sleeps × 4s ≈ 46s, allow 180s
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	sess := newSession("plan-exec")

	// Turn 1: ask for a plan (text only, no tool call expected)
	plan, err := al.ProcessDirect(ctx,
		"I need you to create a simple project structure.\n"+
			"Make a plan with exactly 3 steps to:\n"+
			"1. Describe folder structure\n"+
			"2. Write a README.md\n"+
			"3. Write a main.go with Hello World\n\n"+
			"List the steps as 'Step 1:', 'Step 2:', 'Step 3:' — describe only, don't act yet.",
		sess)
	if err != nil {
		t.Fatalf("T1 plan: %v", err)
	}
	t.Logf("T1 PLAN (%d chars):\n%.500s", len(plan), plan)
	if !strings.Contains(plan, "Step 1") || !strings.Contains(plan, "Step 2") || !strings.Contains(plan, "Step 3") {
		t.Logf("SOFT: plan missing step markers, got: %s", plan)
	}

	liveSleep(t)

	// Turn 2: Execute step 2 (README)
	r2, err := al.ProcessDirect(ctx,
		"Execute Step 2: write README.md in your workspace with title '# Hello Project' and a one-line description.",
		sess)
	if err != nil {
		t.Fatalf("T2: %v", err)
	}
	t.Logf("T2 README: %.300s", r2)

	liveSleep(t)

	// Turn 3: Execute step 3 (main.go)
	r3, err := al.ProcessDirect(ctx,
		"Execute Step 3: write main.go in your workspace with this exact content:\n"+
			"package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"Hello, World!\") }",
		sess)
	if err != nil {
		t.Fatalf("T3: %v", err)
	}
	t.Logf("T3 MAIN: %.300s", r3)

	liveSleep(t)

	// Turn 4: Verify both files via list_dir
	r4, err := al.ProcessDirect(ctx,
		`Use list_dir on your workspace. Are README.md and main.go both listed?`,
		sess)
	if err != nil {
		t.Fatalf("T4: %v", err)
	}
	t.Logf("T4 LIST: %s", r4)
	lower4 := strings.ToLower(r4)
	if !strings.Contains(lower4, "readme") {
		t.Errorf("T4 missing README.md: %s", r4)
	}
	if !strings.Contains(lower4, "main") {
		t.Errorf("T4 missing main.go: %s", r4)
	}

	liveSleep(t)

	// Turn 5: Summary
	r5, err := al.ProcessDirect(ctx,
		"Give me a one-paragraph summary of what we accomplished in this conversation.",
		sess)
	if err != nil {
		t.Fatalf("T5: %v", err)
	}
	t.Logf("T5 SUMMARY (%d chars): %s", len(r5), r5)
	if len(r5) < 50 {
		t.Errorf("summary too short (%d chars)", len(r5))
	}
}
