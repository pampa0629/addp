# ADDP 监控与执行体系图

本文档展示 ADDP 平台的统一执行监控架构和跨模块任务追踪机制。

---

## 目录

1. [监控体系概述](#监控体系概述)
2. [统一执行监控架构](#统一执行监控架构)
3. [任务执行状态流转](#任务执行状态流转)
4. [跨模块监控集成](#跨模块监控集成)

---

## 监控体系概述

ADDP 采用**统一执行监控架构**,通过 `common.task_executions` 表统一记录所有模块的任务执行,Monitor 模块提供集中监控和分析。

**核心特点**:
- **统一记录表**: `common.task_executions` 表记录所有模块任务
- **模块标识**: 通过 `module` 字段区分不同模块的任务
- **任务类型**: 通过 `task_type` 字段区分任务类型
- **集中监控**: Monitor 模块统一查询、分析、可视化

---

## 统一执行监控架构

```mermaid
graph TB
    subgraph "任务执行模块"
        Meta[Meta 模块<br/>scan]
        Transfer[Transfer 模块<br/>sync]
        Develop[Develop 模块<br/>query / workflow / script]
        Manager[Manager 模块<br/>vector_tile_cache_generation / vector_materialized_view_generation / embedding]
        Quality[Quality 模块<br/>check]
        Graph[Graph 模块<br/>kg_build]
        Orchestrator[Orchestrator 模块<br/>orchestration]
    end

    subgraph "统一记录层 (common.task_executions)"
        TaskExec[(common.task_executions 表)]

        Meta --> |写入执行记录| TaskExec
        Transfer --> |写入执行记录| TaskExec
        Develop --> |写入执行记录| TaskExec
        Manager --> |写入执行记录| TaskExec
        Quality --> |写入执行记录| TaskExec
        Graph --> |写入执行记录| TaskExec
        Orchestrator --> |写入执行记录| TaskExec
    end

    subgraph "后台运行实例观测 (common.background_runtime_heartbeats)"
        RuntimeHeartbeat[(后台运行实例心跳)]
        Meta --> |execution worker 心跳| RuntimeHeartbeat
        Transfer --> |bounded / continuous worker 心跳| RuntimeHeartbeat
        Quality --> |execution worker 心跳| RuntimeHeartbeat
    end

    subgraph "监控层 (Monitor 模块)"
        Monitor[Monitor 模块]

        Monitor --> |查询| TaskExec
        Monitor --> |只读查询| RuntimeHeartbeat
        Monitor --> Dashboard[监控看板<br/>实时状态/统计]
        Monitor --> Analysis[执行分析<br/>成功率/耗时分析]
        Monitor --> Alert[告警通知<br/>失败告警]
        Monitor --> Log[日志追踪<br/>错误日志查看]
    end

    classDef module fill:#e1f5ff,stroke:#01579b
    classDef storage fill:#fff9c4,stroke:#f57f17
    classDef monitor fill:#e8f5e9,stroke:#1b5e20

    class Meta,Transfer,Develop,Manager,Quality,Graph,Orchestrator module
    class TaskExec,RuntimeHeartbeat storage
    class Monitor,Dashboard,Analysis,Alert,Log monitor
```

### task_executions 表结构

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | bigint | 执行记录 ID |
| `tenant_id` | int | 租户 ID (租户隔离) |
| `module` | string | 模块名称 (meta/transfer/develop/manager/quality/graph/orchestrator) |
| `task_type` | string | 任务类型 (scan/sync/orchestration/query/workflow/script/vector_tile_cache_generation/vector_materialized_view_generation/embedding/check/kg_build) |
| `source` | string | 触发来源模块 |
| `source_task_id` | string | 任务 ID (对应模块内的任务定义 ID) |
| `trigger_type` | string | `manual` / `scheduled` |
| `status` | string | 执行状态 (pending/running/success/failed/timeout/cancelled) |
| `execution_boundary` | string | `bounded` / `continuous` |
| `attempt` | int | 当前 execution 已取得合法 claim 的次数 |
| `max_attempts` | int | owner 为当前 execution 定义的最大 attempt 数 |
| `lease_owner` | string | 当前 bounded attempt 的可观测 owner；不是写入凭据 |
| `lease_expires_at` | timestamp | 当前 bounded attempt 的租约到期时间 |
| `started_at` | timestamp | 开始时间 |
| `completed_at` | timestamp | 完成时间 |
| `execution_time_ms` | bigint | 执行时长 (毫秒) |
| `metadata` | jsonb | 结果摘要、步骤结果、模块扩展信息 |
| `error_details` | jsonb | 仅保存失败、超时或取消的错误类型、消息和诊断信息；成功 execution 必须为空 |

### 执行运行时角色矩阵

`common.task_executions` 统一记录执行事实。Quality、Meta 和 Transfer bounded 使用相同的 PostgreSQL claim + execution lease 所有权协议；continuous runtime、dispatcher 和 maintenance loop 保持各自专用机制。下面的矩阵区分这些角色，避免把名称相同的后台组件误认为同一机制。

| 运行时角色 | 所属模块 | 进程边界 | 领取/投递事实 | 并发与恢复 | 与 `common.task_executions` 的关系 |
|---|---|---|---|---|---|
| Quality execution worker | Quality | 独立 `quality-worker` | PostgreSQL claim 已授权 `quality/check/pending` execution | 有界槽位；lease token、heartbeat、attempt 和按执行契约恢复 | 执行质量检查、评分和 Issue reconcile，并带所有权条件写终态 |
| Meta scan worker | Meta | 独立 `meta-worker` | PostgreSQL claim `meta/scan/pending` execution | 有界槽位；lease token、heartbeat、attempt 和按扫描提交协议恢复 | 执行扫描、Meta 当前投影更新和终态写入 |
| Transfer bounded worker | Transfer | 独立 `transfer-bounded-worker` | PostgreSQL claim `transfer/sync/pending` bounded execution | 有界槽位；lease token、heartbeat；仅对明确可安全重放的模式自动恢复 | 承担 snapshot、watermark 和 bounded replay，不承载 continuous session |
| Transfer continuous worker | Transfer | 独立 `transfer-continuous-worker` 进程 | `transfer.runtime_leases`、Kafka/CDC position | capacity、heartbeat、fencing；恢复创建新的 recovery execution | 承担 continuous runtime session；`sync_states` 是业务 committed position，不替代 execution 历史 |
| Webhook/Email dispatcher | Monitor | 内嵌 `monitor-backend` | Monitor delivery outbox + `SKIP LOCKED` | 投递 lease、至少一次语义、指数退避和 dead 终态 | 消费告警生命周期 delivery，不创建或改写业务 execution |

**角色边界**:

- execution worker 负责“执行什么以及如何完成”；owner scheduler 只负责“何时创建 execution”；dispatcher 只负责“如何投递通知”。
- Orchestrator scheduler、Manager embedding scheduler、Meta lineage collector 和各模块 cleanup 属于 scheduler/maintenance loop，不自动等同 execution worker。
- Quality、Meta、Transfer bounded 只使用 PostgreSQL claim 单一路线；Backend 只创建 execution，不执行 bounded 业务逻辑，也不保留 Asynq 或本地 channel fallback。
- 每次 claim 生成不可复用 `lease_token`。heartbeat、进度和终态写入同时校验 attempt 与 token；仅凭 Worker 名称或消息 active 状态不能证明所有权。
- `common.background_runtime_heartbeats` 只回答后台进程是否仍在上报，以及其容量与当前占用；Monitor 必须把它与 execution lease、continuous runtime lease 和 delivery lease 分开展示。实例心跳过期不能直接改写任何业务 execution。
- Monitor 对外只投影 `lease_owner`、`lease_expires_at`、attempt、排队/执行时长和恢复原因等安全观测字段，绝不返回 `lease_token` 或 continuous fencing token。

---

## 任务执行状态流转

```mermaid
stateDiagram-v2
    [*] --> pending: 创建任务
    pending --> running: 开始执行
    running --> success: 执行成功
    running --> failed: 执行失败
    running --> timeout: 执行超时
    running --> cancelled: 手动取消

    success --> [*]
    failed --> [*]
    timeout --> [*]
    cancelled --> [*]

    note right of pending
        任务已创建,等待执行
        started_at = null
    end note

    note right of running
        任务正在执行
        started_at = 当前时间
        completed_at = null
    end note

    note right of success
        任务执行成功
        completed_at = 完成时间
        metadata = 结果摘要
    end note

    note right of failed
        任务执行失败
        completed_at = 失败时间
        error_details = 错误信息
    end note

    note right of timeout
        任务执行超时
        completed_at = 超时时间
        error_details = 超时信息
    end note

    note right of cancelled
        任务被手动取消
        completed_at = 取消时间
    end note

    note right of failed
        execution 完成后不得重用
        重试必须创建新的 execution
    end note
```

### 状态说明

| 状态 | 说明 | started_at | completed_at | metadata/error_details |
|------|------|-----------|------------|-------------|
| `pending` | 任务已创建,等待执行 | null | null | null |
| `running` | 任务正在执行 | 开始时间 | null | null |
| `success` | 任务执行成功 | 开始时间 | 完成时间 | metadata |
| `failed` | 任务执行失败 | 开始时间 | 失败时间 | error_details |
| `timeout` | 任务执行超时 | 开始时间 | 超时时间 | error_details |
| `cancelled` | 任务被手动取消 | 开始时间(可选) | 取消时间 | null |

---

## 跨模块监控集成

Monitor 模块统一监控所有模块的任务执行:

```mermaid
sequenceDiagram
    participant User as 用户
    participant Monitor as Monitor 前端
    participant MonitorBE as Monitor Backend
    participant DB as PostgreSQL<br/>(common.task_executions)

    User->>Monitor: 1. 访问监控看板
    Monitor->>MonitorBE: 2. GET /api/monitor/dashboard
    MonitorBE->>DB: 3. SELECT * FROM task_executions<br/>WHERE tenant_id = ?<br/>ORDER BY started_at DESC
    DB-->>MonitorBE: 4. 返回执行记录列表
    MonitorBE->>MonitorBE: 5. 按模块分组统计<br/>(成功率/失败率/平均耗时)
    MonitorBE-->>Monitor: 6. 返回统计数据
    Monitor-->>User: 7. 展示监控看板

    User->>Monitor: 8. 点击某个失败任务
    Monitor->>MonitorBE: 9. GET /api/monitor/executions/:id
    MonitorBE->>DB: 10. SELECT * FROM task_executions<br/>WHERE id = ?
    DB-->>MonitorBE: 11. 返回执行详情
    MonitorBE-->>Monitor: 12. 返回详情 (含 metadata 和错误日志)
    Monitor-->>User: 13. 展示执行元数据、错误详情和日志
```

### 监控功能

**1. 实时监控看板**:
- 当前运行任务数量
- 最近 24 小时任务执行统计
- 按模块分组的成功率/失败率
- 按任务类型分组的平均耗时
- TaskProvider provider health：注册状态、模块 `/health/ready`、capabilities 基础结构，以及标准 `GET /tasks?task_type=` 任务发现响应体是否符合 `{items,total,page,page_size}`
- 后台运行实例健康：分别展示 bounded execution worker、continuous worker 和 dispatcher 的存活实例、容量、当前占用、最近心跳与过期时间
- 执行运行指标：按模块、任务类型和执行边界展示当前积压、吞吐、平均/P95 排队时长、平均/P95 运行时长、失败率、自动 attempt 重试、用户 retry 和恢复次数

执行运行指标使用以下统一口径：

| 指标 | 口径 |
| --- | --- |
| 统计窗口 | 固定允许 `24h`、`7d`、`30d`，以 `created_at` 进入窗口 |
| 当前积压 | 当前全部 `pending` / `running` execution，跨统计窗口计算 |
| 排队时长 | `started_at - created_at`；尚未开始使用查询时刻，未开始即终态使用 `completed_at` |
| 运行时长 | owner 已写入 `execution_time_ms` 优先；否则使用 `completed_at/query_now - started_at` |
| P95 | 窗口内同组 execution 的 PostgreSQL `percentile_cont(0.95)`，不按客户端样本估算 |
| 自动 attempt 重试 | 同一 execution 的 `attempt > 1`，与用户 retry 分离 |
| 用户 retry | 新 execution 的 `retry_of_execution_id` 非空，原 execution 历史不改写 |
| 恢复 | owner 公共 metadata 的 `recovery_reason`，或稳定 `lease_expired` 错误/步骤事实 |
| 吞吐 | 窗口内终态 execution 数除以窗口小时数；不使用当前 Worker 容量伪造理论吞吐 |
| 失败率 | 窗口内 `(failed + timeout) / (success + failed + timeout + cancelled)`；尚未终态的 execution 不进入分母 |

这些指标只用于容量与调度决策，不反向修改 owner task、execution 或 Worker 容量。只有在实际 P95 排队、积压和槽位利用事实持续证明存在隔离需求后，才讨论优先级、租户配额或任务级限流。

Monitor 通过 `GET /executions/runtime-metrics?duration=24h|7d|30d` 暴露上述分组指标，使用 `monitor.statistics.read` 权限；Dashboard 默认展示最近 24 小时，并以低于执行状态列表的频率刷新聚合查询。

**2. 历史执行记录**:
- 按模块、任务类型、状态筛选
- 按时间范围筛选
- 分页查询

**3. 执行详情**:
- 任务参数
- 排队时长、执行时长、attempt / max attempts
- bounded attempt 的 lease owner、lease expiry 和当前 lease 状态；不展示 lease token
- owner 写入的恢复原因或稳定恢复错误码
- 执行元数据摘要和原始 JSON
- 执行结果或错误信息
- 关联日志
- 仅当对应 TaskProvider task type 明确声明 `supports_cancel=true` 时，才在监控侧展示取消入口；`supports_cancel=false` 的执行只展示状态和诊断，不提供标准取消动作。

执行元数据由任务 owner 模块写入 `common.task_executions.metadata`，Monitor 只负责通用展示和轻量摘要，不反向拥有或改写模块产物。对于 Manager 瓦片缓存生成和矢量物化视图，执行详情应能展示实际生成目标、`target_kind`、是否使用外部 3857 优化目标、是否建议矢量物化视图、瓦片生成统计等诊断字段，并保留原始 JSON 作为兜底。

Transfer continuous 运行观测同样遵守 owner 边界：Transfer worker 从业务 Kafka 采集分区 earliest/latest，以目标已提交 `next_offset` 计算 lag、retention 恢复余量和 checkpoint age/health，并写入 `metadata.continuous.diagnostics`。Monitor 只从 `common.task_executions` 展示 health、恢复 circuit 和分区诊断，不直连业务 Kafka，不读取 `transfer.sync_states` 或 `transfer.runtime_leases`。这保证 Monitor 始终是统一观测者，而不是 continuous runtime 的第二个 owner。

Monitor 可以把 owner metadata 无状态归一化为实时观测信号：retention critical、recovery circuit open 与数据库 CDC `schema_change.status=pending` 为严重，retention degraded、checkpoint degraded、恢复等待和 half-open 为警告。观测信号不是新的 execution 状态。普通 continuous 运行信号来自每个任务最新 active execution；schema blocked 信号来自最新一个仍为 pending 的 schema-change 终态 execution，审批将同一公共投影改为 applied 后自动恢复。Monitor evaluator 只扫描 `common.task_executions` 公共事实，把仍存在的信号物化为 `monitor.alert_incidents`，不得读取 `transfer.schema_change_requests`。告警身份包含规则身份和实际任务身份，同一规则、同一任务同时最多一个 `open|acknowledged` 事件。确认只记录操作人和时间，抑制只暂停通知，二者都不得改写 execution metadata 或 owner 私有状态。

通用执行告警由 Monitor 拥有规则策略，但不拥有运行事实。租户规则精确绑定 `module + task_type + source_task_id`，第一版只允许最近失败、最近超时和连续失败；owner 模块负责写真实 `success|failed|timeout`，Monitor 不读取 owner 私有表补判。ad-hoc、子 execution 和 Transfer continuous session 不进入通用规则；同一任务的最新根 execution 已切换为 continuous 时，Monitor 不再沿用其历史 bounded 终态。多个 evaluator 必须先收集全部 active signal，再由一个 reconciler 统一打开、升级和恢复 incident，避免一个 evaluator 错误恢复另一个 evaluator 的告警。

通用规则的外部通知采用显式 `monitor.notification_routes`。规则没有路由时仍保存 incident 和不可变 lifecycle event，但不产生 Webhook/邮件 delivery；有路由时只向当前租户绑定的目标生成 outbox。规则、incident/event、路由和 delivery 均归 Monitor，System 只提供统一操作审计，各任务 owner 不保存通知目标或告警阈值。

Webhook v1 由 Monitor 拥有。告警打开、严重级别由 warning 升为 critical、告警恢复时，Monitor 必须在更新 incident 的同一个 Infra PostgreSQL 事务中写入不可变 `monitor.alert_events`，并为当时已启用且订阅该事件的 `monitor.webhook_destinations` 生成 `monitor.webhook_deliveries` outbox。HTTP 发送只能在事务外执行；dispatcher 使用短事务和 `FOR UPDATE SKIP LOCKED` 领取到期 delivery，按至少一次语义重试。接收方以全局唯一 `delivery_id` 幂等。确认不产生通知；抑制期间仍保留生命周期 event，但对应 delivery 记为 suppressed 且不补发。通知渠道不能反向成为告警、execution 或 Transfer runtime 的事实源。

Webhook 请求固定使用 `monitor.alert.webhook/v1` JSON schema，事件类型只允许 `opened|escalated|resolved`。签名使用目标独立 secret 对 `timestamp + "." + raw_body` 做 HMAC-SHA256，并通过 `X-ADDP-Webhook-ID`、`X-ADDP-Webhook-Timestamp`、`X-ADDP-Webhook-Signature: v1=<hex>` 传递。secret 使用平台 `ENCRYPTION_KEY` 做 AES-256-GCM 加密，API 和投递审计不得返回明文或密文。payload 只包含稳定告警摘要、任务/执行身份和 Console 链接，不携带业务数据、凭据或完整 execution metadata。

Webhook 目标运维不改变告警事实边界。测试投递使用独立 `monitor.webhook.test/v1` schema，同步验证目标当前 URL、secret、SSRF 策略和签名，不生成告警 event 或正式 outbox。人工重投只允许 `dead` delivery，复用原 `delivery_id` 和 payload，并使用目标当前 URL/secret 开启新的最大尝试周期；累计尝试次数和人工重投次数必须保留。删除目标取消未领取投递但保留历史 delivery，已领取请求可以完成。所有写操作进入 System 统一审计日志，Monitor 不建立第二套操作审计。

邮件通知与 Webhook 消费同一条不可变 `monitor.alert_events` 事实流，但使用独立 `monitor.email_destinations` 和 `monitor.email_deliveries`。租户目标只保存名称、收件地址、订阅事件和启用状态；SMTP Relay、TLS 模式、认证凭据与发件身份属于 Monitor 部署配置。邮件 delivery 冻结收件人、主题和正文，dispatcher 在事务外按至少一次语义发送；测试发送不生成 event/outbox，`dead` 人工重投复用原 `delivery_id` 和内容。SMTP 未配置时 dispatcher 不启动，未投递 outbox 保持 `pending`，不能伪造通知结果。

**4. 统计分析**:
- 成功率趋势图
- 执行耗时分布图
- 失败原因分析
- 热点任务 Top 10

**5. 告警通知**:
- Webhook v1 支持 continuous 告警的打开、升级和恢复投递
- 每租户维护独立 Webhook 目标、订阅事件和签名 secret
- 支持指数退避、失败终态和投递审计
- 支持独立测试投递、`dead` 手动重投、目标删除和 System 操作审计
- 通用任务最近失败、最近超时和连续失败按同一告警生命周期评估
- 邮件通知 v1 使用平台统一 SMTP Relay 和独立邮件 outbox；站内通知后续实现

---

## 各模块任务类型

| 模块 | task_type | 说明 | 示例 |
|------|-----------|------|------|
| **Meta** | `scan` | 元数据扫描任务 | 扫描 PostgreSQL 数据库 |
| **Transfer** | `sync` | 数据同步任务 | 表导入、表导出或跨格式同步 |
| **Orchestrator** | `orchestration` | 编排任务执行 | 执行数据处理流水线 |
| **Develop** | `query` | 查询执行任务 | SQL 查询执行 |
| | `workflow` | 工作流执行任务 | 执行空间分析工作流 |
| | `script` | 脚本执行任务 | 执行命令式脚本；当前可由 Jupyter Notebook runtime 承载 |
| **Manager** | `vector_tile_cache_generation` | 瓦片缓存生成任务 | 为空间数据生成瓦片缓存 |
| | `vector_materialized_view_generation` | 矢量物化视图任务 | 为 PostGIS 空间数据创建或刷新 3857 矢量物化视图目标 |
| | `embedding` | 向量化任务 | 对对象存储文件进行向量化 |
| **Quality** | `check` | 数据质量检查任务 | 执行质量规则检查 |
| **Graph** | `kg_build` | 知识图谱构建任务 | 构建知识图谱 |

---

## 相关文档

- [返回核心概念关系图](addp核心概念关系图.md)
- [ADDP 任务编排体系图](addp任务编排体系图.md)
- [ADDP 任务体系规范](../spec/addp任务体系规范.md)
- [Monitor 模块实施报告](../../monitor/docs/Monitor模块实施报告.md)

---

**文档版本**: v1.0
**创建日期**: 2026-02-16
**作者**: ADDP 开发团队
