# ADDP 平台运行监控后续事项

已实现的对象、部署、采集、查询和告警契约迁入[平台运行观测规范](../spec/addp平台运行观测规范.md)。本专题统一维护 Platform 运行观测的能力状态、未实施设计和验收缺口；摘要区分当前实现与实际验收，不将文档整理计为产品验证。模块职责、实现入口和执行监控后续范围分别见[模块契约](../../monitor/CLAUDE.md)及[模块架构说明](../../monitor/docs/Monitor模块架构说明.md)，不在模块入口重复维护本页状态。

## 一、当前实现与剩余工作

| 能力 | 当前实现与验证边界 |
| --- | --- |
| 可选观测基础设施 | 日志、指标独立启停，故障与业务就绪分开。已有标准生命周期和指标链路验证，生产安装、升级和容量 T5 待验。[综合证据][monitor-summary] |
| 主机与来源 | System 管主机、筛选和纳管，Monitor 管可选来源及采集结果。已有 Linux VM 单新 Monitor 绑定验证，原生 macOS 与多实例仍待补。批量运维未开放。[清单契约][monitor-admission] · [实例证据][monitor-instance] |
| 主机列表、资源与趋势 | 26 项固定指标，支持核心资源／负载与连接、按需设备与挂载展开、热力面板和趋势。请求范围及图表交互归[指标目录][monitor-catalog]和[平台展示规范][monitor-presentation]。设备展开、固定 Linux 前端和 Safari 各自认定。父 Job 取消、实源摘要 T2 前置失败及真实挂载页面缺口见[节点证据][monitor-host]。 |
| 磁盘与卷容量 | 默认关闭，提供即时设备表，无物理总量汇总。Desktop VM 来源 T2 和受控页面 T3 已验，Linux 原生来源及新增正式页面未覆盖。[首期范围](#磁盘与卷容量首期) · [节点证据][monitor-host] |
| 服务状态与资源 | 当前租约、进程资源、趋势和有效主机导航已接入。Meta、Quality、Transfer 的双 Worker 入口按实际实例隔离，Transfer continuous 不共享固定 Owner 身份。多 Worker 本机实源、页面 T3 和待验的 Hosted 分开。[实例证据][monitor-instance] |
| 资源总览与时段比较 | 主机 TOP5 和覆盖率有单真实主机 Hosted 与受控多源证据，多真实主机总览待验。主机及进程相邻时段比较已实现，进程正式身份链路、1 小时／24 小时真实长窗待验，同比未开放。[平台展示规范][monitor-presentation] · [综合证据][monitor-summary] · [进程比较](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#服务进程-cpurss-相邻时段比较本地) |
| 平台告警 | Monitor 统一事件、规则、确认、抑制、目标和投递，System 保留日志诊断。检索与排序、CPU／内存、挂载容量、实例 CPU／RSS、服务组失联沿[正式规范](../spec/addp平台运行观测规范.md#七平台告警与通知)。主机实压限测试阈值与单台 VM，服务组真实租约失联 Hosted 仍缺通过证据。[告警证据][monitor-alert] · [主机规则证据][monitor-summary] |
| 引擎及容器专业观测 | Platform 自管 PostgreSQL／Redis／MinIO 单容器部署可登记并关联主机，专业指标沿固定目录。正式 PostgreSQL T4、Linux 来源 T2、Redis Hosted 失败、MinIO 未验和个人 Safari 各自认定。容器配额须部署证明及完整限制层级。[引擎证据][monitor-engine] · [容器证据][monitor-instance] |
| HTTP 接口质量 | 共用全量采集、固定窗口与排行、慢接口和告警。Go／Python 来源及前置失败按固定版本记录。正式 Agent／Copilot 登录页面、Hosted、容量、长期保留仍分别核验。Tenant 数据服务质量归 Service。[HTTP 证据][monitor-http] |
| Trace 与日志 | Go 与 Agent／Copilot Python 同步 HTTP、指定执行短阶段及 Link 已接入。本机第十窗口闭环通过，Link 目标未保留的 404 仍守原范围。Hosted／专用 Runner T4、真实 72 小时留存、其他 Runtime、continuous、Python 异步和 SQL Span 未由此验收。[Trace 证据][monitor-trace] |
| 暂缓能力 | 健康评分、功能热度及泄漏诊断先明确事实与口径。SSH、环境初始化、防火墙、批量启停及扩容还须独立权限、计划、执行与审计。 |

操作细节见[平台展示规范][monitor-presentation]和[告警入口与筛选](../spec/addp平台运行观测规范.md#71-领域入口与权限)。同上下文原位刷新、对象事实变化清值、就近提示和共享自动筛选的测试按[主机][monitor-host]、[实例][monitor-instance]、[引擎][monitor-engine]及[告警][monitor-alert]的证据认定。个人页面的读取结果与受控测试对请求在途状态的证明分别保留。

## 二、后续开发顺序

| 优先级 | 下一步 | 进入条件 |
| --- | --- | --- |
| 1 | 补齐原生宿主与多进程部署覆盖 | Linux VM 单新 Monitor 绑定已验；原生宿主与 Desktop VM 分别声明，多进程按各自实例身份验证，不在线改挂既有实例 |
| 2 | 补齐原功能表的服务质量、引擎专业指标及容器资源 | PostgreSQL 已实现及已验范围见[采集来源与引擎指标证据][monitor-engine]；其他引擎、cAdvisor 节点部署／容器视图及实例资源组后续扩展，按[运行环境边界](../spec/addp平台运行观测规范.md#52-部署与运行环境)分别推进；先明确对象归属、配额和 OS 支持，复用固定目录与唯一采集链 |
| 3 | Trace 长时保留验证与后续覆盖 | 原三 Go HTTP 链短测仅支持原 20 GiB／72 小时外推；真实过期删除、长期合并增长及查询余量另定窗口，不沿用原四十分钟短测或本地闭环授权。各批已验范围见[Trace 证据][monitor-trace]；正式 Runner T4、其他 Runtime、continuous、Python 异步、SQL Span 与真实长时窗口分别确认 |
| 4 | 生产发布及受控运维 | 真实平台 T5；SSH/初始化/防火墙/启停另有权限、计划、执行与审计确认 |

Trace 中 request_id、trace_id、execution 身份分别表达请求、调用链和业务执行；异步任务按运行观测规范第三批明确持久关联并创建短阶段独立 Trace，不能把长任务硬挂在创建请求下。慢请求保留须证明实际采样策略，随机头部采样不能保证保留全部慢请求。

受控运维中 ADDP 管理目标、计划、权限与审计，成熟执行设施实施变更。主机登记不默认保存 SSH 密码，采集身份不具备 Shell 权限；Monitor 不提供任意命令执行器，租户 Orchestrator 任务不隐含获得平台主机操作权。

### CDC 任务与运行资源监控

Tenant 任务页读取 Transfer 当前任务、捕获代次和授权执行。Platform 页读取 Connect／Debezium 与 Redpanda 共享组件的当前运行段。Infra PostgreSQL 通用资源仍归引擎监控，任务源端由任务配置确定。身份、字段、查询与采集边界归[运行观测规范](../spec/addp平台运行观测规范.md#cdc-基础设施组件的自动发现)和[Transfer CDC 契约](../spec/tasks/Transfer数据库CDC.md)。

| 当前能力 | 固定验收与缺口 |
| --- | --- |
| 任务级观测及页面 | API、原生对照及暂停恢复已有 Hosted。固定 `88416bf8a` 的 [Hosted 43357940010](https://github.com/pampa0629/addp/actions/runs/43357940010)完成三阶段真实页面，积压 0→3→0。[原链路](../spec/testing/evidence/Monitor执行监控与留存历史证据.md#2026-11-13-basic-与-deep-串接及-cdc-hosted-闭环) · [页面断言与清理](../spec/testing/evidence/Monitor执行监控与留存历史证据.md#2026-11-13-cdc-任务卡片的-hosted-三阶段断言) |
| PostgreSQL 任务风险与持续告警 | 固定 `668187692` 的 [Hosted 43610830710](https://github.com/pampa0629/addp/actions/runs/43610830710)已验源槽／缓冲恢复范围丢失、捕获失败、消费停滞、活动延迟与可选积压的实际故障。控制与观测版本写入分工、阈值、签名通知、后继重建和清理见[分项证据](../spec/testing/evidence/Monitor告警与规则历史证据.md#可选积压告警与后继-cdc-完整-hosted-通过2026-11-15)。 |
| 任务历史趋势 | Transfer 按现行七天安全历史契约保存分钟观测，按 Tenant／任务身份／generation 授权，缺样为断点。schema 15 已启用，本机 task61 的六个分钟样本已只读核对。固定 `c63888fba` 的 [Hosted 43668756347](../spec/testing/evidence/Monitor执行监控与留存历史证据.md#七天-cdc-任务历史的真实-hosted-断言通过2026-11-15)验证分钟记录。固定 `419548d39` 的 [Hosted 43675672486](../spec/testing/evidence/Monitor执行监控与留存历史证据.md#cdc-历史图表同轮实故障-hosted-通过2026-11-15)验证真实图表与 API 一致、缺样及悬浮。个人 Chrome、七天自然留存和长期容量待验。[任务历史契约](../spec/tasks/Transfer数据库CDC.md#cdc-任务指标历史2026-11-15-已确认) |
| 触发前暂停及评估器重建 | 固定 `7e583307e` 的 [Hosted 43895444567](../spec/testing/evidence/Monitor告警与规则历史证据.md#cdc-evaluator-rebuild-hosted-20261117)完成独占 Monitor 重建、当次暂停评估及 Resume 后完整新窗口。旧时限失败、提前撤销及未知操作结果在证据中保留。 |
| 租户告警页面 | 固定 `b8593c1e7` 的 [Hosted 44007380675](../spec/testing/evidence/Monitor告警与规则历史证据.md#cdc-tenant-alert-ui-hosted-20261117)覆盖三条性能规则，固定 `755d24f32` 的 [Hosted 44033277901](../spec/testing/evidence/Monitor告警与规则历史证据.md#cdc-risk-alert-ui-hosted-20261117)另覆盖捕获持续失败、源槽与消息恢复范围丢失。保持、关闭、通知实收与清理分别沿各轮证据。 |
| 其他 Provider 与组件规则 | MySQL 的 binlog／GTID、Oracle 的 SCN／日志保留与各自捕获延迟按实际任务需求再做。已有 CDC 能力与部分 Owner 原生观测不足以证明 Monitor 专业展示。Platform 共享组件持续告警、长期容量／保留和生产 T5 待验。 |

**共享组件来源的实测范围**

- **实源 T2**。固定 `bbf3e453c` Linux Monitor Metrics T2 含 `cdc-native` 与 `target-budget`。独占 PostgreSQL 15、Redpanda 24.3.18、Debezium Connect 3.6.0.Final，验证真实 WAL 积压 0→1→0、Task 失败、mTLS 缺失或错误客户端身份拒绝、连接失败后的 503 与恢复、受控 HTTP SD 的 2→1→2 和零残留。真实 Prometheus 离线求值的 126 个场景覆盖有效零、缺字段、重复 up、过期、错次、重启及负延迟，初次临时目录不可写修复后才通过。各门禁归[Infra CDC 隔离验证](../../scripts/infra/docs/指标中心与来源.md#cdc-隔离验证)。
- **容量与页面**。受控 34 个目标加入两真实来源至 36 个，Connect 停止时保留 35 个，恢复回 36 个，第 37 个被拒。每档约 80 秒的峰值为 146,350,080 字节，最大查询 0.0841 秒、抓取 0.0087 秒。真实组件与受控身份夹具范围分开，生产满载、七天留存及正式 Platform 页面 Hosted 待验。早期部署级 PG 下拉已由任务关联视图替换，早期受控页面不外推当前入口。
- **个人来源**。2026-11-11 在用户应用配置后核对 System 容器身份、实际 mTLS 及两次间隔 20 秒的 36 个新鲜目标。Connector／Task 及消费组分区为零，无样本的积压、延迟留空。即时和趋势分别返回 200，回执 ID 为 `b318f164-593d-4292-ba5b-5b45cf71e97a`、`3bd3b31c-c67b-495e-909c-b04f9322db0d`。控制台连接超时，个人页面视觉实操未完成。
- **门禁历史**。旧 Console 路由夹具、Infra 停服 provider 登记及 System 投影精确断言曾失败。Monitor 完整前端复验 145 项 T1、793 项浏览器及构建通过，固定 `5660f9df2` 自然前端 CI 通过。固定 `2e1d54946` 的 System Go 和 IAM PostgreSQL 已验 unavailable 空投影。CDC 辅助镜像补所有权后登记检查通过，原 Platform 失败与配置夹具 ps 超时仍各自认定。

固定采集字段参考[Redpanda 24.3 指标](https://docs.redpanda.com/streaming/24.3/reference/public-metrics-reference/) · [Kafka Connect 指标](https://kafka.apache.org/38/operations/monitoring/#connect_monitoring) · [Debezium PostgreSQL 指标](https://debezium.io/documentation/reference/connectors/postgresql.html#postgresql-monitoring) · [Redpanda 积压口径](https://docs.redpanda.com/streaming/24.3/manage/monitoring/#consumer-group-lag)。

## 三、验收状态与剩余缺口

固定源码、运行链接、采样数量、预算、失败／修复和清理结论由下列历史证据专题维护；本页只保留能力状态、设计约束和剩余缺口。后续限定复验通过不倒改早期失败，也不证明当前 HEAD 或父 workflow 整体通过。

| 主题 | 权威记录与阅读范围 |
| --- | --- |
| [综合验收][monitor-summary] | 跨主题结论、Metrics 分组；固定 `9bf8dddcb` Linux 前端及 `f09c1ecf9` 分组结果 |
| [节点与主机资源][monitor-host] | 列表、趋势、个人 Safari 与实源边界；固定 `e5391ca36` 文件系统展开 |
| [实例与服务发现][monitor-instance] | 主机绑定、多 Worker、容器配额与证明、部署预期及发现失败 |
| [告警与规则][monitor-alert] | CPU/RSS、事件检索与排序、服务失联、文件系统及规则入口 |
| [Trace 与日志][monitor-trace] | 同步 HTTP、短阶段 Link、容量外推及本地窗口；第十窗口固定 `47b02db01`，保留首尝试失败 |
| [HTTP 质量与采样][monitor-http] | 公共 mTLS、Go／Python 来源、采样／清理交接；固定 `fa7ae6dc3` Linux T2 与旧失败分别认定 |
| [采集来源与引擎指标][monitor-engine] | 自动接入、采集账号、来源准入；专业 Hosted `b08f2331d` 与来源 T2 `5d91fa31c` 为不同版本证据 |
| [执行监控与留存][monitor-execution] | continuous、Owner 不可用、租户日志、只读维护与事件分页 |
| [部署与前置诊断][monitor-diagnostics] | 资源守卫、Docker CLI／daemon 取证及 Compose 超时 |

[原记录入口及旧锚点](../spec/testing/evidence/Monitor历史证据.md#原记录入口)继续保留；新增证据写入对应专题，本页通过链接更新状态，不复制整份报告。

原生 macOS、多进程、新增引擎主机导航的正式身份验收、其他引擎与集群承载、总览多真实主机、长时段、进程比较的真实运行验收及生产 T5 等缺口按[当前实现与剩余工作](#一当前实现与剩余工作)维护，不能由单台 Linux VM 或单个新 Monitor 进程的结果外推。

聚合门禁须核对对应源码与实际执行项；连接预检未通过、排队、路径跳过和旧批次结果均不计为通过，分项结果只证明各自范围。正式测试产物由现有 CI/验收报告保存，本文不追加临时失败日志。


## 四、验证与维护

验证采用[正式规范的标准门禁与 Hosted 边界](../spec/addp平台运行观测规范.md#九验证与交付)，按改动范围核对已有结果；既有授权窗口不因文档更新自动延续。能力契约归正式规范，完整验收记录归历史证据专题，本页维护状态、缺口与导航，不以文档更新代替产品测试或真实验收。

## 五、服务承载清单接入边界

服务承载清单的唯一 System 编排入口、可选采集的 Console 内存交接及两 Owner 独立失败处理，统一见[正式规范 4.2](../spec/addp平台运行观测规范.md#42-管理员确认的部署清单接入)。接入参数只用于实际宿主上的新进程，不修改全局 `.env`，不自动重启或在线改挂既有实例；原生 macOS 与 Desktop VM 分别声明。

Linux VM 单新 Monitor 绑定的固定版本、失败和覆盖限制见[清单接入与实例绑定证据](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#清单接入与实例绑定)。不据此证明原生宿主或其他进程的承载关联，剩余覆盖见前文开发顺序。

## 六、Business 引擎接入设计

### 已确认的自带部署自动接入方向

2026-10-19 已确认 Infra 与平台自管 Business 部署属于 Platform 运维对象，租户连接、凭据、数据与任务继续按 Tenant 授权。持续发现、候选读取、管理员批量确认及来源更新已接通，分别由[正式规范 4.4](../spec/addp平台运行观测规范.md#44-平台引擎部署登记)、[4.5](../spec/addp平台运行观测规范.md#45-部署侧自动发现来源)维护；不再将原只读基座或批量接入列为待实现步骤。普通操作无需导出／导入文件，旧文件导入、原始来源单项创建及旧协议读取路线已删除，前向迁移保留稳定部署身份与不可变历史。

部署来源、管理员声明、运行可用性和采集结果分别表达：首次主机关联及扩大纳管须确认，容器重建只更新来源，不迁移主机或扩大租户权。部署观测由既有生命周期管理，只执行固定只读 Docker 命令，不把 Docker 权限交给 System Web，不读取容器环境、凭据或业务内容。发现失败清除有效候选，停服不删除已确认声明；观测故障不阻断业务启动，退出仍须核验自有观测进程清理。

平台运维范围按实际运维责任确定。平台自管 Business 的部署和整体资源属于平台运维；租户自行部署的外部服务默认不纳管，委托监控须另有显式授权。平台角色不能因运维关联读取租户连接凭据、表、SQL 或执行结果，共享主机／引擎整体消耗不归算为单一租户用量。

纳管主机、实际部署与 System Engine Instance 分别定位；同一底层部署可对应多个租户登记，不能按 engine_id 重复累计物理消耗。承载身份和有效时间由 System 唯一裁决，Monitor 只消费授权投影，不按 IP、容器名或共享 VM 猜测关联。首批只开放 Infra／Business 两个单容器 PostgreSQL；其他服务仅展示发现事实与未适配原因。多服务／集群、云服务承载、租户 Engine 关联及租户专业监控／用量归属仍未开放。集群可能跨多主机，云服务可允许主机未知，不能由首批单主机模式外推已支持。租户登记与部署的关联须经服务端核验；平台维护同一底层故障事件，租户只读取获授权的影响提示，该租户链路不在本批开放范围。

自动接入的正式身份、批量失败／回滚、页面重读与退出清理范围统一见[自管引擎接入场景](../spec/testing/scenarios/平台观测与语义生命周期.md#自管引擎部署的自动接入)。固定 `b3dcae5a3` 的 T4、先前失败与修复见[自动接入证据](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#infra与business-postgresql-自动接入)；旧手工登记验收不代替自动链路，该接入结果也不证明专业指标采集。

### MinIO 专业监控首期（2026-11-02 已确认，分阶段实施）

固定版本 v3、两个独立 `admin:Prometheus` 账号及 17 项指标已接入发现、查询与页面。源端权限与产品输出各自受限，缺项不补零。真实来源 T2、受控页面及个人 Safari 已有分层证据。全指标、七天趋势与正式身份 Hosted 待验。[权限与指标契约](../spec/addp平台运行观测规范.md#minio-专业观测首期边界2026-11-02-已确认) · [历史证据](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#minio-source-history)

### Redis 专业指标实施状态

Infra／Business 单机 Redis 7 已接通独立最小 ACL、mTLS 来源、22 项固定查询及分组页面。发布迁移和来源／页面分层证据归历史专题。当前读取、合法缺项及历史趋势分开判断。[正式契约](../spec/addp平台运行观测规范.md#redis-专业观测首期边界2026-11-01-确认分阶段实施) · [来源与页面证据](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#redis-source-history)

### Redis 正式全链路验收（2026-11-01 已确认，本地验证）

`redis-engine-admission` 的授权范围与真实链路归正式场景，仍限两域当次纳管对象、真实密码／MFA、当前 User 查询和 Console 页面。首轮 Hosted 在采样等待阶段超时，未执行专业页面、Business 停采恢复及 Infra 持续采集。业务清理失败，基础设施零残留独立认定。验收器修复有本地回归，尚缺修复后的正式通过结果。[正式场景](../spec/testing/scenarios/平台观测与语义生命周期.md#redis-自管接入专业观测与页面) · [失败及修复](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#redis-hosted-首轮失败与验收脚本修复2026-11-07) · [T4 身份边界](../spec/testing/T4-Online验收规范.md#开关身份与拓扑预检)

### PostgreSQL 专业指标实施状态

独立部署账号、固定函数与配对来源已接入。最小权限、标准启停与 Business 认证目录归[4.6](../spec/addp平台运行观测规范.md#46-postgresql-数据库侧汇总读取边界)和[4.7](../spec/addp平台运行观测规范.md#47-postgresql-受保护的固定指标来源)。个人实例须由部署方实际应用后核对采样。

### 后续自动接入的最小身份投影（已确认）

System 私有身份投影及 Common 完整性校验已接通。后台来源读取、管理写权限和当前 User 读取分开，普通浏览器 DTO 不含私有端点。[身份与权限](../spec/addp平台运行观测规范.md#三对象与模块边界) · [部署侧发现](../spec/addp平台运行观测规范.md#45-部署侧自动发现来源)

### PostgreSQL 专业发现、查询与展示方案

固定目录、当前 User 授权与当前数据库容器段趋势分别见[6.1][monitor-catalog]、[6.2](../spec/addp平台运行观测规范.md#62-资源查询与预算)及[6.3][monitor-presentation]。退出容器历史及跨段拼接仍须另定保留规则。固定 `b08f2331d` 专业 T4 与 `5d91fa31c` 来源 Linux T2 各自认定，后加主机导航、七天自然留存、生产 T5 未覆盖。[正式场景](../spec/testing/scenarios/平台观测与语义生命周期.md#postgresql-专业观测与页面) · [专业 T4](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#postgresql-专业观测正式-hosted-验收) · [来源 T2](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#postgresql-专业来源固定版本-linux-t2-复核2026-10-24)

### PostgreSQL 来源恢复首期范围（已确认）

完整启动只恢复安装证明匹配的同容器来源。健康来源保持，账号／权限／证书或容器身份变化由部署者核对后显式应用。[恢复契约](../spec/addp平台运行观测规范.md#47-postgresql-受保护的固定指标来源)。独占双域恢复已有实源证据，早期 Desktop 挂载读取失败和后续 Lima／本机复验各自保留，后者不关闭共享目录上游根因，也不证明无人值守恢复。[恢复历史](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#完整开发启动的-postgresql-来源恢复2026-11-01) · [目录取证](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#pg-directory-history)

## TCP 当前连接数

已按 2026-10-23 确认范围接入主机固定目录、即时卡片及趋势，口径、整数边界与来源要求统一见[正式规范 6.1](../spec/addp平台运行观测规范.md#61-固定指标目录)。宿主网络命名空间不可读或受限 Desktop 未开放 netstat 时显示无数据，不用容器统计或零值代替。本地隔离 T2、受控 HTTP 页面及固定输入版本见[TCP 证据](../spec/testing/evidence/Monitor节点与主机资源历史证据.md#主机-tcp-当前连接数本地)，不外推原生 Linux 正式身份页面或生产验收。

同日已确认 `platform-node-metrics` 的 TCP 限定用途，只增加当次节点即时值、趋势及同一真实 Console 页面的读取。密码／MFA、独立 Prometheus 身份、节点范围、预算与零残留沿[节点控制面场景](../spec/testing/scenarios/平台观测与语义生命周期.md#节点控制面的隔离身份例外)，精确十项标量收据及拒绝要求沿[收据核对](../spec/testing/scenarios/平台观测与语义生命周期.md#清单接入后的真实进程宿主绑定)。标准入口及拒绝检查已有本地证据；真实身份／来源 T4 仍待专门验收会话回传，不读取租户数据或扩大已确认用途。

## 服务实例配额与容器利用率（部署侧证明方式已确认）

2026-10-24 只读核对时，本机 Monitor、Meta／Transfer／Quality Worker 由标准开发入口作为 macOS 原生进程运行，未施加独立 CPU／内存配额。根 Compose 中 Meta／Transfer Worker 的限额示例仍是注释；Business 引擎容器的限额不属于这些原生进程。**开发方式保持原生**，不能为了展示配额把 Worker 改为容器。`GOMAXPROCS` 是 Go 并行度，`GOMEMLIMIT` 是 Go 运行时软限制，均不能替代操作系统硬上限，见 [Go runtime](https://pkg.go.dev/runtime#GOMAXPROCS)及[Go 内存软限制](https://pkg.go.dev/runtime/debug#SetMemoryLimit)。

2026-10-24 已确认的首期范围为**明确一实例一独立 Linux 容器**的运维观测：在实例详情分开显示进程 CPU／RSS 与容器整体用量／硬上界，不扩展批量列表的读取量、默认分页、告警或趋势。无明确配额不生成配额利用率，不用宿主总量补实例分母，不把进程 RSS 当作容器整体用量，不将超限等同泄漏；硬上界也不表示独享或保证可用容量。指标及计算条件统一见[正式规范 6.1](../spec/addp平台运行观测规范.md#61-固定指标目录)，单实例读取范围、固定目录与预算见[6.2](../spec/addp平台运行观测规范.md#62-资源查询与预算)。

原生开发不自动配置额度或修改部署限制；macOS 不生成组用量或有效零值，详情就近解释未接入，不铺四张空卡片。采样、完整部署证明、身份关联及正式身份验收分别判定，既有 CPU／RSS 验收用途不自动扩展为容器读取。

### 已确定：部署侧提供容器关联与完整限制层级

引擎发现与应用容器证明是两个事实来源，`infra`／`business` 引擎来源不能推断 ADDP Backend／Worker 所在容器。2026-10-24 已确认由**部署侧**发布有期限的只读证明，Common 仅采自身 cgroup，System 沿唯一私有投影精确关联当前实例，Monitor 按当前 User 的实例读取权限查询并在完成时重核。私有投影不代替用户授权，不改变 IAM、主机关联或普通实例 DTO；响应不含容器 ID、端点、内核身份或文件路径；应用不持有 Docker socket、不开放宿主 cgroup 层级、不扫描其他进程，不读取环境凭据或业务命令，也不修改现有容器命名空间／挂载配置或保留第二套降级路线。身份匹配、证明发布范围／有效期、指标口径及重核规则统一由[正式规范 6.1](../spec/addp平台运行观测规范.md#61-固定指标目录)维护；Platform User、Permission、System 读取裁决及查询预算统一见[6.2](../spec/addp平台运行观测规范.md#62-资源查询与预算)。

完整限制层级是有效证明的前提：父级约束仍作用于子级，cgroup 命名空间与子树挂载可能隐藏祖先；容器内有限的 `cpu.max`／`memory.max` 或 Docker inspect 配置额度均不能单独证明真实完整层级，见 [cgroup v2 层级规则](https://docs.kernel.org/admin-guide/cgroup-v2.html)及 [cgroup 命名空间](https://man7.org/linux/man-pages/man7/cgroup_namespaces.7.html)。共享容器、父级不可见、权限不足或 Docker Desktop 等不支持来源不提供有效证明；不通过提升应用权限或在业务容器内执行命令绕过。无有效证明时仅可沿当前声明边界读取绝对用量，无有限硬上界时不计算对应利用率，具体判定沿正式规范 6.1、6.2。

验证沿 Common 跨平台采样／固定文件解析及 Common／System／Monitor Go T1、部署发现 T1、System 精确身份／投影的进程登记 PostgreSQL T2、既有 cgroup-source Prometheus T2、前端单位／缺配额／共享边界拒绝 T1/T3，以及双模块 Swagger 生成／覆盖；按受影响 owner 扩散，同步 API、Swagger 与 CI 路径登记。Linux T2 仅用标准一次性受限设施，不修改个人资源限制。

固定版本、失败与修复结论及各层覆盖范围见[实例与服务发现证据][monitor-instance]。共享组 SDK T2 与受控页面 T3 不证明独立 Linux 容器的真实完整限制层级、正式身份的配额查询／页面或 Hosted T4；完整实机及正式身份 Hosted 仍待专门验收会话回传，未执行项不计通过。


## 服务同用途副本失联告警：已确认目标与部署事实来源

2026-10-25 已确认并接通同模块／角色／用途的全副本失联规则、持久运行确认、事件评估与页面。规则权限、当前 User 裁决、窗口及通知统一由[正式规范 7.1.1](../spec/addp平台运行观测规范.md#711-服务组失联规则)维护；首期只表达 System 心跳／租约失联，单副本离线不等于全组失联，也不证明进程退出或业务不可用。

部署预期与分组事实、用途变更、标准开发／生产入口和发布失败边界统一见[正式规范 4.8](../spec/addp平台运行观测规范.md#48-服务部署预期与分组租约观测)。当前用途覆盖 Meta、Transfer、Quality 的正式 Worker，Security Worker 不在本批范围；不增加 Monitor 手工维护、远程 Shell、自动启停或扩缩容。停止前发布失败须在任何停止动作前退出，启动发布故障沿可选观测隔离，文件发布不证明实际启停成功。

首次未确认保持“等待确认运行”，未知和计划停用不伪造恢复；资格重建及恢复条件沿规范 7.1.1，不在后续事项中重复维护。首次启动失败仍须另有部署结果事实，不在本批判断范围。

验证入口保持 `make test-dev-lifecycle`、Common／System／Monitor Go 门禁、System 分组同快照 PostgreSQL 门禁、`make test-monitor-postgres` 及既有前端门禁，沿既有自动发现及 Metrics 输入登记。发布协议、停止屏障与微秒标记的分批结果见[部署预期协议](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#2026-10-25-服务组部署预期协议与只读租约汇总自动接入未完成)、[标准启停接入](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#2026-10-25-标准启停接通服务组预期与停止前发布屏障)及[变更标记消歧](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#2026-10-25-确认运行前置组预期变更标记消歧)；规则、受控时钟、数据库及页面分层结果见[规则与持久确认](../spec/testing/evidence/Monitor告警与规则历史证据.md#2026-10-25-服务组失联规则与持久确认)。各条证据的固定版本、失败与后续修复分别保留，不将前期未完成状态或无关聚合失败改写为当前整体结论。

[重启后的只读核对](../spec/testing/evidence/Monitor告警与规则历史证据.md#重启后的个人环境只读核对2026-10-25)记录构建版本及其包含的实现提交；[Safari 保存复验](../spec/testing/evidence/Monitor告警与规则历史证据.md#safari服务失联规则保存复验2026-10-26)仅证明当时保存、编辑启用、持久重读及在线评估，最终保留一条启用的临时规则，先前显示差异根因仍未定。受控投影／时钟 T2、受控 HTTP T3 或个人在线页面均不代替真实租约失联／恢复、实际通知送达或隔离正式身份 Hosted；真实故障闭环、页面与零残留范围见下节，CI／Hosted 仍待专门会话回传，未执行不计通过，不设置定时任务。

### 服务组失联的真实租约验收范围（2026-10-26已确认）

2026-10-26 已确认沿既有 `platform-node-metrics` 的 `service-availability` 专项验收 Meta 同类双 Worker。临时平台管理员的限定用途、正式 Bootstrap／密码 MFA、进程归属与 Linux pidfd 核验、真实租约故障及通知断言、1800 秒场景／60 分钟 Job 预算与全退出清理，统一见[现行测试场景](../spec/testing/scenarios/平台观测与语义生命周期.md#服务组失联恢复与通知)。标准停止会声明计划停用，不能作为异常失联证据；不触及个人规则或 Worker。

入口接入、PID 归属与恢复保护、本地 runner 回归及历史失败见[专项准备证据](../spec/testing/evidence/Monitor告警与规则历史证据.md#服务失联hosted专项准备与本地runner验证2026-10-26)。T4 场景例外、runner 与 CI 登记已接通，不再列作待实施步骤；本地回归仍不证明实际 pidfd／真实租约故障注入、正式页面或 Hosted 通过。真实故障闭环与零残留仍待专门验收会话安排及结果回传，未执行或清理不完整均不计通过，本会话不触发或设置定时任务。

### 首次启动失败可见（首期范围已确认）

首期接入标准开发启动中的 Go Backend。启动入口能定位具体模块的构建失败、启动进程退出和就绪超时；只发布能归到本次尝试和具体模块的结果。生产 Compose 整批等待失败、Worker 尚未登记、依赖提前失败导致某服务未执行、启动脚本被中断，均不足以判定该服务启动失败。

部署入口发布结果，System 核验归属及新鲜度后提供只读事实。服务监控沿现有平台读取权限单独展示有证据的启动失败，保留现有实例列表、分页和租约状态。首次未登记的失败也可见，无需构造实例。只展示模块、时间和固定原因；日志正文、命令、路径和凭据沿原有诊断边界。新尝试、明确的计划停用及已确认运行使旧失败退出当前视图，其他模块的启动不清除该条结果。无法读取或匹配结果时继续显示原有状态。全量启动若使 Console、Gateway 或 System 不可用，仍由终端报告失败。

启动结果用于诊断展示。运行确认仍由 System 租约判断，既有服务组失联告警不使用启动结果。验证沿 `make test-dev-lifecycle`、`make test-deployment-discovery`、System Go／Swagger 及 Monitor 页面门禁，覆盖新旧尝试隔离、局部启停、未执行与真实失败的区分、尚无实例的展示和拒绝身份。

标准开发启动的就绪屏障覆盖 System 及十八个 Go Backend。System 前置等待，其余模块并行等待各自的 `/health/ready`，复用运行进程也须检查。共用等待函数沿现有预算发布失败原因，任一失败阻止后续 Runtime、Gateway 和前端阶段。Manager 专用存活检查沿既有契约执行。

代码已接通。System 回归及 Swagger 覆盖通过，受控浏览器核对缺权不读取。[真实 PostgreSQL 回归](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#开发-backend-启动失败与重试的-postgresql-t22026-11-07)覆盖失败、重试、登记和注销。隔离 Linux 中的真实 Console 验收分别覆盖 [Standard 构建失败](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#开发-backend-真实构建失败与重试的隔离运行验收2026-11-07)和 [Catalog 进程退出、就绪超时](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#开发-backend-进程退出与就绪超时的隔离运行验收2026-11-07)，核对无实例时可见、重试登记在线及失败提示清除。Go Backend 就绪检查的终端提示复用同一次 PID 判定，分别输出「启动进程退出」或「等待就绪超时」，与保存的失败原因一致。


共用后端就绪等待已补连接／响应的剩余预算限制，以及复用 System 前的统一预算初始化，见[本地修复证据](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#backend-就绪等待的总预算与复用进程初始化修复2026-11-08)。该修复不增加失败状态或替代真实 Hosted。

Standard 持续未就绪与标准重试恢复另已接入现有 Hosted 的 `standard-startup` 分支，限定用途、故障归属、两阶段 Console 证据及清理规则统一见[验收场景](../spec/testing/scenarios/平台观测与语义生命周期.md#standard-开发-backend-启动失败与重试恢复)。当前为实现与本地验证，真实 Hosted 由专门会话执行，不以旧源码的隔离证据代替新场景通过。

## 文件系统容量告警首期

2026-10-27 已接通完整挂载维度的容量告警及挂载行“创建容量告警／查看已有容量规则”。阈值、未知与永久卸载、唯一编辑器、权限和可恢复筛选统一由[文件系统容量规则](../spec/addp平台运行观测规范.md#文件系统容量规则)维护。查看不要求创建／修改权限或当前有效采样；创建须重核当前有效挂载并默认停用，取消、明确 Owner 拒绝或身份变化清空未保存草稿。

规则、持久化和实源前置失败见[首期分层证据](../spec/testing/evidence/Monitor告警与规则历史证据.md#文件系统容量告警首期2026-10-27)；挂载行操作分别见[创建入口](../spec/testing/evidence/Monitor告警与规则历史证据.md#主机挂载行创建容量告警入口2026-10-27)和[查看入口](../spec/testing/evidence/Monitor告警与规则历史证据.md#主机挂载行查看已有容量规则入口2026-10-27)。缺样断言修复的固定 `7ebf98370` 受控 HTTP 前端 CI 及旧失败见[完整 CI 记录](../spec/testing/evidence/Monitor告警与规则历史证据.md#挂载缺样断言修复的完整ci闭环2026-10-28)，不据此宣布其他门禁、父 workflow 或当前 HEAD 全通过。

2026-10-28已补本地受控源经真实Prometheus／SQL／签名HTTP的容量告警触发、缺样保留及同一事件恢复T2，范围及前置失败分别见[限定复验证据](../spec/testing/evidence/Monitor告警与规则历史证据.md#文件系统容量告警受控源实链t2复验2026-10-28)；未据此认定真实Collector、原生主机压力或正式身份通过。个人 Safari 的隐藏页、右侧列／前台刷新及真实 Linux 文件系统挂载行仍未覆盖，范围见[个人前置核对](../spec/testing/evidence/Monitor告警与规则历史证据.md#挂载规则入口的个人safari前置核对2026-10-27)及上述 CI 记录。T1／数据库 T2／受控 HTTP T3 不代替真实来源、正式身份 Hosted 或零残留验收；后续实链结果等专门会话回传，不设置定时任务。

[monitor-admission]: ../spec/addp平台运行观测规范.md#42-管理员确认的部署清单接入
[monitor-catalog]: ../spec/addp平台运行观测规范.md#61-固定指标目录
[monitor-presentation]: ../spec/addp平台运行观测规范.md#63-趋势时段比较与总览
[monitor-fixed-frontend]: ../spec/testing/evidence/Monitor综合验收历史证据.md#五项列表热力面板与服务环比的-linux-前端复核2026-10-24
[monitor-core-summary]: ../spec/testing/evidence/Monitor节点与主机资源历史证据.md#主机列表五项核心摘要本地
[monitor-device-expansion]: ../spec/testing/evidence/Monitor节点与主机资源历史证据.md#主机列表按设备展开2026-10-26
[monitor-load-view]: ../spec/testing/evidence/Monitor节点与主机资源历史证据.md#主机列表负载与连接视图2026-10-26
[monitor-filesystem-expansion]: ../spec/testing/evidence/Monitor节点与主机资源历史证据.md#主机列表文件系统容量与-inode-展开2026-10-26
[monitor-heatmap]: ../spec/testing/evidence/Monitor节点与主机资源历史证据.md#主机资源热力面板本地
[monitor-meta-workers]: ../spec/testing/evidence/Monitor实例与服务发现历史证据.md#meta-双-worker-标准启动与登记绑定专项
[monitor-transfer-workers]: ../spec/testing/evidence/Monitor实例与服务发现历史证据.md#transfer-两类双-worker-标准启动与主机绑定验收入口2026-10-22
[monitor-quality-workers]: ../spec/testing/evidence/Monitor实例与服务发现历史证据.md#quality-多-worker-标准启动登记与主机绑定入口2026-10-22

[monitor-summary]: ../spec/testing/evidence/Monitor综合验收历史证据.md
[monitor-host]: ../spec/testing/evidence/Monitor节点与主机资源历史证据.md
[monitor-instance]: ../spec/testing/evidence/Monitor实例与服务发现历史证据.md
[monitor-alert]: ../spec/testing/evidence/Monitor告警与规则历史证据.md
[monitor-trace]: ../spec/testing/evidence/MonitorTrace与日志历史证据.md
[monitor-http]: ../spec/testing/evidence/MonitorHTTP质量与采样历史证据.md
[monitor-engine]: ../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md
[monitor-execution]: ../spec/testing/evidence/Monitor执行监控与留存历史证据.md
[monitor-diagnostics]: ../spec/testing/evidence/Monitor部署与前置诊断历史证据.md


## 磁盘与卷容量首期

原图硬件卡的“磁盘总容量”不能由现有挂载容量或IO累计量推算。仓库锁定的 [node_exporter v1.12.1 diskstats](https://github.com/prometheus/node_exporter/blob/v1.12.1/collector/diskstats_linux.go) 提供IO及设备信息，没有本项设备容量；[util-linux lsblk实现](https://github.com/util-linux/util-linux/blob/v2.41.2/misc-utils/lsblk.c) 从sysfs的size读取扇区数并左移9位转换为字节。当前受限Desktop来源的非root只读取样已能看到VM的vda与其vda1分区，二者不能相加为物理总量；只证明可行性，见[前置证据](../spec/testing/evidence/Monitor节点与主机资源历史证据.md#块设备容量只读前置核对2026-10-28)。

2026-10-28 用户确认以下首期范围；术语、正式规范、来源与产品按同一协议实施：

1. 产品显示“磁盘与卷容量”，对象是**当前纳管节点可见的已就绪非分区块设备**，逐设备展示IEC容量、可核实的结构类型和采样状态。分区、loop、RAM设备及零容量未就绪对象不作为主表磁盘；正式选择按内核属性和设备关系，不按sda／vda名称猜物理性。映射卷与底层盘分别表示，未知类型明确未知；不输出整机／集群物理总量，不计算块设备使用率，不新增趋势或容量告警，原文件系统明细和告警保持原契约。
2. 原生Linux读取受控宿主sysfs；Desktop仅在内核、设备号及来源归属证明成立时读取其VM可见设备，不能宣称Mac物理磁盘、Docker卷总量或完整存储池。看不到或无法证明的环境明确不支持，不扩大Desktop的宿主／设备访问权限。
3. Infra标准节点入口增加**受管只读采集职责**，只读固定白名单的容量、设备号、分区／必要映射属性及采样／启动证明，不读取序列号、业务文件、凭据或原始设备，不给采集器Docker Socket、特权或Shell管理接口。System继续仅裁决主机身份／权限，不新增CMDB实体或后台投影；Monitor通过当前User和既有主机读取权查询固定目录，不执行远程命令。
4. 唯一发布路线复用 [node_exporter官方Textfile Collector](https://github.com/prometheus/node_exporter/blob/v1.12.1/README.md#textfile-collector)：节点侧自动原子发布专用目录、专用前缀的容量及采样状态／时间，exporter只读消费，再沿原mTLS／Prometheus链路查询。该扩展仅承载新增块容量，不能替代或覆盖原生CPU／文件系统指标，严格拒绝非受管文件；不让用户运行脚本或手工导入JSON，不另开推送或直连查询路径。
5. 能力显式选择、默认关闭，统一随标准节点入口准备、启停及清理；未选择、失败、陈旧或超预算只影响本项，其他观测及业务继续运行。原子发布、源采样时间、同次启动和帧完整性共同判定有效性，不能把exporter刚抓到旧文件当新数据；整组超限拒绝，不截断、不拆批、不提高现有预算。
6. 实施同批完成来源生命周期、目录／API及Swagger、唯一前端设备视图、既有标准测试与CI登记。先做受控sysfs、旧文件／失败／设备变化、预算和权限回归，再做实源T2及页面T3。既有一次性隔离Hosted仅增加当次节点容量／状态查询及同一真实页面的只读用途，不造盘、不挂卸载设备、不加压；原真实密码／MFA、独立Prometheus身份及全退出清理保持原契约，真实运行仍由专门会话安排。

本批不修改个人来源、不重启服务、不设置定时任务。来源、查询、页面及真实运行分别记录验证结果；现有文件系统容量不是设备容量的替代。

当前产品与原生 Linux 容量验收准备已接通，精确核验受管采集器、只读宿主元数据、双端专属卷及逐设备新鲜帧，见[容量验收准备](../spec/testing/evidence/Monitor节点与主机资源历史证据.md#磁盘与卷容量原生来源证明与console收据准备2026-10-29)。Console 列表展开与详情共用唯一设备读数，已有[关闭／故障／恢复](../spec/testing/evidence/Monitor节点与主机资源历史证据.md#容量关闭故障与恢复的console状态验收2026-10-29)、[自动刷新／隐藏恢复](../spec/testing/evidence/Monitor节点与主机资源历史证据.md#容量明细自动刷新与隐藏恢复的console验收2026-10-29)、[切换主机／收起／离页隔离](../spec/testing/evidence/Monitor节点与主机资源历史证据.md#容量迟到响应的console范围隔离验收2026-10-29)及[读取撤权／身份切换](../spec/testing/evidence/Monitor节点与主机资源历史证据.md#容量读取撤权与身份切换的console验收2026-10-29)受控证据。当前专项25场景与独立构建通过；上轮Console完整门禁的开发端口清理断言失败未闭环，本批未盲目重跑；Monitor完整前端长回归仍待稳定环境复验。真实原生Linux、Safari与正式身份Hosted尚未验收，关闭Textfile的旧文件系统验收不能外推为容量扩展通过。

MinIO自动发现已实现：唯一部署发现v2的minio_metrics私有来源、标准私网精确核验、管理员确认及9000 API端口登记、有效主机关联和既有HTTP SD。000262只扩展类型及服务匹配约束，不新增权限、租户范围或旧路径。发现T1、实际双MinIO来源发现T2、System关联／迁移T2及确认页面T3已通过；17项固定查询、四项列表摘要、10／7分组详情及趋势已实现，复用当前User重核与现有页面交互，管理列表已开放MinIO监控跳转；真实来源页面及正式身份全链路仍待验。详见采集来源历史证据的本批记录，CI／Hosted仍等专门会话回传。


2026-11-05 已按确认方案收拢采集部署分组：主机与Infra／Business来源共用工作区采集项目，账号／私有卷／网络仍逐来源隔离，PostgreSQL安全入口仍分容器。统一入口按选定来源操作；旧部署仅显式应用时退役，普通恢复不迁移或构建。正式边界见[采集来源统一部署分组](../spec/addp平台运行观测规范.md#采集来源统一部署分组2026-11-04)，分层结果与原失败保留于[本批证据](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#采集来源统一-compose-分组2026-11-05)。固定源码的Linux主机资源T2及共享语法入口Platform复验已通过；用户已显式应用个人Desktop四项来源，单项目六容器、旧项目容器清零、Prometheus新鲜抓取及Safari指标恢复已只读复验，见[个人复验](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#个人-desktop-统一分组应用与恢复复验2026-11-05)。Desktop未提供的文件系统／网卡／TCP仍不可用；个人来源故障注入和Hosted不在该结论内，后续CI／Hosted仍等专门会话回传。

全量本地启动已按后续确认接入既有主机恢复及末尾只读监控摘要，恢复／检查失败保留非零但业务继续启动；局部与Hosted范围不变。最小来源、配置／登记及完整开发／Infra生命周期门禁通过，真实个人全量启动仍待用户应用，见[启动集成证据](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#全量开发启动的主机恢复与监控摘要2026-11-05)。
