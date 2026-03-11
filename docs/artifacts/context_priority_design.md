# MetaClaw 上下文优先级与记忆系统设计

## 上下文注入顺序（按优先级 + KV Cache 友好排列）

```
Position  Layer              Stability   Cache Hit   Priority
────────  ─────────────────  ──────────  ──────────  ────────
1         Identity + Rules   永不变      ~100%       最高
2         Skills Summary     很少变      ~95%        高
3         Active Facts       偶尔变      ~80%        高
4         Pinned Turns       几乎不变    ~75%        高
5         Topic Summary      compact后稳定 ~60%      中
6         Matched Turns      每轮可能变  ~10%        中
7         Recent 3 Turns     每轮变      ~0%         中
8         Runtime Context    每轮变      ~0%         低
9         Current Message    每轮变      ~0%         必须
```

原则: **稳定的放前面（缓存命中），动态的放后面（低缓存但高相关性）**

## 记忆四层栈

```
┌─────────────────────────────────────────────────────┐
│ Layer 4: Instant Context (每次请求构建, 不持久化)    │
│ FuzzyMemoryIndex.Search() 综合检索                  │
│ 输出: ranked turns + facts + topic summary          │
├─────────────────────────────────────────────────────┤
│ Layer 3: Topic Layer (会话级, topics 表)             │
│ 话题管理: active/idle/compacted/resolved            │
│ compact 时: 旧 turns → LLM 摘要 → fork 新 topic    │
├─────────────────────────────────────────────────────┤
│ Layer 2: Fact Layer (持久, memory_facts 表)          │
│ state 型覆盖 | append 型叠加 | event 型状态迁移      │
│ entity + key 唯一索引, superseded 链可追溯           │
├─────────────────────────────────────────────────────┤
│ Layer 1: Turn Layer (持久, turns 表 + turn_tags)     │
│ tags + score + 倒排索引 + 模糊匹配                  │
│ pending → processed → archived 生命周期              │
└─────────────────────────────────────────────────────┘
```

## Topic 生命周期

```
                    ┌─────────┐
      new topic ───→│ active  │←── 用户回到此话题
                    └────┬────┘
                         │ 5分钟无新 turn
                    ┌────▼────┐
                    │  idle   │ 后台预生成 summary
                    └────┬────┘
                         │ 用户说"继续" / 回溯
                    ┌────▼────┐
   shouldCompact()──│ compact │ turns>12 or tokens>40% or 跨4h
    = true          └────┬────┘
                         │ fork 到新 topic (带 summary)
                    ┌────▼────┐
                    │compacted│ 保留摘要, turns 可归档
                    └─────────┘
                    
                    ┌─────────┐
  用户说"搞定了" ──→│resolved │
                    └─────────┘
```

### Compact 触发条件 (硬指标, 不依赖 LLM)

```go
func shouldCompact(topic *Topic, contextWindow int) bool {
    return topic.TotalTokens > contextWindow*40/100 ||
           topic.TurnCount > 12 ||
           time.Since(topic.CreatedAt) > 4*time.Hour
}
```

## Fact 覆盖规则

```
Type      行为          例子
───────   ────────────  ─────────────────────────────────
state     新值覆盖旧值  latency: 850ns → 420ns → 380ns
append    新旧并存      优化手段: sync.Pool + type switch
event     状态迁移      status: needs_optimization → done
```

覆盖时旧事实标记 superseded (不删除, 可追溯)

## 重要记忆保留 (Pinned)

三类"特别重要"，compact 时不被摘要替代:

```
来源                  触发方式           持久策略
────────────────      ──────────────     ─────────────────────
高分 turn (score>=8)  Reflector 自动打分  pinned=1, 跨 topic 注入
用户 /pin 命令        用户手动标记        pinned=1, 永远注入
全局知识              Fact 层 (无 topic)  Active Facts, 始终可见
```

compact 规则: 普通 turns → 摘要覆盖可归档; pinned turns → 保留原文不被替代

buildContext 查 pinned 时不限 topic: `QueryByScore("", 8)` 查全库

## 模糊检索 (FuzzyMemoryIndex)


### 检索路径 (按权重)

```
路径               权重      方法
──────────────────  ────────  ─────────────────
精确 tag 匹配       1.0       QueryByTags (倒排索引)
模糊 tag 匹配       sim×1.0   Jaro-Winkler >= 0.80
实体名匹配          0.9       matchEntity >= 0.85
Topic 标题匹配      0.7       关键词相似度 >= 0.6
长期记忆 grep       0.5       正则匹配 facts
```

