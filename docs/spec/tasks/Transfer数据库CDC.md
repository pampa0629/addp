# ADDP Transfer数据库CDC规范

[返回任务体系规范](../addp任务体系规范.md)

定义 PostgreSQL / MySQL / Oracle CDC 的配置、身份、捕获、位点、结构审批与恢复边界。通用连续同步规则见[Transfer 同步执行](Transfer同步执行.md)。

阅读导航：[Transfer 数据库 CDC v1 契约（PostgreSQL/MySQL/Oracle）](#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)。

## Transfer 数据库 CDC v1 契约（PostgreSQL/MySQL/Oracle）

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
	Oracle target schema 必须等于连接用户；ledger 必须带固定 ownership comment 并从 Engine Catalog 隐藏。Oracle CDC 目标继续使用 `PartitionedTableChangeApplyProvider`；普通 bounded table write 使用同一 Oracle Engine 的 `TableWriteSessionProvider`，只创建或写入普通表和 `MDSYS.SDO_GEOMETRY`，不得注册 SDE feature class 或修改 geodatabase system tables。两条能力复用同一 Oracle 字段/空间类型校验与 EWKB → SDO 转换规则，不建立 Transfer 私有 Oracle writer。当前支持 `string|int|bigint|float|double|decimal|bool|date|timestamp|json|uuid|bytes|geometry`，明确拒绝独立 `time`、decimal precision > 38 和非 XY geometry。geometry 边界仍是 EWKB + 冻结 `SpatialInfo`，Provider 必须校验 SRID/type/dimension 后转换为标准 WKB，并使用 `SDO_UTIL.FROM_WKBGEOMETRY` 写入 `MDSYS.SDO_GEOMETRY`。CDC ledger 行锁必须使用 `FOR UPDATE NOWAIT` 有界重试并检查 runtime context；锁等待取消与任何业务写失败都必须回滚业务行和 ledger。
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
