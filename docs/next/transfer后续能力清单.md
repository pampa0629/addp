# Transfer 后续能力清单

状态：待评估能力

更新时间：2026-08-04

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
| Replay 扩展到其他目标 | bounded replay 当前只写不存在的新 PostgreSQL 隔离表 | 其他目标的 execution-scoped ledger、隔离保证和能力声明 |

## 三、数据库 CDC

| 能力 | 当前缺口 | 进入实现前必须确认 |
|---|---|---|
| CDC replay | 当前只允许从 committed position resume | 历史范围、独立 apply identity、隔离目标、capture generation 与 retention |
| CDC DLQ | schema/protocol/source/target 错误当前全部阻塞 | 哪些错误允许跳过、删除事件完整性、原始 envelope 审计和 position 推进规则 |
| Truncate | Debezium truncate 事件当前拒绝 | 目标清空权限、事务边界、并发事件顺序和不可逆确认 |
| 自动 DDL | 当前只支持人工确认的安全 additive migration | record 绑定的 schema version、目标 Provider DDL 原子性、回滚和审批策略 |
| 多表 CDC | 当前一个任务只绑定一张源表和一张新目标表 | table identity、路由、每表 schema revision、部分失败和资源生命周期 |
| 无主键 CDC | 当前要求稳定非空主键 | 事件身份、update/delete 定位、重复应用和目标一致性保证 |
| PostgreSQL/MySQL 类型矩阵扩展 | 当前只开放已完成真实 Debezium E2E 的无歧义类型集合 | Debezium wire schema、精度/时区、目标映射、键约束和全生命周期 E2E |
| Oracle CDC | 当前未接入 Oracle redo/LogMiner | 明确 Oracle 版本与部署、许可、权限、RAC/CDB/PDB、LOB、长事务和 Debezium 兼容矩阵 |
| ArcGIS SDE 逻辑变化源 | 普通 Oracle/PostgreSQL 表日志不能等同版本化要素变化 | geodatabase 版本模型、A/D delta tables、空间类型、业务事务和许可边界 |

Oracle 普通表 CDC 与 ArcGIS SDE 逻辑变化源是两个不同能力，不能包装成同一路径。Oracle 12c、空间用户定义类型和 ArcGIS 传统版本化编辑在进入设计前必须分别验证。

## 四、Continuous 运行时与观测

| 能力 | 当前缺口 | 进入实现前必须确认 |
|---|---|---|
| 标准 execution cancel | continuous task 有自己的 Pause/Stop，但 TaskProvider 标准 cancel 未开放 | cancel 与 Pause/Stop 的唯一语义、API owner 和 execution 终态 |
| 单任务跨实例 partition | 当前一个 task 的全部 partition 由同一 runtime owner 承载 | partition lease、全局 fencing、rebalance、状态聚合和吞吐证据 |
| 低频 Meta 统计刷新 | continuous 只在首次建表和正式 schema 变化后自动扫描 | 统计新鲜度目标、扫描成本、去重和 owner scheduler |
| 手工 Meta scan 关联回执 | 手工扫描完成结果目前不回写 Transfer schema change 请求 | 跨模块 correlation id、结果事件、幂等和 Monitor incident 恢复 |
| Flink runtime | 当前没有必须引入 Flink 的语义或规模证据 | 事件时间、stream join、CEP、大规模状态或现有 runtime 基准瓶颈；必须复用现有任务与状态契约 |

## 五、通用目标与执行增强

以下能力已在 Transfer 当前架构文档中记录，但不属于本次同步模式设计的既定交付：

- `row_filter` / predicate 的统一语义。
- Doris Stream Load。
- ClickHouse 排序键、分区键和原生批量接口。
- raw copy 端到端样例任务和更完整的执行展示。
- container child table transfer。

这些能力应在出现具体使用场景时分别确认 Provider 归属和测试矩阵，不能借同步模式扩展在 Transfer 内建立引擎专用旁路。
