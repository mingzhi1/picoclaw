# PicoClaw 配置简化计划

> 目标：将 PicoClaw 的首次配置体验从"读 400 行 JSON 后才能跑"降低到"一条命令 + 一个 API Key 即可启动"。

## 1. 现状诊断

### 1.1 当前配置层级统计

| 模块 | 结构体数量 | JSON 字段总数 | 用户通常需要改的 |
|------|-----------|-------------|----------------|
| **AgentDefaults** | 1 | 17 | 2-3 |
| **Providers** | 20 | ~80 | 1-2 |
| **Channels** | 14 | ~120 | 3-5 |
| **Tools** | 10 | ~40 | 0-2 |
| **ModelList** | 19 条默认 | ~95 | 1 |
| **其他** | 5 | ~15 | 0 |
| **合计** | ~50 | **~370** | **6-12** |

### 1.2 核心痛点

1. **defaults.go 有 379 行** — 启动就生成含 19 个空 ModelConfig 的 config.json
2. **双轨制** — ProvidersConfig(旧) 和 ModelList(新) 并存，migration.go 403 行
3. **模型字段历史债务** — 6 个字段做同一件事
4. **Channel 配置爆炸** — 14 个 channel 各自独立 struct
5. **环境变量命名过长** — 如 `PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_MAX_RESPONSE_SIZE`

---

## 2. 简化方案

### Phase 1: 快速启动体验 (v1.x, 1-2 周)

#### 2.1 新增 `picoclaw init` 交互式初始化

```
$ picoclaw init

  Welcome to PicoClaw!

? Choose your LLM provider:
  > DeepSeek (recommended, cheapest)
    OpenAI / Anthropic / Qwen / Gemini
    Ollama (local, free)
    Other OpenAI-compatible

? Paste your API key: sk-xxx

? Choose a messaging channel:
  > Telegram
    Discord / Skip (CLI only)

? Paste your Telegram bot token: 123456:ABC...

  Config saved to ~/.picoclaw/config.json
```

实现要点:
- 在 cmd/metaclaw/internal/ 下新增 init/command.go
- 生成的 config.json 只包含用户选择的字段
- 依赖 charmbracelet/huh 做终端交互

#### 2.2 最小配置文件支持

目标: 3 行 JSON 就能启动完整实例:

```jsonc
{
  "model_list": [
    { "model_name": "main", "model": "deepseek/deepseek-chat", "api_key": "sk-xxx" }
  ],
  "channels": {
    "telegram": { "enabled": true, "token": "123456:ABC..." }
  }
}
```

当前阻碍: DefaultConfig() 写入 19 个空 ModelConfig
修复: 默认 ModelList 改为空 [], 示例放 config.example.jsonc

#### 2.3 短环境变量别名

```bash
# 现在 (37 字符)
PICOCLAW_AGENTS_DEFAULTS_PRIMARY_MODEL=deepseek-chat

# 新增短别名 (8 字符)
PC_MODEL=deepseek-chat
PC_API_KEY=sk-xxx
PC_CHANNEL=telegram
PC_TG_TOKEN=123456:ABC...
PC_PROXY=http://127.0.0.1:7890
```

---

### Phase 2: 消除双轨制 (v1.x, 2-3 周)

#### 2.4 废弃 ProvidersConfig

| 步骤 | 行动 | 涉及文件 |
|------|------|---------|
| 1 | ProvidersConfig 标记 Deprecated | types_providers.go |
| 2 | LoadConfig 加 warning 日志 | load.go |
| 3 | 新增 picoclaw migrate-config | cmd/migrate_config.go |
| 4 | v2.0 移除 ProvidersConfig 和 migration.go | 净减 ~500 行 |

#### 2.5 清理模型字段历史债务

```diff
 type AgentDefaults struct {
     PrimaryModel   string `json:"primary_model,omitempty"`
     AuxiliaryModel string `json:"auxiliary_model,omitempty"`
-    ModelName      string `json:"model_name,omitempty"`     // Deprecated
-    Model          string `json:"model,omitempty"`          // Deprecated
-    AnalyserModel  string `json:"analyser_model,omitempty"` // Deprecated
-    PreLLMModel    string `json:"pre_llm_model,omitempty"`  // Deprecated
-    DigestModel    string `json:"digest_model,omitempty"`   // Deprecated
 }
```

迁移策略:
1. LoadConfig 中旧字段值自动写入新字段
2. SaveConfig 只序列化新字段
3. 保留 1 个版本的读兼容, 下个大版本移除

预计净减: ~80 行 (含 Getter 方法简化)

#### 2.6 新增 picoclaw doctor

```
$ picoclaw doctor

  Config: ~/.picoclaw/config.json
  [WARN] Using deprecated "providers" config, run `picoclaw migrate-config`
  [WARN] Field "model_name" is deprecated, use "primary_model"
  [OK]   Model "deepseek-chat" reachable (238ms)
  [OK]   Telegram bot token valid
  [FAIL] Brave API key missing (web search disabled)
```

---

### Phase 3: Channel 动态注册表 (v2.0, 远期)

#### 2.7 统一 Channel 入口

当前: 14 个 Channel 各一个 struct, defaults.go ~120 行初始化
目标: 按需加载, 只有用户配置了的 channel 才实例化

```go
// 新设计: 统一的 ChannelEntry
type ChannelEntry struct {
    Type    string         `json:"type"`    // "telegram", "discord", etc.
    Enabled bool           `json:"enabled"`
    Config  map[string]any `json:"config"`  // 动态配置
}
```

收益:
- types_channels.go: 259 行 -> ~40 行
- defaults.go Channel 部分: ~120 行 -> 0 行
- 新增 Channel 不需要改配置代码

风险: 破坏性变更大, 需要完整迁移路径
建议: Phase 1-2 先行, 作为 v2.0 breaking change

---

## 3. 实施路线图

```
Phase 1 (v1.x - 1~2 周)
+-- [P0] 默认 ModelList 改为空 []
+-- [P0] 新增 picoclaw init 交互式初始化
+-- [P1] 新增 config.example.jsonc 示例文件
+-- [P1] 新增短环境变量别名

Phase 2 (v1.x - 2~3 周)
+-- [P0] 标记 ProvidersConfig deprecated + warning
+-- [P1] 新增 picoclaw migrate-config 命令
+-- [P1] 清理模型字段历史债务
+-- [P2] 新增 picoclaw doctor

Phase 3 (v2.0 - 远期)
+-- [P0] Channel 注册表模式重构
+-- [P1] 移除 ProvidersConfig 和 migration.go
+-- [P2] Tools 配置也改为按需加载
```

## 4. 量化预期

| 指标 | 现在 | Phase 1 | Phase 2 | Phase 3 |
|------|------|---------|---------|---------|
| 首次配置最少行数 | ~20 行 | **3 行** | 3 行 | 3 行 |
| defaults.go 行数 | 379 | ~50 | ~40 | ~20 |
| migration.go 行数 | 403 | 403 | 403+warn | **删除** |
| types_channels.go | 259 | 259 | 259 | **~40** |
| types_providers.go | 114 | +deprecated | 114 | **删除** |
| Config struct 总字段 | ~370 | ~370 | ~300 | **~80** |
| 最短环境变量长度 | 37 字符 | **8 字符** | 8 字符 | 8 字符 |
