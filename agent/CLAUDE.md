# Agent 模块

Agent 模块是 ADDP 平台的**自然语言交互入口**，用户通过对话方式完成数据管理、分析和发布操作。

## 模块概述

- **后端**: Python 3.11+ + FastAPI + LangChain Agent Runtime
- **前端**: Vue 3 + Element Plus
- **交互协议**: AG-UI + A2UI `addp.catalog/v1`
- **端口**: Backend 8190（开发）| Frontend 5186（开发）
- **数据库**: PostgreSQL `agent` schema

平台级 Tool、交互和评测契约分别以 `docs/spec/addp智能体Tool开放规范.md`、`docs/spec/addp智能体交互协议规范.md`、`docs/spec/addp智能体评测规范.md` 为事实源；本文只记录 Agent 模块实现和运行约束。

## 目录结构

```
agent/
├── authorization/
│   └── permissions.yaml  # Agent owner Permission Manifest
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

# 前端测试与构建
cd agent/frontend && npm test && npm run build
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
| GET | /api/v1/agent/settings/inference-bindings/:scenario_code | 读取 Agent 推理场景绑定 |
| PUT | /api/v1/agent/settings/inference-bindings/:scenario_code | 更新 Agent 推理场景绑定 |

## IAM Permission 所有权

Agent 是以下 Permission 的唯一 owner：

- `agent.session.*`
- `agent.run.*`
- `agent.configuration.*`

机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Agent 服务启动时的 Module Registry 注册和心跳只描述服务可用性，不向 System 动态注册 Permission。

路由与 Permission 语义映射固定如下：

- Session 列表、详情和消息历史使用 `agent.session.read`，创建和删除分别使用 `agent.session.create/delete`。
- `/chat` 是单一 AG-UI Operation，新建和 Interaction resume 统一按 all-of 校验 `agent.run.create + agent.run.execute`；独立的失败重试路由只继续已有 AgentRun，校验 `agent.run.execute`。
- AgentRun 详情和事件回放使用 `agent.run.read`，取消使用 `agent.run.cancel`。

Session 和 AgentRun 当前都只允许所有者访问。Role Permission 只提供功能 Allow 候选，不取代 owner 对 `user_id + tenant_id` 归属的最终资源判断。

`delegable` 当前统一保守为 `false`。Agent 调用其他 owner Tool 时使用对方 owner 的 Tool Scope 和短期 Delegated Access Token，不把 `agent.run.*` 当作业务 Tool 权限。

## 数据库 Schema

```sql
agent.sessions    -- 会话管理、增量历史摘要和摘要水位
agent.messages    -- 对话历史（AG-UI message id + 有序 parts）
agent.runs        -- AgentRun 生命周期、checkpoint、运行/上下文指标与错误归因
agent.run_steps   -- Tool / Runtime 步骤审计、紧凑事实投影与错误归因
agent.run_events  -- 可按 sequence 安全回放的 AG-UI 事件投影
agent.interactions -- 服务端持久澄清与 owner approval 投影
agent.skill_usage -- Skill 使用统计
agent.inference_scenario_bindings -- Agent 场景到 Inference Model Profile 的平台默认和 Tenant 覆盖
```

阶段 5 的 Agent 评测场景唯一目录为仓库根 `evals/agent-scenarios/`，统一使用 `addp.agent-scenario/v1`。离线门禁通过脚本化 Runtime 决策和受控 owner 响应消费真实结构化事件，不调用真实 LLM；定向在线层消费同一契约，凭据和环境私有 ID 不进入 fixture。

定向在线评测唯一入口为 `evals/agent-scenarios/online_runner.py`，只经过生产 `ToolExecutor` 验证真实委托和 Owner API，不调用 LLM。User Access Token 只通过正式 `addp` OAuth 登录和 OS Keychain 中的 Refresh Token 刷新获得，不接受环境变量或命令参数注入；workflow 输入和证据 JSON 必须放在 ADDP 仓库外。示例：

```bash
python evals/agent-scenarios/online_runner.py \
  --output /tmp/addp-read-only-evidence.json \
  read-only --query railway \
  --locator 'addp://engine/8/path/public/railway?type=table&item_id=60'
