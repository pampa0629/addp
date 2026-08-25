# ADDP 任务体系规范

> 状态：规范。本文定义 ADDP 全平台任务定义、执行记录、任务提供者、编排和监控的统一边界。模块内部任务语义可在各模块专题中继续细化，但不得违背本文。

## 范围

本文回答：

- 什么是任务定义。
- 什么是执行记录。
- 执行结果如何携带统一的 `lineage_facts`。
- `common.task_executions` 如何统一记录 execution。
- TaskProvider 如何声明一个模块可提供的任务类型。
- Orchestrator 如何引用和触发跨模块任务。
- Monitor 如何查询和展示任务执行。

本文不展开：

- Manager 内部瓦片缓存生成、embedding、预览状态和快显结果的详细策略。
- Transfer 各引擎方言和 continuous runtime 的实现细节。
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
10. ad-hoc-only execution type 可以写入统一执行记录，但在没有持久任务定义前不得声明为 TaskProvider 能力或进入 Orchestrator 任务选择。
11. 真实读写 owner 必须在 execution 结果中写入版本化 `lineage_facts`；Meta 负责消费并维护血缘关系，Orchestrator 不重复生成资源血缘。
12. Quality `check`、Meta `scan` 和 Transfer bounded `sync` 的 execution worker 必须是 owner 模块附属的独立进程，并统一使用 PostgreSQL execution claim + lease；owner Backend 不执行这些 bounded execution。
13. owner scheduler 运行在 owner Backend，只负责按任务定义发现到期任务并创建 durable `pending` execution；Worker 不可用不得阻止 scheduler 创建 execution。dispatcher 只负责 outbox/delivery 投递，二者都不得替代 execution worker 成为业务执行事实源。
14. bounded runtime queue 的唯一主路线是 `common.task_executions` PostgreSQL claim，不保留 Redis/Asynq、进程内 channel 或 Backend 内嵌执行 fallback。continuous runtime、dispatcher 和 maintenance loop 继续使用各自专用协议，不强行迁入 bounded claim。

## 核心对象

| 对象 | Owner | 存储 | 含义 |
| --- | --- | --- | --- |
| 任务定义 | 业务 owner 模块 | owner 模块私有表 | 未来应该按什么策略处理什么对象 |
| 执行记录 | Common | `common.task_executions` | 某一次实际执行了什么、状态如何、结果如何 |
| 调度定义 | 任务定义 owner | owner 模块私有表 | 是否启用定时、Cron 表达式、下一次运行时间 |
| 运行时队列 | 执行 owner | bounded 使用 PostgreSQL claim；continuous 使用 runtime lease | 如何把 execution 交给合法运行者 |
| 产物状态 | 产物 owner 模块 | owner 模块私有表或 artifact manifest | 当前产物是否可用、在哪里、由什么配置生成 |
| 编排定义 | Orchestrator | `orchestrator.orchestrations` | 任务级 DAG，当前只引用已有任务定义；inline execution 后续专题再设计 |

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
| `authorization_principal_id` | 任务授权主体 User Principal；只用于定时或延迟执行，不是任务所有者字段 |
| `authorization_membership_id` | 任务授权主体的 Tenant Membership |
| `authorization_version` | 任务被当前 User 授权时的 IAM 授权版本 |
| `authorized_at` | 当前任务定义完成授权的时间 |
| `created_at` / `updated_at` / `deleted_at` | 审计字段 |

这些字段是语义基线，不要求抽取共享表或共享 Go struct。各模块可以增加模块私有字段，但不得改变以上字段含义。

### 任务授权主体

可被 owner scheduler 或 Orchestrator 定时执行的持久任务必须保存 Task Authorization Subject。该事实固定由 `authorization_principal_id + authorization_membership_id + authorization_version + authorized_at` 构成，并满足：

1. 只能从同 Tenant 的当前 User AuthContext 写入；API 不接收客户端自报的 Principal、Membership 或授权版本。
2. 创建、修改任务执行语义、步骤、参数或调度配置时，必须用本次请求的 User AuthContext 原子替换任务授权主体；不能保留修改前的高权限主体。
3. Service Principal、平台三员和缺少 Tenant Membership 的主体不能成为任务授权主体。
4. 定时 execution 创建前必须重新校验 Principal、Membership、Tenant、Role Permission 和授权版本。任一事实失效时本次触发失败并写入可审计错误，不得改用 Service Principal 数据权限继续执行。
5. Task Authorization Subject 不是 Access Token、Role Assignment 或第二套 Membership；任务表、execution、日志和审计均不得保存 User Token 或 Service Token。
6. 手动执行以本次请求的当前 User 为执行授权主体，不借用任务定义中保存的主体；父 execution 固化该主体事实，子 execution 只能沿可验证的 `parent_execution_id` 来源链继承。

### 任务语义身份与重复执行

持久任务定义的 owner 模块必须明确该 `task_type` 的任务语义身份，不得仅依赖自增 `id`、最近 execution 或输出路径判断是否重复。

1. 语义身份必须由规范化后的业务事实构成。对“一个源、一种派生变体只维护一个当前结果”的任务，默认形式为 `tenant_id + stable_source_identity + artifact_variant`。
2. data item 的稳定源身份优先使用 `item_fingerprint`。该指纹已由 `engine_id + full_name` 计算；`engine_id + item_id` 不是跨扫描稳定身份，`locator`、`item_id` 只作当前定位和回查事实。
3. `item_fingerprint` 不表达内容版本。源内容变化必须通过 `source_version`、`content_hash`、`data_updated_at`、`last_modified_at` 或格式专用事实判断，并把旧结果标记为过期。
4. 对已有语义身份的重复创建请求，owner 模块必须幂等复用原任务 ID，并更新名称、描述、规范化配置等可变字段；不得新建重复任务，也不得把“重复任务”作为用户错误。
5. owner 模块必须使用数据库唯一约束或部分唯一索引作为并发防线。如先查后插发生唯一冲突，必须回查并复用已存在任务。
6. 重复执行不复用 execution。每次执行都新建 `common.task_executions` 记录，并刷新同一个当前结果；这就是受管派生任务的更新语义，不另建“更新任务”类型。
7. 同一任务已有 `pending` 或 `running` execution 时，owner 模块必须拒绝并发重复执行；已结束 execution 不阻止后续再次执行。
8. 允许多个任务并存的业务派生场景，语义身份必须包含完整源范围、目标存储、目标名称、placement 和关键生成配置。只有规范化语义真正不同时才能并存，不得用不同任务名称制造重复定义。
9. owner 模块拥有当前结果生命周期、且重复执行会覆盖该结果或其物理产物时，服务端必须要求本次 execution 显式提交 `parameters.existing_result_action=overwrite`。未声明动作时返回 HTTP 409 和稳定错误码 `existing_result_action_required`，且不得创建 execution、重置结果状态或清理物理产物。当前结果存在检查、active execution 检查和 execution 创建必须位于同一个任务定义行锁事务中。
10. `existing_result_action` 是执行动作，不是 UI 确认状态，也不得由服务端根据 `trigger_type` 或 `source` 自动补充。人工执行由前端在收到 409 后展示二次确认，确认后重试并提交 `overwrite`；Orchestrator 可以把 `overwrite` 保存为 Step 参数，并在手动或定时 Pipeline 的每次下游调用中原样提交。任务自身调度若后续开放，保存或启用 schedule 时必须让用户明确知晓会自动刷新当前结果，并由 owner scheduler 在每次 execution 请求中显式提交同一动作。

删除边界保持分离：删除任务定义不默认删除当前结果或 execution 历史；删除当前结果也不删除任务定义。产物 owner 必须在本模块生命周期规范中明确物理对象清理顺序。

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
| `trigger_type` | string | `manual`、`scheduled` 或 `event` |
| `triggered_by` | integer | 触发用户 |
| `execution_config` | jsonb | 本次执行快照配置 |
| `metadata` | jsonb | 结果摘要、步骤结果、模块扩展信息 |
| `error_details` | jsonb | 错误类型、消息和诊断信息 |
| `lease_token` | UUID | 当前 bounded attempt 不可复用的所有权 token；不对普通业务 API 暴露 |
| `lease_owner` | string | 当前 Worker 实例和槽位的观测身份，不作为唯一 fencing 条件 |
| `lease_expires_at` | timestamp | 当前 bounded attempt 的租约截止时间 |
| `attempt` | integer | 当前 execution 已发生的运行尝试次数 |
| `max_attempts` | integer | 该 execution 允许自动恢复的最大 attempt 数 |
| `started_at` | timestamp | 开始时间 |
| `completed_at` | timestamp | 完成时间 |
| `execution_time_ms` | bigint | 执行耗时 |

### `lineage_facts` 执行结果契约

当一次 execution 实际读取、写入或发布了可治理的数据资源时，真实读写 owner 必须在 `metadata` 的结果结构中写入 `lineage_facts`。该字段使用版本化结构，统一记录 `inputs`、`outputs`、`operations`、运行时执行引用和 Meta scan 引用；资源身份使用标准 ResourceLocator，并在可用时携带 item fingerprint。完整字段和关系语义见 [数据血缘能力规范](addp数据血缘能力规范.md) 中的“统一执行事实”。

约束如下：

1. Runtime 只返回节点、端口和运行摘要，不构造 ADDP locator、Meta item 或 Asset 身份。
2. owner 负责把实际资源绑定、`produced_targets`、写入模式和成功提交事实写入 `lineage_facts`；失败或取消不能伪造成功关系。
3. Meta collector 以 `lineage_facts` 为输入，解析并保存关系证据和当前投影；它不解析各模块私有 metadata，也不反向抓取 Runtime 状态。
4. Orchestrator 只通过 `parent_execution_id` 提供父子执行上下文，不重复生成 data item 之间的关系。
5. `lineage_facts` 不得包含连接凭据、Token、临时挂载路径或完整大对象。

### 血缘采集通知

当 owner 成功完成 execution 结果持久化后，应立即通知 Meta 采集该 execution 的血缘事实：

```http
POST /api/v1/meta/lineage/executions/{execution_id}/collect
```

该接口由 Meta 提供，仅接受对应 owner 的 Service Principal；Develop 使用 `addp-develop`，并要求 `meta.lineage.create` Permission。通知必须发生在成功状态和 `lineage_facts` 已写入 `common.task_executions` 之后，不能在事实落库前调用。

立即通知与 Meta 周期 collector 必须共同调用同一个 `LineageService.CollectExecution`，不得复制解析或投影逻辑。通知失败不得把已成功 execution 改为失败，周期 collector 负责漏采和失败重试。Item 元数据 refresh 不触发血缘采集。

约束：

1. `source_task_id` 是字符串软引用。数值型任务 ID 应按十进制字符串写入。
2. `source_task_id` 不建立跨 schema DB 外键。
3. 查询任务定义时必须按 `module + task_type + source_task_id` 路由到 owner 模块。
4. ad-hoc execution 可以没有 `source_task_id`，但必须在 `execution_config` 中保存本次执行所需的完整配置快照。
5. Orchestrator 子步骤 execution 必须写 `parent_execution_id`。
6. execution 完成后不得再被重用；重试必须创建新的 execution。
7. `pending` 只表示 execution 已创建并等待 worker、runner 或 runtime claim，`started_at` 必须为空；`created_at` 是记录创建时间，不能冒充执行开始时间。
8. execution 真正被执行方领取并开始工作时，必须原子完成 `pending → running` 并写入真实 `started_at`。重复的进度更新不得覆盖 `started_at`。
9. `success|failed|timeout|cancelled` 终态必须写 `completed_at`；已经进入过 `running` 的 execution 同时必须按 `completed_at - started_at` 写 `execution_time_ms`。失败和超时必须写安全的 `error_details`。
10. owner 若在执行前排队、跨进程投递或等待 lease，排队时长必须由 `created_at → started_at` 表达，不能提前写 `started_at` 来隐藏排队时间。
11. owner 任务定义上的最近执行摘要或运行状态若随 execution 状态推进，必须与对应 execution 的 claim、`pending → running` 和终态更新分别位于同一数据库事务；不得先推进公共 execution、再单独更新 owner 表，或反向操作。
12. 对已有任务定义发起 execution 时，owner 必须在任务定义行锁保护下检查 active execution，并在同一事务创建唯一 pending execution。重跑还必须在该事务中完成运行材料、待处理结果和任务摘要的重置，不能先重置再进入第二次 claim。
13. 取消操作只有在 owner 能定位并中断真实运行体、等待其停止并由运行体写入 `cancelled` 终态时才能成功；仅修改任务或 execution 状态属于伪取消，必须拒绝。

### Bounded execution 领取、租约和恢复契约

Quality `check`、Meta `scan` 和 Transfer `runtime.boundary=bounded` 必须遵守同一个公共所有权协议：

1. Worker 使用 PostgreSQL `FOR UPDATE SKIP LOCKED` 从 `common.task_executions` 领取本模块、task type 和满足本模块授权前置条件的最早 `pending` execution。
2. claim 必须原子完成 `pending → running`、首次写入 `started_at`、递增 `attempt`、生成新的随机 `lease_token` 并写入 `lease_owner + lease_expires_at`。`lease_owner` 只用于观测，不能替代 token。
3. 未取得 claim 的运行体必须 fail-closed；不得执行外部读取、写入、扫描、结果 reconcile 或进度更新。
4. heartbeat、进度和终态写入必须同时匹配 `execution_id + status=running + attempt + lease_token`。租约过期、被新 attempt 接管或 token 不匹配的旧运行体必须停止，且任何迟到写回都必须被拒绝。
5. owner task 最近执行摘要若随 claim、恢复或终态推进，owner 必须在同一个数据库事务中组合公共 execution 条件更新和 owner 私有表更新。公共仓储不反向理解 owner 表。
6. lease 过期只表示当前 attempt 已失去所有权，不自动证明业务可以安全重放。owner 必须按 execution config 和实际提交协议明确本次 execution 是否允许自动恢复；默认不可安全重放。
7. 只有在尚未产生不可逆外部提交，或者 Provider 使用稳定 operation identity、staging、target ledger、CAS/fencing 等协议保证重复尝试可安全吸收时，recovery 才能把同一未终态 execution 返回 `pending` 并由下一次 claim 增加 attempt。
8. 无法证明安全重放、达到 `max_attempts` 或发现外部提交状态不明确时，recovery 必须把原 execution 收敛为带稳定错误原因的 `failed`，不得盲目再次执行。
9. 自动 recovery 复用的是同一未终态 execution；用户或 API 发起 retry 必须创建新的 execution，并显式保存原 execution 关联，不能把 retry 降格为同一 execution 的下一次 attempt。
10. Worker 优雅停机先停止 claim 新 execution，再取消或收敛当前运行体；不能通过清空 lease 冒充业务已经停止。

