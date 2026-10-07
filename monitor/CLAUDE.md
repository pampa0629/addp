# Monitor 模块说明

## 模块定位

Monitor 模块是 ADDP 的统一执行监控中心，负责查询和展示各模块写入 `common.task_executions` 的任务执行记录、统计趋势和模块健康状态，同时拥有独立的 Platform 日志链路健康与告警域。

平台运行观测扩展已于 2026-10-04 确认，分批实施：首期覆盖 ADDP 纳管节点、运行实例和明确纳管引擎，指标/日志/追踪按需部署，观测设施故障不阻断业务或现有执行查询。System 维护节点及已有对象身份；Monitor 拥有监测目标、策略、资源查询与告警，不复制实例租约、不直接读节点文件。当前已完成集中日志可选部署/启动隔离，可选 Prometheus 中心模板已接入自身采集和节点 HTTP SD；正式节点来源与真实控制面链路已完成 Hosted Linux VM 首次 T4 验收，生产纳管覆盖仍须实际部署及独立验收。System 节点台账、实例首次节点声明与当前绑定裁决、节点管理前端已完成；System 提供固定 Monitor Platform Service Client 专用的有界当前身份投影（`/runtime/observability-identities`，`system.observability_identity.read`），Common 提供唯一共享 DTO 与服务 Client。Monitor 不直接查询 System 私有表，也不能用后台投影替代用户详情授权。节点监测目标后端 CRUD、原子预算和固定 Prometheus 身份的认证 HTTP SD 已接入；目标管理前端已接入 Console，应用埋点尚未实施；节点基础资源查询首批已接入（见文末范围），统一告警域收敛仍待明确确认。详细设计、依赖矩阵、告警领域收敛准入和验收清单见 [平台运行监控与可观测性设计](../docs/next/ADDP平台运行监控与可观测性设计.md)。开发前先读该契约，不把设计状态写成现有能力，不为观测后端增加整体 Ready 强依赖。

节点来源准入和发现投影内核位于 `backend/internal/metricsdiscovery/`，由 Monitor 唯一维护。已实现受控 CIDR/端口、固定 HTTPS 指标路径、每次发现重新解析 DNS 并固定单一 IP、来源 mTLS 准入、节点当前身份筛选和端点预留预算；只支持 node_exporter 主机来源与通过既定 mTLS 入口的 cAdvisor 节点容器来源。目标持久化、公开 HTTP SD 和独立 Prometheus OAuth 身份已消费此内核：管理请求转发当前 User Token，由 System 对节点再次裁决；目标写事务串行化预算和 CAS，发现读取当前身份而不复制 System 台账。启用目标必须通过 mTLS 准入，停用配置保存和撤除采集意图不依赖指标设施或来源在线。Prometheus 生产模板和正式节点来源已完成真实 Hosted T4，包含权限隔离、发现版本、真实采样及故障恢复；资源查询的生效状态查询、后续指标与生产纳管覆盖仍须交付，不能以一次测试部署宣称生产覆盖。T1 归 `make test-go`，目标事务与 IAM/OAuth 使用现有 PostgreSQL T2 自动发现；设计第 10.15–10.20 节记录边界与接线门禁。

## 技术栈与端口

Linux node_exporter 的独立受控部署由 `scripts/infra/node-metrics.py` 拥有，中心或业务启动不会自动调用。生产与独占 T2 复用同一固定镜像、采集器白名单和原生 mTLS 模板；T2 不挂载宿主根、不使用 host 命名空间；手工 Hosted T4 `platform-node-metrics` 已于 2026-10-06 在 Linux VM 首次通过（Run 37463003308），生产宿主权限/物理身份绑定的完整 T5 仍待取得；平台/租户越权、原生发现、生效采样和退出清理由 `make test-node-metrics-online-runner` 及 System Online fixture PostgreSQL 门禁先行覆盖。具体边界见设计第 10.18–10.20 节和 Infra README；不把 exporter 成功响应视为物理节点绑定证据。

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
- 独立访问模块根路径时，AuthContext 加载后依次选择可进入的平台节点资源、平台监测目标、仪表盘、执行记录、告警、通知页面；均不可进入时显示无权限。登录后返回根路径也按此顺序选择，显式页面地址保持原样。
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
- 持久表为 `monitor.log_pipeline_policy/log_pipeline_nodes`、`log_observer_boots`、`platform_log_incidents/events/destinations/deliveries`。单一活动告警按节点、信号及有证据的实例去重，各信号固定严重级别、独立恢复，事件和通知订阅仅为 `opened/resolved`，与通知 outbox 同事务；未知事实不能恢复告警，首次累计计数只建立基线。
- Webhook/SMTP 复用中立发送接口和统一重试算法，至少一次投递；确认/抑制不改写业务实例状态。读取与管理通过 Monitor 服务账号向 System 独立 Platform 审计接口追加最小操作事实。
- 最终失败的平台通知通过单条 `POST /platform/log-notification-deliveries/{id}/retry` 重新入队，仅 Platform User 与 `monitor.log_notification.update` 可操作。保留原投递/事件/正文和发生时间，使用原目标当前配置；目标版本与预期人工次数拒绝并发或延迟重复请求，抑制不能绕过。累计次数保留，本轮上限和退避从 `retry_base_attempt_count` 重新计算；历史恢复告警允许补发，界面必须明确提示。
- 门禁：`make test-go`、`make test-monitor-postgres`、`make test-system-iam-postgres`、`make test-system-runtime-log`、`make test-system-frontend` 和 `make test-platform`；数据库及故障注入必须使用标准 disposable 入口。完整设计见 `docs/next/ADDP模块服务运行日志设计.md` 第十一节。

