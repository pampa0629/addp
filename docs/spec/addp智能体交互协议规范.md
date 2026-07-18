# ADDP 智能体交互协议规范

更新日期：2026-07-18

状态：正式规范。内置 Agent 的 AgentRun、AG-UI、消息 parts、Interaction、ResultRef、Presentation 和 ADDP A2UI Catalog 以本文为准。

## 一、适用范围

本文规定 Agent Runtime 与 Web 客户端之间的唯一交互协议，以及协议投影的持久化和安全边界。Tool 能力开放见 `docs/spec/addp智能体Tool开放规范.md`；owner 写审批的业务状态机见 `docs/spec/addp OAuth授权规范.md`；Agent 数据表和具体实现见 `agent/docs/agent模块设计共识.md`。

## 二、分层模型

ADDP 智能体交互分为四层：

1. AgentRun：Agent 模块持有的可恢复运行事实。
2. AG-UI：一次客户端协议调用的事件流和生命周期。
3. 消息语义：有序 text、ResultRef、InteractionRef 和 PresentationRef。
4. A2UI：Presentation 的声明式 UI 投影。

各层不能互相替代：Presentation 不是结果事实，浏览器动作不是审批事实，AG-UI `runId` 也不是 AgentRun 主键。

## 三、AgentRun 与协议调用

### 3.1 身份

AgentRun 使用 Agent 生成的 UUID，跨等待、断线恢复和失败重试保持不变。AG-UI `RunAgentInput.runId` 表示一次协议调用身份：

- 第一次调用的 `runId` 保存为 `initial_protocol_run_id`；
- 同一会话内不能重复创建相同初始协议调用；
- 恢复或重试可以使用新的协议 `runId`，但必须指向原 AgentRun；
- owner execution、Interaction、ToolCall 和 AgentRun 各自使用独立 ID。

服务端通过 `STATE_SNAPSHOT.agentRunId` 告知客户端稳定 AgentRun ID。所有查询、回放、取消和重试 API 均按当前用户与租户隔离。

### 3.2 生命周期

AgentRun 状态固定为：

```text
running -> waiting -> running -> completed
running -> failed -> running
running|waiting -> cancelled
```

`completed` 和 `cancelled` 是终态；`failed` 只能通过显式 retry 恢复；waiting 只能由对应 Interaction 的合法 resume 恢复。owner execution 生命周期不随 AgentRun 取消而自动取消。

### 3.3 检查点与步骤

语义检查点 Schema 固定为 `addp.agent-checkpoint/v1`，只保存已经观察和用户确认的紧凑事实，不保存模型私有状态、Token、完整 Tool 结果或审批 payload。检查点最大 256 KiB。

AgentRunStep 记录稳定 Tool 或 Runtime 动作、协议调用 ID、ToolCall ID、受限输入投影、输出摘要、事实投影、错误归因和时间。step facts 最大 128 KiB；输出摘要最大 2000 字符。

## 四、AG-UI 事件流

### 4.1 唯一入口

内置 Agent 使用 `POST /api/v1/agent/chat` 返回 `text/event-stream`。旧 `0:`、`dag:` 私有前缀和 `result_type + result_data` 已删除，不提供兼容解析。

当前使用的标准事件包括：

- `RUN_STARTED`、`RUN_FINISHED`、`RUN_ERROR`；
- `TEXT_MESSAGE_START`、`TEXT_MESSAGE_CONTENT`、`TEXT_MESSAGE_END`；
- `TOOL_CALL_START`、`TOOL_CALL_ARGS`、`TOOL_CALL_END`、`TOOL_CALL_RESULT`；
- `STATE_SNAPSHOT`；
- `ACTIVITY_SNAPSHOT`，其中 ADDP A2UI Activity type 为 `a2ui-surface`。

事件必须按运行时发生顺序发出。Tool 事件只表达进度和受限结果投影，不能输出原始授权信息或模型隐藏推理。

### 4.2 安全持久化与回放

AgentRunEvent 以 AgentRun 内单调 `sequence` 保存安全事件投影，单事件最大 512 KiB。允许持久化文本、状态、Tool 名称和进度、Interaction 引用、Presentation 及 ResultRef；禁止持久化：

- Tool 原始参数和原始结果；
- User Access Token、Delegated Access Token 或认证头；
- 完整 workflow 审批 payload 和请求指纹；
- 模型隐藏推理、框架私有状态或未过滤异常对象。

回放入口为：

```text
GET /api/v1/agent/runs/{agent_run_id}/events?after={sequence}
```

客户端传入最后已处理 sequence，只接收更大的事件；SSE `id` 使用稳定 sequence。回放是安全投影重放，不重新执行 Tool 或 owner 操作。

### 4.3 取消与重试

取消入口只允许 `running|waiting`，将 Agent Runtime 和 Agent 模块内的 pending Interaction 投影置为 cancelled，并发出最终状态；不改变 owner approval，不取消已经创建的 owner execution。

重试入口只允许 `failed` AgentRun。重试创建新的协议调用身份并恢复同一语义检查点，不复制 AgentRun，不重放已成功 owner 写操作。失败重试和 Interaction 恢复都必须重新经过当前身份、状态和 owner 校验。

## 五、消息 parts

消息的 `parts` 是有序对象数组，最大 2 MiB。当前稳定类型为：

| type | 语义 |
| --- | --- |
| `text` | 用户或助手可见文本。 |
| `result_ref` | 对 owner 结果的稳定引用。 |
| `interaction_ref` | 对待处理或已处理 Interaction 的引用。 |
| `presentation_ref` | 对可重建 Presentation / A2UI Surface 的引用。 |