公共 `common/execution` 只提供 claim、lease token、heartbeat、带所有权条件的更新和过期领取等通用原语。具体 execution 是否可恢复、owner task 摘要事务、外部副作用幂等和提交边界归 owner/Provider 实现；公共层不承诺跨系统 exactly-once。

ad-hoc execution 的 `task_type` 仍必须是 owner 模块内稳定的业务执行类型，但稳定 execution type 不等于 TaskProvider task type。只有 owner 已提供可保存的任务定义、标准任务列表 / 详情 / 执行接口并允许 Orchestrator 引用时，才能把该类型加入 `task_capabilities[]`。

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
| `event` | owner 或平台生命周期事件触发；当前用于 System cleanup 等事件驱动运维执行 |

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
4. `event` 只表达真实事件触发，不得用来代替 `source`，也不得把普通 API 调用、重试或即时执行伪装成事件。

## 模块任务类型

Common 不维护全量业务 `task_type` 编译期枚举。稳定 execution type 由 owner 模块定义；只有具备持久任务定义并允许跨模块发现、引用和执行的任务类型，才由 owner 模块通过 TaskProvider capabilities 声明。

平台内置模块第一阶段至少应能声明以下任务类型：

| 模块 | task_type | 任务定义 |
| --- | --- | --- |
| Meta | `scan` | `meta.scan_tasks` |
| Transfer | `sync` | `transfer.transfer_tasks` |
| Develop | `query` / `workflow` / `script` | `develop.dev_tasks` |
| Manager | `vector_tile_cache_generation` / `vector_tile_set_generation` / `vector_materialized_view_generation` / `embedding` / `raster_cog_generation` / `raster_mosaic_generation` / `model_3d_glb_generation` / `model3d_tiles_generation` / `gaussian_splat_ksplat_generation` / `point_cloud_copc_generation` / `cad_preview_generation` | `manager.vector_tile_cache_tasks` / `manager.vector_tile_set_tasks` / `manager.vector_materialized_view_tasks` / `manager.embedding_tasks` / `manager.raster_cog_tasks` / `manager.raster_mosaic_tasks` / `manager.model_3d_glb_tasks` / `manager.model3d_tiles_tasks` / `manager.gaussian_splat_ksplat_tasks` / `manager.point_cloud_copc_tasks` / `manager.cad_preview_tasks` |
| Quality | `check` | `quality.check_tasks` |
| Model | `materialization_prepare` / `materialization_publish` | 已审批 `model.logical_tables`（来源驱动、不可变任务定义） |
| Graph | `kg_build` | `graph.build_tasks` |
| Orchestrator | `orchestration` | `orchestrator.orchestrations` |

Manager 的 `cad_preview_generation` 只接受 `data_type=cad + format=dwg|dxf + layout=single` 源 item；结果登记到 `manager.cad_previews`，manifest、thumbnail 和 WebP 瓦片存放于 Manager infra MinIO，不自动升格为业务 data item。

Manager 表格数据剖析首期使用 `task_type=data_profiling` 的 ad-hoc execution，结果归 Manager 私有结果表，`source_task_id` 为空。首期不存在 `manager.data_profile_tasks`，因此 `data_profiling` 不加入上表的 TaskProvider 任务类型，不进入 Orchestrator 任务选择。未来只有在用户可以显式保存可重复剖析配置后，才能先更新数据剖析规范和本规范，再新增持久任务定义并声明 TaskProvider capability。`sample` / `full` 是 execution config 中的剖析模式，不得拆成两个 task type。

System 资源回收（cleanup）不纳入 TaskProvider，也不进入 Orchestrator 编排。cleanup 属于系统级运维资源回收流程，不属于用户数据处理任务；但 cleanup 必须进入 `common.task_executions` 和 System 审计体系。System 创建 `module=system`、`task_type=cleanup` 的父 execution，各模块资源回收执行方创建 `task_type=cleanup_executor` 的子 execution，并通过 `parent_execution_id` 关联。cleanup 不得声明为可编排业务任务，不得出现在 Orchestrator 的任务选择列表中。

Transfer 的内部任务语义统一收敛为同步执行。阶段 1 对外只声明 `task_type=sync`，并通过 TaskProvider 和 `common.task_executions` 关联任务定义。Manager 的导入 / 导出入口通过 client 创建并触发 Transfer `sync`，不得在 Transfer 侧并行保留 `import`、`export`、`transfer` 等旧任务类型。

Model 的 `materialization_prepare` 与 `materialization_publish` 以已审批 LogicalTable 作为来源驱动任务定义，task ID 均为 LogicalTable ID。两者都不接受动态 `batch_id` 作为必填执行参数：prepare 创建批次；publish 按当前 execution 的 Tenant、Actor、`parent_execution_id` 和 LogicalTable ID 解析唯一 prepared 批次。Orchestrator 编排中的 Develop、Quality 和 publish 必须通过同一父 execution 血缘解析批次，不得用空默认值绕过 `execution_contract`，不得把 Schema、表名或 DDL 作为 Step 参数。prepare 可以在稳定 execution outputs 中返回 `batch_id` 供审计和诊断，但下游业务步骤不能据此绕过 Model 的执行域校验。

Transfer `sync` 的稳定语义由以下正交维度表达：

| 维度 | 字段 | 稳定取值 |
| --- | --- | --- |
| 执行边界 | `config.runtime.boundary` | `bounded` / `continuous` |
| 装载方式 | `config.load.mode` | `snapshot` / `incremental` |
| 变化识别 | `config.load.change_detection.type` | `watermark` / `manifest` / `kafka` / `cdc`（按已实现能力开放） |
| 目标应用 | `config.target.policy.apply_mode` | `replace` / `append` / `upsert` / `upsert_delete`（按目标 Provider 能力开放） |

约束：

1. `manual` / `scheduled` 仍只表示 execution 触发方式；`realtime`、`stream`、`micro-batch` 不得进入 `trigger_type` 或伪装成已实现的 Transfer 执行能力。
2. bounded execution 必须具有冻结上界；continuous execution 必须由专用长驻 runtime 承担，不能把无限循环塞入一次性队列任务。
3. watermark 增量必须使用稳定复合游标 `(watermark_field, tie_breaker...)`，读取区间为 `(committed_position, execution_upper_bound]`；普通 watermark 只保证 insert/update，不支持物理删除，不得称为完整 CDC。
4. 增量 committed position 归 Transfer 私有同步状态，不能写入任务定义或用 execution checkpoint 代替。只有目标批次成功提交后才能通过 CAS/fencing 推进同步状态。
5. 同一增量任务默认只允许一个 active execution 推进主状态；任务 claim 与 pending execution 创建必须在同一数据库事务中完成。
6. resume 创建新 execution 并从 committed position 继续；replay 是独立执行参数且永不推进主状态。PostgreSQL/MySQL watermark 只支持 resume，不支持 replay。
7. TaskProvider capability 只能声明已真实实现并验证的边界；不具备真实 worker 中断、资源释放和一致落库能力时必须 `supports_cancel=false`，也不得保留只改数据库状态的伪取消入口。

#### Transfer continuous sync v1 契约

continuous execution 表示一次长期运行的 runtime session，不是把 bounded executor 放进无限循环。正式实现必须使用 Transfer 独立 continuous worker/supervisor；`transfer-bounded-worker` 只通过 PostgreSQL claim 承担 bounded execution。

业务 Kafka 第一版任务配置固定为以下单一路线：

```json
{
  "runtime": {"boundary": "continuous", "record_failure": {"mode": "block"}},
  "load": {
    "mode": "incremental",
    "change_detection": {"type": "kafka"}
  },
  "source": {
    "locator": "addp://engine/30/path/orders.events?type=topic",
    "representation": "native",
    "change_stream": {
      "envelope": "record",
      "encoding": "json",
      "key": {"source": "value", "fields": ["id"]},
      "start": {"mode": "committed", "initial": "earliest"},
      "poll_batch_size": 1000
    }
  },
  "target": {
    "parent_locator": "addp://engine/8/path/public?type=schema",
    "name": "orders",
    "data_type": "table",
    "representation": "native",
    "policy": {"apply_mode": "upsert", "keys": ["id"]}
  },
  "transforms": [
    {
      "type": "field_mapping",
      "version": "v1",
      "mode": "project",
      "fields": [
        {"source": "id", "target": "id", "target_type": "int", "nullable": false}
      ]
    }
  ]
}
```

第一版约束：

1. source 只支持用户在 System 注册的业务 Kafka Engine，locator 必须定位 `type=topic`；Infra Kafka 不进入公开任务配置。
2. `envelope` 只允许 `record`，`encoding` 只允许 `json`，value 必须是 JSON object；tombstone、数组、标量、Avro、Protobuf、Schema Registry 和 Debezium envelope 均不支持。
3. 稳定 key 只允许从 JSON value 的显式非空字段提取；`key.source` 固定为 `value`，`key.fields` 非空。Kafka 原生 record key 第一版只保留为诊断事实，不参与目标身份推导。
4. 同一业务 key 的事件必须由生产者稳定写入同一 partition。Transfer 保证 partition 内顺序，不承诺跨 partition 的同 key 全局顺序。
5. 目标支持声明完整原子应用能力的 PostgreSQL/MySQL native table，并且必须使用 `apply_mode=upsert`、显式目标 keys 和 `PartitionedTableChangeApplyProvider`；普通 `TableUpsertProvider` 不能单独满足 continuous fencing。keyless append 和可能产生重复的普通 append 不进入第一版。
6. Kafka JSON 没有可依赖的表 schema，任务必须通过显式 `field_mapping` 固定目标字段和类型；source key fields 映射后的目标字段必须与 `target.policy.keys` 一一对应且非空。未知字段、缺失必填字段或不兼容 schema 变化必须阻塞 partition 并使当前 execution 失败，不得静默丢弃。
7. `start.mode` 固定为 `committed`。没有 committed state 的 partition 必须由任务显式选择 `initial=earliest|latest`；不得由 consumer 默认值或 auto commit 猜测。
8. Transfer 为每个 partition 在 `transfer.sync_states` 保存 `position.type=kafka_offset`、`position.version=v1` 和 `next_offset`。目标批次成功提交后才允许用 runtime fencing token 和 state version 做 CAS 推进；worker 恢复时必须 seek 到 `next_offset`。
9. 每个 continuous task 必须有服务端生成且不可修改的 `apply_identity` UUID，不写入 config。PostgreSQL Provider 在业务目标库维护 `addp_transfer.apply_positions`，MySQL Provider 在目标业务数据库维护 `_addp_transfer_apply_positions` InnoDB 私有表；两者都以 `apply_identity + source_identity + target_identity + partition` 校验应用主线，并把业务行 upsert 与目标 `next_offset` 在同一事务提交。
10. Transfer 必须把 poll 结果按 partition 拆成顺序批次，每条已映射记录携带消费后 `next_offset`。Provider 锁定 ledger 行，拒绝位置缺口，跳过不大于已应用位置的重复记录；同一批剩余记录出现相同目标 key 时保留最高 `next_offset` 的最后状态，再执行一次原子 upsert。
11. 投递保证固定为 `at-least-once delivery + target monotonic apply`。目标提交与 Infra position CAS 之间崩溃会重放，但目标 ledger 必须吸收重复批次，并阻止租约过期 worker 在新 worker 之后写回旧状态；不得宣称跨 Kafka、业务目标数据库和 Infra PostgreSQL 的分布式 exactly-once。
12. consumer auto commit 禁用。Kafka consumer group 只用于分区分配，不是 Transfer committed position 的事实源。
13. 阶段 2B 的初始范围不提供 replay、DLQ、物理删除、Debezium、CDC、Kafka target 或 continuous append；当前业务 Kafka 已按后文 4A/4B 唯一契约开放 `dead_letter` 和写入新隔离目标的 bounded replay。数据库 CDC、Kafka target、物理删除和 continuous append 仍不得借用业务 Kafka 的 DLQ/replay 路线绕过各自边界。
14. resume 前必须验证 committed `next_offset` 仍位于 Kafka partition 的 `[earliest_offset, latest_offset]` 范围；低于 earliest 表示 retention 已破坏连续恢复条件，execution 必须明确失败，不得静默重置到 earliest 或 latest。高于 latest 视为状态损坏，同样失败。
15. continuous worker 必须由 source Provider 读取每分区 `earliest_offset` 和 `latest_offset`，并以目标已成功应用且已提交的 `next_offset` 计算 `lag_records = latest_offset - next_offset`。consumer fetch position、预取位置或 Kafka consumer group committed offset 不得冒充 Transfer committed position。
16. retention 恢复余量以 `recovery_headroom_records = next_offset - earliest_offset` 表示。时间余量只能在同一 worker session 内至少存在两次有效 source latest 样本时，按 `recovery_headroom_records / source_rate_records_per_second` 估算；冷启动、无已提交位置、样本时间非正或写入速率为零时必须返回未知，不得伪造为无限安全。
17. continuous retention health 只允许 `healthy|degraded|critical|unknown`。`next_offset < earliest_offset` 立即为 `critical`；有效时间余量不大于 critical 阈值时为 `critical`，不大于 degraded 阈值时为 `degraded`，其余为 `healthy`；不能完成判定时为 `unknown`。第一版统一服务端默认阈值为 degraded `6h`、critical `1h`，并要求 critical 小于 degraded；阈值是 Transfer runtime 配置，不进入用户任务 JSON。
18. 观测样本写入 `common.task_executions.metadata.continuous`，至少包含样本时间、总体 retention health、总体 checkpoint health，以及每分区 earliest/latest/committed/lag/headroom/source rate/retention horizon/checkpoint age/checkpoint health。Transfer 是采集和判定 owner；Monitor 只读取统一 execution metadata 并展示，不直连业务 Kafka，不读取或改写 `transfer.sync_states` 与 `transfer.runtime_leases`。
    数据库 CDC 在同一次样本中额外写入唯一公共投影 `metadata.continuous.capture`，固定包含 capture `generation`，以及类型化、安全裁剪后的 `source_recovery` 和 `source_transactions`；尚无有效事实的字段省略。该投影不得包含 connector/source 私有错误、连接信息、凭据、Provider 私有资源名或未声明字段。非数据库 continuous execution 不得保留 `capture`。Monitor 只消费该公共投影，不得读取 `transfer.capture_resources` 或 Provider 私表补判。
