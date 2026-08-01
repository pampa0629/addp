# Transfer 任务语义、增量同步与持续运行时设计

更新时间：2026-07-25

状态：阶段 0/1、工作包 2A-2D、3A-3E、4A-4H 已完成。业务 Kafka DLQ、只读管理、payload availability、task-owned cleanup 与 task-private runtime/control state 生命周期治理、隔离目标 bounded replay v1，以及 PostgreSQL/MySQL 数据库 CDC、人工 additive schema migration 和 schema blocked 统一告警闭环已完成公开 API、Console 操作闭环和真实全生命周期 E2E；Oracle CDC 已明确延期，不进入当前实施序列。Redpanda v24.3.18 已完成单机与生产拓扑认证，并晋级为唯一正式 Infra Kafka broker 实现。

本文整理 Transfer 全量、批增量、Kafka 流式源、数据库 CDC 和持续运行时的概念与推荐技术路线。bounded/watermark、业务 Kafka continuous、业务 Kafka DLQ、隔离目标 bounded replay，以及 PostgreSQL/MySQL CDC v1 已完成；Oracle CDC 已延期，自动 DDL 仍未实现。

当前稳定实现仍以以下文档为准：

- `docs/spec/addp任务体系规范.md`
- `transfer/docs/transfer-基本概念及配置说明.md`
- `transfer/docs/design.md`

当前实现支持 bounded snapshot 的 `replace` / `append`、PostgreSQL bounded watermark incremental、业务 Kafka keyed JSON continuous upsert + `block|dead_letter`、业务 Kafka 显式 offset 范围到新 PostgreSQL 隔离表的 bounded replay，以及 PostgreSQL/MySQL 单表 CDC initial snapshot + upsert/delete。continuous/CDC 使用目标 monotonic apply、业务库 ledger、Infra state CAS 与 runtime fencing；replay 使用独立 execution-scoped apply identity，不读写主状态。

## 一、本文要解决的问题

本文集中回答：

1. Transfer 的任务类型、执行边界、装载方式、触发方式和目标应用方式如何区分。
2. 全量、watermark 批增量和 CDC 持续同步分别是什么。
3. 增量状态与 execution checkpoint 有什么区别。
4. 当前 Go batch 主链路是否需要被 Flink 等流批一体框架替换。
5. 不使用 Flink 时能否支持 MySQL、Oracle CDC。
6. Kafka 作为用户数据源时如何接入。
7. Kafka 作为 ADDP 内部 CDC 总线时是否属于 infra。
8. 后续实现应按什么顺序推进。

本文不重新定义 ResourceLocator、资源树、Meta data item、任务体系公共字段和 System Engine Instance 基础语义。

## 二、设计结论摘要

当前讨论形成以下方向：

1. Transfer 对外只保留一个稳定任务类型：`task_type=sync`。
2. 导入、导出是 Manager 等调用方的用户动作，不是 Transfer 执行类型。
3. `manual` / `scheduled` 是触发方式；`realtime` 不是第三种触发方式。
4. 执行边界分为 `bounded` 和 `continuous`。
5. 装载方式分为 `snapshot` 和 `incremental`。
6. 增量变化识别方式至少包括 `watermark`、`manifest`、`kafka` 和 `cdc`。
7. 目标应用方式与源端装载方式正交，至少需要 `replace`、`append`、`upsert`、`upsert_delete`。
8. 增量主状态由 Transfer 私有状态表保存；execution metadata 只保存本次执行快照和诊断信息。
9. 第一阶段一致性基线是 `at-least-once + 目标幂等`，不宣称通用端到端 exactly-once。
10. 保留当前 Go bounded batch runtime；后续增加独立 continuous runtime，不整体改写为 Flink。
11. 数据库日志 CDC 统一使用 Debezium Connector 捕获并经 Infra Kafka 交给 Transfer；MySQL binlog 已完成，Oracle redo/LogMiner 已延期且不进入当前实施序列。
12. 用户 Kafka 数据源注册为 System Engine；Infra Kafka 不进入 System engines，也不出现在资源树。
13. Infra Kafka 与业务 Kafka 可以在小型部署中物理共用集群，但必须保持不同身份、凭据、ACL、topic namespace 和生命周期。
14. 当前 Asynq worker 继续作为 bounded worker；新增独立 continuous worker 进程角色，一个进程承载多个 runtime session，并通过 DB lease/fencing 支持多实例。
15. Debezium 运行在独立 Kafka Connect distributed 集群中，由 Transfer capture supervisor 通过受控 API 管理，不嵌入 Transfer Go 进程。
16. Schema 变化第一阶段默认严格阻塞，不静默忽略字段，也不默认自动执行目标 DDL。
17. 业务 Kafka 第一版只支持 keyed JSON record -> PostgreSQL/MySQL monotonic upsert；业务目标库通过 partition apply ledger 原子推进 `next_offset`，普通 append 不进入第一版。
18. 数据库 CDC 第一版只支持 PostgreSQL/MySQL 有稳定主键的单表 initial snapshot -> snapshot/upsert/delete -> PostgreSQL/MySQL 新目标表；pause 保持捕获，stop 是不可逆终态。

## 三、Transfer 的稳定定位

Transfer 是 ADDP 的数据搬运与同步执行模块：

```text
source
  -> change/load strategy
  -> transform
  -> target apply strategy
  -> target
```

Transfer 负责：

- 任务定义与执行配置。
- planner 和执行编排。
- 批大小、分区并行和背压策略。
- transform 编排。
- 增量状态、位点和 checkpoint 协调。
- 目标应用策略。
- execution、日志、指标和运行状态。
- 写后 Meta scan 或持续任务中的 Meta 刷新协调。

具体引擎连接、catalog、native table 读写和 change stream 读取能力属于 `common/engine` 插件；具体 encoded 格式读写属于 `common/format`。

### 3.1 导入和导出为什么不是 Transfer 任务类型

用户在 Manager 中看到的“导入”“导出”表达交互意图：

| 用户动作 | Transfer 执行语义 |
|---|---|
| 文件导入数据库表 | bounded snapshot，通常 replace |
| 数据库表导出文件 | bounded snapshot，通常 replace |
| 周期导出快照 | scheduled bounded snapshot |
| 周期同步变化 | scheduled bounded incremental |
| 数据库 CDC 同步 | continuous incremental |

Manager 负责资源选择、预览、字段映射和入口文案；Transfer 负责统一同步任务的执行。因此 Transfer planner 不应按 `import` / `export` 建立分支。

调用来源通过统一 execution 的 `source=manager|console|orchestrator|...` 表达。如果确实需要审计“导入”或“导出”动作，应由调用方审计上下文表达，不应重新引入 Transfer `intent` 事实字段。

## 四、任务语义的正交维度

Transfer 任务不能只用“全量、增量、实时”三个词描述。建议拆成以下维度。

### 4.1 任务类型

```text
provider=transfer
task_type=sync
```

全量、增量、Kafka、CDC 都是 `sync` 内部配置，不扩展为新的 `task_type`。

### 4.2 执行边界

| 取值 | 含义 |
|---|---|
| `bounded` | 本次执行有确定结束条件，处理到冻结上界后结束。 |
| `continuous` | 持续等待并处理新事件，直到被停止、失败或失联。 |

`batch` 更适合描述执行实现或批大小，不足以准确表达是否有界。Kafka 消费也会按 batch poll，但它仍然是 continuous execution。

### 4.3 装载方式

| 取值 | 含义 |
|---|---|
| `snapshot` | 读取本次执行范围内的完整源快照。 |
| `incremental` | 只读取某个已提交边界之后的变化。 |

`snapshot` 只描述源端读取范围，不自动等于目标 `replace`。例如 snapshot + append 在技术上可能成立，但容易重复写入，应通过合法组合校验进行限制。

### 4.4 变化识别方式

| 取值 | 适用对象 | 能力边界 |
|---|---|---|
| `watermark` | 数据库表 | 适合 bounded batch incremental；通常无法可靠发现删除。 |
| `cdc` | 数据库 redo/binlog/WAL 或 CDC topic | 可表达 insert/update/delete 和事务位点。 |
| `manifest` | 文件、对象、快照集合 | 基于 fingerprint、etag、mtime 或版本清单识别变化。 |

自增 ID 是 watermark 的一种受限形态，只适用于纯新增数据，不应再建立平行概念。

### 4.5 触发方式

统一任务体系规定 execution `trigger_type` 只有：

| 取值 | 含义 |
|---|---|
| `manual` | 用户、API、Console、Orchestrator 或其他模块显式触发。 |
| `scheduled` | owner scheduler 按计划触发。 |

continuous 任务也可以被手动或定时启动。因此 `realtime` 不进入 `trigger_type`。

### 4.6 目标应用方式

建议后续把目标策略概念统一为 apply strategy：

| 取值 | 含义 | 常见组合 |
|---|---|---|
| `replace` | 清理或重建目标，再写入本次结果。 | bounded snapshot |
| `append` | 仅追加，不处理重复和更新。 | 事件日志、纯新增数据 |
| `upsert` | 按稳定键新增或更新。 | watermark、无删除 CDC |
| `upsert_delete` | 按稳定键新增、更新和删除。 | 完整 CDC |

稳定字段已 clean break 为 `target.policy.apply_mode=replace|append|upsert|upsert_delete`；当前 PostgreSQL/MySQL 数据库 CDC v1 已开放 `upsert_delete`。旧 `write_mode` 已拒绝，不保留字段别名。

Transfer 拥有应用策略，但目标引擎必须提供真实写入能力，并声明键要求、提交边界、删除能力和幂等语义。不能仅靠 Transfer 配置中的字符串推导目标支持 upsert。

### 4.7 合法组合

| 执行边界 | 装载方式 | 变化识别 | 推荐目标策略 | 示例 |
|---|---|---|---|---|
| bounded | snapshot | 无 | replace | 一次性导入、导出 |
| bounded | incremental | watermark | upsert | 定时表增量同步 |
| bounded | incremental | manifest | append/upsert | 周期文件发现 |
| continuous | incremental | cdc | upsert_delete | 数据库 CDC |
| continuous | incremental | Kafka record | upsert | 第一版仅 keyed JSON 业务事件流；append 后置 |

`continuous + snapshot` 不作为普通稳定组合。数据库 CDC 的初始化 snapshot 是 CDC runtime 的 bootstrap 阶段，不是一个长期 continuous snapshot 模式。

## 五、Bounded snapshot 与当前实现

当前 Transfer 已有稳定的 bounded table/raw-copy 主路径：

```text
task -> Asynq worker -> planner -> executor
     -> common engine/format reader
     -> table batch / byte stream
     -> transform
     -> common engine/format writer
```

这条路线已经具备连续 read/write session、批处理、进度和 restartable retry。它适合继续承担：

- 一次性全量搬运。
- 周期性全量快照。
- 后续 watermark bounded incremental。
- 文件 manifest bounded incremental。

不应为了未来 CDC 而推翻当前 planner、executor 和 common provider 边界。

### 5.1 当前 checkpoint 的真实含义

snapshot checkpoint 仍是观测点；watermark incremental 的 execution 间恢复使用独立 `transfer.sync_states`，不消费 snapshot checkpoint：

- 记录 batch index、累计行数和 source/target marker。
- 用于进度展示和故障诊断。
- retry 创建新 execution 并从头执行。
- 不会消费旧 execution marker 从断点继续。

它不等于增量主状态，也不等于 checkpoint resumable。

## 六、Watermark 批增量

Watermark 是最适合作为第一阶段实现的增量能力，但不能只实现：

```sql
WHERE updated_at > :last_watermark
```

多个记录可能拥有相同时间值，数据库时间精度也可能低于写入并发度。只保存单字段 watermark 会漏数。

### 6.1 推荐复合游标

使用稳定排序的复合游标：

```text
(watermark_field, tie_breaker_key)
```

例如：

```text
(updated_at, id)
```

读取边界应等价于：

```sql
WHERE
  updated_at > :last_time
  OR (updated_at = :last_time AND id > :last_id)
ORDER BY updated_at, id
```

### 6.2 冻结执行上界

每次 bounded incremental 开始时先获得稳定上界 `end_cursor`，然后读取：

```text
(last_committed_cursor, end_cursor]
```

这样本次 execution 有确定结束条件，执行期间新产生的数据留给下一次执行。

需要继续明确：

- watermark 的时区和精度。
- `NULL` 值处理。
- tie breaker 是否必须唯一且不可变。
- 迟到、乱序和回写历史数据的处理。
- 是否提供 lookback window，以及重复记录如何被目标幂等吸收。

### 6.3 删除边界

普通 `updated_at` watermark 无法发现物理删除。第一阶段必须明确：

- watermark 只支持 insert/update；或者
- 源表提供显式 tombstone / soft-delete 字段。

不得把 watermark 同步宣传为完整数据库 CDC。

### 6.4 源端契约与 lookback

watermark 正确性依赖源字段本身可靠。Transfer 创建任务时应明确提示并校验以下前置条件：

- 所有需要同步的 insert/update 都会更新 watermark 字段。
- watermark 值不会因时钟回拨、业务修复或手工更新而倒退。
- tie breaker 唯一、稳定且不可变。
- 从只读副本读取时，复制延迟不会超过任务允许的迟到范围。
- 源字段的时区、精度和事务内赋值时机已经明确。

