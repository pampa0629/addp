# ADDP 智能体能力开放体系专题

更新时间：2026-07-18

状态：实施历史。阶段 0—5 已完成并封板，阶段 6 正式规范迁移已完成。当前规范事实源见第三章，本文只保留架构决策、实施过程和延期条件。

## 一、专题目标

本专题解决两个正交问题：

1. 将 ADDP 的 Skill、Tool 和客户端能力从内置 Agent Runtime 分离，使其他智能体能够复用同一平台能力。
2. 完善 ADDP 内置 Agent 的多轮对话、流式运行、Tool 轨迹、澄清、审批以及 DAG、地图、表格等富交互界面。

ADDP 不绑定唯一 Agent Framework。内置 Agent 是官方 Agent Runtime，但不拥有平台级 Skill、Tool 或 owner 业务事实。

## 二、已确认的架构决策

以下结论已经实施，后续不保留平行路线：

1. `Skill + Tool` 是 ADDP 面向智能体的能力模型。
2. Skill 是可复用任务级知识与工作方法，不绑定单次业务案例、数据集或参数。
3. `workflow-analysis` 是 Skill；铁路占耕地面积计算是评测场景。
4. Tool Manifest 是 AI Tool 契约事实源，owner 模块正式 API 是业务事实源。
5. Python SDK 是访问 ADDP API 的唯一客户端实现；CLI 和内置 Agent Tool Provider 是薄适配器。
6. 不建设中心 Tool 服务，不建立拥有第二套业务状态的 Tool Core。
7. 内置 Agent 直接调用 ToolExecutor / Python SDK，不通过 shell 调用 CLI。
8. ResultRef、Interaction 和 Presentation 分层；Presentation 不能成为业务结果或审批状态事实源。
9. AG-UI 是 Agent Runtime 与 Web 前端的唯一事件协议；A2UI 是其中的声明式表现协议。
10. A2UI 使用官方 `@a2ui/web_core`、ADDP Vue Renderer 和版本化 `addp.catalog/v1`。
11. 不支持 ADDP Catalog 的客户端统一降级为文本摘要、ResultRef 和 `open_url`，不建立框架私有业务分支。
12. 用户、租户、OAuth 和委托身份由 System 持有；写审批由业务 owner 持有；Agent 只保存运行和交互投影。
13. 评测先于表现扩展；只有稳定场景暴露真实需求时才增加 A2UI 组件或 Adapter。

目标调用关系保持为：

```text
Agent Runtime
  -> Skill
  -> Tool Adapter
  -> ToolExecutor
  -> Python SDK
  -> Gateway / owner API
```

Web 交互关系保持为：

```text
AgentRun -> AG-UI -> message parts -> ResultRef / Interaction / Presentation -> A2UI Renderer
```

## 三、正式规范事实源

当前阅读顺序固定为：

1. `docs/concepts/addp术语表.md`：稳定概念与命名。
2. `docs/spec/addp智能体Tool开放规范.md`：Tool Manifest、ToolExecutor、SDK / CLI / Adapter 单一路线。
3. `docs/skills/addp-Skill规范.md`：Skill 目录、粒度、渐进式加载和 Tool 引用。
4. `docs/spec/addp智能体交互协议规范.md`：AgentRun、AG-UI、消息 parts、Interaction、ResultRef、Presentation 和 A2UI。
5. `docs/spec/addp智能体评测规范.md`：场景、在线证据、门禁、比较和正式发布评测基线。
6. `docs/spec/addp授权上下文规范.md` 与 `docs/spec/addp OAuth授权规范.md`：AuthContext、OAuth、委托身份和 owner 写审批。
7. `docs/spec/addp-API设计规范.md` 与 `docs/spec/addp-Swagger集成指南.md`：HTTP API、错误和 Swagger。
8. `agent/CLAUDE.md`、`agent/docs/agent模块设计共识.md`、`common-python/CLAUDE.md`、`common-frontend/README.md`：模块实现。

本文不再定义上述规范的字段、状态机、上限或命令。实现与本文历史记录冲突时，以正式规范和当前代码为准。

## 四、模块边界

| 能力 | Owner |
| --- | --- |
| 用户、租户、OAuth、委托身份、加密模型凭据 | System |
| 多轮对话、AgentRun、对话澄清、AG-UI endpoint | Agent |
| SQL、工作流等专业对象生成 | Copilot |
| workflow definition、审批、执行和结果 | Develop |
| 数据项事实、locator 和资源树 | Meta / Manager |
| execution 统一查询和观测 | Monitor / execution owner |
| Python API Client、ToolExecutor、CLI 公共实现 | common-python |
| A2UI Vue Renderer、Catalog 和共享展示组件 | common-frontend |
| Skill 知识包 | 仓库根目录 `skills/` |

Agent 不复制 Copilot 生成逻辑；Copilot 不维护第二套用户对话；common-python 不成为远程 Tool 服务；common-frontend 不拥有 Result 或 Interaction 状态。

## 五、实施历史

### 阶段 0：文档与契约

完成内容：