19. retention health 与 checkpoint health 是两个正交指标。retention health 回答“当前 committed position 是否仍在源保留窗口内，以及按当前写入速率还剩多少恢复时间”；checkpoint health 回答“存在 source lag 时，目标 committed position 是否长期未推进”。checkpoint health 只允许 `healthy|degraded|unknown`：lag 为 0 时为 `healthy`；lag 大于 0 且最近真实 position commit 距采样时间超过部署阈值时为 `degraded`；缺少真实 position commit 时间或无法计算时为 `unknown`。不得用 execution `updated_at`、worker heartbeat、consumer fetch position 或 diagnostics 采样时间冒充 position commit 时间。
20. `recovery_circuit_state=open`、retention `critical`、retention `degraded`、checkpoint `degraded`、恢复等待和 `half_open` 可以由统一前端归一化为实时观测信号。数据库 CDC 还可从 `metadata.continuous.capture` 唯一派生 `source_recovery_critical`、`source_recovery_unavailable` 和 `source_transactions_unavailable`：源恢复窗口 `critical` 为严重级别，显式 `unknown` 恢复窗口或显式 `unavailable` 事务观测为警告级别；事实对象缺失时不产生信号。Monitor 不得根据活跃事务数、持续时间或 Undo 用量自行设阈值和告警。该信号是 execution metadata 的无状态展示投影，不是新的 task/execution 状态，不写回数据库，也不等同于持久化告警事件或已发送通知。后续若实现通知、确认、抑制和升级策略，必须复用这些 owner 事实并单独定义告警生命周期，不能反向让 Monitor 成为 Transfer runtime owner。
21. Monitor 可以把最新 active execution 的观测信号物化为持久化告警事件。告警状态只允许 `open|acknowledged|resolved`：新信号打开事件，确认后进入 acknowledged，owner 信号消失或任务无 active execution 后自动 resolved。抑制使用独立 `suppressed_until`，不新增 suppressed 状态；确认和抑制均不改变信号、execution 或任务状态。告警按 `tenant + module + task_type + source_task_id + signal_code` 去重，同一身份同时最多一个未恢复事件；恢复后的同类信号再次出现必须创建新事件，保留历史，不复用旧行。
22. 告警通知第一版支持 Monitor 通用 Webhook 和平台统一 SMTP Relay 邮件。告警打开、严重级别升级和恢复必须分别产生且只产生一条不可变 `opened|escalated|resolved` lifecycle event；incident 变化、event 及所有启用通知渠道的 per-destination delivery outbox 必须在同一 Infra PostgreSQL 事务提交。各渠道 dispatcher 在事务外发送，并用 `FOR UPDATE SKIP LOCKED`、领取租约、指数退避和最大尝试次数实现至少一次投递；不得在持有数据库行锁时调用外部网络。
23. Webhook destination 按租户隔离，配置只允许 `name`、`url`、`enabled`、`event_types` 和签名 `secret`。`event_types` 只允许 `opened|escalated|resolved` 且必须非空；secret 必须使用平台 `ENCRYPTION_KEY` 加密，任何 GET/List/Swagger 响应不得返回 secret 明文或密文。默认只允许 HTTPS 公网地址；开发或受管内网接收端必须由部署配置显式允许，不能由任务或普通 API 请求绕过 SSRF 约束。
24. Webhook payload schema 固定为 `monitor.alert.webhook/v1`，只包含 `delivery_id`、event/incident 稳定身份、告警级别/状态、任务定义身份、execution UUID、生命周期时间和 Console 链接；不得发送业务数据、连接凭据、完整 execution metadata 或 owner 私有状态。请求签名固定为 HMAC-SHA256(`timestamp + "." + raw_body`)，接收方必须按 `delivery_id` 幂等并校验 timestamp 防重放。
25. 告警确认只记录处理状态，不产生 Webhook lifecycle event。抑制期间生命周期 event 继续保留，但 delivery 记为 `suppressed` 且到期后不补发；恢复后同类信号再次打开会形成新的 incident 和新的 `opened` event。禁用 destination 只影响尚未领取和后续生成的 delivery，已完成投递审计必须保留。
26. Webhook 测试投递是目标配置运维动作，不得伪造 `opened|escalated|resolved` 告警事件或写入正式 delivery outbox。测试请求使用独立 `monitor.webhook.test/v1` payload，沿用目标 HMAC secret、SSRF 校验和签名 Header，同步返回成功 HTTP 状态；接收端失败统一视为下游调用失败。
27. 手动重投只允许作用于当前租户的 `dead` delivery。重投必须复用原 `delivery_id` 和 payload，以保持接收端幂等；目标必须仍存在且启用，重新入队时使用目标当前 URL 和 secret。`attempt_count` 保存累计尝试次数，`retry_base_attempt_count` 保存本次重投周期开始时的累计基线，dispatcher 的最大尝试次数和退避只按当前周期计算；每次人工重投递增 `manual_retry_count`。
28. 删除 Webhook destination 必须在同一事务中取消该目标尚未领取的 `pending` delivery、清除其中的 secret 和重试时间，再删除目标配置。历史 `delivered|dead|suppressed|cancelled` delivery 必须保留；删除时已经领取的 delivery 可以完成当前请求，但删除后不得再生成新 delivery，也不能再人工重投。
29. Webhook destination 创建、更新、测试、删除及 delivery 人工重投必须复用平台统一 Audit Middleware 写入 System 审计日志。审计请求体必须经过现有敏感字段脱敏，不能记录 secret 明文或密文；Monitor 不新增私有操作审计表。
30. 邮件 destination 按租户隔离，配置只允许 `name`、`recipients`、`enabled` 和 `event_types`。`recipients` 必须是非空邮箱地址数组，去重后最多 50 个；`event_types` 只允许 `opened|escalated|resolved` 且必须非空。租户 API 不得接收或返回 SMTP host、port、username、password、TLS 模式或发件身份。
31. 邮件使用独立 `monitor.email_destinations` 和 `monitor.email_deliveries`，不得复用 Webhook destination 或 delivery 表。邮件 delivery 必须冻结目标名称、收件人、主题、纯文本正文和 HTML 正文；内容只包含稳定告警摘要、任务/执行身份和 Console 链接，不得包含业务数据、连接凭据、完整 execution metadata 或 owner 私有状态。
32. SMTP Relay 属于 Monitor 部署配置。第一版只允许显式 `starttls` 或 `tls`：`starttls` 必须要求服务端成功升级 TLS，`tls` 使用隐式 TLS；不支持明文 SMTP、机会式降级或按端口自动猜测。username/password 必须同时配置或同时为空。SMTP host 为空时邮件 dispatcher 不启动，既有未投递 outbox 保持 `pending`，不得改写为 `suppressed`、`dead` 或已送达。
33. 邮件 delivery 状态只允许 `pending|delivering|delivered|dead|suppressed|cancelled`。SMTP 调用成功后进入 `delivered`；失败按当前尝试周期指数退避，达到最大尝试次数后进入 `dead`。领取租约过期的 `delivering` delivery 可以由新 worker 重新领取，投递身份不变。
34. 邮件测试投递是目标配置运维动作，使用独立测试主题和正文，同步验证目标当前收件人及平台 SMTP Relay，不得伪造告警 lifecycle event 或写入正式 delivery outbox。SMTP 未配置或发送失败统一视为下游调用失败。
35. 邮件人工重投只允许当前租户的 `dead` delivery，必须复用原 `delivery_id`、主题和正文，使用目标当前收件人，并按 `retry_base_attempt_count` 开启新的最大尝试周期；累计 `attempt_count` 和 `manual_retry_count` 必须保留。删除邮件 destination 必须取消该目标尚未领取的 `pending` delivery 并保留历史；删除时已领取的发送可以完成，删除后不得生成新 delivery 或人工重投。
36. 邮件 destination 创建、更新、测试、删除及 delivery 人工重投必须复用平台统一 Audit Middleware 写入 System 审计日志。Monitor 不建立第二套操作审计，不在普通 API、Swagger、投递记录、日志或审计中暴露 SMTP password。
37. 通用执行告警只允许消费 `common.task_executions` 公共事实。各 owner 模块负责写入真实 execution 状态、开始/结束时间和安全错误摘要；Monitor 不得读取 owner 任务表、结果表、worker 私有租约或运行时内部状态补判失败。System 只负责保存模块注册时发布的 TaskProvider 角色声明、解析当前 Backend 租约、认证和操作审计，不成为告警事实或规则 owner。
38. 第一版租户告警规则精确绑定 `tenant + module + task_type + source_task_id`，只允许有稳定任务定义身份且 `parent_execution_id IS NULL` 的根 execution。可配置目标以该任务最新根 execution 的模式为准；最新根 execution 属于 continuous session 时，不得因历史 bounded execution 将该任务继续暴露为通用规则目标。无 `source_task_id` 的 ad-hoc execution、Orchestrator 子 execution 和 System cleanup 子 execution不进入通用规则；父编排失败由 Orchestrator 根 execution 表达。
39. 通用规则类型固定为 `last_terminal_failed|last_terminal_timeout|consecutive_failures`。最近终态查询只考虑 `success|failed|timeout`，忽略 `cancelled`；`failed` 或 `timeout` 告警只能由后续 `success` 恢复，新的 `pending|running` 不证明任务已经恢复。`consecutive_failures` 的阈值只允许 2 到 20，最近 N 个有效终态全部为 `failed|timeout` 时激活，任一 `success` 中断连续失败序列。
40. Transfer continuous session 不消费通用 execution 失败规则。Monitor 继续以 `metadata.continuous`、retention、checkpoint 和 recovery circuit owner 事实运行专用 evaluator；不得把自动恢复产生的失败 session 再解释成通用任务失败。Transfer bounded execution 可以使用通用规则；同一任务从 bounded 切换为 continuous 后，通用 evaluator 必须依据最新根 execution 暂停该任务规则，不能继续沿用旧 bounded 终态打开告警。
41. Monitor 使用 `monitor.alert_rules` 保存租户规则，至少包含稳定 `rule_id` UUID、名称、精确任务身份、规则类型、连续失败阈值、严重级别和启用状态。规则身份进入 incident fingerprint；同一规则和同一任务同时最多一个未恢复 incident。更新规则判定语义、停用或删除规则时，必须先恢复该规则当前未恢复 incident，不能留下永不再评估的活动告警。
42. Monitor 使用 `monitor.notification_routes` 保存通用规则到 Webhook/邮件 destination 的显式绑定。路由必须按租户校验 destination 存在；同一规则、渠道和目标只能一条。规则没有路由时仍创建不可变 alert event，但不生成 Webhook 或邮件 delivery。删除目标时必须同时删除指向它的未使用路由；历史 incident、event 和 delivery 保留。
43. 通用规则 evaluator、Transfer continuous evaluator 和后续其他 evaluator 必须先完成各自公共事实查询，再把全部 active signal 交给同一个 incident reconciler。任何 evaluator 查询失败时，不得把该 evaluator 拥有的现有 incident 误恢复；不同 evaluator 不能独立执行全局“缺失即恢复”。
44. 通用规则 evaluator 使用最终一致的周期扫描即可，第一版不要求 Kafka。若后续使用 PostgreSQL `LISTEN/NOTIFY`，通知只能作为唤醒信号，`common.task_executions` 仍是唯一事实源，周期扫描仍负责丢失唤醒后的收敛。
45. `created_at`、`updated_at` 或全局固定时长不能单独证明 execution 卡死。运行超时由 owner 按任务自身 deadline 写入 `status=timeout`；在统一 heartbeat/deadline 契约完成前，不实现 `pending_stalled` 或 `running_stalled` 通用规则，也不得启用按创建时间批量改写长任务状态的清理逻辑。
46. Monitor 的通用告警目标发现和规则创建校验必须与 System 当前已启用 TaskProvider 的非废弃 `task_capabilities[]` 取交集。历史 execution 必须保留审计，但已经不再由 provider 声明的旧 `module + task_type` 不得出现在新规则目标中，也不得继续创建或重新启用规则；不得在 Monitor 硬编码业务任务类型白名单。
47. 数据库 CDC schema change 的跨模块公共事实固定写入触发阻塞的 execution `metadata.continuous.schema_change`。该对象至少包含 `request_id`、`status=pending|applied|stopped`、generation/revision、源 partition/offset、scope、diff 和检测时间；审批后还包含 `metadata_scan.status=pending|running|success|failed`、attempt，以及成功提交 Meta scan 时返回的 Meta execution UUID。Transfer 必须在私有 request 状态变化的同一 Infra PostgreSQL 事务中更新该投影，Monitor 不得读取 `transfer.schema_change_requests` 补判。
48. `schema_change.status=pending` 是 `schema_change_blocked` 严重告警的唯一 owner 信号。该信号位于已经 `failed + stop_reason=schema_change_blocked` 的终态 execution，不受“只扫描最新 active execution”限制；同一任务只取最新 pending schema-change execution。人工 additive migration 审批把同一投影原子更新为 `applied` 后，Monitor 自动恢复 incident；不可逆 Stop 在删除任务私有控制状态前将其更新为 `stopped`。Stop 与 worker Finish 无论谁先提交，Finish 都必须以已经持久化的 `desired_state=stopped` fencing，不能把任务重新置为 blocked 或留下 pending 公共信号；有效 lease 已释放或过期后，Stop 还必须在 capture cleanup 前把遗留 active execution 取消并将 task 收敛到 idle。若 worker 在 request 提交后、Finish 前失联，lease expiry reconciler 必须将原 execution/task 收敛为 schema blocked 并清空 lease owner，禁止创建或领取 recovery execution。
49. `metadata_scan.status=failed` 当前表示 Transfer 向 Meta 提交自动扫描失败，不证明扫描结果失败。手工 Meta 扫描尚无回写 Transfer 的关联完成事实，因此该状态只在 Transfer 任务详情和 execution metadata 中展示，第一版不得据此创建无法可靠自动恢复的 Monitor incident；未来若要告警，必须先定义关联 Meta execution 或显式完成回执，不能用 Resume、新 execution 或时间流逝猜测恢复。

continuous task 与 execution 生命周期：

1. continuous task definition 必须保存独立活状态 `desired_state=running|paused|stopped`，初始值为 `stopped`。该字段不进入 config JSON，也不复用最近 execution `status`；bounded task 不消费该字段。
   任务实际状态继续使用 `status=idle|running|blocked`；`blocked` 表示系统发现不可安全恢复的条件，但不覆盖用户 `desired_state`。当前只有数据库 CDC schema drift 使用 `blocked`。