如果业务更新根本没有修改 watermark 字段，Transfer 无法通过读取策略自行发现该变化。数据库 trigger、生成列或业务代码统一赋值可以作为源系统治理方式，但 ADDP 不自动修改用户源表。

可以提供显式 lookback window 作为迟到数据风险缓解措施：

```text
effective_start = committed_watermark - lookback_window
```

lookback 会重复读取已经处理的数据，因此只允许与幂等 upsert 目标组合。它只能覆盖窗口范围内的迟到或副本延迟，不能修复从未更新 watermark 的记录，也不能作为完整一致性保证。

## 七、增量状态、execution checkpoint 与 replay

### 7.1 三类状态不能混用

| 状态 | Owner | 用途 |
|---|---|---|
| 任务定义 | `transfer.transfer_tasks` | 保存未来如何同步。 |
| 增量主状态 | Transfer 私有状态表 | 保存下一次从哪里继续。 |
| execution checkpoint | `common.task_executions.metadata` | 保存本次执行进度和诊断快照。 |

增量状态需要独立的 Transfer 私有表，建议语义至少包括：

```text
task_id
source_identity
partition
committed_position
position_type
position_version
state_version
updated_execution_id
updated_at
```

`committed_position` 是 provider 可解释的结构化位置，可以表达复合 watermark、Kafka offset、Oracle SCN、PostgreSQL LSN 或 manifest version。

### 7.2 提交规则

只有目标批次提交成功后，才能推进源位置：

```text
read changes
  -> transform
  -> target commit
  -> compare-and-set source position
```

状态更新必须携带 `state_version` 或 fencing token，防止旧 worker、重复 worker 或并发 execution 覆盖新位置。

### 7.3 任务级并发

同一增量任务的主状态默认只能由一个 active execution 推进。开始增量开发前，需要先把“检查任务不在运行”和“占用执行权”收敛为数据库原子 claim，不能依赖先查再改的非原子流程。

分区型源可以由多个 worker 并行处理不同 partition，但同一 partition 同一时刻只能有一个合法 owner。

### 7.4 Resume 与 replay

resume 和 replay 是执行参数，不是新的 `task_type`：

| 方式 | 起止边界 | 是否推进主状态 |
|---|---|---|
| resume | 从主 committed position 到本次上界 | 成功后推进 |
| replay | 用户指定历史范围 | 永不推进主状态 |

幂等 upsert/delete 只能吸收同一状态的重复应用，不能阻止旧事件覆盖同 key 的新状态；普通 append 接受重复也不能解决顺序正确性。因此 replay v1 只允许写入不存在的新隔离目标，不允许写回主任务目标。replay 必须从原业务 Kafka 的显式 partition/offset 范围按源顺序读取；DLQ 只保存被跳过的记录，不能冒充可重建完整目标状态的历史源。

replay 使用独立 execution 和 execution-scoped apply identity，不读取或推进 `transfer.sync_states`，也不提供“顺便覆盖主水位”的开关。主 continuous task 可以继续运行，因为 replay 目标与主目标强制隔离；资源容量和并发由 Transfer runtime 统一限制。

## 八、持续同步与 ChangeEvent

continuous runtime 处理的是无限事件流，不是把当前 batch executor 放进无限循环。

### 8.1 统一变化事件

不同 CDC 和消息系统进入 Transfer 后，应先归一化为内部 `ChangeEvent`。建议语义示例：

```json
{
  "operation": "update",
  "key": {"id": 1001},
  "before": {"id": 1001, "name": "old"},
  "after": {"id": 1001, "name": "new"},
  "source": {
    "system": "oracle",
    "database": "ORCL",
    "schema": "APP",
    "table": "USERS",
    "partition": "0",
    "position": {
      "kafka_offset": 98765,
      "oracle_scn": "123456789"
    }
  },
  "transaction": {"id": "..."},
  "occurred_at": "2026-07-12T10:00:00Z"
}
```

稳定操作语义建议为：

- `upsert`
- `snapshot`
- `insert`
- `update`
- `delete`

Debezium JSON、Kafka tombstone、Oracle SCN 等协议细节由 source adapter 解释，不能渗透到通用 planner、transform 和目标 writer。

### 8.2 Change stream provider 边界

工作包 2A 已将专用能力升级到正式 Engine Provider 规范。Provider 返回原始 ChangeRecord 和 provider position，不直接返回 ChangeEvent：

```go
type ChangeStreamReaderProvider interface {
    StoreProvider
    OpenChangeStream(
        ctx context.Context,
        connInfo ConnectionInfo,
        topic CatalogPath,
        opts ChangeStreamReadOptions,
    ) (ChangeStreamReader, error)
}

type ChangeStreamReader interface {
    Poll(ctx context.Context, maxRecords int) (*ChangeRecordBatch, error)
    Assignments() []string
    Pause(ctx context.Context, partitions []string) error
    Resume(ctx context.Context, partitions []string) error
    Close(ctx context.Context) error
}
```

JSON 解码、ChangeEvent 归一化和 ChangeApplyWriter 属于 Transfer continuous runtime。目标使用声明完整语义的 `PartitionedTableChangeApplyProvider`，当前实现为 PostgreSQL 与 MySQL：Provider 不理解 ChangeEvent，只接收已映射表行、目标 keys、每条记录 position 和单 partition 批次边界，并把业务数据与目标 apply ledger 原子提交。

Kafka topic 和数据库 CDC 不能伪装成当前 `BatchReadableProvider`。Kafka poll 可以按批返回消息，但它的 offset、rebalance、持续生命周期和 checkpoint 语义与 bounded table batch 不同。

### 8.3 交付保证

第一阶段统一采用：

```text
at-least-once delivery + idempotent target apply
```

处理顺序：

1. 拉取一批事件。
2. 解码并归一化 ChangeEvent。
3. transform。
4. PostgreSQL 在业务目标库同一事务提交目标批次与 partition apply ledger。
5. Transfer 持久化本分区 committed position。
6. 继续处理下一批。

任意外部数据库目标与 Kafka offset 之间通常不存在分布式原子事务。如果进程在步骤 4 和 5 之间崩溃，同一批事件会再次执行。因此：

- upsert/delete 必须基于稳定键。
- 第一版每个 task 使用不可变 `apply_identity`；PostgreSQL 目标账本固定为业务目标库的 `addp_transfer.apply_positions`，MySQL 目标账本固定为同一目标数据库的 `_addp_transfer_apply_positions` InnoDB 私有表。
- Transfer 按 partition 拆批；每条 mapped row 携带消费后的 position，同批同 key 保留最高 position 的最后状态。
- Provider 必须跳过 ledger 已覆盖的记录，并在同一事务推进目标行与 `next_offset`，防止过期 runtime 写回旧状态。
- 第一阶段不得宣称通用端到端 exactly-once。

### 8.4 Continuous execution 生命周期

一个 continuous execution 表示一次 runtime session：

- 每次启动创建新的 execution。
- 正常停止、失败或失联后，本 execution 结束。
- restart/resume 创建新 execution，从 committed position 恢复。
- 已结束 execution 不得重新变回 running。

continuous execution 不使用 0 到 100 的业务进度作为主要观测指标，应展示：

- 每 partition 当前 position。
- source latest position。
- consumer lag。
- events/second、bytes/second。
- last event time。
- last checkpoint time 和 checkpoint age。
- retention health 与 checkpoint health；前者表达恢复窗口，后者只在存在 lag 时表达真实 position commit 是否长期不推进。
- rebalance、retry 和 dead-letter 摘要。
- runtime heartbeat。

### 8.5 取消、暂停与恢复

持续运行时必须支持真实的 context cancel、停止 poll、完成或放弃当前批次、关闭 reader/writer、释放 partition ownership 并一致更新 execution。

只修改数据库状态、不停止实际 worker 不是取消。continuous task 的私有 pause/stop 已真实取消 runtime、关闭 source/target 并一致结束 execution；标准 TaskProvider execution cancel 仍未开放，因此 `supports_cancel=false` 保持不变。

### 8.6 Meta scan

bounded 任务可以在成功后触发一次 Meta deep scan。continuous 任务长期不结束，不能沿用“写完后扫描”：

- 结构首次建立时扫描。
- 目标 schema 发生变化时触发防抖扫描。
- 必要时按低频周期刷新统计。
- 不得逐事件或逐 batch 触发 Meta scan。

### 8.7 Schema 变化策略

CDC 或 Kafka 消息中的 schema 变化与 Meta scan 是两个不同问题：Meta scan 负责重新识别目标事实，不能代替目标 DDL 决策。

第一阶段默认采用严格策略：

```text
schema change detected
  -> 停止对应 task/partition
  -> 保存 source/target schema diff
  -> 当前 execution 以 failed 结束
  -> error_details.code=schema_change_blocked
  -> 用户确认或调整目标
  -> 创建新 execution 从 committed position 恢复
```

不能在目标缺少字段时静默忽略该字段继续运行，否则 execution 表面成功，源目标数据已经不一致。

后续可以显式提供：

| 策略 | 行为 |
|---|---|
| `fail` | 默认；检测到不兼容变化立即阻塞。 |
| `manual` | 生成 schema change request，由用户确认后执行目标变更。 |
| `additive` | 仅在用户显式开启、目标插件声明支持且变更为安全新增 nullable 字段时自动执行。 |

删除字段、修改主键、收窄长度、改变不可兼容类型等破坏性 DDL 不进入自动传播路线。目标 DDL 执行完成后再触发防抖 Meta scan，更新目标 data item 事实。

## 九、为什么当前不整体引入 Flink

Flink 擅长：

- 事件时间和窗口。
- 大规模有状态计算。
- 流式 join、聚合和 CEP。
- 流批统一 SQL/算子执行。
- 大规模并行、状态快照和恢复。

但 ADDP 当前首先需要解决的是：

- 统一任务语义。
- watermark/offset 状态。
- 目标幂等。
- Kafka partition ownership。
- CDC envelope 归一化。
- continuous execution 生命周期。
- Oracle 日志捕获组件集成。

Flink 不会替代 Oracle redo log 捕获，也不会自动让任意外部数据库目标获得 exactly-once。

推荐架构是控制面和语义统一、执行面分开：

```text
              Transfer task/control plane
         task / state / execution / observability
                         |
             +-----------+-----------+
             |                       |
    bounded batch runtime    continuous change runtime
      Go + Asynq worker       long-running supervisor
             |                       |
      table/content batch       ChangeEvent batches
```

当前 Go planner/executor 和 common engine/format provider 继续承担 bounded runtime。continuous runtime 属于同一个 Transfer 模块，但使用适合长驻进程的 supervisor/worker，不把无限消费循环塞进当前一次性 Asynq task。

### 9.1 Bounded worker 继续保留

当前 `transfer/backend/cmd/worker` 继续承担 bounded execution：

- 一次性 snapshot。
- 定时 snapshot。
- watermark bounded incremental。
- manifest bounded incremental。
- bounded replay。

一个 bounded worker 进程通过 Asynq concurrency 并发处理多个有限任务；执行结束后 handler 返回并释放 slot。后续可以在部署和文档中称为 `transfer-bounded-worker`，但不为兼容旧命名保留两套启动入口。

### 9.2 Continuous worker 进程模型

新增独立进程角色 `transfer-continuous-worker`，但它仍属于 Transfer 模块：

```text
Continuous Worker Process
  -> Supervisor
    -> Runtime Session: task A
      -> partition worker 0
      -> partition worker 1
    -> Runtime Session: task B
      -> partition worker 0
```

一个 continuous task 对应一个 runtime session，不对应一个操作系统进程或容器。一个 continuous worker 进程可以承载多个 task session，每个 session 内部再按 partition 创建受限 goroutine。

开发环境可以默认启动一个 continuous worker 实例，但架构不能依赖全局单例。生产环境允许多个实例，通过以下状态原子 claim runtime：

```text
owner_instance_id
lease_until
heartbeat_at
fencing_token
```

同一 task 同一时刻只能有一个合法 owner。第一阶段同一 task 的全部 partition 归同一个 worker 实例；只有单任务吞吐达到明确瓶颈后，才讨论跨实例拆分 partition。

continuous worker 必须设置容量上限，例如 active task 数、总 partition worker 数、内存缓冲和未提交事件数。容量耗尽时新 session 保持 pending，不无限创建 goroutine。

### 9.3 Continuous 启动与恢复

建议启动流程为：

1. API 原子写入任务 desired state，并创建 pending execution。
2. continuous supervisor claim pending execution 和 runtime lease。
3. 建立 source/target session，从 committed position 恢复。
4. 定期续租、写 heartbeat 和运行指标。
5. 停止或失败时结束当前 execution 并释放 lease。
6. 自动恢复需要创建新 execution，不复用旧 execution。

不使用 Asynq 承载长期 continuous session；Asynq 仍只用于 bounded execution 和必要的短期控制任务。

只有出现明确的事件时间窗口、流式 join、大规模状态计算、统一流式 SQL 或现有 runtime 无法承担的并行规模时，再把 Flink 作为可选 runtime 评估；Flink 不作为支持 Kafka、MySQL CDC 或 Oracle CDC 的前置条件。

## 十、不使用 Flink 的数据库日志 CDC

### 10.1 MySQL CDC 优先路线

