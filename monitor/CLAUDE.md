# Monitor 模块说明

## 模块定位

Monitor 模块是 ADDP 的统一运行监控中心，负责查询和展示各模块写入 `common.task_executions` 的任务执行记录、统计趋势和模块健康状态，同时拥有独立的 Platform 运行观测与告警域。

平台运行观测扩展已于 2026-10-04 确认，分批实施：首期覆盖 ADDP 纳管节点、运行实例和明确纳管引擎，指标/日志/追踪按需部署，观测设施故障不阻断业务或现有执行查询。System 维护节点及已有对象身份；Monitor 拥有监测目标、策略、资源查询与告警，不复制实例租约、不直接读节点文件。当前已完成集中日志可选部署/启动隔离，可选 Prometheus 中心模板已接入自身采集和节点 HTTP SD；正式节点来源与真实控制面链路已完成 Hosted Linux VM 首次 T4 验收，生产纳管覆盖仍须实际部署及独立验收。System 平台主机管理、实例首次节点声明与当前绑定裁决、节点管理前端已完成；System 提供固定 Monitor Platform Service Client 专用的有界当前身份投影（`/runtime/observability-identities`，`system.observability_identity.read`），Common 提供唯一共享 DTO 与服务 Client。Monitor 不直接查询 System 私有表，也不能用后台投影替代用户详情授权。节点监测目标后端 CRUD、原子预算和固定 Prometheus 身份的认证 HTTP SD 已接入；目标管理前端已接入 Console，macOS/Linux Go 自身进程指标与服务监控已接入，真实 Hosted 实例链路及默认服务范围已验收（设计 10.49、10.51）；节点基础资源查询首批已接入（见文末范围），统一告警域收敛已于 2026-10-15 确认。平台事件与通知已收敛到 Monitor 唯一入口；主机 CPU/内存规则的独立权限、配置 CRUD 和后台评估已实现并通过本地分层验证，个人环境已验证规则创建、读取、修改、删除、显式启停及真实采样的正常评估，临时规则已清理；受控指标源的触发、缺样保护、恢复与签名 Webhook 联合 T2 已通过（设计 10.72），正式身份与 Gateway 的告警闭环 T4 仍待验收；此前资源规则 CI 证据见设计 10.71。详细设计、依赖矩阵、告警领域收敛准入和验收清单见 [平台运行监控与可观测性设计](../docs/next/ADDP平台运行监控与可观测性设计.md)。开发前先读该契约，不把设计状态写成现有能力，不为观测后端增加整体 Ready 强依赖。

节点来源准入和发现投影内核位于 `backend/internal/metricsdiscovery/`，由 Monitor 唯一维护。已实现受控 CIDR/端口、固定 HTTPS 指标路径、每次发现重新解析 DNS 并固定单一 IP、来源 mTLS 准入、节点当前身份筛选和端点预留预算；只支持 node_exporter 主机来源与通过既定 mTLS 入口的 cAdvisor 节点容器来源。目标持久化、公开 HTTP SD 和独立 Prometheus OAuth 身份已消费此内核：管理请求转发当前 User Token，由 System 对节点再次裁决；目标写事务串行化预算和 CAS，发现读取当前身份而不复制 System 台账。启用目标必须通过 mTLS 准入，停用配置保存和撤除采集意图不依赖指标设施或来源在线。Prometheus 生产模板和正式节点来源已完成真实 Hosted T4，包含权限隔离、发现版本、真实采样及故障恢复；资源查询的生效状态查询、后续指标与生产纳管覆盖仍须交付，不能以一次测试部署宣称生产覆盖。T1 归 `make test-go`，目标事务与 IAM/OAuth 使用现有 PostgreSQL T2 自动发现；设计第 10.15–10.20 节记录边界与接线门禁。

## 技术栈与端口

Linux node_exporter 的独立受控部署由 `scripts/infra/node-metrics.py` 拥有，中心或业务启动不会自动调用。同一入口支持原生 Linux 完整节点视图及 macOS Docker Desktop VM 的受限内核全局视图；Desktop 仅七采集器（含 diskstats），不发布文件系统或网卡/TCP 族，台账使用 `virtual` 且不能代表 Mac 本机或单个容器。生产与独占 T2 复用同一固定镜像、采集器白名单和原生 mTLS 模板；T2 不挂载宿主根、不使用 host 命名空间；手工 Hosted T4 `platform-node-metrics` 已于 2026-10-06 在 Linux VM 首次通过（Run 37463003308），生产宿主权限/物理身份绑定的完整 T5 仍待取得；平台/租户越权、原生发现、生效采样和退出清理由 `make test-node-metrics-online-runner` 及 System Online fixture PostgreSQL 门禁先行覆盖。具体边界见设计第 10.18–10.20 节和 Infra README；不把 exporter 成功响应视为物理节点绑定证据。

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

资源总览唯一入口 `/resource-overview`，通过 `GET /platform/resource_overview` 服务端完整评估当前 Platform User 的纳管主机集合（包含停用主机），复用 System 当前用户主机读取、现有摘要内核及统一预算。两项 TOP5 仅排序有效 CPU 一分钟忙碌率和内存使用率，分别展示有效与缺失覆盖；初始双指标预算最多 50 台，超过预算整份 422，不截页、跨页或补零。采集证据不表示业务健康；不汇总进程 RSS、容器配额、共享挂载或集群容量。页面复用共享十五秒刷新、取消及导航，到主机详情和有独立模块读取权限的服务页；单真实主机的 Hosted 38588001065（源码93b89ab33）已通过本批聚合、预算、刷新/导航、中心/来源/控制故障与停采恢复，退出零残留；同源码 Platform CI 34 项全部通过。六个受控 mTLS 来源的真实 Prometheus T2 已通过完整聚合、TOP5 同值/末项排序、独立指标缺失、有效零、目标停用排除与整份预算拒绝；复用原独占门禁且退出零残留，见设计 10.60。该 T2 不代表六台真实主机；实际多主机 TOP5、独立 Owner 撤权和生产 T5 尚未真实验收，不由上述结果推定。详细边界与门禁见设计 10.58–10.60。

