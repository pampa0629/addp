# Monitor 模块说明

## 模块定位

Monitor 模块是 ADDP 的统一执行监控中心，负责查询和展示各模块写入 `common.task_executions` 的任务执行记录、统计趋势和模块健康状态，同时拥有独立的 Platform 日志链路健康与告警域。

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
- provider health 从 System 读取模块定义中的 TaskProvider 声明及当前有效 Backend 实例池，逐实例复用模块 `/health/ready` 与标准 `GET /tasks?task_type=` 做无副作用探活，再聚合 Provider 状态；Monitor 不挑选单一固定地址、不复制 capabilities、不修复模块声明、不读取 owner 私有表。
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
- Monitor 的执行列表、详情、父子树、统计及关联告警先通过同步 BFF 向各 Owner 请求 `execution-read-scope`。Owner 重新认证 User，按本模块任务读取权限裁决；即时执行仅限触发人，读取历史不授予业务结果权限。无法裁决时返回 503。诊断 GET 以不含请求参数和凭据的最小事实写入独立 System 操作审计。
- 执行记录字段以 `common/execution/task_execution.go` 为准；新增模块写执行记录时应复用 `common/execution/repository.go` 和 `common/execution.EnsureStore`。
- 后台运行健康统一读取 `common.background_runtime_heartbeats`：独立有界执行进程使用 `execution_worker`，Backend 内嵌有界执行监督器使用 `execution_supervisor`，持续运行进程使用 `continuous_worker`，通知投递使用 `dispatcher`。`capacity/active_count` 只表示固定槽位及当前占用，不授予 lease，也不触发自动扩缩容。
- 执行详情使用 `common-frontend/basic` 的唯一 `lineage_facts` 展示归一能力，按“输入资源 / 输出产物”展示 owner 已写入的真实读写事实。可解析的业务输入通过共享 Manager Data Explorer 路由打开；Monitor 不解析查询语句、不请求 Meta 补齐、不将 `addp-infra://` 平台内部产物当作业务数据项或跳转目标，也不据此推断产物生命周期。执行步骤复用共享 `ExecutionSteps`，只展示已记录步骤的状态、时间、耗时及脱敏错误；没有步骤历史时明确说明，不从当前任务定义推测；同一 execution 重试后，旧步骤未标注 attempt 时显示证据不足提示。Monitor 不再返回或展示任意 metadata、执行配置或结果正文。

## 前端公开路由

父执行终态下仍为 `pending|running` 的步骤只表示最后记录。共享 `ExecutionSteps` 必须接收父执行状态，标注历史状态/阶段并提示核对子执行，不把旧 `waiting` 解释为当前编排仍在等待；展示及中英文回归由 `make test-common-frontend` 和 `make test-monitor-frontend` 覆盖，既有前端 CI 自动发现。

详情刷新依据整棵已授权可见树：父执行结束但任一可见子执行仍运行时继续刷新，全部可见执行达到终态才停止；不根据私有步骤引用补查隐藏节点。每次刷新复用 Owner 裁决；返回 403/404 时清空旧树、步骤和事件并显示重新加载入口。异步响应必须属于当前详情请求，不能覆盖关闭后重新打开的页面。

- Monitor 前端遵守 `docs/spec/addp前端路由与可恢复状态规范.md`，模块内公开导航统一通过 `src/utils/moduleNavigation.js`。
- 独立访问模块根路径时，AuthContext 加载后依次选择可进入的仪表盘、执行记录、告警、通知页面；均不可进入时显示无权限。登录后返回根路径也按此顺序选择，显式页面地址保持原样。
- 执行详情 canonical URL 固定为 `/monitor/executions?execution_id={execution_uuid}`；从列表打开详情使用 `push`，关闭详情清除 `execution_id` 使用 `replace`，浏览器前进/后退必须同步打开或关闭详情。
- 告警页默认 `incidents` Tab 和通知页默认 `webhook` Tab 从 URL 省略；`rules`、`email` 等非默认稳定 Tab 使用 `tab` query 并以 `replace` 更新。

## 开发与验证

```bash
bash scripts/dev/start.sh -monitor
bash scripts/dev/restart.sh -monitor
curl http://localhost:8100/health/ready
```

第一期诊断门禁（T0–T3）：先用 `bash scripts/infra/status.sh` 核实 PostgreSQL 实际映射，再配置 `ADDP_TEST_POSTGRES_PORT`、`ADDP_TEST_POSTGRES_PASSWORD`，运行 `make test-module MODULE=monitor`。新 PostgreSQL 门禁为 `make test-monitor-postgres`，浏览器回归已包含在 `make test-monitor-frontend`；所有新增入口同步登记在 CI。真实 Gateway、System、Owner、Worker 链路属于 T4，未执行时不能算作通过。

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

## 平台日志链路

- Infra 使用独立 `addp-log-observer` Service Principal 上报有界安全观测；Monitor 只接收绑定节点的非委托 Platform 服务令牌。不上传日志正文，不访问接收器宿主机文件，不使用虚构租户或任务身份。
- 唯一路由位于 `/api/v1/monitor/platform/log-*`。`monitor.log_pipeline.read/update`、`monitor.log_notification.read/update` 只用于 Platform；`monitor.log_observation.create` 仅授予观测器。System 模块管理提供页面，正文读取仍由 `platform.module_log.read` 独立控制。
- 持久表为 `monitor.log_pipeline_policy/log_pipeline_nodes`、`log_observer_boots`、`platform_log_incidents/events/destinations/deliveries`。单一活动告警按节点、信号及有证据的实例去重，开告警/升级/恢复与通知 outbox 同事务；未知事实不能恢复告警，首次累计计数只建立基线。
- Webhook/SMTP 复用中立发送接口和统一重试算法，至少一次投递；确认/抑制不改写业务实例状态。读取与管理通过 Monitor 服务账号向 System 独立 Platform 审计接口追加最小操作事实。
- 最终失败的平台通知通过单条 `POST /platform/log-notification-deliveries/{id}/retry` 重新入队，仅 Platform User 与 `monitor.log_notification.update` 可操作。保留原投递/事件/正文和发生时间，使用原目标当前配置；目标版本与预期人工次数拒绝并发或延迟重复请求，抑制不能绕过。累计次数保留，本轮上限和退避从 `retry_base_attempt_count` 重新计算；历史恢复告警允许补发，界面必须明确提示。
- 门禁：`make test-go`、`make test-monitor-postgres`、`make test-system-iam-postgres`、`make test-system-runtime-log`、`make test-system-frontend` 和 `make test-platform`；数据库及故障注入必须使用标准 disposable 入口。完整设计见 `docs/next/ADDP模块服务运行日志设计.md` 第十一节。

平台日志通知支持明确的 `wecom` 渠道：固定官方端点、通过既有凭据接口写入完整机器人地址并仅加密保存 key；复用受控 HTTP 客户端，HTTP 2xx 与整数 errcode=0 同时成立才算发送成功。平台 outbox、重试及安全诊断保持同一主路径，租户通知不扩展。Go/PostgreSQL 回归由现有 Monitor owner gate 自动发现，System 配置交互由既有前端门禁覆盖。