### 综合评分

```
最终分 = max(各路径分) × 时间衰减 × 评分权重

时间衰减: same_topic=1.0  <1h=0.9  <1d=0.7  <1w=0.4  >1w=0.2
评分权重: score>=8 → x1.5  score>=5 → x1.0  score<5 → x0.5
```

### 实体名归一化

```
输入 → lowercase → Jaro-Winkler 与已有实体比较
阈值 0.85: "ParseConfig" → "parseconfig" (合并)
           "parseCfg" → "parseconfig" (合并, sim=0.87)
           "config parser" → 新实体 (sim=0.72 < 0.85)
```

## Analyser 输出 (扩展后)

```json
// 单话题
{
  "intent": "code",
  "tags": ["go", "performance"],
  "topic": { "action": "continue", "id": "topic_abc123" },
  "entities": ["parseConfig", "sync.Pool"]
}

// 双话题 (primary + 1 ref)
{
  "intent": "code",
  "tags": ["go", "api"],
  "topic": {
    "action": "multi",
    "primary": "topic_C",
    "refs": ["topic_A"],
    "resolve": []
  },
  "entities": ["parseConfig"]
}

// 三话题 (= resolve旧 + ref中间 + new/continue主)
{
  "intent": "code",
  "tags": ["deploy"],
  "topic": {
    "action": "multi",
    "primary": "topic_C",
    "refs": ["topic_B"],
    "resolve": ["topic_A"]
  },
  "entities": []
}
```

零额外 LLM 调用，在现有 Analyser 的一次调用中完成

## 多话题 Turn 处理

### 频率分布

```
单话题 (~75%): continue / new  → 正常处理
双话题 (~20%): primary + 1 ref → multi 模式
三话题 ( ~5%): resolve+ref+new → multi 模式 (三话题=关旧+提中+开新)
```

> **三话题本质**: "A搞完了（resolve）—— B那个结果怎样（ref）—— 现在来做C（primary）"
> 不是随机乱跳，是有规律的话题收尾切换

### Context Token 分配

```
双话题:
  primary: summary + 最多5 turns    60% budget
  ref[0]:  summary only (1段)       25% budget
  matched turns:                     15%

三话题:
  primary: summary + 最多5 turns    60% budget
  ref[0]:  1句话摘要 (~100字)       10% budget
  resolve: 不注入 (已关闭, ignore)   0%
  matched turns:                     30%
```

### TopicAction 数据结构

```go
type TopicAction struct {
    Primary string   // 主 topic (必须, new or continue)
    Refs    []string // 需要 summary 的 topic (最多 1 个)
    Resolve []string // 顺手关掉的 topic (不注入 context)
}

func (a *Agent) applyTopicAction(action TopicAction) {
    for _, id := range action.Resolve {
        a.topicStore.SetStatus(id, "resolved") // 不需要 compact
    }
    a.topicStore.IdleAllExcept(action.Primary)
    a.topicStore.Activate(action.Primary)
}

func buildTopicContext(primary *Topic, refs []*Topic) string {
    var buf strings.Builder
    buf.WriteString(formatFullTopic(primary))      // summary + turns
    for _, ref := range refs {
        buf.WriteString(fmt.Sprintf(
            "\n> [背景] %s: %s\n",
            ref.Title, truncate(ref.Summary, 100))) // 一行摘要
    }
    // resolve 的: 什么都不写
    return buf.String()
}
```

### 降级策略

```go
// refs > 1 或 resolve > 2 → 说明消息太散，降级
if len(action.Refs) > 1 || len(action.Resolve) > 2 {
    action.Refs = nil
    action.Resolve = nil
    // 只保留 primary
}
```

## 上下文去重

### 重复来源

```
同一条 turn 可能从多个路径被选中:

  Source A: recent turns (最近3轮)     ← 必选
  Source B: matched turns (tag匹配)    ← 可能和 A 重叠
  Source C: pinned turns (score>=8)    ← 可能和 A/B 重叠
  Source D: topic summary              ← 是 turns 的压缩版

  A ∩ B ∩ C 可能都包含同一条 turn
  D 的内容 ⊇ 旧 turns 的要点
```

### 去重流程 (在 buildContext 中)

