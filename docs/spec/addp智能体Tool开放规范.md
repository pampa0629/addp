# ADDP 智能体 Tool 开放规范

更新日期：2026-08-11

状态：正式规范。ADDP Tool Manifest、ToolExecutor、Python SDK、CLI 与 Agent Tool Provider 的能力开放边界以本文为准。

## 一、适用范围

本文规定 ADDP 面向内置和外部智能体开放 Tool 的唯一技术路线，包括：

- Tool 与 owner 模块业务 API 的责任边界；
- `addp.tool-manifest/v1` 的字段和校验要求；
- ToolExecutor、Python SDK、CLI 和 Agent Tool Provider 的单一调用链；
- 风险、认证、审批、审计、错误、输出上限和 ResultRef 声明；
- 新增、修改和删除 Tool 的完成标准。

Skill 的目录与写法见 `docs/skills/addp-Skill规范.md`；OAuth、受委托令牌和 owner 审批状态机见 `docs/spec/addp OAuth授权规范.md` 与 `docs/spec/addp授权上下文规范.md`；HTTP API 和 Swagger 见 `docs/spec/addp-API设计规范.md` 与 `docs/spec/addp-Swagger集成指南.md`。本文不复制这些事实源。

## 二、核心原则

1. ADDP Tool 是面向智能体的稳定操作能力，不等同于任意 HTTP endpoint。
2. Tool Manifest 是 AI Tool 契约事实源，owner 模块正式 API 和业务状态仍是业务事实源。
3. Python SDK 是访问 ADDP API 的唯一客户端实现。
4. CLI、Agent Tool Provider 以及未来存在真实消费者后才建设的 Adapter 都只能是薄适配器。
5. 不建设中心 Tool 服务，不在 common-python、Agent 或 Adapter 中复制 owner 业务状态。
6. Tool 只能通过 Gateway 和 owner 正式 API 访问业务能力，不能直接访问数据库或模块私有接口。
7. 新版本直接替换旧契约；不保留旧名称、旧字段、兼容解析或双路由。

唯一调用链为：

```text
Agent Runtime
  -> Tool Adapter
  -> ToolExecutor
  -> Python SDK
  -> Gateway / owner public API
  -> owner business logic
```

## 三、Tool Manifest

### 3.1 唯一位置与版本

Manifest 唯一位置为：

```text
common-python/addp_common/tools/manifest.json
```

根 Schema 固定为 `addp.tool-manifest/v1`，由 `common-python/addp_common/tools/manifest.py` 严格加载。未知字段、重复 Tool 名称、非法枚举或不一致的授权边界必须拒绝，不能忽略或降级。

### 3.2 发布期授权投影

`common-python/addp_common/tools/manifest.json` 是 Tool 名称、owner、audience、Scope 与 Permission 映射的唯一事实源。System 不得维护手写 Tool Scope 清单，也不得在运行时扫描仓库或读取 Python 包路径。

统一发布期聚合器从该 Manifest 和 owner Permission Manifest 生成：

```text
system/backend/internal/authorization/tools_generated.go
```

该文件是供 System Runtime 注入的只读、确定性 Go 投影，不是第二事实源。生成内容只包含已通过以下校验的 Tool：`audience == owner`、`required_scopes == [tool.name]`、`required_permissions` 非空且全部属于 owner、处于 active 状态并允许委托。CI 必须以同一聚合器的只读检查模式进行字节漂移校验；任何差异都必须先修改唯一 Manifest，再重新生成，不得手工编辑生成文件。

### 3.3 Tool 字段

每个 Tool 必须声明以下字段：

| 字段 | 约束 |
| --- | --- |
| `name` | 全局唯一稳定名称，也是 OAuth Scope 名称。 |
| `version` | Tool 契约版本；输入、输出或语义破坏性变化时升级。 |
| `description` | 用途、适用边界和关键前置条件。 |
| `owner` | 业务事实和权限校验归属模块。 |
| `risk` | 只允许 `read`、`propose`、`write`。 |
| `approval.mode` | 当前只使用 `none` 或 `owner_policy`。 |
| `auth` | 必须是 `delegated_access_token`；`audience` 必须等于 `owner`；`required_scopes` 必须且只能包含 Tool 稳定名称；`required_permissions` 必须是非空、无重复的 owner Permission Key 列表，且对应 Permission 必须 `delegable=true`。 |
| `permission_enforced_by` | 固定为 `owner`。 |
| `audit` | `owner` 表示 owner 记录调用；`required` 表示必须具备完整审计绑定。 |
| `result_ref` | 只允许 `none`、`locator` 或当前已实现的 `execution + addp.result-ref/v1`。 |
| `limits.max_bytes` | UTF-8 紧凑 JSON 输出的硬上限。 |
| `errors` | 允许向 Adapter 暴露的稳定错误码集合。 |
| `input_schema` | JSON Schema Draft 2020-12；必须明确 `additionalProperties`。 |
| `output_schema` | JSON Schema Draft 2020-12；只声明智能体可消费的受限投影。 |

