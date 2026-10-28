# Monitor 模块说明

Monitor 聚合各 Owner 写入的执行事实，提供授权诊断、统计、健康观测、告警和通知；同时拥有独立的 Platform 运行观测域。开发前按下表读取正式契约，能力状态及历史验收分别查对应专题。

## 必读导航

| 范围 | 权威正文 |
| --- | --- |
| 模块职责、实现入口与执行监控后续范围 | [模块架构说明](docs/Monitor模块架构说明.md) |
| 执行存储、运行时角色与租户告警 | [监控与执行体系](../docs/concepts/addp监控与执行体系图.md)及其专题 |
| 执行统计与运行指标 | [正式统计契约](../docs/spec/tasks/执行监控.md#执行统计与运行指标)维护 API 分工、窗口／积压、耗时／重试口径及读取授权；实际验证范围沿该节和模块证据导航 |
| Owner 读取、安全诊断、过程事件、保留及前端所有权 | [执行监控规范](../docs/spec/tasks/执行监控.md)；事件清理、概览只读评估与周期证据见[留存导航](docs/Monitor模块架构说明.md#执行留存与验收) |
| 节点、采集发现、资源查询、平台告警与权限 | [平台运行观测规范](../docs/spec/addp平台运行观测规范.md) |
| 平台日志、可信来源、观测与通知 | [模块服务运行日志规范](../docs/spec/addp模块服务运行日志规范.md) |
| 环境与部署配置 | [配置规范](../docs/spec/addp配置介绍.md) |
| Platform 能力状态与验收缺口 | [后续事项能力表](../docs/next/ADDP平台运行监控与可观测性设计.md#一当前实现与剩余工作)统一维护实现状态和剩余覆盖；本入口保留模块规则及标准命令 |
| 历史通过、失败与覆盖限制 | [主题证据导航](#历史证据导航)；固定版本及未覆盖范围由各专题维护 |

## 核心边界

- 任务定义和执行事实归业务 Owner / Common；Monitor 不读取 Owner 私表或直连业务 Kafka、源数据库来补判状态，不成为第二个执行者。新增执行写入复用 `common/execution` 与 `EnsureStore`。
- 用户身份沿 System opaque Token / AuthContext。Tenant AuthContext 和 Monitor Permission 仅为入口条件；执行列表、详情、树、统计和关联告警均通过同步 BFF 逐请求取得 Owner `execution-read-scope`，不可用时失败关闭。父可读不授予子结果权限，专业数据仍从 Owner 授权入口读取。
- 通用响应仅含安全 DTO，不序列化完整 TaskExecution、原始配置/metadata/错误、业务样本或授权凭据；诊断 GET 复用现有 System 操作审计。
- continuous 信号、通用规则与恢复条件统一沿[执行信号与租户告警](../docs/concepts/monitoring/执行诊断与租户告警.md#执行信号与租户告警)及其正式契约；仅消费 Owner 公共事实，不以历史 pending 维持旧告警，不把 continuous session 计入通用失败规则。
- evaluator 先收集 active signal，再由单一 reconciler 处理 incident。查询失败不能恢复告警；确认/抑制不改写 Owner 事实。生命周期 event 与各渠道 outbox 同事务，dispatcher 在事务外至少一次投递，独立测试不创建正式事件或 outbox。
- Platform 与 Tenant 使用独立权限和事实。System 拥有主机、实例及当前绑定；Monitor 只消费授权投影，不复制租约或私有台账，后台身份不能替代当前用户的节点读取裁决。
- 可选 Prometheus、日志或追踪设施不成为业务 Ready 的强依赖。System 租约 UP/DOWN、采集是否成功和资源压力分别解释，缺样、未知或暂停不能冒充健康/恢复，也不推导自动扩缩容。

## 代码与接口入口

| 入口 | 职责 |
| --- | --- |
| [权限清单](authorization/permissions.yaml) | Monitor 是自身 execution、statistics、health、通知、观测及平台告警 Permission owner；通过 Common 发布期聚合，不在启动时动态注册。 |
| [Router](backend/internal/api/router.go)及[生成 Swagger](backend/docs/) | `/api/v1/monitor` 下的精确路由、Context Guard、Permission、参数和响应；Monitor 自身健康复用 `/health/live`、`/health/ready`，Provider 与后台实例观测分别沿[正式健康口径](../docs/spec/tasks/执行监控.md#provider-与后台运行实例健康)。 |
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
- 执行步骤复用共享 `ExecutionSteps`；资源归一化、业务输入跳转和内部产物展示统一沿[执行血缘资源摘要](../common-frontend/docs/components/页面状态与导航.md#执行血缘资源摘要)，不扩展上游安全投影或专业结果权限。详情清空、刷新与恢复字段见[诊断页面契约](../docs/spec/tasks/执行监控.md#执行诊断页面与授权失效)。
- 主机选择和清单校验复用共享 HostNodeSelect；System `/host-admission` 独占 v2 主机接入，Monitor 只接受同一内存导航草稿并独立校验已有主机及来源，草稿不进入 URL 或客户端持久存储，未知创建结果阻止重复提交。
- 服务与引擎监控的主机引用使用唯一 [loadHostReferences](frontend/src/utils/hostReferences.js)，仅解析当前有权读取的一页，去重且最多四并发；无权/失败只显示已有 ID，未绑定显示未关联，迟到结果不得保留。当前 System node_id 是唯一绑定事实，不用主机名、IP 或部署别名补造。服务与引擎名称链接复用同一 MonitoredHostLink 领域组件；引擎关联失效不回用历史声明的主机，跳转保留原页面地址供浏览器返回恢复。
- 服务登记端点复用 Basic `getRegisteredEndpoint`，只显示 HTTP(S) 主机与端口；Worker 用途只读 System metadata.runtime_name，未知值只显示正式角色，不据此判断任务正在运行。
- 主机、进程和总览共用 ChartRenderer、格式化与 AutoRefreshControl/createAutoRefresh；默认十五秒、10/30/60 秒或关闭沿公开 URL 恢复。隐藏时取消、可见时重读；401/403/404 停止循环；失败清除相应旧值，不覆盖未提交筛选。状态、单位、零值、缺样和历史窗口按正式指标口径解释。

## 运行观测与平台规则

- 采集配置由非委托 Platform User 管理，引用节点始终交 System 当前裁决。主体/类别创建后只读，更新/删除执行 CAS；并发预算、端点预留、停用/删除不依赖来源健康的规则见正式规范。
- `addp-prometheus` 仅以独立 Platform Service Access Token 读取原生 HTTP SD，不获目标管理或后台身份权限；成功空列表与失败分开，保存版本不是 Prometheus 生效回执。凭据、证书和 Secret 文件由各自部署/配置 owner 管理。
- `scripts/infra/node-metrics.py` 是独立来源部署入口；原生 Linux 与 Desktop VM 身份分别确认。Desktop 当前七采集器含 diskstats，只表示 VM 内核块设备视图，不代表 Mac 本机或单容器，也不提供 filesystem/inode/netdev/netstat。
- [固定指标、单位及采样窗口](../docs/spec/addp平台运行观测规范.md#61-固定指标目录)与[资源查询预算](../docs/spec/addp平台运行观测规范.md#62-资源查询与预算)统一由 [resourcequery](backend/internal/resourcequery/) 实现；前端和规则复用这一目录与公式，不另算同名指标或绕过现有 mTLS、预算及期限。
- 主机列表、TOP5 和采集配置页共用同一批量查询内核，`resource_summaries` 显式提交 `node_ids` 与各自所需 `metrics`，不省略指标或逐行补查。列视图、默认分页、指标选择及整批预算沿[列表与总览契约](../docs/spec/addp平台运行观测规范.md#63-趋势时段比较与总览)，不因页面扩列扩大其他消费者读取。
- `/engine-resources` 的固定目录、有效性与历史范围沿[规范 6.1—6.3](../docs/spec/addp平台运行观测规范.md#61-固定指标目录)。模块通过 System 当前 User 裁决并在查询后重核，私有发现投影不授予读取权；不增加手填目标或租户用量归算，失败不保留旧值。来源/查询实链专项沿 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source`。
- `/http-quality` 使用独立七项目录，统计及规则沿[HTTP 质量与规则契约](../docs/spec/addp平台运行观测规范.md#64-http-接口质量分阶段接入)；Go／Python 分别复用 Common `httpmetrics` 与 Common Python 唯一 ASGI 观测内核。统计读取与规则分别授权，当前 User 裁决不由后台投影代替，Tenant 发布服务质量归 Service。能力状态与缺口沿[能力表](../docs/next/ADDP平台运行监控与可观测性设计.md#一当前实现与剩余工作)，固定版本、失败和清理见[HTTP 证据](../docs/spec/testing/evidence/MonitorHTTP质量与采样历史证据.md)；来源 T2 不替代完整正式 backend 登录／页面或正式身份 T4。
- 本地 HTTP 专项使用 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality`，复用专属指标链与规则/outbox 测试库，跳过 PostgreSQL 专业来源探针及后续主机回归；完整回归仍用默认入口。该分项不代表正式身份 T4。
- `/traces` 的 scope、传播、采样预算及因果 Link 沿[Trace 与日志关联契约](../docs/spec/addp平台运行观测规范.md#65-分布式追踪与日志关联二期第一批)。Monitor 使用 `monitor.trace.read` 逐请求核验全部参与实例，关联日志另需 System 正文权限；Link 访问仍须当前实例授权且不递归。采样不能推算全量 QPS，策略由 Monitor 唯一拥有，不新增热编辑控制面。Python 同步 HTTP 复用 Common Python 唯一 ASGI/HTTPX 内核；同步 Trace 与全量 HTTP 质量分别验收，不外推至其他 Runtime、Python 异步或 SQL Span。
- 资源查询策略是 Monitor 平台 CAS 单例，对新请求热生效；资源与规则后台只用最小身份投影，不保存管理员 Token。目标管理、用户观测读取和采集服务权限彼此独立。
- 主机 CPU／内存、服务实例 CPU/RSS 和服务组失联分别沿[主机规则](../docs/spec/addp平台运行观测规范.md#72-主机-cpu-与内存规则)、[实例资源规则](../docs/spec/addp平台运行观测规范.md#服务实例-cpu-与-rss-超限规则)及[服务组失联规则](../docs/spec/addp平台运行观测规范.md#711-服务组失联规则)。参数、CAS、窗口和删除条件由各规则契约维护；停用、未知或缺样不伪造恢复，历史事件保留触发证据。
- 完整挂载身份、阈值、恢复和编辑边界统一见[文件系统容量规则](../docs/spec/addp平台运行观测规范.md#文件系统容量规则)。存储只沿 [EnsureMonitorStore](backend/internal/service/store.go) 前向收敛，替换旧主机／指标唯一索引；新增维度不改写原标量规则及历史信号。真实链路专项沿既有 metrics 入口的 `filesystem-alerts`／`all` 和同一准备／来源／清理内核，现有 CI 分项覆盖；前置检查、SQL 或页面单项通过不代替实链闭环；不操作个人挂载或填充磁盘。
- 挂载行“查看已有容量规则”和“创建容量告警”沿[同一挂载操作契约](../docs/spec/addp平台运行观测规范.md#文件系统容量规则)。查看复用原规则 GET／read 权限，独立于创建／修改权与当前有效采样；服务端在计数／分页前精确定位，非法条件不退回全量。创建、编辑只组合唯一 [ResourceAlertRuleEditor](frontend/src/components/ResourceAlertRuleEditor.vue)，沿模块导航与原权限；草稿仅在组件内，取消、身份或页面上下文变化时清空。
- 平台日志、资源与 HTTP 事件复用唯一事务协调器／outbox；类别订阅、协议和重投沿[平台历史与通知契约](../docs/spec/addp平台运行观测规范.md#73-历史订阅与通知)及[HTTP 通知契约](../docs/spec/addp平台运行观测规范.md#64-http-接口质量分阶段接入)。旧目标不自动扩订，Webhook／SMTP／企业微信不跨协议兜底，人工重投保留原身份和历史时间。
- 平台日志观测只由独立 addp-log-observer 上报有界安全事实；Monitor 不读取接收器宿主文件或上传日志正文。平台操作审计归 System，进程日志、执行事件与审计保留各自事实源。

服务实例详情的 `/platform/container_resource_observations?instance_ids=<单个当前实例>` 沿[容器采样与部署证明](../docs/spec/addp平台运行观测规范.md#61-固定指标目录)、[单实例查询契约](../docs/spec/addp平台运行观测规范.md#62-资源查询与预算)。进程与资源组分开展示，不改变批量摘要、告警或趋势目录；完成时重核投影，不复用过期／变化／共享边界的配额，响应不含私有身份，原生 Mac 无组样本。标准 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=cgroup-source` 只证明 SDK／mTLS／Prometheus 分项，共享夹具不计独立容器完整证明或 Hosted 通过。

## 开发与验证

从仓库根目录使用 `make test-changed` 或明确 Owner 的 `make test-module MODULE=monitor`；快速 Go T1 使用 `make test-module MODULE=monitor GO_ONLY=1`，不替代完整模块门禁。

持久化/真实指标分别沿 `make test-monitor-postgres`、`make test-monitor-metrics`，真实 HTTP Trace、Tempo 与日志关联沿 `make test-monitor-traces`；显式容量基线使用 `make test-monitor-traces MONITOR_TRACES_TEST_ARGS=--capacity`，负载、资源上限及外推判定沿运行观测规范 6.5（仅当次本地设施，不运行 Hosted），前端沿 `make test-monitor-frontend`，共享组件和真实 iframe 按影响扩散至 Common Frontend / Console。System IAM、运行日志及授权/Swagger 变化按规范扩散，不为文档整理重跑无关门禁。API 变更同时执行标准 Swagger 生成与路由覆盖检查。

租户事件的 `TestIntegrationPostgresExecutionEventVisibility` 沿既有 PostgreSQL 服务测试自动发现，覆盖真实 SQL 上的 Owner 范围、历史／本人一次性执行、列表与统计总数、跨租户／类型／子执行隔离、三页事件游标、30 天读取窗口，以及翻页前撤权和 Owner 故障后重新授权。Owner HTTP 为受控协议夹具，仅计 T2，不代表真实 System／Gateway／Worker 的 T4，也不代表事件物理清理或 180 天概览删除已验收。

正式身份的本机执行 Trace／日志闭环使用人工 `make test-monitor-traces-local`，准入、8 CPU／12 GiB、4 GiB 停止线、60 分钟及全部清理沿观测规范“正式身份的本机隔离闭环”。独立构建、独占身份和容器，不借个人服务或 Hosted；静态／守卫回归 `make test-monitor-traces-local-runner` 已聚合到既有 Infra 日志生命周期和 Platform，公共 workflow 禁止调用真实本地入口。

本地数据库先用 `bash scripts/infra/status.sh` 确认实际映射，测试资源与清理只走标准入口。需要运行验收或重启时遵守根 AGENTS.md，由用户在自己的终端执行标准生命周期命令；不接管个人服务，不执行未经单独授权的 keepalive。

## 历史证据导航

[Monitor 历史证据总入口](../docs/spec/testing/evidence/Monitor历史证据.md)保留原记录及旧锚点。能力表引用下列同一批专题，本入口只提供查阅导航，不另行汇总通过状态。下表仅定位已有结果；固定源码、失败／修复、清理和未覆盖项以专题原记录为准，失败、取消、排队或未执行不计通过。本机隔离闭环、Linux T2、受控页面 T3 与正式身份 Hosted T4 分别认定，不合并为当前 HEAD 或生产 T5 通过。

| 主题 | 证据范围 |
| --- | --- |
| [综合验收](../docs/spec/testing/evidence/Monitor综合验收历史证据.md) | 跨主题结论、Metrics 分组、固定版本 Linux 前端与分项失败 |
| [节点与主机资源](../docs/spec/testing/evidence/Monitor节点与主机资源历史证据.md) | 列表／设备／文件系统、趋势、个人 Safari 与实源覆盖 |
| [实例与服务发现](../docs/spec/testing/evidence/Monitor实例与服务发现历史证据.md) | 主机绑定、多 Worker、cgroup、容器证明及部署预期／发现 |
| [告警与规则](../docs/spec/testing/evidence/Monitor告警与规则历史证据.md) | CPU/RSS、事件查询、服务失联、文件系统规则及挂载入口 |
| [Trace 与日志](../docs/spec/testing/evidence/MonitorTrace与日志历史证据.md) | 同步 HTTP、短阶段 Link、容量外推、正式身份本地窗口及历次失败 |
| [HTTP 质量与采样](../docs/spec/testing/evidence/MonitorHTTP质量与采样历史证据.md) | 公共 mTLS、Go／Python 来源、采样／清理交接及限定 Linux T2 |
| [采集来源与引擎指标](../docs/spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md) | 自动接入、采集账号、PostgreSQL 来源 T2 与专业观测 Hosted T4 |
| [执行监控与留存](../docs/spec/testing/evidence/Monitor执行监控与留存历史证据.md) | continuous、Owner 不可用、租户日志、只读维护与过程事件 |
| [部署与前置诊断](../docs/spec/testing/evidence/Monitor部署与前置诊断历史证据.md) | 资源守卫、Docker CLI／daemon 取证及 Compose 超时 |

## 仍需独立证据的范围

- 实际多主机 TOP5、独立 Owner 撤权与全平台容量，不能由六个受控指标来源 T2 或单主机 T4 外推。
- Platform 各项实现状态与剩余覆盖统一由[后续事项能力表](../docs/next/ADDP平台运行监控与可观测性设计.md#一当前实现与剩余工作)维护，宿主、实例资源组和 cAdvisor 支持沿[运行环境边界](../docs/spec/addp平台运行观测规范.md#52-部署与运行环境)。Python 进程指标与采集生效回执仍需独立证据；节点登记不自动证明承载关联。
- 30 天事件物理清理、180 天概览保留目标及只读评估的区别统一见[执行留存与验收](docs/Monitor模块架构说明.md#执行留存与验收)；概览物理删除尚未实施，专业运行时历史及其他 Owner 阶段事件仍须独立接入验收。
- 生产安装权限、物理身份、HA、规模和 T5 均需对应独立验收；历史记录的定位和判定统一见[主题证据导航](#历史证据导航)。