```go
func buildContext(topic *Topic, store *TurnStore) string {
    // ── Stage 1: 收集各来源 ──
    recent  := store.QueryRecent(3)                    // 最近 3 轮
    matched := store.QueryByTags(topic.Tags)           // tag 匹配
    pinned  := store.QueryByScore(topic.ChannelKey, 8) // 高分
    
    // ── Stage 2: Turn ID 去重 (recent 优先) ──
    seen := make(map[string]bool)
    for _, t := range recent {
        seen[t.ID] = true
    }
    var extra []TurnRecord
    for _, t := range append(matched, pinned...) {
        if !seen[t.ID] {
            extra = append(extra, t)
            seen[t.ID] = true
        }
    }
    
    // ── Stage 3: Summary 感知过滤 ──
    // 有 topic summary 时，旧 turn 的信息 summary 已覆盖
    // 只保留高分的额外 turn，低分的 summary 够了
    if topic.Summary != "" {
        var important []TurnRecord
        for _, t := range extra {
            if t.Score >= 8 {
                important = append(important, t)
            }
        }
        extra = important
    }
    
    // ── Stage 4: Token 预算裁剪 ──
    // 总 budget 内: summary + extra + recent 不超过 40% context
    budget := contextWindow * 40 / 100
    used := estimateTokens(topic.Summary)
    for i, t := range extra {
        cost := estimateTokens(t)
        if used + cost > budget {
            extra = extra[:i]
            break
        }
        used += cost
    }
    
    // ── 组装 ──
    var buf strings.Builder
    buf.WriteString(topic.Summary)       // 宏观方向
    for _, t := range extra {            // 高分补充
        buf.WriteString(formatTurn(t))
    }
    for _, t := range recent {           // 微观细节 (始终保留)
        buf.WriteString(formatTurn(t))
    }
    return buf.String()
}
```

### 规则总结

```
层级      去重策略                     原因
──────    ──────────────────────────   ──────────────────────────
recent    不去重，始终保留              LLM 需要连贯对话
matched   ID 去重 vs recent           防同一 turn 出现两次
pinned    ID 去重 vs recent+matched   同上
extra     有 summary 时只留 score>=8   低分 turn 的信息 summary 已包含
全部      token budget 裁剪           防溢出
```


## Turn 存储过滤

不对 LLM 回复做分类筛选（成本不对称），只做截断防膨胀:

```go
func sanitizeReply(reply string) string {
    if len(reply) > 2000 {
        return reply[:2000] + "\n...(truncated)"
    }
    return reply
}
```

DigestWorker 在消化阶段天然完成"筛选重要信息"的工作，不需要在存储时重复做。

## 持久化 (DB-backed Topic)

长对话场景进程会重启，topic 必须持久化。复用 turns.db，加两张小表:

```sql
CREATE TABLE IF NOT EXISTS topics (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',  -- active|idle|compacted|resolved
    summary       TEXT NOT NULL DEFAULT '',
    total_tokens  INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

-- turns 表直接包含 topic_id (不需要兼容旧版)
CREATE TABLE IF NOT EXISTS turns (
    ...
    topic_id TEXT NOT NULL DEFAULT '',
    ...
);
CREATE INDEX IF NOT EXISTS idx_turns_topic ON turns(topic_id);
```

```go
type TopicTracker struct {
    db      *sql.DB
    current string  // 内存缓存当前 topic ID
}

func (t *TopicTracker) SwitchTo(topicID string) {
    t.current = topicID
    t.db.Exec("UPDATE topics SET status='idle' WHERE status='active'")
    t.db.Exec("UPDATE topics SET status='active', updated_at=? WHERE id=?",
        time.Now().Unix(), topicID)
}

func (t *TopicTracker) Create(title string) string {
    id := NewTopicID()
    t.db.Exec("INSERT INTO topics VALUES (?,?,'active','',?,?)",
        id, title, time.Now().Unix(), time.Now().Unix())
    t.current = id
    return id
}

// 启动时恢复: 找最后活跃的 topic
func (t *TopicTracker) Restore() {
    t.db.QueryRow(
        "SELECT id FROM topics WHERE status='active' ORDER BY updated_at DESC LIMIT 1",
    ).Scan(&t.current)
    // 空则等 Analyser 下一轮自动创建
}
```

## LLM 职责分配

整个记忆系统只有 3 个 LLM 调用点:

