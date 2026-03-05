<div align="center">
<img src="assets/logo.jpg" alt="MetaClaw" width="512">

<h1>MetaClaw: 基于 Go 语言的 AI 助手</h1>

<h3>Go 原生 · AI 自举 · 跨平台 · 皮皮虾，我们走！</h3>

  <p>
    <img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go&logoColor=white" alt="Go">
    <img src="https://img.shields.io/badge/Arch-x86__64%2C%20ARM64%2C%20RISC--V-blue" alt="Hardware">
    <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  </p>

**中文** | [English](README.md)

</div>

---

## MetaClaw 是什么？

MetaClaw 是一个使用 **Go 语言** 从零构建的个人 AI 助手。它源自 [PicoClaw](https://github.com/sipeed/picoclaw)（受 [nanobot](https://github.com/HKUDS/nanobot) 启发），并逐步演进为一个拥有独立架构和设计哲学的项目。

**PicoClaw 聚焦于超轻量部署，MetaClaw 聚焦于 _元认知_ — 即 Agent 对自身思维过程的推理能力。**

名字中的 "Meta" 精确反映了本项目的独特之处：

- **元认知 (Meta-cognition)** — 三阶段架构（分析 → 执行 → 反思）本质上是一个"思考如何思考"的系统
- **元数据驱动 (Meta-data)** — 意图、标签、思维链提示 — 一切都是关于对话本身的结构化元数据
- **元编程 (Meta-programming)** — 通过 AI 自举构建，Agent 驱动了自身的架构演进

**核心原则：**

- **Go 原生** — 单一二进制文件，无外部运行时，跨 RISC-V / ARM / x86 运行
- **分层而非单体** — 每个阶段是独立、可测试的组件
- **安全优先** — 4 层文件系统沙箱 + 命令守卫与允许/拒绝模式
- **渠道无关** — 同一套 Agent 逻辑服务于 CLI、Telegram、Discord、钉钉、LINE、企微等
- **扩展优先** — 可选能力（硬件设备、语音）通过统一生命周期框架接入

## 🧠 它如何思考 — 三阶段架构

每次用户交互都经过三个独立阶段。这种分离允许使用专用模型、可观测的中间状态，以及高效的 token 使用。

```
┌──────────────────────────────────────────────────────────┐
│                       入口层                              │
│  Telegram · Discord · 钉钉 · LINE · CLI · MaixCam       │
└────────────────────────┬─────────────────────────────────┘
                         │  MessageBus (发布/订阅)
                         ▼
┌──────────────────────────────────────────────────────────┐
│                    AgentLoop                              │
│                                                          │
│  ┌──────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │ 阶段 1   │  │   阶段 2     │  │     阶段 3       │   │
│  │ 分析     │→ │   执行       │→ │     反思         │   │
│  │          │  │              │  │                  │   │
│  │ 意图     │  │ LLM ↔ 工具  │  │ 评分 + 持久化    │   │
│  │ 标签     │  │ 迭代调用     │  │ TurnStore        │   │
│  │ 思维链   │  │ 多智能体     │  │ ActiveContext    │   │
│  └──────────┘  └──────────────┘  └──────────────────┘   │
└──────────────────────────────────────────────────────────┘
          │                    │                │
  ┌───────────┐       ┌──────────────┐  ┌────────────┐
  │   工具    │       │    记忆      │  │   技能     │
  │  注册表   │       │  TurnStore   │  │  3层加载   │
  │ (+ MCP)   │       │  KVCache     │  │            │
  └───────────┘       │  Digest      │  └────────────┘
                      └──────────────┘
```

### 阶段 1：分析 (Analyse)

快速、低成本的 **辅助模型** 在主 LLM 运行前提取结构化元数据。

- 从用户消息中提取 `{ intent, tags[], cot_prompt }`
- 通过关键词匹配技能 → 注入工具执行计划
- 生成针对性的 **思维链 (CoT) 策略** 传递给阶段 2
- 注入**历史使用统计**保持分析器一致性

### 阶段 2：执行 (Execute)

**主模型** 通过工具调用进行核心推理的迭代循环。

- 组装上下文：系统提示 + 活动上下文 + 即时记忆（相关历史轮次）+ 用户消息
- 历史消息标注 `[turn intent=code tags=[go]]`，提供跨轮次感知
- 阶段 1 的 CoT 提示注入为 `## Thinking Strategy` 段落
- 通过 `AgentRegistry` 进行多智能体路由

### 阶段 3：反思 (Reflect)

轮次后评估，同步评分 + 异步持久化。

- **同步** (<2ms): 轮次评分决定该轮是否"永久保留"在未来上下文中
- **异步**: `TurnStore.Insert()` → SQLite + 标签倒排索引（告别 `LIKE '%tag%'` 全表扫描）
- 后台 `MemoryDigestWorker` 将持久事实提取到长期记忆

### 记忆层级

| 层级 | 作用域 | 用途 |
|------|--------|------|
| **即时记忆** | 每轮 | TurnStore 动态窗口（分数 ≥ 阈值 + 标签匹配） |
| **活动上下文** | 每会话 | 当前文件 + 最近错误，注入为用户消息 |
| **长期记忆** | 持久化 | MemoryDigest 批量提取，按标签可搜索 |
| **KV 缓存** | 持久化 | 写穿透（内存 + SQLite），O(1) 读取，惰性 TTL |

## 📦 安装

### 从源码构建

```bash
git clone https://github.com/user/metaclaw.git
cd metaclaw && task deps && task build
```

### Docker

```bash
git clone https://github.com/user/metaclaw.git && cd metaclaw
docker compose -f docker/docker-compose.yml --profile gateway up   # 首次运行：生成配置
vim docker/data/config.json                                        # 设置 API Key
docker compose -f docker/docker-compose.yml --profile gateway up -d # 启动
```

## 🚀 快速开始

**1. 初始化**

```bash
metaclaw onboard
```

**2. 配置** (`~/.metaclaw/config.json`)

```json
{
  "model_list": [
    {
      "model_name": "gpt4",
      "model": "openai/gpt-5.2",
      "api_key": "your-api-key"
    }
  ],
  "agents": {
    "defaults": {
      "model_name": "gpt4"
    }
  }
}
```

> 使用 `厂商/模型` 格式（如 `zhipu/glm-4.7`、`anthropic/claude-sonnet-4.6`）— 添加新 Provider 无需改代码。

**3. 对话**

```bash
metaclaw agent -m "你好"
```

## 💬 支持的渠道

| 渠道 | 难度 | 说明 |
|------|------|------|
| **Telegram** | ⭐ 简单 | 推荐。长轮询，无需公网 IP |
| **Discord** | ⭐ 简单 | Socket Mode，群组/私信 |
| **Slack** | ⭐ 简单 | Socket Mode，无需公网 IP |
| **WhatsApp** | ⭐ 简单 | 原生 (whatsmeow) 或桥接 |
| **QQ** | ⭐⭐ 中等 | 官方机器人 API |
| **钉钉** | ⭐⭐ 中等 | Stream 模式，无需公网 |
| **企业微信** | ⭐⭐⭐ 较难 | 群机器人 / 自建应用 / AI Bot |
| **LINE** | ⭐⭐⭐ 较难 | 需要 HTTPS Webhook |
| **飞书** | ⭐⭐⭐ 较难 | WebSocket/SDK 模式 |
| **OneBot** | ⭐⭐ 中等 | 兼容 NapCat/Go-CQHTTP |

所有 Webhook 类渠道共享一个 Gateway HTTP 服务器（默认 `127.0.0.1:18790`）。

## ⚙️ 配置

配置文件：`~/.metaclaw/config.json`

| 环境变量 | 说明 | 默认值 |
|----------|------|--------|
| `METACLAW_CONFIG` | 覆盖配置文件路径 | `~/.metaclaw/config.json` |
| `METACLAW_HOME` | 覆盖数据根目录 | `~/.metaclaw` |

### 支持的 Provider

| 厂商 | `model` 前缀 | 协议 |
|------|-------------|------|
| OpenAI | `openai/` | OpenAI |
| Anthropic | `anthropic/` | Anthropic |
| 智谱 (GLM) | `zhipu/` | OpenAI |
| DeepSeek | `deepseek/` | OpenAI |
| Google Gemini | `gemini/` | OpenAI |
| Groq | `groq/` | OpenAI |
| 通义千问 | `qwen/` | OpenAI |
| Ollama | `ollama/` | OpenAI |
| OpenRouter | `openrouter/` | OpenAI |
| LiteLLM | `litellm/` | OpenAI |
| VLLM | `vllm/` | OpenAI |
| Cerebras | `cerebras/` | OpenAI |
| Moonshot | `moonshot/` | OpenAI |
| 火山引擎 | `volcengine/` | OpenAI |

> **负载均衡**：为同一个 `model_name` 配置多个端点 — MetaClaw 自动轮询。

### 🔒 安全模型

MetaClaw 实施了**分层安全模型**，每个文件和命令操作都经过多层验证：

```
文件操作：
  tildeExpandFs          ~ → $HOME 展开
    → whitelistFs        配置 allow_read_paths / allow_write_paths
      → sandboxFs        os.Root 工作区限制（内核级别）
        → hostFs         直接文件系统访问

命令执行：
  denyPatterns           拦截 rm -rf、format、dd、shutdown、fork bomb
  allowPatterns          自定义覆盖（如 git push origin）
  customAllowPatterns    豁免特定命令的拒绝检查
  路径穿越防护            拦截 .. 和工作区外的绝对路径
```

- **内核级沙箱**：使用 Go 1.23+ `os.Root` API — 路径逃逸在操作系统层面被阻断，而不仅仅是字符串匹配
- **白名单路径**：`allow_read_paths` / `allow_write_paths` 允许访问工作区外的特定路径，无需禁用沙箱
- **命令守卫**：多层模式匹配（拒绝 → 自定义允许 → 允许 → 工作区限制）
- **一致性边界**：主 Agent、子 Agent、spawn 任务、心跳任务全部继承相同限制 — 无法通过委派绕过

## CLI 参考

| 命令 | 说明 |
|------|------|
| `metaclaw onboard` | 初始化配置和工作区 |
| `metaclaw agent -m "..."` | 与 Agent 对话 |
| `metaclaw agent` | 交互式聊天模式 |
| `metaclaw gateway` | 启动网关 |
| `metaclaw cron list` | 列出定时任务 |

## 🔗 与 PicoClaw 的关系

MetaClaw 源自 [PicoClaw](https://github.com/sipeed/picoclaw)，但在方向上已有显著分化：

| | PicoClaw | MetaClaw |
|---|---|---|
| **定位** | 超轻量，$10 硬件 | 元认知，架构深度 |
| **架构** | 单循环 Agent | 三阶段（分析 → 执行 → 反思） |
| **记忆** | 基础 | 四层层级 + 倒排索引 TurnStore |
| **安全** | 基础工作区限制 | 4 层沙箱（os.Root + 白名单 + 命令守卫） |
| **场景** | 嵌入式 / IoT | 通用 AI 助手 |

感谢 PicoClaw 和 Sipeed 团队提供的基础，使 MetaClaw 成为可能。

## 🤝 参与贡献

欢迎 PR！代码库刻意保持小巧和可读。🤗

## 📝 许可证

MIT
