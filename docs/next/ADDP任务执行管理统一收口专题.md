# ADDP 任务执行管理统一收口专题

更新时间：2026-11-14

**当前状态：公共执行、Graph 基础持久运行、十个 Cleanup Owner 及安全读取已接线。Common／System 父子 PG、Graph 已列竞争与原 scan 比对、最近执行导航及共享前端独立 Linux 检查已有固定源码通过证据；Manager 三项、Transfer 三项原生清理及后继 Kafka／CDC 整套验收已分别通过。剩余为第六节列明的专用测试缺口、真实验收与待讨论操作；Manager 迟到 writer 未 Hosted，正式用户阶段追踪仍保留 causal-link 首失败。第九节导航补充方案已确认，runner／私有清理台账及本机专项已补齐，Linux 证据待本批提交后核验；新的真实运行仍须单独确认。Graph 高级恢复继续暂缓。**

本专题保留当前方案、已确认边界、关键证据及剩余工作。逐轮排队快照、命令流水、临时测试计数和重复失败过程已合并；历史原件沿固定提交、GitHub Run／Job 和原有私有证据目录追溯。整理不改变契约，也不将历史失败或未运行项改记为通过。

## 一、范围与正式契约

本专题处理 execution 创建之后的领取、运行、续租、恢复、终结及安全投影，包含没有持久任务定义的一次性和运维执行。触发去重、Cron、DAG 和 Owner scheduler 迁移由[调度与任务编排统一改造专题](ADDP调度与任务编排统一改造专题.md)独立负责。

正式规则以以下文档为准，本专题汇总进展与未决事项，不维护第二套状态机：

- [任务体系规范](../spec/addp任务体系规范.md)、[术语表](../concepts/addp术语表.md)。
- [执行记录与租约](../spec/tasks/执行记录与租约.md)、[模块任务契约](../spec/tasks/模块任务契约.md)、[执行监控](../spec/tasks/执行监控.md)。
- [Cleanup 体系规范](../spec/addp-cleanup体系规范.md)、[Transfer 同步执行](../spec/tasks/Transfer同步执行.md)、[引擎插件接口规范](../spec/addp引擎插件接口规范.md)。
- [测试与验收规范](../spec/addp测试与验收规范.md)、[Owner 补充门禁](../spec/testing/Owner补充门禁.md)、[CI 与交付规范](../spec/testing/CI与交付规范.md)。

## 二、统一职责与生命周期

最初问题是公共状态和时间依赖各 Owner 自律维护，Graph 依赖进程内运行句柄，Cleanup 同时使用 Redis、公共执行记录和临时 watcher，外部效果与 PG 结果又无法原子提交。本轮按下列职责收口：

| 所有者 | 唯一职责 | 不能据此推定的事实 |
| --- | --- | --- |
| Common | 公共状态、时间、attempt、租约和受控写入原语；支持 Owner 外层事务 | 不裁定业务权限、安全重放或外部提交结果 |
| Owner | 冻结业务输入、当前权限、业务结果、外部操作和安全重放资格 | 持有 lease 不等于拥有全部数据权限或外部运行体已停止 |
| System | Cleanup 请求／协调及目的限定执行授权 | 不跨读 Owner 私表，不代推子 execution，不替 Owner 核清效果 |
| Monitor | 经 Owner 当前读取裁决的公共安全投影 | 展示不修补错误持久事实，GET 不推进状态 |

| 公共转换 | 必须满足的规则 |
| --- | --- |
| 创建 pending | 身份／来源／配置固定；开始、完成、耗时及有效租约为空 |
| pending → running | 原子写开始时间、attempt + 1、新 token／owner／到期时间；Owner 摘要同事务 |
| heartbeat／progress | 核对当前有效 lease；身份、开始时间和终态字段不可改写 |
| 过期 → pending | 仅 Owner 明确允许安全恢复的未终态执行；保留 UUID／created_at，清除本 attempt 时间、租约及应重新签发的派生授权 |
| running → 终态 | 锁后数据库墙钟、严格未到期；统一完成时间与当前 attempt 耗时，清租约并禁止复活 |
| 未开始 → 拒绝／取消 | 明确前置条件，无 started_at、耗时或虚构 attempt；Graph 直接取消仅限事务证明从未启动的首次 pending |
| 人工重试 | 新 execution、当前权限及新的冻结输入；旧终态和前驱关系保留 |

同一 attempt 的 started_at 不可变。PG 业务结果、公共终态、必要事件及回执在 Owner 事务内原子提交；外部效果使用独立操作事实，不能把 PG 回滚解释为外部操作未发生。旧运行体失权后不得更新进度、结果、终态或事件；只有契约允许的、绑定原身份的结束证据可追加。

已有 bounded Owner 保留各自恢复资格；Transfer continuous 保留独立 runtime lease、position 和 fencing，通过受控适配维护公共执行事实，不套用 bounded 重试。公共时间使用数据库锁后墙钟，未知效果不自动重发；不新增通用 attempt 历史表、万能更新或兼容生产路线。

## 三、Graph 已实施路径与恢复边界

Graph Backend 内嵌有界监督器，使用 Common 持久队列；API 提交即入队，进程内句柄只用于控制当前合法运行体。已开始执行失联默认 failed，不自动重放写图。

- **输入与目标冻结：** 固定 execution、材料有序清单及不可变内容身份、本体／空间映射和配置目标；凭据不进入快照。执行前复核当前权限，不从可变定义重解析成另一目标。
- **锁与提交：** 既有执行按 E → G → T／B；新入队按 G → T → 插入新 E。E 为 execution，G 为 Graph Tenant 级准入行，T／B 为任务和业务行。同类行排序；原生调用和等待均在 PG 锁外。
- **取消与失联：** 持久记录准确 execution／attempt 的取消意图；受理不表示已取消。合法运行体停止新派发并确认在途调用收敛后才能终结；无法确认则保留未决事实。终态竞争不追改已提交成功。
- **外部操作：** 短事务先登记准确操作、身份和目标，再锁外调用；合法所有者确认完整结果。失权后的晚到本地返回不解除 unknown，lease 到期、断连接或瞬时空运行列表均不构成停止证明。
- **Cleanup 交界：** 子 E → G 排他关闭准入；锁外按目标 E → G → T 取消／等待；最后子 E → G 排他核验静止并清理。不持 G 反锁其他 E。材料、审核、删除和重绑定均遵守同一准入；Engine 删除仍保留用户定义。

已确认的控制 API：`POST /api/v1/graph/graphs/{id}/reconcile` 仅接收 version，由服务端核验证据，要求 `graph.graph.reconcile`；`POST .../{id}/reopen` 同时要求 update 与 reconcile，只允许已核清、无活动执行的原物理目标显式开放。Permission 不默认授予角色；禁止 force、上传日志、任意 SQL 或“我已确认”布尔值，不自动重跑、不复活旧 Cleanup，换目标另议。

已确认的只读分期方向保持：Common 只投影准确原生事实与明确不支持，Graph 独占原绑定、证据采信、当前权限及准入裁决；不写业务图控制节点、不增原生管理员。FalkorDB 固定4.20.6尚无充分代际／操作回执证据，继续阻断。Neo4j 现有 ExecuteQuery 可自动重试；未来受控效果必须走不自动重发的唯一原生入口，并在同一原生执行边界核验绑定，不能用先读身份后写入替代。异步索引／约束与 Copilot 各需独立完成／停止证据。

**实现与能力分开：** 持久运行、取消、准入及严格拒绝路径已接入；现有 reconcile 只消费完整 known 操作。配置中的 Engine／version／CatalogPath／index_management 不是原生身份。恢复证据 Provider、可信原生绑定和正向 reopen 尚未实现，当前缺证据继续 409；用户已决定暂缓高级恢复，沿用 System 永久登记身份，不能据此替代原生效果证明。Neo4j 已测身份组合不足及暂缓决定见 §8.41—8.42。

## 四、Cleanup 已实施路径与外部效果

### 4.1 单一持久队列及父子结论

System 在同一 PG 事务冻结请求、actor／Tenant、action／mode／cause、确认和影响摘要、原 deadline、参与 Owner，创建父 pending 及每 Owner 一个子 pending，并提交审计。唯一领取队列是 `common.task_executions`；旧 Redis task／result／request Stream、临时 watcher、GET 写状态和无 lease 更新路线已移除。

| 事实 | 所有者与规则 |
| --- | --- |
| 受理与幂等 | System 私有请求；同一请求 ID 绑定主体与规范摘要，相同 ID 不同内容拒绝，不因超时静默新建 execute |
| 子运行 | System 预建中性意图，Owner 独占领取和推进；同父同 Owner 唯一，不写 source_task_id 冒充有定义任务 |
| 领取与关闭 | 父 P → 子 C 锁序，锁后核验父仍开放、原 deadline 和请求绑定；System 只关闭准入，未领取子由 Owner 拒绝为无开始时间的 failed |
| 完成回执 | 类型化、有界 `metadata.cleanup_receipt`；Owner 私有结果、回执、合法子终态与 PG 效果同事务，终态后不可补写或改写 |
| 父结论 | 原截止时冻结汇总；恢复父仅重读持久事实，不重建子、不刷新 deadline、不重放删除 |
| 迟到与失权 | 仍有有效 lease 的子可提交真实迟到结果，父终态不改；失权子拒绝写回，未知外部效果继续隔离 |

父 timeout 不伪造 Owner timeout 回执；partial_success／skipped 不计为严格删除成功。scan 预计量和 execute 实际量分开；Engine deleting 通过权威复扫及全部必需 Owner 严格成功才可完成删除。

### 4.2 已确认的授权与安全读取

- **执行目的：** 复用 System execution authorization。通用 Cleanup 要当前 `system.cleanup.execute`；Engine 删除派生要当前 `system.engine.delete`，不再叠加通用 Cleanup 权限。cause 由持久来源决定，调用方不能自报切换用途。
- **运行裁决：** 十个对应 Owner runtime 使用 `system.cleanup_authorization.execute`，固定 client／Owner，绑定准确请求／父子／digest／action／scope／attempt／lease；领取与产生效果前复核原用户当前成员、权限和有效窗。不保存 User Bearer，不给普通角色默认增加权限。
- **首期来源：** 只支持 active Tenant 的可信用户请求及已确认 Engine 删除派生；closed Tenant、无可信用户来源事件继续阻断，历史 Tenant 治理另议。
- **公共展示：** 保留真实 pending／failed；父子分别经当前读取授权，且有已提交父关闭或 Owner 拒绝证据才显示“协调已关闭、模块未开始”。Owner 不可用失败关闭，GET 零状态推进。
- **专业结果：** 十 Owner 的 `GET /api/v1/{owner}/cleanup-executions/{execution_id}/result` 各自要求不默认授予的 `<owner>.cleanup_result.read`、当前 Tenant User、子读取范围及本人一次性执行归属，首期仅第一方 unrestricted User。System 浏览器经 Gateway 按需读取，不由 System 后台聚合正文或跨读私表。
- **公开字段：** 保留有用途的计数，原始错误 JSON 改为安全类别和计数；不透传 SQL、对象路径、凭据或任意 details。身份／Tenant／请求变化、读取失败和迟到响应必须清除旧值；父可读不授予子或专业结果读取权。

请求、回执和专业结果首期不新增自动物理保留清理，不能套用事件 30 天或概览评估期限。用户允许研发旧协议记录清理，但不将旧 Redis 结果导入为新可信记录，也不凭删记录证明外部运行已停止。

### 4.3 Owner 外部效果与删除语义

十个 Cleanup Owner 为 Asset、Develop、Graph、Manager、Meta、Model、Quality、Service、Standard、Transfer，均已接线。Engine 删除按正式 cause 注册冻结 **七个** Owner：Manager、Meta、Transfer、Develop、Service、Quality、Graph；不能套用通用 Cleanup 十 Owner 或接受任意子集。

| Owner／用途 | 已收口的责任与安全约束 |
| --- | --- |
| Meta | 仅处理本域 PG，删除 Cleanup 内向 Manager 隐式委托索引删除的路线；准确 Engine 范围和权限失败保持阻断 |
| Manager 索引 | 独立治理自己索引中的失效 Engine 投影，包括 Meta 已无记录的残留；完整有界枚举、准确生命周期及未决投递核验，截断或范围漂移拒绝 |
| Meta＋Manager 手动 execute | 同一次 scan 和确认必须显式配对；Meta-only 返回 409／cleanup_manager_scope_required，零受理／零效果，不自动扩范围 |
| Manager 产物／索引 | 操作绑定 C／attempt／准确目标；持久 hold 约束初始化、Open、ActivateIfSettled、ClaimMaintenance 和新投递。原生完成事实不独立解除 hold；结果事务失败保留隔离，禁止迟到写入绕过 |
| Manager GLB | 保留合法 Cleaned 输入审计，统一 execution → Task → 产物锁序；failed 不等于旧上传停止，未知效果不重复删除 |
| Transfer | 绑定 Capture generation、source fingerprint、connector／topic／group 及 ResourceVersion；实际 invocation 结束后才收口，TTL 和停止意图不代替结束证明 |
| Engine 删除入口 | 保留并禁用 Transfer 任务，保留 DLQ；按既有工作流清理自有 Capture，不借此删除任务或业务行 |
| Transfer 显式 task DELETE | 使用当前普通用户既有 task.delete，独立验证 DLQ 删除及 unknown，不冒充 System 父子 Cleanup 闭环 |

Transfer 针对精确 deleting Engine 的源端连接使用已确认的 `transfer.capture_cleanup` 窄用途：System 现有执行授权出口 `POST /api/v1/system/execution-authorizations/{id}/cleanup-engine-connection`，只供对应 Transfer Backend Owner runtime。绑定开放父请求、当前 C／attempt／lease、原用户 engine.delete 与准确 Engine；凭据仅本次调用内存使用，返回前后复核有效窗。Common 客户端拒绝未知字段／重定向，失败不刷新重试；不放宽普通连接的 active／online 规则，其他 Owner 不借用此用途。

Transfer 新 producer 的持久围栏拒绝已统一为 HTTP 409／`transfer_cleanup_unresolved`，不暴露内部错误链；显式删除的外部清理失败仍为 503。回包丢失只登记 unknown，不自动重发、不清掉任务／索引或持久 hold。

## 五、验收入口与证据口径

### 5.1 标准入口

| 范围 | 标准入口／核验重点 |
| --- | --- |
| Common 执行 | `make test-common-postgres`；真实 PG 竞争、锁后到期、恢复／终态、旧写拒绝及事务回滚 |
| Graph PG | `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"`；运行、取消、准入、未决操作及 Cleanup 竞争 |
| Graph 原生 | `make test-common-falkordb-unit`、`make test-common-falkordb`、`make test-common-neo4j`；两引擎分别记录，不互相代验 |
| Cleanup 协调／授权 | `make test-system-cleanup`、`make test-system-iam-postgres`；准确用途、来源、父子绑定、当前权限及租约 |
| 各 Owner | 既有 `test-<owner>-postgres` 和 `make test-module MODULE=<owner>`；按真实共享依赖扩散，Go-only 仅计 T1 |
| 专项 runner／登记 | `make test-execution-online-ci-runner`、`make test-graph-online-runner`、`make test-cleanup-native-online-runner`、`make test-t2-ci-registration` |
| 真实故障 | 已登记 Hosted suite，经唯一 `make test-online ONLINE_SUITE=<suite>`；固定源码、身份、case、预算及清理范围 |

System／Transfer Go 与 Execution runner 已有独立 CI Job，避免聚合前项失败使本任务没有实际结果；聚合失败仍为失败。共享 Make／workflow／影响选择及登记由 CI 负责人统一整合，新增测试的真实消费者和输入必须同次覆盖。

本地、选中 CI、聚合 CI、T4／Hosted 与 T5 分层计证；queued、skipped、cancelled、未运行均不计通过。输入未变的有效结果可以复用，但文档提交的选择跳过不替代前序代码验收，清理通过不替代业务断言。历史本地 `test-changed` 前置失败、`test-platform` 工具链阻断及部分前端／IAM 失败不追改为全绿；后继实际 CI 仅关闭其对应缺口。

### 5.2 专项身份、故障与退出

`cleanup-native-effects` 每次显式选择一个 case；必需断言唯一来源为 `scripts/test/execution-online-contract.json`。现有 case 包含 Manager success／object-unknown／index-unknown／late-writer，以及 transfer-engine／dlq-unknown／capture-unknown；不能静默跳过未实现断言。

原生专项使用三名分离的普通 User：Manager 与 Transfer 各复用既有十项业务准备权限；Engine User 精确六项为 engine.create／read／delete、cleanup.read、Manager／Transfer cleanup_result.read。前四项通过不默认分配的 `tenant.engine_lifecycle_operator` 模板，两项专业读取通过窄自定义角色组合；不借用管理员、撤权控制身份或扩大默认角色。

Manager 源准备保持其正式授权链；本批 Transfer CDC 仅验证既有 Tenant／任务权限下实际读取，不再创建无消费用途的源 Grant，也不宣称 Grant／Deny、源撤权或运行中收权已闭环。

故障仅作用于本轮准确容器／进程／连接和自有目标。proxy 必须证明准确原请求真实发送、收到 upstream 回应后丢 downstream 回包，并阻断重发；原生探针独立直达服务，不能用客户端超时推断效果。诊断只留闭集阶段／状态／安全原因，读取耗时计入原预算；不归档原始响应、token、SQL 或私有连接。

退出必须恢复本轮暂停与 barrier，停止本轮运行体、删除自有夹具并销毁 Infra／卷／凭据，核实际资源零残留。场景失败与清理失败分别记录；unknown 不靠产品 DELETE 强清或删除执行记录消除。

## 六、剩余工作与共享环境

### 对账口径与已落地项

