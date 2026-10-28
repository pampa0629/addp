# ADDP TaskProvider接口规范

[返回任务体系规范](../addp任务体系规范.md)

定义模块级 Provider 声明、标准端点、任务列表与详情、执行参数契约和 capabilities。编排调用规则见[编排与调度](编排与调度.md)。

阅读导航：[TaskProvider 规范](#taskprovider-规范)、[Provider 基本字段](#provider-基本字段)、[标准 endpoint](#标准-endpoint)、[标准响应体](#标准响应体)、[执行请求体](#执行请求体)、[多任务类型 provider](#多任务类型-provider)、[capabilities.task_capabilities](#capabilitiestask_capabilities)。

## TaskProvider 规范

TaskProvider 是模块的一种角色，不是独立业务 owner，也不是独立注册实体。System 把 Provider 能力声明保存为模块定义的一部分，供 Orchestrator 和 Monitor 发现模块任务能力；模块运行地址只来自同一模块定义下当前租约有效的 Backend 实例。

TaskProvider 按模块声明，不按任务类型注册。一个模块只有一个 provider，并通过 `task_capabilities[]` 声明多个任务类型能力。Provider 的稳定 ID 就是模块定义 ID；重复发布相同声明必须幂等，不递增模块定义版本，声明实际变化时才递增模块定义版本，且不得覆盖管理员 `enabled`。

模块 Backend 必须通过唯一的模块注册请求同时发布模块实例和可选 TaskProvider 声明，不得再调用独立 TaskProvider 注册入口。只有 Backend 可以发布、更新或撤销该角色：Backend 注册不携带声明表示模块不再提供 TaskProvider 角色；Worker、Scheduler 不携带 Provider 声明，也不得清空 Backend 已发布的声明。模块进程离线时声明继续保留，但 Provider 变为不可用；后续任一 Backend 重新注册并取得有效租约后立即恢复可用。

`task_capabilities[]` 只声明已经存在持久任务定义并能通过标准任务 endpoint 被 Orchestrator 引用的类型。仅提供即时 API、没有任务定义的 ad-hoc execution type 不得为了统一监控而注册空任务列表或伪任务详情；它只需写入 `common.task_executions` 并由 owner 提供自身即时执行入口。

### Provider 基本字段

| 字段 | 说明 |
| --- | --- |
| `module_name` | 模块名，例如 `manager` |
| `module_version` | 当前模块定义乐观并发版本 |
| `enabled` | 模块定义的管理员启用意图；不随进程上下线改变 |
| `display_name` | 展示名称 |
| `description` | 描述 |
| `available` | 是否存在 `enabled + backend + up + lease valid` 的可调用实例 |
| `unavailable_reason` | 不可用原因；当前为 `module_disabled` 或 `no_valid_backend`，可用时为空 |
| `backends[]` | System 在读取时从当前有效 Backend 租约生成的临时端点池；声明和数据库均不保存，按 `instance_id` 稳定排序 |
| `backends[].instance_id` | Backend 运行实例 ID |
| `backends[].base_url` | Backend 实例临时调用地址 |
| `backends[].lease_expires_at` | Backend 实例当前租约到期时间 |
| `task_list_endpoint` | 任务列表 endpoint |
| `task_detail_endpoint` | 任务详情 endpoint |
| `task_execute_endpoint` | 任务执行 endpoint |
| `task_status_endpoint` | execution 状态 endpoint |
| `task_cancel_endpoint` | execution 取消 endpoint |
| `capabilities` | provider 能力声明 |

Provider 不维护独立 `is_enabled`。管理员意图只来自模块定义 `enabled`；实例存活只来自模块运行租约，两者不得被 Provider 声明覆盖。

### 标准 endpoint

第一阶段应收敛到以下 endpoint 后缀语义：

| 能力 | Endpoint |
| --- | --- |
| 列任务 | `GET /tasks?task_type=` |
| 查任务 | `GET /tasks/{task_type}/{id}` |
| 执行任务 | `POST /tasks/{task_type}/{id}/execute` |
| 查执行 | `GET /executions/{execution_id}` |
| 取消执行 | `POST /executions/{execution_id}/cancel`，仅当对应 `task_type.supports_cancel=true` |

owner 应将整组 TaskProvider 端点挂载在独立路由空间，并以专用 Runtime Permission 和固定 Service Client Guard 与用户任务管理 API 隔离；例如 Transfer 使用 `/api/v1/transfer/task-provider/tasks`，用户任务定义只使用 `/api/v1/transfer/task-definitions`。不得让机器编排复用用户的宽泛任务读写权限，也不得让用户任务列表复用 TaskProvider 发现路由。

所有 TaskProvider 声明必须使用独立 `/task-provider/` 路由空间和以上 endpoint 后缀语义；公共声明校验器必须拒绝落在人用路由空间的声明。取消接口不是必选能力；未声明支持取消的任务类型不得注册或展示取消入口。确需处理现有路径时，只能作为入口层迁移工作，不得形成长期双轨命名。

System 的模块注册入口接收 TaskProvider 声明时必须校验标准 endpoint：任务详情和执行 endpoint 必须包含 `{task_type}` 与 `{id}`，执行状态 endpoint 必须包含 `{execution_id}`，并且必须分别使用 `/tasks`、`/tasks/{task_type}/{id}`、`/tasks/{task_type}/{id}/execute`、`/executions/{execution_id}` 和 `/executions/{execution_id}/cancel` 标准后缀；前缀只用于 owner 模块和独立 TaskProvider 路由空间，不得发明 `/scan/runs/{execution_id}`、`/tasks/{id}/run` 等私有或旧后缀。Orchestrator 调用 provider 时只替换 `{task_type}`、`{id}`、`{execution_id}` 三类标准占位符；模块私有 UI 或 CRUD 路径可以继续使用 `:id`、`:task_id` 等前端或 Gin 写法，但不得进入 TaskProvider endpoint 契约。

### 标准响应体

TaskProvider 标准 endpoint 必须使用直接响应，不得使用 `{status,data}`、`{status,message}` 或模块私有包装格式。

TaskProvider 任务列表是跨模块编排专用契约，不适用通用业务分页响应 `{data,total,page,page_size,total_pages}`；Orchestrator 和 Monitor 只消费本节定义的 `items` 列表形态。

`GET /tasks?task_type=` 返回任务列表对象：

```json
{
  "items": [],
  "total": 0,
  "page": 1,
  "page_size": 20
}
```

`GET /tasks/{task_type}/{id}` 直接返回 owner 模块的任务定义摘要对象。对象必须包含 `id`、`task_type`、`name`、`status` 和该具体任务的 `execution_contract`；多任务类型 provider 可以按 `task_type` 返回不同任务定义 DTO，但不得再包一层 `data`。`enabled`、`schedule`、`next_run_at` 只允许在该 task type 明确声明并实现 `supports_schedule=true` 的 owner 调度闭环时出现；不支持调度的 TaskProvider 不得暴露这些调度活状态字段。`input_schema.required` 可以声明只能在手动触发或 Orchestrator Step 中绑定的必填运行时输入；`input_defaults` 是部分默认值，只校验已提供字段的类型和约束，不要求覆盖全部 required。执行前，owner 与 Orchestrator 必须在默认值、显式参数和上游输出绑定解析后，对完整参数再执行一次严格 required 校验。

Quality `quality_plan` 是纯手动/Orchestrator 显式执行类型，当前不保存或返回 `enabled`、`schedule`、`next_run_at` 等调度活状态字段。`execution_contract` 是具体任务可执行输入和稳定输出的唯一事实源：

```json
{
  "execution_contract": {
    "input_schema": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    },
    "input_defaults": {},
    "input_ui_schema": {},
    "output_schema": {
      "type": "object",
      "properties": {},
      "additionalProperties": false
    }
  }
}
```

不支持执行输入覆盖或没有稳定输出的任务仍必须返回对应的闭合空对象。`input_defaults` 只提供任务定义中已保存的可覆盖默认值；它不能隐式填充任务定义本身必须持久的业务字段，也不能绕过执行时 required 校验。`input_ui_schema` 只描述 `input_schema.properties` 中已声明字段的控件语义，不得增加输入字段或覆盖服务端 Schema 约束。需要稳定展示顺序时，字段和分组必须在对应 UI Schema 节点声明从 `0` 开始的 `order`；消费者必须按 `order` 排序，不能依赖 JSON 对象属性顺序。算子工作流的分组顺序使用稳定 DAG 拓扑顺序：上游算子在前，同层并行算子按任务定义 `tasks[]` 的保存顺序排列；分组内字段按公开参数声明顺序排列，不依赖画布坐标。`format=resource-locator` 的值在契约和请求中仍使用标准 ResourceLocator，但用户界面只能展示解析后的资源路径、名称和本地化类型，不得直接显示 `addp://` URI、Engine ID、`node_id` 或 `item_id`；无法解析时统一显示“已配置资源”，不得回退为原始内部值。

`resource_tree_picker` 必须遵守字段 Schema 的值类型：`type=string` 直接保存 ResourceLocator 字符串，`type=object` 按声明保存资源对象。共享执行参数表单在资源选择、摘要显示和上游绑定时保持该类型，不能把标量路径包装成对象；UI Schema 不引入另一套值类型声明。

Develop 查询任务的 `content.query_parameters[]` 是全部查询参数的唯一定义事实源，每项固定包含唯一参数名 `name` 和 `type`，可选包含 `description` 与 `default`，不得再保存 `title`、显示名称或第二套引用标识。值参数类型固定为 `string`、`integer`、`number`、`boolean`，分别与 SQL `:name`、Cypher `$name` 或 MQL `{"$param":"name"}` 引用完全一致。关系参数固定使用 `type=relation`，当前只允许 PostgreSQL SQL 以未加引号、未限定 schema 的裸 `name` 引用；其可选 `default` 固定为 `{ "locator": "<ResourceLocator>" }`，只能指向与 `execution_config.engine_id` 相同的已有表。全部参数共享同一命名空间、名称唯一，且每个定义必须被查询引用、每个引用必须有定义；关系参数名不得与同一作用域的 CTE 重名。所有参数类型的 `default` 都是可选字段：已保存默认值进入 `input_defaults`，没有默认值的参数由 `input_schema.required` 声明为执行必填；布尔 `false`、数字 `0` 和空字符串都是明确默认值，不能与字段缺失混淆。`query_parameters[]` 的保存顺序决定 `input_ui_schema.<name>.order`。Copilot 查询草稿可以提议任意参数子集及可选默认值，但它必须与候选文本在同一响应中一起校验和回填；只有用户保存后才成为 Develop 任务事实。查询任务详情必须从该定义派生 `execution_contract`；未定义查询参数时返回闭合空契约。即时查询、手动任务执行和 Orchestrator 都使用“本次覆盖 > 保存默认值 > 缺失则拒绝执行”的同一解析规则，且不能把执行值写回 `content.query_parameters` 或查询文本；关系参数先经同一 AST 编译器变为只读预览查询，Orchestrator 另行提交写入目标 `target_locator`。

`GET /executions/{execution_id}` 直接返回统一 execution 的显式安全状态投影（复用 Common Observation，不序列化 TaskExecution），`execution_id` 必须是 `common.task_executions.execution_id`。TaskProvider 声明的稳定输出只允许持久化在 `common.task_executions.metadata.outputs`，状态接口的 metadata 只保存该 outputs 投影，并把该对象同时投影为响应顶层必填 `outputs`；没有稳定输出时返回闭合空对象 `{}`，不得省略、返回 `null`，也不得把 `metadata.result.outputs`、模块私有结果或整个 `metadata` 解释为稳定输出。

```json
{
  "execution_id": "uuid",
  "status": "success",
  "metadata": {
    "outputs": {
      "target_locator": "addp://engine/2/path/public/result?type=table"
    }
  },
  "outputs": {
    "target_locator": "addp://engine/2/path/public/result?type=table"
  }
}
```

`metadata.outputs` 是 durable execution 结果事实，顶层 `outputs` 只是 TaskProvider HTTP 交接投影，不形成第二持久事实。Go owner 必须复用 `common/taskprovider` 的标准 execution 状态响应与严格输出提取能力，不得在各模块复制不同的嵌套读取、兼容 fallback 或响应包装。

Owner 模块若需要保存下游运行时或外部系统返回的本地执行 ID，不得把该本地 ID 命名为 `execution_id`，以免和统一执行 ID 形成双事实源。字段应按来源命名，例如 Develop 工作流运行时返回的本地执行 ID 保存为 `runtime_execution_id`。TaskProvider 如果需要让下游对已成功的当前平台 execution 做一致性校验，可以把同一 `common.task_executions.execution_id` 声明为稳定输出 `execution_id`；它只是统一身份的显式交接，不是第二执行事实。

Develop 工作流任务可以在 execution 结果摘要中保存 `runtime_status` 诊断对象，用于排查工作流运行时本地状态、进度、错误码和耗时。`runtime_status` 中的本地执行 ID 字段同样必须命名为 `runtime_execution_id`。`runtime_status` 不得作为 Orchestrator 或 Monitor 的主状态源，不得替代 `common.task_executions.status/progress/error_details`，也不得保存运行时完整 `result`、`all_results` 或原始响应体。

错误响应统一使用 ADDP API 规范的 `{error}`：

```json
{
  "error": "任务不存在"
}
```

HTTP 状态码表达错误类型，响应体不得重复携带 `status=error`。

### 执行请求体

执行请求体统一为：

```json
{
  "trigger_type": "manual",
  "source": "orchestrator",
  "parent_execution_id": "uuid",
  "parameters": {}
}
```

约束：

1. `trigger_type` 只能是 `manual` / `scheduled`。
2. `source` 表示触发来源。
3. `parameters` 是符合具体任务 `execution_contract.input_schema` 的本次执行覆盖，只影响当前 execution，不得直接改写任务定义；未提交字段使用任务保存值。
4. Orchestrator 的上游输出绑定只支持完整字符串的内部序列化 `{{step_id.outputs.declared.path}}`；用户只能从 UI 列出的已声明稳定输出中选择，不得手写任意结果路径，也不支持局部字符串插值。
5. 输出绑定引用的 `step_id` 必须存在，并且必须在当前 Step 的 `depends_on` 中显式声明；保存和执行编排时都必须拒绝未知引用、隐式数据依赖、自引用、未在来源任务 `output_schema` 声明的路径和不兼容的目标参数类型。
6. 输出绑定解析只返回被引用输出的原始值，不做隐式类型转换。运行时如果引用步骤没有结果、字段路径不存在，或路径试图进入非对象值，当前 Step 必须失败，不得把缺失值静默改为 `null` 继续执行。
7. provider 必须按执行时重新读取的具体任务输入契约严格校验解析后的参数；未知字段、类型错误或不支持覆盖时必须明确拒绝，不得静默忽略。
8. 普通查询参数只允许绑定值，不得动态替换集合名、字段名、排序方向、关键字或任意查询片段。唯一例外是 Develop 明确声明的 PostgreSQL `relation` 参数：它以裸关系节点出现，并由 AST 按 ResourceLocator 安全编译；不得通过字符串值或模板插值模拟关系参数。
9. 查询参数绑定必须由声明对应能力的 Query Runtime Provider 使用数据库驱动、原生参数 Map 或解析后的结构化对象完成；禁止 `strings.Replace`、模板插值或先格式化为查询字面量再执行。

执行成功受理时 HTTP 状态码必须为 `202 Accepted`，响应体必须返回本次执行的统一 `execution_id`。标准最小响应为：

```json
{
  "execution_id": "uuid",
  "status": "running"
}
```

执行响应不得使用 `{status,data}` 包装；`status` 字段表示 execution status，不表示 HTTP 请求成功或失败。

### Orchestrator 默认值校验

Orchestrator 保存编排和执行前共用同一参数校验路径：用具体任务的 `input_defaults` 补齐 Step 未覆盖的顶层参数，再按 `input_schema` 严格校验。没有默认值的必填参数仍必须提供；显式参数（包括 `null`、`0`、`false`）优先，不能因校验失败回退默认值。显式对象按整体校验，不做深合并来补其内部缺失的必填字段。

该补齐只用于 Orchestrator 校验，不写回 Step，也不扩充发给 owner 的部分覆盖 `parameters`；owner 保持任务配置的唯一事实源，并执行最终领域校验。保存时仅允许完整的声明输出绑定作为延迟值，执行时重取最新具体任务契约、解析绑定后严格校验真实值；完整保存与执行约束见[编排定义规范](编排与调度.md#orchestrator-规范)。

### 多任务类型 provider

多任务类型模块通过 `task_type` 分派到 owner 模块内部不同任务表和服务。

以 Manager 为例：

| provider | task_type | 任务定义表 | 执行 owner |
| --- | --- | --- | --- |
| `manager` | `vector_tile_cache_generation` | `manager.task_definitions` | Manager |
| `manager` | `vector_tile_set_generation` | `manager.task_definitions` | Manager |
| `manager` | `vector_materialized_view_generation` | `manager.task_definitions` | Manager |
| `manager` | `embedding` | `manager.embedding_tasks` | Manager |

约束：

1. `task_id` 只在 `provider + task_type` 命名空间内唯一。
2. Orchestrator Step 必须保存 `provider`、`task_type` 和 `task_id` 三元组。
3. Monitor 展示任务详情时必须按 `module + task_type + source_task_id` 回查 owner provider。
4. 不得为了多任务类型把一个模块拆成多个 provider，例如 `manager_mvt`、`manager_embedding`。

### capabilities.task_capabilities

模块注册发布 TaskProvider 角色声明时必须使用 `task.capabilities/v2` schema，并声明稳定的 `task_capabilities[]` 任务类型能力数组：

```json
{
  "schema_version": "task.capabilities/v2",
  "task_capabilities": [
    {
      "type": "scan",
      "display_name": "扫描任务",
      "description": "执行元数据扫描",
      "definition_schema": { "type": "object" },
      "supports_schedule": true,
      "supports_cancel": false,
      "supports_inline_execution": false,
      "create_url": "/meta/scan",
      "edit_url": "/meta/scan?task_id=:id",
      "deprecated": false
    }
  ]
}
```

每个任务类型至少应包含：

| 字段 | 说明 |
| --- | --- |
| `type` | 稳定任务类型，例如 `vector_tile_cache_generation`；必须匹配 `^[a-z][a-z0-9_]*$` |
| `display_name` | 展示名称 |
| `description` | 任务类型说明 |
| `definition_schema` | 任务定义公开摘要 JSON Schema，不用于 Orchestrator 创建、编辑或渲染完整 owner 任务定义；当前必须是对象 schema |
| `supports_schedule` | 该任务类型是否支持定时 |
| `supports_cancel` | 该任务类型是否支持真实取消；不能中断执行体时必须为 `false` |
| `supports_inline_execution` | v2 保留字段；当前必须为 `false`，不支持无持久任务定义的一次性执行 |
| `create_url` / `edit_url` | owner 模块创建入口与任务定义入口；优先使用 Console 模块路由，例如 `/develop/sql?action=create` |
| `deprecated` | 是否废弃 |

约束：

1. `schema_version` 必须为 `task.capabilities/v2`。
2. `create_url` 和 `edit_url` 属于具体 `task_type`，不得放在 provider 顶层。
3. System 模块注册入口必须校验 capabilities schema，不符合规范的 TaskProvider 声明不得发布成功。
4. `task_type` 是 provider 对外契约，不能随 UI 文案变化；不得使用大写、短横线、空格或本地化文本。
5. provider 顶层私有扩展字段必须使用 `x_` 前缀，例如 `x_owner_features`；未加 `x_` 前缀的未知顶层字段必须被 System 注册入口拒绝，避免与未来标准字段冲突。`task_capabilities[]` 内部只允许本文列出的标准字段，不允许私有扩展字段；任务类型级扩展需要先修订 capabilities 规范。
6. `definition_schema` 必须声明为 JSON 对象 schema，最小值为 `{ "type": "object" }`。System 注册入口以及任务详情契约校验必须复用平台可理解的 JSON Schema 子集：允许 `type`、`title`、`description`、`properties`、`required`、`enum`、`default`、`additionalProperties`、`items`、`minimum`、`maximum`、`minLength`、`maxLength`、`minItems`、`maxItems`、`format`；不得使用 `$ref`、`oneOf`、`anyOf`、`allOf`、`not` 等复杂组合或远程引用。
7. `task_capabilities[]` 不得出现 `execution_schema`、`output_schema` 或 UI schema。owner 必须在每个任务详情的 `execution_contract` 返回精确 `input_schema`、`input_defaults`、`input_ui_schema` 和 `output_schema`，并在执行入口再次按最新契约校验。
8. Orchestrator Step 参数编辑必须以具体任务详情的 `execution_contract` 为唯一能力来源。闭合对象中声明的标量字段渲染为结构化控件，资源引用按 `input_ui_schema` 渲染资源选择器；每个输入可选择使用“工作流配置”、在当前 Step 中“执行时指定”或引用显式依赖的“上游输出”。“工作流配置”表示不提交覆盖字段，执行时读取 owner 任务定义中已保存的当前值；“执行时指定”表示 Step 保存显式覆盖值，但不改写 owner 任务定义。编排画布必须按 `input_schema + input_ui_schema` 生成逻辑输入端口，按 `output_schema` 生成稳定输出端口；资源对象只生成一个逻辑端口，不得把 locator 或自动派生的 geometry 字段拆成用户端口。连接输出端口与输入端口时必须校验类型和环路，原子地写入上游输出绑定并补充 `depends_on`；参数表单和画布连线必须双向同步。资源参数摘要必须展示引擎实例名称、按引擎原生风格格式化的资源路径和本地化资源类型，不得展示 Engine ID 或 `addp://` locator；展示事实不得写回执行参数。资源选择结果已经声明 geometry 字段时，单字段必须自动选中，多字段只能从识别结果中选择，不得要求用户自由输入字段名。闭合空对象不显示参数编辑；不得按 provider 或 task type 硬编码表单能力，也不得保留整份任意 JSON 作为旁路。
9. `supports_inline_execution` 在 `task.capabilities/v2` 中必须为 `false`。内联执行需要新的 endpoint、执行配置 schema 和 Orchestrator Step 模型，必须作为后续专题设计，不得只通过 capabilities 布尔值打开。
10. `supports_cancel` 与 `task_cancel_endpoint` 必须双向一致：任一任务类型声明 `supports_cancel=true` 时 provider 必须注册标准取消 endpoint；没有任务类型支持取消时 provider 不得注册 `task_cancel_endpoint`。模块内部已有取消 API 不等于 TaskProvider 标准取消能力。
11. Orchestrator 可以按 `module_version` 缓存纯 capabilities 解析结果，但每次任务发现、详情读取、执行提交和状态回查前都必须向 System 获取当前 Provider 可用性与 `backends[]`，并从有效端点池中按稳定轮询选择一个实例；不得缓存 `available`、`backends[]` 或已解析的单个地址，也不得在模块离线后继续调用旧地址。非幂等执行 POST 只能发送一次，失败后不得自动换实例重放；后续只读状态回查可以重新解析实例。
12. `create_url` / `edit_url` 应使用 Console 路由形式，可包含模块内深层路径和 query，例如 `/transfer/tasks/:id/edit`、`/develop/workflow?action=edit&id=:id`、`/graph/graphs/:graph_id/build/tasks/:id`；前端负责替换 `:id` / `{id}` / `:task_id` / `{task_id}` / `:graph_id` / `{graph_id}`。地址栏同步、canonical 参数和浏览器历史必须同时遵守 `docs/spec/addp前端路由与可恢复状态规范.md`。
13. 模块新增或删除任务类型时，必须更新自身 capabilities、文档和 Swagger；修改任务级输入/输出契约时必须同步任务详情和执行入口测试。
14. `deprecated=true` 的 task type 不再作为可用任务类型处理。Orchestrator 保存和执行编排时都必须拒绝引用 deprecated task type；ADDP 当前不为废弃任务类型保留兼容迁移路径。历史 execution 查询只按既有 execution 记录展示，不要求 owner 继续提供可编辑任务定义入口。
15. `edit_url` 是 `task.capabilities/v2` 的任务定义入口字段，不承诺任务定义一定可修改。来源驱动且定义不可变的任务必须在该 URL 展示带稳定任务 ID 的只读定义；不得把结果筛选页、无任务身份的模块首页或 Data Explorer 通用入口冒充任务定义入口。
16. 来源驱动任务的 `create_url` 可以指向 owner 的来源选择页，由用户选择源对象后通过 owner 领域动作派生任务定义。此类任务不得同时保留允许调用方直接提交私有任务配置的第二套创建或更新 API。

`common/taskprovider` 是 TaskProvider 契约的公共解析和校验边界，负责校验 `task.capabilities/v2`、标准任务列表响应 `{items,total,page,page_size}`、任务级 `execution_contract` 和输入参数实例。System 模块注册入口、Monitor provider health、Orchestrator 编排保存和执行前校验必须复用该公共能力，不得在各模块重复维护一套 capabilities、任务发现响应或 schema 实例校验逻辑。owner 模块负责生成自身 capabilities、为每个任务生成精确契约并实现标准 endpoint；`common/taskprovider` 不访问 System 模块控制面，不调用 owner 模块，也不处理执行调度。

当前不应默认打开任何模块的 `supports_cancel=true`。标准取消能力必须先在专题中确认 worker 中断、资源清理、状态一致落库、重复取消幂等和可观测诊断等前置条件，再单独更新对应模块能力声明。

API-only 阶段的新增任务类型仍必须注册稳定的 `create_url` / `edit_url`，但这两个 URL 可以先指向 owner 模块已确定的后续 Console 承载路由；专题文档必须明确该阶段尚未实现前端创建/编辑 UI。Orchestrator 和 Monitor 不得仅凭 URL 存在推断 owner 模块已经提供可用 UI。

后续如需 `task.capabilities/v3`，必须先单独修订本文，再实现代码。v3 可以讨论 inline execution、更完整 JSON Schema、标准取消、capabilities 漂移详情和批量编排健康检查；不得在 v2 中通过私有字段、兼容分支或布尔开关提前打开这些能力。