MySQL 先于 Oracle 实现。原因不是建立另一套 CDC 架构，而是当前开发环境已经具备 MySQL 8.0、ROW/FULL binlog、Debezium MySQL Connector 和既有 Infra Kafka/continuous runtime，可先验证第二种数据库日志源是否真正复用统一链路：

```text
MySQL binlog
  -> Debezium MySQL Connector
  -> Infra Kafka
  -> Transfer continuous runtime
  -> PostgreSQL target
```

MySQL CDC v1 先冻结为单表、稳定非空主键、`initial` snapshot、schemaless JSON、`upsert_delete` 和不存在的新 PostgreSQL/MySQL 目标表。源库必须满足 `log_bin=ON`、`binlog_format=ROW`、`binlog_row_image=FULL`、唯一正整数 `server_id`，并为 connector 提供最小 snapshot/binlog 权限；GTID 不是 v1 前提，捕获位点仍只由 Kafka Connect 管理。

3E-A 已把 `transfer.capture_resources` 收敛为 engine-neutral generation 主事实；PostgreSQL slot/publication 与 MySQL connector server id 分别只存在于一对一 provider-owned 子事实。generic 主表不保留兼容字段，也不使用空字符串、伪资源名或 JSON 猜测 provider。

### 10.2 Oracle 后续路线

```text
Oracle Redo / Archive Log
          -> Debezium Oracle Connector
          -> Infra Kafka
          -> Transfer continuous runtime
          -> target engine
```

职责：

| 组件 | 职责 |
|---|---|
| Debezium | 解析 Oracle redo、维护 SCN、事务顺序和 snapshot/增量衔接。 |
| Infra Kafka | 持久化变化事件、削峰、保留 replay 窗口、隔离捕获与目标写入。 |
| Transfer | 消费事件、转换、目标应用、execution 和 committed position。 |
| Target engine plugin | 批量 upsert/delete、提交边界和幂等实现。 |

### 10.3 Oracle 捕获方案判断

| 方案 | 定位 |
|---|---|
| Debezium + LogMiner | 推荐第一路线；无需 Flink，但需正确配置 Oracle 日志和权限。 |
| Oracle GoldenGate | 商业能力强，成本和授权约束明显。 |
| Oracle XStream | 需要按 Oracle 版本和许可单独核验。 |
| OpenLogReplicator | 可研究，但兼容性、运维和长期维护风险更高。 |
| ADDP 自己解析 redo | 不采用。 |
| `updated_at` 轮询 | 只属于 watermark 批增量，不是完整 CDC。 |

Oracle 接入前至少验证：

- Oracle 版本与 Debezium 兼容矩阵。
- ARCHIVELOG。
- supplemental logging。
- Connector 用户权限。
- RAC、CDB/PDB。
- LOB、DDL、表重建、长事务和归档日志保留。
- XStream、GoldenGate 等相关 Oracle 许可。

### 10.4 Snapshot 与 CDC 交接

首次接入不能简单执行“先全量，完成后再开始 CDC”，否则两者之间可能出现数据空洞。正确路线需要：

1. 建立或记录 CDC 日志起点。
2. 执行一致性 snapshot。
3. 捕获并缓冲 snapshot 期间产生的变化。
4. snapshot 应用完成后继续消费积压变化。
5. 进入稳定 continuous 消费。

这一协调优先使用对应 Debezium Connector 已验证的 snapshot 语义，不由 Transfer 自行拼接源库全量查询与 binlog/redo 日志。

### 10.5 Debezium 托管模式

推荐采用独立 Kafka Connect distributed 集群运行 Debezium：

```text
Transfer Capture Supervisor
          -> Kafka Connect REST API
          -> Debezium Connector
          -> Infra Kafka
```

职责边界：

| 组件 | 职责 |
|---|---|
| Kafka Connect | 运行 connector、worker 故障漂移、connector config/offset/status 内部 topic。 |
| Debezium Connector | 捕获数据库日志、维护捕获位点和 snapshot 过程。 |
| Transfer capture supervisor | 创建、更新、停止、删除和监控 connector；关联 Transfer task/execution。 |
| Transfer continuous worker | 消费 Infra Kafka、应用目标并维护消费位点。 |

Transfer 不在 Go 进程内嵌入 Debezium Java runtime。Kafka Connect distributed mode提高捕获服务可用性，但不单独构成“不丢数据”保证；仍需同时满足 Kafka 内部 topic/业务 topic 复制、MySQL binlog 或 Oracle archive log 保留、connector offset 持久化和 Infra Kafka retention 要求。

### 10.6 捕获位点与消费位点

需要同时存在两类位点，但不能混为一个状态：

```text
source database
  -- Debezium capture position -->
Infra Kafka
  -- Transfer topic/partition/offset -->
target
```

- Kafka Connect offset 回答“源数据库日志已经捕获到哪里”，由 Kafka Connect 管理；MySQL 可使用 binlog file/position 或 GTID，Oracle 可使用 SCN。
- Transfer committed position 回答“目标已经可靠应用到哪个 Kafka offset”，由 Transfer 管理。
- ChangeEvent 可以携带 MySQL binlog file/position/GTID 或 Oracle SCN 供诊断与审计使用，但目标应用顺序仍以当前 generation 的 Kafka partition/offset 为准。
- Transfer 不复制维护 Kafka Connect 内部 offset，但不能因此省略自己的消费状态。

## 十一、Kafka 作为用户数据源

用户已有 Kafka，或者业务系统直接向 Kafka 写事件时，Kafka 是正常的外部数据引擎，应由用户在 System 注册 Engine Instance。

### 11.1 Catalog

Kafka catalog 可以建模为：

```text
service(cluster)
  -> topic
```

用户选择的稳定资源是 topic，因此建议 `topic` 作为可选择 leaf。partition 数量、leader、副本和状态属于 topic facts/diagnostics，不作为资源树子节点或用户 locator。正式实现前需先把 `topic` 术语和 `type=topic` ResourceLocator 规则补入术语表、catalog model 和路径规范。

### 11.2 Engine 配置

用户注册的 Kafka Engine 至少涉及：

- bootstrap servers。
- TLS/SASL。
- Schema Registry。
- topic allowlist 或访问范围。
- 只读/可写能力。
- 连接测试与权限诊断。

连接凭据由 System 统一加密、审计和按租户管理。

### 11.3 Decoder

Kafka record 必须先由 source adapter 解码：

| decoder | 用途 |
|---|---|
| `record` | 普通 Kafka 消息；第一版要求从 JSON value 提取稳定 key 并进入 upsert。 |
| `debezium` | 解析 Debezium envelope、before/after/op/tombstone。 |

JSON、Avro、Protobuf 和 Schema Registry 是消息编码能力；`debezium` 是变化事件 envelope。两者是不同维度，后续配置不能混成单一 `format` 枚举。

### 11.4 Kafka offset

Transfer 不依赖 consumer auto commit 作为唯一事实源：

- 禁用自动提交。
- Kafka consumer group 可用于分区分配。
- Transfer 私有状态保存每 partition committed offset。
- worker 启动时从 Transfer committed offset seek。
- 目标提交后才推进 offset。
- rebalance 前停止拉取并完成或放弃当前批次。
- 状态推进使用 CAS/fencing。

### 11.5 Backpressure 和异常消息

continuous runtime 至少需要：

- 按目标吞吐动态 pause/resume partition。
- 限制 poll batch、内存和未提交事件数。
- 区分可重试目标错误与不可解析消息。
- 默认遇到不可解析消息停止对应 partition，避免静默丢数。
- 如果未来支持 dead-letter，必须记录原 topic/partition/offset、错误和审计，并明确推进源 offset 的规则。

## 十二、Infra Kafka 与业务 Kafka

如果 ADDP 同时支持内部 CDC 链路和用户 Kafka 源，需要两个清晰角色。

| 角色 | 管理方式 | 用途 |
|---|---|---|
| Infra Kafka | ADDP 部署配置，不进入 System engines | Debezium CDC 中转、内部变化事件、缓冲和 replay。 |
| 业务 Kafka Engine | 用户在 System 注册 | 用户选择 topic 作为 Transfer 源或目标。 |

这不是两条兼容路线，而是两个不同 owner 和生命周期的资源角色。

### 12.1 Infra Kafka

Infra Kafka：

- 不写入 `system.engines`。
- 不出现在资源树。
- 不接受 Meta 自动扫描。
- 用户创建 Oracle CDC 任务时不需要选择内部 topic。
- broker、凭据、topic prefix、retention 和 cleanup 由 ADDP 管理。
- 只承载 ADDP 内部 CDC 事件。

Oracle CDC 任务的业务 source 仍然是用户注册的 Oracle Engine。Infra Kafka 是 runtime 实现细节，不进入公开任务 endpoint。

### 12.2 业务 Kafka Engine

业务 Kafka：

- 进入 System engines。
- 按租户授权和审计。
- topic 可以成为 Transfer 任务显式 source/target。
- ADDP 默认不负责创建、扩容和删除用户 topic。
- 删除 Engine Instance 不删除 Kafka 业务数据。

### 12.3 能否物理共用

开发环境和小型部署可以共用一个物理 Kafka 集群，但逻辑身份必须分离：

```text
Kafka cluster
  -> infra principal / __addp_cdc.* topics
  -> business principal / user topics
```

必须使用：

- 不同账号和 ACL。
- 不同 topic namespace。
- 不同 retention 策略。
- 独立配置入口。
- 明确 cleanup owner。

业务 Engine 不能浏览 infra topic；infra consumer 也不能任意读取用户 topic。生产环境优先独立集群，至少必须做到凭据和 ACL 隔离。

### 12.4 代码复用

两种 Kafka 角色应复用同一套底层能力：

```text
Kafka common capability
  -> client factory
  -> TLS/SASL
  -> topic/partition reader
  -> offset seek
  -> Schema Registry
  -> event decoder
```

绑定来源不同：

- 业务 Kafka 从 System Engine Instance 解析连接信息。
- Infra Kafka 从 ADDP infra 配置解析连接信息。

不能复制两套 Kafka consumer、decoder 或 offset 逻辑。

### 12.5 Infra Kafka 容量与可恢复性治理

Infra Kafka 用于缓冲和 replay，但不是无限存储。至少需要治理：

- broker 磁盘使用率和磁盘高水位。
- topic 写入速率、分区大小和副本状态。
- Transfer consumer lag 条数和 lag 时间。
- connector 捕获延迟。
- Transfer checkpoint age。
- under-replicated partition。
- 按当前写入速率估算的 remaining retention horizon。

retention 同时受时间与容量约束：

```text
允许的最小 replay 时间
  + 最大下游故障恢复时间
  + 峰值写入速率和安全余量
  -> topic retention 与集群容量
```

`retention.bytes` 可以保护磁盘，但也可能提前删除尚未消费的数据，因此不能把它仅描述为防磁盘打满的安全开关。任务 lag 接近 retention horizon 时应进入 degraded/critical 告警，并明确提示可能失去连续恢复能力。

checkpoint 停滞不能由 Monitor 根据 execution `updated_at` 或页面时间猜测。Transfer 必须保存真实 position commit 时间：lag 为 0 时 checkpoint health 为 healthy；lag 大于 0 且真实 position commit age 超过部署阈值时为 degraded；缺少 commit 时间时为 unknown。Monitor 只把该 owner 事实与 recovery circuit、retention health 归一化为实时观测信号，不在本阶段创建持久化告警或通知记录。

ADDP 不自动暂停用户 Oracle 或其他上游业务写入。平台可以暂停新 replay、限制非关键任务、对目标应用背压并发出告警；是否干预上游业务由用户决定。自动暂停 Debezium connector 也必须谨慎，因为暂停期间数据库归档日志仍可能超出保留窗口。

### 12.6 Redpanda 晋级认证矩阵

Infra Kafka 对 Transfer、Kafka Connect 和运维脚本只暴露 Kafka API，不暴露发行版选择。Redpanda 晋级遵守以下边界：

1. broker 镜像、启动参数和部署级 SASL mechanism 只存在于 Infra 部署层；正式服务名固定为 `redpanda`，内外 bootstrap 地址、Connect REST、topic/group/ACL namespace 和持久卷语义保持一致。
2. `INFRA_KAFKA_*` 与 `KAFKA_CONNECT_*` 仍是唯一部署配置入口；发行版名称不得进入任务 JSON、公开 API、System Engine、Transfer 数据库或 execution metadata。
3. Transfer capture supervisor、continuous worker、DLQ、cleanup 和 Kafka Connect 继续走同一实现。禁止按发行版增加 client、connector plan、repository、任务类型、endpoint 或数据库字段分支。
4. 默认开发 profile 与生产 HA profile 只是同一实现的不同拓扑；认证脚本不得成为第二套运行时数据面。
5. 下表全部通过、证据可复现且资源基线被记录后，才能修订 `docs/spec/addp技术栈规约.md` 和部署文档；晋级后必须删除被替换的 Apache Kafka broker 路径。

Redpanda 认证矩阵：

