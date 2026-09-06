# Transfer 模块说明

## 模块定位

Transfer 模块是 ADDP 的数据传输中枢，统一负责 `sync` 任务、任务配置、字段映射 / 转换编排、写后 Meta 扫描触发和执行运行时。bounded execution 由独立 `transfer-bounded-worker` 通过 PostgreSQL execution claim 承担；continuous execution 由独立 `transfer-continuous-worker`/supervisor 承担。导入 / 导出是 Manager、Develop 等调用方的用户动作语义，不是 Transfer 的 `task_type`。可复用同步配置才创建 `transfer.transfer_tasks`；一次性导出使用 `POST /api/v1/transfer/executions` 直接创建 `source_task_id=null` 的 bounded `sync` execution，响应只暴露统一 `execution_id`。

当前主路径基于 `common/engine`、`common/format`、`common/contentio` 和 `common/engine/contentadapter`：

- Transfer 负责任务 JSON、planner、policy、transform、worker、checkpoint、日志、指标和写后 Meta 扫描触发。
- 一次性执行入口只允许已认证且具备不可委派 `transfer.execution.create` Permission 的模块 Service Client 调用，execution 的 `source` 必须由 `addp-<module>` Client ID 推导，调用方请求不得自报 `source_module`。同一模块使用不可委派 `transfer.execution.read` 回查自己创建的 execution，Transfer 必须再次按 Client ID 校验 `source`，不能借此读取其他模块或用户任务 execution。入口接受强类型 source / target / fields 契约，并在 Transfer 内转换为唯一 planner 配置；查询调用方必须把已解析的全部关系输入按稳定顺序写入 `source.query.inputs[]`，Transfer 校验其标准 ResourceLocator 与 source Engine 一致并据此记录完整血缘，不解析查询文本反推资源。调用方不得创建临时 Transfer task，也不得保存 Transfer task ID。bounded worker 必须同时执行有任务定义和无任务定义的 `sync` execution，并只在 `source_task_id` 存在时更新任务摘要。
- 具体 engine-native 读写由 `common/engine` 提供。
- 具体格式和数据类型读写由 `common/format` 提供。
- content 的定位、读取、写入、range 和 scope list 由 `common/contentio` 表达；multi ref 的组织规则和读写语义由 `common/format` / `common/dataitem` / Transfer 编排层表达；engine content provider 到 contentio 的桥接由 `common/engine/contentadapter` 提供。
- 旧 Transfer 私有 reader / writer 插件体系、旧 `pkg/pipeline`、旧 `pkg/plugin_loader` 不作为新功能入口。

table 类型 Transfer 主链路已经稳定：native table、encoded single file/object、encoded multi refs 和 encoded whole scope 都统一走 planner + executor + common provider，不按具体引擎组合建立专用通道。后续新增 table 能力应优先补 `common/engine` 或 `common/format`，不要在 Transfer 内恢复私有 reader / writer。

Security 对 Transfer 的稳定动作名为 `export`，它表示受保护数据离开源 DataItem，不是 Transfer `task_type`。`bounded + snapshot` 的统一 Native TablePipeline 必须在字段映射、类型转换、空间处理和目标 writer 前遮盖/抑制；原生表不按 `engine_type` 建立保护分支，统一按精确 Locator 与当前表结构校验并处理 `BatchData`。查询源必须从同一 PreparedQuery 取得 ReadSet 和 OutputLineage；当前已验证 PostgreSQL 可证明查询，以及只包含 `$match`、`$unwind`、`$project`、`$sort` 透明阶段的 MongoDB aggregate，其他无法证明完整输出血缘的查询继续失效关闭。MongoDB collection 到 `mongodb_extended_jsonl` 的原始记录格式导出按精确 Locator 与 Meta 当前字段结构校验同一 `export` 投影，在 Provider 保留 BSON 标量的文档对象上完成保护后才编码 Canonical Extended JSON。Security 生成引擎无关的动作投影，Transfer 只在已实现并验证字段级保护执行器的执行形态上开放；raw copy、watermark incremental、Kafka replay、encoded source、continuous/CDC 以及其他无法证明完整字段身份和输出血缘的查询，在各自执行器完成前仍资源级失效关闭。

