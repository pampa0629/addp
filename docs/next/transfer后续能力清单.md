# Transfer 后续能力清单

状态：待评估能力

更新时间：2026-11-01

本文只记录尚未进入 Transfer 当前支持范围的能力，不是排期或兼容路线。当前正式语义见 [Transfer 任务语义与同步模式](../../transfer/docs/transfer-任务语义与同步模式.md)，当前配置与限制见 [Transfer 模块基本概念及配置说明](../../transfer/docs/transfer-基本概念及配置说明.md)。

新增能力前必须先确认真实需求、Provider 契约和可验证的一致性边界；完成后应迁入正式文档并从本清单删除，不能长期保留“已完成”条目。

## 一、Bounded 同步

| 能力 | 当前缺口 | 进入实现前必须确认 |
|---|---|---|
| Manifest incremental | 文件、对象和快照集合尚无基于 fingerprint、etag、mtime 或版本清单的增量主状态 | manifest identity、删除表达、冻结上界、重命名语义和目标幂等策略 |
| Watermark 只读副本 | 当前只读源主库一致性快照，不开放副本读取 | 最大复制延迟、lookback、重复吸收和上界一致性 |
| Watermark lookback | 当前严格从 committed 复合位置继续，不重读迟到窗口 | 时间回拨边界、窗口大小、目标幂等和状态推进规则 |
| MySQL watermark 空间字段 | 当前 MySQL watermark 源不接受空间字段 | MySQL 空间 schema/row 表达、SRID 事实和目标 Provider 矩阵 |
| Snapshot checkpoint resumable | snapshot checkpoint 当前只可观测，retry 从头执行 | Provider resume marker、目标重复写入边界、replace staging 与原子切换 |
| Bounded execution cancel | 标准 TaskProvider 保持 `supports_cancel=false` | worker context 取消、目标事务回滚、任务状态收敛和 API 契约 |
| Bounded 分区并行 | 当前主路径未提供跨 worker 的 source partition 协调 | 分区稳定性、上界冻结、失败重试、最终提交和资源限流 |

## 二、业务 Kafka

