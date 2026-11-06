# ADDP 任务执行管理统一收口专题

更新时间：2026-11-03

**当前状态：公共执行收口、Graph 受控运行与十个 Cleanup Owner 接线已提交；三项 Transfer 原生验收通过。整体仍为部分完成：Graph 恢复证据 Provider、可信原生身份绑定与正向 reopen 尚未实现，用户已决定暂缓；其他原生故障及共享环境切换仍有缺口。**

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

| 优先级 | 剩余项 | 继续条件／交付要求 |
| --- | --- | --- |
| 1 | 后继 Neo4j CI 启动夹具故障 | 九阶段 T2 的42ae源码已获 Linux 实际通过；后继19e源码启动前置 exit64 由 CI 会话跟进，不计专项通过，不自动重跑 |
| 2 | Graph 高级恢复（已暂缓） | 用户决定近期沿用 System 永久登记身份；恢复证据 Provider、可信原生绑定和正向 reopen 暂不实施，不扩大托管／权限边界，缺证据保持阻断 |
| 3 | 其余真实故障验收 | Graph 原生写图回包丢失、两引擎生命周期（正向恢复随第2项暂缓）；Manager 对象／索引未知效果和真正迟到 writer；Cleanup 协调／授权／真实浏览器全矩阵。runner 或 PG 通过不计已完成 T4 |
| 4 | Quality 新实现运行复验 | 历史 bounded-recovery 已通过；是否可复用须按本轮实际输入核对，不能直接升级为全部 Owner 的新实现证据 |
| 5 | 旧环境一次性切换 | 研发旧记录允许清理；准确清单、引用关系、活动运行体及外部未决效果须重新盘点，实际切换与重启仍独立确认 |

共享环境切换顺序保持：刷新只读清单 → 准备默认只读、绑定清单摘要的一次性入口并在标准夹具演练 → 明确窗口和操作范围 → 用户停止旧受理／旧运行体 → 无活动和 unknown 后准确清理 → 用户标准脚本启动 → 核新协议与旧路径退出。历史“9 父＋63 子／11 个 Redis 键”仅为旧盘点快照，不能作为当前删除输入。不创建第三个本地测试 database，不模糊删除、不恢复旧消费者兼容路线。

## 七、确认范围与协作分工

用户已确认由本会话统一牵头，正式契约先提交，按 Common → Graph → Cleanup 实施；已确认细节直接推进。已批准的补充边界包括 Graph 窄 reconcile／原目标 reopen、Cleanup 安全展示、目的限定执行授权、专业结果窄读取、Meta／Manager 独立治理和显式配对、原生三身份／双入口、Engine 授权用途分离、Manager 专属 CI 基线以及 Transfer capture_cleanup 连接用途。

用户于 2026-11-02 明确“后续无需单独确认”，适用于已确认范围内、相关门禁通过后的原生专项继续推进；不扩大为新的权限、概念、模块边界或共享环境切换。遇到新的边界歧义只暂停依赖该决定的工作，其余独立门禁继续。

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