服务监控 `/service-resources` 默认显示当前在线且已启用进程采集的实例；采集失败或暂无样本的行保留。`status` 默认 `up`、`process_metrics_declared` 默认 `true`，页面 URL 省略默认值；显式 `all` 仅用于页面表达不限制，对 System 请求省略该参数。“全部实例”与恢复默认范围保留模块/角色、重置分页；筛选由 System 在计数及分页前执行，不在浏览器裁剪当前页或按观测成功隐藏故障。新增范围交互和 Console 刷新由既有两模块前端门禁覆盖，设计见平台运行监控 10.51。

服务实例行内的 Worker 用途只读取 System 既有 `metadata.runtime_name`：`scan/sync/continuous_sync` 分别显示扫描、单次同步、持续同步任务进程；未知或缺失时仅显示正式角色，不从模块名、主机或 UUID 推断。role 筛选仍为 `worker`，用途不表示当前任务正在执行，也不替代采集状态；中英文及刷新由上述前端门禁覆盖，设计见 10.54。

父执行终态下仍为 `pending|running` 的步骤只表示最后记录。共享 `ExecutionSteps` 必须接收父执行状态，标注历史状态/阶段并提示核对子执行，不把旧 `waiting` 解释为当前编排仍在等待；展示及中英文回归由 `make test-common-frontend` 和 `make test-monitor-frontend` 覆盖，既有前端 CI 自动发现。

恢复上下文复用 execution 已保存的 `recovered_from_execution_id` 和 `recovery_attempt`，在通用详情摘要中展示来源 UUID 与恢复次数，不从当前任务定义补造历史。真实页面验收沿 `transfer-continuous-events` Hosted 部署，使用正式 Console 登录核对运行过程自动刷新、恢复历史及撤权后的清空；测试控制文件只留在当次私有临时目录，截图与报告不包含令牌、业务样本或完整接口响应。

过程事件请求或树刷新收到 401／403／404 时，Monitor 清空该次详情的概览、树、步骤、血缘和事件，并停止详情刷新；终态执行同样处理，不依赖运行中轮询。拒绝必须匹配当前详情请求代数及所选执行，关闭／重新打开后迟到的拒绝不得清除新授权详情。复用既有失败／重新加载界面和事件组件，不增加平台日志接口或共享组件的权限职责；`make test-monitor-frontend` 覆盖终态拒绝与迟到请求，真实撤权沿上述 Hosted suite 复验。

