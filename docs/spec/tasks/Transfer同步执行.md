# ADDP Transfer同步执行规范

[返回任务体系规范](../addp任务体系规范.md)

定义 Transfer sync 的正交配置维度、bounded / continuous 边界、状态推进与观测要求。数据库捕获见[CDC](Transfer数据库CDC.md)，业务 Kafka 的拒绝记录与重放见[Kafka 与 DLQ](Transfer-Kafka与DLQ.md)。

阅读导航：[Transfer 同步执行契约](#transfer-同步执行契约)、[Transfer continuous sync v1 契约](#transfer-continuous-sync-v1-契约)。

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

第一版约束：

1. source 只支持用户在 System 注册的业务 Kafka Engine，locator 必须定位 `type=topic`；Infra Kafka 不进入公开任务配置。
2. `envelope` 只允许 `record`，`encoding` 只允许 `json`，value 必须是 JSON object；tombstone、数组、标量、Avro、Protobuf、Schema Registry 和 Debezium envelope 均不支持。
3. 稳定 key 只允许从 JSON value 的显式非空字段提取；`key.source` 固定为 `value`，`key.fields` 非空。Kafka 原生 record key 第一版只保留为诊断事实，不参与目标身份推导。
4. 同一业务 key 的事件必须由生产者稳定写入同一 partition。Transfer 保证 partition 内顺序，不承诺跨 partition 的同 key 全局顺序。
5. 目标支持声明完整原子应用能力的 PostgreSQL/MySQL native table，并且必须使用 `apply_mode=upsert`、显式目标 keys 和 `PartitionedTableChangeApplyProvider`；普通 `TableUpsertProvider` 不能单独满足 continuous fencing。keyless append 和可能产生重复的普通 append 不进入第一版。
6. Kafka JSON 没有可依赖的表 schema，任务必须通过显式 `field_mapping` 固定目标字段和类型；source key fields 映射后的目标字段必须与 `target.policy.keys` 一一对应且非空。未知字段、缺失必填字段或不兼容 schema 变化必须阻塞 partition 并使当前 execution 失败，不得静默丢弃。
7. `start.mode` 固定为 `committed`。没有 committed state 的 partition 必须由任务显式选择 `initial=earliest|latest`；不得由 consumer 默认值或 auto commit 猜测。
8. Transfer 为每个 partition 在 `transfer.sync_states` 保存 `position.type=kafka_offset`、`position.version=v1` 和 `next_offset`。目标批次成功提交后才允许用 runtime fencing token 和 state version 做 CAS 推进；worker 恢复时必须 seek 到 `next_offset`。
9. 每个 continuous task 必须有服务端生成且不可修改的 `apply_identity` UUID，不写入 config。PostgreSQL Provider 在业务目标库维护 `addp_transfer.apply_positions`，MySQL Provider 在目标业务数据库维护 `_addp_transfer_apply_positions` InnoDB 私有表；两者都以 `apply_identity + source_identity + target_identity + partition` 校验应用主线，并把业务行 upsert 与目标 `next_offset` 在同一事务提交。
10. Transfer 必须把 poll 结果按 partition 拆成顺序批次，每条已映射记录携带消费后 `next_offset`。Provider 锁定 ledger 行，拒绝位置缺口，跳过不大于已应用位置的重复记录；同一批剩余记录出现相同目标 key 时保留最高 `next_offset` 的最后状态，再执行一次原子 upsert。
11. 投递保证固定为 `at-least-once delivery + target monotonic apply`。目标提交与 Infra position CAS 之间崩溃会重放，但目标 ledger 必须吸收重复批次，并阻止租约过期 worker 在新 worker 之后写回旧状态；不得宣称跨 Kafka、业务目标数据库和 Infra PostgreSQL 的分布式 exactly-once。
12. consumer auto commit 禁用。Kafka consumer group 只用于分区分配，不是 Transfer committed position 的事实源。
13. 阶段 2B 的初始范围不提供 replay、DLQ、物理删除、Debezium、CDC、Kafka target 或 continuous append；当前业务 Kafka 已按后文 4A/4B 唯一契约开放 `dead_letter` 和写入新隔离目标的 bounded replay。数据库 CDC、Kafka target、物理删除和 continuous append 仍不得借用业务 Kafka 的 DLQ/replay 路线绕过各自边界。
14. resume 前必须验证 committed `next_offset` 仍位于 Kafka partition 的 `[earliest_offset, latest_offset]` 范围；低于 earliest 表示 retention 已破坏连续恢复条件，execution 必须明确失败，不得静默重置到 earliest 或 latest。高于 latest 视为状态损坏，同样失败。
15. continuous worker 必须由 source Provider 读取每分区 `earliest_offset` 和 `latest_offset`，并以目标已成功应用且已提交的 `next_offset` 计算 `lag_records = latest_offset - next_offset`。consumer fetch position、预取位置或 Kafka consumer group committed offset 不得冒充 Transfer committed position。
16. retention 恢复余量以 `recovery_headroom_records = next_offset - earliest_offset` 表示。时间余量只能在同一 worker session 内至少存在两次有效 source latest 样本时，按 `recovery_headroom_records / source_rate_records_per_second` 估算；冷启动、无已提交位置、样本时间非正或写入速率为零时必须返回未知，不得伪造为无限安全。
17. continuous retention health 只允许 `healthy|degraded|critical|unknown`。`next_offset < earliest_offset` 立即为 `critical`；有效时间余量不大于 critical 阈值时为 `critical`，不大于 degraded 阈值时为 `degraded`，其余为 `healthy`；不能完成判定时为 `unknown`。第一版统一服务端默认阈值为 degraded `6h`、critical `1h`，并要求 critical 小于 degraded；阈值是 Transfer runtime 配置，不进入用户任务 JSON。
18. 观测样本写入 `common.task_executions.metadata.continuous`，至少包含样本时间、总体 retention health、总体 checkpoint health，以及每分区 earliest/latest/committed/lag/headroom/source rate/retention horizon/checkpoint age/checkpoint health。Transfer 是采集和判定 owner；Monitor 只读取统一 execution metadata 并展示，不直连业务 Kafka，不读取或改写 `transfer.sync_states` 与 `transfer.runtime_leases`。
    数据库 CDC 在同一次样本中额外写入唯一公共投影 `metadata.continuous.capture`，固定包含 capture `generation`，以及类型化、安全裁剪后的 `source_recovery` 和 `source_transactions`；尚无有效事实的字段省略。该投影不得包含 connector/source 私有错误、连接信息、凭据、Provider 私有资源名或未声明字段。非数据库 continuous execution 不得保留 `capture`。Monitor 只消费该公共投影，不得读取 `transfer.capture_resources` 或 Provider 私表补判。
19. retention health 与 checkpoint health 是两个正交指标。retention health 回答“当前 committed position 是否仍在源保留窗口内，以及按当前写入速率还剩多少恢复时间”；checkpoint health 回答“存在 source lag 时，目标 committed position 是否长期未推进”。checkpoint health 只允许 `healthy|degraded|unknown`：lag 为 0 时为 `healthy`；lag 大于 0 且最近真实 position commit 距采样时间超过部署阈值时为 `degraded`；缺少真实 position commit 时间或无法计算时为 `unknown`。不得用 execution `updated_at`、worker heartbeat、consumer fetch position 或 diagnostics 采样时间冒充 position commit 时间。
20. `recovery_circuit_state=open`、retention `critical`、retention `degraded`、checkpoint `degraded`、恢复等待和 `half_open` 可以由统一前端归一化为实时观测信号。数据库 CDC 还可从 `metadata.continuous.capture` 唯一派生 `source_recovery_critical`、`source_recovery_unavailable` 和 `source_transactions_unavailable`：源恢复窗口 `critical` 为严重级别，显式 `unknown` 恢复窗口或显式 `unavailable` 事务观测为警告级别；事实对象缺失时不产生信号。Monitor 不得根据活跃事务数、持续时间或 Undo 用量自行设阈值和告警。该信号是 execution metadata 的无状态展示投影，不是新的 task/execution 状态，不写回数据库，也不等同于持久化告警事件或已发送通知。后续若实现通知、确认、抑制和升级策略，必须复用这些 owner 事实并单独定义告警生命周期，不能反向让 Monitor 成为 Transfer runtime owner。
21. Monitor 按 `tenant + module + task_type + source_task_id` 只消费最新根 execution 的观测信号；普通运行信号来自该最新 execution 的 `pending|running` 状态，schema blocked 终态信号按第 48 条处理。告警状态只允许 `open|acknowledged|resolved`：新信号打开事件，确认后进入 acknowledged，最新根 execution 不再提供对应 owner 信号后自动 resolved，不回扫历史 execution 维持旧告警。抑制使用独立 `suppressed_until`，不新增 suppressed 状态；确认和抑制均不改变信号、execution 或任务状态。告警按 `tenant + module + task_type + source_task_id + signal_code` 去重，同一身份同时最多一个未恢复事件；恢复后的同类信号再次出现必须创建新事件，保留历史，不复用旧行。
22. 告警通知第一版支持 Monitor 通用 Webhook 和平台统一 SMTP Relay 邮件。告警打开、严重级别升级和恢复必须分别产生且只产生一条不可变 `opened|escalated|resolved` lifecycle event；incident 变化、event 及所有启用通知渠道的 per-destination delivery outbox 必须在同一 Infra PostgreSQL 事务提交。各渠道 dispatcher 在事务外发送，并用 `FOR UPDATE SKIP LOCKED`、领取租约、指数退避和最大尝试次数实现至少一次投递；不得在持有数据库行锁时调用外部网络。
23. Webhook destination 按租户隔离，配置只允许 `name`、`url`、`enabled`、`event_types` 和签名 `secret`。`event_types` 只允许 `opened|escalated|resolved` 且必须非空；secret 必须使用平台 `ENCRYPTION_KEY` 加密，任何 GET/List/Swagger 响应不得返回 secret 明文或密文。默认只允许 HTTPS 公网地址；开发或受管内网接收端必须由部署配置显式允许，不能由任务或普通 API 请求绕过 SSRF 约束。
24. Webhook payload schema 固定为 `monitor.alert.webhook/v1`，只包含 `delivery_id`、event/incident 稳定身份、告警级别/状态、任务定义身份、execution UUID、生命周期时间和 Console 链接；不得发送业务数据、连接凭据、完整 execution metadata 或 owner 私有状态。请求签名固定为 HMAC-SHA256(`timestamp + "." + raw_body`)，接收方必须按 `delivery_id` 幂等并校验 timestamp 防重放。
25. 告警确认只记录处理状态，不产生 Webhook lifecycle event。抑制期间生命周期 event 继续保留，但 delivery 记为 `suppressed` 且到期后不补发；恢复后同类信号再次打开会形成新的 incident 和新的 `opened` event。禁用 destination 只影响尚未领取和后续生成的 delivery，已完成投递审计必须保留。
26. Webhook 测试投递是目标配置运维动作，不得伪造 `opened|escalated|resolved` 告警事件或写入正式 delivery outbox。测试请求使用独立 `monitor.webhook.test/v1` payload，沿用目标 HMAC secret、SSRF 校验和签名 Header，同步返回成功 HTTP 状态；接收端失败统一视为下游调用失败。
27. 手动重投只允许作用于当前租户的 `dead` delivery。重投必须复用原 `delivery_id` 和 payload，以保持接收端幂等；目标必须仍存在且启用，重新入队时使用目标当前 URL 和 secret。`attempt_count` 保存累计尝试次数，`retry_base_attempt_count` 保存本次重投周期开始时的累计基线，dispatcher 的最大尝试次数和退避只按当前周期计算；每次人工重投递增 `manual_retry_count`。
28. 删除 Webhook destination 必须在同一事务中取消该目标尚未领取的 `pending` delivery、清除其中的 secret 和重试时间，再删除目标配置。历史 `delivered|dead|suppressed|cancelled` delivery 必须保留；删除时已经领取的 delivery 可以完成当前请求，但删除后不得再生成新 delivery，也不能再人工重投。
29. Webhook destination 创建、更新、测试、删除及 delivery 人工重投必须复用平台统一 Audit Middleware 写入 System 审计日志。审计请求体必须经过现有敏感字段脱敏，不能记录 secret 明文或密文；Monitor 不新增私有操作审计表。
30. 邮件 destination 按租户隔离，配置只允许 `name`、`recipients`、`enabled` 和 `event_types`。`recipients` 必须是非空邮箱地址数组，去重后最多 50 个；`event_types` 只允许 `opened|escalated|resolved` 且必须非空。租户 API 不得接收或返回 SMTP host、port、username、password、TLS 模式或发件身份。
31. 邮件使用独立 `monitor.email_destinations` 和 `monitor.email_deliveries`，不得复用 Webhook destination 或 delivery 表。邮件 delivery 必须冻结目标名称、收件人、主题、纯文本正文和 HTML 正文；内容只包含稳定告警摘要、任务/执行身份和 Console 链接，不得包含业务数据、连接凭据、完整 execution metadata 或 owner 私有状态。
32. SMTP Relay 属于 Monitor 部署配置。第一版只允许显式 `starttls` 或 `tls`：`starttls` 必须要求服务端成功升级 TLS，`tls` 使用隐式 TLS；不支持明文 SMTP、机会式降级或按端口自动猜测。username/password 必须同时配置或同时为空。SMTP host 为空时邮件 dispatcher 不启动，既有未投递 outbox 保持 `pending`，不得改写为 `suppressed`、`dead` 或已送达。
33. 邮件 delivery 状态只允许 `pending|delivering|delivered|dead|suppressed|cancelled`。SMTP 调用成功后进入 `delivered`；失败按当前尝试周期指数退避，达到最大尝试次数后进入 `dead`。领取租约过期的 `delivering` delivery 可以由新 worker 重新领取，投递身份不变。
34. 邮件测试投递是目标配置运维动作，使用独立测试主题和正文，同步验证目标当前收件人及平台 SMTP Relay，不得伪造告警 lifecycle event 或写入正式 delivery outbox。SMTP 未配置或发送失败统一视为下游调用失败。
35. 邮件人工重投只允许当前租户的 `dead` delivery，必须复用原 `delivery_id`、主题和正文，使用目标当前收件人，并按 `retry_base_attempt_count` 开启新的最大尝试周期；累计 `attempt_count` 和 `manual_retry_count` 必须保留。删除邮件 destination 必须取消该目标尚未领取的 `pending` delivery 并保留历史；删除时已领取的发送可以完成，删除后不得生成新 delivery 或人工重投。
36. 邮件 destination 创建、更新、测试、删除及 delivery 人工重投必须复用平台统一 Audit Middleware 写入 System 审计日志。Monitor 不建立第二套操作审计，不在普通 API、Swagger、投递记录、日志或审计中暴露 SMTP password。
37. 通用执行告警只允许消费 `common.task_executions` 公共事实。各 owner 模块负责写入真实 execution 状态、开始/结束时间和安全错误摘要；Monitor 不得读取 owner 任务表、结果表、worker 私有租约或运行时内部状态补判失败。System 只负责保存模块注册时发布的 TaskProvider 角色声明、解析当前 Backend 租约、认证和操作审计，不成为告警事实或规则 owner。
38. 第一版租户告警规则精确绑定 `tenant + module + task_type + source_task_id`，只允许有稳定任务定义身份且 `parent_execution_id IS NULL` 的根 execution。可配置目标以该任务最新根 execution 的模式为准；最新根 execution 属于 continuous session 时，不得因历史 bounded execution 将该任务继续暴露为通用规则目标。无 `source_task_id` 的 ad-hoc execution、Orchestrator 子 execution 和 System cleanup 子 execution不进入通用规则；父编排失败由 Orchestrator 根 execution 表达。
39. 通用规则类型固定为 `last_terminal_failed|last_terminal_timeout|consecutive_failures`。最近终态查询只考虑 `success|failed|timeout`，忽略 `cancelled`；`failed` 或 `timeout` 告警只能由后续 `success` 恢复，新的 `pending|running` 不证明任务已经恢复。`consecutive_failures` 的阈值只允许 2 到 20，最近 N 个有效终态全部为 `failed|timeout` 时激活，任一 `success` 中断连续失败序列。
40. Transfer continuous session 不消费通用 execution 失败规则。Monitor 继续以 `metadata.continuous`、retention、checkpoint 和 recovery circuit owner 事实运行专用 evaluator；不得把自动恢复产生的失败 session 再解释成通用任务失败。Transfer bounded execution 可以使用通用规则；同一任务从 bounded 切换为 continuous 后，通用 evaluator 必须依据最新根 execution 暂停该任务规则，不能继续沿用旧 bounded 终态打开告警。
41. Monitor 使用 `monitor.alert_rules` 保存租户规则，至少包含稳定 `rule_id` UUID、名称、精确任务身份、规则类型、连续失败阈值、严重级别和启用状态。规则身份进入 incident fingerprint；同一规则和同一任务同时最多一个未恢复 incident。更新规则判定语义、停用或删除规则时，必须先恢复该规则当前未恢复 incident，不能留下永不再评估的活动告警。
42. Monitor 使用 `monitor.notification_routes` 保存通用规则到 Webhook/邮件 destination 的显式绑定。路由必须按租户校验 destination 存在；同一规则、渠道和目标只能一条。规则没有路由时仍创建不可变 alert event，但不生成 Webhook 或邮件 delivery。删除目标时必须同时删除指向它的未使用路由；历史 incident、event 和 delivery 保留。
43. 通用规则 evaluator、Transfer continuous evaluator 和后续其他 evaluator 必须先完成各自公共事实查询，再把全部 active signal 交给同一个 incident reconciler。任何 evaluator 查询失败时，不得把该 evaluator 拥有的现有 incident 误恢复；不同 evaluator 不能独立执行全局“缺失即恢复”。
44. 通用规则 evaluator 使用最终一致的周期扫描即可，第一版不要求 Kafka。若后续使用 PostgreSQL `LISTEN/NOTIFY`，通知只能作为唤醒信号，`common.task_executions` 仍是唯一事实源，周期扫描仍负责丢失唤醒后的收敛。
45. `created_at`、`updated_at` 或全局固定时长不能单独证明 execution 卡死。运行超时由 owner 按任务自身 deadline 写入 `status=timeout`；在统一 heartbeat/deadline 契约完成前，不实现 `pending_stalled` 或 `running_stalled` 通用规则，也不得启用按创建时间批量改写长任务状态的清理逻辑。
46. Monitor 的通用告警目标发现和规则创建校验必须与 System 当前已启用 TaskProvider 的非废弃 `task_capabilities[]` 取交集。历史 execution 必须保留审计，但已经不再由 provider 声明的旧 `module + task_type` 不得出现在新规则目标中，也不得继续创建或重新启用规则；不得在 Monitor 硬编码业务任务类型白名单。
47. 数据库 CDC schema change 的跨模块公共事实固定写入触发阻塞的 execution `metadata.continuous.schema_change`。该对象至少包含 `request_id`、`status=pending|applied|stopped`、generation/revision、源 partition/offset、scope、diff 和检测时间；审批后还包含 `metadata_scan.status=pending|running|success|failed`、attempt，以及成功提交 Meta scan 时返回的 Meta execution UUID。Transfer 必须在私有 request 状态变化的同一 Infra PostgreSQL 事务中更新该投影，Monitor 不得读取 `transfer.schema_change_requests` 补判。
48. `schema_change.status=pending` 是 `schema_change_blocked` 严重告警的唯一 owner 信号。仅当同一任务最新根 execution 已经 `failed + stop_reason=schema_change_blocked` 且其投影仍为 pending 时，该信号有效；取样范围不扩展到历史 pending schema-change execution。更新的根 execution 取代旧执行的告警信号；新执行不再携带该信号时恢复旧 incident，不能因历史投影仍为 pending 而保持旧告警。人工 additive migration 审批把同一投影原子更新为 `applied` 后，Monitor 自动恢复 incident；不可逆 Stop 在删除任务私有控制状态前将其更新为 `stopped`。Stop 与 worker Finish 无论谁先提交，Finish 都必须以已经持久化的 `desired_state=stopped` fencing，不能把任务重新置为 blocked 或留下 pending 公共信号；有效 lease 已释放或过期后，Stop 还必须在 capture cleanup 前把遗留 active execution 取消并将 task 收敛到 idle。若 worker 在 request 提交后、Finish 前失联，lease expiry reconciler 必须将原 execution/task 收敛为 schema blocked 并清空 lease owner，禁止创建或领取 recovery execution。
49. `metadata_scan.status=failed` 当前表示 Transfer 向 Meta 提交自动扫描失败，不证明扫描结果失败。手工 Meta 扫描尚无回写 Transfer 的关联完成事实，因此该状态只在 Transfer 任务详情和 execution metadata 中展示，第一版不得据此创建无法可靠自动恢复的 Monitor incident；未来若要告警，必须先定义关联 Meta execution 或显式完成回执，不能用 Resume、新 execution 或时间流逝猜测恢复。

continuous task 与 execution 生命周期：

1. continuous task definition 必须保存独立活状态 `desired_state=running|paused|stopped`，初始值为 `stopped`。该字段不进入 config JSON，也不复用最近 execution `status`；bounded task 不消费该字段。
   任务实际状态继续使用 `status=idle|running|blocked`；`blocked` 表示系统发现不可安全恢复的条件，但不覆盖用户 `desired_state`。当前只有数据库 CDC schema drift 使用 `blocked`。
2. `start` 原子设置 `desired_state=running` 并创建 pending execution；`pause` 设置 `paused`，`resume` 设置 `running` 并创建新 execution，`stop` 设置 `stopped`。同一 task 已有合法 active session 时不得重复创建 execution。
3. 每次启动或自动恢复都创建新的 pending execution；一个 execution 对应一个 runtime session，结束后不得重新变回 running。
4. runtime session 由 `transfer.runtime_leases` 保存 owner instance、execution、lease deadline、heartbeat 和 fencing token；lease 与 `transfer.sync_states` 的业务 position 分离。
5. 正常总运行时长不触发 timeout。source poll、target apply、position commit 分别使用操作超时；heartbeat 丢失或 lease 过期以 `failed` 结束当前 execution，并在任务 desired state 仍为 running 时创建新 execution 恢复。worker 正常退出时当前 execution 以 `cancelled + stop_reason=worker_shutdown` 结束；新 worker 必须自动创建 recovery execution，继承上一 execution 的 `trigger_type`，并在 metadata 记录 `recovery_reason` 与 `recovered_from_execution_id`。lease 过期恢复同样创建新 execution，并记录 `recovery_reason=lease_expired`；两种情况都不得把旧 execution 重新置为 running。
   可恢复失败必须立即创建唯一的 pending recovery execution，并通过 deployment-level `recovery_not_before` 控制领取时间；不得依赖 worker 内存 timer，也不得为同一次失败重复创建 execution。worker shutdown 不增加连续失败次数；普通 execution failure 和 lease expiry 增加连续失败次数。`schema_change_blocked`、用户 Pause 和 Stop 不进入自动恢复。
   自动恢复使用指数退避，达到最大连续失败次数后 circuit 进入 `open`，冷却到期后的 pending execution 被领取时转为 `half_open`。circuit open 是临时恢复治理状态，不复用任务 `status=blocked`；此时任务保持 `desired_state=running`、无合法 runtime lease，实际 `status=idle`。任一目标 position 成功提交，或 session 连续稳定运行达到部署阈值后再发生失败，连续失败计数重置。
   recovery execution metadata 至少记录不可变的本次恢复审计 `recovery_reason`、`recovered_from_execution_id`、`recovery_attempt`、`recovery_not_before`、`recovery_backoff_seconds`，以及可变运行状态 `recovery_consecutive_failures`、`recovery_circuit_state=closed|open|half_open`。成功提交 position 只重置连续失败计数和 circuit 状态，不得覆盖本次 recovery execution 的 attempt/backoff 审计。退避初值、上限、最大连续失败次数、circuit 冷却时间和稳定运行阈值是 Transfer runtime 部署配置，不进入任务 JSON。
6. 用户 pause/stop 必须真实停止 poll、完成或放弃当前未提交批次、关闭 source/target session、释放 partition ownership 和 lease，再以 `cancelled` 结束当前 execution，并在 metadata 记录 `stop_reason=paused|stopped`。resume 创建新 execution。
7. 工作包 2B 真实中断闭环完成时可以恢复 Transfer 私有 task-definition stop 控制入口；但 `supports_cancel` 是整个 `sync` task type 的 TaskProvider 能力，在 bounded execution 也具备真实取消前必须继续为 `false`，不得注册标准 execution cancel endpoint。
8. continuous execution 不使用 0 到 100 作为主要进度；Monitor 应展示 per-partition next offset、source latest offset、lag、吞吐、last event time、last committed time、checkpoint age/health、retry/rebalance、恢复 circuit 和 heartbeat。Monitor 只消费 owner 写入的统一 execution metadata；不得自行连接 Kafka 或 Transfer 私有表补算这些事实。
9. Orchestrator v1 不编排 continuous Transfer task。Transfer 的 TaskProvider 任务发现不得把 continuous task 暴露为可选编排步骤，provider execute 入口也必须拒绝 `source=orchestrator` 触发 continuous task；长期服务型 Step 语义进入 Orchestrator v2 专题。
10. `auto_scan_metadata=true` 的 continuous task 不等待 execution 成功结束。普通业务 Kafka task 在目标 Provider 首次成功建立目标结构后，按 task `apply_identity` 触发一次目标父 catalog 的 Meta deep scan。数据库 CDC task 的 connector 必须启用 Debezium `sink` notification，并把通知写入当前 generation-owned 单分区 CDC data topic；Transfer 只接受 connector name 与当前 generation 完全一致的 `aggregate_type=Initial Snapshot` 通知，所有合法初始快照通知都作为目标 `skip` 与 apply ledger、Transfer committed position 一起推进，只有 `type=COMPLETED` 所在 offset 成功提交后才触发首次扫描。该顺序同时覆盖非空表与空表，不依赖首条数据事件、`source.snapshot=last` 或等待时间猜测。首次扫描使用 task 私有持久化 claim：`running(claim_token, lease_until, attempt+1) -> success|failed`，worker 恢复或并发实例不得重复提交；已失败或过期 running claim 由后续 runtime session 接管。Meta 提交失败不得中断 CDC/Kafka 数据面，失败状态由后续 runtime session 重试或由用户手动刷新。目标 schema 经正式流程变化后另行触发 deep scan；普通 DML、单条事件和单个 batch 不触发 Meta scan。claim TTL 统一使用部署配置 `TRANSFER_META_SCAN_CLAIM_TTL`，并且必须大于 Meta client 的 HTTP 超时。
