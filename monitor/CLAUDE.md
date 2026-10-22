# Monitor 模块说明

Monitor 聚合各 Owner 写入的执行事实，提供授权诊断、统计、健康观测、告警和通知；同时拥有独立的 Platform 运行观测域。开发前按下表读取正式契约，能力状态及历史验收分别查对应专题。

## 必读导航

| 范围 | 权威正文 |
| --- | --- |
| 模块职责、实现入口与剩余范围 | [模块架构说明](docs/Monitor模块架构说明.md) |
| 执行存储、运行时角色、统计与租户告警 | [监控与执行体系](../docs/concepts/addp监控与执行体系图.md)及其专题 |
| Owner 读取、安全诊断、过程事件、保留及前端所有权 | [执行监控规范](../docs/spec/tasks/执行监控.md) |
| 节点、采集发现、资源查询、平台告警与权限 | [平台运行观测规范](../docs/spec/addp平台运行观测规范.md) |
| 平台日志、可信来源、观测与通知 | [模块服务运行日志规范](../docs/spec/addp模块服务运行日志规范.md) |
| 配置与后续范围 | [配置规范](../docs/spec/addp配置介绍.md)、[运行观测后续事项](../docs/next/ADDP平台运行监控与可观测性设计.md) |
| 历史通过、失败与覆盖限制 | [Monitor 历史证据](../docs/spec/testing/evidence/Monitor历史证据.md)；不代表当前 HEAD 已重跑 |

## 核心边界

- 任务定义和执行事实归业务 Owner / Common；Monitor 不读取 Owner 私表或直连业务 Kafka、源数据库来补判状态，不成为第二个执行者。新增执行写入复用 `common/execution` 与 `EnsureStore`。
- 用户身份沿 System opaque Token / AuthContext。Tenant AuthContext 和 Monitor Permission 仅为入口条件；执行列表、详情、树、统计和关联告警均通过同步 BFF 逐请求取得 Owner `execution-read-scope`，不可用时失败关闭。父可读不授予子结果权限，专业数据仍从 Owner 授权入口读取。
- 通用响应仅含安全 DTO，不序列化完整 TaskExecution、原始配置/metadata/错误、业务样本或授权凭据；诊断 GET 复用现有 System 操作审计。
- continuous evaluator 只读任务最新根 execution：pending/running 派生运行信号，最新 failed 且 schema_change.status=pending 才保留结构阻塞信号；新根执行取代旧信号，不回扫历史 pending。通用失败规则排除 ad-hoc、子 execution 和 continuous session，规则、路由与通知语义见正式规范。
- evaluator 先收集 active signal，再由单一 reconciler 处理 incident。查询失败不能恢复告警；确认/抑制不改写 Owner 事实。生命周期 event 与各渠道 outbox 同事务，dispatcher 在事务外至少一次投递，独立测试不创建正式事件或 outbox。
- Platform 与 Tenant 使用独立权限和事实。System 拥有主机、实例及当前绑定；Monitor 只消费授权投影，不复制租约或私有台账，后台身份不能替代当前用户的节点读取裁决。
- 可选 Prometheus、日志或追踪设施不成为业务 Ready 的强依赖。System 租约 UP/DOWN、采集是否成功和资源压力分别解释，缺样、未知或暂停不能冒充健康/恢复，也不推导自动扩缩容。

## 代码与接口入口

| 入口 | 职责 |
| --- | --- |
| [权限清单](authorization/permissions.yaml) | Monitor 是自身 execution、statistics、health、通知、观测及平台告警 Permission owner；通过 Common 发布期聚合，不在启动时动态注册。 |
| [Router](backend/internal/api/router.go)及[生成 Swagger](backend/docs/) | `/api/v1/monitor` 下的精确路由、Context Guard、Permission、参数和响应；健康检查复用公共 `/health/live`、`/health/ready`。 |
| [业务服务](backend/internal/service/)与[持久模型](backend/internal/models/) | 执行读取、统计、Provider/Runtime 健康、规则、通知和资源查询。 |
| [metricsdiscovery](backend/internal/metricsdiscovery/) | 唯一节点来源准入、受控 CIDR/端口、固定 HTTPS 路径、DNS 固定 IP、mTLS 与发现预算内核。 |
| [resourcequery](backend/internal/resourcequery/) | 唯一固定指标目录、PromQL、窗口/采样证据、步长及点数预算；不开放任意标签或 PromQL。 |
| [前端 Router](frontend/src/router/index.js)与[页面](frontend/src/views/) | 真实 Console iframe、页面权限、URL 恢复及生命周期。 |

