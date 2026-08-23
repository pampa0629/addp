# Monitor 模块说明

## 模块定位

Monitor 模块是 ADDP 的统一执行监控中心，负责查询和展示各模块写入 `common.task_executions` 的任务执行记录、统计趋势和模块健康状态。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8100`，环境变量 `MONITOR_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus + ECharts，开发端口 `5179`，启动脚本环境变量 `MONITOR_FE_PORT`。
- 存储：PostgreSQL `common.task_executions`，Redis 用于认证缓存。

## 重要目录

```text
monitor/
├── authorization/
│   └── permissions.yaml       # Monitor Permission Manifest，发布期聚合事实源
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # executions、statistics、modules health API
│   ├── internal/service/      # 查询、统计、健康检查服务
│   └── docs/                  # Swagger 产物
├── frontend/src/
│   ├── views/                 # Dashboard、ExecutionList
│   ├── components/            # StatisticsCard、ExecutionTable、ModuleStatusBadge
│   └── api/monitor.js
└── docs/Monitor模块实施报告.md
```

## API 与数据

- Monitor 是 `monitor.execution.*`、`monitor.health.read` 和 `monitor.statistics.*` 的 Permission owner；定义只存在于 `authorization/permissions.yaml`，通过 `common/authorization` 发布期聚合，不在服务启动时动态注册。
- 路由前缀：`/api/v1/monitor`。
- 主要接口：`GET /executions`、`GET /executions/:id`、`GET /executions/stats`、`GET /executions/trend`、`GET /executions/runtime-metrics`、`GET /runtime-instances/health`、`GET /alerts`、`GET /alert-rule-targets`、`GET/POST/PATCH/DELETE /alert-rules`、`GET/POST/PATCH/DELETE /webhook-destinations`、`GET /webhook-deliveries`、`GET/POST/PATCH/DELETE /email-destinations`、`GET /email-deliveries`、`GET /providers/health`、`GET /providers/:module/health`。
- provider health 从 System 读取模块定义中的 TaskProvider 声明及当前有效 Backend 实例池，逐实例复用模块 `/health` 与标准 `GET /tasks?task_type=` 做无副作用探活，再聚合 Provider 状态；Monitor 不挑选单一固定地址、不复制 capabilities、不修复模块声明、不读取 owner 私有表。
- Transfer continuous 的 lag、retention health 与 checkpoint health 来自 `common.task_executions.metadata.continuous.diagnostics`；数据库 CDC 的源恢复窗口和事务观测来自 `metadata.continuous.capture`。Monitor 列表和详情只展示 owner 已写入的安全事实，并可无状态派生 recovery/retention/checkpoint/source recovery/source transaction availability 观测信号，不直连业务 Kafka 或源数据库，不读取 `transfer.sync_states`、`transfer.runtime_leases` 或 `transfer.capture_resources`。事务活跃数、持续时间和 Undo 用量没有平台阈值，不自行派生告警。观测信号不是持久化告警事件或通知状态。
- Monitor 拥有 `monitor.alert_incidents` 告警生命周期；每个 continuous 任务只消费最新根 execution 的公共 metadata。最新 execution 为 `pending|running` 时派生运行观测信号；仅当这个最新 execution 自身为 `failed` 且 `metadata.continuous.schema_change.status=pending` 时派生数据库 CDC schema blocked 信号。更晚的 execution 会覆盖历史失败事实，使旧告警自动恢复。确认和抑制不得改写 owner 事实，同一告警身份同时最多一个未恢复事件。Monitor 不读取 `transfer.schema_change_requests`。
- Webhook v1 使用 `monitor.alert_events` 保存不可变 `opened|escalated|resolved` 生命周期事件，使用 `monitor.webhook_deliveries` 作为 per-destination outbox。incident/event/delivery 同事务写入，dispatcher 在事务外按至少一次语义发送；签名 secret 使用平台 `ENCRYPTION_KEY` 加密且不通过 API 返回。
- Webhook 目标与投递审计接口位于 `/webhook-destinations` 和 `/webhook-deliveries`；目标和投递均按 JWT tenant 隔离。Webhook 不是 System Engine，不使用 Kafka，也不读取 owner 私有表。
- Webhook 目标测试使用独立 `monitor.webhook.test/v1` schema，不创建告警 event/outbox；`dead` 手动重投复用原 `delivery_id`，按 `retry_base_attempt_count` 开启新尝试周期并保留累计次数。删除目标取消未领取投递但保留历史 delivery，所有写操作复用 System Audit Middleware。
- 邮件通知消费同一条 `monitor.alert_events`，但使用独立 `monitor.email_destinations` 和 `monitor.email_deliveries`。租户只配置收件人和订阅事件，SMTP Relay、强制 TLS、认证和发件身份属于部署配置；SMTP host 为空时 dispatcher 不启动，pending outbox 保留。
- 邮件目标测试不创建 event/outbox；`dead` 手动重投复用原 `delivery_id`、主题和正文，使用目标当前收件人。邮件写操作同样复用 System Audit Middleware，SMTP password 不进入 API。
- 通用告警规则由 `monitor.alert_rules` 精确绑定 `module + task_type + source_task_id`，第一版只支持最近失败、最近超时和连续失败；只读取根 execution 公共事实，跳过 ad-hoc、子 execution 和 Transfer continuous session。
- `monitor.notification_routes` 显式绑定通用规则与 Webhook/邮件目标。无路由仍保留 incident/event，但不生成外部 delivery；规则更新、停用或删除必须先恢复其活动 incident。
- 所有 evaluator 先收集 active signal，再由单一 reconciler 统一处理生命周期。任何 evaluator 查询失败不得恢复其拥有的现有 incident。
- 通知前端唯一入口为 `/notifications`，通过 Webhook/邮件页签展示两个渠道；不保留旧 `/webhooks` 页面路由。
- 执行记录字段以 `common/execution/task_execution.go` 为准；新增模块写执行记录时应复用 `common/execution/repository.go` 和 `common/execution.EnsureStore`。

## 前端公开路由

- Monitor 前端遵守 `docs/spec/addp前端路由与可恢复状态规范.md`，模块内公开导航统一通过 `src/utils/moduleNavigation.js`。
- 执行详情 canonical URL 固定为 `/monitor/executions?execution_id={execution_uuid}`；从列表打开详情使用 `push`，关闭详情清除 `execution_id` 使用 `replace`，浏览器前进/后退必须同步打开或关闭详情。
- 告警页默认 `incidents` Tab 和通知页默认 `webhook` Tab 从 URL 省略；`rules`、`email` 等非默认稳定 Tab 使用 `tab` query 并以 `replace` 更新。

## 开发与验证

```bash
bash scripts/dev/start.sh -monitor
bash scripts/dev/restart.sh -monitor
curl http://localhost:8100/health
```

API 或路由变更后运行：

```bash
bash scripts/swagger/gen-swagger.sh monitor
bash scripts/swagger/check-route-coverage.sh monitor
```

## 相关文档

- `monitor/docs/Monitor模块实施报告.md`
- `docs/concepts/addp监控与执行体系图.md`
- `docs/spec/addp任务体系规范.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp-Swagger集成指南.md`
