# Agent 模块

Agent 模块是 ADDP 平台的**自然语言交互入口**，用户通过对话方式完成数据管理、分析和发布操作。

## 模块概述

- **后端**: Python 3.11+ + FastAPI + LangChain Agent Runtime
- **前端**: Vue 3 + Element Plus
- **交互协议**: AG-UI + A2UI `addp.catalog/v1`
- **端口**: Backend 8190（开发）| Frontend 5186（开发）
- **数据库**: PostgreSQL `agent` schema

## 目录结构

```
agent/
├── backend/
│   ├── agents/          # Agent 核心逻辑与结构化内部事件
│   ├── protocol/        # AG-UI / A2UI 协议适配
│   ├── services/        # Interaction 等领域服务
│   ├── tools/           # LangChain Tool Adapter
│   ├── models/          # SQLAlchemy 数据模型
│   ├── api/             # FastAPI 路由
│   ├── middleware/      # 认证中间件
│   ├── config.py        # 配置加载
│   ├── database.py      # 数据库连接
│   ├── main.py          # 应用入口
│   └── requirements.txt
└── frontend/
    ├── src/
    │   ├── views/       # ChatView.vue, Login.vue
    │   ├── store/       # auth.js
    │   ├── api/         # index.js
    │   └── router/      # index.js
    ├── package.json
    └── vite.config.js
```

## 快速启动

```bash
# 独立启动 Agent 模块
bash scripts/dev/start.sh -agent

# 重启 Agent 后端
bash scripts/dev/restart.sh -agent
```

## API 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /health | 健康检查 |
| GET | /api/v1/agent/sessions | 会话列表 |
| POST | /api/v1/agent/sessions | 创建会话 |
| GET | /api/v1/agent/sessions/:id | 会话详情 |
| DELETE | /api/v1/agent/sessions/:id | 删除会话 |
| GET | /api/v1/agent/sessions/:id/messages | 消息历史 |
| POST | /api/v1/agent/chat | 运行智能体（AG-UI SSE） |
| GET | /api/v1/agent/runs/:id | 查询 AgentRun、语义检查点和步骤审计 |
| GET | /api/v1/agent/runs/:id/events?after=:sequence | 按 sequence 回放安全 AG-UI 事件（SSE） |
| POST | /api/v1/agent/runs/:id/cancel | 取消 Agent Runtime 和 pending Interaction |
| POST | /api/v1/agent/runs/:id/retry | 重试失败 AgentRun（AG-UI SSE） |

## 数据库 Schema

```sql
agent.sessions    -- 会话管理、增量历史摘要和摘要水位
agent.messages    -- 对话历史（AG-UI message id + 有序 parts）
agent.runs        -- AgentRun 生命周期、checkpoint、运行/上下文指标与错误归因
agent.run_steps   -- Tool / Runtime 步骤审计、紧凑事实投影与错误归因
agent.run_events  -- 可按 sequence 安全回放的 AG-UI 事件投影
agent.interactions -- 服务端持久澄清
agent.skill_usage -- Skill 使用统计
```

`messages` 不再使用 `result_type + result_data`。表现内容通过 `presentation_ref` 引用 A2UI Surface，澄清状态通过 `interaction_ref` 引用 `agent.interactions`。

## 协议约束

- `/api/v1/agent/chat` 请求体使用标准 AG-UI `RunAgentInput`。
- 响应为 `text/event-stream`，事件使用 AG-UI 编码。
- A2UI 通过 AG-UI Activity 传输，当前 Catalog 为 `addp.catalog/v1`。
- `request_clarification` 是 Agent Runtime 私有的暂停控制能力，不属于平台 Tool Manifest；触发后必须创建持久 Interaction，并以 AG-UI interrupt 和 A2UI `ClarificationChoice` 返回。
- 工作流引擎澄清选项必须来自当前 run 的 `engine.list`；资源澄清 locator 必须来自当前 run 的 `data.search`、`resource.ancestors.get` 或 `data.preview`。Runtime 使用 owner Tool 事实重建选项，未经观察的候选返回 `clarification_option_not_observed`，不得创建 Interaction。
- AgentRun 跨初始 AG-UI 调用和 Interaction resume 调用存在；恢复身份只使用 Interaction 的 `agent_run_id`，不得按新的协议 `runId` 创建第二个 AgentRun。
- 断线重连按 `agent.run_events` 的 run 内 sequence 回放；事件不得保存 Tool 参数或原始结果。取消只停止内置 Agent Runtime 和 pending Interaction，不取消 owner execution；失败重试在同一 AgentRun 中追加新的协议调用事件。
- `agent.run_events` 的 SSE 使用标准 `id` 字段承载 run 内 sequence；客户端用 `after` 回放未处理的安全事件。
- AgentCheckpoint 只保存 owner Tool 紧凑事实和用户已确认选择，不保存模型隐藏推理、框架私有内存、完整样本行或大 Tool 结果。
- `run_steps.output_summary` 只保存状态、计数、类型和结果字节数等受限摘要；即使 Tool 结果被截断为非 JSON，也不得回退保存原文前缀。可复用 owner 事实写入 `run_steps.facts` 和 run checkpoint。
- Runtime 上下文从最新消息向前分配：最近 20 条、单条 6000 字符、消息总计 24000 字符、历史摘要 2000 字符。摘要以 `sessions.summary_message_id` 增量推进，不重复压缩全部历史。
- `runs.metrics` 只保存可由 step/event 验证的结构指标，`runs.context_metrics` 只保存预算事实；Provider 没有返回时不估算 token usage。
- 错误归因使用 `error_source=client|runtime|tool|owner|protocol`、稳定 `error_code` 和最多 1000 字符的受限 `error_message`；不保留 `error_type` 兼容字段。
- ResultRef 由 Tool Manifest 声明驱动；当前 execution 引用只保存 `schema + owner_module + kind + execution:<id>`，不得复制 owner execution 状态或结果。locator 候选没有单一结果身份时不得创建 ResultRef。
- Workflow DAG 只能在 `workflow.validate` 返回 `valid=true` 后生成 A2UI Presentation；draft 未校验或校验失败均不得展示为可用 DAG。
- 不保留 `0:`、`dag:` 或 Vercel AI SDK 兼容流。
- 前端只渲染 Catalog 中注册的组件。

## 共享能力

- ADDP API Client 统一来自 `common-python/addp_common/client`。
- Agent 通过 `common-python/addp_common/auth.resolve_authorization_context()` 消费 System AuthContext，不保存 `JWT_SECRET` 或私有 JWT 解析逻辑。
- A2UI Vue Renderer 位于 `common-frontend/agent-ui`。
- 平台级 Skill 唯一目录为仓库根目录 `skills/`，Agent 从 `agents/addp.yaml` 装配 Tool Manifest 中的稳定 Tool。
- Agent Tool Provider 是 `common-python` `ToolExecutor` 的 LangChain 薄 Adapter，不直接调用模块 API Client。

## 配置项

在根 `.env` 文件中配置：

```env
# LLM 配置
LLM_PROVIDER=openai    # openai | anthropic
LLM_API_KEY=sk-...
LLM_MODEL=gpt-4o
LLM_BASE_URL=          # 可选，用于自定义 API 端点

# Agent 端口
AGENT_BACKEND_PORT=8190
AGENT_FE_PORT=5186
```

## 日志

- 后端日志: `logs/agent-backend.log`
- 后端错误: `logs/agent-backend-stderr.log`
- 前端日志: `logs/agent-frontend.log`
