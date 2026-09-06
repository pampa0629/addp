# Transfer 任务语义与同步模式

状态：正式

更新时间：2026-08-04

本文定义 Transfer `sync` 任务的稳定语义、执行边界、状态所有权和当前支持范围。配置字段、类型矩阵和 API 以 [Transfer 模块基本概念及配置说明](transfer-基本概念及配置说明.md) 为准；公共任务与 execution 字段以 [ADDP 任务体系规范](../../docs/spec/addp任务体系规范.md) 为准；尚未实现的能力见 [Transfer 后续能力清单](../../docs/next/transfer后续能力清单.md)。

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

Console 的 MongoDB 基础结构整形构建器只覆盖通用 `可选单次 $unwind -> $project` 子集，并编译为上述同一 MQL command object。它只接收 Meta 已识别的字段路径和单个数组字段，自动携带文档标识并生成查询内部输出名；筛选、排序、空数组保留和业务计算不进入基础界面。PostgreSQL 目标命名与类型只属于后续 `field_mapping`。构建器不保存独立配置、不生成第二条执行路线，也不以某个业务域的数据结构作为模板；不能无损反向解析的合法 MQL 只允许在高级编辑器中维护。

当 bounded query source 需要向上游步骤创建的既有表写入数据时，任务定义启用通用动态目标模式，不保存物理 `target`；TaskProvider 将 `target_locator` 声明为必填运行时输入，由 Orchestrator 从任意显式依赖的稳定 ResourceLocator 输出绑定。worker 使用通用 Engine capability 校验目标已存在且字段映射的名称、顺序和类型一致，仅通过 table write session append 本 execution 结果，不执行删除、清空、建表或改表。Transfer 不得保存其他业务 owner 的 ID 或调用其 API 解析目标。

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
| `watermark` | bounded incremental | 通过复合游标识别新增和更新，不能可靠发现物理删除。 |
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
| bounded | incremental | watermark | PostgreSQL/MySQL native table | PostgreSQL/MySQL/OceanBase 非空间 native table `upsert` |
| continuous | incremental | kafka | 业务 Kafka keyed JSON object | PostgreSQL/MySQL native table `upsert` |
| bounded | incremental | kafka offset range（replay execution） | 已有业务 Kafka continuous task 的原 topic | 不存在的新 PostgreSQL 隔离表 `upsert` |
| continuous | incremental | cdc | PostgreSQL/MySQL/Oracle 有稳定主键的单表 | 不存在的新 PostgreSQL/MySQL 表 `upsert_delete` |

矩阵之外的组合必须由 planner 明确拒绝，不能通过字段省略、兼容分支或运行时猜测放行。

## 四、Bounded 执行语义

### 4.1 Snapshot

bounded snapshot 使用独立 `transfer-bounded-worker` 从 `common.task_executions` PostgreSQL claim，经 planner 和 executor 执行。execution checkpoint 用于进度、故障定位和 Provider marker 观测，不表示可以从中间 checkpoint 自动续写。

失败后的 retry 语义是：

- `replace` 从头重新执行。
- `append` 拒绝 retry，避免重复追加。
- 动态既有表目标的 lease 过期后不重放当前 writer execution，避免在不受 Transfer 拥有的目标上猜测幂等语义；调用方必须重新发起完整编排并使用新目标。
- execution 完成后如启用 `auto_scan_metadata`，触发一次目标 Meta deep scan。

### 4.2 Watermark incremental

watermark 使用复合位置 `(watermark, tie_breaker...)`。每次 execution 在源数据库一致性快照内冻结执行上界，只读取：

```text
(committed_position, execution_upper_bound]
```

读取必须按完整复合位置稳定排序。PostgreSQL 和 MySQL 源都由声明 `bounded_watermark_read` 的 Provider 冻结上界；MySQL 源限定为 InnoDB 基表。

目标必须按稳定键幂等 upsert。只有目标批次成功提交后，Transfer 才能通过 state version 和 fencing token 对 `transfer.sync_states` 执行 CAS，推进 `watermark/v1` committed position。

watermark 支持 execution 间 resume，但不能发现物理删除。源表的新增和更新必须可靠推进 watermark；时间回拨、未更新 watermark 和当前未开放的只读副本延迟不在保证范围内。

## 五、状态、Checkpoint 与 Replay

以下事实不能混用：

