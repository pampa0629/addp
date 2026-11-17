# ADDP Online 场景：Continuous 与 CDC 验收

[返回同步与引擎消费](同步与引擎消费.md) · [T4 准入与报告](../T4-Online验收规范.md)

## transfer-continuous-events

### 部署、身份与事件闭环

手动 Hosted Linux x86_64 独占部署，Business 自有 Kafka 和 logical WAL PG 源／目标，不能用 Infra Kafka 冒充业务源。System 正式 IAM helper 建 User／Role／Session；普通 User 经 Gateway 控制、正式 Worker 执行，独立物理 oracle 核 snapshot／upsert／delete。强退只 pin 自有 Worker PID，恢复用标准入口，不加产品故障参数／Supervisor／授权替身。

| 入口 | 核对要点 |
| --- | --- |
| 过程事件／当前读取权 | Transfer／Monitor 的 started、累计提交、唯一终态／恢复链；撤权前两端各至少两页、留首游标，撤后旧 Token401。原 Monitor-only User 重新登录后旧游标仍被 Owner／Monitor 拒绝、列表／总数 0；同 Tenant 无 Owner 权及跨 Tenant 也空／0，Owner 不可用失败关闭。 |
| 持续写入分页 | 同 Kafka 任务首批每页 2 条后追加，等目标／Owner 证提交，再沿原游标续读；拼接等于追加后两端全读。真实 Console 自动续页／手刷／顺序／数量，随后才 Worker 失联，页面收敛原执行失败诊断和唯一终态。 |