`content` 仅保留纯文本搜索和上下文用途，不能重新承担结构化结果。客户端必须按 parts 顺序渲染；未知类型不得执行，可降级为安全文本或跳过。

## 六、ResultRef、Interaction 与 Presentation

### 6.1 ResultRef

ResultRef Schema 固定为 `addp.result-ref/v1`，至少包含：

```json
{
  "schema": "addp.result-ref/v1",
  "owner_module": "develop",
  "kind": "execution",
  "ref": "execution:123"
}
```

ResultRef 不复制 workflow、execution、数据项、地图或表格业务事实。受信任客户端按引用调用 owner API 获取当前状态；构造规则必须服从 Tool Manifest 的 `result_ref` 声明。

### 6.2 Interaction

Interaction 表示当前需要用户完成的动作，包含稳定 ID、owner、状态、输入 Schema、AgentRun、创建时间和可选过期时间。当前稳定 `kind` 只有 `clarification` 和 `owner_approval`；资源选择使用 clarification 事实和 `ResourcePicker` Presentation，不建立第三种业务状态。

- Agent 持有对话澄清事实。
- owner 持有业务写审批事实，Agent 只保存投影。
- 浏览器按钮只发送声明式 action；不能提交 `approved=true` 或代替 owner 决策。
- Interaction 只能由原用户、原租户和关联 AgentRun 查询或恢复，重复或终态提交必须拒绝。

### 6.3 Presentation

Presentation 描述 ResultRef 或 Interaction 如何显示，是可重建投影，不是事实源。它至少声明协议、Catalog、Surface 和可选 `open_url`：

```json
{
  "protocol": "a2ui",
  "catalog_id": "addp.catalog/v1",
  "surface_id": "surface:789",
  "open_url": "https://addp.example.com/develop/executions/123"
}
```

Presentation 字符串字段最大 2000 字符。A2UI Surface 不写回 LLM 上下文，不能承载业务结果或审批决定。

## 七、A2UI Catalog

### 7.1 协议与安全

ADDP 使用官方 `@a2ui/web_core/v0_9` 处理 Surface 更新、数据绑定和 action dispatch，Catalog 固定为 `addp.catalog/v1`，单个 Surface 最大 500 KiB。

Catalog props 只允许声明式 JSON、稳定引用和受限展示参数。禁止函数、脚本、任意 HTML、任意 API URL、认证信息以及未注册组件。业务组件不直接解析 A2UI JSON；wrapper 只映射 props / emits，并按 ResultRef 访问 owner API。

### 7.2 当前组件

| 组件 | 用途 | 硬上限或边界 |
| --- | --- | --- |
| `WorkflowDag` | 展示候选 workflow DAG。 | 只展示声明式节点和边，不执行 workflow。 |
| `ClarificationChoice` | 回答 Agent-owned 澄清。 | 只能提交 Interaction 已声明选项。 |
| `ApprovalRequest` | 展示 owner 审批状态和跳转入口。 | 只能 `open_url` 或触发 `check`，不能作出批准决定。 |
| `TablePreview` | 展示受限表格投影。 | 最多 50 列、100 行，单元格只允许 JSON 标量。 |
| `MapView` | 展示受限 WGS84 GeoJSON。 | 最多 200 Feature、5000 个坐标值，不接受 URL。 |
| `ResourcePicker` | 从持久化候选中选择 locator。 | 最多 50 个候选，只能提交当前 Interaction 中已有值。 |

`GraphView` 不在当前 Catalog 中。新增组件必须由评测场景证明需求，并同步升级 Catalog 契约和安全测试。

### 7.3 能力协商与降级

- 支持 `addp.catalog/v1`：渲染注册的 ADDP 组件。
- 只支持 A2UI Basic Catalog：仅渲染安全基础组件。
- 不支持 A2UI：保留文本摘要、ResultRef 和受信任 `open_url`。
- CLI：输出结构化 JSON，由用户显式打开 ADDP 页面。

降级只能改变 Presentation，不得改变 Tool、ResultRef、Interaction 或 owner 业务路径。

## 八、上下文预算

进入 LLM 的历史按最新消息优先选择，固定预算为：

| 项目 | 上限 |
| --- | ---: |
| 历史消息数 | 20 |
| 单条消息 | 6000 字符 |
| 历史消息合计 | 24000 字符 |
| 会话摘要 | 2000 字符 |

Tool 大结果、A2UI Surface 和完整 owner 对象不进入上下文。被截断和省略的消息数量必须进入 `context_metrics`，不能静默丢失可观测性。

## 九、变更与验证

协议变更必须同步 Agent Backend、common-frontend Renderer、Agent Frontend、模块文档与评测契约。破坏性变更直接升级 Schema 或 Catalog 并删除旧解析路径。

最低验证：

```bash
cd agent/backend && venv/bin/python -m unittest \
  tests.test_ag_ui_protocol \
  tests.test_run_events \
  tests.test_messages \
  tests.test_checkpoints \
  tests.test_context
cd common-frontend/agent-ui && npm test
cd agent/frontend && npm test
make test-agent-eval
```

## 十、相关文档

- `docs/concepts/addp术语表.md`
- `docs/spec/addp智能体Tool开放规范.md`
- `docs/spec/addp智能体评测规范.md`
- `docs/spec/addp OAuth授权规范.md`
- `agent/docs/agent模块设计共识.md`
- `common-frontend/README.md`