| 事实 | 所有者 | 用途 |
|---|---|---|
| 任务定义 | Transfer task definition | 保存后续如何同步。 |
| execution | `common.task_executions` | 保存一次运行的状态、进度、日志和诊断。 |
| committed position | `transfer.sync_states` | 保存源端已被目标成功应用的位置。 |
| runtime ownership | `transfer.runtime_leases` | 保存 owner、lease、heartbeat 和 fencing token。 |
| 目标 apply ledger | 目标业务数据库 | 防止重复或失效 worker 覆盖更高 position。 |

execution checkpoint 不是增量主状态。watermark resume 和 Kafka/CDC resume 都从 `transfer.sync_states` 开始；结束的 execution 不会重新变为 running。

业务 Kafka bounded replay 是独立 execution。它读取显式 partition offset 范围，以 execution-scoped apply identity 写入不存在的新 PostgreSQL 隔离表，不修改 owner task、主 committed position、主 runtime lease、主 apply identity 或主目标，因此可以与主 continuous runtime 并行。

### 5.1 私有状态与任务删除

`transfer.sync_states`、`transfer.runtime_leases`、`transfer.capture_resources`、`transfer.dead_letters` 和 `transfer.schema_change_requests` 是 task definition 存续期间的私有当前事实，不承担长期审计。任务删除后，历史由 `common.task_executions`、System audit 和 cleanup result 保留，私有表不能残留孤儿记录。

continuous task 删除前必须先通过唯一 Stop/cleanup 路径停止 runtime，并等待有效 lease 释放或到期；bounded task 没有真实 cancel 能力，运行中必须拒绝删除。外部 capture 和 DLQ 资源清理成功后，Transfer 在单个 Infra PostgreSQL 事务中删除 task-scoped 私有状态和 task definition。目标业务数据、目标 apply ledger 和公共 execution 不随任务定义删除。

## 六、Continuous 运行时

continuous session 不进入 bounded execution claim。Transfer 使用独立 continuous worker 进程角色，一个进程承载多个 task session，每个 session 再按 partition 运行受限 worker。

同一 task 同一时刻只有一个合法 runtime owner。`transfer.runtime_leases` 提供 lease、heartbeat 和 fencing；`transfer.sync_states` 按 partition 保存 `kafka_offset/v1.next_offset`。Kafka auto commit 禁用，consumer group 只承担 partition assignment。

每次启动或恢复都会创建新的 execution。worker shutdown、lease expiry 和普通可恢复失败会结束当前 execution；任务仍为运行期望时，supervisor 按持久化退避和 circuit 状态创建 recovery execution，并从 committed position 恢复。schema drift、Pause 和 Stop 不自动恢复。

### 6.1 交付保证

外部数据库目标与 Kafka position 之间没有分布式事务。当前基线是：

```text
at-least-once delivery
  + stable key
  + target monotonic apply
  + target data/ledger atomic transaction
  + Infra state CAS/fencing
```

PostgreSQL 和 MySQL 的 `PartitionedTableChangeApplyProvider` 在目标业务数据库内把业务行变化与 partition apply ledger 放入同一事务。Provider 跳过 ledger 已覆盖的记录并单调推进 `next_offset`，阻止重复批次或失效 worker 写回旧状态。ADDP 不宣称跨系统的通用端到端 exactly-once。

### 6.2 业务 Kafka

业务 Kafka 是用户注册的 System Engine，catalog 固定为 `service(cluster) -> topic`。topic 是可选择 leaf，partition 只属于运行时 assignment、position 和 diagnostics，不进入资源树或 ResourceLocator。

当前 source adapter 只接受 JSON object value，并从 value 中用户确认的非空字段提取稳定 key，归一化为 `operation=upsert`。字段建议可以由 Topic 尾部有界样本生成，但样本不是 schema；只有用户确认后的完整 `field_mapping` 才进入任务配置。

记录错误策略必须显式选择：

| 模式 | 语义 |
|---|---|
| `block` | 确定性 JSON、字段、key 或类型错误阻塞当前 partition。 |
| `dead_letter` | 先保存原始记录与审计，再以目标 `skip` ledger 和 Infra CAS 单调推进。 |

source/poll、目标数据库、fencing、retention 和 Infra 故障不能进入 DLQ。DLQ topic 不是 replay source。

