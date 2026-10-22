# Monitor 历史验收证据

[返回证据索引](../验收证据索引.md) · [Monitor 模块契约](../../../../monitor/CLAUDE.md)

以下汇总模块说明与后续专题中的历史记录，仅适用于记录所指源码、运行尝试与覆盖范围。本次整理没有重跑这些 CI，也未查询其最新状态；失败、排队、分项与整轮通过分别保存，不推导当前 HEAD 或生产 T5 已通过。

## 节点、实例与资源告警

| 历史范围 | 源码 | 原记录 | 已记录结果与限制 |
| --- | --- | --- | --- |
| 节点来源首次接入 | `原文未单列 SHA` | [37463003308](https://github.com/pampa0629/addp/actions/runs/37463003308) | T4 通过：2026-10-06 Linux VM 原生来源、正式身份与控制面；不代表物理宿主或生产 T5。 |
| 基础资源读取 | `794450a30` | [37485747576](https://github.com/pampa0629/addp/actions/runs/37485747576) | T4 通过：八项即时/趋势、预算 CAS/热生效、权限隔离、中心故障/新样本、停用历史排除和零残留。 |
| 节点资源正式页面 | `b3a64756c` | [37563564755](https://github.com/pampa0629/addp/actions/runs/37563564755) | T4 通过：2026-10-07 密码/MFA、单 iframe、八项资源/趋势、安全管理员拒绝、故障恢复和零残留；不含该次后的自动刷新。 |
| 节点详情自动刷新 | `1ce34d770` | [37566181124](https://github.com/pampa0629/addp/actions/runs/37566181124) | T4 及同提交 Platform CI 通过：自然 15 秒、服务端 end 前进、URL 不变、关闭恢复、16 秒无定时请求；隐藏/慢请求/撤权由 T3 单独覆盖。 |
| CPU 一分钟忙碌率 | `5bb4173cf` | [37573777193](https://github.com/pampa0629/addp/actions/runs/37573777193)、[37573754535](https://github.com/pampa0629/addp/actions/runs/37573754535) | Hosted T4 / Platform CI 通过：九项资源范围、完整窗口与核集合、节点/目标恢复、停用历史及清理；26 项 runner 回归。此前凭据过期与旧异步断言失败不计通过，CPU 单独预热/恢复零值仅 T3。 |
| 文件系统字节容量 | `08556c553` | [37602308231](https://github.com/pampa0629/addp/actions/runs/37602308231)、[37602281641](https://github.com/pampa0629/addp/actions/runs/37602281641)、[37602281771](https://github.com/pampa0629/addp/actions/runs/37602281771) | Hosted T4、Platform CI、Release/T2 通过：六挂载/30 序列、同次采样公式、精确 URL/趋势、恢复新样本、停用历史与零残留；本地对应门禁及 27 项 runner 已记录通过。 |
| 文件系统 inode | `86853c844` | [37610515534](https://github.com/pampa0629/addp/actions/runs/37610515534)、[37610486715](https://github.com/pampa0629/addp/actions/runs/37610486715) | Hosted T4 / Platform CI 通过：六挂载/24 序列，五挂载有效、EFI 明确空值，URL/趋势、恢复及零残留。首次冗余赋值断言失败后修正；本地 22 inode/18 字节/15 CPU 场景、43 单元/53 浏览器及指标/PostgreSQL 通过。 |
| 磁盘吞吐与单位 | `5c490bf5c` | [37740681212](https://github.com/pampa0629/addp/actions/runs/37740681212)、[37740647211](https://github.com/pampa0629/addp/actions/runs/37740647211) | Hosted T4 / Platform CI 通过：真实 sda 读写、精确趋势、IEC/经过时长/负载数量、MFA/URL、故障恢复及零残留；六截图无敏感归档。本地 32 runner、150 Console 单元/119 浏览器、构建和平台门禁通过。 |
| 网络接口吞吐 | `193c0e82e` | [37791100970](https://github.com/pampa0629/addp/actions/runs/37791100970)、[37791001755](https://github.com/pampa0629/addp/actions/runs/37791001755)、[37791001784](https://github.com/pampa0629/addp/actions/runs/37791001784) | Hosted T4、Platform CI、Monitor PostgreSQL/metrics 通过：16 接口/32 序列，两趋势各 21 点，84 响应/七截图、故障恢复/停用历史及零残留；36 归档无敏感模式。原模块入口曾在两个五秒超时退出，宿主 39 项及平台独立复跑通过，不抹去首轮失败。 |
| 磁盘 IO 时间 | `839ef8338e；生产实现 88d7ade1d` | [37808051558](https://github.com/pampa0629/addp/actions/runs/37808051558)、[37808006295](https://github.com/pampa0629/addp/actions/runs/37808006295)、[37808006338](https://github.com/pampa0629/addp/actions/runs/37808006338)、[37803604603](https://github.com/pampa0629/addp/actions/runs/37803604603) | 最终标准 Monitor 模块门禁返回 0；21 IO 场景、完整/受限 T2 通过。生产实现的 PostgreSQL/metrics CI 通过，修正后路径跳过不计重跑；修正 Hosted T4 / Platform CI / Release 通过：25 项目录、102 响应、七截图、三条时间趋势、恢复及零残留。首轮 EFI inode 选样失败、先前 mTLS 超时和跨 Owner 预检失败保留。 |
| Desktop 七采集器个人应用 | `51d7a66bfb` | [37877201811](https://github.com/pampa0629/addp/actions/runs/37877201811)、[37877201781](https://github.com/pampa0629/addp/actions/runs/37877201781) | 原记录为 Platform CI / Release-T2 排队，未记通过。用户标准 up 已应用七采集器；18 设备、vda 五指标与约 13 秒新鲜样本的只读 mTLS 诊断不等于 Monitor 完整窗口/页面；Safari 会话过期，页面当时未验。 |
| 当前实例 CPU/RSS 趋势 | `原文未单列 SHA` | [38227755626](https://github.com/pampa0629/addp/actions/runs/38227755626) | Hosted T4 通过：当前实例两序列、启动后裁剪、URL、刷新恢复、来源/中心/控制故障、停用历史与零残留；个人 Safari 两曲线复核。上一轮故障断言未复现，根因未确证；旧单次通过不证明长期无故障。 |
| 服务自动刷新 | `7f365279b` | [38329665208](https://github.com/pampa0629/addp/actions/runs/38329665208) | Hosted T4 通过：自然定时/关闭恢复、25 项节点与进程协议、中心/来源/控制面恢复、停用/删除及零残留。38325976574 的 System CPU 缺窗口未复现、固定时刻诊断未触发，根因未确认，不记作故障修复。 |
| 主机列表自然刷新 | `4e9fe672f` | [38340694203](https://github.com/pampa0629/addp/actions/runs/38340694203) | Hosted T4 通过：自然刷新、关闭恢复、原故障链及零残留；前次导航 Test ended 未复现，根因未确认，安全耗时诊断保留。 |
| 资源总览与 TOP5 | `93b89ab33` | [38588001065](https://github.com/pampa0629/addp/actions/runs/38588001065) | 单真实主机 Hosted T4 / 同源码 Platform CI 34 项通过：聚合、预算、刷新/导航、中心/来源/控制故障与停采恢复、零残留。六个受控 mTLS 来源 Prometheus T2 的完整聚合、TOP5 同值/末项排序、缺失/有效零/停用/预算拒绝通过；不代表真实六主机。 |
| 相邻等长比较：缺样保护 | `68d7cbd03，包含 ac7522bf5` | [38983748074](https://github.com/pampa0629/addp/actions/runs/38983748074) | 完整 Hosted T4 通过：三次比较、来源/中心/控制面恢复、节点启停和目标停用/删除，零残留；比较分支为 0 项完整、3 项不完整，不当作完整样本比较通过。 |
| 相邻等长比较：完整样本 | `74adc2d45` | [39082438625](https://github.com/pampa0629/addp/actions/runs/39082438625) | 完整 Hosted T4 通过：CPU/内存各 41 自然有效点，三次五分钟比较完整；独立重算原始报告、刷新恢复及零残留。不扩展为长时段、进程比较、同比或 T5。 |
| 内存压力资源告警 | `8111363d2` | [39016166744](https://github.com/pampa0629/addp/actions/runs/39016166744) | 正式身份/Gateway 的 Hosted T4 通过：真实内存打开、缺样保持、恢复同一事件、HMAC 通知/自动重试及零残留；不表示生产满载或自动扩容。 |
| 受限 CPU 资源告警 | `53be389c5` | [39082008910](https://github.com/pampa0629/addp/actions/runs/39082008910) | Hosted T4 通过：固定测试阈值下触发、恢复、签名通知重试和零残留；不作为生产容量或 T5。 |

## 平台资源与主机接入专项

本节保留[运行监控后续专题](../../../next/ADDP平台运行监控与可观测性设计.md#三验收状态与剩余缺口)原有的专项细节；上表是历史索引，本节补充预算、观测数量、身份和清理边界。记录未在本次整理中重跑或刷新状态。

### 时段比较与资源告警

完整样本比较已通过 [Hosted 专项](https://github.com/pampa0629/addp/actions/runs/39082438625)（源码 `74adc2d45`）：自然采样形成 CPU/内存各 41 个有效评估点，三次 5 分钟比较均进入完整分支，原始报告独立重算、页面刷新恢复及退出零残留通过。原 1800 秒场景预算和浏览器期限保持不变；该专项与 CPU、内存压力分别验收，不推定长时段或进程比较已经验证。

资源告警已分别通过[真实内存 T4](https://github.com/pampa0629/addp/actions/runs/39016166744)（源码 `8111363d2`）和[受限 CPU T4](https://github.com/pampa0629/addp/actions/runs/39082008910)（源码 `53be389c5`）。内存覆盖打开、缺样保持、恢复及签名重试；CPU 在 4 核 VM 以 1 核硬配额加压约 96.51 秒，固定测试阈值下触发并恢复同一事件，两条通知 delivered，首次 503 后自动重试共三次请求。两轮均验证正式身份隔离与退出零残留。该证据不代表生产满载、容量评估、自动扩容或生产 T5 已验收。

### 清单接入与实例绑定

主机组合筛选、详情返回及重置已有个人 Safari 验证。System 清单接入 → Monitor 采集保存已通过 [Hosted 专项](https://github.com/pampa0629/addp/actions/runs/39127927703)（受验源码 `fb13955d8f3cc48b254f2c840e55b8f8f638d2cc`，包含 `70c62a09a`、`9263ab6ee`）：单台 Linux VM 创建一次主机，正式 400 拒绝后只重试同一主机采集，刷新、两 Owner 独立读取及准确数量均通过。场景约 63.535 秒，浏览器约 39.370 秒；配置退役通过 API 删除采集并核验 404、停用主机且核验版本递增，随后独占部署销毁与外层零残留通过，不表示产品已提供主机 DELETE。

新进程真实绑定已通过 [Hosted 专项](https://github.com/pampa0629/addp/actions/runs/39169909912)（精确源码 `fee7cb91d8ef674d2beb41036a288519bc1d6be7`）：新 Monitor 首次绑定当前主机，旧实例退出且资源为 `not_active`，System 对等实例保持未绑定；停用主机后同一进程拒绝绑定但资源有效，重新启用后恢复。真实 Console 从当前主机名进入同一 iframe 的资源页，收据包含精确九项有效标量与主机版本 3，后退及刷新恢复筛选。场景约 162.642 秒，绑定浏览器约 12.533 秒；配置退役与外层部署销毁、Infra 零残留分别核验通过。浏览器收据按端点、主机及完整指标集合选择，不能由并发网卡响应替代；原角色、预算和单一路径保持不变。

### 本批证据的覆盖限制

上述为一次性 Linux VM 的单个新 Monitor 进程验证，不代表原生 macOS、多进程、Business 引擎承载、总览多真实主机或生产 T5 已验收。精确源码的 Console 与 Platform consistency CI 已实际执行通过；本地 Console 聚合的 4 项其他场景失败单独保留，不用远端结果覆盖本地失败事实。

### Infra与Business PostgreSQL 自动接入

2026-10-20 已核验 [engine-admission Hosted 专项](https://github.com/pampa0629/addp/actions/runs/39324018076/job/118305077445)，精确源码 `b3dcae5a3e62b37dbbb15f9e28067a94b6454545`（包含成功更新审计状态修复 `0155d5db578ef3324d81db8aa7a8d2482539f55e`）。独占 Linux x86_64 部署通过正式三员 Bootstrap、真实密码与 MFA 建立非委托 Platform User；一次集合请求接入 Infra／Business 两个单容器 PostgreSQL，显式关联同一新建主机。浏览器验证保存、刷新恢复、已接入项禁选与两个当前容器来源；两张截图已目视核对。

场景报告 `engine_admission.result=passed`，覆盖 Owner 列表／详情读取、版本冲突、来源变化／重复成员拒绝、第二项失败整批无根记录、修改与停用退役，身份隔离通过。正式场景约 62.544 秒，浏览器约 26.469 秒；原有身份、断言及预算不变。外层 `summary.txt` 分别记录 `result=passed`、`cleanup=passed`、`infra_cleanup=zero_residuals`，不能仅用页面通过替代完整验收。

修复前实际 PUT 因成功审计缺少 HTTP 状态导致 PostgreSQL 约束拒绝；新增 HTTP Handler／PostgreSQL 回归先复现 400，再验证正常更新、旧版本 409、停用与两条成功审计。`make test-module MODULE=system GO_ONLY=1`、`make test-system-iam-runner`（19 项）及 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package api --test engine-deployments"`、对应 `--package repository` 均通过。本地只使用 `addp_iam_test`；T2 的受控认证上下文不冒充真实身份 T4。同源码 [System 前端](https://github.com/pampa0629/addp/actions/runs/39323690209/job/118303942964)、[Platform consistency](https://github.com/pampa0629/addp/actions/runs/39323690209/job/118303942260) 和 [System IAM PostgreSQL](https://github.com/pampa0629/addp/actions/runs/39323690244/job/118304788473) CI 已实际通过。

此结果限于自动发现、管理员确认接入和单主机承载声明，不表示数据库专业指标、容器资源、其他引擎／集群、多真实主机、原生 macOS／Desktop VM、租户连接关联或生产 T5 已验。没有读取租户业务数据或执行专业采集 SQL，个人部署未重启。此前未通过的尝试保留于各次 CI 归档，不计为通过。

## continuous 诊断与 Owner 不可用

| 尝试 | 源码 | 已记录结果与边界 |
| --- | --- | --- |
| [38329160596](https://github.com/pampa0629/addp/actions/runs/38329160596) | `bfc305953` | 整轮 T4 失败：真实自动刷新、恢复来源和稳定码已验，但撤权只清事件、未清终态概览/步骤；Monitor-only 与后续 CDC 未运行。清理通过、残留 0。本地修正后前端 56 单元/118 浏览器及构建通过，401 用例改为会话失效导航且内容全清，没有放宽旧内容断言。 |
| [38337857450](https://github.com/pampa0629/addp/actions/runs/38337857450) | `b0765f802` | 整轮 T4 失败：live/recovery 与旧内容清空通过，新鲜 Monitor-only 提示断言失败；清理及 Infra 零残留。StatusAnnouncer 与 el-result 同文案造成 strict-mode 双匹配，修正只定位唯一可见标题，保留播报和无详情/步骤/事件/树断言。 |
| [38381655995](https://github.com/pampa0629/addp/actions/runs/38381655995) | `65de8a82938ba1bbdd56df6f7c000be7f67076cc`，包含 `ba2c99652` | 完整 Hosted T4 通过：live/recovery/revocation 三份报告及九组接口/Worker 检查，Worker 失联新 UUID、暂停/恢复、跨/同租户 Owner 权限、Owner 不可用、撤权与新鲜 Monitor-only 拒绝、CDC 事件、删定义后历史读取。五模块源码一致、37 归档无私有控制文件或访问令牌/JWT 模式，领域/Infra 零残留。修正的前端 59 单元/131 浏览器与构建、transfer runner 65 项通过；不含后续 Workflow/Notebook 监督器或 T5。 |
| [39026543398](https://github.com/pampa0629/addp/actions/runs/39026543398) | 原文未单列 SHA | 整轮失败：隔离入口漏启 Security，在 Owner 故障阶段之前退出；清理及 Infra 零残留，不计 Owner 故障通过。后续场景预检 Security，并只读等待独占部署的 Transfer projection initialized，不写保护事实或连接本地共享业务库。 |
| [39082349255](https://github.com/pampa0629/addp/actions/runs/39082349255) | `009ceea39` | Owner503 八项页面检查与同进程停止/恢复通过，三张截图、领域/Infra 清理通过；整轮仍失败，原因是把运行中 CDC 错误要求为 running 终态事件，后续 CDC/审批/调度未运行。修正要求 CDC 仍运行且无终态；终态执行仍恰有一个匹配终态事件。 |
| [39083559464](https://github.com/pampa0629/addp/actions/runs/39083559464) | `946c4d13e` | 完整隔离 Hosted T4 通过：14 业务检查、真实 Owner503 八项页面检查、撤权/Monitor-only、Kafka 恢复、CDC 审批与双向扫描隔离、调度当前主体、删定义后历史。相同 Owner PID 停止/恢复证明 true，归档 `12121475487` 的三张截图已目视核验；业务残留 0、cleanup passed、外层 Infra zero_residuals。保留产品历史，不外推 Notebook、其他 Owner 或 T5。 |

Owner503 场景使用正式普通 User 与真实 Monitor 响应；故障夹具仅停止当次隔离部署中经 pidfd、可执行文件、UID、数据库和私有目录核验的 Transfer Backend，再恢复同一进程。浏览器分别验证终态详情、运行树、非空列表的清空与重新裁决；独立网络中断仅影响事件读取，不冒充 Owner 503。控制文件只在当次私有目录，公开回执不包含 Token、业务样本或完整接口响应。标准入口仍为既有 transfer-continuous-events Hosted suite 与 `make test-transfer-continuous-online-runner`。

## 分项与未通过范围

- 2026-10-15 的 `make test-module MODULE=monitor GO_ONLY=1` 记录为 T1 通过。`TestExecutionReadAPIReauthenticatesThroughOwnerForEveryRequest` 使用真实 Monitor 路由/Owner HTTP Client/共享范围 Handler 和独立身份夹具，覆盖逐请求重认证、跨/同租户、无定义本人/缺主体、父子隔离、删定义历史、列表/统计、动态撤权和 Owner503/恢复；响应不含业务载荷。故意保留旧权限时应 404 实得 200，恢复后通过；不是 System IAM、PostgreSQL T2、完整模块或新增 T4。
- Desktop 来源的独占 metrics T2 与 Monitor PostgreSQL 分项记录为通过：实际七采集器、18 设备/七原始计数及独立命名空间、五趋势、故障后新样本和零残留。第三次模块运行的 T0/Go/47 单元/61 浏览器/构建通过，但后续 backend unavailable 导致整入口失败；个人开关的端口夹具已隔离并复验，十秒生命周期超时、约三秒查询失败与此前 mTLS 超时未确认根因。最终独占重跑通过不覆盖整轮失败；个人 mTLS 诊断不等于正式页面。
- 资源告警联合 `make test-monitor-metrics` T2 使用受控 mTLS、真实 Prometheus/PostgreSQL 和临时 TLS 签名接收端，覆盖 503 自动重试、缺样不恢复、低值恢复与 outbox；同一临时 Compose 只初始化 addp_test，退出清理。System 身份是夹具，不代替正式身份 T4 或真实群通知。
- 全部早期“目录九项/十八项/二十项/二十二项”仅表示各次验收切片，当前目录与 Desktop 七采集器以正式规范为准。历史六采集器、文件系统/inode/网卡空值记录不改写为最新功能。
- 各次默认跨 Owner test-changed 预检缺依赖均未计为通过。真实多主机 TOP5、独立 Owner 撤权、全平台容量、生产宿主/物理身份与 T5 仍需各自独立证据；不把单主机 T4、六个受控来源 T2 或历史 CI 排队合并为通过。

### PostgreSQL 固定指标来源与引擎身份投影

2026-10-20，标准 `make test-monitor-metrics` 在专属临时 PostgreSQL 15／SQL Exporter／Nginx／Prometheus Compose 中通过。Infra 与 Business 依次验证独立 SCRAM 身份、固定 16 项指标、真实双向 TLS、无证书／明文／配置及重载路径拒绝、连续 SQL 失败时无旧数据库样本、错误计数递增、恢复产生新快照；关闭统计时不将缺失计数填零。完整主机、进程、趋势、六来源总览及资源告警生命周期回归通过，退出核验当次容器、网络、卷和临时文件零残留。两个作用域顺序复用专属测试 PostgreSQL，不等同两个正式部署或专业指标 T4。

同轮 `make test-module MODULE=common GO_ONLY=1`、`MODULE=system GO_ONLY=1`、`MODULE=monitor GO_ONLY=1`、System Swagger 生成／路由覆盖通过；`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package repository"` 在标准 `addp_iam_test` 通过，验证同一只读可重复读快照内两个平台引擎身份、私有字段排除、主机及部署停用、Desktop 主机类型拒绝、控制面失败整份拒绝。现有固定 Monitor Service 权限与拒绝其它身份的路由回归仍通过，没有发布新角色或权限。

证据仅覆盖数据库汇总、受保护指标来源及最小后台身份投影。标准可选来源生命周期、Prometheus 引擎发现、当前 User 授权的专业查询与页面仍待接入，未安装个人部署，未计为专业监控正式身份 T4 或生产 T5。

### PostgreSQL 采集账号认证边界

2026-10-20，`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 独占 T2 通过。Infra／Business 依次复用同一本轮 PostgreSQL 15：先证实 PUBLIC CONNECT 可让实际采集账号连接镜像默认 `postgres`，安装本方 HBA 首部规则并真实重载后，跨库和 Unix socket 路径明确被 HBA 拒绝，维护库 SCRAM、管理员连接、密码轮换及真实 mTLS 指标读取仍通过。额外验证数据库保留字 `all` 不成为通配授权、输入文件版本不符拒绝写入、原文件字节／属主／权限恢复，30 项配置校验及 38 项 CI 注册测试通过。全退出确认当次容器、网络、卷和临时文件零残留；不创建额外 database，不读取业务表，不触碰个人数据库。该结果交付规则生成和独占认证证据，正式安装生命周期与自动发现尚待串接，不计为专业指标 T4/T5。

### Business PostgreSQL 受管认证目录

2026-10-20，标准 `make test-monitor-metrics` 在仓库外隔离源码快照通过；快照从当时已提交的 `main` 创建，仅叠加本会话认证目录与对应门禁改动，不包含另一会话尚未提交的 HTTP 质量实现。38 项配置校验覆盖工作区归属、私有权限、链接／硬链接、文件漂移、锁竞争、输入版本、模板更新与中断更新拒绝；实际 Compose 解析确认只读目录挂载、禁止自动创建缺失目录且无模板回退。真实专属 PostgreSQL 分别覆盖原 PGDATA 及新只读目录：容器写入拒绝、宿主原子替换可见、重载后认证／mTLS、新样本及原文件恢复；主机、进程、趋势、六来源总览及资源告警生命周期回归通过，告警事件 2、成功投递 2、HTTP 尝试 3，退出核验当次容器、网络、卷及临时文件零残留。

同轮 `make test-business-config` 与 `make test-build-registration` 通过；既有 Monitor metrics CI 依目录输入声明覆盖新文件及 Business 启动／Compose／环境模板，打包入口递归包含 `scripts/infra/`，不新增 Job 或第二条部署路线。该结果不代表共享工作区的其他并行实现、个人 Business 数据卷迁移、采集角色自动安装、来源自动启停、专业发现／查询页面或 T4/T5 已通过；个人实例未改动。

### 平台 HTTP 接口质量：本地一期

2026-10-20，Common 统一 Router 采集基础已在 `39b132c9926f9f404287ab5aeaa8c9d94eebd9f4` 提交；Monitor 查询、页面、权限迁移 227、HTTP 规则和通知产品代码已纳入 `3ee46b77fd84fe1a915874a11e0e97b843cac8bd`。以下分别记录本地分项，不表示共享工作区整个 HEAD 已重跑。

- T1：基础阶段的固定源码 `make test-go` 全部通过且无跳过；产品阶段 `make test-module MODULE=monitor GO_ONLY=1`、System 对应 T1、`make test-authorization` 及双语 Swagger 覆盖通过。后续没有将基础阶段的全 Go 结果扩大到整个最新 HEAD。
- T2：`make test-monitor-postgres` 通过，覆盖 HTTP CRUD、并发 CAS、冻结恢复证据、缺样保护、查询期间策略变化及完成租约过期；最后一次运行输入无漂移。`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test platform-alert-management"` 通过，覆盖五项权限仅授现有平台系统管理员、刷新令牌撤销和幂等重跑。
- T2：完整 `make test-monitor-metrics` 在仓库外固定源码（基线 `688ecb005843ecadf7240a0e5249517de3a8381d` 叠加本专项文件）通过，含既有主机、进程、PostgreSQL 来源及资源告警回归。此后历史趋势补充与最终 HTTP 文件以 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality` 独立复验通过：真实 SDK 请求、13 项原生窗口场景、有权历史趋势、HTTP 503 窗口打开事件、签名通知首次 503 后重试、成功请求恢复同一事件、停止来源清空数值；HTTP 生命周期约 195.54 秒。两份快照输入均无漂移，退出分别核验当次容器、网络、卷和临时文件零残留。专项跳过 PostgreSQL 专业来源探针与后续主机回归，不能替代完整入口。
- T3：Monitor 78 项单元、226 项浏览器及构建通过，包含持续慢接口、筛选趋势、401/403/404 清空、迟到响应隔离和规则 CAS；Console 186 项单元、191 项浏览器及构建通过；Common 前端 241 项单元、System 前端 149 项单元及 189 项浏览器和构建通过。最后的 HTTP/权限相关文件与已验证输入相符，不扩展为其它会话后续修改的结果。

前次历史趋势试验在过去完整窗口形成前断言失败，未计为通过；最终仅等待自然历史形成，查询错误、采集状态或数据形状异常立即失败，未放宽有效失败点条件。配置选择矩阵验证 HTTP 专项不调用 PG 专业探针，默认及 postgres-source 保留原探针；39 项配置测试通过。

按用户决定，本轮不扩展或运行 Hosted HTTP T4，不借现有主机资源场景身份操作本专项。System 当前 User／后台最小投影在 T2 使用受控夹具，不能冒充真实密码/MFA 身份；个人部署未重启。Python HTTP 采集、Trace／日志链路关联及调用拓扑尚未实现，生产容量与 T5 未验；Tenant 发布服务质量仍由 Service 按对象权限提供。


### PostgreSQL 采集账号安装与来源自动启停

2026-10-20，唯一活动安装版本 `000002_lifecycle.sql` 与产品生命周期基础已随 `3ee46b77fd84fe1a915874a11e0e97b843cac8bd` 入库；本批补齐实库验证、容器完整 ID、挂载无序比较、macOS 路径别名及停止流程的锁内操作，避免把格式差异误判为部署变化。Infra／Business 分别按显式开关安装、停用及重新开启本方账号和受控指标来源；安装期间 NOLOGIN，验证 HBA 更新／重载后才启用 Login，不主动终止未知会话，凭据丢失拒绝自动重置。

- 固定源码完整 `make test-monitor-metrics` 复验通过：基线 `c171574023c7ab02a088cec2616baa1c4cb4284e20` 仅叠加本会话产品和验证文件，未混入共享工作区其他未提交改动。覆盖原 PGDATA 与 Business 只读认证目录、两个作用域的真实安装／SCRAM／mTLS、幂等启用／停用／重新启用、TLS 失败关闭、遗留会话拒绝且不杀连接、凭据丢失保护；包含既有主机、进程、趋势、六来源总览和资源告警回归，事件 2、成功投递 2、HTTP 尝试 3。退出核验当次容器、网络、卷与临时文件零残留，不代表整个共享 HEAD 或正式身份 T4。
- 完整门禁首轮在既有停止进程检查的外层 30 秒期限超时，原始未就绪状态未被保留，原因尚未确认；测试仅补充 JSON 状态诊断，并使内部失败能在原外层期限前返回，不延长期限或改变有效性判定。复验该检查在约 0.02 秒通过，不能据此声称已修复首轮超时根因。
- 最新控制面固定快照的 `make test-infra-runtime-log-lifecycle` 47 项通过，包含 8 项纯生命周期契约；Business 启停从各自 `.env` 读取选择前清除四项 Infra 继承值，验证旧配置／缺配置不会误启用，显式 Business 配置可独立生效，并验证可选来源失败后核心仍运行。`make test-business-config` 与 `make test-build-registration` 同一控制快照通过，构建登记核对 26 个 Go 服务／Worker、59 个镜像。完整指标回归先于这一配置隔离补充；最终来源专项另行核对最终输入。

最终 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 在同一基线叠加最终源码后通过：39 项配置与 8 项生命周期纯契约、两种认证文件布局及产品启停实库检查全部通过，退出零残留；额外核对每次实际 mTLS 返回的采样时间处于当次请求窗口，不接受旧快照。完成后核对本会话运行／探针文件与快照记录无字节漂移。

所有实际 PostgreSQL 验证均由现有标准门禁管理的 UUID 专属 PG15 执行，不使用个人或共享数据库，不修改个人 `.env`、凭据、卷或运行容器。新开关默认关闭；个人认证目录切换、正式 Prometheus 发现、Monitor 引擎专业查询／页面及 T4/T5 尚未验收，不能把来源部署计为专业监控全部完成。既有 Monitor metrics CI 通过目录／输入登记覆盖产品 SQL、Python、Business 启停与两个新增测试文件；纯契约同步进入既有 Infra 生命周期 T1，不新增 Job。

### HTTP 追踪二期第一批（本地）

2026-10-20，用户确认普通成功 5%、单次 Server 请求 ≥1 秒优先及 Tempo 72 小时初值，明确本轮只做本地验证。规范和 T0–T3 标准先修订；权限迁移 228、Monitor `monitor.trace.read`、当前 User 实例裁决及查询 API 分别已进入 `d2bbac317`、`13c87907e`，规范提交为 `dd9e10a26`。共享版本提交 `d5c39624f`、`857cf2b8e` 随后包含本批主体、收尾 Go 修正及其他会话工作；以下证据只覆盖本会话冻结输入，不能据此宣称整个版本或最新共享 HEAD 已验收。

本批覆盖已登记 Go 平台 backend/ingress 的同步 HTTP Server/Client：Gateway 重建外部 Trace/request ID，内部保留 Context、TLS、超时、取消、流式响应及 HTTP 101 双向流，外域重定向剥离关联头；不记录 URL/query、正文、凭据、SQL 或租户/用户身份。请求日志由唯一 Router 工厂生成安全 JSON，Context 中的 Trace/Span ID 沿原接收器、Alloy 和 Loki 结构化元数据关联。Monitor 只返回安全跨度与实际匹配的 Client/Server 调用边，离线实例也逐请求核验当前 User；日志读取另需 System 正文权限，Tenant 不开放平台 Trace。

策略版本 1 由 Monitor 唯一维护：生产端队列 2048，满时计数丢弃；真实等待 120 秒、10000 待决 Trace、100 Span/秒采样后目标预算；失败/慢/普通为有界优先级，不能承诺任意负载保留所有失败或 5% 普通请求。独立 Tempo bucket/账号、20 GiB bucket 配额及约 5 GiB 工作空间预留沿正式 Infra 入口；预留不构成文件系统硬配额。

- T1：仓库外固定源码基线 `28b145070` 仅叠加本方 SDK、全部 Context 直接消费者、唯一日志工厂及查询实现；最终 `make test-go` 的 22 个 Go 模块及各自 `go mod tidy -diff` 通过。另已执行 Common 完整 `GO_ONLY=1`，包括队列满与导出失败、隐私/外域传播、4xx 与失败分离、Context 日志、实际 HTTP 101 echo 及唯一共享传输检查。后续 Router 唯一所有权断言同时禁止别名 RequestID 中间件；新 Context 接口的生产实现、接口及测试替身保持一致。
- T2：最终 SDK 的 `make test-monitor-traces` 通过，`TestOwnedTracePipeline` 327.97 秒、包耗时 337.035 秒。三个真实原生 Go 进程经独占 TLS Alloy → Tempo/MinIO，完成真实 120 秒尾采样；实际观察失败、慢请求及 12/200 普通请求被保留，不能把小样本比例当作 5% 的统计证明。原窄时间窗可读取 5 个 Span、2 条匹配调用边；真实接收器 → Alloy → Loki 的精确 Trace 元数据、当前 User 历史实例读取/撤权、来源与查询证书互拒、只读代理路径/方法拒绝、采集端停止时业务仍 Ready 与失败计数、恢复后新 Trace 可查均通过。退出核验当次容器、网络、卷和门禁凭据/源目录零残留。未启用的子进程 helper 在父测试进程内按设计 Skip，三个实际受管子进程均运行并正常退出，不把 Skip 当验收证据。
- T2：独立 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--source-root <固定源码> --package migration --test platform-alert-management"` 通过，覆盖 227/228 权限、仅现有平台系统管理员获默认授权、刷新撤销及幂等重跑；使用标准测试数据库，不操作个人业务库。
- T3：最终 `make test-monitor-frontend` 为 81 单元/232 浏览器及构建，System 为 150/189 及构建，Console 为 186/195 及构建，全部通过；共享前端 241 单元通过。覆盖中英文、实际调用边与缺失边不推测、失败/无权限/撤权清空、迟到响应隔离、Trace → System 精确实例日志 → Trace 的唯一跨模块桥、长跨度一小时窗口及 URL 刷新恢复。其余模块的前端与其他会话后续改动不在此证据范围。

T0：最终 `make test-dev-lifecycle`、50 项 Infra 日志/指标/追踪生命周期、30 项 module-gate、Go 依赖规约、构建/前端与 T2/Online CI 登记均通过；共享前端另以 `make test-common-frontend` 通过。生命周期聚合早期失败来自停止命令 mock 未同步追踪 profile 与既有 PG 清理依赖，已补齐受控夹具、仓库外环境和空来源/独立 state，不变更实际停止权限或操作个人资源。

授权/Swagger 最终标准入口通过，覆盖全部 Go 与两项 Python OpenAPI；隔离快照补挂既有 Agent/Copilot venv 解释器，不安装依赖。曾有三个既有 Swagger shell 夹具在 15 秒原期限内超时；停止重编译后按原输入和期限复验通过，不能据此声称修复了未定位的历史超时根因。

根 Go 首轮发现本方 Context 迁移误触 Graph 创建及遗漏 Transfer 导出测试替身，已分别撤销无关调用参数、补齐真实接口签名后全量复验；旧失败不计通过。Console 初次误用同模块导航，导致 iframe 内进入无权限页且公开 URL 未变化，已改用现有 `openConsoleRoute` 跨模块桥并完整复验。追踪早期实际运行还证实 Tempo 默认 120 秒 WAL 时间容差不足以容纳 120 秒尾采样及入库延迟；现从唯一策略生成 300 秒容差，不降低慢阈值、采样等待或扩大用户查询窗。重启测试随机发布端口变化仅修复当次夹具，产品端点不自动改挂。

本轮未运行 Hosted/正式密码 MFA T4、生产安装发布 T5、72 小时自然过期等待或容量压测，未重启或接管个人服务。Python、异步 execution Links、SQL/数据库 Span、浏览器采集及引擎实例追踪不在本批；平台总体请求量仍由全量 HTTP 指标提供，不能从采样 Trace 倒算。下一步优先在一次性隔离设施实测压缩字节/Span、对象存储增长、采样/丢失与峰值待决占用，再复核 20 GiB/72 小时是否满足实际负载。


### HTTP 追踪隔离容量基线（本地）

2026-10-20 用户确认两档各 600 秒的本地隔离负载：50 请求/秒，普通/5xx/1.1 秒慢请求为 90%/5%/5%；70 请求/秒为 60%/30%/10%。复用真实 Go 三进程五 Span 链、策略 v1 的 120 秒尾采样和自然五分钟块轮换，保持 72 小时/20 GiB 配置。运行容器含发生器合计最多 6 CPU/6 GiB，所有当次卷占用达到 2 GiB 停止，每轮最长 40 分钟。以完整落盘物理成本（块、索引、MinIO 元数据/分配开销）外推，并额外留一份完整合并副本；按至少 100 Span/秒核算，72 小时预测 ≤16 GiB 才通过，工作空间另与既有 5 GiB 预留比较。

首轮预检因 Prometheus 标签值含空格、夹具错误按空白分列而失败，未计容量通过；解析已改为从行末拆数值，增加反例。实际完整首档（冻结源码 `y3p8h4tb/source`，基线 `5c0b555e6`）发送 30000 请求：普通 27000、5xx 1500、实际慢请求 1500，漏发/传输/非预期响应均零。Alloy 新 Trace 30000，策略保留 3936/舍弃 26064；Tempo 接收 19680 Span，未见提前淘汰、拒绝、刷块失败或 OOM。控制器等最后七条自然切块、完成对象计数核对后进入查询，但一次二十条明细在五秒内仅得到九条即超时，门禁失败、第二档未运行。最后物理 Trace 占用 4472832 字节，当次卷峰值 132960256 字节，Tempo 工作空间峰值 3080192 字节；这些仅作诊断量，不能据单档推定 20 GiB/72 小时通过。独立 bucket 实际读回硬配额 21474836480 字节。该失败报告未在查询前保存完整对象清单，现已修正报告时序：先保存负载/对象/物理成本，再执行查询，查询失败仍保留档次数据且不得判通过。

首档失败原始报告为 `/private/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-trace-capacity-285a431fcaa244eca6ad79d6b3538fca.json`，对应冻结源码同级 `capacity.log` 保留错误、单条明细 0.37～0.79 秒及清理证据。该轮自动退出核验容器、网络、卷零残留。用户随后明确批准先修查询瓶颈，再开同上限的本地窗口重测。

查询修正保留每主体二/每 Monitor 八请求、五秒、1000 Span、100 参与实例及 2 MiB JSON：明细最多四并发，System 当前 User 裁决与 Tempo 搜索/明细共用最多八个活动上游调用；按搜索顺序输出，任一错误/预算超限取消并等待工作者，整体重新授权前不返回部分数据。冻结源码 `lr0ofhwc/source` 的 Monitor Go T1 及竞态检查通过，包含四并发/八上游/主体限额/顺序/撤权/取消/累计预算；原默认 `make test-monitor-traces` 326.97 秒通过并清零。Tempo 默认五十分片仅为调查候选，没有未经对照修改。

2026-10-20 23:53 的重测启动误判了含其他会话 make/link 的 guard；完整日志确认第一档已短时开始，实际次数未完整保存，不能记零流量或有效容量。中止暴露 Compose down 遗留一次性 trace-load 的清理缺口：自动清理失败与本方按项目/服务标签核验后恢复清零分别记录在 `addp-trace-capacity-f503663cd61b47d58d2f4e3a099636bd.json`。门禁现先等待自有 Compose CLI 退出，并显式移除本轮一次性发生器，再 down 和核验零残留；新增模拟正常移除与拒删失败回归，不改变个人服务或其他 owner 资源。

2026-10-21 冻结源码 `k0pjymvv/source` 完整重跑 `make test-monitor-traces MONITOR_TRACES_TEST_ARGS=--capacity` 退出 0，原始报告 `addp-trace-capacity-16a165520915484dac28ef86f7e0637e.json` 为 `passed`，清理字段 `zero_owned_containers_networks_volumes=true`。当次运行容器合计硬限制 6 CPU／5.75 GiB；274 次采样覆盖至 2013.49 秒，卷峰值 209321984 字节（199.625 MiB），待决 Trace 峰值 8404，Tempo 工作空间峰值 6217728 字节（5.93 MiB），未触发停止线、OOM、采集拒收、导出失败、提前淘汰或刷块失败。真实尾采样、自然五分钟切块、72 小时配置和 bucket 配额均未缩短或放宽。

| 档次 | 实际负载及发送结果 | 自然落盘样本 | 一次二十条查询 |
| --- | --- | --- | --- |
| 50 请求/秒 | 600.7696 秒，30000 请求；普通/5xx/慢 27000/1500/1500；漏发、传输错误、非预期响应均 0 | 19755 Span／3951 Trace；物理 4481024 字节；保留速率 32.883 Span/秒 | 4.68848046 秒；每条五 Span、两条匹配边，无 Partial |
| 70 请求/秒 | 600.9632 秒，42000 请求；普通/5xx/慢 25200/12600/4200；漏发、传输错误、非预期响应均 0 | 本档新增 58785 Span，累计 78540 Span／15708 Trace；累计物理 13139968 字节；本档保留速率 97.818 Span/秒 | 1.970312333 秒；同样完整二十条 |

两档 SDK 最终队列／丢弃／导出失败均 0；导出、Tempo 接收和活动对象块计数相符，所有保留数据自然落盘。单位成本采用两档累计物理成本的较大值 226.8298659 字节/Span；按至少 100 Span/秒、259200 秒和两份完整副本外推为 11758860247 字节（10.9513 GiB），低于 16 GiB 判定线，保留原 20% 余量。相同数据模型与公式下的预算边界约 146.10 Span/秒，不是已压测吞吐或可提高采样预算的依据；策略目标仍为 100 Span/秒。第一档查询距五秒上限仅约 0.31 秒，不能据两次样本承诺延迟 SLA。

原始报告与完整日志分别位于 `/private/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-trace-capacity-16a165520915484dac28ef86f7e0637e.json`、`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-capacity-k0pjymvv/capacity-final.log`。同一冻结输入的 `make test-infra-runtime-log-lifecycle` 为 58 项通过（39.872 秒），`GOMAXPROCS=2 GOFLAGS=-race make test-module MODULE=monitor GO_ONLY=1` 通过，`python3 scripts/ci/check-t2-ci-registration.py --repository .` 通过；既有 Monitor T2 自动登记命中新夹具，容量负载仍只由显式参数启动。另有默认功能 T2 通过（326.97 秒）及独立 IAM runner 20 项通过；后者只修正既有 PlatformTrace 迁移选择器断言，未扩展 IAM 权限。

共享主机并非独占：第一档负载准确时段为上海时间 00:07:13.037–00:17:13.807，第二档为 00:24:50.333–00:34:51.297。其他 owner 提供的日志文件时间估算中，p65 的 00:08:43.202–00:09:08.902 落在首档负载，p82 的 00:18:10.909–00:20:28.969 落在首档排空期，503 诊断的 00:27:48.655–00:28:14.678、00:29:08.459–00:29:35.868 落在第二档负载；这些不是原生进程起止记录，不能据此推断干扰因果，也不能称主机零重叠。发送完整性、异常丢失与查询仍按本方实际计数判定；容器的当次硬配额不代表宿主全部任务总配额。

此前各项失败、中止及未运行仍不计通过。本专项始终只做本地验证，不运行 Hosted，不接管/重启个人服务；本次结论限定于所测三 Go 进程五 Span HTTP 链、属性及单节点存储，未证明真实 72 小时留存/过期删除、长期合并峰值或生产 HA，也不替代全量 HTTP 指标及 QPS 告警实压。


### PostgreSQL 专业发现、查询与页面

2026-10-21，专业发现与查询主体随共享版本 `d5c39624f` 入库；Swagger 引用修复 `667774d20`，两项 Go 测试夹具修复 `5c0b555e625c51510649543e865c4c2520ac845b`。本批仅覆盖已确认纳管并绑定有效主机的 Infra／Business PostgreSQL，不开放 Tenant 引擎、业务表、SQL 正文或凭据。私有投影、统一 SD 准入、当前 User Owner 读取与返回前重核、十二项即时／四项摘要／当前数据库容器段趋势，以及真实产品页面已经具备；不把数据库整体消耗归算给租户。

- T1：仓库外冻结基线 `25addd430` 仅叠加本会话输入，`make test-module MODULE=monitor GO_ONLY=1`、最终 `make test-module MODULE=common GO_ONLY=1` 与 `make test-module MODULE=system GO_ONLY=1` 分别通过。最终 System 输入覆盖投影补充后的 4 MiB 预算和未来容器启动事实拒绝。两项旧失败分别来自负面服务身份误用 Web Token 类型、准入测试使用未启动的固定端口；修正夹具后全包通过，不改变产品授权条件。
- T2：最终 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 通过：39 项配置、9 项纯生命周期契约、真实 PG15／Prometheus 查询链路的十二项即时、四项摘要、容器隔离及趋势、统计重置速率断点、SQL 权限撤销时 `up=1` 仍清空、恢复后的新鲜数据，以及两作用域／两种认证布局的安装和启停。项目 `addp-metrics-t2-efa0ddcde25c4641b418f2811b582055` 退出核验当次容器、网络、卷与临时文件零残留。最终运行／探针输入与工作区逐文件字节一致；登记断言补充独立再验通过。未操作个人数据库或服务。
- T3：86 项 Monitor Node 单元通过；首次浏览器 240 项中 238 通过、两项新增场景误匹配 Select 箭头 SVG，修正为精确图表可访问名称。最终 PostgreSQL 专项九项浏览器及构建通过，覆盖双语单位／零值、URL 重载与前进后退、503／403／404／错误主体／SQL 缺样清空、Tenant／Service／Delegated／任一 Owner 权限缺失拒绝。其余 232 项浏览器来自同轮有效输入，不声称最终全量 241 项在同一调用重跑。
- T0：最终 `make test-infra-runtime-log-lifecycle` 51 项通过；Swagger 修复已通过正式生成与 86 个公开方法覆盖校验；部署发现 34 项沿无变化输入复用。既有目录和输入登记覆盖新 PG 集成测试与 Common 私有投影文件，未新增重复 CI Job。

专项首轮专业查询通过，但后续只读 HBA 布局的 Docker 读取返回通用失败，原根因未复现；现增加关闭式退出分类且不回显私有输出，最终该分支通过，不能据此宣称原根因已修复。第二轮初始速率在 95 秒期限尚未完整，整批失败；按既有 SD 30 秒、scrape 15 秒、SQL 5 秒、完整一分钟及余量导出 130 秒就绪预算，保留四样本／epoch／reset／新鲜度条件并加入安全诊断，第三轮通过。首两轮失败不计通过。

正式身份 T4 的新增专业读取及真实 Console 用途已于 2026-10-21 获用户确认，已接入现有 `engine-admission`；其后真实 Hosted 的结果见下文，不能由本地契约计为通过。冻结源码基线 `2e46ea4e0` 的 `make test-node-metrics-online-runner` 通过：115 项契约、Node／Bash 语法及 System 的 Metrics 身份夹具 Go 测试；新增两个采集来源的归属、私有状态、各自端口与 mTLS、清理失败处理一并验证。`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--source-root <冻结源码> --package repository --test engine-deployments"` 亦通过，使用实际映射和标准 `addp_iam_test` owner 锁，覆盖当前声明、主机有效性与整批失败回滚，不改变个人运行库。


来源生命周期提交 `c828ffd3b` 的 Metrics 作业 `118632764390` 已失败：PostgreSQL 来源分支通过，后续 `TestIntegrationMetricsHTTPAlertLifecycle` 将 Compose DNS `metrics-source` 当作 IP 解析。测试声明和唯一 SD 现统一为 Docker inspect 的真实来源 IP，并为同一一次性进程证书补齐该 IP SAN，Desktop 仍沿既有精确回环传输；不修改产品解析／证书校验或扩大准入。第一次本地复验因尚未同步该 SAN 在 SDK 自检处失败，第二次到达历史趋势前置条件而提前退出（当时未保存状态／组数，不能据此确认根因）；现保持原 45 秒等待及有效失败点要求，仅允许零观察组继续等待自然完整窗口，并保留状态／组数诊断。后续本机专项分别在进程观测前置因 `no_sample` 或 CPU `no_data` 失败，未到达最终 HTTP 告警分支；最终诊断显示两来源 UP=1、身份各一且启动时刻匹配，CPU 一分钟窗口仅两采样、前一分钟边界缺失，不能降低四采样／完整边界条件换取通过，亦未归因为并发或已确认的产品缺陷。失败后各次自有容器／网络／卷均只读核对为零。上述失败不计通过，本批 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality` 未通过，原生 Linux CI 和新的历史等待分支仍待实际运行。旧 Platform 作业已通过本方 47 项生命周期，后续既有开发生命周期聚合失败未定位为本方根因，不把该作业记通过。生产发布、T5 与七天自然留存未验收。

最终冻结输入的 `make test-module MODULE=monitor GO_ONLY=1` 再次通过（含新增历史等待测试文件的全包编译及全部 Go T1）；Python／Bash 语法与配置登记覆盖有效。Hosted 接入的最终查询参数回归沿 `python3 -m unittest scripts/test/platform-node-metrics-online_test.py` 86 项通过；两个新增指标项目的既有资源拒绝另沿 `scripts/test/online-hosted-node-metrics-gate_test.py` 对应场景验证通过。正式 Hosted 专业读取与页面扩展提交为 `89749f1d6`，调度后按真实结果追加，不能由这些本地契约计为 T4 通过。


2026-10-21 后续收口：`009a5e42598038f37cc0a202edfbe88a49acc7bd` 的 [Platform consistency](https://github.com/pampa0629/addp/actions/runs/39519915079/job/118960943889) 已通过。原 [Metrics 作业](https://github.com/pampa0629/addp/actions/runs/39519915536/job/118961147933) 在后续资源总览阶段被取消，不计整项通过。正式 [Hosted engine-admission](https://github.com/pampa0629/addp/actions/runs/39519991703) 首次在基础设施构建时连接 GitHub 超时，证据上传亦超时；第二次真实运行已通过管理员接入页面，但在 `engine-professional-current-container-queries` 阶段由验收脚本错误拒绝默认 `policy_version=0`，专业页面未执行，整项失败。第二次报告明确为 `cleanup=passed`、`infra_cleanup=zero_residuals`。默认预算未保存时合法版本为零；脚本和负面回归修正已随 `c4ff143d7` 入库，本次冻结该输入的 `make test-node-metrics-online-runner` 为 117 项 Python 契约、Node/Bash 语法及 Metrics 身份 Go 夹具通过，仍不替代真实专业 T4。

[Metrics 后续原生 Linux 运行](https://github.com/pampa0629/addp/actions/runs/39522656609/job/118975847648)（`95a8b2a25678475bdaa65ba07649f10344092916`，attempt 1）已通过真实 HTTP 告警的触发、签名通知及恢复，随后 `TestIntegrationMetricsHTTPRealSource` 在停源清空断言失败。旧日志中前置失败来源检查于 `02:31:00.147 UTC` 放行，Go 检查于同秒执行，但未保存失败 scrape 的原始时间戳，不能据此确证旧失败的唯一成因。代码核对及确定性红绿回归确认夹具存在评估时间竞态：前置检查使用 Prometheus 当前时刻，Go 消费者使用向下取整至秒的时刻，后者可能仍位于失败采样之前。现只把停源前置检查统一到消费者的整秒时间，保留原 65 秒期限、完整窗口、来源身份、对等进程存活和停源数值清空断言；不修改产品 PromQL、TLS、权限或新鲜度。本次冻结源码基线为 `53cc8e07ddd810dd43fccbfd0106279050c3b25a`，仅叠加两项夹具输入；完整 `make test-monitor-metrics` 已退出零：40 项部署配置、9 项生命周期契约、PG 专业来源与两种认证布局、PromQL 固定窗口、真实进程/HTTP 查询及告警/签名通知/恢复、停源 HTTP 和进程数值清空、对等进程存活、原生与 Desktop 受限来源、监控中心 WAL 恢复、六来源总览/TOP5 和资源告警生命周期均通过。项目 `addp-metrics-t2-855a0575dbd145eaacb375b10e0478ed` 的容器、网络、卷及临时文件核验零残留，原始日志 `/tmp/addp-http-stop-boundary-metrics.log`。两项脚本最终哈希与冻结输入一致；共享主机未声明独占，不据本轮测试推导性能 SLA。`python3 scripts/ci/check-t2-ci-registration.py --repository .` 通过，沿既有 Metrics Job 自动登记，无需新增 workflow；冻结副本因没有 Git 元数据而无法运行的首次登记检查不计通过。原生 Linux 新输入 CI 与正式专业 T4 仍待后续真实结果，不重跑旧输入、不恢复定时轮询。


### Metrics 原生 CI 预算与互补分组

2026-10-22，`328d41941` 停源时序修复后的原生完整作业仍未通过：[328 输入](https://github.com/pampa0629/addp/actions/runs/39604162101/job/119236588666)、[后继 eb9 输入](https://github.com/pampa0629/addp/actions/runs/39609484036/job/119254592962)、[441 输入](https://github.com/pampa0629/addp/actions/runs/39612107825/job/119263766820)、[dc36584 输入](https://github.com/pampa0629/addp/actions/runs/39623631812/job/119303604167) 均在约 20 分钟取消，不能用 HTTP 局部通过、条件跳过或清理片段计为整项成功。最新已取得日志显示 Job 于 `15:28:24 UTC` 启动，PG 专业来源、认证和生命周期持续至 `15:34:13`；其后 HTTP、停源、主机及中心恢复持续正常推进，总览刚于 `15:48:36` 开始便在 `15:48:39` 被取消，完整总览和资源告警尚未执行结束。没有证据要求放宽产品采样或新鲜度；问题在于新增 PG 长场景与其余全部回归串入原 20 分钟 Job。

现保留根 `make test-monitor-metrics` 默认全量顺序路径，CI 改为同一入口的 `postgres-source`、`http-quality`、`host-resources` 三个互补场景，各自随机项目及 Linux Runner，原 20 分钟上限、子步期限、窗口/权限/身份断言和退出清零均不变；`fail-fast: false` 不因一项失败取消其他项。稳定检查名 `Monitor metrics infrastructure` 汇总当前选择和整个矩阵结果，任何已选分组失败、取消或跳过均拒绝通过。不是并行保留旧单 Job 全量路线，也不新增产品配置或测试身份。本地分组真实结果及新输入云端结果按后续实际证据分别记录；之前排队的旧输入不取消、不重复派发，不恢复定时轮询。


本次冻结源码基线 `667b25923fd6b25007ae55e6741a806d22367351`，仅叠加本方分组编排输入。三个标准命令均退出 0：`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 为 315.279 秒，`--case=http-quality` 为 403.107 秒，`--case=host-resources` 为 736.779 秒，覆盖完整专业来源/认证/生命周期、真实进程与 HTTP 窗口/告警/停源清空，以及主机/受限来源/中心 WAL/总览 TOP5/资源告警。三个分组的容器、网络、卷及临时目录均由各自退出路径清零，随后另读回三类资源全部为零；共享主机并非独占，不由本机耗时承诺 Linux CI 时长。结果 `/tmp/addp-metrics-partition-results.json`，各自日志 `/tmp/addp-metrics-partition-{postgres-source,http-quality,host-resources}.log`；原生进程场景封装前后的检查语句 AST 完全一致（除函数作用域及 `global base`），没有删减原断言。默认全量路径仍包含同一批场景，新的本地证据来自三个互补分组，不声称本轮又执行了无参数全量调用。

T0：42 项指标配置契约（含分组选择和失败/取消/跳过拒绝）、38 项 CI 登记回归、实际 T2 登记检查和 `make test-workflow-security` 均通过。既有根 Make 入口、owner 自动发现和变更输入覆盖有效，无新增依赖或平行测试实现。先前排队的 `01925025a` / [Release 39626206650](https://github.com/pampa0629/addp/actions/runs/39626206650) 选择成功但 Metrics 作业 `119318812307` 实际跳过，不能作为替代通过证据；未取消或重跑它。当次新输入 Linux 三分组及稳定汇总检查尚待 CI，现已取得下述终态证据；专业指标/页面正式身份 T4 的既有缺口仍不由本次 T2 关闭。


2026-10-22，精确提交 `b100eda7b803d180e0db53297331ba618de11b67` 的 [Release 39634536388](https://github.com/pampa0629/addp/actions/runs/39634536388) attempt 1 已取得完整 Metrics T2 通过证据：

| 分组 / 汇总 | 实际 Job | 结论与范围 |
| --- | --- | --- |
| `http-quality` | [119356253850](https://github.com/pampa0629/addp/actions/runs/39634536388/job/119356253850) | `Verify isolated metrics lifecycle` 成功：完整窗口、真实 SDK 进程及 HTTP 查询、HTTP 告警触发/签名通知/恢复、停源清空与对等进程存活；HTTP 告警场景实际 180.29 秒。 |
| `postgres-source` | [119356254038](https://github.com/pampa0629/addp/actions/runs/39634536388/job/119356254038) | 同一标准入口成功：真实 PG 专业指标、来源、认证和安装/停用/恢复生命周期，非 HTTP UP 或局部阶段替代。 |
| `host-resources` | [119356254095](https://github.com/pampa0629/addp/actions/runs/39634536388/job/119356254095) | 同一标准入口成功：主机及 Linux 受限来源、停源与新样本恢复、SIGKILL 后 WAL 重放、六来源总览/TOP5、资源告警触发/通知/恢复；资源告警场景实际 180.24 秒。 |
| 完整汇总 | [119387244768](https://github.com/pampa0629/addp/actions/runs/39634536388/job/119387244768) | `Require every selected metrics case` 成功，三个已选分组全部真实成功，非条件跳过。 |

三个分组完整原始日志均以 `zero owned containers/networks/volumes and temporary files` 结束。本会话复核全部日志文件与独立审计记录的 SHA-256 一致，日志源码为上述精确提交，并再次通过 GitHub API 确认四个 Job 均为 `completed/success`；未重跑测试。证据位于 `/tmp/addp-ci-20261021-180013/job-{119356253850,119356254038,119356254095,119387244768}.log` 和 `metrics-complete-independent-audit.json`。此前约 20 分钟取消的记录仍不计通过；本次仅关闭 Metrics 完整 T2 的 CI 预算收口，不将父 Release 的其他 Owner 结论并入，也不扩展为 PostgreSQL 专业页面 Hosted T4、七天自然留存或 T5 通过。定时任务保持停用。


### PostgreSQL 来源准入协议修复

2026-10-22，冻结 `c7753fe38fa55d3d2431d42f4ac49c05c3ba754c` 的 `make test-node-metrics-online-runner` 通过 117 项契约及 Metrics 身份 Go 夹具。随后 [Hosted engine-admission 39680591321 / 119501760697](https://github.com/pampa0629/addp/actions/runs/39680591321/job/119501760697) 通过真实管理员引擎接入页面，在 `engine-professional-current-container-queries` 因 130 秒完整采样窗口未就绪而失败，专业页面尚未执行；外层报告为 `cleanup=passed`、`infra_cleanup=zero_residuals`。运行中发现 HTTP SD 由 200 转为 422，不能把接入页面或来源 UP 计为专业 T4 通过。

确定性实源回归复现了协议缺陷：PostgreSQL 的 Nginx 在缺少客户端证书时返回通用 400，而同一 Monitor `SourcePolicy.Admit` 只认可明确 TLS 证书拒绝或 HTTP 401/403。旧来源 T2 直接接受 400，未经过该准入。现保持 `ssl_verify_client on`，仅将 Nginx 内部证书缺失/校验失败 496/495 映射为 403，拒绝请求不转发上游；不修改 Monitor 准入、TLS 信任、权限、窗口或新鲜度。新增实源 Go 集成由原 `postgres-source` 探针调用，覆盖同一生产准入、无证书及不受信证书拒绝；既有输入登记和 Metrics CI 已覆盖，无新增平行门禁。

冻结上述基线、只叠加本方三项来源/测试输入的标准验证：

- 红阶段 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 在新实源准入测试明确失败为 `metrics source protection could not be verified`，原始日志 `/tmp/addp-pg-admission-red.log`；退出后另核对项目 `addp-metrics-t2-a0a767df28ca404799e582dc6f33cab7` 的容器、网络、卷均为零。
- 修正后的同一完整入口退出 0：十二项指标/四项摘要/当前容器趋势、统计重置、SQL 失败无旧值、两作用域与只读认证目录、安装/停用/恢复及新增正式准入/证书拒绝均通过。项目 `addp-metrics-t2-e4c94596f14645fd83228e116598b893` 的容器、网络、卷与临时文件清零，随后独立读取三类资源均为空；日志 `/tmp/addp-pg-admission-green.log`。
- `make test-module MODULE=monitor GO_ONLY=1` 全包通过，日志 `/tmp/addp-pg-admission-go-t1.log`；首次因冻结副本无 Git 元数据而未运行模块门禁的调用不计通过，补齐副本元数据后沿同一标准入口重跑。T2 CI 登记校验通过，最终三项输入与冻结验证副本逐文件字节一致。本轮不重跑输入未变的 HTTP/主机 T2，不操作个人数据库或服务。

上述实源红绿证明来源协议修复，尚不能替代新源码的原生 Linux CI 或正式专业 T4；后续结果须按实际执行与退出清理记录。


### 引擎监控的当前承载主机导航

2026-10-22，引擎监控列表与详情增加当前承载主机名称和资源跳转；沿 System 当前有效 `node_id`，只在已有 `platform.host_node.read` 下读取当页主机名称。不回用 `declared_node_id` 或旧部署别名；失效关联明确显示当前无有效关联，名称读取失败只保留当前 ID，不清空合法引擎指标。服务与引擎页面共同使用 Monitor 的 `MonitoredHostLink` 领域组件，删除服务原内联导航；名称加载继续复用唯一 `loadHostReferences` 的去重、四并发和迟到结果保护。跳转使用原 Monitor 导航入口及 push，浏览器返回恢复原筛选、详情、指标、范围和刷新选择；不增加权限、API、后台读取或部署操作。

冻结基线 `1e59fda800edf9ff1e8d33ddccb891d123b765f6`、只叠加本方前端输入的 `make test-monitor-frontend` 退出 0：87 项 Node 单元、266 项确定性浏览器及生产构建通过。新增五项浏览器覆盖中英文详情/主机跳转/原趋势 URL 返回、列表当前名称与改名、名称 403 时指标保留、撤权拒绝迟到名称与后续额外读取、失效关联不回用旧声明；原服务主机名称与撤权回归亦包含在完整门禁。中英文截图已检查，保存在 `/tmp/addp-engine-host-nav-visuals/`；完整日志 `/tmp/addp-engine-host-nav-frontend.log`。成功后原浏览器输出目录与端口租约由标准入口移除。

`make test-frontend-ci-registration` 最终通过 20 项登记回归及 21 个受控前端登记检查；首次因冻结副本没有 Git 元数据而未完成实际登记检查，补齐副本元数据后重新执行才计通过。既有 Monitor 前端 owner/Make/CI 路径自动覆盖，无新增测试路线或 workflow。九项实现/测试输入与验证副本逐文件一致，另核对冻结基线的 8677 个未改跟踪 blob 一致；仅文档摘要随后更新，不以共享工作区其他改动替代验证输入。

本批没有读取或轮询云端 CI、调度 Hosted、恢复定时任务、修改个人数据库或重启服务。上述结果为 T0/T1/T3，不替代 PostgreSQL 正式身份 T4、原生多宿主/多进程或生产 T5，云端结果继续等待专门会话回传。


### PostgreSQL 专业观测正式 Hosted 验收

2026-10-22，CI 专门会话回传 [Hosted 39683888149 / Job 119512936927](https://github.com/pampa0629/addp/actions/runs/39683888149/job/119512936927) 的真实通过结果。本会话独立复核官方制品：源码为 `b08f2331dc6c76b520b958dc5a3d79c054f80900`、工作区干净，System／Monitor／Gateway 三服务均为相同提交；正式平台管理员密码/MFA及无权限对照身份与当次输入一致，独立 Prometheus 身份隔离保持。场景报告为 `stage=complete`、`result=passed`，外层 `cleanup=passed`、`infra_cleanup=zero_residuals`。

当次 Infra／Business 两个单容器 PostgreSQL 均覆盖：发现与批量接入、来源变化与重复拒绝、整批回滚、版本管理与退役；专业观测固定十二项即时、当前页四项摘要、当前数据库容器趋势、真实 Console 页面重载/有效零及单位、身份拒绝、来源停止清空与恢复新样。浏览器证据记录一次双部署摘要、四次即时和四次趋势响应，主体及固定目录完整，已检查 `engine-resources-detail.png`。官方制品在 `/tmp/addp-ci-20261022-manual-followup/artifacts-39683888149/`，协调审计为同目录上级的 `independent-passes-audit.json`；本次复核没有重复运行验收。

结论仅关闭上述提交的 PostgreSQL 专业查询与页面正式身份 T4，不能覆盖其后 `4a0a9ea1d` 新增的主机导航、其他引擎/容器/集群、七天自然留存或生产 T5。先前失败仍分别保留，不将本次通过追溯为旧输入通过。

同时回传的 [Release 39683857764 PostgreSQL 来源 T2 / Job 119520816486](https://github.com/pampa0629/addp/actions/runs/39683857764/job/119520816486) 已失败：新增准入探针的 Python `subprocess.run(timeout=45)` 终止整个 `go test ... -timeout=30s` 命令，日志出现依赖下载而未见 `=== RUN TestIntegrationMetricsPostgreSQLAdmission` 或 Go 测试自身超时报错。外层期限包含下载、编译与执行，不能把该结果当作 mTLS 断言失败，也不能据温缓存本地通过解释冷构建耗时的唯一原因。原始日志 `/tmp/addp-ci-20261022-manual-followup/job-119520816486.log`。若后续仍复现，先分清构建准备与实际测试期限，保留原测试超时和准入断言；不直接放宽等待。当前已有自动复验由专门 CI 会话跟进，本会话未查询、重跑、取消或设置定时；该新增 Linux T2 仍不计通过。


### PostgreSQL 准入探针的构建与执行期限

2026-10-22，CI 专门会话再次回传 [4a0a9ea1d 的 PostgreSQL 来源 T2 / Job 119557101259](https://github.com/pampa0629/addp/actions/runs/39696991326/job/119557101259) 失败。它与前次 `b08f2331d` 失败使用完全相同的准入探针，均由外层 45 秒终止包含依赖下载、编译和执行的 `go test`，日志未进入准入测试。现将同一正式准入分为私有目录 `go test -c` 准备和直接执行两个阶段：每次准备最多 180 秒，实际测试仍为 Go 30 秒、外层 45 秒；构建失败立即退出，不执行遗留文件，执行超时不重试、不计成功。产品代码、mTLS 证书拒绝、权限、采样窗口、新鲜度及 Job 20 分钟上限均未改。

冻结基线 `0cad0ddf334524fa31295b167389002b0edd9e1f`，只叠加本方输入，沿标准 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 执行红绿验证。新增确定性回归用模拟的 70 秒冷准备及 1 秒执行复现旧计时缺陷（无实际等待），并覆盖构建失败禁止执行旧文件、真实执行超时保持失败；红阶段为 2 项失败、1 项错误，停在配置检查，日志 `/tmp/addp-pg-admission-budget-red.log`。修正后同一完整门禁退出 0：45 项配置回归、9 项生命周期契约，以及 Infra、Business、只读认证目录三次真实正式准入均通过；十二项专业指标、四项摘要、当前容器趋势、统计重置、SQL 失败/恢复、只读认证目录的原子更新/重载/还原和安装/停用/恢复生命周期亦通过。日志 `/tmp/addp-pg-admission-budget-green.log`，末尾确认容器、网络、卷及临时文件零残留；随后独立读取精确项目 `addp-metrics-t2-54b61b59556949878eed8c39bd039590` 的三类 Docker 资源均为空。

两个最终脚本与成功验证副本逐字节一致；T2 CI 登记检查通过，既有根 Make、输入登记及 Release 分组已覆盖，无新增工作流或平行门禁。本地真实 T2 使用现有依赖缓存，冷准备预算由确定性回归覆盖，不代替新输入在原生 Linux 的冷构建结果；该项继续等待专门 CI 会话回传。本轮不重复专业 Hosted 已通过验收，不自行查询、重跑云端 CI、设置定时或操作个人服务。


### 同模块双 Worker 的实源隔离

2026-10-22，既有 `http-quality` 标准 T2 扩展为同一模块一个 Backend、两个 Worker，仍使用同一受限非 root 来源容器、原 CPU/内存配额、独立 mTLS 及唯一 HTTP SD。三个真实 Common SDK 进程分别具有实例身份、启动时刻和端点；两个 Worker 各自的 CPU/RSS/时长与当前段趋势有效，双向分别交换实例身份、端点或启动时刻时，摘要和趋势均不能借用对方数值。先停止 Backend，再停止其中一个 Worker；两次均核对失败来源的当前值清空，仍运行 Worker 的即时值和趋势继续有效。既有 HTTP 完整窗口、历史故障、真实告警触发、签名通知及恢复断言保留。

冻结基线 `6d42e4621f2bed671814c11eb365fcde4064f301`，只叠加本方测试输入。首次完整 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality` 已通过新增三进程查询，但被旧 HTTP 夹具的两进程数量限制拒绝，不计整项通过；日志 `/tmp/addp-same-role-workers-t2-initial.log`，失败退出后独立确认项目 `addp-metrics-t2-aa424071344545babdb1d19ee508ad7a` 的容器、网络和卷均为零。同步 HTTP 查询与告警两处夹具约束后，同一标准完整分组退出 0：45 项配置回归、固定窗口检查、三进程与双 Worker 身份拒绝、HTTP 查询/告警/通知/恢复、依次停源与对等进程保持均通过。日志 `/tmp/addp-same-role-workers-t2.log`，项目 `addp-metrics-t2-67bc578dfb73485783ae7aabde6f5d87` 的容器/网络/卷及门禁内部临时目录清零，另读三类资源均为空。五项最终测试输入与成功冻结副本逐字节一致；T2 CI 登记检查通过，既有目录登记与 Release 分组已覆盖，不新增 Make/workflow 路线。

本次是 Docker Desktop 上的 Linux SDK／真实 Prometheus／Monitor 查询 T2，未执行真实 System 多 Worker 登记与宿主绑定、业务 Worker 副本启动或原生 macOS 拓扑 T4，不把局部通过扩大为这些范围。既有 PostgreSQL 专业 Hosted 证据不变；本批云端结果继续等待专门 CI 会话回传，不自行轮询、重跑或设定时，也未重启个人服务。


### Meta 双 Worker 标准启动与登记绑定专项

2026-10-22，按用户确认范围补齐原生 Meta 的唯一编号启动路径：`ADDP_DEV_META_WORKER_REPLICAS` 默认 `1`、仅接受 `1/2`，两个单元共用同一 Worker 产物，各自独立 PID、进程身份和可选指标/TLS；任务并发仍是每进程独立配置。标准启动复用健康单元、只补缺失单元，活动缩容及外来/重复 PID 拒绝；模块停止和局部重启覆盖全部选中 Worker。未改变 Common 登记、System 首次主机声明、Monitor 授权、IAM 或业务任务调度。首次应用需部署方先停止旧 Meta，再使用标准入口启动；个人服务与配置未切换。

同一 `platform-node-metrics` 增加 `meta-workers` Hosted 专项：真实一 Backend／双 Worker 登记和同主机绑定，独立端点及当前段资源/趋势，精确停止一个 Worker 后由标准入口补回且健康实例保持，主机启停、中心故障与新样恢复，正式密码/MFA的真实 Console 双实例趋势及主机导航，最后配置退役和整套部署清零。身份、权限与 1800 秒场景／60 分钟 Job 预算均沿原路线；不执行租户任务、加压或读取租户业务。该专项尚未实际调度执行，不能计为真实 Meta 登记/绑定 T4 通过，也不覆盖原生 macOS、跨主机、容器副本、任务竞争或 HA。

本地验证使用冻结基线 `016f606ee18892a20a0497308f681ca03f69e994`，只叠加本批 24 项实现、规范与测试输入，避免共享工作区其他会话 WIP。最终 `make test-dev-lifecycle` 退出 0：100 项启动/配置测试、59 项 Infra 生命周期回归、完整 Bash 生命周期/局部重启及 Common schema/repository/projectionstore 回归通过，四模块 server/worker 命令包编译通过。新增四项测试使用真实原生 Python 进程夹具执行生产 Worker 启动和精确停止入口，覆盖逐实例指标/主机输入、复用、只补缺失、缩容/重复/外来 PID 拒绝；它们不是正式 Meta Go Worker 登记或页面 T4。日志 `/tmp/addp-meta-workers-dev-lifecycle-receipts.log`。

曾有一轮生命周期提取夹具缺少启动单元初始化，修正后完整门禁通过；随后一次复验及一次带诊断复验在新增夹具的五秒启动收据等待处失败，当时两个进程仍存活。保留期限并补进程/收据/控制器诊断后，最终串行完整门禁通过；未证明这两次等待失败的唯一原因，不将后续通过追溯为已修复未知环境故障。失败日志 `/tmp/addp-meta-workers-dev-lifecycle-verified.log`、`/tmp/addp-meta-workers-dev-lifecycle-final-check.log`。Online 首轮 123 项及身份 Go 夹具通过；一次并行复验有两项既有引擎 CLI 夹具失败，未计通过，后续结果须按下述最终记录判断。

最终串行 `make test-node-metrics-online-runner` 退出 0：123 项 Python 正反向契约、JS/Bash 语法及 System Metrics 身份 Go 夹具通过，日志 `/tmp/addp-meta-workers-online-runner-final-check.log`；包括新 Meta 单元独立证书/计划、限定标准命令、SDK 来源映射、当前实例 DTO、真实浏览器收据拒绝与清理编排。并行轮引擎 CLI 失败的唯一原因仍未确认，仅增加测试阶段输出诊断，未修改引擎产品、认证、读取期限或清理边界。`make test-workflow-security` 退出 0（原标准 Release workflow 固定版本安全审计），实际 T2 和 Online CI 登记检查均通过；新增 choice 已同步原 Online 受控登记及删除 choice 的拒绝回归。24 项最终输入与成功冻结副本逐字节一致，之后仅更新本证据与后续事项摘要；本次未读、轮询或重跑云端 CI，未设置定时，也未重启个人服务。


### 服务监控的登记地址查询与重置

2026-10-22，服务监控补齐按登记端点主机名／IP 精确筛选及一键重置。复用 System 已有 `registered_host`，由服务端在计数、分页前过滤；不按端口、DNS 解析结果、运行环境主机名或承载主机补匹配，也不为无监听端点的 Worker 推断地址。地址沿唯一 URL 保留到分页、刷新和趋势重载，未提交的地址草稿不影响自动刷新；重置清空筛选、页码及趋势，恢复在线且已声明采集的默认范围，保留刷新频率，同一默认 URL 上也清除未提交草稿。无新增 API、Swagger、权限或采集配置，未重启个人服务。

冻结基线 `d75dce1f1f2e345e7686e8d073baad9e8915ef48`，仅叠加本批八项实现／测试／规范输入，使用实际核验为 Node `v24.19.0` 的运行时，沿标准 `make test-monitor-frontend` 最终退出 0：88 项 Node、269 项浏览器及生产构建均通过。新增中英文回归验证同一主机不同端口均匹配、不借用相同宿主名／未登记端点的 Worker、只请求当页摘要、趋势及重载保留地址、重置与未提交草稿隔离；已检查截图 `/tmp/addp-service-address-visuals/service-address-{zh-cn,en}.png`。日志 `/tmp/addp-service-address-frontend-final.log`。首轮 268 项通过、中文新用例因英文关闭按钮名称定位超时而失败；改用同页既有关闭按钮定位后，完整门禁重跑通过，未调整产品时序或等待期限。首轮日志 `/tmp/addp-service-address-frontend.log`。

`make test-frontend-ci-registration` 通过 20 项回归及 21 个受控前端登记检查，日志 `/tmp/addp-service-address-registration.log`；现有 Monitor owner 与 CI 路径自动覆盖，无新增工作流。八项最终输入与成功冻结副本逐文件字节一致，随后仅追加本证据及后续事项摘要。成功浏览器输出目录与端口租约已由标准入口清理；首轮失败证据按标准保留在临时目录。本次为确定性 T1/T3 及构建验证，不计正式身份 Hosted T4，通过结果已交清单维护会话；CI 与已接入的 Meta 专项继续等待专门会话回传，未查询、调度或设置定时任务。


### 最新 Linux Metrics 三分组人工复核

2026-10-22 10:43（Asia/Shanghai），按用户要求仅手动核对现有自动运行，没有设置定时、重跑门禁、手工触发 Hosted 或重跑容量。最新已完成且实际执行三个 Metrics 分组的结果为精确源码 `c7ef8ea0e69afdb75ae6d194a381a53a77b4cd3f` 的 [Release 39703683536](https://github.com/pampa0629/addp/actions/runs/39703683536)，attempt 1、push 触发；包含既有来源 IP/SAN、停源整秒对齐、PostgreSQL 准入准备/执行分离和同模块双 Worker 测试输入。

| 分组 / 汇总 | 实际 Job | 人工复核结果 |
| --- | --- | --- |
| `http-quality` | [119586364227](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119586364227) | 标准 `Verify isolated metrics lifecycle` 成功；三真实 Linux SDK 进程及双 Worker 隔离/互换拒绝、HTTP 完整窗口与历史点、告警触发/签名通知/恢复均通过（告警 165.23 秒）；依次停止 Backend 和一个 Worker 后仅对应来源清空，其余 Worker 保持有效。 |
| `postgres-source` | [119586364302](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119586364302) | 同一步骤成功；三次正式来源准入测试实际进入并通过（各 0.02 秒），完整专业查询、来源认证及生命周期通过，新增冷准备与实际执行分离已由原生 Linux 路径覆盖。 |
| `host-resources` | [119586364231](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119586364231) | 同一步骤成功；主机/受限来源、停源恢复、监控中心 SIGKILL/WAL 重放、六来源总览/TOP5 与资源告警生命周期通过（资源告警 150.36 秒）。 |
| 完整汇总 | [119597645628](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119597645628) | `Require every selected metrics case` 成功，三个已选分组全部实际执行并通过，未用条件跳过代替。 |

GitHub API 确认四个 Job 均为 `completed/success`；四份原始日志的 checkout 均为上述 SHA，三个分组末尾均明确 `zero owned containers/networks/volumes and temporary files`。原始日志、Job 元数据及独立哈希审计保存在 `/tmp/addp-p19-metrics-manual-20261022/`，审计为 `audit.json`。这是复核既有 Linux T2 的结果，没有重新执行测试；文档增量通过 `git diff --check`。在仓库外冻结 HEAD `48515ba5ecfb1064630530490c05f9c766e2c35b`、仅叠加本段证据的标准 `GOMAXPROCS=2 GOFLAGS=-p=2 make test-changed` 实际选择平台 T0，但在 `TestTransferReviewSourceDrift/config` 因既有 `transfer/docs/transfer-基本概念及配置说明.md` 语义/溯源漂移而失败，未执行完后续门禁；该文档与冻结 HEAD 逐字节一致，本次未修改，不计 T0 通过，也不扩修无关内容。日志为审计目录的 `evidence-test-changed.log`；此结果与上表既有 Linux Metrics T2 成功分别记录。

此前 ParseAddr、停源竞态、20 分钟取消和准入外层 45 秒失败仍按其原输入保留，不追溯改成通过。本次关闭上述 `c7ef8ea0e` 输入的完整 Linux Metrics T2 缺口；核对时更晚 main 的运行仍有排队或 Metrics 未选中，不能扩大为最新共享工作区、后续前端改动、额外 Hosted T4、72 小时留存/容量、生产 T5 或其它 Owner CI 全部通过。


### 服务实例时间查询共享入口

2026-10-22，服务监控接入 System 已有实例时间契约：登记／离线判定时间、全部时间、15 分钟至 7 天预设及自定义范围；相对时段每请求从同一当前锚点重算，自定义范围固定为 URL 中的 UTC 起止，边界含起点、不含终点。当前状态、采集声明与时间取交集，不自动切换状态；恢复在线的实例退出离线时间结果。时间筛选不进入进程趋势请求，退出实例仍不开放历史趋势。查询、分页、重载、草稿隔离与重置沿原页面生命周期；新增相应时间列并保留在线状态与采集状态相邻。

时间控件、预设目录、RFC3339 规范化和 API 边界换算已统一到 Common Frontend 的 `RuntimeInstanceTimeFilter`／`runtimeInstanceTime.mjs`；System 原内联控件、日期校验与换算删除，日志页面改用同一预设目录，不保留旧导出别名。自定义范围未完整时的提示由共享控件就地呈现，删除 System 表格空态的重复提示。模块只组合自身列表和草稿提交；不改变 System 时间裁决、API/Swagger、权限或生产身份。

验证采用冻结基线 `a7353b132e7e8832549f247410671cec4e03fd1e`，叠加本批 22 项实现／测试／规范输入，Node 实际为 `v24.19.0`，不带共享工作区其他会话改动。`make test-common-frontend` 最终通过 249 项契约／工具／唯一所有权测试，日志 `/tmp/addp-service-time-common-verified.log`；`make test-system-frontend` 最终退出 0，152 项单元、194 项浏览器及构建通过，日志 `/tmp/addp-service-time-system-verified.log`。Console 的 `make test-console-frontend` 通过 188 项单元、206 项浏览器及构建，日志 `/tmp/addp-service-time-console-final.log`；随后对最终输入沿原标准浏览器入口补跑 `npm run test:e2e -- e2e/process-resource-navigation.spec.js`，九项服务导航全部通过，包括新增自定义时间提交、语言切换、重载及重置仍保持一个 iframe，日志 `/tmp/addp-service-time-console-scope.log`。

首轮失败均未计通过：新单元测试误保留默认趋势范围、System 两个日志消费者仍引用被删除导出、隔离 Console 副本缺少夹具模块依赖链接；另修正新增时间列插入位置与重复验证提示。相对时段新浏览器用例曾错误要求自然启动没有毫秒耗时，并在初始化或交互期间暂停时钟，分别造成空白页或加载遮罩退场停滞；改为页面就绪后仅暂停时钟验证窗口、交互前恢复。产品时钟、查询窗口、超时及业务断言未放宽，定向用例沿标准入口通过；相关失败和定向日志为 `/tmp/addp-service-time-{monitor,system,console}.log`、`/tmp/addp-service-time-monitor-{final,verified}.log`、`/tmp/addp-service-time-system-final.log` 与 `/tmp/addp-service-time-relative-clock-final.log`。完整最终 Monitor 结果按下段记录，不以定向成功替代。

最终 `make test-monitor-frontend` 退出 0：89 项单元、272 项浏览器及生产构建均通过，日志 `/tmp/addp-service-time-monitor-complete.log`；覆盖相对窗口随每次请求前移、未提交草稿隔离、自定义真实日期录入与 UTC 转换、半开边界、离线后恢复退出结果、中英文窄屏、分页／刷新／重载及趋势隔离。22 项最终实现／测试／规范输入与成功冻结副本逐字节一致。`make test-frontend-ci-registration` 通过 20 项回归和 21 个受控前端登记检查，日志 `/tmp/addp-service-time-registration.log`；现有 Common／System／Monitor／Console 路径已覆盖，无需新增 Make 或 workflow。

上述四组成功浏览器运行的输出目录与精确端口租约均已由标准入口清理，另行核对不存在。已人工检查此前同一共享控件的中英文窄屏截图 `/tmp/addp-service-time-visuals/service-time-{zh-cn,en}.png`；最终全量同场景及真实日期录入均通过。最终运行的补充截图捕获未取得新文件，仅为临时产物收集，不计新的截图证据，也未据此重复执行测试。本批为本地 T0／T1／T3 与构建验证，不代表正式身份 Hosted T4；CI 与 Hosted 等专门会话回传，没有查询、调度、设置定时或重启个人服务。


### 服务实例离线原因筛选共享入口

2026-10-22，服务监控复用 System 当前离线观测的 `stop_reason=graceful|lease_expired`，支持全部原因、正常退出及租约超时筛选，并在列表对应原因旁说明租约超时只表示失联。原因与状态、采集声明和时间取交集，不自动切换到 DOWN，不按浏览器时钟或采集失败推断退出；实例恢复后退出原因结果。沿原表单搜索提交、回到第一页并关闭旧趋势，分页／刷新／重载／Console iframe 保留已提交原因，自动刷新不应用原因草稿，重置清除原因；原因参数不进入进程趋势，离线实例仍不能打开历史趋势。

原因控件、枚举规范化和中英文译名统一到 Common Frontend 的 `RuntimeInstanceStopReasonSelect`／`runtimeInstanceStopReason.mjs`；System 原内联选择器和枚举删除，模块概览、实例列表与日志诊断同步消费共享译名。System 仍选择立即查询，Monitor 仍搜索提交；没有 API/Swagger、权限、后端状态裁决或部署变化。现有 Common／System／Monitor／Console 前端门禁及自动路径登记覆盖，不新增 Make、workflow 或测试路线。

冻结基线 `66b1f8ab8725bd3c32c9b8a0150694561c894a62`，仅叠加本批输入，实际 Node `v24.19.0`。最终 `make test-common-frontend` 退出 0，251 项通过，日志 `/tmp/addp-service-reason-common-final.log`；`make test-system-frontend` 退出 0，152 项单元、194 项浏览器及构建通过，日志 `/tmp/addp-service-reason-system-final.log`；`make test-monitor-frontend` 退出 0，90 项单元、275 项浏览器及构建通过，日志 `/tmp/addp-service-reason-monitor-final.log`；`make test-console-frontend` 退出 0，188 项单元、208 项浏览器及构建通过，日志 `/tmp/addp-service-reason-console.log`。新增中英 T3 覆盖原因／状态／时间交集、计数分页、范围切换、恢复移除、草稿隔离、重置、未知原因不分类，以及真实 Console 语言切换／重载保持一个 iframe。`make test-frontend-ci-registration` 通过 20 项回归与 21 个受控前端登记检查，日志 `/tmp/addp-service-reason-registration.log`。

首轮 Monitor 为 273 项浏览器通过、两项新用例失败：测试错误要求手动刷新保留未提交草稿，既有行为是手动刷新恢复已提交条件；改为断言选择不提交，并由独立自动刷新用例验证草稿隔离，未改产品交互。标准入口定向两项通过后，完整 Monitor 门禁通过，首轮与定向日志为 `/tmp/addp-service-reason-monitor.log`、`/tmp/addp-service-reason-targeted.log`。首轮 System 在发现运行日志仍引用移走的译名后精确中断当次独立门禁，不计通过；补齐消费者与唯一所有权回归后沿相同入口完整重跑通过，未停止个人服务。初轮日志 `/tmp/addp-service-reason-system.log`，保留中断状态与当时已通过项，不扩大为完整验收。

最终仅为新窄屏截图增加“下拉弹层已关闭”的断言，再沿 `npm run test:e2e -- e2e/process-resources.spec.js --grep 'service stop reasons'` 重跑中英两项退出 0，日志 `/tmp/addp-service-reason-visual-final.log`。已检查稳定截图 `/tmp/addp-service-reason-visuals/service-stop-reason-{zh-cn,en}.png`；18 项最终代码／测试输入与成功验证副本逐字节一致，三项规范及能力摘要同步更新。四组成功浏览器门禁和最终截图专项的输出目录、精确端口租约均独立核对已清理；失败／中断证据按标准保留在临时目录。本批为本地 T0／T1／T3 与构建，不计正式身份 Hosted T4；CI 和 Hosted 等专门会话回传，未查询、调度、设置定时或重启个人服务。


### 主机固定窗口探针的准备与执行期限

2026-10-22，专门 CI 会话回传 [6e55a4fad 的 host-resources / Job 119600712786](https://github.com/pampa0629/addp/actions/runs/39708924861/job/119600712786) 实际失败。原始日志 `/tmp/addp-ci-20261022-030015/job-119600712786.log` 显示同一 Python 60 秒包住依赖下载、Go 编译及真实执行：命令从 02:53:52 UTC 开始，约 02:54:46 才进入首项 Go 测试；DiskIO、Disk、Network、HTTP、Process、CPU、Filesystem 七项已通过，FilesystemInodes 开始后被外层终止。不能称测试未启动或产品断言失败。当前源码未有替代修复，现将同一八项选择器改为当次私有目录 `go test -c` 准备最多 180 秒，再直接执行二进制，Go／外层真实执行上限均为原有效 60 秒；保持 count=1、原八项断言、后续故障／告警／清理及 Job 20 分钟。构建失败／超时禁止运行旧产物，执行超时不重试，不改产品公式、窗口或权限。

沿标准 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources` 增加确定性红绿验证：模拟 70 秒冷准备与 8 秒执行，以及构建失败／超时、旧产物拒绝、实际执行超时不重试。原合并计时在配置阶段出现三项失败、一项错误，日志 `/tmp/addp-host-window-budget-red.log`。随后配置回归暴露旧选择器源码约束仍要求单个裸模式字面量，保留唯一选择目录再组合二进制参数后，最终 48 项配置回归全部通过；中间失败日志 `/tmp/addp-host-window-budget-green.log`，未计通过。T2 CI 登记检查通过，现有 Make、原 probe／配置回归的输入登记与三分组 Release 已覆盖，无需新脚本或 workflow。

真实本地 T2 在冻结基线 `a190663c261a7e63ff00885d006138571c41610e` 仅叠加本方三项输入，以 `GOMAXPROCS=2 GOFLAGS=-p=2` 调用同一标准入口，**未通过**，日志 `/tmp/addp-host-window-budget-final.log`。这次编译已独立完成，但首项 DiskIO 的 Docker promtool 调用在既有 30 秒 Context 内未完成（signal killed）；随后 Disk／Network 分别在 3.26／13.82 秒通过，HTTP 开始后达到独立执行的 60 秒上限。与云端的冷准备占用预算分别记录；伴随观测为本机同时运行其他门禁、18 个逻辑核且 load average 51.19／134.64／115.96，尚未证明它是执行变慢的唯一根因，未放宽期限或盲目重复运行。该次初始清理遇到异步一次性 promtool 资源仍占用网络／卷，不能计零残留；仅对精确项目 `addp-metrics-t2-d2a7516da2b74f4ab03fb83404308598` 复用原门禁的 compose／cleanup 函数完成后续清理，独立读取容器、网络、卷均为空，日志 `/tmp/addp-host-window-owned-cleanup-final.log`。首次复用清理因缺少当次 TLS 路径参数而未执行 Compose，补齐清理私有路径后成功；不接触个人服务、配置或其他项目。

三个最终输入与隔离副本逐字节一致；指纹、日志摘要及清理核验记录为 `/tmp/addp-host-window-budget-audit.json`。本轮仅将计时修复和 48 项 T0 回归落地，不计完整本地 T2、原生 Linux 或 Hosted T4 通过；新输入原生 Linux 结果继续由专门 CI 会话回传，没有查询、重跑或设置定时。来信同时附带的 PostgreSQL 来源 `6d42e4621` 三次真实准入成功及退出清零已核对原始日志哈希，与 P0 已记录的更新 `c7ef8ea0e` 三分组通过不冲突，不重跑或重复回填 PG，既有专业 T4 边界保留。


2026-10-22 原生 Linux 复验终态：专门 CI 会话回传精确源码 `9a2b3d28691741397f171b94866c125722bbe388` 的 [host-resources / Job 119660741437](https://github.com/pampa0629/addp/actions/runs/39727468958/job/119660741437)，push、attempt 1，实际 `Verify isolated metrics lifecycle` 成功。本会话复核本地保存的完整原始日志：真实准备编译约 46.67 秒，与保留的 Go／进程 60 秒执行上限分离；DiskIO、Disk、Network、HTTP、Process、CPU、Filesystem、Inodes 八项逐项 PASS。后续 ResourceQueries 两次、PressureEvidence 两次、ResourceOverview 和 ResourceAlertLifecycle 均真实通过，十四项 Go 运行零 FAIL／SKIP；完整门禁仍覆盖 mTLS 拒绝、溢出、来源故障／恢复、HTTP SD 失败／重定向／空集、OAuth、受限节点、中心 SIGKILL／持久样本、六源排行与查询前预算。告警在同一事件上由真实低样本恢复，签名 resolved 投递落库，`events=2 delivered=2 HTTP attempts=3`；末尾明确当次容器、网络、卷及临时文件清零。required Metrics 汇总 `119670125126` 成功由专门 CI 审计回传，本次未自行查询云端或重跑。

原始日志 `/tmp/addp-ci-20261022-063001/job-119660741437.log` 的 SHA-256 为 `54634e180dbd8d850f77e553ff9ec1206761ff73674b7d0c06aab3a2ceed4084`；中央独立审计为同目录 `monitor-9a2-linux-independent-audit.json`，本会话复核记录 `/tmp/addp-host-window-linux-receipt-audit.json` 保存两份证据指纹、八项窗口／十四项通过、零失败跳过、选择成功及清零回执。当前两个探针输入与已验源码字节一致；本轮只补证据，`git diff --check` 通过，不重复运行有效 T2。由此关闭该输入的原生 Linux host-resources 计时修复验收缺口；此前本地 promtool 30 秒／执行 60 秒失败与补清理记录仍按原结果保留，云端成功不证明其唯一根因或已修复。本结论不外推最新共享工作区、原生 macOS、72 小时留存、全部观测功能或生产验收；既有 PostgreSQL 专业 T4 与来源通过记录不变，未设置定时或操作个人服务。


## 服务监控承载主机 IP 筛选（2026-10-22）

Monitor 服务监控新增承载主机 IP 输入，复用 System `/platform/module-instances` 的 `node_ip`：前端只处理单值字符串与首尾空白，IPv4／IPv6、映射地址规范化和 `host_node_ips` 精确匹配继续由 System 拥有。无监听端点 Worker 可匹配；登记端点或运行环境名称相同但没有对应承载 IP 时不匹配，不由地址推断主机绑定。搜索、范围切换、分页、趋势、重载、主机跳转返回、自动刷新草稿隔离及重置沿唯一 URL；无效地址保留条件并显示查询错误，不回退全部结果，列表条件不进入趋势接口。双语说明位于输入框旁。

在冻结基线 `12fa8f30ba249cec7f70c8cceae47e3e4636a8c1` 上仅叠加本批输入，经 Node 24 标准入口验证：`make test-monitor-frontend` 的 91 项 T1、279 项确定性浏览器 T3 及生产构建通过。`make test-console-frontend` 首轮 189 项单测通过，浏览器 209 通过／1 失败：原有角色切换用例只提供 Worker，模拟接口此前忽略角色条件；本次让夹具按 System 的模块／角色／IP 取交集后，该用例错误地期待 Backend 查询仍返回 Worker。只修正此用例为 21 个 Worker 与独立 Backend，并断言实际 Backend ID、真实分页及零 CPU；产品筛选不放宽。随后沿 `npm run test:e2e -- e2e/process-resource-navigation.spec.js` 的全部 11 项服务导航用例通过，Console `npm run build` 通过。其他 209 项有效结果沿未变输入保留，未将定向复验冒充完整 210 项再次执行。

`make test-frontend-ci-registration` 的 20 项回归及 21 个前端登记检查通过；首次在无 `.git` 的归档副本执行时，回归已过但正式检查无法读取索引，补齐同一基线的前端 package 登记后通过，未修改 Make／workflow。现有 Monitor／Console glob、变更影响传播及 Platform CI 已覆盖新增用例，无需新入口。最终输入指纹及日志摘要为 `/tmp/addp-host-ip-final-audit.json`，原始日志为 `/tmp/addp-host-ip-*.log`；Console 实际 iframe 截图 `/tmp/addp-host-ip-visuals/console-host-ip.png` 已查看，双语窄屏无横向溢出由 Monitor T3 验证。此次仅为本地确定性 T1／T3 与构建证据，System 后端语义未改；未运行真实服务／Hosted T4，也未查询云端 CI、设置定时或重启个人服务，后续结果等待专门会话回传。


## 服务监控宿主／运行环境名称筛选（2026-10-22）

服务监控复用 System 既有 `node_name`，精确匹配部署声明的 `host_node_name` 或进程 `runtime_hostname`，忽略大小写。前端只规范化单值字符串首尾空白；不把登记端点、名称前缀或主机管理显示名称当作匹配对象，不按名称推断绑定或额外读取主机详情。输入框旁解释匹配范围；名称与 IP、模块、角色、状态、采集声明、离线原因及时间取交集。搜索／范围切换回第一页、关闭旧趋势；分页／重载／主机跳转返回保留条件，自动刷新只用已提交值，重置也清除未提交草稿，列表条件不进入趋势 API。无监听端点的多个 Worker 可按同一宿主名查询，各自实例与趋势身份不变。

冻结基线 `7b77919f87ef27201a7d754801e06dafe2c85832`，仅叠加本批前端、测试和规范输入，Node `v24.19.0`。`make test-monitor-frontend` 退出 0，92 项 T1、282 项确定性浏览器 T3 和构建通过；`make test-console-frontend` 退出 0，189 项单测、211 项浏览器和构建通过。新增双语场景覆盖宿主名／运行环境名匹配、前缀及同名登记端点排除、分页、趋势、组合条件、自动刷新草稿隔离、重复参数清除和重置；Console 覆盖名称与 IP 条件下的主机跳转返回、语言切换及重载保持唯一 iframe。`make test-frontend-ci-registration` 通过 20 项回归和 21 个前端登记检查；原批处理在此项启动前因检测到另一浏览器门禁退出，确认该入口只运行静态 Python 校验后单独执行通过，没有终止对方或重复执行有效浏览器结果。现有 Monitor／Console 门禁及路径扩散已覆盖，无需修改 Make 或 workflow。

原始日志 `/tmp/addp-host-name-test-{monitor-frontend,console-frontend,frontend-ci-registration}.log`，输入／日志指纹和成功门禁清理核验见 `/tmp/addp-host-name-final-audit.json`。本批输入与验证快照逐字节一致，标准浏览器输出目录及当次端口租约均已清理；保留截图 `/tmp/addp-host-name-visuals/`，已查看中文窄屏和 Console 页面，说明紧邻控件，双语窄屏无页面横向溢出断言通过。本批仅为本地 T0／T1／T3 和构建证据，未改后端、Swagger、IAM 或部署；未执行真实服务／Hosted T4，不查询云端 CI、不设置定时、不操作个人服务，后续结果等待专门会话回传。


## Transfer 两类双 Worker 标准启动与主机绑定验收入口（2026-10-22）

复用 Meta 的唯一启动单元实现，Transfer bounded／continuous 各由部署输入选择 1 或 2 个进程，默认各 1；单实例也使用 `-1` 编号。两类各复用原有二进制，独立 PID、正式日志进程身份、采集端点和 TLS 目录，共用部署声明的实际主机 ID。重复启动保留存活实例，只编译和补回缺失单元；活动缩容、重复／外来 PID、非法副本数及持续同步双副本配固定 Owner 均拒绝。Transfer 局部停止／重启覆盖全部四个 Worker，其他模块保持 PID。已有 continuous 故障验收改为唯一编号 PID，仍核验原二进制、专属部署、UID 和数据库边界；未改 claim、租约／fencing、任务并发度、System 登记协议或 IAM。

`platform-node-metrics` 增加 `transfer-workers`，与 `meta-workers` 共用验收主路径和 Console 浏览器实现：一个 Backend 加四个 Worker 独立登记并关联当次主机，按真实端点／SDK 身份及 `sync`／`continuous_sync` 类型校验来源；分别停止、补回两类中的一个 Worker，检查对等实例、当前趋势、新旧隔离、主机启停、观测中心故障和标准清理。已有 workflow 手工枚举、准入脚本及登记检查同次更新，仍用原预算、身份和零残留要求。这里只接通执行入口，未调度或执行真实 Hosted。

本地最终验证：

- `make test-dev-lifecycle` 退出 0：118 项部署／生命周期单测、59 项 Infra 日志／来源生命周期单测，标准端口与生命周期脚本通过；覆盖新增 Transfer 四 Worker 局部重启、其他模块保留、重启停机前拒绝及缺少第二实例时按共用产物补建。Common schema／repository／projectionstore 测试通过，Security、Meta、Quality、Transfer 的 cmd 入口编译检查通过（`-run '^$'`，不计业务执行测试）。
- `make test-node-metrics-online-runner` 退出 0：131 项 Runner／夹具／Hosted 编排／登记拒绝回归及 System Metrics 身份夹具 Go 测试通过；浏览器脚本语法检查通过，未运行真实浏览器 T4。
- `make test-transfer-continuous-online-runner` 退出 0：51 项持续执行 Runner／故障安全／隔离夹具回归及 System Online 身份夹具 Go 测试通过。

首次完整生命周期门禁在新增“第二 Worker 缺失”夹具失败：测试未选中对应 Backend，未进入标准构建阶段，因此没有 events 文件。夹具改为保留 Backend 与第一 Worker、仅缺少第二 Worker后，标准生命周期脚本和完整 `make test-dev-lifecycle` 重新执行通过；未修改产品启动条件或放宽断言。原始日志为 `/tmp/addp-transfer-workers-dev-final.log`、`/tmp/addp-transfer-workers-online-gate.log`、`/tmp/addp-transfer-workers-continuous-gate.log`，输入与日志指纹见 `/tmp/addp-transfer-workers-final-audit.json`。本次只验证本地 T0／T1 与原生临时进程生命周期，不计正式多实例登记／主机绑定 T4、租户同步任务、多 Worker 抢占或 HA 通过。未改个人配置、数据库或运行服务；CI 和 Hosted 结果等专门会话回传，未设置定时任务。


## Quality 多 Worker 标准启动、登记与主机绑定入口（2026-10-22）

复用 Meta／Transfer 的唯一启动与观测流程，新增部署输入 `ADDP_DEV_QUALITY_WORKER_REPLICAS`，默认 1、仅允许 1 或 2。单副本亦使用 `quality-worker-1`，双副本追加 `quality-worker-2`，共用原 Worker 二进制；独立 PID、启动身份及采集计划，沿既有主机声明及 Quality 正式登记。标准启动只补缺失单元，所属 Backend Ready 后才启动 Worker；重复启动复用现有实例，运行中缩容、外部 PID、重复 PID 与非法数量封闭拒绝；模块停止／重启覆盖所有编号单元。Quality 的 claim、任务并发、租约及权限未变，旧未编号启动路径已删除。

同一 `platform-node-metrics` 增加 `quality-workers` 专项，沿共用 runner、Console 场景和清理入口验证一个 Backend 与两个 `quality-bounded` Worker，登记与同主机绑定、独立采集身份、当前资源／趋势、停止补回一个 Worker、对等实例保持、主机启停和中心故障恢复。同步手工 workflow 选项与登记检查；原 `test-platform` 已聚合生命周期和 Online runner，不新增调度或第二套入口。

本轮标准本地验证均退出 0：`make test-dev-lifecycle` 包含 124 项启动／配置 Python 检查、59 项基础设施生命周期检查、完整原生进程／局部重启／构建 shell 夹具、Common 三个 package 校验，以及 Security／Meta／Quality／Transfer 的 cmd 编译检查（`-run '^$'` 不计业务用例通过）；`make test-node-metrics-online-runner` 的 138 项身份、TLS、绑定、趋势、浏览器回执及清理反例，加 System Metrics 身份 Go 夹具通过。原始日志 `/tmp/addp-quality-workers-dev.log`、`/tmp/addp-quality-workers-online.log`，输入及日志指纹见 `/tmp/addp-quality-workers-final-audit.json`；受测本批输入在门禁期间未漂移。本地原生进程为隔离临时夹具，未用个人真实服务证明登记／绑定。

真实 `quality-workers` Hosted T4 尚未运行；任务竞争、重复执行、租约接管、跨主机和生产 HA 均不在本批验收结论内。CI 与 Hosted 等专门会话回传；本轮未查询或调度云端验收、未设置定时、未修改个人配置／数据库或重启个人服务。功能清单由维护会话统一更新。


### Linux Metrics 后续输入复核（f09c1ecf9）

2026-10-22 按用户要求再次手动核对本专项 CI。精确源码 `f09c1ecf962b7184f71a0b92f489f2a7f1fada30` 的 [Release 39750726710](https://github.com/pampa0629/addp/actions/runs/39750726710)，push、attempt 1，已完整实际执行三组：`http-quality` [119734076006](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119734076006)、`postgres-source` [119734076020](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119734076020)、`host-resources` [119734075933](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119734075933) 的 `Verify isolated metrics lifecycle` 均成功，稳定汇总 [119755672239](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119755672239) 的 `Require every selected metrics case` 成功。汇总实际为 selected=true、选择及分组结果 success，未用条件跳过代替。

四份原始日志 checkout 均为上述 SHA，前三日志均明确当次容器、网络、卷及临时文件零残留；HTTP 十四项 Go PASS（告警生命周期 165.35 秒、真实 Linux 三进程、停 Backend/Worker 隔离）、主机十四项及 PG 七项均零 FAIL/SKIP。日志、元数据及 SHA-256 独立审计在 `/tmp/addp-p19-metrics-audit-20261022-f09/`，索引 `audit.json`。父会话复核日志哈希、执行项和清理回执，不重复执行测试；此前失败与取消按原输入保留。

较后 `7b77919f8`／39751069208 及 `610110d8`／39759525003 的 Metrics matrix 实际跳过，不能当作新 T2；核对时 `6eb1ce457`／39772136972 仍排队且尚无 Metrics Job。本次仅关闭 f09c1ecf9 输入的 Linux Metrics T2 待收口，不扩为当前共享 main、后续 Python Trace、额外 Hosted 或容量通过；未设置定时、手工触发或重跑。


### HTTP 追踪二期第二批（Python 本地）

2026-10-22 按用户确认范围，先修订运行观测规范及验收标准，再并行实现 Common Python 显式 SDK／ASGI／HTTPX 内核、Agent/Copilot 生命周期及日志关联；复用唯一部署声明、mTLS、Alloy/Tempo、采样与读取权限。只补同步 HTTP Trace，不把 Python 接入记为全量 HTTP 指标或进程指标，不增加第三方模型、异步 execution、SQL、其他 Runtime 或 Hosted 范围。生产部署投影仅显式新增 Agent/Copilot backend；依赖固定为 OpenTelemetry API/SDK/proto 1.45.1 并同步共享锁。未改个人虚拟环境，测试使用仓库外依赖叠加层。

冻结基线 `427c662b0c009ec331ec007587f1ed4a344b464d`，只叠加本批 35 个输入，输入清单与 SHA-256 为 `/tmp/addp-p19-python-trace-final/inputs.json`；原始日志及聚合报告位于同目录。代码、锁、配置和测试指纹在验收期间无漂移；随后 main 的 `f2694f848`／`f0a673271` 仅改 GeoPython 文档／算子／测试，不涉及本批受验输入。标准本地入口结果：

- `make test-common-python COMMON_PYTHON=/tmp/addp-p19-python-trace-final/bin/common`：411 项通过、48 子测试通过，1 项因该基础环境没有可选 GeoPandas 跳过，不计通过。包括真实回环 mTLS／官方 protobuf、安全投影、并发关联、SDK 环境污染与导入失败隔离、2048 队列、两秒失败计数及三秒关闭；HTTPX 保持原 TLS／超时／认证／流式协议，并拒绝外域和重定向携带平台关联头。
- `make test-copilot COPILOT_PYTHON=/tmp/addp-p19-python-trace-final/bin/copilot`：184 项通过，保留 3 项依赖弃用警告；`make test-agent-eval ADDP_AGENT_PYTHON=/tmp/addp-p19-python-trace-final/bin/agent ADDP_AGENT_EVAL_REPORT=/tmp/addp-p19-python-trace-final/agent-eval-final.json`：退出 0，7 项场景契约、Agent 评测／持久化及 Common Python 聚合均通过，仅 offline，不计真实模型／工具或 T4。Agent 最终输入还覆盖真实临时 Uvicorn 进程收到 SIGTERM 后六秒内退出 0，并实际经过追踪关闭 finally。
- `make test-infra-runtime-log-lifecycle`：60 项通过；`make test-python-ci-registration`：10 项回归及 11 个 Python owner 登记通过；`make test-workflow-security`：固定 zizmor 标准离线审计通过；现有 T2 登记检查通过（22 hosted-service／12 gate-owned Compose／9 hosted-only／2 owner-managed）。新增 Python 目录及混合源 fixture 已纳入 Trace 门禁输入，Linux Job 在执行前准备 Python 3.12、共享依赖及固定 FastAPI/Uvicorn，不新增门禁或手工触发路径。
- `make test-monitor-traces MONITOR_TRACES_PYTHON=/tmp/addp-p19-python-trace-final/bin/agent`：退出 0，整次 384.86 秒，真实 Pipeline 328.87 秒。Go Gateway → Python Copilot fixture → Go System 产生预期 5 Span／2 条匹配边；首阶段三进程分别导出 202／404／404 Span，丢弃与失败均为零。保留真实 120 秒尾采样，失败／慢链及 14/200 普通链可查；安全投影、历史参与实例、当前 User 撤权、读写 mTLS 隔离、只读代理，以及 System/Copilot 分别查询同一精确 Trace 的 Loki 元数据均通过。Collector 停止期间请求与 Ready 不受影响、损失计数出现，恢复后新链及五 Span／两边通过；三个受控子进程均正常退出，最后明确当次容器、网络、卷及设施临时目录零残留。父进程的 `TestOwnedTraceProcess` 是受控子进程专用 helper，未启用 child 标记时按原设计跳过，不计业务通过。

两项合并复核修正已验证：SDK 顶层读取非法环境不能破坏追踪关闭时的业务导入，初始化中途失败释放 Provider／Client／线程；默认 HTTPX transport 显式保留 `trust_env=False`，不重新引入环境 CA。Python fixture 使用自有 SIGTERM 处理，以确保 Uvicorn 信号重投后仍执行 finally，不吞 Go 子进程退出错误。极端持续滴流时，HTTPX 活动 socket 无公开取消接口，最多留一个网络守护线程，运行时禁止再出站／重入累积，迟到成功不得计数；该有界退化由阻塞传输测试覆盖，正常实链退出清零独立验证。

本轮只证明上述冻结输入的本地 T0/T1/T2；未运行 Hosted、容量重跑或真实 72 小时留存／删除，未重启个人服务或设置定时。先前容量外推及 Linux Metrics 证据保留原边界，不作为新增 Python 吞吐／长期容量或最新共享 main 的验收；本批推送后的远端自动 CI 结果另记。


### 服务实例 CPU 与 RSS 超限告警（2026-10-22）

实现范围：从当前授权运行实例中选择具体实例，分别设置 CPU 占用核数和 RSS 容量阈值；CPU 保持完整一分钟窗口，RSS 表单用 GiB 输入并转换为字节。规则新建停用、实例与指标不可变、更新/删除按 CAS；沿用平台资源规则权限。后台来源、连续窗口、事件协调器和事务 outbox 复用现有实现；`process_resource` 通知类别须显式订阅。只保留 opened/resolved 生命周期，离线、退出、缺样、查询错误和停用不恢复活动事件；重启不迁移旧规则，已有事件保持打开时的恢复条件。

验证结果：

- `make test-module MODULE=monitor GO_ONLY=1`：退出 0，Monitor Go T1 通过；包括固定单位、完整窗口、边界、缺样、重复样本、非法值与 API 权限/严格输入。
- 已执行 `bash scripts/infra/status.sh` 核对本机共享 PostgreSQL 实际映射为 25432；在 `ADDP_TEST_POSTGRES_DATABASE=addp_test` 下通过 `make test-monitor-postgres`，退出 0，33 项顶层 PostgreSQL T2 通过。新增 CPU/RSS 生命周期均覆盖 CAS、重复规则拒绝、活动事件删除拒绝、退出/启动身份改变/采集失败不恢复、恢复条件冻结、有效零值恢复、显式订阅与 outbox 隔离、停用竞争丢弃旧结果；当次夹具执行清理及规则残留核验。
- `make test-monitor-frontend` 中 96 项确定性测试通过；首轮浏览器 285/286 通过，失败仅为新增测试使用错误控件名称。后续新增证据/订阅场景并修正 Element Plus 禁用控件定位和两位小数断言；通过标准 `npm run test:e2e -- e2e/process-rules.spec.js` 定向复验，最终 7/7、退出 0。未修改的 282 项既有浏览器场景复用首轮通过证据，HTTP 与主机告警表单回归包含在内；不把首轮非零结果写成整批重跑通过。`npm run build` 退出 0。
- `bash scripts/swagger/gen-swagger.sh monitor` 与 `bash scripts/swagger/check-route-coverage.sh monitor`：最终均退出 0，90 个公开路由方法覆盖一致。共享源码在生成期间变化导致的一次拒绝已重新生成，不使用该次失败产物作为通过证据。
- 本地日志：`/tmp/addp-process-alerts-go.log`、`/tmp/addp-process-alerts-pg.log`、`/tmp/addp-process-alerts-ui.log`、`/tmp/addp-process-alerts-ui-final.log`、`/tmp/addp-process-alerts-build.log`、`/tmp/addp-process-alerts-swagger.log`。

边界：PostgreSQL T2 使用受控 System/指标来源夹具，浏览器 T3 使用隔离 HTTP 夹具，不能计为真实身份、真实 Prometheus 压力或正式通知端 T4 验收。本轮未重启个人服务，未执行 CI 或 Hosted，等待专门会话回传；未设置定时任务。


2026-10-22 补查 Console 嵌入场景：`console/frontend/e2e/platform-alert-navigation.spec.js` 新增服务实例规则弹窗用例，验证弹窗位于视口内、内容与遮罩透明度、名称输入、真实选择控件的候选选择及关闭；只模拟 HTTP 响应，不写个人配置。标准 `npm run test:e2e -- e2e/platform-alert-navigation.spec.js` 最终 Chromium 5/5、退出 0；`npm run test:e2e -- e2e/platform-alert-navigation.spec.js --browser webkit --grep painted` 的同一新增场景 1/1、退出 0。初次诊断用例的动态夹具导入及控件定位失败已修正，不计产品故障。现有 Console 浏览器门禁自动发现该文件，无需新增 CI 或依赖登记；WebKit 仅为本地诊断，未扩展 CI 浏览器矩阵。个人 Safari 仍出现辅助功能树有表单但用户与截图看不到弹窗，且控制工具有 noWindowsAvailable／ScreenCaptureKit 错误；隔离浏览器通过不能证明此真实故障已修复，仍待前台手动点击复现及根因确认。本轮未修改产品样式，未创建或启用真实规则，未查询 CI／Hosted、未设置定时。

### Python Trace 接入后的开发依赖／OpenAPI 阻断修复

2026-10-22 针对 `c05c70480` 引入共享 OpenTelemetry 依赖后的本地可复现阻断，核实 Copilot 旧 venv 没有 `opentelemetry`，editable `addp-common` 源码已更新但已安装元数据仍不含新依赖，旧 FastAPI／LangChain 导入探针却判完整。Agent 的共享元数据同样陈旧。全量 restart 在 start 的依赖准备之前生成 Python OpenAPI，因此仅补 start 探针仍不能解除阻断。

按既有生命周期原则先修规范：Agent/Copilot 共用 `addp_prepare_python_backend`，在共享安装锁内检查／创建声明版本的 venv，再交 `addp_sync_python_dependencies` 按完整 requirements、Common Python 输入及实际已安装环境指纹同步。删除两个 Backend 的 `NEED_INSTALL` 少量包探针与重复 pip 路线；相对 editable 声明固定在所属 Runtime 工作目录解析。全量 restart 停止后先准备两个 Backend，再生成全部文档；局部保持 Go 停服前预检，停止后只准备、生成和校验所选 Python Backend。普通 start 先检查服务运行状态，活动实例不改环境；同步／OpenAPI 失败不进入启动、不放宽错误。Math 既有完整共享导入失败会触发安装，Jupyter 指纹纳入共享 pyproject，其他原生 Runtime 已调用统一指纹同步；未扩展其产品能力或另加安装路线。

验证基线为 `b0daa28446e22dfa466c695caa7b27150ae39009`；产品输入与隔离副本逐字节一致，标准日志和指纹见 `/tmp/addp-p19-swagger-dependency-fix/`。`make test-dev-lifecycle` 最终退出 0，包含 124 项配置／部署 Python 检查、60 项设施生命周期检查、完整启停／原子构建 shell 回归、Common 三个 package 校验及四模块 cmd 编译检查（不计业务执行）。新增回归覆盖已有旧 venv 缺共享依赖、实际环境元数据失效、输入不变复用、失败清除成功记录、同环境并发只安装一次、共享锁释放，以及全量／局部 Python Swagger 前准备、停止／准备／生成／覆盖失败拒绝和未选服务保留。首轮新增夹具因 macOS `/var` 与 `/private/var` 临时目录别名比较失败，统一 resolve 后整套重跑通过，未放宽产品断言。`make test-build-registration` 的 59 项／9 项回归及 26 Go 单元、59 镜像登记通过；现有 Platform consistency 的 `make test-platform` 已包含本入口，无需新增 CI 编排。

将当前 Agent/Copilot venv 用 APFS 独立副本复制到隔离源码树，仅在副本调用标准 Backend 准备函数：完整 requirements 安装、`pip check`、应用真实导入、`gen-swagger.sh agent/copilot` 与 `check-route-coverage.sh agent/copilot` 均通过，分别 14／12 个公开路由方法；OpenAPI 与提交产物完全一致。再次调用同一准备入口，两者均按指纹跳过安装。原用户 venv 只读复核仍保持原元数据状态，Copilot 仍无 OpenTelemetry，未就地修包或操作用户服务。

使用上述真实准备后的隔离解释器，`make test-common-python` 411 项及 48 子测试通过，1 项可选 GeoPandas 缺失跳过、不计通过；`make test-copilot` 184 项通过（3 项既有依赖弃用警告）；`make test-agent-eval` offline 的七场景、Agent 及 Common Python 子门禁通过；`make test-swagger` 的 18 项脚本回归与所有 Go/FastAPI 路由覆盖通过。此次只修本地准备／生成链，无 API 契约变化、无需改 OpenAPI 产物；未运行 Hosted、容量或用户服务重启。用户需在自己的终端经标准 restart 同步旧环境，推送后的自动 CI 状态另记，不能把排队当作通过。


2026-10-22 23:22 用户在自己的终端完成重启后，本会话只读核验当前源码 `a4cdb6b81409d2d3237eba01cff3d1a8b86f8690`：Agent/Copilot 的 API／SDK／proto 三个依赖均为 1.45.1，两者依赖指纹与实际环境一致且 `pip check` 通过，`addp_python_dependencies_current` 返回可复用，没有执行安装。已核原生 PID 74166／74147 分别监听实际保存端口 8190／8087；两者 `/health/live`、`/health/ready`、`/openapi.json` 全部 HTTP 200，实时 OpenAPI 与已提交投影深相等。标准 `PYTHONDONTWRITEBYTECODE=1 bash scripts/swagger/check-route-coverage.sh agent copilot` 退出 0，分别 14／12 个公开路由方法一致。由此关闭本次本机缺依赖／生成阻断，不外推正式业务、LLM、Trace 尾采样或 Hosted 验收；未由 AI 重启或改写 venv。只读核对修复提交 `673cf8eef` 的自动 Platform CI `39803050868`，Platform consistency、Common Python、Copilot Backend 与 Agent offline 均仍 queued，不计远端通过。


### 观测身份投影 PostgreSQL 断言误报修复（2026-10-23）

CI `39775896930`／System IAM Job `119824550350` 的原始失败是 `private field leaked: 5433`。旧用例对整个 JSON 搜索端口数字，允许投影的随机 UUID 同样可能命中；原 CI 没有保存当次完整快照，不能还原具体命中的身份。将测试主机固定为合法 UUID `54330000-0000-4000-8000-000000000001`，沿标准 PostgreSQL 入口稳定复现相同误报。现改为解码 JSON 后比较完整预期结构和字段值，包括节点、进程、引擎、来源键及无专业来源时的 `metrics.state=unavailable`；任何额外字段仍拒绝，不删除私有字段的否定验证，不修改产品代码、IAM、投影或授权范围。

已先运行 `bash scripts/infra/status.sh` 核对当前共享 PostgreSQL 为 `25432`。在 `addp_iam_test` 使用标准 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package repository"`：旧断言加固定 UUID 的红色复现失败；修正结构预期后通过；随后仅在当次测试输入临时注入嵌套 `source_key.published_ports=[5433]`，同一入口按完整结构断言明确失败，证明泄露仍被拒绝；删除临时注入后最终重新执行，7 项 Repository 顶层 T2 与测试库重置均通过、退出 0，无跳过。最终文件与注入前通过版本逐字节一致。原始日志 `/tmp/addp-observability-projection-red.log`、`/tmp/addp-observability-projection-mutation.log`、`/tmp/addp-observability-projection-final.log`；中间首次结构预期遗漏已有指标状态且错误强制启动时刻为 UTC 的失败已修正，不计产品故障。

本轮仅修改既有测试和此证据，现有 Release/T2 System IAM Job 自动发现，无新增测试入口、依赖或 CI 编排。完整 System IAM CI 等专门会话回传，不把本地 Repository 分包 T2 计为全批 CI；未调度或查询 CI／Hosted、未设置定时、未重启个人服务。Safari 实际弹窗可见性问题仍未解决。
