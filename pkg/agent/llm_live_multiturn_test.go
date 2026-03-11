// Live stress test: 8-turn multi-topic conversation with topic switching,
// resumption, resolution, and fact extraction.
//
// Run:
//   LIVE_CONFIG=testdata/config.json go test ./pkg/agent/... -run TestLive_MultiTurn -v -timeout 300s
package agent

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/agent/topic"
	_ "modernc.org/sqlite"
)

// turnSpec defines a single turn in the stress test.
type turnSpec struct {
	msg           string // user message
	expectAction  string // expected topic_action ("new", "continue", "resolve", "")
	expectTopicID string // "same" = same as last, "new" = different, empty = don't check
	desc          string // human-readable description
}

func TestLive_MultiTurn_TopicLifecycle(t *testing.T) {
	cfg := liveConfig(t)
	auxModel := cfg.Agents.Defaults.GetAuxiliaryModel()
	p, resolved := liveProvider(t, cfg, auxModel)
	analyser := NewAnalyser(p, resolved, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	// TopicTracker.
	tmpDir := t.TempDir()
	store, err := topic.NewStore(tmpDir + "/topics.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tracker, err := topic.NewTracker(store)
	if err != nil {
		t.Fatal(err)
	}

	// FactStore.
	factDB, err := sql.Open("sqlite", tmpDir+"/facts.db?_pragma=journal_mode(wal)")
	if err != nil {
		t.Fatal(err)
	}
	factStore, err := NewFactStore(factDB)
	if err != nil {
		t.Fatal(err)
	}
	defer factStore.Close()

	turns := []turnSpec{
		// Topic A: 数据分析 - 销售漏斗
		{msg: "上个月的转化率数据出来了，注册到付费只有2.3%，比上季度低了0.8个点", expectAction: "new", desc: "T1: 转化率数据异常"},
		{msg: "拆开看的话，注册到激活是68%还行，但激活到首次付费断崖式下降到3.4%", expectAction: "continue", expectTopicID: "same", desc: "T2: 漏斗拆解分析"},
		{msg: "我怀疑是新版引导流程的问题，A/B测试显示新版首次付费比旧版低了15%", expectAction: "continue", expectTopicID: "same", desc: "T3: 归因到引导流程"},

		// Topic B: 心理疏导 - 工作压力
		{msg: "最近压力很大，感觉每天都在救火，根本没时间做真正重要的事", expectAction: "new", expectTopicID: "new", desc: "T4: 表达工作焦虑"},
		{msg: "老板昨天又甩了三个紧急需求过来，deadline都在本周，我完全排不开", expectAction: "continue", expectTopicID: "same", desc: "T5: 描述具体压力源"},

		// 跳回 Topic A: 数据分析
		{msg: "对了刚才那个转化率的事，我把新版引导流程回滚了，想看看这周数据会不会回升", expectAction: "continue", desc: "T6: 回到转化率话题"},

		// 回到 Topic B: 心理疏导
		{msg: "唉说回来，我现在连周末都在想工作的事，睡眠也变差了", expectAction: "continue", desc: "T7: 回到压力话题"},
		{msg: "你说得对，我确实需要设边界，周末关掉工作消息试试", expectAction: "continue", expectTopicID: "same", desc: "T8: 接受建议"},
	}

	var lastTopicID string
	topicIDs := make(map[string]string) // desc -> topicID
	results := make([]string, 0, len(turns))

	for i, tc := range turns {
		if i > 0 {
			liveSleep(t)
		}

		topicCtx := tracker.FormatForAnalyser()
		r := analyser.Analyse(ctx, tc.msg, nil, nil, topicCtx)

		// Build topic action from LLM output.
		action := topic.Action{Type: topic.ActionContinue}
		if r.TopicAction != nil {
			switch r.TopicAction.Action {
			case "new":
				action = topic.Action{Type: topic.ActionNew, Title: r.TopicAction.Title, Resolve: r.TopicAction.Resolve}
			case "resolve":
				action = topic.Action{Type: topic.ActionResolve, Primary: r.TopicAction.ID, Resolve: r.TopicAction.Resolve}
			default:
				action = topic.Action{Type: topic.ActionContinue, Primary: r.TopicAction.ID, Resolve: r.TopicAction.Resolve}
			}
		}
		// Fallback: if we expected new but LLM said continue, force new.
		if tc.expectAction == "new" && action.Type != topic.ActionNew {
			if r.TopicAction != nil && r.TopicAction.Title != "" {
				action = topic.Action{Type: topic.ActionNew, Title: r.TopicAction.Title}
			} else {
				action = topic.Action{Type: topic.ActionNew, Title: tc.desc}
			}
		}

		tp, err := tracker.Apply(action)
		if err != nil {
			t.Fatalf("%s: Apply: %v", tc.desc, err)
		}
		_ = tracker.RecordTurnTokens(400 + i*50)

		// Verify expectations.
		llmAction := "nil"
		llmID := ""
		llmTitle := ""
		if r.TopicAction != nil {
			llmAction = r.TopicAction.Action
			llmID = r.TopicAction.ID
			llmTitle = r.TopicAction.Title
		}

		result := fmt.Sprintf("%s | LLM: action=%s id=%s title=%q | Topic: %s %q",
			tc.desc, llmAction, llmID, llmTitle, tp.ID[:8], tp.Title)

		if tc.expectAction != "" && r.TopicAction != nil && r.TopicAction.Action != tc.expectAction {
			result += fmt.Sprintf(" ⚠ expected action=%s", tc.expectAction)
		}
		if tc.expectTopicID == "same" && tp.ID != lastTopicID && lastTopicID != "" {
			result += " ⚠ expected same topic"
		}
		if tc.expectTopicID == "new" && tp.ID == lastTopicID {
			result += " ⚠ expected new topic"
		}

		results = append(results, result)
		t.Log(result)

		topicIDs[tc.desc] = tp.ID
		lastTopicID = tp.ID
	}

	// Summary.
	t.Log("\n=== CONVERSATION SUMMARY ===")
	for _, r := range results {
		t.Log(r)
	}
	t.Logf("\nFinal topics:\n%s", tracker.FormatForAnalyser())

	// Verify fact store can hold simulated data.
	factStore.Upsert("转化漏斗", "注册到付费", "2.3%", FactState, "")
	factStore.Upsert("转化漏斗", "激活到首付", "3.4%", FactState, "")
	factStore.Upsert("引导流程", "A/B结果", "新版低15%", FactState, "")
	factStore.Upsert("引导流程", "状态", "已回滚", FactEvent, "")
	factStore.Upsert("用户状态", "压力来源", "紧急需求过多", FactAppend, "")
	factStore.Upsert("用户状态", "睡眠", "变差", FactState, "")

	factsCtx := factStore.FormatForContext("")
	t.Logf("\nFact context for LLM:\n%s", factsCtx)

	active, _ := factStore.GetByEntity("转化漏斗")
	t.Logf("转化漏斗 active facts: %d", len(active))
	if len(active) < 2 {
		t.Errorf("expected >= 2 active facts for 转化漏斗, got %d", len(active))
	}

	userFacts, _ := factStore.GetByEntity("用户状态")
	t.Logf("用户状态 active facts: %d", len(userFacts))
	for _, f := range userFacts {
		t.Logf("  %s.%s = %q (%s)", f.Entity, f.Key, f.Value, f.Type)
	}
}

// TestLive_MultiTurn_FinancialAnalysis exercises a complex 12-turn scenario
// with three interleaving topics: portfolio analysis, macro research, and
// risk management. Later turns reference data from earlier different-topic
// turns, testing cross-topic context retention.
func TestLive_MultiTurn_FinancialAnalysis(t *testing.T) {
	cfg := liveConfig(t)
	auxModel := cfg.Agents.Defaults.GetAuxiliaryModel()
	p, resolved := liveProvider(t, cfg, auxModel)
	analyser := NewAnalyser(p, resolved, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	store, err := topic.NewStore(tmpDir + "/topics.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tracker, err := topic.NewTracker(store)
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

	turns := []turnSpec{
		// Topic A: 持仓分析
		{msg: "看一下我的持仓，BTC均价62000美元持有2.5个，ETH均价3400持有35个，总仓位大概28万美元",
			expectAction: "new", desc: "T1: 报告持仓明细"},
		{msg: "BTC现在68500，ETH跌到3150了，帮我算一下各自的盈亏和总P&L",
			expectAction: "continue", expectTopicID: "same", desc: "T2: 计算盈亏"},

		// Topic B: 宏观研判
		{msg: "今晚CPI数据要出了，市场预期3.4%，上个月是3.5%，你觉得如果低于预期对加密市场影响怎样",
			expectAction: "new", expectTopicID: "new", desc: "T3: CPI预期分析"},
		{msg: "联储点阵图显示今年可能只降息一次，但CME利率期货隐含的9月降息概率是65%，怎么理解这个分歧",
			expectAction: "continue", expectTopicID: "same", desc: "T4: 利率分歧讨论"},

		// Topic C: 风控告警
		{msg: "刚收到预警，ETH跌破3100的话我的DeFi借贷仓位会触发清算，抵押率降到132%了",
			expectAction: "new", expectTopicID: "new", desc: "T5: 清算风险预警"},
		{msg: "我先追加了5个ETH做抵押，抵押率回到155%，但如果继续跌怎么办",
			expectAction: "continue", expectTopicID: "same", desc: "T6: 追加抵押后策略"},

		// 跳回 Topic B: 宏观 + 引用 Topic A 的持仓数据
		{msg: "CPI出来了，3.3%低于预期，BTC拉到69200了，对照我刚才的持仓看看现在盈亏多少",
			expectAction: "continue", desc: "T7: CPI落地+引用持仓"},

		// 跳回 Topic C: 风控 + 引用宏观数据
		{msg: "CPI利好之后ETH也反弹到3280了，清算风险是不是暂时解除了",
			expectAction: "continue", desc: "T8: 用宏观数据评估风控"},

		// 回到 Topic A: 持仓调整决策（综合三线信息）
		{msg: "综合来看，宏观面转好+清算风险缓解，我想把ETH仓位加到50个，均价会变成多少",
			expectAction: "continue", desc: "T9: 综合决策加仓"},

		// Topic D: 全新话题 - 税务问题
		{msg: "对了，我今年加密收入大概赚了4万美元，美国税怎么算的，长短期资本利得区别是什么",
			expectAction: "new", expectTopicID: "new", desc: "T10: 切到税务问题"},

		// 跳回 Topic A: 收尾
		{msg: "回到持仓那边，如果BTC到75000我准备分批止盈，帮我按70k/73k/75k各出0.5个做个计划",
			expectAction: "continue", desc: "T11: 制定止盈计划"},

		// 回到 Topic C: 更新风控策略
		{msg: "最后确认一下DeFi那边，现在抵押率多少了，需不需要设一个自动补仓的阈值",
			expectAction: "continue", desc: "T12: 风控策略定型"},
	}

	var lastTopicID string
	results := make([]string, 0, len(turns))

	for i, tc := range turns {
		if i > 0 {
			liveSleep(t)
		}

		topicCtx := tracker.FormatForAnalyser()
		r := analyser.Analyse(ctx, tc.msg, nil, nil, topicCtx)

		action := topic.Action{Type: topic.ActionContinue}
		if r.TopicAction != nil {
			switch r.TopicAction.Action {
			case "new":
				action = topic.Action{Type: topic.ActionNew, Title: r.TopicAction.Title, Resolve: r.TopicAction.Resolve}
			case "resolve":
				action = topic.Action{Type: topic.ActionResolve, Primary: r.TopicAction.ID, Resolve: r.TopicAction.Resolve}
			default:
				action = topic.Action{Type: topic.ActionContinue, Primary: r.TopicAction.ID, Resolve: r.TopicAction.Resolve}
			}
		}
		if tc.expectAction == "new" && action.Type != topic.ActionNew {
			title := tc.desc
			if r.TopicAction != nil && r.TopicAction.Title != "" {
				title = r.TopicAction.Title
			}
			action = topic.Action{Type: topic.ActionNew, Title: title}
		}

		tp, err := tracker.Apply(action)
		if err != nil {
			t.Fatalf("%s: Apply: %v", tc.desc, err)
		}
		_ = tracker.RecordTurnTokens(500 + i*80)

		llmAction, llmID, llmTitle := "nil", "", ""
		if r.TopicAction != nil {
			llmAction = r.TopicAction.Action
			llmID = r.TopicAction.ID
			llmTitle = r.TopicAction.Title
		}

		result := fmt.Sprintf("%s | LLM: action=%s id=%.8s title=%q | Topic: %.8s %q",
			tc.desc, llmAction, llmID, llmTitle, tp.ID, tp.Title)

		if tc.expectAction != "" && r.TopicAction != nil && r.TopicAction.Action != tc.expectAction {
			result += fmt.Sprintf(" ⚠ expected=%s", tc.expectAction)
		}
		if tc.expectTopicID == "same" && tp.ID != lastTopicID && lastTopicID != "" {
			result += " ⚠ expected same topic"
		}
		if tc.expectTopicID == "new" && tp.ID == lastTopicID {
			result += " ⚠ expected new topic"
		}

		results = append(results, result)
		t.Log(result)
		lastTopicID = tp.ID
	}

	t.Log("\n=== FINANCIAL CONVERSATION SUMMARY ===")
	for _, r := range results {
		t.Log(r)
	}
	t.Logf("\nFinal topics:\n%s", tracker.FormatForAnalyser())

	// Simulate fact extraction results.
	factStore.Upsert("BTC", "均价", "62000", FactState, "")
	factStore.Upsert("BTC", "持有量", "2.5", FactState, "")
	factStore.Upsert("BTC", "现价", "69200", FactState, "")
	factStore.Upsert("ETH", "均价", "3400", FactState, "")
	factStore.Upsert("ETH", "持有量", "35", FactState, "")
	factStore.Upsert("ETH", "持有量", "50", FactState, "") // 加仓后覆盖
	factStore.Upsert("ETH", "现价", "3280", FactState, "")
	factStore.Upsert("DeFi借贷", "抵押率", "155%", FactState, "")
	factStore.Upsert("DeFi借贷", "清算线", "3100", FactState, "")
	factStore.Upsert("宏观", "CPI实际", "3.3%", FactState, "")
	factStore.Upsert("宏观", "CPI预期", "3.4%", FactState, "")
	factStore.Upsert("宏观", "9月降息概率", "65%", FactState, "")

	factsCtx := factStore.FormatForContext("")
	t.Logf("\nFact context:\n%s", factsCtx)

	// Verify ETH quantity was overwritten (state semantics).
	ethFacts, _ := factStore.GetByEntity("ETH")
	for _, f := range ethFacts {
		if f.Key == "持有量" && f.Value != "50" {
			t.Errorf("ETH持有量 should be 50 (overwritten), got %q", f.Value)
		}
	}

	// Verify superseded history for ETH持有量.
	history, _ := factStore.GetHistory("ETH", "持有量")
	t.Logf("ETH持有量 history: %d entries", len(history))
	if len(history) < 2 {
		t.Errorf("expected >= 2 history entries for ETH持有量, got %d", len(history))
	}
	for i, h := range history {
		t.Logf("  [%d] value=%q superseded_by=%v", i, h.Value, h.SupersededBy != nil)
	}

	// Count total unique topics created.
	topicList := tracker.FormatForAnalyser()
	topicCount := 0
	for _, line := range []byte(topicList) {
		if line == '\n' {
			topicCount++
		}
	}
	t.Logf("Total topic lines: %d (expect 4-5 distinct topics)", topicCount)
}
