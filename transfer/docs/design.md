# Transfer 当前架构设计

更新时间：2026-11-02

本文记录 Transfer 模块当前稳定架构。旧的 Transfer 私有 Reader / Writer 插件体系、ConnectorRegistry、`pkg/pipeline` 和旧任务 JSON 不再作为主路径保留。

阅读导航：[架构分层](#二架构分层)、[table 主路径](#四table-transfer-主路径)、[raw copy](#六raw-copy-主路径)、[持续运行时](#十持续运行时与后续方向)、[验证导航与验收范围](#架构说明与验收范围)。配置示例与字段以[配置正文](transfer-基本概念及配置说明.md)为准，执行边界与状态所有权以[任务语义](transfer-任务语义与同步模式.md)为准；平台契约由[任务体系规范](../../docs/spec/addp任务体系规范.md#专题导航)统一导航。

## 一、模块定位

Transfer 负责一次数据传输任务的业务编排：

- 任务创建、更新、启动、停止、重试和执行历史查询。
- source / target endpoint 配置解析和校验。
- planner 将 endpoint 转换为可执行计划。
- executor 调用 common engine / format / contentio 完成读写。
- transform 编排，当前稳定类型为 `field_mapping`。
- checkpoint、进度、日志和指标回写。
- 写入成功后触发 Meta deep scan。

Transfer 不负责具体引擎连接、格式解析或数据类型 reader / writer 实现。

## 二、架构分层

```text
Transfer API / Frontend
  -> TaskService / ExecutionService
  -> bounded: PostgreSQL claim -> Bounded Worker -> Planner -> Executor
  -> continuous: Continuous Worker -> ChangeEvent adapter -> ChangeApplyWriter
  -> common/engine + common/format + common/contentio
  -> Source / Target engine
```

| 层 | 主要代码 | 职责 |
|---|---|---|
| API | `backend/internal/api` | REST API、认证后的租户上下文、请求 DTO |
| Service | `backend/internal/service` | 任务 CRUD、执行记录、重试、队列入队、Meta scan 触发 |
| Bounded Worker | `backend/internal/worker` | 从 `common.task_executions` claim bounded execution 并调用执行服务 |
| Continuous Worker | `backend/internal/continuous` | 通过 lease/fencing 领取 continuous execution，消费 change stream 并提交目标变化与 position |
| Planner | `backend/internal/planner` | 解析新 endpoint JSON，解析 System engine，生成 table transfer plan 或 raw copy plan |
| Executor | `backend/internal/executor` | 按计划执行 table reader / transform / writer，或执行 encoded single content 原样复制 |
| Common | `common/engine`、`common/format`、`common/contentio` | 引擎、格式、内容 I/O 和数据类型能力 |

## 三、任务配置模型

`transfer.transfer_tasks.config` 是任务配置事实，source / target endpoint 经唯一 planner 转为 table、raw copy 或对应的运行计划。字段和可执行示例统一见[Config JSON](transfer-基本概念及配置说明.md#二任务-config-json)，组合范围见[当前支持矩阵](transfer-任务语义与同步模式.md#三当前支持矩阵)。

`source.locator` 指向已存在资源；`target.parent_locator` 指向已存在父 node，`target.name` 表达待写入资源名，不接受待创建资源的虚拟 locator。`data_type`、`representation` 和 layout 决定 Provider 组合；`format` 只属于 encoded endpoint，其源事实与目标声明统一见[format 声明与来源](transfer-基本概念及配置说明.md#format-声明与来源)。

旧 `connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等字段出现即拒绝。

## 四、table Transfer 主路径

Transfer 不按具体引擎组合分叉，而是按 data type / representation / layout 选择统一链路。

```text
source endpoint
  -> table reader
  -> table batch
  -> transform
  -> table writer
  -> target endpoint
```

| source / target 形态 | 读取 / 写入来源 |
|---|---|
| native table | `common/engine` `TableReadSessionProvider`、`BatchReadableProvider`、`TableWritePreparer`、`TableWriteSessionProvider`、`BatchWritableProvider` |
| encoded single file/object | engine content provider + contentadapter + `common/format` `TableReaderProvider` / `TableWriterProvider` |
| encoded multi file/object | contentio + `[]format.RelatedRef` + `MultiTableReaderProvider` / `MultiTableWriterProvider` |
| encoded whole scope | contentio Reader / Lister + `ScopeTableReaderProvider` |

格式与原生目标写侧分别由[格式支持](transfer-基本概念及配置说明.md#33-格式)和[原生目标写侧](transfer-基本概念及配置说明.md#已接入的原生目标写侧)维护。Parquet 保留 single table 与 whole scope dataset 的读取边界及 `field_selection` 下推；Shapefile 沿 multi reader / writer，range source 可利用 `.shx` / `.dbf` 窗口，不能把这些布局合并为普通单文件路径。

## 五、Transform

`field_mapping` 只作用于 table batch，承担字段投影、重命名、默认值、目标类型声明与 geometry schema 同步。`project` 只输出声明字段并可下推 source `field_selection`；`passthrough` 保留源 row 后再覆盖／新增映射字段，不下推该选择。完整配置见[field_mapping](transfer-基本概念及配置说明.md#五field_mapping-transform)。

过滤、表达式、聚合与 CRS transform 不属于该 transform。源端只读查询和轻量 ETL 构造见[query source](transfer-基本概念及配置说明.md#23-bounded-query-source-与轻量-etl-构造)，不能把源查询能力记成另一套 Transfer transform。

## 六、raw copy 主路径

raw copy 用于 non-table encoded single content 的原始字节复制，不进入 `common/format` table reader / writer，也不做文档解析、媒体转码或格式转换。

```text
source encoded file/object
  -> engine content reader
  -> byte stream
  -> engine content writer
  -> target encoded file/object
```

支持类型、endpoint、完整目标路径和 `replace` 限制统一见[raw copy 配置](transfer-基本概念及配置说明.md#四raw-copy-支持范围)，目标类型／格式的省略与显式一致性见[目标继承](transfer-基本概念及配置说明.md#raw-copy-目标继承与一致性)。

raw copy 的覆盖语义由 Transfer 执行：目标引擎必须提供 `ResourceDeleteProvider`，先删除目标资源再创建写入，不依赖 create 的隐式覆盖行为。

## 七、写入策略

replace / append / upsert / upsert_delete 是 Transfer apply policy；upsert 和 upsert_delete 必须由 common engine 的强类型 Provider 真实实现。

`replace`、`append`、`upsert`、`upsert_delete` 的适用组合、目标能力和失败重试统一见[写入策略](transfer-基本概念及配置说明.md#六写入策略)与[支持矩阵](transfer-任务语义与同步模式.md#三当前支持矩阵)，不再按单一数据库维护独立能力摘要。

`TableWritePreparer` 只做 ensure / create table / schema evolution，不承载 replace / append 策略。DeleteResource 是 common engine 提供的原子删除能力；watermark upsert 使用独立强类型 Provider。

## 八、Checkpoint 和重试

execution checkpoint、进度字段和 Provider marker 用于观测执行过程，恢复提交点由增量同步状态单独维护。raw copy 成功后的行数与字节数字段见[执行记录表的指标字段](tables/task_executions表.md#二指标字段)，`checkpoint_offset` 的口径见[Checkpoint、进度和重试](transfer-基本概念及配置说明.md#十checkpoint进度和重试)。

失败 snapshot retry 创建新 execution 并从头执行，append 拒绝 retry；snapshot checkpoint resumable 尚未开放。watermark 与 continuous 的 resume 读取 `transfer.sync_states` committed position，不消费 execution marker，准入条件与独立动作边界见[恢复分级](transfer-基本概念及配置说明.md#恢复分级)及[状态、Checkpoint 与 Replay](transfer-任务语义与同步模式.md#五状态checkpoint-与-replay)。

## 九、写后 Meta 扫描

启用 `auto_scan_metadata` 后，bounded 写后、continuous 首次目标建立／CDC 初始快照完成和审批后的补扫按各自时机提交；完整条件、claim 与失败语义见[写后 Meta 扫描](transfer-基本概念及配置说明.md#十一写后-meta-扫描)及[Meta Scan 时机](transfer-任务语义与同步模式.md#八meta-scan-时机)。

Transfer 只提交实际目标边界：native table 使用完整表 locator 的 `targets`，encoded/raw content 使用实际写出文件／对象的 `ref_groups`，Shapefile 只带本次生成的 refs，不扩大到父 schema、database 或目录。Meta 提交失败不回滚已提交的数据或 schema，也不能把提交成功记作扫描完成。

Transfer 不直接写目标 Meta attributes。目标 item 的 `data_type`、`format`、`layout`、`type_info`、`format_info` 和 capabilities 由 Meta scan 重新识别。

## 十、持续运行时与后续方向

### 持续运行时

continuous worker 是独立常驻进程，一个进程承载多个 runtime session；`transfer.runtime_leases` 维护 owner/lease/heartbeat/fencing，不进入 bounded execution claim。业务 Kafka 与 PostgreSQL/MySQL/Oracle CDC 共用唯一 consumer/apply/CAS 主循环，不建立第二套 CDC consumer：

```text
业务 Kafka topic / 数据库 CDC 的 Infra Kafka topic
  -> ChangeStreamReaderProvider
  -> 对应 record / Debezium ChangeEvent adapter
  -> 同一 Continuous Worker / ChangeApplyWriter
  -> 目标 PartitionedTableChangeApplyProvider
  -> business table + apply ledger (one target transaction)
  -> transfer.sync_states per-partition position CAS
```

业务 Kafka 与 CDC 的来源／目标范围统一见[支持矩阵](transfer-任务语义与同步模式.md#三当前支持矩阵)；类型、协议、租约、交付与恢复约束由[Continuous 运行时](transfer-任务语义与同步模式.md#六continuous-运行时)及[配置正文](transfer-基本概念及配置说明.md#八continuouskafka-v1-契约与当前实现边界)维护。错误处理、DLQ 来源隔离及 replay 目标限制统一见[Kafka 与 DLQ 正式契约](../../docs/spec/tasks/Transfer-Kafka与DLQ.md#transfer-业务-kafka-dlq-与-bounded-replay-v1-契约)。bounded replay 仍由独立 bounded worker claim execution，其主状态与失败边界见[Replay](transfer-任务语义与同步模式.md#五状态checkpoint-与-replay)。

Oracle Spatial 在捕获侧使用 generation-owned 镜像，随后进入同一 Debezium 主路径。Oracle Free 对三维 `SDO_GEOMETRY` 的 `TO_WKBGEOMETRY` 会丢弃 Z，因此 XY 使用 WKB BLOB、XYZ 使用 `TO_GEOJSON` CLOB，adapter 在同一入口归一化为 EWKB；完整源约束见[Oracle CDC](transfer-基本概念及配置说明.md#oracle-cdc)。Oracle CDC target 与普通 bounded table write 分别消费 partitioned apply 和 table write session，不共享 CDC ledger 语义；schema、ledger 所有权与字段限制统一见[CDC 正式契约](../../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)。

### 容器子表约束

container child table transfer 已进入主链路：容器 item 仍保持 `data_type=container`，任务端点以 `data_type=table` 加 `source.options.child_name` 选择已由 Meta 深扫确认的 child；不得把 child 展开为平行的 Meta item 或使用 provider 私有 layer/table 参数。

### 尚未完成的增强

后续增强的当前缺口、未支持项及实施前置条件统一由[后续能力清单](../../docs/next/transfer后续能力清单.md)维护，按以下主题查阅：[Bounded 同步](../../docs/next/transfer后续能力清单.md#一bounded-同步)、[业务 Kafka](../../docs/next/transfer后续能力清单.md#二业务-kafka)、[数据库 CDC](../../docs/next/transfer后续能力清单.md#三数据库-cdc)、[Continuous 运行时与观测](../../docs/next/transfer后续能力清单.md#四continuous-运行时与观测)、[通用目标与执行增强](../../docs/next/transfer后续能力清单.md#五通用目标与执行增强)。现有能力的可执行组合仍以[当前支持矩阵](transfer-任务语义与同步模式.md#三当前支持矩阵)和相应 Provider 声明为准。

### 架构说明与验收范围

本页说明链路和职责，架构或能力摘要不作为当前版本的运行通过记录。测试分层与公共入口遵循[测试与验收规范](../../docs/spec/addp测试与验收规范.md#三标准入口)，按目的直接查阅：

| 阅读目的 | 权威入口与范围 |
| --- | --- |
| 常规模块、Go 定位、PostgreSQL 与 Hosted runner 回归 | [README 标准门禁](../README.md#常规模块与专项门禁)：统一维护命令、环境前提、身份与测试隔离要求；runner 回归不等于真实 Hosted 验收。 |
| 格式集成与 Oracle CDC 专项 | [ArcGIS 门禁](../README.md#arcgis-开放格式专项)、[Oracle CDC 入口缺口](../README.md#oracle-cdc-专项入口缺口)：保留临时对象权限、清理与授权故障窗口；普通门禁成功或未激活专项不能证明 Oracle CDC 矩阵通过。 |
| 固定版本证据、失败与修复、清理及未覆盖项 | [分版本证据索引](../CLAUDE.md#专项约束与分版本证据)：分别认定所列源码与运行尝试，不作为当前 HEAD 已重跑的证明。 |
| 操作、身份与结果核对 | [任务表 API](tables/tasks表.md#五api)、[快速开始验证](快速开始.md#五开发验证)、[任务语义验证范围](transfer-任务语义与同步模式.md#十一验证范围)：区分用户与 TaskProvider 服务身份、对应 Permission 和完整执行链路。 |

目标数据、原子提交、来源隔离、真实停止／恢复与清理须按对应场景核验；未运行、失败、跳过或未覆盖项不计为通过。各后续增强的独有验收边界见[清单验收范围与证据](../../docs/next/transfer后续能力清单.md#六验收范围与证据)。