2026-11-14 按当前已提交源码、标准入口和已归档固定版本证据重新对账；检索起点为 `4741647a9`，其它会话未提交改动不计交付，也未纳入本轮文档提交。2026-11-08 的 `31a653f76`／`839a4d6c0` 仅保留为早期对账／父子 PG 基线。G1—G9、C1—C6、V1、C2a—C2j 与 C5a—C5m 的逐项要求沿[Owner 矩阵](../spec/testing/Owner补充门禁.md#graphcleanup-执行管理最小验证矩阵)核对；源码存在、专用用例覆盖、固定 SHA 实际运行和真实服务验收分别认定。本轮只读核对并修订文档，验证仅执行文档链接检查和标准变化选择触发的平台检查，没有新跑 Graph PG／原生引擎、浏览器、Hosted 或本机真实窗口；复核的五个官方 Job 的源码与 success 终态和既有原始记录匹配，未把旧证据升格为当前 HEAD 全部通过。

| 项目 | 已定位代码／标准门禁 | 实际证据与剩余边界 |
| --- | --- | --- |
| Common公共执行、P→C、回执与结果原子提交 | `common/execution/cleanup.go`、`common/cleanup/runtime.go`；`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test execution-lifecycle"`，包含cleanup_protocol／cleanup_lock_order及professional-result原子提交 | `839a4d6c0` [Common PG](https://github.com/pampa0629/addp/actions/runs/42446889335/job/128628596647) 实际success，原日志包含全部七个新增竞争／拒绝／回滚子项、原生命周期及结果原子提交，0SKIP；C2细矩阵仍非全覆盖 |
| Graph持久构建／取消／准入、Cleanup三阶段 | `execution_control.go`、`build_runtime.go`、`cleanup_runtime.go`；Graph PG分组已包含repository与service runtime | 同一Common PG日志包含ExactClaimAndCancellation、WaitForAdmissionPastLease、UncertainEffects及GraphCleanupRuntime；不代表全部锁排列、真实原生回包丢失或positive reopen |
| System请求／幂等／父协调／只读结果 | `cleanup_acceptance.go`、`cleanup_durable.go`、`cleanup_read.go`；已有 `make test-system-cleanup`，不借IAM代验 | `839a4d6c0` [System Cleanup PG](https://github.com/pampa0629/addp/actions/runs/42446889335/job/128628596160) 实际通过8个主用例，新增ParentRecoveryAdmission／ClosureRollback及原PublicScopeSerialization均通过，0SKIP |
| System／Monitor安全展示与专业结果 | 公共ProjectCleanupAdmission、System读取、Monitor查询及共享CleanupAdmission／CleanupModuleResults已接；Common前端、System／Monitor前端与Monitor PG标准入口已登记 | `66d0321de` [Monitor PG](https://github.com/pampa0629/addp/actions/runs/40880761833/job/123475011425) 含CleanupObservationSnapshot，0SKIP；共享parser／唯一所有权T1及页面代码存在，C5消费者T3／服务T4尚非全矩阵 |
| System永久登记身份 | EngineService.Create／Restore与 `TestDeletedEngineCanRestoreItsIDAndDoesNotReserveAddress` 已定位；复原旧登记保留ID，新登记取得新ID | 代码／用例存在，不据此声明迁移、恢复、跨Owner重绑完整T4通过，更不当作严格原物理代际证明 |
| Manager原生成功／对象未知／索引未知 | `cleanup-native-effects`三个显式case，原权限及9／11／11项 | 三个固定源码Hosted已通过，见§8.43；本次复核各Job的success／源码，与原官方报告记录对照，不重跑 |
| Transfer Engine／DLQ unknown／Capture unknown | 三个显式case及两种产品删除入口，原7／7／8项 | 三个固定源码Hosted已通过，见§8.2；不升级为源撤权或所有Owner的物理清理证明 |
| Manager迟到writer | `run_late_writer`及精确容器／上传barrier控制已实现；`make test-execution-online-ci-runner` | `9cd0b267d` [Linux runner](https://github.com/pampa0629/addp/actions/runs/42397885129/job/128479082973) 实际408项；12类新增反例及父timeout／子failed契约已收口，真实Hosted仍待单独确认 |
| Graph／Cleanup受控故障runner | `graph-execution-online.py`、`cleanup-execution-online.py`及浏览器撤权脚本已登记；`make test-graph-online-runner`／Execution runner | 受控提取、十Owner逻辑结果、Owner故障、截止、撤权／分页／迟到响应均有runner；本专题尚无这两个profile的固定源码Hosted通过记录，不重复开发已存在入口 |
| Neo4j身份反例T2 | `make test-common-neo4j`固定5.26.24、九阶段；启动与清理夹具已修正 | `9cc552de2` [Linux T2](https://github.com/pampa0629/addp/actions/runs/42216557525/job/127891879034) 实际success、九阶段、0SKIP、zero_residuals；后继包含19e，旧启动故障已关闭，不再列待办；高级恢复能力未实现 |

### 本次新增关闭的记录缺口

| 已完成项 | 固定源码与实际证据 | 对账结论 |
| --- | --- | --- |
| Common／Graph 原 scan 基线 | `c05f2292c` 的 [完整 Common PG](https://github.com/pampa0629/addp/actions/runs/43351341944/job/131643546461) 与 Go workspace 已通过，原始 PG 含 Common 14 项、Graph 原组及新增 6／2 项，见下方实施验证 | 删除“固定源码 Linux 须单独核验”的过时待办；不关闭新图创建提交窗口或其它锁排列 |
| 最近执行直达、Monitor／Develop 分工及同次回跳 | `9cb8b4e8c` 的 Develop／Console／Monitor 前端、Develop PG 和 Execution runner 分项已实际通过；`307de5e6b` 的 [Develop 前端](https://github.com/pampa0629/addp/actions/runs/43590178112/job/132433054640) 实际 320 项浏览器及构建通过、selected=true，包含标签顶部位置回归 | 产品实现与受控 T1／T2／T3 已交付；新鲜排队中／运行中的正式用户完整导航仍缺真实证据，不重复开发页面 |
| 共享前端与前端 CI 登记 | `2d5e63d43` 的 [共享前端](https://github.com/pampa0629/addp/actions/runs/43586730624/job/132422749269) 379 项及[前端登记](https://github.com/pampa0629/addp/actions/runs/43586730624/job/132422749381) 34 项／21 前端实际通过，均独立 Job、selected=true | 两项 Linux 执行证据已补齐，不再受原平台聚合前置失败阻断；不改写原聚合失败 |
| Transfer CDC 审批精度与两条 Kafka 路径 | `b3d1b86c1` 的真实 PG／Go T1 已通过；后继 `d12e8279b` 的 [Hosted 19 项](https://github.com/pampa0629/addp/actions/runs/43253234380/job/131304474365) 全部通过，正常退出业务删除／DLQ 缺失／历史可读、硬终止恢复／唯一终态／拒删及零派发、外层零残留均有记录 | §8.45 的旧“仅本地／待 Hosted”只属于前轮，已由后继闭环补齐；不承诺硬终止缺退出证明后也可删除 |

上述前端和 Transfer 原始结果详见[Monitor 执行历史证据](../spec/testing/evidence/Monitor执行监控与留存历史证据.md)；较早失败、跳过、缺失回执及首次现场保持，不以新文档或外层清理覆盖。范围外 Security 依赖、SQL pool、指标夹具等 CI 失败不由本会话接管，也不作为本会话待开发项。

本轮文档验证：新增本地链接／锚点9项及本轮 `git diff --check` 通过。仅含三份自有文档的隔离副本执行 `make test-changed`，识别0个业务模块后按现行规则触发 `make test-platform`；首轮因副本缺历史Git对象在来源校验失败，补齐只读历史后该前置通过。后继聚合在既有 `metrics-http-budget_test.py::test_native_timeout_reaps_owned_group_without_completing_the_stage` 报临时pid文件缺失（169项中1个error），**整项未通过**；未安装／未选择的PostgreSQL指标恢复不计通过，不接管该范围外用例。原日志为 `/tmp/addp-execution-doc-audit-changed-first.log`、`/tmp/addp-execution-doc-audit-changed.log`；没有以历史CI替代本次聚合结果。文档核对与待确认方案不依赖改动该用例。

### 准确剩余清单与推进顺序

“缺专用用例”不等于已证明生产缺陷；只有复现或已确认安全／一致性要求才进入修复。以下逐项替代原 P1a—P5 的粗略待办，已完成部分不继续累计。

| 顺序／性质 | 剩余事项与定位 | 下一交付及关闭条件 |
| --- | --- | --- |
| 1／runner 已补齐，待 Linux 与真实证据 | 最近执行（pending／running／success）到 Monitor、Develop 结果与同次回跳的真实导航。现有 `execution-result-navigation.spec.js` 是受控 T3；真实 `execution-trace-local.spec.js` 只在业务终态后按 UUID 直入 Monitor | 按第九节已确认的自有样本、调用和观察窗口补唯一 runner、确定性回归及登记；不改权限、策略、采样或旧首失败，不在本轮运行 |
| 2／待单次真实运行 | 正式用户阶段追踪：`25a62c07c` 的 causal-link 分步诊断、首失败落盘及对应 Linux 已完成；上一实跑仍停在 causal-link，精确原因未知 | 原两组样本、四身份、11 项业务权限、采样、预算和清理不变；新的真实窗口单独确认，先读首失败再决定修复，不从合成用例宣称根因解决 |
| 3／基础 PG 缺口 | C2j／C3 其它跨类与外键锁排列；C4 新建图配置在目标核验与提交间的窗口、其它最终事务竞争。`KnowledgeGraphService.Create` 先解析／探测目标，`KnowledgeGraphRepository.Create` 直接插入；现有删除、入队与 scan 漂移用例不覆盖此窗口 | 下一小切片先明确新图与同目标关闭的准入关系，真实双连接 PG 验证两序及回滚；若需要新增 Engine／Graph 跨模块提交权威先讨论。目标重绑、deleting 显式重评估／确认同样是待设计事项，不从现有 execute／Restore 推导可用 |
| 4／消费者 T3 与读取 PG 缺口 | C5g／h／k／l 的 Cleanup 专用组合仍未齐。System 已有 503、非法 attempt、迟到专业响应和身份切换；Monitor 已有 closed pending 清除及通用 401／403／404／503 失效，不能把这些当作全部 Cleanup 组合 | 补父详情撤权 404 且不自动补查子、closed pending→Owner 合法拒绝／running 矛盾的整体替换、空／省略／非法／隐藏计数区别和两消费者双语展示。C5e 的 claim 未提交跨截止、C5j 的 HTTP 裁决后新增未裁决 Owner 仍缺专用真实 PG；复用现有共享组件与标准入口 |
| 5／待单次 Hosted | Manager late writer 的 runner、12 类反例与 `9cd0b267d` Linux 408 项已完成；§8.44 尚无该 case 的真实通过记录 | 单独确认后按原 11 项、权限／预算／精确资源及控制恢复／零残留验收；不重跑已通过的 success／object／index 三项 |
| 6／已实现 runner，真实覆盖待补 | Graph／Cleanup 受控故障 profile 已有入口、控制与报告，尚无本专题可归属的固定源码完整 Hosted 通过记录；G6 原生回包丢失／两引擎效果与停止事实仍缺专用证据 | 对照实际 profile 补缺而非重建；受控提取／HTTP 与真实原生效果分别计证。平台上下文／审计／父子树及跨 Owner 离线恢复不得从原生 Manager／Transfer 个例外推；新增 Provider 信任边界先讨论 |
| 7／历史证据适用性 | Quality `1471f8fc8` 的 [bounded-recovery Hosted](https://github.com/pampa0629/addp/actions/runs/40653912002) 已通过，但其后 Quality Worker／启动及 Common 执行输入有变更；已有当前产品路径，非待重新实现 | 保留旧 SHA 的通过，先据固定输入闭包核对后继 T0／T2 是否覆盖同一恢复资格、租约与旧写拒绝。未取得本轮完整恢复真实证据，不盲目要求所有 Owner 重跑；是否新跑 Quality 另行确认 |
| 8／运维准备，非已授权操作 | 旧协议一次性切换：在现有 `scripts/` 和登记中未定位标准切换入口；原研发旧数据清理许可不包含当前活动／unknown 的准确删除范围 | 后续先刷新只读清单、引用和未决事实，再讨论绑定清单摘要的单一演练／切换入口；不新增兼容路线。实际环境窗口、停旧受理、清理、标准启动均须另行确认 |
| 暂缓／不计本轮阻塞 | Graph 高级恢复：恢复证据 Provider、可信原生绑定、正向 reopen；Transfer 硬终止后允许删除的生产核清；历史／closed Tenant 治理 | 均未获本轮实施许可；System 永久登记 ID 不替代原物理代际／效果／停止证明。缺证据继续拒绝，不以本次导航或 PG 补测绕开 |

Common／System P1a 已补的父子竞争、重复拒绝及事务回滚在下节各自固定源码证据下关闭；剩余 C2j／跨 Owner T4 已分别列入第 3／6 项，不把“矩阵未穷尽”当作重建公共协议理由。本轮优先第 1 项方案与文档收口；其余按上述小切片推进或等待专项授权，不启动新的运行。

共享环境切换的后续顺序仍是：刷新只读清单 → 确认单一演练入口及范围 → 单独确认窗口 → 用户停止旧受理／旧运行体 → 无活动与 unknown 后准确清理 → 用户标准脚本启动 → 核新协议与旧路径退出。历史“9 父＋63 子／11 个 Redis 键”仅是旧快照，不作当前删除输入；不创建第三个本地测试 database，不模糊删除、不自行重启。

### 本轮 PG 增补验证

Common 父子准入与 System 协调的用例定位见 [Owner 矩阵](../spec/testing/Owner补充门禁.md#graphcleanup-执行管理最小验证矩阵)。两个批次均只补测试与门禁，沿 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test execution-lifecycle"`、`make test-system-cleanup` 和 `make test-t2-ci-registration` 验证。本地两个 PG 入口串行使用 Infra 实际映射的 `addp_test`。

| 固定版本 | 实际覆盖与结果 |
| --- | --- |
| 2026-11-08 · `839a4d6c0178fbb02d7c2620ff06ec815b337a0c` | Common 覆盖多领取者、父子提交顺序、并发与重复拒绝、prepare 失败、claim／拒绝外层回滚及原生写失败七项。System 补 ParentRecoveryAdmission、ClosureRollback，AdmissionRace 改用实际 PG 阻塞屏障。本地和 [Common PG](https://github.com/pampa0629/addp/actions/runs/42446889335/job/128628596647)／[System Cleanup PG](https://github.com/pampa0629/addp/actions/runs/42446889335/job/128628596160) 均通过。Common 包含生命周期和专业结果原子提交，System 8 个主用例，两组 0 SKIP。同源 [System Go T1](https://github.com/pampa0629/addp/actions/runs/42446889319/job/128627595778) 通过。 |
| 2026-11-09 · `21555a8ad` | Common 覆盖 P／C／Owner 行锁跨截止或失租的六种交错、11 种非法绑定在父活跃／关闭时的反例、失租恢复和七种残留字段。截止后提交未启动拒绝，临时失租保留子记录。System 覆盖父／子摘要漂移和缺预期子，只读快照及协调均失败，持久事实与审计保留，父领取随事务回滚。本地及 [Common Linux PG](https://github.com/pampa0629/addp/actions/runs/42550397199/job/128944704717)／[System Cleanup Linux PG](https://github.com/pampa0629/addp/actions/runs/42550397199/job/128944704053) 均通过。Common 36 个矩阵末级用例及原专业结果原子提交，System 9 个主用例含 3 个绑定反例，两组 0 SKIP。同源 [System Go](https://github.com/pampa0629/addp/actions/runs/42550397212/job/128944443300) 通过。 |

首批 Common 屏障只认直接阻塞者而超时，改为核验真实阻塞链。System 首轮误比较变化的 `observed_at`，改为比较持久事实。首轮日志保留，产品逻辑未改。随机 execution 专用触发器、函数、表和记录按夹具退出清理。新增 System 主用例由标准入口强制要求 PASS。

两批本地 T2 登记均通过 85 项 runner／选择反例、9 项 Manager 入口反例和登记检查。`839a4d6c0` 的 [Platform consistency](https://github.com/pampa0629/addp/actions/runs/42446889319/job/128627595947) 在 SQL 连接池 Node 预检失败，未执行登记。独立提交 `76ed462fa` 修正预检，包含它的 `7c92e7c84` [Platform consistency](https://github.com/pampa0629/addp/actions/runs/42508155900/job/128815197630) 通过并实际执行 85＋9 项。`21555a8ad` 的 [Platform consistency](https://github.com/pampa0629/addp/actions/runs/42550397212/job/128944443256) 在 Transfer 来源漂移检查失败，未执行登记，该版本仍只有本地登记结果。两批均未运行全工作区 `test-changed` 或新增 Hosted，Graph 高级恢复仍暂缓。

### Graph 基础 PG 增补验证

2026-11-09 的 G2 摘要回滚首轮确认公共执行、任务摘要和控制记录均回滚，同时复现 `ClaimBuild` 返回未提交的租约与任务对象。修正错误分支后不再返回这些对象，移除本轮任务专用触发器后，同一 pending 执行以 attempt=1 领取并写入匹配的开始时间。G3 用两个连接的实际 PG 阻塞关系验证取消等待。成功先提交后，取消返回原终态，进度、写入计数、任务统计和取消控制保持。

本地 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 和固定提交 `cc0de098b` 的 [Linux Common PG](https://github.com/pampa0629/addp/actions/runs/42554724259/job/128958459086) 均通过 9 个 Graph 主用例，含两项增补及 Cleanup Runtime，0 SKIP。本地 Graph Go 门禁通过，同源 [Execution online CI runner](https://github.com/pampa0629/addp/actions/runs/42554724132/job/128957781489) 通过 414 项。上述结果覆盖 PG 回滚和终态交错。

### Graph G8 输入冻结验证

真实 PG 六个子场景覆盖入队前材料元数据或 CatalogPath 冲突时拒绝且不留下执行／控制记录、私有映射快照改写时以 attempt=0 拒绝、正常编辑本体后仍按原冻结标签和唯一键产生待审项、同大小材料字节变化时在抽取前失败，以及 Engine 登记版本变化时在读取材料前失败。活动材料的上传准入和删除均拒绝。材料读取、System 登记及 Copilot 用受控输入，执行、任务摘要、外部操作记录和待审项使用真实 PG。

首轮发现无图写入的构建跳过 Engine 版本核验，仍调用 Copilot 并成功。修复后统一在读取材料及外部处理前校验冻结登记，拒绝版本漂移，既有图操作仍核验冻结目标。该轮覆盖登记版本，不扩展到物理代际或高级恢复。

本地 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 通过 10 个主用例，含新增六个子场景，0 SKIP。`make test-module MODULE=graph GO_ONLY=1` 及 T2 登记 85＋9 项和登记检查通过。Graph 分组与 Common 全量 PG 均要求 BuildInputRuntime 和 CleanupRuntime 的准确 PASS。本节记录本地结果。

### Graph 审核与 Cleanup 竞争 PG 增补验证

2026-11-12 增补六项，复用 `TestIntegrationPostgresGraphCleanupRuntime/review_cleanup_interleavings`，由既有 Graph 分组与 Common 全量 PostgreSQL 门禁的必需 PASS 入口执行，Graph 源码路径自动命中 Linux Common PostgreSQL Job，无新增服务、Job、权限或生产入口。

- 关闭先提交：在实际 Cleanup 关闭事务持 G 排他锁时启动审核，以 `pg_blocking_pids` 确认审核等待；提交后拒绝审核。关闭事务失败则关闭标记、归属和版本一起回滚，等待审核可正常受理。最终专业结果写入失败时，归档和公共／专业回执一起回滚，已提交关闭仍保留。
- 审核先受理：审核命令及准确目标操作在 G 共享事务中登记，确认 Cleanup 实际等待后提交。关闭随后可提交，但在途／未知操作及未完成审核回执仍阻清理，新审核被拒绝。原命令可以在关闭后保存已知结果，随后 Cleanup 正常完成；未知效果令 Cleanup 失败关闭并保留操作和审核；审核回执更新失败保留原命令与未提交结果，仍阻清理。
- 各场景均保留原构建终态及摘要、其他待审项和 Engine 清理应保留的审核历史；随机 Tenant 事实与专用触发器／函数在退出清理。PG 为真实事务与锁交错，原生图写入由持久操作协议夹具代表，不计为 Neo4j／FalkorDB 原生效果或 Hosted 证明。

本地 `make test-module MODULE=graph GO_ONLY=1`、`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 已通过；Graph PG 仍为 10 个主用例，新增六个子场景均准确 PASS、零 SKIP，退出事实清理检查通过。首轮关闭屏障错误来自测试回调复用 GORM 更新语句的参数，改用同一事务的新查询 Session 后通过；尚未复现需要修改生产逻辑的新缺陷。T2 登记 85＋9 项及注册检查通过。固定源码 `1ff28df73c8985b104a2b2888f34153bdf9323c3` 的 [Linux Go workspace](https://github.com/pampa0629/addp/actions/runs/43162604533/job/130997042017) 与 [Common PostgreSQL 15](https://github.com/pampa0629/addp/actions/runs/43162604574/job/131000500767) 均已终态 success；原始 Go 日志确认 Graph server／API／repository／service 包实际通过，PG 日志确认 Graph 10 个主用例与新增六个子场景准确 PASS、零 SKIP。Graph 高级恢复继续暂缓，未运行 Hosted 或环境切换。

### Graph 同项审核竞争 PG 增补验证

2026-11-12 在既有必需入口 `TestIntegrationPostgresGraphCleanupRuntime` 增加 `review_command_competition`，覆盖批准／拒绝／修改的全部九种有序组合（含相同动作）。赢家通过生产仓储的命令领取、操作登记和回执事务；失败方调用真实 Service 的三种审核入口。以赢家未提交的事务和 `pg_blocking_pids` 固定交错，确保失败方读取旧 pending 后确实等待，再核验赢家提交后重新裁决并返回冲突，不用 sleep 推测顺序。

核验唯一 command／actor 所有权，失败方不得改变审核行任何字段或时间戳、不得进入外部图访问入口；批准／修改赢家各保留一条准确已知操作，拒绝赢家为零操作。错误 command／actor 的回执、完成后的重复 Service 请求和过期命令新增操作均拒绝；原始内容、赢家修改内容、审核人及原构建终态／摘要、其他待审项保持准确。复用上一批真实 PG 夹具的公共部分，未增加产品抽象、权限、接口或生产分支。

本地 `make test-module MODULE=graph GO_ONLY=1` 与 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 通过；10 个 Graph 主用例、原六个交错及新增九个竞争子场景准确 PASS，零 SKIP，夹具事实退出清理通过。标准入口、目录选择和 CI 登记输入未变，沿用已覆盖 Graph 源码路径及既有必需主测试。尚未复现需要修改生产逻辑的新缺陷。固定源码 `b66672161a8204eed1bf5e01235733008bb21c7d` 的 [Linux Go workspace](https://github.com/pampa0629/addp/actions/runs/43182072933/job/131060229760) 与 [Common PostgreSQL 15](https://github.com/pampa0629/addp/actions/runs/43182072938/job/131060542355) 均已终态 success；原始 Go 日志确认 Graph server／API／repository／service 包实际通过，PG 日志确认 Graph 10 个主用例、原六个交错及新增九个竞争子场景准确 PASS、零 SKIP／FAIL。原生图效果由持久操作协议夹具代表，不能替代原生故障或 Hosted 验收；高级恢复继续暂缓。

### Graph 入队与 Cleanup 竞争 PG 增补验证

2026-11-12 在既有必需入口 `TestIntegrationPostgresGraphCleanupRuntime` 增加 `enqueue_cleanup_interleavings`，覆盖普通构建／人工重跑与 Cleanup 的两种提交顺序，共四项。沿实际入队仓储与三阶段 Cleanup Runtime，用 `pg_blocking_pids` 固定真实阻塞关系；关闭先提交时另外暂停在关闭已提交、取消尚未开始处，防止后续归档或任务更新掩盖错误准入。

首轮真实 PG 复现普通构建和重跑均在关闭先提交后错误入队，反向两项准确取消通过。根因是入队的可重复读快照在等 G 前已建立，而 G 行只提供锁、不更新内容，等锁后的普通读取仍可见旧 Graph。保留冻结本体映射的一致快照，将既有目标读取改为 `FOR SHARE`，使快照建立后发生的目标行变化以 PostgreSQL `40001` 沿已有冲突映射回滚整次入队；不改隔离级别、不自动重试、不新增控制事实或技术路线。

修复后四项通过：关闭先提交拒绝且不留下新 execution／控制记录，不重置材料已完成检查点或删除原待审项；入队先提交则 Cleanup 取消准确的新 execution，保留冻结快照、摘要及重跑前驱绑定，attempt 仍为0、started_at 为空、无租约或原生操作。Cleanup 成功回执、准确取消计数及归档关闭范围一致，原执行和原冻结输入不变，测试自有 Tenant 事实退出清理通过。材料读取／原生处理不在本批范围，未启动构建监督器、图引擎或开发服务。

本地 `make test-module MODULE=graph GO_ONLY=1` 与 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 通过：Graph 10 个主用例、原六个审核／Cleanup 场景、九个同项审核组合及新增四项准确 PASS、零 SKIP。既有 Graph 源码目录、必需主测试和 Common PG 标准入口已覆盖，无需新增 Make／CI 路径。`make test-t2-ci-registration` 的85＋9项及登记检查通过。固定源码 `01cdea4fc4d649bcaa70fb18d11f4ce0fbb6fbe9` 的 [Linux Go workspace](https://github.com/pampa0629/addp/actions/runs/43187972475/job/131079841459) 与 [Common PostgreSQL 15](https://github.com/pampa0629/addp/actions/runs/43187972310/job/131080229635) 均已终态 success；原始 Go 日志确认 Graph server／API／repository／service 包通过，PG 日志确认 Graph 10 个主用例、原六项审核／Cleanup、九项审核竞争及新增四项入队竞争准确 PASS、零 SKIP／FAIL。Graph 高级恢复继续暂缓，未触发 Hosted 或环境切换。

### Graph 任务／材料删除与 Cleanup 竞争 PG 增补验证

2026-11-12 在既有必需入口 `TestIntegrationPostgresGraphCleanupRuntime` 增加 `delete_cleanup_interleavings`，任务删除与材料删除各覆盖关闭先提交、删除先提交、删除语句失败回滚及 Cleanup 最终回执失败，共八项。使用生产删除仓储与三阶段 Cleanup Runtime，以 `pg_blocking_pids` 核实实际锁等待；任务删除在子审核／材料已删除而任务行尚未删除处设屏障，准确目标的 PostgreSQL `AFTER DELETE` 触发器报错，验证部分写入完整回滚。

关闭先提交时删除拒绝、原事实不变；删除先提交时只删除准确任务及其所属审核／材料，或准确单个材料，Cleanup 按剩余任务／材料／审核计数归档；同图另一任务、材料、审核及原执行／冻结输入完整保留，无额外操作或取消。删除失败恢复所有原行及时间戳，Cleanup 随后按原事实完成；删除已提交而 Cleanup 结果写入失败时，仅回滚该轮归档与公共／专业回执，持久关闭保留、已提交删除不恢复。未复现需要修改生产逻辑的新缺陷，未修改删除语义、权限或操作范围。

本地 `make test-module MODULE=graph GO_ONLY=1` 与 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 通过：Graph 10 个主用例、原6／9／4项竞争场景及新增八项准确 PASS，零 SKIP／FAIL；自有 Tenant 事实退出清理、专用触发器／函数删除通过。既有 `graph/backend/` 登记与必需主测试覆盖，无新 Make／workflow／服务依赖；T2 登记及门禁编排输入相对上批未变，未重复运行登记测试。固定源码 `cab93eb9459f6a93ef1c35ed9351d45846ef21a4` 的 [Linux Go workspace](https://github.com/pampa0629/addp/actions/runs/43198933395/job/131116961476) 与 [Common PostgreSQL 15](https://github.com/pampa0629/addp/actions/runs/43198933411/job/131117339862) 均已终态 success；原始 Go 日志确认 Graph server／API／repository／service 包实际通过，PG 日志确认 Graph 10 个主用例、原6／9／4项及新增八项准确 PASS、零 SKIP／FAIL。证据仅为真实 PG 事务／锁和持久事实，不计业务图／MinIO 物理效果或 Hosted 验收；Graph 高级恢复继续暂缓，未重启服务或触发 Hosted。

### Graph 图配置删除与 Cleanup 竞争 PG 增补验证

2026-11-12 在既有必需入口 `TestIntegrationPostgresGraphCleanupRuntime` 增加 `graph_delete_cleanup_interleavings`。删除后无剩余图／同 Engine 仍有另一张图，各覆盖关闭先提交、删除先提交、图删除失败回滚及 Cleanup 最终回执失败，共八项。沿生产 `DeleteDefinition` 和三阶段 Cleanup Runtime，以 `pg_blocking_pids` 固定真实锁等待；复用删除屏障并增加任务数核验，确保报错前审核／材料／任务已实际删除、图配置尚未删除。

关闭先提交时旧版本请求由 version CAS 拒绝，再用当前正确版本核验仍被关闭准入拒绝。删除先提交时 Cleanup 重新读取零／剩余准确图及其定义，仅归档剩余范围；图删除的 `AFTER DELETE` PG 错误使原图和所有子记录完整回滚。删除已提交而 Cleanup 回执写入失败时，只回滚该轮剩余图归档及公共／专业回执，保留持久关闭、不恢复已删除配置。原 execution、冻结输入及通过生产命令／回执事务生成的已知操作记录逐字段不变；该操作是 PG 持久协议夹具，不是原生图写入证明。未复现需要修改生产逻辑的新缺陷，未改变删除语义、权限或作用范围。

本地 `make test-module MODULE=graph GO_ONLY=1` 与 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 通过：Graph 10 个主用例、原6／9／4／8项及新增八项准确 PASS，零 SKIP／FAIL，自有 Tenant 事实及专用触发器／函数退出清理通过。`graph/backend/` 及既有必需主测试继续命中标准 Common PG 门禁；登记／编排输入相对上批未变，未重复运行登记测试，无需新增 Make／workflow。固定源码 `54e93321ad424604c0132cf97da1bedc6c342cd0` 的 [Linux Go workspace](https://github.com/pampa0629/addp/actions/runs/43201655069/job/131126328636) 与 [Common PostgreSQL 15](https://github.com/pampa0629/addp/actions/runs/43201654477/job/131126972608) 均已终态 success；原始 Go 日志确认 Graph server／API／repository／service 包通过，PG 日志确认 Graph 10 个主用例、原6／9／4／8项及新增八项准确 PASS、零 SKIP／FAIL。Graph 高级恢复继续暂缓，未重启服务、触发 Hosted 或操作业务图／MinIO。

### Graph Cleanup 已领取子执行与父关闭 PG 增补验证

2026-11-12 在既有必需入口 `TestIntegrationPostgresGraphCleanupRuntime` 增加 `parent_closure_interleavings`，首次关闭准入前／已提交关闭后两个位置，各验证正常完成及专业结果写入失败，共四项。真实 Runtime 已提交子 claim，使用 `pg_blocking_pids` 固定等待 G，另以 C 的 `FOR UPDATE NOWAIT` 返回55P03核实 C 行确已被占用；数据库墙钟到达冻结协调 deadline 后，Common 父 lease 终态接口仍可在 C、G 被占用时独立提交 timeout。

随后继续持有已关闭 P 的行更新锁，释放 G；原已领取的子执行按自身合法 lease 完成成功或失败终态，attempt、started_at、父绑定和冻结输入不替换，P 全部字段／时间戳不变，原构建及领域事实保留。结果写入失败时归档及公共／专业回执同事务回滚，已提交 Graph 关闭不被父关闭或子失败撤销。四秒协调预算仅用于本轮夹具，原夹具默认一分钟、产品预算及12秒场景等待上限不变；夹具时间改由数据库墙钟生成。未复现生产逻辑缺陷，无权限、接口或生产配置变化。

本地 `make test-module MODULE=graph GO_ONLY=1` 与 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 通过：Graph 10 个主用例、原6／9／4／8／8项及新增四项准确 PASS，零 SKIP／FAIL，自有 Tenant 事实及专用触发器退出清理通过。沿既有 Graph 源码目录、必需主测试及 Common PG 编排，登记输入相对上批未变，未重复运行登记测试。固定源码 `a46d5adf297175268f730b5919984d98aee9e098` 的 [Linux Go workspace](https://github.com/pampa0629/addp/actions/runs/43204588316/job/131136411182) 与 [Common PostgreSQL 15](https://github.com/pampa0629/addp/actions/runs/43204588232/job/131152181228) 均已终态 success；原始 Go 日志确认 Graph server／API／repository／service 包通过，PG 日志确认 Graph 10 个主用例、原6／9／4／8／8项及新增四项准确 PASS、零 SKIP／FAIL。新增四项为 `admission/complete`、`admission/result_rollback`、`after_closure/complete`、`after_closure/result_rollback`，分别耗时4.50／4.51／4.49／4.49秒。本批只验证 Common 父围栏与 Graph Runtime 的 PG 组合；System 私有截止快照／审计、真实当前授权及跨进程拓扑不由受控授权夹具代验。Graph 高级恢复继续暂缓，未重启服务或触发 Hosted。

### Graph Cleanup 最终子终态更新回滚 PG 增补验证

2026-11-12 在既有必需 `TestIntegrationPostgresGraphCleanupRuntime` 增加 `terminal_update_rollback/running` 与 `terminal_update_rollback/timeout`。真实 Runtime 已提交子 claim 与领域关闭后暂停；父正常保留原状态，父截止沿 Common 合法 lease 提交 timeout，再放行最终事务。专用 PG 触发器先核实归档和准确子 attempt 的专业结果均已写入、末尾 UPDATE 带公共回执及成功终态，再注入 SQL 错误；观察实际 P0001 与专属错误，避免更早失败冒充最终回滚。

断言归档、专业结果与公共回执整体回滚，子按原 attempt 独立收敛 failed 并保留安全原因、原开始时间及冻结输入；已提交领域关闭、父整条记录与原构建／任务／材料／审核／控制／操作事实不变。沿既有 Graph PG 主测试、Make 与 CI 选择，无新增权限、预算、依赖或生产路径。四秒协调截止只复用上一批夹具配置，12秒场景上限不变；受控授权与 Common 父关闭不代验 System 私有截止快照／审计、真实权限或跨进程拓扑。

本地 `make test-module MODULE=graph GO_ONLY=1` 与 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 通过：Graph 10 个主用例、原6／9／4／8／8／4项及新增两项准确 PASS，零 SKIP／FAIL，夹具自有事实及专用触发器／函数退出清理通过。父截止初测仅差异 `LeaseExpiresAt`：复用 GORM 结构体回读 SQL NULL 保留旧时间指针，改从空结构体读取已提交快照后通过；未发现生产缺陷。额外 `make test-t2-ci-registration` 的首阶段85项中8项失败，均因既有 HighGo SharedSQLPool 新必需项与旧登记夹具不一致，后续检查未执行；本批未修改对应入口或夹具，不计为通过。固定源码 `c0f1f785281cff5c58fd719e09a4fee586e9a8fa` 的 [Linux Go workspace](https://github.com/pampa0629/addp/actions/runs/43229751083/job/131221609597) 与 [Common PostgreSQL 15](https://github.com/pampa0629/addp/actions/runs/43229751019/job/131259464039) 均已终态 success；PG Job 于2026-11-13 00:03:06（Asia/Shanghai）结束。原始 Go 日志确认 Graph server／API／repository／service 包通过，PG 日志确认 Graph 10 个主用例、原6／9／4／8／8／4项及新增两项准确 PASS、零 SKIP／FAIL；`running`／`timeout` 分别耗时0.43／4.35秒。Graph 高级恢复继续暂缓，未重启服务或触发 Hosted。

### Graph scan 后候选漂移核查与已确认方案

2026-11-13 基于 `4fce73e77` 所在共享 main 核对：System `createEngineExecute` 将成功 validation scan 的总影响摘要写入 `Confirmation.ImpactDigest`，并冻结 `BasedOnScan`；该总摘要按 Owner 模块摘要聚合。Graph scan 生成稳定对象引用的模块摘要，execute 仅复核本次阶段1与阶段3的图 ID 集合，不消费原 scan 回执或比较其模块摘要。生产 `BuildRepository.CreateTask` 只锁 G 并检查既有图准入，未读取 Engine 生命周期，所以 validation 后、Graph 关闭前仍存在新增任务窗口。新图配置创建是否还存在独立 G／提交时生命周期窗口，当前仅源码观察，未作原生或跨服务复现。

**本地实证。** 临时双场景用例挂入既有必需 Graph PG 主入口，通过 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"`、已核实 healthy 的本地25432／`addp_test` 执行。实际创建独立父子／授权绑定并运行两次只读 scan，execute 引用准确第二次 scan ID，确认值按 System 相同算法聚合。对照：第一次 scan 后、第二次 scan 前经生产 `CreateTask` 新增任务，第二次摘要改变且 scan 不关闭／归档图谱，PASS。反例：两次 scan 摘要一致后经同一入口新增任务，execute 仍 `success`、图谱 `archived`、准入关闭、任务数2且有成功回执；要求拒绝漂移的断言实际 FAIL。原有10个主用例除新增反例导致的汇总失败外，其原有场景及6／9／4／8／8／4／2组均PASS。受控 HTTP 仅提供原租约／scope绑定授权响应，不代验 System 当前IAM、真实 deleting Engine、跨进程时序或原生图效果；Engine 范围仍只归档，不代表删除业务图、材料文件或任务记录。

临时用例和原始失败日志保存在系统临时目录 `/tmp/addp-graph-scan-drift-probe_test.go`、`/tmp/addp-graph-scan-drift-probe-pg.log`，原有 Tenant 自有事实退出清理通过；已移除临时挂载，未把成功接受漂移固化成正式验收条件，也未提交失败测试或生产修复。恢复原测试入口后，同一标准 Graph PG 门禁重新 exit0：10个主用例、原6／9／4／8／8／4／2组全PASS，零SKIP／FAIL。该绿灯只证明临时挂载已撤回且原回归保持，不代表候选漂移已修复。未触发新的 Hosted，不以本地反例声称 Linux 或产品验收。

**已确认方向。** 2026-11-13用户同意严格摘要相等、复用本 Owner 原 scan 回执与失败保留原状态；正式事实源为[Owner原scan基线契约](../spec/addp-cleanup体系规范.md#owner-原-scan-基线与严格影响比对)。 优先复用 `BasedOnScan` 与既有 Common 公共执行／类型化回执，让 Common 提供只读的“本 Owner 原 scan 基线”解析契约：核准确来源请求、Tenant、Owner、父子绑定、成功终态及有效回执；Graph 只消费自己的模块摘要，不读 System 或其他 Owner 私表，不新增公开API、权限、凭据或候选清单。System 继续独占完整Owner集合、用户确认和总摘要裁决，不允许 Owner 用自己的摘要推断全局通过。无需把总摘要当作 Graph 摘要，也不先新增请求字段或私有快照实体。

Graph 在 C→G 内、关闭／取消／归档前比较当前模块影响与原基线；变化则失败关闭本次请求，不执行未确认范围。最终清理仍在同一 G 围栏内复核候选与静止性，失败保留既有持久关闭、外部效果证据、原父子历史，不自动重跑／重开。缺失、伪造或越界原scan不得旁路；既有使用随机 `BasedOnScan` 的测试须改为真实匹配的成功scan夹具。

**已确认取舍。** 首期采用严格摘要相等：新增、删除／范围缩小或影响分类改变都重新评估、显式确认；实现与审计最简单，但删除先提交时不能继续执行旧确认，后续合法新请求须引用删除后新scan。若要求“只阻新增、允许范围缩小”，单一摘要无法证明子集，必须另行设计 Owner 持久候选集合及其授权／保留语义，不能用数量下降或仅比较图ID替代。该选择改变先前未明确关联原scan的删除竞争用例解释，并新增 Common 的原scan证据消费契约，现已获用户明确确认，按正式规范同步 Common／Graph 与门禁。当前 `BeginDeletion` 明确拒绝 deleting，`Restore` 仅接受 deleted，所以“检测后要求新评估”不代表已有 deleting 原工作流的重新确认入口。首批失败继续保留 deleting／原失败，不复活原请求；若要恢复该工作流，必须另行明确显式重评估与确认的职责和接口，不靠通用execute或直接改库绕过。新图创建的提交围栏另按证据细化，不将本方案宣称为全生命周期完全封闭。

### Common／Graph 原 scan 基线修复实施验证

正式契约先提交 `8a41cf52b`。Common新增唯一只读 `ReadCleanupScanReceipt`：按 `BasedOnScan` 定位同Tenant／Owner的公共scan子项，严格核来源父子、请求、scope、成功终态和类型化回执；普通读取不取原P／C行锁、不读私表，不因scan请求者与execute请求者不同增加权限条件。Graph在首次C→G关闭／取消之前及最终清理事务中比较原模块摘要；前置不符只提交既有 `scope_changed` 失败结果，缺来源无伪造回执，最终不符保留已提交关闭／原历史，无归档或成功回执。没有兼容或无基线旁路。

旧随机scan引用已改为实际公共scan生命周期与专业／公共结果夹具。删除先提交和重跑删除旧待审项时，候选减少也拒绝旧确认；已提交删除／排队事实、冻结输入和原执行历史逐字段保留。新的Graph真实Runtime分组跑两次scan，覆盖复扫前新增可被摘要发现、未变化成功、复扫后新增／减少／等量替换拒绝、缺scan失败；最终分组另核关闭后生产新建拒绝，以明确的直接PG故障注入证明末尾摘要仍拒绝变化且保留关闭，以及空范围成功。此PG故障不声称生产入口可绕过准入。原父截止与最终SQL回滚用例继续保留。

本地标准验证：`make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=graph GO_ONLY=1`、`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 与 `--test execution-lifecycle`；Common基线14项，Graph10主用例、原6／9／4／8／8／4／2项及新增6／2项均已实际PASS且零SKIP／FAIL，夹具退出清理自有事实。`make test-t2-ci-registration`已通过86＋9项及登记核对；沿原必需主测试、路径发现和Linux Common PG，无新Make／workflow／服务。固定源码 `c05f2292cb361674d38d1898870431caee8f12a9` 的 [Linux Go workspace](https://github.com/pampa0629/addp/actions/runs/43351341678/job/131642870339) 与 [完整 Common PostgreSQL 15](https://github.com/pampa0629/addp/actions/runs/43351341944/job/131643546461) 均已终态 success。原始 Go 日志核实 Common execution／cleanup 及 Graph server／API／repository／service 包通过；PG 日志核实 Common基线14项、Graph10主用例、原6／9／4／8／8／4／2项及新增6／2项准确PASS、零SKIP／FAIL。PG Job于2026-11-13 10:26:48、Go Job于10:27:57（Asia/Shanghai）结束。受控授权及Common父终态不代验System私有截止审计、真实IAM、跨进程／原生图效果；其他Owner未接入本次强制比对。新图创建提交围栏、其他跨类锁／外键排列和deleting显式重新确认仍是独立缺口。未重启服务、运行Hosted或实施Graph高级恢复。

### 租户任务执行到阶段追踪（已确认最小切片）

2026-11-13 用户经“调用追踪体系”会话明确委托本会话主导：租户先从Monitor中自己有权查看的任务／执行进入，必要时下钻Trace，不逐次申请平台诊断签发。已核对现有执行详情、同步Owner读取裁决、公共execution事实、enqueue/latest私有关联及短阶段SDK；平台Trace与运行日志权限保持独立。用户已明确批准按此切片实施，正式事实源见[租户执行阶段追踪](../spec/tasks/执行监控.md#租户执行阶段追踪)；本节保留分工，代码与实际门禁另记。

**首期产品路径。** 沿唯一 `/monitor/executions?execution_id=...`，保留状态、排队／运行耗时、attempt、过程事件、安全错误和Owner专业结果导航；详情内显式打开“阶段追踪”才查询，不在列表逐行探测或打开详情时自动扇出。先接已审计生产者 Orchestrator orchestration 与 Develop bounded query；其他Owner／任务类型不能仅凭引用格式合法自动开放。Manager同步preview没有execution，不人为造任务或诊断实体。

**已确认服务边界。** 新增 Tenant `GET /api/v1/monitor/executions/by-execution-id/{execution_id}/trace-stages`，仅接受执行身份，不接受trace/span/instance ID、任意时间窗或TraceQL。入口维持当前第一方Tenant User边界，复用现有 `monitor.execution.read` 与本次User的Owner execution-read-scope；每次查询、切换执行、刷新都重新核权，不加平台逐次签发或 `monitor.trace.read`，不扩大角色默认权限。父可读不授子可读，Owner失败整份失败关闭；撤权及迟到响应不得保留旧内容。服务端从已授权权威execution解析私有enqueue/latest，再按六字段精确匹配Tempo样本；不把私有配置或引用直接放入通用Observation。

| 项目 | 首期确认契约 |
| --- | --- |
| 可见范围 | 最多两个已持久阶段锚点；六字段精确匹配，且为已审计Owner的addp.execution根，phase/kind/outcome满足既有闭集和生产者契约，attempt从实际样本取得。enqueue槽另须phase=enqueue、attempt=0；latest正常为claim/dispatch/terminal，可属于旧attempt，不伪装当前进度。相同引用只查询一次、保留各来源槽位分别核验；latest误指enqueue仍属契约异常，不能为去重放宽。 |
| 租户字段 | 槽位enqueue/latest、阶段、真实attempt、阶段动作结果、起止时间、短阶段耗时；Owner名复用已有execution模块。不返回trace/span ID、内部启动实例、运行节点、HTTP路由、ParentID、Links/Edges、整Trace计数／时长、日志正文或私有业务配置。 |
| 可用性 | 分清未记录关联、精确阶段样本可读、当前未找到样本、暂不可查询；各槽独立说明超出查询窗口，不推断过期／丢弃，也不连带隐藏另一个可查槽。畸形引用、样本错绑、全局预算／上游失败使阶段查询失败且不返回部分样本，不降格为正常未命中；原execution详情与事件仍按原接口读取。 |
| 查询与采样 | 在Owner裁决前，使用main.go同一TraceService一次身份2／Monitor8准入及一次5秒Context，涵盖Owner／记录读取／Tempo／解码／响应，返回前再检查原Context，不声称可抢占CPU解码。先按同Tenant内部行取得准确Owner，仅裁决这一Owner；沿现有Authorizer，registry与单Owner RPC串行，整体通过同一callUpstream占一个上游槽（不扇出4个Owner），Tempo同池，不新建池。裁剪前完整候选Detail（含不展示Span及Link）累计JSON≤2MiB／Span≤1000／实例≤100；每次原始OTLP响应仍按既有独立2MiB限制，不另改成原始字节累计限额。Trace数沿原100上限，本切片来源至多两个。保留普通5%、慢1秒、尾采样120秒、72小时／20GiB及原采样策略，不强制留样、不重放业务拿样本。 |
| 覆盖限制 | 六字段仅证明精确锚点，不证明HTTP祖先、兄弟、子执行或整Trace归属；首期不沿Link跳转／递归。Orchestrator dispatch下游可能创建另有Owner权限的Develop子执行，不能凭父可读开放子Trace。不把两阶段包装成完整跨服务瀑布图；总任务耗时继续取execution。 |

每个锚点的内部查询时间窗由服务端单独从其真实StartedAt确定为 `[StartedAt, min(StartedAt+现有1小时上限, 查询时刻)]`，同时服从现有72小时查询政策；超政策或未来时间只说明当前不可查询。两锚点可相隔多日，不把execution.created_at→completed_at当一条平台查询窗。用户不提交时间范围，不通过延长等待、重查或重放业务补齐样本。

**职责。** 本会话负责Monitor执行页／详情、Owner授权查询编排、租户接口／DTO／Swagger与契约、状态失效和验收矩阵。“调用追踪体系”会话按用户明确授权直接协作，已回执精确锚点边界；本方案已确认，负责tracequery精确匹配与最小安全投影、复用TraceService总预算及对应后端测试，必要的Common SDK修改先另报。本轮不需改SDK或采集，不重复开发页面或改平台Trace／日志能力；各自通过publish-main只提交归属范围。

**最小验收。** T1核跨租户／同租户无Owner权限、撤权不发Tempo查询、父可读子不可读、六字段逐项错绑、HTTP及同Trace其他Span不输出、不返回隐藏节点数量／ID、enqueue/latest去重、旧attempt真实展示、无关联／未命中／超窗口／解析及上游失败分别表达，以及共用总期限／并发／字节／Span预算。既有Monitor PG门禁验证授权后读取权威记录、不写业务状态、父子过滤和私有字段不泄露。Monitor／Console T3覆盖唯一可恢复详情地址、按需加载、切换／撤权／关闭后的旧响应丢弃、四主题及移动布局；正式User→Gateway→Owner→Tempo实链另行明确身份与运行验收，不用受控T1/T3或旧平台Trace实链代替。既有Common／Monitor Go、Monitor PostgreSQL和Monitor前端标准入口已自动覆盖本轮文件，无需新增runner或workflow。

**实施与本地实证（2026-11-13）。** 正式契约 `06dd954ee`、精确阶段安全投影 `041900c98`、单Owner授权接口与PG回归 `d5bb0011a`、按需详情与失效清空 `d6e97ea23` 均已提交。Common／Monitor Go T1、Monitor PG T2（含真实库的 `TestIntegrationPostgresExecutionTraceOwnerRead`）、授权清单与Swagger覆盖检查均通过；完整Monitor前端通过152项T1、839项受控浏览器回归与构建，新增16项覆盖双语／四主题窄屏、缺样与失败区分、零值、撤权与迟到响应清空；旧attempt及精确锚点由后端T1另行覆盖。后端Go与前端使用本轮归属文件的隔离验证快照，真实PG通过共享工作区标准入口；不把其他会话在制失败计入本轮，也不将受控Owner／Tempo回执当正式用户实链。

**Linux验证。** 固定后端 `d5bb0011a` 的 [Linux Go](https://github.com/pampa0629/addp/actions/runs/43362081153/job/131677702093) 和 [Monitor PG](https://github.com/pampa0629/addp/actions/runs/43362081159/job/131679194427) 已成功，PG日志确认新增真实用例实际PASS；[平台一致性总作业](https://github.com/pampa0629/addp/actions/runs/43362081153/job/131677702161)在既有StarRocks Hosted runner出现2失败／1错误，未记为通过；与本轮无关，未修改或重跑。

**前端后继收口。** 前端 `d6e97ea23` 的 [Monitor Linux前端作业](https://github.com/pampa0629/addp/actions/runs/43364564823/job/131685812363)已取得失败终态：152项单元测试通过，842项浏览器用例中841通过、1失败，构建未执行；不能把汇总步骤成功记为完整门禁通过。唯一失败为既有 `e2e/cdc-resources.spec.js:57`，切换denied后未出现 `cdc-error`；原始日志确认本轮16项阶段追踪用例均执行，且唯一失败清单不含阶段用例，因此只登记该有限范围通过。CDC测试与夹具由其领域会话在 `665f16796` 修正，本会话未重复修复或重跑。已核对该提交包含 `d6e97ea23`，阶段组件／详情接线／API源码未变；其自然触发的 [Monitor Linux前端作业](https://github.com/pampa0629/addp/actions/runs/43387788680/job/131760256011)实际选中执行，152项单元测试、842项浏览器回归（含全部16项阶段追踪用例）及Vite构建全部通过，完整Linux前端门禁缺口已关闭；原失败仍保留，selection=false或仅summary成功不作为通过证据。未重启服务、未触发新Hosted，实际Tenant User→Gateway→Owner→Tempo验收仍缺；其他Owner和完整跨服务Trace不属于此最小切片。

2026-11-13用户已同意新增租户安全阶段字段及执行授权消费语义；按术语→正式规范→后端／前端顺序实施，保持现有Monitor＋Owner入口权限，隐藏技术ID及平台原始Trace／日志。若用户需要首期就是完整跨服务瀑布图，应先设计更充分的执行归属证明，不能在现有六字段上自动扩大。

<a id="tenant-stage-chain-acceptance"></a>

### 正式用户阶段追踪真实链路验收方案（凭据fixture已修复；本机真实链路待复验）

本节记录前述最小切片的已确认运行验收范围，不变更产品权限、采样或Owner边界。建议复用[正式身份本机隔离闭环](../spec/addp平台运行观测规范.md#正式身份的本机隔离闭环)及唯一人工入口 `make test-monitor-traces-local`：租户trace-stages请求与执行详情浏览器断言已接入，原平台独立校核保留，相关自然Linux门禁已通过。2026-11-14用户随后单独批准一次原范围本机隔离真实验收，该轮已在准备阶段失败并清理；授权不扩展为重试、续窗、下一轮或个人服务操作，后续真实运行仍须另行确认。

#### 1. 环境、身份与权限

沿当前已提交SHA的仓库外私有源码副本及独占产品部署，使用正式System、Gateway、Security、Develop、Orchestrator、Monitor、Console／Nginx和Trace／日志设施。当前个人服务不接管、不重启；共享addp-postgres不连接。独占平台库 `addp_online` 与合成查询库 `trace_fixture` 均只存在于本轮专属容器，沿已有准入与自动销毁，不在共享本地PG新建database。归属标签、动态端口、构建身份与运行实例不匹配即拒绝。此为真实身份的本机隔离技术窗口，保持已有 `formal_runner_t4=false, hosted=false, capacity=false`，不包装成专用Runner T4或生产验收。

| 身份 | 首轮用途与准确边界 |
| --- | --- |
| 业务Tenant User | 复用System-owned `execution-trace-local` 夹具的原11项权限：`orchestrator.workflow.create/read/delete/execute`、`develop.task.create/read/delete/execute`、`develop.data_read.execute`、`monitor.execution.read`、`system.execution_authorization.create`。真实密码登录，经正式Tenant选择／会话与AuthContext确认User、first_party_access_token、unrestricted、准确Tenant及权限集合；不持有 `monitor.trace.read` 或平台日志权限。 |
| 准备User | 沿已有初始Tenant管理员→真实TOTP登记／验证→AAL2及有效step-up→正式分配 `tenant.infrastructure_administrator` 的唯一流程，准备本轮合成Engine；精确权限由现行角色事实核对，不复制自定义Engine授权、不直接写IAM表，不替业务User提交执行或查询阶段。 |
| 平台诊断User | 沿现有真实密码＋TOTP＋Platform AuthContext，仅通过既有System实例／日志和Monitor平台Trace接口独立校核本轮精确检查点；不代业务User调用租户阶段接口，不把平台权限赋给业务User。 |
| 原平台安全负例User | 仅保留现有夹具与会话清理责任，不扩为新的Tenant负例身份或增加权限。 |

不新增User、角色、权限、默认授权或诊断签发。首轮不增加真实撤权、跨Tenant、父可读子不可读的身份场景；这些已有确定性／PG覆盖，真实负例仍独立列缺口，不能因本轮同User有两Owner读取权而声称已验证父子授权隔离。

#### 2. 固定样本与真实路径

仅创建已有两组样本，各一个禁用定时的编排定义＋一个Develop query定义，各手动执行一次：成功为 `SELECT 1 AS fixture_value`，受控失败为 `SELECT 1 / 0 AS fixture_value`，均访问本轮独占PostgreSQL，不读个人／租户真实数据、不创建业务表、不写外部结果。合计最多2次编排提交、2个父execution和2个子execution；不手工补建execution、改私有引用／历史时间或注入Span。核验实际终态、父子绑定与无额外attempt，不因等待观测重跑或重试业务。

业务所有HTTP请求经本轮Nginx→Gateway正式控制面；阶段路径为 `GET /api/v1/monitor/executions/by-execution-id/{execution_id}/trace-stages`，不传query/body或技术ID。Monitor以该User同步向准确Owner的 `GET /execution-read-scope` 核权，再从权威execution读取私有锚点，通过部署mTLS查询代理访问真实Tempo。浏览器使用业务User独立登录，沿Console唯一 `/monitor/executions?execution_id=...` 打开父／子详情；不得使用现有平台管理员浏览器脚本替代租户页面验收。

取证不依赖HTTP请求恰好被5%采样：用现有请求完成日志的同一request_id核对Gateway／Monitor入口及准确Owner授权路由，结合正式User AuthContext与Owner范围响应确认身份／用途；单一阶段请求只命中对应Owner，不把独立页面树读取造成的其他Owner调用混入计数。平台诊断User只读本轮模块／实例及精确execution检查点，从真实日志取得Trace／Span关联，与真实Tempo样本校对；不直读execution私有列、不查询其他Tenant、不沿Link递归。Tempo代理当前不记录访问正文，不新增代理或抓包；证据证明真实依赖与精确结果一致，不声称获得完整HTTP跨服务瀑布或全部内部调用时间。

#### 3. 两类核心断言与页面验收

| 编号 | 必须取得的证据与通过条件 |
| --- | --- |
| A1 正式业务身份 | 业务User的真实登录／AuthContext及原11项准确权限成立；无平台Trace／日志权限；同一业务会话分别获得两Owner当前授权，阶段接口返回 `200`、`Cache-Control: no-store`。 |
| A2 真实样本闭环 | 两组编排／子查询的身份、父子绑定和success／failed终态与正式Owner接口相符；检查点真实产生，不伪造或回填。 |
| A3 可读阶段 | 失败样本的父、子各自latest均为 `available`；与平台独立取得的精确terminal根Span逐项核对phase、attempt、outcome、起止时间及毫秒耗时，且绑定正确Owner／实例／六字段。预期terminal／failed／attempt=1来自实际本轮样本，不能从业务状态推算阶段值。 |
| A4 正常缺样 | 每个Owner只在预先固定的两个enqueue候选（失败样本优先，其次成功样本）中选自然返回 `not_found` 的槽；真实enqueue检查点存在且attempt=0，同精确平台Trace查询正常返回404 `trace_not_found`。响应只有槽位与状态，无样本字段；同execution的可读latest仍保留，普通执行详情及过程事件继续可读。只能说明本次未找到已保留样本，不能断言被采样丢弃或已过期。 |
| A5 输出边界 | DTO严格白名单及两槽，0值不丢；无Trace／Span／实例ID、Parent／Link、配置、SQL、凭据或日志正文；业务User访问平台Trace／日志入口沿原规则拒绝，不通过平台接口补齐租户数据。 |
| A6 真实Console | 打开详情尚不查询阶段，显式打开才发精确请求；失败样本父／子各验证一次可读展示，已确认缺样候选各验证一次缺样文案。阶段结果不改任务状态／总耗时；收起及关闭后清空，切换execution不串样。浏览器仍沿现有240秒测试／300秒容器上限，不要求同一真实窗口重复四主题矩阵，四主题／撤权／迟到竞态继续沿已通过T3证据。 |
| A7 清理 | 正式定义删除及旧会话拒绝、资源归属与外层零残留全部完成；业务断言与清理分别计证，清理成功不把缺样未覆盖或业务失败改为通过。 |

`not_recorded` 与 `outside_window` 不冒充正常未命中：新鲜、已启用生产者若无关联，记录实测并判样本前置失败；不关闭SDK制造not_recorded，不改时钟／历史或等待72小时制造outside_window。503／504／422／429、错误码不符、错绑或预算失败均是失败，不能归为正常缺样或通过重复刷新掩盖。若两个候选均可读，则该Owner的A4未覆盖，整体本轮不得判全部通过；不追加候选业务、改采样或自动开下一轮。

#### 4. 观察预算、证据与清理

复用原普通5%、慢1秒、尾采样120秒及72小时／20GiB政策，单查询仍共用Owner前开始的5秒／原并发与数据预算。两组业务终态后，沿原固定125秒自然尾采样等待；随后阶段观测共用一个最多90秒窗口，最多6轮、轮间至少15秒。每轮只查询失败父／子；成功父／子仅在最后一次补读，阶段API最多14次，另有最多4次浏览器显式打开。无并发扇出、不重置总截止；首次缺样及后继状态均保留，只有窗口末次仍未命中且独立精确404才计A4。上游错误立即停止；窗口只等待同批真实样本可见性，不重跑业务。上述是待确认的验收客户端上限，不修改产品查询预算。

整轮从准备／构建起最多60分钟，沿8 CPU／12 GiB总硬配额、4 GiB实际占用停止线、四秒完整采样及最多五秒间隔；每阶段使用全局剩余时间，剩余不足直接拒绝进入该阶段，不续窗、加配额或由本会话自行重启个人服务。已有宿主Docker计量／构建准入若失败，保留首失败并停止，不将本方案作为其绕行入口。业务截止后必须完成取消与清理，清理超时／失败另列，不能将超窗报告为通过。

先经业务User正式API删除本轮自有编排及query定义并核404，已有Engine及执行历史／授权／遥测只存在于本轮独占库／卷；Engine产品删除及历史保留策略不是本轮断言，不能把外层销毁记为Cleanup业务删除通过。四个原身份均登出并验证旧会话拒绝，关闭浏览器；再按准确run标签移除本轮容器、网络、卷、镜像标签、私有源码／构建缓存、日志和临时秘密，核验零残留。不得全局prune、删除共享缓存／Infra或他人对象。异常也只清本轮所有物，未完成的正式定义／会话清理须列为失败，不能以最后销毁掩盖。

只保留仓库外权限0600的脱敏报告：固定SHA／镜像与输入摘要、随机run标识、本轮execution UUID、身份／权限集合核对布尔值、断言结果、安全错误码、阶段观测／候选顺序／时间、耗时与资源峰值、清理摘要。原始Trace／Span／运行实例ID和完整API／AuthContext／日志仅在私有目录中暂存用于当轮核对，退出销毁，不进入租户页面或最终归档；密码、令牌、Cookie、TOTP及认证截图／HAR／浏览器trace／视频不落归档。平台诊断与业务User的独立会话不得混用。

#### 5. 确认后的实施顺序

1. 先将本方案确认结果落正式观测规范及现有入口说明，再在现有 `monitor-traces-local-gate.py`／`monitor-traces-local-online.py` 与Console的现有本机隔离浏览器支撑中接入租户阶段场景；保持唯一Make入口及当前四身份／权限，不新建Hosted workflow或旁路runner。租户与平台浏览器输入需按明确身份拆分，不把技术引用交给租户测试页。新增调用／断言的运行结果不能用旧入口验收结果替代。
2. 实施前核对现有源码输入摘要登记、路径发现与CI覆盖，必要适配同次提交。最小充分验证沿 `make test-monitor-traces-local-runner`、受影响Console前端标准入口及既有登记检查；若实际修改Common／Monitor／System生产代码或身份边界，重新识别门禁与确认范围，不借验收扩权。
3. 代码和确定性门禁通过后提交固定SHA，提出一次本机隔离真实运行的准确命令／身份／预算与清理摘要，取得单独确认后才执行。本轮后续取得的单次真实运行授权已用于2026-11-14首轮；本节不构成重试、下一轮或个人服务操作的自动授权。

**入口实施实证（2026-11-13）。** 已在原人工入口接入租户阶段HTTP读取、DTO白名单与精确终态校核、固定缺样候选、同一观察截止、单Owner请求日志关联，以及四次按需浏览器读取；平台／租户使用独立私有输入及真实登录上下文，复用既有正式登出并核旧会话拒绝。正式定义／四身份清理在业务成功或失败后均尝试，失败不由外层销毁掩盖。Source指纹登记已包含新接线；既有Execution CI路径选择补入Monitor来源，无新Make目标、workflow、权限或生产接口变更。

本地 `make test-monitor-traces-local-runner` 通过216项Python及既有System身份夹具Go测试；`make test-execution-online-ci-runner` 通过417项Python聚合与登记；共享登录的 `make test-monitor-http-local-runner` 通过53项Python、7项Node及Go。`make test-console-frontend` 通过260项单元、357项完整受控浏览器回归及构建，新增租户登录场景所在10项认证浏览器专项另验通过；首轮既有devPorts回收断言失败保留，未改该断言，后继完整标准入口通过，不声称已定位其偶发根因。以上是确定性／受控环境证据，尚未执行 `make test-monitor-traces-local`、身份准备、样本业务、独立产品部署或Hosted，真实链路验收仍待单独确认。

**入口自然Linux CI收口。** 固定 `0af7dd7cd576d6b3d95ce19f5a0f9443cff1d7a8`（含实现 `9c1a0e889`），Platform Run `43425265125`、push／attempt 1 的两个相关Job均已实际选择并执行成功：[Execution runner](https://github.com/pampa0629/addp/actions/runs/43425265125/job/131885017413)为207＋19＋158＋35＝419项Python全部OK、登记一致；[Console前端](https://github.com/pampa0629/addp/actions/runs/43425265125/job/131885017678)为260项单元、358项浏览器及Vite构建通过，日志含新增租户登录场景。实际执行步骤与selected=true均已独立核验，不以汇总绿或条件跳过替代。

本地417／357计数不改写：固定CI源码已包含前序 `165101b3e` 在原聚合模块 `online-continuous-fixture_test.py` 新增的2项风险夹具测试，远端首组为207；远端Console完整358项包含本地完整357项之后另由10项认证专项验证的新增登录场景。原始日志分别留在 `/tmp/addp-stage-execution-ci-runner.log`、`/tmp/addp-stage-console-full-second.log`、`/tmp/addp-stage-console-login-proof.log` 与 `/tmp/addp-ci-2200-1113-fjfm1ic6/logs/131885017413.log`、`131885017678.log`。早期Vite回收断言失败仍保留；本轮入口实施与相关Linux门禁缺口已关闭，正式Tenant User→Gateway→Owner→Tempo本机隔离窗口仍未运行，不能记为真实链路或T4验收通过。此次仅补证据，未追加运行、重试、Hosted、服务操作或授权变化。

**首轮本机隔离真实窗口（2026-11-14，失败终态）。** 用户单独授权后，启动前发现main新增纯文档提交，先停止调用并复核39项源码输入无变化，再固定 `148f444fd87a1dcc4925648eb8fbeeb935b7384e`；`make test-monitor-traces-local` 实际仅调用1次。窗口为08:25:49.881408—08:25:58.142596（Asia/Shanghai），共8.261188秒，run为 `addp-trace-local-505a25574c6e4999b5f5ea22fc28f538`，Make退出2／入口失败。首失败阶段 `isolated_deployment_credentials`，调用本轮 `trace-deployment-fixture.py` 的子命令退出1，固定错误 `command_failed_1`；报告仅保存输出SHA256 `91d48b1eaa0e0daf2896aaa631f764929d1edd215d1a3ed32601a5009a7c29c0` 及 `traceback` 标记，无具体异常类型或错误位置，不足以确认OpenSSL／证书生成或配置渲染根因，未重放该命令推定原始错误。

未进入产品构建、依赖就绪、正式身份／登录、业务样本、租户阶段或Console断言；A1—A6均未运行，A7仅外层资源清理已完成，正式定义删除和会话拒绝尚无业务对象可验。`resource_samples=[]`、`dependencies_ready=false`，不能声称四秒资源采样、8 CPU／12 GiB实测峰值或4 GiB停止线已被本轮运行验证。入口 `cleanup.zero_owned_resources=true, failures=[]`，另按本轮精确标签／名称只读复核容器、网络、卷、镜像标签及builder容器均0；不外推个人／共享环境的全局清理。没有创建业务身份／任务、重启个人服务、运行Hosted／容量／定时，没有重试或自动追加下一轮；清理不改变本轮失败和未覆盖结论。

原始脱敏报告、外层起止回执及只读清理复核保留于 `/tmp/addp-tenant-stage-real-20261114-ejaet07p/{report.json,window.json,audit.json}`，报告权限0600、SHA256为 `8d5dcaa733d2c1ba4c3e6d43423fa30a2c369089d9aee2e58db6e4d412eae662`。早期失败报告未写入input_sha256或产品构建身份；audit中的39项输入摘要由固定Git源码只读复算并明确标注，不冒充本轮动态构建回执。临时证书／私有源码按原生命周期销毁，未保留秘密或原始子命令输出。“调用追踪体系”会话只读复核同一报告及固定源码，确认原证书fixture未输出细分步骤、OpenSSL子进程正文已丢弃，现有确定性测试对该证书命令使用mock而非真实执行；默认OpenSSL配置依赖仅是静态候选，未证实为本轮根因。后续获单独授权补充凭据准备的有限步骤／封闭异常类别回执与确定性回归，不新增命令、探针或等待；该诊断实施不补写本轮根因。完成诊断与门禁后，新的真实运行仍另行确认，不以改环境、放宽预算或重试绕过首失败。

**凭据准备诊断收口（2026-11-14）。** 按本轮确认范围先补正式观测契约，再将原唯一凭据fixture改为可导入且无执行副作用的实现；原九次OpenSSL调用、参数、顺序、文件与权限操作保持不变。回执只含16个固定步骤的已完成前缀／当前步骤及封闭错误类别，上限2048字节，无异常正文、路径、参数或秘密；入口仅识别准确fixture命令的原捕获流，错误／缺失／冲突／超限回执明确不可用，仍保留原命令失败。将fixture本体补入固定源码输入摘要，当前清单40项；现有Make生命周期与Platform consistency执行新增用例，Execution Linux门禁核验登记，既有路径选择已覆盖；无新增入口、探针、等待、配置或权限。

新增6项确定性测试逐点注入全部16步故障，核验命令次数、九次原调用顺序、异常脱敏、成功／失败回执一致性、限额与错误输入拒绝、准确命令归属及失败退出码；既有产品渲染衔接测试同步核验成功回执。本地实施与确定性验证未启动本机Trace真实凭据准备、身份准备、产品部署或业务验收，不改变原首失败结论；首轮报告SHA256再次核对未变。下一步先以携带诊断的固定源码取得实际失败步骤与类别，新的真实窗口另行确认；诊断不足前不修改OpenSSL默认配置或推定根因。

本地确定性门禁：`make test-monitor-traces-local-runner` 222项Python、登记与System身份夹具Go通过；`make test-infra-runtime-log-lifecycle` 共537项Python（含前述222项）、7项Node及夹具Go通过，PostgreSQL指标真实恢复未选择／未安装，不计为通过；`make test-execution-online-ci-runner` 207＋19＋158＋35＝419项Python及登记通过。新增凭据用例未启动OpenSSL、Docker或服务，既有HTTP离线TLS单测仍按原标准入口执行。首个本地新增测试因macOS临时路径别名导致prepare故障未注入而失败，已将测试工作目录解析为规范路径并重跑通过，原失败日志保留于`/tmp/addp-credentials-diagnostics-local.log`；最终日志为`/tmp/addp-credentials-diagnostics-{local-final,infra,execution}.log`。以上本地结果不代替真实窗口或Hosted；本次固定源码的Linux核验结果如下。

**固定源码Linux CI已收口。** `4b4c3975799dd0f3a97c3be7b2c8e1cfcfa9963e`、push／attempt 1 的三个相关执行Job均到成功终态：[Platform consistency](https://github.com/pampa0629/addp/actions/runs/43536539805/job/132266825020)于北京时间09:42:42完成，日志09:13:52明确记录凭据runner整组222项Python全部OK，无跳过，后续登记、System身份夹具Go及169项生命周期回归通过；固定源码的标准入口无名称过滤、模块无load_tests／skip，新增6个无条件方法确实纳入该次整组执行，不能只依据Job绿或源码登记推定。[Execution runner](https://github.com/pampa0629/addp/actions/runs/43536539805/job/132266825193)于09:10:49完成，selected=true，207＋19＋158＋35＝419项Python及登记通过。[Trace T2](https://github.com/pampa0629/addp/actions/runs/43536539623/job/132270120130)于09:13:26完成，实际使用新fixture，回执16步全部完成、step=finalize／error_category=none，TestOwnedTracePipeline通过，末尾确认容器／网络／卷及临时文件零残留。这是Linux真实Trace T2证据，不是正式Tenant业务用户验收或Hosted。

原始日志、固定源码方法／输入清单及核验摘要保留于`/tmp/addp-4b4c39757-linux-ci/{platform.log,execution.log,trace-t2.log,selection.log,source-manifest.json,audit.json,latest.json}`（目录0700、文件0600）。本轮仅只读跟进既有CI并补证据，无需代码修复、未重跑CI；首轮报告SHA256再次核对未变，其失败阶段和根因未明的结论保留，其他会话改动保留，未重启个人服务、运行Hosted或追加真实窗口。本次诊断实现及Linux门禁缺口已关闭；后继本机单轮复验与原因核对见下，Linux成功不外推为本机链路已通过。


**携新诊断本机单轮复验（2026-11-14，失败终态）。** 用户另行批准一次后，先核对当前`8be7ef2c20de434c86454b4e60b6fc7cfa39b4de`与已验CI的`4b4c39757`：40项专项输入逐字节一致、均无未提交改动；其间其他会话的结果导出／CDC等已提交改动未改变本轮身份夹具、阶段追踪读取、样本、采样、预算或清理入口。不将旧SHA的CI结果记为新HEAD全部代码已验证。本轮固定8be，仅由原Make入口建立已提交私有源码快照，不混入其他会话在制改动。

`make test-monitor-traces-local`实际仅调用1次，原四身份／11项业务权限、60分钟、8 CPU／12 GiB、4 GiB停止线、四秒检查及查询／浏览器预算不变。窗口为北京时间09:52:56.757367—09:53:05.116420，共8.358316秒，run为`addp-trace-local-c5250c3e909f4b2e9e71359dc07f4666`，Make退出2。本轮首失败仍为`isolated_deployment_credentials / command_failed_1`；完整诊断回执明确`completed_steps=[prepare]`、`step=ca_server`、`error_category=openssl_command_failed`，即第一条自签CA的OpenSSL调用失败、凭据fixture退出1；OpenSSL原始子进程码未单独归档。尚未进入其他CA／CSR、配置渲染、构建、依赖就绪或正式身份／业务／阶段／Console断言。`resource_samples=[]`，不记资源峰值／停止线实测通过；A1—A6未运行，A7仅外层清理完成，无正式定义或会话可验。

原报告`cleanup.zero_owned_resources=true, failures=[]`；按本轮精确标签／名称另行只读复核容器、网络、卷、镜像标签、builder及私有工作目录均0，未删除共享缓存或他人对象。脱敏报告、起止、源码核对、清理和诊断保留于`/tmp/addp-tenant-stage-real-diag-20261114-4wvaifaw/{report.json,window.json,preflight.json,audit.json,diagnosis.json,run.log}`，目录0700／文件0600；preflight输入摘要来自启动前Git核对，不冒充提前失败时未写出的构建回执。报告SHA256为`47f1668a7ed8ad916c89796e7ad068641a4cf207171936a616201ef26e0e1335`。首轮旧报告SHA256仍为`8d5dcaa733d2c1ba4c3e6d43423fa30a2c369089d9aee2e58db6e4d412eae662`，未改写或补算。

**原因与证据限度。** 退出后只读查询版本／文件事实：当前PATH选中Homebrew OpenSSL 4.1.0，OPENSSLDIR为`/opt/homebrew/etc/openssl@4`，其`openssl.cnf`不存在；fixture未传`-config`，runner窄环境未传`OPENSSL_CONF`。对应版本[req.c](https://github.com/openssl/openssl/blob/openssl-4.1.0/apps/req.c#L532)先读取默认配置，失败即沿初始退出码1返回；[配置加载实现](https://github.com/openssl/openssl/blob/openssl-4.1.0/apps/lib/apps.c#L339)打开文件失败返回空。由实际ca_server失败、宿主文件事实和该版本源码，原因已收敛为fixture隐式依赖宿主默认OpenSSL配置，而本机缺少该文件；该次只读判断未重放原命令或恢复原stderr；后继受控离线复现与修复见下，不回填原始输出。原OpenSSL正文仍被原实现丢弃，未恢复或归档私钥／证书；没有重放证书命令、修改宿主配置、切换工具链、重启个人服务、运行Hosted或追加下一轮。

**后继实施。** 用户已确认fixture私有配置与随后明确的SKID／AKID方案，实施及验证见下；不通过给宿主补文件或切换openssl版本绕过。新的本机真实窗口仍须单独确认，正式身份／业务／阶段／Console链路仍未覆盖。


**私有OpenSSL配置与显式密钥绑定（2026-11-14）。** 用户已确认自包含最小配置。正式观测规范先同步：prepare在本轮私有目录生成0600的req／dn及CA密钥标识配置，九次子进程均显式绑定OPENSSL_CONF、六次req另带-config；未改宿主配置、环境、工具链、证书用途／RSA 2048／1天／CN／SAN／EKU、身份权限、预算及清理。旧实现的受控离线用例先复现ca_server失败，修复后九次真实生成及全部16步骤完成；原16点注入、封闭类别、私有配置失败先于OpenSSL、环境不全局改写等确定性检查通过。原两次真实报告SHA256复核未变，未追加真实窗口。

`make test-monitor-traces-local-runner`在加入密钥标识前，224项中223通过／1失败，原失败不能改记为通过。唯一失败为新增受控离线链验证：OpenSSL 4.1服务器证书验证返回error 18；同一次离线生成的只读解析确认三组CA／叶证书均无SKID／AKID、subject与issuer同名。对应版本[req说明](https://docs.openssl.org/4.1/man1/openssl-req/#history)和[x509说明](https://docs.openssl.org/4.1/man1/openssl-x509/#history)确认4.0起不再自动添加密钥标识；现有fixture在CA和对应叶证书使用相同CN，缺少标识后不能通过当前OpenSSL信任链校验。没有改为信任叶证书、忽略校验、换工具链或删断言；测试临时证书仍由TemporaryDirectory退出销毁。日志为`/tmp/addp-trace-private-config-{red,green,chain-diagnostic,chain-facts}.log`；其中red为旧实现回归，green仍因新增链验证失败，文件名不表示通过。

用户已明确同意显式绑定SKID／AKID：CA在私有配置加入`subjectKeyIdentifier=hash`、`authorityKeyIdentifier=keyid:always`，叶证书在原扩展文件加入`subjectKeyIdentifier=hash`、`authorityKeyIdentifier=keyid,issuer`，保持现有CN／SAN／EKU、算法、期限、九次生成及原运行预算／权限不变；统一显式声明各版本的签发密钥关联，不按OpenSSL版本走分支。受控离线T1已在本机OpenSSL 4.1.0通过原九次生成、全部16步、RSA 2048／一天有效期、真实CA和叶SKID／AKID匹配、三条正确信任链及错误CA／用途／名称三项拒绝；共22次命令（含验证），小于原定24次，约0.38秒／材料与输出峰值32992字节，私有目录清理完成。`make test-monitor-traces-local-runner`整组224项、登记与System身份夹具Go通过；原ca_server及后继error 18失败日志保留，不能以当前通过改写历史。原标准入口／Platform consistency与Trace T2已覆盖fixture，Execution runner已有scripts路径选择与登记核验，无新增入口、依赖或环境。本轮仅确定性与离线验证，未启动新的真实窗口或Hosted；后续真实运行仍单独确认。


本批最终本地门禁：`make test-monitor-traces-local-runner`224项Python、登记及夹具Go通过；`make test-infra-runtime-log-lifecycle`539项Python（包含224项）、7项Node及夹具Go通过，真实PostgreSQL指标恢复未选择／未安装，不计为通过；`make test-execution-online-ci-runner`207＋19＋158＋35＝419项Python及登记通过。最终日志为`/tmp/addp-trace-private-config-keyids-green.log`、`/tmp/addp-trace-private-config-infra-final.log`、`/tmp/addp-trace-private-config-execution-final.log`。标准Make／现有路径和Linux编排已覆盖，无需新增入口或CI环境；本地通过不代替Linux或真实验收；本批Linux实际核验结果如下。


**私有配置／密钥绑定Linux证据已收口。** 实现`edd6c31ca2e7970ad82b97053fc8ac79d9a6bf26`、push／attempt 1 的三个相关执行Job均成功终态：[Platform consistency](https://github.com/pampa0629/addp/actions/runs/43546730624/job/132299691394)于北京时间11:38:38完成，日志11:08:57确认224项Python全部OK、无跳过，新增私有配置失败用例及离线用例均由原无过滤模块入口实际执行；离线摘要为OpenSSL 3.0.13、原九次生成／共22次命令、1.652425秒、材料／输出峰值33073字节、三项拒绝与私有目录清理通过，后续登记、Go身份夹具及169项生命周期回归通过。本机OpenSSL 4.1.0与该Linux版本使用同一配置／扩展路径，不靠版本分支或放宽信任。[Execution runner](https://github.com/pampa0629/addp/actions/runs/43546730624/job/132299691443)于11:03:08完成，selected=true，207＋19＋158＋35＝419项Python及登记通过。[Trace T2](https://github.com/pampa0629/addp/actions/runs/43546730635/job/132304967523)于11:23:55完成，新fixture真实完成全部16步骤、finalize／none，TestOwnedTracePipeline通过，退出核验容器／网络／卷／临时文件零残留。

原始Job日志、固定源码方法／本地验证清单及核验摘要留在`/tmp/addp-edd6c31ca-linux-ci-20261114/{platform.log,execution.log,trace-t2.log,selection.log,source-manifest.json,local-validation.json,audit.json,latest.json}`（目录0700／文件0600）。两次真实失败报告哈希再次核对未变，旧ca_server及离线error 18失败保留；其他会话改动保留。当前已确认的私有配置与SKID／AKID开发及相关Linux门禁缺口已关闭，本轮没有改宿主配置、重启个人服务、手工重跑CI、触发Hosted或追加本机真实窗口。剩余为原本机完整运行中的构建／资源守卫、正式身份、业务、阶段读取和Console断言复验；不以离线／Linux Trace T2通过冒充正式Tenant用户链路验收。后继单次本机真实运行已获另行批准，实际结果见下；Linux结果不外推为该轮正式用户链路通过。

**携私有配置／密钥绑定的本机单轮（2026-11-14，浏览器失败、总回执缺失）。** 固定源码`37ca500918eef11fc63ce28a0b92b6dacb28d17e`；40项专项输入与已验`edd6c31ca`逐字节一致、无未提交改动。其间其他会话已提交的资源导航授权、Avro、Transfer及文档改动经核对未改变本轮四身份／11项业务权限、样本、采样、预算或清理入口，不把旧SHA的CI外推为新HEAD全量验证。原`make test-monitor-traces-local`仅调用1次，北京时间11:47:21启动，run为`addp-trace-local-bee89889f95b41f790cf6273075206a3`。本轮越过此前ca_server失败，完成凭据与部署准备、七个ARM64二进制和十个产品镜像构建，身份夹具退出0。按固定控制流已越过前置API业务／阶段检查并进入真实Console，但原runner总回执未写出，逐项API断言及资源采样未归档，不能记为完整通过。

保留下的浏览器安全回执为`result=failed, stage=causal-link`，仅确认平台密码／MFA及保留失败终态可见两项；浏览器容器于12:02:35启动、12:06:38退出1、OOMKilled=false。当前回执不能定位关联行点击、实例读取、目标Trace读取或后续断言中的具体失败点，不推断根因。等待会话中断后原exec句柄已不可恢复，Make退出码和原runner终态报告不可用；后置安全核验与恢复清理单独留证，不伪造原报告或补算资源峰值。正式定义DELETE／404和四会话注销未取得回执，不记为通过。

已用原Lifecycle清理实现恢复本轮精确归属清理；首次恢复的镜像通配查询漏掉仓库路径，后置复核发现十个本轮镜像标签，按精确仓库前缀补清，退出0，原中间回执保留。最终12:20后的只读核验确认本轮容器／网络／卷、builder容器与状态卷、十个产品镜像标签及私有目录均0；仅保留脱敏证据目录`/tmp/addp-tenant-stage-real-keyids-20261114-57cayiaf`（0700／文件0600）中的`preflight.json`、原未完成`window.json`、`browser-failure.json`、`interruption.json`、`cleanup.json`、`image-cleanup.json`、`audit.json`和空`run.log`。原两轮失败报告SHA256均未变。未重启个人服务、改权限／预算、触发Hosted或追加运行。本轮之后的causal-link安全诊断及首失败落盘已按单独批准实施，见下；原真实失败与证据缺失不由新确定性验证改记为通过。完整Console、请求链、正式API清理与资源守卫仍需另行批准的真实运行，不以外层零残留替代。

**causal-link诊断与首失败落盘（2026-11-14）。** 正式观测契约先同步后实施：原关联绑定、点击、实例响应／状态／JSON／身份、目标Trace响应／状态／查询范围及页面断言分别记录固定子步骤；仅保留既有Promise收到的HTTP状态及封闭错误类别，不输出异常正文、URL、响应体、凭据或技术引用。原两个响应Promise原位观察，原业务请求、断言、240秒／300秒及整轮预算不变。宿主在原总报告旁独占预留0600的首失败文件，只向原浏览器绑定该单个文件；首失败立即冻结并同步写入／fsync，最多2048字节，后继错误与私有目录清理不覆盖。空文件不作通过回执，写失败不替换首因，观察或写入前硬终止仍可能无证据。没有新增入口、依赖、身份、探针或等待；既有OWN_INPUTS、Console及Execution路径选择已覆盖。

确定性用例直接执行现有浏览器用例的合成页面调用，覆盖18个失败子步骤、原200／404成功分支、原请求／点击数量、HTTP状态、真实Playwright断言异常分类、错误脱敏、文件权限／拒绝覆盖、写入失败保留首因，以及子进程直接退出后首失败仍保留；宿主侧注入验证单文件绑定、原300秒及外部证据在私有目录清理后保留。原三轮真实失败证据核验未变，本批没有重启服务、Hosted或新的真实运行；不将确定性验证记为旧causal-link根因已修复。最终本地`make test-monitor-traces-local-runner`通过227项Python、登记及System身份夹具Go；`make test-console-frontend`通过31文件／284项单测、370项浏览器夹具及生产构建；补充带GITHUB_ACTIONS标记的单测环境复跑284项通过，仅合成测试Worker隔离标记，真实准入仍拒绝Hosted；`make test-execution-online-ci-runner`通过207＋19＋28＋158＋35＝447项及登记。初轮夹具失败与超时日志保留，不计为通过。日志、源码摘要与旧证据哈希核对见`/tmp/addp-causal-link-{runner-complete,console-complete,browser-unit-ci-env,execution}.log`及`/tmp/addp-causal-link-prepublish.json`；固定源码的Linux实际结果见下；完整Console／请求链／正式API清理和资源守卫仍待真实证据，不以合成用例替代。

**`25a62c07c` Linux终态核验（2026-11-14）。** 固定`25a62c07c634d6fa0ea955d9343b33c82e6122f0`、Run 43564942822、push／attempt 1，三个相关Job均已到终态。[Console](https://github.com/pampa0629/addp/actions/runs/43564942822/job/132356611715)于北京时间14:39:35成功：专项文件30项、全部284项单测、358项浏览器夹具及构建通过，无跳过；固定源码30项由10个普通用例＋18个失败子步骤＋2条200／404参数化分支组成，包含首失败立即冻结／落盘、错误脱敏、权限／覆盖拒绝及真实Node提前退出后文件保留。358与本地370的差额来自其他会话未提交的11项配置页用例和1项栅格配置用例；CI沿原完整入口执行，没有缩减本次专项。[Execution runner](https://github.com/pampa0629/addp/actions/runs/43564942822/job/132356611551)于14:20:32成功，207＋19＋28＋158＋35＝447项及登记通过。

[Platform consistency](https://github.com/pampa0629/addp/actions/runs/43564942822/job/132356611678)于14:49:52失败，不能记为整个Job通过；其中本次Trace local runner已于14:25:14实际通过227项Python、无跳过，后续登记及身份夹具Go于14:25:20通过。按固定源码完整模块入口、206＋18＋3＝227项方法计数和无自定义loader／过滤核验，新增两个宿主落盘用例确实执行通过。最终失败发生在后续`test-online-runner`的指标来源授权夹具：`MetricSourceGrants.target() takes 5 positional arguments but 6 were given`，共4项错误；相关`online-engine-registration.py`及`metric-service-revision-lifecycle-online_test.py`不在本次七文件改动内。按本会话范围仅保留该原始失败，不调查、修复或重跑无关夹具；本次诊断实现无需追加修改，不宣称聚合门禁全绿。

Job原始日志、固定源码／用例清单及核验摘要留在`/tmp/addp-25a62c07c-linux-ci-20261114/{console.log,execution.log,platform.log,run.json,latest.json,observations.jsonl,source-manifest.json,preservation.json,audit.json}`（目录0700／文件0600）。三轮真实失败证据及原11份本地日志哈希核对未变；未手动重跑CI、重启服务、触发Hosted或新开真实窗口。本次新增诊断与首失败落盘的Linux执行证据已补齐；下一步建议单独确认一次携新诊断的本机隔离真实复验，读取causal-link首个失败子步骤，保持原身份权限、采样、预算和清理范围；旧实跑根因及完整真实链路仍未收口。

## 七、确认范围与协作分工

用户已确认由本会话统一牵头，正式契约先提交，按 Common → Graph → Cleanup 实施；已确认细节直接推进。已批准的补充边界包括 Graph 窄 reconcile／原目标 reopen、Cleanup 安全展示、目的限定执行授权、专业结果窄读取、Meta／Manager 独立治理和显式配对、原生三身份／双入口、Engine 授权用途分离、Manager 专属 CI 基线以及 Transfer capture_cleanup 连接用途。

已确认方案内的实现和确定性门禁直接推进；每次新的 Hosted、环境切换仍须单独确认。遇到新的权限、概念、模块边界或操作范围歧义，只暂停依赖该决定的工作，其余独立门禁继续。

| 负责人 | 当前职责 |
| --- | --- |
| 本会话 | 正式契约、Common／Graph／System／Owner 整合、专项 runner、按证据修复与分层交付；只提交自有改动 |
| CI 会话 `01a115b5-90bc-7d11-9a68-ffd2af505611` | 共享 Make／workflow／选择／登记、相关实际 CI 终态协作，不代产品判断或自动扩大运行范围 |
| 原 Graph／日志／Ontology／GLB 等会话 | 本轮交接已纳入实现，无需继续为本专题预留开发容量；新发现按准确文件范围协作 |
| p0 功能清单维护会话 | 接收实际实现与验证层级，由其核对清单；主责不代改工作簿，不把方案或清理成功记为功能完成 |

稳定规则已进入正式规范，本专题只维护阶段性结果、剩余项与待讨论设计，整体关闭后删除，不继续积累逐轮运行流水。

## 八、关键证据与未决方案

### 8.1 已完成的实现与确定性门禁

| 里程碑 | 固定源码／关键证据 | 实际结果与边界 |
| --- | --- | --- |
| 初始恢复时间修复 | `cc293ab89547744a646ae7c9eae531eb7d83b594`；[Platform](https://github.com/pampa0629/addp/actions/runs/40645503241)、[Release/T2](https://github.com/pampa0629/addp/actions/runs/40645503267) | 七项相关 CI 通过；恢复 pending 清空 started_at |
| 首期产品接线 | `138ef83ba657f5554576aed9594e17f88949336a`；[Owner／Common／System Cleanup T2](https://github.com/pampa0629/addp/actions/runs/40877071516) | 十 Owner、持久隔离、迟到写入阻断与专业结果已提交；当轮 IAM／Monitor 失败，后续分别修正，非整轮通过 |
| IAM／Monitor 修正 | `af28a3fc750287d20ee9620ce2e0088b033b4db1` [完整 IAM PG](https://github.com/pampa0629/addp/actions/runs/40885300963/job/123487644831)；[Monitor PG](https://github.com/pampa0629/addp/actions/runs/40880761833/job/123475011425) | 实际通过，覆盖权限目录、迁移与 Cleanup 只读快照；不覆盖真实故障 |
| Manager GLB 收口 | `0fc6299d8150ec5e5cf3ae4cd72657cd759ea969` [Manager PG](https://github.com/pampa0629/addp/actions/runs/40889932763/job/123503994261) | 九个新增审计／竞争／锁序场景实际通过；外部删除为受控回调 |
| Engine 删除用途分离 | `1abab579ce140636aafd6fca4a4aac832db1dd84` [IAM PG](https://github.com/pampa0629/addp/actions/runs/41217708095/job/124593738613)、[Cleanup PG](https://github.com/pampa0629/addp/actions/runs/41217708095/job/124593738618) | 六权限身份、两用途隔离、当前权限到期和真实评估受理实际通过 |
| Manager CI 基线 | `9c64c571541bf086556d9b5f03c0778689c46633` [Manager PG](https://github.com/pampa0629/addp/actions/runs/41317682955/job/124881638812)；`1d15c0b44a9314783ab3efaeecd59009ade818b7` [夹具复验](https://github.com/pampa0629/addp/actions/runs/41319301973/job/124885911941) | 完整生产初始化 31 项、PostgreSQL 15.19／PostGIS 3.7.0／pgvector 0.8.0、零残留；夹具 Linux 复验通过 |
| 专项 proxy 修复 | `9591622eaf1e391f5f2d755ce1028797ec7d1eed` [Execution runner](https://github.com/pampa0629/addp/actions/runs/41331263958/job/124921686220)、[Manager PG](https://github.com/pampa0629/addp/actions/runs/41331263940/job/124921864453) | 真实 SDK chunked body 红绿复现；102＋168 runner 与 31 项 PG 通过，不代表当轮 Hosted 通过 |
| Manager 两阶段读取／摘要 | `2f83f41b85167aa001b676ed4e9f53e56161141e` [Manager PG](https://github.com/pampa0629/addp/actions/runs/41396663330/job/125119851418)、[Execution runner](https://github.com/pampa0629/addp/actions/runs/41396663333/job/125119346932) | 31 项 PG、114＋169 runner 通过；System 未变输入复用 `3f281d49c` 的实际独立门禁 |
| Transfer 窄连接用途 | `8ceefc5b0300dfb2fbdff0244bb90e07bbec0cef`；[独立 Go／runner](https://github.com/pampa0629/addp/actions/runs/41427877937)、[PG](https://github.com/pampa0629/addp/actions/runs/41427877912) | System／Transfer Go、Execution115＋171、Common67／Transfer19／Manager31／Cleanup6／IAM177 顶层 PG 实际通过，PG 无失败／跳过；聚合失败不计通过 |
| Kafka group 精确缺失 | `c63ce20610ba5c5aec8983a4610036131814c485` [Transfer Go](https://github.com/pampa0629/addp/actions/runs/41465288530/job/125344420897)、[runner](https://github.com/pampa0629/addp/actions/runs/41465288530/job/125344421494)、[Transfer PG](https://github.com/pampa0629/addp/actions/runs/41465288396/job/125344986466) | 实际通过，115＋173 runner／19 PG；精确无矛盾 Dead 缺失描述可判 absent，错身份仍拒绝 |

### 8.2 已取得的真实运行证据

| 场景 | 固定源码与实际 Job | 实际结果／可确认范围 |
| --- | --- | --- |
| Quality bounded recovery | `1471f8fc80208f7b6d4c1e841cf030fc5d0bfd80`；[Hosted 40653912002](https://github.com/pampa0629/addp/actions/runs/40653912002) | 崩溃补回、暂停后真实租约过期、attempt2／新时间及旧 heartbeat 拒绝、投影与清理通过；仅此历史源码和场景 |
| Transfer DLQ unknown | `046b7e973ad2b77f5ad7debd958c45dc2f95bae0`；[Job124524382801](https://github.com/pampa0629/addp/actions/runs/41196761622/job/124524382801) | 2026-10-31 **7/7**：真实 DLQ、显式删除真实发送／丢回包、原生 topic 消失、任务和索引保留、不重发、新 producer409、目标业务行保留 |
| Transfer Engine 清理基线 | `01b8d23f5e3d565385247c2713709fb237acb3d2`；[Job125357128912](https://github.com/pampa0629/addp/actions/runs/41468942958/job/125357128912) | 2026-11-02 **7/7**：真实 Capture、实际 invocation 停止、原生 Capture 消失、Engine 流程保留任务和 DLQ、专业结果／回执一致、显式 task DELETE 删除 DLQ、业务行保留 |
| Transfer Capture unknown | `5878c3ae2d799d2899dd4c6b21276fb37de64501`；[Job125440763556](https://github.com/pampa0629/addp/actions/runs/41493050232/job/125440763556) | 2026-11-03 **8/8**：真实 Capture 与停止、connector 消失、真实删除回包丢失、unknown hold 保留、Engine 仍 deleting、新 producer 拒绝、业务行保留；**不表示 Engine 删除完成** |

上述三项 Transfer Job 均独立核过十四服务同准确 SHA、checkout 干净、原普通身份／权限、精确 checks、控制恢复、退出清理及 Infra 零残留，成功报告不含失败诊断。它们不覆盖 Graph 原生写图、positive reopen、Manager native late writer 或全部 Cleanup 矩阵。

保留关键原件摘要，避免逐轮重复文件清单：

| 场景 | 原日志 SHA-256 | 私有原件目录（临时，不替代持久证据） |
| --- | --- | --- |
| DLQ unknown | `8842c50a56d28692dd88c2d744f237042e85f6d7e9359d419816cd71a7a61ab9` | `/tmp/addp-transfer-dlq-hosted-sixth-20261031/` |
| Engine 基线 | `877461d5ca352ff1543293d1e0c3e5d034fa2069cbb702a302217fffc12444de` | `/tmp/addp-transfer-engine-hosted-thirteenth-20261102/` |
| Capture unknown | `6a45d1c53d56ab9ca39150ed89ca02ab0b012b3aca2092577448c0a44c56aff9` | `/tmp/addp-transfer-capture-unknown-hosted-second-20261103/` |

### 8.3 过程记录合并：已修正问题与仍未知的历史原因

| 问题类别 | 收敛结果 |
| --- | --- |
| 身份／预检 | 六权限不能直接建 custom 角色，已走正式模板组合并补共享译文；System／Gateway ready 按其正式缺省结构核验，业务 Backend 仍严格核实例身份 |
| 夹具／读取用途 | 删除 DLQ 分支无关 CDC Grant，以及 CDC 分支无消费用途的 Grant；Meta 扫描、Engine active／online 就绪在原 deadline 内核验，创建不自动重试 |
| 公共结果消费 | 按 cause 区分通用十 Owner 与 Engine 七 Owner；完整核验已授权公共子概览，仅保存原 Manager／Transfer 安全诊断，专业结果权限独立 |
| Manager 运行链 | CI 数据库补齐 vector；proxy 严格有界解码 chunked；生命周期读取改为既有脱敏 descriptor；预评估和 validation 对同一索引范围生成一致 digest；真实缺失按共享 ErrNotFound 返回404 |
| Transfer 清理链 | 专用 deleting Engine 连接裁决，保持源指纹与租约围栏；精确 Kafka group 缺失解释修正，不重发删除、不增加等待 |
| 诊断与真实消费者 | 补 execution 最后状态、初始行、父子及 validation／execute 的安全阶段；诊断计入原预算；重命名同步 System Cleanup PG 真实 DTO 消费者，不保留旧别名 |
| Capture 初态误判 | 曾将 PG seed 的 enabled=true 当作公开 continuous 初态，导致首轮0/8；已更正为真实 prepare_task 的 enabled=false／无调度契约，未改产品语义。`5878c3ae2` 的独立 runner115＋173及第二轮8/8通过 |
| 历史原因未证实 | DLQ 第五轮未观察到 running、Engine 第八轮初始目标行不匹配未获唯一现场根因，后继未复现；不得用成功倒推已修复未知原因 |

失败历史继续保留在 Git 与原 Run：DLQ 前五轮均未完成正式七项，Engine 前十二轮均0/7，Capture 首轮0/8；对应过程未被改记通过。Capture 首轮 [Job125404236001](https://github.com/pampa0629/addp/actions/runs/41482506517/job/125404236001) 保留错误预期实证；Engine 库存失败 [Job124907076861](https://github.com/pampa0629/addp/actions/runs/41326379080/job/124907076861)、validation 失败 [Job125068442244](https://github.com/pampa0629/addp/actions/runs/41379980834/job/125068442244)、Transfer group 失败 [Job125319788652](https://github.com/pampa0629/addp/actions/runs/41458076890/job/125319788652) 可追溯关键阶段。旧报告缺少底层细节的地方保持未知，不因本地复现声明现场唯一根因。

### 8.41 固定Neo4j原生身份反例T2

**范围与提交。** 复用 `make test-common-neo4j`、固定 Neo4j Community **5.26.24**、同一独占 Compose 服务和镜像，保持回环地址、库名和原连接身份；仅操作本轮自有数据卷，退出销毁私有归档、完整存储副本和凭据，不运行 Hosted 或接管个人服务。实现提交 `3527dccc508deeadab26214ab4ebfd10dca3d568`；CI 负责人提交 `eccf94b5016b29a0d6d14b4b9592be0a3234f6c7` 将六项生命周期 T1 接入现有 Make 目标，未增加 Job／服务／镜像／预算。

| 原生阶段／观察 | 实测结论 |
| --- | --- |
| baseline → restart | 同卷重启，候选身份稳定 |
| post-dump → recreated | 归档后追加标记，再销毁原卷、同名新建；新存储身份不同，数据为空 |
| restored → restored-restart | 新卷加载旧数据库归档，db.info 的 id／name／creationDate 被复制，实例 ID 可区分此情形；归档后标记消失 |
| clone-recreated → full-clone → full-clone-restart | 再次销毁并确认新存储后，加载包括 Neo4j 自身 system 存储的整卷副本；db.info、dbms.info 实例 ID、实际暴露的 databaseID 均与原目标相同，但归档后数据已回退 |

九阶段证实：**这组原生 ID 可随存储复制，不能证明严格原物理代际。** 目标身份、准确操作结果和停止证明不能互代；不能据此保存可信绑定、声明恢复支持或开放 reopen。生产 Provider／Graph 恢复能力未改，现有409和关闭准入保持，公共终态不改、无 force 或重跑。固定版本连接可读和错误密码拒绝已验证，不能外推普通角色的细粒度授权、其他版本或部署形态。

**本地验证。** Darwin／arm64 的最终标准 `make test-common-neo4j` exit0，181.06秒；六项生命周期 T1、原 Common／Graph 领域用例与九阶段全部通过，0SKIP、zero_residuals。日志24751字节，SHA-256 `7447843aa7c355f37ae59b675a2a5cfe01f6dd829ec0d512d128b8adc4706920`。`make test-common-falkordb-unit` 通过；`make test-t2-ci-registration` 78＋9及登记一致通过。原件 `/tmp/addp-neo4j-identity-t2-20261103/`。

首次 T2 曾因 Docker port=0 在重启后重新分配端口而失败；检查正确拒绝，清理零残留。已先用 T1 复现，再在采集基线前固定本轮首次分配端口，后续端口冲突直接失败，不换地址规避同目标断言；旧失败不改记通过。

**Linux 实际终态。** `3527dccc5` 没有独立 push Run；已推送后继 `42ae2bc5996b389fceea940f3ed3825457744bc7` 包含该提交，十个任务文件及 CI Make 输入已独立核同。[Run41529522013／Job125579406480](https://github.com/pampa0629/addp/actions/runs/41529522013/job/125579406480) 于北京时间2026-11-03 **06:09:42—06:17:38** 实际 success（push／attempt1）。主责 fresh API 和独立原日志确认真实选中、标准 Make、六项 T1、原 Common／Graph 主项、九个唯一阶段全部通过，版本5.26.24／community、0SKIP、zero_residuals；整卷克隆身份反例同样成立。原日志82718字节，SHA-256 `d0a9c169a8882a0087f4b1bf7aedfaf3fde2a7981fdcacaa15002d3db5358882`；独立证明 `/tmp/addp-neo4j-identity-t2-20261103/linux-final/root-independent-proof.json`。

该成功只收口准确42ae的身份反例 T2，不声明可信身份、Provider／绑定／正向 reopen 已实现或重复运行稳定。后继19e05289c的 [Job125628721437](https://github.com/pampa0629/addp/actions/runs/41548048797/job/125628721437) 曾在启动前置exit64／缺少initial-password参数，未进入九阶段；该失败保留。夹具故障后由包含19e的 `9cc552de2` 收口：[Run42216557525／Job127891879034](https://github.com/pampa0629/addp/actions/runs/42216557525/job/127891879034) 于2026-11-06 20:41:02（Asia/Shanghai）实际success，九个唯一身份阶段、0SKIP、zero_residuals。本次只读复核官方原日志与祖先关系，不新增运行；该成功不升级为可信原生绑定或高级恢复实现。

### 8.42 正向恢复的新边界（待讨论）

**当前决定（2026-11-03）：用户认可问题分析，近期沿用 System 的统一登记身份，高级恢复先搁置。** 两种高级恢复方案均保留为未批准实施的后续方向。登记 ID 不作为严格原物理身份证明；恢复证据 Provider、可信原生绑定和正向 reopen 暂不实施。现有已实现的执行管理、未决效果记录和失败关闭规则保持。

近期按既有[Engine Instance 身份与生命周期](../concepts/addp引擎体系图.md#engine-instance-身份与生命周期)及[System engines 表](../../system/docs/tables/engines表.md)收口，不新增另一套“数据库 ID”：

- System 的 `engine_id` 是登记实例的永久身份，由数据库 identity sequence 单调分配，不回收、不复用、不允许调用方指定；删除保留墓碑。搬迁或恢复原登记对象可沿用原 ID，改接另一实际引擎必须新建登记并由 Owner 显式重绑，历史 execution 快照不随之改写。
- Graph 具体目标继续绑定准确 Tenant／Graph、`engine_id` 和完整 `catalog_path`，已有 version／attempt／lease 校验保持。一个 Engine 中的多个数据库／图不能仅用 engine_id 混为一个目标；名称／地址相同不自动继承旧绑定或结果。
- 该规则避免平台登记身份复用导致引用串用；不能自动识别外部在同地址或同路径重建、恢复旧副本，也不能证明某次写入已提交或旧运行体已停止。未知效果继续隔离，不依据 engine_id、超时、删记录或人工确认直接放行，不自动重跑。

以后重新启动本项时，再明确恢复目标和治理边界，两种候选方向保留如下；均未批准实施：

| 后续方向 | 必须解决的条件与代价 |
| --- | --- |
| 严格原物理代际 | 取得不随数据副本复制、能实际控制／观察存储替换与恢复的可信来源，并在后续写入边界核验；现有 System 登记 ID／配置 version 和已测 Neo4j ID 均不能代替，还须独立证明操作结果及旧调用终结。 |
| 受管逻辑世代 | 明确可治理的图目标与写入／恢复范围；Graph 世代事实置于业务图备份之外，原生围栏／准确回执与业务效果原子提交，核清后显式开放。需解决控制面自身回退、外部旁路、内部元数据可见性、回执保留及 Cleanup；单独增加 UUID 或控制节点不足。 |

未来无论采用哪条路线，都不追改公共旧终态、不自动重放；索引／约束、空间操作和 Copilot 分别证明效果与停止事实。先统一术语与正式契约，再实施和验收；当前不新增权限、业务图元数据、存储生命周期权威或生产恢复接口。

### 8.43 Manager 原生成功基线准备

三类原生 Cleanup 已验收：[原生成功](#原生成功manager-success)、[对象效果未知](#对象效果未知manager-object-unknown)、[索引效果未知](#索引效果未知manager-index-unknown)。三类沿用原三名业务 User、独立源办理员、精确权限、各场景的原有断言与独占 Hosted 部署。总预算 120 分钟，业务预算 900 秒，保护投影、扫描与 MinIO 就绪共用 180 秒窗口。[T4 原生 Cleanup 契约](../spec/testing/T4-Online验收规范.md#原生-cleanup-的独立有限验收)

对象和索引效果未知下列各轮均通过控制恢复、显式清理及零残留核对。各轮业务断言以自己的结果为准。

#### 原生成功（manager-success）

**成功基线通过。** 第八轮 `9f1f68d5` 于 2026-11-06 06:21:43（Asia/Shanghai）成功结束。16 个镜像、Model3D smoke 和 14 个服务准入通过。首扫后实际观察到原生对象和索引投影，Engine 删除后读回终态并确认对象与索引均已清除。九项断言全部通过，含专业回执一致、清理审计保留及源 OBJ／MTL／纹理保持不变。控制恢复、显式清理和零残留均通过。官方五件归档与作业原始日志已交叉核验。[第八轮 Hosted](https://github.com/pampa0629/addp/actions/runs/42109764720/job/127533929017)

| 已落地的收口 | 验证证据及边界 |
| --- | --- |
| 准确源 Grant 前置；Meta 只取结构身份；统一 GatewayClient 保留 ZIP／GLB raw、响应头及 JSON；OBJ／MTL／纹理逐项核验长度和摘要 | `a641b1b6e` [Linux 专项](https://github.com/pampa0629/addp/actions/runs/41677128582/job/126241760694)198＋182项通过。 |
| 既有 `--jobs 1` 实时委托日志、固定阶段及返回码；固定 vcpkg 仅构建产品消费的 Release，保留依赖版本和全部 smoke | `f5bd7b905` [Product build](https://github.com/pampa0629/addp/actions/runs/41758545081/job/126340402757)真实 Linux amd64 Converter／Runtime 及全部 smoke 通过，不外推 arm64 或保证后续冷构建时长。 |
| Manager 私有 `storage_ref` 的唯一四字段 JSON 协议，复用已有 `object_ref`；保留 Manager 桶／本 Tenant 围栏及对象名的字面百分号、空格和 Unicode；删除误用 infra locator 的 URL 路线 | `277c8f6c5` 本地及[同提交 Linux 专项](https://github.com/pampa0629/addp/actions/runs/41815464385/job/126524957842)均200＋189项通过；第七轮已通过真实引用及对象探针。 |
| 模型准备 HTTP 失败只记录闭集操作及白名单错误码；成功报告拒绝该诊断，即使为空或 null；不加请求、重试、身份或等待窗口 | `3eae16d39` 本地及[同提交 Linux 专项](https://github.com/pampa0629/addp/actions/runs/41852005816/job/126650489413)均201＋190项通过、登记一致；Linux selected=true。 |
| 原生库存失败按首个未通过的探针记录闭集原因，成功报告拒绝该字段 | `8b03c91c` 本地及 Linux 专项均 401 项通过，登记一致。[同提交 Linux 专项](https://github.com/pampa0629/addp/actions/runs/42014953009/job/127203046053) |
| 首次 Meta 扫描先等待 Manager 的 Tenant 保护投影。原 User 对本轮模型指纹执行既有 GLB 列表 GET，原 180 秒内只等待明确的保护未就绪响应 | `241ac08e` 本地及同提交 Linux 专项均 403 项通过，登记一致。首扫时序、拒绝及超时边界有反例覆盖。[Linux 专项](https://github.com/pampa0629/addp/actions/runs/42054235504/job/127338131663) |

第八轮之前的失败和取消：

| 轮次 | 首个失败点或取消原因 |
| --- | --- |
| [首轮](https://github.com/pampa0629/addp/actions/runs/41586728539/job/125753855807)、[第三轮](https://github.com/pampa0629/addp/actions/runs/41736284282/job/126266219326) | 构建超时取消。 |
| [第二轮](https://github.com/pampa0629/addp/actions/runs/41639055422/job/125927590186) | ZIP 解码错误。 |
| [第四轮](https://github.com/pampa0629/addp/actions/runs/41791842460/job/126446790438) | GLB 生成与授权下载已完成，库存引用解析失败。 |
| [第五轮](https://github.com/pampa0629/addp/actions/runs/41833476102/job/126586309842) | 模型准备返回 403。原调用及错误码未归档，第六轮未复现。 |
| [第六轮](https://github.com/pampa0629/addp/actions/runs/41999442501/job/127150683595) | 库存检查失败，未记录具体探针。 |
| [第七轮](https://github.com/pampa0629/addp/actions/runs/42039158037/job/127285084068) | 引用和对象探针通过，首个失败点为索引未出现。未归档投递回执，唯一根因未知。 |

#### 对象效果未知（manager-object-unknown）

**首轮失败。** 固定版本 `2515e673` 于 2026-11-06 10:16:34（Asia/Shanghai）以失败结束。GLB、原生对象和索引可见、删除受理及对象实际消失共五项通过。后续拒绝验证收到 HTTP 500，原报告未标明具体请求。[对象未知 Hosted](https://github.com/pampa0629/addp/actions/runs/42138271441/job/127631855385)

以真实快显入口和 System 连接读取的 409 响应复现了 Manager 将生命周期拒绝包装成 HTTP 500 并返回下游文案。修正保留原查询及权责，快显动作在创建任务前返回安全的 409。runner 将现有拒绝链分为三个阶段，11 项断言保持原契约。修正 `1f24cf9b` 的本地后端和同提交 Linux 门禁均通过，含 Swagger 97 个公开路由及 runner 403 项。[Go 工作区](https://github.com/pampa0629/addp/actions/runs/42153149805/job/127680944498) · [平台一致性](https://github.com/pampa0629/addp/actions/runs/42153149805/job/127680944701) · [Execution runner](https://github.com/pampa0629/addp/actions/runs/42153149805/job/127680944784)

**第二轮失败。** `e52be373` 于 2026-11-06 18:07:58（Asia/Shanghai）失败，官方报告仍登记五项。阶段标记定位为 `manager_new_generation_refusal` HTTP 500；前序已观察持久隔离，Engine 读取及重复删除拒绝（409）已越过。该轮未完成拒绝链、隔离复核及源字节校验。[修正版 Hosted](https://github.com/pampa0629/addp/actions/runs/42188743649/job/127795258412)

按生产接线加入 Meta Client 后，回归复现了 Mosaic 提前查询将已清理 Meta 项的 404 包装成 500，绕过了 Engine 生命周期检查。Manager 删除该独立查询，从既有 Engine → Meta 解析取得 Mosaic 的规范来源，Engine 拒绝时在读 Meta 前返回既有的安全 409。权限、路由、11 项断言与等待预算沿用原契约。

**第三轮通过。** `c105b4ba` 于 2026-11-07 00:29:01（Asia/Shanghai）通过全部 11 项。实际对象变更后代理丢弃回包，持久隔离保留、Engine 保持 deleting、重复删除返回 409 且未重发、新生成被拒绝、源 OBJ／MTL／纹理不变。快显拒绝的精确 409 和错误码由回归测试验证，Hosted 按既有断言核对拒绝行为。[通过的 Hosted 作业](https://github.com/pampa0629/addp/actions/runs/42240606344/job/127969596209)

#### 索引效果未知（manager-index-unknown）

**首轮失败。** `f85842e7` 于 2026-11-07 10:56:42（Asia/Shanghai）失败，正式记录 5/11；GLB、原生对象与索引可见、删除受理及对象实际消失已通过。断言失败仍标为 `engine_tombstone_read`，无法区分代理回执、隔离记录或索引探针，未归档其现场值，实际根因保留未知。[首轮 Hosted](https://github.com/pampa0629/addp/actions/runs/42320210994/job/128242675201)

**诊断轮失败。** `69ecb66c` 于 2026-11-07 20:05:34（Asia/Shanghai）失败，正式记录仍为 5/11。首个失败探针为 `manager_unknown_index_hold`：准确代理绑定成立、仅转发一次且已有上游响应，索引目标为一条，投递 unknown 计数为零；Manager 子项在 `manager_cleanup_index_effect` 失败，Transfer 成功，Engine 保持 deleting。[诊断轮 Hosted](https://github.com/pampa0629/addp/actions/runs/42364486834/job/128379566458)

现行只读关联回执恢复允许投递离开 unknown，但不能结束原 Cleanup 或解除 hold。专项原先依赖瞬时状态，无法正确核验该合法恢复。修正只读取原快照中的精确 Tenant＋Engine／物理索引、原 execution／attempt、端点及代次一致的 prepared 操作、唯一删除投递、目标与隔离出口；拒绝前后排除瞬时 unknown 计数，其余事实仍完整比较。失败诊断仅增加该绑定 hold 的安全计数分类，不增加读取、重试、权限或等待预算，原 11 项验收目标保留。该轮未归档恢复后的具体投递状态，不能声称已证明 Hosted 中实际恢复为 succeeded。 本地标准 Manager Go T1、完整 PostgreSQL T2（含原回执恢复、不重发与 hold 保留）和 Execution runner 406 项已通过。

修正 `baa0965d4` 的同提交 Linux [Execution runner](https://github.com/pampa0629/addp/actions/runs/42371923824/job/128401640079) 实际通过 406 项；[Manager PostgreSQL T2](https://github.com/pampa0629/addp/actions/runs/42371923680/job/128401773028) 通过完整门禁，包含原回执恢复用例及数据库资源零残留。[Go 工作区](https://github.com/pampa0629/addp/actions/runs/42371923824/job/128401640127) 中 Manager 的 15 个有测试包通过，9 包无测试；该作业整体因 Ontology 的 Transfer 文档来源校验失败，不能记为全仓通过。[平台一致性](https://github.com/pampa0629/addp/actions/runs/42371923824/job/128401640129) 在 SQL pool 的 Node 前置检查失败，尚未进入平台检查主体；两项范围外失败未扩展修复。

**复验通过。** `ba3406a40` 于 2026-11-07 22:36:36（Asia/Shanghai）成功结束，11 项断言全部通过。原生对象和索引实际清除；删除请求仅转发一次且回包丢失，绑定原操作的持久隔离保留，Engine 保持 deleting，重复删除及新生成被拒绝，源 OBJ／MTL／纹理未变。五份官方归档与 14 个服务源码／实例绑定已交叉核验；本轮仅触发一次、attempt=1，身份权限、范围及预算保持原契约。[通过的 Hosted 作业](https://github.com/pampa0629/addp/actions/runs/42375229408/job/128411336585)

**后续边界。** 旧 Runtime 迟到写入另见 [§8.44](#844-manager-迟到写入验收准备)，新的 Hosted 须单独确认。Graph 高级恢复继续暂缓。原生成功和对象未知的通过证据分别位于 `/tmp/addp-manager-success-hosted-eighth-20261106/` 与 `/tmp/addp-manager-object-unknown-hosted-third-20261106/`。


### 8.44 Manager 迟到写入验收准备

`manager-late-writer` 已登记，runner 已验证。真实 Hosted 尚未运行，须单独确认。Graph 高级恢复继续暂缓。

#### Runner 验证结果

首轮复现父状态误判。修正后，runner 核对同一次公开 GET 中的请求、Engine、原用户、scan、Owner 集合及父子 execution。Manager 计划冻结失败时，子项应为 attempt=1、已开始且 `failed`，唯一失败阶段为 `manager_cleanup_plan`，没有清理回执。父在原 30 秒协调截止后为 `timeout`。授权失败、其他 Owner 或错误请求的失败不能代替。失败诊断复用同一次 GET 的安全投影。

本地 `make test-execution-online-ci-runner` 与固定提交 `9cd0b267d` 的 [Linux Execution runner](https://github.com/pampa0629/addp/actions/runs/42397885129/job/128479082973) 均实际通过 408 项，包含 12 类新增反例。Linux Job 命中既有 scripts 路径并通过登记校验。同提交 [平台门禁](https://github.com/pampa0629/addp/actions/runs/42397885129/job/128479082998) 在范围外 SQL pool Node 前置检查失败，未计整体通过。

#### 真实 Hosted 待核验

暂扣本轮准确 pending GLB 的真实上传，观察旧 attempt=1 在真实租约失效后失败，再发起同一 Engine 的只读删除评估。释放同一上传后，独立核对原生对象实际出现、旧终态和 pending 审计保持、公开结果未发布、新生成返回 409 以及源字节不变。退出时核对控制恢复和零残留。

#### 运行范围与预算

沿用原三名业务 User、源授权办理员和 11 项目标。仅控制本轮 Model3D 暂停及准确 Manager 容器的终止与补回，保留原镜像和容器身份。Engine 只做只读评估，禁止 DELETE。诊断不补读隐藏 Owner，沿用原请求次数、等待上限、入口、环境和权限。

部署限 120 分钟、业务限 900 秒、上传 barrier 限 180 秒、旧租约收敛限 90 秒，其他就绪和评估上限沿用原约定。barrier 过期即失败退出，不续期、不重发。退出须恢复本轮暂停的实例、释放本轮 barrier 并清理本轮资源。完整验收规则见 [T4 原生 Cleanup 规范](../spec/testing/T4-Online验收规范.md#原生-cleanup-的独立有限验收)。

### 8.45 Transfer CDC 审批精度与 Kafka 旧会话（后继 19 项已通过；额外核清待讨论）

[固定源码 Hosted 证据](../spec/testing/evidence/Monitor执行监控与留存历史证据.md#2026-11-10-固定源码复验)中，Kafka 持续写入、故障恢复及旧游标／浏览器核验通过，整套停在 CDC 审批补扫 claim。真实 PostgreSQL 回归使用同一纳秒样本，旧路径以 `.299069081` 生成来源，存储后读回 `.299069000`，claim 事务报冲突。审批更新现在以 `UPDATE … RETURNING` 取同一行的实际应用时间，按既有事务写来源，领取继续做精确比较。回归同时核对审批回滚、claim、完成及安全投影。标准 `make test-transfer-postgres` 本地与固定 `b3d1b86c1` 的 Linux CI 均通过 21 个主用例，零跳过。Linux Transfer Go T1 已实际执行通过。 [Linux PostgreSQL](https://github.com/pampa0629/addp/actions/runs/42820637708/job/129855701128) · [Linux Go T1](https://github.com/pampa0629/addp/actions/runs/42820637751/job/129844991841)

同一 Hosted 中，Kafka 旧 Worker 经 pidfd 验证已退出，替代 Worker 恢复；旧 invocation 有会话创建记录，缺少原运行体退出确认。业务删除在外部操作登记前等待所有历史会话，最终拒绝。现行 [Transfer 停止与 Cleanup 约束](../spec/addp-cleanup体系规范.md#十五已确认决策)只承认原 `Run` 返回后的停止证明。租约失效解决谁能继续推进执行，不能授权删除 DLQ。独占部署整体销毁的零残留与业务删除结果分别记账。

**已确认分开验证两条路径**。正常退出用独立自有 Kafka 任务，核对 invocation 停止回执、正式删除、精确 DLQ 资源缺失及授权历史仍可读。硬终止场景继续核验恢复和唯一终态，并以当前契约核验删除失败关闭、任务仍存在、未登记或派发本轮 DLQ 删除及外层销毁零残留。两组分别出具回执，不将硬终止后的销毁计为业务删除成功。两条路径当时已接入既有 runner，本地专项通过 98 项 Python、语法及身份夹具 Go；该时点的“待真实运行”已由后继固定源码验收补齐，不再作为当前待办。

固定 `e613a7381` 的 Linux Online 登记检查及 Execution online CI runner 通过。Platform consistency 随后在运行日志／指标生命周期测试失败，未运行到完整 Online 聚合，新增 Kafka 98 项仍只有本地专项结果。Transfer Go 与 PostgreSQL 按路径跳过，CDC 生产代码与前述 `b3d1b86c1` 相同，沿用其实际通过证据。 [Execution online runner](https://github.com/pampa0629/addp/actions/runs/42829768747/job/129876456020) · [Platform 登记与后续失败](https://github.com/pampa0629/addp/actions/runs/42829768747/job/129876456038)

**后继已闭环。** `d12e8279b3dacb87f9fc196aa3f2ddb79598e0dd` 的 [Run 43253234380／Job 131304474365](https://github.com/pampa0629/addp/actions/runs/43253234380/job/131304474365) 于 2026-11-13 实际通过整套 19 项；CDC 审批、basic／deep 串接、正常 Kafka 退出后的正式删除／准确 DLQ 缺失／授权历史读取，以及强退后的恢复／唯一终态／缺退出确认拒删／零删除派发均有回执，外层清理零残留。原阶段“98 项只有本地”、先前失败及旧拒删保留为历史，不能据后继通过改写。完整结果与适用范围见[后继证据](../spec/testing/evidence/Monitor执行监控与留存历史证据.md#2026-11-13-basic-与-deep-串接及-cdc-hosted-闭环)；本轮只对账，不重跑。

**如产品需要硬终止后仍可删除**，另立核清契约。由可信部署控制面提供绑定原主机／运行单元代次与确切进程的退出证明，由 Transfer 绑定 Tenant、task、execution、owner、fencing token 核验，单独保存核清事实。还须证明准确 DLQ 上该 invocation 的在途写入已排空或被原生围栏拒绝，且未遗留未知外部效果；进程退出证据单独不够。匹配全部证据后才允许既有授权入口重新显式提交删除，沿原 task 锁、外部操作登记、未知效果隔离和目标不存在核验。错代际、失联、过期、作用域不符和证据不可用继续阻断。现有 System 实例心跳及 Hosted pidfd 私有观察均不足以直接承担此产品信任边界。生产证据来源、权限及核清操作另议。

## 九、最近执行与同次回跳的真实验收补充（runner 已实施，Linux 待核验，真实未运行）

### 9.1 目标与既有覆盖

正式产品契约沿[执行监控的 Develop 专业详情导航](../spec/tasks/执行监控.md#develop-专业详情导航)：最近执行按同 Tenant／Develop／任务类型／任务定义的 `created_at DESC, id DESC` 选最近受理，包含 pending／running，不能退到旧成功记录；Monitor 展示通用事实，Develop 只展示该次专业结果、日志和冻结输入；返回保持同 UUID 与原 Monitor 筛选／分页。标签位于内容上方，切换和刷新保留本次路由。

已有证据：Develop PG 验证最近受理及旧完成不换记录；`console/frontend/e2e/execution-result-navigation.spec.js` 对 pending／running／success 验证模块 iframe、结果页、日志、刷新、返回及浏览器后退／前进；`307de5e6b` 的 320 项 Linux 浏览器与构建含顶部标签回归。它们均不替代正式 User 的真实服务链路。

现有真实入口是 `make test-monitor-traces-local`，实现为 `monitor-traces-local-gate.py`／`monitor-traces-local-online.py` 与 `execution-trace-local.spec.js`。原业务先等两组编排及 Develop 子执行终结，再打开浏览器，租户场景按 UUID 直接进入 Monitor；没有经过任务行最近执行、Develop 结果页或非终态导航。本方案补已有入口，不增加旁路 Make／workflow，不接管个人部署。

### 9.2 保持的身份、部署与证据边界

- 复用原四身份。业务 User 仍仅原 11 项：`orchestrator.workflow.create/read/delete/execute`、`develop.task.create/read/delete/execute`、`develop.data_read.execute`、`monitor.execution.read`、`system.execution_authorization.create`；由真实密码／MFA／AuthContext 确认本次 Tenant、User 和准确集合，不增配置、平台 Trace／日志或 task.update 权限。准备 User 继续独占合成 Engine 的准备与删除；平台诊断 User 仍只核原阶段追踪，不能代业务 User 走导航。
- 沿原仓库外私有源码副本、独占平台库／`trace_fixture`、产品镜像、Nginx→Gateway→Owner 及 Console iframe。新源码在实施、确定性门禁和提交后才冻结完整 SHA／输入摘要；现在不预填一个已验收版本。原租户／引擎／运行实例归属任一不符即拒绝，不连接个人服务或共享 addp-postgres。
- 原两组成功／失败编排、四个 execution、四个阶段浏览器样本、A1—A7、causal-link 首失败保全及精确检查点不变；不以新增导航样本替换原可读／自然缺样候选。新样本只证明导航，不产生新的 Trace 缺样或父子隔离结论。
- 不改查询并发策略、资源配额、采样、时钟、产品等待期限和清理语义；不暂停／终止 Backend／Worker、不改 execution 私有列、不拦截或伪造正式 API 返回，不通过执行结果缓存造 pending／running。

### 9.3 推荐样本：沿原单 Engine 额度产生真实排队

固定隔离部署的默认 `develop.execution_policy` 为每 Backend Query 总并发20、单 Engine并发5，均为正；参见[执行策略](../spec/configuration/执行与模块策略.md#develop-配置字段与范围)。业务 User 没有 `develop.configuration.read/update`，不得为验证擅自授予或调用该接口。实施前应将默认值来源、单 Develop Backend 及本轮新部署未修改策略纳入源码／生命周期准入；运行中按以下五个实际 running 及目标实际 pending 核验调度，不凭默认数值推导状态。准入不能证明上述隔离条件则停止，不自动读取私表或修改策略。

新增最多 **6 个 Develop query 定义、7 次正式执行提交**，无新增编排定义：5 个占位定义各执行1次，1个导航定义先成功1次再受理1次。连同原阶段样本，整轮最多8个Develop定义、2个编排定义，11个execution（2个父＋9个Develop执行）；不存在第8次导航提交、自动重试或额外候选。全部定义由同一原业务User创建，绑定同一本轮自有 PostgreSQL Engine。

候选合成只读 SQL 固定为 `SELECT 42 AS fixture_value FROM pg_catalog.pg_sleep(20)`；结果最多一行，不读取业务表、不建表、不写业务数据，执行 timeout 仍为原30秒。它是待验证的固定夹具输入：先补当前 Develop preflight／Provider 的确定性契约用例，确认受控准备和冻结路径接受后，未来真实运行仍必须经正式预检／授权。任何拒绝保持首失败，不换SQL、加权限或在真实窗口临时修样本。原 User 执行结束后既有的有界结果保存不是新增业务数据写入。

| 次序 | 准备与真实事实 | 观察目标 |
| --- | --- | --- |
| 1 | 创建导航定义，正式提交一次并等其 success；记录旧 UUID 与实际受理顺序 | 同一任务确有可读的旧成功，后续不能退回它；不更新任务定义或冻结输入 |
| 2 | 创建五个独立占位定义，分别提交并核对准确 UUID 的 running；然后立即第二次提交导航定义 | 由产品调度返回目标 pending，任务行 last_execution_id 必须等于该次202受理的 UUID；只观察，不能靠睡眠时长推断排队 |
| 3 | 在目标仍 pending 的有限窗口走完整导航并回跳 | started_at为空、attempt=0、无有效运行租约／已完成结果；不显示旧成功结果 |
| 4 | 占位查询自然结束，目标被真实 claim；不再提交新执行或改配置 | 同一 UUID 变 running，开始时间由实际领取产生、attempt=1；再走同一导航，仍无旧结果 |
| 5 | 目标自然 success；五个占位执行也须逐一终态核对 | 同一 UUID 的结果恰为一行42，旧成功UUID与当前分开；第三次导航、标签和返回均通过 |

五个占位查询只使用本轮既有额度，全部纳入取消／终态／定义清理与资源采样，不是容量测试。不得改为大量查询“撞中”排队。pending 或 running 在入口到回跳间先变化，记录 `state_window_lost` 并失败，不把终态导航计作非终态通过，也不续窗／补交执行。此方法依赖可观察的有限窗口，不能保证首次一定命中；这种未覆盖必须如实报告。

### 9.4 每个状态的导航与断言

三个状态均由同一真实租户浏览器会话完成，不用平台账号或后台API响应代替页面点击。原平台／阶段浏览器步骤先执行；其失败立即停止，新增样本不用于绕过旧 causal-link。只有前缀通过且剩余预算足够，才创建导航样本。

| 编号 | 必需断言 |
| --- | --- |
| N1 最近受理 | 从 `/develop/tasks` 定位准确导航定义，点击行内“最近执行”。与正式受理回执及当前任务读取事实核对，路由、Monitor详情和Owner事实均为新 UUID，任务定义／类型／Tenant准确，旧success不被选中；顶部模块级执行列表仍是独立入口 |
| N2 同次通用详情 | Console真实iframe中打开该 UUID；pending／running／success来自真实Owner投影，身份、创建／开始时间与attempt语义一致，不制造开始时间或把页面读取当作状态推进 |
| N3 结果页直达 | 点击“查看执行结果”，到 `/develop/executions/{同一UUID}`，`return_to` 保存本次Monitor URL；不回任务管理或任务编辑器。pending／running不出现旧UUID结果；success只有本次一行42，输入与执行日志各自属本次UUID |
| N4 顶部标签与刷新 | 结果／日志／输入三个标签几何位置均在内容上方，切换后UUID不变；在日志或输入标签刷新，URL中的tab与return_to、实际选中项仍一致。真实环境沿原截图／trace／video关闭策略，仅保存安全断言回执 |
| N5 同次回跳 | “返回本次执行”回到原Monitor URL并打开同UUID详情；使用实际合法的 `module=develop`、`task_type=query`、准确source_task_id、page=1／page_size=20作为上下文，分别核参数语义与页面筛选／分页。无需造150条执行来验证第3页；第3页编码／恢复沿已有T3证明 |
| N6 历史与读取边界 | 后退／前进仍在同一执行详情和结果页间；原旧成功仍能独立读取，不能改写其时间／结果或将新结果串给旧UUID。只读取本轮自有定义／执行，不读取其他Tenant或新增撤权身份 |
| N7 零额外命令 | 页面操作只读取／导航，不点击执行、重跑、删除或编辑；新增导航期间trace-stages请求计数不增加，不消耗原四次阶段显式打开。夹具新增正式执行提交恰为7次，未知受理立即停而不补交。pending／running各自入口与回跳处的实际读取仍为目标状态，否则未覆盖 |
| N8 分层清理 | 六个新定义按原User的task.delete逐个正式删除并读回404；所有七个执行先核终态。现有Develop查询没有可复用的取消HTTP入口，本方案不新增接口／权限；失败退出时在原总截止内等产品自身终态，再按正式规则删除。无法确认终态或删除时保留领域清理失败，原外层独占部署销毁另记，不能推断远端已停；会话注销、未知结果与外层容器／卷／目录零残留分别报告 |

范围限这三个状态的真实happy path及结果身份。读取拒绝403／404／503、迟到响应、跨Tenant、父可读子不可读、四主题、目标物理代际等继续引用既有分层证据或独立缺口，不扩本轮身份／故障／验收结论。

### 9.5 时间、调用、安全报告与清理

- 整轮仍最多3600秒、8 CPU／12 GiB、4 GiB停止线；原采样与阶段观测保持5%／慢1秒／尾120秒、125秒自然等待、90秒阶段窗口／至多14次阶段API与4次阶段显式打开。新增导航不消费或重置这组采样候选，也不在新慢查询后重复等待尾采样。
- 浏览器仍为单个240秒测试／300秒容器；导航组在该原窗口内最多120秒，开始前须保留120秒加至少10秒注销／首失败落盘余量，不足则准备失败且不创建额外样本。pending／running各完整导航最多15秒，本组单个正式控制请求最多5秒且服从剩余时间，deadline取全局／组／状态中最早值。原步骤超时不可借本组延长。
- 夹具控制最多6次定义创建、7次执行提交，状态轮询全组至多45次精确GET、间隔至少2秒，三个状态的额外对照读取合计至多12次；所有等待共用上述120秒，不并行扇出轮询。页面自然读取另按原页面逻辑和120秒上限计数留证，不能宣称固定请求总数；超出已确认的阶段请求上限则失败。清理删除／404核验及原会话注销沿原总截止与清理边界，不算业务通过证据。
- 报告只新增固定导航阶段、三种状态命中、UUID／本轮任务绑定、断言布尔值、请求数量与封闭错误类别；原首失败文件仍独占、0600、有界、fsync、不覆盖。不能只保存最终success而丢pending／running首失败；正常缺样不等于错误，新增导航失败也不降格为正常缺样。原始响应、SQL正文／参数、Token、私有路径、Trace技术引用和浏览器抓包不归档。
- 浏览器内新增资源需要供宿主退出清理：在原本轮私有目录增加一个最多4 KiB、0600的 `navigation-ownership.json`，仅含本轮Tenant／User／Engine绑定、六个定义和七次提交的序号／意图摘要／已确认ID／未知标记，以及浏览器与宿主共用的0—45状态读取计数；原子替换并fsync，先落意图再提交，收到准确回执后补已知ID。宿主finally沿此唯一台账回收已知资源；受理后尚未来得及记ID的崩溃仍算未知，不能宣称领域清理通过。该文件不含Token／SQL／原始响应，是已确认的私有清理输出，不是新产品实体／API，不复用或覆盖原首失败文件；完成安全汇总后随原私有目录销毁。受理不明保留原意图与清理缺口，不另造ID重试。最终清理涵盖新增六定义／七执行、原四身份／会话、Engine及全部本轮独占资源，不扩大到个人数据或旧失败目录。旧三轮首失败报告及其哈希保持，零残留不替代业务／API清理回执。

### 9.6 确认后的实施与验收顺序

1. **方案已确认，本轮只实施 runner 与确定性门禁。** 新增六定义／七次提交、20秒只读夹具、120秒导航观察及请求上限、4 KiB私有清理台账均沿本节；原权限、查询timeout、并发策略、资源／整轮／浏览器／采样预算不变。新的真实运行另行确认。
2. 沿现有host／业务／浏览器实现新增阶段、唯一规范的私有导航输入及封闭报告，不保留旧schema兼容路线。补输入拒绝、样本归属、额度前置、旧success不替代、非终态窗口丢失、预算不足、受理不明、首失败与精确清理的确定性回归。先证明固定SQL经过当前preflight／Provider契约，不靠实际窗口试错后放宽。
3. 更新原 `OWN_INPUTS`、输入摘要与在线登记的真实闭包，包含新消费的TaskManagement／ExecutionDetail、共享Monitor导航／return_to、Console iframe／路由、fixture及相关依赖声明；复用原自动选择，不增workflow或复制测试入口。沿 `make test-monitor-traces-local-runner`、`make test-console-frontend`、`make test-develop-frontend` 及实际受影响的登记／共享门禁验证并提交，跟进相关Linux实际执行结果；不将跳过或旧输入通过计为新方案通过。
4. 之后另行提交固定源码、完整输入摘要、身份／样本／调用与预算、资源归属及清理清单供**单次真实运行确认**。本方案不是运行授权；不得自动新开窗口、Hosted、环境切换、重启个人服务或追加下一轮。Graph高级恢复继续暂缓。


### 9.7 本轮实施与证据边界

唯一导航控制器和页面断言已接入原 `execution-trace-local.spec.js`；受控 T3 直接复用同一页面断言。私有台账采用封闭 schema、0600、4 KiB、原子替换与 fsync；提交前记意图，浏览器与宿主共用45次状态读取额度，受理不明或执行未终结不计领域清理成功。保留原首失败，并增加封闭导航错误类别、三状态命中及页面读取计数；不归档 SQL、凭据或原始响应。原身份、7次提交与全部预算保持。

Linux 沿既有 Execution runner 接入确定性入口及固定 SQL 的真实 Provider 预检，不运行真实验收；复用共享登记唯一入口，语法预检仍先执行，Job 仍为10分钟。源码／用例及最终 Linux 结果在本批提交后核验；新的本机隔离真实运行继续单独确认，原 causal-link 首失败不被这些确定性结果替代。


本机专项结果：导航控制／台账 Node 16项、宿主与原 Trace Python 235项、浏览器安全诊断与会话绑定47项、同一导航断言的 T3 六项通过；固定 SQL Provider 预检与 Develop Go T1通过。CI 登记28＋158项、前端登记34项及 Console 构建通过。共享 Make 的重复登记和语法预检顺序问题已修正，相关11项复核通过。首次受控 success 失败为 fixture 的行数组不符合现行行对象契约，已修 fixture，保留42与一行断言；不改产品结果语义。

本机聚合失败另记：Console 首次单元门禁的 devPorts 清理 PID 标记失败，后继285项单元通过；浏览器夹具启动超时的原证据保留，后继六项专项通过。Develop 全量319／320通过，失败为既有 graph-fit-view 缩放断言；Execution 聚合的既有 Cleanup脚本模拟出现20秒超时及输出时序失败，未接管该范围外实现，整项不计通过。首轮日志分别为 `/tmp/addp-navigation-console.log`、`/tmp/addp-navigation-query-browser-serial.log`、`/tmp/addp-navigation-develop-frontend.log`、`/tmp/addp-navigation-execution.log`；新的 Linux 结果按固定 SHA 单独记录，不以本机专项通过替代聚合或真实验收。
