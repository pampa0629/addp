# Monitor 告警与规则：验收结论与待办

[返回 Monitor 证据入口](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本页只保留仍影响后续开发或验收的结论、限制和待办，每个主题保留少量代表性证据。逐轮构建、排队、取消、已解决失败、临时收据和重复复验已从正文删除，追溯时查 Git 历史。固定版本通过不代表当前 HEAD 或其他验收层级通过。

现行规则见[平台告警与通知](../../addp平台运行观测规范.md#七平台告警与通知)及[Transfer 告警契约](../../tasks/Transfer同步执行.md#通用执行告警规则与统一评估)；本页不重复维护权限、阈值及接口规则。

[检索与列表](#alert-evidence-name) · [服务组失联](#alert-evidence-availability) · [文件系统容量](#alert-evidence-filesystem) · [CPU／RSS](#alert-evidence-process) · [CDC 风险与性能](#alert-evidence-cdc) · [待办](#alert-evidence-todos)

<a id="alert-evidence-name"></a>
<a id="alert-evidence-navigation"></a>

<!-- 保留仍有引用的旧入口。 -->
<a id="平台告警自动筛选与字段提示2026-10-29"></a>

## 检索、排序与页面

**结论**：告警名称、当前主机名称、级别和首次发现时间可以组合筛选，计数／分页前在服务端取交集；严重且未恢复的事件优先。告警名取首次触发证据，主机名取当前授权的主机事实，两者不能混用。自动筛选、未提交输入取消、Console URL 提交完成后的刷新恢复已有受控测试。

| 代表性证据 | 已验证范围 |
| --- | --- |
| `a6048b9e71b0689893a3cf90db5a0ef4822d8866`；[名称查询 PG Job 120933711310](https://github.com/pampa0629/addp/actions/runs/40110942812/job/120933711310) | 真实 PostgreSQL 名称查询、三评估器首次名称冻结、改名／删除后历史不回填通过；不代表真实指标触发或页面验收。 |
| `a2928b9a76a749f0ca5002754c2bc12d118e31fe`；[Monitor Job 120975073843](https://github.com/pampa0629/addp/actions/runs/40128534338/job/120975073843) | 名称检索的完整 Monitor 前端门禁通过。 |
| `994db320ae7147cb0e566db4960eb256306cc9e1`；[Monitor Job 121040604365](https://github.com/pampa0629/addp/actions/runs/40150409107/job/121040604365) | 当前主机名称、分页与导航恢复的完整前端门禁通过。 |
| `226d1ad73564b9e681b9f0153f294132b92c2d3d`；[Console Job 123746756104](https://github.com/pampa0629/addp/actions/runs/40959069552/job/123746756104) | 219 单元、270 受控浏览器及构建通过，包含 API 先返回但公开 URL 尚未提交时的刷新隔离；请求结束不能代替导航提交完成。 |

**实际页面边界**：个人 Safari 验过已有日志告警的名称／时间组合、排除与清除恢复、普通组跨页排序及前台自动筛选。样本没有严重未恢复事件或主机资源告警，不能据此宣称严重置顶、正向主机名命中、同时间 ID 排序或全部权限组合已实操；这些相应断言以 SQL／受控页面证据认定。

<a id="alert-evidence-availability"></a>

<!-- 保留仍有引用的旧入口。 -->
<a id="2026-10-25-服务组失联规则与持久确认"></a><a id="重启后的个人环境只读核对2026-10-25"></a><a id="safari服务失联规则保存复验2026-10-26"></a><a id="服务失联hosted专项准备与本地runner验证2026-10-26"></a>

## 服务组失联

**结论**：受控组事实与真实 PostgreSQL 已验证首次在线确认、连续全组失联打开、在线恢复同事件、未知保持、冻结条件、显式订阅和 outbox 去重；本地完整前端及构建通过。实际资格依赖部署预期和首次运行确认，不由历史登记推断。现行规则见[服务组失联规范](../../addp平台运行观测规范.md#711-服务组失联规则)。

**未完成**：个人 Safari 只验规则保存、启用、持久重读和在线评估，历史实操结束留有一条启用临时规则，未记录后续删除。Hosted `service-availability` 只有本地 runner 准备证据，没有真实 Linux 租约失联／恢复、通知实收及退出清理通过证据。

<a id="alert-evidence-filesystem"></a>

<!-- 保留仍有引用的旧入口。 -->
<a id="文件系统容量告警首期2026-10-27"></a><a id="主机挂载行创建容量告警入口2026-10-27"></a><a id="主机挂载行查看已有容量规则入口2026-10-27"></a><a id="挂载缺样断言修复的完整ci闭环2026-10-28"></a><a id="文件系统容量告警受控源实链t2复验2026-10-28"></a><a id="挂载规则入口的个人safari前置核对2026-10-27"></a>

## 文件系统容量

**结论**：精确挂载身份的规则、缺样不恢复、冻结证据及挂载行创建／只读查看入口已有 PG／受控页面验证。唯一表单拥有写入；查看已有规则不要求当前容量样本或创建权限。规则正文见[容量规范](../../addp平台运行观测规范.md#文件系统容量规则)。

**代表性证据**：固定基底 `41024f0800a646ee613f53f518673de2c494527e` 叠加两脚本的 `--case=filesystem-alerts` 本地 T2 通过。受控 Linux 高／低容量经真实 Prometheus、Monitor、独占 PostgreSQL 和签名 HTTP，完成打开、缺样保持及原事件恢复；2 次通知送达、3 次 HTTP，退出资源清零。前端后继 `7ebf98370ebc965e09cb5744983dbf8ee7fbbbce` 的[Job 122257072557](https://github.com/pampa0629/addp/actions/runs/40523616999/job/122257072557)通过 130 单元／488 浏览器及构建。

**限制**：上述是受控容量源，不是真实主机磁盘压力。个人 Desktop 未提供文件系统挂载行，实际挂载入口、原生 Collector、正式身份 Hosted 和生产 T5 未由这些证据覆盖。

<a id="alert-evidence-process"></a>

<!-- 保留仍有引用的旧入口。 -->
<a id="服务实例-cpurss-告警原生-linux-固定版本复核2026-10-23"></a>

## 服务实例 CPU／RSS

**结论与证据**：固定 `dfa4ad16105d0421af13e55f3e4099acdd6751a2` 的 [Linux T2 Job 120318541196](https://github.com/pampa0629/addp/actions/runs/39908130012/job/120318541196)通过真实 SDK／Prometheus／PostgreSQL 告警链路。受限 CPU／内存负载下两个事件打开，释放后原事件恢复；四条 opened／resolved 成功投递，每条首次 503 后重试，签名身份和正文一致；其他进程继续采集，规则及设施清零。CPU 保持完整一分钟历史，不用不足持续窗口的样本向上取整。现行行为见[实例规则](../../addp平台运行观测规范.md#服务实例-cpu-与-rss-超限规则)。

**限制**：System 身份仍是 T2 夹具，不代表正式密码／MFA 页面 T4、容量或 T5。个人 Safari 仅确认弹窗及真实实例候选可见，未保存／启用规则或验证真实告警；早期窗口显示异常未取得稳定根因。

<a id="alert-evidence-cdc"></a>

<!-- 保留仍有引用的旧入口。 -->
<a id="可选积压告警与后继-cdc-完整-hosted-通过2026-11-15"></a><a id="cdc-性能后继风险重建与-scheduled-完整-hosted-通过2026-11-14"></a>

## CDC：不可恢复风险、捕获失败与持续性能

**已验结论**：PostgreSQL 同一隔离套件已完成源槽缺失／缓冲恢复范围丢失、捕获持续失败，以及消费停滞、捕获延迟、可选积压阈值的实际故障闭环。已解决的权限发布、健康事实落库、探测握手截止时间和控制／观测写入冲突不再逐轮记账；行为由[风险契约](../../tasks/Transfer同步执行.md#cdc-任务不可恢复风险告警2026-11-13-已确认)、[捕获失败](../../tasks/Transfer同步执行.md#cdc-捕获持续失败告警2026-11-14-已确认)及[性能规则](../../tasks/Transfer同步执行.md#cdc-任务持续性能告警2026-11-14-已确认)维护。

**关键证据**：固定 `6681876925f19a275099519c12b4e9832ceb7121` 的 [Hosted 43610830710／Job 132495296433](https://github.com/pampa0629/addp/actions/runs/43610830710/job/132495296433)实际 gate、suite 和外层均通过，23 项检查、7 份真实 Console 回执完成；归档 `14122462773` 已核对下载摘要及 ZIP CRC。

- **阈值与身份**：只为当轮任务／generation 配置可选积压阈值 256，实际积压窗口约 314 秒，满足 300 秒；两条默认性能告警也以真实推进样本满足 180 秒。捕获失败实测超过 60 秒；不能用目标消费暂停冒充捕获失败。
- **暂停、缺样与恢复**：正式 Pause／Resume 保持当前 generation，暂停消费类评估并清空待定窗口，捕获类独立评估；新鲜缺样后 32 秒内 16 次检查保持原已确认事件和最后观测时间。恢复后积压严格低于半阈值持续约 75 秒，原三条性能事件关闭，未新建事件替代恢复。
- **通知与业务结果**：性能 6 条、捕获失败 2 条、不可恢复风险 4 条，共 12 条唯一签名 Webhook 实收、HTTP 202，与正式 delivered 审计匹配。不可恢复风险经正式 Stop 结束，随后新任务／新目标完整快照重建；不能理解为丢失数据自动恢复。三字段精确物理行集、后继 scheduled basic→deep、父执行／原发起人读取和对等用户拒绝通过。
- **清理**：故障与领域清理通过，规则、临时源记录、锁、CPU、Connector 配置和 Webhook 清理／恢复，任务正式删除并读回不存在，owned 槽及 publication 清理，`uncertain_mutation=false`。原硬退出 Kafka 任务按场景留给部署销毁，领域预期保留 1 项；外层最终零残留，不能写成领域零项。个人任务和原逻辑槽未操作。

**仍有效的证据边界**：真实捕获故障覆盖 Connector PAUSED，Task FAILED、连接失败、59 秒不触发等变体来自确定性测试；7 份 Hosted 页面回执验证任务指标，不等于告警 UI 全部交互已做正式实操。其他源类型、其他通知渠道、HA 和生产 T5 不随该版本升级为已验收。

### 触发前暂停与评估器重建

固定 `cfb49acf10fe80134b01f6d904765a8a5a53139e` 的 [Monitor PG15 Job 132657334331](https://github.com/pampa0629/addp/actions/runs/43662027612/job/132657334331)实际通过 `TestIntegrationPostgresCDCPerformanceRules`；[Go workspace Job 132653230261](https://github.com/pampa0629/addp/actions/runs/43662027629/job/132653230261)的 Monitor 包通过。真实 PG 中验证触发前暂停清空窗口、无事件／通知、重建评估器后重新累计完整窗口。专项入口为 `make test-monitor-postgres MONITOR_POSTGRES_TEST_ARGS=--case=cdc-performance`，默认完整 PG 范围未缩小；当批完整入口的 HTTP CAS 失败不由定向成功抵消。

该固定源码的评估器重建只认受控 Owner 事实的 T2；触发前正式 Pause／Resume 由[后续新源码 Hosted](#cdc-pretrigger-pause-hosted-20261115)单独补证，[暂停清窗后重建的完整 Hosted](#cdc-evaluator-rebuild-hosted-20261117)再按其固定源码另认定，不追认旧轮。后续[七天历史 Hosted](Monitor执行监控与留存历史证据.md#七天-cdc-任务历史的真实-hosted-断言通过2026-11-15)另认定，不代替此场景。

### 触发前暂停的实故障 T4 准备（2026-11-15）

沿同一 PostgreSQL 性能故障加入消费停滞／可选积压的 pending→Pause→Resume：先读到至少 30 秒的真实 pending 和目标锁阻塞下的源端积压，确认尚无 incident／event／投递或签名接收；暂停后两条消费规则 inactive、待触发／恢复窗口清空并保持 16 秒；恢复创建新 execution，generation 不变，原 180／300 秒阈值不变。恢复期间以同一个只读 SQL 快照核对窗口与生命周期计数，要求各自接近原阈值前仍无事件／投递，最后的不足窗口与实际打开窗口必须有同一新 trigger_since。捕获延迟继续独立评估；不重启 Monitor，评估器重建仍只认上方 T2。

复用隔离 Infra owner 的精确 PostgreSQL 容器、既有只读 helper 和正式租户 API，不新增权限、产品接口、任务或探针；只返回本 Tenant／Task／generation／规则／目的地的安全窗口及计数。新 pending 观测替换原 75 秒预热空等，保留原打开后的暂停、缺样、恢复、通知、图表、风险、重建、scheduled 及清理；scenario 1450 秒、分发 1500 秒保持。

冻结检出 `3f812eb686a76e9188943a27189c909aa8aaaa06` 仅叠加本批 Runner／测试／场景文档后，`make test-transfer-continuous-online-runner` 通过 145 项及既有语法、System 身份夹具 Go（`/tmp/addp-cdc-pretrigger-runner-final-isolated.log`）。反例覆盖越权／错规则快照、负计数／私有字段、已有生命周期、暂停保留窗口、恢复复用旧窗口、提前激活／投递，以及缺失临界前有效样本；恢复后的合法 unknown 状态不能冒充有效累计。初版替身阶段和无效时间样本失败原件保留在同前缀 Runner 日志，不计为产品或 Hosted 失败。

额外 `make test-t2-ci-registration` 在既有 Common PG 替身 `test_full_gate_cannot_pass_using_only_the_previous_execution_cases` 的 15 秒子进程超时，87 项中 1 项错误；后续 10 项和登记比较未执行，不计通过，也未修复或重跑该范围。原件 `/tmp/addp-cdc-pretrigger-registration.log`。本批没有新增入口、服务或 CI 路线，既有 CDC Runner 与手工 Hosted 发现已覆盖。此时新源码真实 T4 尚未执行，不能以确定性测试或旧轮通过追认。

### 新增触发前暂停首次 Hosted 失败及参照点修订（2026-11-15）

固定 `501943a075bd328d7033ee82f384fd08d90d1120` 的 [Hosted 43703210703／Job 132778634342](https://github.com/pampa0629/addp/actions/runs/43703210703/job/132778634342) 停在 performance_pending，尚未正式 Pause。三条规则的 incident／event／delivery 均为 0；消费停滞窗口约 90.091 秒、积压窗口约 44.999 秒，均 pending／inactive。锁前 baseline committed=10（10:36:05Z），实际停滞窗口已在 committed=11 上累计（10:36:21Z 至 10:37:51Z）。新加的直接 buffer 核对错误地要求跨这两个阶段与旧基线完全相等；同一有效窗口内停滞不要求锁前采样也有同一位点。最后一次 buffer 未在失败前保存，因此本轮不能认定该条件是唯一失败原因，也不推断具体业务／控制事件。

修订在校验前保存公开安全 buffer，仍要求新鲜 available、实际 offset 积压 >256、目标锁已建立、真实 pending 至少 30 秒、原阈值及完整生命周期零值，只移除跨阶段 committed 恒等的错误前提。正向编排回归明确让初始位点 0、实际 pending 位点 1，避免把锁前正常推进误报为告警错误；恢复窗口及临界前断言不放宽。没有修改产品状态或通过重试隐藏该次失败。

该轮故障／领域 cleanup=passed、uncertain_mutation=false；CPU、Connector、规则、源记录及 Webhook 已恢复／清理，任务 3／2 删除读回不存在，硬退出 Kafka 任务按原约定留给部署销毁，外层 cleanup=passed、infra_cleanup=zero_residuals。原生 verify-clean、暂停后清窗、恢复累计和后继新检查尚未走到，不计通过。Artifact `14157603649` 已核对 SHA-256 `7a2f22c3cf2619ac14bf7ca3e73cf01196e94ee4e5255b65ea474f184a324de8` 与 ZIP CRC，完整原件位于 `/tmp/addp-cdc-pretrigger-hosted-43703210703/`。

<a id="cdc-pretrigger-pause-hosted-20261115"></a>

### 触发前暂停及恢复完整窗口的实故障 T4 通过（2026-11-15）

固定修订源码 `ef3c407b43486a1bd38565e223b822d805083516` 的 [Hosted 43705792261／Job 132786461188](https://github.com/pampa0629/addp/actions/runs/43705792261/job/132786461188)实际通过整套 26 项检查和 8 份 Console 回执。preflight 的 repository_clean=true，System／Security／Meta／Transfer／Monitor／Gateway 六服务均为该源码；scenario 1378.446 秒，仍在原 1450 秒及分发 1500 秒预算内。首轮 `501943a07` 的失败、未覆盖范围及原件保留在上方，不以此次通过改写。

| 实际观测 | 消费停滞（180 秒） | 可选积压（300 秒，阈值 256） |
| --- | --- | --- |
| 正式 Pause 前 pending 已累计 | 61.300 秒 | 45.999 秒 |
| Pause 后状态及待触发／恢复窗口 | paused／inactive，均清空 | paused／inactive，均清空 |
| Resume 后临界前仍无 incident／event／投递 | 178.094 秒 | 298.604 秒 |
| 首次打开距新 trigger_since 的真实时间 | 200.003 秒 | 320.003 秒 |

暂停前同一只读快照中三条性能规则的 incident／event／delivery 均为 0，真实新鲜 offset 积压为 947；正式暂停后连续 16 秒、8 次核对两条消费规则窗口为空、生命周期与签名接收继续为空。Resume 请求为 11:10:03.687107Z，两条规则新 trigger_since 均为 11:10:26.518908759Z，晚于请求且不同于暂停前的窗口；恢复使用新 execution、generation 仍为 1。上表临界前样本均新鲜、inactive，计数全为 0；原持续时间满足后才打开，同一新 trigger_since 与不足窗口一致。捕获延迟独立继续评估，没有借消费 Pause 重置；不将其已合法打开的事件混入两条消费规则的零事件断言。

同轮原性能告警的打开后暂停保持、缺样保持、恢复关闭，以及捕获失败、不可恢复风险、替代任务重建、scheduled basic→deep 扫描、历史图表均通过；共 12 条唯一签名 opened／resolved 接收、202 回执与 delivered 审计。CPU／Connector 配置恢复、专用源行／规则清理、目标精确行集、Webhook 删除及接收端关闭均核对，cleanup_failures=[]、uncertain_mutation=false；4 条正式任务删除读回不存在，1 条硬退出 Kafka 任务按原拒删契约留给部署销毁，领域残留 1 不冒充 0。外层 summary 的 cleanup=passed、infra_cleanup=zero_residuals；原生槽／publication 的 verify-clean 已走到并通过。个人 task61 及原有两个逻辑槽、当前任务槽的前后只读状态一致，未操作个人任务或槽。

Artifact `14159537304` 的 SHA-256 `9c3e845cb0e64d5a2e839db0146cea655d624c0f9f42d81f41ca947b2e163691` 与 ZIP CRC 已核对；原件及独立汇总位于 `/tmp/addp-cdc-pretrigger-hosted-43705792261/`。修订源码 `make test-transfer-continuous-online-runner` 本地 145 项通过；[自然 Platform consistency Job 132786240249](https://github.com/pampa0629/addp/actions/runs/43705720875/job/132786240249)实际执行 145 项并通过整个所选门禁。额外登记入口的既有 Common 替身超时仍保留其失败边界，不由此抵消。本轮没有重启 Monitor，评估器重建仍缺独立实故障 T4。

### 暂停清窗后评估器重建的实故障 T4 准备（2026-11-16）

沿现有 T2 顺序，在真实性能故障的正式 Pause／清窗／16 秒保持后，仅终止本轮独占 Monitor，经 pidfd 确认退出，再用标准 `scripts/dev/start.sh -monitor` 重建；核对不同 PID 与相同二进制 SHA-256。新进程启动完成后必须取得更晚 reset_at 的暂停评估，确认两条消费规则仍 inactive、窗口／incident／event／delivery 为空，原暂停 execution／generation 未变，然后正式 Resume 并保留原 180／300 秒、临界前零事件／投递及最终签名送达断言。有效窗口本身按原契约持久化；本场景不将重建解释为无条件清空窗口，不把停机时间补作持续样本。

只修改原 Runner、进程故障工具及各自回归和场景说明；沿既有标准入口及 Platform 自动发现，不新增产品 API、权限、入口或预算。冻结检出 `c34bc696c8f67fc2ea37e7445cf1e99d6a5f3956` 仅叠加本批五文件后，`make test-transfer-continuous-online-runner` 通过 150 项 Python、原语法及 System 身份夹具 Go 检查（`/tmp/addp-cdc-evaluator-runner-frozen.log`）。反例覆盖未证实退出不拉起替代、PID 复用／构建变化、启动失败不重试、私有回执字段、重建后保留窗口／提前生命周期或通知、任务身份改变及没有新后台评估；失败仍恢复既有原生故障、清理规则，不先 Resume。

首轮工作区标准入口在既有 `test_additive_oracle_bounds_large_surplus_and_rejects_missing_duplicate_and_wrong_values` 的 `verify-cdc-altered` 子进程 45 秒超时，150 项中 1 项错误，未走到 Go，原件 `/tmp/addp-cdc-evaluator-runner-first.log` 保留；未修改该夹具及其超时，也不据主机负载推定唯一原因。冻结门禁验证最终增加“新进程实际暂停评估”断言后的源码，首轮失败不被改写。此阶段尚未完成新源码实际 Hosted，不追认前轮未重建 Monitor 的 T4。

### 评估器重建首轮 Hosted 失败与等待边界修订（2026-11-16）

固定 `913a14fad283fb8cc2ca8b6ce4f43b5507e780ce` 的 [Hosted 43843679319／Job 133218391405](https://github.com/pampa0629/addp/actions/runs/43843679319/job/133218391405)实际完成 Monitor 重建：PID 70403→114365，相同二进制 SHA-256 `8e9f3d90ed1111eee44cd8f83d69570a58de5dec83305954d9bc4e1c1ad23396`；重建完成 10:13:58.377878Z 后，新评估器在 10:14:12.560089Z 写入 paused／inactive，trigger_since／recovery_since 为空，两条消费规则的 incident／event／delivery 均为 0。随后正式 Resume 使用新 execution、generation 1 不变。整套仍失败在 performance_trigger 的 450 秒收敛等待，只有前 17 项检查完成，不认定本补充 T4 通过。

首次恢复 pending 起点为 10:14:41.822106746Z，后来窗口重置并从 10:16:56.722067686Z 重新累计。最后的消费停滞不足窗口为 179.801 秒，计数均为 0，后在新窗口约 195.838 秒时打开；积压到等待结束只有 285.100 秒、仍 pending、计数均为 0，尚不足原 300 秒。Owner 回执中的 available 样本连续，不能写成 Owner 停采。机器读取日志分别在 10:16:42.572618Z、10:16:57.577028Z 返回 200；按当前最新快照读取及时间关系，评估器可能漏读中间的 10:16:43.280445Z 样本，使所读 10:16:26.327842Z→10:16:56.722068Z 间隔超过前份 30 秒有效期约 0.394 秒。该解释与现行缺样重置算法相符，但报告没有逐轮窗口历史，不能据此认定所有重置的唯一原因。已证实的失败边界是重新累计后的完整积压窗口尚未满足，旧墙钟等待已经耗尽。

修订保留样本有效期、评估周期、阈值、缺样重置、临界前零生命周期和同一完整新窗口的全部断言，仅将本段收敛等待 450→600 秒，场景 1450→1650 秒、分发 1500→1700 秒；Hosted Job 仍为 60 分钟。新增确定性回归在尚未触发时中断并重新累计，旧 450 秒版本实际失败（`/tmp/addp-cdc-evaluator-window-reset-red-boundary.log`），600 秒版本通过（`/tmp/addp-cdc-evaluator-window-reset-green.log`）。首个较早重置替身未跨过旧截止线，原输出另保留，不冒充 red。最终七文件摘要与冻结检出逐一一致后，标准 `make test-transfer-continuous-online-runner` 通过 151 项 Python、原语法及 System 身份夹具 Go（`/tmp/addp-cdc-evaluator-runner-revised.log`）；一次复制源目录错误而运行旧 150 项的日志另存 `/tmp/addp-cdc-evaluator-runner-stale-input.log`，不计新源码验证。

该轮性能与领域 cleanup=passed、cleanup_failures=[]、uncertain_mutation=false，任务 3／2 删除读回不存在，硬退出 Kafka 任务按原契约交部署销毁，外层 cleanup=passed、infra_cleanup=zero_residuals。后继新窗口全套触发／恢复及签名投递、风险重建、scheduled、历史图表和最终原生 verify-clean 未完成，不计通过。Artifact `14216306323` 已核对 SHA-256 `be03a8880f3593b986d76aec601586b33370d012705b8dd147987f3e488300b1` 与 ZIP CRC，原件位于 `/tmp/addp-cdc-evaluator-hosted-43843679319/`。同源码的 [Platform consistency Job 133217624659](https://github.com/pampa0629/addp/actions/runs/43843456806/job/133217624659)成功，确定性／CI 不抵消实故障失败。修订源码需另跑同一 Hosted 并重新核对完整链路及清理。

### 修订轮 Resume 提前撤销及单次等待修订（2026-11-16）

固定 `1084e1562548f5e5aee6487d4e67783223b18dd2` 的 [Hosted 43881581512／Job 133348563840](https://github.com/pampa0629/addp/actions/runs/43881581512/job/133348563840)失败在 performance_pretrigger_resume，尚未进入新性能窗口，不能用于验证 600 秒收敛修订。Monitor PID 70514→114888，相同二进制 SHA-256 `2bd6a9cd7214a034f8ed0b96c7310124ce1adc3c5bcd1b233fcf4709a709b4af`；新进程完成于 14:24:04.181570Z，新评估器在 14:24:18.360694Z 写入两条 paused／inactive 空窗口，incident／event／delivery 为 0，原暂停 execution／generation 保持。

正式 Resume 请求始于 14:24:18.896726Z；Gateway 在 14:24:28.909067Z 明确记录 `context canceled`，HTTP 日志耗时 10013 毫秒、状态 502；Transfer 同请求日志耗时 10012 毫秒、状态 500。验收 GatewayClient 默认 HTTP 等待为 10 秒，代理与追踪 Transport 不设置新的 10 秒截止；ConnectClient 单次默认已有 15 秒，Resume 沿既有多次状态读取及源端健康检查。因此这里不能把撤销后的 500／502 日志视为验收端已经取得服务端最终业务失败响应；回执正确保留 uncertain_mutation=true，没有重发 Resume，也没有凭随后清理成功清除此标记。

本次修订仅为性能故障段两次正式 Resume 显式使用 60 秒 HTTP 等待，沿同一 User Token、Gateway、路径及原权限；每次只发一次，失败仍保留未知操作结果，其他 API 继续原等待。原生连接／探测超时、CPU 故障、目标锁、阈值、缺样规则和 1650／1700 秒总预算均未调整；现有日志足以定位提前撤销的等待边界，没有新增产品日志或修改 Transfer。新增回归核对两次 Resume 均显式传入 60 秒，失败只调用一次且既有故障清理仍通过。最终七文件与冻结检出摘要一致后，标准 `make test-transfer-continuous-online-runner` 通过 152 项 Python、原语法及 System 身份夹具 Go（`/tmp/addp-cdc-evaluator-runner-control-wait.log`）；真实结果仍需新源码另证。

该轮性能／领域 cleanup=passed、cleanup_failures=[]，任务 3／2 删除读回不存在，硬退出 Kafka 任务按原拒删契约由部署销毁；summary 的 cleanup=passed、infra_cleanup=zero_residuals。未知操作结果仍单独保留，后续完整窗口、通知、风险重建、scheduled、历史图表及最终原生 verify-clean 均未走到，不计通过。Artifact `14234520613` 已核对 SHA-256 `e76095b8e7d0e8d38e5d7ea8c6a6c4d211b2a5888cd209573cd148c30f8aa0b9` 与 ZIP CRC，原件位于 `/tmp/addp-cdc-evaluator-hosted-43881581512/`。固定源码 preflight 六服务一致且工作区干净，[Platform consistency Job 133346940845](https://github.com/pampa0629/addp/actions/runs/43881111817/job/133346940845)成功；这些结果不抵消本轮真实失败。

<a id="cdc-evaluator-rebuild-hosted-20261117"></a>

### 暂停清窗后评估器重建的完整实故障 T4 通过（2026-11-17 记录）

固定 `7e583307e44e7451d388c3622f5edc3d4588ae56` 的 [Hosted 43895444567／Job 133397650216](https://github.com/pampa0629/addp/actions/runs/43895444567/job/133397650216)实际通过整套 27 项检查、8 份 Console 回执。preflight 工作区干净，System／Security／Meta／Transfer／Monitor／Gateway 六服务均为本源码；场景耗时 1329.967 秒，在当前 1650／1700 秒预算内。前两轮 `913a14fad`／`1084e1562` 的失败、未完成范围及未知操作结果标记保留在上方，不被此次通过改写。

Monitor 经 pidfd 确认旧 PID 64169 退出后，由标准 `start.sh -monitor` 创建 PID 104969，同一二进制 SHA-256 `ef4145551718007850957fdfd5d250b397b3e1333f3a903f4704229e019f3c3a`。重建完成时刻为 15:38:44.258727Z，新的暂停评估 reset_at 为 15:38:58.316940Z；两条消费规则仍 paused／inactive，trigger_since／recovery_since 为空，incident／event／delivery 均为 0，原暂停 execution／generation 不变。正式 Resume 请求为 15:38:58.378648Z，返回新的 execution，generation 仍为 1；两条规则从 15:39:23.599458Z 的有效新窗口起累计。所有时间取本轮真实时钟，未写入替身事实或修改产品阈值。

| 实际观测 | 消费停滞（180 秒） | 可选积压（300 秒，阈值 256） |
| --- | --- | --- |
| 正式 Pause 前 pending 已累计，尚无生命周期 | 43.899 秒 | 31.399 秒 |
| 新评估器启动后的暂停评估 | 空窗口、inactive、计数为 0 | 空窗口、inactive、计数为 0 |
| 恢复后临界前仍无 incident／event／投递 | 165.000 秒 | 285.698 秒 |
| 首次打开距当前有效新窗口起点 | 184.717 秒 | 304.717 秒 |

暂停保持的 16 秒／8 次零事件与零签名接收、重建后的新暂停评估、恢复后不足窗口及完整窗口触发均已实证；临界前及实际打开窗口有相同的新 trigger_since。本轮恢复新窗口未再次重置；允许中途缺样重置的等待边界由独立回归及首轮失败事实记录，不声称该重置在本轮又发生。捕获延迟仍独立评估，不因消费暂停清零。性能段两次 Resume 各沿原 User／Gateway 单次请求，使用已记录的独立 60 秒等待，没有失败后重发；不将本轮通过外推为所有原生慢探测或所有错误原因已覆盖。

同轮原性能打开后暂停保持、缺样保持、恢复关闭、捕获持续失败、不可恢复风险、替代任务重建、scheduled basic→deep／原发起人及读取隔离、19 份分钟历史和真实历史图表均通过。性能 6 条、捕获失败 2 条、不可恢复风险 4 条，共 12 条唯一 HMAC 签名 opened／resolved 接收、202 回执与 delivered 审计逐一匹配。CPU／Connector 配置恢复、专用源行和规则清理、目标精确行集、Webhook 删除及接收端关闭均核对，cleanup_failures=[]、uncertain_mutation=false；原生槽／publication 的最终 verify-clean 已走到并通过。

4 条正式任务删除读回不存在，1 条硬退出 Kafka 任务按原拒删契约交部署销毁，领域残留 1 不冒充 0；外层 summary 为 result=passed、cleanup=passed、infra_cleanup=zero_residuals。个人环境在本轮前后只读对照一致：task61 仍 running／未删除，原有两个逻辑槽及当前任务槽的存在和活跃状态均保持；早先共享库恢复模式导致的读取不可用保留为当时事实，不推断为任务改变。未重启个人服务或操作个人任务、源表和槽。

Artifact `14242081152` 的 SHA-256 `f60cd40eda38dcffd979f787ab4b2737a395bb0ee08c13f4d4e29df55408b611` 与 ZIP CRC 已核对；完整原件、独立汇总位于 `/tmp/addp-cdc-evaluator-hosted-43895444567/`，个人前后回执位于 `/tmp/addp-cdc-evaluator-final-preserved-before.json` 和 `/tmp/addp-cdc-evaluator-final-preserved-after.json`。标准 `make test-transfer-continuous-online-runner` 本地通过 152 项、原语法及 System 身份夹具 Go；[自然 Platform consistency Job 133396660611](https://github.com/pampa0629/addp/actions/runs/43895176021/job/133396660611)实际执行 152 项并成功。初始夹具超时、旧输入无效验证及两轮 Hosted 失败各守原边界，未被当前成功抵消。

<a id="alert-evidence-todos"></a>

## 后续待办

1. 补服务组失联的真实租约故障／恢复、正式页面、签名通知和清理；历史临时规则的后续处置无记录，不能假定已删除。
2. 补真实主机容量源／挂载行的实际验收；现有受控容量 T2 和前端通过各守原范围。
3. CDC 触发前暂停／恢复完整窗口及暂停清窗后的评估器重建已有上述实故障 T4；其他源类型和租户告警 UI 完整正式覆盖仍须独立取证，不重复已完成的 PostgreSQL 闭环。
