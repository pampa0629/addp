# Transfer 任务语义、增量同步与持续运行时设计

更新时间：2026-07-13

状态：阶段 0/1 与工作包 2A-2D 已完成；工作包 3A 已冻结 PostgreSQL CDC v1 契约，3B 已完成 Infra Kafka/Kafka Connect，3C 已完成 capture control plane，3D 已完成 Debezium 数据面、目标原子 upsert/delete、公开任务创建、Console Wizard 和全链路验收。

本文整理 Transfer 全量、批增量、Kafka 流式源、数据库 CDC 和持续运行时的概念与推荐技术路线。bounded/watermark、业务 Kafka continuous 和 PostgreSQL CDC v1 已完成第一版；replay、DLQ、MySQL/Oracle CDC 和自动 DDL 仍未实现。

当前稳定实现仍以以下文档为准：

- `docs/spec/addp任务体系规范.md`
- `transfer/docs/transfer-基本概念及配置说明.md`
- `transfer/docs/design.md`

当前实现支持 bounded snapshot 的 `replace` / `append`、PostgreSQL bounded watermark incremental、业务 Kafka keyed JSON continuous upsert，以及 PostgreSQL 单表 CDC initial snapshot + upsert/delete。continuous/CDC 使用目标 monotonic apply、业务库 ledger、Infra state CAS 与 runtime fencing；replay 仍未实现。

## 一、本文要解决的问题

本文集中回答：

1. Transfer 的任务类型、执行边界、装载方式、触发方式和目标应用方式如何区分。
2. 全量、watermark 批增量和 CDC 持续同步分别是什么。
3. 增量状态与 execution checkpoint 有什么区别。
4. 当前 Go batch 主链路是否需要被 Flink 等流批一体框架替换。
5. 不使用 Flink 时能否支持 Oracle CDC。
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
11. Oracle CDC 推荐使用 Debezium Oracle Connector 捕获 redo log，经 Infra Kafka 交给 Transfer。
12. 用户 Kafka 数据源注册为 System Engine；Infra Kafka 不进入 System engines，也不出现在资源树。
13. Infra Kafka 与业务 Kafka 可以在小型部署中物理共用集群，但必须保持不同身份、凭据、ACL、topic namespace 和生命周期。
14. 当前 Asynq worker 继续作为 bounded worker；新增独立 continuous worker 进程角色，一个进程承载多个 runtime session，并通过 DB lease/fencing 支持多实例。
15. Debezium 运行在独立 Kafka Connect distributed 集群中，由 Transfer capture supervisor 通过受控 API 管理，不嵌入 Transfer Go 进程。
16. Schema 变化第一阶段默认严格阻塞，不静默忽略字段，也不默认自动执行目标 DDL。
17. 业务 Kafka 第一版只支持 keyed JSON record -> PostgreSQL monotonic upsert；业务目标库通过 partition apply ledger 原子推进 `next_offset`，普通 append 不进入第一版。
18. PostgreSQL CDC 第一版只支持有稳定主键的单表 initial snapshot -> snapshot/upsert/delete -> PostgreSQL 新目标表；pause 保持捕获，stop 是不可逆终态。

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

稳定字段已 clean break 为 `target.policy.apply_mode=replace|append|upsert|upsert_delete`；当前 PostgreSQL CDC v1 已开放 `upsert_delete`。旧 `write_mode` 已拒绝，不保留字段别名。

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

replay 默认必须满足以下条件之一：

- 写入隔离目标；或者
- 目标使用幂等 upsert/delete；或者
- 业务明确接受追加重复。

replay 不应提供“顺便覆盖主水位”的开关，否则会破坏单一状态事实源。

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

JSON 解码、ChangeEvent 归一化和 ChangeApplyWriter 属于 Transfer continuous runtime。第一版目标使用 PostgreSQL `PartitionedTableChangeApplyProvider`：Provider 不理解 ChangeEvent，只接收已映射表行、目标 keys、每条记录 position 和单 partition 批次边界，并把业务数据与目标 apply ledger 原子提交。

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
- 第一版每个 task 使用不可变 `apply_identity`，目标账本固定为业务目标 PostgreSQL 的 `addp_transfer.apply_positions`。
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

只有出现明确的事件时间窗口、流式 join、大规模状态计算、统一流式 SQL 或现有 runtime 无法承担的并行规模时，再把 Flink 作为可选 runtime 评估；Flink 不作为支持 Kafka 或 Oracle CDC 的前置条件。

## 十、不使用 Flink 的 Oracle CDC