平台日志通知支持明确的 `wecom` 渠道：固定官方端点、通过既有凭据接口写入完整机器人地址并仅加密保存 key；复用受控 HTTP 客户端，HTTP 2xx 与整数 errcode=0 同时成立才算发送成功。平台 outbox、重试及安全诊断保持同一主路径，租户通知不扩展。Go/PostgreSQL 回归由现有 Monitor owner gate 自动发现，System 配置交互由既有前端门禁覆盖。

## 节点监测目标与认证发现

- Platform User 管理 `/platform/monitoring_targets`；列表/详情/创建/更新/删除分别消费 `monitor.monitoring_target.read/create/update/delete`。非委托 User Access Token 支持第一方和 OAuth，引用节点始终转给 System 当前裁决；默认仅平台系统管理员获得管理权限。
- 固定 `addp-prometheus` 非委托 Platform Service Access Token 读取 `/platform/metrics_discovery`，仅授予 `monitor.metrics_discovery.read`，不授予目标管理、后台身份投影或 Tenant Runtime 角色。System 连续迁移 000191 发布身份和权限；可选凭据关闭时撤销授权版本和旧令牌。
- 唯一持久表 `monitor.monitoring_targets` 保存 UUID、节点引用、监测类别、来源、启用意图和版本。主体/类别不可改写，更新及删除执行 CAS；全局非阻塞事务锁保证 1,000 保存目标、九个启用端点的预算不会被并发突破。来源异常不阻止停用或删除。
- `ADDP_OBSERVABILITY_METRICS_ENABLED` 严格选择；`MONITOR_METRICS_ALLOWED_CIDRS/PORTS` 是明确网络准入集合，CA 和独立客户端证书由 `MONITOR_METRICS_CA_FILE/CLIENT_CERT_FILE/CLIENT_KEY_FILE` 注入。关闭或缺配置不阻断 Monitor 原有 Ready；相应采集请求返回 disabled/unconfigured。`PROMETHEUS_SERVICE_CLIENT_SECRET` 只配置 System 的独立发现身份，不能借用日志或业务客户端凭据。
- `GET /platform/metrics_discovery` 返回原生 HTTP SD 数组；成功空列表与失败分开，不接受分页、调用者标签、query 或正文。保存版本不是实际 Prometheus 生效回执，版本只进入内部发现元数据。
- 现有 `make test-monitor-postgres` 自动运行目标并发预算和 CAS 回归。`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package iam --test prometheus-credential"` 验证可选身份及撤销；`--package api --test oauth-client-credentials` 使用真实 System OAuth Handler 验证最小平台身份、Tenant 拒绝及旧令牌撤销。两者是既有标准入口的精确选择，CI 仍使用完整门禁，不缩减范围。
- 指标中心生产模板已接入原生 OAuth2/HTTP SD；控制面 HTTPS origin、独立来源证书和 Secret 文件由部署注入，标准 Infra 预检生成唯一配置。`make test-monitor-metrics` 验证真实 Prometheus 的认证发现、令牌续取、身份标签清理、失败保留旧目标及成功空列表移除；控制面为受控协议夹具，不能替代真实 System/Monitor 部署全链路验收。正式节点来源与控制面已取得 Hosted T4；生产纳管覆盖、cAdvisor 及面向用户的生效状态仍待后续验收，具体输入见 `scripts/infra/README.md`。


## Platform 节点基础资源查询