2026-10-13 提交 `bfc305953` 的 [Hosted T4 38329160596](https://github.com/pampa0629/addp/actions/runs/38329160596) 在真实页面验证了过程自动刷新、恢复来源与稳定失败码，随后在撤权后的 `old_content_cleared` 失败：事件行被清空，但终态概览／步骤未清空；新 Monitor-only 浏览器和后续 CDC 阶段未运行。suite 清理通过、残留为 0，不能据此把整个 T4 计为通过。已将事件拒绝绑定到 Monitor 详情清空；本地 `make test-monitor-frontend` 的 56 项单元、118 项浏览器及构建通过，包含三种拒绝及关闭后迟到事件的回归。首次 401 用例错误要求会话失效后仍显示重新加载按钮，改为验证既有会话失效导航且全部旧详情消失；没有放宽内容清空断言。真实撤权复验仍待运行。

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

主机详情提供“相关告警”入口，沿当前主机 ID 与资源类别打开平台告警列表。资源告警可查看保存的触发条件、窗口数值和采样区间；有独立主机读取权限时解析当前名称，有资源观测权限且主机仍可读时才能跳转到主机资源。历史趋势通过原主机详情路由的固定 `end` 展示，停止自动刷新、校验响应时段、保留缺样，并可返回实时趋势；当前资源卡片仍表示当前值。规则与采集配置列表共用同一去重且最多四并发的主机引用解析，身份或请求失效后丢弃结果。本批不改 API、权限及告警状态，详细契约和验证见运行监控设计 10.73。

## 平台日志链路

- Infra 使用独立 `addp-log-observer` Service Principal 上报有界安全观测；Monitor 只接收绑定节点的非委托 Platform 服务令牌。不上传日志正文，不访问接收器宿主机文件，不使用虚构租户或任务身份。
- 日志专用观测、策略和健康诊断位于 `/api/v1/monitor/platform/log-*`，沿 `monitor.log_pipeline.read/update` 与仅授予观测器的 `monitor.log_observation.create`。通用事件及通知管理位于 `/platform/alerts` 与 `/platform/alert-notification-*`，仅接受非委托 Platform User，分别使用 `monitor.platform_alert.read/update` 与 `monitor.platform_alert_notification.read/update`；写操作同时核验对应读取权限。旧通知权限已停用、旧管理路由删除，完整页面唯一归 Monitor `/platform-alerts`；System 保留日志诊断、来源策略、活动数量及上下文跳转，正文读取仍由 `platform.module_log.read` 独立控制。
- 持久表为 `monitor.log_pipeline_policy/log_pipeline_nodes`、`log_observer_boots`、`platform_alert_incidents/events/destinations/deliveries`。单一活动告警按来源类别、主体类型、主体 ID 和信号去重；日志实例信号保留有证据的实例身份，各信号固定严重级别、独立恢复，事件和通知订阅仅为 `opened/resolved`，与通知 outbox 同事务；未知事实不能恢复告警，首次累计计数只建立基线。
- Webhook/SMTP 复用中立发送接口和统一重试算法，至少一次投递；确认/抑制不改写业务实例状态。读取与管理通过 Monitor 服务账号向 System 独立 Platform 审计接口追加最小操作事实。
- 最终失败的平台通知通过单条 `POST /platform/alert-notification-deliveries/{id}/retry` 重新入队，仅 Platform User 与 `monitor.platform_alert_notification.read/update` 可操作。保留原投递/事件/正文和发生时间，使用原目标当前配置；目标版本与预期人工次数拒绝并发或延迟重复请求，抑制不能绕过。累计次数保留，本轮上限和退避从 `retry_base_attempt_count` 重新计算；历史恢复告警允许补发，界面必须明确提示。
- 门禁：`make test-go`、`make test-monitor-postgres`、`make test-system-iam-postgres`、`make test-system-runtime-log`、`make test-system-frontend` 和 `make test-platform`；数据库及故障注入必须使用标准 disposable 入口。完整设计见 `docs/next/ADDP模块服务运行日志设计.md` 第十一节。

平台日志通知支持明确的 `wecom` 渠道：固定官方端点、通过既有凭据接口写入完整机器人地址并仅加密保存 key；复用受控 HTTP 客户端，HTTP 2xx 与整数 errcode=0 同时成立才算发送成功。平台 outbox、重试及安全诊断保持同一主路径，租户通知不扩展。Go/PostgreSQL 回归由现有 Monitor owner gate 自动发现，Monitor 配置交互及 System 诊断跳转由两模块既有前端门禁覆盖。

## 节点监测目标与认证发现

- Platform User 管理 `/platform/monitoring_targets`；列表/详情/创建/更新/删除分别消费 `monitor.monitoring_target.read/create/update/delete`。非委托 User Access Token 支持第一方和 OAuth，引用节点始终转给 System 当前裁决；默认仅平台系统管理员获得管理权限。
- 固定 `addp-prometheus` 非委托 Platform Service Access Token 读取 `/platform/metrics_discovery`，仅授予 `monitor.metrics_discovery.read`，不授予目标管理、后台身份投影或 Tenant Runtime 角色。System 连续迁移 000191 发布身份和权限；可选凭据关闭时撤销授权版本和旧令牌。
- 唯一持久表 `monitor.monitoring_targets` 保存 UUID、节点引用、监测类别、来源、启用意图和版本。主体/类别不可改写，更新及删除执行 CAS；全局非阻塞事务锁保证 1,000 保存目标、九个启用端点的预算不会被并发突破。来源异常不阻止停用或删除。
- `ADDP_OBSERVABILITY_METRICS_ENABLED` 严格选择；`MONITOR_METRICS_ALLOWED_CIDRS/PORTS` 是明确网络准入集合，CA 和独立客户端证书由 `MONITOR_METRICS_CA_FILE/CLIENT_CERT_FILE/CLIENT_KEY_FILE` 注入。关闭或缺配置不阻断 Monitor 原有 Ready；相应采集请求返回 disabled/unconfigured。`PROMETHEUS_SERVICE_CLIENT_SECRET` 只配置 System 的独立发现身份，不能借用日志或业务客户端凭据。
- `GET /platform/metrics_discovery` 返回原生 HTTP SD 数组；成功空列表与失败分开，不接受分页、调用者标签、query 或正文。保存版本不是实际 Prometheus 生效回执，版本只进入内部发现元数据。
- 现有 `make test-monitor-postgres` 自动运行目标并发预算和 CAS 回归。`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package iam --test prometheus-credential"` 验证可选身份及撤销；`--package api --test oauth-client-credentials` 使用真实 System OAuth Handler 验证最小平台身份、Tenant 拒绝及旧令牌撤销。两者是既有标准入口的精确选择，CI 仍使用完整门禁，不缩减范围。
- 指标中心生产模板已接入原生 OAuth2/HTTP SD；控制面 HTTPS origin、独立来源证书和 Secret 文件由部署注入，标准 Infra 预检生成唯一配置。`make test-monitor-metrics` 验证真实 Prometheus 的认证发现、令牌续取、身份标签清理、失败保留旧目标及成功空列表移除；控制面为受控协议夹具，不能替代真实 System/Monitor 部署全链路验收。正式节点来源与控制面已取得 Hosted T4；生产纳管覆盖、cAdvisor 及面向用户的生效状态仍待后续验收，具体输入见 `scripts/infra/README.md`。


## Platform 节点基础资源查询

- 资源读取接口 `/platform/resource_observations`、`/platform/resource_trends`、`/platform/resource_summaries` 使用 `monitor.resource_observation.read`；System 连续迁移 000193 仅给平台系统管理员发布该权限，采集服务与 Tenant 身份无此能力。当前请求内转发 User Token，由 System 裁决节点；读取当前启用目标及受控解析端点，节点/目标停用返回 `not_connected`，不查历史作为当前值。
- `internal/resourcequery/` 唯一维护 9 个标量目录项、PromQL、timestamp 证据、步长与响应预算。CPU 核数、一分钟 CPU 忙碌率、内存总量/可用量/使用率、1/5/15 分钟负载和运行时长支持即时及趋势；负载不是 CPU 利用率。CPU 忙碌率须证明一分钟完整窗口及核集合/启动时间一致，缺样本或计数器重置返回空值；文件系统容量与挂载趋势已沿同一 API 实施（见下文），磁盘读写吞吐按 device 沿同一 API 实施（见下文），网络接口收发速率按 device 沿同一 API 实施（见下文）。每个网格点明确评估/采样时间、单位、状态与空值，缺失不变成零，过期值不算当前有效值。
- `/settings/resource-query-policy` 沿用模块级 `monitor.configuration.read/update`，仅 Platform User，唯一 `monitor.resource_query_policy` 单例保存 CAS 版本与完整预算。每个查询读一次已提交预算，对新请求热生效；没有环境回退或重启要求。标准平台审计记录安全结果与保存版本，不记录 PromQL、Token 或内部地址。
- mTLS 查询部署输入独立于节点准入和 collector；关闭或不完整配置不阻断业务。生产只通过标准生命周期的 `metrics-query.yml` 挂载，不增加必需 Infra 依赖。
- Go T1、Monitor PostgreSQL 及 System IAM Migration T2 沿既有标准入口自动发现；同一个 `platform-node-metrics` suite 已在 `794450a30` 取得扩展后的真实 Hosted T4（run 37485747576），覆盖八项即时/趋势查询、预算 CAS/热生效、权限隔离、中心故障与新样本恢复、停用后不复用历史及独立清理零残留；具体证据见设计 10.22 节。用户资源查询页面现已接入 Console（见下文）；更多指标和生产 T5 仍须分批补齐，该次 Linux VM 通过不代表生产纳管覆盖。

## 平台监测目标管理前端

- Monitor 唯一拥有 `/monitoring-targets`、`/monitoring-targets/new`、`/monitoring-targets/:id`，Console 使用 `/monitor` 前缀集成真实 iframe；分页与详情由公开 URL 恢复，端点草稿不进入 URL。
- 入口只接受非委托 Platform User，需同时具备 `monitor.monitoring_target.read` 和 `platform.host_node.read`；新建、修改、删除分别消费对应独立 Permission。Context、主体、委托或权限变化同步取消请求并清空列表与草稿，迟到响应不得覆盖新范围。
- 主体/类别创建后只读，更新/停用与删除携带保存版本。CAS 冲突保留草稿并停止写入，须明确重新加载；启用标记仅表示保存配置，不表示实际采集健康。平台主机管理导航使用共享 Console bridge。
- 标准门禁为 `make test-monitor-frontend`、`make test-console-frontend`、`make test-common-frontend` 与 `make test-frontend-ci-registration`。Console 浏览器生命周期自建 Monitor Vite 夹具，CI 安装其锁定依赖；Monitor 前端变更扩散至 Console，后端变更不扩散。无需重启个人开发环境。

## 平台节点资源前端

- Monitor 唯一拥有 `/node-resources` 和 `/node-resources/:node_id`，Console 集成菜单、搜索与真实 iframe；列表 search/page/page_size、详情 range/metric/refresh 可恢复，默认值省略。System 主机管理提供节点列表和详情；列表整页一次固定 CPU/内存及采集证据概览，选中后读取九项即时资源与单项趋势。概览逐节点沿当前 User Token 核验，整批共享预算/超时/并发租约，复用原公式和新鲜度，不逐行查询详情。
- 入口仅接受非委托 Platform User，同时具备 `platform.host_node.read` 和 `monitor.resource_observation.read`；上下文、主体、权限或节点变化同步取消请求并清空旧观测。关闭/未配置/不可用/超时/预算及并发超限分别提示，失败不保留旧观测。
- 详情默认完成一轮后等待 15 秒自动刷新，可选 10/30/60 秒或关闭，选择通过 URL 恢复；列表只手动刷新一次批量概览，不设置计时器。隐藏时取消请求并暂停，可见且开启时立即刷新；身份/路由变化及卸载清理，401/403/404 停止重试，手工刷新重新裁决。时间明确标注；趋势窗口以本轮即时响应的服务端 end 为准。valid 零值有效，stale/no_data/not_connected 不展示为当前数值，曲线保留断点，明细展示原状态与采样/评估时间。不把 Load Average 解释为 CPU 利用率。
- 图表及数字/时间格式复用 Common Chart 和 Basic 的唯一实现；共享折线最多 1,000 点，null 保留为空点。既有 Workbench 消费者与 Monitor/Console/共享前端标准门禁共同验证，新增测试沿既有自动发现和 CI 登记；确定性页面 T3 不代替真实后端或页面 T4。

- 同一 `platform-node-metrics` Hosted Linux VM 已于 2026-10-07 完成真实 Console/Monitor 节点资源页面 T4（Run 37563564755，代码 `b3a64756c`）：正式密码/MFA、同一非委托 Platform User、八项资源/趋势、单 iframe 历史及刷新恢复、安全管理员入口拒绝与无业务请求、预算及监测故障隔离/恢复、停用后历史排除、退出零残留通过。沿用既有临时身份例外，未增加权限或生产角色；T3 仍独立计量，生产 T5、更多指标及自动刷新未在该次验收范围。细节与失败诊断见设计 10.25。

节点资源详情自动刷新已在实现提交 `1ce34d770` 完成真实 Hosted T4（Run 37566181124）：自然 15 秒计时、服务端 end 前进、URL 不变、关闭后刷新恢复及 16 秒无定时请求通过；同提交 Platform CI 全部通过，退出零残留。隐藏/恢复、慢请求、撤权和卸载由 T3 单独覆盖；完整证据见设计 10.26，生产 T5 与更多指标仍待分批实施。

CPU 忙碌率及趋势新增固定 `node.cpu.busy_percent`（percent，60 秒窗口），本地 Go/真实 Prometheus/PostgreSQL/前端门禁及 15 个窗口/核集合场景已通过。九项读取范围经用户确认，沿用原隔离身份与权限边界；实现提交 `5bb4173cf` 的完整 Hosted T4（Run 37573777193）及 Platform CI（Run 37573754535）已通过，真实页面、两次同一采集身份 grant、节点/目标恢复和停用历史排除、独立清理零残留均已复核。初始全空响应的有界等待实际收敛，CPU 单独预热/恢复零值由 T3 单独证明；26 项 runner 回归覆盖凭据寿命和拒绝语义。首轮凭据过期与旧 CI 异步断言失败不计为通过；完整证据见设计 10.27。文件系统容量的后续完成证据见下文，IO 与生产 T5 仍待后续批次，默认 test-changed 跨 owner 数据库预检失败未改记为成功。

文件系统容量与趋势沿设计 10.28 的已确认口径实施：新增五个 `node.filesystem.*` 目录键，使用率为 `used/(used+available)`；系列必填受控 `dimensions`，标量为 `{}`，挂载只含设备、挂载点和类型。既有资源 API 接受完整三项精确选择器，不开放任意标签或 PromQL。多挂载与历史挂载的序列上界参与步长和总点数预算，超限返回 422。详情依次读取九项概览、五项挂载表与一项趋势，挂载错误不清除有效概览；精确挂载与刷新选项由同一公开 URL 恢复。固定 node_exporter 排除规则显式写入唯一部署配置；不按挂载行求物理容量总量，不修改 IAM 或 Tenant 归属。最新本地 `test-changed`、Go、真实指标 T2、PostgreSQL、前端和 27 项 Online runner 回归已通过；实现 `08556c553` 的扩展 Hosted T4（Run 37602308231）、同实现 Platform CI（Run 37602281641）及 Release/T2（Run 37602281771）均已通过。真实六个挂载/30 条容量序列、同次采样公式、精确挂载 URL/趋势、恢复新样本与停用后历史排除已复核，退出零残留。证据按设计 10.28 分层计量；inode 的后续完成证据见下文，IO、全局容量和生产 T5 尚未完成。


文件系统 inode 沿设计 10.29 实施：四个 `node.filesystem.inodes_*` 固定键，共用设备/挂载点/类型及原精确选择器。总数、空闲数来自既有 filesystem collector，同次采样派生已用和使用率；零总数、整数范围/精度异常、缺失及 statfs 错误保留已知维度与空值。字节容量与 inode 独立查询、独立提示，唯一挂载表组合两组点事实；不按挂载汇总物理数量。目录共 18 项，12 项请求预算不放宽；页面顺序读取九项概览、五项字节容量、四项 inode 及单项趋势。锁定 promtool 与既有 T2 探针增加 inode 情景，真实 Hosted T4 沿同一 suite 扩展 inode 读取与 URL 恢复，本地 22 个 inode/18 个字节容量/15 个 CPU 原生场景、43 项 Monitor 单测/53 项 T3 及完整指标/PostgreSQL门禁通过；修正实现 `86853c844` 的 Hosted T4（Run 37610515534）与 Platform CI（Run 37610486715）完整通过。真实六挂载/24 条 inode 序列，其中五挂载有效、EFI 明确空值，完整 URL/趋势、恢复新采样、停用历史排除和清理零残留已复核；首次浏览器测试冗余赋值失败已记录并修正。默认跨 owner 预检失败不计为通过，生产 T5 与 IO 未完成，证据见设计 10.29。

## 服务实例资源监控（2026-10-10）

Console「运行监控 → 服务监控」读取 System 唯一平台实例列表，再用当前 User Token 一次批量读取三项固定进程指标：一分钟 CPU 核占用、当前 RSS 和运行时长。CPU 用核当量，RSS 用 IEC 容量单位，时长用天/小时/分钟；释义放在对应表头。角色、租约和当前主机关系仍由 System 裁决，未绑定主机也可独立观测实例，不从整机容量计算进程百分比。

Common Go SDK 支持 macOS/Linux 自身进程，部署显式启用独立 mTLS 端点后随正式注册发布不可变声明；重启自动发现新实例，无需手工复制实例 ID 或登记第二个目标。进程与主机共用原 HTTP SD 作业、来源预算、查询传输和并发 limiter；私有端点不公开给浏览器，后台投影不替代用户授权。关闭或采集失败不影响业务注册、心跳和 Ready；未接入、离线、窗口不足、失败及过期保留空值。列表与打开的当前实例趋势共用自动刷新：默认 15 秒，可选 10/30/60 秒或关闭，唯一 refresh query 恢复设置，每页 20 项；进程来源及默认服务范围的 Hosted T4 已通过；Python 和生产 T5 尚未完成，分层证据见设计 10.49、10.51；当前进程趋势已沿 10.55 完成个人运行及 Hosted T4 验收（Run 38227755626），生产 T5 仍待完成。本机 Manager/Meta/Transfer backend 已完成实际采集与 Safari 默认五条服务首屏复验，证据及边界见设计 10.52。标准开发入口已按具体启动单元分配进程采集输入，Meta/Transfer 三个 Worker 已完成本机独立 PID/mTLS/实例身份与 Safari 八条进程资源复验，合计九来源；不提供多副本启动，证据及窗口恢复边界见设计 10.53。

服务自动刷新沿设计 10.56：一轮依次取 System 当前页、批量摘要和已选实例趋势，窗口取本轮 queried_at；自动周期保留已校验画面直到新响应完整到达，失败清除对应旧值，未提交筛选草稿不被覆盖。关闭弹窗只继续列表轮询，不自动选择重启后的新实例；隐藏取消在途请求，回到可见立即读取，401/403/404 停止自动循环。主机与服务页使用 shared AutoRefreshControl/createAutoRefresh，原主机控件、定时器和可见性监听已删除。Shared T1、Monitor/Console T3、CI 登记及既有 Hosted runner 同次验证；完整 Hosted 38329665208（源码 7f365279b）已通过自然定时/关闭恢复、25 项节点与服务进程协议、指标中心/主机来源/控制面故障恢复、停用与删除及零残留。此前 Hosted 38325976574 的 System CPU 单次缺窗口未复现，失败时的固定时刻探针未触发，根因仍未确认；不将此次通过记作偶发故障修复。分层证据见设计 10.56，不扩大生产 T5 或 IAM。

## 主机接入与采集状态（2026-10-08）

服务进程趋势沿设计 10.55：`GET /platform/process_resource_trends` 只接受当前实例记录 ID 和整秒起止时间，固定 CPU 核占用/RSS 两条序列；复用原用户授权、身份/启动时间、来源准入、预算与租约检查，开始裁剪到本次启动之后，离线或声明失效不读取历史。前端沿服务监控行内“查看趋势”对话框，`instance_id/range` 可恢复，使用共享 ChartRenderer，缺样本保留断点；容器由共享图表实际高度撑开，桌面与窄屏均验证画布完整、不与下一张图重叠。趋势 API、Go T1/真实 metrics T2、Monitor/Console T3、Swagger 和同一 Hosted suite 的 runner 同次覆盖；个人 Safari 两张真实曲线、URL 范围恢复、启动裁剪、旧实例重新选择及来源身份已复验，同一 suite 的新 Hosted T4（Run 38227755626）完整通过，真实 CPU/RSS 趋势、刷新恢复、各来源故障隔离/恢复、停用历史排除及退出零残留已闭合，分层证据见 10.55。上一轮故障断言失败未复现、原因未确证，安全现场记录保留；生产 T5 未执行。

用户入口统一为 System「平台主机管理」、Monitor「运行监控 → 主机监控」与「高级设置 → 采集配置」。内部 HostNode、MonitoringTarget 和既有路由保持唯一职责。采集配置从当前用户有权读取的 System 主机列表选择名称，不复制主机身份；资源详情直接携带该主机进入采集配置。System 的允许服务模块绑定名单放在高级设置，从已有模块定义中选择，不代表实际部署关联。

即时资源查询增加有界 `collection` 证据：未接入、无样本、采集中、失败与陈旧；只有新鲜成功的 up 才判断文件系统与网络接口采集器是否提供；两类证据须唯一且与 up 同次抓取，否则为 unknown。即时 collection 的 filesystem/network 均必需；各资源区域使用自己的当前响应就地解释缺值，吞吐缺值不直接认定为窗口不足。趋势不附带当前采集状态。固定查询与资源查询复用同一 mTLS Client 和请求期限，不查询全中心目标列表。Desktop VM 的七采集器提供内核块设备 IO，不提供文件系统、inode 或网卡族；不能用容器配额冒充 VM 容量。部署自动登记尚待可信身份和正式调用契约，本批不修改 IAM，也不保存管理员 Token。


资源展示及磁盘吞吐沿设计 10.35：字节显示 IEC 容量单位，时长显示经过的天/小时/分钟，load average 为系统任务数量平均值，无百分比或“负载”单位后缀。共享 formatBytes/formatDurationSeconds 唯一维护展示换算，API 原始单位不变。目录共 20 项；新增 node.disk.read_bytes_per_second / write_bytes_per_second 为完整一分钟平均 B/s，以 device 单维度查询与展示，文件系统仍为 device/mountpoint/fstype 三维度，不混合选择、不跨设备层叠加。概览、文件系统、inode、磁盘及趋势沿原请求预算分别读取；采集缺失和窗口不足保留空值。Desktop 六采集器仍不启用 diskstats；原生完整来源由既有 diskstats 提供。标准 metrics T2 的 promtool 场景选择及真实完整/受限来源查询同步覆盖磁盘目录。

磁盘原生页面验收沿设计 10.36 完成：实现 `5c490bf5c` 的 Hosted T4（Run 37740681212）及 Platform CI（Run 37740647211，33 项任务）均通过。本地 32 项 Online runner 回归、Console 150 项单测/119 项浏览器回归及构建、完整 `make test-platform` 重跑通过；默认跨 Owner `test-changed` 预检失败单独保留。Hosted 实际设备 sda 的读写即时值及两条精确设备趋势有效，IEC 速率/容量、经过时长、系统负载数量、真实密码/MFA、URL 恢复、故障后新样本和停用历史排除已复核；六张截图齐全，外层销毁报告零残留且归档无凭据文件或敏感值模式命中。Desktop 受限来源的磁盘空值仍属部署边界；网卡吞吐的本批实施与验收见下文，生产 T5 尚未完成。


网络接口吞吐沿设计 10.37 实施：新增 node.network.receive_bytes_per_second / transmit_bytes_per_second，目录共 22 项，固定完整一分钟 B/s。网络与磁盘共用唯一设备计数器校验及表格/趋势交互，分组目录按族独立，不把同名磁盘设备和接口关联；各接口单独展示，不汇总桥接、虚拟或回环，不推断物理接口容量。缺窗口、重置、重复或缺失证据保留空值。原生 Linux 来源沿现有 host 网络命名空间；Desktop 受限来源仍不启用 netdev。既有指标 T2、Monitor/Console 前端和 Hosted 同一 suite 扩展网络读取、精确接口恢复、身份拒绝及故障恢复，生产 IAM 不变；本批 22 模块 Go、Monitor PostgreSQL、完整/受限 metrics T2、Monitor 46 单测/60 浏览器回归、Console 155 单测/123 浏览器回归及构建、31 个 Online runner 测试组与 Swagger 覆盖通过。完整模块入口在平台 Online 宿主脚本的两个 5 秒超时退出，39 项宿主脚本原断言独立复跑及完整 `make test-platform` 重跑通过；跨 Owner 默认 test-changed 预检失败单独保留。实现 `193c0e82e` 的 Platform CI（Run 37791001755）、Monitor PostgreSQL/metrics T2（Run 37791001784）及真实 Hosted T4（Run 37791100970）成功：16 个接口/32 条收发序列、两条精确接口趋势各 21 点、IEC 速率、真实密码/MFA、自动刷新/URL 恢复、故障后新样本及停用历史排除复核通过。84 个资源响应、七张截图与报告闭合完整，外层销毁零残留，36 个归档文件无凭据文件或敏感值模式命中。个人 Monitor 加载新增目录需用户重启，Desktop 网络空值仍为受限部署范围；生产 T5 未执行。


磁盘 IO 时间沿设计 10.38 实施：固定目录共 25 项，磁盘五项在唯一 device 表与趋势交互中展示。新增 IO 忙碌时间占比（percent）及平均读取/写入耗时（milliseconds，页面 ms）；平均耗时包含排队与处理，不是纯等待，完成次数为零时空值，有完成请求的真实零耗时可显示零。忙碌占比接受有限 0–100，不代表饱和度。磁盘与网络共用唯一完整一分钟计数器校验，平均耗时还要求时间/完成次数同次采样；不跨设备层聚合，不新增权限、API 或请求族。Desktop diskstats 仍关闭。Monitor 47 单测/61 浏览器回归、Console 155 单测/123 浏览器回归及构建、22 模块 Go、Monitor PostgreSQL、32 个 Online runner 测试组和 Swagger 覆盖通过；最终标准 `make test-module MODULE=monitor` 串行完整返回 0，含平台 T0、Monitor Go/前端、独占 metrics 与 PostgreSQL T2；21 个 IO 时间及全部既有原生场景、完整来源 18 设备/五项磁盘趋势、受限来源空值、故障恢复与零残留通过。首次真实 Hosted 在默认选中 EFI 挂载却要求 inode 有效时失败，按真实有效容量/inode 选样修复，生产查询不变；修复提交 `839ef8338e` 的 Hosted T4（Run 37808051558）、Platform CI（Run 37808006295，33 项任务）及 Release/T2 工作流（Run 37808006338）成功。生产实现提交 `88d7ade1d` 的 Monitor PostgreSQL/metrics 实际 CI 门禁（Run 37803604603）成功，修复后路径跳过不计为重跑。下载后同一闭合校验确认 25 项目录、102 个真实响应、七张截图、三条新 IO 时间趋势各 21 点/5 个有效点、%/ms、正式密码/MFA、恢复新采样和停用历史排除；外层销毁零残留，36 个文件无凭据文件或敏感模式命中。首次失败、先前 mTLS 超时和跨 Owner 默认预检失败分别保留；Desktop 磁盘采集仍关闭，生产 T5 未执行。

Desktop 磁盘采集沿设计 10.39：七采集器与五项既有磁盘目录，读取 VM 内核块设备统计，不代表 Mac 物理磁盘或单个容器。原非 root/无特权/无 host 命名空间/四个只读挂载不变，filesystem/netdev/netstat 仍禁用。历史六采集器验收记录不改写；当前部署与 T2 契约按本节执行。

10.39 本地分项验收：最终标准 metrics T2 验证实际 Desktop 七采集器、18 设备/七原始计数及身份的独立命名空间对照、五项精确设备趋势、恢复新样本和零残留，Monitor PostgreSQL 25432/addp_test 通过。模块第三次运行中的平台 T0、Monitor Go、47 单测/61 浏览器回归及构建通过，但整个模块入口在后续资源查询 backend unavailable 时退出；此前个人开关干扰的端口夹具已补输入隔离并复验，10 秒生命周期超时及约 3 秒查询失败未确认根因。T2 握手诊断不改生产预算，最终独占重跑通过；不将分项通过写成完整模块入口通过。个人来源仍待用户标准 up 应用，CI/个人页面和生产 T5 状态各自记录。


10.39 个人来源已由用户通过标准 up 应用：实际七采集器成功，中心收到原节点 18 个设备及 vda 活跃的五项磁盘原始计算，新鲜样本约 13 秒。只读 mTLS 中心诊断不等于 Monitor 完整窗口或页面验收；Safari 登录过期，正式页面仍待用户重新登录。实现已随 main 推送至 51d7a66bfb，Platform CI 37877201811 和 Release/T2 37877201781 已触发，初查排队；文件系统/inode/网卡的 Desktop 空值与生产 T5 边界不变。

主机列表与详情统一使用上述刷新控件，默认 15 秒；搜索/分页/refresh 沿 URL 恢复，进入详情及返回列表保留选择。每轮只批量查询 System 当前授权页，节点与观测同时发布，不覆盖未提交搜索；隐藏时取消、可见立即重读，授权拒绝清空并停止。Monitor/Console 既有前端门禁及 platform-node-metrics 同一 Hosted suite 覆盖，真实列表自然刷新、关闭恢复和原故障链路已在 Hosted 38340694203（源码4e9fe672f）通过，退出零残留；前次导航Test ended未复现，根因仍未确认，安全耗时诊断保留。见设计10.57；不代表后续总览或生产T5已验收。

### 2026-10-13 continuous 租户执行诊断撤权回归

Hosted `38337857450`（`b0765f802`）的 live/recovery 和旧内容清空已通过，失败阶段为新鲜 Monitor-only 会话的拒绝提示断言；整项 T4 失败，清理通过且 Infra 零残留。相同精确文本断言在标准 Monitor 浏览器门禁的冷启动 403/404 场景均复现 strict-mode 双匹配：无障碍 `StatusAnnouncer` 与可见 `el-result` 标题使用同一提示。断言改为唯一可见结果标题的可见性与完整文本校验，保留无障碍播报，以及无详情、步骤、事件和执行树断言；不删除播报，不放宽 Owner 裁决或修改生产逻辑。既有 `make test-monitor-frontend` 自动发现两个新回归，Console Hosted 同一 continuous suite 继续验证正式登录与撤权。

该最小验收修正的 `make test-monitor-frontend` 已通过（59 单元、131 浏览器及构建），`make test-transfer-online-runner` 已通过（65 项）；修正前两个新用例明确失败于 strict-mode 双匹配。Hosted 复验另行记录，未完成前不计为通过。

该修正以 `ba2c99652` 提交；包含修正的 main `65de8a82938ba1bbdd56df6f7c000be7f67076cc` 已在 [Hosted continuous T4 38381655995](https://github.com/pampa0629/addp/actions/runs/38381655995) 通过。下载后核对 live/recovery/revocation 三个闭合浏览器报告及九组接口/Worker 检查：真实事件进展、Worker 失联后新 UUID 恢复、暂停/恢复与安全失败码、跨租户与同租户 Owner 权限、Owner 不可用、撤权清空及新鲜 Monitor-only 会话拒绝、CDC 快照及增删改事件、删除任务后的授权历史读取均通过。正式登录的拒绝截图仅显示错误标题与重新加载按钮，无执行内容。System/Gateway/Meta/Transfer/Monitor 五个实际构建的源码均匹配上述 main；场景清理通过、残留 0，外层 Infra 销毁零残留。37 个归档文件未发现私有控制文件或标准访问令牌/JWT 模式。此前失败保持原记录，不将其改记为通过；本次不包含工作区尚未发布的 Workflow/Notebook 监督器改造、180 天概览清理或生产 T5。

### 2026-10-15 租户执行读取链路 T1

新增 `TestExecutionReadAPIReauthenticatesThroughOwnerForEveryRequest`：真实 Monitor 路由、HTTP Owner 裁决 Client、Owner 共享读取范围 Handler 和认证中间件串联，身份事实由独立 HTTP 夹具提供。逐请求核对 Owner 调用及身份重新读取，验证跨租户、同租户他人一次性执行、缺少发起主体、父可读子不可读、已删除定义历史、列表与统计总数、详情/树/事件的动态撤权及恢复，以及 Owner 不可用时整份 503、恢复后可读；响应不带执行配置、业务载荷或 Owner 原始错误。临时让身份夹具保留撤权前权限时，回归明确失败于应 404 实际 200；恢复后 `make test-module MODULE=monitor GO_ONLY=1` 通过。新增测试由现有 Monitor Go T1 及 Platform CI `Go workspace tests` 自动发现，无新增注册或 Swagger 变更。该证据仅为使用身份夹具的 T1，不代表真实 System IAM、PostgreSQL T2、完整模块门禁或新的 Hosted T4 通过；本次不修改这些实现或重跑历史 IAM 门禁。


## 平台告警与资源规则

平台告警归 Monitor，Platform/Tenant 的事件、规则和通知相互隔离。唯一平台入口 `/platform-alerts` 包含事件、通知管理和资源规则页签；System 保留日志诊断与带来源上下文的跳转。原日志管理接口及页面路径已删除，原有通知目标仍只订阅日志，资源订阅须显式选择。两类事件使用同一事务协调器与 outbox，日志协议保持 `addp.platform-log-alert/v1`，资源协议为 `addp.platform-resource-alert/v1`。

资源规则使用 `/platform/resource-alert-rules` 的分页 GET、POST 及 `/:id` 的 PUT/DELETE，固定绑定 System 主机 UUID 和 CPU 忙碌率或内存使用率。新建始终停用，参数与启停使用配置版本 CAS；评估不推进配置版本。独立的查看、创建、修改、删除权限为 `monitor.platform_resource_rule.*`，只接受非委托 Platform User；创建和修改沿当前用户向 System 核验主机。迁移 000211 只向既有平台系统管理员发布这四项权限，不改 IAM 内核或租户权限。事件和通知仍分别使用 `monitor.platform_alert.*`、`monitor.platform_alert_notification.*`。

后台以十五秒单轮评估，按主机合并固定指标与 uptime，复用同一资源查询预算、并发 limiter、mTLS 客户端与最小身份投影。不访问 System 私有表，不使用管理员 Token；没有启用规则时不访问采集后端。缺样本、控制面不可用、预算不足、来源变化、暂停或重启均不能当作恢复，连续证据窗口需要重新积累。已有告警按触发时保存的恢复参数判定；打开证据不可变，恢复事件保留新窗口证据。活动告警阻止规则删除，停用不伪造恢复，删除规则保留已恢复历史。

本批由现有 Monitor Go、PostgreSQL、真实 metrics、前端、授权目录/迁移及 Swagger 门禁覆盖；metrics 的 INPUT_FILES 已扩展规则实现和证据测试，未增加旁路 suite 或 workflow。本批包含 `common/authorization` 校验同步，个人环境验收按根 AGENTS.md 由用户执行 `./scripts/dev/restart.sh -all` 并重新登录；不因本地 T1–T3 通过而宣称 Hosted T4 或生产 T5 完成。实现契约及分层证据见运行监控设计 10.68–10.69。

资源告警联合 T2 继续使用 `make test-monitor-metrics`：受控 mTLS 源经真实 Prometheus 和后台规则循环驱动 PostgreSQL 事件/outbox，再向临时 TLS 接收端发送签名 Webhook，包含 HTTP 503 重试、缺样不恢复及低值恢复。PostgreSQL 由同一临时 Compose 独占，只初始化 `addp_test`；临时密码文件是数据库启动的必需输入，只渲染共享指标源的 Hosted 准备不需要该凭据。Go 编译依赖与联合测试统一登记在该门禁的 service/models/repository 目录输入；原有采集及 TOP5 场景保持完整。System 身份仍为模块夹具，运行记录见设计 10.72，不将该 T2 当作正式身份的 T4 或真实群通知验证。