| 能力 | 当前缺口 | 进入实现前必须确认 |
|---|---|---|
| Kafka 原生 record key | 当前稳定 key 只从 JSON value 的显式字段提取 | key 编码、复合键、key/value 一致性校验和字段映射交互 |
| 无 key append | 当前只支持 keyed upsert | at-least-once 下重复记录的产品语义、目标去重或显式接受重复的审计边界 |
| Schema Registry | 当前不连接 Registry | Registry 类型、认证、subject/version 选择、兼容策略和缓存失效 |
| Avro / Protobuf | 当前只解码 JSON object | 编码与 envelope 分离、逻辑类型、schema version 与历史消息绑定 |
| Kafka target | 当前 Kafka 只能作为业务 source | writer Provider、partition key、交付确认、幂等、事务和目标资源 owner |
| [Replay 扩展到其他目标](../spec/tasks/Transfer-Kafka与DLQ.md#transfer-业务-kafka-dlq-与-bounded-replay-v1-契约) | 当前仅业务 Kafka record/json 的显式 offset ranges 可写入不存在的新 PostgreSQL 隔离表，不含 CDC 重放或 DLQ topic 读取 | 其他目标的 execution-scoped ledger、隔离保证和能力声明 |

业务 Kafka 的记录错误策略与 replay 分工见[业务 Kafka 语义](../../transfer/docs/transfer-任务语义与同步模式.md#62-业务-kafka)及[Checkpoint 与 Replay](../../transfer/docs/transfer-任务语义与同步模式.md#五状态checkpoint-与-replay)，完整约束以[Kafka 与 DLQ 契约](../spec/tasks/Transfer-Kafka与DLQ.md#transfer-业务-kafka-dlq-与-bounded-replay-v1-契约)为准。replay 从原业务 topic 读取，DLQ topic 不是 replay source；它使用独立 bounded execution、execution-scoped apply identity 和隔离目标，不改主状态、水位、lease、主 apply identity 或主目标，可与主 runtime 并行，失败不改变主任务状态或触发 continuous 恢复 circuit。DLQ 的 Tenant/task 查询隔离与 payload 安全见[公开查询与来源隔离](../../transfer/docs/数据库架构.md#dlq-公开查询与来源隔离)。DLQ 查询成功或 `payload_available=true` 不证明 replay 通过，也不代表 CDC replay/DLQ 已开放；固定版本证据、失败与未覆盖项按[模块验证](../../transfer/CLAUDE.md#开发与验证)的原始场景认定。

## 三、数据库 CDC

| 能力 | 当前缺口 | 进入实现前必须确认 |
|---|---|---|
| [CDC replay](../spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle) | 尚未开放历史范围重放；现有 committed position resume 受[生命周期](../../transfer/docs/transfer-任务语义与同步模式.md#64-生命周期)和审批准入约束，不能替代 replay | 历史范围、独立 apply identity、隔离目标、capture generation 与 retention |
| [CDC DLQ](../spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle) | 尚未开放；schema/protocol/source/target 错误仍失败／阻塞，不能套用业务 Kafka 的 `dead_letter` 跳过 | 哪些错误允许跳过、删除事件完整性、原始 envelope 审计和 position 推进规则 |
| [Truncate](../spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle) | 尚未开放；Debezium `op=t` 作为协议错误拒绝，不执行目标清空，不静默跳过或推进 committed offset | 目标清空权限、事务边界、并发事件顺序和不可逆确认 |
| [自动 DDL](../spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle) | 尚未开放；人工 additive 审批限 PostgreSQL/MySQL 源当前阻塞消息中的新增 nullable、非主键、非 geometry 字段，Oracle 源 drift 须 Stop 后新建任务与目标表；完整准入、状态转换及失败语义见[审批恢复配置](../../transfer/docs/transfer-基本概念及配置说明.md#schema-drift-与审批恢复) | record 绑定的 schema version、目标 Provider DDL 原子性、回滚和审批策略 |
| [多表 CDC](../spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle) | 尚未开放；每个任务只捕获一张源表并创建一张新目标表，不接管、清空或合并已有目标 | table identity、路由、每表 schema revision、部分失败和资源生命周期 |
| [无主键 CDC](../spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle) | 尚未开放；源表主键必须稳定、非空且捕获期间不可修改，Debezium record key 须完整包含复合主键，映射后与目标 keys 一一对应 | 事件身份、update/delete 定位、重复应用和目标一致性保证 |
| [PostgreSQL/MySQL/Oracle 类型矩阵扩展](../spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle) | 当前开放范围见[源类型与空间捕获](../../transfer/docs/transfer-基本概念及配置说明.md#源类型与空间捕获)及[目标应用约束](../../transfer/docs/transfer-基本概念及配置说明.md#cdc-目标应用与位点)；未声明或不兼容类型仍拒绝 | Debezium wire schema、精度/时区、目标映射、键约束和全生命周期 E2E |
| Oracle CDC 高级能力 | 当前支持单实例 CDB/PDB、普通字段与 Oracle Spatial 单表，并提供源恢复窗口和未提交事务压力观测；捕获与类型边界见下述专题 | RAC、普通业务 LOB、长事务阈值/告警策略、归档日志自动治理 |
| [ArcGIS SDE 逻辑变化源](ArcGIS%20SDE支持设计.md) | 普通 Oracle/PostgreSQL 表日志不能等同版本化要素变化 | geodatabase 版本模型、A/D delta tables、空间类型、业务事务和许可边界 |

单表、主键与事件约束见[数据库 CDC 语义](../../transfer/docs/transfer-任务语义与同步模式.md#63-数据库-cdc)。单表与主键要求属于捕获准入条件，未满足时不得启动捕获；运行时主键或 schema 漂移按[Schema 变化](../../transfer/docs/transfer-任务语义与同步模式.md#七schema-变化)阻塞，当前消息和 committed position 不推进。已有单表 snapshot/upsert/delete 结果不能补作多表、无主键或 Truncate 通过；固定版本证据、失败及未覆盖项沿[验证范围](../../transfer/docs/transfer-任务语义与同步模式.md#十一验证范围)的原始场景认定。

Oracle 普通表 CDC 与 ArcGIS SDE 逻辑变化源是两个不同能力，不能包装成同一路径。Oracle Spatial 的捕获、冻结空间事实与源／目标限制统一见[CDC 正式契约](../spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)及[Oracle CDC 配置](../../transfer/docs/transfer-基本概念及配置说明.md#oracle-cdc)。ArcGIS 传统版本化编辑仍需独立验证版本、权限、事务和许可边界。基础 `MDSYS.SDO_GEOMETRY` Spatial 读取继续由 Oracle Engine Provider 独立支持。

类型声明与实际验证须按源／目标组合、snapshot／insert／update／delete、精度／时区及空间维度分别核对，空间源捕获不代表目标写入同样受支持。固定源码版本、有效证据、失败与修复结论及未覆盖项沿[模块验证](../../transfer/CLAUDE.md#开发与验证)的原始场景认定，其他专项通过不能补作 CDC 类型矩阵通过；Oracle 的既有数据面范围见[Oracle 既有验证与验收边界](../../transfer/docs/transfer-基本概念及配置说明.md#oracle-既有验证与验收边界)，标准调用与隔离清理门禁缺口见[Oracle 专项入口说明](../../transfer/README.md#oracle-cdc-专项入口缺口)。能力声明、已有用例或部分结果不代表全部组合或当前版本已验收；未运行、失败、跳过或未覆盖项不计为通过。

## 四、Continuous 运行时与观测

| 能力 | 当前缺口 | 进入实现前必须确认 |
|---|---|---|
| 标准 execution cancel | continuous task 有自己的 Pause/Stop，但 TaskProvider 标准 cancel 未开放 | cancel 与 Pause/Stop 的唯一语义、API owner 和 execution 终态 |
| 单任务跨实例 partition | 当前一个 task 的全部 partition 由同一 runtime owner 承载 | partition lease、全局 fencing、rebalance、状态聚合和吞吐证据 |
| 低频 Meta 统计刷新 | continuous 只在首次建表和正式 schema 变化后自动扫描 | 统计新鲜度目标、扫描成本、去重和 owner scheduler |
| 手工 Meta scan 关联回执 | 手工扫描完成结果目前不回写 Transfer schema change 请求 | 跨模块 correlation id、结果事件、幂等和 Monitor incident 恢复 |
| Flink runtime | 当前没有必须引入 Flink 的语义或规模证据 | 事件时间、stream join、CEP、大规模状态或现有 runtime 基准瓶颈；必须复用现有任务与状态契约 |

## 五、通用目标与执行增强

以下目标写侧与执行增强能力仍待评估，模块职责以[当前架构](../../transfer/docs/design.md)为准：

- `row_filter` / predicate 的统一语义。
- Doris Stream Load。
- ClickHouse 排序键、分区键和原生批量接口。
- raw copy 端到端样例任务和更完整的执行展示。

这些能力应在出现具体使用场景时分别确认 Provider 归属和测试矩阵，不能借同步模式扩展在 Transfer 内建立引擎专用旁路。

容器子表的现行主路径与身份边界见[架构约束](../../transfer/docs/design.md#容器子表约束)，具体读取入口见[读取布局与入口](../../transfer/docs/transfer-基本概念及配置说明.md#读取布局与入口)。实际格式与目标能力仍由对应 Provider 声明决定；这些约束不代表所有格式、任意目标或端到端场景已验收。标准门禁、固定版本证据、失败与未覆盖项见[模块验证](../../transfer/CLAUDE.md#开发与验证)，未运行、失败、跳过或未覆盖项不计为通过。