- 唯一读取接口 `/platform/resource_observations`、`/platform/resource_trends` 使用 `monitor.resource_observation.read`；System 连续迁移 000193 仅给平台系统管理员发布该权限，采集服务与 Tenant 身份无此能力。当前请求内转发 User Token，由 System 裁决节点；读取当前启用目标及受控解析端点，节点/目标停用返回 `not_connected`，不查历史作为当前值。
- `internal/resourcequery/` 唯一维护 9 个标量目录项、PromQL、timestamp 证据、步长与响应预算。CPU 核数、一分钟 CPU 忙碌率、内存总量/可用量/使用率、1/5/15 分钟负载和运行时长支持即时及趋势；负载不是 CPU 利用率。CPU 忙碌率须证明一分钟完整窗口及核集合/启动时间一致，缺样本或计数器重置返回空值；文件系统容量与挂载趋势已沿同一 API 实施（见下文），磁盘/网络速率尚未发布。每个网格点明确评估/采样时间、单位、状态与空值，缺失不变成零，过期值不算当前有效值。
- `/settings/resource-query-policy` 沿用模块级 `monitor.configuration.read/update`，仅 Platform User，唯一 `monitor.resource_query_policy` 单例保存 CAS 版本与完整预算。每个查询读一次已提交预算，对新请求热生效；没有环境回退或重启要求。标准平台审计记录安全结果与保存版本，不记录 PromQL、Token 或内部地址。
- mTLS 查询部署输入独立于节点准入和 collector；关闭或不完整配置不阻断业务。生产只通过标准生命周期的 `metrics-query.yml` 挂载，不增加必需 Infra 依赖。
- Go T1、Monitor PostgreSQL 及 System IAM Migration T2 沿既有标准入口自动发现；同一个 `platform-node-metrics` suite 已在 `794450a30` 取得扩展后的真实 Hosted T4（run 37485747576），覆盖八项即时/趋势查询、预算 CAS/热生效、权限隔离、中心故障与新样本恢复、停用后不复用历史及独立清理零残留；具体证据见设计 10.22 节。用户资源查询页面现已接入 Console（见下文）；更多指标和生产 T5 仍须分批补齐，该次 Linux VM 通过不代表生产纳管覆盖。

## 平台监测目标管理前端

- Monitor 唯一拥有 `/monitoring-targets`、`/monitoring-targets/new`、`/monitoring-targets/:id`，Console 使用 `/monitor` 前缀集成真实 iframe；分页与详情由公开 URL 恢复，端点草稿不进入 URL。
- 入口只接受非委托 Platform User，需同时具备 `monitor.monitoring_target.read` 和 `platform.host_node.read`；新建、修改、删除分别消费对应独立 Permission。Context、主体、委托或权限变化同步取消请求并清空列表与草稿，迟到响应不得覆盖新范围。
- 主体/类别创建后只读，更新/停用与删除携带保存版本。CAS 冲突保留草稿并停止写入，须明确重新加载；启用标记仅表示保存配置，不表示实际采集健康。节点台账导航使用共享 Console bridge。
- 标准门禁为 `make test-monitor-frontend`、`make test-console-frontend`、`make test-common-frontend` 与 `make test-frontend-ci-registration`。Console 浏览器生命周期自建 Monitor Vite 夹具，CI 安装其锁定依赖；Monitor 前端变更扩散至 Console，后端变更不扩散。无需重启个人开发环境。

## 平台节点资源前端

- Monitor 唯一拥有 `/node-resources` 和 `/node-resources/:node_id`，Console 集成菜单、搜索与真实 iframe；列表 search/page/page_size、详情 range/metric/refresh 可恢复，默认值省略。System 台账提供节点列表和详情，选中后读取九项即时资源与单项趋势，不批量扇出查询。
- 入口仅接受非委托 Platform User，同时具备 `platform.host_node.read` 和 `monitor.resource_observation.read`；上下文、主体、权限或节点变化同步取消请求并清空旧观测。关闭/未配置/不可用/超时/预算及并发超限分别提示，失败不保留旧观测。
- 详情默认完成一轮后等待 15 秒自动刷新，可选 10/30/60 秒或关闭，选择通过 URL 恢复；列表不扇出查询。隐藏时取消请求并暂停，可见且开启时立即刷新；身份/路由变化及卸载清理，401/403/404 停止重试，手工刷新重新裁决。时间明确标注；趋势窗口以本轮即时响应的服务端 end 为准。valid 零值有效，stale/no_data/not_connected 不展示为当前数值，曲线保留断点，明细展示原状态与采样/评估时间。不把 Load Average 解释为 CPU 利用率。
- 图表及数字/时间格式复用 Common Chart 和 Basic 的唯一实现；共享折线最多 1,000 点，null 保留为空点。既有 Workbench 消费者与 Monitor/Console/共享前端标准门禁共同验证，新增测试沿既有自动发现和 CI 登记；确定性页面 T3 不代替真实后端或页面 T4。