| 编号 | 认证面 | 必须验证的行为 | 通过标准 | 证据入口 |
|---|---|---|---|---|
| RP-01 | 单一数据面 | 默认启动使用唯一 `redpanda` broker、同镜像 `redpanda-init` 与 Kafka Connect 3.6.0.Final | `docker compose config` 中无 Apache Kafka 镜像、`kafka` broker service 或第二个数据面 | profile 静态检查、容器镜像与版本输出 |
| RP-02 | SASL/ACL | `admin`、`connect`、`transfer` 三个 principal 使用部署级机制认证；Connect 只能写 CDC namespace，Transfer 只能读 CDC、读写 DLQ，不能读 Connect 内部 topic 或写 CDC topic | 正向操作成功，所有反向越权操作失败，ACL 列表可审计 | `redpanda-init` 的 `VERIFY_ACL=1` 探针 |
| RP-03 | Kafka Connect | Debezium Connect 3.6.0.Final distributed worker 使用相同 Kafka API 连接并持久化 config/offset/status | REST ready，三个 compact internal topic 正常，worker 重启后 connector 状态恢复 | Connect REST、internal topic describe、重启前后状态 |
| RP-04 | topic/group/admin API | 创建/describe/delete topic，produce/fetch，earliest/latest offset，consumer group create/describe/delete，prefix ACL 与 topic config API | ADDP 现有 admin/client 调用全部成功且错误分类不变 | `redpanda-init` 探针、Go Kafka/Transfer 集成测试 |
| RP-05 | PostgreSQL CDC | Debezium initial snapshot 及 insert/update/delete 经 Infra Kafka 分别进入 PostgreSQL/MySQL 目标 | 数据值、删除、committed position、目标 ledger、connector/slot/publication/topic cleanup 全部符合 E2E 断言 | `ADDP_CDC_DATA_E2E=1` |
| RP-06 | MySQL CDC | Debezium initial snapshot 及 insert/update/delete 经同一数据面分别进入 PostgreSQL/MySQL 目标 | 数据值、删除、committed position、目标 ledger、connector/topic/schema-history cleanup 全部符合 E2E 断言 | `ADDP_MYSQL_CDC_DATA_E2E=1` |
| RP-07 | DLQ | 确定性 keyed DLQ 写入、读取、payload availability 与权限隔离 | DLQ E2E 通过，Connect 不能写 DLQ，清理后 payload 状态按既有规则收敛 | `ADDP_DLQ_KAFKA_INTEGRATION=1` 及 continuous DLQ E2E |
| RP-08 | retention | time/bytes topic config 可读写；超过边界后 earliest offset 前移，resume 对过期 committed position 明确失败 | 不静默跳到 earliest，diagnostics 与既有 degraded/critical 规则一致 | retention 探针、continuous diagnostics E2E |
| RP-09 | Stop cleanup | CDC stop 按 connector -> provider resource -> data/schema-history topic -> group/ACL 清理，业务 Kafka task-owned cleanup 删除 DLQ topic | 重复 cleanup 幂等，权限/网络失败阻断后续数据库删除，无残留 ADDP-owned broker 资源 | PostgreSQL/MySQL CDC 与 DLQ cleanup E2E |
| RP-10 | 重启恢复 | broker 与 Connect 分别重启，持久卷保留 topic、offset、ACL/user 和 connector state，Transfer 从 committed `next_offset` 恢复 | 重启后无数据空洞或回退，旧 worker fencing 仍有效 | 容器重启探针、CDC/continuous recovery E2E |
| RP-11 | 资源占用 | 单 broker 空载、Connect ready、CDC steady 三个阶段记录 CPU、内存、磁盘 | 无 OOM/重启/磁盘异常；结果只作为认证证据，不直接承诺生产容量 | `docker stats --no-stream`、volume size、阶段时间戳 |
| RP-12 | 支持范围门禁 | 搜索任务/API/数据库/能力声明中的发行版分支和部署层中的旧 broker 路径 | 应用契约无 `redpanda` 运行时概念；部署层无 Apache Kafka broker profile | `rg -i redpanda`、Compose 与 schema/Swagger diff |

认证结论只有 `passed` 或 `not-certified`。任何必测项未执行、跳过或失败时均为 `not-certified`，不能以“Kafka API 兼容”替代真实 CDC、cleanup、恢复和资源验证。

### 12.7 Redpanda 生产拓扑认证门禁

单 broker/单 Connect 认证只证明 Kafka API 与 ADDP 当前数据面兼容，不证明生产可用性。生产拓扑认证必须在独立 HA profile 中使用 3 broker、3 副本、producer `acks=all`、至少 2 个 broker 确认后才承诺写入、至少 2 个同组 Kafka Connect worker 和 SASL_SSL；它仍是一个 Infra Kafka 集群和一个 Connect distributed 集群，不建立第二数据面或第二 control plane。

“3 副本、`acks=all`、确认 quorum=2”是 ADDP 的统一耐久性语义，不把实现专有配置名提升为任务或运行时概念。Redpanda 通过 RF=3 的 Raft majority 实现该语义。Redpanda v24.3.18 会接受但忽略 `min.insync.replicas`，因此应用层不得下发或校验该属性；部署门禁直接验证单节点故障仍可提交、双节点故障拒绝提交、已确认记录在硬故障重启后完整恢复。发行版原生健康信息只属于部署观测和认证证据，不进入任务/API/数据库/Swagger/capability。

| 编号 | 认证面 | 必须验证的行为 | 通过标准 |
|---|---|---|---|
| RP-HA-01 | 拓扑与 quorum | 3 broker 形成单一集群，controller/quorum、broker membership、topic replicas 和确认策略可观测 | 三节点均在 membership 中；认证 topic 为 3 replicas，producer 显式 `acks=all`；不存在第二 bootstrap namespace |
| RP-HA-02 | SASL_SSL | 内外 Kafka listener 使用受信 CA 和 hostname 校验，admin/connect/transfer 继续使用 SCRAM-SHA-256 | 正确 CA + hostname 认证成功；无 CA、错误 hostname 或错误密码失败；不启用 insecure skip verify |
| RP-HA-03 | Connect distributed | 两个 Connect worker 使用相同 `GROUP_ID`、config/offset/status topic，connector task 只能由一个 worker 持有 | 两个 worker REST 均 ready；worker membership 可观测；停止当前 owner 后 connector 在另一 worker 恢复 RUNNING |
| RP-HA-04 | broker 故障 | 持续有序写入期间硬杀任一非多数 broker，再恢复该 broker | producer/consumer 无不可恢复错误；记录序列完整、无重复、无空洞；已确认记录在 broker 重启后仍存在，集群最终无 under-replicated partition |
| RP-HA-05 | quorum 边界 | 单 broker 故障仍可读写；同时失去两个 broker 时禁止宣称可用 | 单故障通过；双故障下 `acks=all` 明确失败且恢复后数据一致，不能通过降低 acks 或副本数绕过 |
| RP-HA-06 | CDC 与 cleanup | PostgreSQL/MySQL CDC、DLQ、retention、Stop cleanup 在 3 replicas/确认 quorum=2 和双 Connect worker 下复用现有 E2E | 所有既有断言通过；connector、slot/publication、topic/group/ACL 无残留 |
| RP-HA-07 | 持续负载与资源 | 在固定时长和固定消息大小下记录吞吐、p95/p99、CPU、内存、网络与磁盘增长 | 无 OOM/restart/under-replicated 残留；所有采样参数与结果进入证据，不把开发机结果当生产容量承诺 |
| RP-HA-08 | 契约门禁 | HA/TLS 节点、证书和故障注入只存在于部署/认证层 | 任务/API/数据库/Swagger/capability 无拓扑或发行版字段 |

证书由认证脚本在操作系统临时目录生成并以只读 volume 注入，不提交私钥或长期测试证书。HA profile 不进入默认 `up.sh`，避免开发机误启动生产拓扑；认证结束后必须清理 HA 资源并恢复默认单机 Redpanda profile。

2026-07-26 已通过 RP-HA-01 至 RP-HA-08：Redpanda v24.3.18 以 3 broker、RF=3、`acks=all`、确认 quorum=2 和 `write.caching=false` 完成单 broker `SIGKILL`、双 broker 无 quorum、5000 条连续序列、双 Connect worker owner failover、SASL_SSL、PostgreSQL/MySQL CDC、DLQ、retention、Stop cleanup 与恢复验证。固定性能探针为 20000 条 1024-byte 消息、目标 2000 records/s；正式 profile 晋级后的本机样本为 1997.80 records/s、p95 8 ms、p99 28 ms，仅作为认证证据。可重复入口为 `bash scripts/test/certify-infra-kafka-ha.sh`。

## 十三、建议配置形态

13.1 的 PostgreSQL watermark、13.2 的业务 Kafka continuous、13.3 的 PostgreSQL CDC v1 和 13.4 的 MySQL CDC v1 都是当前可提交 API；13.5 的 Oracle CDC 仍只表达后续语义方向。

### 13.1 Watermark bounded incremental

```json
{
  "runtime": {
    "boundary": "bounded"
  },
  "load": {
    "mode": "incremental",
    "change_detection": {
      "type": "watermark",
      "field": "updated_at",
      "tie_breaker": ["id"],
      "start": "committed",
      "end": "execution_upper_bound"
    }
  },
  "source": {
    "locator": "addp://engine/1/path/public/orders?type=table",
    "data_type": "table",
    "representation": "native"
  },
  "target": {
    "parent_locator": "addp://engine/2/path/public?type=schema",
    "name": "orders",
    "data_type": "table",
    "representation": "native",
    "policy": {
      "apply_mode": "upsert",
      "keys": ["id"]
    }
  },
  "transforms": []
}
```

### 13.2 用户 Kafka continuous source

