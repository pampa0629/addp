# ADDP 任务体系规范

> 状态：规范。本文定义 ADDP 全平台任务定义、执行记录、任务提供者、编排和监控的统一边界。模块内部任务语义可在各模块专题中继续细化，但不得违背本文。

## 范围

本文回答：

- 什么是任务定义。
- 什么是执行记录。
- `common.task_executions` 如何统一记录 execution。
- TaskProvider 如何声明一个模块可提供的任务类型。
- Orchestrator 如何引用和触发跨模块任务。
- Monitor 如何查询和展示任务执行。

本文不展开：

- Manager 内部 MVT、embedding、QuickView 的详细策略。
- Transfer 内部全量、增量、实时同步的详细语义。
- Develop 算子工作流内部节点模型。
- Meta scan 具体扫描范围和 detector 规则。

这些内容由对应模块或专题文档定义；本文只要求它们能纳入统一任务体系。

## 基本原则

1. ADDP 不建立统一任务总表。
2. `Task` 是抽象概念，不是跨模块实体表。
3. 任务定义归 owner 模块私有表。
4. 执行记录统一进入 `common.task_executions`。
5. 调度定义归任务定义 owner 模块。
6. 运行时队列只是执行机制，不是任务定义。
7. 产物状态归产物 owner 模块。
8. Orchestrator 只编排任务能力，不拥有业务任务定义。
9. Monitor 只聚合观察，不成为任务 owner。

## 核心对象

| 对象 | Owner | 存储 | 含义 |
| --- | --- | --- | --- |
| 任务定义 | 业务 owner 模块 | owner 模块私有表 | 未来应该按什么策略处理什么对象 |
| 执行记录 | Common | `common.task_executions` | 某一次实际执行了什么、状态如何、结果如何 |
| 调度定义 | 任务定义 owner | owner 模块私有表 | 是否启用定时、Cron 表达式、下一次运行时间 |
| 运行时队列 | 执行 owner | Redis / Asynq / DB claim / 进程内队列 | 如何把 execution 投递给 worker |
| 产物状态 | 产物 owner 模块 | owner 模块私有表或 artifact manifest | 当前产物是否可用、在哪里、由什么配置生成 |
| 编排定义 | Orchestrator | `orchestrator.orchestrations` | 任务级 DAG，引用已有任务或声明允许的一次性执行步骤 |

## 任务定义规范

任务定义表达可被再次执行的稳定配置。任务定义不得保存每次执行历史，只能保存最近执行摘要。

所有持久任务定义应具备以下公共字段语义：

| 字段 | 说明 |
| --- | --- |
| `id` | owner 模块内任务 ID |
| `tenant_id` | 租户 ID |
| `name` | 任务名称 |
| `description` | 任务描述 |
| `enabled` | 是否启用定时或自动触发 |
| `schedule` | Cron 表达式，空表示无定时 |
| `last_run_at` | 最近执行时间 |
| `next_run_at` | 下一次计划执行时间 |
| `last_execution_id` | 最近一次 `common.task_executions.execution_id` |
| `last_execution_status` | 最近一次执行状态 |
| `created_by` | 创建人 |
| `created_at` / `updated_at` / `deleted_at` | 审计字段 |

这些字段是语义基线，不要求抽取共享表或共享 Go struct。各模块可以增加模块私有字段，但不得改变以上字段含义。

## 执行记录规范

`common.task_executions` 只保存 execution，不保存任务定义和调度定义。

目标字段语义：

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `id` | bigint | 执行记录自增 ID |
| `execution_id` | string | UUID，全局唯一，跨模块追踪 |
| `tenant_id` | integer | 租户 ID |
| `module` | string | 执行 owner 模块 |
| `task_type` | string | owner 模块内稳定任务类型 |
| `source` | string | 本次执行由哪个模块或入口触发 |
| `source_task_id` | string | owner 模块任务定义 ID；一次性执行可为空 |
| `source_task_name` | string | 任务名称冗余，用于展示 |
| `parent_execution_id` | string | 父 execution UUID，主要用于编排子步骤 |
| `status` | string | 执行状态 |
| `progress` | integer | 0 到 100 |
| `current_step` | string | 当前步骤描述 |
| `trigger_type` | string | `manual` 或 `scheduled` |
| `triggered_by` | integer | 触发用户 |
| `execution_config` | jsonb | 本次执行快照配置 |
| `metadata` | jsonb | 结果摘要、步骤结果、模块扩展信息 |
| `error_details` | jsonb | 错误类型、消息和诊断信息 |
| `started_at` | timestamp | 开始时间 |
| `completed_at` | timestamp | 完成时间 |
| `execution_time_ms` | bigint | 执行耗时 |

