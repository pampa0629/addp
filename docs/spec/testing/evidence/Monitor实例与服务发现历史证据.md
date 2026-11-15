# Monitor 实例与服务发现历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本页只保留长期有用的结论、能力限制、代表性证据和待办。结果归对应源码、环境和实际范围，不证明当前 HEAD 已复验；受控进程／页面、真实来源T2、本机Safari和正式身份Hosted分别认定。已解决的准备失败、构建、轮询及CI跟进流水不再保留，详细过程可查Git与原运行。

[清单与绑定](#清单接入与实例绑定) · [多Worker](#多-worker-与副本隔离) · [筛选与趋势](#服务筛选与分页恢复证据导航) · [cgroup](#服务实例-cgroup-证据导航) · [部署预期](#服务组部署预期与运行确认证据导航) · [观察器](#部署观察器生命周期) · [启动失败](#backend-启动失败与就绪屏障) · [本机采集](#本机真实采集与预算) · [待办](#后续待办)

现行身份、权限、采样与展示统一由[平台运行观测规范](../../addp平台运行观测规范.md)维护；启动／停止和构建边界归[开发生命周期规范](../../addp开发服务生命周期与构建身份规范.md)，本页不重复维护操作流程。

## 清单接入与实例绑定

| 固定源码与最小Hosted证据 | 结论与限制 |
| --- | --- |
| `fb13955d8f3cc48b254f2c840e55b8f8f638d2cc`；[39127927703](https://github.com/pampa0629/addp/actions/runs/39127927703) | 单台Linux VM仅创建一次主机，正式400后只重试同一主机的采集配置；System清单与Monitor独立读取、刷新及数量通过。采集API删除404、主机停用版本递增及部署／Infra清理通过；**不表示产品已提供主机DELETE**。 |
| `fee7cb91d8ef674d2beb41036a288519bc1d6be7`；[39169909912](https://github.com/pampa0629/addp/actions/runs/39169909912) | 一个新Monitor进程绑定当前主机，旧实例退出为not_active；对等System实例保持未绑定。主机停用后同进程绑定拒绝但自身资源有效，启用后恢复。真实Console导航、九项标量与主机版本、返回／刷新保留筛选通过，配置退役和设施零残留。 |

仅证明一次性Linux VM的单新进程，不外推原生macOS、多进程／多主机、Business承载或生产T5。端点、进程实例和主机绑定是不同事实，不从地址、名称或数据库所在VM猜绑定，也不在线改挂旧实例。

## 多 Worker 与副本隔离

**真实来源T2**：冻结基线 `6d42e4621f2bed671814c11eb365fcde4064f301` 加归属测试输入，在既有 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality` 中验证一个Backend与两个同模块Worker的独立实例／端点／启动时刻。CPU／RSS／趋势不可借用对等实例，停Backend或一个Worker只清失效来源，另一Worker继续有效；HTTP告警／通知／恢复及项目容器／网络／卷清理通过。最小日志 `/tmp/addp-same-role-workers-t2.log`。这是Linux SDK／Prometheus／查询T2，未证明真实System多Worker登记和主机绑定。

<a id="meta-双-worker-标准启动与登记绑定专项"></a>
<a id="transfer-两类双-worker-标准启动与主机绑定验收入口2026-10-22"></a>
<a id="quality-多-worker-标准启动登记与主机绑定入口2026-10-22"></a>

**已接通的标准启动**：Meta与Quality可选1／2个Worker；Transfer bounded／continuous各可选1／2，单副本也用具体 `-1` 单元。复用健康实例、只补缺失，拒绝活动缩容、重复／外来PID和非法数量；Transfer continuous双副本不能共用固定Owner。副本数不等于每进程任务并发，未修改claim／租约／fencing或IAM。各批开发生命周期、Online runner与隔离临时进程门禁已有通过；日志分别见 `/tmp/addp-meta-workers-dev-lifecycle-receipts.log`、`/tmp/addp-transfer-workers-final-audit.json`、`/tmp/addp-quality-workers-final-audit.json`。

**仍未取得真实Hosted**：`platform-node-metrics` 的 `meta-workers`、`transfer-workers`、`quality-workers`仅完成入口和本地验证，不把脚本存在、命令编译或临时进程当正式业务Worker页面通过。真实登记／绑定、停止补回、主机启停与中心故障的完整链路须独立验收；任务竞争、租约接管、跨主机和HA不由此证明。

## 服务筛选与分页恢复证据导航

**受控验证结论**：登记端点、承载IP、宿主／环境名称、时间和离线原因分别按自身口径匹配，不能相互代替或为无监听端点的Worker猜地址。筛选在计数／分页前生效，已提交条件随分页、刷新、重载和Console单iframe导航恢复；未提交草稿不影响自动刷新。`lease_expired`只表示失联，不证明进程退出。

<a id="服务监控自动筛选与就近提示2026-10-29"></a>

自动筛选的下拉即时提交、文本防抖／IME、回第一页、清旧趋势、重置与迟到提交取消已由本地Monitor497项浏览器及构建验证；共享前端聚合当时仍有一项所有权失败，不记全套通过。时间／离线原因此前的完整Monitor门禁分别272／275项通过，受控页面不计正式身份Hosted。公共交互归[自动筛选与已提交范围](../../../../common-frontend/docs/components/页面状态与导航.md#自动筛选与已提交范围)，不再保存逐控件、逐语言的门禁流水。

<a id="服务进程-cpurss-相邻时段比较本地"></a>
<a id="服务趋势弹窗就近提示2026-10-31"></a>

**趋势与比较**：两图独立视口、同上下文刷新保留、切换身份／范围或失败清空已有受控T3。CPU用核、RSS用原容量；相邻等长时段比较按完整评估网格求均值及原单位差，零基线有效，缺样／陈旧／启动裁剪不出差值，不计算增长百分比，缩放不重算比较。最终比较定向33项、构建通过，不能说末次修改后又全量复跑；提示组合首轮82通过／2失败，修正断言后两项定向通过。最小记录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-comparison-s_1g44u9`、`/tmp/addp-service-trend-tips-scope.log`。个人Safari仅另证布局、真实曲线与提示可见，未补证正式身份长窗／同比。现行口径归[规范6.3](../../addp平台运行观测规范.md#63-趋势时段比较与总览)。

## 服务实例 cgroup 证据导航

**已验证的采集层**：固定 `1f9b44df83d48a78b96fbe4b3d91e447624b8518` 的 [40085914201／Job120853412269](https://github.com/pampa0629/addp/actions/runs/40085914201/job/120853412269)实际通过 `cgroup-source`。三个真实SDK共享一个受限组，核对独立boot／组inode／PID namespace、完整分钟CPU与memory.current及错身份拒绝，设施零残留。合法 `core_sched.force_idle_usec` 点号字段解析已修，畸形、超量和非规范输入仍拒绝；无样本不补零。旧Job缺样没有当时逐来源诊断，不能倒推它唯一由此字段造成。

**配额视图的证据边界**：部署只读证明、System当前租约／实例精确关联、Monitor当前用户先授权及查询后重核、无完整层级／有限上界不算利用率，已有T1、真实PostgreSQL和受控页面T3；本地最终414项浏览器及构建通过。最小记录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-container-chain-final-bxc7x1qp/`。共享组T2**不证明一实例独立容器、原生Linux完整限制层级或正式身份配额页面T4**，原生Mac无容器限额；不靠提升应用权限或Docker socket补证。现行职责归[规范6.1](../../addp平台运行观测规范.md#61-固定指标目录)、[规范6.2](../../addp平台运行观测规范.md#62-资源查询与预算)。

**240迁移已恢复**：固定 `954220f1688f1737dc8ffc0984aecfc98019ea4a` 修正JSONB约束表达式，隔离PG验证严格前置、失败回滚与限定恢复；获用户授权后现场先恢复239/clean，正常Runner后只读确认240/clean、摘要及保护完整。只证明迁移，不是服务就绪或容器链路验收；恢复条件与授权统一见[240定向恢复](../../../guide/troubleshooting/数据库迁移.md#240-号进程内核身份声明迁移的定向恢复)，不复制恢复命令。

## 服务组部署预期与运行确认证据导航

<a id="2026-10-25-服务组部署预期协议与只读租约汇总自动接入未完成"></a>
<a id="2026-10-25-标准启停接通服务组预期与停止前发布屏障"></a>
<a id="2026-10-25-确认运行前置组预期变更标记消歧"></a>

**已完成的底座**：部署入口按模块／角色／用途原子发布预期，System只读汇总当前有效租约；计划停用必须在首个停止动作前成功发布。局部启停不改其他组，发布失败不伪装实际成功；同秒实质变更用严格前进的UTC微秒标记区分，时钟停滞／回退拒绝写入，幂等重发不变。真实PostgreSQL、发布／停止屏障和本地生命周期门禁通过；最小记录 `/tmp/addp-service-expectations-postgres-final.log`、`/tmp/addp-service-intent-hooks-lifecycle-final.log`、`/tmp/addp-service-intent-confirmation-receipt.json`。

旧标题“自动接入未完成”只指早期阶段。后续规则与持久运行确认已经接通，**部署意图不证明实际运行，历史登记总数不证明首次在线确认**。现行权威正文为[规范4.8](../../addp平台运行观测规范.md#48-服务部署预期与分组租约观测)与[规范7.1.1](../../addp平台运行观测规范.md#711-服务组失联规则)。受控数据库、个人页面及尚缺的真实租约故障／恢复／签名通知Hosted统一见[告警证据](Monitor告警与规则历史证据.md#alert-evidence-availability)，不重复维护状态和验收摘要。

## 部署观察器生命周期

**停止路径**：单次SIGTERM落入Popen析构时可能使SystemExit被忽略，已在受控真实子进程及隔离Linux重现。修复沿合作式取消、唤醒和明确检查点退出，先回收自有Docker子进程，不重发命令或重置预算；归属拒绝、单次TERM及未确认退出保留运行记录不变。固定 `1fb051a50e7c46975411542a0e9340f7fcb97929` 的 [40219785488／Job121248639865](https://github.com/pampa0629/addp/actions/runs/40219785488/job/121248639865)实际Platform门禁通过。更早间歇退出失败缺当时Python栈，后继未复现不等于全部旧故障唯一归因；若再发生，沿已有有界诊断取证，不靠加信号、强删或扩大等待制造成功。

**ready交接竞态已闭合**：旧ready在新写入者启动后才退役，父进程打开的旧inode被原子替换后nlink变0，严格单链接检查正确拒绝。固定 `029e14776064a9a033ae2dd859f44951a0903b33` 在原控制锁内先校验并移除旧ready再创建新child，保留同环境／仅凭据变化的幂等路径及全部私有文件校验。确定性文件屏障复现后修复，七种危险文件仍在创建child前拒绝；[43620893019／Job132526100819](https://github.com/pampa0629/addp/actions/runs/43620893019/job/132526100819)实际通过56项发现、6项证明及完整Platform门禁。**不是缺少原子写，也不放宽nlink检查**；不从一次通过外推其他私有文件或长期运行认证。

## Backend 启动失败与就绪屏障

<a id="开发-backend-启动失败与重试的-postgresql-t22026-11-07"></a>
<a id="开发-backend-真实构建失败与重试的隔离运行验收2026-11-07"></a>
<a id="开发-backend-进程退出与就绪超时的隔离运行验收2026-11-07"></a>

**已验范围**：基线 `51b5b26c7c5e88fc8d7e301c29c0dd01b540df56` 加归属测试的真实PostgreSQL，验证失败首次可见、重试清除、拒绝旧尝试迟到、登记成功后清除及注销不复活。固定 `26aa994ec405585e9306a76c6d9ca6083d25553a` 的一次性Lima Linux VM另以标准启动、正式密码／MFA和平台模块读取权验证：Standard真实编译失败后成功重试；Catalog登记前退出／持续未就绪超时后重试恢复。Console失败原因和新实例在线结果真实可见，VM及存储删除。只计本机隔离真实运行，不是Hosted、生产Compose或Worker验收。

<a id="backend-就绪等待的总预算与复用进程初始化修复2026-11-08"></a>

**后续等待修复**：连接／响应／休眠共同消耗原总预算，迟到HTTP成功不计就绪；复用System也先初始化预算，Ontology原单请求5秒上限及Manager专用存活入口保持。终端与发布原因复用同次PID判定。部署发现和完整本地生命周期门禁通过，最小日志 `/tmp/addp-backend-readiness-budget-final-verified.log`、`/tmp/addp-backend-readiness-lifecycle.log`。Ontology旧夹具适配及登记遗漏由 `2b05bc27c8452cc58a7775449a413725abaac0a2` 的 [Platform Job128673609447](https://github.com/pampa0629/addp/actions/runs/42462171750/job/128673609447)／[Falkor Job128673807568](https://github.com/pampa0629/addp/actions/runs/42462171776/job/128673807568)实际通过，不再作为待办。

**Standard新版Hosted仍未执行**：`platform-node-metrics / standard-startup`已有本地runner和Console受控验证，原60秒就绪／1800秒场景预算不变；旧Lima尝试在环境准备期因EROFS或宿主空间下降中止，未注入所需故障，且不能据guest剩余空间推定底层原因。准备资源已清理，不能用其容器健康、旧版本Catalog超时或本地总预算修复替代新Standard链路。入口见[正式场景](../scenarios/平台观测与语义生命周期.md#standard-开发-backend-启动失败与重试恢复)。

## 本机真实采集与预算

**2026-11-08单副本恢复**：将计划中的旧Worker键改为真实 `meta-worker-1`、`transfer-bounded-worker-1`、`transfer-continuous-worker-1` 后由用户标准重启。八项真实目标均唯一UP、严格mTLS与当前UUID匹配；Safari实际打开三个Worker的CPU／RSS趋势并观察刷新推进。最小记录 `/tmp/addp-worker-restarted-live-20261108.json`、`/tmp/addp-worker-restarted-safari-observations-20261108.json`。本轮没有读取完整趋势数组，不能说全部21点有效或重验缩放／比较；八实例仍未关联原生Mac主机，不能借Docker Desktop VM绑定。

**后续来源与预算**：首次20项计划因12目标上限报 `obs_budget_exceeded`，配置／证书准备不是实际采集。随后target-budget隔离T2以1CPU／2GiB、15秒采样验证24→34→35超限→34恢复；34目标约25816活跃时序／1720.55样本每秒，容器及卷清零。计数器／路由重放是受控负载，不证明真实产品吞吐、500000样本持续压力或七天实际磁盘占用。

2026-11-10用户应用后，本机只读mTLS复验34个外部目标全部UP（27进程、6引擎、1主机），新增十后端各一健康目标、启动与HTTP采样新鲜，八个Go后端另有新鲜CPU／RSS。最小成功日志 `/tmp/addp-metrics-34.wXH9VB`。这是当次真实采样，不补证所有实例真实页面、原生主机绑定、多副本任务竞争或正式身份Hosted；私有计划、端点、证书与预算由部署规范管理，不在历史页保留配置制作流水。

## 后续待办

1. **优先补Standard新版启动失败／重试Hosted**，核对真实失败提示、新尝试身份、实际Ready、控制进程保持及清理；旧版本本机隔离结果只保留其原范围。
2. **多Worker与独立容器分别验收**：Meta／Transfer／Quality专项补真实登记、主机绑定、停止补回和页面；容器配额补完整限制层级及正式身份链路，不能用共享组T2代替。
3. **个人环境剩余范围按需补证**：原生Mac登记及新实例显式绑定、1小时／24小时真实长窗、正式身份比较等；先核对所属专题后续结果，不重复已有有效采样或把配置准备计通过。

服务组失联的真实故障／通知待办统一归告警专题。240迁移、点号字段、ready交接和已闭环的runner适配不再列为待办；本次整理不运行服务、修改权限或重跑历史验收。