- 确认 `Skill + Tool` 能力模型、owner 边界和单一 SDK 路线。
- 将 Skill 统一到仓库根 `skills/`，删除原 Agent 私有 Skill 路径。
- 补充 Agent Runtime、AgentRun、Tool、ResultRef、Interaction、Presentation、AG-UI 和 A2UI 等术语。
- 明确铁路占耕地面积计算属于 `workflow-analysis` 评测场景。

### 阶段 1：AG-UI + A2UI 最小纵向验证

实施状态：2026-07-15 完成。

完成内容：

- `POST /api/v1/agent/chat` 切换为 AG-UI SSE。
- 删除 `0:` / `dag:` 私有流格式和前端兼容解析。
- 建立 `addp.catalog/v1`、Vue Renderer、`WorkflowDag` 和 `ClarificationChoice`。
- 建立服务端 Interaction，刷新后可继续回答澄清。
- 消息切换为有序 parts，Presentation 与业务结果分离。

### 阶段 2：Skill + SDK + CLI 最小闭环

实施状态：完成。

完成内容：

- Tool Manifest 落到 `common-python/addp_common/tools/manifest.json`。
- ToolExecutor 建立 Manifest 到 Python SDK 的唯一执行映射。
- 首批 9 个 Tool 形成 workflow-analysis 最小闭环。
- `addp tools list`、`addp tools get`、`addp tool call` 建立稳定 CLI 入口。
- workflow 校验与执行拆分为唯一正式 owner API 路径。

### 阶段 3：内置 Agent 运行时完善

实施状态：完成。

#### 3.1 AgentRun 与语义检查点

- 建立 AgentRun、AgentRunStep、`addp.agent-checkpoint/v1` 和安全事实投影。
- AgentRun 与 AG-UI 协议调用 ID 分离，等待和恢复保持同一 AgentRun。
- 上下文按最新消息优先，记录选择、省略和截断指标。

#### 3.2 事件重放、取消与失败重试

- 建立 AgentRunEvent 单调 sequence 和安全 SSE 回放。
- Tool 原始参数、原始结果、Token 和模型隐藏推理不进入事件存储。
- 取消只结束 Agent Runtime 和 pending Agent Interaction，不取消 owner execution。
- retry 只允许失败 AgentRun，恢复同一检查点并使用新的协议调用身份。

#### 3.3 ResultRef 与断线恢复

- `addp.result-ref/v1` 只引用 owner 结果，不复制业务事实。
- ResultRef 构造受 Tool Manifest 声明约束，不从任意 ID 猜测。
- 前端按 ResultRef 调 owner API 获取当前结果。

#### 3.4 指标、错误归因与上下文预算

- AgentRun 记录 Runtime、Tool、owner、interaction 等稳定错误来源。
- step 保存受限输入投影、输出摘要和结构事实。
- 会话摘要使用水位增量推进，不在 LLM 调用期间持有数据库事务。

### 阶段 4：认证与外部 Agent

实施状态：完成。

#### 4.1 统一授权上下文

- Go 与 Python 模块统一消费 System AuthContext。
- 用户、租户、状态、OAuth client、audience 和 scopes 不从客户端参数推断。
- owner 对 Delegated Access Token 使用精确 audience、scope 和路由默认拒绝。

#### 4.2 OAuth 登录与刷新令牌

- 用户令牌统一为 opaque token，删除用户 JWT 刷新旧路径。
- 实现 Authorization Code + PKCE、Device Flow 和 Refresh Token Family 严格轮换。
- CLI Refresh Token 只进入 OS Keychain；浏览器 Access Token 只保存在内存会话。

#### 4.3 外部 Agent 与受委托执行

- 每次 Tool 调用由 System 基于当前 User Access Token 签发短期 Delegated Access Token。
- `workflow.run` 由 Develop 持有审批请求、决定、原始 payload 和一次性消费事实。
- Agent 只保存 owner Interaction 投影，不保存完整审批 payload。
- 真实审批闭环已完成；同一 AgentRun 重复消费返回 `approval_already_consumed`，跨 AgentRun 重放优先返回 `approval_forbidden`。

### 阶段 5：扩展与评测

实施状态：2026-07-18 封板。

#### 5.1 智能体评测基线

- 建立 `addp.agent-scenario/v1` 和统一场景 evaluator。
- 黄金路径覆盖只读查询、审批执行、拒绝与跨 AgentRun 越权。
- 离线轨迹不调用真实 LLM，断言稳定协议和 owner 副作用。

#### 5.2 评测驱动的 A2UI 组件

- 根据黄金场景增加 `ApprovalRequest`、`TablePreview`、`MapView` 和 `ResourcePicker`。
- 组件只消费声明式安全数据和稳定引用，均有硬上限和拒绝测试。
- 没有场景需求的 `GraphView` 未实施。

#### 5.3 在线黄金证据

- `online_runner.py` 经过生产 ToolExecutor 验证真实委托与 owner API。
- 完成三份仓库外在线黄金证据和真实审批串行验收。
- Runner 不自动批准、不保存 Token、不把环境私有 ID写入 fixture。