2. `start` 原子设置 `desired_state=running` 并创建 pending execution；`pause` 设置 `paused`，`resume` 设置 `running` 并创建新 execution，`stop` 设置 `stopped`。同一 task 已有合法 active session 时不得重复创建 execution。
3. 每次启动或自动恢复都创建新的 pending execution；一个 execution 对应一个 runtime session，结束后不得重新变回 running。
4. runtime session 由 `transfer.runtime_leases` 保存 owner instance、execution、lease deadline、heartbeat 和 fencing token；lease 与 `transfer.sync_states` 的业务 position 分离。
5. 正常总运行时长不触发 timeout。source poll、target apply、position commit 分别使用操作超时；heartbeat 丢失或 lease 过期以 `failed` 结束当前 execution，并在任务 desired state 仍为 running 时创建新 execution 恢复。worker 正常退出时当前 execution 以 `cancelled + stop_reason=worker_shutdown` 结束；新 worker 必须自动创建 recovery execution，继承上一 execution 的 `trigger_type`，并在 metadata 记录 `recovery_reason` 与 `recovered_from_execution_id`。lease 过期恢复同样创建新 execution，并记录 `recovery_reason=lease_expired`；两种情况都不得把旧 execution 重新置为 running。
   可恢复失败必须立即创建唯一的 pending recovery execution，并通过 deployment-level `recovery_not_before` 控制领取时间；不得依赖 worker 内存 timer，也不得为同一次失败重复创建 execution。worker shutdown 不增加连续失败次数；普通 execution failure 和 lease expiry 增加连续失败次数。`schema_change_blocked`、用户 Pause 和 Stop 不进入自动恢复。
   自动恢复使用指数退避，达到最大连续失败次数后 circuit 进入 `open`，冷却到期后的 pending execution 被领取时转为 `half_open`。circuit open 是临时恢复治理状态，不复用任务 `status=blocked`；此时任务保持 `desired_state=running`、无合法 runtime lease，实际 `status=idle`。任一目标 position 成功提交，或 session 连续稳定运行达到部署阈值后再发生失败，连续失败计数重置。
   recovery execution metadata 至少记录不可变的本次恢复审计 `recovery_reason`、`recovered_from_execution_id`、`recovery_attempt`、`recovery_not_before`、`recovery_backoff_seconds`，以及可变运行状态 `recovery_consecutive_failures`、`recovery_circuit_state=closed|open|half_open`。成功提交 position 只重置连续失败计数和 circuit 状态，不得覆盖本次 recovery execution 的 attempt/backoff 审计。退避初值、上限、最大连续失败次数、circuit 冷却时间和稳定运行阈值是 Transfer runtime 部署配置，不进入任务 JSON。
6. 用户 pause/stop 必须真实停止 poll、完成或放弃当前未提交批次、关闭 source/target session、释放 partition ownership 和 lease，再以 `cancelled` 结束当前 execution，并在 metadata 记录 `stop_reason=paused|stopped`。resume 创建新 execution。
7. 工作包 2B 真实中断闭环完成时可以恢复 Transfer 私有 task-definition stop 控制入口；但 `supports_cancel` 是整个 `sync` task type 的 TaskProvider 能力，在 bounded execution 也具备真实取消前必须继续为 `false`，不得注册标准 execution cancel endpoint。
8. continuous execution 不使用 0 到 100 作为主要进度；Monitor 应展示 per-partition next offset、source latest offset、lag、吞吐、last event time、last committed time、checkpoint age/health、retry/rebalance、恢复 circuit 和 heartbeat。Monitor 只消费 owner 写入的统一 execution metadata；不得自行连接 Kafka 或 Transfer 私有表补算这些事实。
9. Orchestrator v1 不编排 continuous Transfer task。Transfer 的 TaskProvider 任务发现不得把 continuous task 暴露为可选编排步骤，provider execute 入口也必须拒绝 `source=orchestrator` 触发 continuous task；长期服务型 Step 语义进入 Orchestrator v2 专题。
10. `auto_scan_metadata=true` 的 continuous task 不等待 execution 成功结束。普通业务 Kafka task 在目标 Provider 首次成功建立目标结构后，按 task `apply_identity` 触发一次目标父 catalog 的 Meta deep scan。数据库 CDC task 的 connector 必须启用 Debezium `sink` notification，并把通知写入当前 generation-owned 单分区 CDC data topic；Transfer 只接受 connector name 与当前 generation 完全一致的 `aggregate_type=Initial Snapshot` 通知，所有合法初始快照通知都作为目标 `skip` 与 apply ledger、Transfer committed position 一起推进，只有 `type=COMPLETED` 所在 offset 成功提交后才触发首次扫描。该顺序同时覆盖非空表与空表，不依赖首条数据事件、`source.snapshot=last` 或等待时间猜测。首次扫描使用 task 私有持久化 claim：`running(claim_token, lease_until, attempt+1) -> success|failed`，worker 恢复或并发实例不得重复提交；已失败或过期 running claim 由后续 runtime session 接管。Meta 提交失败不得中断 CDC/Kafka 数据面，失败状态由后续 runtime session 重试或由用户手动刷新。目标 schema 经正式流程变化后另行触发 deep scan；普通 DML、单条事件和单个 batch 不触发 Meta scan。claim TTL 统一使用部署配置 `TRANSFER_META_SCAN_CLAIM_TTL`，并且必须大于 Meta client 的 HTTP 超时。

#### Transfer 数据库 CDC v1 契约（PostgreSQL/MySQL/Oracle 已完成）

数据库 CDC 第一版只允许以下三种 source provider 进入同一数据面：

```text
PostgreSQL 单表 -> Debezium PostgreSQL Connector -> Infra Kafka
  -> Transfer Continuous Worker -> PostgreSQL/MySQL/Oracle 新目标表
MySQL 8.0 单表 -> Debezium MySQL Connector -> Infra Kafka
  -> Transfer Continuous Worker -> PostgreSQL/MySQL/Oracle 新目标表
Oracle 单表 -> Debezium Oracle Connector -> Infra Kafka
  -> Transfer Continuous Worker -> PostgreSQL/MySQL/Oracle 新目标表
```

公开任务配置不暴露 provider 类型、connector、replication slot、publication、schema-history topic、Infra Kafka data topic 或 consumer group；source provider 只由 source locator 对应的 System Engine 解析结果决定：

```json
{
  "runtime": {"boundary": "continuous", "record_failure": {"mode": "block"}},
  "load": {
    "mode": "incremental",
    "change_detection": {
      "type": "cdc",
      "bootstrap": "initial_snapshot"
    }
  },
  "source": {
    "locator": "addp://engine/12/path/public/orders?type=table",
    "data_type": "table",
    "representation": "native"
  },
  "target": {
    "parent_locator": "addp://engine/20/path/public?type=schema",
    "name": "orders_cdc",
    "data_type": "table",
    "representation": "native",
    "policy": {"apply_mode": "upsert_delete", "keys": ["id"]}
  },
  "transforms": [
    {
      "type": "field_mapping",
      "version": "v1",
      "mode": "project",
      "fields": [
        {"source": "id", "target": "id", "target_type": "int", "nullable": false}
      ]
    }
  ]
}
```

契约约束：

1. 当前 CDC source 和 target 均允许 PostgreSQL、MySQL 8.0 或 Oracle native table；target 必须声明完整原子应用能力，一个任务只捕获一张源表。公开 capability 固定声明 `sources=[postgresql,mysql,oracle]`、`targets=[postgresql,mysql,oracle]`、`bootstrap=[initial_snapshot]`、`apply_mode=upsert_delete`。Oracle 支持普通字段和 `MDSYS.SDO_GEOMETRY`，但仍不开放普通业务 LOB；Oracle readiness 必须从 `V$PARAMETER.cluster_database` 得到明确 `FALSE`，`TRUE` 或未知值都在 capture generation 创建前拒绝。多表 connector、无主键表、RAC、ArcGIS SDE 和 Kafka target 均未进入当前实现。
2. 源表必须有稳定、非空且捕获期间不可修改的主键；Debezium record key 必须完整包含该复合主键。源主键经 `field_mapping` 映射后必须与 `target.policy.keys` 一一对应。
3. `bootstrap` 第一版只允许 `initial_snapshot`，对应 Debezium `snapshot.mode=initial`。Debezium 负责一致性 snapshot、日志起点和 snapshot/stream 无空洞交接；Transfer 只按 Infra Kafka 顺序消费，不另起 bounded snapshot 路线。
4. 初次 bootstrap 的目标表必须不存在，由本任务按固定字段映射创建。第一版不接管、清空或合并已有目标表，避免 snapshot upsert 无法识别目标残留行。
5. Debezium payload 固定使用无 schema wrapper 的 JSON envelope；key/value converter 的 `schemas.enable=false`，`tombstones.on.delete=false`。为消除 schemaless JSON 的类型歧义，connector 同时固定 `decimal.handling.mode=string` 和 `time.precision.mode=connect`，MySQL 额外固定 `binary.handling.mode=base64`。Oracle 普通源表字段中的 `CLOB`、`NCLOB`、`BLOB`、`BFILE`、`LONG`、`LONG RAW`、`XMLTYPE` 和原生 `JSON` 必须在 capture plan 冻结、创建 generation 之前明确拒绝；Oracle Spatial generation-owned 镜像表中的 WKB `BLOB` 是唯一内部传输例外，不代表开放普通业务 LOB。内部 CDC data topic 第一版固定为单 partition。connector 同时固定 `notification.enabled.channels=sink`、`notification.sink.topic.name=<当前 generation data topic>`；数据事件和 `Initial Snapshot` 通知必须进入同一分区，以 Kafka offset 顺序作为快照数据先于 `COMPLETED` 的唯一时序事实。
6. Debezium `op=r` 归一化为 `snapshot` 并按 upsert 应用，`op=c|u` 归一化为 `upsert`，`op=d` 使用 Kafka record key 归一化为 `delete`。`Initial Snapshot` 的 `STARTED|IN_PROGRESS|TABLE_SCAN_COMPLETED|COMPLETED|SKIPPED` 通知归一化为 `skip`，必须严格校验通知字段、类型和当前 generation connector name；只有 `COMPLETED` 同时产生初始快照完成语义，`SKIPPED` 只表示 connector 重启时无需再次执行初始快照。未知或畸形通知、`op=t|m`、空 value tombstone、来源表身份不匹配和未知 op 均视为协议错误，不得静默推进 offset。
7. delete 只按映射后的稳定目标 key 执行物理行删除，不依赖 `before` 包含完整旧行。目标必须使用 `apply_mode=upsert_delete`，目标 `PartitionedTableChangeApplyProvider` 必须真实声明并实现 `operations=[upsert,delete,skip]` 后才可开放 CDC capability；`skip` 只推进目标 ledger，不写业务行。
8. snapshot/upsert/delete 必须与目标 Provider 的业务库 apply ledger 在同一事务按 partition monotonic apply；PostgreSQL 使用 `addp_transfer.apply_positions`，MySQL 使用目标业务数据库内的 `_addp_transfer_apply_positions` InnoDB 私有表，Oracle 使用目标 schema 内的 `_ADDP_TRANSFER_APPLY_POSITIONS` 私有表。`transfer.sync_states` 继续保存 `kafka_offset/v1.next_offset`，目标提交后才允许以 runtime fencing + state version 做 CAS 推进。
	Oracle target schema 必须等于连接用户；ledger 必须带固定 ownership comment 并从 Catalog 隐藏。Oracle CDC 目标继续使用 `PartitionedTableChangeApplyProvider`；普通 bounded table write 使用同一 Oracle Engine 的 `TableWriteSessionProvider`，只创建或写入普通表和 `MDSYS.SDO_GEOMETRY`，不得注册 SDE feature class 或修改 geodatabase system tables。两条能力复用同一 Oracle 字段/空间类型校验与 EWKB → SDO 转换规则，不建立 Transfer 私有 Oracle writer。当前支持 `string|int|bigint|float|double|decimal|bool|date|timestamp|json|uuid|bytes|geometry`，明确拒绝独立 `time`、decimal precision > 38 和非 XY geometry。geometry 边界仍是 EWKB + 冻结 `SpatialInfo`，Provider 必须校验 SRID/type/dimension 后转换为标准 WKB，并使用 `SDO_UTIL.FROM_WKBGEOMETRY` 写入 `MDSYS.SDO_GEOMETRY`。CDC ledger 行锁必须使用 `FOR UPDATE NOWAIT` 有界重试并检查 runtime context；锁等待取消与任何业务写失败都必须回滚业务行和 ledger。
