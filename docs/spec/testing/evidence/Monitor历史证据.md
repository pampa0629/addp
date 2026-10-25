# Monitor 历史验收证据

[返回证据索引](../验收证据索引.md) · [Monitor 模块契约](../../../../monitor/CLAUDE.md)

以下汇总模块说明与后续专题中的历史记录，仅适用于记录所指源码、运行尝试与覆盖范围。本次整理没有重跑这些 CI，也未查询其最新状态；失败、排队、分项与整轮通过分别保存，不推导当前 HEAD 或生产 T5 已通过。

## 2026-10-24 概览保留只读评估第一期

Common 增加 `AssessRetention`，System 沿已有公共维护循环输出有界批次汇总。数据库强制只读／可重复读，使用数据库时间计算 180 天时间候选，并检查完整父子树、租约、关系异常、树外重试／恢复引用及预算。首期始终保留 Owner 行内事实释放和跨模块引用覆盖未核验的原因；不调用 Owner 私表、不新增用户 API、不提供物理删除或专业结果期限。批次不是全库唯一树数，不能累加为完整盘点。

本地标准验证：`make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=system GO_ONLY=1` 通过；`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test execution-retention"` 在实际 Infra 映射 25432／`addp_test` 通过 11 组 T2，覆盖多层树／子种子、年轻子执行、跨租户父子及反向引用、环与断链、恢复引用、非法 JSON 来源、查询／行／树预算、查询失败不发布部分报告、并发新增子执行的一致快照、取消和拒绝继承可写事务。执行行及专业配置／结果／错误和过期过程事件前后不变，夹具逐项清理后零残留。三个门禁的实时输入守卫均未发现变化；Common PostgreSQL 并发锁拒绝验证通过，拒绝发生在数据库测试启动前。默认 Common PostgreSQL CI 已登记并强制要求新增测试，不新增 workflow。

首次 T2 的三个夹具断言失败已保留在会话记录：共享时间指针被后续更新改写、时间精度比较及注入钩子时机；修正后沿同一标准入口通过。一次 System Go 运行虽通过，但期间修改了 Common 测试文件，守卫标记输入变化，不作为最终证据；最终两模块 Go T1 与 PostgreSQL T2 在同一稳定输入指纹下通过。

