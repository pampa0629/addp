# Transfer 任务语义与同步模式

状态：正式

更新时间：2026-11-01

本文定义 Transfer `sync` 任务的稳定语义、执行边界、状态所有权和当前支持范围。配置字段和类型矩阵见[配置说明](transfer-基本概念及配置说明.md)，API 入口、身份与动作权限见[任务表](tables/tasks表.md#五api)；公共任务与 execution 字段以 [ADDP 任务体系规范](../../docs/spec/addp任务体系规范.md) 为准；尚未实现的能力见 [Transfer 后续能力清单](../../docs/next/transfer后续能力清单.md)。

阅读导航：[触发身份与调度授权](#触发身份与调度授权)、[状态与恢复](#五状态checkpoint-与-replay)、[业务 Kafka 与 DLQ](#62-业务-kafka)、[continuous 生命周期](#64-生命周期)、[诊断与恢复窗口](#65-诊断与恢复窗口)、[验证范围](#十一验证范围)。

## 一、模块定位

Transfer 是 ADDP 的数据搬运与同步执行模块。对外只提供一个稳定任务类型：

```text
provider=transfer
task_type=sync
```

“导入”和“导出”是 Manager 等调用方的用户动作，不是 Transfer 的任务类型。调用来源由统一 execution 的 `source` 表达，Transfer 不再引入 `import`、`export` 或 `transfer` 等平行任务类型。

Transfer 负责：

- 任务定义、planner、transform 与执行编排。
- 执行边界、装载方式和目标应用策略。
- 增量位置、运行时租约、fencing、重试和恢复。
- execution、日志、进度、诊断和 Meta scan 触发。

具体引擎的 catalog、native table 读写和 change stream 读取属于 `common/engine` Provider；encoded 格式读写属于 `common/format` Provider。Transfer 组合这些能力，不按具体源目标组合建立专用通道。

bounded query source 是源引擎下推后的连续读取边界：任务保存 capability 已声明支持的只读查询语言和查询文本，Transfer 通过 `QueryReadSessionProvider` 分批消费结果。MongoDB MQL 必须是单个 JSON command object，不接受裸 pipeline 数组；聚合查询使用 `{"aggregate":"<collection>","pipeline":[...]}`。pipeline 可以在源端使用 `$project` 将对象子字段投影为扁平列，使用 `$unwind` 展开数组，并以 `$group` / `$unionWith` 完成确定性关系整形；最终输出必须是一条文档对应一行。Transfer 不递归摊平 BSON/JSON，也不解释数组的业务粒度。`$out`、`$merge` 和所有写查询禁止进入该路径。

Console 的 MongoDB 基础结构整形构建器只覆盖通用 `可选单次 $unwind -> $project` 子集，并编译为上述同一 MQL command object。它只接收 Meta 已识别的字段路径和单个数组字段，自动携带文档标识并生成与选择顺序无关的稳定查询内部输出名；筛选、排序、空数组保留和业务计算不进入基础界面。PostgreSQL 目标命名与类型只属于后续 `field_mapping`。构建器不保存独立配置、不生成第二条执行路线，也不以某个业务域的数据结构作为模板；不能无损反向解析或与当前 Meta 事实不一致的已有 MQL 只读展示，Transfer Console 不提供高级 MQL 编辑入口。

bounded query source 执行前必须从同一 `PreparedQuery` 取得 Provider 证明的完整 `ReadSet`。有 `source.query.inputs[]` 时，Transfer 将其解析为 Engine Catalog leaf 集合并与 ReadSet 精确比对；无 `inputs` 时，查询必须精确只读取 source locator 所选 leaf。Transfer 不解析 SQL/MQL 文本自行推测资源，也不允许未验证的输入进入执行血缘。

关系型 native table 的 Console 构建器只扩展 Transfer 的轻量 ETL 能力。源固定为用户已选择的单表，界面只提供字段投影和一层 `all|any` 参数化行过滤，目标字段命名与类型继续归后续 `field_mapping`。值过滤必须同时受 Meta 字段类型与 `compute.query.parameters.types` 约束；`bigint/decimal` 固定以十进制字符串通过 `string` capability 无损传递，不经过 JavaScript Number。Planner 在调用 Provider 前再次校验参数开关、语言与运行时值类型，并拒绝无法由 JSON Number 无损表达的大整数。构建器不提供 Join、多表输入、聚合、计算字段、别名、排序、Limit 或方言私有函数，也不提供高级 SQL 编辑入口。仅严格属于可逆子集的 SQL 可在 Transfer Console 中结构化编辑；其他合法只读 SQL 只读展示，复杂数据开发归 Develop。该界面只是标准 `source.query` 的受限编写方式，不保存第二份可视化 DSL，不建立第二条执行路线。

bounded snapshot 可以在目标选择完成后启用“允许编排覆盖本次执行目标”。该能力与 source 是直接表、SQL 还是 MQL 无关；任务必须保存一个可直接执行的默认目标，并以 `target.override_policy=existing_table_append` 明确声明覆盖边界。默认目标和覆盖目标都必须是已存在原生表，`target.policy.apply_mode` 固定为 `append`。TaskProvider 将 `target_locator` 声明为带默认值的可选输入：未提供时写入任务默认目标，Orchestrator 可以用固定参数或显式上游稳定 ResourceLocator 输出覆盖本 execution，覆盖值不得写回任务定义。worker 使用通用 Engine capability 校验实际目标存在且字段映射的名称、顺序和类型一致，仅通过 table write session append，不执行删除、清空、建表或改表。Transfer 不得保存其他业务 owner 的 ID 或调用其 API 解析目标。

SuperMap 空间表也遵守同一边界。`supermap/sdx_postgis` 继续使用 PostgreSQL/PostGIS 原生 Provider；`supermap/sdx_postgresql` 的 bounded table 读取和写入由 `bound_runtime_engine_id` 指向的兼容 Workflow Runtime 执行 SDK direct table session。Transfer 通过 Tenant Service Token 读取 Runtime Descriptor，并校验 `addp.workflow/v1` 与完整表读写 direct 算子，不按固定 `engine_type` 选择 Runtime。两种方向都使用统一 `BatchData` 业务模型；当前 `supermap.table-batch/v1` HTTP 线协议固定为 JSON，geometry 值是 EWKB 字节并按 JSON 标准 base64 编码。Transfer 和 Common Spatial 不解析或生成 SuperMap 私有 geometry Blob，也不建立 PostGIS→SuperMap、ArcGIS SDE→SuperMap 等引擎组合通道。

`supermap/sdx_postgresql` 写会话提交不是“最后一批数据已发送”。运行时只有在 SDK `DatasetVector.Append` 完成全部批次，随后完成 `ComputeBounds()`、`UpdateSpatialIndex()`，并关闭重开校验记录数、Bounds 和索引状态后，才能向 Transfer 返回成功。首期只进入 bounded snapshot 支持矩阵；continuous/CDC 必须另行定义目标幂等和位置提交语义。

SuperMap iObjects 字段模型不提供 decimal/numeric 原生字段；Transfer 写入 `supermap/sdx_postgresql` 时将统一 decimal 映射为 64 位 Double，源端以字符串返回的 decimal 值在 Runtime 中转换为 Double。需要保持任意精度的任务必须先显式映射为字符串字段，不能把该路径视为 decimal 无损传输。

## 二、正交语义维度

Transfer 任务不能只用“全量、增量、实时”描述。稳定配置由以下正交维度组成。

### 2.1 执行边界

| 取值 | 含义 |
|---|---|
| `bounded` | 本次 execution 有确定结束条件，处理到冻结上界后结束。 |
| `continuous` | 持续等待并处理变化，直到暂停、停止、失败或运行时失联。 |

Kafka poll 会分批读取，但不因此成为 bounded；数据库 CDC 的 initial snapshot 是 continuous CDC 的 bootstrap，不是独立的 continuous snapshot 模式。

### 2.2 装载方式

| 取值 | 含义 |
|---|---|
| `snapshot` | 读取本次执行范围内的完整源快照。 |
| `incremental` | 只读取已提交边界之后的变化。 |

### 2.3 变化识别方式

| 取值 | 当前用途 | 边界 |
|---|---|---|
| 无 | bounded snapshot | 读取完整快照。 |
| `watermark` | bounded incremental | 单字段游标只识别新增；复合游标识别新增和更新。两者都不能可靠发现物理删除。 |
| `kafka` | continuous incremental | 消费业务 Kafka record。 |
| `cdc` | continuous incremental | 通过数据库日志捕获 insert、update 和 delete。 |

`manifest` 是已保留的概念方向，但尚未进入支持范围。自增 ID 只是 watermark 的受限形态，不建立平行模式。

### 2.4 触发方式

统一 execution 的 `trigger_type` 只使用：

| 取值 | 含义 |
|---|---|
| `manual` | 用户、API、Console、Orchestrator 或其他模块显式触发。 |
| `scheduled` | owner scheduler 按计划触发。 |

`continuous` 是执行边界，不是 `realtime` 触发类型。

#### 触发身份与调度授权

触发标签不授予身份或权限。用户任务定义与固定 Service Client 的 TaskProvider 入口、精确动作 Permission 统一见[任务表 API](tables/tasks表.md#五api)。手动执行以同 Tenant 的本次 User 为主体，不借用任务创建者或已保存的调度主体；用户与服务身份及父执行来源链按[任务授权主体规范](../../docs/spec/tasks/任务定义与授权.md#任务授权主体)核验。

带 Cron 的 bounded 任务使用调度授权；暂停清除本地授权，恢复由当前 User 重新授权，不沿用暂停前的主体或以 Service 身份替代。附加执行权限、主体与摘要的原子更新、触发时重核及失败语义统一见[Transfer 定时任务授权](../../docs/spec/tasks/任务定义与授权.md#transfer-定时任务授权)，模块内分工与操作导航见[调度授权与控制](transfer-基本概念及配置说明.md#调度授权与控制)。任务与调度授权均不代替源读、目标写及运行期保护核验。

### 2.5 目标应用方式

| 取值 | 含义 |
|---|---|
| `replace` | 清理或重建目标，再写入本次结果。 |
| `append` | 仅追加，不处理重复和更新。 |
| `upsert` | 按稳定键新增或更新。 |
| `upsert_delete` | 按稳定键新增、更新和删除。 |

稳定字段是 `target.policy.apply_mode`。目标引擎必须通过强类型 Provider 和 capability 声明真实写入、键、事务及幂等能力，Transfer 不能只依据配置字符串推定支持。

## 三、当前支持矩阵

| 执行边界 | 装载方式 | 变化识别 | 当前源 | 当前目标与应用方式 |
|---|---|---|---|---|
| bounded | snapshot | 无 | 当前 table/raw-copy 支持矩阵内的源；声明查询读取会话的只读原生查询 source | table `replace|append`；raw copy `replace` |
| bounded | incremental | watermark | PostgreSQL/MySQL/OceanBase/openGauss/TiDB native table；OceanBase/openGauss/TiDB 限非空间表 | PostgreSQL/MySQL/OceanBase/openGauss/TiDB native table `upsert`；OceanBase/openGauss/TiDB 限非空间表 |
| continuous | incremental | kafka | 业务 Kafka keyed JSON object | PostgreSQL/MySQL native table `upsert` |
| bounded | incremental | kafka offset range（replay execution） | 已有业务 Kafka continuous task 的原 topic | 不存在的新 PostgreSQL 隔离表 `upsert` |
| continuous | incremental | cdc | PostgreSQL/MySQL/Oracle 有稳定主键的单表 | 不存在的新 PostgreSQL/MySQL/Oracle 表 `upsert_delete` |

矩阵之外的组合必须由 planner 明确拒绝，不能通过字段省略、兼容分支或运行时猜测放行。

CDC 的源／目标能力与未支持项见[CDC 正式契约](../../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)；Oracle 目标 schema、字段与空间限制见[目标应用契约](../../docs/spec/tasks/Transfer数据库CDC.md#oracle-目标应用约束)。Oracle 目标支持仅适用于本表的数据库 CDC 组合，不扩展业务 Kafka 或 watermark 的目标范围；实际通过范围见[验证范围](#十一验证范围)，Oracle 专项仍须区分[标准入口缺口与未覆盖项](../README.md#oracle-cdc-专项入口缺口)。

## 四、Bounded 执行语义

### 4.1 Snapshot

bounded snapshot 使用独立 `transfer-bounded-worker` 从 `common.task_executions` PostgreSQL claim，经 planner 和 executor 执行。execution checkpoint 用于进度、故障定位和 Provider marker 观测，不表示可以从中间 checkpoint 自动续写。

失败后的 retry 语义是：

- `replace` 从头重新执行。
- `append` 拒绝 retry，避免重复追加。
- 使用目标覆盖的 execution 在 lease 过期后不重放，避免在本次动态目标上猜测幂等语义；调用方必须重新发起完整编排。
- execution 完成后如启用 `auto_scan_metadata`，触发一次目标 Meta deep scan。

### 4.2 Watermark incremental

watermark 有两种明确配置，不建立平行的变化识别类型：

- `tie_breaker=[]`：仅同步新增。`field` 必须自身精确匹配非空主键或唯一约束，并由用户确认可靠单调递增且不可变；目标 `policy.keys` 必须是该字段映射后的目标字段。
- `tie_breaker` 非空：同步新增和更新。位置为 `(watermark, tie_breaker...)`，其中 tie breaker 必须精确匹配非空主键或唯一约束；目标 `policy.keys` 必须与 tie breaker 的字段映射一一对应。

每次 execution 在源数据库一致性快照内冻结执行上界，只读取：

```text
(committed_position, execution_upper_bound]
```

读取必须按完整游标位置稳定排序。PostgreSQL、MySQL、MySQL 模式 OceanBase、openGauss 和 TiDB 源都由声明 `bounded_watermark_read` 的 Provider 冻结上界；MySQL、OceanBase 与 TiDB 源限定为目录报告为 InnoDB-compatible 的基表，OceanBase、openGauss 与 TiDB 当前只支持非空间表。

目标必须按稳定键幂等 upsert。只有目标批次成功提交后，Transfer 才能通过 state version 和 fencing token 对 `transfer.sync_states` 执行 CAS，推进 `watermark/v1` committed position。

watermark 支持 execution 间 resume，但不能发现物理删除。单字段模式不会重新读取游标值未变化的已有记录，因此已有记录更新不在保证范围内；复合模式要求源表的新增和更新都可靠推进 watermark。时间回拨、未更新 watermark 和当前未开放的只读副本延迟不在保证范围内。

## 五、状态、Checkpoint 与 Replay

以下事实不能混用：

| 事实 | 所有者 | 用途 |
|---|---|---|
| 任务定义 | Transfer task definition | 保存后续如何同步。 |
| execution | `common.task_executions` | 保存一次运行的状态、进度、日志和诊断。 |
| committed position | `transfer.sync_states` | 保存源端已被目标成功应用的位置。 |
| runtime ownership | `transfer.runtime_leases` | 保存 owner、lease、heartbeat 和 fencing token。 |
| 目标 apply ledger | 目标业务数据库 | 防止重复或失效 worker 覆盖更高 position。 |

bounded 任务的 pause/resume 只控制未来调度，不中断已有 execution；无 Cron 的手动任务不支持这两个操作。入口与权限见[任务表控制说明](tables/tasks表.md#用户任务定义与控制)，命令见[调度操作示例](快速开始.md#暂停与恢复定时调度)。这与下面的 execution 间位置恢复分开认定。

execution checkpoint 不是增量主状态。watermark resume 和 Kafka/CDC resume 都从 `transfer.sync_states` 开始；结束的 execution 不会重新变为 running。

业务 Kafka bounded replay 是独立 execution。它读取显式 partition offset 范围，以 execution-scoped apply identity 写入不存在的新 PostgreSQL 隔离表，不修改 owner task、主 committed position、主 runtime lease、主 apply identity 或主目标，因此可以与主 continuous runtime 并行。

### 5.1 私有状态与任务删除

`transfer.sync_states`、`transfer.runtime_leases`、`transfer.capture_resources`、`transfer.dead_letters` 和 `transfer.schema_change_requests` 是 task definition 存续期间的私有当前事实，不承担长期审计。任务删除后，历史由 `common.task_executions`、System audit 和 cleanup result 保留，私有表不能残留孤儿记录。

continuous task 删除前必须先通过唯一 Stop/cleanup 路径停止 runtime，并等待有效 lease 释放或到期；bounded task 没有真实 cancel 能力，运行中必须拒绝删除。外部 capture 和 DLQ 资源清理成功后，Transfer 在单个 Infra PostgreSQL 事务中删除 task-scoped 私有状态和 task definition。目标业务数据、目标 apply ledger 和公共 execution 不随任务定义删除。

## 六、Continuous 运行时

continuous session 不进入 bounded execution claim。Transfer 使用独立 continuous worker 进程角色，一个进程承载多个 task session，每个 session 再按 partition 运行受限 worker。

同一 task 同一时刻只有一个合法 runtime owner。状态、execution/session 与租约／位点分工见[正式状态与租约契约](../../docs/spec/tasks/Transfer同步执行.md#任务状态执行身份与运行租约)；task 唯一约束及原子领取、CAS/fencing 细节见[租约持久约束](数据库架构.md#运行租约字段与-fencing)。`transfer.sync_states` 按 partition 保存 `kafka_offset/v1.next_offset`；Kafka auto commit 禁用，consumer group 只承担 partition assignment。

continuous 的用户控制、自动恢复与审计分工见[生命周期](#64-生命周期)；进程、租约和 committed position 的所有权仍按本章约束。

### 6.1 交付保证

外部数据库目标与 Kafka position 之间没有分布式事务。当前基线是：

```text
at-least-once delivery
  + stable key
  + target monotonic apply
  + target data/ledger atomic transaction
  + Infra state CAS/fencing
```

PostgreSQL、MySQL 和 Oracle 的 `PartitionedTableChangeApplyProvider` 在目标业务数据库内把业务行变化与 partition apply ledger 放入同一事务；CDC 各目标的 ledger 归属见[数据面与捕获约束](../../docs/spec/tasks/Transfer数据库CDC.md#数据面与捕获约束)，Oracle 写入能力与目标事务边界见[目标应用契约](../../docs/spec/tasks/Transfer数据库CDC.md#oracle-目标应用约束)。Provider 的重复批次跳过、position 单调推进和失效 worker 拒绝沿[共享提交契约](../../docs/spec/tasks/Transfer同步执行.md#共享目标提交与-infra-cas)及[分区账本应用](../../docs/spec/tasks/Transfer同步执行.md#目标应用身份与分区账本)。ADDP 不宣称跨系统的通用端到端 exactly-once。

### 6.2 业务 Kafka

业务 Kafka 是用户注册的 System Engine，catalog 固定为 `service(cluster) -> topic`。topic 是可选择 leaf，partition 只属于运行时 assignment、position 和 diagnostics，不进入资源树或 ResourceLocator。

当前 source adapter 只接受 JSON object value，并从 value 中用户确认的非空字段提取稳定 key，归一化为 `operation=upsert`。字段建议可以由 Topic 尾部有界样本生成，但样本不是 schema；只有用户确认后的完整 `field_mapping` 才进入任务配置。

记录错误策略必须显式选择：

| 模式 | 语义 |
|---|---|
| `block` | 确定性 JSON、字段、key 或类型错误阻塞当前 partition。 |
| `dead_letter` | 先保存原始记录与审计，再以目标 `skip` ledger 和 Infra CAS 单调推进。 |

source/poll、目标数据库、fencing、retention 和 Infra 故障不能进入 DLQ。DLQ topic 不是 replay source。

公开 DLQ 查询只返回 tenant/task scoped 的安全控制索引，不暴露 Infra Kafka payload reference 或原始 key/value/headers。查询权限、来源隔离与字段范围见[公开查询说明](数据库架构.md#dlq-公开查询与来源隔离)，后台 availability 判定、瞬时失败语义与验收范围见[availability 专题](数据库架构.md#availability-失败与验收边界)；payload 与控制索引分工、查询和治理的完整契约统一见[DLQ 正式规范](../../docs/spec/tasks/Transfer-Kafka与DLQ.md#transfer-业务-kafka-dlq-与-bounded-replay-v1-契约)。

### 6.3 数据库 CDC

当前数据库 CDC 主路径固定为：

```text
PostgreSQL/MySQL/Oracle 单表
  -> 对应 Debezium Connector
  -> Infra Kafka
  -> Transfer Continuous Worker
  -> PostgreSQL/MySQL/Oracle 新目标表
```

Kafka Connect 管理数据库日志捕获位置，Transfer 管理目标已应用的 Infra Kafka position，两者不能互相替代。公开任务配置不出现 connector、slot、publication、server id、内部 topic、consumer group 或 ACL。

CDC 固定使用 initial snapshot。有稳定非空主键的单表事件按以下规则归一化：

| Debezium operation | Transfer operation |
|---|---|
| `r` | snapshot/upsert |
| `c`、`u` | upsert |
| `d` | delete |

tombstone、truncate、message、来源身份不匹配和协议未知字段严格阻塞，不推进 committed position。各源类型矩阵与协议约束统一见[CDC 正式契约](../../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)，Oracle 的类型与平台限制见[Oracle 源限制](#oracle-源限制)；ArcGIS SDE 保持后续独立 Provider。

ArcGIS SDE 后续数据面不进入上述 Oracle Debezium/LogMiner 分支。唯一规划路线是 workspace-scoped `SDELogicalChangeSourceProvider` 读取 enterprise geodatabase traditional versioning 的 registry、state lineage 和 adds/deletes 语义，由 Transfer capture adapter 将规范化变化及 SDE 原生位置写入 generation-owned Infra Kafka topic，再复用现有 continuous worker。SDE Provider 必须承担无空洞 initial bootstrap 和原生位置恢复；Transfer 的 `sync_states` 仍只提交 Kafka offset。branch versioning、真实数据面和公开任务选项在真实 ArcGIS Enterprise Geodatabase E2E 完成前保持关闭。

#### Oracle 源限制

Oracle 普通字段沿 LogMiner 捕获，`MDSYS.SDO_GEOMETRY` 由同一 Oracle capture Provider 通过 generation-owned 镜像进入现有 connector/consumer，不建立第二套消费链路。空间事实冻结与校验见[Oracle Spatial 冻结事实与解码](../../docs/spec/tasks/Transfer数据库CDC.md#oracle-spatial-冻结事实与解码)，镜像编码与 DDL 保护见[镜像专题](../../docs/spec/tasks/Transfer数据库CDC.md#oracle-spatial-镜像与-ddl-保护)；内部传输列不代表开放普通业务 LOB，也不得作为旁路。

普通业务 LOB、JSON/XML 等不支持的源字段，必须在 capture plan 冻结、generation 与外部资源创建前拒绝；RAC 或 readiness 无法确认单实例的源，同样在 generation 和外部资源创建前拒绝。能力声明不替代[实际验收](#十一验证范围)，Oracle 专项缺口见[标准入口说明](../README.md#oracle-cdc-专项入口缺口)。

### 6.4 生命周期

本节描述 continuous 的用户控制与自动恢复；用户入口与逐动作权限见[任务表](tables/tasks表.md#用户任务定义与控制)，当前 User 与执行授权遵循[触发身份规则](#触发身份与调度授权)。它不使用 bounded 定时任务的暂停清除／恢复重建调度授权流程。

阅读导航：[用户运行控制](#用户运行控制)、[自动恢复与退避](#自动恢复与退避)、[恢复审计与运行状态](#恢复审计与运行状态)。

#### 用户运行控制

Pause 停止目标应用并结束当前 session；启动或 Resume 创建新 execution，恢复须确认 committed position 仍处于 Kafka retention 范围。位置已被 retention 清除时必须明确失败，不能静默跳到 earliest。真实停止、释放 session／lease 与恢复约束见[停止契约](../../docs/spec/tasks/Transfer同步执行.md#continuous-的真实停止)；任务控制与标准 execution cancel 的区别见[取消能力边界](../../docs/spec/tasks/Transfer同步执行.md#任务级控制与标准取消范围)。

数据库 CDC 的 Pause 不暂停 connector。捕获仍会写入 Infra Kafka，主要风险是 backlog、磁盘和 retention；connector 或 Kafka 故障时还要分别观测 PostgreSQL WAL、MySQL binlog 或 Oracle redo/archive log 容量风险。捕获健康与源日志窗口的判读见[诊断章节](#捕获健康与源日志窗口)。

数据库 CDC 的 Stop 是不可逆终态，已停止任务不能再次 Start/Resume；重新同步必须创建新任务和新目标表。资源所有权、源身份核验及 Provider 清理顺序统一见[停止确认与清理顺序](../../docs/spec/tasks/Transfer数据库CDC.md#停止确认与清理顺序)，目标业务数据、ledger、任务定义及执行／审计保留沿[保留对象与清理验收](../../docs/spec/tasks/Transfer数据库CDC.md#保留对象与清理验收)。Stop 状态或请求成功不代替实际停止、外部资源清理与恢复验收。

#### 自动恢复与退避

以下自动恢复由 supervisor 管理，均要求任务仍为 `desired_state=running`。

触发条件、新旧 execution 与 `trigger_type` 继承见[恢复触发与执行身份](../../docs/spec/tasks/Transfer同步执行.md#恢复触发与执行身份)；唯一 pending execution、`recovery_not_before` 前禁止领取及不自动恢复的情形见[领取与排除条件](../../docs/spec/tasks/Transfer同步执行.md#恢复领取与排除条件)，计数重置、冷却与 `open/half_open` 见[circuit 状态](../../docs/spec/tasks/Transfer同步执行.md#指数退避与-circuit-状态)。

新 worker 在任务 `desired_state=running` 时原子创建 recovery execution，并从 `transfer.sync_states` 已提交位置继续。runtime lease 过期时旧 execution 先以 `failed + stop_reason=lease_expired` 结束，再创建 recovery execution。每次领取继续递增 fencing token。

参数管理归属见[配置说明](transfer-基本概念及配置说明.md#自动恢复与运行策略)，恢复前的位置核验见[生命周期控制](#用户运行控制)。

#### 恢复审计与运行状态

恢复 execution 的不可变审计、可变计数／circuit 字段和部署参数归属统一见[恢复审计与部署配置](../../docs/spec/tasks/Transfer同步执行.md#恢复审计与部署配置)。

任一目标 position 成功提交，或 session 稳定运行达到配置的稳定运行窗口后再失败，只重置 `recovery_consecutive_failures` 和 circuit 状态；本次 execution 的 `recovery_attempt`、`recovery_not_before` 与 `recovery_backoff_seconds` 作为审计事实保持不变。

字段持久化不自动扩大公开 DTO；专业读取范围以[执行监控契约](../../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)为准。上述语义、配置或状态观测不作为运行通过记录，固定版本证据、失败与未覆盖项见[验证范围](#十一验证范围)。

### 6.5 诊断与恢复窗口

本节区分 Kafka 分区位置、数据库捕获健康、源日志恢复窗口与未提交事务压力；分区采样与健康口径见[采样诊断契约](../../docs/spec/tasks/Transfer同步执行.md#continuous-采样与健康诊断)，数据库捕获见[CDC 正式契约](../../docs/spec/tasks/Transfer数据库CDC.md#捕获线程与源连接健康)，公共样本范围见[诊断安全投影](../../docs/spec/tasks/Transfer同步执行.md#诊断样本与-capture-安全投影)。

阅读导航：[分区位置与 Kafka 恢复窗口](#分区位置与-kafka-恢复窗口)、[捕获健康与源日志窗口](#捕获健康与源日志窗口)、[安全投影与读取边界](#安全投影与读取边界)。

#### 分区位置与 Kafka 恢复窗口

continuous worker 的分区位置与目标已提交 `next_offset`、lag 计算统一见[位置事实](../../docs/spec/tasks/Transfer同步执行.md#分区位置与-lag-事实)；恢复余量、时间估算和 unknown 条件见[窗口估算](../../docs/spec/tasks/Transfer同步执行.md#恢复余量与未知时间估算)。retention 与 checkpoint 的不同判定分别见[retention 健康](../../docs/spec/tasks/Transfer同步执行.md#retention-健康判定与阈值)和[checkpoint 健康](../../docs/spec/tasks/Transfer同步执行.md#checkpoint-健康与真实提交时间)。

`transfer.sync_states.position_committed_at` 单独保存真实 position commit 时间。

采样间隔与阈值的管理入口见[配置说明](transfer-基本概念及配置说明.md#诊断与安全投影)。

#### 捕获健康与源日志窗口

判定与失败语义由 CDC 正式契约维护，本专题保留消费方需要的字段与使用边界：

| 观测职责 | 字段或使用约束 | 权威正文 |
| --- | --- | --- |
| 捕获线程与源连通性 | `connector_status` 与 `source_status` 分开；使用 connector 实际源数据库凭据，Oracle 为独立 `cdc_user`，不以业务 schema-owner 或 System 普通 Engine 连通性缓存替代 | [健康聚合与冻结连接身份核验](../../docs/spec/tasks/Transfer数据库CDC.md#捕获线程与源连接健康) |
| 源事务日志恢复窗口 | `source_recovery` 公开 capture position、最早可用 position、SCN headroom、最早日志时间形成的事实窗口秒数和可选 FRA 使用率；不替代 Infra Kafka retention | [恢复窗口状态与 FRA 边界](../../docs/spec/tasks/Transfer数据库CDC.md#源日志恢复窗口) |
| 源端未提交事务压力 | `source_transactions` 公开活跃事务数、最老事务起始 SCN、由数据库计算的持续秒数和合计 Undo blocks/records；不替代 source health、恢复窗口或已提交事件 lag | [事务观测来源与失败边界](../../docs/spec/tasks/Transfer数据库CDC.md#源未提交事务压力) |

capture 是否 `running`、恢复窗口的 `healthy/critical/unknown`、FRA 空值以及事务零值／不可用分别按上述正文判断，不将 `unknown` 当作离线，也不以事务观测失败覆盖已成功的恢复窗口。Transfer 不内置长事务时长或 Undo 阈值，不据事务压力改变 capture 状态；公共投影及 Monitor 消费继续沿下节读取边界。

#### 安全投影与读取边界

公共采样对象、同一次样本中的唯一 `metadata.continuous.capture`、类型化字段与敏感信息排除统一见[诊断样本与 Capture 安全投影](../../docs/spec/tasks/Transfer同步执行.md#诊断样本与-capture-安全投影)。User、模块结果与 TaskProvider 的身份、读取范围及专业响应裁剪沿[专业读取契约](../../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)；Monitor 只消费 Owner 公共投影，不直连 Kafka 或读取 Transfer 私表补判。

恢复前的位置核验统一见[生命周期](#64-生命周期)；诊断展示不替代恢复资格或实际恢复结果。完整链路与未覆盖项按[验证范围](#十一验证范围)认定。

## 七、Schema 变化

Schema 变化决定目标能否继续应用；Meta scan 只重新识别目标事实，不能代替 DDL 决策。

数据库 CDC 默认严格阻塞。发现字段、类型、主键或 envelope/source 漂移时：

```text
停止应用当前 partition
  -> 保存 schema diff
  -> 当前消息和 committed position 不推进
  -> execution 以 schema_change_blocked 结束
  -> task 进入 blocked
```

PostgreSQL/MySQL 原 capture generation 唯一可恢复的变化是用户人工确认的 additive migration，且仅限当前阻塞消息实际包含的新增 nullable、非主键、非 geometry 字段。服务端重新验证源事实，复用目标 Provider 幂等加列并追加 mapping revision。审批落库后，任务状态为 `status=idle, desired_state=paused`。用户显式 Resume 创建新 execution，从原 committed position 重放当前消息。Oracle 第一期的任何 schema drift 均不允许在原 generation 审批恢复。 [审批落库与显式恢复](../../docs/spec/tasks/Transfer数据库CDC.md#审批落库与显式恢复)

字段删除、类型或主键变化、非 nullable/geometry 新增和协议变化不能在原 generation 恢复，只能 Stop 后创建新任务和新目标表。当前不提供自动 DDL。

Schema change 公共观测只通过触发阻塞的 execution metadata 投影。Monitor 不读取 `transfer.schema_change_requests` 等私有状态。

## 八、Meta Scan 时机

启用 `auto_scan_metadata` 后：

- bounded execution 成功写入后扫描一次。
- 普通业务 Kafka continuous 目标首次成功建立结构后提交一次目标表的精确 deep scan，不等待任务结束。
- 数据库 CDC 在当前 generation 的 `COMPLETED` 所在 offset 成功提交后触发首次扫描；同分区通知、connector 身份校验和目标 `skip` 语义见[事件与通知契约](../../docs/spec/tasks/Transfer数据库CDC.md#事件编码与初始快照通知)，空源表与提交条件见[首次扫描提交契约](../../docs/spec/tasks/Transfer同步执行.md#cdc-快照完成与提交条件)。
- continuous 首次扫描以 task 级 claim 防止重复提交；接管、提交失败恢复与 TTL 沿[首次扫描 claim 契约](../../docs/spec/tasks/Transfer同步执行.md#首次扫描-claim-与失败接管)。
- 人工 additive schema migration 成功后再次扫描。
- 普通 DML、单条事件和单个 batch 不触发扫描。

Meta 提交失败不回滚已经提交的数据或 schema。Transfer 只触发扫描，不直接写目标 Meta attributes。原生表扫描必须提交完整表 locator 的 `targets`，bounded 写后、continuous 首次扫描和 additive migration 补扫均不得扩大到所属 schema／database。

## 九、Infra Kafka 与业务 Kafka

| 角色 | 身份与可见性 | 生命周期 |
|---|---|---|
| Infra Kafka | ADDP 部署配置；不注册为 System Engine，不进入资源树 | topic、ACL 和 CDC capture 资源由 Transfer/Infra owner 管理 |
| 业务 Kafka | 用户注册的 System Engine；topic 可被选择为 Transfer source | 连接和 topic 生命周期由用户或外部系统管理 |

小型部署可以物理共用兼容 Kafka API 的集群，但必须保持不同 principal、凭据、ACL、topic namespace 和生命周期。当前唯一 Infra Kafka 实现是 Redpanda，数据库日志解析由独立 Debezium Connect distributed 集群承担；Transfer 不在 Go 进程内嵌入 Debezium。

## 十、当前架构取舍

- bounded worker 使用 Go + PostgreSQL execution claim/lease；continuous worker 使用独立常驻进程角色。
- 不把 continuous consumer 放入一次性 bounded execution handler 无限运行。
- 不依赖 Kafka auto commit 作为 Transfer committed position。
- 不把 Infra Kafka topic 暴露为用户资源。
- 不把 watermark 轮询称为 CDC。
- 不因支持 Kafka 或数据库 CDC 整体引入 Flink。
- PostgreSQL/MySQL/Oracle 复用同一任务配置、API 和 continuous consumer，不按数据库类型建立平行路线。
- 不通过忽略未知字段维持表面成功。

只有出现已验证的事件时间窗口、stream join、CEP、大规模有状态计算或当前 runtime 无法满足的并行规模时，才重新评估 Flink；评估也必须复用同一任务语义、ChangeEvent、execution 和 committed position 契约。

## 十一、验证范围

标准门禁、固定版本证据、失败与修复结论及未覆盖项统一见[模块验证](../CLAUDE.md#开发与验证)，操作与结果核对见[快速开始](快速开始.md#五开发验证)。本专题的语义与支持矩阵不作为某一版本的运行通过记录。

调度暂停／恢复、snapshot 重跑、watermark 续读及 continuous 停止／恢复分别验收。静态路由与授权声明、调度更新或 execution 创建成功，不证明真实 System → Gateway → Owner／Worker 链路、目标数据或公开入口已通过；未运行、失败、跳过和未覆盖项不计为通过。

continuous 过程事件场景核对持续写入时沿原游标续读、Console 刷新，以及 Worker 失联后原执行的失败诊断和唯一终态。场景通过范围按对应源码版本的 Hosted 回执认定。[Transfer continuous Hosted 场景](../../docs/spec/testing/scenarios/同步与引擎消费.md#transfer-continuous-events)