```json
{
  "runtime": {
    "boundary": "continuous",
    "record_failure": {"mode": "block"}
  },
  "load": {
    "mode": "incremental",
    "change_detection": {
      "type": "kafka"
    }
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
    "parent_locator": "addp://engine/20/path/public?type=schema",
    "name": "orders",
    "data_type": "table",
    "representation": "native",
    "policy": {
      "apply_mode": "upsert",
      "keys": ["id"]
    }
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

### 13.3 PostgreSQL CDC v1（工作包 3A-3D 已完成第一版）

以下 PostgreSQL CDC 公开任务配置示例使用 PostgreSQL 目标；真实任务也可选择声明完整原子应用能力的 MySQL 目标，且不暴露 Infra Kafka topic：

```json
{
  "runtime": {
    "boundary": "continuous",
    "record_failure": {"mode": "block"}
  },
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
    "policy": {
      "apply_mode": "upsert_delete",
      "keys": ["id"]
    }
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

Debezium connector 名称、slot、publication、内部 topic、consumer group 和 connector runtime 参数属于 Transfer/infra 执行配置，不进入用户任务的通用 source endpoint。第一版要求单表、稳定主键和不存在的新目标表；`op=r|c|u|d` 分别归一化为 snapshot/upsert/delete，目标固定为 `upsert_delete`。完整约束已升级到 `docs/spec/addp任务体系规范.md`。

生命周期固定为：pause 只停止目标应用，connector 继续捕获并把 backlog 写入 Infra Kafka；stop 删除 ADDP-owned connector/slot/publication/topic 并进入不可恢复终态。暂停的常态风险是 Kafka retention 和磁盘，connector/Kafka 故障导致 slot 不推进时才会额外造成源库 WAL 堆积。Stop 必须由服务端校验 `confirmed=true` 和与任务名称完全一致的 `confirmation_text`，Console 同时显示高危二次确认并要求输入任务名称。

### 13.4 MySQL CDC v1（工作包 3E 已完成）

MySQL CDC 沿用 PostgreSQL CDC 的公开任务形态、Infra Kafka、ChangeEvent、目标 monotonic apply、execution/state 与 lifecycle 契约，source locator 指向 MySQL table，捕获实现使用 Debezium MySQL Connector。v1 固定 MySQL 8.0 单表、稳定非空主键、initial snapshot、ROW/FULL binlog、默认严格 schema drift 阻塞和 PostgreSQL/MySQL 新目标表；公开 API、`continuous.database_cdc` capability、人工 additive migration 与 Console 共用唯一数据库 CDC 路线，不增加 provider 字段或 MySQL 专用 endpoint。

### 13.5 Oracle CDC（延期）

若未来恢复 Oracle CDC，公开任务形态仍应沿用相同 Infra Kafka/ChangeEvent/目标应用契约，source locator 指向 Oracle 表，捕获实现使用 Debezium Oracle Connector + LogMiner。恢复实施前必须重新冻结 Oracle 版本、RAC、CDB/PDB、LOB、权限和许可矩阵，不能基于当前 MySQL 路线推定兼容。

2026-07-24 调研确认 Oracle 12c 不存在 XE/Free 版本；仍支持 Oracle 12c 的 ArcGIS Enterprise 版本要求 Oracle SE2/EE，而当前固定的 Debezium 3.6 官方测试矩阵不包含 Oracle 12c。ArcGIS Enterprise Geodatabase 默认 `ST_GEOMETRY` 或可选 `SDO_GEOMETRY` 也落在 Debezium Oracle 当前不支持的用户定义/Oracle supplied spatial type 范围，传统版本化编辑还会写入 A/D delta tables，不能把普通表 redo CDC 等同于 ArcGIS 逻辑要素 CDC。因此 Oracle 及 ArcGIS SDE 同步不进入当前 3F 实施；未来若恢复，必须先分别冻结普通 Oracle 表 CDC 与 ArcGIS 逻辑变化源的产品边界，不能增加版本兼容分支或伪装成同一路径。

## 十四、推荐实施顺序

### 阶段 0：文档和现有能力纠偏

1. 将本文讨论结论确认后升级到相关规范。
2. 移除当前 TaskProvider 对未实现 `stream` / `micro-batch` 的错误能力声明。
3. 统一当前配置文档与 planner 对 `mode=batch` 是否必填的规则。
4. 删除或真正实现伪取消路径；在具备真实中断前保持 `supports_cancel=false`。
5. 把手动任务启动改为数据库原子 claim。

### 阶段 1：Watermark bounded incremental

1. 定义增量状态表和 position schema。
2. 实现复合 watermark、冻结上界和稳定排序。
3. 实现 target upsert 能力声明和目标键校验。
4. 实现 CAS/fencing 状态提交。
5. 支持 resume；replay 后置或只提供最小受控接口。
6. 增加源库时间回拨、相同 timestamp 并发、watermark 未更新、只读副本延迟和目标重复应用等破坏性测试。

### 阶段 2A：Continuous/Kafka 契约冻结（已完成）

1. 术语表、任务体系、catalog/path、Engine Provider 和 capability 规范已升级。
2. 已确认业务 Kafka 与 Infra Kafka 的 owner/身份边界。
3. 已确认 ChangeStreamReaderProvider 返回原始 ChangeRecord；ChangeEvent 和 ChangeApplyWriter 归 Transfer runtime。
4. 已确认 `transfer.sync_states` 的 per-partition `kafka_offset/v1.next_offset` 与独立 `transfer.runtime_leases`。
5. 已确认第一版只支持 keyed JSON record -> PostgreSQL/MySQL partitioned monotonic upsert，不支持普通 append；目标表写入与业务库 apply ledger 必须同事务提交。

### 阶段 2B：Kafka continuous runtime 实现（已完成第一版）

1. 增加用户可注册 Kafka Engine、`service -> topic` catalog 和 ChangeStreamReaderProvider。
2. 实现专用 continuous runtime supervisor、runtime lease 和容量限制。
3. 实现 keyed JSON record -> ChangeEvent(operation=upsert) -> PostgreSQL/MySQL `PartitionedTableChangeApplyProvider`，并原子提交业务表与目标 apply ledger。
4. 实现 partition state、lag、heartbeat、真实暂停和停止。
5. 已覆盖目标提交后 Infra position 可重复应用、rebalance、worker lease/fencing 失效、pause/resume/stop、目标锁取消、retention 已越过 committed position 和严格 schema drift；lag 与 retention horizon 提前告警由工作包 2D 完成。
6. 当时将 Debezium envelope 与 upsert/delete 后置到工作包 3D，业务 Kafka DLQ/bounded replay 后置到 4A/4B；现均已完成。Schema Registry、Avro 和 Protobuf 继续后置。

### 阶段 2C：Continuous 产品化与运行保障（已完成第一版）

1. Console Wizard 已按目标 Provider capability 开放业务 Kafka topic -> PostgreSQL/MySQL，以及 PostgreSQL/MySQL 单表 CDC -> PostgreSQL/MySQL 新目标表两类路线，不提供未实现路线。
2. 任务列表和详情支持 continuous start/pause/resume/stop，execution 详情展示 owner、heartbeat、lease、fencing token、partition committed `next_offset`、最近提交时间和 checkpoint age。
3. resume 前校验 committed position 是否仍在 Kafka earliest/latest 范围，retention 已清除时明确失败，不静默重置。
4. PostgreSQL 目标锁等待响应 context 取消，并验证业务写入与 apply ledger 同时回滚。
5. 未知字段、缺失必填字段和类型不兼容保持严格阻塞；lag/latest offset 与 retention horizon 主动告警由工作包 2D 完成。

### 阶段 2D：Continuous 保留窗口观测与告警（已完成第一版）

1. Kafka Provider 返回 topic 每分区 earliest/latest provider position；Transfer 不使用 consumer fetch position 或 consumer group offset 代替已提交位置。
2. Transfer continuous worker 按固定间隔采样，计算每分区 lag records、recovery headroom records、source rate 和 retention horizon seconds，并将样本写入统一 execution metadata。
3. 冷启动、无 committed position 或零写入速率时 retention horizon 为 unknown，不伪造为健康时间。默认 degraded/critical 阈值分别为 6h/1h，只由 Transfer runtime 配置。
4. Transfer execution 详情和 Monitor execution 详情展示总体 health、分区 earliest/latest/committed/lag/headroom/rate/horizon 与采样时间；Monitor 不直连 Kafka 或 Transfer 私表。
5. 用 Redpanda 验证 latest 增长、retention earliest 推进、degraded/critical 判定与 metadata 的 fencing 保护。
6. pause/stop 改变 desired state 后立即禁止新的 progress/diagnostics 写入；与暂停竞态的 fencing 错误必须将 execution 收敛为 cancelled，不得误记为 failed。

### 工作包 3A：PostgreSQL CDC 契约冻结（已完成）

1. 已冻结 PostgreSQL/MySQL 单表、稳定主键、Debezium `initial` snapshot 和 PostgreSQL/MySQL 新目标表的唯一第一版范围。
2. 已冻结 schemaless JSON Debezium envelope：`r -> snapshot/upsert`、`c|u -> upsert`、`d -> delete`，tombstone/truncate/message 事件严格拒绝。
3. 已冻结 Kafka Connect capture position 与 Transfer committed position 的双位点 owner 边界。
4. 已冻结 `upsert_delete`、目标 ledger 原子应用、默认 strict schema drift、resume-only 和不提供 replay/DLQ/自动 DDL；后续工作包 4G 只增加专用人工 additive migration，不改变任务配置策略。
5. 已冻结 pause 保持 connector 捕获、stop 不可逆清理 capture resource、重新同步必须新建任务和新目标表；Stop API 与 Console 都必须显式高危确认。

### 工作包 3B：Infra Kafka 与 Kafka Connect 部署（已完成）

1. 已在 ADDP infra 中固定唯一 Redpanda v24.3.18 路线和 Debezium Connect 3.6.0.Final distributed 进程角色。
2. 已实现固定端口、健康检查、Connect compact internal topics、CDC topic namespace 和 `admin/connect/transfer` SASL principal/ACL；生产 3 broker/2 Connect 拓扑仍由部署规范约束，不在单机开发 Compose 内伪装验证。
3. 已实现 retention/capacity 配置、Kafka 磁盘水位、connector/task 状态和本地业务 PostgreSQL slot/WAL lag 运维检查；cleanup owner 已进入正式规范。
4. 已为业务 PostgreSQL 开发容器启用 logical replication、slot WAL 上限和 replication HBA，不修改 Infra PostgreSQL。真实 Debezium smoke 已验证 `initial snapshot -> r,c,u,d -> Infra Kafka`，并确认 connector/slot/publication/topic/table 清理无残留。

### 工作包 3B-RP：Redpanda 适配认证与正式晋级（已完成）

1. Kafka API 数据面、Kafka Connect 3.6.0.Final、Transfer runtime、任务/API/数据库契约保持唯一；认证通过后 Redpanda 直接替换旧 broker 路径，不保留运行时选择。
2. 部署级 SASL mechanism 必须贯通 Kafka init、Connect、capture admin、continuous reader 和 DLQ writer，不得由任务配置或发行版判断派生。
3. 按 12.6 的 RP-01 至 RP-12 执行真实认证并保存可复现结果；任一项未通过时结论保持 `not-certified`。
4. 认证完成前不修改正式支持范围；认证和晋级决策完成后，先修订正式技术栈与配置规范，再替换默认部署。
5. 2026-07-25 已通过 RP-01 至 RP-12：Redpanda v24.3.18、Debezium Connect 3.6.0.Final、SCRAM-SHA-256/ACL、topic/group/admin API、PostgreSQL/MySQL CDC、DLQ、真实 time/bytes retention、Stop cleanup、broker/Connect 重启恢复和资源快照均通过。可重复入口为 `bash scripts/test/certify-infra-kafka.sh`。

### 工作包 3B-RP-HA：Redpanda 生产拓扑认证（已完成）

1. HA override 只把同一正式集群扩展到 3 broker、3 replicas、`acks=all`、确认 quorum=2 和 2 个同组 Connect worker；Transfer 仍只解析 bootstrap servers 和 Kafka API 连接属性，不解析发行版或 quorum 实现。
2. SASL_SSL 证书、broker 节点名、Connect worker 名和故障注入只属于认证部署，不进入应用配置模型以外的新概念。
3. 按 12.7 的 RP-HA-01 至 RP-HA-08 执行；单机 RP-01 至 RP-12 结果不能替代 HA 证据。
4. 认证结论与单机矩阵共同构成正式晋级门禁；发行版和拓扑仍不得进入公开 capability。
5. 2026-07-25 已完成 Redpanda v24.3.18 + Debezium Connect 3.6.0.Final 生产拓扑认证；完整入口为 `bash scripts/test/certify-infra-kafka-ha.sh`，结束后自动清理认证资源、删除临时私钥并恢复默认单机 Redpanda profile。

### 工作包 3C：Capture control plane（已完成第一版）

1. 已增加 Transfer capture supervisor 和 Kafka Connect REST client，支持创建/更新、状态、pause/resume 与删除；产品 pause 路径明确不暂停 connector，只持续监控捕获健康。
2. 已增加 `transfer.capture_resources` generation/resource 事实、稳定内部命名、单分区 topic 显式创建、start/resume 复用、不可逆 stop 和统一幂等 cleanup。
3. Stop API 已增加服务端不可逆确认；Console 已增加 CDC danger 输入任务名确认和 pause 的 Kafka retention/WAL 风险提示。
4. 3C 完成时公开 task 创建仍拒绝 CDC；该临时关闭规则已由 3D 数据面闭环删除。

### 工作包 3D：PostgreSQL CDC 数据面（已完成第一版）

1. 已实现 Debezium 3.6 schemaless JSON adapter、snapshot/upsert/delete ChangeEvent 和严格 envelope/source/schema 校验；Decimal 固定 string、时间固定 Connect 毫秒编码，并在 capture 前校验 PostgreSQL CDC v1 类型矩阵。后续增强已加入 PostGIS geometry：generation 冻结 OGC type/SRID/dimension，Debezium `{wkb,srid}` 严格解码为 EWKB，目标按同一空间事实建表且不做重投影。
2. 已扩展 PostgreSQL `PartitionedTableChangeApplyProvider`，把 upsert/delete 与目标 ledger 原子提交。
3. 已复用 continuous worker、`kafka_offset/v1`、CAS/fencing、retention 位置保护和 capture generation，不建立第二套 CDC consumer。
4. 已完成 PostgreSQL -> PostgreSQL initial snapshot、update、delete、worker 崩溃换 owner 恢复、目标 ledger/Infra state 对齐和 stop cleanup 端到端测试。

### 工作包 4A：业务 Kafka DLQ 与 bounded replay 契约冻结（已完成）

1. DLQ v1 只处理业务 Kafka record 的 JSON 解码、字段校验和类型转换错误；source/poll、目标数据库、fencing、retention、Infra 故障以及 PostgreSQL CDC schema/protocol 漂移继续阻塞。
2. continuous task 使用显式 `runtime.record_failure.mode=block|dead_letter`；实现落地后新配置必须显式提交，不依赖省略字段猜默认值。
3. dead-letter identity 固定由 task/apply identity + source identity + partition + offset 推导。先把原始 key/value/headers、错误和 execution 审计写入 Transfer-owned Infra Kafka DLQ，再幂等落 `transfer.dead_letters` 控制索引，随后由目标 Provider 以 `operation=skip` 只推进目标 ledger，最后 CAS 推进 Infra 主 position；任一步失败都不得越过源记录。
4. replay v1 与 DLQ 解耦：它从原业务 Kafka 的显式 offset 范围读取，创建独立 bounded execution，并强制写入不存在的新 PostgreSQL 隔离目标。DLQ 不是完整 replay source，原目标不允许被历史事件回写。
5. replay 不修改任务定义、`desired_state`、主 runtime lease、`transfer.sync_states` 或主 apply ledger，不提供覆盖主水位、编辑原始 payload、同目标回放或 PostgreSQL CDC replay。

### 工作包 4B：业务 Kafka DLQ 与 bounded replay 实现（已完成）

1. 已扩展 `PartitionedTableChangeApplyProvider` 的 `skip` operation 和 capability validator；PostgreSQL 已在目标事务内实现 ledger-only skip，并通过真实数据库集成测试验证不修改业务行。
2. Infra Kafka DLQ topic/ACL、`transfer.dead_letters` 控制索引、确定性 UUID v5 identity、`transfer.dead_letter/v1` 无损 envelope 与 payload -> index 幂等写入组件已完成。
3. 已完成业务 Kafka 确定性 record error 分类，并在同一 continuous 主循环接通 payload -> index -> target skip -> source CAS；DLQ、目标 apply 或 CAS 任一步失败均停止后续步骤，真实 Kafka/Infra PostgreSQL/业务 PostgreSQL E2E 已验证坏记录被审计跳过、有效记录写入且两侧 position 对齐。公开任务 API 现接受显式 `block|dead_letter`，Console 默认仍显式发送 `block`。
4. bounded replay runner 核心已实现：只接受业务 Kafka `record/json + block` plan、显式 per-partition 半开 ranges 和 execution-scoped apply identity；先校验完整 retention，再校验隔离目标不存在，最后才允许 prepare/write。runner 的依赖中不存在 `SyncStateRepository` 或 runtime lease，不能读写主 committed position。Kafka offset 空洞通过“partition 已分配且固定历史上界内 poll 为空”判定已追到冻结 end，不把该 fetch 进度提交为 consumer group 或主状态。
5. 已增加唯一 owner API `POST /task-definitions/:id/replay`：请求严格只接受 ranges 与新目标 `parent_locator + name`；请求时冻结 retention 快照并拒绝原目标/已有目标，execution config 保存 owner task 快照、ranges、target 和独立 apply identity，bounded worker 通过 execution marker 进入唯一 replay 数据面。
6. replay 请求创建和 worker 执行都不 claim 或更新 owner task，不创建/修改 `transfer.sync_states`；PostgreSQL prepare 使用 `RequireTargetAbsent` 和非 `IF NOT EXISTS` DDL 防止并发竞态。单元测试已覆盖非法/重复 range、配置覆盖拒绝、主任务并行与状态隔离；真实 Kafka/PostgreSQL E2E 已验证 retention 快照、`[start,end)`、新隔离表、独立 ledger 和原目标不变。公开 `dead_letter`、replay API 与 capability 已同时开放。

### 工作包 4C：DLQ 只读管理与 Console 操作闭环（已完成第一版）

1. owner task 下已增加唯一只读 API：`GET /task-definitions/:id/dead-letters` 与 `GET /task-definitions/:id/dead-letters/:identity`。查询同时受认证租户和 task owner 约束，不提供跨 task 管理入口或全局 identity 直查；Transfer capability 同步声明路由、过滤字段和 `exposes_payload=false`。
2. 列表采用 `{data,total,page,page_size,total_pages}`，固定支持 `page`、`page_size` 以及 `source_partition`、`error_category`、`error_code`、`payload_available` 精确过滤，按 `last_observed_at DESC, identity DESC` 稳定排序。
3. 第一阶段只公开安全控制索引，不公开 Infra Kafka payload reference，也不读取原始 key/value/headers。详情用于展示完整 safe error、execution 与观测审计事实；DLQ 仍不是 replay source，不能从某一 DLQ 行直接生成 replay 请求。
4. Console 只对当前业务 Kafka `runtime.record_failure.mode=dead_letter` task 展示 DLQ 卡片、过滤、分页和详情；payload unavailable 明确展示为不可用，不显示“可回放”等误导状态。
5. bounded replay Console 表单只对业务 Kafka `record/json + block` owner task 开放，要求用户显式输入 per-partition `[start_offset,end_offset)` 和新的 PostgreSQL `parent_locator + name`。表单调用既有唯一 replay API，不允许从 DLQ payload 或 DLQ identity 自动补齐 source range。
6. 本工作包未增加 DLQ 删除、编辑、原始 payload 查看、跨任务聚合、自动重放、原目标覆盖或 payload retention 探测请求链路。后端单元测试覆盖 tenant/task scope、过滤、稳定排序和敏感字段不泄露；Transfer 全量 Go 测试/vet、前端测试/build、Swagger 25 路由覆盖均通过。payload availability 的周期性收敛属于后续独立治理工作，不与只读管理 API 建立第二条 Kafka consumer 路线。

### 工作包 4D：DLQ payload availability 生命周期治理（已完成第一版）

1. 已在 Transfer continuous worker 内增加唯一低频 reconciler，按 identity 游标分批扫描 `payload_available=true` 控制索引。它不进入 HTTP 请求、不加入 consumer group、不提交 Kafka offset，也不修改 owner task、execution、sync state 或目标 ledger。
2. 每批按控制索引快照中的 Infra Kafka topic/partition/offset 精确核验：record 必须仍位于当前 retention 边界内，exact offset 必须存在，record key 必须等于 dead-letter identity，schema header 必须为 `transfer.dead_letter/v1`。代码不解码、不记录或复制原始 payload value。
3. topic/partition 已删除、offset 低于 earliest、offset 不小于 latest，或 compaction 后 fetch 已跨过 exact offset 时确认 unavailable；认证、网络、broker、fetch timeout 等运行故障保持原状态并等待下一轮，不能伪造 payload 丢失。
4. `false` 更新使用 identity + 当前 payload reference + `payload_available=true` CAS。若 continuous runtime 已重复观测同一 source record并写入新 payload offset，旧 probe 结果必须更新 0 行，不能覆盖新的 available 状态。availability 更新不得改写首次/最近观测时间或错误审计事实。
5. reconciler interval、batch size、单批 timeout 和 fetch bytes 属于 Transfer 部署策略，不进入 task JSON。多 continuous-worker 实例允许重复核验；CAS 和只从 true 向 false 的相同引用更新保证结果幂等，不为此新增第二套 leader/lease 事实。
6. 单元与 race 测试已覆盖 exact record、compacted hole、identity/schema 不匹配、游标轮转、partial probe error 和 stale reference CAS；真实 Infra Kafka 已验证 payload 存在后为 available、删除 topic 后收敛 unavailable，真实 PostgreSQL 已验证旧 reference 更新 0 行、当前 reference 更新成功且不改写 `last_observed_at`。Transfer 全量 Go 测试/vet、前端测试/build 与 Swagger 25 路由覆盖均通过。

### 工作包 4E：DLQ task-owned cleanup 生命周期（已完成第一版）

1. 用户直接删除 task 与 System physical cleanup 已复用同一个 task-owned resource cleanup；TaskService 和 cleanup executor 不再各自维护外部资源删除逻辑。System logical cleanup、普通 pause 和 stop 继续保留 DLQ 审计资源。
2. 物理清理顺序固定为：PostgreSQL CDC capture cleanup（如适用）→ 业务 Kafka 确定性 DLQ topic 幂等删除 → 最终 Infra PostgreSQL 事务。最终事务统一删除 task-private state 并 soft/unscoped delete task definition；任一步失败均停止后续步骤。
3. DLQ topic 只能由 Infra Kafka admin principal 删除。`UnknownTopicOrPartition` 视为幂等成功；权限、网络、broker 或超时错误必须阻止索引与任务删除。topic 已删除而数据库删除失败时，后续重试从 unknown topic 继续，不恢复 payload 或建立补偿 topic。
4. 当前业务 Kafka continuous task 物理删除时都尝试删除 `__addp_dlq.<tenant>.<task>`，不根据当前 `record_failure.mode` 猜测历史；当前配置已改变但仍有 DLQ 控制索引的 task 也走同一路径。只有既非业务 Kafka且无 DLQ 索引的 bounded/CDC task 才跳过 Kafka cleanup。
5. cleanup 不删除目标业务数据、目标 apply ledger 或统一 execution 历史；公开 Delete API 路由保持不变，不新增旁路 cleanup API。运行中删除返回 409，外部 cleanup 失败返回不泄露内部细节的双语 503。
6. 单元与 race 测试已覆盖 logical 保留、topic-before-index 顺序、Kafka 失败阻断后续删除、当前配置变化但保留 DLQ 索引、直接 Delete soft-delete 与 System physical unscoped delete复用；真实 Kafka 已验证 admin topic 删除及重复删除幂等，真实 PostgreSQL 已验证 tenant/task scoped 索引删除。Transfer 全量 Go 测试/vet、前端测试/build、Swagger 25 路由覆盖和差异检查均通过。

### 工作包 4F：task-private runtime/control state 生命周期（已完成第一版）

1. `transfer.sync_states`、`transfer.runtime_leases`、`transfer.capture_resources` 与 `transfer.dead_letters` 都是 task definition 存续期间的私有当前事实，不承担长期审计。task 删除后由 `common.task_executions`、System audit 和 cleanup execution/result 保留历史，因此这些私有表不得残留孤儿行。
2. continuous task 物理清理前必须复用唯一 stop 路径：设置 `desired_state=stopped`、取消 pending execution、等待 active lease owner 释放或 lease 到期。等待超时返回失败，不删除 capture/DLQ、私有状态或 task definition；lease 已过期但仍为 pending/running 的 execution 在最终事务中以 `stop_reason=cleanup` 收敛为 cancelled 并保留。
3. bounded task 没有真实 cancel 能力；直接 Delete 与 System physical cleanup 都拒绝删除 `status=running` 的 bounded task，不能用状态更新伪造 worker 已停止。
4. 外部 capture/DLQ cleanup 成功后，在单个锁 task 行的 Infra PostgreSQL 事务中删除 tenant/task scoped dead letters、sync states、runtime leases、capture resources，并在同一事务 soft/unscoped delete task definition。事务再次验证 desired/status 与有效 lease，避免 stop-check 后并发 Start 重新创建运行事实。
5. 直接 Delete 在该事务中 soft-delete task definition，System physical cleanup 在同一事务 unscoped delete。两者继续保留统一 execution、目标业务数据、目标 apply ledger 和 replay 隔离结果。
6. `TRANSFER_CONTINUOUS_RUNTIME_STOP_TIMEOUT|POLL_INTERVAL` 是所有 continuous stop/cleanup 的唯一部署策略；不保留 capture-scoped 旧环境变量或 fallback。
7. 已移除 task definition 与 DLQ 索引的独立删除入口；单元测试覆盖 soft/physical delete、四类私有状态、execution 终态化和 runtime guard 整体回滚，真实 PostgreSQL 验证同事务删除及 active lease 回滚。Transfer 全量 Go 测试/vet、关键包 race、前端测试/build、Swagger 25 路由覆盖和差异检查均通过。

### 工作包 4G：人工确认 additive schema migration（已完成）

1. 保持 Debezium schemaless JSON 与默认严格阻塞，不增加自动 DDL 或 task-level schema evolution 开关。只有当前阻塞消息实际出现的新增 nullable 非 geometry 字段可以进入人工 additive request；missing、incompatible、主键、非 nullable、geometry 和协议变化仍不可恢复。
2. `transfer.schema_change_requests` 保存 task/generation/execution、Kafka partition/offset、schema diff、from/to mapping revision、审批字段和 applied 审计；同一 generation 同时只允许一个 pending request，physical cleanup 随 capture generation 级联删除。
3. 唯一审批 API 为 `POST /task-definitions/:id/schema-change/approve`。请求必须逐字段提交 source/target/`target_type`/`nullable=true` 并完整覆盖 pending unexpected fields；普通 UpdateTask 继续拒绝修改已创建 generation 的 CDC config。
4. 服务端重新读取源表，验证既有 mapping/主键未变化、审批字段仍存在且 nullable、类型受 provider v1 支持；目标 PostgreSQL/MySQL 复用 `PartitionedTableChangeApplyProvider.Prepare...` 的既有安全 schema evolution，只新增 nullable 列，不建立 Transfer 私有 DDL。
5. 外部目标 DDL 与 Infra PostgreSQL 通过 request/task 行锁和幂等 retry 收敛：目标列已存在时必须完全兼容；Infra 事务提交时追加 mapping、递增 generation `schema_revision`、标记 request applied，并把 task 置为 paused。审批不隐式恢复，用户通过既有 Resume 从原 committed offset 创建新 execution。
6. DDL 成功后触发目标表 Meta deep scan；扫描失败不回滚 schema revision，但必须记录为可观测结果。触发使用 request 持久化 `pending -> running(token, lease) -> success|failed` claim；并发调用只能有一个 claimant，过期 `running` 只由相同重复审批 POST 接管，GET 保持只读，迟到 claimant 不能覆盖新结果。真实 Meta 失败不由重复审批自动循环重试，用户使用 Meta 既有手动扫描能力；claim TTL 只属于部署配置。真实 PostgreSQL/MySQL CDC E2E 必须验证 blocked offset 未推进、审批后目标加列、Resume 重放当前消息、后续数据继续同步，以及不安全变化继续被拒绝。
7. 已完成：新增 `GET /task-definitions/:id/schema-change` 和唯一审批 API，Console 任务详情提供逐字段确认；repository/service/API/source inspection、cleanup 和前端行为测试已覆盖。真实 PostgreSQL 与 MySQL CDC E2E 均验证 pending request、blocked offset 不推进、审批、重复审批幂等、目标加列、任务进入 paused、Resume 重放阻塞消息、后续同步和 Stop cleanup；PostgreSQL E2E 进一步注入“目标 DDL 已提交但 Infra task update 失败”，验证 request/revision/config 整体回滚后两个并发相同审批收敛为同一 request、单次 revision 和单份 mapping。Meta scan claim 已通过 repository 测试覆盖过期接管与旧 token fencing，并通过真实 PostgreSQL E2E 验证并发审批只触发一次 Meta 调用、重复审批 POST 接管过期 claim；MySQL E2E 验证真实 Meta 失败后重复审批不自动重试。Swagger 27 个公开路由覆盖一致。

### 工作包 4H：Schema change 公共观测与告警闭环（已完成）

1. Transfer 将 schema change 的跨模块事实固定投影到触发阻塞的 `common.task_executions.metadata.continuous.schema_change`，不把 private request 表暴露给 Monitor。投影包含 request、generation/revision、源 position、diff、`status=pending|applied|stopped` 和 Meta scan 提交状态，不包含 claim token 或内部错误文本。
2. pending request 创建、additive 审批和不可逆 Stop 分别在同一 Infra PostgreSQL 事务内写入 `pending`、`applied`、`stopped`；Meta scan claim/completion 也与 private request 原子更新。Stop 与 schema-blocked worker Finish 通过已持久化 `desired_state=stopped` fencing，覆盖任意提交顺序；lease 释放或过期后，Stop 在 capture cleanup 前统一取消遗留 active execution、关闭 pending 投影并把 task 收敛到 idle。若 worker 在 request commit 后、Finish 前失联，lease expiry reconciler 将原 execution/task 收敛为 schema blocked，禁止创建或领取 recovery execution。任何投影失败都会回滚对应 private 状态变化，不能形成双轨事实。
3. Monitor continuous evaluator 保留最新 active execution 的 runtime/retention/checkpoint 信号，另外只从终态 execution 的 `schema_change.status=pending` 派生 `schema_change_blocked` 严重告警。审批或 Stop 使信号消失后，经统一 incident reconciler 自动恢复并生成既有 lifecycle event/outbox；普通 continuous failed execution 仍不进入通用失败规则。
4. Meta scan `failed` 当前只表示自动提交扫描失败，Transfer 任务详情继续提供可观察结果。手工 Meta 扫描尚无关联完成回执，因此不创建无法可靠恢复的 Monitor incident，也不以 Resume、新 execution 或时间流逝猜测恢复。
5. Transfer repository 测试覆盖 Meta claim/result 投影和 Stop 收敛；Monitor SQLite/PostgreSQL 测试覆盖终态 JSON/JSONB 查询、incident 打开与恢复；共享前端测试覆盖 pending/applied/stopped 信号归一化。PostgreSQL/MySQL 真实 CDC E2E 验证原阻塞 execution 的 pending/applied 与 Meta attempt/execution UUID 投影，且 Stop 后 connector/topic/group/ACL/capture/request/execution 测试数据均清理。

### 工作包 3E：MySQL CDC（已完成）

1. 3E-A 已完成：capture generation 主事实已改为 engine-neutral，PostgreSQL/MySQL provider-specific source resource 子事实与 generation 同事务创建并外键级联清理；generic model 中的 slot/publication 已删除，不保留双轨读取。新安装基线与 017 clean-break 迁移一致，真实 PostgreSQL 已验证旧数据迁入、旧列删除、唯一索引、单一外键、级联清理和 PostgreSQL capture control E2E。
2. 3E-B：数据库 CDC 任务 JSON 使用唯一 `DatabaseCDCTaskSpec`，只表达 continuous + incremental CDC、单表、initial snapshot、完整 field mapping 和 PostgreSQL/MySQL `upsert_delete` 目标等通用语义；source provider 不写入任务 JSON，只能由 source locator 对应的 System Engine 解析结果决定。不得并行保留 PostgreSQL/MySQL 两个同形 parser，也不得在未解析 Engine 时用配置形态推断 provider。创建、更新和启动必须在进入 capture 前完成真实 provider 校验。
3. 3E-B 的 MySQL 8.0 v1 只支持有稳定主键的单表和无歧义类型集合：有符号 `TINYINT/SMALLINT/MEDIUMINT/INT/BIGINT`、`CHAR/VARCHAR/TEXT`、`DECIMAL/NUMERIC`、`FLOAT/DOUBLE`、`DATE/TIME/DATETIME/TIMESTAMP`（最高毫秒精度）、`JSON`、`BINARY/VARBINARY/BLOB`。拒绝所有 unsigned 整数、`TINYINT(1)`/`BOOL` 布尔歧义、`BIT`、`ENUM/SET`、`YEAR`、空间类型、超过毫秒的时间精度以及 zero date 默认值；字段 mapping 必须覆盖源表完整 schema，源主键必须按顺序一一映射到目标 keys。
4. MySQL capture 前置校验固定要求 MySQL 8.0、`log_bin=ON`、`binlog_format=ROW`、`binlog_row_image=FULL`、非零 server id，并验证 connector 凭据可读取 binlog 状态。GTID 可为 `ON|OFF`，仅作为诊断事实，不进入 Transfer committed position；Kafka partition offset 仍是目标应用唯一主顺序。
5. Debezium MySQL connector 固定 `initial` snapshot、schemaless JSON、decimal string、Connect 毫秒时间和 binary base64。除单分区数据 topic 外，每个 MySQL capture generation 还拥有独立 schema-history topic；Debezium 3.6 history record 使用空 key，不能依赖 compact topic 提供正确的历史保留语义，因此该 topic 固定为单分区 `cleanup.policy=delete + retention.ms=-1`，并由 capture cleanup 显式删除。其名称、ownership 和 connector server id 属于 MySQL provider 子事实，必须与 connector/data topic 一起创建、授权和清理，不能依赖 Kafka 自动建 topic 或留下未登记资源。
6. MySQL envelope 使用独立严格 source schema adapter，不能复用 PostgreSQL exact source schema；两者只复用 outer envelope、key/row 映射和统一 `ChangeEvent`。MySQL `file/pos/gtid/row/server_id` 只用于协议校验和诊断，不替代 Kafka offset。tombstone、truncate、来源身份不匹配、未知 source/envelope 字段、字段/类型变化都阻塞且不得推进 offset。
7. 3E-B 已完成：MySQL planner、源字段/主键/binlog 前置校验、Debezium connector config、严格 envelope adapter 已接入既有 PostgreSQL monotonic target apply；内部继续使用唯一 capture supervisor 和 continuous worker 主路径。单元测试覆盖 provider 解析、类型矩阵、connector/schema-history 资源、snapshot/c/u/d、空 BLOB、source/envelope/schema drift 和 runner committed progress；真实 MySQL 8.0.46 + Debezium 3.6.0.Final + Infra Kafka 已验证 preflight、connector RUNNING、initial snapshot 与 c/u/d 的实际 schemaless envelope。该验证不替代 3E-C 的完整恢复/生命周期 E2E。
8. 3E-C 环境契约：Business MySQL 使用独立 `MYSQL_CDC_USER`/`MYSQL_CDC_PASSWORD`，启动后由 root 幂等创建或更新 connector 用户，并把权限收敛为 `SELECT`、`RELOAD`、`SHOW DATABASES`、`REPLICATION SLAVE`、`REPLICATION CLIENT`、`LOCK TABLES`。初始化必须在每次 `business/scripts/start.sh -mysql` 等待 ready 后执行，不能只依赖首次建卷的 `/docker-entrypoint-initdb.d`；Business MySQL 必须显式启用非零 server id、binlog、ROW format 和 FULL row image。CDC E2E 使用独立 schema/table，不复用包含 MySQL v1 非支持类型的业务样例表。
9. 3E-C 真实全生命周期 gate：公共 API 创建 PostgreSQL/MySQL CDC 后，必须在真实 PostgreSQL/MySQL source、Debezium 3.6、Infra Kafka 和 PostgreSQL/MySQL target 矩阵上覆盖 initial snapshot、insert/update/delete、pause/resume backlog、worker crash 后 lease/fencing recovery、retention 越界拒绝、strict schema drift 阻塞，以及 stop 后 provider 专属 connector/topic/group/ACL/slot/publication/schema-history 资源的幂等清理；目标 apply ledger 必须始终与 Transfer committed offset 单调一致，MySQL 不伪造 slot/publication 资源。retention segment 推进若无法在共享开发 Kafka 中稳定制造，必须用隔离 topic 的确定性真实 Kafka integration 补证，不能降低为只验证错误文本。
10. 3E-C 产品开放 gate：上述 E2E 通过后，删除公开 API 的临时 MySQL 拒绝分支；唯一 continuous capability 增加结构化 `database_cdc`，声明 `sources=[postgresql,mysql]`、`targets=[postgresql,mysql]`、`bootstrap=[initial_snapshot]`、`apply_mode=upsert_delete`。Console 将 PostgreSQL-specific CDC helper clean break 为唯一 database CDC helper，按 source provider 使用各自可证明的类型矩阵，继续复用同一 Wizard、详情和生命周期操作，不增加 MySQL 专用 endpoint、页面或任务 JSON 字段。
11. 3E-C 已完成：Business 初始化连续执行两次后账号权限和 MySQL binlog 前置条件保持一致；真实 PostgreSQL/MySQL source -> Debezium 3.6.0.Final -> Infra Kafka -> PostgreSQL/MySQL target 矩阵 E2E 通过公共 API 覆盖 snapshot、insert/update/delete、worker lease/fencing recovery、pause/resume backlog、schema drift 不推进位置、additive target DDL/Meta scan/resume、`DeleteRecords` 推进 earliest 后的 retention 拒绝，以及 provider 专属 capture 资源 cleanup。两类目标 ledger 均与 Transfer committed offset 对齐；API gate 已删除，capability 与 Console 已按第 10 项开放。

### 工作包 3F：Oracle CDC（延期，不进入当前实施序列）

接入 Oracle LogMiner，集中验证 Oracle 权限、日志、snapshot、LOB、DDL、长事务和版本兼容矩阵；Oracle 不与 MySQL 工作包并行开放。

### 阶段 4：按真实需求评估 Flink

只有前述单一路线已经稳定，并出现当前 continuous runtime 无法满足的有状态计算需求时，才评估 Flink runtime。引入时也应消费同一任务语义、ChangeEvent 和 execution/state 契约，不能建立第二套 Transfer 产品模型。

## 十五、当前明确不采用的路线

1. 不恢复 `task_type=import|export|transfer`。
2. 不把 `realtime` 加入统一 execution `trigger_type`。
3. 不把 Kafka change stream 伪装成当前 table batch reader。
4. 不把 continuous consumer 直接塞进一次性 Asynq task 无限运行。
5. 不由 ADDP 自己解析 Oracle redo log。
6. 不把 watermark 轮询称为完整 CDC。
7. 不依赖 Kafka auto commit 作为 Transfer 增量状态事实源。
8. 不在第一版以普通 append 消费 Kafka record；缺少稳定 key 的 topic 不进入 continuous v1。
9. 不宣称任意外部目标的通用 exactly-once。
10. 不让用户从资源树选择或访问 Infra Kafka topic。
11. 不让同一 Kafka 配置同时承担 infra 和业务 Engine 身份。
12. 不在当前阶段整体推翻 Go bounded batch runtime。
13. 不并行保留旧配置与新语义字段；正式实施时 clean break。
14. 不为每个 continuous task 启动独立操作系统进程，也不把 continuous worker 设计成不可扩展的全局单例。
15. 不通过静默忽略未知字段处理 schema 变化。
16. 不由 ADDP 自动暂停用户上游业务写入。

## 十六、设计确认状态与后续问题

16.1 至 16.5 已随阶段 0/1 升级为正式规范并实现；16.6 至 16.9 已由工作包 2A 确认并实现 keyed JSON -> PostgreSQL continuous v1；16.10 至 16.12 和 16.14 已由工作包 3A 确认并升级到正式规范。Infra Kafka/Kafka Connect 的代码部署属于工作包 3B；MySQL CDC 已完成 3E 全生命周期 E2E 与产品入口开放，Oracle 已延期，Flink 只在出现明确有状态计算或规模证据后评估。

### 16.1 任务配置采用嵌套结构还是扁平结构（已确认）

分析：执行边界、装载方式、变化识别和目标策略是正交维度。全部扁平化会快速产生 `mode`、`incremental_type`、`watermark_field`、`cdc_*` 等互相依赖字段，也容易把 task config 与统一 execution 对象混淆。

建议：采用有限嵌套，但把 `execution.boundary` 改为更明确的 `runtime.boundary`：

```text
runtime.boundary
load.mode
load.change_detection
source
transforms
target.policy.apply_mode
```

嵌套只用于稳定概念边界，不建立多层抽象。该结构已进入 Transfer 配置正式规范并同步本文示例；旧 `mode=batch` 不兼容。

### 16.2 `write_mode` 是否改为 `apply_mode`（已确认）

分析：`overwrite/append` 主要描述写入方式，而 `upsert/upsert_delete` 描述变化如何应用到目标。继续扩展 `write_mode` 会让“删除事件”“冲突键”“幂等”语义变得含混。

结论：已 clean break 为 `target.policy.apply_mode=replace|append|upsert|upsert_delete`，删除 `write_mode`，不保留字段别名。目标引擎通过强类型 Provider 和 capability 声明 prepare、upsert、delete、事务/批次提交和幂等能力；Transfer 负责组合校验和执行策略。

### 16.3 增量状态表和 position JSON（已确认）

分析：watermark、Kafka offset、Oracle SCN 等位置结构不同，但都需要按 task/source/partition 唯一、CAS 更新和审计。runtime lease 与业务 committed position 生命周期不同，不宜放在同一 JSON 中。

建议：至少分为两个 Transfer 私有事实：

- `transfer.sync_states`：保存 committed position、position type/version、state version 和最近提交 execution。
- `transfer.runtime_leases`：保存 owner instance、lease、heartbeat 和 fencing token。

`position` 使用带 `type`、`version` 的 JSONB，由对应 source provider 解释；公共列只保存跨 provider 必需的身份、版本和审计字段。阶段 1 已实现 watermark position v1；工作包 2B 已实现 per-partition `kafka_offset/v1.next_offset`，业务 Kafka continuous 与 PostgreSQL CDC 复用该位置契约。后续新增 position type 时不得修改已有 type 的字段含义。

### 16.4 Watermark 第一版源与一致上界（已确认）

分析：不同数据库的事务快照、时间类型和复合游标查询能力不同。如果第一版同时覆盖过多数据库，会把增量语义验证与方言适配混在一起。

结论：第一版只支持 PostgreSQL native table，使用复合游标和数据库一致性读获得本次上界，并已通过真实并发写入集成测试验证相同 watermark 值不会漏读；验证稳定后再接 MySQL。只读副本未进入第一版，后续开放前必须明确最大复制延迟和 lookback 策略。

### 16.5 Replay 是否进入第一版（已确认）

分析：replay 会引入历史范围选择、目标重复应用、与主任务并发、审计和资源限流，显著扩大第一版状态机。

结论：阶段 1 只支持 resume。工作包 4B 已在第二阶段实现业务 Kafka bounded replay；它永不推进主 committed position，也不提供覆盖主水位开关。

### 16.6 Kafka catalog 与 ResourceLocator（已确认）

分析：用户选择的是 topic，而 partition 是 Kafka 的执行分片和观测事实。把 partition 暴露成资源树可选叶子，会使任务绑定固定分区，破坏 Kafka 扩分区和 consumer group 分配语义。

结论：catalog 使用 `service(cluster) -> topic`，`topic` 是可选择 leaf；partition 数量、leader、副本和当前状态作为 topic facts/diagnostics，不作为资源树子节点和用户 locator。ResourceLocator 使用 `type=topic`。该结论已进入术语表、Catalog/路径、Engine Provider 和 capability 正式规范。

### 16.7 普通 Kafka record 的 schema/key 与 Schema Registry（已确认）

分析：普通 Kafka 消息的 key、value 编码和业务 schema 没有统一保证；一开始同时支持 JSON、Avro、Protobuf 和多种 Registry 会稀释 continuous runtime 主线验证。

结论：第一版只支持 JSON object value，并要求从 value 的显式非空字段提取稳定 key；目标必须使用 PostgreSQL 或 MySQL 原子单调 apply Provider。普通 append 无法满足 at-least-once 下的目标幂等要求，因此后置。Kafka 原生 record key 第一版只保留为诊断事实；Debezium JSON envelope、Schema Registry、Avro 和 Protobuf 后置。配置从一开始区分 `encoding` 与 `envelope`。

### 16.8 Continuous timeout、失联和自动恢复（已确认）

分析：continuous execution 正常情况下不会因运行时间过长而 timeout；需要判断的是 runtime heartbeat 丢失、source poll 卡死、checkpoint 长期不推进和目标持续失败。

结论：

- 不设置按总运行时长结束的 execution timeout。
- 使用连续多次 heartbeat 失败判定 lost，阈值由部署配置而不是任务随意设置。
- source poll、target apply 和 checkpoint 分别设置操作超时。
- 自动恢复使用指数退避、最大连续失败次数和 circuit breaker。
- 每次恢复创建新 execution，并从 committed position 开始。
- 可恢复失败立即创建唯一 pending recovery execution，使用持久化 `recovery_not_before` 控制领取；worker shutdown 不计失败，普通 execution failure 与 lease expiry 计入连续失败，schema drift、Pause、Stop 不自动恢复。
- 达到最大连续失败次数后 circuit 进入 `open`，冷却到期领取 pending execution 时进入 `half_open`；open 不复用任务 `blocked`，任务保持 `desired_state=running` 且无活 lease 时实际状态为 `idle`。
- 任一目标 position 成功提交或 session 达到稳定运行阈值后，连续失败计数重置。退避/circuit 参数只属于 Transfer runtime 部署配置，不进入任务 JSON。

具体秒数应通过阶段 2 压测确定，不在概念规范中硬编码。

### 16.9 Dead-letter 与 replay 第二阶段范围（已确认）

分析：把坏消息写入 DLQ 后推进源 offset，实质上是经过审计的数据跳过；若 DLQ 写入或回放语义不完整，容易从“不中断任务”退化为静默丢数。

结论：阶段 1 严格阻塞；工作包 4B 已完成第二阶段 v1 并公开。DLQ 只覆盖业务 Kafka 的记录级数据错误，使用确定性 identity 保存原 topic/partition/offset、原始 key/value/headers、错误和 task/execution 审计；只有 DLQ payload、控制索引和目标 `skip` ledger 都成功后才允许推进源 offset。数据库 CDC schema/protocol 漂移、source/poll、目标、fencing、retention 和 Infra 错误不得进入 DLQ。

DLQ 不保存伪造的“已回放”状态，也不作为完整 replay source。bounded replay 从原业务 Kafka 的显式 offset 范围读取，只写不存在的新 PostgreSQL 隔离目标，并以独立 execution 表达状态；同目标 replay、主水位覆盖、payload 编辑和 PostgreSQL CDC replay 均不进入 v1。

### 16.10 Infra Kafka 发行版、部署与容量（已确认）

分析：这是部署与运维选择，依赖预期吞吐、最长下游故障、数据保留、节点数量和生产环境。过早选择多种兼容发行版会形成测试矩阵。

结论：唯一参考实现固定为 Redpanda v24.3.18 + Debezium Connect 3.6.0.Final（内置 Kafka Connect 4.3.0）。开发环境使用 1 broker/1 Connect、单副本确认；生产参考使用 3 broker/至少 2 Connect、RF=3、`acks=all` 和确认 quorum=2。ADDP 以实际故障语义而不是 `min.insync.replicas` 等实现专有配置名定义耐久性；不保留 Apache Kafka broker profile 或运行时发行版选择。

- Connect shared internal topics 固定为 `__addp_connect_configs`、`__addp_connect_offsets`、`__addp_connect_status`，使用 compact 和开发/生产 `1/3` 复制因子。
- CDC topic 固定为单 partition `__addp_cdc.<tenant>.<task>.<generation>`，使用 delete cleanup、默认 7 天 retention；生产必须显式设置并校验 `retention.bytes`。
- principal 分离为 infra admin、`addp-connect`、`addp-transfer`，业务 Kafka principal 不得访问 infra namespace。
- Transfer capture supervisor 是 task-level connector/topic/slot/publication cleanup owner；Kafka Connect shared internal topics 和 broker 数据目录归 Infra 部署 owner。
- 容量基线至少为 `峰值编码字节/秒 * 恢复窗口秒数 * 副本因子 * 1.3`。time/bytes 任一先到都会缩短真实恢复窗口，backup 不替代 source WAL、connector offset 和 Kafka retention。
- 端口固定为 host Kafka `19092`、Connect REST `18083`；Connect REST 只供内部控制面使用，不经 Gateway 对外暴露。

### 16.11 Debezium connector 托管边界（已确认）

分析：将 Debezium 嵌入 Go continuous worker 会混合 JVM runtime、connector HA 和目标消费；让用户自己管理 connector 又会破坏 ADDP 任务的一致生命周期。

结论：采用独立 Kafka Connect distributed + Transfer capture supervisor 单一路线。capture supervisor 通过 Kafka Connect API 管理 connector 和状态映射；continuous worker 只消费 Infra Kafka。Kafka Connect 管 capture offset，Transfer 管 target committed offset，两者不能互相替代。connector、slot、publication、topic 和 group 均由服务端按 task/generation 生成，不进入公开任务配置。

### 16.12 数据库 CDC 第一版范围与生命周期（已确认）

分析：如果 stop 后删除 connector/slot 再直接 resume，会产生无法补齐的捕获空档；如果重新 snapshot 只做 upsert，又无法删除目标中已经不存在于源端的残留行。若为了 resume 让已 stop 的任务永久保持捕获，stop 就失去真实资源终止语义。

结论：第一版 source 支持 PostgreSQL 或 MySQL 8.0 有稳定主键的单表，使用 Debezium `initial` snapshot，经单 partition Infra Kafka data topic 写入不存在的 PostgreSQL 或 MySQL 新目标表。snapshot/c/u/d 归一化为 snapshot/upsert/delete，目标固定 `upsert_delete`，schema drift 默认严格阻塞；只有人工确认的 additive migration 可在原 generation 恢复。

- pause 只停止目标应用，connector 继续捕获并推进源日志位置；resume 在 capture healthy 且 Kafka committed position 未过期时无损恢复。
- 正常 pause 的主要资源代价是 Infra Kafka backlog、磁盘和 retention；connector/Kafka 故障时还必须分别观测 PostgreSQL WAL 或 MySQL binlog 保留风险。
- stop 是不可逆终态，清理 ADDP-owned connector、provider 专属资源、data/schema-history topic、group 和 ACL；已 stop 任务不得 start/resume，重新同步创建新任务和新目标表。
- Stop API 必须由服务端校验 `confirmed=true` 和与任务名称完全一致的 `confirmation_text`，Console 同时显示 danger 二次确认并要求输入任务名称；stop 保留目标业务数据、任务、execution、目标 ledger 和清理审计。
- 第一版只支持 resume，不支持 CDC replay、DLQ、truncate、自动 DDL、多表、无主键或 Oracle。

### 16.13 Oracle 第一版支持范围（延期）

分析：Oracle RAC、CDB/PDB、LOB、DDL 和长事务会显著增加验证矩阵。第一版应先证明通用 CDC 链路，而不是承诺所有 Oracle 部署形态。

建议：若未来恢复 Oracle CDC，第一版仍应限定为一个明确受支持版本和单实例部署、表级选择、insert/update/delete、稳定主键和基础标量类型；初始 snapshot + LogMiner continuous 必须闭环。RAC、复杂 LOB、自动 DDL 传播和无主键表不进入第一版。最终版本范围必须结合用户真实 Oracle 环境、许可和 Debezium 兼容矩阵确认，不能仅凭文档猜测。

### 16.14 Schema 变化与 Meta 协议（已确认）

分析：Schema change 决定目标是否能继续应用，Meta scan 只负责识别变化后的事实。二者不能互相替代，也不能通过忽略未知字段维持假成功。

结论：默认仍是严格 `fail`，不增加 `manual|additive` 任务配置开关。检测到 schema change 时先阻塞 partition、保存 schema diff，并以 `schema_change_blocked` 结束 execution；任务进入 `status=blocked`，当前消息和 committed offset 不推进。

唯一原 generation 恢复路线是人工确认 additive migration：只接受当前阻塞消息实际包含的新增 nullable 非 geometry 字段，服务端持久化 pending request，用户逐字段确认目标映射后，服务端重新验证源事实并复用目标 Provider 的安全 schema evolution。审批完成后 mapping revision 与 task config 在 Infra 事务内更新，任务进入 paused，用户再通过既有 Resume 从原 committed offset 恢复。

跨模块观测只使用触发阻塞的 `common.task_executions.metadata.continuous.schema_change` 投影。Transfer 在创建 private request 的同一事务内写入 `status=pending`、request/revision/source position/diff；审批时在同一事务内改为 `status=applied`，不可逆 Stop 时改为 `status=stopped`，并继续投影 Meta scan 的提交状态。Monitor 仅将 `pending` 解释为 `schema_change_blocked` 严重告警，`applied|stopped` 自动恢复，不读取 Transfer 私表。Meta scan `failed` 目前只表示提交扫描失败；由于手工 Meta 扫描没有回写 Transfer 的关联完成事实，不创建无法可靠恢复的 Monitor incident。

字段删除、类型或主键变化、非 nullable/geometry 新增、envelope/source 漂移继续不可恢复，只能 Stop 后新建任务和目标表。真正全自动 DDL 仍后置；若未来需要，必须先采用能把 schema version 与每条 Kafka record/offset 绑定的内部 envelope，不能通过运行时查询最新源 schema 猜测历史消息结构。

### 16.15 何时评估 Flink

分析：不能按“项目发展到某阶段”或“数据量看起来很大”引入 Flink，应由现有 runtime 的可测瓶颈或新计算语义触发。

建议：只有满足以下至少一个条件并有基准数据时才启动正式评估：

- 需要事件时间窗口、stream join、CEP 等当前 Transfer 明确不承担的有状态计算。
- 单 task 需要跨进程分布式 partition/state，现有 continuous worker 已经成为实测瓶颈。
- checkpoint state 规模和恢复时间超出现有 DB lease/position 模型的目标。
- 多级流式 DAG 需要统一状态快照和反压传播。

普通 Kafka 搬运、数据库 CDC、字段映射或单目标 upsert 不构成引入 Flink 的理由。即使引入，也必须复用相同 Task、ChangeEvent、state 和 execution 契约。

## 十七、与统一任务体系的边界

本文不改变以下正式规则：

- Transfer 对外只声明 `task_type=sync`。
- 任务定义归 `transfer.transfer_tasks`。
- 每次 bounded execution 或 continuous runtime session 都写入 `common.task_executions`。
- retry/restart 创建新的 execution，不复用已结束 execution。
- `trigger_type` 只允许 `manual` / `scheduled`。
- Orchestrator 通过 `provider=transfer + task_type=sync + task_id` 引用任务。
- continuous execution 后续如声明标准取消能力，必须真实中断 worker、释放资源并一致落库。
- TaskProvider capability 只能声明已经真实实现并验证的能力。

Transfer 内部 position、partition state、connector runtime 状态和 CDC 诊断信息属于 Transfer 私有执行状态；`common.task_executions` 保存跨模块可理解的状态、摘要和观测信息，不成为 Kafka/Oracle 私有状态数据库。