该成功只收口准确42ae的身份反例 T2，不声明可信身份、Provider／绑定／正向 reopen 已实现或重复运行稳定。后继19e05289c的 [Job125628721437](https://github.com/pampa0629/addp/actions/runs/41548048797/job/125628721437) 启动前置 exit64／缺少 initial-password 参数，未进入领域或九阶段测试；由 CI 会话跟进，保持失败事实，不用前序成功覆盖、不在本轮追加运行。

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

`manager-success` 基线已验收，后续分别验证对象／索引未知效果和旧 Runtime 迟到写入。Graph 高级恢复继续暂缓。沿原三业务 User、独立源办理员、精确权限、九项断言及独占 Hosted 部署推进。120分钟总预算、900秒业务预算、保护投影、扫描与 MinIO 就绪共用的180秒窗口不变；完整协议以 [T4 原生 Cleanup 契约](../spec/testing/T4-Online验收规范.md#原生-cleanup-的独立有限验收)为准。

**最新真实结果。** 第八轮 `9f1f68d5` 于 2026-11-06 06:21:43（Asia/Shanghai）成功结束。16 个镜像、Model3D smoke 和 14 个服务准入通过。首扫后实际观察到原生对象和索引投影，Engine 删除后读回终态并确认对象与索引均已清除。九项断言全部通过，含专业回执一致、清理审计保留及源 OBJ／MTL／纹理保持不变。控制恢复、显式清理和零残留均通过。官方五件归档与作业原始日志已交叉核验。[第八轮 Hosted](https://github.com/pampa0629/addp/actions/runs/42109764720/job/127533929017)

| 已落地的收口 | 验证证据及边界 |
| --- | --- |
| 准确源 Grant 前置；Meta 只取结构身份；统一 GatewayClient 保留 ZIP／GLB raw、响应头及 JSON；OBJ／MTL／纹理逐项核验长度和摘要 | `a641b1b6e` [Linux 专项](https://github.com/pampa0629/addp/actions/runs/41677128582/job/126241760694)198＋182项通过。 |
| 既有 `--jobs 1` 实时委托日志、固定阶段及返回码；固定 vcpkg 仅构建产品消费的 Release，保留依赖版本和全部 smoke | `f5bd7b905` [Product build](https://github.com/pampa0629/addp/actions/runs/41758545081/job/126340402757)真实 Linux amd64 Converter／Runtime 及全部 smoke 通过，不外推 arm64 或保证后续冷构建时长。 |
| Manager 私有 `storage_ref` 的唯一四字段 JSON 协议，复用已有 `object_ref`；保留 Manager 桶／本 Tenant 围栏及对象名的字面百分号、空格和 Unicode；删除误用 infra locator 的 URL 路线 | `277c8f6c5` 本地及[同提交 Linux 专项](https://github.com/pampa0629/addp/actions/runs/41815464385/job/126524957842)均200＋189项通过；第七轮已通过真实引用及对象探针。 |
| 模型准备 HTTP 失败只记录闭集操作及白名单错误码；成功报告拒绝该诊断，即使为空或 null；不加请求、重试、身份或等待窗口 | `3eae16d39` 本地及[同提交 Linux 专项](https://github.com/pampa0629/addp/actions/runs/41852005816/job/126650489413)均201＋190项通过、登记一致；Linux selected=true。 |
| 原生库存失败按首个未通过的探针记录闭集原因，成功报告拒绝该字段 | `8b03c91c` 本地及 Linux 专项均 401 项通过，登记一致。[同提交 Linux 专项](https://github.com/pampa0629/addp/actions/runs/42014953009/job/127203046053) |
| 首次 Meta 扫描先等待 Manager 的 Tenant 保护投影。原 User 对本轮模型指纹执行既有 GLB 列表 GET，原 180 秒内只等待明确的保护未就绪响应 | `241ac08e` 本地及同提交 Linux 专项均 403 项通过，登记一致。首扫时序、拒绝及超时边界有反例覆盖。[Linux 专项](https://github.com/pampa0629/addp/actions/runs/42054235504/job/127338131663) |

**历史范围。** [首轮](https://github.com/pampa0629/addp/actions/runs/41586728539/job/125753855807)及[第三轮](https://github.com/pampa0629/addp/actions/runs/41736284282/job/126266219326)构建超时取消；[第二轮](https://github.com/pampa0629/addp/actions/runs/41639055422/job/125927590186)遇到 ZIP 解码错误；[第四轮](https://github.com/pampa0629/addp/actions/runs/41791842460/job/126446790438)已完成 GLB 生成／授权下载，但库存引用解析失败。[第五轮](https://github.com/pampa0629/addp/actions/runs/41833476102/job/126586309842)模型准备返回 403，原调用和错误码未归档，第六轮未复现。[第六轮](https://github.com/pampa0629/addp/actions/runs/41999442501/job/127150683595)库存失败，未记录具体探针。[第七轮](https://github.com/pampa0629/addp/actions/runs/42039158037/job/127285084068)通过引用及对象探针，首个失败点为索引未出现。该轮未归档投递回执，无法确认历史失败的唯一原因。

**剩余工作。** Manager 对象效果未知、索引效果未知及旧 Runtime 迟到写入仍需各自的 Hosted 证据。下一项建议先验证 `manager-object-unknown`，运行须单独确认。第八轮私有证据位于 `/tmp/addp-manager-success-hosted-eighth-20261106/`。
