# Monitor 综合验收历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录跨主题汇总、分项失败、Metrics 分组与前端固定版本复核。固定源码、环境、失败记录、修复结论与未覆盖范围以各记录为准；本次仅整理文档，没有重跑验收或刷新 CI 状态，不推导当前 HEAD 或生产 T5 已通过。跨主题语境与原有顺序见 [Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

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

## 时段比较与资源告警

完整样本比较已通过 [Hosted 专项](https://github.com/pampa0629/addp/actions/runs/39082438625)（源码 `74adc2d45`）：自然采样形成 CPU/内存各 41 个有效评估点，三次 5 分钟比较均进入完整分支，原始报告独立重算、页面刷新恢复及退出零残留通过。原 1800 秒场景预算和浏览器期限保持不变；该专项与 CPU、内存压力分别验收，不推定长时段或进程比较已经验证。

资源告警已分别通过[真实内存 T4](https://github.com/pampa0629/addp/actions/runs/39016166744)（源码 `8111363d2`）和[受限 CPU T4](https://github.com/pampa0629/addp/actions/runs/39082008910)（源码 `53be389c5`）。内存覆盖打开、缺样保持、恢复及签名重试；CPU 在 4 核 VM 以 1 核硬配额加压约 96.51 秒，固定测试阈值下触发并恢复同一事件，两条通知 delivered，首次 503 后自动重试共三次请求。两轮均验证正式身份隔离与退出零残留。该证据不代表生产满载、容量评估、自动扩容或生产 T5 已验收。

## 分项与未通过范围

- 2026-10-15 的 `make test-module MODULE=monitor GO_ONLY=1` 记录为 T1 通过。`TestExecutionReadAPIReauthenticatesThroughOwnerForEveryRequest` 使用真实 Monitor 路由/Owner HTTP Client/共享范围 Handler 和独立身份夹具，覆盖逐请求重认证、跨/同租户、无定义本人/缺主体、父子隔离、删定义历史、列表/统计、动态撤权和 Owner503/恢复；响应不含业务载荷。故意保留旧权限时应 404 实得 200，恢复后通过；不是 System IAM、PostgreSQL T2、完整模块或新增 T4。
- Desktop 来源的独占 metrics T2 与 Monitor PostgreSQL 分项记录为通过：实际七采集器、18 设备/七原始计数及独立命名空间、五趋势、故障后新样本和零残留。第三次模块运行的 T0/Go/47 单元/61 浏览器/构建通过，但后续 backend unavailable 导致整入口失败；个人开关的端口夹具已隔离并复验，十秒生命周期超时、约三秒查询失败与此前 mTLS 超时未确认根因。最终独占重跑通过不覆盖整轮失败；个人 mTLS 诊断不等于正式页面。
- 资源告警联合 `make test-monitor-metrics` T2 使用受控 mTLS、真实 Prometheus/PostgreSQL 和临时 TLS 签名接收端，覆盖 503 自动重试、缺样不恢复、低值恢复与 outbox；同一临时 Compose 只初始化 addp_test，退出清理。System 身份是夹具，不代替正式身份 T4 或真实群通知。
- 全部早期“目录九项/十八项/二十项/二十二项”仅表示各次验收切片，当前目录与 Desktop 七采集器以正式规范为准。历史六采集器、文件系统/inode/网卡空值记录不改写为最新功能。
- 各次默认跨 Owner test-changed 预检缺依赖均未计为通过。真实多主机 TOP5、独立 Owner 撤权、全平台容量、生产宿主/物理身份与 T5 仍需各自独立证据；不把单主机 T4、六个受控来源 T2 或历史 CI 排队合并为通过。

## Metrics 原生 CI 预算与互补分组

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


## 最新 Linux Metrics 三分组人工复核

2026-10-22 10:43（Asia/Shanghai），按用户要求仅手动核对现有自动运行，没有设置定时、重跑门禁、手工触发 Hosted 或重跑容量。最新已完成且实际执行三个 Metrics 分组的结果为精确源码 `c7ef8ea0e69afdb75ae6d194a381a53a77b4cd3f` 的 [Release 39703683536](https://github.com/pampa0629/addp/actions/runs/39703683536)，attempt 1、push 触发；包含既有来源 IP/SAN、停源整秒对齐、PostgreSQL 准入准备/执行分离和同模块双 Worker 测试输入。

| 分组 / 汇总 | 实际 Job | 人工复核结果 |
| --- | --- | --- |
| `http-quality` | [119586364227](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119586364227) | 标准 `Verify isolated metrics lifecycle` 成功；三真实 Linux SDK 进程及双 Worker 隔离/互换拒绝、HTTP 完整窗口与历史点、告警触发/签名通知/恢复均通过（告警 165.23 秒）；依次停止 Backend 和一个 Worker 后仅对应来源清空，其余 Worker 保持有效。 |
| `postgres-source` | [119586364302](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119586364302) | 同一步骤成功；三次正式来源准入测试实际进入并通过（各 0.02 秒），完整专业查询、来源认证及生命周期通过，新增冷准备与实际执行分离已由原生 Linux 路径覆盖。 |
| `host-resources` | [119586364231](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119586364231) | 同一步骤成功；主机/受限来源、停源恢复、监控中心 SIGKILL/WAL 重放、六来源总览/TOP5 与资源告警生命周期通过（资源告警 150.36 秒）。 |
| 完整汇总 | [119597645628](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119597645628) | `Require every selected metrics case` 成功，三个已选分组全部实际执行并通过，未用条件跳过代替。 |

GitHub API 确认四个 Job 均为 `completed/success`；四份原始日志的 checkout 均为上述 SHA，三个分组末尾均明确 `zero owned containers/networks/volumes and temporary files`。原始日志、Job 元数据及独立哈希审计保存在 `/tmp/addp-p19-metrics-manual-20261022/`，审计为 `audit.json`。这是复核既有 Linux T2 的结果，没有重新执行测试；文档增量通过 `git diff --check`。在仓库外冻结 HEAD `48515ba5ecfb1064630530490c05f9c766e2c35b`、仅叠加本段证据的标准 `GOMAXPROCS=2 GOFLAGS=-p=2 make test-changed` 实际选择平台 T0，但在 `TestTransferReviewSourceDrift/config` 因既有 `transfer/docs/transfer-基本概念及配置说明.md` 语义/溯源漂移而失败，未执行完后续门禁；该文档与冻结 HEAD 逐字节一致，本次未修改，不计 T0 通过，也不扩修无关内容。日志为审计目录的 `evidence-test-changed.log`；此结果与上表既有 Linux Metrics T2 成功分别记录。

此前 ParseAddr、停源竞态、20 分钟取消和准入外层 45 秒失败仍按其原输入保留，不追溯改成通过。本次关闭上述 `c7ef8ea0e` 输入的完整 Linux Metrics T2 缺口；核对时更晚 main 的运行仍有排队或 Metrics 未选中，不能扩大为最新共享工作区、后续前端改动、额外 Hosted T4、72 小时留存/容量、生产 T5 或其它 Owner CI 全部通过。


## Linux Metrics 后续输入复核（f09c1ecf9）

2026-10-22 按用户要求再次手动核对本专项 CI。精确源码 `f09c1ecf962b7184f71a0b92f489f2a7f1fada30` 的 [Release 39750726710](https://github.com/pampa0629/addp/actions/runs/39750726710)，push、attempt 1，已完整实际执行三组：`http-quality` [119734076006](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119734076006)、`postgres-source` [119734076020](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119734076020)、`host-resources` [119734075933](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119734075933) 的 `Verify isolated metrics lifecycle` 均成功，稳定汇总 [119755672239](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119755672239) 的 `Require every selected metrics case` 成功。汇总实际为 selected=true、选择及分组结果 success，未用条件跳过代替。

四份原始日志 checkout 均为上述 SHA，前三日志均明确当次容器、网络、卷及临时文件零残留；HTTP 十四项 Go PASS（告警生命周期 165.35 秒、真实 Linux 三进程、停 Backend/Worker 隔离）、主机十四项及 PG 七项均零 FAIL/SKIP。日志、元数据及 SHA-256 独立审计在 `/tmp/addp-p19-metrics-audit-20261022-f09/`，索引 `audit.json`。父会话复核日志哈希、执行项和清理回执，不重复执行测试；此前失败与取消按原输入保留。

较后 `7b77919f8`／39751069208 及 `610110d8`／39759525003 的 Metrics matrix 实际跳过，不能当作新 T2；核对时 `6eb1ce457`／39772136972 仍排队且尚无 Metrics Job。本次仅关闭 f09c1ecf9 输入的 Linux Metrics T2 待收口，不扩为当前共享 main、后续 Python Trace、额外 Hosted 或容量通过；未设置定时、手工触发或重跑。


## 五项列表、热力面板与服务环比的 Linux 前端复核（2026-10-24）

专门 CI 会话回传 [Platform 39960204230 / Monitor job 120436001184](https://github.com/pampa0629/addp/actions/runs/39960204230/job/120436001184)，固定检出 `9bf8dddcbe8a589b0c7a85af6ded4bb44485a87e`，push、attempt 1。独立核对原始日志的实际 `make test-monitor-frontend`、检出 SHA、完整 411 条浏览器执行序号和最终汇总：112 项单元测试通过、失败／取消／跳过均为 0；411 项浏览器全部通过（17.9 分钟）；构建通过（7.06 秒），Monitor frontend gate 为 selected／success。包含中英文 20 台主机精确五项批量摘要、默认每页 20 台及独立状态、四项热力面板用例和十五项服务进程比较用例。

固定版本的 `monitor/frontend/e2e/process-comparison.spec.js` 与 `d7ec3e9ffac1761c60fd0a5f76dc0063a6354398` 字节一致，SHA-256 为 `4e6560c91f6a1ab2e8c72294776296a2918391f6f1817b987eca8c5f2b378954`。复核关闭环比时先挂起实际 `process_resource_summaries` 响应，断言旧四次趋势请求与 `23:50:15` 起点仍保留，释放后等待第五次请求再断言 `23:55:15`；这项同步修正已在后继完整 Linux 门禁通过。原 `35f` 失败记录保留，原 `d7ec` Platform 39952965397 已取消，不计通过，亦不重复派发已修正事项。

原始日志 `/tmp/addp-ci-manual-20261023-155348/job-120436001184.log` SHA-256 为 `e1ccc444f59ff2203f8bc8e1453fdba19ce55f36a5d216573bf8d6e14aed49e8`；同目录 `Monitor-9bf-frontend-independent-audit.json` 为回传审计，`Monitor-9bf-domain-audit.json` 为本次领域核对。核对使用原始日志哈希、`git show` 固定源码字节比较及执行项断言，没有重跑或查询／调度 CI、Hosted，也未操作个人服务或设置定时。这是受控 HTTP 下的前端 T1／T3／构建证据，不替代新摘要实源或正式身份 T4、长窗、容量及生产验收，不外推当前共享 HEAD。父 Platform 的无关 FTP 失败不在本会话修复范围，不将该 workflow 写为全绿；该次核对时个人 Safari 右侧两列截图与前台自动刷新尚未完成，后续实景结果见下一节。
