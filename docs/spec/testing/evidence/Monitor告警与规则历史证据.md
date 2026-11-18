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

在同一隔离 PostgreSQL 场景，以至少 30 秒的实际 pending、目标锁及源端积压替代 75 秒空等。Pause 前消费停滞与可选积压未产生事件／投递，Pause 后空窗口连续保持 16 秒。Resume 后用同一只读 SQL 快照核对临界前零生命周期、新 execution、原 generation 和同一新 trigger_since 上的完整 180／300 秒。捕获延迟独立评估。本阶段不重建 Monitor，场景／分发预算仍为 1450／1500 秒。

冻结基线 `3f812eb686a76e9188943a27189c909aa8aaaa06` 叠加本批 Runner、测试和场景文档，标准 runner 145 项及语法／System 夹具 Go 通过。正反例覆盖授权、窗口、任务归属与安全投影。初版替身断言失败仍留在原日志，真实 T4 此时未执行。额外登记门禁的既有 Common PG 替身在 15 秒截止超时，87 项中 1 项错误，后续 10 项及登记比较未执行。日志为 `/tmp/addp-cdc-pretrigger-runner-final-isolated.log` 和 `/tmp/addp-cdc-pretrigger-registration.log`。

### 新增触发前暂停首次 Hosted 失败及参照点修订（2026-11-15）