约束：

1. `source_task_id` 是字符串软引用。数值型任务 ID 应按十进制字符串写入。
2. `source_task_id` 不建立跨 schema DB 外键。
3. 查询任务定义时必须按 `module + task_type + source_task_id` 路由到 owner 模块。
4. ad-hoc execution 可以没有 `source_task_id`，但必须在 `execution_config` 中保存本次执行所需的完整配置快照。
5. Orchestrator 子步骤 execution 必须写 `parent_execution_id`。
6. execution 完成后不得再被重用；重试必须创建新的 execution。

## 执行状态枚举

只允许以下状态：

| 状态 | 说明 |
| --- | --- |
| `pending` | 已创建，等待运行 |
| `running` | 正在运行 |
| `success` | 成功完成 |
| `failed` | 失败完成 |
| `timeout` | 超时结束 |
| `cancelled` | 被取消 |

不得使用 `completed` 表达执行成功。

## 触发类型和来源

`trigger_type` 只允许：

| 触发类型 | 说明 |
| --- | --- |
| `manual` | 用户、API、Console、Orchestrator 或其他模块显式触发一次执行 |
| `scheduled` | owner 模块调度器按计划触发 |

`source` 记录触发来源模块或入口，例如：

| source | 说明 |
| --- | --- |
| `console` | Console 触发 |
| `meta` | Meta 自身触发 |
| `manager` | Manager 触发 |
| `transfer` | Transfer 触发 |
| `develop` | Develop 触发 |
| `orchestrator` | Orchestrator 触发 |
| `system` | System 生命周期或内部动作触发 |

约束：

1. `orchestrator`、`api`、`retry`、`system_immediate` 不得进入 `trigger_type`。
2. 编排触发下游任务时，`source=orchestrator`，`trigger_type` 仍为 `manual` 或 `scheduled`。
3. 重试来源应进入 `metadata` 或后续专门重试关联字段，不得扩展 `trigger_type`。

## 模块任务类型

Common 不维护全量业务 `task_type` 编译期枚举。`task_type` 由 owner 模块通过 TaskProvider capabilities 声明。

平台内置模块第一阶段至少应能声明以下任务类型：

| 模块 | task_type | 任务定义 |
| --- | --- | --- |
| Meta | `scan` | `meta.scan_tasks` |
| Transfer | `import` | `transfer.transfer_tasks` |
| Develop | `query` / `workflow` / `script` | `develop.dev_tasks` |
| Manager | `mvt_generation` / `embedding` | `manager.mvt_tasks` / `manager.embedding_tasks` |
| Quality | `check` | `quality.check_tasks` |
| Graph | `kg_build` | `graph.build_tasks` |
| Orchestrator | `orchestration` | `orchestrator.orchestrations` |

System cleanup 阶段 1 不纳入 TaskProvider，也不进入 Orchestrator 编排。cleanup 属于系统级运维清理流程，不属于用户数据处理任务；后续若为了监控和审计接入 `common.task_executions`，也必须作为 system-owned execution 记录处理，不得声明为可编排业务任务，不得出现在 Orchestrator 的任务选择列表中。

Transfer 的内部任务语义由 Transfer 专题确认。阶段 1 先把接口层纳入统一任务体系，对外只声明 `task_type=import`，并通过 TaskProvider 和 `common.task_executions` 关联任务定义。后续如专题确认需要增加导出、同步等任务类型，必须先修订本文，再按 clean break 方式迁移，不在同一阶段并行保留 `import`、`export`、`sync`、`transfer` 多套语义。