9. Kafka Connect offset 是捕获位点，回答源数据库日志已捕获到哪里；Transfer committed position 是消费位点，回答目标已应用到哪个 Infra Kafka offset。两者由各自 owner 管理，任何一方都不能代替另一方。
10. PostgreSQL LSN、MySQL binlog file/position/GTID、Oracle SCN、事务 ID 和事件时间可以进入 ChangeEvent 诊断信息，但不得替代 Kafka partition offset，也不承诺把一个源数据库事务跨多行原子提交到目标；交付保证仍是 at-least-once + target monotonic apply。
11. PostgreSQL CDC v1 接受可无歧义映射到 ADDP `string|bool|int|bigint|float|double|decimal|date|time|timestamp|json|uuid|geometry` 的源字段；完整源表字段必须逐一映射且声明类型必须与真实 PostgreSQL 类型一致。PostGIS 只开放带 typmod 的 `geometry(<OGC type>[Z], <positive SRID>)`，不开放 `geography`、未约束的 generic geometry、M/ZM、geometry 主键或运行时重投影。capture generation 创建前必须从源库冻结每个 geometry 字段的 OGC type、SRID、维度和 nullable，并持久化为 generation source spatial facts；Debezium schemaless JSON geometry 固定按 `{wkb: <base64>, srid: <number>}` 解码。进入 ADDP 数据面的标准中间表示仍是行内 `[]byte` EWKB 加表级 `datatype.SpatialInfo`，`geom.T` 只允许作为 native encoding 与 EWKB 转换、校验时的短生命周期对象，不进入 ChangeEvent、Provider、任务配置或持久状态。消息 SRID、几何类型或维度与冻结事实不一致时按 schema drift 阻塞且不得推进 offset。目标 Engine Provider 使用映射后的字段名和同一组空间事实转换为目标 native geometry；PostgreSQL 目标按此创建 geometry typmod 列。跨引擎或跨执行边界的空间链路默认复用 native geometry ↔ EWKB + `SpatialInfo` 边界，不能引入数据库私有的跨层几何对象；只有 source 与 target 通过能力声明协商出完全相同的 native geometry encoding，且链路不经过编码转换、CRS 转换或中间几何算子时，planner 才可选择 native encoding 直通。PostGIS `geometry` 是数据库内部类型而不是跨 Provider encoding；当前 PostgreSQL CDC 经过 Debezium 和 Infra Kafka，不满足 native 直通条件。`bytea`、数组、interval、枚举和其他用户定义类型在创建 capture 前明确拒绝，不得先启动 connector 再让运行时失败。Decimal 以原始十进制字符串传递；date 使用 epoch days；time 和无时区 timestamp 只允许声明精度 `0..3` 并使用毫秒编码，默认微秒精度或显式精度大于 3 的列必须拒绝，不能静默截断；带时区 timestamp 接受 RFC 3339 并按 UTC instant 写入目标 `timestamp`；JSON/JSONB 使用合法 JSON 字符串。
    Debezium 的 geometry 属性名固定为 `wkb`，但真实 PostgreSQL/PostGIS connector 已验证该 base64 字节可以是带内嵌 SRID 的 EWKB。adapter 必须使用 ADDP 通用 WKB/EWKB 解析器，并要求内嵌 SRID、旁路 `srid` 和 generation 冻结事实一致；不得仅按 JSON 属性名强制使用标准 WKB 解码器。
    MySQL CDC v1 只接受有符号 `TINYINT/SMALLINT/MEDIUMINT/INT/BIGINT`、`CHAR/VARCHAR/TEXT`、`DECIMAL/NUMERIC`、`FLOAT/DOUBLE`、`DATE/TIME/DATETIME/TIMESTAMP`（精度 `0..3`）、`JSON` 和 `BINARY/VARBINARY/BLOB`。拒绝 unsigned、`TINYINT(1)`/`BOOL`、`BIT`、`ENUM/SET`、`YEAR`、空间类型、超过毫秒精度和 zero-date 默认值；完整字段、nullable 与复合主键顺序必须和 field mapping 一致。MySQL source adapter 必须按自己的 Debezium source schema 严格校验，不能复用 PostgreSQL exact source schema。
    Oracle CDC v1 接受可无歧义映射到 ADDP `string|bool|int|bigint|float|double|decimal|timestamp|geometry` 的字段；完整源表字段、nullable 与复合主键顺序必须和 field mapping 一致。`NUMBER` 在 Debezium JSON 中按十进制字符串严格转换，`DATE` 与精度 `0..3` 的 `TIMESTAMP` 使用 Connect 毫秒时间编码。普通业务 LOB/binary、JSON/XML、超过毫秒精度的 timestamp 及其他无法稳定映射的类型必须在创建 capture 前拒绝。Oracle Engine 必须另行配置 `cdc_database_name`、`cdc_user`、`cdc_password`；CDC 账号、ARCHIVELOG、FORCE LOGGING、minimal supplemental logging 和源表 `SUPPLEMENTAL LOG DATA (ALL) COLUMNS` 必须在 connector 创建前验证通过。Oracle source adapter 使用自己的 Debezium source schema 和 SCN 语义，不复用 PostgreSQL/MySQL adapter。
    Debezium 3.6 LogMiner 不能直接稳定捕获 `MDSYS.SDO_GEOMETRY`：snapshot 只产生 JVM `STRUCT` 标识，stream 产生空值，且纯空间 update 会消失。因此 Oracle Spatial CDC 不允许把原表直接交给 connector，也不允许 consumer 猜测 `STRUCT`/redo SQL。Transfer capture Provider 必须使用源 Engine 的 schema-owner 账号，在源表所属 schema 内按 generation 创建 ADDP-owned 镜像表、行级同步触发器和 schema DDL guard；镜像表保留原字段名和主键，把每个 `SDO_GEOMETRY` 通过 `SDO_UTIL.TO_WKBGEOMETRY` 转为内部 BLOB，其余字段保持 Oracle native 类型。行级触发器先建立，Provider 随后锁定源表并幂等回填镜像，最后 connector 只捕获镜像表，以保证 initial snapshot 与后续变化无空洞。DDL guard 在 generation 运行期间明确拒绝源表 `ALTER|DROP|RENAME`，避免镜像静默漏掉 schema drift；必须先 Stop 并完成资源清理后才能修改源表结构。
    Oracle Spatial generation 创建前必须通过 Oracle Engine Catalog Facts 冻结每个 geometry 字段的 OGC type、正 SRID、XY/XYZ 维度和 nullable，并持久化为 generation source spatial facts。connector 对镜像表固定 `lob.enabled=true`、`binary.handling.mode=base64`；adapter 只把 insert/update `after` 中的 base64 WKB 转为标准 EWKB，并按冻结事实校验 SRID、几何类型和维度。Debezium 对 BLOB 的 update/delete `before` 可能给出 unavailable placeholder；Oracle Spatial adapter 只校验完整字段集合和稳定主键，不读取该旧空间值，delete 仍只按 record key 应用。这里的 BLOB 是 Provider-owned 内部载体，不代表开放用户 LOB CDC。
12. schema drift 默认严格阻塞。字段缺失、主键变化、类型不兼容、geometry 空间事实变化或 envelope/source 结构变化必须在 execution metadata 保存 missing/unexpected/incompatible schema diff，并以 `schema_change_blocked` 结束 execution；任务原子进入 `status=blocked`，当前消息和 committed offset 均不得推进。
13. PostgreSQL/MySQL 中唯一可在原 capture generation 内恢复的 schema change 是人工确认的 additive migration：当前阻塞消息只能包含新增字段，`missing_fields` 与 `incompatible_fields` 必须为空；新增字段必须仍存在于源表、允许 NULL、不是主键且不是 geometry。服务端为该 task/generation/阻塞 execution 创建唯一 pending schema change request，并记录当前 Kafka partition/offset、schema diff 和 mapping revision。`GET /task-definitions/:id/schema-change` 是查询当前 request 与服务端复查建议的唯一入口；任务 JSON 不增加 `manual|additive` 策略开关。Oracle 第一期不开放 additive inspection/approval，任何 schema drift 均只能 Stop 后创建新任务和新目标表。
14. PostgreSQL/MySQL additive 审批必须通过唯一 `POST /task-definitions/:id/schema-change/approve`，显式覆盖 pending request 的全部新增源字段，并逐一提交新的 target 名、`target_type` 和 `nullable=true`。服务端必须重新验证现有字段、主键和待新增字段的实时源事实，拒绝重名、覆盖既有 source/target、类型不匹配、非 nullable、geometry、缺失字段或不完整审批；普通 `PUT /task-definitions/:id` 继续禁止修改已有 capture generation 的 CDC config。
15. 目标 PostgreSQL/MySQL DDL 必须复用各自 `PartitionedTableChangeApplyProvider` 的 prepare/evolution 主路径，只允许幂等新增 nullable 列并验证既有列兼容，不能建立 Transfer 私有 DDL 通道。Oracle target 虽实现同一 Provider 的 prepare/evolution 能力，但 Oracle source schema drift 第一期仍不开放 additive inspection/approval，不得因此新增审批分支。外部 DDL 与 Infra 状态无法组成分布式事务，因此审批使用 Infra request/task 行锁串行执行；若目标 DDL 已提交但 Infra 事务回滚，重试必须先验证已存在目标列完全兼容，再完成同一 request，不能创建补偿列或第二条恢复路径。
16. 审批完成时在同一 Infra PostgreSQL 事务追加唯一 field mapping、递增 capture `schema_revision`、把 request 标记为 `applied`，并把 task 收敛为 `status=idle, desired_state=paused`。服务端随后触发目标表 Meta deep scan；Meta scan 失败不得回滚已完成 DDL/mapping revision，但必须记录并返回可观测错误。用户继续使用既有 Resume API 创建新 execution，从未推进的 committed offset 恢复；审批 API 不隐式启动 runtime。
    Meta scan 触发固定使用 request 内的持久化 claim 状态机：`pending -> running(claim_token, lease_until, attempt+1) -> success|failed`。只有 `pending` 或 claim 已过期的 `running` 可通过 CAS 取得新 UUID token；完成更新必须同时匹配 request、`running` 和 token，过期 claimant 的迟到结果不得覆盖新 claimant。并发审批只能有一个 claimant 调用 Meta；进程在 claim 后崩溃时，由相同重复审批 POST 在 `TRANSFER_META_SCAN_CLAIM_TTL` 到期后接管，schema change GET 始终只读。真实 Meta API 返回失败后状态固定为 `failed`，不得由重复审批自动循环重试；用户可在 Meta 使用既有手动扫描能力。claim TTL 是部署配置，不进入任务 JSON，并且必须大于 Meta client 的 HTTP 超时。
17. request 只允许 `pending -> applied`，历史 request 是 generation 内 mapping revision 审计事实。重复审批已 applied request 必须幂等返回同一结果；同一 generation 同时只能有一个 pending request。下一次新增字段再次阻塞时创建下一 revision request。task physical cleanup 随 capture generation 级联删除这些私有 request；统一 execution 和 System audit 继续保存长期审计。
18. PostgreSQL/MySQL 的删除字段、类型变化、主键变化、非 nullable 新增、geometry 新增以及其他协议漂移，以及 Oracle 的任何 schema drift，均是当前 generation 的不可恢复终态，只能不可逆 Stop 后创建新任务和新目标表。数据库 CDC 仍不支持 replay、DLQ、Schema Registry、Avro、Protobuf、truncate、历史补洞或全自动 DDL。

捕获资源与生命周期：

1. Kafka Connect 使用独立 distributed 集群。Transfer capture supervisor 是 connector 生命周期 owner；continuous worker 只消费 Infra Kafka，不在 Go 进程内嵌入 Debezium。
2. connector、provider 专属资源、CDC data/schema-history topic 和 consumer group 由服务端按 task/generation 生成稳定内部名称，不接受用户自定义，也不进入 System Engine、Meta 资源树或公开任务 config。
   `transfer.capture_resources` 只保存 engine-neutral generation、connector/data topic/group、source identity 与 lifecycle 状态，并以 `source_type` 明确 provider。PostgreSQL slot/publication 只存在于一对一 `transfer.postgresql_capture_resources`；MySQL connector server id、schema-history topic name 和 ownership 只存在于一对一 `transfer.mysql_capture_resources`；Oracle schema-history topic name、Spatial 镜像表、行级触发器、DDL guard 名称及 ownership 只存在于一对一 `transfer.oracle_capture_resources`。provider 子事实必须与 generation 同事务创建并通过外键级联清理，不得在 generic 主表保留 provider 字段、兼容读取或伪资源占位。
3. PostgreSQL publication 只包含任务源表；slot 和 publication 均由 ADDP 创建并拥有，不得复用或删除用户已有 replication slot/publication。MySQL 不创建或登记伪 slot/publication；每个 generation 使用唯一非零 connector server id。MySQL 和 Oracle generation 均拥有独立单分区 schema-history topic。Debezium 3.6 history record key 为空，因此该 topic 固定 `cleanup.policy=delete`、`retention.ms=-1`，由 Stop 显式删除，不能改成 compact 或依赖自动建 topic。Oracle 表级 `ALL COLUMN LOGGING` 是可被多个任务共享的 source readiness，不是 generation-owned 资源，Stop 不删除该配置。Oracle Spatial 镜像表、行级触发器和 DDL guard 则属于 generation-owned Provider 资源：创建时必须拒绝同名非 ADDP 对象，Stop 必须先删除 connector，再核对源连接、schema、源表和对象身份，随后删除 DDL guard、行级触发器和镜像表；任一步失败进入 `cleanup_failed`，不得遗留第二条清理路径。
4. `pause` 只停止目标应用 runtime，Debezium connector 继续捕获并把变化写入 Infra Kafka；正常 pause 的主要代价是 Kafka backlog、磁盘和 retention 窗口。connector、Kafka 或网络故障时，还必须按 provider 独立观测 PostgreSQL slot/WAL、MySQL binlog 保留或 Oracle redo/archive log 容量风险。
5. pause 的无损恢复只在 connector 健康且 committed position 未被 Kafka retention 清除时成立，不是无限期保证。暂停期间必须继续观测 connector 状态、slot/WAL lag、topic 容量和 committed position 的 retention horizon。
   capture generation 的 `running` 必须同时满足 Kafka Connect connector/task 为 `RUNNING`，以及使用该 connector 实际源数据库凭据执行的最小只读探针成功。`connector_status` 只表达 Kafka Connect 线程状态，`source_status` 只表达 CDC 数据源连通性，二者不得互相替代；任一失败或无法完成观测时 generation 必须为 `failed`。Oracle 必须探测独立 `cdc_user` 连接，不得用 schema-owner 业务连接或 System Engine 的普通连接状态代替 LogMiner 捕获连接。探针使用当前 System Engine 连接配置前必须重新核对 generation 冻结的 source engine、provider、database 和 connection fingerprint；连接身份变化时明确失败，不得改用新端点继续旧 generation。
   `source_recovery` 是与上述连通状态、Infra Kafka retention 分离的 provider-neutral 恢复窗口观测，固定使用 `health=healthy|critical|unknown`。Oracle 由 Kafka Connect connector offset 中当前 `scn` 作为 capture position，并与 CDC 账号可见的当前 database incarnation 下 redo/archive 最早 `FIRST_CHANGE#` 比较：capture SCN 小于最早可用 SCN 时为 `critical`，仍在窗口内时为 `healthy`；offset、动态视图或时间事实不足时为 `unknown`，不得把 unknown 当作 source offline。Oracle 同时记录最早可用日志时间形成的事实窗口秒数和可选 FRA 使用率；未启用 FRA 时容量字段必须为空，不得伪造为 0%，也不得按 SCN 差值估算剩余时间。
   `source_transactions` 是与 `source_recovery` 分离的 provider-neutral 未提交事务压力观测。Oracle 通过同一 CDC 账号查询 `V$TRANSACTION`，公开活跃事务数、最老事务起始 SCN、最老事务持续秒数和合计 Undo blocks/records；无活跃事务时计数与 Undo 用量为 0，最老事务字段为空。平台不硬编码“长事务”时长或 Undo 阈值，也不据此改变 capture 状态；查询失败只使该观测不可用，不能覆盖已成功的 `source_recovery` 或把 source 判为 offline。两类观测均由 Transfer capture owner 持久化并通过任务摘要公开，Monitor 只能消费 owner 事实，不得直连 Oracle 或 Kafka Connect。
6. `resume` 只允许从 paused 或可恢复失败状态创建新 execution；继续使用同一 capture generation、connector offset、topic 和 Transfer committed position。`status=blocked` 时 start/resume API 必须返回 `409 Conflict`；只有 pending additive schema change request 经专用审批 API applied 后，任务才进入 paused 并重新允许 Resume。
7. `stop` 是数据库 CDC task 的不可逆终态：停止目标 runtime，停止并删除 connector，删除 ADDP-owned provider 专属资源、data/schema-history topic、consumer group 和 ACL，使原 capture generation 失效。已停止任务不得再次 start/resume；重新同步必须创建新任务和新目标表并重新 initial snapshot。
   `desired_state=stopped` 同时也是 continuous task definition 的初始值，因此不能单独作为数据库 CDC 已进入不可逆终态的判据。永久停止必须以 `transfer.capture_resources` 中当前 generation 的 `status=stopped` 为事实；尚无 capture generation 的新任务允许首次 start。`status=cleanup_failed` 表示不可逆 Stop 已开始但资源清理未完成，此时只允许重试 Stop 清理，不允许 start/resume。
8. Stop API 必须由服务端要求显式不可逆确认，不能只依赖前端弹窗；沿用唯一 `POST /task-definitions/:id/stop` 路由，CDC task 请求体必须提交 `confirmed=true` 且 `confirmation_text` 与当前任务名称完全一致。Console 同时使用 danger 样式二次确认并要求输入任务名称，明确说明不能恢复、内部捕获资源会被删除且重新同步必须新建任务。普通业务 Kafka continuous stop 不要求该请求体，仍按其既有可恢复 committed-position 语义处理，不能混用 CDC 的终态提示。
9. stop 不删除目标业务表、目标行、目标 apply ledger、任务定义、execution 或清理审计。任务删除和 System cleanup 必须再次幂等清理残留 capture 资源，并保留统一 execution/审计事实。