- 同一 `platform-node-metrics` Hosted Linux VM 已于 2026-10-07 完成真实 Console/Monitor 节点资源页面 T4（Run 37563564755，代码 `b3a64756c`）：正式密码/MFA、同一非委托 Platform User、八项资源/趋势、单 iframe 历史及刷新恢复、安全管理员入口拒绝与无业务请求、预算及监测故障隔离/恢复、停用后历史排除、退出零残留通过。沿用既有临时身份例外，未增加权限或生产角色；T3 仍独立计量，生产 T5、更多指标及自动刷新未在该次验收范围。细节与失败诊断见设计 10.25。

节点资源详情自动刷新已在实现提交 `1ce34d770` 完成真实 Hosted T4（Run 37566181124）：自然 15 秒计时、服务端 end 前进、URL 不变、关闭后刷新恢复及 16 秒无定时请求通过；同提交 Platform CI 全部通过，退出零残留。隐藏/恢复、慢请求、撤权和卸载由 T3 单独覆盖；完整证据见设计 10.26，生产 T5 与更多指标仍待分批实施。

CPU 忙碌率及趋势新增固定 `node.cpu.busy_percent`（percent，60 秒窗口），本地 Go/真实 Prometheus/PostgreSQL/前端门禁及 15 个窗口/核集合场景已通过。九项读取范围经用户确认，沿用原隔离身份与权限边界；实现提交 `5bb4173cf` 的完整 Hosted T4（Run 37573777193）及 Platform CI（Run 37573754535）已通过，真实页面、两次同一采集身份 grant、节点/目标恢复和停用历史排除、独立清理零残留均已复核。初始全空响应的有界等待实际收敛，CPU 单独预热/恢复零值由 T3 单独证明；26 项 runner 回归覆盖凭据寿命和拒绝语义。首轮凭据过期与旧 CI 异步断言失败不计为通过；完整证据见设计 10.27。文件系统容量的后续完成证据见下文，IO 与生产 T5 仍待后续批次，默认 test-changed 跨 owner 数据库预检失败未改记为成功。

文件系统容量与趋势沿设计 10.28 的已确认口径实施：新增五个 `node.filesystem.*` 目录键，使用率为 `used/(used+available)`；系列必填受控 `dimensions`，标量为 `{}`，挂载只含设备、挂载点和类型。既有资源 API 接受完整三项精确选择器，不开放任意标签或 PromQL。多挂载与历史挂载的序列上界参与步长和总点数预算，超限返回 422。详情依次读取九项概览、五项挂载表与一项趋势，挂载错误不清除有效概览；精确挂载与刷新选项由同一公开 URL 恢复。固定 node_exporter 排除规则显式写入唯一部署配置；不按挂载行求物理容量总量，不修改 IAM 或 Tenant 归属。最新本地 `test-changed`、Go、真实指标 T2、PostgreSQL、前端和 27 项 Online runner 回归已通过；实现 `08556c553` 的扩展 Hosted T4（Run 37602308231）、同实现 Platform CI（Run 37602281641）及 Release/T2（Run 37602281771）均已通过。真实六个挂载/30 条容量序列、同次采样公式、精确挂载 URL/趋势、恢复新样本与停用后历史排除已复核，退出零残留。证据按设计 10.28 分层计量，inode、IO、全局容量和生产 T5 尚未完成。


文件系统 inode 沿设计 10.29 实施：四个 `node.filesystem.inodes_*` 固定键，共用设备/挂载点/类型及原精确选择器。总数、空闲数来自既有 filesystem collector，同次采样派生已用和使用率；零总数、整数范围/精度异常、缺失及 statfs 错误保留已知维度与空值。字节容量与 inode 独立查询、独立提示，唯一挂载表组合两组点事实；不按挂载汇总物理数量。目录共 18 项，12 项请求预算不放宽；页面顺序读取九项概览、五项字节容量、四项 inode 及单项趋势。锁定 promtool 与既有 T2 探针增加 inode 情景，真实 Hosted T4 沿同一 suite 扩展 inode 读取与 URL 恢复，分层证据见设计 10.29，尚未真实运行的项目不计为通过。
