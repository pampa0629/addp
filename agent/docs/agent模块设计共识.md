# Agent 模块设计共识

更新时间：2026-10-04

本文只记录 Agent 模块内部边界。平台级 Tool、AG-UI / A2UI 和评测契约分别以 `docs/spec/addp智能体Tool开放规范.md`、`docs/spec/addp智能体交互协议规范.md`、`docs/spec/addp智能体评测规范.md` 为事实源；Skill 与认证分别引用各自既有规范。专题文档只保留架构决策和实施历史。

## 一、模块定位

Agent 是 ADDP 官方提供的自然语言交互产品和一种 Agent Runtime，负责：

- 会话、消息和历史摘要；
- Agent run 和多轮上下文；
- Skill 路由和 Tool 调用循环；
- 对话澄清 Interaction；
- AG-UI 事件输出；
- ResultRef、Interaction 和 Presentation 的消息编排。

Agent 不拥有平台级 Skill、owner 模块业务结果或审批事实，也不复制 Copilot 的结构化生成逻辑。

### 进程内执行内核

Agent 保留产品、路由和持久状态边界，唯一执行内核采用 DeerFlow Harness 的 `create_deerflow_agent` SDK，不部署 DeerFlow Server，也不保留自研模型／工具循环。依赖固定到官方源码提交 `130a9ab9f035a8f319502f8d5e044ed05962b2aa`，Agent 使用 Python 3.12；共享 Inference LangChain Adapter 与 Copilot 统一使用 LangChain Core 1.x。

每次请求构建独立、无框架持久化后端的 Harness。ADDP 中间件负责 Skill 轮数上限、受控 Tool 事件、owner 事实投影和暂停；工具仍唯一通过现有 Manifest Adapter → ToolExecutor 调用 owner。工具串行执行，暂停后同一模型响应中的其他调用不得产生副作用；有效澄清优先于同批业务工具。业务 AgentRun ID 仅进入运行上下文，不写入框架 `configurable.run_id`，恢复时从 ADDP 语义检查点重建，不恢复框架消息快照。

本阶段不启用 shell、文件、浏览器、子代理、框架记忆或自主审批；不注册 DeerFlow 内置业务工具。会话摘要沿用 ADDP 原有预算，Harness 不建立第二套持久记忆。模型只经 Inference Runtime；纯文本内容块被共享 Adapter 规范化，非文本内容明确拒绝。

产品镜像保留仓库相对层级：后端位于 `/app/agent/backend`，平台级 Skill 位于 `/app/skills`。开发和容器复用同一 Skill 加载路径，不新增容器专用 loader 或私有 Skill 副本。镜像构建检查应用导入、Skill 装配与依赖一致性；Agent 镜像的增量构建输入包含平台 `skills/`。

## 二、唯一交互协议

平台语义任务使用唯一的两阶段选择：初始模型只识别是否需要平台能力目录；Runtime 用 `platform.capabilities.list` 读取 Ontology 已激活定义后，模型才选择 capability，再由定义中的 Skill/Tool 引用装配领域执行。`platform_routed=true` 的 Skill 禁止从描述直接选中；目录失败不能退回旧路由。当前仅 Transfer 创建领域启用，普通对话和其他 Skill 不产生 Ontology 依赖。业务字段、资源身份与写入策略仍由正式事实和必要用户确认约束，不要求用户输入内部步骤。详细边界见 Ontology 最小设计契约 1.12。

Agent 后端唯一流式接口是：

```text
POST /api/v1/agent/chat
Content-Type: application/json
Request: AG-UI RunAgentInput
Response: text/event-stream
Events: AG-UI
```

不再使用 Vercel AI SDK 私有流、`0:` / `dag:` 前缀或 `result_type + result_data`。

A2UI 通过 AG-UI `ACTIVITY_SNAPSHOT` / `ACTIVITY_DELTA` 传输，第一批 ADDP Catalog 组件为：

- `WorkflowDag`；
- `ClarificationChoice`。

## 三、持久状态

PostgreSQL `agent` schema 当前包含：

- `sessions`：用户会话和历史摘要；
- `messages`：消息文本、AG-UI message id 和有序 parts；
- `runs`：跨 AG-UI 调用存在的 AgentRun、生命周期和语义检查点；
- `run_steps`：Tool 调用与 Runtime 控制动作的受限审计记录；
- `run_events`：可按 sequence 安全回放的 AG-UI 事件投影；
- `interactions`：服务端持久澄清，或 owner approval 的 ID、open URL、请求指纹、摘要和状态投影；
- `skill_usage`：Skill 使用统计。

AgentRun 使用唯一语义恢复路线：

1. 新用户目标创建 `agent.runs`，状态进入 `running`。
2. owner Tool 返回的引擎和 locator 等紧凑事实写入 `checkpoint.observed`，Tool 调用写入 `run_steps`。
3. 创建澄清或 owner approval 投影时，Interaction 通过 `agent_run_id` 关联 AgentRun，run 状态进入 `waiting`。
4. 用户回答澄清后，选中的 owner 事实写入 `checkpoint.confirmed`；用户检查审批时，Agent 使用原始 User Access Token 查询 owner，只有 owner 返回 `approved|consumed` 才完成 Interaction；两种情况都恢复同一个 AgentRun。
5. Runtime 使用预算化会话历史、增量摘要、Skill 和 AgentCheckpoint 重建模型调用，不序列化 LangChain 内部对象、隐藏推理或完整 Tool 大结果；通过 Manifest 结构与字节校验的受限 Tool 结果完整进入当前 ToolMessage，不截取 JSON 前缀。审计摘要只记录类型与字节数，不保存原文前缀。Skill 推理轮数耗尽后明确失败，不额外调用未绑定工具的模型收尾。
6. 所有模型调用经 Inference Runtime；Agent 只拥有 `reasoning`、`general-chat` Scenario Binding，同一 AgentRun 固定 reasoning Profile 快照，不拥有 Provider、Deployment 或 credential。
6. 最终进入 `completed`、`failed` 或 `cancelled`。