#### Transfer 业务 Kafka DLQ 与 bounded replay v1 契约（4A/4B 已完成）

该契约是当前公开实现的唯一技术路线。Transfer capability 与 Swagger 已公开业务 Kafka `block|dead_letter` 和唯一 bounded replay API；Console 新建任务默认仍显式发送 `block`，不得依赖字段省略猜默认值。

业务 Kafka continuous task 的记录失败策略固定使用：

```json
{
  "runtime": {
    "boundary": "continuous",
    "record_failure": {"mode": "block"}
  }
}
```

`mode` 只允许 `block|dead_letter`。4B clean break 落地后，业务 Kafka 新建和更新任务必须显式提交该字段，Console 默认显式发送 `block`；不得把字段省略解释为兼容旧配置。数据库 CDC 固定为 `block`，不得提交 `dead_letter`。

DLQ v1 约束：

1. 只有业务 Kafka record 在 JSON object 解码、未知/缺失字段、key 提取或字段类型转换阶段产生的确定性数据错误可以进入 DLQ。source open/poll、目标 prepare/apply、数据库锁或连接、runtime lease/fencing、retention 越界、Infra Kafka/Infra PostgreSQL 错误必须失败并阻塞；它们不是可跳过数据。
2. 数据库 CDC 的 envelope/source/schema/protocol 漂移继续按 `schema_change_blocked` 处理，不能通过 DLQ 越过。Debezium tombstone、truncate、message 和未知 operation 也不得降级为业务 Kafka DLQ。
3. dead-letter identity 固定由 `apply_identity + source_identity + partition + record_offset` 计算，重复处理同一源记录必须得到同一 identity。Infra Kafka topic 固定使用 Transfer-owned `__addp_dlq.<tenant_id>.<task_id>` namespace，原业务 Kafka principal 不得访问。
4. DLQ envelope schema 固定为 `transfer.dead_letter/v1`，至少包含 dead-letter identity、tenant/task/execution、source identity、原 topic/partition/offset/timestamp、原 key/value/headers 的无损 base64、稳定错误 code/category/message 和 detected_at。不得在错误文本中写入连接凭据。
5. `transfer.dead_letters` 只保存查询和审计所需的控制索引、Infra Kafka payload reference 和首次/最近观测时间；原始大 payload 只保存在 Infra Kafka。控制索引按 dead-letter identity 唯一，重复写入不得产生第二条逻辑记录。
6. 处理顺序固定为：幂等写 DLQ payload -> 幂等落控制索引 -> PostgreSQL/MySQL/Oracle 目标以 `operation=skip` 原子推进各自业务目标 ledger -> Transfer 以 runtime fencing + state version CAS 推进 `transfer.sync_states`。只有前一步成功才能执行下一步；任一步失败都不得提交后续 position。
7. `skip` 只表示该 source position 已经由显式 dead-letter 策略审计跳过，不携带业务 row，也不修改目标业务表。目标 Provider 必须在同一事务校验 apply identity/source/target/partition 和单调位置后推进 ledger。
8. 目标 ledger 已推进但 Infra state CAS 前崩溃时，重试必须复用同一 dead-letter identity，目标 `skip` 必须按 monotonic ledger 幂等吸收，随后重新提交 Infra state；不得产生静默丢数或位置缺口。
9. DLQ topic 和控制索引在普通 pause/stop 后保留；task 删除和 System cleanup 由 Transfer cleanup owner 幂等删除。Infra retention 到期只删除 payload 后，控制索引必须明确显示 payload unavailable，不得伪装为可 replay。
10. 每个 task 的 DLQ topic 第一版固定为 1 partition，record key 固定为 dead-letter identity，`cleanup.policy=compact,delete`；compaction 收敛同一 identity 的物理重复记录，delete retention 形成真实 payload 可用边界。Transfer principal 只在 `__addp_dlq.` namespace 拥有 Create/Write/Read/Describe/DescribeConfigs，Connect 和业务 Kafka principal 不得访问；retention 与 replication factor 只属于部署配置，不进入任务 JSON。
11. DLQ 管理 API 必须挂在 owner task 下，唯一只读路由为 `GET /task-definitions/:id/dead-letters` 和 `GET /task-definitions/:id/dead-letters/:identity`。服务端必须先按认证租户校验 task ownership，再以 `tenant_id + task_id` 查询控制索引；不得提供跨 task 查询、全局 identity 直查或旧路由。
12. 列表使用统一分页响应 `{data,total,page,page_size,total_pages}`，默认按 `last_observed_at DESC, identity DESC` 稳定排序；只允许 `source_partition`、`error_category`、`error_code`、`payload_available` 精确过滤。`page_size` 最大 100，非法过滤或分页参数返回 `400 Bad Request`，不存在的 task 或不属于当前租户的 task 统一返回 `404 Not Found`。
13. 第一阶段列表与详情只返回安全控制事实：identity、source topic/partition/offset/timestamp、首次/最近 execution、稳定错误 code/category/safe message、payload availability、首次/最近观测时间和 occurrence count。不得公开 Infra Kafka payload topic/partition/offset、原始 key/value/headers、`apply_identity` 或租户内部字段；也不提供删除、编辑、重新分类、伪回放状态或以 DLQ 行直接创建 replay 的 API。
14. `payload_available` 只表达控制索引已确认的 payload 可用状态，不承诺无限保存。管理 API 不因读取列表或详情而扫描、消费或改写 Infra Kafka；payload retention/compaction 的可用性收敛由 Transfer-owned 独立治理流程负责，不能在请求链路中引入昂贵的逐条 Kafka 探测。
15. payload availability 治理必须由 Transfer continuous worker 内的独立低频 reconciler 承担，不进入 HTTP 请求链路，也不建立 consumer group、提交 offset 或创建第二套 replay source。reconciler 只扫描当前 `payload_available=true` 的控制索引，并按 identity 游标分批轮转，允许多实例重复核验但结果必须幂等。
16. 核验必须使用控制索引当前保存的 `payload_topic + payload_partition + payload_offset` 精确读取。只有 exact offset 的 record key 等于 dead-letter identity，且 `addp-schema=transfer.dead_letter/v1` 时才确认仍可用；topic/partition 不存在、offset 已越过当前 `[earliest,latest)` 或 fetch 已跨过该 compacted hole 时确认不可用。认证、网络、broker、超时或其他瞬时错误不得把 payload 标记为不可用。
17. 标记不可用必须使用 `identity + payload_topic + payload_partition + payload_offset + payload_available=true` 条件更新；重复观测若已写入新的 payload reference，旧核验结果必须因 CAS 不匹配而失效。该更新只修改 `payload_available` 和技术更新时间，不得改写 `first_observed_at`、`last_observed_at`、错误、execution 或 source 审计事实。
18. availability 核验可以在进程内读取 Kafka record key/header 以确认身份，但不得反序列化、记录日志、返回或持久化原始 value/key/headers 副本。公开 API 继续只返回安全控制索引；payload 从 `false` 恢复为 `true` 的唯一主路径是同一 source record 被 runtime 再次观测并成功写入新的 DLQ payload/reference。
19. 普通 pause/stop 和 System logical cleanup 必须保留 DLQ topic 与控制索引。用户直接删除任务和 System physical cleanup 必须走同一个 task-owned resource cleanup：数据库 CDC 先完成既有 capture cleanup；业务 Kafka task 随后由 Infra Kafka admin 幂等删除确定性 `__addp_dlq.<tenant_id>.<task_id>` topic；外部资源清理成功或确认不存在后，才允许进入唯一最终事务删除 task-private state 与任务定义。
20. DLQ topic 删除失败、权限不足或 Infra Kafka 不可用时，必须保留控制索引和任务定义并返回失败，不能先删数据库事实形成不可追踪的孤儿 Kafka topic。topic 已删除但控制索引或任务定义删除失败时，重试必须把 unknown topic 视为成功并继续数据库清理。
21. 当前为业务 Kafka continuous 的 task，无论 `record_failure.mode=block|dead_letter`，物理删除时都必须尝试删除确定性 DLQ topic；当前配置已改变但仍保留 `transfer.dead_letters` 索引的 task 也必须执行同一删除。只有既非业务 Kafka且不存在 DLQ 控制索引的 bounded/CDC task 才可跳过 Kafka cleanup，不能仅根据当前 `record_failure.mode=block` 猜测从未产生 DLQ。
22. task-owned resource cleanup 不删除目标业务表、目标行、业务目标 apply ledger 或 `common.task_executions` 历史。直接任务删除仍使用 owner 模块既有 soft-delete；System physical cleanup 使用 unscoped delete，但两者必须复用同一外部资源清理与最终数据库事务，不保留两条删除路线。直接删除运行中任务返回 `409 Conflict`；外部资源 cleanup 不可用或失败返回 `503 Service Unavailable`，响应不得泄露 Kafka、数据库或凭据细节。
23. 物理清理 continuous task 前，cleanup 必须先把 `desired_state` 原子设置为 `stopped`，取消尚未 claim 的 pending execution，再等待 active runtime owner 释放 lease 或 lease 到期。等待超时必须停止后续清理；不得在 worker 仍可能续租、写目标或提交 position 时删除 `runtime_leases` / `sync_states`。lease 已过期但 execution 仍为 pending/running 时，最终删除事务必须以 `stop_reason=cleanup` 收敛为 `cancelled` 终态并保留该 execution。直接 Delete 与 System physical cleanup 使用相同的统一 stop timeout/poll policy。
24. bounded task 没有真实 worker 中断能力。直接 Delete 继续对 `status=running` 返回 409；System physical cleanup 遇到 running bounded task 必须记录失败并保留任务及其私有状态，不能通过改数据库状态伪取消运行体。
25. capture/DLQ 外部资源成功清理且 continuous runtime 已停止后，必须在一个锁定 task definition 行的 Infra PostgreSQL 事务中删除该 task 的 `transfer.dead_letters`、`transfer.sync_states`、`transfer.runtime_leases`、`transfer.capture_resources`，并在同一事务 soft/unscoped delete task definition。事务中发现 task 已重新进入 running/desired running 或仍有有效 runtime owner/lease 时整体失败回滚，封住私有状态提交后并发 Start 重新创建运行事实的窗口。
26. `sync_states`、`runtime_leases` 和 `capture_resources` 是仅服务于仍存在 task definition 的当前运行控制事实，不是长期审计实体；任务删除后不得保留孤儿行。长期审计由保留的 `common.task_executions`、System audit 和 cleanup execution/result 承担。目标业务数据、业务目标 apply ledger、bounded replay 隔离结果和公共 execution 历史仍不随 task 删除。

bounded replay v1 约束：

1. replay 不是新的 `task_type`，也不修改任务定义。唯一 owner API 为 `POST /task-definitions/:id/replay`，请求只接受显式 per-partition `[start_offset,end_offset)` ranges 和一个不存在的新 PostgreSQL target endpoint；不接受 source、mapping、key 或主任务目标的任意覆盖。
2. replay v1 只支持业务 Kafka `record/json` task。source ranges 从原业务 Kafka topic 读取，提交前必须验证完整落在当前 retention `[earliest_offset,latest_offset]` 内；DLQ topic 不是 replay source。
3. replay 是独立 bounded execution，使用 execution-scoped apply identity 和独立目标 ledger，从 range start 顺序处理到冻结 end 后结束。它不读取或写入 `transfer.sync_states`、主 task `desired_state`、主 runtime lease、主 apply identity 或主目标 ledger。
4. replay target 必须是不存在的新 PostgreSQL table。仅有幂等 upsert/delete 不足以保证历史事件写回原目标时的时间顺序，因此原目标、已有目标、append 目标和“业务接受重复”均不得绕过隔离要求。
5. replay 不允许编辑 DLQ payload、替换历史 record、覆盖主 committed position或在成功后自动切换主任务 target。若要用 replay 结果替换业务数据，必须由独立、显式的数据发布或切换流程负责。
6. replay 可与主 continuous runtime 并行，因为 source 只读且 target 强制隔离；Transfer 必须仍按部署容量限制 replay 并发。replay failure 不改变主任务状态，不触发 continuous 自动恢复 circuit。
7. replay execution metadata 至少保存 source ranges、请求时 earliest/latest 快照、隔离 target identity、execution apply identity、每分区当前位置和最终统计。所有终态沿用统一 execution status。

Infra Kafka/Kafka Connect 部署基线（工作包 3A 已冻结，3B 实现）：

1. 版本固定为 Redpanda v24.3.18 和 Debezium Connect 3.6.0.Final；该 Connect 镜像内置 Kafka Connect 4.3.0。不得同时维护 Apache Kafka 或另一 Kafka 发行版作为 CDC 部署主线，任务/API/数据库也不得出现发行版选择。
2. 开发环境使用 1 broker 和 1 Connect worker，replication factor 为 `1`；生产参考使用 3 broker 和至少 2 Connect worker，replication factor 为 `3`，producer `acks=all`，由 Raft majority 提供确认 quorum=2。
3. Kafka Connect shared internal topics 固定为 `__addp_connect_configs`（1 partition）、`__addp_connect_offsets`（25 partitions）和 `__addp_connect_status`（5 partitions），均使用 `cleanup.policy=compact`；复制因子按开发 `1`、生产 `3`。
4. 任务级 CDC topic 固定命名为 `__addp_cdc.<tenant_id>.<task_id>.<capture_generation>`，第一版 1 partition、`cleanup.policy=delete`、默认 `retention.ms=604800000`（7 天）。生产必须同时配置并校验 `retention.bytes`；time/bytes 任一先到即构成真实恢复边界。
5. 容量规划至少使用 `峰值编码字节/秒 * 允许恢复秒数 * 副本因子 * 1.3`，并预留 Connect internal topics、segment 和 broker 高水位空间。备份不替代 connector offset + source WAL + Kafka retention 的连续恢复条件。
6. principal 固定分离为 infra admin、`addp-connect` 和 `addp-transfer`：infra admin 创建/配置/删除内部 topic 与 ACL；Connect 只读写 shared internal topics，并向 `__addp_cdc.*` 写入；Transfer 只描述/读取 `__addp_cdc.*` 和使用自己的 consumer group。业务 Kafka principal 不得访问 infra namespace。
7. 单机开发 Compose 只在本机端口和 Docker 网络使用 `SASL_PLAINTEXT/SCRAM-SHA-256` 以验证 principal/ACL；生产必须使用 `SASL_SSL`，密码从部署 secret 注入，Kafka bootstrap 和 Connect REST 都不得暴露到公共网络。
8. Kafka Connect 禁止依赖 broker auto-create 创建 CDC topic。capture supervisor 在创建 connector 前按规范显式创建 topic/ACL，在删除 connector 并确认停止后删除任务级 topic。shared internal topics 和 broker 数据目录由 Infra 部署 owner 管理，不随单个 task cleanup 删除。
9. PostgreSQL connector 固定使用 `plugin.name=pgoutput`、`publication.autocreate.mode=filtered`、单表 `table.include.list`、服务端生成的 slot/publication 名称和 `slot.drop.on.stop=false`。stop/cleanup 由 capture supervisor 在 connector 停止后显式删除 ADDP-owned slot/publication，不依赖 connector 退出副作用。

