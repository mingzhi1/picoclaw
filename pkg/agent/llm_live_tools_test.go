// Tool-exercising live tests — each case gets its own fresh session key so
// conversation history never bleeds between cases.
//
// Run single test:
//
//	go test ./pkg/agent/... -run TestLive_Tools_InstallSkill -v -timeout 90s
//
// Run all tool tests:
//
//	$env:LIVE_SLEEP="5s"; go test ./pkg/agent/... -run "TestLive_Tools" -v -timeout 300s
package agent

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// newSession returns a collision-resistant session key for live tests.
func newSession(label string) string {
	return fmt.Sprintf("live-%s-%s-%04x",
		label,
		time.Now().Format("150405"),
		rand.Uint32()&0xffff, //nolint:gosec — test use only
	)
}

// ---------------------------------------------------------------------------
// 1. Install skill
//    Agent must call find_skills → install_skill and report back.
// ---------------------------------------------------------------------------

func TestLive_Tools_InstallSkill(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Prompt is explicit so the agent is forced to call find_skills first, then install_skill.
	resp, err := al.ProcessDirect(ctx,
		"Search the skill registry for a skill related to 'weather'. "+
			"If you find any results, install the first one. "+
			"Tell me: what skill did you install, or what was not found?",
		newSession("install-skill"))
	if err != nil {
		t.Fatalf("ProcessDirect: %v", err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
	t.Logf("Response (%d chars):\n%s", len(resp), resp)

	lower := strings.ToLower(resp)
	// Soft check: response must acknowledge the skill search outcome.
	if !strings.Contains(lower, "skill") &&
		!strings.Contains(lower, "install") &&
		!strings.Contains(lower, "found") &&
		!strings.Contains(lower, "not found") {
		t.Logf("SOFT: expected skill search report, got: %s", resp)
	}
}

// ---------------------------------------------------------------------------
// 2. Fetch a website and extract specific content
//    Agent must call web_fetch and parse the JSON response body.
// ---------------------------------------------------------------------------

func TestLive_Tools_FetchWebsite(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// httpbin.org/get returns JSON containing the caller's IP in {"origin": "x.x.x.x"}
	// We ask for the exact field so the test checks that the agent reads & relays it.
	resp, err := al.ProcessDirect(ctx,
		`Fetch the URL https://httpbin.org/get using the web_fetch tool. `+
			`Find the "origin" field in the JSON and tell me its value. `+
			`Quote the value exactly.`,
		newSession("web-fetch"))
	if err != nil {
		t.Fatalf("ProcessDirect: %v", err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
	t.Logf("Response (%d chars):\n%s", len(resp), resp)

	// An IP address always contains digits and dots, e.g. "203.0.113.42"
	// Check for at least one digit followed by a dot.
	hasIPLike := false
	for i := 0; i < len(resp)-1; i++ {
		if resp[i] >= '0' && resp[i] <= '9' && resp[i+1] == '.' {
			hasIPLike = true
			break
		}
	}
	if !hasIPLike {
		t.Logf("SOFT: expected IP-like value in response, got: %s", resp)
	}
}

// ---------------------------------------------------------------------------
// 3. Check whether a (non-existent) file exists
//    Agent must call list_dir or read_file and report absence clearly.
// ---------------------------------------------------------------------------

func TestLive_Tools_CheckFileExists(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The workspace is a fresh TempDir — "hello.txt" will never be there.
	// Prompt tells the agent exactly what tool to use and what to report.
	resp, err := al.ProcessDirect(ctx,
		`Use list_dir to check if "hello.txt" exists in your workspace directory. `+
			`Then answer clearly: does it exist? (yes/no)`,
		newSession("check-file"))
	if err != nil {
		t.Fatalf("ProcessDirect: %v", err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
	t.Logf("Response (%d chars):\n%s", len(resp), resp)

	lower := strings.ToLower(resp)
	// Must contain an existence statement.
	if !strings.Contains(lower, "exist") &&
		!strings.Contains(lower, "no") &&
		!strings.Contains(lower, "not found") &&
		!strings.Contains(lower, "hello") {
		t.Errorf("expected file-existence statement, got: %s", resp)
	}
}

// ---------------------------------------------------------------------------
// 4. Write a file, then read it back in the SAME turn
//    Tests multi-iteration: write_file → read_file → stop.
//    Timeout set to 45s to allow up to 3 LLM iterations.
// ---------------------------------------------------------------------------

func TestLive_Tools_WriteFile(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Explicit two-step instruction so the model cannot short-circuit.
	resp, err := al.ProcessDirect(ctx,
		`Step 1: Write a file called "notes.txt" in your workspace with exactly this content: `+
			`"Hello from picoclaw test!"`+"\n"+
			`Step 2: Read "notes.txt" back with read_file and confirm the exact content to me.`,
		newSession("write-file"))
	if err != nil {
		t.Fatalf("ProcessDirect: %v", err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
	t.Logf("Response (%d chars):\n%s", len(resp), resp)

	// After reading back, the response must contain the exact written string.
	if !strings.Contains(resp, "Hello from picoclaw test!") &&
		!strings.Contains(strings.ToLower(resp), "hello from picoclaw") {
		t.Errorf("expected written content in response, got: %s", resp)
	}
}

// ---------------------------------------------------------------------------
// 5. Multi-turn: write → check → read  (same session, 3 separate turns)
//    This is the strongest test: verifies file persists across turns and
//    that the agent can answer questions about past actions.
// ---------------------------------------------------------------------------

func TestLive_Tools_WriteCheckRead(t *testing.T) {
	cfg := liveConfig(t)
	al := liveLoop(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sess := newSession("write-check-read")

	// Turn 1 — write
	r1, err := al.ProcessDirect(ctx,
		`Write a file called "version.txt" in your workspace with this exact content: "picoclaw version: 1.0.0"`,
		sess)
	if err != nil {
		t.Fatalf("T1 write: %v", err)
	}
	t.Logf("T1 (write): %s", r1)
	lower1 := strings.ToLower(r1)
	if !strings.Contains(lower1, "version") &&
		!strings.Contains(lower1, "written") &&
		!strings.Contains(lower1, "created") &&
		!strings.Contains(lower1, "saved") {
		t.Logf("SOFT T1: expected write confirmation, got: %s", r1)
	}

	liveSleep(t)

	// Turn 2 — check existence via list_dir
	r2, err := al.ProcessDirect(ctx,
		`Use list_dir on your workspace and tell me: is "version.txt" listed there?`,
		sess)
	if err != nil {
		t.Fatalf("T2 check: %v", err)
	}
	t.Logf("T2 (check): %s", r2)
	lower2 := strings.ToLower(r2)
	if !strings.Contains(lower2, "version") &&
		!strings.Contains(lower2, "yes") &&
		!strings.Contains(lower2, "listed") &&
		!strings.Contains(lower2, "exist") {
		t.Logf("SOFT T2: expected file confirmation, got: %s", r2)
	}

	liveSleep(t)

	// Turn 3 — read back and verify exact content
	r3, err := al.ProcessDirect(ctx,
		`Read "version.txt" and quote its exact content.`,
		sess)
	if err != nil {
		t.Fatalf("T3 read: %v", err)
	}
	t.Logf("T3 (read): %s", r3)
	// Hard check: must contain the version string we wrote.
	if !strings.Contains(r3, "1.0.0") {
		t.Errorf("T3 must contain '1.0.0'; got: %s", r3)
	}
}