Manifest 不保存第二套 HTTP 路径事实。ToolExecutor 通过 Python SDK 方法完成映射，owner API 仍由对应客户端实现和 Swagger 持有。

### 3.4 当前 Tool 集合

| Tool | Owner | Role Permission | 风险 | 审批 | ResultRef | 最大输出 |
| --- | --- | --- | --- | --- | --- | ---: |
| `engine.list` | System | `system.engine.read` | read | none | none | 128 KiB |
| `data.search` | Manager | `manager.search.execute` | read | none | locator | 128 KiB |
| `resource.ancestors.get` | Meta | `meta.catalog.read` | read | none | locator | 128 KiB |
| `data.preview` | Manager | `manager.data_item.read` | read | none | locator | 256 KiB |
| `workflow.operators.list` | Develop | `develop.task.read` | read | none | none | 512 KiB |
| `workflow.draft.generate` | Copilot | `copilot.workflow.execute` | propose | none | none | 256 KiB |
| `query.draft.generate` | Copilot | `copilot.sql.execute` | propose | none | none | 256 KiB |
| `notebook.draft.generate` | Copilot | `copilot.notebook.execute` | propose | none | none | 512 KiB |
| `transfer.draft.generate` | Copilot | `copilot.transfer.execute` | propose | none | none | 256 KiB |
| `workflow.validate` | Develop | `develop.task.execute` | read | none | none | 128 KiB |
| `workflow.run` | Develop | `develop.task.execute` | write | owner_policy | execution | 64 KiB |
| `execution.get` | Develop | `develop.task.read` | read | none | execution | 128 KiB |

这是当前完整集合。未出现在 Manifest 中的 API 不能被 Adapter 自行包装为 ADDP Tool。

## 四、执行语义

### 4.1 ToolExecutor

`common-python/addp_common/tools/executor.py` 是 Manifest 到 Python SDK 的唯一执行映射。一次调用必须按以下顺序完成：

1. 按稳定名称读取 Tool 定义；不存在时返回 `tool_not_found`。
2. 使用输入 Schema 校验参数；失败返回 `invalid_arguments`。
3. 要求非空 `agent_run_id` 和 `tool_call_id`，作为委托与审计绑定。
4. 使用进入 Runtime 的 User Access Token 向 System 申请精确 `audience + scope` 的短期 Delegated Access Token。
5. 校验 System 返回的 audience、scopes、AgentRun 和 ToolCall 绑定。
6. 通过对应 Python SDK client 调用 owner 正式 API。
7. 只允许 Manifest 声明的 owner 错误码透传；其他 HTTP 错误统一为 `owner_api_error`。
8. 使用输出 Schema 校验响应，再检查紧凑 JSON 字节上限。

ToolExecutor 不重试 write Tool，不缓存委托令牌，不把源 User Access Token 传给 owner，也不把 owner 原始响应绕过输出 Schema 返回。

### 4.2 长任务与大结果

Tool 不同步等待长任务完成。标准路径为 owner 创建 execution，Tool 返回 `execution_id`，智能体使用 `execution.get` 查询状态，最终消息返回 ResultRef、locator 或 owner 结果引用。

完整表格、地图要素、文件内容、A2UI Surface 和 owner 大对象不得作为无界 Tool Result 进入模型上下文。超限统一返回 `result_too_large`，不能截断后伪装成完整业务结果。

### 4.3 `workflow.run`

`workflow.run` 的两次调用由同一个 Tool 契约表达：

- 首次请求只包含 workflow 执行输入，不包含审批字段；owner 返回 `approval_required` 投影且不得创建 execution。
- 恢复请求只包含 `approval_id + request_fingerprint`，不得再次携带 workflow payload。

审批事实、身份隔离、过期、拒绝、一次性消费及跨 AgentRun 重放规则由 OAuth 规范持有。Manifest 必须声明当前稳定错误码，包括 `approval_forbidden`、`approval_not_found`、`approval_not_approved`、`approval_rejected`、`approval_expired`、`approval_request_mismatch` 和 `approval_already_consumed`。

### 4.4 `query.draft.generate`

`query.draft.generate` 的 `resources[]` 只承载由 owner Tool 验证过的具体 data item，不承载 database、schema、directory 等执行容器。查询执行范围由 Develop 的任务或 execution 契约持有，不进入 Copilot Tool 输入。