### 同步 (每轮, 延迟敏感)

**LLM-1 Analyser** (fast model, ~500ms)
- 触发: 每条用户消息进来时
- 输入: 用户消息 + 当前活跃 topic 列表
- 输出: `{ intent, tags, topic_action }`
- 做: intent 分类, tag 提取, topic 判断
- 不做: 实体抽取, 关系分析, 摘要

**LLM-2 Executor** (capable model, 主模型)
- 触发: Analyser 之后
- 输入: system prompt + context(程序组装) + 用户消息
- 输出: 回复 + tool calls
- 做: 回答用户 / 执行工具
- 不做: 任何记忆管理

### 异步 (后台, 不阻塞用户)

**LLM-3 DigestWorker** (cheap model)
- 触发: topic idle 5分钟后 / 定时轮询
- 输入: 一批 pending turns + 现有 facts
- 输出: `{ topic_summary, updated_facts }`
- 做: 生成 topic summary, 提取/更新 facts, 筛选重要信息
- 不做: 实时响应

### 纯程序逻辑 (无 LLM)

```
shouldCompact()       — 硬指标: turns>12 / tokens>40% / 跨4h
dedup()               — Turn ID 精确去重
sanitizeReply()       — 截断 > 2000 字符
buildContext()        — 排序注入, 稳定前/动态后
topicTracker.switch() — DB 写穿 + 内存缓存
```

### 每轮时间线

```
t=0ms    用户消息 → 查 topic 列表 (内存, <1ms)
t=1ms    LLM-1 Analyser → intent + tags + topic (~500ms)
t=501ms  程序: applyTopic + shouldCompact + buildContext (<5ms)
t=510ms  LLM-2 Executor → 回复 (~2-5s)
t=3000ms 程序: sanitizeReply + 存 turn (<1ms) → 用户看到回复
...
t+5min   LLM-3 DigestWorker (后台) → summary + facts (~2s, 用户无感)
```

### 模型选择

```
角色        模型要求          推荐                  占总成本
──────      ──────────────    ──────────────────    ────────
LLM-1      快+结构化输出      GPT-4o-mini / Haiku   ~3%
LLM-2      强+工具调用        GPT-4o / Sonnet       ~92%
LLM-3      便宜+摘要能力      GPT-4o-mini / Haiku   ~5%
```

## 实现优先级（长对话 + 持久化修正）

```
Phase   Feature              LOC    持久化       收益     长对话必要性     状态
─────   ────────────────────  ─────  ──────────   ─────   ──────────      ────

MVP (必做):
  1     Topic Tracker (DB)    ~75    topics 表    *****   必须: 话题隔离   ✅ done
  2     shouldCompact         ~10    复用 topics  ****    必须: 防溢出     ✅ done (逻辑 + loop 接入)
  3     Context 排序 (cache)  ~0     无           ****    必须: 免费收益   ✅ done (BuildPhase2Messages)
  4     上下文去重            ~10    无           ***     必须: 防重复注入 ✅ done (BuildInstantMemory dedup)
  5     Reply 截断            ~5     无           **      必须: 防膨胀     ✅ done (sanitizeReply/sanitizeUserMsg)
  6     Fact 覆盖             ~40    facts 表     ***     必须: 事实管理   ⬜ next
─────                        ─────
MVP 总计                      ~140

按需扩展:
  7     Multi-Topic Turn      ~50    无           **      可选: primary降级够用

过度设计 (有 Topic 层后价值极低):
  8     FuzzyMemoryIndex      ~80    无           *       Topic 已解决检索精度
  9     实体图/Jaro-Winkler   ~90    无           *       收益不抵复杂度
```

## 实现 FAQ

### Q1: Analyser 怎么知道已有 topic 列表？

只传最近 5 个非 resolved 的 topic，格式精简:

```
Analyser prompt 注入:

当前话题列表:
- [topic_A] Go函数性能优化 (active, 5min ago)
- [topic_B] API集成 (idle, 2h ago)
- [topic_C] 部署脚本 (idle, 1d ago)
```

```go
func topicListForAnalyser(db *sql.DB) string {
    rows, _ := db.Query(`
        SELECT id, title, status, updated_at FROM topics
        WHERE status IN ('active','idle')
        ORDER BY updated_at DESC LIMIT 5`)
    // 格式化为上面的文本
}
```