证据只身份／页数／事件数／游标／封闭检查，禁配置／业务值／原始 checkpoint／内部 Worker 租约／完整响应。合成 Meta1000／1201 条截断另沿[执行监控专项](../../tasks/执行监控.md#大量事件分页与截断真实入口验收)，不由此证明 30 天清理、180 天概览、其他 Owner 或 Kafka HA。

### CDC 指标、历史与页面

字段白名单与缺样语义归[CDC 生命周期](../../tasks/Transfer数据库CDC.md#捕获资源与生命周期)。任务、generation、当前 execution 和实际源端证据须精确绑定；原生对象名／LSN／Jolokia／SQL 只进程内核，公开只安全类型化快照和布尔断言。

| 场景 | 独立物理／页面断言 |
| --- | --- |
| Pause／Resume | 先取新鲜槽／WAL／延迟／缓冲；暂停消费后源三条增删改，committed 不变、latest／积压位置差增 3、目标原行集／槽活跃。Resume 新 execution 而 generation 不变，准确应用变更、两次新样本积压 0；每 execution 累计进度独立。 |
| 原生源对照 | 只读 helper 核自有槽／publication／Connector 归属，Business 容器核 logical／pgoutput／库、单表 publication、WAL 距离／安全余量。Host Gate 固定 Connect 完整 ID／项目／服务，只读本任务 streaming MBean Connected／MilliSecondsBehindSource；API 对齐，-1 空、其他负值保留，真实变化须数值。WAL 不是目标积压、不要求同步下降，不造磁盘压力／lost；Stop 清当前观测。 |
| 当前指标页 | 同 User 三阶段 `cdc_initial`／`cdc_growth`／`cdc_resumed`进 `/monitor/cdc-resources?view=tasks&task_id=<本轮任务>`，逐项对 API 与原生事实。暂停读当前 `buffer_observation`，恢复同一消息末端／零积压，不能用旧 execution 零值。任一页面失败停后续并清理。 |
| 七天 API／图表 | `cdc-metric-history`核同任务代次整分钟、白名单／采样时间及高积压→后继有效 0、WAL／延迟，空档不造值。`cdc_history`页逐点对真实 ECharts／Canvas 与 API，故障前>256、故障后完成分钟 null、恢复 0、`connectNulls=false`和真鼠标悬浮；创建前空档不代故障断点。只留核验后局部截图，不加调试 API 或模拟响应。 |

原生 T2 同白名单核事件计数推进／LastEvent 拒绝，页面核快照完成／计数／年龄；不从元数据认真实覆盖。历史保留到独占部署销毁，删除任务不恢复定义读取权；七天自然留存／长期容量仍未由短轮证明。

### 实故障告警与通知

规则／阈值／通知契约统一见[捕获失败](../../tasks/Transfer同步执行.md#cdc-捕获持续失败告警2026-11-14-已确认)、[不可恢复风险](../../tasks/Transfer同步执行.md#cdc-任务不可恢复风险告警2026-11-13-已确认)、[性能规则](../../tasks/Transfer同步执行.md#cdc-任务持续性能告警2026-11-14-已确认)、[Webhook](../../tasks/Transfer同步执行.md#租户告警通知与渠道投递)。只给本场景 User 增加告警读／确认、Webhook 目标 CRUD 和投递读取，不扩大共享夹具或个人权限。故障必须自有 Tenant／task／generation／容器且 Hosted 归属已证。

| 故障／窗口 | 必须证明 |
| --- | --- |
| 捕获持续失败 | 正式暂停目标，仅暂停当前 Connector；新鲜明确失败满 60 秒才 warning。确认后 Connect 缺样保持原确认／LastObservedAt、无恢复通知；解除暂停、仍失败后正式恢复 Connector，新鲜 healthy 关闭原事件，opened／resolved 实收与审计。退出解除故障，不恢复目标消费；warning 不代丢失 critical。 |
| 两类恢复丢失 | 源增事件后按精确 generation trim 主题至 committed 早于最早恢复点；删本 Connector，核槽不活跃／publication 单表再删槽，两个 critical 各唯一打开。源 PG 缺样保持原确认／时间／零恢复；恢复采样风险仍在，正式 Stop 关闭旧代次，再建新任务／新目标按真实源三字段完整 snapshot 并验正常观测。Stop 不可逆，不当自动找回遗失事件。 |
| 真实性能积压 | 自有目标事务锁阻提交、临时 Connect CFS quota3000／period100000（0.03CPU）、≤60000 条 label7000 字节源、批次 1000；Connector max.batch.size=64／max.queue.size=128。不同新鲜样本分别证有消息但提交不变 180 秒、计数推进且延迟>60 秒满 180 秒，及 offset 距离>256 满 300 秒；阈值不是消息条数，不用 Pause／假时钟／JMX 代实停滞。 |
| 已打开事件的 Pause／缺样／恢复 | 确认后 Pause 连续 32 秒：两消费规则暂停、原 incident／LastObservedAt 不变、零关闭；捕获类继续。Resume 记新 execution。暂停 Connect／消息代理分别取新鲜 unavailable，不强求串行有效期重叠，三规则 unknown／原事件保持；解故障、恢复配置／解锁／CPU 后消费 0、延迟<=30 秒满 60 秒、积压严格<128 满 60 秒才关闭原事件。 |
| 触发前 Pause | 至少 30 秒但未达原阈值的真实 pending，三规则无 incident／生命周期／投递。Pause 后两消费规则 inactive、trigger_since／recovery_since 清空；Resume 仅认请求后的全新窗口，180／300 秒临界前须有零事件／投递实证，完整窗口后才打开。捕获类不随消费 Pause 清窗。 |
| 暂停中重建评估器 | 只终止本轮 Monitor，pidfd 证退出、标准 `scripts/dev/start.sh -monitor`拉新 PID，二进制摘要同、环境无验收 Token；任务暂停、execution／generation 未变。新进程 reset_at 晚于重建完成且窗口／通知空，才 Resume 全窗口。不是重建清有效窗口，不算停机时间。两次 Resume 各仅一次，HTTP60 秒、失败未知不重发；三性能收敛最多 600 秒，缺样仍重置并重计，临界／完整窗口断言不放宽。 |

窗口／零计数由自有 Infra 只读 PG 在**同一 SQL 快照**按任务代次／规则／目的地取，对照 User API／签名端，避免跨请求临界误判。场景／派发仍 1650／1700 秒。清源记录后只收清理后新鲜零积压；DB 侧按主键最多三行核原精确两行／三字段及原值、空值沿 psql 文本语义，仅返布尔，不回读整批大行。后继风险绑定 Pause 后 `last_execution_id`；自动恢复须原 execution failed、新 `recovered_from_execution_id`精确指旧、同 task／generation，不改旧终态。

**页面与签名。** 三性能、捕获 warning、两类 critical 各 open／unknown／resolved 阶段，重新核普通身份／代次／原 incident／时间及正式 API。每事件只由真实 Console 确认一次，Runner 不再 API 重复确认，未知不重发；通知页按实收 delivery UUID 分页定位，排除同目标其他执行。捕获／风险不提供性能 evaluation_state，不伪 unknown 标签，用新鲜不可用观测＋LastObservedAt 冻结证明；风险阶段独占源库／Redpanda 同时暂停以取得两来源新鲜缺样。

仅本 Hosted 启现有私网 Webhook 选项，接收端回环随机口；六类各开／关共 12 条，逐一 HMAC／事件／投递 UUID／202／delivered，缺一不算送达。原文／目标地址整页截图／登录输出不归档，正式删目标读回不存在、关闭接收端。受控页面不代真实 Hosted，最终 30 项／17 份浏览器／12 签名范围见固定证据。

### 删除、扫描与清理

| 对象 | 不可省略的后置条件 |
| --- | --- |
| 硬退出 Kafka 任务 | 只读 PG 先证旧 invocation 未确认退出、无删除登记；一次 Gateway DELETE、HTTP60 秒覆盖现有 45 秒 stop 窗口。502／503、任务仍读、会话／外部操作登记前后同，不重发。闭集失败分类 transport_timeout／transport_unavailable／invalid_response／contract_failed 与已收状态分阶段留证；该任务单独记预期保留，由外层销毁。 |
| 正常退出 Kafka 任务 | 新自有任务／独立目标，真实 Worker 写 DLQ、正式 Stop 证会话退出后删 200／读 404、精确 Infra DLQ 缺失，原授权历史可读。原生只取证，不修改会话／Kafka／围栏。 |
| bounded 定时子扫描 | 自动 deep、精确字段／物理行； `metadata_scan.basic_execution_id`、deep 和生产 UUID 各不同，两扫描成功、继承原人类来源／直接父，另一 User 均不可读。关扫描、只 basic 或仅 deep 受理不算通过。 |

专业 `initial_authorization`仅既有状态／受控 code，无 Grant 引用／进度／凭据／附加字段；Monitor 通用 DTO 不含它。业务首败和清理失败分别保留。除已证拒删任务外均等终态／正式删／404；领域预期留存 1 项与外层零残留分别认定。

finally 逐项恢复规则／源记录／锁／Connector 配置／CPU／暂停状态；CPU quota=-1 并读回 `cpu.max`，不能用 `--cpus 0`无更新冒清理。凭据只私有 stdin，结束核任务／槽／publication／Webhook、应用／Business／Infra／凭据。Runner 标准 `make test-transfer-continuous-online-runner`，不以其通过认真实 T4。

### 固定版本结果与未覆盖项

证据正文维护各轮失败、Job、清理和覆盖范围；不在方法页重复逐秒轮询及临时 ID。

| 固定源码／证据 | 独立范围 |
| --- | --- |
| `a8b16b395`，[持续写入](../evidence/Monitor执行监控与留存历史证据.md#2026-11-10-固定源码复验) | 续页／刷新／原终态与替代 Worker 恢复已验；该轮 CDC 审批／Kafka 清理失败，前两轮停准备。 |
| `668187692`，[可选积压](../evidence/Monitor告警与规则历史证据.md#可选积压告警与后继-cdc-完整-hosted-通过2026-11-15)／[完整风险与 scheduled](../evidence/Monitor告警与规则历史证据.md#cdc-性能后继风险重建与-scheduled-完整-hosted-通过2026-11-14) | 原 23 项，默认／可选性能、捕获与丢失风险、恢复重建／scheduled；不追认后继 UI。 |
| `c63888fba`，[43668756347](../evidence/Monitor执行监控与留存历史证据.md#七天-cdc-任务历史的真实-hosted-断言通过2026-11-15) | 19 份历史，峰 41744→0、WAL／延迟／清理；不证图表或七天自然留存。 |
| `419548d39`，[43675672486](../evidence/Monitor执行监控与留存历史证据.md#cdc-历史图表同轮实故障-hosted-通过2026-11-15) | 25 项／8 浏览器，真实故障分钟 null 及恢复 0 图表。 |
| `ef3c407b4`，[43705792261](../evidence/Monitor告警与规则历史证据.md#cdc-pretrigger-pause-hosted-20261115) | 26 项，触发前 Pause／完整 180／300 秒；未重建 Monitor。 |
| `7e583307e`，[43895444567](../evidence/Monitor告警与规则历史证据.md#cdc-evaluator-rebuild-hosted-20261117) | 27 项，暂停清窗跨重建、临界前零事件／全窗口后打开，前两败另保留。 |
| `b8593c1e706e78e23668649eb9595021d32a3e3a`，[44007380675](../evidence/Monitor告警与规则历史证据.md#cdc-tenant-alert-ui-hosted-20261117) | 28 项／11 浏览器，三性能告警 UI，前两失败不抹；不由该轮推捕获／风险页。 |
| `755d24f32`，[44033277901](../evidence/Monitor告警与规则历史证据.md#cdc-risk-alert-ui-hosted-20261117) | 后继 30 项／17 浏览器／12 签名及清理通过，含捕获／两丢失 UI、新鲜双来源缺样、个人任务／槽前后只读不变。 |

以上均不外推其他 Provider、七天自然留存／容量、生产 T5 或下节治理试点；未执行、前置失败、领域清理失败与完整通过不合并。

### continuous 治理与位点恢复试点

仍为[领取与交接契约](../../tasks/Transfer同步执行.md#continuous-领取与会话交接)的待实施标准，不由上列既有 Hosted 或 Runner 追认。第一阶段业务 Kafka→PG，每窗独立中断／接管，逐分区绑定故障前提交位置、目标全行／ledger、Infra checkpoint 及新 Worker 首次位置。

| 中断窗口 | 接替者起点与通过条件 |
| --- | --- |
| 目标事务提交前 | 批首 `next_offset`；业务／ledger 回滚、checkpoint 仍批首，真实重读后都到批末。 |
| 目标已提交、Infra CAS 未成 | 同批首；目标／ledger 已到末、checkpoint 未动，真实重读被 ledger 吸收、无重复业务，再 CAS。 |
| CAS 已成 | 批末之后的 next_offset，下一条可区分消息；旧 token 迟到 CAS 拒绝、旧批不覆新结果。 |

另核：A 运行且空槽、B 具资格时分断企业治理→System／System→Worker；资格到期 B pending、A 原 execution／lease 继续提交，仅恢复网络不领 B，须新确认。两次 `ClaimNext`之间撤资格，第一笔真实 DB 结果及第二笔未发起分别证明。

正常退出须 runner 关闭、旧 `worker_shutdown`不增失败数；强退无停止确认、lease 过期前不接管。两个合格 Worker 争用只一新会话，recovered UUID／递增 token 正确、desired_state 不因退出改变。运行失败／lease 过期核退避／上限／circuit，治理断连不重置 pending recovery／失败数／`recovery_not_before`；无资格保持待领、真领取后才 half_open，成功提交 position 重置失败并保审计，Pause／Stop／schema 阻塞不因治理恢复而重启。

第二阶段 PG CDC 固定快照后的 generation／slot／publication／Connector／topic，交接中源可继续增删改／捕获；新 execution 沿原代次／资源，从 Transfer 已提交 next_offset 接目标，不重快照／重代次／CDC Stop。全行、ledger 和 checkpoint 连续；LSN／Connect offset／Kafka group offset 不代目标恢复位置。本地／Compose 仅用原入口核注册／停领／重注册，无需企业治理凭据；截止时刻／双领取可控时钟，三个提交窗及 CDC 交接须实数据面独立留证。