调用方可以传入可选 `current_query` 表示编辑器已有候选文本。它不是资源事实，也不产生第二条执行路径；生成结果仍只返回候选查询文本。对于 MQL，当 `resources=[]` 且 `current_query` 是单个 JSON command object，并且恰好通过 `find`、`aggregate`、`count` 或 `distinct` 之一声明主 collection 时，Copilot 可以跳过资源发现并基于现有命令继续生成。其他无资源情况继续进入当前 Query Engine 范围内的数据发现和确认流程。

Copilot 默认保留 `current_query` 已声明的主 collection，除非用户明确要求修改；没有 `resources[]` 字段事实时只能复用编辑器中已经出现的字段，不得把 `current_query` 当作已验证 metadata 或据此编造新字段。MongoDB database locator 不得写入 `resources[]`、`current_query` 或生成的 MQL。

## 五、Adapter 边界

### 5.1 Python SDK

Python SDK 负责 HTTP、认证头、请求和响应解析；不负责 Agent 规划、Skill 加载、审批决定或 owner 业务状态。业务模块的 client 方法是 ToolExecutor 的唯一远程调用实现。

### 5.2 CLI

CLI 只负责参数解析、调用 ToolExecutor 和进程协议：

```text
addp tools list
addp tools get <tool-name>
addp tool call <tool-name> --arguments '<json>'
```

成功时 stdout 只输出 Tool 结果 JSON；失败时 stdout 输出 `ToolExecutionError.as_dict()` 的标准错误 JSON，stderr 只输出诊断，进程使用稳定非零退出码。CLI 不实现第二套 HTTP client 或审批逻辑。

### 5.3 Agent Tool Provider

内置 Agent Provider 只把稳定 Tool 名和 JSON Schema 映射为 Runtime Tool，并把结构化调用交给 ToolExecutor。它可以生成 Runtime 事件和受限审计投影，但不能改变 Tool 输入输出语义。

当前不开放 MCP Adapter。只有存在真实外部消费者、宿主协议和隔离评测后，才可在同一 Manifest、ToolExecutor 与 SDK 主线上增加薄 Adapter；不得新建 Tool Core。

## 六、ResultRef 声明

ResultRef 是消息对 owner 结果的稳定引用，不是新的全局 Artifact 实体。Manifest 只声明某 Tool 结果是否可转换为 ResultRef：

- `none`：不得升级为 ResultRef；
- `locator`：结果中的可信 ResourceLocator 可作为资源引用；
- `execution`：使用 `addp.result-ref/v1` 引用 owner execution。

ResultRef 的消息结构、Presentation 投影和客户端加载规则见 `docs/spec/addp智能体交互协议规范.md`。ToolExecutor 返回 owner 结果，Agent Runtime 才能按 Manifest 声明构造 ResultRef；不得从任意形似 ID 的字段猜测引用。

## 七、错误与安全

所有 Tool 共享以下基础错误类别：

- `tool_not_found`、`tool_not_implemented`；
- `invalid_arguments`；
- `delegation_rejected`、`delegation_unavailable`、`invalid_delegation_response`；
- `owner_api_error`、`owner_api_unavailable`、`invalid_owner_response`；
- `result_too_large`。

owner 业务错误只有在 Manifest `errors` 中显式声明时才能保留稳定 code。错误消息和 details 不得包含 Token、完整请求 payload、审批指纹、owner 私有响应或模型隐藏推理。

## 八、变更与验证

新增或修改 Tool 必须按单一路线同步完成：

1. 先确认 owner 模块、正式 API、权限与业务状态归属；概念冲突先修订规范。
2. 更新 owner API、Swagger 和必要的双语错误。
3. 更新 Python SDK client。
4. 更新 Manifest 和 ToolExecutor 映射，删除被替换的旧名称或旧输入路径。
5. 更新 Agent Adapter / CLI 消费面，不复制业务逻辑。
6. 增加输入、输出、授权、错误、输出上限和 owner 副作用测试。
7. 更新受影响的 Skill 与智能体评测场景。

最低验证：

```bash
cd common-python && ../agent/backend/venv/bin/python -m unittest discover -s tests
make test-agent-eval
```

完成标准是 Manifest 严格加载、SDK/ToolExecutor/Adapter 只有一条实现路径、owner 权限与 Swagger 同步、关键测试通过，仓库中不存在旧 Tool 名称或兼容分支。

## 九、相关文档

- `docs/concepts/addp术语表.md`
- `docs/skills/addp-Skill规范.md`
- `docs/spec/addp智能体交互协议规范.md`
- `docs/spec/addp智能体评测规范.md`
- `docs/spec/addp OAuth授权规范.md`
- `docs/spec/addp授权上下文规范.md`
- `docs/spec/addp-API设计规范.md`
- `common-python/README.md`
