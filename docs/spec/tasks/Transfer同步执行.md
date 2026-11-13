# ADDP Transfer同步执行规范

[返回任务体系规范](../addp任务体系规范.md)

定义 Transfer sync 的正交配置维度、bounded / continuous 边界、状态推进与观测要求。数据库捕获见[CDC](Transfer数据库CDC.md)，业务 Kafka 的拒绝记录与重放见[Kafka 与 DLQ](Transfer-Kafka与DLQ.md)。

阅读导航：

| 阅读目的 | 正文入口 |
| --- | --- |
| 配置维度与通用同步约束 | [Transfer 同步执行契约](#transfer-同步执行契约) |
| continuous 配置、目标提交、位点与观测规则 | [continuous 主契约](#transfer-continuous-sync-v1-契约) |
| 业务 Kafka 来源、显式映射、目标账本、CAS 与位置恢复 | [来源配置与目标提交](#continuous-来源配置与目标提交) |
| 分区位置、窗口估算、retention／checkpoint 与安全样本 | [采样与健康诊断](#continuous-采样与健康诊断) |
| 通用规则目标、失败判定、模式切换与统一评估 | [通用告警规则](#通用执行告警规则与统一评估) |
| 租户告警通知、Webhook／邮件投递与送达验收 | [通知与渠道投递](#租户告警通知与渠道投递) |
| continuous Owner 信号、告警恢复与过程事件分工 | [观测信号、告警与过程事件](#continuous-观测信号告警与过程事件分工) |
| 进度展示、Monitor 读取与 Orchestrator 编排限制 | [进度观测与编排边界](#进度观测与编排边界) |
| 用户期望、实际状态、execution/session 与租约 | [任务状态、执行身份与运行租约](#任务状态执行身份与运行租约) |
| 任务级 Pause/Stop、标准取消与停止验收 | [停止与标准取消](#任务级-pausestop-与标准取消) |
| 自动恢复触发、唯一领取、退避、circuit 与审计 | [自动恢复契约](#continuous-自动恢复退避与-circuit) |
| 首次扫描触发、精确目标、claim 与结果核对 | [首次扫描契约](#continuous-首次扫描与-claim) |
| 标准入口、分版本证据、失败与修复、清理和未覆盖项 | [验证与历史证据](#验证与历史证据入口) |

## Transfer 同步执行契约

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
3. watermark 增量必须使用稳定单字段或复合游标：`tie_breaker=[]` 的单字段游标只保证 insert，非空 tie breaker 的复合游标 `(watermark_field, tie_breaker...)` 保证 insert/update；读取区间统一为 `(committed_position, execution_upper_bound]`，两种模式都不支持物理删除，不得称为完整 CDC。
4. 增量 committed position 归 Transfer 私有同步状态，不能写入任务定义或用 execution checkpoint 代替。只有目标批次成功提交后才能通过 CAS/fencing 推进同步状态。
5. 同一增量任务默认只允许一个 active execution 推进主状态；任务 claim 与 pending execution 创建必须在同一数据库事务中完成。
6. resume 创建新 execution 并从 committed position 继续；replay 是独立执行参数且永不推进主状态。PostgreSQL/MySQL/OceanBase/TiDB/openGauss watermark 只支持 resume，不支持 replay，OceanBase/TiDB/openGauss 限非空间表。
7. TaskProvider capability 只能声明已真实实现并验证的边界；不具备真实 worker 中断、资源释放和一致落库能力时必须 `supports_cancel=false`，也不得保留只改数据库状态的伪取消入口。

## Transfer continuous sync v1 契约

continuous execution 表示一次长期运行的 runtime session，不是把 bounded executor 放进无限循环。正式实现必须使用 Transfer 独立 continuous worker/supervisor；`transfer-bounded-worker` 只通过 PostgreSQL claim 承担 bounded execution。

### continuous 来源配置与目标提交

本节的业务 Kafka 来源、JSON 消息与 upsert 目标范围不替代[数据库 CDC 契约](Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)。共享原子提交与 Infra CAS 规则不扩展已声明的目标支持矩阵。

- 输入与配置：[业务 Kafka 样例](#业务-kafka-配置样例)、[来源与协议](#业务-kafka-来源与消息协议)、[稳定键与分区](#稳定键与分区顺序)、[目标与映射](#业务-kafka-目标能力与显式映射)。
- 执行与恢复：[起点与窗口](#显式起点与恢复窗口)、[应用身份与分区账本](#目标应用身份与分区账本)、[共享提交与 CAS](#共享目标提交与-infra-cas)。
- 使用范围：[DLQ/replay 与 CDC](#dlq-replay-与-cdc-能力边界)、[配置与提交验收](#来源配置与目标提交验收边界)。

#### 业务 Kafka 配置样例

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

#### 业务 Kafka 来源与消息协议

- source 只支持用户在 System 注册的业务 Kafka Engine，locator 必须定位 `type=topic`；Infra Kafka 不进入公开任务配置。
- `envelope` 只允许 `record`，`encoding` 只允许 `json`，value 必须是 JSON object；tombstone、数组、标量、Avro、Protobuf、Schema Registry 和 Debezium envelope 均不支持。

#### 稳定键与分区顺序

- 稳定 key 只允许从 JSON value 的显式非空字段提取；`key.source` 固定为 `value`，`key.fields` 非空。
- Kafka 原生 record key 第一版只保留为诊断事实，不参与目标身份推导。
- 同一业务 key 的事件必须由生产者稳定写入同一 partition。
- Transfer 保证 partition 内顺序，不承诺跨 partition 的同 key 全局顺序。

#### 业务 Kafka 目标能力与显式映射

- 目标支持声明完整原子应用能力的 PostgreSQL/MySQL native table，并且必须使用 `apply_mode=upsert`、显式目标 keys 和 `PartitionedTableChangeApplyProvider`；普通 `TableUpsertProvider` 不能单独满足 continuous fencing。
- keyless append 和可能产生重复的普通 append 不进入第一版。
- Kafka JSON 没有可依赖的表 schema，任务必须通过显式 `field_mapping` 固定目标字段和类型；source key fields 映射后的目标字段必须与 `target.policy.keys` 一一对应且非空。
- 未知字段、缺失必填字段或不兼容 schema 变化必须阻塞 partition 并使当前 execution 失败，不得静默丢弃。

#### 显式起点与恢复窗口

- `start.mode` 固定为 `committed`。
- 没有 committed state 的 partition 必须由任务显式选择 `initial=earliest|latest`；不得由 consumer 默认值或 auto commit 猜测。
- consumer auto commit 禁用。
- Kafka consumer group 只用于分区分配，不是 Transfer committed position 的事实源。
- resume 前必须验证 committed `next_offset` 仍位于 Kafka partition 的 `[earliest_offset, latest_offset]` 范围；低于 earliest 表示 retention 已破坏连续恢复条件，execution 必须明确失败，不得静默重置到 earliest 或 latest。
- 高于 latest 视为状态损坏，同样失败。

#### 目标应用身份与分区账本

- 每个 continuous task 必须有服务端生成且不可修改的 `apply_identity` UUID，不写入 config。
- PostgreSQL Provider 在业务目标库维护 `addp_transfer.apply_positions`，MySQL Provider 在目标业务数据库维护 `_addp_transfer_apply_positions` InnoDB 私有表；两者都以 `apply_identity + source_identity + target_identity + partition` 校验应用主线，并把业务行 upsert 与目标 `next_offset` 在同一事务提交。
- Transfer 必须把 poll 结果按 partition 拆成顺序批次，每条已映射记录携带消费后 `next_offset`。
- Provider 锁定 ledger 行，拒绝位置缺口，跳过不大于已应用位置的重复记录；同一批剩余记录出现相同目标 key 时保留最高 `next_offset` 的最后状态，再执行一次原子 upsert。

#### 共享目标提交与 Infra CAS

- Transfer 为每个 partition 在 `transfer.sync_states` 保存 `position.type=kafka_offset`、`position.version=v1` 和 `next_offset`。
- 目标批次成功提交后才允许用 runtime fencing token 和 state version 做 CAS 推进；worker 恢复时必须 seek 到 `next_offset`。
- 投递保证固定为 `at-least-once delivery + target monotonic apply`。
- 目标提交与 Infra position CAS 之间崩溃会重放，但目标 ledger 必须吸收重复批次，并阻止租约过期 worker 在新 worker 之后写回旧状态；不得宣称跨 Kafka、业务目标数据库和 Infra PostgreSQL 的分布式 exactly-once。

#### DLQ replay 与 CDC 能力边界

- 业务 Kafka 已开放 `dead_letter` 和写入新隔离目标的 bounded replay，唯一契约见[Kafka 与 DLQ](Transfer-Kafka与DLQ.md#transfer-业务-kafka-dlq-与-bounded-replay-v1-契约)。
- 数据库 CDC、Kafka target、物理删除和 continuous append 仍不得借用业务 Kafka 的 DLQ/replay 路线绕过各自边界。

#### 来源配置与目标提交验收边界

Topic 选择、尾部有界样本与用户确认映射的操作规则见[配置专题](../../../transfer/docs/transfer-基本概念及配置说明.md#业务-kafka-来源与映射)；样本建议不形成新的 Topic schema，也不扩大源读或目标写授权。各目标 ledger、CDC 原子应用与 Oracle 目标独有约束仍沿[CDC 数据面契约](Transfer数据库CDC.md#数据面与捕获约束)和[Oracle 目标契约](Transfer数据库CDC.md#oracle-目标应用约束)。

配置保存、读取样本或创建 pending execution 不证明目标提交、Infra CAS、重放吸收或失效 worker 拒绝已实际通过。源读、目标业务行与账本、committed position、故障恢复和清理须分别核对；证据入口见[Transfer 验收](#transfer-验证与证据)。

### continuous 采样与健康诊断

- 位置与窗口：[分区位置与 lag](#分区位置与-lag-事实)、[恢复余量与 unknown](#恢复余量与未知时间估算)。
- 健康判定：[retention](#retention-健康判定与阈值)、[checkpoint 与真实提交时间](#checkpoint-健康与真实提交时间)。
- 读取与使用：[样本与 Capture 安全投影](#诊断样本与-capture-安全投影)、[观测验收边界](#采样诊断的读取与验收边界)。

#### 分区位置与 lag 事实

- continuous worker 必须由 source Provider 读取每分区 `earliest_offset` 和 `latest_offset`，并以目标已成功应用且已提交的 `next_offset` 计算 `lag_records = latest_offset - next_offset`。
- consumer fetch position、预取位置或 Kafka consumer group committed offset 不得冒充 Transfer committed position。

#### 恢复余量与未知时间估算

- retention 恢复余量以 `recovery_headroom_records = next_offset - earliest_offset` 表示。
- 时间余量只能在同一 worker session 内至少存在两次有效 source latest 样本时，按 `recovery_headroom_records / source_rate_records_per_second` 估算；冷启动、无已提交位置、样本时间非正或写入速率为零时必须返回未知，不得伪造为无限安全。

#### retention 健康判定与阈值

- continuous retention health 只允许 `healthy|degraded|critical|unknown`。
- `next_offset < earliest_offset` 立即为 `critical`；有效时间余量不大于 critical 阈值时为 `critical`，不大于 degraded 阈值时为 `degraded`，其余为 `healthy`；不能完成判定时为 `unknown`。
- 第一版统一服务端默认阈值为 degraded `6h`、critical `1h`，并要求 critical 小于 degraded；阈值是 Transfer runtime 配置，不进入用户任务 JSON。

#### checkpoint 健康与真实提交时间

- retention health 与 checkpoint health 是两个正交指标。
- retention health 回答“当前 committed position 是否仍在源保留窗口内，以及按当前写入速率还剩多少恢复时间”；checkpoint health 回答“存在 source lag 时，目标 committed position 是否长期未推进”。
- checkpoint health 只允许 `healthy|degraded|unknown`：lag 为 0 时为 `healthy`；lag 大于 0 且最近真实 position commit 距采样时间超过部署阈值时为 `degraded`；缺少真实 position commit 时间或无法计算时为 `unknown`。
- 不得用 execution `updated_at`、worker heartbeat、consumer fetch position 或 diagnostics 采样时间冒充 position commit 时间。

#### 诊断样本与 Capture 安全投影

- 观测样本写入 `common.task_executions.metadata.continuous`，至少包含样本时间、总体 retention health、总体 checkpoint health，以及每分区 earliest/latest/committed/lag/headroom/source rate/retention horizon/checkpoint age/checkpoint health。
- Transfer 是采集和判定 owner；Monitor 只读取统一 execution metadata 并展示，不直连业务 Kafka，不读取或改写 `transfer.sync_states` 与 `transfer.runtime_leases`。
- 数据库 CDC 在同一次样本中额外写入唯一公共投影 `metadata.continuous.capture`，固定包含 capture `generation`，以及类型化、安全裁剪后的 `source_recovery` 和 `source_transactions`；尚无有效事实的字段省略。
- 该投影不得包含 connector/source 私有错误、连接信息、凭据、Provider 私有资源名或未声明字段。
- 非数据库 continuous execution 不得保留 `capture`。
- Monitor 只消费该公共投影，不得读取 `transfer.capture_resources` 或 Provider 私表补判。

#### 采样诊断的读取与验收边界

`position_committed_at` 的持久事实与采样展示见[分区诊断说明](../../../transfer/docs/transfer-任务语义与同步模式.md#分区位置与-kafka-恢复窗口)，采样间隔、阈值和策略配置页面沿[运行配置](../../../transfer/docs/transfer-基本概念及配置说明.md#诊断与安全投影)。数据库捕获健康、源日志窗口和事务压力分别沿[CDC 观测契约](Transfer数据库CDC.md#捕获线程与源连接健康)及其相邻专题，不与 Kafka retention 或 checkpoint health 混用。

公共与专业响应的允许字段、身份与读取授权沿[专业读取规范](执行监控.md#transfer-专业执行读取与响应)，不能由原始 metadata、私有位置或诊断字段的持久化推定访问权。健康状态或时间余量估算不证明暂停后仍可恢复、实际位点已提交或目标数据正确；恢复前仍按[位置恢复边界](../../../transfer/docs/transfer-任务语义与同步模式.md#用户运行控制)核对，证据入口见[Transfer 验收](#transfer-验证与证据)。

### 通用执行告警规则与统一评估

- 事实与目标：[公共事实与职责](#通用告警公共事实与职责)、[目标与最新根执行模式](#规则目标与最新根执行模式)。
- 判定与管理：[失败规则与恢复](#通用失败规则与恢复判定)、[continuous 模式切换](#continuous-专用评估与模式切换)、[规则与通知路由](#规则生命周期与通知路由)。
- 评估与验收：[汇总与查询失败](#evaluator-汇总与查询失败边界)、[超时与停滞限制](#超时与未开放停滞规则)、[实际验收](#通用告警实际验收边界)。

#### 通用告警公共事实与职责

- 通用执行告警只允许消费 `common.task_executions` 公共事实。
- 各 owner 模块负责写入真实 execution 状态、开始/结束时间和安全错误摘要；Monitor 不得读取 owner 任务表、结果表、worker 私有租约或运行时内部状态补判失败。
- System 只负责保存模块注册时发布的 TaskProvider 角色声明、解析当前 Backend 租约、认证和操作审计，不成为告警事实或规则 owner。

#### 规则目标与最新根执行模式

- 第一版租户告警规则精确绑定 `tenant + module + task_type + source_task_id`，只允许有稳定任务定义身份且 `parent_execution_id IS NULL` 的根 execution。
- 可配置目标以该任务最新根 execution 的模式为准；最新根 execution 属于 continuous session 时，不得因历史 bounded execution 将该任务继续暴露为通用规则目标。
- 无 `source_task_id` 的 ad-hoc execution、Orchestrator 子 execution 和 System cleanup 子 execution不进入通用规则；父编排失败由 Orchestrator 根 execution 表达。
- Monitor 的通用告警目标发现和规则创建校验必须与 System 当前已启用 TaskProvider 的非废弃 `task_capabilities[]` 取交集。
- 历史 execution 必须保留审计，但已经不再由 provider 声明的旧 `module + task_type` 不得出现在新规则目标中，也不得继续创建或重新启用规则；不得在 Monitor 硬编码业务任务类型白名单。

#### 通用失败规则与恢复判定

- 通用规则类型固定为 `last_terminal_failed|last_terminal_timeout|consecutive_failures`。
- 最近终态查询只考虑 `success|failed|timeout`，忽略 `cancelled`；`failed` 或 `timeout` 告警只能由后续 `success` 恢复，新的 `pending|running` 不证明任务已经恢复。
- `consecutive_failures` 的阈值只允许 2 到 20，最近 N 个有效终态全部为 `failed|timeout` 时激活，任一 `success` 中断连续失败序列。

#### continuous 专用评估与模式切换

- Transfer continuous session 不消费通用 execution 失败规则。
- Monitor 继续以 `metadata.continuous`、retention、checkpoint 和 recovery circuit owner 事实运行专用 evaluator；不得把自动恢复产生的失败 session 再解释成通用任务失败。
- Transfer bounded execution 可以使用通用规则；同一任务从 bounded 切换为 continuous 后，通用 evaluator 必须依据最新根 execution 暂停该任务规则，不能继续沿用旧 bounded 终态打开告警。

#### 规则生命周期与通知路由

- Monitor 使用 `monitor.alert_rules` 保存租户规则，至少包含稳定 `rule_id` UUID、名称、精确任务身份、规则类型、连续失败阈值、严重级别和启用状态。
- 规则身份进入 incident fingerprint；同一规则和同一任务同时最多一个未恢复 incident。
- 更新规则判定语义、停用或删除规则时，必须先恢复该规则当前未恢复 incident，不能留下永不再评估的活动告警。
- Monitor 使用 `monitor.notification_routes` 保存通用规则到 Webhook/邮件 destination 的显式绑定。
- 路由必须按租户校验 destination 存在；同一规则、渠道和目标只能一条。
- 规则没有路由时仍创建不可变 alert event，但不生成 Webhook 或邮件 delivery。
- 删除目标时必须同时删除指向它的未使用路由；历史 incident、event 和 delivery 保留。

#### evaluator 汇总与查询失败边界

- 通用规则 evaluator、Transfer continuous evaluator 和后续其他 evaluator 必须先完成各自公共事实查询，再把全部 active signal 交给同一个 incident reconciler。
- 任何 evaluator 查询失败时，不得把该 evaluator 拥有的现有 incident 误恢复；不同 evaluator 不能独立执行全局“缺失即恢复”。
- 通用规则 evaluator 使用最终一致的周期扫描即可，第一版不要求 Kafka。
- 若后续使用 PostgreSQL `LISTEN/NOTIFY`，通知只能作为唤醒信号，`common.task_executions` 仍是唯一事实源，周期扫描仍负责丢失唤醒后的收敛。

#### 超时与未开放停滞规则

- `created_at`、`updated_at` 或全局固定时长不能单独证明 execution 卡死。
- 运行超时由 owner 按任务自身 deadline 写入 `status=timeout`；在统一 heartbeat/deadline 契约完成前，不实现 `pending_stalled` 或 `running_stalled` 通用规则，也不得启用按创建时间批量改写长任务状态的清理逻辑。

#### 通用告警实际验收边界

continuous 的 Owner 信号和 schema blocked 恢复沿[专用告警契约](#continuous-观测信号告警与过程事件分工)，Webhook／邮件 lifecycle event、outbox 和真实投递沿[渠道契约](#租户告警通知与渠道投递)，不得用一次执行状态、规则保存或测试通知代替相应链路结果。用户执行读取与安全 DTO 继续沿[执行监控规范](执行监控.md#通用执行读取与过程事件)，规则身份或通知路由不额外授予执行和专业结果读取权。

目标发现、最新模式切换、有效终态序列、规则变更、查询失败、通知路由及实际恢复分别核对。证据入口见[Monitor 验收](#monitor-验证与证据)；既有执行诊断、平台资源告警或局部门禁不外推为本节所有规则与外部通知已验收。

### 租户告警通知与渠道投递

- 共同规则：[生命周期与事务 outbox](#通知生命周期与事务-outbox)。
- Webhook：[目标与协议安全](#webhook-租户目标与协议安全)、[确认抑制与测试](#webhook-确认抑制与测试)、[重投删除与审计](#webhook-重投删除与审计)。
- 邮件：[目标与冻结内容](#邮件租户目标与冻结投递内容)、[SMTP 与投递状态](#smtp-部署与邮件投递状态)、[测试重投删除与审计](#邮件测试重投删除与审计)。
- 实际范围：[配置与送达验收](#通知配置与实际送达验收)。

#### 通知生命周期与事务 outbox

- 告警通知第一版支持 Monitor 通用 Webhook 和平台统一 SMTP Relay 邮件。
- 告警打开、严重级别升级和恢复必须分别产生且只产生一条不可变 `opened|escalated|resolved` lifecycle event；incident 变化、event 及所有启用通知渠道的 per-destination delivery outbox 必须在同一 Infra PostgreSQL 事务提交。
- 各渠道 dispatcher 在事务外发送，并用 `FOR UPDATE SKIP LOCKED`、领取租约、指数退避和最大尝试次数实现至少一次投递；不得在持有数据库行锁时调用外部网络。

#### Webhook 租户目标与协议安全

- Webhook destination 按租户隔离，配置只允许 `name`、`url`、`enabled`、`event_types` 和签名 `secret`。
- `event_types` 只允许 `opened|escalated|resolved` 且必须非空；secret 必须使用平台 `ENCRYPTION_KEY` 加密，任何 GET/List/Swagger 响应不得返回 secret 明文或密文。
- 默认只允许 HTTPS 公网地址；开发或受管内网接收端必须由部署配置显式允许，不能由任务或普通 API 请求绕过 SSRF 约束。
- Webhook payload schema 固定为 `monitor.alert.webhook/v1`，只包含 `delivery_id`、event/incident 稳定身份、告警级别/状态、任务定义身份、execution UUID、生命周期时间和 Console 链接；不得发送业务数据、连接凭据、完整 execution metadata 或 owner 私有状态。
- 请求签名固定为 HMAC-SHA256(`timestamp + "." + raw_body`)，接收方必须按 `delivery_id` 幂等并校验 timestamp 防重放。

#### Webhook 确认抑制与测试

- 告警确认只记录处理状态，不产生 Webhook lifecycle event。
- 抑制期间生命周期 event 继续保留，但 delivery 记为 `suppressed` 且到期后不补发；恢复后同类信号再次打开会形成新的 incident 和新的 `opened` event。
- 禁用 destination 只影响尚未领取和后续生成的 delivery，已完成投递审计必须保留。
- Webhook 测试投递是目标配置运维动作，不得伪造 `opened|escalated|resolved` 告警事件或写入正式 delivery outbox。
- 测试请求使用独立 `monitor.webhook.test/v1` payload，沿用目标 HMAC secret、SSRF 校验和签名 Header，同步返回成功 HTTP 状态；接收端失败统一视为下游调用失败。

#### Webhook 重投删除与审计

- 手动重投只允许作用于当前租户的 `dead` delivery。
- 重投必须复用原 `delivery_id` 和 payload，以保持接收端幂等；目标必须仍存在且启用，重新入队时使用目标当前 URL 和 secret。
- `attempt_count` 保存累计尝试次数，`retry_base_attempt_count` 保存本次重投周期开始时的累计基线，dispatcher 的最大尝试次数和退避只按当前周期计算；每次人工重投递增 `manual_retry_count`。
- 删除 Webhook destination 必须在同一事务中取消该目标尚未领取的 `pending` delivery、清除其中的 secret 和重试时间，再删除目标配置。
- 历史 `delivered|dead|suppressed|cancelled` delivery 必须保留；删除时已经领取的 delivery 可以完成当前请求，但删除后不得再生成新 delivery，也不能再人工重投。
- Webhook destination 创建、更新、测试、删除及 delivery 人工重投必须复用平台统一 Audit Middleware 写入 System 审计日志。
- 审计请求体必须经过现有敏感字段脱敏，不能记录 secret 明文或密文；Monitor 不新增私有操作审计表。

#### 邮件租户目标与冻结投递内容

- 邮件 destination 按租户隔离，配置只允许 `name`、`recipients`、`enabled` 和 `event_types`。
- `recipients` 必须是非空邮箱地址数组，去重后最多 50 个；`event_types` 只允许 `opened|escalated|resolved` 且必须非空。
- 租户 API 不得接收或返回 SMTP host、port、username、password、TLS 模式或发件身份。
- 邮件使用独立 `monitor.email_destinations` 和 `monitor.email_deliveries`，不得复用 Webhook destination 或 delivery 表。
- 邮件 delivery 必须冻结目标名称、收件人、主题、纯文本正文和 HTML 正文；内容只包含稳定告警摘要、任务/执行身份和 Console 链接，不得包含业务数据、连接凭据、完整 execution metadata 或 owner 私有状态。

#### SMTP 部署与邮件投递状态

- SMTP Relay 属于 Monitor 部署配置。
- 第一版只允许显式 `starttls` 或 `tls`：`starttls` 必须要求服务端成功升级 TLS，`tls` 使用隐式 TLS；不支持明文 SMTP、机会式降级或按端口自动猜测。
- username/password 必须同时配置或同时为空。
- SMTP host 为空时邮件 dispatcher 不启动，既有未投递 outbox 保持 `pending`，不得改写为 `suppressed`、`dead` 或已送达。
- 邮件 delivery 状态只允许 `pending|delivering|delivered|dead|suppressed|cancelled`。
- SMTP 调用成功后进入 `delivered`；失败按当前尝试周期指数退避，达到最大尝试次数后进入 `dead`。
- 领取租约过期的 `delivering` delivery 可以由新 worker 重新领取，投递身份不变。

#### 邮件测试重投删除与审计

- 邮件测试投递是目标配置运维动作，使用独立测试主题和正文，同步验证目标当前收件人及平台 SMTP Relay，不得伪造告警 lifecycle event 或写入正式 delivery outbox。
- SMTP 未配置或发送失败统一视为下游调用失败。
- 邮件人工重投只允许当前租户的 `dead` delivery，必须复用原 `delivery_id`、主题和正文，使用目标当前收件人，并按 `retry_base_attempt_count` 开启新的最大尝试周期；累计 `attempt_count` 和 `manual_retry_count` 必须保留。
- 删除邮件 destination 必须取消该目标尚未领取的 `pending` delivery 并保留历史；删除时已领取的发送可以完成，删除后不得生成新 delivery 或人工重投。
- 邮件 destination 创建、更新、测试、删除及 delivery 人工重投必须复用平台统一 Audit Middleware 写入 System 审计日志。
- Monitor 不建立第二套操作审计，不在普通 API、Swagger、投递记录、日志或审计中暴露 SMTP password。

#### 通知配置与实际送达验收

共同事件、两渠道独立目标／delivery、前端唯一入口和接收端 Header 等接入说明见[租户外部通知专题](../../concepts/monitoring/执行诊断与租户告警.md#租户外部通知契约)；SSRF 放行、TLS 和 SMTP Relay 的部署归属见[通知配置规范](../configuration/Monitor与运行观测.md#告警评估与通知策略)。Platform 渠道和订阅仍沿[平台通知契约](../addp平台运行观测规范.md#73-历史订阅与通知)，不因租户通知可用而自动扩展范围。

配置保存、事务 outbox 入队、测试发送成功和真实告警生命周期投递分别验收。实际送达须核对固定源码、真实接收端、不可变 event 与 delivery 证据，既有执行诊断或进程清理结果不替代通知链路验收；证据入口见[Monitor 验收](#monitor-验证与证据)。

### continuous 观测信号、告警与过程事件分工

阅读导航：[Owner 实时信号](#continuous-owner-实时观测信号)、[告警生命周期](#continuous-告警生命周期与最新根执行)、[Schema 公共事实](#schema-change-公共事实投影)、[Schema 告警恢复与竞态](#schema-blocked-告警恢复与停止竞态)、[扫描失败限制](#扫描提交失败的告警限制)、[过程事件与告警的区别](#过程事件与告警事实边界)。

#### continuous Owner 实时观测信号

- `recovery_circuit_state=open`、retention `critical`、retention `degraded`、checkpoint `degraded`、恢复等待和 `half_open` 可以由统一前端归一化为实时观测信号。
- 数据库 CDC 还可从 `metadata.continuous.capture` 唯一派生 `source_recovery_critical`、`source_recovery_unavailable` 和 `source_transactions_unavailable`：源恢复窗口 `critical` 为严重级别，显式 `unknown` 恢复窗口或显式 `unavailable` 事务观测为警告级别；事实对象缺失时不产生信号。
- Monitor 不得根据活跃事务数、持续时间或 Undo 用量自行设阈值和告警。
- 该信号是 execution metadata 的无状态展示投影，不是新的 task/execution 状态，不写回数据库，也不等同于持久化告警事件或已发送通知。
- 后续若实现通知、确认、抑制和升级策略，必须复用这些 owner 事实并单独定义告警生命周期，不能反向让 Monitor 成为 Transfer runtime owner。

#### continuous 告警生命周期与最新根执行

- Monitor 按 `tenant + module + task_type + source_task_id` 只消费最新根 execution 的观测信号；普通运行信号来自该最新 execution 的 `pending|running` 状态，schema blocked 终态信号按[Schema 告警恢复与停止竞态](#schema-blocked-告警恢复与停止竞态)处理。
- 告警状态只允许 `open|acknowledged|resolved`：新信号打开事件，确认后进入 acknowledged，最新根 execution 不再提供对应 owner 信号后自动 resolved，不回扫历史 execution 维持旧告警。
- 抑制使用独立 `suppressed_until`，不新增 suppressed 状态；确认和抑制均不改变信号、execution 或任务状态。
- 告警按 `tenant + module + task_type + source_task_id + signal_code` 去重，同一身份同时最多一个未恢复事件；恢复后的同类信号再次出现必须创建新事件，保留历史，不复用旧行。

#### CDC 任务不可恢复风险告警（2026-11-13 已确认）

本批在现有任务级观测上增加不可恢复风险告警，不新增采样器、任务状态或公共观测存储。Transfer 是当前 task／capture generation 及样本有效性的事实 owner；Monitor 不读 Transfer 私表、不直连源库／Kafka，不把任务当前事实写回已结束 execution。

Transfer 提供唯一机器读取入口 `GET /runtime/cdc-risk-observations?after_id=<上一页任务ID>`，固定每批最多 100 个当前 CDC 任务，按稳定任务 ID 正序发布；首批 after_id 为 0。仅 `addp-monitor` Platform Service Access Token 且具备不可委派、非租户自定义的 `transfer.cdc_risk_observation.read` 可以读取；该 Permission 只加入 `platform.monitor_runtime`，不加入 User 角色。Monitor 经已有 Gateway 路由调用，分页读取完整清单，不能从可能已清理的 execution 历史发现暂停任务。100 是单次传输界限，不是任务容量上限。

响应固定为版本、观察时间、items、next_after_id 和 has_more；每项只含 Tenant／task ID、当前 generation、最近 execution UUID、任务观测状态及 source／buffer 两项风险状态、固定原因码和采样／过期时间。禁止返回 Connector／Topic／Group／槽名称、源连接、事件正文或异常原文。已完成捕获清理的任务明确返回 closed；任务删除／不再属于 CDC 时退出当前清单。只有成功取得完整清单才可按缺失恢复原事件，任一页失败或不完整不以缺失推定关闭。读取失败、未初始化、停止清理未完成、缺样和过期均为 unknown。每项 Tenant 身份由 Owner 数据行发布，不接受调用者伪造；后台跨 Tenant 事实不授予用户读取权。

- `cdc_source_recovery_lost`：仅本任务 PostgreSQL 逻辑槽的可信观测明确为 missing 或原生 WAL `lost` 时触发 critical。`reserved|extended|unreserved` 不是已经丢失；槽不活跃本身不触发本项告警。其他源端尚未开放本项原生判定。
- `cdc_buffer_recovery_lost`：仅当前 generation 的有效 `buffer_observation.recovery_state=lost` 触发 critical；暂停期间照常评估。积压大于零本身不触发本批告警。

两项分别处理 active／clear／unknown。新鲜明确风险打开／维持事件；同源新鲜 clear 或 Owner 明确 closed 才恢复。单项未知、Transfer／Gateway 不可用只保留对应已有事件，不更新 LastObservedAt，不冒充恢复，也不阻断其他来源的既有规则评估。generation 变化后，必须读到当前代次的明确事实才结束旧代次事件。去重身份为 Tenant＋Transfer sync task＋generation＋signal code，沿原事件事务、确认／抑制及通知 outbox；execution UUID 仅保留为当前 Owner 关联依据，不把其历史诊断当作风险事实。用户列表、更新及通知继续沿现有任务／执行读取权限，不以后台 Service 事实扩大用户可见范围。风险事件按自身 Tenant／模块／任务类型复用 Owner 的 TaskHistory 准入，不依赖对应 execution 行仍被保留；普通 execution 告警仍按其执行范围过滤。

#### Schema change 公共事实投影

- 数据库 CDC schema change 的跨模块公共事实固定写入触发阻塞的 execution `metadata.continuous.schema_change`。
- 该对象至少包含 `request_id`、`status=pending|applied|stopped`、generation/revision、源 partition/offset、scope、diff 和检测时间；审批后还包含 `metadata_scan.status=pending|running|success|failed`、attempt，以及成功提交 Meta scan 时返回的 Meta execution UUID。
- Transfer 必须在私有 request 状态变化的同一 Infra PostgreSQL 事务中更新该投影，Monitor 不得读取 `transfer.schema_change_requests` 补判。

#### Schema blocked 告警恢复与停止竞态

- `schema_change.status=pending` 是 `schema_change_blocked` 严重告警的唯一 owner 信号。
- 仅当同一任务最新根 execution 已经 `failed + stop_reason=schema_change_blocked` 且其投影仍为 pending 时，该信号有效；取样范围不扩展到历史 pending schema-change execution。
- 更新的根 execution 取代旧执行的告警信号；新执行不再携带该信号时恢复旧 incident，不能因历史投影仍为 pending 而保持旧告警。
- 人工 additive migration 审批把同一投影原子更新为 `applied` 后，Monitor 自动恢复 incident；不可逆 Stop 在删除任务私有控制状态前将其更新为 `stopped`。
- Stop 与 worker Finish 无论谁先提交，Finish 都必须以已经持久化的 `desired_state=stopped` fencing，不能把任务重新置为 blocked 或留下 pending 公共信号；有效 lease 已释放或过期后，Stop 还必须在 capture cleanup 前把遗留 active execution 取消并将 task 收敛到 idle。
- 若 worker 在 request 提交后、Finish 前失联，lease expiry reconciler 必须将原 execution/task 收敛为 schema blocked 并清空 lease owner，禁止创建或领取 recovery execution。

#### 扫描提交失败的告警限制

- `metadata_scan.status=failed` 当前表示 Transfer 向 Meta 提交自动扫描失败，不证明扫描结果失败。
- 手工 Meta 扫描尚无回写 Transfer 的关联完成事实，因此该状态只在 Transfer 任务详情和 execution metadata 中展示，第一版不得据此创建无法可靠自动恢复的 Monitor incident；未来若要告警，必须先定义关联 Meta execution 或显式完成回执，不能用 Resume、新 execution 或时间流逝猜测恢复。

#### 过程事件与告警事实边界

| 事实 | 职责与权威正文 |
| --- | --- |
| `common.execution_events` 执行过程事件 | 记录一次实际 execution／attempt 的 started、progress、终态与截断；continuous 的事务、Runtime fencing、安全字段及预算沿[过程事件写入契约](执行监控.md#transfer-continuous-过程事件)，读取授权与通用存储沿[执行监控规范](执行监控.md#通用执行读取与过程事件)。 |
| Monitor incident、告警 lifecycle event 与 delivery | 按本节 Owner 信号和最新根执行维护告警，通知事务 outbox、Webhook／邮件和投递审计沿[渠道投递契约](#租户告警通知与渠道投递)；不能把投递记录或测试发送当作执行过程与恢复事实。 |

过程事件的 30 天滚动清理与概览 180 天目标保留、只读评估和物理删除分别沿[留存契约](执行监控.md#概览保留与清理契约)认定；不将一种事件的状态、预算或留存要求直接套到另一种事件。证据分别见[Transfer 验收](#transfer-验证与证据)和[Monitor 验收](#monitor-验证与证据)，静态契约或局部结果不证明完整运行、告警或外部通知链路通过。

### 进度观测与编排边界

阅读导航：[进度与 Monitor 读取](#continuous-进度与-monitor-读取)、[Orchestrator 编排限制](#orchestrator-编排限制)。

#### continuous 进度与 Monitor 读取

- continuous execution 不使用 0 到 100 作为主要进度；Monitor 应展示 per-partition next offset、source latest offset、lag、吞吐、last event time、last committed time、checkpoint age/health、retry/rebalance、恢复 circuit 和 heartbeat。
- Monitor 只消费 owner 写入的统一 execution metadata；不得自行连接 Kafka 或 Transfer 私有表补算这些事实。

#### Orchestrator 编排限制

- Orchestrator v1 不编排 continuous Transfer task。
- Transfer 的 TaskProvider 任务发现不得把 continuous task 暴露为可选编排步骤，provider execute 入口也必须拒绝 `source=orchestrator` 触发 continuous task；长期服务型 Step 语义进入 Orchestrator v2 专题。

### 任务状态、执行身份与运行租约

阅读导航：[用户期望与实际状态](#用户期望与实际运行状态)、[运行控制与执行身份](#运行控制与执行身份)、[租约与业务位点](#runtime-lease-与业务位点)、[读取与验收边界](#状态租约的读取与验收边界)。

#### 用户期望与实际运行状态

- continuous task definition 必须保存独立活状态 `desired_state=running|paused|stopped`，初始值为 `stopped`。
- 该字段不进入 config JSON，也不复用最近 execution `status`；bounded task 不消费该字段。
- 任务实际状态继续使用 `status=idle|running|blocked`；`blocked` 表示系统发现不可安全恢复的条件，但不覆盖用户 `desired_state`。
- 当前只有数据库 CDC schema drift 使用 `blocked`。

#### 运行控制与执行身份

- `start` 原子设置 `desired_state=running` 并创建 `status=pending` 的 execution。`pause` 设置 `desired_state=paused`，`resume` 设置 `desired_state=running` 并创建新的 pending execution，`stop` 设置 `desired_state=stopped`。
- 同一 task 已有合法 active session 时不得重复创建 execution。
- 每次启动或自动恢复都创建新的 pending execution，一个 execution 对应一个 runtime session，结束后不得重新变回 running。显式 Resume 沿 Start 路径创建新执行，`retry_of_execution_id` 和 `metadata.recovered_from_execution_id` 均为空。

#### runtime lease 与业务位点

- runtime session 由 `transfer.runtime_leases` 保存 owner instance、execution、lease deadline、heartbeat 和 fencing token；lease 与 `transfer.sync_states` 的业务 position 分离。

租约字段、task 唯一约束、同事务 claim／owner／lease／token 更新和 partition state CAS 见[运行租约与 fencing 持久约束](../../../transfer/docs/数据库架构.md#运行租约字段与-fencing)，不以 execution heartbeat 或 Kafka group committed offset 代替 Transfer 已提交位置。

#### 状态／租约的读取与验收边界

字段归属与 CDC 阻塞后的状态收敛见[任务状态与阻塞持久事实](../../../transfer/docs/数据库架构.md#任务状态与-cdc-阻塞收敛)，当前 User、逐动作 Permission 和读取范围见[任务表入口](../../../transfer/docs/tables/tasks表.md#用户任务定义与控制)及[专业读取契约](执行监控.md#transfer-专业执行读取与响应)。持久状态与私有租约字段不自动成为公共响应或读取授权。

状态保存、owner／heartbeat 存在或 lease 过期，不证明 Worker 真正停止、目标位点已提交或故障恢复成功。停止与 Cleanup 的实际停止证明沿[停止验收](#停止与清理验收边界)，运行态与数据事实分别认定，证据入口见[Transfer 验收](#transfer-验证与证据)。

### 任务级 Pause/Stop 与标准取消

阅读导航：[continuous 真实停止](#continuous-的真实停止)、[任务控制与标准取消范围](#任务级控制与标准取消范围)、[停止与清理验收](#停止与清理验收边界)。

#### continuous 的真实停止

- 用户 pause/stop 必须真实停止 poll、完成或放弃当前未提交批次、关闭 source/target session、释放 partition ownership 和 lease，再以 `cancelled` 结束当前 execution，并在 metadata 记录 `stop_reason=paused|stopped`。
- resume 创建新 execution。

#### 任务级控制与标准取消范围

任务定义的 Stop 当前只用于 continuous runtime。bounded Pause/Resume 只控制未来调度，不中断 active execution，不能把任务控制当作 bounded execution 的真实取消；具体路由、当前 User 与逐动作 Permission 见[任务定义控制入口](../../../transfer/docs/tables/tasks表.md#用户任务定义与控制)。

- `supports_cancel` 是整个 `sync` task type 的 TaskProvider 能力，在 bounded execution 也具备真实取消前必须继续为 `false`，不得注册标准 execution cancel endpoint。

当前不提供 execution 级 cancel/stop API，`transfer.task.cancel` 在 Manifest 中仍为 disabled；已有任务级 Stop 不代表该 Permission 已启用或 TaskProvider 取消能力已开放。两类后续事项分别见[bounded 取消规划](../../next/transfer后续能力清单.md#一bounded-同步)和[标准取消规划](../../next/transfer后续能力清单.md#四continuous-运行时与观测)。

#### 停止与清理验收边界

数据库 CDC Stop 的服务端确认、不可逆终态和资源清理沿[CDC 停止契约](Transfer数据库CDC.md#恢复与不可逆停止)，不能混用普通业务 Kafka 的可恢复 committed-position 语义。任务删除／System Cleanup 的原 invocation 实际停止证明与未知外部效果处理沿[Transfer 清理职责](../addp-cleanup体系规范.md#transfer)和[正式 Owner 约束](../addp-cleanup体系规范.md#十五已确认决策)。

路由、capability、Permission 或 execution 状态的静态核对不证明真实 Worker 中断、目标事务回滚、session／lease 释放或资源清理通过；实际身份链路与停止、恢复须分别验收，操作与身份核对见[任务表验收边界](../../../transfer/docs/tables/tasks表.md#执行恢复与验收边界)，其他证据见[Transfer 验收](#transfer-验证与证据)。

### continuous 自动恢复、退避与 circuit

阅读导航：[触发与执行身份](#恢复触发与执行身份)、[领取与排除条件](#恢复领取与排除条件)、[指数退避与 circuit](#指数退避与-circuit-状态)、[审计与部署配置](#恢复审计与部署配置)、[读取与实际验收](#恢复读取与实际验收边界)。

#### 恢复触发与执行身份

- 正常总运行时长不触发 timeout。
- source poll、target apply、position commit 分别使用操作超时；heartbeat 丢失或 lease 过期以 `failed` 结束当前 execution，并在任务 desired state 仍为 running 时创建新 execution 恢复。
- worker 正常退出时当前 execution 以 `cancelled + stop_reason=worker_shutdown` 结束，由新 worker 自动创建 recovery execution。
- lease 过期恢复同样创建新 execution，并记录 `recovery_reason=lease_expired`。自动恢复继承上一 execution 的 `trigger_type`，通过公共字段 `retry_of_execution_id` 关联上一 execution，Transfer 恢复审计另在 `metadata.recovered_from_execution_id` 记录同一 UUID。旧 execution 保持原终态。 [公共执行记录字段](执行记录与租约.md#执行记录规范)

#### 恢复领取与排除条件

- 可恢复失败必须立即创建唯一的 pending recovery execution，并通过 deployment-level `recovery_not_before` 控制领取时间；不得依赖 worker 内存 timer，也不得为同一次失败重复创建 execution。
- worker shutdown 不增加连续失败次数；普通 execution failure 和 lease expiry 增加连续失败次数。
- `schema_change_blocked`、用户 Pause 和 Stop 不进入自动恢复。

#### 指数退避与 circuit 状态

- 自动恢复使用指数退避，达到最大连续失败次数后 circuit 进入 `open`，冷却到期后的 pending execution 被领取时转为 `half_open`。
- circuit open 是临时恢复治理状态，不复用任务 `status=blocked`；此时任务保持 `desired_state=running`、无合法 runtime lease，实际 `status=idle`。
- 任一目标 position 成功提交，或 session 连续稳定运行达到部署阈值后再发生失败，连续失败计数重置。

#### 恢复审计与部署配置

- recovery execution metadata 至少记录不可变的本次恢复审计 `recovery_reason`、`recovered_from_execution_id`、`recovery_attempt`、`recovery_not_before`、`recovery_backoff_seconds`，以及可变运行状态 `recovery_consecutive_failures`、`recovery_circuit_state=closed|open|half_open`。
- 成功提交 position 只重置连续失败计数和 circuit 状态，不得覆盖本次 recovery execution 的 attempt/backoff 审计。
- 退避初值、上限、最大连续失败次数、circuit 冷却时间和稳定运行阈值是 Transfer runtime 部署配置，不进入任务 JSON。

#### 恢复读取与实际验收边界

恢复读取已提交位置、lease 过期的 `stop_reason` 和领取时 fencing 递增等运行细节见[Transfer 生命周期](../../../transfer/docs/transfer-任务语义与同步模式.md#自动恢复与退避)；策略管理入口见[配置说明](../../../transfer/docs/transfer-基本概念及配置说明.md#自动恢复与运行策略)。部署参数保存、pending execution 创建、冷却到期或状态展示均不证明实际恢复成功。

持久字段与安全 DTO 的区别、User／模块结果／TaskProvider 的身份及读取范围见[执行记录表](../../../transfer/docs/tables/task_executions表.md#自动恢复字段与安全-dto)和[专业读取契约](执行监控.md#transfer-专业执行读取与响应)，不得因恢复 metadata 存在而扩大响应或授权。证据入口见[Transfer 验收](#transfer-验证与证据)；真实故障后的新执行、目标提交、恢复与清理结果分别核对。

### continuous 首次扫描与 claim

阅读导航：[触发与精确目标](#首次扫描触发与精确目标)、[CDC 快照提交条件](#cdc-快照完成与提交条件)、[claim 与失败接管](#首次扫描-claim-与失败接管)、[结构变更与非触发事件](#结构变更与非触发事件)、[来源与验收边界](#首次扫描来源与验收边界)。

#### 首次扫描触发与精确目标

- `auto_scan_metadata=true` 的 continuous task 不等待 execution 成功结束。
- 普通业务 Kafka task 在目标 Provider 首次成功建立目标结构后，按 task `apply_identity` 提交完整目标表 locator 的 `targets`，触发一次精确 Meta deep scan，不扫描所属 schema／database。

#### CDC 快照完成与提交条件

- 数据库 CDC task 的 connector 必须启用 Debezium `sink` notification，并把通知写入当前 generation-owned 单分区 CDC data topic；Transfer 只接受 connector name 与当前 generation 完全一致的 `aggregate_type=Initial Snapshot` 通知，所有合法初始快照通知都作为目标 `skip` 与 apply ledger、Transfer committed position 一起推进，只有 `type=COMPLETED` 所在 offset 成功提交后才触发首次扫描。
- 该顺序同时覆盖非空表与空表，不依赖首条数据事件、`source.snapshot=last` 或等待时间猜测。

通知编码、身份校验、`skip` 与 `COMPLETED/SKIPPED` 的协议区别见[CDC 事件契约](Transfer数据库CDC.md#事件编码与初始快照通知)。

#### 首次扫描 claim 与失败接管

- 首次扫描使用 task 私有持久化 claim：`running(claim_token, lease_until, attempt+1) -> success|failed`，worker 恢复或并发实例不得重复提交；已失败或过期 running claim 由后续 runtime session 接管。
- Meta 提交失败不得中断 CDC/Kafka 数据面，失败状态由后续 runtime session 重试或由用户手动刷新。
- claim TTL 统一使用[部署配置](../configuration/Kafka与Capture部署.md#meta-补扫-claim-租期) `TRANSFER_META_SCAN_CLAIM_TTL`，并且必须大于 Meta client 的 HTTP 超时。

这里是 task 级首次扫描 claim；request 级结构审批补扫的接管入口与失败终态另见[审批补扫 claim](Transfer数据库CDC.md#审批补扫-claim-接管与失败)，不得互换恢复规则。

#### 结构变更与非触发事件

- 目标 schema 经正式流程变化后另行触发 deep scan；普通 DML、单条事件和单个 batch 不触发 Meta scan。

#### 首次扫描来源与验收边界

后台扫描的精确目标、持久来源和父 execution 绑定以[生产来源契约](生产来源与产物扫描.md#生产执行与后台扫描的来源)为准。claim、提交状态或保存的 Meta execution UUID 不额外授予调用或读取权限；私有字段、安全投影、提交成功与实际扫描完成的区别见[任务表结果核对](../../../transfer/docs/tables/tasks表.md#首次扫描的提交与结果核对)。

Meta API 提交失败的反馈与仓储／fencing 错误的不同处理见[持久化与失败边界](../../../transfer/docs/数据库架构.md#九写后-meta-扫描)。提交成功或数据面继续运行不证明 Meta 执行完成或目标 attributes 正确，必须通过已授权的 Meta 入口核对对应执行与目标事实；证据入口见[Transfer 验收](#transfer-验证与证据)。

### 验证与历史证据入口

测试分层与标准入口遵循[测试与验收规范](../addp测试与验收规范.md#三标准入口)。本节集中维护证据导航，各专题的权限、事务、停止、回滚、告警与送达条件仍在对应正文中分别认定。

#### Transfer 验证与证据

- 标准门禁、环境前提与 Oracle CDC 专项入口缺口：[README 启动与验证](../../../transfer/README.md#启动与验证)。
- 实际覆盖范围、完整身份／Worker／数据面链路与未覆盖项：[任务语义验证范围](../../../transfer/docs/transfer-任务语义与同步模式.md#十一验证范围)。
- 固定源码、运行尝试、有效证据、失败与修复、清理结果：[分版本证据索引](../../../transfer/CLAUDE.md#专项约束与分版本证据)。

#### Monitor 验证与证据

- 标准入口与模块验证边界：[Monitor 开发与验证](../../../monitor/CLAUDE.md#开发与验证)。
- 告警、通知、执行诊断、事件与留存的分版本通过／失败、清理及未覆盖项：[Monitor 历史证据](../../../monitor/CLAUDE.md#历史证据导航)。

证据仅适用于所列源码版本、运行尝试和实际场景；未运行、失败、跳过或未覆盖项不计为通过。契约、静态检查、配置保存、入队或局部成功不外推为完整认证、运行、数据提交、清理或外部通知验收通过。