公开 DLQ 查询只返回 tenant/task scoped 的安全控制索引，不暴露 Infra Kafka payload reference 或原始 key/value/headers。原始 payload 由 Infra Kafka retention 管理；后台 reconciler 只有在 topic、partition、offset 明确消失或记录身份不匹配时才把 payload 标记为 unavailable，网络、认证和 broker 故障不能被误判为 payload 已丢失。

### 6.3 数据库 CDC

当前数据库 CDC 主路径固定为：

```text
PostgreSQL/MySQL/Oracle 单表
  -> 对应 Debezium Connector
  -> Infra Kafka
  -> Transfer Continuous Worker
  -> PostgreSQL/MySQL 新目标表
```

Kafka Connect 管理数据库日志捕获位置，Transfer 管理目标已应用的 Infra Kafka position，两者不能互相替代。公开任务配置不出现 connector、slot、publication、server id、内部 topic、consumer group 或 ACL。

CDC 固定使用 initial snapshot。有稳定非空主键的单表事件按以下规则归一化：

| Debezium operation | Transfer operation |
|---|---|
| `r` | snapshot/upsert |
| `c`、`u` | upsert |
| `d` | delete |

tombstone、truncate、message、来源身份不匹配和协议未知字段严格阻塞，不推进 committed position。完整 PostgreSQL/MySQL/Oracle 类型矩阵见基本概念及配置说明。Oracle 普通字段直接由 LogMiner 捕获；`MDSYS.SDO_GEOMETRY` 由同一 Oracle capture Provider 通过 generation-owned WKB 镜像表捕获，不建立第二套 consumer。普通 Oracle LOB/RAC 仍未开放，ArcGIS SDE 保持后续独立 Provider。

ArcGIS SDE 后续数据面不进入上述 Oracle Debezium/LogMiner 分支。唯一规划路线是 workspace-scoped `SDELogicalChangeSourceProvider` 读取 enterprise geodatabase traditional versioning 的 registry、state lineage 和 adds/deletes 语义，由 Transfer capture adapter 将规范化变化及 SDE 原生位置写入 generation-owned Infra Kafka topic，再复用现有 continuous worker。SDE Provider 必须承担无空洞 initial bootstrap 和原生位置恢复；Transfer 的 `sync_states` 仍只提交 Kafka offset。branch versioning、真实数据面和公开任务选项在真实 ArcGIS Enterprise Geodatabase E2E 完成前保持关闭。

### 6.4 生命周期

Pause 停止目标应用并结束当前 session；Resume 在 committed position 仍处于 Kafka retention 范围时创建新 execution 并恢复。

数据库 CDC 的 Pause 不暂停 connector。捕获仍会写入 Infra Kafka，主要风险是 backlog、磁盘和 retention；connector 或 Kafka 故障时还要分别观测 PostgreSQL WAL、MySQL binlog 或 Oracle redo/archive log 容量风险。

capture generation 的健康由两个独立事实聚合：`connector_status` 表示 Kafka Connect connector/task 线程状态，`source_status` 表示使用 connector 实际数据库凭据执行最小只读查询的结果。只有两者都健康时 capture 才是 `running`；任一失败或无法观测时 capture 为 `failed`。Oracle 使用独立 `cdc_user` 探测，不能因为 Kafka Connect REST 仍返回 `RUNNING` 就推断 LogMiner 数据源可达，也不能用业务 schema-owner 连接或 System 的普通 Engine 连通性缓存代替。

`source_recovery` 单独表达源事务日志恢复窗口，不替代上述健康事实，也不替代 Kafka retention。Oracle 使用 Kafka Connect connector offset 的 `scn` 与当前可用 redo/archive 最早 SCN 比较，公开 capture position、最早可用 position、SCN headroom、最早日志时间形成的事实窗口秒数和可选 FRA 使用率。capture SCN 已早于日志窗口时为 `critical`；事实完整且仍在窗口内时为 `healthy`；offset、视图或时间不足时为 `unknown`。FRA 未启用时容量为空，SCN 差值不换算成时间。

`source_transactions` 单独表达源端当前未提交事务压力，不替代 `source_recovery`、source health 或已提交事件 lag。Oracle 从 `V$TRANSACTION` 公开活跃事务数、最老事务起始 SCN、由数据库计算的持续秒数以及合计 Undo blocks/records。无活跃事务时计数和 Undo 用量为 0，最老事务字段为空。Transfer 不内置长事务时长或 Undo 阈值，也不据此改变 capture 状态；事务观测失败不能覆盖已成功的恢复窗口观测。