### 10.1 推荐链路

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

### 10.2 Oracle 捕获方案判断

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

### 10.3 Snapshot 与 CDC 交接

首次接入不能简单执行“先全量，完成后再开始 CDC”，否则两者之间可能出现数据空洞。正确路线需要：

1. 建立或记录 CDC 日志起点。
2. 执行一致性 snapshot。
3. 捕获并缓冲 snapshot 期间产生的变化。
4. snapshot 应用完成后继续消费积压变化。
5. 进入稳定 continuous 消费。

这一协调优先使用 Debezium 已验证的 snapshot 语义，不由 Transfer 自行拼接 Oracle 查询和 redo 日志。

### 10.4 Debezium 托管模式

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

Transfer 不在 Go 进程内嵌入 Debezium Java runtime。Kafka Connect distributed mode提高捕获服务可用性，但不单独构成“不丢数据”保证；仍需同时满足 Kafka 内部 topic/业务 topic 复制、Oracle archive log 保留、connector offset 持久化和 Infra Kafka retention 要求。

### 10.5 捕获位点与消费位点

需要同时存在两类位点，但不能混为一个状态：

```text
Oracle
  -- Debezium capture position / SCN -->
Infra Kafka
  -- Transfer topic/partition/offset -->
target
```

- Kafka Connect offset 回答“Oracle 日志已经捕获到哪里”，由 Kafka Connect 管理。
- Transfer committed position 回答“目标已经可靠应用到哪个 Kafka offset”，由 Transfer 管理。
- ChangeEvent 可以携带 Oracle SCN 供诊断、审计和目标防乱序使用。
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

ADDP 不自动暂停用户 Oracle 或其他上游业务写入。平台可以暂停新 replay、限制非关键任务、对目标应用背压并发出告警；是否干预上游业务由用户决定。自动暂停 Debezium connector 也必须谨慎，因为暂停期间数据库归档日志仍可能超出保留窗口。

## 十三、建议配置形态

13.1 的 PostgreSQL watermark JSON 已是当前可提交 API；13.2 是工作包 2A 已冻结、待 2B 实现后才能提交的正式目标配置；13.3 仍只表达 CDC 后续语义方向。

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
    "boundary": "continuous"
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

### 13.3 PostgreSQL CDC v1（工作包 3A 已冻结）

PostgreSQL CDC 公开任务配置只引用源 PostgreSQL 表和目标 PostgreSQL Engine，不暴露 Infra Kafka topic：

