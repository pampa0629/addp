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

这仍是受控 Owner 事实的 T2，触发前正式 Pause／Resume 的实故障 T4 尚未完成。后续[七天历史 Hosted](Monitor执行监控与留存历史证据.md#七天-cdc-任务历史的真实-hosted-断言通过2026-11-15)另认定，不反向补齐此场景。

### 触发前暂停的实故障 T4 准备（2026-11-15）

沿同一 PostgreSQL 性能故障加入消费停滞／可选积压的 pending→Pause→Resume：先读到至少 30 秒的真实 pending 和目标锁阻塞下的源端积压，确认尚无 incident／event／投递或签名接收；暂停后两条消费规则 inactive、待触发／恢复窗口清空并保持 16 秒；恢复创建新 execution，generation 不变，原 180／300 秒阈值不变。恢复期间以同一个只读 SQL 快照核对窗口与生命周期计数，要求各自接近原阈值前仍无事件／投递，最后的不足窗口与实际打开窗口必须有同一新 trigger_since。捕获延迟继续独立评估；不重启 Monitor，评估器重建仍只认上方 T2。

复用隔离 Infra owner 的精确 PostgreSQL 容器、既有只读 helper 和正式租户 API，不新增权限、产品接口、任务或探针；只返回本 Tenant／Task／generation／规则／目的地的安全窗口及计数。新 pending 观测替换原 75 秒预热空等，保留原打开后的暂停、缺样、恢复、通知、图表、风险、重建、scheduled 及清理；scenario 1450 秒、分发 1500 秒保持。

冻结检出 `3f812eb686a76e9188943a27189c909aa8aaaa06` 仅叠加本批 Runner／测试／场景文档后，`make test-transfer-continuous-online-runner` 通过 145 项及既有语法、System 身份夹具 Go（`/tmp/addp-cdc-pretrigger-runner-final-isolated.log`）。反例覆盖越权／错规则快照、负计数／私有字段、已有生命周期、暂停保留窗口、恢复复用旧窗口、提前激活／投递，以及缺失临界前有效样本；恢复后的合法 unknown 状态不能冒充有效累计。初版替身阶段和无效时间样本失败原件保留在同前缀 Runner 日志，不计为产品或 Hosted 失败。

额外 `make test-t2-ci-registration` 在既有 Common PG 替身 `test_full_gate_cannot_pass_using_only_the_previous_execution_cases` 的 15 秒子进程超时，87 项中 1 项错误；后续 10 项和登记比较未执行，不计通过，也未修复或重跑该范围。原件 `/tmp/addp-cdc-pretrigger-registration.log`。本批没有新增入口、服务或 CI 路线，既有 CDC Runner 与手工 Hosted 发现已覆盖。此时新源码真实 T4 尚未执行，不能以确定性测试或旧轮通过追认。

### 新增触发前暂停首次 Hosted 失败及参照点修订（2026-11-15）

固定 `501943a075bd328d7033ee82f384fd08d90d1120` 的 [Hosted 43703210703／Job 132778634342](https://github.com/pampa0629/addp/actions/runs/43703210703/job/132778634342) 停在 performance_pending，尚未正式 Pause。三条规则的 incident／event／delivery 均为 0；消费停滞窗口约 90.091 秒、积压窗口约 44.999 秒，均 pending／inactive。锁前 baseline committed=10（10:36:05Z），实际停滞窗口已在 committed=11 上累计（10:36:21Z 至 10:37:51Z）。新加的直接 buffer 核对错误地要求跨这两个阶段与旧基线完全相等；同一有效窗口内停滞不要求锁前采样也有同一位点。最后一次 buffer 未在失败前保存，因此本轮不能认定该条件是唯一失败原因，也不推断具体业务／控制事件。

修订在校验前保存公开安全 buffer，仍要求新鲜 available、实际 offset 积压 >256、目标锁已建立、真实 pending 至少 30 秒、原阈值及完整生命周期零值，只移除跨阶段 committed 恒等的错误前提。正向编排回归明确让初始位点 0、实际 pending 位点 1，避免把锁前正常推进误报为告警错误；恢复窗口及临界前断言不放宽。没有修改产品状态或通过重试隐藏该次失败。

该轮故障／领域 cleanup=passed、uncertain_mutation=false；CPU、Connector、规则、源记录及 Webhook 已恢复／清理，任务 3／2 删除读回不存在，硬退出 Kafka 任务按原约定留给部署销毁，外层 cleanup=passed、infra_cleanup=zero_residuals。原生 verify-clean、暂停后清窗、恢复累计和后继新检查尚未走到，不计通过。Artifact `14157603649` 已核对 SHA-256 `7a2f22c3cf2619ac14bf7ca3e73cf01196e94ee4e5255b65ea474f184a324de8` 与 ZIP CRC，完整原件位于 `/tmp/addp-cdc-pretrigger-hosted-43703210703/`。

<a id="alert-evidence-todos"></a>

## 后续待办

1. 补服务组失联的真实租约故障／恢复、正式页面、签名通知和清理；历史临时规则的后续处置无记录，不能假定已删除。
2. 补真实主机容量源／挂载行的实际验收；现有受控容量 T2 和前端通过各守原范围。
3. CDC 触发前暂停与评估器重建仍缺实故障 T4；其他源类型和告警 UI 完整正式覆盖须独立取证，不重复已完成的 PostgreSQL 闭环。