不传 summary（太长），不传 resolved（已关闭）。LLM 靠 title 匹配。

### Q2: 首轮 cold start

```go
// Analyser 返回 topic_action 时:
// 如果没有匹配的已有 topic → action="new"
// title 由 LLM 生成（Analyser prompt 里加一句）:
//   "如果是新话题，生成一个简短标题 (5字以内)"

// 完整的 topic_action 输出:
// 新话题: { "action": "new", "title": "Go性能优化" }
// 已有:   { "action": "continue", "id": "topic_A" }
```

Restore() 恢复旧 topic 后，如果用户消息跟旧 topic 无关，
Analyser 正常判断为 `new`，旧 topic 自动 idle。不需要特殊处理。

### Q3: shouldCompact 同步还是异步？

**异步，分两种情况:**

```go
func (a *Agent) handleCompact(topic *Topic) {
    if !shouldCompact(topic, a.contextWindow) {
        return
    }
    
    if topic.Summary != "" {
        // 情况 A: 已有 summary (之前 idle 时后台生成的)
        // → 直接用，零延迟
        a.topicTracker.SetStatus(topic.ID, "compacted")
        return
    }
    
    // 情况 B: 没有 summary (比如一直在聊没 idle 过)
    // → 标记 dirty，后台 DigestWorker 会处理
    // → 本轮先用 recent turns 撑着，下一轮 summary 就好了
    topic.SummaryDirty = true
}
```

**永远不同步阻塞用户。** 最差情况是本轮没 summary，下一轮就有了。

### Q4: topic.TotalTokens 怎么维护？

topics 表加一列，每次 insert turn 时递增:

```sql
ALTER TABLE topics ADD COLUMN total_tokens INTEGER DEFAULT 0;
```

```go
// 在 TurnStore.Insert 里，存完 turn 后:
if r.TopicID != "" {
    s.db.Exec("UPDATE topics SET total_tokens = total_tokens + ?, updated_at = ? WHERE id = ?",
        r.Tokens, time.Now().Unix(), r.TopicID)
}
```

不需要每次 SUM 查询，O(1) 递增。

### Q5: buildContext 和 buildTopicContext 合并

只有一个入口 `buildContext`，multi-topic 是其内部分支:

```go
func buildContext(action TopicAction, store *TurnStore, tracker *TopicTracker) string {
    primary := tracker.Get(action.Primary)
    
    // Stage 1-4: 正常的 dedup 流程 (对 primary topic)
    recent  := store.QueryRecent(3)
    matched := store.QueryByTags(primary.Tags)
    extra   := dedup(recent, matched)
    extra    = summaryFilter(extra, primary.Summary)
    extra    = budgetTrim(extra, contextWindow)
    
    // 组装 primary
    var buf strings.Builder
    buf.WriteString(primary.Summary)
    for _, t := range extra  { buf.WriteString(formatTurn(t)) }
    for _, t := range recent { buf.WriteString(formatTurn(t)) }
    
    // 如果有 ref topic → 追加一行摘要
    for _, refID := range action.Refs {
        ref := tracker.Get(refID)
        buf.WriteString(fmt.Sprintf("\n> [背景] %s: %s\n",
            ref.Title, truncate(ref.Summary, 100)))
    }
    
    return buf.String()
}
```

### Q6: Fact 的 entity+key 谁生成？

DigestWorker 的 LLM 输出用 JSON tool call:

```python
_DIGEST_TOOL = {
    "name": "save_digest",
    "parameters": {
        "summary": "string: 话题进展摘要",
        "facts": [{
            "entity": "string: 相关对象名 (函数名/文件名/概念)",
            "key":    "string: 属性名 (status/latency/approach)",
            "value":  "string: 当前值",
            "type":   "state | append | event"
        }]
    }
}
```

```
DigestWorker prompt:
"提取关键事实，每条事实包含:
 - entity: 具体对象 (parseConfig, docker, bug#123)
 - key: 属性 (status, latency, error_cause)
 - value: 当前值
 - type: state(会变的数值) / append(累加的列表) / event(状态变化)"
```

LLM 输出不准怎么办？**容错:**
- entity 为空 → 跳过
- key 为空 → 跳过
- type 不在枚举里 → 默认 state

### Q7: 旧数据兼容

**不需要兼容旧版本。** 所有表从零建起，topic_id 直接内建到 turns 的 CREATE TABLE 中。