只读原生查询结果到 table 的 bounded 搬运属于同一主链路：source 必须消费 `common/engine.QueryReadSessionProvider`，target 继续消费 table write session。MongoDB 嵌套 BSON 的路径投影、数组展开、去重和关系合并由只读 MQL 在源端完成；Transfer 只搬运最终扁平行、执行显式字段映射和严格类型转换，不提供递归 JSON 自动摊平器。

MongoDB 查询的 Console 基础结构整形构建器只是一种 MQL authoring 能力：通用支持 `可选单次 $unwind -> $project`，保存事实仍只有 `source.query` 中的标准 MQL command object。基础界面只决定文档/单数组的行粒度和源字段选择；单数组模式只允许展开一个数组，可以选择多个数组元素叶子字段，并可选择多个不位于任何数组下的父文档叶子字段随每个元素行重复携带；文档标识自动携带。查询输出名由编译器确定，PostgreSQL 目标名称和类型只在后续 `field_mapping` 配置。构建器不得暴露筛选、排序、空数组保留、投影别名等 MQL 拼装细节，不得保存第二份 DSL、硬编码业务 collection/字段、猜测多数组粒度或承担业务聚合；超出可逆子集的合法查询进入高级 MQL 编辑器。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8083`，环境变量 `TRANSFER_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5176`，启动脚本环境变量 `TRANSFER_FE_PORT`。
- 数据库：PostgreSQL `transfer` schema。
- 依赖：System、Meta、PostgreSQL、Redis、MinIO/S3。

## 重要目录

```text
transfer/
├── authorization/
│   └── permissions.yaml       # Transfer Permission Manifest，发布期聚合事实源
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # tasks、executions、engines、capabilities
│   ├── internal/planner/      # source/target endpoint -> table transfer plan
│   ├── internal/executor/     # 基于 common engine/format/contentio 的 table transfer executor
│   ├── internal/service/      # task、execution、system engine resolver、Meta scan 触发
│   ├── internal/worker/       # PostgreSQL bounded runner；owner scheduler 位于 Backend
│   └── pkg/vfs/               # 底层虚拟文件辅助能力，非新 transfer reader/writer 主入口
├── docs/
│   ├── 数据库架构.md
│   ├── transfer-基本概念及配置说明.md
│   ├── transfer-任务语义与同步模式.md
│   └── tables/
└── frontend/src/
    ├── views/                 # TaskList、TaskWizard、ExecutionList、TaskDetail
    ├── components/
    └── api/