Manager 的 MVT、embedding、QuickView 细节由 Manager 专题确认。本文只要求 Manager 用同一个 provider 声明多个任务类型，并按 `module + task_type + source_task_id` 关联执行记录。持久化 embedding 任务执行必须复用任务服务创建的主 execution；ad-hoc embedding 可以自行创建 execution，但不得产生 owner 任务定义，且没有 `source_task_id` 时必须写完整 `execution_config`。

Manager 中 QuickView / MVT 的 `ready`、`generating`、`preparing` 等状态属于 artifact state，不是统一 execution status。Manager 的即时向量化内存轮询状态虽不持久化为任务定义，但属于 execution-like 状态，成功态也必须使用 `success`，不得使用 `completed`。

Graph 的 `kg_build` 任务定义由 `graph.build_tasks` 保存。`graph.build_tasks.status` 是构建任务最近执行摘要，必须使用统一 execution status；成功态为 `success`。`graph.build_materials.status=completed` 属于材料处理状态，不是任务执行状态，不进入 TaskProvider 和 Monitor 的统一 execution 枚举。

Develop 的任务类型按开发方式划分为 `query`、`workflow`、`script`。`script` 表示命令式代码开发任务，当前可由 Jupyter Notebook runtime 承载；`notebook` 只是脚本开发的实现形态和 UI 入口，不作为独立 `task_type` 声明，不进入 TaskProvider capabilities。

Develop 的 `develop.dev_tasks.content` 必须使用规范字段：`query` 使用 `content.query` 和 `content.query_type`，引擎绑定写入 `execution_config.engine_id`；`workflow` 使用 `content.workflow_definition` 和可选 `content.inputs`；`script` 的 Notebook 形态使用 `content.notebook_path`。不得再新增或消费 `content.sql`、`content.workflow_def`、`content.input_data` 等旧字段。

Develop 的 `create_url` / `edit_url` 必须指向具体开发方式的专属页面：`query` 指向查询工作台，`workflow` 指向工作流编辑器，`script` 指向脚本开发当前承载页面。`/develop/tasks` 只是任务定义集散页和列表页，不得作为 TaskProvider 的创建或编辑目标。

## TaskProvider 规范

TaskProvider 是模块的一种角色，不是独立业务 owner。System 保存 provider 注册信息，供 Orchestrator 和 Monitor 发现模块任务能力。

TaskProvider 按模块注册，不按任务类型注册。一个模块只有一个 provider，但 provider 可以声明多个 `task_types`。

### Provider 基本字段

| 字段 | 说明 |
| --- | --- |
| `module_name` | 模块名，例如 `manager` |
| `display_name` | 展示名称 |
| `description` | 描述 |
| `base_url` | 模块 API 基础地址 |
| `task_list_endpoint` | 任务列表 endpoint |
| `task_detail_endpoint` | 任务详情 endpoint |
| `task_execute_endpoint` | 任务执行 endpoint |
| `task_status_endpoint` | execution 状态 endpoint |
| `task_cancel_endpoint` | execution 取消 endpoint |
| `capabilities` | provider 能力声明 |
| `is_enabled` | provider 是否启用 |

### 标准 endpoint

第一阶段应收敛到以下 endpoint 语义：

| 能力 | Endpoint |
| --- | --- |
| 列任务 | `GET /tasks?task_type=` |
| 查任务 | `GET /tasks/{task_type}/{id}` |
| 执行任务 | `POST /tasks/{task_type}/{id}/execute` |
| 查执行 | `GET /executions/{execution_id}` |
| 取消执行 | `POST /executions/{execution_id}/cancel`，仅当对应 `task_type.supports_cancel=true` |

后续实现应收敛到以上 endpoint 语义。取消接口不是必选能力；未声明支持取消的任务类型不得注册或展示取消入口。确需处理现有路径时，只能作为入口层迁移工作，不得形成长期双轨命名。

System 注册 TaskProvider 时必须校验标准 endpoint：任务详情和执行 endpoint 必须包含 `{task_type}` 与 `{id}`，执行状态 endpoint 必须包含 `{execution_id}`，且不得使用 `/provider/tasks` 旧路径。Orchestrator 调用 provider 时只替换 `{task_type}`、`{id}`、`{execution_id}` 三类标准占位符；模块私有 UI 或 CRUD 路径可以继续使用 `:id`、`:task_id` 等前端或 Gin 写法，但不得进入 TaskProvider endpoint 契约。

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
3. `parameters` 是本次执行覆盖或模板化参数，不得直接改写任务定义。
4. provider 不支持参数覆盖时必须明确拒绝，不得静默忽略。