```json
{
  "runtime": {
    "boundary": "continuous"
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

### 13.4 Oracle CDC（后续）

Oracle CDC 沿用相同公开任务形态和 Infra Kafka/ChangeEvent/目标应用契约，但 source locator 指向 Oracle 表，捕获实现使用 Debezium Oracle Connector + LogMiner。Oracle 版本、RAC、CDB/PDB、LOB 和权限矩阵在 PostgreSQL CDC 链路稳定后单独冻结，不与 PostgreSQL 第一版并行实现。

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
5. 已确认第一版只支持 keyed JSON record -> PostgreSQL partitioned monotonic upsert，不支持普通 append；目标表写入与业务库 apply ledger 必须同事务提交。

### 阶段 2B：Kafka continuous runtime 实现（已完成第一版）

1. 增加用户可注册 Kafka Engine、`service -> topic` catalog 和 ChangeStreamReaderProvider。
2. 实现专用 continuous runtime supervisor、runtime lease 和容量限制。
3. 实现 keyed JSON record -> ChangeEvent(operation=upsert) -> PostgreSQL `PartitionedTableChangeApplyProvider`，并原子提交业务表与目标 apply ledger。
4. 实现 partition state、lag、heartbeat、真实暂停和停止。
5. 已覆盖目标提交后 Infra position 可重复应用、rebalance、worker lease/fencing 失效、pause/resume/stop、目标锁取消、retention 已越过 committed position 和严格 schema drift；lag 与 retention horizon 提前告警由工作包 2D 完成。
6. 当时将 Debezium envelope 与 upsert/delete 后置到工作包 3D；现已完成。DLQ、Schema Registry、Avro 和 Protobuf 继续后置。

### 阶段 2C：Continuous 产品化与运行保障（已完成第一版）

1. Console Wizard 已开放业务 Kafka topic -> PostgreSQL 和 PostgreSQL 单表 CDC -> PostgreSQL 新目标表两条已实现路线，不提供未实现路线。
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

1. 已冻结 PostgreSQL 单表、稳定主键、Debezium `initial` snapshot 和 PostgreSQL 新目标表的唯一第一版范围。
2. 已冻结 schemaless JSON Debezium envelope：`r -> snapshot/upsert`、`c|u -> upsert`、`d -> delete`，tombstone/truncate/message 事件严格拒绝。
3. 已冻结 Kafka Connect capture position 与 Transfer committed position 的双位点 owner 边界。
4. 已冻结 `upsert_delete`、目标 ledger 原子应用、strict schema drift、resume-only 和不提供 replay/DLQ/自动 DDL。
5. 已冻结 pause 保持 connector 捕获、stop 不可逆清理 capture resource、重新同步必须新建任务和新目标表；Stop API 与 Console 都必须显式高危确认。

### 工作包 3B：Infra Kafka 与 Kafka Connect 部署（已完成第一版）

1. 已在 ADDP infra 中增加唯一 Apache Kafka 4.3.0 KRaft 路线和 Debezium Connect 3.6.0.Final distributed 进程角色。
2. 已实现固定端口、健康检查、Connect compact internal topics、CDC topic namespace 和 `admin/connect/transfer` SASL principal/ACL；生产 3 broker/2 Connect 拓扑仍由部署规范约束，不在单机开发 Compose 内伪装验证。
3. 已实现 retention/capacity 配置、Kafka 磁盘水位、connector/task 状态和本地业务 PostgreSQL slot/WAL lag 运维检查；cleanup owner 已进入正式规范。
4. 已为业务 PostgreSQL 开发容器启用 logical replication、slot WAL 上限和 replication HBA，不修改 Infra PostgreSQL。真实 Debezium smoke 已验证 `initial snapshot -> r,c,u,d -> Infra Kafka`，并确认 connector/slot/publication/topic/table 清理无残留。

### 工作包 3C：Capture control plane（已完成第一版）

1. 已增加 Transfer capture supervisor 和 Kafka Connect REST client，支持创建/更新、状态、pause/resume 与删除；产品 pause 路径明确不暂停 connector，只持续监控捕获健康。
2. 已增加 `transfer.capture_resources` generation/resource 事实、稳定内部命名、单分区 topic 显式创建、start/resume 复用、不可逆 stop 和统一幂等 cleanup。
3. Stop API 已增加服务端不可逆确认；Console 已增加 CDC danger 输入任务名确认和 pause 的 Kafka retention/WAL 风险提示。
4. 3C 完成时公开 task 创建仍拒绝 CDC；该临时关闭规则已由 3D 数据面闭环删除。

### 工作包 3D：PostgreSQL CDC 数据面（已完成第一版）

1. 已实现 Debezium 3.6 schemaless JSON adapter、snapshot/upsert/delete ChangeEvent 和严格 envelope/source/schema 校验；Decimal 固定 string、时间固定 Connect 毫秒编码，并在 capture 前校验 PostgreSQL CDC v1 类型矩阵。
2. 已扩展 PostgreSQL `PartitionedTableChangeApplyProvider`，把 upsert/delete 与目标 ledger 原子提交。
3. 已复用 continuous worker、`kafka_offset/v1`、CAS/fencing、retention 位置保护和 capture generation，不建立第二套 CDC consumer。
4. 已完成 PostgreSQL -> PostgreSQL initial snapshot、update、delete、worker 崩溃换 owner 恢复、目标 ledger/Infra state 对齐和 stop cleanup 端到端测试。

### 工作包 3E：Oracle CDC（PostgreSQL 稳定后）

接入 Oracle LogMiner，集中验证 Oracle 权限、日志、snapshot、LOB、DDL、长事务和版本兼容矩阵；MySQL CDC 与 Oracle 一样后置，不与 PostgreSQL v1 并行开放。

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

16.1 至 16.5 已随阶段 0/1 升级为正式规范并实现；16.6 至 16.9 已由工作包 2A 确认并实现 keyed JSON -> PostgreSQL continuous v1；16.10 至 16.12 和 16.14 已由工作包 3A 确认并升级到正式规范。Infra Kafka/Kafka Connect 的代码部署属于工作包 3B，Oracle 和 Flink 仍后置。

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

`position` 使用带 `type`、`version` 的 JSONB，由对应 source provider 解释；公共列只保存跨 provider 必需的身份、版本和审计字段。阶段 1 已实现 watermark position v1；工作包 2A 已冻结 Kafka position v1，待 2B 实现。CDC 后续增加新 position type，不修改已有 type 的字段含义。

### 16.4 Watermark 第一版源与一致上界（已确认）

分析：不同数据库的事务快照、时间类型和复合游标查询能力不同。如果第一版同时覆盖过多数据库，会把增量语义验证与方言适配混在一起。

结论：第一版只支持 PostgreSQL native table，使用复合游标和数据库一致性读获得本次上界，并已通过真实并发写入集成测试验证相同 watermark 值不会漏读；验证稳定后再接 MySQL。只读副本未进入第一版，后续开放前必须明确最大复制延迟和 lookback 策略。

### 16.5 Replay 是否进入第一版（已确认）

分析：replay 会引入历史范围选择、目标重复应用、与主任务并发、审计和资源限流，显著扩大第一版状态机。

结论：阶段 1 只支持 resume。replay 未进入当前实现，后续引入时永不推进主 committed position，也不提供覆盖主水位开关。

### 16.6 Kafka catalog 与 ResourceLocator（已确认）

分析：用户选择的是 topic，而 partition 是 Kafka 的执行分片和观测事实。把 partition 暴露成资源树可选叶子，会使任务绑定固定分区，破坏 Kafka 扩分区和 consumer group 分配语义。

结论：catalog 使用 `service(cluster) -> topic`，`topic` 是可选择 leaf；partition 数量、leader、副本和当前状态作为 topic facts/diagnostics，不作为资源树子节点和用户 locator。ResourceLocator 使用 `type=topic`。该结论已进入术语表、Catalog/路径、Engine Provider 和 capability 正式规范。

### 16.7 普通 Kafka record 的 schema/key 与 Schema Registry（已确认）

分析：普通 Kafka 消息的 key、value 编码和业务 schema 没有统一保证；一开始同时支持 JSON、Avro、Protobuf 和多种 Registry 会稀释 continuous runtime 主线验证。

结论：第一版只支持 JSON object value，并要求从 value 的显式非空字段提取稳定 key；目标固定为 PostgreSQL upsert。普通 append 无法满足 at-least-once 下的目标幂等要求，因此后置。Kafka 原生 record key 第一版只保留为诊断事实；Debezium JSON envelope、Schema Registry、Avro 和 Protobuf 后置。配置从一开始区分 `encoding` 与 `envelope`。

### 16.8 Continuous timeout、失联和自动恢复（已确认）

分析：continuous execution 正常情况下不会因运行时间过长而 timeout；需要判断的是 runtime heartbeat 丢失、source poll 卡死、checkpoint 长期不推进和目标持续失败。

结论：

- 不设置按总运行时长结束的 execution timeout。
- 使用连续多次 heartbeat 失败判定 lost，阈值由部署配置而不是任务随意设置。
- source poll、target apply 和 checkpoint 分别设置操作超时。
- 自动恢复使用指数退避、最大连续失败次数和 circuit breaker。
- 每次恢复创建新 execution，并从 committed position 开始。

具体秒数应通过阶段 2 压测确定，不在概念规范中硬编码。

### 16.9 Dead-letter 是否进入第一版（已确认）

分析：把坏消息写入 DLQ 后推进源 offset，实质上是经过审计的数据跳过；若 DLQ 写入或回放语义不完整，容易从“不中断任务”退化为静默丢数。

结论：第一版不提供 DLQ，遇到不可解析或不可应用事件时严格阻塞对应 partition。第二阶段再设计 Transfer-owned Infra Kafka DLQ，必须保存原 topic/partition/offset、原始载荷引用、错误、task/execution 和回放状态；只有 DLQ 写入成功后才允许推进源 offset。

### 16.10 Infra Kafka 发行版、部署与容量（已确认）

分析：这是部署与运维选择，依赖预期吞吐、最长下游故障、数据保留、节点数量和生产环境。过早选择多种兼容发行版会形成测试矩阵。

结论：唯一参考实现固定为 Apache Kafka 4.3.0 KRaft + Debezium Connect 3.6.0.Final（内置 Kafka Connect 4.3.0）。开发环境使用 1 broker/1 Connect、RF/ISR=`1/1`；生产参考使用 3 broker/至少 2 Connect、RF/ISR=`3/2`。

- Connect shared internal topics 固定为 `__addp_connect_configs`、`__addp_connect_offsets`、`__addp_connect_status`，使用 compact 和开发/生产 `1/3` 复制因子。
- CDC topic 固定为单 partition `__addp_cdc.<tenant>.<task>.<generation>`，使用 delete cleanup、默认 7 天 retention；生产必须显式设置并校验 `retention.bytes`。
- principal 分离为 infra admin、`addp-connect`、`addp-transfer`，业务 Kafka principal 不得访问 infra namespace。
- Transfer capture supervisor 是 task-level connector/topic/slot/publication cleanup owner；Kafka Connect shared internal topics 和 broker 数据目录归 Infra 部署 owner。
- 容量基线至少为 `峰值编码字节/秒 * 恢复窗口秒数 * 副本因子 * 1.3`。time/bytes 任一先到都会缩短真实恢复窗口，backup 不替代 source WAL、connector offset 和 Kafka retention。
- 端口固定为 host Kafka `19092`、Connect REST `18083`；Connect REST 只供内部控制面使用，不经 Gateway 对外暴露。

### 16.11 Debezium connector 托管边界（已确认）

分析：将 Debezium 嵌入 Go continuous worker 会混合 JVM runtime、connector HA 和目标消费；让用户自己管理 connector 又会破坏 ADDP 任务的一致生命周期。

结论：采用独立 Kafka Connect distributed + Transfer capture supervisor 单一路线。capture supervisor 通过 Kafka Connect API 管理 connector 和状态映射；continuous worker 只消费 Infra Kafka。Kafka Connect 管 capture offset，Transfer 管 target committed offset，两者不能互相替代。connector、slot、publication、topic 和 group 均由服务端按 task/generation 生成，不进入公开任务配置。

### 16.12 PostgreSQL CDC 第一版范围与生命周期（已确认）

分析：如果 stop 后删除 connector/slot 再直接 resume，会产生无法补齐的捕获空档；如果重新 snapshot 只做 upsert，又无法删除目标中已经不存在于源端的残留行。若为了 resume 让已 stop 的任务永久保持捕获，stop 就失去真实资源终止语义。

结论：第一版只支持 PostgreSQL 有稳定主键的单表，使用 Debezium `initial` snapshot，经单 partition Infra Kafka topic 写入不存在的 PostgreSQL 新目标表。snapshot/c/u/d 归一化为 snapshot/upsert/delete，目标固定 `upsert_delete`，schema drift 固定 fail。

- pause 只停止目标应用，connector 继续捕获并推进 slot；resume 在 capture healthy 且 Kafka committed position 未过期时无损恢复。
- 正常 pause 的主要资源代价是 Infra Kafka backlog、磁盘和 retention；connector/Kafka 故障导致 slot 停滞时，源库 WAL 才会额外持续增长。
- stop 是不可逆终态，清理 ADDP-owned connector/slot/publication/topic；已 stop 任务不得 start/resume，重新同步创建新任务和新目标表。
- Stop API 必须由服务端校验 `confirmed=true` 和与任务名称完全一致的 `confirmation_text`，Console 同时显示 danger 二次确认并要求输入任务名称；stop 保留目标业务数据、任务、execution、目标 ledger 和清理审计。
- 第一版只支持 resume，不支持 replay、DLQ、truncate、自动 DDL、多表、无主键、MySQL 或 Oracle。

### 16.13 Oracle 第一版支持范围

分析：Oracle RAC、CDB/PDB、LOB、DDL 和长事务会显著增加验证矩阵。第一版应先证明通用 CDC 链路，而不是承诺所有 Oracle 部署形态。

建议：在 PostgreSQL CDC 链路稳定后，Oracle 第一版优先限定为一个明确受支持版本和单实例部署、表级选择、insert/update/delete、稳定主键和基础标量类型；初始 snapshot + LogMiner continuous 必须闭环。RAC、复杂 LOB、自动 DDL 传播和无主键表后置。最终版本范围必须结合用户真实 Oracle 环境与 Debezium 兼容矩阵确认，不能仅凭文档猜测。

### 16.14 Schema 变化与 Meta 协议（已确认）

分析：Schema change 决定目标是否能继续应用，Meta scan 只负责识别变化后的事实。二者不能互相替代，也不能通过忽略未知字段维持假成功。

结论：第一版只使用固定 `fail` 路线，不增加 `manual|additive` 任务配置开关。检测到字段增删、主键变化、类型不兼容或 envelope/source 结构变化时阻塞 partition、保存 schema diff，并以 `schema_change_blocked` 结束 execution；任务进入 `status=blocked`，禁止 start/resume/retry。由于 capture generation 创建后配置不可修改且第一版不能跳过当前消息，唯一处理方式是 Stop 旧任务并创建新任务、新目标表重新 initial snapshot。自动 DDL、安全 additive evolution、schema change request 和可恢复 generation migration 后置。

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