```

## 核心 API

Transfer 是 `transfer.execution.*`、`transfer.task.*` 和 `transfer.task_provider.*` 的 Permission owner；定义只存在于 `authorization/permissions.yaml`，通过 `common/authorization` 发布期聚合，不在服务启动时动态注册。模块 Runtime 创建一次性执行使用不可委派的 `transfer.execution.create`，按来源隔离回查结果使用不可委派的 `transfer.execution.read`；用户任务管理只使用 `transfer.task.*`；Orchestrator Runtime 只使用 `transfer.task_provider.read|execute`，并由固定 `addp-orchestrator` Service Client Guard 收窄。`transfer.task.cancel` 是 IAM 目标目录能力，当前真实执行取消仍未实现，首次 SQL seed 前必须通过路由覆盖门禁处理。

路由前缀：`/api/v1/transfer`。

- 公共连通：`GET /ping`。
- 资源选择与资源树：统一使用 Meta resource-tree / item API；Transfer 不保留私有数据源树、节点 children 或表 metadata 代理接口。
- TaskProvider 标准任务：`GET /task-provider/tasks`、`GET /task-provider/tasks/:task_type/:id`、`POST /task-provider/tasks/:task_type/:id/execute`、`GET /task-provider/executions/:execution_id`，其中 `task_type` 固定为 `sync`；四个端点不接受用户任务权限。
- 任务定义：`GET /task-definitions`、`POST /task-definitions`、`GET /task-definitions/statistics`、`GET /task-definitions/:id`、`PUT /task-definitions/:id`、`DELETE /task-definitions/:id`、`POST /task-definitions/:id/start|pause|resume`、`GET /task-definitions/:id/executions`。`pause/resume` 只控制 owner schedule，不中断 active execution。
- “传输任务”列表页提供传输任务创建助手，由 Copilot `/api/v1/copilot/transfer/generate` 识别源资源意图并给出候选。唯一候选也必须由用户确认；之后在助手内依次确认目标引擎、目标父位置、目标表、字段映射和任务配置。MySQL 新表的 decimal 精度与小数位复用 Transfer 字段定义推荐 API 基于源数据生成并展示确认。Copilot 接口不创建或启动任务，最终仍使用本模块 `task-definitions` API 和 `transfer.task.create` 权限。
- 数据库 CDC 结构变更：`GET /task-definitions/:id/schema-change` 查询当前请求，`POST /task-definitions/:id/schema-change/approve` 人工审批 additive migration。
- 业务 Kafka DLQ 只读管理：`GET /task-definitions/:id/dead-letters`、`GET /task-definitions/:id/dead-letters/:identity`。只公开 tenant/task scoped 安全控制索引，不返回 Infra Kafka payload reference 或原始 key/value/headers。
- 字段映射：字段映射写入 `config.transforms[type=field_mapping]`，不提供独立 mappings 主路径。
- 对象存储目录选择统一走 Meta resource-tree；Transfer 不再保留私有 object-storage 浏览 API。
- 执行记录：用户管理接口为 `GET /executions`、`GET /executions/statistics`、`GET /executions/:execution_id`；模块一次性 execution 的来源隔离回查接口为 `GET /executions/:execution_id/result`。
- 执行管理：`POST /executions/:execution_id/retry`、`GET /executions/:execution_id/progress|logs` 按统一 `execution_id` 定位执行记录；当前没有真实 worker 中断能力，因此不提供 cancel/stop API，TaskProvider 保持 `supports_cancel=false`。
- 转换器：`GET /transforms`、`GET /transforms/stats`、`GET /transforms/:name`、`POST /transforms/:name/validate|test`。

## 执行规则

- 新任务配置必须使用 `runtime.boundary`、`load.mode`、source / target endpoint 和 `target.policy.apply_mode` JSON；旧顶层 `mode`、`write_mode`、`connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等字段出现即拒绝。
- Transfer 任务类型固定为 `sync`；不得新增或兼容 `import`、`export`、`transfer` 等旧任务类型。
- table transfer 统一走 `internal/planner` + `internal/executor`，按 data type / representation / layout 分叉，不按具体引擎组合分叉。
- encoded file/object 读写必须通过 `common/engine` content provider + `common/engine/contentadapter` + `common/format` provider，不在 Transfer 中新增私有 reader / writer。
- Shapefile 等 multi 文件格式通过 `contentio.Reader` / `contentio.Writer` + `[]format.RelatedRef` 与 `common/format` multi table provider 接入。
- FileGDB/PGeo 等 container table 源只接受通用 `source.options.child_name`；planner 必须从 Meta `type_info.container.children` 校验 child 并解析 `native.table`，任务配置不得直接保存 `layer` 或 `child_table` 等 provider 私有入口。
- overwrite / append 是 Transfer policy；删除指定资源由 `common/engine` ResourceDeleteProvider 提供。
- checkpoint 当前只用于进度展示、故障定位和 provider marker 观测；失败执行 retry 按 restartable 从头重新入队，append 任务 retry 会被拒绝。不得宣称 table Transfer 已支持 checkpoint resumable。
- 大数据传输要优先考虑批大小、连续读取 / 写入 session、进度日志和 restartable retry。
- bounded Worker 不使用消息载荷；以 `common.task_executions` 中冻结的执行配置为唯一输入，并通过 `lease_token` 条件写入。
- bounded query-source 可声明通用“写入已存在表”目标：任务定义只保存 source、字段映射和转换，TaskProvider 将 `target_locator` 声明为必填运行时输入。Worker 通过通用 Engine capability 校验已存在表结构，严格校验 field mapping 的目标列名称、顺序和类型，再仅用 `TableWriteSessionProvider` append 本 execution 结果；不得调用 delete、truncate、prepare 或 DDL。稳定输出为 `execution_id + target_locator + row_count`。该模式 lease 过期后不自动重试，由上层重新发起完整编排。Transfer 不保存 LogicalTable ID，不调用 Model API，不持有 Model Permission。
- 所有 bounded `sync` 任务统一声明稳定输出 `execution_id + target_locator + row_count`。固定目标任务使用闭合空输入契约，目标 locator 从 execution 冻结的任务目标配置解析；只有执行期目标任务要求输入 `target_locator`。创建子资源时必须保留父 locator 体系，`addp://engine/...` 与 `addp-infra://...` 分别使用各自解析器，Infra 目标不得经过业务 ResourceLocator 解析。下游只能消费稳定输出，不能读取 Transfer 私有配置或拼接物理路径。
- 所有 TaskProvider 稳定输出只写入 `common.task_executions.metadata.outputs`；`GET /task-provider/executions/:execution_id` 使用 `common/taskprovider` 标准状态响应把同一对象投影为顶层 `outputs`。用户执行详情继续使用 Transfer DTO，不复用或包装 TaskProvider 机器契约。
- 工作包 2B/2C 已实现业务 Kafka keyed JSON record -> PostgreSQL/MySQL monotonic upsert；数据库 CDC 已在同一 continuous worker 主循环中实现 PostgreSQL/MySQL/Oracle Debezium envelope -> PostgreSQL/MySQL/Oracle snapshot/upsert/delete。两条路线共用 `ChangeStreamReaderProvider`、partition position、目标 ledger、Infra state CAS、runtime lease/fencing 和 retention 防护；不得新增第二套 CDC consumer。
- continuous resume 前必须验证 committed `next_offset` 仍在 Kafka 保留范围内；低于 earliest offset 时明确失败，不能静默跳到 earliest。PostgreSQL/MySQL/Oracle 目标被锁时必须响应 context 取消并回滚业务写入与 apply ledger；Oracle 通过 `FOR UPDATE NOWAIT` 有界重试检查 runtime context。
- 工作包 4B 已完成：业务 Kafka DLQ 按确定性 record error -> Infra Kafka payload -> `transfer.dead_letters` -> 目标 `skip` ledger -> Infra CAS 运行；公开任务 API 接受显式 `runtime.record_failure.mode=block|dead_letter`，Console 默认显式发送 `block`。唯一 replay API 为 `POST /task-definitions/:id/replay`，只接受显式 partition offset ranges 与不存在的新 PostgreSQL `parent_locator + name`，并通过独立 bounded execution/apply identity 写隔离目标，不能触碰主任务状态、水位或目标。
- 工作包 4D 的 DLQ payload availability 治理由 continuous worker 内唯一 reconciler 承担：按 identity 游标分批核验当前 Infra Kafka exact topic/partition/offset，只有 topic/partition/offset 明确消失或 exact record 身份/schema 不匹配时才以 payload reference CAS 标记 unavailable；网络、认证和 broker 错误保持原状态。reconciler 不解码/记录 payload、不提交 Kafka offset，也不进入 HTTP 请求链路。
- task 直接删除与 System physical cleanup 必须复用同一 task-owned resource cleanup：CDC capture（如适用）→ 业务 Kafka 确定性 DLQ topic → tenant/task scoped `transfer.dead_letters` → task definition。logical cleanup、pause/stop 保留 DLQ；Kafka 删除失败时不得先删索引或任务。
- physical cleanup 在删除私有状态前必须把 continuous task 收敛到 desired stopped 并等待 active lease 释放；running bounded task 无真实 cancel 能力，必须拒绝删除。外部资源清理后，`dead_letters/sync_states/runtime_leases/schema_change_requests/capture_resources` 在同一事务删除，公共 execution 和目标业务事实继续保留。
- continuous worker 从同一 ChangeStreamReader 采集分区 earliest/latest，用 Transfer committed `next_offset` 计算 lag、recovery headroom、source rate 和 retention horizon，并将 `healthy|degraded|critical|unknown` 写入 execution metadata。Monitor 只展示该 metadata，不直连 Kafka 或 Transfer 私表。
- 工作包 3A 已冻结 PostgreSQL 单表 CDC v1 契约，3B 已完成 Infra Kafka/Kafka Connect，3C 已实现 capture control plane，3D 已实现 Debezium adapter、严格 schema/protocol 校验、目标 upsert/delete + ledger 原子提交、公开任务创建和 Console Wizard。
- 工作包 3E 和 Oracle CDC 第一期已完成：`transfer.capture_resources` 是 engine-neutral generation 主事实，一对一 `postgresql_capture_resources` / `mysql_capture_resources` / `oracle_capture_resources` 保存 provider 私有资源；generic 主表不得恢复 provider 字段或使用伪资源占位。数据库 CDC 任务 JSON 只使用唯一 `DatabaseCDCTaskSpec`，provider 必须由 System Engine 解析，不能用配置形态推断；PostgreSQL/MySQL/Oracle 共用唯一 capture supervisor、continuous worker、API、capability 和 Console 路线。
- 数据库 CDC pause 时 connector 继续捕获，主要风险是 Infra Kafka retention；stop 是不可逆终态，必须由服务端显式确认并清理 ADDP-owned connector、provider 专属资源、data/schema-history topic、group 和 ACL，已停止任务不得重新 start/resume。重新同步创建新任务和新目标表，不保留兼容分支。
- 数据库 CDC 数据面固定接受 Debezium Connect 3.6.0.Final schemaless JSON；`r -> snapshot/upsert`、`c|u -> upsert`、`d -> delete`，tombstone、truncate、message、来源身份不匹配和未知字段严格阻塞且不得推进 offset。PostgreSQL、MySQL、Oracle 使用各自严格 source adapter 和类型矩阵，Decimal/Oracle NUMBER 固定 string、时间固定 Connect 毫秒编码，MySQL binary 固定 base64；Oracle Spatial 由 capture Provider 使用 generation-owned 镜像表把 `SDO_GEOMETRY` 转为 base64 WKB，再由 Oracle adapter 转成标准 EWKB。字段、类型或 envelope/source 漂移统一以 `schema_change_blocked` 阻塞。PostgreSQL/MySQL 当前阻塞消息中的新增 nullable 非 geometry 字段可通过专用 API 人工审批；Oracle schema drift 只阻塞，必须 Stop 后创建新任务和新目标表。当前公开能力仍不支持 CDC replay/DLQ、Schema Registry、Avro、Protobuf、Kafka target、普通 Oracle LOB/RAC 或 ArcGIS SDE。
- additive migration 提交后的 Meta deep scan 使用 `schema_change_requests` 中唯一 `pending -> running(token, lease) -> success|failed` claim；并发审批不能重复触发，过期 claim 只由相同重复审批 POST 接管，GET 保持只读，迟到 token 不得覆盖新结果。真实 Meta 失败不自动重试；统一的 `TRANSFER_META_SCAN_CLAIM_TTL` 必须大于 Meta client 60 秒超时。
- 业务 Kafka 注册为 System Engine，topic 使用 `type=topic` ResourceLocator；partition 只属于 runtime assignment/position/diagnostics。Infra Kafka 来自 ADDP 部署配置，不进入 System engines、资源树或用户任务 JSON。
- continuous source 必须消费 common `ChangeStreamReaderProvider`；原始 ChangeRecord 到 ChangeEvent 的解码以及 ChangeApplyWriter 归 Transfer runtime。目标必须消费声明原子、单调及所需 operations 的 `PartitionedTableChangeApplyProvider`。PostgreSQL 把业务行与业务库 `addp_transfer.apply_positions` 原子提交；MySQL 把业务行与同一目标数据库的 `_addp_transfer_apply_positions` InnoDB 私有表原子提交；Oracle 把业务行与连接用户 schema 内的 `_ADDP_TRANSFER_APPLY_POSITIONS` 原子提交。Oracle CDC target 使用 partitioned apply；普通 bounded target 使用 Oracle `TableWriteSessionProvider`，两者复用统一字段/空间映射，但不共享 CDC ledger 语义。当前均拒绝 `time`、decimal precision > 38 和非 XY geometry。普通 `TableUpsertProvider` 不足以阻止失效 worker 写回旧状态。
- continuous session 不进入 Asynq；使用 `transfer.runtime_leases` 做 owner lease、heartbeat 和 fencing，并使用 `transfer.sync_states` 按 partition 保存 `kafka_offset/v1.next_offset`。
- bounded watermark source 必须消费声明 `bounded_watermark_read` 的 common engine Provider；PostgreSQL/MySQL 都在源数据库一致性快照内冻结复合上界。目标只按幂等 `TableUpsertProvider` 能力选择，当前 PostgreSQL、MySQL 与 MySQL 模式 OceanBase 可作为非空间目标；Transfer 不按源或目标引擎类型建立专用执行分支。
- `auto_scan_metadata=true` 的普通业务 Kafka continuous task 在目标结构建立后触发一次目标父 catalog Meta deep scan。数据库 CDC connector 必须把 Debezium `Initial Snapshot` 通知写入当前 generation-owned 单分区 data topic；合法通知统一作为 target `skip` 推进，只有严格匹配当前 connector 的 `COMPLETED` offset 在目标 ledger 和 Transfer position 提交后才触发首次扫描，空表同样适用，不使用 `source.snapshot=last` 或时间猜测。首次扫描使用 task 私有持久化 claim 防重复，Meta 提交失败不阻断数据面，失败或过期 claim 由后续 runtime session 重试。普通事件/batch 不扫描，schema 正式变化后另行扫描。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh transfer` 和 `bash scripts/swagger/check-route-coverage.sh transfer`。

## 前端公开路由

- Transfer 前端遵守 `docs/spec/addp前端路由与可恢复状态规范.md`，模块内公开导航统一通过 `src/utils/moduleNavigation.js`。
- 任务和执行身份固定使用 path parameter：`/tasks/:id/edit|detail`、`/executions/:execution_id`；不得把编辑中的具体对象退化为列表 URL 或 iframe 私有状态。
- 列表进入创建、编辑、详情和执行页使用 `push`；保存或取消后离开已失效的表单历史项使用 `replace`。

## 开发与验证

```bash
bash scripts/dev/start.sh -transfer
bash scripts/dev/restart.sh -transfer
curl http://localhost:8083/health/ready
```

常用日志：

- `logs/transfer-backend.log`
- `logs/transfer-bounded-worker.log`
- `logs/transfer-continuous-worker.log`

## 相关文档

- `transfer/docs/数据库架构.md`
- `transfer/docs/transfer-基本概念及配置说明.md`
- `transfer/docs/transfer-任务语义与同步模式.md`
- `transfer/docs/design.md`
- `transfer/docs/transfer转换器架构分析.md`
- `transfer/docs/transfer高性能分析.md`
- `transfer/docs/tables/tasks表.md`
- `transfer/docs/tables/task_executions表.md`
