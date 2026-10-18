# ADDP Transfer-Kafka与DLQ规范

[返回任务体系规范](../addp任务体系规范.md)

定义业务 Kafka DLQ、bounded replay 和 Kafka / Kafka Connect 部署基线。连续执行和数据库捕获分别见[同步执行](Transfer同步执行.md)、[数据库 CDC](Transfer数据库CDC.md)。

阅读导航：[Transfer 业务 Kafka DLQ 与 bounded replay v1 契约](#transfer-业务-kafka-dlq-与-bounded-replay-v1-契约)、[Kafka 与 Kafka Connect 部署基线](#kafka-与-kafka-connect-部署基线)。

## Transfer 业务 Kafka DLQ 与 bounded replay v1 契约

该契约是当前公开实现的唯一技术路线。Transfer capability 与 Swagger 已公开业务 Kafka `block|dead_letter` 和唯一 bounded replay API；Console 新建任务默认仍显式发送 `block`，不得依赖字段省略猜默认值。

业务 Kafka continuous task 的记录失败策略固定使用：

```json
{
  "runtime": {
    "boundary": "continuous",
    "record_failure": {"mode": "block"}
  }
}
```

`mode` 只允许 `block|dead_letter`。业务 Kafka 新建和更新任务必须显式提交该字段，Console 默认显式发送 `block`；不得把字段省略解释为兼容旧配置。数据库 CDC 固定为 `block`，不得提交 `dead_letter`。

DLQ v1 约束：

1. 只有业务 Kafka record 在 JSON object 解码、未知/缺失字段、key 提取或字段类型转换阶段产生的确定性数据错误可以进入 DLQ。source open/poll、目标 prepare/apply、数据库锁或连接、runtime lease/fencing、retention 越界、Infra Kafka/Infra PostgreSQL 错误必须失败并阻塞；它们不是可跳过数据。
2. 数据库 CDC 的 envelope/source/schema/protocol 漂移继续按 `schema_change_blocked` 处理，不能通过 DLQ 越过。Debezium tombstone、truncate、message 和未知 operation 也不得降级为业务 Kafka DLQ。
3. dead-letter identity 固定由 `apply_identity + source_identity + partition + record_offset` 计算，重复处理同一源记录必须得到同一 identity。Infra Kafka topic 固定使用 Transfer-owned `__addp_dlq.<tenant_id>.<task_id>` namespace，原业务 Kafka principal 不得访问。
4. DLQ envelope schema 固定为 `transfer.dead_letter/v1`，至少包含 dead-letter identity、tenant/task/execution、source identity、原 topic/partition/offset/timestamp、原 key/value/headers 的无损 base64、稳定错误 code/category/message 和 detected_at。不得在错误文本中写入连接凭据。
5. `transfer.dead_letters` 只保存查询和审计所需的控制索引、Infra Kafka payload reference 和首次/最近观测时间；原始大 payload 只保存在 Infra Kafka。控制索引按 dead-letter identity 唯一，重复写入不得产生第二条逻辑记录。
6. 处理顺序固定为：幂等写 DLQ payload -> 幂等落控制索引 -> PostgreSQL/MySQL/Oracle 目标以 `operation=skip` 原子推进各自业务目标 ledger -> Transfer 以 runtime fencing + state version CAS 推进 `transfer.sync_states`。只有前一步成功才能执行下一步；任一步失败都不得提交后续 position。
7. `skip` 只表示该 source position 已经由显式 dead-letter 策略审计跳过，不携带业务 row，也不修改目标业务表。目标 Provider 必须在同一事务校验 apply identity/source/target/partition 和单调位置后推进 ledger。
8. 目标 ledger 已推进但 Infra state CAS 前崩溃时，重试必须复用同一 dead-letter identity，目标 `skip` 必须按 monotonic ledger 幂等吸收，随后重新提交 Infra state；不得产生静默丢数或位置缺口。
9. DLQ topic 和控制索引在普通 pause/stop 后保留；task 删除和 System cleanup 由 Transfer cleanup owner 幂等删除。Infra retention 到期只删除 payload 后，控制索引必须明确显示 payload unavailable，不得伪装为可 replay。
10. 每个 task 的 DLQ topic 第一版固定为 1 partition，record key 固定为 dead-letter identity，`cleanup.policy=compact,delete`；compaction 收敛同一 identity 的物理重复记录，delete retention 形成真实 payload 可用边界。Transfer principal 只在 `__addp_dlq.` namespace 拥有 Create/Write/Read/Describe/DescribeConfigs，Connect 和业务 Kafka principal 不得访问；retention 与 replication factor 只属于部署配置，不进入任务 JSON。
11. DLQ 管理 API 必须挂在 owner task 下，唯一只读路由为 `GET /task-definitions/:id/dead-letters` 和 `GET /task-definitions/:id/dead-letters/:identity`。服务端必须先按认证租户校验 task ownership，再以 `tenant_id + task_id` 查询控制索引；不得提供跨 task 查询、全局 identity 直查或旧路由。
12. 列表使用统一分页响应 `{data,total,page,page_size,total_pages}`，默认按 `last_observed_at DESC, identity DESC` 稳定排序；只允许 `source_partition`、`error_category`、`error_code`、`payload_available` 精确过滤。`page_size` 最大 100，非法过滤或分页参数返回 `400 Bad Request`，不存在的 task 或不属于当前租户的 task 统一返回 `404 Not Found`。
13. 第一阶段列表与详情只返回安全控制事实：identity、source topic/partition/offset/timestamp、首次/最近 execution、稳定错误 code/category/safe message、payload availability、首次/最近观测时间和 occurrence count。不得公开 Infra Kafka payload topic/partition/offset、原始 key/value/headers、`apply_identity` 或租户内部字段；也不提供删除、编辑、重新分类、伪回放状态或以 DLQ 行直接创建 replay 的 API。
14. `payload_available` 只表达控制索引已确认的 payload 可用状态，不承诺无限保存。管理 API 不因读取列表或详情而扫描、消费或改写 Infra Kafka；payload retention/compaction 的可用性收敛由 Transfer-owned 独立治理流程负责，不能在请求链路中引入昂贵的逐条 Kafka 探测。
15. payload availability 治理必须由 Transfer continuous worker 内的独立低频 reconciler 承担，不进入 HTTP 请求链路，也不建立 consumer group、提交 offset 或创建第二套 replay source。reconciler 只扫描当前 `payload_available=true` 的控制索引，并按 identity 游标分批轮转，允许多实例重复核验但结果必须幂等。
16. 核验必须使用控制索引当前保存的 `payload_topic + payload_partition + payload_offset` 精确读取。只有 exact offset 的 record key 等于 dead-letter identity，且 `addp-schema=transfer.dead_letter/v1` 时才确认仍可用；topic/partition 不存在、offset 已越过当前 `[earliest,latest)` 或 fetch 已跨过该 compacted hole 时确认不可用。认证、网络、broker、超时或其他瞬时错误不得把 payload 标记为不可用。
17. 标记不可用必须使用 `identity + payload_topic + payload_partition + payload_offset + payload_available=true` 条件更新；重复观测若已写入新的 payload reference，旧核验结果必须因 CAS 不匹配而失效。该更新只修改 `payload_available` 和技术更新时间，不得改写 `first_observed_at`、`last_observed_at`、错误、execution 或 source 审计事实。
18. availability 核验可以在进程内读取 Kafka record key/header 以确认身份，但不得反序列化、记录日志、返回或持久化原始 value/key/headers 副本。公开 API 继续只返回安全控制索引；payload 从 `false` 恢复为 `true` 的唯一主路径是同一 source record 被 runtime 再次观测并成功写入新的 DLQ payload/reference。
19. 普通 pause/stop 和 System logical cleanup 必须保留 DLQ topic 与控制索引。用户直接删除任务和 System physical cleanup 必须走同一个 task-owned resource cleanup：数据库 CDC 先完成既有 capture cleanup；业务 Kafka task 随后由 Infra Kafka admin 幂等删除确定性 `__addp_dlq.<tenant_id>.<task_id>` topic；外部资源清理成功或确认不存在后，才允许进入唯一最终事务删除 task-private state 与任务定义。
20. DLQ topic 删除失败、权限不足或 Infra Kafka 不可用时，必须保留控制索引和任务定义并返回失败，不能先删数据库事实形成不可追踪的孤儿 Kafka topic。topic 已删除但控制索引或任务定义删除失败时，重试必须把 unknown topic 视为成功并继续数据库清理。
21. 当前为业务 Kafka continuous 的 task，无论 `record_failure.mode=block|dead_letter`，物理删除时都必须尝试删除确定性 DLQ topic；当前配置已改变但仍保留 `transfer.dead_letters` 索引的 task 也必须执行同一删除。只有既非业务 Kafka且不存在 DLQ 控制索引的 bounded/CDC task 才可跳过 Kafka cleanup，不能仅根据当前 `record_failure.mode=block` 猜测从未产生 DLQ。
22. task-owned resource cleanup 不删除目标业务表、目标行、业务目标 apply ledger 或 `common.task_executions` 历史。直接任务删除仍使用 owner 模块既有 soft-delete；System physical cleanup 使用 unscoped delete，但两者必须复用同一外部资源清理与最终数据库事务，不保留两条删除路线。直接删除运行中任务返回 `409 Conflict`；外部资源 cleanup 不可用或失败返回 `503 Service Unavailable`，响应不得泄露 Kafka、数据库或凭据细节。
23. 物理清理 continuous task 前，cleanup 必须先把 `desired_state` 原子设置为 `stopped`，取消尚未 claim 的 pending execution，再等待 active runtime owner 释放 lease 或 lease 到期。等待超时必须停止后续清理；不得在 worker 仍可能续租、写目标或提交 position 时删除 `runtime_leases` / `sync_states`。lease 已过期但 execution 仍为 pending/running 时，最终删除事务必须以 `stop_reason=cleanup` 收敛为 `cancelled` 终态并保留该 execution。直接 Delete 与 System physical cleanup 使用相同的统一 stop timeout/poll policy。
24. bounded task 没有真实 worker 中断能力。直接 Delete 继续对 `status=running` 返回 409；System physical cleanup 遇到 running bounded task 必须记录失败并保留任务及其私有状态，不能通过改数据库状态伪取消运行体。
25. capture/DLQ 外部资源成功清理且 continuous runtime 已停止后，必须在一个锁定 task definition 行的 Infra PostgreSQL 事务中删除该 task 的 `transfer.dead_letters`、`transfer.sync_states`、`transfer.runtime_leases`、`transfer.capture_resources`，并在同一事务 soft/unscoped delete task definition。事务中发现 task 已重新进入 running/desired running 或仍有有效 runtime owner/lease 时整体失败回滚，封住私有状态提交后并发 Start 重新创建运行事实的窗口。
26. `sync_states`、`runtime_leases` 和 `capture_resources` 是仅服务于仍存在 task definition 的当前运行控制事实，不是长期审计实体；任务删除后不得保留孤儿行。长期审计由保留的 `common.task_executions`、System audit 和 cleanup execution/result 承担。目标业务数据、业务目标 apply ledger、bounded replay 隔离结果和公共 execution 历史仍不随 task 删除。

bounded replay v1 约束：

1. replay 不是新的 `task_type`，也不修改任务定义。唯一 owner API 为 `POST /task-definitions/:id/replay`，请求只接受显式 per-partition `[start_offset,end_offset)` ranges 和一个不存在的新 PostgreSQL target endpoint；不接受 source、mapping、key 或主任务目标的任意覆盖。
2. replay v1 只支持业务 Kafka `record/json` task。source ranges 从原业务 Kafka topic 读取，提交前必须验证完整落在当前 retention `[earliest_offset,latest_offset]` 内；DLQ topic 不是 replay source。
3. replay 是独立 bounded execution，使用 execution-scoped apply identity 和独立目标 ledger，从 range start 顺序处理到冻结 end 后结束。它不读取或写入 `transfer.sync_states`、主 task `desired_state`、主 runtime lease、主 apply identity 或主目标 ledger。
4. replay target 必须是不存在的新 PostgreSQL table。仅有幂等 upsert/delete 不足以保证历史事件写回原目标时的时间顺序，因此原目标、已有目标、append 目标和“业务接受重复”均不得绕过隔离要求。
5. replay 不允许编辑 DLQ payload、替换历史 record、覆盖主 committed position或在成功后自动切换主任务 target。若要用 replay 结果替换业务数据，必须由独立、显式的数据发布或切换流程负责。
6. replay 可与主 continuous runtime 并行，因为 source 只读且 target 强制隔离；Transfer 必须仍按部署容量限制 replay 并发。replay failure 不改变主任务状态，不触发 continuous 自动恢复 circuit。
7. replay execution metadata 至少保存 source ranges、请求时 earliest/latest 快照、隔离 target identity、execution apply identity、每分区当前位置和最终统计。所有终态沿用统一 execution status。

## Kafka 与 Kafka Connect 部署基线

1. 版本固定为 Redpanda v24.3.18 和 Debezium Connect 3.6.0.Final；该 Connect 镜像内置 Kafka Connect 4.3.0。不得同时维护 Apache Kafka 或另一 Kafka 发行版作为 CDC 部署主线，任务/API/数据库也不得出现发行版选择。
2. 开发环境使用 1 broker 和 1 Connect worker，replication factor 为 `1`；生产参考使用 3 broker 和至少 2 Connect worker，replication factor 为 `3`，producer `acks=all`，由 Raft majority 提供确认 quorum=2。
3. Kafka Connect shared internal topics 固定为 `__addp_connect_configs`（1 partition）、`__addp_connect_offsets`（25 partitions）和 `__addp_connect_status`（5 partitions），均使用 `cleanup.policy=compact`；复制因子按开发 `1`、生产 `3`。
4. 任务级 CDC topic 固定命名为 `__addp_cdc.<tenant_id>.<task_id>.<capture_generation>`，第一版 1 partition、`cleanup.policy=delete`、默认 `retention.ms=604800000`（7 天）。生产必须同时配置并校验 `retention.bytes`；time/bytes 任一先到即构成真实恢复边界。
5. 容量规划至少使用 `峰值编码字节/秒 * 允许恢复秒数 * 副本因子 * 1.3`，并预留 Connect internal topics、segment 和 broker 高水位空间。备份不替代 connector offset + source WAL + Kafka retention 的连续恢复条件。
6. principal 固定分离为 infra admin、`addp-connect` 和 `addp-transfer`：infra admin 创建/配置/删除内部 topic 与 ACL；Connect 只读写 shared internal topics，并向 `__addp_cdc.*` 写入；Transfer 只描述/读取 `__addp_cdc.*` 和使用自己的 consumer group。业务 Kafka principal 不得访问 infra namespace。
7. 单机开发 Compose 只在本机端口和 Docker 网络使用 `SASL_PLAINTEXT/SCRAM-SHA-256` 以验证 principal/ACL；生产必须使用 `SASL_SSL`，密码从部署 secret 注入，Kafka bootstrap 和 Connect REST 都不得暴露到公共网络。
8. Kafka Connect 禁止依赖 broker auto-create 创建 CDC topic。capture supervisor 在创建 connector 前按规范显式创建 topic/ACL，在删除 connector 并确认停止后删除任务级 topic。shared internal topics 和 broker 数据目录由 Infra 部署 owner 管理，不随单个 task cleanup 删除。
9. PostgreSQL connector 固定使用 `plugin.name=pgoutput`、`publication.autocreate.mode=filtered`、单表 `table.include.list`、服务端生成的 slot/publication 名称和 `slot.drop.on.stop=false`。stop/cleanup 由 capture supervisor 在 connector 停止后显式删除 ADDP-owned slot/publication，不依赖 connector 退出副作用。