执行响应必须返回本次执行的统一 `execution_id`。标准最小响应为：

```json
{
  "execution_id": "uuid",
  "status": "running"
}
```

如果模块使用统一响应包装，`data.execution_id` 必须指向同一个 unified execution，顶层仍建议保留 `execution_id`，便于 Orchestrator 直接追踪。

### 多任务类型 provider

多任务类型模块通过 `task_type` 分派到 owner 模块内部不同任务表和服务。

以 Manager 为例：

| provider | task_type | 任务定义表 | 执行 owner |
| --- | --- | --- | --- |
| `manager` | `mvt_generation` | `manager.mvt_tasks` | Manager |
| `manager` | `embedding` | `manager.embedding_tasks` | Manager |

约束：

1. `task_id` 只在 `provider + task_type` 命名空间内唯一。
2. Orchestrator Step 必须保存 `provider`、`task_type` 和 `task_id` 三元组。
3. Monitor 展示任务详情时必须按 `module + task_type + source_task_id` 回查 owner provider。
4. 不得为了多任务类型把一个模块拆成多个 provider，例如 `manager_mvt`、`manager_embedding`。

### capabilities.task_types

TaskProvider 注册时必须使用 `task.capabilities/v1` schema，并声明稳定的 `task_types[]`：

```json
{
  "schema_version": "task.capabilities/v1",
  "task_types": [
    {
      "type": "scan",
      "display_name": "扫描任务",
      "description": "执行元数据扫描",
      "definition_schema": { "type": "object" },
      "execution_schema": { "type": "object" },
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
| `type` | 稳定任务类型，例如 `mvt_generation` |
| `display_name` | 展示名称 |
| `description` | 任务类型说明 |
| `definition_schema` | 任务定义 schema，用于创建或编辑持久任务 |
| `execution_schema` | 执行参数 schema，用于本次执行参数覆盖或 inline execution |
| `supports_schedule` | 该任务类型是否支持定时 |
| `supports_cancel` | 该任务类型是否支持真实取消；不能中断执行体时必须为 `false` |
| `supports_inline_execution` | 是否支持无持久任务定义的一次性执行 |
| `create_url` / `edit_url` | owner 模块前端入口；优先使用 Console 模块路由，例如 `/develop/sql?action=create` |
| `deprecated` | 是否废弃 |

约束：

1. `schema_version` 必须为 `task.capabilities/v1`。
2. `create_url` 和 `edit_url` 属于具体 `task_type`，不得放在 provider 顶层。
3. System 注册入口必须校验 capabilities schema，不符合规范的 provider 不得注册成功。
4. `task_type` 是 provider 对外契约，不能随 UI 文案变化。
5. Orchestrator 可以缓存 TaskProvider capabilities，但必须能从 System 刷新。
6. `create_url` / `edit_url` 应使用 Console 路由形式，可包含模块内深层路径和 query，例如 `/transfer/tasks/:id/edit`、`/develop/workflow?action=edit&id=:id`、`/graph/graphs/:graph_id/build/tasks/:id`；前端负责替换 `:id` / `{id}` / `:task_id` / `{task_id}` / `:graph_id` / `{graph_id}`。
7. 模块新增或删除任务类型时，必须更新自身 capabilities、文档和 Swagger。
8. 历史 execution 中已经出现的 `task_type` 不应被 owner 模块静默移除；如任务类型废弃，应标记 `deprecated=true`，直到历史查询和迁移完成。

## Orchestrator 规范

Orchestrator 拥有编排定义，不拥有业务任务定义。

Orchestrator Step 支持两类模式：

| 模式 | 是否持久化 owner 任务 | 说明 |
| --- | --- | --- |
| 任务引用 | 是，已由 owner 模块创建 | `provider + task_type + task_id` |
| 内联执行 | 否 | provider 明确声明支持一次性 execution spec，Orchestrator 只传执行配置 |

约束：

1. 默认不支持 Orchestrator 创建 owner 模块持久任务。
2. 用户需要新建持久任务时，应跳转到 owner 模块声明的 `create_url`。
3. 编排触发下游任务时，必须传 `source=orchestrator`。
4. 编排触发下游任务时，`trigger_type` 只能是 `manual` 或 `scheduled`。
5. Orchestration 自身执行记录 `module=orchestrator`、`task_type=orchestration`。
6. Orchestrator 子步骤 execution 必须写 `parent_execution_id`。
7. Orchestrator 可以把已保存的编排定义作为 `orchestration` 任务暴露给任务库，但保存编排时必须校验编排定义之间的引用图，不得直接引用自身，也不得通过多层 `orchestrator/orchestration` 引用形成递归执行。
8. Monitor 回跳任务定义时应使用 TaskProvider capabilities 中对应 `task_type.edit_url`，不得硬编码 `module + task_type` 映射。

## 调度规范

调度定义归任务 owner 模块。

| 对象 | 责任 |
| --- | --- |
| owner task | 保存 `schedule`、`enabled`、`next_run_at`、`last_run_at` |
| owner scheduler | claim due task、创建 execution、投递 worker |
| common scheduler | 提供 Cron 解析和进程内调度工具 |
| common execution | 记录调度触发产生的一次 execution |

长期应以 DB-driven due task claim 为主。进程内 Cron 只作为触发器或辅助工具，避免多实例、重启恢复、漏跑补偿和调度审计问题。

## Monitor 规范

Monitor 不拥有任务定义。Monitor 聚合观察：

| 层级 | 来源 | 监控内容 |
| --- | --- | --- |
| execution | `common.task_executions` | 状态、耗时、进度、错误、父子关系 |
| schedule | owner 任务状态 API | 是否启用、下一次运行、最近运行摘要 |
| runtime queue | owner 模块队列状态 API | pending、active、retry、dead、延迟 |
| artifact state | owner 模块状态 API | 产物是否 ready、缓存位置、版本、失败原因 |
| provider health | System TaskProvider / module health | 模块是否可调用 |

Monitor 可以查询 owner 模块公开的只读状态 API，但不得直接依赖 owner 私有表结构。

Monitor 必须支持按执行记录查询 parent / child execution 树。树边使用 `common.task_executions.execution_id` 与 `parent_execution_id` 连接。Monitor 只读取 `common.task_executions`，不得回查 owner 私有表拼装树。

入口包括：

| 入口 | 用途 |
| --- | --- |
| `GET /api/v1/monitor/executions/{id}/tree` | 按统一执行记录自增 `id` 查询，适合 Monitor 表格内部跳转 |
| `GET /api/v1/monitor/executions/by-execution-id/{execution_id}/tree` | 按全局 UUID 查询，适合各模块拿到 `execution_id` 后跳转统一监控 |

前端模块拿到执行响应中的 `execution_id` 后，应使用 `common-frontend` 的 `openMonitorExecution(execution_id)` 进入统一监控页。该工具会优先通过 Console iframe bridge 切换父级路由到 `/monitor/executions?execution_id=...`，独立运行时再回退为打开 Console 路由。业务模块不得自行硬编码 Console 端口或拼装跨模块 iframe URL。

## 模块接入检查清单

模块要纳入任务体系，至少满足：

- 有明确任务定义 owner。
- 持久任务定义表只保存定义和最近执行摘要。
- 每次执行写入 `common.task_executions`。
- `trigger_type` 只写 `manual` / `scheduled`。
- `source` 写触发来源。
- `task_type` 稳定并声明到 TaskProvider capabilities。
- 多任务类型模块使用一个 provider 和多个 `task_types`。
- execution 能按 `module + task_type + source_task_id` 回查任务定义。
- ad-hoc execution 保存完整 `execution_config`。
- Swagger 和模块文档同步。

## 与相关文档的关系

- Meta 扫描任务细节见 [元数据扫描机制规范](addp元数据扫描机制规范.md)。
- 扫描后派生产物任务边界见 [扫描后派生能力与任务边界](../next/扫描后派生能力与任务边界.md)。
- Transfer 内部任务语义后续以 Transfer 专题为准。
- Manager 派生产物任务内部语义后续以 Manager 专题为准。