未运行：工作区整体 `make test-changed`、完整 Common PostgreSQL 聚合及真实维护 T4；共享工作区还有其他会话的业务改动，本批仅验证新增共享评估和实际消费者 System，未重跑其无关门禁。没有启动／重启个人服务，没有进行执行概览删除、Owner 释放裁决、专业结果到期或长期容量验收；此前固定版本 T4 不扩展为本功能通过。

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
| [39905778336 / Job120257300658](https://github.com/pampa0629/addp/actions/runs/39905778336/job/120257300658) | `5be2c2e0e9c6fb1f49da54a076753f2568462fd4` | 整套失败：14 项检查及四份浏览器分项报告通过，新增多页／保存游标撤权及不可见总数已有分项证据；但旧验收白名单拒绝 Transfer 已声明的专业 `initial_authorization.status/code`，成功的定时执行在校验及清理历史读取时被拒绝，task 3 未完成删除。领域 cleanup failed、残留 1；外层部署销毁不能抵消该失败，不计整套通过。 |
| [39923509570 / Job120314175151](https://github.com/pampa0629/addp/actions/runs/39923509570/job/120314175151) | `9e67500b26136ef909086d70ee9536d925ff3581` | 固定版本完整 Hosted T4 通过：六服务同提交，场景 176.851 秒，16 项唯一检查、四份真实浏览器报告一致通过。补齐保存游标撤权、不可见总数、定时扫描与父子读取隔离及删定义后历史；任务 3／2／1 均 verified_absent，领域残留 0、无不确定写入，部署清理及 Infra zero_residuals。官方归档 `12493568384`，范围及限制见下文。 |

Owner503 场景使用正式普通 User 与真实 Monitor 响应；故障夹具仅停止当次隔离部署中经 pidfd、可执行文件、UID、数据库和私有目录核验的 Transfer Backend，再恢复同一进程。浏览器分别验证终态详情、运行树、非空列表的清空与重新裁决；独立网络中断仅影响事件读取，不冒充 Owner 503。控制文件只在当次私有目录，公开回执不包含 Token、业务样本或完整接口响应。标准入口仍为既有 transfer-continuous-events Hosted suite 与 `make test-transfer-continuous-online-runner`。

### 2026-10-23 固定版本租户日志终审

对 `39923509570` 的官方归档及原始 Job 日志完成独立核验。归档 SHA-256 为 `5c32cadccc438db1e7c1e16cd5edd9056c0169c1a87fa75dbeb2efe57902be2d`；原始标准输出中的 suite JSON、`transfer-continuous-events.json` 与外层 `suite_report` 完全一致，源码干净且 System、Gateway、Meta、Transfer、Monitor、Security 六服务均为上述完整提交。四份浏览器报告逐一核对本轮 Run ID、Tenant、Principal、execution UUID 及各自的闭合检查集合，Owner 不可用详情清空及恢复诊断截图已目视复核，恢复来源绑定以本轮报告和 DOM 断言为据。既有标准入口仍为 `make test-online ONLINE_SUITE=transfer-continuous-events`，由原 Hosted 生命周期执行；修复提交的 `make test-transfer-continuous-online-runner` 已通过 57 项确定性回归及 System 身份夹具 Go 测试，本次终审没有重跑或再次触发。

同一 Tenant 2 普通读取 User 37 在 Owner 与 Monitor 实际读取每页 2 条、共 2 页 4 条事件；正式撤权后旧 Token 为 401，重新登录的同一 Monitor-only User 使用保存的第一页游标仍被 Owner 403／Monitor 404 拒绝，执行列表总数为 0。同租户无 Owner 权限及跨租户身份也返回空列表与零总数。真实 Owner 进程停止／同 PID 恢复、详情／树／步骤／事件与列表旧内容清空、独立事件网络故障，以及 Worker SIGKILL 后换 PID、租约收敛及新 execution 恢复均通过。Kafka、CDC 独立审批人及精确扫描边界沿原断言保持。

此前未达到的定时子阶段本次完整通过：普通 User 33 的 `scheduled` 执行、实际父／Meta scan 绑定、peer 可读父而 scan 为 404、物理行集、调度暂停及目标精确扫描均有报告证据。最后的 `deleted_definition_history_retained_with_owner_permissions` 实际执行。逐项清理回执为 scheduled 3、CDC 2、Kafka 1 全部 `verified_absent`，原始 Backend 日志有三次 DELETE 200 及各自随后 GET 404；suite 的 `cleanup=passed`、`residual_resources=0`、`uncertain_mutation=false` 与外层部署清理、Infra 零残留分别核验。

该通过只覆盖固定版本、此套件的真实身份与拓扑，不外推当前后续 HEAD、其他 Owner、Security 日志专项、Notebook 运行许可、容量、长时间运行、HA 或生产 T5。API 每页 2 条的多页证据不代表超过 100 条的浏览器加载更多、每日 1000 条截断或完整节点历史已验。`history_cleanup=pending_deployment_destruction` 表示本轮保留历史后随独占部署销毁，不能写成执行历史留存清理已验；30 天事件物理维护周期及 180 天概览整树安全删除仍未由此 T4 证明。旧 `5be2c2e0e` 失败保留，不由此次通过覆盖。

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


### PostgreSQL 专业来源固定版本 Linux T2 复核（2026-10-24）

专门 CI 会话回传 [Release 40048425338 / job 120741235374](https://github.com/pampa0629/addp/actions/runs/40048425338/job/120741235374)，固定源码 `5d91fa31c326fc636c73810edb542ae7f95e4db0`、attempt 2，由协调会话对原 push 的失败 Job 进行唯一一次定向复验。本会话独立核对原始日志、固定脚本与关键测试断言，没有重新运行或查询／调度 CI。实际 `Verify isolated metrics lifecycle` 执行 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source`，51 项准备测试通过（1.118 秒），进入真实 PostgreSQL、mTLS、Prometheus 及查询／生命周期阶段，最终该分组 selected／success。

覆盖十二项原生专业指标、四项摘要、当前数据库容器段趋势、启动边界／外来身份拒绝、统计重置留空及 SQL 失败清空／新样恢复；原始日志含四次 `TestIntegrationMetricsPostgreSQLSource` 和三次 `TestIntegrationMetricsPostgreSQLAdmission` 实际 PASS。Infra／Business 两个作用域均验证固定函数与受限采集身份、mTLS／受限 HTTP、权限拒绝、密码轮转及幂等；Business 另验证维护账号 SCRAM、其他库／socket 拒绝与原管理员规则保留。两作用域账号安装、NOLOGIN、重新启用、已有未知会话拒绝和凭据丢失拒绝通过，仓库外认证目录只读挂载、宿主原子更新、真实 reload 与完整还原亦通过。脚本最后明确报告当次 owned 容器／网络／卷及设施临时文件零残留，不能只以摘要或 cleanup 成功替代这些实际执行断言。

原 [job 120735545333 / attempt 1](https://github.com/pampa0629/addp/actions/runs/40048425338/job/120735545333) 仍记失败：51 项准备中 `test_business_postgres_compose_mount_is_readonly_and_requires_preparation` 的 `docker compose ... config --format json postgres` 在原 15 秒期限超时（共 50 项通过、1 项错误），尚未执行产品采集阶段。原准备测试冻结源码与同 SHA Git 源码逐字节一致，复验门禁脚本亦与该 SHA 一致；本次成功不回写首轮为通过，也不据此断言暂态环境的唯一根因已确认。未修改断言、期限或权限来取得通过。

证据位于 `/tmp/addp-ci-20261024-020017/`：`job-120741235374.log` SHA-256 为 `91633f44e24988756d38ea144d46d6f7f07c9144114aee45add85ce38622f9c3`，`monitor-metrics-gate-frozen.sh` 为 `95cd991422623e4cad7cb193bd4337181af62085b295efb0ab7abe8be1ba2626`，已用哈希及 `git show` 精确比对。`Monitor-PG-5d91-retry-actual-T2-pass.json` 为回传审计，`Monitor-PG-5d91-domain-audit.json` 保存本次领域核对；首失败原始日志与审计保留在 `/tmp/addp-ci-20261024-013016/`。本次只证明该固定版本的 PG source 实源 T2，不代表当前共享 HEAD 全批、七天自然留存、容量或新的 Online T4；既有独立版本专业 T4 按其原证据保留。未操作个人服务／数据库、未设置定时，待确认的容器配额语义及采集范围未实施、未计为已授权。

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


### Safari 服务实例规则弹窗实景复核（2026-10-23）

05:49–05:53（Asia/Shanghai），在用户确认可操作 Safari 后，用原生 CUA 操作已登录的平台系统管理员及真实 Console `/monitor/platform-alerts?tab=process-rules`。检查器读到页面曾 `document.hidden=true`，随后为 `false`；活动弹窗 `display=block`、`visibility=visible`、遮罩和内容透明度均为 1，动画列表为空，标题坐标的命中检测确为弹窗标题，父层命中 iframe。检查器开启时 iframe 高度约 227 CSS px、弹窗顶部约 34 px，标题仍在可见区域；没有取得样式隐藏、动画卡住或其他元素遮挡的证据。期间工具出现用户操作中止及 ScreenCaptureKit 捕获错误，辅助功能树与截图一度不同步。

窗口恢复前台并关闭检查器后，真实截图清楚显示弹窗与遮罩；随后实际关闭、确认列表恢复、重新打开，以及展开真实实例候选下拉层均正常，截图包含 Monitor／Meta／Transfer／Manager／System 当前候选。保持表单未保存，未创建、启用规则或发送通知。本轮仅补充证据，没有修改产品代码、样式、浏览器设置或重启服务。此前现象与后台窗口／重绘不同步相关的判断得到支持，但未建立 Safari 底层异常的稳定复现，不称为代码根治或全部告警 T4 通过。既有 Chromium 5/5、WebKit 弹窗专项 1/1 是隔离 T3 证据，本次是个人开发环境只读 UI 复核，分别陈述。CI／Hosted 仍等专门会话回传，未查询或调度、未设置定时。


### Python Trace 与依赖修复的远端 CI 分项回执（2026-10-23）

本次手动只读核验本专项原始 Job 日志、checkout SHA 和路径选择，没有调度 Hosted、定时或容量。证据清单及原始日志 SHA-256 见 `/tmp/addp-p19-async-trace-investigation-20261023/ci-audit.json`。

- `c05c7048080af98d968a0e6e31437794bdb430b6` 的 [Trace T2 Job 119877525491](https://github.com/pampa0629/addp/actions/runs/39778225752/job/119877525491) 实际执行 `make test-monitor-traces` 并通过；`TestOwnedTracePipeline` 328.64 秒，真实 120 秒尾采样保留失败、慢链及 12/200 普通链，最后明确当次容器、网络、卷和临时文件零残留。日志 SHA-256 `b0b6d3df6a02bc1c90af0f41631ea043fad17b1bf13ae872ceb1fd355e81beb9`。该 Release/T2 workflow 总结失败来自其他专项，本记录只证明本 Job。
- 同一 `c05c70480` 在 Platform 的 [Common Python](https://github.com/pampa0629/addp/actions/runs/39778225763/job/119822518588)、[Agent offline](https://github.com/pampa0629/addp/actions/runs/39778225763/job/119822519104)、[Copilot Backend](https://github.com/pampa0629/addp/actions/runs/39778225763/job/119822519135) 原始日志均为 `selected=true` 并实际执行：分别 411 passed／48 子测试／1 项可选缺依赖跳过、七场景及子门禁通过、184 passed。可选跳过不计通过；不将该 workflow 的无关失败外推为本专项失败，也不将其总结写为全绿。
- 依赖准备修复 `673cf8eef7cb776de26a0b04c8dbdc86384becbf` 的 [Platform consistency](https://github.com/pampa0629/addp/actions/runs/39803050868/job/119908856837) 实际完整执行 `make test-platform` 并通过；含 124 项配置／部署、60 项设施生命周期、统一 Backend 准备／指纹及 dev lifecycle／原子构建，末尾 Swagger 覆盖通过。同 run 的 Common Python、Agent、Copilot 三 Job 虽状态 success，原始日志是路径 `selected=false`，属于跳过，不能作为这三项重测通过证据；三项实际执行证据使用上面的 c05 原输入。

由此关闭前两批本专项自动 CI 待核项；不是对后续共享 main、异步第三批、真实身份 T4、真实 72 小时或生产容量的证明。用户重启后的本机只读结果仍沿前节，不重复重启个人服务。


### 执行短阶段与 Trace Link（二期第三批，本地）

2026-10-23，按用户确认的边界，先修订术语、观测与执行记录规范，再实现 Common 持久引用和独立短阶段 Trace／Link，首接 Orchestrator → Develop bounded query。入队、领取、派发、终态分别结束，不把 SQL、等待、心跳或整个长任务包进 Span；耗时仍以 execution 为准。不新增表或 Tenant Trace 字段，纯值引用保存在 execution_config／metadata 的保留 namespace；终态业务 metadata 继续原替换／清空语义，只保留权威私有观测字段，不累积旧 outputs／result。阶段成功只在原 owner 外层事务提交后记录；失败提交为 rejected，畸形私有引用停用当次追踪并保留原事实。

Monitor 仅接受 addp.http／addp.execution 两种明确契约；Link 目标和实际参与实例一起沿当前 Platform User 逐项重核，数字实例 ID 来自 System，目标撤权拒绝整份结果；不递归拉取，不生成 HTTP 调用边。页面展示阶段、attempt、outcome 与因果链接，按目标起始时刻另发受限查询，撤权清除原详情。采样唯一策略升级 v2：failed／timeout 阶段进入既有失败预算，cancelled／rejected 按普通阶段；HTTP 慢请求口径和 5%／120 秒／10000 待决／100 Span 每秒／70:20:10／72 小时／20 GiB 初值均保持。OTel SDK 环境资源污染由唯一导出投影剥离，不修改进程全局环境。

冻结源码基线 `dfcf4ace806a87c912b7787233d40fcde5f29a14`，仅叠加本批明确路径；输入 SHA-256、日志和最终分项清单位于 `/tmp/addp-p19-execution-trace-20261023/plan.json`、`verification.json`。测试在隔离源码快照执行，PostgreSQL 因标准入口校验容器归属，改用原工作区标准入口，并验证源码指纹、共享连接与并发门禁；未绕过归属检查。共享 main 后续其他会话改动不算该冻结输入的验收。

- T0/T1：`GOMAXPROCS=2 GOFLAGS=-p=2 make test-go` 最终退出 0，22 个 Go 模块含 tidy-diff／CGO 禁用检查通过。Common／Monitor 单 owner 门禁通过；最终 Orchestrator／Develop 分别经 `make test-module MODULE=<owner> GO_ONLY=1` 通过。后续 owner 私有修正另经各自 T1 及 PostgreSQL 验证，未重跑无输入变化的全模块。Common 验证 SDK 所属、独立 root／Context、不可变入口、有界 Link／属性、敏感资源剥离；Develop 真实授权读投影回归确认 Tenant 不泄露 execution_config 私有引用。
- T2 owner：先执行 `bash scripts/infra/status.sh`，实际共享 PostgreSQL 为 25432、唯一 `addp_test`；`make test-orchestrator-postgres` 与 `make test-develop-postgres` 均通过。最终 Develop metadata 修正后 T1、PostgreSQL 再次通过（后者 13.70 秒）；覆盖入队／领取／派发／终态、租约及事务拒绝、重读引用、伪造剥离、故障恢复、畸形引用和不影响其他 task type。旧实现配新用例稳定失败于“保留旧业务 metadata”，修正后覆盖 omitted／replace／nil／typed nil／空对象／非法类型。中间测试错误要求数据库读回 nil，已按既有 JSONMap 空值归一化为空对象修正，未改变共享存储语义或放松旧结果清除断言。
- 真实 Trace T2：`GOMAXPROCS=2 GOFLAGS=-p=2 MONITOR_TRACES_PYTHON=/Users/pampa/code/addp/agent/backend/venv/bin/python make test-monitor-traces` 退出 0，Pipeline 328.53 秒；实际 120 秒尾采样保留失败／慢链及 10/200 普通链。首阶段三个生产进程分别导出 204／404／404 Span，队列、丢弃、失败均零；另一个受控进程重读持久检查点，真实 Alloy／Tempo 验证四个独立阶段 root、两条持久 Link、目标单独撤权拒绝、失败短阶段优先及精确终态 Trace／Span 的 Loki 日志。独立采样的 Link 目标允许查无数据，不能宣称完整任务必留存；第四进程未单独汇总上述导出计数。原 Go → Python → Go 五 Span／两边、mTLS、只读代理、Collector 故障恢复继续通过，退出明确当次容器／网络／卷／设施临时文件零残留。父进程 TestOwnedTraceProcess 是受控 child helper，按设计跳过，不计业务通过。
- 页面 T3：`make test-monitor-frontend` 最终退出 0，98 项确定性测试、295 项 Chromium E2E 与 build 通过；`make test-console-frontend` 189 项确定性测试（29 文件）、218 项 E2E 与 build 通过。此前 Monitor 两次在执行场景前因隔离副本 node_modules 内嵌符号链接导致双 Playwright 实例失败；仅修副本依赖解析后全量通过，没有放宽测试或改产品依赖。T3 为受控 HTTP 夹具，不计真实身份验收。
- 配置／登记：`make test-dev-lifecycle` 退出 0，124 项配置及 60 项设施生命周期检查、shell／编译检查通过，覆盖唯一 v2 渲染。现有 Make owner 自动发现、T2 注册和路径选择已核；Trace 门禁输入补 Common execution 与新实链 fixture，Develop PostgreSQL 既有 regex 补新回归。三个 go.mod 仅将已有版本转直接依赖，无升级；无新门禁、数据库、构建方式或 workflow。`bash scripts/swagger/gen-swagger.sh monitor`、`bash scripts/swagger/check-route-coverage.sh monitor` 通过，90 个公开方法一致，产物同步。
- 平台聚合：`make test-platform` 非零。此前本专项需要的配置、依赖／CI 注册、变更选择与安全等子检查通过（T2 注册 22 hosted-service／12 Compose／9 hosted-only／2 owner-managed）；在 Online Raster 既有工具链测试要求 Node 24、当前解释器为 Node 25 时失败，215 项中此项失败。不扩展修复其他专项，不计整套平台通过；末尾授权／Swagger未执行，另以标准 `make test-authorization` 补验最终退出 0，授权与全模块 Swagger 覆盖通过。首次补验隔离快照无 Python venv，Go 授权及全部 Go Swagger 已通过但 Agent/Copilot 覆盖未执行，未计全套通过；仅在副本准备既有 Python 环境后复验，不改用户 venv。

本批只有上述本地 T0/T1/T2/T3 证据，未运行 Hosted、正式身份 T4、容量重跑或真实 72 小时保留／删除，未设置定时或重启个人服务。原 HTTP 两档容量结果不外推新增阶段。其他 Runtime、continuous、SQL Span、Python 异步、长期合并及生产 HA 仍未覆盖，不能将本批记为全链路追踪全部完工；新提交的远端自动 CI 状态另记。


### 服务实例 CPU/RSS 真实采样告警链路 T2（2026-10-23）

沿原标准入口新增 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=process-alerts`，只使用门禁自建非 root 容器的 0.25 CPU／128 MiB 原硬上限。三个真实 Common SDK 进程经原 mTLS／唯一 HTTP SD 进入 Prometheus；其中一个 Worker 在加压前固定 CPU 触发／恢复值 0.025／0.008 核，RSS 按真实空闲基线加 12／8 MiB 固定阈值，随后有界计算并持有 24 MiB 匿名内存，释放后由自然新样本判断恢复。测试控制仅存在于测试二进制，单次最多 180 秒，未改生产采集器或个人服务。

冻结基线 `9f6e77140d9576dc8ef2419082f765175232f312` 加本批七项输入，在隔离源码副本执行：

- `make test-module MODULE=monitor GO_ONLY=1`：退出 0，Monitor Go T1 通过。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=process-alerts`：退出 0；48 项配置／分组回归、八项固定窗口测试及真实进程告警专项通过，无专项 Skip。专项实际耗时 245.08 秒：CPU 打开证据为 0.0269–0.0511 核，释放后的恢复证据为 0.00378–0.00400 核；RSS 从约 30.8 MiB 基线增至约 57.6 MiB，恢复证据约 30.4–30.6 MiB。两项均使用推进中的连续样本，CPU 保留完整一分钟，不改写计数器或历史；两个原事件分别恢复，没有新增结束状态。四个 opened／resolved 事件共收到八次回环 TLS 请求，每个事件首次 503、随后成功，投递 ID／事件 ID／正文和持久证据均一致；旧日志订阅未收到该类别，对等 Backend 与 Worker 的三项当前值继续有效。退出明确报告 owned 容器／网络／卷及设施临时目录零残留，并独立按当次项目标签读回核实。
- `make test-release-runner`：退出 0，62 项发布／登记回归及正式登记检查通过；`make test-workflow-security`：退出 0。新增场景已加入原 Release Metrics 矩阵，仍保持每项 20 分钟及所有分组真实成功才通过的汇总要求；默认全量入口包含原分组及新专项，无第二套执行路线。

原始日志及输入摘要保存在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-alert-real-r7sgulbj/` 的 `process-alerts-t2.log`、`monitor-go.log`、`release-registration.log`、`workflow-security.log` 与 `inputs.json`；七项工作区输入与冻结副本哈希一致。此结果为真实 SDK／Prometheus／PostgreSQL／签名接收端下的 T2，System 当前用户与身份投影仍是夹具，不替代正式密码／MFA、真实 Console、Hosted T4、生产容量或 T5。本轮未运行其他 Metrics 分组，复用原有证据边界；未查询／调度 CI 或 Hosted，等待专门会话回传，未重启个人服务、未设置定时任务。


### CPU/RSS 告警专项的就绪分段与失败诊断（2026-10-23）

专门 CI 会话回传 [39879255330 / 120203117355](https://github.com/pampa0629/addp/actions/runs/39879255330/job/120203117355)（`691441e81c5069f2d2d10903b0623d49135fdf17`）和此前同症状失败：已打开两个原事件并持久化签名通知，RSS 恢复，CPU 仍为 risk，场景在 270 秒退出。旧日志只打印 LastValue 指针，没有 CPU 数值或实际释放时刻，不能据此断言 CPU 未释放或生产恢复逻辑错误。代码确认冷来源的完整 CPU 基线与规则创建后预热串行消耗同一场景预算；本机新增诊断还确认抓取的毫秒抖动可使相邻样本只有 14.997 秒，严格持续窗口需要第三个样本。

本批只调整测试设施：准备阶段以原 65 秒等待确认三个精确来源的完整 CPU 历史；Go 场景区分规则创建后完整 CPU 窗口、空闲正常、真实 RSS 释放和同一事件恢复。保留来源八分钟寿命、270 秒总预算、各阶段 100 秒、可执行测试 300 秒、0.25 CPU／128 MiB、固定阈值、全部生命周期与签名投递断言。未修改生产采集／查询／告警判断、权限或个人服务。失败日志增加规则数值、评估／采样时刻及有界窗口查询；来源准备失败的诊断只保留固定信号与数值，不输出端点、任意标签或凭据。

冻结基线 `0c3946573e375da4cbd10bd78793e2eeaf17ed34` 加本批输入，证据目录为 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-alert-repair-x2oq49qd/`：

- `make test-module MODULE=monitor GO_ONLY=1`：最终输入退出 0，见 `monitor-go-t1-final.log`。新增确定性回归验证不足 15 秒不能舍入恢复、第三个推进样本及规则创建后完整 CPU 分钟边界。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=process-alerts` 的最终尝试：前置 51 项配置回归通过，包括真实零、缺失／重复／替换来源拒绝及诊断去除私有标签；整轮未通过。`TestIntegrationMetricsNetworkWindow` 的 promtool 已打印 SUCCESS，但容器命令在原 30 秒期限被终止，后续固定窗口进程亦未正常结束；尚未进入 CPU/RSS 专项。见 `process-alerts-t2-final.log`，不能把前置单测或 SUCCESS 文本计为 T2 通过。
- 此前两次编排尝试分别在空闲预热 100.40／100.43 秒失败，均未加压；前者证明缺样期间起点重置，后者记录低于固定恢复阈值的有效 CPU 值但严格连续窗口仍未就绪。第三次在 65 秒 CPU 来源准备阶段失败，旧版未保留该阶段的原始数值，原因未确认；新诊断的安全序列化仅有单测证据，不能反推这次准备失败已修复。见 `process-alerts-overlap-failed.log`、`process-alerts-cadence-failed.log`、`process-alerts-source-readiness-failed.log`。
- 四次项目的容器／网络／卷已逐项按精确 Compose 项目标签核对为零，见 `failed-project-cleanup.json`。最终尝试的自动清理曾失败，后续确认无容器、网络无端点且归属标签匹配后，仅删除该项目遗留网络／卷；人工清零不改变原门禁失败结果。

该批次当时的结论为测试编排修正与诊断代码已落地、Go／配置回归通过；该批输入的真实 CPU/RSS 触发与恢复 T2 **未通过**，Linux CI 与正式身份 T4 也未确认。源码输入见 `input-sha256.json`；未自行查询、触发 CI／Hosted 或设置定时，不将上述历史 T2 通过外推到该批。后续固定版本的 Linux 通过证据见下一节，不改变本段各次失败及清理记录。

### 服务实例 CPU/RSS 告警原生 Linux 固定版本复核（2026-10-23）

专门 CI 会话回传 [Release 39908130012 / job 120318541196](https://github.com/pampa0629/addp/actions/runs/39908130012/job/120318541196)，push、attempt 1，实际检出 `dfa4ad16105d0421af13e55f3e4099acdd6751a2`。已独立复核原始日志与该提交的测试源码：`Verify isolated metrics lifecycle` 实际运行 `make test-monitor-metrics`、`MONITOR_METRICS_TEST_ARGS=--case=process-alerts`，`TestIntegrationMetricsProcessAlertLifecycle` PASS，213.73 秒；退出报告 owned 容器／网络／卷及设施临时文件零残留。此处接收并复核既有运行，没有再次查询、调度 CI／Hosted 或设置定时。

规则在加压前固定 CPU 触发／恢复 0.025／0.008 核，RSS 为真实空闲基线加 12／8 MiB，连续窗口各 15 秒；保留完整一分钟 CPU 历史、原资源上限与生命周期断言。真实计算／持有匿名内存后打开两个独立事件，CPU 触发证据 0.03378～0.05800 核，RSS 62,111,744 B；释放后确认真实 RSS 回落，原两个事件恢复，CPU 恢复证据 0.00267 核，RSS 34,353,152～36,708,352 B。触发／恢复各证据窗口含两个推进样本，恢复样本均在释放之后。四个不可变 opened／resolved 事件、四项成功投递、八次签名 TLS 收件断言通过，每事件首次 503 后重试的投递 ID、事件 ID 及正文一致；旧日志订阅零误投，另外两个进程继续采集，测试清理断言当次规则零残留。

原始日志与独立审计位于 `/tmp/addp-ci-20261023-120029/`：`job-120318541196.log` SHA-256 为 `75c5b5f66faa6585d7cbf4a16024aebec2c5024740dfe7d3a05e58134cb56571`，`dfa4-process-alert-lifecycle-frozen.go.txt` 为 `15605f1114d79fba801d09df548c558f4df111e6c1eba235379493732ffb010e`，后者与 `git show dfa4ad16105d0421af13e55f3e4099acdd6751a2:monitor/backend/internal/service/process_alert_lifecycle_prometheus_test.go` 的哈希一致。另有 `Monitor-dfa4-real-process-independent-audit.json` 与 `monitor-dfa4-artifacts.json`；该 run 无下载产物，证据为原始日志及固定源码断言。该结果仅证明该 SHA 的原生 Linux 实源 T2，身份仍为 T2 夹具，不替代正式密码／MFA 页面 T4、长时段、生产容量或当前 HEAD 全批验证；`6dba`／`691`／`9e5`／`d01`／`f6` 等原失败保留，不以此次成功推定其历史根因均已修复。主机五项列表摘要、热力面板及后续前端修复另按各自证据判断。

### 正式身份的本机执行 Trace／日志闭环（2026-10-23）

用户确认仅在本机一次性隔离部署，先修规范与标准入口，再验证正式密码／MFA、Orchestrator → Develop bounded query、Platform Trace 查询与精确日志关联。新增人工 `make test-monitor-traces-local`；静态／准入／守卫／业务契约标准入口 `make test-monitor-traces-local-runner` 聚合到既有 Infra runtime-log lifecycle / Platform，CI 登记明确拒绝公共 workflow 调用真实入口。所有构建与运行容器、浏览器共享 8 CPU／12 GiB 上限，独占卷达到 4 GiB 即停止，最长 60 分钟后进入不可跳过的清理；不运行 Hosted、容量、定时或个人服务重启。

Common 为独立短阶段增加 `EndExecution`，与原 End 共用一次终结内核，固定 `execution.checkpoint`，仅规范 execution_id／phase／attempt／outcome，trace/span 取真实阶段 Context；UUID 不进 Span、索引或 Tenant 投影。成功／拒绝沿原 owner 最外层事务结果，不造第二条状态路径；无效 UUID、非法 outcome、关闭追踪不伪造关联。System 本地身份夹具经独占 PostgreSQL `cluster_name` 与 `addp_online` 核验后，调用正式 Bootstrap／IAM 服务创建当次三员、Tenant 准备员及精确十一权限业务 User；仅输出私有登录凭据，不预签会话。准备员只做 Engine 准备，User 实际执行，Platform 系统管理员读取 Trace／日志，安全管理员为无权限负例。

实施前识别的验证范围及当前准备结果：

- 冻结 Common／Orchestrator／Develop 13 个 Go 文件的标准 `make test-module MODULE=<owner> GO_ONLY=1` 全通过，见 `/tmp/addp-p19-checkpoint-20261023-5bqj4s22/manifest.json`、`receipt.json`。System 身份夹具独立冻结后完整 System Go 通过，见 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-local-identity-o4eet7fw/plan.json` 与 `system-go.log`。首次新用例的临时目录未显式 0700 被正确拒绝，已修测试准备且最终通过，未降低产品权限。
- 先执行 `bash scripts/infra/status.sh` 核实共享 PostgreSQL 25432，以标准 `make test-orchestrator-postgres`、`make test-develop-postgres` 在 addp_test 通过（15.59／27.50 秒），源码／连接／并发守卫无冲突。日志位于 `/tmp/addp-p19-formal-local-20261023/`；本轮正式身份部署使用另一独占容器，不能把这两次 Repository T2 记为正式身份验收。
- `make test-monitor-traces` 退出 0：真实 Pipeline 327.65 秒，120 秒尾采样保留失败／慢链和 9/200 普通链，首三进程分别导出 204／404／404 Span，队列／丢弃／失败为零；独立短阶段、持久 Link 与目标单独撤权、Go → Python → Go、精确 Loki trace/span、mTLS 与故障恢复通过；退出明确当次容器／网络／卷及设施临时文件零残留。child helper 按设计跳过，不计业务通过。这仍是受控 T2，正式 owner 日志由下面本地身份入口另验。
- `make test-monitor-traces-local-runner` 最终通过：31 项 Python 契约（含源输入冻结、Docker Context 优先级／远端拒绝、有界资源／真实卷停止线／采样超时／进程组回收／清理、精确正式 API DTO、最小日志、失败阶段采样、独立 Cookie Jar 与正式退出）、两份 JS 语法、完整 Online CI 登记检查及 System ExecutionTrace 定向 Go。最初源码快照夹入另会话未提交 Catalog 引用造成不完整编译，已仅在快照恢复本专项集成版本；未修改其工作区或把该次记为通过。
- `make test-infra-runtime-log-lifecycle` 通过（上述本地 runner 加既有 60 项设施生命周期）。Console 新增四项本地浏览器输入／准入／TOTP／禁止 Token 注入契约通过；`make test-console-frontend` 聚合首轮因隔离副本缺 System Vite 依赖失败，补副本依赖后 192/193 确定性通过，唯一失败是既有 devPorts 的 HMR 缓存准备断言，未进入其后 E2E/build。不修复或外推无关专项，也不计整套 Console 通过；本专项真实浏览器由独立标准本地入口验证。
- 共享扩散 `make test-go` 首次在 System tidy-diff 检出新增测试把已有 go-sqlite3 直接导入仍声明 indirect；只修声明、不升级版本，最终扩散结果另记。输入、标准日志及分项报告统一保留 `/tmp/addp-p19-formal-local-20261023/`；一次误在共享工作区启动的重复 Go 门禁已仅终止自己的进程组，不作为冻结输入证据。

此提交先落入口与准备证据；正式身份本机隔离验收尚未执行，不记通过。成功、失败、停止线和清理结果将追加本节；本地技术证据不替代专用 Runner T4、实例单独撤权、SQL Span、其他 Runtime、真实 72 小时留存或生产容量。


首次本地窗口在构建器准备阶段等待 Docker Desktop 个人凭据助手，尚未下载基础镜像、创建业务部署或发送业务请求。主动中断自有 gate 后，进程组、Builder 容器／卷和私有目录经精确过滤复查均为空；最终报告写入还暴露 Path.open 不支持 opener 的接口错误，原自动报告未生成，不伪造为通过。已修为内置 open 的 O_EXCL／0600 路径，并补真实文件权限与拒绝覆写测试；Docker 客户端改为当次 0700 目录、空 auths、无凭据助手，只读公共工具插件并固定已核对 Unix endpoint，不借个人凭据。标准 local runner 复验结果另记。后续操作继续扣减首次窗口已耗时间，未放宽原 60 分钟、8 CPU／12 GiB 或 4 GiB 停止线。首次失败及清理复查见 local-first-interrupted-receipt.json 与 local-acceptance.log。


### 同设备双向吞吐同图（2026-10-23）

主机资源页对同一磁盘的读／写、同一网卡的收／发使用一次现有趋势查询，同图显示两条独立折线；按全部有效点共同选择图表单位，并在图旁明确单位及末次评估值。所选方向仍决定采样明细，响应数组乱序不改变方向；真实零值保留，陈旧／缺样各自断开，缺序列、错误设备或评估网格错位拒绝整幅趋势。复用共享 ChartRenderer、格式化和请求隔离，没有新增采集、接口、权限、插值或设备汇总；其他标量、文件系统、磁盘 IO 耗时及 CPU／内存比较保持原单指标路径。规范见平台运行观测 6.3。

冻结基线 `c71bd5cfaddd4607c2d0907691f2d2a06a57d432` 加本批六项前端输入执行 `make test-monitor-frontend`：退出 0，100 项确定性测试、299 项浏览器测试与构建通过。随后仅补图旁明确的容量单位及独立测试截图路径；最终六项输入再次冻结，执行 `npm test`（100／100，无 Skip）、标准 `npm run test:e2e -- e2e/node-resources.spec.js`（65／65）及 `npm run build`，全部退出 0。最终主机页覆盖中英文、620px、双向单位／零值、响应乱序、方向明细、重载恢复、错位拒绝／恢复与独立缺样；其余未受最后单位文本影响的页面复用完整门禁证据，不宣称最终输入又完整重跑299项。现有 Monitor 前端路径及标准门禁自动发现，无新增依赖或 CI 编排。

末版专项首次为64／65，一次点击写入方向后 URL 未变化，截图显示选择弹层仍打开；带 trace 的四组合12次检查、加入输入事件记录的单场景10次诊断及最终标准65项均未复现，不能据此确认该次失败根因或宣称修复了间歇点击／Safari 故障。临时诊断代码已删除，不增加重试、超时或移除 URL／明细断言；新截图使用每次浏览器测试的独立 outputPath。保留首次失败证据 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-monitor-playwright-results-lf2gXA/`。

日志、两版冻结输入及诊断记录位于 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-paired-throughput-ge5httgy/`：`frontend.log`、`inputs.json`、`inputs-final.json` 与 `snapshot-final/monitor/` 下的 `unit-final.log`、`node-verified.log`、`build-final.log`、`node-final.log`、`paired-trace.log`、`selection-probe.log`。最终六项输入与工作区哈希一致。仅为隔离夹具 T1／T3，未运行真实 Safari、Hosted T4、CI 或后台重启，等待专门会话回传，未设置定时任务。当前仍为折线，不将本批计为原图的分钟柱形、内存双Y轴、资源热力或完整监控交付。


构建器独立客户端修正后已实际启动；第二次仍在业务部署前主动停止，用于修正启动清单中一次性 init 的前台成功语义及显式 MinIO 镜像绑定，未发业务请求。自动报告 `local-report-final.json` 记录失败／中断及零残留，实际最高硬配额 6 CPU／10 GiB、未发生 OOM。进一步将全部私有可写目录计入原 4 GiB 停止线，只读复用公共 Go 依赖缓存，及时删除当次已完成 Go 对象缓存，未借用个人凭据或写共享缓存；标准 runner 的初始化序列、目录预算及私有文件回归共 35 项通过。全部重试继续使用首次窗口截止时间，不重新取得一小时。


第三次在 product_compile 阶段由守卫自动报 `owned_volume_stop_line_reached`，业务部署／身份／查询均未执行；报告 `local-report-window-final.json` 明确失败且 zero_owned_resources=true。最近成功样本为 4,235,309,056 字节（低于 4 GiB），独立只读 du 随后观察到私有目录 4,266,260 KiB（约 4.07 GiB）；旧守卫在触线断言前未追加触线样本，不能用最近成功样本冒充峰值，已将样本落盘移到断言之前并增回归。只读主仓库 `.git` 约 1.2 GiB，因此快照唯一切换为同提交的 file:// 浅克隆（depth=1、no-local），不复制无关历史，也不放宽资源／时间／验收断言；标准 runner 仍通过。下一次继续沿首次绝对截止时间与同样停止线，不能计为容量或正式身份已通过。


### 内存容量与使用率双 Y 轴（2026-10-23）

主机资源页选择任一内存指标时，沿一次既有固定查询取得总量、可用量和使用率。共享 ChartRenderer 的显式 `valueAxes` 展示属性统一绘图：左轴为按有效容量样本选择的 IEC 单位，右轴固定 0–100%；只换算字节值，百分比不缩放。各序列按指标身份与同一评估网格对齐，缺样各自断线，错位或缺序列拒绝整图；采样明细仍由 URL 所选指标决定。相邻时段比较只读取使用率，容量缺样不伪造比例或阻止本已完整的使用率比较。不新增指标、接口、权限、采集或 Workbench 持久化配置，不推定内存泄漏。

本地冻结输入根为 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-memory-dual-axis-bwf4sj9m`，基线 `34dc7b291515c59f30ce93d03578f82dd2b26f45`；`inputs-final.json` 记录本批文件摘要，`snapshot-final` 只叠加本批改动，未纳入共享工作区其他 owner 的 WIP。验证结果：

- `make test-common-frontend`：261 项通过，无失败／跳过；双轴映射、范围与配置拒绝、单位、数值、null、主题及旧单轴语义均有确定性验证（`common.log`）。
- `make test-monitor-frontend` 首轮确定性测试 100/101；旧单指标断言同步后，最终 `npm test` 为 101/101，通过（`unit-final.log`）。首轮浏览器 299/301，另两项失败均为告警跳转测试仍预期单一使用率查询；同步为三指标组合，并保留完整的错误时窗拒绝夹具后，沿标准 `npm run test:e2e -- host-alert-links.spec.js resource-comparison.spec.js` 复验 29/29（含新增容量缺样不影响使用率比较场景，`affected-browser-final.log`）。其余输入未变的 273 项沿全量通过证据复用，合计 302 个唯一场景具有通过结果；未再重复整轮，不将最初失败的聚合命令记作通过。初始失败与截图保留在 `monitor-fixed.log` 及 `addp-monitor-playwright-results-VV4xu8`。
- 新增中英文 620 像素窄屏实图已逐一核对左右轴、图例与刻度；浏览器同时验证乱序响应、GiB→TiB 容量单位变化、百分比保持、可用量独立缺样、刷新恢复、URL 重载、采样明细及撤权清除。截图：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-memory-axes-9q4js8/zh-cn.png`、`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-memory-axes-CR6PqE/en.png`。
- Monitor 最终 `npm run build` 通过（`build-final.log`）；Console 标准浏览器入口 `npm run test:e2e -- process-resource-navigation.spec.js --grep 'exact managed host'` 1/1，通过，核对同一 iframe、实际主机趋势及返回筛选（`console-navigation.log`）。
- `make test-frontend-ci-registration` 20 项及 21 个前端登记检查通过（`registration.log`）；现有共享依赖扩散与 Monitor 前端登记已覆盖本批，无需新增 workflow。Hosted 既有脚本的内存比较查询预期同步复用同一指标组合；`make test-node-metrics-online-runner` 的 138 项确定性测试与 Metrics Go 身份夹具通过（`online-runner.log`），不是实际 Hosted 结果。

以上仅为本地 T1/T3 和验收脚本的确定性验证，不计真实身份／数据源 T4 或生产 T5。本轮未查询、触发或重跑云端 CI／Hosted，未设置定时任务，未重启个人服务。CI 和 Hosted 结果由专门会话回传。


本轮最终实现与失败边界（2026-10-23，正式身份本地入口）：Common 检查点及 owner 接线提交 `3f59dc5c56d44f11bb8c8fa7b8518ec36028f965`；入口后续修正至 `d4b31264d`。共享扩散最终 `make test-go` 退出 0，22 个模块通过；最终 `make test-infra-runtime-log-lifecycle` 退出 0，包含 40 项本地入口契约、JS 语法／CI 登记／System ExecutionTrace Go，以及既有 60 项生命周期检查。40 项包含真实回环 Cookie Jar／退出、守卫失败前拒绝新业务请求、客户端与 daemon 分别校验无凭据代理、完整可写预算／触线样本、采样失败步骤和总硬配额。标准入口准备验证通过不等于正式身份实际执行通过。完整 Console 结果仍为上述 192/193，E2E/build 未执行，不把专项四项通过记为全量通过。

实际操作始终共用首次窗口的绝对截止 `2026-10-23 09:10:30 Asia/Shanghai`，没有逐次取得新一小时。浅克隆修正后六个后端与 runtime-log 均已从 `34dc7b291515c59f30ce93d03578f82dd2b26f45` 编译；安全哈希见 `shallow-source-receipt.json`，该次最高已记录 3,377,561,600 字节、6 CPU／10 GiB，随后 MinIO 公共基础镜像匿名令牌请求超时。诊断确认宿主构建客户端与 Docker daemon 的代理不同：客户端仅可显式使用已核验、不含凭据的回环代理；daemon 仅可使用已核验的本地代理。不得继承任意代理／凭据，不向业务容器注入代理，宿主 API 访问固定回环绕过公共代理。修正后在当次 BuildKit 内实际观察到 MinIO 的 Go 构建，公共镜像下载及源码步骤已推进，但尚未证明全部公共构建成功。

最后一次实测 `addp-trace-local-898d068c3ab94a8badc3651328e69299`（源码 `83ef3e512`）仍在 product_minio 阶段自动停止，报告 `command_deadline_exceeded`；该命令未到其 1200 秒上限，整窗也未到 3600 秒，结合入口的共享失败传播可确定来自四秒资源采样命令超时。旧版未记录采样子步骤，不能断言具体是 Docker 列表、卷扫描或绑定目录扫描，也不能宣称已修复其根因。最后成功样本为 2,873,073,664 字节，未触及 4 GiB；未知失败样本的磁盘峰值不记为已知。现为采样器预留 0.25 CPU，运行服务＋浏览器／身份辅助＋采样器合计 7.95 CPU，内存仍在 12 GiB 内；维持四秒采样预算及原停止线，新增失败步骤／耗时记录和业务请求前后守卫。新配额及诊断只经确定性测试，尚未经过新的真实窗口，不认定采样稳定性已通过。

八次实际启动中，首轮报告写入异常使用明确标注的人工复查回执，其余七份自动报告全部为 failed 且清理通过；均未创建业务部署、未准备正式身份、未发送成功／失败查询，也未进行真实 Console／Trace／日志闭环。`local-final-receipt.json` 汇总每次报告哈希、停止阶段、资源记录与独立清理复查；当次容器、网络、卷、构建器和私有目录残留均为零。全部原始安全报告、准备门禁日志及输入摘要位于 `/tmp/addp-p19-formal-local-20261023/`。因此整体仍为部分实现，正式身份验收未执行；不是业务链路失败或通过的证明。后续优先在同样 8 CPU／12 GiB／4 GiB 停止线下确认一个新的最长 60 分钟本地窗口，先核对采样稳定性，再执行正式身份闭环；未经确认不续开窗口，不运行 Hosted、容量或定时，不接管个人服务。

代码提交 `d4b31264dc7308ca7b1b7479e9b697a97580b16b` 推送后单次只读核对：[Platform CI 39881617580](https://github.com/pampa0629/addp/actions/runs/39881617580) 与 [Release/T2 39881617528](https://github.com/pampa0629/addp/actions/runs/39881617528) 均为 queued，不计为通过；安全回执 `ci-receipt.json`。没有手工调度任何 workflow 或设置后续跟踪。


### 网卡收发速率柱状展示与局部缩放（2026-10-23）

主机网卡趋势复用共享 ChartRenderer，以并排柱状显示同一网卡的接收／发送速率。保持现有完整一分钟平均字节速率、原时段、原步长和评估点，不换算累计量、分钟分桶流量或带宽利用率；图旁说明口径与末次值，轴显示 IEC 速率单位。真实零值保留，陈旧／缺样独立为 null；错误设备、网格错位、预算错误及撤权继续清除旧趋势。磁盘保持折线，内存保持双轴。共享展示属性 maxRows 显式允许该有界视图最多 1,000 行，其他柱图默认仍为 500，Workbench 持久化契约不变。密集常显数值标签关闭，tooltip 与采样明细保留数值及单位。

近千组柱在窄屏全窗会成为密集概览，因此同一共享 renderer 提供本地缩放滑块。缩放保留全部 rows 与 series 数据，不触发请求或重算指标；临时 start/end 通过共享事件交宿主保存，刷新同一图表恢复，切换主机／网卡／时段／身份或查询失败时清空。组件通过既有主题变量绘制滑块，主题重绘保留当前相对视口，不新增持久化、接口、权限、依赖版本或后端开关。

本地冻结输入：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-network-bars-gate-k95pp9k0`，基线 `79751c6233d7650a50db7ae060ccdc91a43ba624`，最终实现与测试摘要见 `inputs-final.json`／`snapshot-final`。期间共享版本提交 `9e5fa266b`（v0.1.24）已包含产品实现；交付前逐文件核对，当前工作区实现与冻结输入相同，最终测试增强和本节另行提交。

- `make test-common-frontend`：最终 264/264，通过，无失败／跳过；覆盖默认上限不变、显式上限类型／边界／超限拒绝、饼图拒绝覆盖、完整结果约束、隐藏标签仍保留零／null／tooltip、缩放保留全数据及主题颜色（`common-final.log`）。
- Monitor `npm test`：101/101，通过（`monitor-unit-final.log`）。沿标准浏览器入口 `npm run test:e2e -- node-resources.spec.js network-bars.spec.js`：最终 69/69，通过（`browser-final.log`）；随后仅增强绘图动画完成等待和时段切换重置断言，`npm run test:e2e -- network-bars.spec.js` 最终 2/2，通过（`network-settled-browser.log`），其余 67 项输入未变、沿有效证据复用。覆盖中英文 620 像素窄屏、5 分钟 21 点／1 小时 241 点／24 小时 961 点／7 天 984 点、乱序响应、原步长与单位、各方向独立缺样／陈旧值排除、真实零值、切换方向及采样明细、错误设备／预算失败后恢复；既有磁盘、内存双轴与撤权回归同时通过。
- 缩放使用实际鼠标拖动绘制出的滑块手柄，确认不新增请求，完整两组 984 点仍保留；主题重绘和刷新恢复视口，切换时段及查询错误清空视口。最终截图等待图引擎动画实际完成，已核对中英文五分钟及七天局部稳定绘图：`addp-network-bars-EXT7Am/zh-cn-5m.png`、`addp-network-bars-MPmUTl/zh-cn-7d-zoom.png`、`addp-network-bars-8UKtyV/en-5m.png`、`addp-network-bars-n2SGks/en-7d-zoom.png`（均位于同一 macOS 临时目录父级）。
- Monitor `npm run build` 通过（`build-final.log`）；`make test-frontend-ci-registration` 20 项及 21 个前端登记检查通过（`registration.log`）。现有共享依赖发现、Monitor 前端 Make 入口与 Playwright 自动发现覆盖新增 spec，无需新增 workflow。

初始失败不计通过：新增七天用例曾误写 630 秒／961 点，核对后端既有 plan 后修正为 615 秒／984 点，未改变产品步长、上限或查询；初始滑块测试使用配置坐标偏移，真实渲染手柄中心受图引擎布局影响，点击未命中。运行时绘制边界诊断保留在 `zoom-hit-test.log`；最终从实际绘制手柄取点并执行真实拖动，未增加等待预算、重试或减弱缩放断言。初始失败日志与报告分别保留在 `monitor-browser.log`、`network-zoom-browser.log` 及对应 `addp-monitor-playwright-results-*`；最终等待动画完成避免将过渡帧当稳定实图，未据此修改产品数据或过滤模式。

以上为本地 T1/T3，不计真实来源与身份的 Hosted T4 或生产 T5；未运行完整 Monitor 其他领域浏览器全集，不外推其结果。本轮未查询、触发或重跑云端 CI／Hosted，未设置定时任务、重启个人服务或修改个人数据库。CI 和 Hosted 结果等专门会话回传。清单第 38 行仅补网络柱图的局部缩放，第 39 行补网卡速率柱图；不能将全部图表拖拽缩放、图例筛选、资源热力面板及原行其他缺口同时标为完成。


### CPU、内存与磁盘趋势复用共享时间缩放（2026-10-23）

主机 CPU、内存和磁盘折线趋势接入既有共享 ChartRenderer 滑块，与网卡柱图沿同一路径保存临时视口。缩放只改变图中可见范围，完整 rows／series、查询窗口／步长、采样明细及相邻时段比较保持不变；说明紧邻图表展示。视口绑定主机、指标组、设备维度、时段、固定锚点、比较模式和当前身份，刷新及主题重绘保留；同组内存指标、同设备读写方向切换保留，不同组或上述范围变化以及查询失败时清空。不新增指标、API、权限、依赖或持久化配置；负载、运行时长、文件系统、服务及引擎图表不在本批接入范围。

本地冻结输入根为 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-time-zoom-io4lexon`，基线 `a53690c9f933e7ca0a6a32f18bfee0a9a6f043e3`，`snapshot` 只叠加本批路径，`inputs-final.json` 记录七项实现／测试／规范的 SHA-256。交付前工作区逐文件一致；期间 main 上其他提交未改变 Monitor 前端、common-frontend 或本批标准测试执行脚本。现有 Make 前端 owner 登记、共享依赖扩散与 Playwright 自动发现覆盖新增场景，无需新增 workflow。

- `make test-common-frontend`：264/264，通过，无失败／跳过（`common.log`）；Monitor `npm test`：101/101，通过（`unit.log`）。共享 renderer 产品代码未再修改。
- Monitor 标准 `npm run test:e2e -- node-resources.spec.js network-bars.spec.js host-resource-zoom.spec.js resource-comparison.spec.js`：最终 96/96，通过（`browser-final.log`）。新增九项覆盖 CPU／内存／磁盘中英文窄屏、真实鼠标拖动、七天完整 984 点不裁剪、主题及容器尺寸变化、手动／自动刷新后恢复、内存双轴和同设备双方向切换、独立缺样、时段／比较模式／不同指标组切换及查询失败／身份撤销清除；CPU 和内存时段对比仍使用原完整范围，采样明细行数和比较结果不受局部缩放影响。既有主机资源、网卡柱图和时段比较回归同时通过，未把此结果外推整个 Monitor 浏览器全集。
- Monitor `npm run build` 通过（`build.log`），仅有既有产物 chunk 大小提示；`make test-frontend-ci-registration` 20 项及 21 个前端登记检查通过（`registration.log`）。
- 等待图引擎实际绘制动画完成后，逐一核对六张稳定图：中文 CPU／内存／磁盘分别为 `addp-host-zoom-wyeyJO/cpu-zh-cn.png`、`addp-host-zoom-DgobXJ/memory-zh-cn.png`、`addp-host-zoom-dVqTTx/disk-zh-cn.png`，英文分别为 `addp-host-zoom-ZNYrtD/cpu-en.png`、`addp-host-zoom-SsDgoL/memory-en.png`、`addp-host-zoom-xn49JR/disk-en.png`（均在上述冻结输入根的同一 macOS 临时目录父级）。轴、单位、内存双轴、图例、局部时段与滑块均可见，窄屏无横向溢出。

初始新增场景为 8/9，自动刷新场景失败于测试夹具读取不到图引擎模块；日志 `zoom-browser.log` 与报告 `addp-monitor-playwright-results-ymYElk` 保留。查明 Playwright 虚拟时钟同时替换 Performance 的资源读取接口并返回空数组；共享测试 helper 改为从页面实际资源请求记录模块 URL，再查询真实图实例，不保留性能表 fallback，也不改变产品逻辑、等待预算、重试或业务断言。修正后自动刷新专项 1/1（`automatic-zoom.log`），随后上述最终 96 项整组通过；最初失败不计通过。测试时钟只在隔离浏览器验证刷新，不是定时任务。

以上为本地 T1/T3 与构建／登记证据，浏览器使用受控 HTTP 夹具，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5；本批未新跑 T2，原指标口径和来源未变。未查询、触发或重跑 CI／Hosted，未设置定时任务、重启个人服务或修改个人数据库；云端结果等专门会话回传。清单第 38 行仅补 CPU／内存／磁盘共享时间缩放子项，不能将所有图表缩放、完整图例交互验收或原行其他缺口一并记为完成。


### 主机趋势图例筛选与悬停交互验收（2026-10-23）

沿现有共享 ChartRenderer 补齐主机 CPU、内存、磁盘及网卡趋势的真实鼠标交互验收，产品渲染器、页面、指标、API、权限及查询均未修改。测试共用唯一 `resource-chart.fixture.js`，将原缩放专用状态读取统一为图表状态读取，按真实绘制图例的边界点击，并用图引擎坐标转换定位原始评估点后移动鼠标；不以 dispatchAction 模拟点击或直接调用 tooltip formatter 代替交互。已有缩放与网卡测试同步使用该 helper，旧函数路径删除。

冻结根 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-trend-interactions-mlaz8a33`，基线 `3217e418c76a837ce91e0865969341b52ba5ca03`；`inputs-final.json` 记录四项测试输入，交付前逐项哈希一致。标准入口提供独立回环 Vite 端口、源码及缓存隔离和退出清理，没有访问个人 Console 或登录态。本轮最终使用已安装的 Node v24.11.1（`fnm exec --using v24.11.1` 仅影响当次命令，遵从根 `.node-version`，不修改全局配置）。

- `npm run test:e2e -- host-trend-interactions.spec.js network-bars.spec.js host-resource-zoom.spec.js`：最终 22/22，通过，无失败／跳过（`browser-node24.log`）。新增 11 项覆盖四类主机图表的中英文 620 像素场景、单项隐藏／全部隐藏／恢复、悬停时间与精确值及单位、真实零／空值／陈旧值、缩放后原始索引、内存左右轴单位、采样明细和 URL／查询次数不变；CPU／内存完整时段比较结果不受显隐／悬停／缩放影响，查询失败及身份撤销清除可见提示与旧图。原 11 项缩放／网卡场景同时通过，未外推全模块或其他趋势图。
- Monitor `npm test`：101/101，通过（`unit-node24.log`）；`npm run build` 通过，仅有既有 chunk 大小提示（`build-node24.log`）。`make test-frontend-ci-registration`：20 项及 21 个前端登记检查通过（`registration-node24.log`）；新增 spec 沿既有 Playwright 自动发现，未新增入口、依赖或 workflow。
- 已逐一查看最终八张悬停稳定截图，确认中文／英文时间、CPU 百分比、内存 GiB 与百分比、磁盘及网卡 MiB/s 均清楚，缩放后图例和提示可见。截图父目录为上述冻结根的同一 macOS 临时目录父级：中文 `addp-trend-interaction-wE3AC0/cpu-zh-cn.png`、`addp-trend-interaction-WtUfKD/memory-zh-cn.png`、`addp-trend-interaction-VPTfxJ/disk-zh-cn.png`、`addp-trend-interaction-uMrVNX/network-zh-cn.png`；英文 `addp-trend-interaction-Oiz3T8/cpu-en.png`、`addp-trend-interaction-tQcryg/memory-en.png`、`addp-trend-interaction-XnPQa1/disk-en.png`、`addp-trend-interaction-2TGDiL/network-en.png`。

最初单项测试将陈旧百分比夹具错误设为超过 100，页面按原响应校验拒绝整图，日志 `interaction-cpu-initial.log` 及报告 `addp-monitor-playwright-results-OkYGg3` 保留。夹具修正为范围内的陈旧百分比，仍要求该点不显示数值，未放宽产品校验、等待或断言；随后专项通过。默认终端 Node 为 v25.2.1，之前的 22 项及新增断言后 11 项通过保留在 `interaction-browser.log`／`interaction-browser-final.log`，最终交付结果以符合仓库工具链的上述 Node 24 门禁为准。

本批仅为受控 HTTP 夹具的本地 T1/T3、构建及登记证据，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5；未新跑 T2 或其余领域浏览器全集，未重跑输入未变的共享组件单测。未查询／触发／重跑 CI 或 Hosted，未设置定时任务、重启个人服务或修改个人数据库。清单第 38 行可补上述四类主机图表的图例筛选与悬停本地验收，不代表所有图表、真实运行及原行其他缺口均已完成；CI 和 Hosted 等专门会话回传。


### 负载、运行时长与文件系统趋势统一时间缩放（2026-10-23）

主机页全部既有趋势统一开启现有共享 ChartRenderer 滑块，删除原 CPU／内存／磁盘／网络类别开关；新增覆盖负载、运行时长、文件系统容量／比例及 inode 数量／比例，不新增 renderer、指标、API、查询或权限。沿既有相对视口、图旁说明、原查询步长和完整结果；刷新保留，指标组、主机、时间、比较模式或身份改变及查询失败时重置。文件系统沿完整 device／mountpoint／fstype 区分对象，容量与 inode 的不同指标切换重置；不按挂载名猜同一资源。原有负载任务数、时长秒、容量 IEC、比例与数量口径均未改变。

冻结根 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-all-trend-zoom-kztnpbgt`，基线 `dcf9b22460d42ff3d8782051e214cf5da1746ab6`，`inputs-final.json` 为四项实现／测试／规范输入摘要，交付前与工作区逐文件一致；快照不包含其他会话 WIP。所有命令使用已安装 Node v24.11.1（`fnm exec --using v24.11.1`），沿标准入口及独立端口运行：

- Monitor `npm test` 101/101、`npm run build`、`make test-frontend-ci-registration` 的 20 项及 21 个前端登记均通过（`unit.log`、`build.log`、`registration.log`）；构建仅有既有 chunk 大小提示。共享 renderer 产品代码、依赖及测试注册入口未改，既有 Monitor owner／Playwright 自动发现覆盖本批，无需新增 workflow。
- `npm run test:e2e -- node-resources.spec.js network-bars.spec.js host-resource-zoom.spec.js host-trend-interactions.spec.js resource-comparison.spec.js` 首轮 119/120，一项在窄屏切换后的同步布局检查失败，不能记聚合通过（`browser-final.log`，失败报告 `addp-monitor-playwright-results-6Mdfe1`）。完成下述诊断并仅修测试的绘制完成条件后，`npm run test:e2e -- host-resource-zoom.spec.js` 最终 22/22，通过（`zoom-final.log`）；其余 98 项输入未变，复用本轮通过证据，合计 120 个唯一相关场景具备通过结果，不声称首轮命令成功或整个 Monitor 浏览器全集通过。
- 缩放矩阵覆盖中英文 620 像素、七天 984 点、负载小数、运行时长、文件系统 GiB／百分比与 inode 整数／百分比、真实鼠标拖动、不新增请求或裁剪数据、主题及宽窄切换、刷新保留、缺样断点、范围／错误重置。新增受控两挂载点夹具沿真实表格按钮选择，验证刷新保留、挂载变更及容量／inode 切换重置与精确请求维度；现有自动刷新场景增加负载缩放恢复，原 CPU／内存／磁盘／网络和时段比较／撤权回归保留。
- 最终十二张新类别中英文稳定实图已逐一核对单位、曲线和滑块，路径见 `zoom-final.log` 中 load／uptime／filesystem-percent／filesystem-capacity／inode-percent／inode-count 截图条目，均在上述冻结根同一 macOS 临时目录父级；未将沿用的运行时长秒轴改为其他时间单位。

失败诊断保留 `layout-diagnostic.log`、`resize-event-diagnostic.log`、`resize-observer-diagnostic.log`。首次观察未重现瞬态，不据单次通过宣称已定位；随后在宽屏实际绘制完成再缩回窄屏的事件采样中，记录 viewport=620、容器=572、Canvas=852、页面 scrollWidth=876，绘制完成后 Canvas=572、页面=620。共享 ResizeObserver 按既有异步路径正常恢复；旧测试在两次视口设置后立即检查页面宽度，未等待实际绘制。最终用同一默认断言预算等待两次 Canvas／容器宽度一致且页面无溢出，不加重试、延长超时、放宽宽度限制或修改产品布局。三个诊断场景不是交付通过计数；本批最初负载／挂载专项 3/3 另存 `zoom-focused.log`。

以上为本地 T1/T3、构建及登记证据，浏览器为受控 HTTP 夹具，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5；未新跑 T2 或未涉及 owner 的门禁，未把缩放接入外推为专业图表或全部原始功能完成。未查询／触发／重跑 CI 或 Hosted、设置定时任务、重启个人服务或修改个人数据库。清单第 38 行可补主机负载／时长／文件系统共享缩放的本地结果；真实页面与来源验收仍等专门会话回传。


### 主机运行时长趋势的自适应显示单位（2026-10-23）

主机运行时长曲线按完整已加载时窗内有效点的最大秒数选择秒／分钟／小时／天，纵轴与 tooltip 使用同一单位、两位小数；缩放不重选量级，刷新跨量级可以更新。共享 `scaleDurationValue` 与原复合时长格式化共用唯一 day／hour／minute／second 换算表，不读取时区或舍弃原始精度；Monitor 只换算图表 rows 与字段呈现。API 秒值、采样证据、重启判断、当前卡片及采样明细的原复合时长格式均不变。零值有效，陈旧／缺样不选级且保持 null，全缺样默认秒；不新增配置、依赖、API 或权限。

冻结根 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-uptime-units-k46w9uio`，基线 `d01c5772f84d19c74010d1bd048433c8000d85be`；最终 `inputs-final.json` 记录十一项产品／测试／规范输入，交付前与工作区逐项一致，快照排除其他 owner WIP。各命令通过 `fnm exec --using v24.11.1` 使用根工具链，沿已有独立端口及清理入口：

- `make test-common-frontend`：267/267，通过，无失败／跳过（`common.log`）。包含时长量级阈值、小数／零／非法值及原复合时长格式回归；其他共享单位与图表约束保持。Monitor `npm test` 101/101、`npm run build` 及 `make test-frontend-ci-registration` 的 20 项／21 个前端登记均通过（`unit.log`、`build.log`、`registration.log`），构建仅有既有 chunk 大小提示。现有共享依赖发现、Monitor owner 与 Playwright 自动发现覆盖新增 spec，无需修改 workflow。
- 新增 `npm run test:e2e -- uptime-units.spec.js` 首次 2/2，通过（`uptime-browser.log`）；覆盖中英文秒→分钟→小时→天、真实悬停、跨量级刷新保留缩放、图例显隐、原采样明细与当前卡片不变、陈旧极值排除、独立缺样／全缺样、真实零、错误清除与恢复，查询仍只含原 uptime 指标。267 项共享 T1 与真实 Monitor 消费页面共同验证新格式化入口，不外推其他消费模块的完整页面验收。
- 组合 `npm run test:e2e -- node-resources.spec.js network-bars.spec.js host-resource-zoom.spec.js host-trend-interactions.spec.js resource-comparison.spec.js uptime-units.spec.js` 首轮 115/122，不能记成功（`browser-final.log`，失败报告 `addp-monitor-playwright-results-g8XJhD`）。失败包括拖动未改变视口、动画／加载等待、创建 page 阶段超时、明细点击、错误切换及缩放值捕获；创建 page 的一项尚未运行产品。未延长 5 秒断言／30 秒场景预算，不加重试或跳过。
- 确认滑块实时事件经过 ECharts 节流：最初新英文场景捕获 54.770992366412216，后续实际视口为 59.732824427480914。共享测试 helper 改为在 mouseup 后等待已应用 start／end 与实际滑块 `_range` 一致，再保存或比较；只等真实事件完成，不调用 dispatchAction 或强制 flush，不改变产品行为。最终 `npm run test:e2e -- host-resource-zoom.spec.js host-trend-interactions.spec.js network-bars.spec.js uptime-units.spec.js` 37/37，通过（`affected-browser-final.log`）。
- 其余三个失败场景保留原产品和断言，沿 `npm run test:e2e -- node-resources.spec.js resource-comparison.spec.js --grep 'network throughput restores|memory capacity and usage.*zh-cn|expired-valid clears' --trace on` 诊断复验，3/3，通过（`remaining-browser-diagnostic.log`）。未受 helper 调整影响的其余 82 项沿本轮有效通过证据复用，合计 122 个唯一相关场景具有通过结果；不宣称初始聚合通过或完整 Monitor 浏览器全集通过。

诊断 `interaction-diagnostic.log` 的三个场景通过，记录了实际鼠标命中、绘制帧、页面可见性与请求错误，未复现最初的加载／动画失败；临时诊断 hooks 已从最终快照和工作区移除。新的通过结果不证明这些历史偶发失败的根因已经确定或修复；只有上述滑块中间状态的同步条件得到针对性补齐，其余历史加载／动画超时仍待定位，不据 CPU 使用率快照推定原因。原 34.6 分钟聚合执行失败保留，不通过重试计成功。

最终中英文窄屏悬停图已逐一核对：`addp-uptime-units-VtugVb/zh-cn.png`、`addp-uptime-units-Fq3pIK/en.png`（与冻结根位于同一 macOS 临时目录父级）。286,435 秒的图轴及悬停显示 3.32 天／days，明细仍为 3 天 7 小时 33 分钟／对应英文；图例和缩放保留。以上为本地 T1/T3、构建／登记，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5，未新跑 T2。未查询／触发／重跑云端 CI 或 Hosted、设置定时任务、重启个人服务或修改个人数据库；云端验收继续等专门会话回传。清单第 38 行仅补时长趋势显示量级与当前本地验证，历史偶发失败和其他原行缺口不能据此关闭。


CI 专门会话回传补充（2026-10-23）：固定提交 `2cd3e278fc00c19183f3508f45253d31e9eef9f8` 的 [Platform Monitor frontend Job 120242877828](https://github.com/pampa0629/addp/actions/runs/39901181026/job/120242877828) 已实际执行 `Run Monitor frontend gate` 并成功。Owner 已核对 Job 的提交身份／实际步骤及原始日志：101 项单测、339 项浏览器均通过，失败／跳过为零，构建 10.36 秒；包含 memory／disk／network 中英文六项图例／悬停／缩放及新增两项时长单位场景。原始材料为 `/tmp/addp-ci-20261023-070013/critical-120242877828.json`、`job-120242877828.log`、`Monitor-2cd3-frontend-actual-pass-audit.json`。这是该固定输入的前端 CI 证据，不表示父 Platform workflow 全部通过，不覆盖后续服务独立缩放／进程环比提交、实源告警 T2 或 Hosted；旧输入失败及尚未复现的加载／动画偶发根因继续保留，未重跑任何门禁。

### 服务实例 CPU/RSS 趋势的独立时间缩放（2026-10-23）

服务趋势弹窗的 CPU、RSS 图分别接入既有共享 ChartRenderer 时间滑块，图旁提供中英文操作提示；两个临时视口独立，不裁剪完整响应、不新增请求或 URL 参数。相同已提交范围的手动／自动刷新保留相对视口；关闭弹窗、切换实例／时段／筛选或身份变化时重置，错误及撤权清除旧图与视口。指标、当前运行实例历史边界、权限及生产采集均未改变。

冻结基线 `10bfdbdf194ebef14afef40cb4f7db4b1d3a689e` 加本批七项输入，使用 Node 24.19.0、既有依赖和标准隔离入口执行：

- `npm run test:e2e -- process-trend-zoom.spec.js`：7/7 通过，见 `zoom-browser.log`。中英文窄屏真实拖动分别缩放两图；完整样本／真实零／缺样及陈旧断点保留，无额外请求／URL 变化；悬停、图例显隐、主题重绘、手动／自动刷新保留，时段／实例／关闭再打开／身份变化重置，503、403 和错误运行身份的清除与恢复均通过。
- `make test-monitor-frontend`：退出 0，101 项单元测试、346 项浏览器测试及生产构建通过，见 `frontend-full.log`；上述 7 项包含在 346 项内，不累加为唯一场景数量。完整门禁同时覆盖复用多图定位参数后的主机缩放、图例／悬停及既有进程页面布局／筛选，未改共享渲染器实现。构建保留原有大包提示，不将提示计为失败或性能验收。

证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-trend-zoom-p2lmhr_b/` 保存原始日志、`inputs.json`、`frontend-inputs-sha256.json` 及原表只读行号引用。浏览器使用独立端口和受控 API，成功后由标准入口清理运行目录；不操作 Safari、个人登录态或服务，不重启后端。现有 Monitor 前端标准门禁及 Platform CI 矩阵自动发现新 spec，无新增依赖、入口或 workflow。本轮未查询或触发 CI／Hosted，真实页面验收等专门会话回传，不设置定时。此项前端通过不替代上一批 CPU/RSS 告警真实触发／恢复 T2 或正式身份验收。


二次本地窗口（2026-10-23 17:11:14–18:11:14 Asia/Shanghai，用户重新授权）：保留 8 CPU／12 GiB、4 GiB 停止线及既有正式身份范围。第一次实测由 `measure_owned_volumes` 资源采样超时停止，最后成功样本 3,236,450,304 字节，44 项成功样本；自动报告明确失败、清理通过。按已记录子步骤调查，原采样把 Docker 容器创建／启动／退出与 du 放在同一四秒预算。只调整为当次只读辅助容器复用，卷集合变化才重建，采样串行且退出先释放 BuildKit 卷挂载；保留 0.25 CPU／64 MiB、无网络、只读卷及原采样／停止边界，辅助容器 /data 用受限 tmpfs，不产生额外匿名卷。代码 `97714a1ac68624ecaf56573051bb9febcaf2075b`；最终 `make test-infra-runtime-log-lifecycle` 退出 0，42 项入口契约、JS 语法／登记／System ExecutionTrace Go及原60项生命周期通过。

同窗口第二次实际冷构建记录 56 项采样，最长测量 1.139 秒、最大相邻采样完成间隔约4.27秒，约四分钟构建观察未再超时；随后在 product_minio 正确触发 `owned_volume_stop_line_reached`。完整触线样本 4,362,616,832 字节，超过停止线 67,649,536 字节（约64.52 MiB），末次测量0.902秒，运行硬配额3.25 CPU／5.0625 GiB；没有放宽预算或把触线记录作通过。两次均未创建业务部署、未准备正式身份或执行查询，Trace／日志／Console闭环未执行，清理均通过。安全报告／源码哈希／窗口及独立清理回执见 `/tmp/addp-p19-formal-local-20261023-window2/{attempt-1-report.json,attempt-2-report.json,attempt-2-source.json,receipt.json,lifecycle-final.log}`。

当前缺口是既有 MinIO 基础依赖的冷构建已达到本地准备预算。本会话提出仅在核验固定 MinIO／mc 源提交及镜像ID后复用既有产品镜像，本专项后端／前端仍按当前已提交源码构建；该依赖准备范围调整正在等待用户明确确认，尚未实施，不凭已有缓存降低原验证口径，也不续开或延长本窗口。不运行 Hosted、容量或定时，不接管个人服务。


## 服务进程 CPU／RSS 相邻时段比较（本地）

2026-10-23，沿用户确认的均值与原单位变化量口径，当前运行实例支持 5 分钟／1 小时／24 小时与上一等长时段比较。CPU 用核、RSS 用 IEC 容量，按未舍入评估点均值计算本期减上期；零基线有效，不给增长百分比。两期任一点缺样、陈旧或启动裁剪时不出均值／差值，各指标独立显示有效／预期点数；CPU 启动保护沿后端曲线评估时刻，抓取新鲜度独立核验。结果和时间就近展示，图中仅显示本期；缩放不重算比较。主机与服务共用 `common-frontend/basic/src/utils/evaluationPeriodComparison.mjs` 及 Monitor 唯一 `ResourcePeriodComparison.vue`，旧主机均值循环与重复卡片已删除。

冻结输入基线 `fb3d25f4b52324273d1fdcc3cb27c30df695b569`，仅叠加本批文件；证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-comparison-s_1g44u9`，`inputs.json` 记录本批最终文件，`frontend-full-inputs-sha256.json` 与 `frontend-inputs-sha256.json` 分别记录完整门禁及最终针对性回归的 435 项输入。交付前与共享工作区核验无前端输入漂移。实际命令均使用 Node v24.19.0：

- `make test-common-frontend`：272 项通过；共享均值、分界点归属、缺样、启动裁剪网格及唯一消费路径由确定性测试覆盖。
- `make test-monitor-frontend`：首次完整版本 109 项 T1、361 项浏览器 T3 及构建通过，见 `monitor-frontend-full.log`。此后仅修正比较函数内 CPU 预热判断的时间依据，并新增对应边界测试，没有把这份完整门禁记成最终源码重新全跑。
- 最终 `npm --prefix monitor/frontend test`：110 项 T1 通过；`npm --prefix monitor/frontend run test:e2e -- process-comparison.spec.js resource-comparison.spec.js`：33 项 T3 通过（15 项服务比较及 18 项主机比较回归）；`npm --prefix monitor/frontend run build`：通过。见 `final-monitor-unit.log`、`final-comparison-browser.log`、`final-build.log`。新增用例在独立副本恢复旧抓取时间判断时失败（`startup-regression-red.log`），最终评估时间判断通过。该末次修改只影响启用服务比较的分支，最终定向回归覆盖此分支，其余未变分支沿用前述完整门禁有效证据，不重复累计测试项。
- `make test-frontend-ci-registration`：20 项检查通过，21 个前端登记检查通过；共享／Monitor 现有自动发现和 Platform 前端编排覆盖本批，不新增 Makefile 或 workflow 路径。受影响的是新增共享纯计算的直接消费者 Monitor，未改动其他模块的导出或消费行为。

最终 T3 使用 HTTP 协议夹具与真实共享图表，覆盖两种语言／窄屏、原单位升降及零值、两期缺样、启动裁剪、48 小时查询的自适应步长、7 天拒绝比较、URL／刷新／关闭、身份及错误清除、缩放不重算比较；复制保存并人工检查了 `screenshots/` 下中英文卡片截图。未连接个人库或重启个人服务，未运行本批正式身份真实数据／长窗 T4 或 Hosted，CI／Hosted 等专门会话回传；此结果不覆盖尚未通过的 CPU／RSS 告警实源触发恢复 T2，不证明同比或长期留存已实现。原表第 18 行（B 列 17）仍为部分实现。


MinIO 产品镜像核验复用入口准备（2026-10-23，原窗口结束后）：用户确认复用已有固定版本 MinIO 产品镜像，先核验 MinIO／mc 源提交与镜像 ID，本专项代码继续标准源码构建。收到确认时北京时间18:45，原18:11:14截止时间已过，因此没有启动新容器，也没有执行真实镜像二进制核验、业务部署或正式身份闭环。先修运行观测规范，再将唯一入口改为从当前提交的 scripts/infra/Dockerfile.minio 提取两组件固定发布版本／40位源码提交；已有镜像必须有不可变sha256 ID和Linux ARM64身份，两个只读／无网络／0.25CPU／256MiB检查逐一以该ID运行--version，核对发布版本及嵌入提交。通过的同一ID用于卷测量、真实MinIO及两个存储初始化步骤；缺失、不匹配、命令失败直接拒绝，不拉取替代品或回退冷构建。旧MinIO冷构建及其专用故障分类分支已删除。镜像版本与嵌入提交校验不等同于重新源码编译／字节级可复现证明。

代码及规范提交5681227a0，已推送main。标准 make test-infra-runtime-log-lifecycle 退出0：46项当时的入口契约、两份JS语法、完整CI登记、System ExecutionTrace定向Go及既有60项生命周期通过；随后仅加强mc不匹配／缺失镜像用例，最终 make test-monitor-traces-local-runner 退出0，47项入口契约及其余定向检查通过，60项未改输入复用当次有效结果。没有新增测试入口或依赖，现有Platform生命周期及路径登记仍覆盖；未查询或手工调度CI。安全输入摘要与日志见 /tmp/addp-p19-minio-reuse-preparation-20261023/{receipt.json,minio-reuse-lifecycle.log,minio-reuse-final-tests.log}，明确标注为窗口外的确定性准备，actual_minio_binary_verification与formal_identity_closure均为not_run。

当前已具备核验后复用的单一路径；真实MinIO／mc核验及正式密码/MFA、执行、Trace／日志／页面闭环均未运行、未计通过。需新的同上限本地窗口方可继续实际运行；不自动续开、不运行Hosted、容量或定时，不接管个人服务。


### 主机固定目录全部图例与悬停交互（2026-10-23）

本批只补齐既有主机资源交互的本地验收，不新增产品指标、算法、权限、接口或部署变更。`host-trend-interactions.spec.js` 沿同一鼠标／图例／缩放 helper，覆盖当前全部 25 项固定指标：CPU 核数／忙碌率、内存三项、1／5／15 分钟负载、运行时长、五项文件系统容量、四项 inode、五项磁盘及两项网卡指标。每种组合实际读取对应采样时间／数值／单位，验证零与缺样／陈旧值、图例隐藏至全隐藏及恢复、缩放后精确悬停、原始图表点／URL／采样明细不变且无额外查询；负载按任务数、inode 与核数按整数，容量／速率和时长沿现有单位。错误／撤权清除补到负载与字节／inode 挂载分支。

冻结基线 `d23233e15e61eb77d5bef576043eef11ec210a99`，证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-interactions-rest-5ixp4m82`；`inputs.json` 记录本批文件，`frontend-inputs-sha256.json` 记录 432 项前端输入，提交前与共享工作区一致。Node v24.19.0 下，最终标准入口 `npm --prefix monitor/frontend run test:e2e -- host-trend-interactions.spec.js` 48/48 通过（`interaction-final-browser.log`，4.2 分钟，42 个中英文指标组合、2 个原比较回归和 4 个错误／撤权场景；不将此前 40 项聚合重复累计）。`npm --prefix monitor/frontend test` 110/110 通过，`make test-frontend-ci-registration` 的 20 项及 21 个前端登记检查通过（`unit.log`、`registration.log`）；原有 Playwright glob 和 Monitor CI 自动覆盖本文件，无需另加编排。42 张窄屏实图保存在 `screenshots/`，负载和 inode 截图已人工核对。

本批仅为受控 HTTP 夹具加真实图表的本地 T3 与 T1／登记证据；产品源码未变，未重建或重跑输入未变的其余模块／前端全集。未运行 T2、真实身份／来源 T4、Hosted 或生产 T5，未追查／重跑 CI、设置定时任务或重启个人服务。历史加载／动画偶发失败仍不能由本次通过推定根因已修复。原表第 38 行图例／悬停本地专项缺口可关闭，真实运行、留存与原行其他缺口保留。


## 主机 TCP 当前连接数（本地）

2026-10-23，按用户确认口径新增固定主机标量 `node.tcp.current_connections`，单位 `connections`，只读当前宿主网络命名空间的 ESTABLISHED＋CLOSE_WAIT，不含监听／TIME_WAIT。复用唯一 `resourcequery` gauge、节点与来源身份、预算、新鲜度、即时和趋势 API；固定主机目录由 25 项增至 26 项，标量由九项增至十项。整数须在 0～2^53−1，合法零值保留，缺样不变成零；采集器缺失、重复原始序列及外来节点不能构造有效值。受限 Docker Desktop 来源不开放 netstat，不补容器命名空间统计，返回 `no_data`。页面新增就近口径／缺样提示、即时卡片及同一趋势、缩放、图例与悬停能力。CPU／内存摘要、TOP5、告警、生产权限及身份不变。

冻结产品基线 `3f0105d08bf2fa632f1888c5a9331c062cf89240`，仅叠加本批文件，证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-tcp-current-7nslrz9k`；`inputs.json` 记录本批文件，`product-inputs-sha256.json` 记录 2118 项产品、测试及共享依赖输入，提交前与共享工作区一致。登记检查另同步当时最新已提交编排输入，版本见 `registration-head.txt`；未混入其他会话未提交实现。实际验证：

- `make test-module MODULE=monitor GO_ONLY=1`：Monitor Go T1 通过（`go-t1.log`）。固定目录、节点／来源隔离、整数／安全上限、负值／非有限值、缺样／陈旧／未来样本均有确定性用例；默认跳过的集成用例不算 T2。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources`：退出 0（`host-resources-t2.log`）。真实 node_exporter／Prometheus 的全来源即时及趋势、受限 Desktop 的 TCP 无数据分支通过；同一入口执行五个固定 promtool 场景：有效零、有效数量、重复来源、外来节点和缺失采集器。原有主机来源故障恢复及预算回归通过，退出核验当次容器／网络／卷和临时文件零残留。这是本地隔离 T2，不是原生 Linux 正式身份页面 T4，亦不作为服务实例 CPU／RSS 告警实源验收结果。
- Node v24.19.0 下 `make test-monitor-frontend`：111 项 T1、405 项浏览器 T3 及构建全部通过（`frontend-full.log`）。覆盖 TCP 中英文整数／零值、来源缺失后恢复、错误小数／负数／越界拒绝，及真实鼠标图例／悬停／缩放；其余主机、服务和错误／撤权场景一并回归。`screenshots/` 保留中英文 TCP 窄屏趋势实图，中文精确整数与单位已人工核对。页面测试使用受控 HTTP 夹具，不冒充真实来源页面。
- `bash scripts/swagger/gen-swagger.sh monitor` 与 `bash scripts/swagger/check-route-coverage.sh monitor`：最终生成成功，90 个公开路由方法覆盖一致（`swagger-generation-final.log`、`swagger-coverage.log`）；生成工具固定 module v1.16.6。首次未复制根 go.work 的生成失败仅为临时副本准备错误，保留在 `swagger-generation.log`，不计为通过证据。
- `make test-node-metrics-online-runner`：最终 139 项本地入口／身份／收据守卫及 System 既有 Metrics Go 夹具通过（`online-runner-final.log`）；`make test-frontend-ci-registration`：20 项检查、21 个前端登记通过（`registration-final.log`）。原有 Monitor 指标 T2、前端及 Console／Online 路径自动发现覆盖本批，无新增 workflow 或标准测试入口。
- `npm test -- --testNamePattern='Online scalar resource receipt identity'`（Console frontend）：最终 8 项指定收据测试通过（`console-receipts-final.log`），包含精确十项且拒绝旧九项请求；其余 186 项因过滤未执行，不计为 Console 全量通过。

用户同日确认原一次性 `platform-node-metrics` 临时平台管理员限定用途增加当次主机 TCP 当前值、趋势和同一真实 Console 页面。规范与标准 runner 已同步精确十项标量；仅为多出的 TCP 选中与恢复两轮各六读，将原 174 项收据上限调整为 186，仍严格拒绝未授权指标、重复／缺失、错误节点或 query。总预算、密码／MFA、独立 Prometheus、节点范围、无租户业务及退出清零保持原约束。本会话没有触发或轮询 CI／Hosted，没有设置定时任务或重启个人服务；真实身份／来源 T4 及 CI 等专门会话回传，生产 T5 未运行。原表第 37 行（B36，主机实时资源总览列表）的 TCP 指标能力已落地，但主机列表仍只展示核心摘要，综合健康评分与完整总览列仍未实现；第 38 行（B37，资源趋势曲线图）新增 TCP 本地证据，不由本次结果推导留存、长期比较或其他真实运行范围通过。


## 主机资源热力面板（本地）

2026-10-23，主机详情新增 Monitor 领域组合组件 `HostResourceHeatmap.vue`，复用已授权的即时 CPU 忙碌率、内存使用率和每个完整设备／挂载点／文件系统身份的使用率；不新增采集、接口、查询、权限或持久状态。百分比沿原指标与共享格式化，不从容量重新推导；CPU 保留一分钟窗口，每项就近展示原状态与采样时间。有效使用率按 0～100% 连续混合现有主题绿／红色，基础进度条复用 Element Plus；合法零和 100% 有效，陈旧／缺样不着色、不补零。未采集分区和来源失败显示原原因，分区不可用不抹除可读 CPU／内存。完整挂载身份保留于色块及趋势导航，CPU／内存导航清除挂载维度。没有综合健康分、颜色告警阈值或新增资源规则。

冻结基线及本批文件见 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-heatmap-s0n4pjbs/final-inputs.json`；最终 436 项前端／共享／浏览器入口输入记录在 `final-frontend-inputs-sha256.json`。初始完整回归的产品输入相同，仅本次新增测试的刷新同步／截图检查和服务环比测试的请求同步随后修订，完整回归输入另记 `full-frontend-inputs-sha256.json`，不把两次输入混为一份。

- `make test-monitor-frontend`：完整批次执行 111 项 T1，通过 407 项浏览器用例、失败 2 项，命令退出 2，未执行其后的构建（`frontend-full.log`）。两项分别为新热力用例在刷新完成前挂起下一轮，以及既有服务环比用例在取消比较后、尚未取得新摘要与趋势请求前断言上一条请求。最后仅修正这两个测试文件，产品及共享输入未变；最终 `npm --prefix monitor/frontend run test:e2e -- host-resource-heatmap.spec.js process-comparison.spec.js` 的 19/19 项通过（`final-affected-browser.log`，4 项热力及 15 项服务比较）。其余 390 项未变用例复用上述完整批次有效结果，不将完整入口先前的非零结果写成最终全量重新通过，也不重复累计交叠用例。服务用例用受控挂起唯一 `process_resource_summaries` 响应，证明 UI 已取消比较时旧请求仍在；释放后必须新增且只新增一条正确五分钟趋势请求，再核对起点。受控探针初轮误指向不存在的 observations 端点，失败记录保留在 `final-affected-browser-before-endpoint-fix.log`，已改为 API 客户端实际使用的 summaries，未改产品请求或数据口径。
- `npm --prefix monitor/frontend test`：111 项 T1 通过（`unit.log`）。
- 最终标准 `npm --prefix monitor/frontend run test:e2e -- host-resource-heatmap.spec.js`：4/4 浏览器 T3 通过（`heatmap-final-verified.log`），覆盖中英文精确数值／原挂载比例、相同挂载点的不同设备／文件系统、窄屏、主题变量动态变化、合法零／满量程、无额外请求、当前指标／完整挂载导航、陈旧／缺样不着色、分区故障后恢复、未采集、权限拒绝及身份切换后迟到响应清除。初轮失败为测试对 URL 编码和现有缺源文案的错误预期、夹具改写未采集状态，以及在上一轮刷新完成前挂起下一轮所造成的测试自锁；最终使用解析后的 URL、原文案／数据状态并在挂起前等待既有刷新按钮可用，未增加产品重试或改变刷新逻辑。
- `npm --prefix monitor/frontend run build`：最终构建通过（`build-final.log`）；未修改后端/API/Swagger，无需重建后端或新增 T2。
- `make test-frontend-ci-registration`：20 项检查及 21 个前端登记通过（`registration.log`）。原 Monitor Playwright glob 和前端 CI 自动覆盖新增组件／用例，无新增标准入口或 workflow；未触发或轮询 CI。

`screenshots/` 保留最终中英文浅色／深色窄屏面板实图，已人工核对中文浅色布局与单位。T3 使用受控 HTTP 夹具与真实组件，未操作个人 Safari 或重启个人服务；未运行真实身份／来源页面 Hosted、T4 或生产 T5，CI 和 Hosted 等专门会话回传，未设置定时任务。原表第 39 行（B38，主机底层资源明细）的热力面板能力已实现；该行的完整硬件汇总／分区健康判定及新增页面真实运行证据仍需分别核对，不能由本批界面色彩推定健康判定或告警通过。


### 正式身份本地第三窗口：镜像核验、守卫修复与部署配置拒绝（2026-10-23）

用户重新授权窗口为北京时间19:18:00–20:18:00，上限仍为8 CPU／12 GiB、全部当次卷及私有可写目录4 GiB停止线。六次尝试共用这一绝对截止时间，没有按重试续时。标准入口均为 `make test-monitor-traces-local`；未运行Hosted、容量模型或定时任务，未接管个人开发服务。

首次真实核验已有MinIO产品镜像成功：Linux ARM64，镜像ID `sha256:dd87261892226d0e651ba1d059205cdd4e93d94be3dd9c819abffbf23d994d0b`；MinIO `RELEASE.2025-10-15T17-29-55Z`嵌入源提交`9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`；mc `RELEASE.2025-08-13T08-35-41Z`，源提交`7394ce0dd2a80935aded936b09fa12cbb3cb8096`。两组件均以同一不可变镜像ID执行只读、无网络版本检查并匹配当前固定配方，报告保存版本输出哈希；这不等同于重新源码编译或可复现构建证明。

| 尝试 | 实际停止点 | 结果与清理 |
| --- | --- | --- |
| 1 | Orchestrator产品镜像构建；卷测量解析通用异常 | 失败，129项成功测量；零残留 |
| 2 | System镜像前的私有目录测量；du非零退出 | 失败，38项成功测量；零残留 |
| 3 | 对象缓存清理0.75秒期限 | 失败，41项成功测量；该过短期限方案已撤掉，零残留 |
| 4 | 全部产品镜像后主动停止BuildKit，被误判容器失败 | 失败，169项成功测量；零残留 |
| 5 | 前置停止检查复现BuildKit退出1、非OOM | 失败，在昂贵编译前拒绝；零残留 |
| 6 | 全部16个产品镜像及浏览器依赖完成后，Trace部署配置生成被拒绝 | 失败，180项成功测量；零残留 |

缓存清理会使du遍历中的文件消失。当前唯一测量路径在原四秒总预算内，仅对明确ENOENT重新进行完整扫描；不采用带缺失诊断的部分字节数，其他错误或预算耗尽仍停止。第4次实际发生一次私有目录完整重扫，最终测量0.508秒；该次最长测量1.055秒、最大完成间隔4.24秒。卷内扫描当次没有重扫，不据此推定首轮卷异常的全部根因。BuildKit通过成功的主动停止命令后实际退出码1，当前只允许当次明确主动停止的精确构建器ID出现该已核验状态；停止前须运行且非OOM，产品服务、一次性任务、其他退出码及OOM仍拒绝。停止检查已前置，末尾复用同一实现。第6次真实通过前后停止检查，180项测量最长0.807秒、最大完成间隔4.43秒。

第6次固定源码为`4aee4813909077cd3fa35906bf70353278ba89e2`，六个后端、Console及其余产品前端／Nginx共16镜像从该源码构建，报告保存不可变ID；浏览器依赖准备通过。部署生成拒绝根因为本地入口误用`module`，正式生成器只接受`module_name`。最终代码`162cb8747d211d3fe184e8acba48117f6123e934`已修正该字段，并把产品Trace／Compose配置准备移到构建器创建及编译之前，增加细分阶段和固定容器状态诊断，不引入兼容字段或第二路线。真实产品生成器的跨入口回归在恢复旧字段时失败、修正后通过；配置拒绝不得启动构建器亦有覆盖。

最终 `make test-infra-runtime-log-lifecycle` 退出0：55项本地入口／业务契约、JS语法、CI登记、System ExecutionTrace定向Go，以及既有60项生命周期通过。标准实际入口最后一次仍为失败；最终配置修复后的完整部署未重跑，正式密码/MFA、Tenant成功／受控失败执行、Trace与日志及Console闭环均未运行、未计通过。当前源码不是第6次完整实际运行的输入，不能把确定性回归记成正式运行验收。剩余窗口不足以重建并完成闭环，没有启动注定超时的完整重跑，也没有续开新窗口。

六次成功测量的总体峰值3,421,573,120字节（约3.19 GiB）、6.25 CPU／10.0625 GiB，均未触及原上限；这只覆盖准备构建，不能证明业务部署的资源表现。六次自动清理全部通过，独立复核当次容器、网络、卷、构建器、镜像标签和私有目录均为零，原MinIO镜像保留。安全报告、绝对窗口、输入提交、红绿回归及最终门禁日志见 `/tmp/addp-p19-formal-local-20261023-window3/` 中的 `attempt-1-report.json` 至 `attempt-6-report.json`、`receipt.json`、`cleanup-audit.json`、`deployment-contract-red.log`、`preflight-final-test.log`、`lifecycle-final.log`。清单实际第19行（B18）及第45行（B44）仍为部分实现，正式身份缺口保留；不将本次结果计为Linux Runner T4、Hosted、T5或真实72小时证明。


## 主机列表五项核心摘要（本地）

2026-10-23，按用户确认范围，主机列表展示 CPU 逻辑核数／一分钟忙碌率、内存总量／使用率与运行时长，默认每页 20 台。复用原单位、当前节点身份／版本、采样新鲜度、采集状态和列表生命周期；容量用 IEC 单位，运行时长用复合时长。固定主机与操作列、标题允许换行，每项保留独立状态。当前页只进行一次摘要 API 请求，不逐台读取详情或补齐失败列；返回乱序仍按节点身份映射，单项陈旧不抹除其他有效值，整批缺项拒绝并清空旧摘要，主机列表仍保留。

`/platform/resource_summaries` 唯一契约现在显式携带 `node_ids` 和 `metrics`，只接受上述五项的非空无重复子集，不保留省略参数的旧请求。唯一批量内核复用原公式、两次固定 Prometheus 请求、整批期限／并发和逐节点 User 授权；在 Owner 读取与指标查询前按节点数乘指标数预检预算。默认 20 × 5 = 100，21 台五项请求整批拒绝，不拆批或提高预算；TOP5 与采集配置页显式保持 CPU／内存百分比两项，原 50 台 TOP5 上限、比较和热力指标范围不变。API、调用方、Swagger 及既有 Hosted 收据／拒绝测试同步，仍只在原已确认十项节点指标的读取范围内选五项，不新增权限、角色或身份用途。

冻结基线 `d798875a0732cb18e42d1772799d7c91b007542e`，仅叠加本批文件。证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-list-core-v8j8u6ci`，`inputs.json` 记录最终本批文件，`frontend-inputs-sha256.json` 记录 437 项前端／共享／浏览器入口输入。其他会话的 engineApprovalInitialization 两项共享未提交 WIP 未混入冻结副本；本任务源码及其余前端输入与工作区核对一致，不外推整个共享工作区的 WIP。实际验证：

- `make test-module MODULE=monitor GO_ONLY=1`：Monitor Go T1 通过（`go-t1.log`）。五项／两项显式投影共用内核；20 台五项、50 台两项边界通过，21 台五项在任何 Owner／来源读取前拒绝；空／重复／未知／分组／不在摘要范围的指标拒绝，停用、跨节点、当前用户 Token 与完成时新鲜度沿原规则核验。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources`：退出 0（`host-resources-t2.log`）。真实 node_exporter／Prometheus 上五项摘要的完整来源及受限 Desktop VM 读取、外来节点缺样隔离通过；原 TOP5 的受控多源覆盖／预算及来源故障恢复回归通过。退出核验当次容器／网络／卷和临时文件零残留；该结果是本地隔离 T2，不是多真实主机、正式身份页面 T4 或生产 T5。
- Node v24.19.0 下，`npm --prefix monitor/frontend run test:e2e -- host-list-core.spec.js`：20 台主机的 2 项中英文专项通过（`core-list-browser.log`），验证一次精确五项批量读取、默认每页20台、CPU 核数／百分比、IEC 容量、复合时长、乱序身份匹配、单项陈旧、整批缺项后的清除／恢复及无逐行详情查询。后续完整门禁另覆盖最终表头换行。
- 最终 `make test-monitor-frontend`：112 项 T1、411/411 浏览器 T3 及构建全部通过，命令退出0（`frontend-full.log`，浏览器约15分钟，构建4.46秒）；本次输入下无失败或跳过。覆盖主机／采集列表的投影隔离、TOP5、详情、热力、趋势与服务／告警回归；不把前面的专项与整批交叠用例重复累计。`screenshots/` 保留最终五项列表中英文实图，已人工核对中文布局、单位与可读表头。
- 首轮 `npm --prefix monitor/frontend run test:e2e -- node-resources.spec.js monitoring-target-list.spec.js resource-overview.spec.js`：89 通过、3 失败（`focused-browser.log`）；失败是旧列表用例仍断言两列数值或不带 metrics 的请求，已按精确五项和必填选择修订，不放宽采集配置页两项断言或产品校验。
- `make test-node-metrics-online-runner`：139 项本地入口／身份／收据守卫及 System Metrics Go 夹具通过（`online-runner.log`）；新增拒绝省略指标选择、旧两项列表请求及缺失核心列的反例。仅为准备好的正式入口本地检查，没有运行 Hosted。
- `bash scripts/swagger/gen-swagger.sh monitor` 和 `bash scripts/swagger/check-route-coverage.sh monitor`：生成成功，90 个公开路由方法覆盖一致（`swagger-generation.log`、`swagger-coverage.log`）。固定 Swag module v1.16.6。`make test-frontend-ci-registration`：20 项检查与21个前端登记通过（`registration.log`）。现有 Monitor 前端／Go／指标 T2、Console／Online 路径自动发现覆盖本批，没有新增 workflow 或标准入口。

本会话不触发／轮询 CI 或 Hosted，不设置定时任务，不修改 IAM 或重启个人服务。因摘要请求契约同次更新，个人环境体验需由用户按标准入口重启 Monitor 后端；前端改动不要求重启其他后端。原表第37行（B36，主机实时资源总览列表）仍是部分实现：本批核心五项已具备，三档负载、TCP 与存储／网卡明细仍在主机详情，未补完整列表或综合健康评分；新列表的真实身份页面 Hosted 与生产规模证据继续待专门会话回传。

### 五项列表、热力面板与服务环比的 Linux 前端复核（2026-10-24）

专门 CI 会话回传 [Platform 39960204230 / Monitor job 120436001184](https://github.com/pampa0629/addp/actions/runs/39960204230/job/120436001184)，固定检出 `9bf8dddcbe8a589b0c7a85af6ded4bb44485a87e`，push、attempt 1。独立核对原始日志的实际 `make test-monitor-frontend`、检出 SHA、完整 411 条浏览器执行序号和最终汇总：112 项单元测试通过、失败／取消／跳过均为 0；411 项浏览器全部通过（17.9 分钟）；构建通过（7.06 秒），Monitor frontend gate 为 selected／success。包含中英文 20 台主机精确五项批量摘要、默认每页 20 台及独立状态、四项热力面板用例和十五项服务进程比较用例。

固定版本的 `monitor/frontend/e2e/process-comparison.spec.js` 与 `d7ec3e9ffac1761c60fd0a5f76dc0063a6354398` 字节一致，SHA-256 为 `4e6560c91f6a1ab2e8c72294776296a2918391f6f1817b987eca8c5f2b378954`。复核关闭环比时先挂起实际 `process_resource_summaries` 响应，断言旧四次趋势请求与 `23:50:15` 起点仍保留，释放后等待第五次请求再断言 `23:55:15`；这项同步修正已在后继完整 Linux 门禁通过。原 `35f` 失败记录保留，原 `d7ec` Platform 39952965397 已取消，不计通过，亦不重复派发已修正事项。

原始日志 `/tmp/addp-ci-manual-20261023-155348/job-120436001184.log` SHA-256 为 `e1ccc444f59ff2203f8bc8e1453fdba19ce55f36a5d216573bf8d6e14aed49e8`；同目录 `Monitor-9bf-frontend-independent-audit.json` 为回传审计，`Monitor-9bf-domain-audit.json` 为本次领域核对。核对使用原始日志哈希、`git show` 固定源码字节比较及执行项断言，没有重跑或查询／调度 CI、Hosted，也未操作个人服务或设置定时。这是受控 HTTP 下的前端 T1／T3／构建证据，不替代新摘要实源或正式身份 T4、长窗、容量及生产验收，不外推当前共享 HEAD。父 Platform 的无关 FTP 失败不在本会话修复范围，不将该 workflow 写为全绿；该次核对时个人 Safari 右侧两列截图与前台自动刷新尚未完成，后续实景结果见下一节。

### Safari 主机列表右侧列与前台自动刷新（2026-10-24）

沿用户已重启的个人环境及现有平台系统管理员会话，只读复验 `http://localhost:5170/monitor/node-resources`。最初辅助功能树仍报告页面隐藏／自动刷新暂停，原生横向滚动返回 `noWindowsAvailable`；AX Raise／窗口菜单操作未使其恢复。用户手动激活当前 Safari 标签并保持前台后，树显示焦点在当前 HTML、自动刷新已开启，原生滚动条拖动可用。该前后观察说明这次验收所需的现场条件已恢复，不能反推全部历史窗口／截图问题的根因。

已实际截图核对内存使用率与运行时长的表头和值，两列可读、百分比和复合时长单位正确，固定主机名称及查看资源入口仍保留。08:30:23（Asia/Shanghai）显示 43.20%、3 天 12 小时 21 分钟；08:31:35 显示 42.46%、3 天 12 小时 23 分钟，且自动刷新仍开启。整个前台记录区间没有点击手动刷新、重载或修改每 15 秒的选择；仅横向滚动、拖动表格滚动条及临时收起／恢复 Safari 侧栏，中间另留 18 秒一次性观察等待。08:29:19～08:31:58 的六份辅助功能文本记录多次 CPU／内存数值自然变化和时长推进，恢复原侧栏后两列仍可读，默认每页 20 台、当前一台 VM。由此确认本次单机页面前台自动更新及右侧列视觉复验通过；没有网络瀑布证据，不推断精确请求次数、重入并发或隐藏期间网络取消已在此实测。

证据目录 `/tmp/addp-safari-host-list-Su1V5L/`：`foreground-before.txt`、`scrolled.txt`、`right-actual.txt`、`right-columns-final.txt`、`automatic-after-cycle.txt`、`restored-sidebar.txt` 及对应原生 JPEG 截图；`verification.json` 保存采样时间、读数、动作与限制。`health-ready.json` 记录 Monitor HTTP 200、ready／registered、实际后端构建 `e931905933bab26701e2586eb8df9a49d9da4a30`。本次文本／状态／时长推进／观察间隔及截图格式完整性断言通过；首次记录脚本误把原生 JPEG 当作 PNG 校验而失败，已按实际格式改名并核对，未修改截图像素或产品代码。不重启个人服务、不改配置或凭据，没有 CI／Hosted 查询、调度或定时任务。此结果仅为个人单台 Desktop VM 的页面现场复验，不计一次性隔离 Hosted／T4、多真实主机、其他角色／撤权、长窗或生产 T5。


### 正式身份本地第四窗口：依赖核验通过，完整闭环未验收（2026-10-24）

用户“继续”后沿原上限新开窗口：2026-10-23 23:54:17至2026-10-24 00:54:17（Asia/Shanghai），8 CPU／12 GiB、4 GiB停止线；所有重试沿同一绝对截止，无Hosted、容量模型或定时。实际运行在截止前受控结束，清理完成。

本窗口定位并修正四项入口问题：

- Compose仍会插值未启用capacity profile中的挂载，缺少TRACE_T2_WORK使配置拒绝；仅声明当次私有工作目录，trace-load仍不选中。
- 一次性容器成功退出后自动删除，与资源守卫“列举后inspect”竞态；现在保留当次oneoff及初始化容器的退出状态至统一清理，非零退出和OOM仍拒绝，未用忽略检查或重试丢失状态的方法计成功。
- trace-query以UID 0且移除全部capability运行时，Nginx创建临时目录的chown被拒绝。真实前置nginx -t复现operation not permitted／chown；改为现有Trace T2同样的宿主私有证书所有者身份，保留cap_drop、只读和网络边界，实际校验及健康检查通过。
- Loki配置校验通过但实际启动permission denied。第8次实测进程UID10001，而可写/loki卷已变成root:root、755；初始化脚本此前成功chown10001并chmod750。空卷在创建消费者时的镜像copy-up覆盖了初始化权限。当前当次新建数据卷统一nocopy，由原初始化路径负责权限；第9次只读测量Loki /loki和Tempo /var/tempo均保持10001:10001、750，修正后实际服务不再退出。

为避免依赖启动错误在整轮产品构建后才暴露，先修规范，再将同一组独占依赖的实际启动移到编译前；同一私有Compose、网络、卷和凭据继续使用，不创建第二组或新增验收选项。依赖运行期间构建器与源码编译各1 CPU／2 GiB；业务构建后仅更新同一Compose内的实际业务镜像ID。原总上限、停止线、采样与业务断言不变。失败诊断只保留固定错误类别、输出哈希及Unix卷权限，不导出登录、SQL或运行日志正文。

当前最终代码`f07a5a6cc201a0ed676f0ad013ca5ddfcf3b23a8`。第9次标准入口`make test-monitor-traces-local`实际通过产品配置、MinIO/mc、Loki完整启动参数校验、Nginx校验、独占PostgreSQL／业务PostgreSQL／Redis／MinIO、两个存储初始化及遥测依赖检查；两代理healthy，Loki／Tempo／Alloy持续运行，报告dependencies_ready=true，并越过构建器前置检查进入编译。24项成功资源测量，峰值7.45 CPU、9.375 GiB、1,085,509,632字节，最长测量0.880秒。此为依赖准备通过，不是正式身份或全业务验收。

本窗口共九次实际尝试：1为模板配置拒绝；2为oneoff删除竞态；3为依赖启动失败；4、5为Nginx前置校验失败；6因余量不足受控中断旧顺序构建；7、8在新顺序中复现Loki权限失败；9完成依赖核验后因余量不足受控中断编译。完整入口报告均保留failed，其中两次为local_acceptance_interrupted，不改写成通过。第2、3次完成的16个产品镜像分别属于其各自固定提交，不能当作最终修复源码已完成重建。正式密码/MFA、普通Tenant执行、Trace查询、日志和Console闭环仍未运行、未计通过；没有续开新窗口。

最终`make test-infra-runtime-log-lifecycle`退出0：58项本地入口／业务契约、JS语法、CI登记、System ExecutionTrace定向Go及既有60项生命周期通过；未更改业务API或采样策略，现有Platform／owner路径继续覆盖。九次自动清理及独立审计均零当次容器、网络、卷、构建器、镜像标签和私有目录，原MinIO产品镜像保留。安全证据位于`/tmp/addp-p19-formal-local-20261023-window4/`，包括`window.json`、九份`attempt-*-report.json`、`attempt-9-mount-audit.json`、`attempt-9-planned-stop.json`、`receipt.json`、`cleanup-audit.json`、`lifecycle-final.log`；未手工调度或跟踪无关CI。原清单实际第19行（B18）、第45行（B44）继续部分实现，正式身份缺口保留，不计Linux Runner T4、Hosted、T5、72小时或生产容量通过。

### 正式身份本地第五窗口：标准数据库初始化修正，页面部署依赖仍阻断（2026-10-24）

本窗口由用户明确授权，绝对边界为2026-10-24 08:28:23～09:28:23（Asia/Shanghai），合计8 CPU／12 GiB、卷及私有可写目录4 GiB停止线。三次尝试沿同一截止时间，没有自动续期。第三次在截止前失败退出并完成清理，未运行Hosted、容量模型或定时任务。

第一次固定源码`1cf5be3660172abfc0e57b1fd9e3fb1be65e5f31`，基础依赖及七个二进制编译通过，在System产品镜像步骤返回`command_failed_2`；原输出仅保留哈希，不能据此断言具体原因。补充公共构建固定错误类别诊断后，第二次源码`8a3006670e7f6cfaddf018253515ccb37ab5bc78`的16个正式产品镜像与浏览器依赖通过，但Develop启动退出1、非OOM。其Docker标准输出为空，实际正文由runtime-log保存在当次卷中；入口随后补充只读提取固定错误类别／哈希，不导出正文。

源码核对发现隔离入口漏执行现有`scripts/infra/init-postgresql.sql`，而Develop初始化直接在`develop` schema创建导出会话表。运行观测规范与标准入口同步补齐初始化步骤：在当次独占平台库执行原SQL并使用`ON_ERROR_STOP=1`，失败即停止后续准备；不新建额外数据库、不触碰共享Infra。修正提交`2f9414696911a4701b23ce19081c2cab5f1f7925`。第三次实际通过该初始化，独立只读核对当次集群／平台库归属、Develop schema、PostGIS、pgvector及标准更新时间触发器函数均为true。随后七个二进制、16个产品镜像与浏览器依赖再次通过，System及其余五个后端按标准等待顺序就绪，已越过此前Develop启动失败。

第三次最终仍失败于`isolated_deployment_start`：Meta及Service页面容器退出1，均非OOM，诊断命中`not found`。产品配置核对确认，根Nginx对一批旧模块页面采用静态upstream，本入口为满足该依赖而启动了非本专项页面；这些页面又直接静态代理未部署的对应Backend。现有Monitor等页面已使用按请求解析。未通过假上游、增加未授权业务后端或忽略容器失败来继续；产品Nginx的部分部署行为修正需另行确认，未在本窗口实施。

三份完整入口报告均为failed，正式密码/MFA、普通Tenant执行、Trace／日志读取及真实Console闭环全部未运行、未计通过，实际运行提交核对探针亦尚未到达。标准入口准备门禁`make test-monitor-traces-local-runner`退出0（60项契约、JS语法、CI登记及System身份夹具定向Go）；最终`make test-infra-runtime-log-lifecycle`退出0，另含既有60项生命周期检查。没有改动业务API、权限、采样或保留策略，未跟踪无关CI。

全窗口578项成功资源测量，峰值硬配额7.45 CPU／10,066,329,600字节（9.375 GiB），卷及私有可写目录合计最高3,635,802,112字节，最长单次测量1.597秒，未触及停止线。三次自动清理和独立审计均为当次容器、网络、卷、构建器、镜像标签及私有目录零残留，已核验的原MinIO产品镜像保留。证据目录`/tmp/addp-p19-formal-local-20261024-window5/`含`window.json`、三份`attempt-*-report.json`、`attempt-3-database-init-audit.json`、`receipt.json`、`cleanup-audit.json`及`lifecycle-final.log`。实际清单第19行（B18）、第45行（B44）仍为部分实现；本证据不计正式身份闭环、Linux Runner T4、Hosted、72小时或生产T5通过。


### 正式身份本地第六窗口：按需页面及正式登录通过，执行闭环仍未验收（2026-10-24）

用户确认修正产品Nginx部分部署行为并新开60分钟窗口，绝对时间为2026-10-24 09:27:58～10:27:58（Asia/Shanghai），仍限8 CPU／12 GiB、当次卷及私有可写目录4 GiB停止线。三次完整尝试沿同一截止时间，未运行Hosted、容量或定时，过期没有自动续开。

提交`e25a09264e39a0c48e01af696b87df15c1b5f103`删除根Nginx七个旧模块页面的静态upstream，统一为请求时Docker DNS解析，保留去除模块前缀后的路径与查询参数；Console／Gateway仍是入口必需依赖。隔离入口只选择Console、System、Monitor页面和根Nginx，六个后端及身份、采样、业务断言保持。新增标准`make test-nginx-routes`与实际完整入口共享同一真实Nginx探针，同时接入既有Trace T2，并将nginx/加入其路径输入。最终实测11项嵌套路径、查询参数、静态资源／plugins／WASM及API路径通过，缺失模块502且已部署页面仍可读，探针清理通过。开发期两次探针失败分别涉及就绪重试stderr污染及回显服务遗漏Gateway 8000端口，均清理后修正，未当作产品或正式身份通过。

共享工作区首次完整准备门禁受另一专项未提交的Online登记组合影响，不计通过；仅保留其本专项63项契约通过事实。已提交源码e25a09264的私有副本随后实际运行标准`make test-infra-runtime-log-lifecycle`退出0（63项入口契约、JS语法、Online登记、System身份夹具Go及既有60项生命周期），构建登记与T2登记检查均退出0；私有准备源码副本已删除，没有纳入或修复并行未提交工作。

第一次实际完整入口源码`6621748b0b1a8497597e4f4ee5bb3274503402ff`：七个二进制、10个产品镜像、产品Nginx同一11项路由探针、六后端及四页面服务就绪通过，六后端实际/health/live提交均匹配该SHA。两个Platform User完成真实密码／TOTP登录、两个Tenant User完成真实密码登录；AuthContext为普通非委托User，Platform MFA身份、负例无平台诊断权限及业务User精确11项权限检查通过。随后在synthetic_engine_registration阶段以SuiteError失败，尚未创建本批成功／失败执行闭环；原报告没有HTTP状态，不把源码推断写成已实测403。

核对角色与API发现租户管理员模板不含引擎操作权限，而夹具遗漏已批准的独立准备能力。提交`6b7704611ff20856cfc03db9f4c3b6cfd1150e3a`通过既有IAM角色服务向当次准备成员显式赋予仅含system.engine.create／system.engine.execute的临时角色，业务User仍为原11项；增加正式AuthContext准备权限检查，以及只保留固定断言码或HTTP状态／稳定错误码的安全诊断。该修正代码和单测通过，但本窗口未到达修正后的运行验证，不宣称引擎登记已通过。第二次实际源码6b7704611在Security产品镜像构建失败，固定诊断命中failed to solve／timed out；未保留原始公共构建输出，不能进一步断言具体外部请求或原因，清理通过。

为在原总上限内减少闲置配额，先修规范，再由提交`1c5a5cc26dd7291f0a142a5a67adea1a908d3d2b`将编译与构建器串行：计划停止原构建器，以2 CPU／3 GiB及两个Go线程编译，完成后恢复1 CPU／2 GiB构建器；确认原ID运行且非OOM，并与采样锁同步后才清除计划停止状态。第三次实际完成七个二进制、构建器恢复及10个产品镜像，在browser_dependencies阶段达到本窗口总期限，以command_deadline_exceeded失败退出并清理。未缩短真实尾采样等待或省略任何业务／页面断言来计通过。

当前源码1c5a5cc26的`make test-infra-runtime-log-lifecycle`退出0：66项入口／业务契约、JS语法、CI登记、System ExecutionTrace定向Go及既有60项生命周期通过。源码未修改产品业务API、内置角色、采样或保留策略，不需生成Swagger；普通Tenant执行、Trace／Link与精确日志、Console闭环及退出后旧令牌拒绝本窗口均未通过。三份完整报告全部failed；正式登录的实际局部证据不能替代完整闭环，也不能外推Linux Runner T4、Hosted、72小时或生产T5。

全窗口516项成功资源测量，峰值7.8 CPU／10,468,982,784字节（9.75 GiB），卷及私有可写目录最高3,619,434,496字节，最长测量1.126秒；无OOM或资源停止线触发。三次自动清理及独立审计均为当次容器、网络、卷、构建器、镜像标签和私有源码／秘密零残留，原核验MinIO产品镜像保留。安全证据位于`/tmp/addp-p19-formal-local-20261024-window6/`，含window.json、三份attempt-*-report.json、nginx-route-gate-3.log、prepare-source.json、committed-lifecycle.log、final-lifecycle.log、t2-registration.log、receipt.json及cleanup-audit.json。仅只读查看过源码6621748的既有Release and T2自动运行40050940303，当时queued，未计通过，未手工触发工作流或跟踪无关CI。清单第19行（B18）、第45行（B44）继续部分实现。


## 服务实例容器配额：自身 cgroup 采集基础（2026-10-24，本地）

用户已确认采用部署侧只读容器身份／限制层级证明，应用只采自身用量。本批仅实现 Common 的自身 cgroup v2 采集基础：同一进程部署对象显式设置 `cgroup_v2=true` 后，Linux 在原 mTLS 端点发布当前组 CPU 累计秒数、整体内存当前字节及内核 boot／cgroup inode／PID namespace 身份。默认不读组统计，不读取其他进程／组；文件／路径／整数／响应读取量有界，采样前后核对成员路径、组 inode 和 cgroup namespace，失败不生成组零值或影响原进程 CPU／RSS／启动数据。组用量与进程用量分别采集，不计算配额利用率。

最终冻结源码：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-release-p9kt3o_j/source`，输入清单 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-release-p9kt3o_j/inputs.json`（SHA-256 `c89f711a4b14528236c568a2dcde8f5a39f5564906868d19fa5781d93c90bc8f`），交付前已复核当前任务源文件逐项匹配。

- `make test-module MODULE=common GO_ONLY=1`：通过，151 个 Go package 报告 ok；包含配置严格布尔选择、有界读取、真实零值、缺失／重复／非法／超长文件、共享组与进程用量区分、不可用时不保留旧组值。macOS T1 不代替 Linux 原生采样，亦不计为其他模块 T2。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=cgroup-source`：最终冻结版本通过。53 项准备回归、原有八类固定窗口检查及 Linux 挂载坐标测试执行通过；三个独立 SDK 进程在同一专属、非 root、private cgroup 的受限容器运行，经真实 mTLS／HTTP SD／Prometheus 读取，与独立内核 boot ID、组 inode、PID namespace 对照一致，CPU／内存原始量均有效。共享组明确不证明“一实例独立容器”或配额；未做任何比例计算或用户 API 读取。退出的 owned containers／networks／volumes／临时文件均为零，并额外核对本轮三次受控项目的容器／网络／卷零残留。
- 上一冻结版本 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/source` 的 `make test-dev-lifecycle` 串行通过：130 项单元／生命周期回归、66 项 Infra 日志生命周期回归及后续标准端口、构建身份、并发／安装锁、Owner 命令构建检查均执行至入口成功退出。`make test-build-registration` 串行通过，59 项＋9 项回归，检查 26 个 Go 服务／Worker、59 个产品镜像登记。新增 cgroup 分项的五场景矩阵、分流及预算约束已在最终版本 53 项准备回归再次执行；未因新增分项减少原有四个场景。
- 上一冻结版本还实际通过 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality`：真实进程资源／趋势／混合身份拒绝／停源隔离、真实 HTTP 查询及告警生命周期（165.71 秒）通过并零残留。随后仅对组采样增加 namespace 前后核对及分项编排，已由最终 cgroup-source 实源分项补验；不将此前完整 HTTP 结果表述为最终源码全批重跑。

保留的失败：首次并行 `test-dev-lifecycle` 有 12 项 Worker 回执超时；首次并行 `test-build-registration` 有 3 项超时 error、3 项未等到构建事件的 failure。同一 Worker 夹具单独诊断随后启动正常，两个原标准入口串行均通过，未改产品逻辑、放宽超时或删除断言；尚未证明首次超时的唯一环境根因，不能把首次执行改记通过。新增分项另一次误在共享工作区执行，仅准备回归一项旧分流字符串断言失败，在资源创建前退出；断言已随真实新分项契约同步，最终冻结入口通过，该次不计为产品 T2 通过或最终版本证据。

CI：`cgroup-source` 作为现有 Release 指标矩阵第五个真实分项，与默认 all 共用唯一标准入口、原 20 分钟 Job 和零残留要求。Common 目录会按既有规则命中 Go 消费者及产品镜像；本机未运行全量产品镜像构建、原生 Linux 全包 T1 或正式身份 Hosted。CI／Hosted 结果仍等专门会话回传，不轮询、不自行触发重跑、不设置定时任务。未更改 IAM、个人部署限额或服务状态，未扩展既有 T4 Token 用途；本批无用户 HTTP API／DTO 变化，不要求生成 Swagger。

尚未实现：部署侧完整且新鲜的容器／限制层级证明、System 私有投影精确关联、Monitor 配额查询与页面。不能将采集基础计为服务实例配额视图或容器监控闭环完成。

原始日志与指纹：
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-release-p9kt3o_j/common-t1.log`：`1e17914fa134ee70075fd36ceba70a356b7d12736d693bee65990eb7b58fbe66`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-release-p9kt3o_j/cgroup-t2.log`：`e01b40538e2751291a6aa4f1ebc40aba1a40f93f2aca84035f229fea9afe6b41`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/common-t1.log`：`6c4c5676a95e9847a5b6487ae2095d19705596aae8bdfb042cc7bf0478659f1e`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/metrics-t2.log`：`76a62821ade34329719415c648a1372d1f5ffe9d71981111c6ba5e86e849294f`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/dev-lifecycle.log`：`0607b9c8148824291c81270dbd29de06b50ce53b232b2765cfbb7fa95144fac8`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/build-registration.log`：`96d399f371844f82e7dec186994a49a1326c4bc9f078381f1e8aca8006ce3554`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/dev-lifecycle-serial.log`：`a8857c205b427f30663e7e1495ccb3782ee9b7a2594b39f1252a804b0d58d110`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/build-registration-serial.log`：`18a14e13947e6803a759306be2ca5c66310cedfa4de80b1be80d858bad2d394a`。

## 进程资源声明 240 迁移启动阻断修复（2026-10-24，本地）

本批开发中新增的 JSONB 子对象减键表达式缺少括号，现场标准启动在迁移阶段失败；已修正为先取子对象再减键，保留完整严格约束。为现场留下的 `240/dirty` 补既有 `iam-migration-repair --migration 240 --apply` 的限定案例：串行化锁内逐条校验 1—239 的文件名／摘要、原已验证 CHECK 及不可变触发器函数，仅恢复 `239/clean`；不改实例、权限、角色或 checksum，之后仍由正常 Runner 执行 240。默认不带 `--apply` 明确拒绝。

独立冻结仅含本次启动修复的八个文件，不含仍在开发验证的配额查询／界面；输入清单 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-repair-qi1nba46/inputs.json`，SHA-256 `71b05ac64be67fa6123439da503ffa0ef309b8ea80fc54719a426cf2608fedb0`。已通过：

- `make test-module MODULE=system GO_ONLY=1`：System Go T1，`system-t1.log`；不将被环境隔离的集成测试计为 T2。
- `make test-system-iam-runner`：30 项确定性入口回归，`runner-t0.log`。
- `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test process-cgroup-repair --source-root <上述冻结 source>"`：实际 `addp_iam_test`，精确复现错误 SQL 与事务回滚；拒绝 clean 状态、历史摘要变更、触发器停用和旧约束变更；只恢复失败标记后正常执行完整目录至 `240/clean`，重复启动通过，拒绝重放恢复及已记录 240 的回退。`repair-t2.log` 无条件跳过，测试拥有 Schema 在退出清理。
- `make build-iam-migration-repair` 及构建产物不带 `--apply` 的调用：构建通过，默认拒绝修改并退出 2；`build.log`、`apply-guard.log`。

现场仅做只读元数据核对：开发库 `addp` 为 `240/dirty`，checksum 恰好 239 项且全部匹配当前目录，240 无记录，原 CHECK 和不可变触发器完整。尚未获得本次个人开发库恢复授权，未执行恢复或服务重启；以上本地隔离通过不能记为现场已恢复、CI 或 Hosted 通过。修复工具的使用边界及标准重启步骤见数据库迁移故障排查的 240 专节。

随后用户明确授权此次开发库限定恢复。已执行标准构建产物 `addp-iam-migration-repair --migration 240 --apply`，退出成功并报告 `239/clean`；再以只读事务核对数据库确为 `addp`、单行状态 `239/clean`、摘要仍为 239 项且逐项匹配、无 240 摘要、原 CHECK 与不可变保护完整。现场操作日志为同一证据目录的 `现场限定恢复.log`。已告知用户在自己的终端执行 `./scripts/dev/restart.sh -all`，正常应用修正后的 240；AI 未重启服务，此处不宣称现场已达到 `240/clean`。修复已单独发布为 `954220f1688f1737dc8ffc0984aecfc98019ea4a`，不包含待验证的容器配额查询／页面。

交付前再次只读核对，现场随后已正常达到 `240/clean`：checksum 共 240 项且无差异、240 摘要存在、新严格 CHECK 及不可变保护匹配。AI 未执行额外恢复或服务重启；此项只证明迁移状态与约束生效，不代替服务就绪、容器实机或正式身份验收。

## 服务实例容器配额：部署证明、精确关联与即时视图（2026-10-24，本地）

本批接通已确认的一实例一独立 Linux 容器路线：部署侧沿现有观察进程发布有期限的只读完整层级证明，Common 不可变声明提供实际内核边界，System 精确关联当前租约、模块／角色和启动时刻，Monitor 沿当前用户授权／受限来源／预算读取四项组资源。查询后重读证明，变化则拒绝。页面在既有实例详情中分开展示进程 CPU／RSS 与整体组用量、硬上界及利用率，原因就在卡片旁；原生 Mac 不铺四张空卡片。没有完整证明／有限上界时不推算比例，不更改部署额度、应用权限、命名空间或 Docker socket，不增加告警／历史目录。

证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-container-chain-final-bxc7x1qp/`。最终执行输入为 `inputs-diagnostic.json`，SHA-256 `ed2954a8e9fa40c9034bab9817deb839ad71c62ea4e1fc3115458cb41b054544`；交付输入为 `delivery-inputs.json`，SHA-256 `3d8ed24ab1fdd789a9bcecc71e2c2a8a466d9f26bfd6f6d210b13b45a4c102a0`。二者仅观测规范文档不同：保留其他会话已发布的独立段落，并移除本批已接通链路的过时计划措辞；产品及测试源文件逐项一致。早期分层通过结果仅在相应源文件未变时复用，未把临时排错版本计为最终通过。

实际通过的标准入口与范围：

- `make test-module MODULE=common GO_ONLY=1`：151 个 Go package，`common-t1-v4.log`；包含身份格式、声明深复制、私有投影重复容器／边界拒绝、自身采集及原生平台不支持行为。对应 Common 输入与交付一致。
- `make test-module MODULE=system GO_ONLY=1`：合并迁移恢复工具与本批关联实现后的最终 T1 通过，`system-t1-combined.log`。`make test-system-iam-runner` 的最终恢复工具入口为 30 项通过，证据见上一节。
- `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package repository --test observability-identity --source-root <冻结 source>"`：真实 `addp_iam_test` 中登记／重复登记、不可变 cgroup 声明、原始 JSONB 严格约束、私有投影及既有实例筛选通过，`system-pg-t2-v6.log`；该 T2 输入中的登记、投影和 240 SQL 与交付一致，之后只增加上一节独立验证的恢复 CLI，不将此项表述为全量 IAM T2。
- `make test-module MODULE=monitor GO_ONLY=1`：最终 Monitor T1 通过，`monitor-t1-final.log`；包含固定单实例 API 契约、角色／参数拒绝、当前用户先授权、精确组标签、原完整 CPU 窗口、证明变化拒绝、无证明不出比例及 CPU／内存上界独立判定。集成条件跳过不计 T2。
- `make test-deployment-discovery`：34 项既有发现与原子发布、5 项新增宿主层级／证明代际／生产只读投射测试通过，`deployment-runner-v3.log`；父级更严上界、缺父级、非零 CPU burst、重复部署、超预算数字、来源变化及不擅自把宿主 CPU 当分配额度均覆盖。此处为受控文件／命令夹具，不能代替真实宿主完整层级证明。
- `make test-dev-lifecycle`：`dev-lifecycle-fixed.log` 完整退出 0；130 项准备、66 项 Infra 生命周期、60 项后续生命周期、端口／构建身份／并发锁／多 Worker 和四个 Go owner 的命令编译均通过。首次两项失败是截取生产启动阶段的测试未设置项目根目录及私有输入目录，已按真实阶段契约补齐，不调整产品默认值或断言；其后仅增加安全诊断函数及测试，已由下项准备回归覆盖。
- `make test-build-registration`：59＋9 项回归及 26 个 Go 服务／Worker、59 个产品镜像登记通过，`build-registration-final.log`。新文件沿既有 owner／输入发现命中，现有五场景指标矩阵保持，未另设 CI 或 Hosted 调度。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=cgroup-source`：最终 `cgroup-query-t2-diagnostic.log` 退出 0。八类固定窗口、来源 mTLS／标签控制／超量拒绝、Linux 挂载坐标及三个真实 SDK 来源均通过；新增 `TestIntegrationMetricsCgroupSources` 真实执行 57.58 秒，验证完整一分钟组 CPU、memory.current、错误 cgroup 身份拒绝，以及共享组绝不推断容器额度。标准退出与独立按项目标签核对均为容器／网络／卷零残留，临时文件清零。
- `make test-monitor-frontend`：最终 `frontend-final.log` 完整退出 0，114 项确定性测试、414 项浏览器 T3（20.4 分钟）及生产构建通过；新增有／无证明的中英文视图、单位、缺配额原因与原生实例不铺空卡片覆盖。更早的全量执行为 411／413 通过、两项导航／刷新超时；原入口最终版本全部通过，未放宽超时、加产品重试或删除断言，不能将首次失败改记成功。
- `bash scripts/swagger/gen-swagger.sh system monitor`、`bash scripts/swagger/check-route-coverage.sh system monitor`：双模块生成及 247／91 个公开路由方法覆盖通过，`swagger-v3.log`、`swagger-coverage-v3.log`；之后无 API／DTO 变化，生成文件与交付一致。

实源门禁保留两次失败记录：`cgroup-query-t2-final.log` 在并行环境的前置窗口测试累计超过既有 60 秒，未进入新增查询；串行 `cgroup-query-t2-serial.log` 八类窗口通过，但初始夹具采样在 65 秒内未满足，缺少当次目标诊断，唯一根因未查明。随后同一标准入口增加脱敏失败诊断及对应确定性测试，才执行上述最终验证；不记录原始目标 URL、OAuth 配置或 scrape 错误，最终准备回归亦通过。两次失败的项目分别为 `addp-metrics-t2-324f7394baad45dc96ae9f0b049909ce`、`addp-metrics-t2-379720025b1f4c31affdfd63682db7ee`，退出后均独立核对容器／网络／卷为零，不以未进入目标断言的执行计通过。

最终实源项目为 `addp-metrics-t2-8ca4e7a7aeff4f9f805140ee6d13dc0e`。该夹具刻意让三个进程共享同一 cgroup，证明采集／查询和拒绝边界，**不证明一实例独立容器、部署发布器在原生 Linux 上读取完整层级、System 至真实用户页面的整个配额闭环或正式身份 T4**。这些实机／正式身份验收尚未运行，当前 Mac 原生进程也没有容器配额。CI、产品镜像全包、Hosted 及生产 T5 未在本会话执行或轮询，继续由专门会话回传；未扩大已有 Hosted Token 用途，未设置定时任务。

## 自身 cgroup Linux CI 缺样：点号统计字段解析与逐源诊断（2026-10-24）

专门 CI 会话回传源码 `4108c283a183e4bfc89e76a3b38aa7cc1f6e48a8` 的 [Run 40080160036／Job 120835774116](https://github.com/pampa0629/addp/actions/runs/40080160036/job/120835774116)：RSS 三源进入发现后，`check_self_cgroup_samples` 的三条组样本断言失败。已读取原始日志 `/tmp/addp-ci-20261024-100026/job-120835774116.log` 第 796—804 行。该日志未标明缺失的具体族或来源；TLS 负例、udev 提示和 System 240 迁移恢复均不能替代本条 CI 根因／通过证据。核对 `f72ac75b1` 后原字段名解析规则仍未改变，不能声称既有功能提交已修复该失败。

独立复现了一个确定的采集缺陷：[Linux v6.11 `kernel/cgroup/rstat.c`](https://github.com/torvalds/linux/blob/v6.11/kernel/cgroup/rstat.c#L555) 在 `CONFIG_SCHED_CORE` 下输出 `core_sched.force_idle_usec`，原解析器只允许小写字母／下划线，遇到这个合法字段会丢弃整组 CPU／内存。先添加真实字段形状的回归，`make test-module MODULE=common GO_ONLY=1` 因此测试失败（其余 150 个 package 通过）；再将唯一解析路径改为有界点号分段名称。仍拒绝空段、前后点号、路径字符、重复键、非规范数字及超长内容，仍仅读取 `usage_usec`，没有忽略畸形内容或补零。修后同一入口 151 个 package 通过。

最小诊断沿现有专属 T2：各 SDK 启动记录只增加固定布尔事实（是否取得自身样本、根组文件可读、解析有效、内核是否有该字段）；每个来源按固定序号报告三项 Prometheus 样本数量和独立内核身份匹配。缺样时每来源仅一次五秒、最多 256 KiB 的原 mTLS 读取，区分源端未输出与发现／入库问题；不记录端点、原始正文、任意附加字段或凭据。原精确三源集合、独立内核身份、真实有限数值和后续完整窗口／新鲜度断言全部保留，没有新增等待以迁就缺样。

冻结源码目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-ci-fix-d0aycm9k/source`，基线 `704ecab46edeedc47da520512bc2abd083984271`，`inputs.json` SHA-256 `5a2b1e01e8d4ae476f3397d6687e5588e4073ec38b9c8ae94abfa054206ce7d3`，交付前任务文件逐项复核无漂移。证据位于同级目录：

- `common-red.log`／`common-green.log`：上述 Common 标准入口红／绿结果。
- `system-t1.log`／`monitor-t1.log`：`make test-module MODULE=system GO_ONLY=1`、`make test-module MODULE=monitor GO_ONLY=1` 均退出 0；不将环境门控的集成跳过计为 T2。
- `cgroup-t2.log`：`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=cgroup-source` 退出 0，55 项准备回归、八类窗口、mTLS／标签／超量限制、Linux 挂载坐标和新增点号字段回归通过；三个真实 SDK 的三族各一条且身份匹配，`TestIntegrationMetricsCgroupSources` 实际执行 59.34 秒通过完整组 CPU／内存、错误身份拒绝及共享组不推算配额。标准清理和独立项目标签核对均为容器／网络／卷零残留、临时文件清零，项目 `addp-metrics-t2-8efb0820d7a748e6b9789bbd909d59b1`。日志 SHA-256 `e4fadba2e2766ec6f8120d97f5deb36721208db348da18451357fc2551782ec6`。

本机 Docker Desktop 内核的三条启动诊断均为 `core_sched_field_present=false`；新增合法字段回归另在真实 Linux 专属容器执行通过。上述本机交付时尚未取得修复后 Linux CI 诊断，当时没有将旧 CI 缺样断言为唯一根因或记为通过。现有 Release 五场景矩阵及 Common／Monitor 路径与输入自动发现已覆盖本次六个文件，无新 API／DTO、权限、迁移、配置或前端变化；未操作个人服务、keepalive 或数据卷。

2026-10-24 专门 CI 会话回传后，已独立核对固定修复源码 `1f9b44df83d48a78b96fbe4b3d91e447624b8518` 的 [Release and T2 gates／Run 40085914201／Job 120853412269](https://github.com/pampa0629/addp/actions/runs/40085914201/job/120853412269)，事件为 push、attempt 1。checkout 日志与 SHA 一致，实际标准入口为 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=cgroup-source`。原始日志 `/tmp/addp-ci-20261024-120030/job-120853412269.log` SHA-256 `4a47c59cfef55b7b7f603959439e9408de61893f6eb240652c94656ce4638a3d` 与独立审计 `/tmp/addp-ci-20261024-120030/Monitor-1f9-cgroup-independent-audit.json` 一致。第 791—803 行显示挂载坐标／点号字段回归通过，三个 SDK 均报告 `core_sched_field_present=true`、解析及自身样本有效，三项 Prometheus 族各一条且匹配独立读取的内核身份；真实完整分钟 CPU／内存查询和错误身份拒绝用例实际运行 58.68 秒通过。第 808 行确认本轮容器、网络、卷及临时文件零残留，后续作业摘要为 success。

因此，本条固定版本 Linux cgroup-source T2 已通过，原旧版本失败及本机证据继续分别保留；不再因同一缺样重复验收。该结果仅证明三个 SDK 的共享组原始量采集、完整窗口读取与身份拒绝，**不证明独立容器关联、完整限制层级、正式身份配额页面或当前 main／告警级别筛选的 CI**。本次只补录回传证据，没有重跑 CI／Hosted、设置定时任务或操作用户服务；同一证据已由协调会话通知清单维护会话，不重复发送。


### 正式身份本地第七窗口：准备守卫修正及IAM权限冲突，闭环未通过（2026-10-24）

用户授权窗口为2026-10-24 16:32:47–17:32:47（Asia/Shanghai），准备、构建、等待与重试共用该截止时间；最多8 CPU／12 GiB、所有当次卷及私有可写目录达到4 GiB停止。仅运行根入口`make test-monitor-traces-local`，不运行Hosted、容量或定时，不接管个人开发服务或共享数据库。实际8次尝试全部失败，正式登录→执行→Trace查询→日志→页面闭环仍未通过；第六窗口的登录局部证据不能替代本窗口或完整闭环。

本次修正：`b5b543863501d371d851a9dd69601f55875677f3`在依赖启动前准备全部当次Compose空卷和构建器，保留独占所有权／Compose标签；`5a1a4c0330809c836edb13e08ec4b1e3b578b1a8`把受0.25 CPU／64 MiB及30秒准入预算限制的只读卷辅助容器准备与首次完整测量分开，之后才启动依赖。周期四秒检查预算、最长五秒间隔、CPU／内存／磁盘上限、OOM及测量失败即停止均保持。`4108c283a183e4bfc89e76a3b38aa7cc1f6e48a8`补充首次资源检查的固定操作／耗时诊断；`d9a3cec22e12db098182e25bc96a62e0916f5f32`细分依赖初始化阶段及固定公共诊断标记，不保存凭据、SQL或原始失败正文。

每次实际结果（同一窗口，无自动续期）：

| 尝试 | 实际源码提交 | 停止阶段／原因 | 成功资源测量次数 |
| --- | --- | --- | --- |
| 1 | `c93da8151` | `isolated_dependencies_start`；`prepare_volume_sampler`超过资源检查预算 | 2 |
| 2 | `c93da8151` | 同上，未计通过 | 2 |
| 3 | `b5b543863` | `product_compile`；`measure_owned_volumes`超时，依赖已就绪 | 71 |
| 4 | `cdd038f22` | `bounded_builder`；`command_deadline_exceeded`，尚无完整资源样本；旧报告不能进一步定位 | 0 |
| 5 | `3a5cd4c54` | 同上，尚无完整资源样本 | 0 |
| 6 | `5a1a4c033` | `initial_resource_sample`；`command_deadline_exceeded` | 0 |
| 7 | `4108c283a` | `isolated_dependencies_start`；退出2，固定标记为空，未能定位具体准备命令或唯一根因 | 6 |
| 8 | `d9a3cec22` | `formal_identity_preparation`；退出1，固定标记为空；未到达真实登录及业务执行 | 253 |

第8次完成7个源码二进制、10个实际产品镜像、浏览器依赖、同一产品Nginx的11项路径／参数断言、缺席模块502及已部署模块仍可读检查；六个Backend均经`/health/live`核验实际提交为`d9a3cec22e12db098182e25bc96a62e0916f5f32`。正式身份准备失败后未继续业务；没有本次正式登录、成功／受控失败父子execution、真实检查点、自然尾采样、精确Trace／Link／日志、Console页面或旧会话拒绝的通过证据。

代码调查确认一项必须先讨论的权限冲突：此前`6b7704611ff20856cfc03db9f4c3b6cfd1150e3a`用`local.execution_trace_preparation`自定义角色授予`system.engine.create/execute`，但当前`system/authorization/permissions.yaml`及迁移目录中两者均不可租户定制；`TenantRoleService.CreateRole`必经`validateCustomizablePermissions`拒绝该方案。这是此前夹具设计错误，原精确权限单测未覆盖真实目录约束；现有运行报告尚不能证明它是第8次退出1的唯一原因。没有修改权限目录、制造AAL2、直接写授权表或擅自授予更大角色。建议讨论仅当次准备User通过正式密码／TOTP、真实增强认证及角色分配取得现有`tenant.infrastructure_administrator`；其11项权限超出此前准备身份两项初值，尚未确认或实施，普通执行User仍保持原11项权限。完整重跑需要另行确认新窗口。

当前最终代码的`make test-infra-runtime-log-lifecycle`通过70项本地入口契约、60项生命周期测试、Node语法、Online登记及System fixture Go检查；日志为`/tmp/addp-p19-formal-local-20261024-window7/dependency-diagnostics-lifecycle.log`。此前67／68／69项中间版本及各60项生命周期门禁分别记录，不代替最终运行验收；现有标准入口及CI登记继续覆盖本次脚本，未新增Hosted触发或定时。

原始脱敏报告为`/tmp/addp-p19-formal-local-20261024-window7/attempt-{1..8}-report.json`，同目录`window.json`、各`attempt-*-source.json`／日志、`receipt.json`及`cleanup-audit.json`保留窗口及源码身份。独立只读清理核对于17:22:07完成：8次均为当次容器、网络、卷、构建器容器／状态卷、镜像标签及私有目录零残留，复用前已有MinIO不可变镜像仍保留。334次成功资源样本的硬配额峰值7.8 CPU／10468982784字节（9.75 GiB），卷及私有目录实际分配峰值3637747712字节（约3.39 GiB），成功单次测量最长2.640秒；超时失败样本单独按失败记录，不从成功最大值中隐去。没有观察到OOM或4 GiB停止线触发；资源守卫超时及身份准备失败不得计为通过。


## 平台告警级别组合筛选（2026-10-24，本地）

对应原功能表 Excel 第 51 行、B 列 50“告警信息管理”。事件查询新增现有 `critical|warning` 精确筛选，与类别、状态和主体取交集，在计数及分页前执行；省略／空值读取全部级别，排序、事件快照、权限和生命周期不变。未知级别、重复／未知参数及编码错误返回 400。Monitor 页面选择级别回到首页，刷新、重载和浏览器返回保留条件；Console 同一 iframe 内同步级别地址，切换通知页保留筛选。未实现名称／时间组合检索、高等级置顶、依赖拓扑、影响范围或根因分析。

源码为基线 `e814fed8d` 加本节同批任务改动；19 项任务输入摘要见 `/tmp/addp-monitor-severity-inputs.json`，收据 SHA-256 为 `da396ec8485ef25e3582eafbf4e1456756d06915d31031847be8c4f1d73c2f33`。与本批无关的工作区文件未纳入输入或提交。

| 标准命令 | 实际结果与范围 | 本机证据 |
| --- | --- | --- |
| `make test-module MODULE=monitor GO_ONLY=1` | 7 个 Go 测试包通过；含平台 User 原权限守卫及级别／重复／未知查询拒绝；不计为完整模块 T0–T3 | `/tmp/addp-monitor-severity-go.log` |
| `make test-monitor-postgres` | 35 项 PostgreSQL T2 顶层用例通过，无跳过；本地先经 Infra status 核对 `25432`，只使用 `addp_test`。新增级别用例覆盖两级交错数据、计数／分页顺序、类别／状态／主体交集、空结果及 SQL 字面拒绝；当次独有主体清理后核对零行 | `/tmp/addp-monitor-severity-postgres.log` |
| `make test-monitor-frontend` | 115 项 T1、417 项隔离浏览器 T3 及 Vite 构建通过；新增中英文级别选择、页码重置、刷新／返回／重载、无效条件不扩查和失败清空均包含于本轮整套通过结果 | `/tmp/addp-monitor-severity-frontend-full.log` |
| `make test-console-frontend`；`npm --prefix console/frontend run test:e2e -- e2e/platform-alert-navigation.spec.js` | Console 195 项 T1、219 项 T3 及构建通过；随后同一测试文件增加级别同步断言，最终 5 项专项再次通过，验证同一 iframe 文档、真实模块路由和通知页重载，产品输入未再变化 | `/tmp/addp-monitor-severity-console.log`、`/tmp/addp-monitor-severity-console-navigation-final.log` |
| `bash scripts/swagger/gen-swagger.sh monitor`；`bash scripts/swagger/check-route-coverage.sh monitor` | 生成成功，91 个公开路由方法覆盖一致；新增级别参数与枚举的双语文档 | `/tmp/addp-monitor-severity-swagger.log`、`/tmp/addp-monitor-severity-coverage.log` |
| `make test-frontend-ci-registration` | 20 项登记回归及 21 个前端登记核对通过；现有 Monitor／Console 前端与 Monitor PostgreSQL workflow 已覆盖本批路径，无新增入口／依赖或编排 | `/tmp/addp-monitor-severity-ci-registration.log` |

最初浏览器专项因测试使用错误中文标签、随后点击 Element Plus 内部输入被选中标签遮挡而失败；修订测试定位到既有可操作下拉框外层，3 项专项和最终整套 417 项均通过，原失败日志仍保留为 `/tmp/addp-monitor-severity-browser.log`、`/tmp/addp-monitor-severity-new-interactions.log`，不计通过。不改变共享组件或以强制点击绕过交互。

本批 T3 使用受控 API 返回，不代表正式身份、个人 Safari 或 Hosted 验收；没有重启个人服务、修改 IAM／个人业务库、设置定时任务或主动跟踪 CI。CI／Hosted 结果继续等专门会话回传。


## 平台告警发现时间范围查询（2026-10-24，本地）

对应原表 Excel 第 51 行、B 列 50“告警信息管理”。沿已有 `opened_at` 发现时间和 System 时间边界口径新增 `time_from/time_to` RFC3339 查询：起始含、结束不含，支持单边界，空边界不限，双边界须开始早于结束；UTC 公元 1—9999 年。与类别／状态／主体／严重级别取交集，在计数和分页前完成，排序不变；不按恢复或最近观测时间改判。服务使用唯一 `PlatformAlertFilter`，删除原位置参数调用，不保留第二条查询路径。无新权限、迁移、依赖、配置或后台任务。

页面复用 Element Plus 日期时间控件及共享时间规范化，本地时间输入、UTC 参数提交。查询才应用草稿并回首页；刷新不读取未提交草稿，清除时间保留其他筛选；单边界分页、重载／返回及 Console 同一 iframe／通知页恢复已提交时间。错误、撤权和迟到响应沿原清理链。非法／重复时间、非法年份、相等／反向区间拒绝，不删条件扩大查询。未增加预设时段、恢复时间查询、名称搜索、高等级置顶或根因分析。

任务输入基线 `25c5aab9165f582556ead36f8d5f4bb75118dfb5`，20 项任务文件收据 `/tmp/addp-alert-time-inputs.json` SHA-256 `393c3e979cf80567a63372d642bb283c6b097ba5ee06bc6d344e6df7bc34e891`。验证后任务产品／测试输入未漂移；最后只修订规范中年份范围的文字。验证期间已核对 Common、Common Frontend、Monitor、Console、System Frontend、根 Makefile 和相关 runner 无额外 HEAD 变化；其他会话改动不纳入本批结论。

| 标准入口 | 实际结果与范围 | 本机原始证据 |
| --- | --- | --- |
| `make test-module MODULE=monitor GO_ONLY=1` | 7 个 Go 测试包通过，仅计 Go T1；含时间参数及沿用权限守卫，合法单边界／时区／小数秒，非法日期／年份／无时区／相等／反向／重复参数拒绝 | `/tmp/addp-alert-time-go-final.log` |
| `make test-monitor-postgres` | 36 项顶层 PostgreSQL T2 用例通过，无跳过；新时间用例含 7 项子场景，覆盖微秒边界、带偏移的同一时刻、已恢复告警按打开时间、单边界／不限、交集与多页计数、显式公元一年上界不误作不限及无效范围拒绝。当次独有主体清理核对零行；先经 Infra status 确认实际映射 25432，仅用 `addp_test` | `/tmp/addp-alert-time-postgres.log`，SHA-256 `a7d76fd7062b30d53184f06c8b6a56d232dcff5c9f3962e27d9a526e783e651c` |
| `make test-monitor-frontend` | 116 项 T1、421 项隔离浏览器 T3 及构建通过；4 项新增时间交互纳入整套，含中英文／680px 窄屏、草稿刷新隔离、边界、单边界翻页、重载／返回／清除、错误与迟到响应 | `/tmp/addp-alert-time-monitor-frontend.log`，SHA-256 `2762557de9d29540fb2848432b4f5f7507f7f32631df31759083f587e50ae313` |
| `make test-console-frontend` | 195 项 T1、219 项 T3 及构建通过；真实 Monitor 模块页在同一 iframe 内同步／重载时间与级别，通知页往返恢复参数 | `/tmp/addp-alert-time-console-frontend.log`，SHA-256 `85507dd3414396562339f1bba0a722ea143e05e90da844470ccf92d24baca482` |
| `bash scripts/swagger/gen-swagger.sh monitor`；`bash scripts/swagger/check-route-coverage.sh monitor` | 生成成功、91 个公开路由方法覆盖一致，两个时间参数双语文档同步 | `/tmp/addp-alert-time-swagger.log`、`/tmp/addp-alert-time-swagger-coverage.log` |

首次浏览器专项有两项因测试将共享中文占位“起始时间”误写为“开始时间”而失败；按实际共享译名修订后，四项专项及上述整套最终通过。原失败 `/tmp/addp-alert-time-browser.log` 保留，不计通过；后续专项 `/tmp/addp-alert-time-browser-green.log` 只作为中间证据，最终草稿／翻页增强以整套 421 项结果为准。

现有 owner／路径自动发现、PostgreSQL `TestIntegrationPostgres` 与前端标准入口已实际命中新用例，不新增 Makefile／workflow。T3 使用受控 API，不代替正式身份、个人 Safari 或 Hosted；没有重启个人服务、修改个人业务库或 IAM，没有查询／重跑 CI／Hosted或设置定时任务，相关结果继续等专门会话回传。


### 发现时间查询：个人 Safari 重启后只读复验（2026-10-24）

用户完成重启后，`curl --silent --show-error --max-time 10 -w '\nHTTP %{http_code}\n' http://localhost:8100/health/ready` 返回 HTTP 200，Monitor 已登记、本地依赖与 System 登记均 ready。运行源码为 `f651ad550fa41a942d40dccbf782ae4d02c51f10`，已用 `git merge-base --is-ancestor` 确认包含本功能提交 `ea320ab686f9fe40c36ee628284e4de0049ae547`。

在个人 Safari 现有平台系统管理员会话，通过真实 Console `/monitor/platform-alerts` 只读复验：截图可见发现时间控件和就近说明；结束时间单边界可查到现有告警；相等时间草稿提示无效并禁用搜索；查询 10 月 24 日至 25 日后现有 10 月 23 日告警被排除，整页刷新后日期与空结果保留；扩大起始日期至 23 日后该告警重新显示；组合警告级别后，清除时间保留 `severity=warning` 及现有告警。日期框使用本地时间，URL 使用对应 UTC。未创建、确认、抑制或删除任何告警。

操作记录 `/tmp/addp-alert-time-safari-result.json`，最终 AX 状态 `/tmp/addp-alert-time-safari-state.txt`，截图 `/tmp/addp-alert-time-safari.png`，摘要内记录两份文件的 SHA-256。当前只有一条现有告警，本次不证明多页或微秒边界；这些范围仍分别依赖上文 PostgreSQL T2／受控浏览器 T3。个人 Safari 结果不升级为正式隔离身份 T4、Hosted 或生产证据；CI／Hosted 继续等专门会话回传，本次未查询或触发，也未设置定时任务。


### 正式身份本地第八窗口：真实MFA与准备角色通过，成功查询被保护链拒绝（2026-10-24）

用户确认准备User仅在当次隔离环境，经真实密码／TOTP与正式IAM API取得既有`tenant.infrastructure_administrator`；普通执行User的原11项权限保持。先修规范和入口，`558cb372659a9b863d14ae78fdbe7ba1014870bf`删除不可租户定制的自定义准备角色，夹具不预授准备角色、不制造会话或AAL2。实际入口核对内置角色当前11项权限及同一Membership，自助TOTP登记替换Cookie／Access Token、验证旧令牌401；真实AAL2和有效step-up后分配限于窗口期限的角色，再经正式Refresh重读同一身份及准确授权。普通User权限在准备后再次核对。

本次唯一运行窗口为2026-10-24 20:55:05–21:55:05（Asia/Shanghai），8 CPU／12 GiB、卷和私有可写目录4 GiB停止线保持。每次运行均为根入口`make test-monitor-traces-local`、当前已提交源码私有副本、产品构建与原六后端／三页面，不复用旧产品镜像，只有固定MinIO／mc镜像按既有规则核验复用。不运行Hosted、容量或定时，不接管个人开发服务／共享数据库。三次尝试都失败，完整执行→Trace→日志→页面闭环未通过。

| 尝试 | 实际源码 | 结果与实际覆盖 |
| --- | --- | --- |
| 1 | `558cb372659a9b863d14ae78fdbe7ba1014870bf` | 7个源码二进制及依赖准备通过；`product_image_gateway`退出2，固定标记`failed to solve`／`not found`。缺少足以确定唯一根因的正文诊断，不能认定已修复镜像故障。未进入身份阶段。 |
| 2 | `25c5aab9165f582556ead36f8d5f4bb75118dfb5` | 7个二进制、10个产品镜像、浏览器依赖、11项产品Nginx路径／参数断言、缺席模块502及已有模块可读通过；六后端运行提交均匹配该源码。真实平台密码／MFA／AuthContext、普通User最小权限、准备User真实MFA／内置角色、两次旧Token拒绝、合成引擎注册与连接测试通过。成功查询编排终态不匹配，未保存进一步错误码，不计执行通过。 |
| 3 | `b57e481009c83fb200cd0ccd986937802d6c363e` | 同样完成源码／产品／实际版本／Nginx及上述五项身份和引擎断言。复现成功查询失败：父执行为`orchestrator.execution.child_failed`，Develop子执行为`develop.query.protection_required`。已实际走通正式User提交与Orchestrator派发到Develop子执行；实际子执行失败，不能算成功查询通过。 |

第3次的父执行UUID为`9fad5ff2-f121-4f67-9e81-e1c64293b7ac`，子执行UUID为`de10928e-f672-41f5-8780-6b8e0b3a5457`，仅保留身份引用、状态、步骤与稳定码。`b57e48100`补充这条失败诊断，不输出错误正文、SQL、参数或结果，原终态断言未放宽。普通User准备后仍保持原11项权限；没有为查询失败扩大权限、增加源Grant或绕过保护。由于成功查询断言已失败，未继续受控失败场景、真实检查点／自然尾采样、精确Trace／Link／日志读取、平台Trace／日志无权限与最终退出负例、真实页面闭环；浏览器接收端未观察到页面回执，不计页面通过。

代码和规范调查另确认入口漏了既有的新Tenant保护初始化前置：`docs/spec/addp数据安全与隐私保护实现规范.md`明确实例就绪不等于新Tenant已完成Owner首轮投影；Develop以30秒周期消费实际Security投影。本地夹具原来没有等待这项状态。窗口结束后，仅做代码与不启动环境的门禁：`8c2a83abe`复用既有`waitForOwnerProtection`，在导出凭据前只读等待当次Tenant、固定Develop的持久初始化checkpoint，最长60秒，仍受身份准备／总窗口预算；不开放可选Owner参数，不直接写投影／checkpoint，不把业务拒绝重试成成功。标准入口同时覆盖已有`ProtectionReadiness`单测，并补固定Owner／精确Tenant／导出顺序约束；失败报告只保留Owner已有类型化`stage/reason`，不保存SQL上下文。该准备缺口已修代码，但尚未运行验证，不能认定是实际保护拒绝的唯一根因，也不能宣称闭环已修复。

标准代码门禁分层记录：初次共享工作区的`make test-monitor-traces-local-runner`在本专项73项通过后，被无关未提交拓扑登记阻断，不计整入口通过；仓库外冻结已提交main与本次代码后，`make test-infra-runtime-log-lifecycle`的73项入口、60项生命周期、Node语法／登记及System fixture Go检查通过，冻结源码随后删除。执行诊断版本同入口74＋60通过。最终`8c2a83abe`输入在共享工作区运行同一标准入口75＋60通过，Go范围为`ExecutionTrace|ProtectionReadiness`；其余无关工作区改动未提交本专项。既有CI输入与owner登记命中，未新增workflow或Hosted调度；本次未轮询／宣称CI通过。公开产品API未变，无Swagger变更。

证据目录`/tmp/addp-p19-formal-local-20261024-window8/`：`window.json`、`attempt-{1..3}-source.json`／报告／日志、`preparation-evidence.json`／`preparation-lifecycle.log`、`execution-diagnostics-lifecycle.log`、`readiness-final-lifecycle.log`、`receipt.json`及`cleanup-audit.json`。另有初始准备冻结的输入清单／原日志，路径见`preparation-evidence.json`及`/tmp/addp-p19-mfa-preparation-path`，只保留脱敏文件，源码副本已移除。

21:52:18独立只读核对三次全部当次容器、网络、卷、构建器容器／状态卷、镜像标签及私有目录零残留，复用前已有MinIO镜像保留；页面回执读取器亦已退出。684次成功资源测量的合计硬配额峰值7.8 CPU／10468982784字节（9.75 GiB），卷及私有目录实际分配峰值3649245184字节（约3.40 GiB），成功测量最长2.769秒；没有OOM、资源采样失败或磁盘停止线触发。窗口结束后没有新增真实运行。当前状态仍部分实现；下一次完整运行必须另行确认本地窗口，重点先验证当次Owner初始化，再验证成功／受控失败查询与Trace／日志／页面，不能把本次MFA及引擎准备通过替代完整闭环。


### 正式身份本地第九窗口：构建失败、资源守卫停止与源码准入阻断（2026-10-24）

用户另行授权22:48:15–23:48:15（Asia/Shanghai）的本地窗口，8 CPU／12 GiB、当次卷及私有目录4 GiB停止线保持；不运行Hosted、容量或定时，不接管个人服务及共享数据库。原保护就绪修正`8c2a83abe`已在本次实际重建源码中，但三次尝试均未进入正式身份阶段，不能宣称当次Tenant初始化或执行闭环已运行通过。

| 尝试 | 实际源码 | 结果与实际覆盖 |
| --- | --- | --- |
| 1 | `752da6994bd72001d3cb08a7e67303f51e7830b2` | 独占依赖准备及7个源码二进制编译通过；`product_image_gateway`退出2，仍仅有`failed to solve`／`not found`固定标记，未取得可确定唯一构建根因的证据。 |
| 2 | `0569a8c72ff4060b74a1de93ce9aec2feddbd062` | 独占依赖准备通过；`product_compile`期间卷测量耗时4.105秒，超过原4秒单次预算，`resource_sample_deadline_exceeded`停止。没有放宽守卫、借用旧业务镜像或将终止算通过。 |
| 3 | `abeb0272e916a325898724c5135108a2e0a1286f` | `source_snapshot`以`commit_all_local_inputs_before_acceptance`拒绝，尚未启动环境。共享工作区`system/backend/go.mod`、`go.sum`有其他会话未提交依赖修改；保留这些改动，不代为提交、不跳过私有Owner输入一致性检查。 |

`0569a8c72`仅为原入口补充固定脱敏构建分类，包括缓存键／校验和、镜像内容摘要、匿名令牌／HEAD请求等；不导出任意错误正文、路径、SQL或秘密，不改变产品构建、资源上限及验收口径。最小充分标准门禁`make test-monitor-traces-local-runner`通过76项入口检查、Node语法、CI登记及System fixture Go检查（`ExecutionTrace|ProtectionReadiness`）。未修改Makefile或workflow，现有路径登记实际命中。先用3类含私密哨兵的样本确认分类缺失，再补实现通过；该诊断增强不代表实际Gateway构建故障已修复。只读`docker manifest inspect --insecure localhost:5001/debian-slim:bookworm`当前返回0及OCI清单，只证明当时清单可读，不证明失败时的BuildKit路径、内容或网络正常。

三次运行均走`make test-monitor-traces-local`及当次剩余绝对时间，不另开或重置60分钟。报告与独立审计确认每次当次容器、网络、卷、构建器容器／状态卷、镜像标签及私有源码目录零残留；既有MinIO镜像保持原不可变ID。181次成功测量的合计硬配额峰值7.45 CPU／8992587776字节（8.375 GiB），卷及私有目录实际分配峰值3608891392字节（约3.36 GiB），成功测量最长2.547秒；第二次失败测量另列4.105秒，不能混入成功峰值掩盖失败。没有OOM或4 GiB磁盘停止线触发。

证据目录`/tmp/addp-p19-formal-local-20261024-window9/`：`window.json`、`attempt-{1..3}-source.json`／报告／日志、`build-diagnostics-runner.log`／`build-diagnostics-inputs.json`、`source-admission-conflicts.json`、`gateway-base-manifest-readonly.json`、`function-row-mapping.json`、`receipt.json`及`cleanup-audit.json`。只保留脱敏证据；页面回执接收端在第1次结束后退出，未观察到页面回执。

截至本次记录，准备角色／真实MFA／普通User权限、成功／受控失败查询、真实阶段检查点、自然尾采样、精确Trace／Link／日志、平台无权限及正式退出负例、真实Console页面均未在本窗口运行，不得沿用第八窗口的局部通过冒充本次通过。功能清单第19行（B18，接口管理总述）、第45行（B44，实时耗时接口追踪）仍为部分实现；已向维护会话发送阶段回报，M列未据此升级。下一步先恢复已提交源码准入并取得实际构建失败分类，再补完整正式身份闭环。


### System依赖准入恢复与Gateway单项构建复现（2026-10-24）

用户要求先核对共享System依赖、恢复准入并调查Gateway构建，再在条件具备后另开原上限60分钟正式身份本地窗口。首次核对时`system/backend/go.mod`／`go.sum`仍有其他会话未提交的Avro间接依赖；未覆盖或代为提交。随后该依赖随`be747a4a2d914d67696a675b1ceea0cac732b087`落到main，两个文件干净，直接复用标准`committed_inputs`函数逐项核对通过，保留准确输入哈希。共享准入阻断已解除，但这项只读结果不代替环境运行验收。

独立调查限定2026-10-24 23:29:55–23:49:55（Asia/Shanghai）最多20分钟，仍守8 CPU／12 GiB、4 GiB停止线，第二次沿原绝对截止时间计算剩余预算，不续时。本次只从已提交源码经根`make build`编译System／Gateway／runtime-log，再用`make build-images IMAGE_BUILD_ARGS="--verify --jobs 1 --services <owner> ..."`复现原System镜像→当次BuildKit全缓存清理→Gateway镜像顺序；复用既有隔离构建器／只读卷测量／资源守卫及退出清理，不启动业务后端、数据库、身份或页面，不计完整`make test-monitor-traces-local`通过。一次性诊断脚本和报告仅在系统临时目录，不新增仓库公共验收入口。

| 单项尝试 | 实际源码 | 结果 |
| --- | --- | --- |
| 1 | `133d9c0121630fb7f0a987e74f092c667721b431` | `product_compile`期间完整测量4.059秒超过原4秒预算，守卫停止；未进入镜像构建，不计Gateway失败重现或编译通过。 |
| 2 | `be747a4a2d914d67696a675b1ceea0cac732b087` | 3个源码二进制均重新编译，System产品镜像、当次缓存清理及Gateway产品镜像全部通过；23:42:27结束。System镜像ID为`sha256:aaf46c29ca32cfad6782cdc55197f2671300b0875e3a780c3eeb197a80a787f6`，Gateway为`sha256:01b85677d78e85902c1aebf0e7adb8131e8ae882f567c13b2b7bf518f708e7de`，当次标签与资源随后全部删除。 |

两次共有153次成功资源测量，合计硬配额峰值2.25 CPU／3288334336字节（3.0625 GiB），卷及私有目录实际分配峰值2579521536字节（约2.40 GiB），成功测量最长3.345秒；第一次失败的4.059秒另列。没有OOM／4 GiB停止线触发；标准清理与独立审计均确认当次容器、网络、卷、构建器容器／状态卷、镜像标签和私有源码目录零残留，既有MinIO镜像保持。诊断第2次记录只读测量分步骤耗时，不改变四秒预算；空闲时3次本专项容器列表读取约0.029–0.032秒，仅作当时参考，不推定编译时测量超时的唯一根因。

只读读取Gateway基础镜像`localhost:5001/debian-slim:bookworm`的OCI清单、配置和全部层：配置468字节、压缩层28117255字节的HEAD／完整GET与清单大小及SHA-256均匹配。只证明检查当时仓库内容完整，不证明过去失败时的BuildKit缓存、导出或网络状态。历史窗口只保留`failed to solve`／`not found`及哈希，已清理原始错误；本次相同构建顺序成功，不能确定历史唯一根因，也不宣称修复了Gateway间歇构建故障。

为避免再丢失原因，`20a9efcc6e49743bd3846b3caafa7e8ab87b1b5c`给同一标准本地入口的`product_image_*`失败补封闭诊断：仅固定原因／失败步骤、已知产物输入名、Dockerfile行号、最多8项HTTP状态／进程退出码，未知原因明确未分类；不保存任意正文、URL、内容摘要值、凭据或SQL，不对身份／业务命令开启该分类。规范同步同次完成，原构建路线、资源及验收断言不变。先以缺失函数验证测试失败，再实现；`make test-monitor-traces-local-runner`通过79项、Node语法、CI登记及System fixture Go检查（`ExecutionTrace|ProtectionReadiness`），现有路径自动发现命中，未新增Makefile／workflow。成功复现发生在该诊断提交前，准确源码如表；这项纯失败报告变化不改变成功构建路径，也不把单测当成实际失败重现。

证据目录`/tmp/addp-p19-gateway-investigation-20261024/`：`window.json`、`attempt-{1,2}-report.json`／日志、`current-source-admission.json`、`base-blobs.json`／`base-blob-integrity.json`、`idle-daemon-readonly-timing.json`、`structured-diagnostics-runner.log`／`structured-diagnostics-inputs.json`、`receipt.json`及`cleanup-audit.json`。正式60分钟窗口尚未启动：当前构建已通过，但历史原因未定，已向用户确认是否接受这一前提作为新窗口准入。正式身份执行→Trace→日志→真实页面仍待运行，不运行Hosted、不重跑容量、不设置定时；功能清单第19、45行保持部分实现。


## 平台告警名称检索与触发时名称证据（2026-10-24—25，本地）

对应已只读核对的 Excel 第51行、B列50“告警信息管理”。只读调查确认平台 incident 原来没有名称，规则有当前 `name`，但旧 JSON 证据只存规则 ID／版本和采样事实；页面“异常原因”是指标／信号译名，Tenant 的 `rule_name` 属于独立领域。用户确认采用首次触发名称、缺失历史不补造后，先修订运行观测规范7.1，再将规则名称保存到已有 resource／HTTP／process JSON 证据的 `rule_name`。后续评估／恢复沿 opening 证据，不用改名后的当前规则补写，旧记录仍缺失；日志名称按固定 signal 目录与当前请求语言派生。无新表、数据库列、DDL、权限、角色或通知协议。

唯一 `GET /platform/alerts` 增加 `search`，列表只读 `name` 与查询使用同一事实源。支持最多100个Unicode字符、去首尾空白、大小写不敏感的字面包含，百分号／下划线／反斜线不作通配符；非法UTF-8、控制字符、超长和重复参数拒绝。名称与类别／状态／主体／级别／发现时间取交集，先过滤再计数／分页，保留原排序。名称列与异常原因分开，旧规则告警显示“未记录名称”；名称草稿提交／清除回首页，刷新／重载／通知往返恢复已提交条件，错误与迟到响应沿原清理机制。

| 标准入口 | 实际结果与覆盖 | 本机证据 |
| --- | --- | --- |
| `make test-module MODULE=monitor GO_ONLY=1` | 最终7个Go测试包通过，仅计Go T1；API参数及原身份守卫、名称目录与缺失历史处理 | `/tmp/addp-alert-name-go-verified.log` |
| `make test-monitor-postgres` | 最终38项顶层T2通过，无跳过；其中名称查询16子场景覆盖Unicode／大小写／字面通配符／注入文本／中英日志及接收器后缀／多页计数与时间、级别、状态交集。三个真实评估器的触发／改名／恢复名称保持；删除规则后历史仍可检索；模拟旧证据缺少名称时，当前规则及后续恢复不回填。新查询夹具清理核对零行。仅用已核实25432端口的 `addp_test`，未触及个人业务库 | `/tmp/addp-alert-name-postgres-final.log`，SHA-256 `1f18856d031502aef2aab863a2fc166f273eb3a45c01b6eba406f6e274ea4904` |
| `make test-monitor-frontend` | 最终117项T1、424项受控浏览器T3和Vite构建通过；含中英文、680px窄屏、逐字键入、字面名称检索、页码重置、多页／重载／通知往返、缺失名称提示、非法URL拒绝和失败清空 | `/tmp/addp-alert-name-frontend-final.log`，SHA-256 `1e3bffb892e98c99befc8d3f1489741dedee269a870205b303778e4b3611e6b7` |
| `make test-console-frontend`；Console目录下 `node ../../scripts/test/frontend-browser-gate.mjs e2e/platform-alert-navigation.spec.js` | Console195项T1、219项T3及构建先通过；随后名称输入草稿修复不改Console文件，最终源码再通过5项真实iframe导航专项，覆盖名称／级别／时间条件及同一iframe重载恢复。不将先前整套冒充修复后整套重跑 | `/tmp/addp-alert-name-console.log`、`/tmp/addp-alert-name-console-navigation-final.log` |
| `bash scripts/swagger/gen-swagger.sh monitor`；`bash scripts/swagger/check-route-coverage.sh monitor` | 生成成功、91条公开路由方法覆盖一致，名称query与列表投影、JSON证据同步 | `/tmp/addp-alert-name-swagger.log`、`/tmp/addp-alert-name-swagger-coverage.log` |

首轮Go构建因两处旧测试夹具返回类型未同步而失败，修订为实际列表投影后通过，原失败 `/tmp/addp-alert-name-go.log` 保留。首轮完整前端为422通过／2失败，失败是名称框只绑定已提交路由值、未保留输入草稿，键入被控件还原；逐字键入专项也复现，两份原失败 `/tmp/addp-alert-name-frontend.log`、`/tmp/addp-alert-name-typing-final.log` 不计通过。改为独立 `v-model` 草稿并沿路由／身份同步后，最终上述117／424完整门禁通过，没有加定时查询或自动重载兜底。

30个任务实现／测试／契约文件收据 `/tmp/addp-alert-name-inputs.json` SHA-256 `c6a539735647b78d7716ef188435fe4f7680a7ed559f28072728adf9ea8438b0`，最终内容摘要核对无漂移；本节只追加证据。现有Monitor、Console路径选择、PostgreSQL自动发现和前端标准入口已命中新用例，无新增依赖、测试入口或workflow。以上是本地共享工作区T1/T2及受控API的T3，不代表固定提交CI、个人Safari、正式身份T4／Hosted或生产T5。没有重启个人服务、修改个人配置、创建正式告警或运行CI／Hosted，也没有设置定时任务；正式结果继续等专门会话回传。


### 名称检索固定提交 CI：PG通过，前端浏览器被取消（2026-10-25）

专门CI会话回传后，本会话仅核对已落地的原始日志与Job元数据，没有查询、重跑或触发CI。两份日志checkout均为 `a6048b9e71b0689893a3cf90db5a0ef4822d8866`：

- [Release/T2 Run 40110942812／Monitor PostgreSQL Job 120933711310](https://github.com/pampa0629/addp/actions/runs/40110942812/job/120933711310)：真实 `Run Monitor PostgreSQL gate` 步骤及Job均为success，原始日志实际有38项顶层测试通过，含名称检索16子场景、三个规则评估器和旧名称不回填；无FAIL或SKIP。该证据可计为此固定提交的PostgreSQL T2通过。
- [Platform Run 40110942794／Monitor frontend Job 120922337484](https://github.com/pampa0629/addp/actions/runs/40110942794/job/120922337484)：117项单测实际通过，424项浏览器只出现至363项进度即 `The operation was canceled`；真实测试步骤及Job为cancelled，没有整套浏览器通过或Vite构建记录。后续summary步骤的success只表示摘要写入成功，不能作为产品门禁通过。不能将“363项进度”当作363项已通过，也不能把本地424项或旧时间筛选的421项结果替代本次CI全套。

证据目录 `/tmp/addp-ci-20261024-180033/`，原始日志 `job-120933711310.log` SHA-256 `8754ba1551e3fc6738e372ef2dc60b14c583bad53eb2ee5123d457ea0d4973f8`、`job-120922337484.log` SHA-256 `d2f1dc03e6a0b1c54afc6df56f383db885822659e1d2146b94af555060cfdeb0` 均独立核对一致；同时读取 `jobs-40110942812.json`、`jobs-40110942794.json` 的实际步骤终态及协调审计 `Monitor-a604-actual-CI-audit.json`。后继Platform仍由专门CI会话跟进，复用前须证明名称功能及共享输入等价。本会话不接管其他owner的失败，不另派验收、不设置定时任务；本地、个人Safari、正式身份／Hosted和CI范围继续分别记录，功能清单第51行仍为部分实现。


### 名称检索前端 CI 取消原因更正与限定预算修复（2026-10-25）

专门CI会话补回check-run原始annotations后，独立核对 `/tmp/addp-ci-20261024-200030/Monitor-a604-check-annotations.json` 与 `Monitor-f2ca-check-annotations.json`：两轮均明确 `The job has exceeded the maximum execution time of 20m0s`。取消应归因于前端Job的20分钟上限；此前协调按其他门禁失败时间关联取消的推断不成立，不能说由Common／Redis失败联动取消，也不据此接管这些owner。

[Platform Run 40119830116／Monitor frontend Job 120948753235](https://github.com/pampa0629/addp/actions/runs/40119830116/job/120948753235) 为push attempt1、源码 `f2ca8c25e3a69bba0e50d9ad6412cfd51bb02fce`；Job实际18:15:04Z开始、18:35:22Z结束，测试步骤18:16:02Z开始、18:35:19Z取消。117项单测通过，浏览器到423/424进度后中止，无完整通过报告或build。与 `a6048b9e71b0689893a3cf90db5a0ef4822d8866` 比较，Monitor前端、Common Frontend、Node版本、Makefile及唯一browser launcher均无源码差异，不能用旧ea320的421项结果覆盖新424项。该轮与a604原轮仍计取消，不计T3全套／构建通过；原38项PG和本地完整门禁的既有证据范围保留。

核对现行启动与安装链：`prepare-frontend-gate`沿固定Node／锁文件安装，Job随后安装Chromium，再执行唯一 `make test-monitor-frontend`。Monitor配置显式单Worker；共享隔离按整轮门禁分配端口、缓存与输出，不能仅凭此推断提升Worker并发安全。本批保留117单测、424浏览器、所有断言、单Worker与构建，不加skip／重试，不更改产品或单用例超时。只将Monitor矩阵项登记 `timeout_minutes: 30`，Job沿该项／缺省20分钟读取；其他前端仍20。30分钟覆盖已测接近20分钟的完整串行规模，并为安装、剩余用例、构建及退出留有受限余量，不能把增加预算本身记成远端通过。

同步CI规范与现有前端登记检查：拒绝丢失Monitor的30分钟例外、越界／非数字／重复预算、改变全局20分钟默认或给其他owner扩预算。先增加测试，标准 `make test-frontend-ci-registration`因缺失新校验函数失败（22项中9个错误含子场景），补齐唯一校验和矩阵声明后，同一入口22项及当前21个前端实际登记通过；`python3 scripts/test/changed-gate_test.py`标准脚本25项通过，独立调用同一影响矩阵确认workflow改动自动选择Monitor。`git diff --check`通过。本批仅改CI编排／登记／文档，产品、用例和依赖未变，不重复本地15分钟整套或重跑旧超时运行。推送只沿main正常触发，后继实际117＋424＋build是否全过继续等专门CI会话回传，不自行轮询、重试、派发Hosted、重启用户服务或设置定时任务。

原始后继日志 `/tmp/addp-ci-20261024-200030/job-120948753235.log` SHA-256 `580d23329fc832e57792bd5f66e330627eed281e0d14ba5ebb057546c9e0e15d`；两份annotation文件SHA-256分别为 `a240f2e0643b2676aca613f5f7335a1f9a9a7f8105aee8c4860b40542a193e6a`（a604）、`1cf37185d974dd0961e1bc2c152580aeada267ea50ddb7ddea56550c40343e3d`（f2ca）。本地红／绿与选择回归日志 `/tmp/addp-monitor-ci-budget-{red,green,selection}.log`。名称检索个人Safari、正式身份／Hosted缺口仍独立，功能清单第51行保持部分实现。


### 名称检索前端 CI 预算闭环：固定 a292 整套通过（2026-10-25）

专门CI会话回传后，本会话仅独立核对落地日志、Job步骤、日志摘要和Git输入差异，未轮询、重跑或触发CI。主验收为唯一自动push [Platform Run40128534338／Monitor frontend Job120975073843](https://github.com/pampa0629/addp/actions/runs/40128534338/job/120975073843)，attempt1、完整源码 `a2928b9a76a749f0ca5002754c2bc12d118e31fe`。真实 `Run Monitor frontend gate` 步骤和Job均success；原始日志实际117项单测通过、fail／skip均0，424项受控浏览器全部通过（22.4分钟），Vite构建成功（10.78秒），并非仅摘要步骤成功。Job实际24分钟左右完成，证明原20分钟预算不能容纳这轮完整门禁，Monitor限定30分钟可完成本次全套。

名称功能提交 `a6048b9e71b0689893a3cf90db5a0ef4822d8866` 至该验收提交仅六项文件变化：workflow、前端登记检查及其测试、CI规范与证据文档、retention门禁模拟测试。已用Git实际比较确认Monitor产品／前端／424用例、Common Frontend、Node版本、Makefile和标准browser launcher没有差异；单Worker保留，其他前端仍20分钟。因此该固定版本可闭合本批名称前端T1/T3／build及其限定CI预算适配，原两轮20分钟超时继续保留为未通过，原38项PostgreSQL CI另有独立证据。

同SHA的既有GitHub schedule [Run40132268218／Job120986322295](https://github.com/pampa0629/addp/actions/runs/40132268218/job/120986322295) 亦实际117／424／构建通过（浏览器21.9分钟、构建10.53秒）；它是既有workflow的调度，不是本会话创建定时或手工追加运行，只作补充稳定性证据，不替代前述唯一push主验收。

证据目录 `/tmp/addp-ci-20261024-220030/`：两份原始日志 `job-120975073843.log` SHA-256 `04a94d867b231c443c58ab27e32b73038567a6081312f178fe9be68ea5da711a`、`job-120986322295.log` SHA-256 `004918f057e8ce35f6b23e1397e2df8dcd88a72359f76e18a5d7a04aface9388` 均核对一致；两份jobs元数据确认实际步骤与head_sha。协调审计为 `Monitor-a292-actual-full-frontend-CI-audit.json`，本会话核对收据 `Monitor-a292-owner-independent-audit.json`。本轮仅追加文档并执行 `git diff --check`，不重复仍有效的本地测试。CI受控浏览器不替代用户重启后的Safari名称检索、正式身份／Hosted T4或整行其他开发缺口；第51行仍为部分实现。

### 告警名称检索个人 Safari 只读复验（2026-10-25）

用户完成重启后，`curl --silent --show-error --max-time 10 -w '\nHTTP %{http_code}\n' http://localhost:8100/health/ready` 实际返回200、ready／registered；运行源码为 `7b3a5d6cac630ac76edbfdebda61e2ee89f674cf`，Git祖先检查确认包含名称功能 `a6048b9e71b0689893a3cf90db5a0ef4822d8866`。在原生Safari现有平台系统管理员会话，通过Console及Monitor iframe的真实页面复验，不使用内置浏览器、不提取登录凭据。

首次读取仍是重启前保留的页面内容，告警名称显示“未记录名称”；完整重载后，现有日志告警正确显示固定信号名称“源容量接近额度”。名称框先输入“源”、再追加“容量”，草稿分别保留；提交后Console及iframe地址均保留 `severity=warning&search=源容量`，列表返回原有一条告警。替换为“不存在的告警名称”并提交，真实列表显示“暂无数据”；清空名称后 `search` 移除、“警告”级别保留、原有告警重新显示。再输入匹配名称，整页重载及通知管理→告警事件往返均恢复相同名称、级别和一条匹配结果。原生AX状态与实际截图均核对了名称输入框、名称列及查询结果。

本次只读查询，没有创建、确认、抑制、删除告警／规则或操作通知发送；当前数据仅一条日志告警，不能据此宣称真实多页、规则改名／删除后的名称冻结、旧规则缺名不补造或大小写／通配符专项在Safari通过，这些仍分别引用已有T2和受控T3范围。该证据只计个人Safari页面复验，不代替正式隔离身份T4／Hosted或生产T5；功能清单第51行仍部分实现，设备名称检索、高等级置顶、拓扑／影响范围及根因分析仍有缺口。

本机证据：`/tmp/addp-alert-name-safari-result.json`，匹配／刷新／空结果／清除／通知往返的 `addp-alert-name-safari-{match,refresh,empty,clear,return}.txt`，匹配／空结果／最终页面的 `addp-alert-name-safari-{match,empty,final}.png`，均位于 `/tmp/`。本轮只追加本节，执行 `git diff --check`；不重复仍有效的产品门禁，不查询或触发CI／Hosted，不重启服务、不设置定时任务。清单只读核对Excel第51行、B列50“告警信息管理”，由维护会话统一更新M列。

### 当前主机名称筛选：事实来源与本地分层验证（2026-10-25）

只读核对确认 System 的 `HostNode.display_name` 是可编辑的当前纳管名称，主机资源事件以 `resource/host_node` 与 UUID 引用；日志观测器标识、服务登记宿主名／运行环境名、模块名不是这份名称。用户确认首期仅查当前主机名称对应的主机资源告警后，先修订运行观测规范7.1，再在唯一告警列表增加 `host_name`，沿告警名称已有100字符、去首尾空白、忽略大小写的字面包含口径。非空参数复用当前 User Bearer、System 完整有界 `HostNodesForUser` 投影及原主机读取权；最多100台、五秒／四MiB，包含停用主机，失败／超限拒绝，不使用地址搜索、分页首批或后台身份兜底。匹配UUID与现有条件在计数／分页前取交集；改名后按新名查询，不存触发时主机名、不推断其他主体的承载关系。空值不增加System依赖，无数据库迁移、新权限、角色或私有观测字段。

页面在“主机名称”框旁标明仅主机资源告警，沿原独立输入草稿、提交／清空回首页、URL／刷新／通知往返恢复和失败清空。无主机读权时不显示可编辑入口；已有非空URL条件保留并由后端拒绝，不悄悄撤去筛选。已有当页主机引用展示仍由唯一共享解析器负责。

| 标准入口／脚本 | 本次实际结果与范围 |
| --- | --- |
| `make test-module MODULE=monitor GO_ONLY=1` | 7个Go测试包通过，仅计Go T1；新增主机名称额外权限、空条件不增加权限、重复／非法参数等API守卫用例。 |
| `make test-monitor-postgres` | 39项顶层T2通过、无失败／跳过。新增名称15个子场景及附加断言覆盖Unicode／大小写、字面百分号／下划线／反斜线、停用主机、多个同名片段主机、准确多页与时间／名称／级别／状态／ID交集、改名后旧名不匹配、同UUID的其他主体不误关联、Owner失败／超限／超时拒绝及空条件无依赖。System读取使用受控Reader，非真实身份T4；新增随机夹具退出清理并核对零行。已先运行Infra状态，实际共享映射25432，仅使用 `addp_test`。 |
| `make test-monitor-frontend` | 首轮118项T1通过；整套T3为426通过／2失败（15.5分钟），失败均是新增中英文用例缺少 `resourceIncident` 导入，在进入页面前发生ReferenceError。原轮门禁失败，未执行其后构建，不计整套当前源码通过。 |
| Monitor目录 `node ../../scripts/test/frontend-browser-gate.mjs e2e/platform-alerts.spec.js`；`npm run build` | 仅补齐上述测试导入，产品代码和其余输入未变。最终告警页34项T3全部通过，含原有名称／时间／生命周期及新增中英文680px主机筛选、连续输入、多页／刷新／通知往返、字面字符、空匹配、清除保留其他条件、错误清空与无权／非法URL拒绝；标准构建步骤成功。首轮其余394项与最终该页34项共同覆盖当前428个用例，但不将组合证据冒充一次全量绿色门禁；后继完整CI结果仍待专门会话回传。 |
| `make test-console-frontend` | 195项T1、220项真实iframe受控T3及构建通过；新增主机名称与告警名称／时间／级别在同一iframe和Console URL恢复。此前同一标准browser launcher的5项导航专项也通过。 |
| 标准Monitor Swagger生成／覆盖；Common Manifest `--coverage-report` | 生成成功，91条公开路由方法一致；授权覆盖报告 `complete=true`，新增参数及条件主机读取权限已同步，不修改IAM目录或角色。 |

现有路径选择、Monitor PostgreSQL自动发现及前端入口已命中，`changed-gate.py --dry-run`实际选择Monitor和Console；无新增入口、依赖或workflow。Monitor仍沿既有30分钟完整前端预算，CI规范删除易过期的“当前用例数”表述，不改变预算、Worker、断言或重试。没有轮询、手工派发或重跑CI／Hosted，也没有个人数据库写入、服务重启或定时任务；提交后只沿main既有push触发编排。

证据 `/tmp/addp-alert-host-name-{go,go-final,postgres,frontend,alerts-final,build,console,console-full,swagger,swagger-coverage}.log`，授权覆盖及影响选择回执同前缀；`/tmp/addp-alert-host-name-inputs.json`记录22项实现／测试／契约摘要、唯一测试导入修正与全部日志SHA-256。真实Safari尚未加载本批后端，当前主机名称筛选的个人运行／正式隔离身份／Hosted未验，不沿用前一批“告警名称”Safari证据代替。Excel第51行仍为部分实现，高等级置顶、拓扑／影响范围及根因分析等缺口保留。

### 当前主机名称筛选个人 Safari 只读复验（2026-10-25，排除与恢复）

用户完成重启后，`curl --silent --show-error --max-time 10 -w '\nHTTP %{http_code}\n' http://localhost:8100/health/ready` 实际返回200／ready，Monitor已登记，运行提交 `5f90384a19a06edff2a8a2bc61a65375ea31c857`，包含产品提交 `994db320ae7147cb0e566db4960eb256306cc9e1`（`git merge-base --is-ancestor`退出0）；Build ID为 `20261025T023212Z-monitor-81346-3DgCB9`。使用个人Safari既有平台系统管理员会话，在真实Console完整刷新后复验；没有读取登录凭据或使用受控API代替个人运行数据。

先在“主机监控”读取当前纳管名称 `Docker Desktop Linux VM（Business / Infra）`。告警页“警告＋全部状态”实际显示5条日志链路告警，增加告警名称“源容量”后为2条；主机名称分段输入 `dOcKeR Desktop`，草稿保留，提交后Console与iframe URL均包含 `host_name=dOcKeR+Desktop`，显示空结果且无查询错误，截图可见输入框及旁边“仅查询主机资源告警”的说明。整页刷新、通知管理只读往返后，主机名、告警名、级别及全部状态均恢复；未知名称“不存在的测试主机”同样为空。清空主机名称后URL移除 `host_name`，其余三个条件保留，原2条日志告警恢复。

本次证明个人Safari输入、非主机事件排除、空结果、清除及条件恢复；**该警告／全部状态数据集没有主机资源告警，真实主机告警正向命中及大小写匹配不计Safari已验**。真实多页主机告警、改名／停用、权限变更与正式隔离身份T4／Hosted／生产T5仍未验；已有本地T2／受控T3的覆盖保持独立。未创建、确认、抑制、删除告警或规则，未操作通知发送或更改主机。

原始AX、截图及ready响应位于 `/tmp/addp-alert-host-safari-*`；操作汇总 `/tmp/addp-alert-host-safari-result.json`（SHA-256 `03c55fa2e39f830ab01b2c52e0b836290af992265a0625dbcb1298e82f64278f`）记录各文件摘要，完整过滤状态引用 `refresh.txt`／`return.txt`，不把无变化的差分AX当完整页面。本轮仅追加证据并执行 `git diff --check`，产品未变，不重复有效产品门禁。没有查询、触发或重跑CI／Hosted，也未设置定时任务，继续等专门会话回传。清单第51行仍部分实现；本次只补个人Safari排除与恢复证据。

### 当前主机名称筛选固定 994 前端 CI：完整门禁通过（2026-10-25）

专门CI会话回传后，本会话独立核对已落地原始日志、Job及实际测试步骤终态与内容摘要，没有轮询、手工派发或重跑CI。[Platform Run40150409107／Monitor frontend Job121040604365](https://github.com/pampa0629/addp/actions/runs/40150409107/job/121040604365) 为push／attempt1、固定源码 `994db320ae7147cb0e566db4960eb256306cc9e1`；真实 `Run Monitor frontend gate` 和Job均completed／success，日志实际118项单测通过、fail／skip均0，428项浏览器以单Worker全部通过（18.7分钟），Vite构建成功（7.01秒）。包括主机名称规范URL及非法条件保留单测、当前主机名称筛选与分页／刷新／导航恢复的中英文真实页面用例。该固定版本已具备一次完整绿色的前端T1／受控Linux T3／build证据，不再只有上一节的本地组合结果；本地首轮两项测试导入失败仍保留为失败。

同Run的Platform consistency Job121040604338仍因独立的部署发现observer退出核验失败；不能把此Monitor Job成功写成整轮CI全绿，也不能把通用清理失败当作主机筛选业务失败。该证据不代替个人Safari、正式身份／Hosted、真实运行环境或生产T5，不扩展为当前HEAD整套产品已验。后续 `05b232920` 仅补退出诊断，原清理故障根因未定，其实际结果继续等专门CI会话回传。

证据目录 `/tmp/addp-ci-20261025-020029/`：`job-121040604365.log` SHA-256 `6af0ee1d76fb65d2713185e59c7c8657ccc9bc3af7855e2012090f92f65d874d`与协调审计一致；`jobs-40150409107.json`确认固定head_sha及实际步骤终态，协调审计 `Monitor994-actual-full-frontend-CI-audit.json`、本会话独立收据 `Monitor994-owner-independent-audit.json`。本轮仅补权威证据文档并执行 `git diff --check`，不重复有效产品门禁；清单业务CI结果由协调会话统一回报，本会话只补文档出版指针，不重复派发或重跑验收。

### 持续部署发现 Linux 退出失败调查：仅补诊断，根因未证实（2026-10-25）

专门CI会话回传后，只读核对 [Platform Run40150409107／Job121040604338](https://github.com/pampa0629/addp/actions/runs/40150409107/job/121040604338) 原始日志的checkout与实际失败步骤，源码为 `994db320ae7147cb0e566db4960eb256306cc9e1`。`make test-platform → test-dev-lifecycle → test-infra-runtime-log-lifecycle` 的61项中，`test_logs_failure_does_not_prevent_selected_metrics_start` 在 `addCleanup/stop_discovery_observer` 收到“部署发现进程尚未退出，不删除运行记录”；Job实际失败，后续摘要写入成功不能覆盖。日志没有当时的进程状态／等待／信号／子进程证据，不能断言是僵尸、锁、Docker子进程或预算不足，更没有证据把它归因于新增告警 `host_name` 逻辑。

本机基线 `make test-deployment-discovery` 的34项发现＋5项容器证明，以及 `make test-infra-runtime-log-lifecycle` 的79项前置、61项生命周期、登记／Go夹具均通过，未复现原Linux退出失败；macOS通过不能替代该Linux问题的根因证据。没有对同一未变输入反复加压运行、放宽原100次、50ms间隔的退出观察循环、增加信号／sleep／重试、强删记录或跳过断言。

本批只在现有“无法确认退出”失败分支追加即时有界诊断。Linux从同UID普通proc文件读取状态、固定等待类型、SIGTERM挂起／屏蔽／忽略／处理位及直接子进程数量；每个文件最多8192字节，拒绝符号链接／非UTF8／超量，读取失败明确未知，其他平台明确不可用。只输出固定键、枚举、布尔及数量，不输出命令行、路径、PID清单、任意wait文本或随机运行标识。字段依据为 [Linux proc状态手册](https://man7.org/linux/man-pages/man5/proc_pid_status.5.html)及[内核proc文档](https://docs.kernel.org/filesystems/proc.html)；瞬时诊断不参与归属或清理裁决。原SIGTERM只发一次、foreign拒绝、确认退出前保留记录、退出后由控制流程清空来源等主路径不变，无新配置、接口或权限。

先补三项诊断回归，标准发现入口因缺失诊断函数产生3个错误，保留红灯日志；实现后再补真实文件读取的大小／编码／符号链接拒绝，最终 `make test-deployment-discovery` 为38项发现＋5项容器证明全部通过。`make test-dev-lifecycle` 最终退出0，132项原生生命周期、79项前置、61项Infra生命周期、Online登记／Go夹具、端口／缓存／锁／原子构建及现有共享包检查通过；末尾Worker命令的“无测试”仅计编译覆盖，不外推为Worker功能验收。标准影响选择继续命中Platform；原Make与CI登记已覆盖，无新增入口或workflow。`git diff --check`通过。

原始失败 `/tmp/addp-ci-20261025-013034/job-121040604338.log`；本机 `/tmp/addp-discovery-exit-{baseline,diagnostics-red,diagnostics-final,dev-final}.log`、`/tmp/addp-discovery-infra-baseline.log`及`/tmp/addp-discovery-exit-selection.txt`。本批不宣称原CI退出故障已修复，Linux实测与新诊断尚待专门CI会话回传；没有轮询、手工派发或重跑CI／Hosted，也没有操作个人服务、keepalive、数据库或数据卷，不设置定时任务。已只读核对功能清单第28／30／34行的主机管理内容，本次诊断不等于新增主机注册、远程初始化或批量运维，不据此升级功能完成程度。

### 持续部署发现固定 05b Linux 门禁通过：原间歇失败仍未闭环（2026-10-25）

专门CI会话回传后，只读独立核对落地原始日志、Job及实际步骤终态。[Platform Run40151852077／Job121044843183](https://github.com/pampa0629/addp/actions/runs/40151852077/job/121044843183) 为push／attempt1、固定源码 `05b232920cd1801fcbb409a392aa6386f73efe26`；实际 `Run platform consistency gate` 从01:48:44至02:11:09 UTC完成success，Job亦success，后续平台／Swagger检查实际走完，非仅摘要成功。原失败路径的Infra运行日志／Trace／PostgreSQL metrics生命周期标准聚合61项于01:51:17记录OK（19.245秒）；后续部署发现38项及容器证明5项也实际OK。不将其他同数测试组或初始MinIO缓存用例误记为该组，不把同Job其他既有跳过项算作运行验收通过。

本轮只证明退出诊断增强版本在该Linux标准门禁通过，没有重现994原轮的 `stop_discovery_observer` 失败，也没有取得失败时的新退出诊断；**原间歇故障根因仍未证实，不能认定已修复或注销**。保留原失败日志、归属检查、信号次数、观察循环及未确认退出不删记录的边界，不为采样反复触发或扩大预算；后续已有正常push若复发，继续由专门CI会话取回有界诊断再定位。不据此提升主机注册、远程运维、主机名称筛选Safari／Hosted或生产完成程度。

证据目录 `/tmp/addp-ci-20261025-023003/`：原始 `job-121044843183.log` SHA-256 `323e6228ec32acda74e8ea701ee4a611e1ec6473f929e39a391d4f33ba981e1e`与协调审计一致，`jobs-40151852077.json`确认准确head_sha及实际步骤success；协调审计为 `Observer05b-actual-Linux-Platform-diagnostic-audit.json`，本会话新收据 `Observer05b-owner-independent-audit.json`。本轮仅补证据文档并执行 `git diff --check`；没有轮询、派发或重跑CI／Hosted、操作个人服务或设置定时任务。

### 执行概览只读维护真实进程验收（2026-10-25）

本批仅补第一期只读候选评估的真实 System 维护首轮，人工 Hosted Linux `execution-retention-readonly`，独占 System／Gateway／Infra；不启用 180 天概览物理删除，不改变任何 Owner 专业结果期限、业务实现或现有读取授权。System-owned helper 通过正式 Tenant Service 建立两个非默认 Tenant，普通测试 User 仅获既有 `monitor.execution.read`；实际 AuthContext 校验后，由受限 Common 历史 helper 准备合成旧记录，标准生命周期停止／启动的仅是本轮独占 System。维护使用产品既有首轮与周期，不直接调用评估函数代替真实进程，不增加测试 API 或产品调度参数。

首轮 [Run40138788559／Job121005906648](https://github.com/pampa0629/addp/actions/runs/40138788559/job/121005906648)，固定源码 `2a3cbb1f677af23f955ff02038205d525aafc93d`，**失败**于历史准备，后续 14 棵树的维护断言未运行，不能计为通过。夹具错误地使用了现行 `chk_task_executions_status` 禁止的 `unknown_fixture`；原日志仅记录 `prepare:CalledProcessError`，没有当时的 SQLSTATE，不补写为已观测的数据库错误码。应用及凭据清理为 passed、Infra 为 zero_residuals。官方 Artifact `12577428981` 已下载并独立核对 ZIP SHA-256 `1a4fe210c3d121de2f13e977751df08dfe51f1b47121d105aa87a2527843b9b8`；原日志 `/tmp/addp-retention-t4-40138788559.log`、提取报告 `/tmp/addp-retention-t4-40138788559/`、审查回执 `/tmp/addp-retention-readonly-t4-receipt-20261025.json`。

修正提交 `86f58b3764c4c3e47af1a141d81e21f94109150c` 改用合法 pending 夹具，并补当前存储 CHECK 与所有夹具状态的交叉回归；未知状态的防御仍仅由 Common 单元层覆盖，不放宽或删除生产约束。测试驱动的 SQL 失败只记录 SQLSTATE，汇总失败只保留白名单计数，拒绝归档原始 SQL、私有字段或任意保留原因内容。标准入口 `make test-execution-retention-online-runner` 实际 **28 项 Python 回归通过**，System `cmd/online-test-fixture` Go T1 通过，本批 `TestExecutionRetentionFixtureHasOnlyMonitorReadPermission` 明确 PASS；同包三员、Catalog 授权 observation、PG 授权成员、Metrics 与 Raster Policy 的 5 个既有 PostgreSQL 专项因未设置标准 DSN 跳过，不计为本批 T2 通过；含只读汇总封闭性、计数错误、整行／事件变化拒绝、容器身份拒绝、初次日志落盘等待、准备失败、业务失败与清理残留拒绝，最终显式 PASS／SKIP 日志 `/tmp/addp-retention-t4-runner-final.log`。本轮不重复未变产品的既有 Common PostgreSQL T2；全库注册检查在共享未提交工作区曾受并行 Graph 契约修改阻断，不计为通过，也不修复该 Owner 的业务实现。本批注册验证、唯一人工 Hosted profile、Online 分发器及标准 runner 归属已由上述定向门禁覆盖。

修正版真实验收 [Run40140020076／Job121009605836](https://github.com/pampa0629/addp/actions/runs/40140020076/job/121009605836) 绑定 `86f58b3764c4c3e47af1a141d81e21f94109150c`，已完成且独立审查通过：Run／Job、真实运行步骤均 success，attempt 1；前述排队状态现由实际结果闭合，首轮失败仍保留。验收要求 16 条合成历史形成 14 棵执行树、5 棵时间候选，核对完整超期树、年轻子执行、running／pending、有效 lease、完成时间缺失、缺父／跨 Tenant／自指及树外 retry／recovery 引用；全部树始终保留 Owner 释放和消费者引用未核验原因。维护前后执行完整行与 30 天内事件的数据库摘要必须一致，报告仅保留安全批次事实，退出还须核验独占应用／Infra／卷／凭据清理。该证据不代表用户执行读取权限、Owner 释放、专业结果生命周期、30 天事件物理维护、小时周期、概览删除、容量／HA 或生产 T5 验收；清单实际第327／328／329行继续部分实现。

修正版官方报告实际为 10 项唯一断言通过：16 个种子、14 棵完整树、5 棵时间候选，本批读取 24 行、20 次查询，游标16、无后页、未超预算。保留原因为 Owner 释放未核验14、消费者引用未核验14、活动执行2、完成时间缺失3、有效lease2、未超期1、关系异常4、树外执行引用2；时间候选并非允许删除。执行完整行和保留期内事件不变均为 true，汇总多余字段数为0。System／Gateway 的正式 preflight 均为同一固定源码与独立 Build ID／源码指纹，工作区干净、网络仅回环；普通 Tenant User 的唯一角色为 `online.execution_retention_readonly`，所验证 Permission 仅 `monitor.execution.read`。

Suite 内的 `cleanup=pending_deployment_destruction` 只是场景完成时尚保留合成历史；独立核对最终 Host `summary.txt`，结果和清理均 passed、Infra为 zero_residuals，应用停止、测试卷／网络和凭据销毁均已完成。该销毁不计180天概览物理删除。官方 Artifact `12579077155` 的5个文件仅为既有安全报告及生命周期日志，不含凭据目录或原始 System 进程日志；ZIP已下载，SHA-256独立匹配上传声明 `eaa740d8a10f7a767b66f266b2dc51cb3b2c2f2d0f08457cadc2a4b2807c7c2c`。证据：`/tmp/addp-retention-t4-40140020076.zip`、同名提取目录及 `.log`、`/tmp/addp-retention-t4-40140020076-run.json`、`/tmp/addp-retention-readonly-t4-rerun-receipt-20261025.json`。已实际比较固定源码至后续证据提交：Common execution、System维护与身份夹具、测试驱动、Hosted路径及workflow均无变化；后续仅证据记录与Make中Go结果显式输出，不重复有效T4。

截至2026-10-25 07:17（Asia/Shanghai），同修正提交的 [Platform CI Run40139997741](https://github.com/pampa0629/addp/actions/runs/40139997741) 仍queued，未计为通过；该聚合CI不能由本批T4替代。当前闭合的范围仅为本地28项回归／Go T1与固定版本真实System首轮只读维护；5项既有PG专项的跳过、未运行的更广生命周期及首次失败均继续保留。Excel实际327／328／329行仍为部分实现，已向维护会话同步；Owner引用／释放、专业结果生命周期与物理删除不进入本轮开发。



### 正式身份本地第十窗口：执行、Trace、日志与真实页面闭环通过（2026-10-25）

用户接受“当前源码准入和Gateway构建已通过、历史间歇失败唯一根因未定”的前提，另行授权2026-10-25 06:17:43–07:17:43（Asia/Shanghai）最多60分钟本地窗口；8 CPU／12 GiB、当次卷及私有可写目录4 GiB停止线、单次完整资源测量4秒预算保持。只运行根`make test-monitor-traces-local`，不运行Hosted、不重跑容量、不设置定时，不接管个人服务或以共享数据库承载测试。两次尝试共用同一绝对截止时间，未重置60分钟，所有产品均从各次准确已提交源码私有副本构建；仅固定MinIO／mc按原规则核验复用。本次未改产品代码、身份权限、采样或验收断言。

| 尝试 | 实际源码 | 结果与实际覆盖 |
| --- | --- | --- |
| 1 | `7b3a5d6cac630ac76edbfdebda61e2ee89f674cf` | **失败**。7个源码二进制、10个产品镜像、浏览器依赖及11项Nginx路径／参数、缺席模块502／已部署模块可读通过；`isolated_deployment_start`的容器详情检查耗时4.070秒，触发原资源守卫停止，身份与业务断言未运行。 |
| 2 | `47b02db01342a049c4fe01ae1f3ee15369b04863` | **通过**。重新完成全部产品构建、Nginx及实际版本检查；六个后端`/health/live`提交均准确匹配该SHA，12项后端断言和6项真实页面断言全部为真。06:55:09报告在完成清理后写入，早于原07:17:43截止；06:55:37独立残留审计通过。 |

第2次通过的范围：

- System-owned夹具导出凭据前，实际只读等待当次Tenant、固定Develop的保护初始化checkpoint；本次运行跨过了第八窗口尚未验的准备前置。正式密码／真实TOTP／AuthContext链路通过；准备User真实MFA后经正式IAM API给自身分配既有`tenant.infrastructure_administrator`，Refresh重读同一主体／Tenant／Membership及准确权限，MFA和角色分配后的旧Token拒绝。普通执行User仍准确持有原11项权限，没有为验收扩大授权或伪造AAL2。
- 普通User经正式API创建合成Engine上的查询任务与编排，实际完成Orchestrator→Develop的成功和受控失败父子执行；终态与父子关系一致，公开执行树DTO未输出新增私有Trace字段。成功父／子UUID为`257c2e73-ade0-426f-bde3-e90223d5d82d`／`21179a6c-028b-41ff-a6a8-172afbd790e5`，受控失败为`ffbd76ac-fc82-4a1a-9354-3f8e62f25cca`／`3afd46a8-3400-48d1-8993-4d9c411f2e0e`。仅使用已批准的合成常量查询，不将SQL、参数或结果写入证据。
- Platform系统管理员经System正式日志接口读到两Owner、两组执行的真实enqueue／claim／dispatch／terminal共16条检查点，核对实际attempt／outcome／Context。沿原v2采样自然等待125秒后，两个失败终态独立Trace均实际可查，各为一个`addp.execution`终态Span，并准确关联持久enqueue／dispatch两个Link及当前System实例身份；再以相同trace/span核对精确检查点日志。Orchestrator终态Trace／Span为`8eaca8355f7e95edf240a6a6c1673417`／`191017df7a1d550d`，Develop为`9d5510fc1f897f0a70da060bdd2db770`／`4e005bdc29851e44`。未开全采样、注入日志、直读execution私有列或手工补链。
- 普通Tenant User与Platform安全管理员读取平台Trace／日志均实际403；原User经正式API删除当次任务定义／编排后重读404；四个API客户端正式退出后旧Access Token均401。准备角色的旧Token拒绝和最终退出拒绝分别验证，不把会话撤销写成单实例撤权。
- 真实Console Chromium页面通过密码／MFA登录并核对Platform系统管理员AuthContext，Monitor iframe显示同一失败终态Span；点击因果Link前实际重核当前实例，并按目标started_at构造新查询窗口，返回受限结果；随后在System iframe显示相同execution UUID／trace／span的真实检查点日志。六项页面断言全部通过，没有mock API、注入Token或采集密码页面截图／视频／trace。

页面回执`link_result=not_found`：被点击的普通enqueue目标独立采样，本次未保留，实际查询404；它仍通过当前实例授权、精确目标时间窗及有界未保留结果断言，不表示目标Span曾实际读取成功。原普通5%／失败优先、120秒等待、72小时／20 GiB初值及5秒／2 MiB查询、原并发预算全部保持；不能以本次两组查询或未保留目标推定全调用链完整留存、未执行或生产容量。本次仍仅为本机隔离正式身份技术闭环，`formal_runner_t4=false`，不代替Hosted／正式专用Runner T4或生产T5；其他Runtime／continuous／Python异步、SQL Span及真实长期保留等既有边界不变。

两次共有507次成功资源测量；合计硬配额峰值7.8 CPU／10468982784字节（9.75 GiB），卷及私有目录实际分配峰值3652866048字节（约3.40 GiB），成功测量最长2.766秒；第一次失败的4.070秒另列，不混入成功峰值。两次均未记录OOM或4 GiB停止线触发。按记录的完成时刻减单次耗时重建测量开始间隔，两次峰值约4.018／4.017秒，低于5秒；完成时间戳间隔峰值为5.350／6.330秒，不能与开始间隔混用。第2次共287次测量，均在原4秒及资源范围内；通过不抹去第1次失败，也不宣称解决了Docker状态读取偶发超时或历史Gateway构建故障。

标准清理和独立审计均确认两次当次容器、网络、卷、构建器容器／状态卷、镜像标签和私有目录零残留，既有MinIO镜像保持原不可变ID，两个只读页面回执接收端均已退出。首次独立审计在无筛选全镜像列表读取时20秒超时，未计审计通过；随后仅对标准入口实际创建的当次产品镜像reference精确过滤，完成相同残留核验，未放宽产品资源守卫或清理标准。

证据目录`/tmp/addp-p19-formal-local-20261025-window10/`：`window.json`、`attempt-{1,2}-source.json`／报告／日志、`attempt-2-browser-receipt.json`、`receipt.json`、`cleanup-audit.json`、`evidence-review.json`、`function-row-mapping.json`及初次审计读取未完成记录。第二次run_id为`addp-trace-local-4eb709c8ced44689a5dd7fdb2884ea58`，第一次为`addp-trace-local-54dc65c1ba754a9fb05ce5a1ec0ca2e4`。交叉复核原始脱敏报告、12＋6断言、六模块SHA、原截止时间与全部资源边界，`git diff --check`通过；本次仅更新已有证据，不重复未变代码的T0/T1门禁，不调查／触发其他专项CI或Hosted。

清单已只读核对Excel实际第19行（B18，接口管理总述）与第45行（B44，实时耗时接口追踪）；两项仍为部分实现，但“正式身份本地有界执行→Trace→日志→页面闭环”可以记为已验，不能继续笼统写成本地闭环未验，也不能升级为全域追踪、正式Runner T4或生产完成。工作簿由维护会话统一回填，本会话未编辑。后续优先调查资源守卫Docker状态读取的偶发超时，在原4秒标准内补充分步骤证据后再讨论优化，不自动扩展本次运行范围。


### 资源守卫四秒截止、分步计时与等待优化（2026-10-25）

用户确认先调查、讨论后实施最小修正，仅确定性门禁及短时只读探针；不新开正式身份窗口，不运行Hosted、容量或定时。实现及规范提交为`569940d925f998e8273530bb0017c7ce685a7915`。资源守卫沿用唯一CLI全量检查，保留8 CPU／12 GiB、4 GiB停止线、一次性退出状态与OOM拒绝，不改身份、Trace采样或产品API。

已修正两项可验证的实现问题：命令从进程创建前开始计时，并使用有界进程等待替代固定50毫秒睡眠；整轮沿同一单调时钟四秒绝对截止，输出读取／解析、状态校验及末步成功判定均计入，重扫不重置。进程组TERM／KILL与回收保持，超时后的取消清理另计，不把超时结果记为成功。成功与失败均保留当轮阶段、容器／卷数、每步骤及命令启动／等待／输出处理／取消清理耗时，锁等待单列；监测循环保留原失败明细。触及停止线的实测值保存在失败明细，超量结果不进入成功样本。

调查确认历史报告中的操作名只表示累计预算耗尽位置，缺少失败当轮分解，不能断言该操作单独耗时四秒。旧实现内存模拟的末步4.01秒误放行已由回归拒绝。Docker CLI批量inspect逐对象串行读取，但繁忙阶段唯一根因仍未定；未将等待优化或空闲态成功宣称为偶发超时稳定性已验。

验证输入为已提交`407e5d38ddf906652cc4c3e495b77b0daa57a2f9`的仓库外临时副本，仅叠加本专项四文件；`candidate-inputs.json`记录各文件SHA-256，提交前核验无漂移。共享工作区入口曾被其他会话未完成的拓扑登记阻断，本会话未修复或跟进该无关问题。

- `make test-monitor-traces-local-runner`退出0：91项确定性检查、页面JS语法及CI登记通过。新增回归覆盖同一四秒预算递减、进程创建／输出处理／JSON解析／末步超时拒绝、即时结束有界等待、取消及TERM／KILL回收、线程诊断隔离、成功／失败计时、锁等待和脱敏；原问题已先在旧实现复现失败。该入口也在下述生命周期门禁中完整执行，System正式身份夹具的ExecutionTrace／ProtectionReadiness Go专项通过（2.026秒），不计真实身份部署或页面验收。
- `make test-infra-runtime-log-lifecycle`退出0：除上述91项外，61项生命周期／部署／采样与容量契约单测通过（87.385秒）；只是确定性契约测试，没有执行容量模型或新部署。
- `make test-changed`未完成：准入时缺少`ADDP_TEST_POSTGRES_PORT`、`ADDP_TEST_POSTGRES_PASSWORD`，未执行其计划的Monitor PostgreSQL等T0–T3整套门禁，不计通过。现有CI的Platform consistency及Monitor execution and notification integration (PostgreSQL 15)负责对应独立复验，当前没有新的CI通过结论。
- 同轮10次、三种调用方式、五类只读操作，共150次均通过：容器列表中位耗时旧封装57.24毫秒／新封装39.18毫秒，卷列表57.07／38.75毫秒，小证据目录du 55.04／6.85毫秒；固定公共镜像元数据57.59／56.35毫秒，仅小幅变化。直接调用对照、每次耗时及零失败均见`readonly-profile-paired.json`。当次本专项容器和卷标签列表均为空；没有创建或更改Docker对象，不代表带负载完整采样的稳定性结论。

脱敏证据目录为`/tmp/addp-p19-resource-guard-investigation-20261025/`，含调查前后探针、`deadline-simulation.json`、`docker-log-correlation.json`、`candidate-inputs.json`、`runner-red.log`、`candidate-runner.log`、`candidate-lifecycle.log`、`candidate-test-changed.log`及`verification-receipt.json`。生命周期日志SHA-256为`816bf3cda2244c910182b657c1e3c08a678b5dd1f5ce2dfc4ec882de1c002333`，同轮探针JSON为`f0f777c873985e5822798867c2407fc7adbc3a30e0f2edcb4230a2286e6badb3`。既有正式身份闭环证据不变；下一步需先讨论有界繁忙阶段验证方案，利用新计时定位尖峰，不自动申请或续开窗口。


### 资源守卫有界繁忙阶段：唯一一轮本地诊断通过（2026-10-25）

用户确认先补充条款与编排检查，再执行一轮40分钟、8 CPU／12 GiB、4 GiB停止线诊断；四秒完整采样口径保持，失败即停并清理，不重试或续窗。实现及规范提交`8d391306b7814e2b3bad737e38c74cd9015eb409`，仅在已有本地生命周期文件增加可测试编排和汇总，仓库外临时驱动调用同一准入、构建、部署、守卫及清理；不增加产品参数、Makefile或workflow入口。既有Platform确定性登记覆盖新增检查，真实诊断不登记Hosted或定时。

运行前在已提交`435c1aeae677030329e9f9a40863d5b12c24f3ba`的仓库外临时副本仅叠加本专项四文件，输入SHA-256核对无漂移。`make test-infra-runtime-log-lifecycle`退出0，内含99项本地入口／诊断确定性检查、JS语法／CI登记、System ExecutionTrace／ProtectionReadiness Go专项（0.911秒），另61项生命周期检查通过（44.018秒）。覆盖阶段绝对截止、三组及六次检查、120／300秒观察、样本不足与时间边界拒绝、失败立即传播且恢复包装、无身份调用。`make test-changed`仍因缺少`ADDP_TEST_POSTGRES_PORT`／`ADDP_TEST_POSTGRES_PASSWORD`在准入停止，Monitor整套T0–T3未运行、不计通过；现有Platform consistency及Monitor PostgreSQL CI负责相应独立复验，没有据此声称本次CI已通过。验证副本运行前已删除，共享未提交改动未混入运行。

实际命令`python3 /tmp/addp-p19-resource-busy-20261025/run-once.py`，只调用`run_resource_guard_diagnostic`及原根`make build`／`make build-images`（由`IMAGE_BUILD_ARGS`传入`--verify --jobs 1`等固定参数），未调用正式身份主入口。窗口为2026-10-25 11:36:18–12:16:18 Asia/Shanghai，run_id=`addp-trace-local-c20f1a664e4b4858861db41f91b80180`；准备／构建约820秒结束，七个二进制与十个产品镜像成功，空闲120秒、三组启动及六次前后检查、稳态300秒均完成。六个真实Backend的live构建SHA全部为上述`8d391306b...`。执行阶段约1269.118秒，含标准清理1283.831秒，于11:57:42结束，独立审计11:57:50完成，均在原截止前；只有这一轮。内部沿既有60分钟计数器扣除1200秒以落实40分钟，原始`elapsed_seconds`含此偏移，实际时间以window及原始单调时钟交叉核对。

全部323个完整资源样本通过，按阶段汇总如下（秒，最近秩分位数；启动14个含六次明确检查）：

| 阶段 | 样本数 | P50 | P95 | P99／最大 |
| --- | ---: | ---: | ---: | ---: |
| 准备／依赖启动 | 26 | 0.520 | 2.848 | 3.792 |
| 源码编译 | 99 | 0.459 | 0.700 | 1.009 |
| 镜像构建／当次缓存清理 | 77 | 0.487 | 0.791 | 1.096 |
| 构建后空闲 | 31 | 0.365 | 0.581 | 1.167 |
| 三组服务启动 | 14 | 0.543 | 2.361 | 2.361 |
| 就绪后稳态 | 76 | 0.385 | 0.611 | 0.791 |

单轮最大原始耗时3.791999666秒，采样开始间隔最大4.009778秒，分别满足严格小于四秒及开始间隔不超过五秒。完成间隔最大7.307824秒单列，不混作开始间隔或单轮耗时。容器集合最多32个（含保留退出状态的一次性容器），卷集合9个。合计容器硬配额峰值7.45 CPU／9462349824字节（8.8125 GiB），当次卷及私有目录实际分配峰值3614146560字节（约3.366 GiB）；没有OOM、非预期退出或停止线触发。镜像阶段出现一次明确文件消失，沿原四秒预算完整重扫，最终0.773223秒完成；未接受不完整字节数。

最慢样本处于`isolated_telemetry_start`，约11:37:22–11:37:26，17个容器／9个卷。容器列表2.070810秒（命令等待2.038939秒）、inspect 0.655207秒、卷列表0.111185秒、卷扫描0.582725秒、私有目录扫描0.371444秒；Docker命令等待合计3.351132秒，进程创建、输出处理及校验占比较小。该轮没有超时，不能作为历史4秒失败已复现或唯一根因已修复的证据。新证据将该轮主要耗时定位到Docker命令等待，仍不能区分CLI、daemon及虚拟机调度。只读日志交叉核对在相邻时段找到133条Docker Desktop backend记录，无固定超时／OOM标记；保留的daemon日志未覆盖本次时段，不将缺少标记当作无故障证明，也未打开全局debug或更改个人Docker配置。

标准清理和独立只读审计均确认当次容器、网络、卷、构建器容器／状态卷、十个精确产品镜像reference及私有工作目录零残留；固定MinIO镜像按已核验不可变ID保留。证据目录`/tmp/addp-p19-resource-busy-20261025/`包含window、单轮日志／report、原始分步统计、`receipt.json`、`cleanup-audit.json`、`timing-analysis.json`、`docker-log-correlation.json`、确定性门禁日志及只读`function-row-mapping.json`。报告SHA-256为`631c0fa5d60244bb97920fa939d8f666e2c686da4066a73519b0f79daa809697`，清理审计为`ce51424b732708961c518bc4e15e2ed70f7e72f2a450ed4e3c75f7c52cfa7b06`。

本次仅证明已确认繁忙模型通过，历史尖峰唯一根因仍未定；不改变既有正式身份闭环结论，不计新正式身份／页面、Hosted、容量、72小时或生产T5通过，没有设置定时。只读核对清单实际第19行／B18（存算引擎管理／服务管理／接口管理／总述）、第45行／B44（实时耗时接口追踪）及现有M列，维持部分实现；工作簿未编辑，临时解密依赖已删除。下一步优先讨论Docker CLI到daemon等待的分层取证，未经确认不再启动负载或调整四秒口径。


### 主机监控类型／纳管状态筛选与分页回跳修正（2026-10-25）

Monitor 列表复用 System 已有主机 `node_kind`／`enabled` 与名称／地址交集查询，类型／状态选择同时提交名称草稿并回首页；重置保留每页数量及刷新偏好。详情、返回、刷新和 Console 单历史保留已提交条件，指标请求仍只有匹配当前页的五项摘要，空页不请求指标。共享前端唯一 `HostNodeFilters`／`resolveHostNodeFilters` 替换 System 原控件及类型／状态解析分支，删除迁出的“全部类型／状态”词条；不新增后端接口、权限、指标、数据库或部署依赖，System API／Swagger 契约不变。

新增真实分页用例暴露原页面缺陷：切第二页后重读立即将未知总数设为零，Element Plus 的页数监听自动回写第一页。诊断实际记录 `1 → 2 → 1` 三次 System 列表请求，见 `/tmp/addp-host-list-pagination-diagnostic.log`；没有将其归因为查询慢或来源异常。修正为未知总数使用 `null`、只用成功返回的总数发布分页；不加重试、延迟或改写服务端分页。最终八项专项覆盖中英文390px组合筛选、停用字符串false、翻页与每页数量、当前页唯一摘要、空结果无指标读取、失败／旧响应／身份变化清除、自动刷新不提交草稿，以及第二页空结果／失败不回写第一页。

本轮最终命令显式将已核验的 Node `24.19.0` 加入当次 PATH（路径 `/Users/pampa/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin`），不更改个人运行环境。实际结果：

| 标准入口 | 结果及范围 |
| --- | --- |
| `make test-common-frontend` | 277项共享T1通过，无失败／跳过，含规范值、双语和唯一所有权；原System重复路径已删除。 |
| `make test-system-frontend` | 172项单测、240项受控API浏览器回归及构建通过；保留原主机管理的检索、编辑、权限与响应隔离契约。 |
| `make test-monitor-frontend` | 119项单测、436项受控API浏览器回归（16.6分钟）及构建通过；新增八项含上述分页红灯的最终回归。 |
| `make test-console-frontend` | 195项单测、222项受控iframe浏览器回归（10.1分钟）及构建通过。 |
| Console标准browser launcher，`e2e/node-resource-navigation.spec.js e2e/host-node-navigation.spec.js` | 最终六项通过，模块内导航保持文档，浏览器历史按Console既有机制恢复当前iframe和条件，不要求跨外层历史永久保持原Frame对象。 |
| `make test-frontend-ci-registration`；`changed-gate.py --dry-run` | 22项登记回归通过，21个前端登记完整；现有共享／System／Monitor／Console路径已命中，无新Make入口或workflow。干跑包含共享工作区其他owner，仅计路径发现，不计实际门禁通过。 |

保留先前失败：Select内部输入值及空态文案的测试断言错误；Console新增用例错误要求浏览器历史后原Frame不卸载，纠正为已有单历史契约后六项专项和上述完整门禁均通过。分页回跳是真实产品缺陷，已由真实请求序列定位并通过最终回归。首轮误用系统Node25的结果及主动中断轮次不计标准通过；该版本下Console曾有Vite退出缓存未清零失败，未证实根因、不因Node24后继通过宣称修复。所有旧轮均不由局部成功覆盖。

关键16项实现／测试摘要及日志摘要在 `/tmp/addp-host-list-final-receipt.json`，SHA-256 `ec59a5a6613d572360515e95daac332ab8109bef3b52b3d6ec4e9bb10f38f48c`；最终日志为 `/tmp/addp-host-list-{common-node24-final,system-node24,monitor-node24-final,console-node24-corrected-full,console-navigation-node24-corrected,frontend-registration}.log`。这些是本地共享工作区T1／受控T3／构建证据，不代表固定提交CI、个人Safari、真实System身份T4／Hosted、多真实主机或生产T5；本批未运行后端T2（后端未变）。未重启个人服务、操作个人数据库、轮询／派发／重跑CI或Hosted，也未设置定时任务。功能清单第36行仍部分实现，数据库／任务／角色／网卡关联筛选及真实运行验收保留；第37行的其他总览列与健康评分缺口不变。