Go 后端与 Vue 3 / Element Plus / ECharts 前端沿平台技术栈。开发首选端口为 Backend `8100`、Frontend `5179`，实际值以标准启动输出及 `.dev-state/ports.env` 为准，见[端口规范](../docs/spec/addp端口分配.md)。

## 前端共享与导航

- 遵守[可恢复路由规范](../docs/spec/addp前端路由与可恢复状态规范.md)，模块内导航只通过 [moduleNavigation.js](frontend/src/utils/moduleNavigation.js)；Console 跳转保持 iframe、单历史和返回筛选上下文。
- 独立根入口在 AuthContext 加载后依次选择有权进入的平台节点资源、采集配置、仪表盘、执行记录、告警、通知；无可进入页时显示无权限，显式地址保持原样。
- 执行详情唯一地址为 `/monitor/executions?execution_id={execution_uuid}`：打开用 push，关闭清除 execution_id 用 replace，前进/后退同步详情。默认 incidents/webhook 页签省略 tab，rules/email 等非默认值使用 replace。
- 通知唯一入口 `/notifications`；平台事件、通知及主机/HTTP 规则唯一入口 `/platform-alerts`。System 保留主机与日志诊断、来源策略和带上下文跳转，不恢复旧日志管理或 webhooks 路由。
- 执行步骤复用共享 `ExecutionSteps`，资源血缘复用 Basic 的唯一 lineage_facts 归一能力；业务 ResourceLocator 通过共享 Manager 路由打开，addp-infra 平台内部产物不推断为业务数据项或生命周期。详情清空、刷新与恢复字段见[诊断页面契约](../docs/spec/tasks/执行监控.md#执行诊断页面与授权失效)。
- 主机选择和清单校验复用共享 HostNodeSelect；System `/host-admission` 独占 v2 主机接入，Monitor 只接受同一内存导航草稿并独立校验已有主机及来源，草稿不进入 URL 或客户端持久存储，未知创建结果阻止重复提交。
- 服务与引擎监控的主机引用使用唯一 [loadHostReferences](frontend/src/utils/hostReferences.js)，仅解析当前有权读取的一页，去重且最多四并发；无权/失败只显示已有 ID，未绑定显示未关联，迟到结果不得保留。当前 System node_id 是唯一绑定事实，不用主机名、IP 或部署别名补造。服务与引擎名称链接复用同一 MonitoredHostLink 领域组件；引擎关联失效不回用历史声明的主机，跳转保留原页面地址供浏览器返回恢复。
- 服务登记端点复用 Basic `getRegisteredEndpoint`，只显示 HTTP(S) 主机与端口；Worker 用途只读 System metadata.runtime_name，未知值只显示正式角色，不据此判断任务正在运行。
- 主机、进程和总览共用 ChartRenderer、格式化与 AutoRefreshControl/createAutoRefresh；默认十五秒、10/30/60 秒或关闭沿公开 URL 恢复。隐藏时取消、可见时重读；401/403/404 停止循环；失败清除相应旧值，不覆盖未提交筛选。状态、单位、零值、缺样和历史窗口按正式指标口径解释。

## 运行观测与平台规则

- 采集配置由非委托 Platform User 管理，引用节点始终交 System 当前裁决。主体/类别创建后只读，更新/删除执行 CAS；并发预算、端点预留、停用/删除不依赖来源健康的规则见正式规范。
- `addp-prometheus` 仅以独立 Platform Service Access Token 读取原生 HTTP SD，不获目标管理或后台身份权限；成功空列表与失败分开，保存版本不是 Prometheus 生效回执。凭据、证书和 Secret 文件由各自部署/配置 owner 管理。
- `scripts/infra/node-metrics.py` 是独立来源部署入口；原生 Linux 与 Desktop VM 身份分别确认。Desktop 当前七采集器含 diskstats，只表示 VM 内核块设备视图，不代表 Mac 本机或单容器，也不提供 filesystem/inode/netdev/netstat。
- 固定目录当前共 25 项；CPU、负载、容量/inode、磁盘吞吐/IO 时间、网卡吞吐及进程 CPU/RSS/时长使用各自单位与完整窗口，不跨设备层累加、不用共享挂载或进程 RSS 推导集群容量。资源查询与采集证据共用现有 mTLS、预算与期限，指标和公式只由 resourcequery 维护。
- `/engine-resources` 提供 Infra／Business PostgreSQL 的十二项专业指标、当前页四项摘要和当前数据库容器段最长七天趋势。System 逐请求裁决当前 User 的引擎部署读取，私有投影只用于发现且查询后重核；同一 HTTP SD、mTLS、端点去重和资源预算，不增加手填目标或租户用量归算。SQL 快照不完整、过期、统计重置或权限变化均不保留旧值。来源/查询实链专项沿 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source`。
- 平台 HTTP 质量入口为 `/http-quality`，使用独立七项 HTTP 目录和固定 1/5/15 分钟窗口，模板排行最多十项；Gateway ingress 与 Backend 分层读取。统计读取与 HTTP 规则分别授权，当前 User 的实例裁决和后台最小身份投影不得混用；契约见正式规范 6.4。Go 使用 Common 唯一采集工厂，Python 未接入，Tenant 发布服务质量归 Service。
- 本地 HTTP 专项使用 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality`，复用专属指标链与规则/outbox 测试库，跳过 PostgreSQL 专业来源探针及后续主机回归；完整回归仍用默认入口。该分项不代表正式身份 T4。
- `/traces` 及 Trace 搜索/详情使用独立 `monitor.trace.read`，逐请求以当前 User 核验所有参与实例（含离线历史），关联日志另走 System 正文权限。实际 Client/Server 父子匹配才构成调用边；Orchestrator→Develop bounded query 的短阶段以独立 Trace 和最多两条因果 Link 关联，Link 目标同样核验当前 User 实例权限，不递归读取。采样结果不能推算全量 QPS。传播、120 秒尾采样、72 小时/20 GiB 初值及本地验收见正式规范 6.5；当前策略是 Monitor-owned 不可变版本 2，不新增热编辑控制面。Python 同步 HTTP 由 Common Python 唯一 ASGI/HTTPX 内核接入 Agent/Copilot，复用同一部署声明、采样与日志关联；不代表 Python 全量 HTTP 指标或其他 Runtime 已接入。
- 资源查询策略是 Monitor 平台 CAS 单例，对新请求热生效；资源与规则后台只用最小身份投影，不保存管理员 Token。目标管理、用户观测读取和采集服务权限彼此独立。
- 平台主机 CPU/内存规则创建即停用，按主机合并指标与 uptime；缺样、控制面不可用、预算不足、来源变化、暂停或重启均不作为恢复。活动 incident 阻止规则删除，停用不伪造恢复，历史事件保留触发时证据。
- 平台日志、资源与 HTTP 事件复用同一事务协调器/outbox，协议分别为 addp.platform-log-alert/v1、addp.platform-resource-alert/v1 与 addp.platform-http-alert/v1。旧目标不自动订阅新类别；Webhook/SMTP/企业微信不跨协议兜底，人工重投保留原身份和历史时间。
- 平台日志观测只由独立 addp-log-observer 上报有界安全事实；Monitor 不读取接收器宿主文件或上传日志正文。平台操作审计归 System，进程日志、执行事件与审计保留各自事实源。

## 开发与验证

从仓库根目录使用 `make test-changed` 或明确 Owner 的 `make test-module MODULE=monitor`；快速 Go T1 使用 `make test-module MODULE=monitor GO_ONLY=1`，不替代完整模块门禁。

持久化/真实指标分别沿 `make test-monitor-postgres`、`make test-monitor-metrics`，真实 HTTP Trace、Tempo 与日志关联沿 `make test-monitor-traces`；显式容量基线使用 `make test-monitor-traces MONITOR_TRACES_TEST_ARGS=--capacity`，负载、资源上限及外推判定沿运行观测规范 6.5（仅当次本地设施，不运行 Hosted），前端沿 `make test-monitor-frontend`，共享组件和真实 iframe 按影响扩散至 Common Frontend / Console。System IAM、运行日志及授权/Swagger 变化按规范扩散，不为文档整理重跑无关门禁。API 变更同时执行标准 Swagger 生成与路由覆盖检查。

本地数据库先用 `bash scripts/infra/status.sh` 确认实际映射，测试资源与清理只走标准入口。需要运行验收或重启时遵守根 AGENTS.md，由用户在自己的终端执行标准生命周期命令；不接管个人服务，不执行未经单独授权的 keepalive。

## 仍需独立证据的范围

- 实际多主机 TOP5、独立 Owner 撤权与全平台容量，不能由六个受控指标来源 T2 或单主机 T4 外推。
- 容器/专业引擎、Python 进程指标、Agent/Copilot 同步 HTTP 之外的追踪和面向用户的采集生效回执按[后续事项](../docs/next/ADDP平台运行监控与可观测性设计.md)分别推进；节点登记不自动证明承载关联。
- 180 天概览清理、专业运行时历史及其他 Owner 阶段事件沿任务规范；已落地的 30 天过程事件清理不代替这些范围。
- 生产安装权限、物理身份、HA、规模和 T5 均需对应独立验收。历史失败、排队或部分通过仍按[证据专题](../docs/spec/testing/evidence/Monitor历史证据.md)记录，不合并为当前工作区通过。