固定 `501943a075bd328d7033ee82f384fd08d90d1120` 的 [Hosted 43703210703／Job 132778634342](https://github.com/pampa0629/addp/actions/runs/43703210703/job/132778634342)在 `performance_pending` 失败，未执行正式 Pause。消费停滞／积压已有约 90.091／44.999 秒 pending，三条规则的 incident／event／delivery 均为 0。锁前 committed=10，实际停滞在 committed=11 上累计，夹具错误要求跨阶段恒等。最后一次 buffer 未保存，无法确认唯一失败原因。

修订先保存公开安全 buffer，移除跨阶段位点恒等，保留新鲜 available、>256 积压、目标锁、至少 30 秒真实 pending 和零生命周期断言。正向反例用不同锁前／pending 位点，恢复窗口与临界前断言保持。

故障与领域清理通过，`uncertain_mutation=false`，两正式任务删除读回，硬退出 Kafka 任务交部署销毁，外层 `cleanup=passed`、`infra_cleanup=zero_residuals`。原生 verify-clean、Pause 清窗和恢复累计未执行。Artifact `14157603649` 已核对 SHA-256 `7a2f22c3cf2619ac14bf7ca3e73cf01196e94ee4e5255b65ea474f184a324de8` 和 ZIP CRC，原件 `/tmp/addp-cdc-pretrigger-hosted-43703210703/`。

<a id="cdc-pretrigger-pause-hosted-20261115"></a>

### 触发前暂停及恢复完整窗口的实故障 T4 通过（2026-11-15）

固定 `ef3c407b43486a1bd38565e223b822d805083516` 的 [Hosted 43705792261／Job 132786461188](https://github.com/pampa0629/addp/actions/runs/43705792261/job/132786461188)通过 26 项和 8 份 Console 回执，六服务源码一致、工作区干净，1378.446 秒在 1450／1500 秒内。

| 检查 | 消费停滞（180 秒） | 可选积压（300 秒，阈值 256） |
| --- | --- | --- |
| Pause 前 pending | 61.300 秒 | 45.999 秒 |
| Pause 后（16 秒／8 次） | paused／inactive，窗口及生命周期为空 | 同左 |
| Resume 后临界前仍无事件／投递 | 178.094 秒 | 298.604 秒 |
| 距新窗口起点首次打开 | 200.003 秒 | 320.003 秒 |

暂停前新鲜积压 947，三个规则均无生命周期。Resume 是新 execution、原 generation 1，临界前和打开同属晚于 Resume 的新 trigger_since。捕获延迟独立评估。本轮无 Monitor 重建。原性能恢复、捕获／风险、任务重建、scheduled basic→deep、历史图表和 12 条唯一签名通知／202／delivered 核对通过。

领域清理与原生 verify-clean 通过，`uncertain_mutation=false`。四正式任务删除读回，一硬退出 Kafka 任务交部署销毁，外层 `cleanup=passed`、`infra_cleanup=zero_residuals`。个人 task61 及原有和当前任务槽只读对照一致。Artifact `14159537304` 已核对 SHA-256 `9c3e845cb0e64d5a2e839db0146cea655d624c0f9f42d81f41ca947b2e163691` 与 ZIP CRC，原件 `/tmp/addp-cdc-pretrigger-hosted-43705792261/`。本地 145 项和[Platform Job 132786240249](https://github.com/pampa0629/addp/actions/runs/43705720875/job/132786240249)通过，原失败保持原判。

### 暂停清窗后评估器重建的实故障 T4 准备（2026-11-16）

在原 Pause／清窗／16 秒保持之后，经 pidfd 确认当轮独占 Monitor 退出，由标准入口重建，校验异 PID、同二进制。新评估须有晚于进程启动的 reset_at、两规则 inactive／空窗／零生命周期及原暂停 execution／generation，之后才 Resume。原 180／300 秒和签名投递断言保留。有效窗口的持久化规则仍适用，重建及停机不能补算连续样本。

冻结基线 `c34bc696c8f67fc2ea37e7445cf1e99d6a5f3956` 叠加五文件后，标准 runner 150 项及语法／System 夹具 Go 通过。首轮工作区在既有 `verify-cdc-altered` 子进程 45 秒超时，150 项中 1 错误，Go 未执行，原因未定。日志为 `/tmp/addp-cdc-evaluator-runner-frozen.log` 和 `/tmp/addp-cdc-evaluator-runner-first.log`。这两次是 runner 验证，实际 Hosted 另认定。

### 评估器重建首轮 Hosted 失败与等待边界修订（2026-11-16）

固定 `913a14fad283fb8cc2ca8b6ce4f43b5507e780ce` 的 [Hosted 43843679319／Job 133218391405](https://github.com/pampa0629/addp/actions/runs/43843679319/job/133218391405)完成 Monitor 重建（PID 70403→114365，二进制 SHA-256 同为 `8e9f3d90ed1111eee44cd8f83d69570a58de5dec83305954d9bc4e1c1ad23396`）及新暂停空窗／零生命周期评估。Resume 后新 execution、原 generation 1。前 17 项完成，`performance_trigger` 的 450 秒收敛等待失败。

恢复窗口中途重置。消费停滞在新窗口约 195.838 秒打开，临界前 179.801 秒时计数为 0。积压到截止仅 285.100 秒，仍 pending，未达到 300 秒。Owner available 连续。评估器可能错过中间快照，所读间隔超过前份 30 秒有效期约 0.394 秒，缺逐窗历史，不能定为唯一根因或记作 Owner 停采。

修订等待 450→600 秒，场景／分发 1450／1500→1650／1700 秒，Job 仍 60 分钟。阈值、样本有效期、重置和临界前断言保留。旧截止实际失败、新截止通过的确定性反例与 runner 151 项通过见 `/tmp/addp-cdc-evaluator-window-reset-red-boundary.log`、`/tmp/addp-cdc-evaluator-window-reset-green.log`、`/tmp/addp-cdc-evaluator-runner-revised.log`。旧输入所得 150 项不计修订验证。

性能及领域清理通过，两正式任务删除读回，硬退出 Kafka 任务交部署销毁，外层清理／Infra 零残留，`uncertain_mutation=false`。完整性能恢复及签名投递、风险重建、scheduled、图表与最终 verify-clean 未执行。Artifact `14216306323` 已核对 SHA-256 `be03a8880f3593b986d76aec601586b33370d012705b8dd147987f3e488300b1` 和 ZIP CRC，原件 `/tmp/addp-cdc-evaluator-hosted-43843679319/`。[Platform Job 133217624659](https://github.com/pampa0629/addp/actions/runs/43843456806/job/133217624659)通过，Hosted 仍失败。

### 修订轮 Resume 提前撤销及单次等待修订（2026-11-16）

固定 `1084e1562548f5e5aee6487d4e67783223b18dd2` 的 [Hosted 43881581512／Job 133348563840](https://github.com/pampa0629/addp/actions/runs/43881581512/job/133348563840)完成 Monitor 重建（PID 70514→114888，二进制 SHA-256 同为 `2bd6a9cd7214a034f8ed0b96c7310124ce1adc3c5bcd1b233fcf4709a709b4af`）及新暂停空窗／零生命周期评估，保留原 execution 和 generation。在 `performance_pretrigger_resume` 失败，未进入 600 秒窗口。

验收端 10 秒 HTTP 等待先取消，Gateway 的 502／`context canceled` 和 Transfer 的 500 是取消后的日志，验收端未取得最终业务结果。结果 `uncertain_mutation=true`，没有重发 Resume。修订仅对性能段两次 Resume 使用 60 秒、每次单请求，沿原身份、路径及总预算。原生探测时限、故障、窗口和阈值保持。runner 152 项及语法／System 夹具 Go 通过，见 `/tmp/addp-cdc-evaluator-runner-control-wait.log`。

领域与外层清理通过，两个正式任务删除读回，硬退出 Kafka 任务交部署销毁，Infra 零残留。未知变更结果保持，完整窗口、通知、风险、scheduled、图表及最终 verify-clean 未走到。Artifact `14234520613` 已核对 SHA-256 `e76095b8e7d0e8d38e5d7ea8c6a6c4d211b2a5888cd209573cd148c30f8aa0b9` 和 ZIP CRC，原件 `/tmp/addp-cdc-evaluator-hosted-43881581512/`。同源码六服务一致，[Platform Job 133346940845](https://github.com/pampa0629/addp/actions/runs/43881111817/job/133346940845)通过。

<a id="cdc-evaluator-rebuild-hosted-20261117"></a>

### 暂停清窗后评估器重建的完整实故障 T4 通过（2026-11-17 记录）

固定 `7e583307e44e7451d388c3622f5edc3d4588ae56` 的 [Hosted 43895444567／Job 133397650216](https://github.com/pampa0629/addp/actions/runs/43895444567/job/133397650216)通过 27 项与 8 份 Console 回执。六服务同源码、预检工作区干净，1329.967 秒在 1650／1700 秒内。

经 pidfd 确认 PID 64169 退出后标准入口启动 PID 104969，二进制 SHA-256 同为 `ef4145551718007850957fdfd5d250b397b3e1333f3a903f4704229e019f3c3a`。新暂停评估晚于进程启动，空窗且计数为 0，原暂停 execution／generation 不变。16 秒／8 次无事件及签名接收。正式 Resume 返回新 execution、原 generation 1，临界前与打开沿同一个新 trigger_since。

| 实际观测 | 消费停滞（180 秒） | 可选积压（300 秒，阈值 256） |
| --- | --- | --- |
| Pause 前 pending | 43.899 秒 | 31.399 秒 |
| 恢复后临界前仍无事件／投递 | 165.000 秒 | 285.698 秒 |
| 距当前有效新窗口首次打开 | 184.717 秒 | 304.717 秒 |

本轮新窗口未再重置。缺样重置和旧等待失败凭独立回归及早期证据判定。捕获延迟独立，两次 Resume 各一次请求、60 秒等待。本轮也通过打开后暂停／缺样保持、原事件恢复、风险及后继任务、scheduled basic→deep、19 份分钟历史和图表。12 条唯一 HMAC 通知（性能 6、捕获失败 2、风险 4）逐一匹配 202 与 delivered。

`cleanup_failures=[]`、`uncertain_mutation=false`，原生槽／publication 的 verify-clean 通过。四正式任务删除读回，一硬退出 Kafka 任务交部署销毁，领域保留 1 项，外层 `cleanup=passed`、`infra_cleanup=zero_residuals`。个人 task61 和原槽前后只读一致，较早恢复模式下不可读的记录不推成任务改变。Artifact `14242081152` 已核对 SHA-256 `f60cd40eda38dcffd979f787ab4b2737a395bb0ee08c13f4d4e29df55408b611` 与 ZIP CRC，原件及汇总 `/tmp/addp-cdc-evaluator-hosted-43895444567/`。本地和 [Platform consistency Job 133396660611](https://github.com/pampa0629/addp/actions/runs/43895176021/job/133396660611)均执行 152 项并通过，前两轮 Hosted 失败及旧夹具失败仍保持原判。

### 租户告警页面实故障闭环准备（2026-11-17）

沿同一 `transfer-continuous-events` 性能故障新增 `cdc_alert_open`／`cdc_alert_unknown`／`cdc_alert_resolved` 三次真实 Console 页面回执，沿原 User、任务、generation、三条规则和签名通知。打开阶段逐项对照正式 API 的 incident ID、任务、代次、状态、打开时间；由页面各确认一次并核对已确认，Runner 删除原三次确认 POST。缺样阶段核对同一事件的 acknowledged／unknown、安全最后观测时间保持，通知页没有 resolved；恢复阶段核对原 incident resolved 及 opened／resolved 投递 UUID、已送达和 HTTP 202，与独立签名接收和审计关联。页面确认开始至完整浏览器回执前沿原机制标记未知操作结果，失败不重发。通知目标 URL、登录输出和完整响应不归档，不截图通知整页。

本批仅修改原 Hosted Runner／回归、原 Online 页面 spec 和现有场景／证据；没有产品 UI／接口／权限、入口或预算变化。标准 `make test-transfer-continuous-online-runner` 在冻结检出 `252942fc223e1fe865cdb3623fd7988d51c21fad` 叠加四个实际输入后，154 项 Python、原 Node／Shell 语法及 System 身份夹具 Go 完整通过（`/tmp/addp-cdc-alert-ui-runner-frozen.log`）。回归核对三阶段编排及页面唯一确认、错误任务／代次／投递或回执检查缺失的拒绝、浏览器失败后原生故障和规则清理；普通 Console T3 配置明确排除 `e2e/online`，这些实际页面断言必须由新源码 Hosted 证明。前轮 27 项／8 页回执未覆盖本补充，当前尚未运行实际新 T4，不追认通过。

### 租户告警页面首轮 Hosted 失败与客户端生命周期修订（2026-11-17）

固定 `0531f97a0ecfe95536274e5ffe8d919a7ab6b7e2` 的 [Hosted 43992449101／Job 133736209624](https://github.com/pampa0629/addp/actions/runs/43992449101/job/133736209624)失败在 performance_alert_open_page。已有前 17 项检查、暂停清窗／评估器重建／恢复完整窗口断言及三条性能告警真实打开和 3 条 opened 签名实收；浏览器只完成 alert_task_generation，失败阶段为 cdc_alert_open_rows，未进入确认／缺样／关闭页面。首次取 Artifact 时网络下载失败，仅重新读取同一原件，没有重新派发 Hosted。

根因位于新增 Online spec：声明了 alertAPI，但原 signedPage 的 finally 仍即时 dispose，未将登录后通过身份核验的客户端赋给它；currentIncidents 首次读取直接调用未赋值的 alertAPI。浏览器失败回执的阶段与仅一条页面告警 GET 符合该路径，Monitor 日志没有确认 POST。本轮 uncertain_mutation=true 按预先设置保留，不借“实际尚未确认”或后续清理把标记清掉。修订仅让告警三阶段持有该客户端至外层 finally 释放，其余阶段仍即时释放；增加安全阶段标签，不新增产品接口／日志、权限或预算。

新增回归执行实际 signedPage 闭包，只替换浏览器和网络边界：固定发布源码确实因告警客户端为 undefined 失败（`/tmp/addp-cdc-alert-ui-api-red-published.log`），修订后三个告警阶段均能继续读取并最终释放，普通阶段保持即时释放（`/tmp/addp-cdc-alert-ui-api-green-actual.log`）。早期回归名称、缺导入与夹具缺 route 的失败原件单独保留，不冒充有效 red。最终冻结输入一致后，`make test-transfer-continuous-online-runner` 完整通过 155 项、Node／Shell 语法和 System 身份夹具 Go（`/tmp/addp-cdc-alert-ui-runner-client-fix.log`）；[同源码自然 Platform consistency Job 133736064214](https://github.com/pampa0629/addp/actions/runs/43992409449/job/133736064214)成功仍不能抵消页面实故障失败。

性能／领域 cleanup=passed、cleanup_failures=[]，CPU／Connector 配置恢复、临时源行和规则清理、Webhook 删除及接收端关闭均完成；任务 3／2 删除读回不存在，硬退出 Kafka 任务按原约定由部署销毁，外层 cleanup=passed、infra_cleanup=zero_residuals。未知操作结果保留；后续页面、恢复通知、风险重建、scheduled、历史图表及最终原生 verify-clean 未走到，不计通过。Artifact `14289647480` 已核对 SHA-256 `033ea43b0a73e1ac4a5c9a510cb09f37bcb806d523835052ddd860acb4cd99f2` 与 ZIP CRC，原件 `/tmp/addp-cdc-alert-ui-hosted-43992449101/` 保留。修订后的真实页面闭环需另以新源码完整联验。

### 修订轮缺样通知列表范围失败及投递定位修订（2026-11-17）

固定 `1a5d2e5696f05bb7e67b0f7338f7fe3c2d2a31bc` 的 [Hosted 44002520914／Job 133769826122](https://github.com/pampa0629/addp/actions/runs/44002520914/job/133769826122)通过 cdc_alert_open 的完整五项：任务／代次／incident 对照、页面确认及 opened 送达展示；Monitor 原生日志确认三个 acknowledge POST 均 200，浏览器各只发一次。缺样页面的 unknown_rows_match_api 和 last_observed_preserved 也已通过，失败阶段为 cdc_alert_notification_filter；整套仍失败，前 17 项完成，不外推恢复页面或整套通过。

新增页面原来错误地要求所选 Webhook 目标的全部可见行数等于本任务三条性能告警的投递数。现有 CreateDeliveriesTx 对无规则的执行信号可向同租户启用目标投递，目标身份不等于仅该任务身份；正式 API 与原 HMAC 接收对照始终按 incident／event／delivery 精确筛选。本轮缺样前已出现真实执行故障，旧全表要求与通知契约不一致；报告未保存实际页面行数和失败子断言，不能认定它是本轮唯一失败原因。修订按已核验的投递 UUID 在所选目标中分页定位、逐项核对状态／事件／202；保留正式 API 中本任务恰有原 opened、没有 resolved 的严格判断，其他任务行不代替本任务证据。

回归执行实际通知列表断言片段，使用三条绑定投递加一条其他任务投递：固定发布源码实际因 4≠3 失败（`/tmp/addp-cdc-alert-ui-row-scope-red.log`），修订后逐项定位通过（`/tmp/addp-cdc-alert-ui-row-scope-green.log`）。最终冻结输入摘要一致后，`make test-transfer-continuous-online-runner` 完整通过 156 项、原 Node／Shell 语法及 System 身份夹具 Go（`/tmp/addp-cdc-alert-ui-runner-row-scope.log`）。没有修改通知产品语义、权限、页面过滤参数或预算；新源码仍须重新完成同一 Hosted。[原源码 Platform consistency Job 133769550913](https://github.com/pampa0629/addp/actions/runs/44002439912/job/133769550913)成功，不能抵消本轮实故障失败。

本轮 uncertain_mutation=false，性能／领域 cleanup=passed、cleanup_failures=[]；任务 3／2 删除读回不存在，硬退出 Kafka 任务由部署销毁，外层 cleanup=passed、infra_cleanup=zero_residuals。恢复页面、剩余通知、后继风险重建、scheduled、历史图表及最终原生 verify-clean 未走到，不计通过。Artifact `14293873081` 已核对 SHA-256 `0d925cb934d74d79daeb8757c0b7e457c976e353e1728f7625f29a87a44ae70b` 与 ZIP CRC，原件 `/tmp/addp-cdc-alert-ui-hosted-44002520914/` 保留。新一轮须按修订源码核对完整页面、签名送达和清理，不追认本轮未完成部分。

<a id="alert-evidence-todos"></a>

<a id="cdc-tenant-alert-ui-hosted-20261117"></a>

### 三条性能告警的租户页面实故障闭环通过（2026-11-17）

固定源码 `b8593c1e706e78e23668649eb9595021d32a3e3a` 的 [Hosted 44007380675／Job 133785893483](https://github.com/pampa0629/addp/actions/runs/44007380675/job/133785893483)整套通过；System／Security／Meta／Transfer／Monitor／Gateway 六个服务均核对同一提交，仓库干净。28 项 suite 检查、11 份真实 Console 浏览器回执、12 条唯一 opened／resolved 签名实收及 delivered／HTTP 202 对照通过。沿原隔离 PostgreSQL、User 55／Tenant 2、任务 3／generation 1 和三条性能规则，没有扩权限、改变产品阈值或增加故障替身；预算维持场景 1650 秒／派发 1700 秒／Job 60 分钟。

- `cdc_alert_open`：三条事件的任务、代次、ID、打开时间与 API 一致；页面各确认一次并读回同一事件 acknowledged。浏览器确认计数逐 incident 恰为一次，原生日志在 03:26:36 UTC 有三个实际 acknowledge HTTP POST，均 200；排除启动路由登记，整套另有原捕获失败与不可恢复风险两次 API 确认，不能把全套请求数写成三次。opened 通知页逐项核对绑定的三个投递 UUID、已送达及 HTTP 202。
- `cdc_alert_unknown`：同一三个 incident 保持 acknowledged，评估为 unknown，LastObservedAt 与故障前安全观测逐项相同。正式 API 沿任务事件范围严格核对仅原 opened、零 resolved；页面在所选目标按签名实收的 UUID 分页定位，不把同目标其他执行的通知纳入本任务断言。
- `cdc_alert_resolved`：原三个 incident 恢复为 resolved，关闭时间与 API 一致；通知页核对该任务六个 opened／resolved UUID、状态及 202，与签名接收及投递审计一致。三个阶段各五项检查均通过，回执绑定同一 User／Tenant／任务／代次／incident／投递；没有保存含目标地址的整页截图、登录输出或消息正文。

原有触发前暂停清窗、暂停后的评估器重建和恢复完整窗口断言保留通过；不足持续时间没有事件或通知。性能缺样／暂停保持、捕获失败、源槽与消息恢复范围风险、后继任务重建、scheduled basic→deep／发起人／读取隔离、任务历史 API 和图表的积压增长／回零／故障断点同轮通过。这不把捕获失败和不可恢复风险的 API 闭环扩大为其告警页面全部交互已验。

性能与风险 cleanup=passed、cleanup_failures=[]，CPU／Connector 配置恢复、规则和源行清理、目标行集核对、Webhook 删除及接收端关闭均通过。任务 5／4／3／2 删除后读回不存在；硬退出 Kafka 任务 1 按原约定保留至部署销毁，领域报告的 1 项预期部署残留不能写成领域零残留。原生 verify-clean 与停止后当前观测清空已执行；最终外层 summary 的 cleanup=passed、infra_cleanup=zero_residuals。个人环境仅只读核对：task61 仍 running，原两个槽 `addp_cdc_t1_task46_g1`／`addp_cdc_t1_task58_g1` 仍不活跃，当前任务槽 `addp_cdc_854107b8813848f8841f48460e8216d1_g1` 仍活跃，前后结果完全相同。

本地 `make test-transfer-continuous-online-runner` 的 156 项 Python、原 Node／Shell 语法和 System 身份夹具 Go 通过；[同源码自然 Platform consistency Job 133785625920](https://github.com/pampa0629/addp/actions/runs/44007302602/job/133785625920)成功，原始日志确认 156 项 Runner 实际运行。Artifact `14295904734` 已核对 SHA-256 `d32af8d96313a5d7e0429c9b8a363aae9f40713e0eed4695c066b770922d1e5f` 和 ZIP CRC；原件、逐项复核及个人前后对照保留在 `/tmp/addp-cdc-alert-ui-hosted-44007380675/`。前两轮失败源码、阶段、未知操作结果和清理记录继续保留，不追认为通过；本轮结果不外推其他 Provider、所有告警类型页面、七天自然留存或生产 T5。

### 捕获失败与恢复能力丢失的租户页面验收准备（2026-11-17）

沿原 `transfer-continuous-events` 扩展六个页面阶段：捕获失败的打开／页面确认、Connect 缺样保持、健康恢复关闭；源端及消息恢复能力丢失的打开／页面各确认、源库与本轮独占 Redpanda 同时缺样保持、正式确认 Stop 后旧事件关闭。复用上一轮唯一告警／通知页面断言，按真实任务、generation、incident 与签名 delivery UUID 对照正式 API；删除原捕获失败及源端风险的 Runner 确认 POST，补上消息风险的页面确认，所有确认仅发送一次。捕获与风险原契约没有性能规则的 evaluation_state，页面只核对真实已有状态、原因和最后有效观测时间；缺样由正式任务观测证明，不新增 unknown 字段或产品 UI。

本批只改原 Runner／回归、Online spec 和现有场景／证据，不改产品服务、接口、权限、规则阈值或预算。现有 `make test-transfer-continuous-online-runner`／Platform 和手工 Hosted 自动覆盖；普通 Console T3 排除 Online spec，不能作为真实页面证据。冻结检出 `3ad3fead614e20627167428acb7a67a1840ac213` 叠加本批四个实际输入后，标准入口的 158 项 Python、原 Node／Shell 语法及 System 身份夹具 Go 完整通过（`/tmp/addp-cdc-risk-ui-runner-frozen.log`），覆盖六阶段浏览器失败与缺样时间漂移后恢复原生故障、不重复确认，以及九阶段绑定／回执拒绝。新整套预期 30 项检查、17 份浏览器回执及原六类 opened／resolved 共 12 条签名通知；此前三条性能告警页面成功不追认本补充。个人 task61 与三个现有槽已保存只读基线，故障动作仅限 GitHub Hosted 独占资源。

<a id="cdc-risk-alert-ui-hosted-20261117"></a>

### 捕获失败与两类恢复能力丢失的租户页面 Hosted 通过（2026-11-17）

固定源码 `755d24f325355acd511be8ea88aa8cfa4cc9ab8b` 的 [Hosted 44033277901／Job 133870034806](https://github.com/pampa0629/addp/actions/runs/44033277901/job/133870034806)整套通过；六个服务构建身份一致，preflight 仓库干净。30 项 suite 检查、17 份真实 Console 回执及六类 opened／resolved 的 12 条唯一签名实收、delivered／HTTP 202 对照通过，原有性能告警、暂停清窗、评估器重建和完整持续窗口均保留。普通 User／Tenant、原独占任务／generation、规则权限和 1650／1700 秒预算不变，没有个人服务或资源写入。

- 捕获持续失败沿原 Connector 暂停取得超过 60 秒的新鲜失败事实，warning incident `62` 由真实页面确认一次。Connect 缺样期间原 acknowledged、LastObservedAt 及零恢复通知保持；原 Connector 健康恢复后，同一 incident 关闭。`cdc_alert_capture_open`／`unknown`／`resolved` 各五项页面检查与 API 及两条签名投递一致。
- 源端恢复能力丢失与消息恢复范围丢失沿原专用槽删除和主题 trim 故障产生两条 critical，原 incident 分别为 `65`／`64`。两条均由页面各确认一次；本轮独占源库与 Redpanda 同时暂停后，分别取得故障之后的新鲜 unavailable。原事件及最后有效观测时间保持且没有恢复通知；采样恢复后两个已知不可恢复风险仍存在，正式确认 Stop 后才关闭原 incident，新任务健康不替代旧位点恢复。`cdc_alert_risk_open`／`unknown`／`resolved` 各五项检查与 API 及四条签名投递一致。
- 三组共九个告警页面阶段复用唯一实现，按同一租户、task／generation、incident ID 及签名 delivery UUID 定位。新增两组没有伪造性能规则的 evaluation_state／unknown 页面标签；缺样由任务 API 与 LastObservedAt 冻结共同证明。通知页逐项核对状态、事件类型及 202，排除同目标其他执行行；没有归档通知 URL、登录输出或消息正文。

性能／风险及捕获故障清理通过，cleanup_failures=[]；临时 CPU／Connector 配置恢复、规则与源行清理、目标行集核对、Webhook 删除及接收端关闭分别核验。任务定义 5／4／3／2 删除后读回不存在；硬退出 Kafka 任务 1 按原约定保留到部署销毁，领域的预期残留 1 与外层最终零残留分别记录。原生 verify-clean、停止后的当前观测清空、后继重建、scheduled basic→deep／发起人／读取隔离及历史图表增长／回零／故障断点通过。外层 summary 的 cleanup=passed、infra_cleanup=zero_residuals；uncertain_mutation=false。个人 task61 和原两个槽及当前任务槽的前后只读结果完全一致。

本地标准 `make test-transfer-continuous-online-runner` 的 158 项 Python、原 Node／Shell 语法和 System 身份夹具 Go 通过。[同源码自然 Platform consistency Job 133869832508](https://github.com/pampa0629/addp/actions/runs/44033218323/job/133869832508)成功，原始日志确认对应 158 项 Runner 实际执行。排除启动路由登记后，原生 Monitor 日志恰有六个实际 acknowledge POST 且均 200：三次原性能告警、一次捕获失败、两次恢复能力丢失，分别对应页面各一次；没有 Runner API 重复确认。Artifact `14308061493` 的 SHA-256 `8cd59d529561d0c7bcadc8b2d81cd1e5cbcf928b208eb7df54c0b19d9c441b2b` 与 ZIP CRC 已核对，原件和逐项复核位于 `/tmp/addp-cdc-risk-ui-hosted-44033277901/`。本轮没有修改产品 API／权限／告警阈值，既有失败记录保留，不外推其他 CDC Provider、七天自然留存、共享组件持续规则或生产 T5。

## 后续待办

1. 补服务组失联的真实租约故障／恢复、正式页面、签名通知和清理；历史临时规则的后续处置无记录，不能假定已删除。
2. 补真实主机容量源／挂载行的实际验收；现有受控容量 T2 和前端通过各守原范围。
3. CDC 暂停／恢复完整窗口、评估器重建及六类任务告警页面闭环已有上述 PostgreSQL 实故障 T4；其他源类型、共享组件持续规则、七天自然留存与容量仍须独立取证，不重复已完成范围。