Oracle CDC 普通源表字段明确拒绝 `CLOB`、`NCLOB`、`BLOB`、`BFILE`、`LONG`、`LONG RAW`、`XMLTYPE` 和原生 `JSON`。拒绝发生在 capture plan 冻结、generation 创建和外部资源创建之前。Oracle Spatial 镜像表使用的 WKB `BLOB` 是 generation-owned 内部传输实现，不是普通业务 LOB 支持，也不能作为旁路开放这些类型。

Oracle RAC 当前没有 capture/provider 契约。readiness 必须读取 `V$PARAMETER.cluster_database` 并只接受明确 `FALSE`；`TRUE` 或无法解释的值都在 capture generation 和外部资源创建前拒绝，不能把单实例 LogMiner 路线旁路复用于 RAC。

数据库 CDC 的 Stop 是不可逆终态。它清理 ADDP-owned connector、Provider 专属捕获资源、data/schema-history topic、consumer group 和 ACL；已停止任务不能再次 Start/Resume。重新同步必须创建新任务和新目标表。Stop 保留目标业务数据、目标 ledger、任务定义、execution 和审计记录。

### 6.5 诊断与恢复窗口

continuous worker 定期读取每个 partition 的 earliest/latest position，并以目标已提交 `next_offset` 计算：

- lag records。
- recovery headroom records。
- source rate。
- retention horizon。
- checkpoint age 与 health。

诊断写入 `common.task_executions.metadata.continuous`。数据库 CDC 在同一次诊断采样中把当前 capture generation 的安全摘要写入唯一 `metadata.continuous.capture`，仅包含 `generation`、类型化 `source_recovery` 和 `source_transactions`；私有错误、连接信息、凭据、Provider 资源名和未知字段不得进入公共 execution。非数据库 continuous execution 不保留该对象。Monitor 只读取公共 execution metadata，不直连 Kafka，也不读取 Transfer 的 `sync_states`、`runtime_leases`、`capture_resources` 或 Provider 私表。Resume 前必须验证 committed position 没有被 retention 清除，不能静默跳到 earliest。

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

PostgreSQL/MySQL 原 capture generation 唯一可恢复的变化是用户人工确认的 additive migration，且仅限当前阻塞消息实际包含的新增 nullable、非主键、非 geometry 字段。服务端重新验证源事实，复用目标 Provider 幂等加列，追加 mapping revision，并将任务置为 paused；用户随后 Resume，从原 committed position 重放当前消息。Oracle 第一期的任何 schema drift 均不允许在原 generation 审批恢复。

字段删除、类型或主键变化、非 nullable/geometry 新增和协议变化不能在原 generation 恢复，只能 Stop 后创建新任务和新目标表。当前不提供自动 DDL。

Schema change 公共观测只通过触发阻塞的 execution metadata 投影。Monitor 不读取 `transfer.schema_change_requests` 等私有状态。

## 八、Meta Scan 时机

启用 `auto_scan_metadata` 后：

- bounded execution 成功写入后扫描一次。
- 普通业务 Kafka continuous 目标首次成功建立结构后提交一次目标父 catalog deep scan，不等待任务结束。
- 数据库 CDC 通过同一 generation-owned 单分区 data topic 中的 Debezium `Initial Snapshot` 通知推进目标 `skip`；只有严格匹配当前 connector 的 `COMPLETED` 通知在目标 ledger 和 Transfer position 提交后才触发首次扫描，空源表同样触发，不依赖数据事件或 `source.snapshot=last`。
- continuous 首次扫描使用持久化 claim，避免 recovery、resume 或并发实例重复提交；失败或过期 claim 由后续 runtime session 重新领取。
- 人工 additive schema migration 成功后再次扫描。
- 普通 DML、单条事件和单个 batch 不触发扫描。

Meta 提交失败不回滚已经提交的数据或 schema。Transfer 只触发扫描，不直接写目标 Meta attributes。

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
- 不为 PostgreSQL 和 MySQL 建立两套同形任务配置、API 或 continuous consumer。
- 不通过忽略未知字段维持表面成功。

只有出现已验证的事件时间窗口、stream join、CEP、大规模有状态计算或当前 runtime 无法满足的并行规模时，才重新评估 Flink；评估也必须复用同一任务语义、ChangeEvent、execution 和 committed position 契约。