Manager 的快显和业务派生任务细节由 Manager 专题确认。本文只要求 Manager 用同一个 provider 声明多个任务类型，并按 `module + task_type + source_task_id` 关联执行记录。快显缓存生成任务类型为 `vector_tile_cache_generation`，任务定义表为 `manager.vector_tile_cache_tasks`，结果表为 `manager.vector_tile_cache`，结果是 Manager infra PMTiles artifact；业务矢量瓦片集生成任务类型为 `vector_tile_set_generation`，任务定义表为 `manager.vector_tile_set_tasks`，结果是 Business 存储中 `data_type=media + format=pmtiles + layout=single` 的 Meta item，不设 Manager 结果表。两者按源能力选择唯一生成路径：PostgreSQL/PostGIS 表由 Manager 原生 `ST_AsMVT` 生成，MySQL、Oracle 等标准 EWKB 可读数据库表由 Manager 物化临时 FlatGeobuf 后调用 GeoPython Workflow `vector_to_pmtiles`，NFS、MinIO/S3 文件或对象由受控访问计划调用同一 operator；MVT 是 PMTiles 内部 tile encoding，不是任务类型。矢量物化视图、栅格、三维、点云与 embedding 的既有任务边界保持不变。

Manager 已有结果动作统一适用于 `vector_tile_cache_generation`、`vector_materialized_view_generation`、`raster_cog_generation`、`model_3d_glb_generation`、`model3d_tiles_generation`、`gaussian_splat_ksplat_generation`、`point_cloud_copc_generation` 和 `cad_preview_generation`。这些任务的标准执行参数只允许 `existing_result_action:string`，当前枚举只有 `overwrite`；结果表中存在与任务语义身份对应的未删除结果时，服务端必须要求 `parameters.existing_result_action=overwrite` 才能刷新。该动作只作用于本次 execution，不改写 owner 任务定义。

上述八类 Manager 受管当前结果任务当前统一声明 `supports_schedule=false`，Manager 不为它们启动 owner scheduler；这不限制 Orchestrator 定时 Pipeline 调用。需要周期性刷新时，由用户在 Orchestrator Step 参数中显式配置 `existing_result_action=overwrite`，Orchestrator 每次调用原样提交。`embedding` 的独立逐 item 调度语义不在此限制内。

`raster_mosaic_generation` 与 `vector_tile_set_generation` 的结果是用户业务存储中的派生 data item，Manager 不拥有其生命周期，因此不使用当前结果覆盖确认；重跑按任务配置和目标数据集自身的幂等/恢复规则执行。`embedding` 的批量结果更新具有逐 item 跳过、过期重建等独立语义，也不使用本确认参数。这三者的具体任务详情必须返回空的闭合 `execution_contract.input_schema`，执行入口必须拒绝非空 `parameters`。

Manager 中矢量物化视图、瓦片缓存、COG、GLB、KSplat 等派生产物的 `ready`、`generating`、`stale`、`failed` 等状态属于 artifact state，不是统一 execution status。PreviewState 只保存预览偏好和交互视角，不保存 execution 状态。Manager 的即时向量化内存轮询状态虽不持久化为任务定义，但属于 execution-like 状态，成功态也必须使用 `success`，不得使用 `completed`。

Graph 的 `kg_build` 任务定义由 `graph.build_tasks` 保存。`graph.build_tasks.status` 是构建任务最近执行摘要，必须使用统一 execution status；成功态为 `success`。`graph.build_materials.status=completed` 属于材料处理状态，不是任务执行状态，不进入 TaskProvider 和 Monitor 的统一 execution 枚举。

Develop 的任务类型按开发方式划分为 `query`、`workflow`、`script`。`script` 表示命令式代码开发任务，当前可由 Jupyter Notebook runtime 承载；`notebook` 只是脚本开发的实现形态和 UI 入口，不作为独立 `task_type` 声明，不进入 TaskProvider capabilities。

Develop 的 `develop.dev_tasks.content` 必须使用规范字段：`query` 使用 `content.query` 和 `content.query_type`，执行目标统一写入 `execution_config.engine_id` 并指向 System 中具备 query 能力的真实 Engine；DuckDB 联邦查询绑定平台内置 DuckDB Runtime Engine，不使用独立模式字段或虚拟 Engine。`workflow` 使用 `content.workflow_definition` 和可选 `content.inputs`，执行目标只写 `execution_config.engine_id` 指向具体工作流运行时实例，不写 `execution_config.engine_type`，运行时类型必须由后端按该实例 ID 从 System 查询；`script` 的 Notebook 形态使用 `content.notebook_path`。Develop 的 ad-hoc 临时执行同样必须提交 `execution_config`，不得使用顶层 `engine_id` 表达查询目标。`/develop/engines` 统一返回 System 中具备 query 能力的真实 Engine Instance，不提供 Develop 私有查询模式或 `id=0` 虚拟 Engine。不得再新增或消费 `content.sql`、`content.workflow_def`、`content.input_data`、`execution_config.data_source_id` 等旧字段。

查询工作台的即时执行固定使用 `POST /api/v1/develop/executions` 创建 ad-hoc execution，写入 `module=develop`、`task_type=query`、`source=develop`、`source_task_id=null`，并在 `execution_config` 保存 `content`、目标 `engine_id`、语言和 timeout 快照。前端通过 `GET /api/v1/develop/executions/{execution_id}` 回查状态和结果。不得保留直接执行并同步返回结果的 `/develop/execute` 或其他旁路。查询结果只能保存受限预览，并明确记录 `result_limit`、`truncated` 和 capability 驱动的 `result_kind`；完整无界结果不得写入 execution metadata。

Develop 启动归一化和数据库迁移只允许删除上述旧字段，不得把 `content.sql`、`content.workflow_def`、顶层 `content.nodes/content.edges` 或 `content.input_data` 搬迁为规范字段；仍含旧字段的历史任务应按规范重新创建或由人工一次性处理。

Develop 的 `create_url` / `edit_url` 必须指向具体开发方式的专属页面：`query` 指向查询工作台，`workflow` 指向工作流编辑器，`script` 指向脚本开发当前承载页面。`/develop/tasks` 是唯一任务定义列表；不得再建立 `/develop/sql-tasks` 等按开发方式拆分的平行任务列表。`/develop/tasks` 不得作为 TaskProvider 的创建或编辑目标。

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

第一阶段应收敛到以下 endpoint 语义：

| 能力 | Endpoint |
| --- | --- |
| 列任务 | `GET /tasks?task_type=` |
| 查任务 | `GET /tasks/{task_type}/{id}` |
| 执行任务 | `POST /tasks/{task_type}/{id}/execute` |
| 查执行 | `GET /executions/{execution_id}` |
| 取消执行 | `POST /executions/{execution_id}/cancel`，仅当对应 `task_type.supports_cancel=true` |

后续实现应收敛到以上 endpoint 语义。取消接口不是必选能力；未声明支持取消的任务类型不得注册或展示取消入口。确需处理现有路径时，只能作为入口层迁移工作，不得形成长期双轨命名。

System 的模块注册入口接收 TaskProvider 声明时必须校验标准 endpoint：任务详情和执行 endpoint 必须包含 `{task_type}` 与 `{id}`，执行状态 endpoint 必须包含 `{execution_id}`，并且必须分别使用 `/tasks`、`/tasks/{task_type}/{id}`、`/tasks/{task_type}/{id}/execute`、`/executions/{execution_id}` 和 `/executions/{execution_id}/cancel` 标准后缀，不得使用 `/provider/tasks`、`/scan/runs/{execution_id}`、`/tasks/{id}/run` 等私有或旧路径。Orchestrator 调用 provider 时只替换 `{task_type}`、`{id}`、`{execution_id}` 三类标准占位符；模块私有 UI 或 CRUD 路径可以继续使用 `:id`、`:task_id` 等前端或 Gin 写法，但不得进入 TaskProvider endpoint 契约。

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

`GET /tasks/{task_type}/{id}` 直接返回 owner 模块的任务定义摘要对象。对象必须包含 `id`、`task_type`、`name`、`status` 和该具体任务的 `execution_contract`；多任务类型 provider 可以按 `task_type` 返回不同任务定义 DTO，但不得再包一层 `data`。`enabled`、`schedule`、`next_run_at` 只允许在该 task type 明确声明并实现 `supports_schedule=true` 的 owner 调度闭环时出现；不支持调度的 TaskProvider 不得暴露这些调度活状态字段。

Quality `check` 是纯手动/Orchestrator 显式执行类型，当前不保存或返回 `enabled`、`schedule`、`next_run_at` 等调度活状态字段。`execution_contract` 是具体任务可执行输入和稳定输出的唯一事实源：

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

不支持执行输入覆盖或没有稳定输出的任务仍必须返回对应的闭合空对象。`input_defaults` 只提供任务当前保存的工作流配置，不能使缺少必填定义参数的任务变成可保存任务；任务定义本身必须始终完整且可直接执行。`input_ui_schema` 只描述 `input_schema.properties` 中已声明字段的控件语义，不得增加输入字段或覆盖服务端 Schema 约束。需要稳定展示顺序时，字段和分组必须在对应 UI Schema 节点声明从 `0` 开始的 `order`；消费者必须按 `order` 排序，不能依赖 JSON 对象属性顺序。算子工作流的分组顺序使用稳定 DAG 拓扑顺序：上游算子在前，同层并行算子按任务定义 `tasks[]` 的保存顺序排列；分组内字段按公开参数声明顺序排列，不依赖画布坐标。`format=resource-locator` 的值在契约和请求中仍使用标准 ResourceLocator，但用户界面只能展示解析后的资源路径、名称和本地化类型，不得直接显示 `addp://` URI、Engine ID、`node_id` 或 `item_id`；无法解析时统一显示“已配置资源”，不得回退为原始内部值。

Develop 查询任务的 `content.query_parameters[]` 是查询参数定义事实源，每项固定包含 `name`、`type`、`default`，可选包含 `title`、`description`。参数名必须唯一且真实出现在当前查询语言的参数位置中；查询中的全部参数位置也必须有对应定义。`query_parameters[]` 的保存顺序决定 `input_ui_schema.<name>.order`，四种首期类型固定为 `string`、`integer`、`number`、`boolean`。Copilot 查询草稿可以提议同形状的 `query_parameters[]`，但它必须与候选文本在同一响应中一起校验和回填；只有用户保存后才成为 Develop 任务事实。查询任务详情必须从该定义派生非空 `execution_contract`；未定义查询参数时返回闭合空契约。即时查询在 `POST /api/v1/develop/executions` 顶层提交同语义的 `parameters` 覆盖，不能把执行值写回 `content.query_parameters` 或查询文本。

`GET /executions/{execution_id}` 直接返回统一 execution 对象，`execution_id` 必须是 `common.task_executions.execution_id`。

Owner 模块若需要保存下游运行时或外部系统返回的本地执行 ID，不得在 execution 结果摘要中再次使用 `execution_id` 字段，以免和统一执行 ID 形成双事实源。字段应按来源命名，例如 Develop 工作流运行时返回的本地执行 ID 保存为 `runtime_execution_id`，而不是覆盖或并列一个新的 `execution_id`。

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
8. 查询参数只允许绑定值。表名、集合名、字段名、排序方向、关键字和任意查询片段必须保留在任务查询定义中，不得通过参数动态替换。
9. 查询参数绑定必须由声明对应能力的 Query Runtime Provider 使用数据库驱动、原生参数 Map 或解析后的结构化对象完成；禁止 `strings.Replace`、模板插值或先格式化为查询字面量再执行。

执行成功受理时 HTTP 状态码必须为 `202 Accepted`，响应体必须返回本次执行的统一 `execution_id`。标准最小响应为：

```json
{
  "execution_id": "uuid",
  "status": "running"
}
```

执行响应不得使用 `{status,data}` 包装；`status` 字段表示 execution status，不表示 HTTP 请求成功或失败。

### 多任务类型 provider

多任务类型模块通过 `task_type` 分派到 owner 模块内部不同任务表和服务。

以 Manager 为例：

| provider | task_type | 任务定义表 | 执行 owner |
| --- | --- | --- | --- |
| `manager` | `vector_tile_cache_generation` | `manager.vector_tile_cache_tasks` | Manager |
| `manager` | `vector_tile_set_generation` | `manager.vector_tile_set_tasks` | Manager |
| `manager` | `vector_materialized_view_generation` | `manager.vector_materialized_view_tasks` | Manager |
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

## Orchestrator 规范

Orchestrator 拥有编排定义，不拥有业务任务定义。

Orchestrator v1 的 Step 只支持引用已由 owner 模块创建的持久任务定义，形态固定为 `provider + task_type + task_id`。内联执行不是 v1 Step 模式；后续如需要，必须先作为 Orchestrator 执行模型 v2 专题修订 capabilities、endpoint、Step 模型、状态机和审计语义。

Step v1 只允许以下字段：

| 字段 | 说明 |
| --- | --- |
| `id` | Step 稳定标识 |
| `name` | Step 展示名称 |
| `provider` | TaskProvider 模块名 |
| `task_type` | provider 声明的任务类型 |
| `task_id` | owner 模块内的任务定义 ID |
| `parameters` | 本次执行时指定的覆盖值或上游声明输出绑定；未出现的字段使用 owner 任务定义中已保存的工作流配置 |
| `depends_on` | 显式控制依赖和数据依赖 |
| `timeout` | Step 执行超时时间，单位秒 |

约束：

