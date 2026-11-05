# Monitor 综合验收历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录跨主题汇总、分项失败、Metrics 分组与前端固定版本复核。验收结论按各记录的固定源码、环境、运行尝试和层级认定。原顺序见[Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

原始过程、逐步诊断及详细归档清单见[精简前的固定记录](https://github.com/pampa0629/addp/blob/be8f8daa4d38a7830585577f03b77bf2b5ed3931/docs/spec/testing/evidence/Monitor%E7%BB%BC%E5%90%88%E9%AA%8C%E6%94%B6%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

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
| 主机时段比较与资源告警 | 见专题 | [四轮 Hosted 记录](#时段比较与资源告警) | 缺样保护、完整样本、内存压力及受限 CPU。 |

## 平台资源与主机接入专项

本节保留[运行监控后续专题](../../../next/ADDP平台运行监控与可观测性设计.md#三验收状态与剩余缺口)原有的专项细节；上表是历史索引，本节补充预算、观测数量、身份和清理边界。记录未在本次整理中重跑或刷新状态。

## 时段比较与资源告警

| Hosted T4 场景 | 固定源码与运行 | 实际断言与清理 |
| --- | --- | --- |
| 主机相邻等长比较，缺样保护 | `68d7cbd03`（含 `ac7522bf5`）· [38983748074](https://github.com/pampa0629/addp/actions/runs/38983748074) | 三次比较为 0 项完整、3 项不完整，均命中缺样分支。来源／中心／控制面恢复、节点启停、目标停用／删除和零残留通过。 |
| 主机相邻等长比较，完整样本 | `74adc2d45` · [39082438625](https://github.com/pampa0629/addp/actions/runs/39082438625) | CPU、内存分别自然采集到 41 个有效评估点，三次 5 分钟比较均进入完整分支。原始报告独立重算、页面刷新恢复及退出零残留通过。沿原 1800 秒场景预算与浏览器期限。 |
| 内存压力告警 | `8111363d2` · [39016166744](https://github.com/pampa0629/addp/actions/runs/39016166744) | 经正式身份与 Gateway 验证真实内存压力触发、缺样时保持、恢复同一事件、HMAC 签名通知与自动重试。身份隔离和退出零残留通过。 |
| 受限 CPU 告警 | `53be389c5` · [39082008910](https://github.com/pampa0629/addp/actions/runs/39082008910) | 4 核 VM 内以 1 核硬配额加压约 96.51 秒，在固定测试阈值下触发并恢复同一事件。两条签名通知为 `delivered`，首次 503 后自动重试，实际共三次请求。身份隔离和退出零残留通过。 |

时段比较证据限于上述主机窗口。长时段、进程比较和同比分别认定。两轮告警覆盖受控压力下的完整生命周期，生产满载、容量、自动扩容及生产 T5 均未由这些运行验收。

## 分项与未通过范围

- 2026-10-15 的 `make test-module MODULE=monitor GO_ONLY=1` 记录为 T1 通过。`TestExecutionReadAPIReauthenticatesThroughOwnerForEveryRequest` 使用真实 Monitor 路由/Owner HTTP Client/共享范围 Handler 和独立身份夹具，覆盖逐请求重认证、跨/同租户、无定义本人/缺主体、父子隔离、删定义历史、列表/统计、动态撤权和 Owner503/恢复；响应不含业务载荷。故意保留旧权限时应 404 实得 200，恢复后通过；不是 System IAM、PostgreSQL T2、完整模块或新增 T4。
- Desktop 来源的独占 metrics T2 与 Monitor PostgreSQL 分项记录为通过：实际七采集器、18 设备/七原始计数及独立命名空间、五趋势、故障后新样本和零残留。第三次模块运行的 T0/Go/47 单元/61 浏览器/构建通过，但后续 backend unavailable 导致整入口失败；个人开关的端口夹具已隔离并复验，十秒生命周期超时、约三秒查询失败与此前 mTLS 超时未确认根因。最终独占重跑通过不覆盖整轮失败；个人 mTLS 诊断不等于正式页面。
- 资源告警联合 `make test-monitor-metrics` T2 使用受控 mTLS、真实 Prometheus/PostgreSQL 和临时 TLS 签名接收端，覆盖 503 自动重试、缺样不恢复、低值恢复与 outbox；同一临时 Compose 只初始化 addp_test，退出清理。System 身份是夹具，不代替正式身份 T4 或真实群通知。
- 全部早期“目录九项/十八项/二十项/二十二项”仅表示各次验收切片，当前目录与 Desktop 七采集器以正式规范为准。历史六采集器、文件系统/inode/网卡空值记录不改写为最新功能。
- 各次默认跨 Owner test-changed 预检缺依赖均未计为通过。真实多主机 TOP5、独立 Owner 撤权、全平台容量、生产宿主/物理身份与 T5 仍需各自独立证据；不把单主机 T4、六个受控来源 T2 或历史 CI 排队合并为通过。

## Metrics 原生 CI 预算与互补分组

### 原单 Job 的取消与分组验证

2026-10-22 的原生整套作业约 20 分钟后取消，涉及 [328d41941](https://github.com/pampa0629/addp/actions/runs/39604162101/job/119236588666)、[eb9](https://github.com/pampa0629/addp/actions/runs/39609484036/job/119254592962)、[441](https://github.com/pampa0629/addp/actions/runs/39612107825/job/119263766820) 和 [dc36584](https://github.com/pampa0629/addp/actions/runs/39623631812/job/119303604167)。末轮日志于 `15:28:24 UTC` 启动，PG 专业来源、认证及生命周期执行至 `15:34:13`，其后进入 HTTP、停源、主机和中心恢复。总览在 `15:48:36` 开始，作业于 `15:48:39` 取消，完整总览和资源告警尚未完成。

根入口 `make test-monitor-metrics` 保留无参数全量顺序。CI 通过同一入口分为 `postgres-source`、`http-quality`、`host-resources`，各用独立项目和 Linux Runner，维持原 20 分钟、子步骤期限、窗口、身份、权限及退出清理要求。矩阵配置 `fail-fast: false`，稳定检查 `Monitor metrics infrastructure` 要求所有选中分组实际执行并成功。

分组本地验证绑定 `667b25923fd6b25007ae55e6741a806d22367351` 及本方编排输入。分别以 `MONITOR_METRICS_TEST_ARGS=--case=<分组>` 运行三个标准入口，PG／HTTP／主机耗时 315.279／403.107／736.779 秒，均退出 0。各自清除容器、网络、卷和临时目录，另行核对三类资源均为零。本地证据限于共享主机上的三次分组运行。原生进程断言的 AST 核对仅见函数作用域及 `global base` 差异。42 项配置契约、38 项 CI 登记回归、实际 T2 登记及 `make test-workflow-security` 均通过。结果见 `/tmp/addp-metrics-partition-results.json` 和 `/tmp/addp-metrics-partition-{postgres-source,http-quality,host-resources}.log`。

### 固定版本的 Linux 三分组终态

下表各轮均为 attempt 1。三个分组的 `Verify isolated metrics lifecycle` 和稳定汇总的 `Require every selected metrics case` 均实际执行并通过。各轮独立核对原始日志、检出 SHA 与归档摘要，三个分组均有容器、网络、卷和临时文件零残留回执。

| 固定源码与 Release | `http-quality` | `postgres-source` | `host-resources` | 稳定汇总 |
| --- | --- | --- | --- | --- |
| `b100eda7b803d180e0db53297331ba618de11b67` · [39634536388](https://github.com/pampa0629/addp/actions/runs/39634536388) | [119356253850](https://github.com/pampa0629/addp/actions/runs/39634536388/job/119356253850) | [119356254038](https://github.com/pampa0629/addp/actions/runs/39634536388/job/119356254038) | [119356254095](https://github.com/pampa0629/addp/actions/runs/39634536388/job/119356254095) | [119387244768](https://github.com/pampa0629/addp/actions/runs/39634536388/job/119387244768) |
| `c7ef8ea0e69afdb75ae6d194a381a53a77b4cd3f` · [39703683536](https://github.com/pampa0629/addp/actions/runs/39703683536) | [119586364227](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119586364227) | [119586364302](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119586364302) | [119586364231](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119586364231) | [119597645628](https://github.com/pampa0629/addp/actions/runs/39703683536/job/119597645628) |
| `f09c1ecf962b7184f71a0b92f489f2a7f1fada30` · [39750726710](https://github.com/pampa0629/addp/actions/runs/39750726710) | [119734076006](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119734076006) | [119734076020](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119734076020) | [119734075933](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119734075933) | [119755672239](https://github.com/pampa0629/addp/actions/runs/39750726710/job/119755672239) |

`b100eda7b` 首次以互补分组完成原单 Job 未跑完的范围。HTTP 覆盖真实 SDK、完整窗口、签名通知恢复及停源隔离（告警 180.29 秒）。PG 覆盖真实专业指标、来源认证与安装／停用／恢复。主机分组覆盖受限来源、停源与新样本恢复、SIGKILL 后 WAL 重放、六来源总览与 TOP5，以及资源告警通知和恢复（告警 180.24 秒）。原日志 `/tmp/addp-ci-20261021-180013/job-{119356253850,119356254038,119356254095,119387244768}.log`，独立审计 `metrics-complete-independent-audit.json`。

### 当时的取消、跳过与验收边界

原 ParseAddr、停源竞态、约 20 分钟取消、准入外层 45 秒失败均按原输入保留。`01925025a` 的 [Release 39626206650](https://github.com/pampa0629/addp/actions/runs/39626206650) 虽然选择成功，Metrics Job `119318812307` 实际跳过。`7b77919f8`／39751069208 和 `610110d8`／39759525003 的 Metrics 矩阵也实际跳过。`6eb1ce457`／39772136972 在核对时仍排队，尚无 Metrics Job。只有表中实际执行的固定版本计入本节 T2，后续 Python Trace、正式身份或 PostgreSQL 专业页面 Hosted T4、72 小时留存与容量、七天自然留存及生产 T5 各自需要证据。

## 最新 Linux Metrics 三分组人工复核

2026-10-22 10:43（Asia/Shanghai）人工复核 `c7ef8ea0e` 的自动 push 运行。该版含来源 IP／SAN、停源整秒对齐、PostgreSQL 准入准备与执行分离及同模块双 Worker 输入。HTTP 通过三真实 Linux SDK 进程、双 Worker 隔离与互换拒绝、完整窗口／历史点、告警签名与恢复（165.23 秒），依次停 Backend 和一个 Worker 时仅相应来源清空。PG 三次真实准入各约 0.02 秒，并通过专业查询、来源认证和生命周期。主机分组完成受限来源、停源恢复、SIGKILL／WAL、六来源 TOP5 与资源告警（150.36 秒）。原日志、Job 元数据与 `audit.json` 在 `/tmp/addp-p19-metrics-manual-20261022/`。

当时整理证据时另以冻结 `48515ba5ecfb1064630530490c05f9c766e2c35b` 仅叠加证据，实际运行 `GOMAXPROCS=2 GOFLAGS=-p=2 make test-changed`。平台 T0 在 `TestTransferReviewSourceDrift/config` 因既有 Transfer 配置文档溯源漂移失败，后续门禁未执行。该配置文档与冻结源码一致，日志为上述目录的 `evidence-test-changed.log`。此失败与固定版本 Linux T2 成功分别认定。

## Linux Metrics 后续输入复核（f09c1ecf9）

2026-10-22 后续自动 push 的 `f09c1ecf9` 已完成上表四个 Job。HTTP 14 项 Go 用例通过，覆盖真实 Linux 三进程、停 Backend／Worker 隔离及告警生命周期（165.35 秒）。主机 14 项、PG 7 项均零 FAIL／SKIP。日志、元数据及 SHA-256 独立审计见 `/tmp/addp-p19-metrics-audit-20261022-f09/` 的 `audit.json`。

## 五项列表、热力面板与服务环比的 Linux 前端复核（2026-10-24）

专门 CI 会话回传 [Platform 39960204230 / Monitor job 120436001184](https://github.com/pampa0629/addp/actions/runs/39960204230/job/120436001184)，固定检出 `9bf8dddcbe8a589b0c7a85af6ded4bb44485a87e`，push、attempt 1。独立核对原始日志的实际 `make test-monitor-frontend`、检出 SHA、完整 411 条浏览器执行序号和最终汇总：112 项单元测试通过、失败／取消／跳过均为 0；411 项浏览器全部通过（17.9 分钟）；构建通过（7.06 秒），Monitor frontend gate 为 selected／success。包含中英文 20 台主机精确五项批量摘要、默认每页 20 台及独立状态、四项热力面板用例和十五项服务进程比较用例。

固定版本的 `monitor/frontend/e2e/process-comparison.spec.js` 与 `d7ec3e9ffac1761c60fd0a5f76dc0063a6354398` 字节一致，SHA-256 为 `4e6560c91f6a1ab2e8c72294776296a2918391f6f1817b987eca8c5f2b378954`。复核关闭环比时先挂起实际 `process_resource_summaries` 响应，断言旧四次趋势请求与 `23:50:15` 起点仍保留，释放后等待第五次请求再断言 `23:55:15`；这项同步修正已在后继完整 Linux 门禁通过。原 `35f` 失败记录保留，原 `d7ec` Platform 39952965397 已取消，不计通过，亦不重复派发已修正事项。

原始日志 `/tmp/addp-ci-manual-20261023-155348/job-120436001184.log` SHA-256 为 `e1ccc444f59ff2203f8bc8e1453fdba19ce55f36a5d216573bf8d6e14aed49e8`；同目录 `Monitor-9bf-frontend-independent-audit.json` 为回传审计，`Monitor-9bf-domain-audit.json` 为本次领域核对。核对使用原始日志哈希、`git show` 固定源码字节比较及执行项断言，没有重跑或查询／调度 CI、Hosted，也未操作个人服务或设置定时。这是受控 HTTP 下的前端 T1／T3／构建证据，不替代新摘要实源或正式身份 T4、长窗、容量及生产验收，不外推当前共享 HEAD。父 Platform 的无关 FTP 失败不在本会话修复范围，不将该 workflow 写为全绿；该次核对时个人 Safari 右侧两列截图与前台自动刷新尚未完成，后续实景结果见下一节。

## Monitor 前端整套门禁超时复核（2026-11-01，0e69a25b1）

专门会话回传固定源码 `0e69a25b17e18fbb9a6a0b29b88c0827eb50d714` 的 [Platform run41304702173／Monitor job124842615491](https://github.com/pampa0629/addp/actions/runs/41304702173/job/124842615491)。已只读核对归档job／annotations／receipt与原始日志哈希 `02abf702b4c2d3df615ccdb9b386b49a252215f2a9dc345224c1e5c50d0ad826`，三者源码一致。Job为completed/cancelled，annotation明确超过30分钟最大执行时间；不是人工取消，不计整套门禁通过。

日志131项单测通过。浏览器声明561项、单Worker，连续启动序号为1至560；最后启动uptime-units的中文用例，未启动561、没有最终浏览器成功汇总，后续build未执行。仅凭启动序号不能为任一新增场景或整套补认成功。最大相邻启动间隔12.84853秒，未见断言失败块；不能把最后打印位置推断为该用例挂死或产品回归。

耗时拆分：checkout为12:10:40—12:15:32 UTC（292秒），含全分支／标签历史fetch；正式门禁步为12:15:59—12:40:53（1494秒）。浏览器首项12:16:04.025356、末项12:40:50.612102，启动跨度1486.586746秒。证据说明仓库获取与大套串行运行共同耗尽30分钟上限；不证明冷checkout的具体网络／对象耗时根因，也不将“接近跑完”算作通过。后续应先核对如何减少仓库获取开销、保留固定事件before/head及完整变更选择；当前不改预算、Worker数、断言、用例集合或共享CI编排。

原始文件在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-line-nodes-q5rkxkgn/terminal-ci/124842615491/`，独立有界审计为 `/tmp/addp-monitor-0e69-frontend-timeout-audit.json`。审计脚本验证日志哈希、固定SHA、序号连续性和时间差；本次证据增量另经 `git diff --check`。未查询／重跑CI、Hosted或容量验收，未设置定时任务；不把本地Redis专项证据升级为该旧输入的整套Linux前端通过。

## Redis 必填认证与 MinIO 隔离构建闭包修复（2026-11-01）

[MinIO Job 124825429970](https://github.com/pampa0629/addp/actions/runs/41298861954/job/124825429970) 固定 `4803bb7221f4dc505aea8dba8b097469c47ecc0a`。原日志 `/tmp/addp-ci-20261101-200009/job-124825429970.log` 第 650—665 行显示 builder 构建通过，随后正式 Infra Compose 的 MinIO build 在插值阶段因缺 `REDIS_PASSWORD` 失败，cache-save 跳过。

修复沿 `minio-build-cache.py`，由每个 Compose 子进程独立生成 `INFRA_FALKORDB_PASSWORD` 和 `REDIS_PASSWORD` 的随机解析值。值只供插值，隔离继承的部署密码，保留父环境、正式 Redis 必填认证及原 Compose 构建入口。构建登记同时补齐 `Dockerfile.redis-metrics`，按已有辅助镜像规则校验。

- **脚本回归**。`make test-infra-minio-cache` 先复现继承 Redis 输入，修正后 15 项通过，覆盖子进程值隔离、参数及输出保密、冷／热缓存和失败传播。日志 `/tmp/addp-minio-redis-input-red.log`、`/tmp/addp-minio-redis-input-final.log`。
- **原生 Compose 解析**。`/tmp/addp-minio-compose-input-proof.py` 以真实 Compose／extends 副本和 `--env-file /dev/null` 运行 `docker compose config --format json`。修前复现缺变量，修后完整解析、两个值独立且未进入 MinIO 构建定义，临时目录已删除。警告与 JSON 分离、macOS 路径规范化的两次审计失败保留。日志 `/tmp/addp-minio-compose-input-red.log`、`/tmp/addp-minio-compose-input-green.log`。
- **构建登记**。`make test-build-registration` 的缺文件反例先失败，修后 60 项登记、9 项选择及仓库核查通过，26 个 Go 服务／Worker 与 59 个产品镜像一致。日志 `/tmp/addp-redis-observer-build-registration-red.log`、`/tmp/addp-minio-redis-build-registration-final.log`。

### 92dcfe06f 的 MinIO Linux 实际构建回传

[MinIO Job 124862692691](https://github.com/pampa0629/addp/actions/runs/41311505167/job/124862692691) 为 push／attempt 1，固定 `92dcfe06fdfbaf98884946b76b06aa476472bc5f`，已选中且实际成功。

| 核对项 | 该次结果 |
| --- | --- |
| 镜像构建 | builder、正式 Infra Compose 构建、离线 MinIO／mc 二进制版本和摘要、两个预期镜像身份均核对通过。 |
| 同 Runner 暖构建及缓存保存 | `ordinary_compose_cached=true`。原样 Compose 缓存核验、inline 元数据导出、image save、Actions Cache save 和 artifact upload 均通过。 |
| 跨运行缓存恢复 | 本次 `cache_hit=false`，只覆盖当次冷准备和同 Runner 暖构建。新 Runner 恢复及业务 Hosted 另行验收。 |

原日志 `/tmp/addp-ci-20261101-220010/pass-job-124862692691.log` 的 SHA-256 为 `2fbee89b572afbc5f1e3920b1e12723a85d4e4c8a608ab3a1fcb0ad54b5cee13`，审计见 `/tmp/addp-ci-20261101-220010/MinIO-92dc-actual-Linux-proof.json`。精确键恢复、镜像身份与热构建判定见 [CI 与交付规范](../CI与交付规范.md#ci-编排与登记)。

## 独立引擎 CLI 临时树依赖闭包修复（2026-11-01）

[Platform Job 124862693255](https://github.com/pampa0629/addp/actions/runs/41311505152/job/124862693255) 同样固定 `92dcfe06f`。`make test-platform → test-online-runner → test-node-metrics-online-runner` 在 162 项中的两项 `EngineSourceCommandTest` 失败。独立 engine-up 返回 1，部分启动失败用例缺 owner 文件。原日志 `/tmp/addp-ci-20261101-220010/job-124862693255.log`。

本地沿同一入口复现两项失败（`/tmp/addp-platform-engine-cli-red.log`）。临时 CLI 树的认证子进程在发布 owner 前加载 `private-state.py` 时触发 `FileNotFoundError`，该树也漏了退出清理所需的 `deployment-docker.py`。修复提交 `7347b36c808358abc3665419a1b872a0dc8cfafb` 仅补复制这两份 `scripts/utils/` 真实依赖，保留原归属、错误脱敏及清理断言。

- **本地标准聚合**。初轮另有 LibreSSL 3.3.6 缺少 `-addext` 的六项环境错误。最终仅对命令前置 OpenSSL 3.6.4，执行 `PATH=/opt/homebrew/opt/openssl@3/bin:$PATH make test-node-metrics-online-runner`，162 项及 System Metrics 身份 Go 夹具通过。原用例继续验证无 `PYTHONPATH` 启动／重启发现、失败后保留 owner、错误 run／attempt 与外来 service 拒绝且不回收，以及恢复本方归属后清理至零个模拟资源。日志 `/tmp/addp-platform-engine-cli-green.log`。
- **冻结输入复核**。仓库外仅叠加本项修复，两项 `EngineSourceCommandTest` 在 4.076 秒内通过，退出删除冻结目录。日志 `/tmp/addp-platform-engine-cli-frozen.log`。

本节只归档 CLI 夹具的本地复验。原 Platform Job 仍为失败，后继自然 Platform 终态及真实 Hosted 结果需要独立证据。

## 后续本机浏览器三项失败的独立诊断（2026-11-02）

| 验证范围 | 固定输入与实际结果 | 证据 |
| --- | --- | --- |
| 本机原完整门禁 | `22f8b8ef6fdc3e5ced8974cbccf90cd469deba51` 叠加拓扑工作区改动。132 项单元通过，浏览器 589／592 通过、3 项失败，构建未执行。首次中断与后续诊断失败单列。诊断证实休眠干扰，原失败缺少 trace，具体停顿点无法复原。 | [原失败、休眠诊断与清理范围](Monitor部署与前置诊断历史证据.md#本机-monitor-浏览器超时与系统休眠诊断2026-11-02) |
| 稳定唤醒后本机复验 | `39059d2ed2d711b992947c1debee6872a0d60578`。沿原入口复验三项，3／3 通过，耗时 13.6 秒。标准 runner 清理当次服务、缓存和端口租约，成功 trace 单独保留。 | [三项复验与运行回执](Monitor部署与前置诊断历史证据.md#用户确认稳定唤醒后的原三项本机复验2026-11-02) |
| 固定 Linux 完整门禁 | `3f281d49c7c3533a347db4a270d090cabfd63301`。[Monitor 前端 Job 125102562739](https://github.com/pampa0629/addp/actions/runs/41391135443/job/125102562739) 的 592 项浏览器用例、Vite 构建及整 Job 均通过，覆盖原三项。 | [固定源码、原始日志与历史取消](Monitor部署与前置诊断历史证据.md#后续-linux-完整前端门禁回传2026-11-02) |

原本机整套门禁仍记失败。本机复验只覆盖三项受控 T3，Linux 结论限于固定 Job。真实 Safari、Redis／MinIO 来源及 Hosted 另按各自证据认定。