```

审批场景依次运行 `approval-request`、在 Develop Owner 页面明确批准或拒绝，再运行 `approval-resume` 或 `approval-rejection`；Runner 不自动决定审批。

统一评测门禁唯一入口为 `evals/agent-scenarios/gate.py`。默认运行全部场景契约、Agent 评测与持久化测试、common-python 全量测试和 Agent 前端测试；`--require-online` 额外要求三个黄金场景的仓库外在线证据，并重新使用场景 `evaluate_trace` 校验。报告 Schema 固定为 `addp.agent-evaluation-gate/v2`，报告输出也必须位于仓库外。v2 记录 Git revision、脏工作区标志、场景契约摘要、在线证据摘要和检查耗时，但不得记录证据路径、approval/AgentRun 标识或 trace；历史归档由外部发布流程负责。

```bash
python evals/agent-scenarios/gate.py --output /tmp/addp-agent-evaluation-gate.json
```

仓库标准入口为 `make test-agent-eval`；根 `make test` 已包含该离线门禁。人工发布验收使用 `make test-agent-eval-release`，并显式提供 `ADDP_AGENT_READ_ONLY_EVIDENCE`、`ADDP_AGENT_APPROVAL_EVIDENCE`、`ADDP_AGENT_REJECTION_EVIDENCE` 三份仓库外证据。在线证据固定 24 小时有效，未来时间只容忍 5 分钟时钟偏差。

两份已归档 v2 报告使用 `make compare-agent-eval` 只读比较，并显式提供 `ADDP_AGENT_EVAL_BASELINE`、`ADDP_AGENT_EVAL_CURRENT`。比较结果 Schema 为 `addp.agent-evaluation-comparison/v1`；场景/检查删除、passed 状态退化或当前报告失败才返回回归，契约摘要变化和检查耗时差值只用于审查，不设置阈值。比较不重跑门禁、不读取在线证据，也不保存输入路径或证据摘要。

正式发布基线比较使用 `make compare-agent-eval-release`。baseline 和 current 必须都是显式指定的仓库外 `online_required + passed + worktree_dirty=false` 报告，且普通回归比较通过；否则结果为 `regressed|blocked` 并返回非零。dirty 或 offline 报告仍可用于普通开发比较，但不能成为正式 baseline。归档对象和 baseline 指针由外部发布系统持有，比较器不扫描目录、不选择最新文件、不自动更新 baseline，也不重新按当前时间判定历史在线证据过期。

阶段 5 已封板。当前评测协议固定为 `addp.agent-scenario/v1`、`addp.agent-online-evidence/v1`、`addp.agent-evaluation-gate/v2` 和 `addp.agent-evaluation-comparison/v1`；A2UI 继续使用 `addp.catalog/v1`。破坏性修改必须升级版本并删除旧解析路径。`GraphView`、趋势 UI/阈值、外部归档自动化、MCP Adapter 和多智能体委派只有在专题文档规定的真实场景或外部系统条件满足后才能重新启动。

评测 Schema、门禁、比较和正式发布资格的完整规则见 `docs/spec/addp智能体评测规范.md`；本文不另设评测契约。

`messages` 不再使用 `result_type + result_data`。表现内容通过 `presentation_ref` 引用 A2UI Surface，澄清状态通过 `interaction_ref` 引用 `agent.interactions`。

## 协议约束

- `/api/v1/agent/chat` 请求体使用标准 AG-UI `RunAgentInput`。
- 响应为 `text/event-stream`，事件使用 AG-UI 编码。
- A2UI 通过 AG-UI Activity 传输，当前 Catalog 为 `addp.catalog/v1`。
- `request_clarification` 是 Agent Runtime 私有的暂停控制能力，不属于平台 Tool Manifest；触发后必须创建持久 Interaction，并以 AG-UI interrupt 和 A2UI `ClarificationChoice` 返回。
- `workflow.run` 返回 `approval_required` 时，Agent 只保存 Develop approval ID、open URL、请求指纹和摘要，以 A2UI `ApprovalRequest` 暂停当前 AgentRun。客户端只能打开 Owner 页面或提交 `{action:"check"}`；Agent 必须使用原始 User Access Token 查询 Develop，只有 `approved|consumed` 才恢复同一 AgentRun。
- `workflow.run` 的完整 workflow payload 不得写入 `agent.interactions`、`agent.run_steps` 或 checkpoint。首次 Tool step 只保存引擎 ID、任务数和是否存在 engine-specific 配置；恢复调用只保存 approval ID 与请求指纹。
- 工作流引擎澄清选项必须来自当前 run 的 `engine.list`；资源澄清 locator 必须来自当前 run 的 `data.search`、`resource.ancestors.get` 或 `data.preview`。Runtime 使用 owner Tool 事实重建选项，未经观察的候选返回 `clarification_option_not_observed`，不得创建 Interaction。
- AgentRun 跨初始 AG-UI 调用和 Interaction resume 调用存在；恢复身份只使用 Interaction 的 `agent_run_id`，不得按新的协议 `runId` 创建第二个 AgentRun。
- Interaction resume 必须沿用该 AgentRun 已记录的 Skill，不重新交给路由模型选择，避免批准后偏离原 Tool 白名单或退化为直接回复。
- 断线重连按 `agent.run_events` 的 run 内 sequence 回放；事件不得保存 Tool 参数或原始结果。取消只停止内置 Agent Runtime 和 pending Interaction，不取消 owner execution；失败重试在同一 AgentRun 中追加新的协议调用事件。
- `agent.run_events` 的 SSE 使用标准 `id` 字段承载 run 内 sequence；客户端用 `after` 回放未处理的安全事件。
- AgentCheckpoint 只保存 owner Tool 紧凑事实和用户已确认选择，不保存模型隐藏推理、框架私有内存、完整样本行或大 Tool 结果。
- 持久化总量门禁固定为：单个 replayable run event 512 KiB、单条 message parts 2 MiB、AgentCheckpoint 256 KiB、单个 step facts 128 KiB；超限直接拒绝，不截取 JSON 前缀或转存旁路字段。
- `run_steps.output_summary` 只保存状态、计数、类型和结果字节数等受限摘要；即使 Tool 结果被截断为非 JSON，也不得回退保存原文前缀。可复用 owner 事实写入 `run_steps.facts` 和 run checkpoint。
- Runtime 上下文从最新消息向前分配：最近 20 条、单条 6000 字符、消息总计 24000 字符、历史摘要 2000 字符。摘要以 `sessions.summary_message_id` 增量推进，不重复压缩全部历史。
- `runs.metrics` 只保存可由 step/event 验证的结构指标，`runs.context_metrics` 只保存预算事实；Provider 没有返回时不估算 token usage。
- Agent 只通过 ADDP Inference Runtime 调用模型。`reasoning` 场景用于路由、Tool Calling 和 ReAct，`general-chat` 用于会话摘要；同一 AgentRun 内的 reasoning Profile 首次解析后固定复用。
- 场景按“Tenant 显式绑定 > 平台默认绑定 > inference_scenario_not_configured”解析，不回退环境变量、任意 Profile 或另一个场景。
- Agent 不保存或读取 Provider、endpoint、上游模型名和 API Key；这些事实只属于 Inference。
- 错误归因使用 `error_source=client|runtime|tool|owner|protocol`、稳定 `error_code` 和最多 1000 字符的受限 `error_message`；不保留 `error_type` 兼容字段。
- ResultRef 由 Tool Manifest 声明驱动；当前 execution 引用只保存 `schema + owner_module + kind + execution:<id>`，不得复制 owner execution 状态或结果。locator 候选没有单一结果身份时不得创建 ResultRef。
- Workflow DAG 只能在 `workflow.validate` 返回 `valid=true` 后生成 A2UI Presentation；draft 未校验或校验失败均不得展示为可用 DAG。
- 不保留 `0:`、`dag:` 或 Vercel AI SDK 兼容流。
- 前端只渲染 Catalog 中注册的组件。

## 共享能力

- ADDP API Client 统一来自 `common-python/addp_common/client`。
- Agent 通过 `common-python/addp_common/auth.resolve_authorization_context()` 消费 System AuthContext，不保存 `JWT_SECRET` 或私有 JWT 解析逻辑。
- 原始 User Access Token 只进入 Agent Runtime 和 System 委托签发接口；每次平台 Tool 使用当前 AgentRun UUID 与 LangChain `tool_call_id` 换取短期 Delegated Access Token，owner Client 不接收原始 Token。
- A2UI Vue Renderer 位于 `common-frontend/agent-ui`。
- 平台级 Skill 唯一目录为仓库根目录 `skills/`，Agent 从 `agents/addp.yaml` 装配 Tool Manifest 中的稳定 Tool。
- Agent Tool Provider 是 `common-python` `ToolExecutor` 的 LangChain 薄 Adapter，不直接调用模块 API Client。

## 前端公开路由

- 模块内聊天首页为 `/`，会话为 `/sessions/:session_id`；Console 公开 URL 分别为 `/agent`、`/agent/sessions/:session_id`。
- 当前会话由 path 参数唯一驱动。点击和创建会话使用 `push`，首次自动选择与删除当前会话后的重定向使用 `replace`。
- `/agent` 旧模块内前缀不再接受；无效或无权访问的会话 ID 保留错误上下文，不得静默切换到其他会话。

## 配置项

在根 `.env` 文件中配置：

```env
# Agent Service Principal；Inference Runtime 由 System Runtime Descriptor 发现
AGENT_SERVICE_CLIENT_SECRET=replace-with-unique-agent-secret-32bytes

# Agent 端口
AGENT_BACKEND_PORT=8190
AGENT_FE_PORT=5186
```

## 日志

- 后端日志: `logs/agent-backend.log`
- 后端错误: `logs/agent-backend-stderr.log`
- 前端日志: `logs/agent-frontend.log`

## 相关文档

- `docs/spec/addp智能体Tool开放规范.md`
- `docs/spec/addp智能体交互协议规范.md`
- `docs/spec/addp智能体评测规范.md`
- `docs/skills/addp-Skill规范.md`
- `docs/spec/addp OAuth授权规范.md`
- `agent/docs/agent模块设计共识.md`
- `docs/next/ADDP智能体能力开放体系专题.md`