AG-UI 请求中的 `runId` 是协议调用标识；AgentRun 是服务端逻辑运行身份。一次 AgentRun 可以经历初始调用和若干次 resume 调用，恢复身份以 Interaction 的 `agent_run_id` 为准。

Interaction resume 沿用 AgentRun 已记录的 Skill 和 Tool 白名单，不重新执行主路由选择；协议调用 ID 可以变化，但业务运行身份和能力边界不变。

断线重连只回放同一 AgentRun 的 `run_events`，并以客户端已处理的 sequence 为游标；事件表不保存 Tool 参数或原始结果。取消仅停止 Agent Runtime 和 pending Interaction，不取消 owner execution；失败重试在同一 AgentRun 内以新的协议调用 ID 追加事件。

消息 parts 支持：

- `text`；
- `result_ref`；
- `interaction_ref`；
- `presentation_ref`。

ResultRef 只能由 Tool Manifest 的 `result_ref` 声明生成。当前 `execution` 映射只保存 schema、owner、kind 和 `execution:<id>`；Agent 不复制 execution 状态或结果。搜索候选、preview 和 locator 事实没有单一业务结果身份时不得升级为 ResultRef。

AgentRun 可观测事实分为：

- `runs.metrics`：由 step/event 计算的协议调用数、step 数、失败 step 数、事件数、文本字符数和运行时长；不伪造 Provider 未返回的 token usage。
- `runs.context_metrics`：记录本次 Runtime 重建选入、遗漏、截断的消息数和字符预算，不复制消息正文。
- `sessions.summary_message_id`：已压缩的早期消息水位，摘要只合并水位后的新早期消息。

上下文唯一预算为最近 20 条消息、单条 6000 字符、消息总计 24000 字符和摘要 2000 字符，从最新消息向前分配。错误字段唯一使用 `error_source=client|runtime|tool|owner|protocol` + `error_code` + 最多 1000 字符的受限 `error_message`，不保留 `error_type`。

写入和破坏性操作的 approval 不归 Agent 持有。`workflow.run` 的权威审批由 Develop 持有；Agent 不保存完整 workflow payload，只保存 owner approval 的可恢复投影。`ApprovalRequest` 的“检查审批状态”动作只提交 `{action:"check"}`，不能产生批准事实。

## 四、前端边界

Agent 前端使用 Vue 3 和 Element Plus：

- `@ag-ui/client` 负责 AG-UI 请求、SSE 和事件状态；
- `common-frontend/agent-ui` 负责 A2UI MessageProcessor、ADDP Catalog 和 Vue Renderer；
- `MessagePartsRenderer` 负责按顺序渲染消息 parts；
- DAG、地图、表格等继续复用 `common-frontend` 业务组件。

完整 A2UI Surface 不进入 LLM 上下文。客户端遇到未注册组件时拒绝渲染。

Develop execution ResultRef 由前端以当前用户身份调用 owner API 按需加载。AG-UI 传输错误后，Web 客户端只回放同一 AgentRun 的安全事件重建临时界面，不重新提交用户消息或 Tool 调用。

## 五、Tool 与客户端边界

Agent 的 LangChain Tool 只是一种 Tool Adapter。访问 ADDP API 的 Client 统一来自 `common-python/addp_common/client`，Agent 目录不保留模块私有 API Client。

平台级 Skill 只位于仓库根目录 `skills/`。Agent 读取 `agents/addp.yaml`，以 Tool Manifest 的稳定名称建立工具白名单；原私有 Skill 目录已删除，不保留双轨。

## 六、认证

内置 Agent 前端使用 ADDP 现有登录和用户访问令牌。Agent 后端通过 `common-python/addp_common/auth.resolve_authorization_context()` 调用 System `/api/v1/system/auth/context`，不保存 `JWT_SECRET`、不自行解析 Token。原始用户 Token 只用于进入 Runtime 和向 System 申请当前 Tool 调用的短期 Delegated Access Token，不再传给 owner 模块 API。

每次平台 Tool 调用必须绑定当前 AgentRun UUID 和 LangChain `tool_call_id`。System 根据 Tool Manifest 的 owner audience 与稳定 Tool 名 Scope 签发 `addp_dat_`；owner 默认拒绝委托令牌，只在对应 Tool 路由完成 audience、Scope、用户、租户、资源权限和审批校验后放行。

AG-UI 流式请求使用 `common-frontend` 的 `createAuthenticatedFetch()`，与普通 Axios Client 共用 Token 刷新核心；Agent 前端不保存静态请求 Token，也不复制刷新逻辑。

外部 CLI 的 OAuth PKCE / Device Flow 不在 Agent 模块实现。

## 七、相关文档

- `docs/spec/addp智能体Tool开放规范.md`
- `docs/spec/addp智能体交互协议规范.md`
- `docs/spec/addp智能体评测规范.md`
- `docs/skills/addp-Skill规范.md`
- `docs/concepts/addp术语表.md`
- `docs/spec/addp OAuth授权规范.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp登录认证的统一要求.md`
- `common-python/CLAUDE.md`
- `common-frontend/README.md`
- `docs/next/ADDP智能体能力开放体系专题.md`