1. 默认不支持 Orchestrator 创建 owner 模块持久任务。
2. 用户需要新建持久任务时，应跳转到 owner 模块声明的 `create_url`。
3. 编排触发下游任务时，必须传 `source=orchestrator`。
4. 编排触发下游任务时，`trigger_type` 只能是 `manual` 或 `scheduled`。
5. Orchestration 自身执行记录 `module=orchestrator`、`task_type=orchestration`。
6. Orchestrator 子步骤 execution 必须写 `parent_execution_id`。
7. 保存和执行编排时，Orchestrator 必须校验 `provider + task_type` 已由 capabilities 声明且未 deprecated，并从具体任务详情取得 `execution_contract`，按其 `input_schema` 校验 Step `parameters` 的 `required`、类型、`enum`、额外字段、长度、数量和数值范围。保存阶段仅允许完整的声明输出绑定作为延迟值；执行阶段必须重新取得最新契约、校验输出路径与输入类型，在绑定解析后使用真实值再次严格校验，再调用 owner。schema 实例校验错误必须保留稳定 rule、参数 path 和约束值，由 Orchestrator Service 包装 Step 上下文，HTTP 层按 `Accept-Language` 映射为用户可读消息，不得直接把公共校验器的英文诊断作为 API 文案。该平台校验不替代 owner 执行入口的最终领域校验。
8. Orchestrator 可以把已保存的编排定义作为 `orchestration` 任务暴露给任务库，但保存编排时必须校验编排定义之间的引用图，不得直接引用自身，也不得通过多层 `orchestrator/orchestration` 引用形成递归执行。
9. Step 的 `depends_on` 表示当前步骤依赖的前置步骤，也是上游输出绑定允许读取的显式数据依赖范围；执行器必须先执行依赖步骤，再执行当前步骤。缺失依赖、循环依赖、隐式输出依赖或自引用必须导致编排保存或执行失败，不得静默跳过。
10. 上游输出绑定只支持内部序列化 `{{step_id.outputs.declared.path}}`；路径必须由来源任务的 `output_schema` 声明并与目标 `input_schema` 类型兼容，不支持局部字符串插值，也不做隐式类型转换。运行时路径解析不到值时，当前 Step 必须失败。
11. v1 不支持并行执行、分支或条件、Step 级重试策略、人工确认步骤，也不得通过 `condition`、`retry`、`approval`、`parallel`、`branch` 或其他私有 Step 字段提前打开这些控制流能力。后续如需要，必须作为 Orchestrator 执行模型 v2 专题设计，明确状态机、失败语义、资源隔离、审计、UI 表达和迁移边界。
12. 一个 Step 可以依赖多个上游 Step，只有全部直接依赖成功后才进入可执行状态；一个 Step 也可以被多个下游 Step 依赖。参数连线隐含执行依赖，同一任务对存在参数连线时不得再保存或显示重复的纯控制连线；同一任务对的多条参数连线共享一个去重后的 `depends_on` 项。纯控制连线和参数连线必须共同参与环路检测。一个输入最多绑定一个上游输出，一个输出允许连接多个下游输入。
13. Monitor 回跳任务定义时应使用 TaskProvider capabilities 中对应 `task_type.edit_url`，不得硬编码 `module + task_type` 映射。
14. Orchestrator Create/Update 必须使用严格 JSON 解码并拒绝未知字段；Step 结构、DAG、模板依赖、TaskProvider 引用、编排递归引用和调度表达式校验必须返回稳定的结构化领域错误，由同一 Handler 校验路径按 `Accept-Language` 映射为 `{error}` 响应，不得直接暴露 Go、JSON、Cron 或 Repository 的原始英文错误。
15. 编排定义和执行记录是 Tenant 资源。用户 HTTP 请求必须使用 System AuthContext 的 `tenant` 会话模式和唯一当前 Tenant；服务间调用必须使用调用模块自己的 Confidential OAuth Client，通过 Client Credentials 获取 Tenant Service Access Token，并只发送 Bearer。`platform` 模式、客户端 query/body/header `tenant_id` 和缺失 Tenant 上下文均不得解释为默认 Tenant 或全 Tenant 访问。Create/Update 请求只允许用户可编辑字段，Get/Update/Delete/Execute 和执行查询必须在 Repository 或统一执行仓储中同时限定资源 ID 与 Tenant ID；跨 Tenant 访问统一表现为资源不存在。

### 编排调度与子任务自身调度

Orchestrator 的调度和 Step 引用任务的自身调度不是继承关系，也不是覆盖关系。

核心语义：

1. 编排调度只决定 Orchestration run 何时启动。
2. 子任务自身调度只决定该 owner 任务作为独立任务何时自动执行。
3. Orchestrator 执行 Step 时，只消费 `provider + task_type + task_id` 指向的任务定义和 Step 参数，不读取、不触发、不修改该任务定义上的 `schedule` / `enabled` / `next_run_at`。
4. 一个任务定义同时被自身调度和某个编排引用时，两者是两个独立执行入口；不得将其视为重复调度错误。
5. 编排触发子任务产生的 execution 必须通过 `source=orchestrator` 和 `parent_execution_id` 表达编排上下文；子任务自身调度产生的 execution 不应写该编排 execution 为父执行。
6. 如果用户不希望某个任务被自身计划自动执行，应在 owner 模块关闭该任务自身调度；Orchestrator 不负责替用户关闭或屏蔽被引用任务的 owner schedule。

示例：

- Transfer 任务 A 自身配置每天 01:00 执行。
- Orchestration B 自身配置每天 03:00 执行，且某个 Step 引用 Transfer 任务 A。

则系统应产生两个独立入口：

| 时间 | 入口 | 语义 |
|---|---|---|
| 01:00 | Transfer owner scheduler | A 作为独立 Transfer 任务执行 |
| 03:00 | Orchestrator scheduler | B 启动一次编排 run；执行到对应 Step 时调用 A 的执行能力 |

这两次执行可以使用同一个 owner 任务定义 A，但 execution 上下文不同。前者没有编排父执行；后者必须关联 B 的 execution。

## 调度规范

调度定义归任务 owner 模块。

| 对象 | 责任 |
| --- | --- |
| owner task | 保存 `schedule`、`enabled`、`next_run_at`、`last_run_at` |
| owner scheduler | claim due task、创建 execution、投递 worker |
| common scheduler | 提供 Cron 解析和进程内调度工具 |
| common execution | 记录调度触发产生的一次 execution |

长期应以 DB-driven due task claim 为主。进程内 Cron 只作为触发器或辅助工具，避免多实例、重启恢复、漏跑补偿和调度审计问题。

调度实现边界：

1. `common/scheduler` 是业务代码使用 `robfig/cron` 的统一封装边界。模块需要 Cron 校验、下次执行时间计算或轻量进程内维护任务时，应通过 `common/scheduler` 调用；业务模块不得直接注册 `cron.New` / `AddFunc`。
2. DB-driven due task claim 的多实例主路线以 PostgreSQL 事务内行锁为准，推荐使用 `FOR UPDATE SKIP LOCKED` claim 到期任务并在同一事务中推进或占用任务；SQLite 单测只验证基础计算和推进行为，不作为多实例锁语义来源。
3. 新增、删除或变更任何 `supports_schedule=true` 的 task type 时，必须同步巡检 owner 任务定义是否具备完整调度闭环，并更新 capabilities、Swagger/API 文档和用户可见调度入口。
4. 审计日志归档属于 System 固定系统维护任务，不属于 owner task schedule。它可以使用配置驱动的进程内调度，但必须复用 `common/scheduler`，只使用 infra MinIO 的 system bucket，不使用 System 引擎管理中的业务对象存储；归档路径按 `tenant_{id}/audit-logs/...` 组织，平台级日志归入 `tenant_0`。

平台级约束：

1. 用户可配置、可持久化、可重复执行的任务定义型调度，默认必须采用 DB-driven due task claim 路线。
2. `common/scheduler` 的平台主职责是 Cron 校验、下次执行时间计算和必要的进程内辅助触发；不得把“各模块各自维护内存任务注册表”作为长期主路线。
3. 只要某个 task type 对外声明 `supports_schedule=true`，其 owner 任务定义就必须把 `schedule`、`enabled`、`next_run_at` 作为活状态字段参与完整调度闭环。
4. 所谓完整调度闭环，至少包括：保存/更新任务定义时校验 `schedule`、计算并回写 `next_run_at`；scheduler 按 `next_run_at` claim due task；触发 execution 后推进下一次 `next_run_at`；任务关闭或 schedule 清空时清空 `next_run_at`。
5. 不允许长期保留“表结构里有 `next_run_at`，但运行时不回写、不 claim、不以其为调度事实源”的半统一状态。
6. 某个 task type 在未具备 owner scheduler、`next_run_at` 回写和 due task claim 闭环前，必须声明 `supports_schedule=false`；不得只因为模型上预留了 `schedule` 字段就对外宣称支持定时。
7. 固定系统任务、启动期保活任务或纯进程内维护任务，如果不属于用户可配置的任务定义型调度，可以继续使用配置驱动或轻量进程内 Cron；但这类任务不得伪装成 owner task schedule，也不得与任务定义型调度并称为同一条技术路线。
8. 固定系统任务如果后续演进为用户可配置、可审计、可监控的任务定义，必须迁移到 owner task + owner scheduler 的统一模型，不保留配置 Cron 与任务定义 Cron 双轨并存。
9. 用户可见的调度配置入口必须复用共享前端调度能力，默认使用 `common-frontend` 的 `ScheduleConfig` / `ScheduleDisplay` 或其在同一共享能力上的等价封装。
10. 某个模块存在秒级 Cron、策略载荷转换或行业特定预设等特殊需求时，应扩展共享调度能力，而不是在模块内私写一套独立的调度 UI、Cron 校验和描述逻辑。
11. 平台级持久任务定义字段统一使用 `schedule` 表达 Cron 字符串；前端策略字段如 `schedule_mode`、`schedule_time`、`schedule_value`、`cron_expression` 只能作为交互载荷或转换中间态，不得替代 owner 任务定义上的 `schedule` 事实字段。

## Monitor 规范

Monitor 不拥有任务定义。Monitor 聚合观察：

| 层级 | 来源 | 监控内容 |
| --- | --- | --- |
| execution | `common.task_executions` | 状态、耗时、进度、错误、父子关系 |
| schedule | owner 任务状态 API | 是否启用、下一次运行、最近运行摘要 |
| runtime queue | owner 模块队列状态 API | pending、active、retry、dead、延迟 |
| artifact state | owner 模块状态 API | 产物是否 ready、缓存位置、版本、失败原因 |
| provider health | System TaskProvider / module health / 标准任务列表 endpoint | 模块是否声明 TaskProvider 角色、当前是否可调用、无副作用任务发现 endpoint 是否可访问 |

Monitor 可以查询 owner 模块公开的只读状态 API，但不得直接依赖 owner 私有表结构。provider health 不新增 TaskProvider 专用 health endpoint，应复用模块 `/health` 与标准 `GET /tasks?task_type=` 这类无副作用 endpoint 做探活。

provider health 至少检查以下内容：

| 检查项 | 来源 | 说明 |
| --- | --- | --- |
| registration | System `module_definitions.task_provider` | 模块是否声明 provider 并具备基础 endpoint。 |
| capabilities | System `module_definitions.task_provider.capabilities` | JSON 是否可解析、`schema_version` 是否为 `task.capabilities/v2`、`task_capabilities[]` 是否非空。 |
| backend_health | 每个 `provider.backends[].base_url + /health` | 当前有效端点池中的每个 Backend 实例是否可访问。 |
| task_discovery | 每个 `provider.backends[].base_url + task_list_endpoint + ?task_type=` | 每个 Backend 实例上、每个未 deprecated task type 的标准任务发现 endpoint 是否可访问，且响应体必须是标准任务列表对象，包含 `items`、`total`、`page`、`page_size`。 |

System 返回 `available=false` 或空 `backends[]` 时，Monitor 直接判定 Provider `down`，不对空地址发起探测；`available=true` 时逐个检查当前端点池，再聚合 Provider 状态。provider health 状态只使用：

| 状态 | 说明 |
| --- | --- |
| `up` | 所有 Backend 实例的全部检查通过。 |
| `degraded` | capabilities 非法，或部分 Backend 实例 / task type 检查失败。 |
| `down` | 没有有效 Backend 租约，或全部 Backend 实例不可访问 / 任务发现全部失败。 |
| `unknown` | 无可检查 task type，或 System 注册信息暂时无法获取。 |

Monitor 探测任务发现时只发送 `GET` 请求，不读取 owner 私有表，不触发执行，不创建 execution。deprecated task type 不作为可用任务类型处理，不进入健康失败统计。

Monitor 必须支持按执行记录查询 parent / child execution 树。树边使用 `common.task_executions.execution_id` 与 `parent_execution_id` 连接。Monitor 只读取 `common.task_executions`，不得回查 owner 私有表拼装树。

cleanup execution 属于系统运维执行记录。Monitor 必须能展示 cleanup 父子 execution 树、scan / execute 关联、触发方式、清理模式、影响记录数、释放空间、错误摘要和各模块结果；但不得提供 Orchestrator 编排入口，也不得把 cleanup 当作 TaskProvider 任务定义处理。cleanup 的正式语义见 [ADDP Cleanup 体系规范](addp-cleanup体系规范.md)。

入口包括：

| 入口 | 用途 |
| --- | --- |
| `GET /api/v1/monitor/executions/{id}/tree` | 按统一执行记录自增 `id` 查询，适合 Monitor 表格内部跳转 |
| `GET /api/v1/monitor/executions/by-execution-id/{execution_id}/tree` | 按全局 UUID 查询，适合各模块拿到 `execution_id` 后跳转统一监控 |
| `GET /api/v1/monitor/providers/health` | 查询所有 TaskProvider 的运行态健康。 |
| `GET /api/v1/monitor/providers/{module}/health` | 查询单个 TaskProvider 的运行态健康。 |

前端模块拿到执行响应中的 `execution_id` 后，应使用 `common-frontend` 的 `openMonitorExecution(execution_id)` 进入统一监控页。该工具会优先通过 Console iframe bridge 切换父级路由到 `/monitor/executions?execution_id=...`，独立运行时再回退为在新窗口打开 Console 路由，避免覆盖当前业务模块页面。业务模块不得自行硬编码 Console 端口或拼装跨模块 iframe URL。

## 模块接入检查清单

模块的持久任务定义要纳入 TaskProvider 和 Orchestrator，至少满足：

- 有明确任务定义 owner。
- 持久任务定义表只保存定义和最近执行摘要。
- 每次执行写入 `common.task_executions`。
- `trigger_type` 只写 `manual` / `scheduled`。
- `source` 写触发来源。
- `task_type` 稳定并声明到 TaskProvider capabilities。
- 多任务类型模块使用一个 provider，并在 `task_capabilities[]` 中声明多个任务类型能力。
- execution 能按 `module + task_type + source_task_id` 回查任务定义。
- ad-hoc execution 保存完整 `execution_config`。
- Swagger 和模块文档同步。

ad-hoc-only execution 不要求存在任务定义或声明 TaskProvider capability，但必须有明确 execution owner、稳定 `task_type`、完整 `execution_config`、统一 execution 状态和 owner API 文档。没有 `source_task_id` 时不得伪造任务详情回查能力。

## 与相关文档的关系

- Meta 扫描任务细节见 [元数据扫描机制规范](addp元数据扫描机制规范.md)。
- Transfer 稳定任务语义以本文、[Transfer 任务语义与同步模式](../../transfer/docs/transfer-任务语义与同步模式.md) 和 [Transfer 模块基本概念及配置说明](../../transfer/docs/transfer-基本概念及配置说明.md) 为准；尚未实现的能力统一记录在 [Transfer 后续能力清单](../next/transfer后续能力清单.md)。
- Manager 派生产物任务内部语义后续以 Manager 专题为准。