#### 5.4—5.6 统一门禁与发布审计

- `gate.py` 成为唯一离线/在线门禁入口。
- 报告升级为 `addp.agent-evaluation-gate/v2`，旧 v1 解析删除。
- Make 入口接入根测试；在线证据固定 24 小时新鲜度和 5 分钟未来偏差。
- 报告记录 revision、dirty、契约/证据摘要和检查耗时，不复制 trace 或审批上下文。

#### 5.7—5.8 比较与正式发布基线

- 建立 `addp.agent-evaluation-comparison/v1` 和只读报告比较。
- 回归只由场景/检查删除、passed 状态退化或 current failed 构成。
- 正式 baseline 必须是 clean `online_required + passed` 报告并通过 release-ready 比较。
- 归档对象和 baseline 指针由外部发布系统持有，仓库不扫描或自动更新。

#### 5.9 封板审查

- 4 个场景契约、3 个评测驱动组件类别、3 份在线黄金证据、gate v2、comparison v1 和 4 个 Make 入口保持一致。
- 自动化覆盖离线 Runtime、在线证据、持久化边界、严格报告加载、普通比较和 clean/dirty release-ready 正反路径。
- 没有为封板新增运行时实体、服务、存储、兼容路径或无需求组件。

### 阶段 6：正式规范化

#### 6.1 拆分设计

实施状态：2026-07-18 完成。

- 盘点 Skill、OAuth、AuthContext、API/Swagger 和模块文档的既有职责。
- 确认只新增 Tool 开放、交互协议、评测三份正式规范，不建设第四份总规范。
- 补齐“智能体评测报告”和“智能体发布评测基线”术语。

#### 6.2 正式迁移

实施状态：2026-07-18 完成。

- 创建 `docs/spec/addp智能体Tool开放规范.md`。
- 创建 `docs/spec/addp智能体交互协议规范.md`。
- 创建 `docs/spec/addp智能体评测规范.md`。
- 本专题删除对应规范性正文，只保留架构决策和实施历史。
- 文档总入口、Skill、Agent、common-python 与 common-frontend 模块引用切换到正式规范。

## 六、主要实施落点

| 范围 | 主要文件 |
| --- | --- |
| Tool 契约与执行 | `common-python/addp_common/tools/manifest.json`、`manifest.py`、`executor.py` |
| Agent Runtime | `agent/backend/api/chat.py`、`api/runs.py`、`services/runs.py`、`services/run_events.py` |
| 检查点与引用 | `agent/backend/agents/checkpoint.py`、`context.py`、`result_refs.py` |
| Interaction | `agent/backend/services/interactions.py`、`models/interaction.py` |
| A2UI | `agent/backend/protocol/a2ui.py`、`common-frontend/agent-ui/` |
| Agent Web | `agent/frontend/src/agent/createAgentClient.js`、`views/ChatView.vue`、`components/MessagePartsRenderer.vue` |
| 评测 | `evals/agent-scenarios/`、`scripts/test/agent-evaluation-gate.sh`、`Makefile` |

## 七、延期事项与启动条件

以下事项不是当前主线，也不得写成已开放能力：

| 延期事项 | 重新启动条件 |
| --- | --- |
| `GraphView` | 稳定评测场景需要图谱交互，且现有文本/链接无法满足。 |
| MCP Adapter | 出现明确外部消费者、宿主协议和隔离评测；继续复用同一 Manifest、ToolExecutor 和 SDK。 |
| 多智能体委派 | 明确委派身份、预算、隔离、审批和审计场景，并先补正式规范与评测。 |
| 趋势 UI / 阈值 | 外部发布系统已持续归档同一版本报告，并积累足以解释分布的真实样本。 |
| 外部归档产品实现 | 发布系统明确 owner、保留期、不可变对象和 baseline 指针事务。 |

## 八、外部参考采用结论

- Agent Skills Specification：采用渐进式 Skill 包结构和按需加载思想。
- Codex Skills：作为外部 Agent 消费 ADDP Skill 的参考。
- Hermes Agent：作为可替换 Agent Runtime 和本地 Tool 消费参考。
- AG-UI：采用为 Agent 与前端之间的事件协议。
- A2UI 与 `@a2ui/web_core`：采用为声明式表现协议和官方框架无关核心。
- `@a2ui-vue/vue`：不采用。
- CopilotKit：参考 AG-UI、A2UI 和 Human-in-the-loop 设计，不作为 ADDP Runtime 事实源。

## 九、相关文档

- `docs/concepts/addp术语表.md`
- `docs/spec/addp智能体Tool开放规范.md`
- `docs/skills/addp-Skill规范.md`
- `docs/spec/addp智能体交互协议规范.md`
- `docs/spec/addp智能体评测规范.md`
- `docs/spec/addp授权上下文规范.md`
- `docs/spec/addp OAuth授权规范.md`
- `agent/CLAUDE.md`
- `common-python/CLAUDE.md`
- `common-frontend/README.md`
