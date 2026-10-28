# ADDP 平台运行监控后续事项

已实现的对象、部署、采集、查询和告警契约迁入[平台运行观测规范](../spec/addp平台运行观测规范.md)。本专题统一维护 Platform 运行观测的能力状态、未实施设计和验收缺口；摘要区分当前实现与实际验收，不将文档整理计为产品验证。模块职责、实现入口和执行监控后续范围分别见[模块契约](../../monitor/CLAUDE.md)及[模块架构说明](../../monitor/docs/Monitor模块架构说明.md)，不在模块入口重复维护本页状态。

## 一、当前实现与剩余工作

| 能力 | 当前实现 | 验证边界或剩余工作 |
| --- | --- | --- |
| 可选观测基础设施 | 日志、指标按 Infra 生命周期组织，可分别关闭；故障与业务就绪隔离 | 已有标准生命周期及指标链路验证；生产安装、升级与容量 T5 未完成 |
| 平台主机管理 | 主机登记、详情、编辑、纳管启停；名称/地址、类型、状态组合筛选及分页 | 组合筛选的本地门禁及个人复验见[组合筛选证据](../spec/testing/evidence/Monitor节点与主机资源历史证据.md#主机监控类型纳管状态筛选与分页回跳修正2026-10-25)；未提供批量运维 |
| 管理员确认接入 | System 确认主机与服务允许名单，生成新进程参数；Monitor 独立保存可选采集，见[清单契约][monitor-admission]。 | Linux VM 单新 Monitor 绑定与覆盖限制见[主机绑定证据](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#清单接入与实例绑定)；Infra／Business 两个单容器 PostgreSQL 的自动接入 T4 见[自动接入证据](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#infra与business-postgresql-自动接入)。原生 macOS 绑定与多进程仍不足，自动接入不等于专业采集已验 |
| 采集配置 | 主机来源 CRUD、认证发现、独立采集启停、列表与详情采集结果 | 已接入；主机纳管状态、配置启停与实际采集结果分别表达 |
| 主机资源与趋势 | 26 项固定主机指标（含 TCP）、设备／挂载明细及共享图表交互；目录与缺值口径见[规范 6.1][monitor-catalog]，缩放、单位、图例及组合图见[规范 6.3][monitor-presentation]。 | 指标链路、交互和 TCP 分层结果见[节点证据][monitor-host]；原 Linux Hosted 不覆盖全部新增交互，新增真实页面 Hosted 待回传。真实 Linux 采集与挂载读数的[验收准备](../spec/testing/evidence/Monitor节点与主机资源历史证据.md#真实-linux-采集与挂载页面验收准备2026-10-28)已补（含列表展开／手工刷新／收起），实际运行未验。文件系统视口按完整挂载身份隔离；仅显示来源支持的指标，不据此诊断内存泄漏 |
| 主机列表资源视图 | 核心资源／负载与连接两种列视图、[当前页摘要][monitor-core-summary]及按需[设备展开][monitor-device-expansion]／文件系统展开；筛选恢复、唯一展示、分组读取和预算沿[规范 6.3][monitor-presentation]。 | 五项摘要见[固定 `9bf8dddcb` Linux 前端][monitor-fixed-frontend]；[文件系统展开][monitor-filesystem-expansion]含固定 `e5391ca36`、首轮失败与修复，父 workflow 取消不计整体通过。[负载／连接证据][monitor-load-view]保留实源摘要 T2 前置失败；个人仅验三档负载与导航、TCP 缺样，右侧截图／前台刷新未验。设备／文件系统实源、个人 Safari 与正式身份 Hosted 不由前端门禁代替；列表扩展不扩大 TOP5／采集配置的两项读取 |
| 主机资源热力面板 | 复用当前 CPU、内存和各完整挂载使用率，提供主题热力与趋势入口，见[规范 6.3][monitor-presentation]。 | 本地交互见[热力面板证据][monitor-heatmap]，固定 Linux 前端见[`9bf8dddcb` 证据][monitor-fixed-frontend]；真实来源正式页面 Hosted 待回传。不新增接口或查询，不计算健康分或告警阈值 |
| 服务状态、资源与趋势 | 实例筛选／重置、租约状态、Go 进程资源与趋势及当前主机导航，沿[规范 6.3][monitor-presentation]；Meta、Quality 及 Transfer 两类 Worker 已支持默认单实例、可选双实例、独立 PID 与缺失补回，Transfer 持续同步双实例拒绝固定 Owner 身份。 | 绑定、双 Worker 实源隔离、趋势／环比见[实例证据][monitor-instance]，新增真实页面 Hosted 待回传。[Meta][monitor-meta-workers]／[Transfer][monitor-transfer-workers]／[Quality][monitor-quality-workers]专项的实例数量、登记／绑定、补回与页面断言由各专题维护；仅接通入口，真实 Hosted 未执行。原生 macOS 绑定、其他模块副本和全部语言覆盖仍不足，不开放退出实例历史 |
| 资源总览与 TOP5 | 主机数量、纳管/采集状态、CPU 与内存 TOP5、覆盖率 | 单真实主机 Hosted 与受控多源 T2 见[跨主题证据](../spec/testing/evidence/Monitor综合验收历史证据.md#节点实例与资源告警)；不宣称多真实主机完整 T4，不提供全局物理容量合计 |
| CPU/内存时段比较 | 主机与进程相邻等长时段的完整网格比较，单位和启动／预热保护沿[规范 6.3][monitor-presentation]。 | 主机完整样本／缺样见[主机比较证据](../spec/testing/evidence/Monitor综合验收历史证据.md#时段比较与资源告警)，进程本地结果见[进程比较证据](../spec/testing/evidence/Monitor实例与服务发现历史证据.md#服务进程-cpurss-相邻时段比较本地)，测试失败与修复后的 Linux 前端见[固定 `9bf8dddcb` 证据][monitor-fixed-frontend]。进程正式身份真实运行待回传；1 小时／24 小时真实长窗与同比未覆盖 |
| 平台统一告警 | Monitor 唯一管理入口，事件、确认、抑制、通知目标、投递与重试 | 入口及限定权限已发布；System 保留日志诊断和上下文跳转 |
| 主机资源规则 | CPU/内存规则 CRUD、CAS、后台连续窗口评估、未知保护和恢复 | 本地、受控来源／签名通知 T2、个人操作及 CPU／内存实压 T4 的独立范围见[主机规则证据](../spec/testing/evidence/Monitor综合验收历史证据.md#节点实例与资源告警)；仅覆盖测试阈值与单台 VM，生产 T5 未覆盖 |
| 服务实例资源规则 | 当前运行实例 CPU 核消耗／RSS 规则、持续窗口、未知保护及原事件恢复，复用统一事件和通知链路 | 固定 `dfa4ad161` 的原生 Linux 实源 T2 已回传并独立复核，覆盖真实触发／释放／恢复、签名重试、对等进程隔离及退出清零，见[专项证据](../spec/testing/evidence/Monitor告警与规则历史证据.md#服务实例-cpurss-告警原生-linux-固定版本复核2026-10-23)；原失败保留，不外推当前 HEAD、正式身份页面 T4、长窗或生产 T5；不新增退出结束状态，不将 RSS 超限解释为泄漏诊断 |
| 服务组失联规则 | 当前部署同模块／角色／用途全副本失联判定、当前运行确认持久化、规则四权限及默认停用CRUD，复用统一事件与显式通知订阅 | 规则与个人页面范围见[告警证据][monitor-alert]，真实租约故障、正式身份和 Hosted 未验。首次未确认保持等待，未知和计划停用不伪造恢复 |
| 引擎与容器专业观测 | Infra／Business 只读发现、单容器 PostgreSQL 批量确认与显式主机关联，见[规范 4.4](../spec/addp平台运行观测规范.md#44-平台引擎部署登记)及[4.5](../spec/addp平台运行观测规范.md#45-部署侧自动发现来源)；当前主机导航沿[6.3][monitor-presentation]，无需导出／导入文件。 | 自动接入、专业 T4 与来源 T2 按[引擎指标证据][monitor-engine]的各自固定版本认定，不外推当前 main。新增主机导航仅 T1/T3，个人环境未启用；其他引擎／集群未开放。容器详情与证明的实现／未验范围见[实例证据][monitor-instance]，不由 PostgreSQL 专业验收代替 |
| 平台 HTTP 接口质量与慢接口 | Common 统一全量采集，Monitor 固定窗口、实例/模板排行及趋势；HTTP 规则、持续慢接口标记、平台事件与 outbox | 公共 mTLS 前置失败、后续 Linux T2 及清理结果见[HTTP 证据][monitor-http]；旧失败不改判，也不代替个人本地实链、完整 Agent/Copilot 正式 backend 登录／页面、正式密码／MFA T4、Hosted、容量或长期保留。Tenant 发布服务质量仍归 Service 对象权限 |
| Trace 与调用拓扑 | 可选 Go 平台 backend/ingress 与 Agent/Copilot Python backend 同步 HTTP Trace，经同一 Alloy 尾采样后进入 Tempo；Monitor 查询及实际 Client/Server 调用边，复用 System 精确日志关联 | 各批及正式身份本地第十窗口见[Trace 证据][monitor-trace]；本机隔离闭环已验，首次失败与 Link 目标未保留的 404 仍分别保留。`formal_runner_t4=false`，不代替 Hosted／专用 Runner T4 或 T5；真实 72 小时留存／删除、长期合并、其他 Runtime、continuous、Python 异步及 SQL Span 未覆盖，原 Go HTTP 容量外推不扩展至后续批次 |
| 健康评分、功能热度、泄漏诊断 | 暂缓 | 先确定事实来源和可解释口径，不由资源阈值直接推导 |
| SSH、环境初始化、防火墙、批量启停及扩容 | 不在首期 | 需要独立操作计划、权限、执行与审计契约 |

平台事件已接入级别／首次发现时间筛选、首次触发名称检索、当前主机名称筛选及严重未恢复事件优先排序。筛选交集、计数与分页、URL 恢复、名称事实和排序规则统一见[正式规范 7.1](../spec/addp平台运行观测规范.md#71-领域入口与权限)；历史名称不回填，当前主机筛选不推断日志／实例／模块关联。各批固定版本、失败及个人复验范围见[告警与规则证据][monitor-alert]，不由接口或页面单项结果外推完整验收。

主机监控列表的类型／纳管状态组合筛选沿 System 现有口径接入；共享控件、分页／详情／刷新恢复及仅查询当前页摘要的契约见[正式规范](../spec/addp平台运行观测规范.md)。本批验证及正式身份／Hosted 边界见[组合筛选证据](../spec/testing/evidence/Monitor节点与主机资源历史证据.md#主机监控类型纳管状态筛选与分页回跳修正2026-10-25)，不由旧主机管理页验收代替。

## 二、后续开发顺序

| 优先级 | 下一步 | 进入条件 |
| --- | --- | --- |
| 1 | 补齐原生宿主与多进程部署覆盖 | Linux VM 单新 Monitor 绑定已验；原生宿主与 Desktop VM 分别声明，多进程按各自实例身份验证，不在线改挂既有实例 |
| 2 | 补齐原功能表的服务质量、引擎专业指标及容器资源 | PostgreSQL 已实现及已验范围见[采集来源与引擎指标证据][monitor-engine]；其他引擎、cAdvisor 节点部署／容器视图及实例资源组后续扩展，按[运行环境边界](../spec/addp平台运行观测规范.md#52-部署与运行环境)分别推进；先明确对象归属、配额和 OS 支持，复用固定目录与唯一采集链 |
| 3 | Trace 长时保留验证与后续覆盖 | 原三 Go HTTP 链短测仅支持原 20 GiB／72 小时外推；真实过期删除、长期合并增长及查询余量另定窗口，不沿用原四十分钟短测或本地闭环授权。各批已验范围见[Trace 证据][monitor-trace]；正式 Runner T4、其他 Runtime、continuous、Python 异步、SQL Span 与真实长时窗口分别确认 |
| 4 | 生产发布及受控运维 | 真实平台 T5；SSH/初始化/防火墙/启停另有权限、计划、执行与审计确认 |

Trace 中 request_id、trace_id、execution 身份分别表达请求、调用链和业务执行；异步任务按运行观测规范第三批明确持久关联并创建短阶段独立 Trace，不能把长任务硬挂在创建请求下。慢请求保留须证明实际采样策略，随机头部采样不能保证保留全部慢请求。

受控运维中 ADDP 管理目标、计划、权限与审计，成熟执行设施实施变更。主机登记不默认保存 SSH 密码，采集身份不具备 Shell 权限；Monitor 不提供任意命令执行器，租户 Orchestrator 任务不隐含获得平台主机操作权。

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

### PostgreSQL 专业指标实施状态

2026-10-20 确认的两个作用域独立采集身份及固定汇总函数已接通，数据库侧最小读取边界归[正式规范 4.6](../spec/addp平台运行观测规范.md#46-postgresql-数据库侧汇总读取边界)。来源模板、mTLS、开关默认关闭与作用域隔离、账号启停／NOLOGIN、未知会话及凭据丢失拒绝、Business 仓库外认证目录归[4.7](../spec/addp平台运行观测规范.md#47-postgresql-受保护的固定指标来源)。来源生命周期已沿标准启停接入；标准脚本不签发证书或扩大 CA 信任，不由 Monitor／Web 请求重启数据库，停用保留函数、配置、历史和数据卷。

Business 认证目录首次切换须由部署方沿标准生命周期重建容器并保留数据卷；文件准备不代表采集已生效，个人环境未切换／启用不计已接入。实库、回归、CI 与正式身份结果按[引擎指标证据][monitor-engine]分别认定：专业查询／页面 T4 为固定 `b08f2331d`，后续来源 Linux T2 为固定 `5d91fa31c`；两者不互相替代，不抹去先前失败，也不证明当前 HEAD、七天自然留存或生产 T5。专业 T4 不覆盖其后新增的主机导航，个人部署仍未据此自动启用。

### 后续自动接入的最小身份投影（已确认）

2026-10-20 确认的引擎最小身份投影及 Common 完整性校验已实现。主体、字段、同快照裁决与整体预算见[正式规范第三章](../spec/addp平台运行观测规范.md#三对象与模块边界)；专业来源的端点／当前数据库运行身份扩展见[4.5](../spec/addp平台运行观测规范.md#45-部署侧自动发现来源)、[4.7](../spec/addp平台运行观测规范.md#47-postgresql-受保护的固定指标来源)。私有投影不进入普通浏览器 DTO，不授予后台部署写权或租户读取权，不读取 System 私表；用户专业查询仍逐请求接受 System 当前 User 裁决。投影 T1／仓储 T2／Swagger 不推导专业 T4，分层记录及断言修正见[引擎指标证据][monitor-engine]。

### PostgreSQL 专业发现、查询与展示方案

沿部署发现 → System 当前引擎身份 → Monitor 唯一 HTTP SD → Prometheus → Monitor 授权查询 → Console 页面这一条链，不另设手工采集目标、数据库凭据或租户 Engine 关联。专业来源的归属、当前数据库容器匹配及失效移除统一沿规范 4.5、4.7；专业来源失败不清空合法主机／进程身份，采集启停不改写确认摘要或管理历史。

十二项专业查询目录及速率的启动／统计代际条件归[正式规范 6.1](../spec/addp平台运行观测规范.md#61-固定指标目录)，三个查询入口、当前 User 双侧权限与完成时重核归[6.2](../spec/addp平台运行观测规范.md#62-资源查询与预算)，当前数据库容器段、最多七天趋势及页面职责归[6.3](../spec/addp平台运行观测规范.md#63-趋势时段比较与总览)。退出容器历史及跨段拼接仍须另定保留／授权规则；HTTP 200 或 up=1 不证明 SQL 采集成功，不用旧样本或有效零值代替失败。

验证沿 Common／System／Monitor T1、投影与持久化 T2、固定指标 mTLS T2、Monitor／Console 前端及 Swagger 既有门禁，部署发现协议／标签／Prometheus 输入同步现有 CI 登记。2026-10-21 确认的正式身份读取、真实页面及全退出清理范围统一见[PostgreSQL 专业观测测试场景](../spec/testing/scenarios/平台观测与语义生命周期.md#postgresql-专业观测与页面)；实际覆盖与未覆盖项见[固定专业 T4](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#postgresql-专业观测正式-hosted-验收)和[后续来源 T2](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md#postgresql-专业来源固定版本-linux-t2-复核2026-10-24)。未执行、失败或清理不完整均不计通过。


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
