# ADDP 任务执行管理统一收口专题

更新时间：2026-10-29

状态：用户已于 2026-10-29 确认第七节方案和分工，授权主责先统一并提交正式契约，再依次推进 Common、Graph 和 Cleanup 实施及受影响门禁。新的 Hosted 验收、实际生产／共享环境切换，单独确认；Graph 人工核清权限、原目标重新开放及 Cleanup 安全展示已于同日补充确认。各阶段的实现和验证结果分别记录，不将设计确认计为通过。

## 一、范围与结论

本专题处理任务体系中的执行管理：一次 execution 如何创建、领取、运行、续租、恢复和终结，以及这些事实如何被 Owner、Monitor 和测试消费。它也包含没有持久任务定义的一次性执行与运维 execution，不能把所有 execution 都当作可调度任务。

当前已有统一执行记录和 bounded 租约协议，主要问题是公共字段与状态转换尚未完全由唯一入口强制维护，部分正确性依赖各 Owner 自行补齐。目标是收口公共执行事实及其写入约束，同时保留 Owner 对业务授权、外部副作用、提交和安全重放的裁决。

任务定义、Cron、触发去重和 DAG 依赖属于相邻范围。[调度与任务编排统一改造专题](ADDP调度与任务编排统一改造专题.md)讨论“何时、为什么创建执行”；本专题讨论“创建之后如何可靠管理执行”。两个专题分别确认和实施，本次不引入统一 Trigger，不迁移 owner scheduler。

正式契约继续以以下文档为准；其中标注“已确认目标、待实施”的条款不代表当前代码行为，本专题也不以建议替代正式规范：

- [任务体系规范](../spec/addp任务体系规范.md)、[术语表](../concepts/addp术语表.md)。
- [执行记录与租约](../spec/tasks/执行记录与租约.md)、[模块任务契约](../spec/tasks/模块任务契约.md)。
- [执行架构与存储](../concepts/monitoring/执行架构与存储.md)、[执行监控](../spec/tasks/执行监控.md)。
- [Cleanup 体系规范](../spec/addp-cleanup体系规范.md)、[Transfer 同步执行](../spec/tasks/Transfer同步执行.md)。
- [测试与验收规范](../spec/addp测试与验收规范.md)、[CI 与交付规范](../spec/testing/CI与交付规范.md)。

## 二、现状与证据边界

2026-10-28 完成主要源码审查，2026-10-29 在 `ebdb5b82e721834c69eda8d3c1127580cb667583` 及共享工作区重新核对下列关键路径。此处区分源码事实、风险和实际运行证据，不将调用检索当作完整运行验收。

| 执行类别 | 当前机制 | 本专题需要处理的范围 |
| --- | --- | --- |
| Quality、Security discovery | 共用 bounded claim；按 Owner 条件和 attempt 上限恢复同一未终态 execution | 公共字段完整性、恢复授权、业务提交事务及真实恢复证据 |
| Meta、Transfer bounded、Develop、Manager、Model、Orchestrator、Ontology | 共用 bounded claim；当前过期路径以失败收敛为主，不自动重放外部效果 | 终态字段、时间、所有权校验和 Owner 事务适配 |
| Graph 构建 | 任务与 execution 同事务推进，Backend 内 goroutine 与运行句柄管理 | 持久领取、进程失联收敛和旧运行体写回边界尚需统一 |
| System cleanup 与各 Owner executor | System 协调请求与结果，各模块执行；公共表保存执行历史 | 明确协调状态和执行所有权，替换无租约通用写入；不把整个 cleanup 重写成业务任务 |
| Transfer continuous | 独立 runtime lease、position 和 fencing；恢复创建新 execution | 保留原协议，经明确的 Owner 所有权适配维护公共记录，不套用 bounded 恢复 |
| Monitor | 聚合公共执行事实，并经过 Owner 读取裁决 | 核对状态、时间和事件投影，不能在展示层修补错误持久事实 |

当前检索到 9 个模块使用公共 `ClaimNext`：Quality、Security、Meta、Transfer、Develop、Manager、Model、Orchestrator、Ontology。该数量是本次审查快照，实施时必须重新发现调用方，不维护第二份生产注册清单。

### 2.1 已确认的实现缺口

| 编号 | 源码事实 | 影响与结论 |
| --- | --- | --- |
| E1 | [公共仓储](../../common/execution/repository.go)的 `Update` 使用 `Save`；`UpdateFields` 在上下文缺少 lease 时直接更新；另有通用状态推进入口 | 入口本身不能强制执行完整生命周期和所有权规则。存在入口不等于全部调用方均已越权，须迁移真实消费者后删除 |
| E2 | [lease.go](../../common/execution/lease.go)的普通租约更新禁止改 token、attempt 等，但未禁止改 `started_at`、`created_at` 和耗时；终态耗时由调用方传入 | 公共时间事实可能被覆盖或遗漏，当前正确性依赖调用方自律 |
| E3 | [Security discovery](../../security/backend/internal/service/discovery_service.go)在 `Execute` 进入时另取 `started`，正常和失败耗时从该值计算，恢复失败则从持久 `StartedAt` 计算 | 同类执行已有两个计时起点；源码可确认口径不同，本轮没有测量实际时间差 |
| E4 | [Ontology projection](../../ontology/backend/internal/repository/projection_runtime.go)的成功与失败终态调用没有提供 `execution_time_ms`，Common 终态入口也不生成该字段 | 有开始时间的终态可能不保存规范要求的耗时，不能用 Monitor 的派生显示代替持久事实完整性 |
| E5 | [Graph](../../graph/backend/internal/repository/build_repository.go)和 [System cleanup](../../system/backend/internal/service/cleanup_orchestrator_service.go)仍有独立状态推进；其他模块 cleanup 也消费通用更新 | 不能在未处理这些调用方时直接禁止一切无 lease 写入，或对所有 bounded 记录施加完整租约约束 |
| E6 | [Makefile](../../Makefile)的 `test-platform` 与 `test-online-runner`串行聚合多项独立检查；[Platform CI](../../.github/workflows/platform-ci.yml)以单步骤运行平台总门禁 | 前项失败会使后项没有实际结果，须改善独立调度和汇总，不能把未运行记为失败或通过 |

### 2.2 需要测试确认的风险

公共租约原语使用调用方或进程时间；Ontology 额外使用数据库 `clock_timestamp()`，公共事件入口在取得行锁后复核到期。不同路径对“等待锁期间到期”的防护不完全相同。应以真实 PostgreSQL 并发测试核对，再统一权威时钟与更新边界；当前没有新的运行故障证据，不能宣布已经复现租约穿透。

恢复时 progress、current_step、计数、metadata、授权引用分别属于 execution、attempt 或业务提交事实，尚需逐字段确定归属。不能为了整齐一律清空，也不能默认全部继承。动态子执行授权必须沿现行 attempt 绑定规则清除并重新签发，不能连同根执行授权或身份血缘一起无差别清空。

### 2.3 已取得的固定版本证据

- 修复 `cc293ab89547744a646ae7c9eae531eb7d83b594` 已清空恢复 pending 的 `started_at`。该 SHA 的产品构建、Go、平台一致性及 Common／Quality／Monitor／Security PostgreSQL 共 7 项相关 CI 通过：[Platform](https://github.com/pampa0629/addp/actions/runs/40645503241)、[Release/T2](https://github.com/pampa0629/addp/actions/runs/40645503267)。
- 源码 `1471f8fc80208f7b6d4c1e841cf030fc5d0bfd80` 的 [Quality Hosted T4](https://github.com/pampa0629/addp/actions/runs/40653912002)通过实际崩溃补回、暂停后租约过期与旧 heartbeat 拒绝；两个 execution 重新领取为 attempt 2，时间投影正确，资源与 Infra 清理通过。
- 同一 Hosted 源码的平台总门禁曾被既有 StarRocks 检查阻断，后续 runner CI 未运行。此历史事实只用于说明串行阻断，不在本专题调查或修复 StarRocks，也不否定上述独立 Hosted 结果。
- 详细范围、失败记录和清理证据沿[部署与执行恢复](../spec/testing/scenarios/部署与执行恢复.md#bounded-worker-恢复时间专项)。Quality 结果不替代其他 Owner、所有迟到终态组合、跨主机、生产、HA 或 T5 验收。

## 三、目标职责和生命周期契约

Common 唯一维护公共状态与时间、attempt／租约及公共更新准入；Owner 维护任务和业务结果，决定是否允许重放，并在自己的事务中组合公共原语。System 继续独占执行授权的签发和消费裁决；Monitor 只读取安全投影。不得将 Owner 私有表、恢复资格或外部提交协议移入 Common。

| 转换 | 公共入口的目标保证 | Owner 必须提供的事实 |
| --- | --- | --- |
| 创建 pending | 固定身份、来源、配置；开始／完成／耗时和有效租约为空 | 冻结业务输入、主体与授权前置条件；活动 execution 准入 |
| pending → running | 原子写当前开始时间、attempt + 1、新 token、owner 和到期时间 | 领取范围、真实运行体容量；同事务任务摘要 |
| heartbeat／progress | 核对当前有效所有权；禁止修改身份、开始时间或终态字段 | 当前进度和允许的业务计数／摘要 |
| 过期 → pending | 只允许尚未终态的合法过期 attempt；保持 UUID／created_at，清除本 attempt 的租约与时间 | 安全重放证明、attempt 上限、领域重置和按契约处理授权 |
| running → 终态 | 核对所有权，统一生成完成时间与当前 attempt 耗时，清除租约；拒绝复活 | 业务提交结果、安全错误和需要同事务提交的摘要／事件 |
| pending → 拒绝或其他合法终态 | 明确 pending 条件；不伪造开始时间、运行耗时或领取次数 | 执行前拒绝原因；从未启动的取消例外须按 Graph 已确认目标提供事务证明，不能放宽恢复或历史不明记录 |
| 人工重试 | 新 execution，保留原执行终态和前驱关联 | 当前权限及新的冻结输入；不复用旧授权 |

同一 attempt 的 `started_at` 不可变。终态耗时由已持久化的开始时间计算，不能由 Owner 重新取起点；无开始时间的终态耗时为空。恢复后原创建时间至当前开始时间的差值不等于纯排队耗时之和，不在本次引入 attempt 历史表或累计时长实体。

过程事件沿现有安全字段、预算和保留规范。已接入事件的状态转换须维持同事务；尚未接入的 Owner 需要明确接入范围，不能把所有内部动作强制扩展为新产品事件。Trace、进程心跳和日志均不代替执行所有权。

## 四、建议实现路线

### 4.1 公共入口收口

1. 在现有 `common/execution` 内提供语义明确的创建、领取、进度、续租、恢复、pending 终结和当前 attempt 终结入口。使用明确参数或闭合字段集合，不再接受可任意修改生命周期字段的 map。
2. 统一时间与耗时生成。生产 lease 采用数据库锁后墙钟及严格未到期条件，真实 PostgreSQL 测试验证锁等待和相等边界；测试夹具不向生产 API 注入时钟。
3. 授权附加、异常无租约记录收敛等使用明确前置条件，不把它们塞回万能更新入口。Common 不读取 System 或 Owner 私表来猜授权和业务资格。
4. 公共原语支持 Owner 的外层事务和既有锁顺序；Owner 摘要、结果、必要事件失败时一起回滚。外部提交另按 Provider 协议确认，不将 PG 事务当作外部系统事务。
5. 调用方逐项切换后删除旧 `Save`、无条件状态写入和隐式 lease fallback。采用原接口原位收紧或完整切换调用方的提交，不提交永久兼容包装或两条生产写路线。

### 4.2 Owner 接入顺序

先迁移已使用公共 claim 的 9 个 Owner，保持各自的恢复资格不变。优先修正 Security 和 Ontology 的公共时间字段，再检查其他 Owner 的 pending 失败、过期收敛、摘要事务、授权和专业回执。

Graph 与 Cleanup 的具体建议见下文。建议使用同一公共执行队列，但保留各自领域控制事实；不能把跨系统副作用交给公共租约推断。

Transfer continuous 保持独立 runtime 所有权，公共字段通过明确的受控适配写入。禁止用“没有 bounded lease 就直接写”的方式识别 continuous，也不能在消费者未迁移完时破坏其取消、恢复和 position 提交。

### 4.2.1 Graph：持久入队、受控取消与失联核清

已确认目标的状态转换与锁序以[Graph 构建契约](../spec/tasks/模块任务契约.md#graph-构建任务)为准。本节汇总根因、迁移范围和实施交界，不作为第二份独立状态机。

**当前事实。** [构建仓储](../../graph/backend/internal/repository/build_repository.go)已经在任务行锁下创建唯一 pending execution，并在重跑时重置材料和待审项；但 [BuildService](../../graph/backend/internal/service/build_service.go)随后直接创建 goroutine，以进程内 map 保存取消句柄。开始与终态缺少公共 lease，材料 `Save`、待审项创建和进度更新也未共同核对运行所有权。构建开始时还会读取可变的图谱、本体和空间映射。上述是源码核对结果，不能称为已经复现的线上故障。

[Graph Cleanup](../../graph/backend/internal/service/cleanup_service.go)的引擎路径只检查任务摘要为 running；logical 路径可直接把未完成任务摘要改为 cancelled，physical 路径可删除任务、材料和审核记录。这些操作没有停止构建运行体，也没有同步对应 execution，是本次必须一并封闭的入口。

**推荐路线。** Graph Backend 内嵌有界监督器，复用 Common claim／heartbeat／终态原语；不新增独立 Graph Worker 部署，不把 Graph 业务逻辑放入 Common。API 事务提交即表示入队，进程重启仍可发现尚未领取的 pending；已经开始的执行失联默认失败，不自动恢复为 pending。

| 阶段 | 必须落地的约束 |
| --- | --- |
| 入队 | 当前权限与目标可用性校验后，在 Graph 任务锁下固定 execution UUID、输入和最近执行摘要；重跑重置与新 pending 同事务，存在活动执行或待核清外部操作则拒绝 |
| 输入冻结 | 保存本轮分块／置信度／文档上下文配置、材料有序清单和不可变内容身份、本体实体／关系／继承／唯一键与空间映射、目标 Engine／Graph 身份；凭据不进快照，执行时仍复核当前访问许可 |
| 材料与图目标 | 仅文件路径和数量不足以冻结内容。使用不可变对象版本或内容摘要并在读取时验证，活动执行引用期间禁止删除／覆盖；执行使用固定目标，不逐次从可变图谱定义重新解析去另一个 Engine |
| 领取 | 只有真实可用槽位才能领取；pending → running、开始时间、attempt/token 和任务摘要同事务；拒绝凭进程句柄推断所有权 |
| PG 写入 | 材料进度、待审项生成、统计及任务摘要写入都先核对本 execution 当前有效 lease；原子提交公共状态及相关私有事实，失败一起回滚；不允许先修改材料再发现 lease 已失效 |
| 外部调用 | 每次 Copilot／图引擎／空间图层操作前复核当前所有权和取消请求，传递有界 context；外部请求不放在长 PG 事务内。返回后重新核对 lease，失权则拒绝进度、结果、终态和事件写回 |
| 正常结束 | 保留既有“部分图写入保留、材料失败仍继续其他材料、任一材料失败则整体 failed”的规则；当前 attempt 终态时间统一由 Common 生成 |
| 失联 | 过期收敛 failed，与任务摘要同事务；本次不自动重放图写入，不用 MERGE 推断整个构建幂等；将无法确认的运行／外部效果记录为 Owner 私有待核清事实 |

快照与控制事实建议由 Graph 私有执行记录承载，按 execution UUID 唯一，保存输入摘要、取消请求和停止／待核清证据。Common 只保留定位和安全配置摘要。该记录不是第二个执行状态机，也不是通用 attempt 历史；具体表名在正式契约修订时确定。现有材料和审核项须能识别产生它们的 execution，不能让旧执行修改新执行的材料进度或待审项。

**锁顺序。** 采用 Graph 会话已细化并经用户确认的 E → G → T／B：E 是既有 execution，G 是 Graph 私有 Tenant 级准入协调行，T／B 是任务／材料／审核等业务行；同类多行按 ID 排序。普通动作共享锁 G，关闭准入和最终清理排他锁 G，持久关闭范围仍精确到受影响图谱。新入队 G → T → 插入新 E，不反向锁已有 E；已有执行 claim、进度、终态、取消都按 E → G → T／B。人工审核是独立命令，走 G → T／审核行及本身的操作事实，不要求原构建 execution 仍 running。

Cleanup 分三阶段：先 Cleanup 子 E → G 排他关闭准入并提交；锁外逐项按目标 E → G → T 请求取消和等待核清；最后 Cleanup 子 E → G 排他 → 业务行核验并清理。最终检查其他 execution 使用受 G 保护的普通读取，不持 G 再反锁那些 E。所有影响静止性判定的删除、重绑定、材料变更和审核路径都要遵守 G；取得锁后由 Common 复核租约有效性，不在 Graph 复制算法。外部调用与等待停止均在数据库锁外。

**取消。** 保留一个取消入口，改为持久取消请求，支持请求落到非运行实例的 Backend。请求绑定准确 execution 和观察到的 attempt，幂等登记请求人、原因和时间；运行中响应显式表示“取消请求已受理”，不能显示“已取消”。持有 lease 的监督器发现请求后停止发起新外部操作，等待实际运行体与在途调用收敛，再以当前 lease 写 cancelled。终态竞争由事务判定：先成功结束就保持 success，不追改 cancelled。找不到运行体或无法确认外部请求停止时，保持待确认／失联核清，不靠等待超时制造 cancelled。首次 pending 且 attempt=0、事务内证明从未产生运行体／外部操作时可直接取消，不伪造领取与时长；此为用户已确认、待实施的现行取消契约补充，不能用于历史不完整记录或恢复后的 pending。

**核清与再次操作。** execution 失败与“旧运行体和外部操作已停止”是不同事实。所有可能产生外部效果的操作先在短 PG 事务登记精确身份和目标，再释放锁调用；返回后由有效所有者确认结果。待核清由尚未关闭的操作事实推导，不能靠可随意清除的布尔值。私有控制记录保留原实例、attempt 与外部请求关联。失权旧实例仅可追加绑定原身份的停止确认，不能续租、改终态或补写业务结果；本地函数返回不等于远端提交已确定。仍有不确定操作时，人工重跑、材料删除、审核写图及涉及同一图目标的 Cleanup 均拒绝。解除必须由 Graph 的核清命令验证停止／外部完成证据、记录操作人及依据；不能仅提供一个“我已确认”的布尔开关，不能把清租约、等 TTL 或后台心跳失效当作证据。无法提供可核验证据的 Provider 保持阻塞，另列能力缺口。

**与 Cleanup 的交界。** Graph Owner 先冻结本域新构建／审核写入，再通过同一取消协议停止当前工作；未停止或待核清时返回明确失败／影响信息，保留定义与材料。对 pending、running 及待核清记录均检查，不能只检查任务摘要的 running。只有安全收敛后才能按原有逻辑／物理规则清理。System 不直接取消 Graph execution、不删 Graph 表；引擎删除仍保留用户定义，不把本次改造扩大为删除业务图数据。

**删除旧路径。** 删除 API 直接启动 goroutine、无 lease 的 Start／Finish、运行材料无条件 Save、Cleanup 直接改 cancelled 和绕过运行检查的删除路径。进程内句柄只作为持有当前 lease 的运行体控制工具，不能继续作为领取或跨实例取消的事实源。

### 4.2.2 Cleanup：单一持久队列与有界协调

已确认的公共队列与回执原则以[Cleanup 规范](../spec/addp-cleanup体系规范.md)中的“持久队列与完成回执”为准。本节父子收口、API 和切换细则已随第七节确认，正式契约已提交；代码与各层验证仍须逐项记录实际结果。

**当前事实。** [System 协调服务](../../system/backend/internal/service/cleanup_orchestrator_service.go)先创建 running 父 execution，再写 Redis task、发布 Stream 并启动临时 watcher；读取详情时还会重算并写入状态。各 Owner 的消费、创建 execution、业务处理、公共终态和 Redis result 是分离步骤。[Asset executor](../../asset/backend/internal/service/cleanup_service.go)可作为具体例子：新消息生成新 UUID，创建 execution 失败后仍继续进入业务处理；结果 hash 与终态不在一个事务中。本次不是断言全部 Owner 都有相同故障，实施前须逐个核对。

**推荐唯一技术路线。** 直接以 `common.task_executions` 作为 Cleanup 的持久领取队列。System 保留协调权，各 Owner 保留执行权；不再新增 dispatch／inbox／outbox 队列，也不保留 Redis 作为另一套权威状态。`cleanup.request` Stream、task/result hash、临时 watcher 和 GET 写状态路径整体移除。Engine／Tenant 等领域生命周期事件保留，其受理方转成持久 cleanup 请求；这不把所有生命周期通知都改成 System 同步任务。

| 事实 | 唯一所有者与拟定存储 | 约束 |
| --- | --- | --- |
| 请求、确认和范围 | System 私有 cleanup 请求记录 | 固定 task ID、原始 actor／历史 Tenant、action/mode、cause、based_on_scan、参与模块、确认／影响摘要、绝对 deadline 与父 execution；没有模块私有路径和凭据 |
| 执行所有权与时间 | Common execution | System 事务创建父 pending 及每个参与模块一个 pending 子 execution；子 module=Owner、type=cleanup_executor、source=system；只有该 Owner 领取和推进 |
| 模块完成回执 | 子 execution 的 `metadata.cleanup_receipt`，由执行 Owner 写入 | 与子 execution 唯一绑定；保存闭合、有界结果状态、安全标准摘要、impact digest 与完成身份，不存原始专业数据、token、SQL、对象路径或任意详情 |
| 专业结果与外部操作事实 | 各 Owner 私有存储 | 使用已有领域结果承载；没有持久承载且确需保留的 Owner 才补本域记录。System 不跨读私表，Common 不解释物理删除规则 |
| 父协调结论与审计 | System 请求、父 execution 和 System 审计 | 同一事务提交终态和安全汇总；GET 只读。父终态冻结，不随之后到达的结果改写 |

回执复用现有子 execution 存储，不新增共享回执表或 `common/cleanup` 持久化包。使用独立稳定字段和明确 schema，不复用泛化 `result`；Common 提供字段、数量、字符串和整体尺寸校验及受控事务写入。Owner 写入、System 类型化读取都核对 Tenant、module、父子 UUID、action 和 attempt；未知版本、非法内容或绑定不符一律拒绝，不能退回原始 map。当前安全 summary／impact 含义可复用，但含任意 statistics/details/errors 的原始 result 不能直接作为回执。终态后回执不可新增或改写，普通 progress／metadata 更新不能改写该区域；Monitor 通用 metadata 白名单不因此自动扩大。这样可与 Owner PG 结果和子终态原子提交，又不把专业结果变成公共日志或另一套状态表。

**受理与去重。** System 在一次 PG 事务中完成权限／确认复核、冻结请求、父子 pending、创建审计。每个 `(parent_execution_id, owner_module)` 最多一个子记录，唯一约束只作用于 cleanup_executor，数据库兜住多实例竞争；未创建或未领取成功一律不得执行。生命周期重复请求沿来源持久操作 ID 去重，HTTP 同参恢复使用首次提交前生成并保存的请求 ID，绑定经认证的 Tenant／actor 与规范摘要；相同 ID 不同内容拒绝，新的人工动作必须用新 ID。请求 ID 不是授权凭据，返回原结果也须经过当前读取裁决。不能拿“Tenant 相同、参数一样”去合并两次合法 scan，也不能在超时后静默新建 execute。该请求 ID 必须进入创建 API 和前端恢复流程，同次删除旧自动重发路径。

System 预建 pending 子记录仅表示一项已授权、待 Owner 受理的中性工作要求，不表示 Owner 已开始，更不等于数据访问授权。创建权与运行权分开：System 不为子记录写 started_at／attempt／租约，不伪造 Owner 结果；Owner 领取前校验 action/mode、上下文、父请求绑定、参与身份和当前允许消费的授权事实。不满足时留下无开始时间的执行前失败。System 不得把“有一条子记录”作为绕过 Owner 数据权限的凭据。Cleanup 没有可编排任务定义，task ID 不写入 `source_task_id`，避免改变现有“有定义历史／本人一次性执行”的读取分界；事件来源不得伪造人类身份。

**协调运行。** System 内嵌监督器领取父 execution 后只做结果汇总和截止判断，可在安全、未终态且未超过原 deadline／attempt 上限时恢复同一父 execution。恢复只重读持久事实，不重新建子、不发布重复删除命令、不刷新原 deadline。达到上限或原截止则留下明确失败／超时结论；启动恢复由持久扫描完成，不依赖原 HTTP 请求和 watcher 仍活着。父 attempt 的运行时间按 Common 当前 attempt 口径，端到端协调窗口单独使用请求的创建时间和 deadline，不能复用 started_at 混算。

**子执行准入。** 复用各 Owner Backend 内嵌监督器，独立有界 cleanup 槽位；业务 Worker 与 Cleanup 不混用无限并发。领取采用父 execution → 本模块子 execution 的锁序，锁后复核父未终态、原 deadline 未过、请求完整。System 以父锁串行化收口与领取，只关闭准入，不推进子 execution；Owner 启动及正常监督循环先发现已关闭协调下的本模块 pending，走明确拒绝入口收敛为 failed，不 claim、不增加 attempt，started_at 仍为空，原因是“协调已关闭、执行未开始”，不是删除失败。Owner 离线期间子可暂存 pending，但父详情必须显示“协调已关闭、该模块未开始”，不把它表达为仍会启动的工作。Monitor 若需同样标签须另有 Cleanup 安全投影，不跨读 System 私表、不从所有父终态泛化推断。子业务和终态路径不反向取得父锁，仍核验当前授权及领域条件，不能把自身 lease 当作全部许可；System 不能在父锁内调用 Owner HTTP 或读取私表。以此解决“协调已经结束，离线 Owner 恢复后才开始删除”的竞态。

**结果与失败。** Owner 业务中的 PG 变更必须在每个提交边界检查自己的 lease；可原子完成的私有最终结果、安全回执、子 execution 终态及已接入的过程事件在同一 PG 事务提交。回执不可变，绑定请求、父子 UUID、模块、action 与 attempt；相同已提交回执只可按完整身份和摘要幂等读回，冲突内容拒绝，不能绕过 lease 重新生成结果。scan 的预计数量／空间与 execute 的实际数量／空间分开，不能相加冒充实际释放。外部资源删除无法与 PG 事务合一，必须复用 Owner 产物登记／操作凭据，记录已提交、未提交或待核清事实；不能把结果落库失败解释为“删除未发生”。执行开始后默认不自动重放，scan 也先按这一保守规则运行，后续仅在明确安全证明和单独确认下开放恢复。过期收敛不能伪造模块完成回执；只有真正确认的结果进入释放空间和删除数量。

| 情况 | 父协调结论 | 子 execution／回执与下一步 |
| --- | --- | --- |
| 原截止前收到所有合法结果 | 按现行 completed／completed_with_errors 规则终结；严格删除另按全 success 判定 | 保存各 Owner 真实结果；partial_success、skipped 不能自动当作严格删除成功 |
| 原截止到达仍缺结果 | timeout，明确列出未收到结果的模块 | 不伪造 Owner timeout 回执；关闭未领取子的准入，由 Owner 拒绝收敛；已运行子保留其真实状态 |
| 父已超时，子仍有有效 lease 后完成 | 父终态及原汇总不变 | 允许子保存真实终态和回执，详情明确属于协调截止后完成；不据此自动把 Engine 变为 deleted |
| 子 lease 已过期、token 不符或已经终态 | 不能影响已冻结父结论 | 拒绝旧运行体的结果／事件／终态，Owner 走独立待核清；不得回写“迟到成功”覆盖 failed |
| 父进程重启但请求仍在窗口内 | 原请求恢复汇总 | 同一父 UUID、同一子集合，父可增加 attempt；已经开始的子不重新领取 |
| 外部删除结果不确定 | 保持失败／超时及核清缺口 | Owner 证明最终效果后，通过经授权的新操作处理剩余项，不重用原终态、不自动重试物理删除 |

父详情中的“当前子事实”与“截止时协调结论”分开表达；正常查询不能触发状态推进。Monitor 保持公共状态、时间、父子关系和经过允许的安全诊断投影，不自动开放 cleanup 原始 metadata。父可读不授予子读取权，System cleanup.read 也不自动授予 Owner 专业详情读取权。System 页面需要的标准结果直接来自回执；推荐将现有任意 `statistics/details/errors` 透传改为标准安全摘要与 Owner 专业详情入口，详情由 Owner 当前授权裁决。该变化须在用户确认后同次迁移全部消费方，不暗中删除现有功能，也不在 Monitor 增加透传补回。

过程事件仍是闭合 kind／step、有限计数的诊断事实，不承载专业结果、影响摘要 digest 或原始错误。现行 30 天事件保留、每 execution＋attempt 每 UTC 日 1000 条预算、API 单页至多 100 条分别保持；不把完成回执当作事件预算的一部分，也不因父可读放宽子事件读取。

**保持的业务边界。** 普通 scan 的参与集合与 Engine 删除必须完整参与集合仍按现有能力注册规则冻结，离线不等于无需执行；空集合拒绝。execute 必须基于可执行 scan、原确认和影响摘要，Engine deleting 继续权威复扫、全模块严格成功才可成墓碑。重启续办只恢复原任务身份，不把失败／partial／timeout 重解释为删除许可；失败重试入口当前尚未实现，本次若要提供须显式纳入后续确认范围。Tenant 删除后的历史 tenant_id 仍用于资源归属和审计，但不复活已删除 Membership、不用普通用户已失效 Token 访问数据；必要的生命周期执行权限由 System 沿既有目的限定契约裁决，缺少明确授权能力的 Owner 阻断并报告。

**保留与切换。** PG 请求详情替代 Redis 一小时临时详情，任务历史改为持久事实的分页读取；这是对外契约变化。首期不新增自动物理删除策略，现有执行概览、过程事件和审计保留规则分别沿用；请求／回执／私有结果的引用释放须单独登记，不能顺便启用 180 天概览删除。切换前只读盘点旧 Redis 活动任务、公共无租约执行与外部未决操作；停止受理并安全排空后一次切换，仍未核清则阻断切换。旧记录仅保留可证实的历史，不能反推已丢失的私有结果，也不能同时运行旧消费者与新监督器。

**正式契约修订。** Graph 会话按用户授权先修订术语表、Cleanup 规范、模块任务契约和执行租约规范中的已确认目标，再由本会话核对整体：Owner 创建子记录改为 System 预建意图／Owner 独占领取；类型化 metadata 回执与专业结果分开；Redis TTL 详情改为 PG 事实；取消受理、从未启动的撤销和真实运行终态分开；父协调超时与子运行结果分开。公开 API 变动在实施时同次更新调用方、双语 Swagger 及标准 API 门禁，不保留双格式响应。

### 4.3 存储约束与存量

运行模式明确、写入方迁移后，才能增加适用的状态／时间／租约组合约束。跨字段 CHECK 只防止非法记录组合，不能替代 attempt/token 的并发条件更新。

先只读分类存量异常，说明来源和可修正依据；有证据的记录才执行经确认的修正，没有证据不补造开始时间、attempt、授权或结果。迁移必须通过现有 Common 存储入口及真实 PostgreSQL 门禁，不能长期保留两套有效规则。

### 4.4 CI 与验收组织

平台静态登记、共享 runner 契约和 Owner runner 应独立产生结果，继续调用唯一 Make／脚本入口。独立 Job／矩阵项失败不取消其他独立项；汇总必须拒绝必需项失败、取消和意外未运行。环境、安全或必需前置失败时依赖项不得继续，明确记录原因。

本地与 CI 复用同一影响选择。Common 变更仍扩散真实消费者，不主观缩小门禁；共享本地 PostgreSQL T2 保持串行和规定测试库。只调整门禁组织，不顺手修复无关领域失败。

Hosted 只抽取已重复的受控进程身份核验、暂停恢复、清理和安全报告能力，业务故障条件留在 Owner。复用现有 suite，不新建通用故障平台。新的真实验收另行确定源码、身份、场景、预算和清理范围；上一次“一次 Hosted”授权已经使用，不作为无限重跑许可。

## 五、实施阶段与验收

| 阶段 | 交付物与依赖 | 最小充分验证 |
| --- | --- | --- |
| A 契约确认 | 状态转换与字段归属表；Graph／Cleanup 设计决定；正式规范先修订 | 文档链接、调用方清单和规范一致性审查；不计产品验证 |
| B 门禁隔离 | 在已确认组织方案下拆出独立结果，保持原覆盖和总失败传播 | 标准入口／选择／登记反例、独立失败仍收集结果、前置失败阻断、必需项遗漏拒绝；固定 SHA 自然 CI |
| C 公共原语及现有 Owner | 收紧写入，统一时间与终态，适配 9 个 Owner、continuous 调用和已有事件／trace | `make test-changed`；共享 Go `make test-go`；Common、受影响 Owner 与 Monitor 的已有 T2 标准门禁 |
| D 独立运行路径 | Graph 和 Cleanup 按已确认契约闭环接入；无主遗留记录分类与约束 | Owner 事务和幂等准入、重复事件、失联、取消、迟到写回；数据库迁移与失败传播；未有标准门禁时同次补登记 |
| E 真实故障验收 | 按独立 Worker、内嵌监督器、协调执行及外部副作用差异选择场景 | 相关 `test-*-online-runner` 先通过，再运行经批准的现有 Hosted suite；核固定 SHA、普通身份、断言、归档及零残留 |

Common 确定性矩阵至少覆盖竞争领取、到期前后与锁等待、恢复 pending、再次 claim、上限失败、旧 heartbeat／进度／终态／事件拒绝、公共字段不可改写、终态不可复活，以及 Owner 事务失败全部回滚。Owner 增加自身重放、授权、结果和副作用断言，不重复实现公共算法。

涉及的现有 PostgreSQL 入口包括 `test-common-postgres`、`test-quality-postgres`、`test-security-postgres`、`test-meta-postgres`、`test-transfer-postgres`、`test-develop-postgres`、`test-manager-postgres`、`test-model-postgres`、`test-orchestrator-postgres`、`test-ontology-postgres`、`test-monitor-postgres`；具体按实施影响与当前注册选择。Ontology 投影激活与图联动还须覆盖 `test-ontology-falkor`，不能只验 PG 修订。Graph 的具体既有入口和 Cleanup 需要新增的入口见下表；不能沿用旧 Go 通过记录冒充新协议 T2。

阶段 B 可在接口实现之前独立推进，但共享 Make／workflow 变更由单一负责人整合。C、D 的提交需维持每个已迁移调用方只有一条主路径；整体接口收口必须等待全部调用方闭环。阶段 E 不为“全覆盖”重复仍有效的场景；输入发生变化时才按影响复验，并逐 Owner 记录尚未验证项。

### 5.1 Graph／Cleanup 的实施拆分与门禁

顺序为：确认本专题 → 正式契约 → Common 受控原语和完整 PG 矩阵 → Graph 构建／取消／Cleanup 交界 → Cleanup 协调和全部现有 executor → 删除旧通用入口／存储约束 → 经另行批准的真实故障验收。已经迁移部分 Owner 时，不提前删除剩余真实调用方；也不增加临时兼容生产分支。

| 范围 | 现有入口与必须补齐内容 |
| --- | --- |
| Common PG | `make test-common-postgres`；补真实 PG 下的竞争、锁等待期间到期、当前时间、事务回滚及旧写拒绝。当前 runner 的 execution PG 名称为闭合选择，新增测试必须检查实际被选中，不能假定 `*AgainstPostgres` 自动发现 |
| Graph PG | 复用 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"`；当前运行 `internal/repository` 的 `TestIntegrationPostgresGraph*`。监督器测试若在 service 包，须同次扩展 runner；新增冻结输入、精确领取、任务／材料／待审项事务、取消竞争、失联核清和 Cleanup 删除准入 |
| Graph 原生图 | 复用 `make test-common-falkordb` 和 `make test-common-neo4j`；核外部提交已发生但平台失权、取消仍有在途调用、失败保留部分图写入、旧实例不能再发新操作。PG 通过不替代这层 |
| System Cleanup PG | 新增 System 所属标准 T2，建议名 `test-system-cleanup`（尚不存在）；同入口覆盖请求／审计原子受理、唯一父子、重复请求、协调重启、父关闭与子领取竞争、迟到真实结果及旧子拒绝。`test-system-iam-postgres` 默认不执行 cleanup service，不能直接冒充该覆盖 |
| 各 Cleanup executor | 逐 Owner 覆盖“未创建／未领取禁止执行、每个 PG 提交围栏、结果回执与终态原子提交、外部副作用不确定时不重放”；复用其标准模块／T2 入口。Asset、Develop、Graph、Manager、Meta、Model、Quality、Service、Standard、Transfer 为本次检索到的消费者，实施时重新发现；不凭模块名推断其全部物理清理已验收 |
| API／前端 | 取消仅受理、真实终态刷新、协调超时与迟到结果区分、PG 历史分页、私有详情转 Owner、跨租户／父子读取隔离；同步 Swagger、路由覆盖、模块前端与受影响 Console T3 |
| CI 登记 | 新入口同次接入根 Make、串行 `test-integration`、服务依赖元数据、影响选择、Release/T2 Job、必需汇总和漏登记反例；Graph 后端现已被 Common PG／FalkorDB／Neo4j 路径声明命中。`test-module MODULE=graph` 不替代 Common 所属 T2 |
| T4 | Graph 扩展既有 `graph-protection` 的独立执行生命周期 case；Cleanup 需先新增正式 suite、普通身份夹具、固定范围和清理断言。首批建议只做 scope=[meta] scan 的重启／失联／迟到／重复受理，物理删除必须另定自有产物及操作计数场景，不能从 scan 结果外推 |

新 Cleanup 协调路线的必需存储是 PG。Redis 仅在仍实际依赖它的 Owner 或生命周期来源事件场景使用，不为了覆盖已删除的旧 Stream 路线再增加并行协议。Owner 的原生数据库、对象存储等依赖按实际行为声明，不能用模拟计数证明真实物理删除。

本轮仅方案和源码复核，以上产品测试、CI、T4 均未运行。文档交付仅检查链接／锚点、入口是否存在及 `git diff --check`；未来测试名称明确标注“拟新增”，不计为已覆盖。

本轮整合检查已通过：本专题 27 个本地链接／锚点和 21 个既有 Make 目标有效；Graph 会话的六份正式文档草稿新增 31 处链接／锚点有效，相关文档差异检查通过。正式草稿涵盖术语表、公共租约、模块任务契约、Cleanup 规范、Owner 补充门禁和 Graph 模块说明，其中最小矩阵列出 16 项场景；这些是文档验证，不是产品门禁结果。

## 六、会话协作建议

采用“一个主责会话统一设计与整合，少量领域会话核对或承担独立文件范围”的方式。用户已于 2026-10-29 确认本会话牵头并授权与相关会话核对；用户随后确认第七节方案和分工，代码实施现已授权；以下按实际文件边界和 Common → Graph → Cleanup 顺序执行，新的 Hosted 不在该授权内。

| 会话原名与 threadId | 已核对的工作范围 | 建议职责 |
| --- | --- | --- |
| 修复 RetryExpired 的 attempt 开始时间清理；`01a1811b-d160-77c0-817c-286daeb9fca8` | 本次公共时间修复、Quality 恢复验收和整体审查 | 主责：规范、Common、调用方清单、核心迁移和最终整合；`common/execution/**` 只由本会话修改 |
| CI修复-持续进行；`01a115b5-90bc-7d11-9a68-ffd2af505611` | 已完成本轮入口、路径选择和 CI 缺口核对，确认职责无冲突 | 后续统一整合 Make／workflow／影响选择／平台登记与入口包装；本会话提供公共库、Owner 用例和业务夹具，不并改共享编排 |
| p145-security；`01a10003-a16c-76c3-b047-237c2317425d` | 最近完成保护算法展示与入口修正 | 核对 discovery 重放、Finding／保护投影事务与授权边界；公共接口稳定后可承担 Security 私有适配，不能改公共租约 |
| 租户日志改造；`01a0f7e1-50f2-7063-9df2-dd918416b75a` | 已完成本轮父子状态、完成回执、事件／权限／保留复核 | 继续只读复核 Monitor／诊断和读取边界；公共事件代码、状态转换及本轮专题由主责整合 |
| p19-接口质量与调用链；`01a1585e-ae3c-7e60-a4b6-4ecaadb2bc17` | HTTP 质量、调用链及采样证据 | 按需复核 execution 短阶段 trace 的事务提交语义，不承担本次生命周期主改造 |
| ontology模块-系统本体；`01a04725-c1d4-7261-b90c-f55c3849ec63` | Ontology 投影、PG 发布与 FalkorDB 联动 | 按需复核投影锁顺序、激活指针和授权／租约双重条件，避免公共抽象破坏既有提交协议 |
| 全面支持falkordb；`01a1476a-4a03-7fa2-99f2-cca6aa13a857` | 已完成 Graph 全路径审查、状态／锁序表及六份正式契约草稿，草稿尚未提交；用户另已确认五项原则 | 本轮维护 Graph／Cleanup 正式契约与最小验证矩阵，本会话只维护总专题；后续可承担 Graph 私有实现，仍须等整体实施确认，不改 Common／System／Ontology |
| 文档清理；`01a148c8-5547-7810-b108-bbfbcd185654` | 当前正整理 Catalog；已请求只读 Cleanup 概念复核，尚待回复 | 仅做规范一致性核对，不作为 Cleanup 产品 Owner，不重复编辑本专题或 Graph 会话正在修订的正式文档 |

`p12-服务与主机监控` 正在处理主机／服务页面交互，执行实例观测不等于任务所有权，本批不要求其参与核心修改。栅格、拓扑、格式、数据权限等会话按实际受影响路径协作，不因共用 Common 就全部派发产品改造。`p0-hunan功能清单维护` 只接收实际实现和验证结果，方案确认不能记成功能完成。

不按“一个模块一个会话”拆散公共状态机。本轮实际请 Graph 历史负责会话、租户日志、CI 和文档清理核对，没有向 Security、Ontology、p19、p12 或 p0 派发实施。没有清晰活跃负责会话的 Cleanup 协调与各 Owner 适配由主责承接；不能把“文档清理”当作 Cleanup 产品 Owner，也不能因其他会话标题含 System 就指定其承担该重构。

共享工作区内，派发前必须再次读取最新进展，明确文件、接口版本、验收和合入顺序；已在处理的不重复派发。各会话只提交自有改动，实施前再次核对未经提交的同文件修改。消息、并行实施和运行验收分别以用户确认的范围为准。

## 七、确认状态与实施前决定

已确认且不重复征求的内容：本会话统一牵头；公共 PG 单一队列，System 预建 Cleanup 父子 pending、Owner 独占子运行；Graph 取消意图与终态分离、从未启动 pending 的有证据撤销；Graph 私有准入／待核清与 E → G → T／B 锁序及三阶段清理；子 execution 类型化 metadata 回执、Graph 执行快照不扩大 Ontology 迁移。用户已进一步明确授权正式契约提交和按顺序实施；该授权不包含新的 Hosted 或实际环境切换。

以下具体产品与操作边界已经用户确认，作为本轮实施范围：

1. 按 5.1 的顺序实施，由本会话负责 Common、System 协调、其余 Owner 适配及整合；Graph文档会话已交接既有WIP及已确认细则，后续Graph私有路径由本牵头会话接管；CI会话单一负责标准入口和登记，日志会话复核权限／事件。
2. Cleanup 采用 4.2.2 的父子收口细则：父冻结截止结论；有效子迟到可以记录事实，失权子写回拒绝；父只关闭准入，未领取子由 Owner 拒绝收敛。父协调恢复不刷新期限，不重放子删除。
3. 对外接口同时调整：取消受理与完成分开；Cleanup 同参受理 ID、PG 持久详情／分页、标准安全摘要与 Owner 专业详情分开。Graph人工核清／重新开放的精确API、Permission和证据边界已按下节确认，不授予无条件解锁权。既有调用方同次迁移，不能只改后端或悄悄删私有统计显示。
4. 首期不新增自动物理保留清理、失败 Engine 删除重试或 Ontology 模型迁移。旧活动任务／外部未决操作只读盘点后排空切换，有无法核清项就保留阻断，不凭新协议改造授权修正历史或重跑删除。实际生产／共享环境切换命令和新的 Hosted 范围另行确认。

Common 统一采用 PostgreSQL 锁后墙钟、严格未到期判定及封闭字段写入；细节在正式规范与 PG 反例矩阵中收口。若调查发现需要改变上述业务边界，先报告决定，不用 Owner 私有补丁绕过公共约束。已有确定性或 Hosted 成功证据只有输入与范围仍匹配时复用。

### 实施中补充确认的两项边界

用户于 2026-10-29 补充确认：在现有 Graph 主资源 version 规则下新增 `POST /api/v1/graph/graphs/{id}/reconcile`，只接收 version，由服务端逐项核验持久未决操作证据；新增窄 Permission `graph.graph.reconcile`，不默认授予角色。新增 `POST /api/v1/graph/graphs/{id}/reopen`，要求 `graph.graph.update` 与 `graph.graph.reconcile`，只允许已核清、无活动执行且生命周期合法的原物理目标重新开放；不自动重跑、不复活旧 Cleanup，也不允许 force／人工确认布尔值／任意 SQL 或上传日志放行。换 Engine／CatalogPath 的重绑定不纳入首期重新开放。

有效证据限于可信持久事实证明尚未派发、原合法所有者已完整持久化结果，或绑定原操作／attempt／目标且能证明不会继续写入的可信 Provider／运行体终结证明。现有通用图 Provider 与 Copilot 同步抽取均未提供完整持久操作核验协议，因此首期不能保证全部失联场景可核清；缺证据必须保留阻断。

上述新增 Permission 与原目标开放范围，以及[Cleanup 安全展示契约](../spec/addp-cleanup体系规范.md#协调关闭与模块未开始的安全展示)，均已获用户确认并纳入本轮实施；换目标另议，新的 Hosted 验收仍单独确认。

完成时必须同时满足：运行路径有明确所有权；公共字段有唯一维护入口；旧路径删除；Owner 事务／副作用边界有证据；受影响标准门禁实际执行；真实验收按固定版本与范围通过，清理可核验。未运行、失败、跳过和未确认设计分别记录。

已实施的稳定规则迁入现有 concepts／spec 与模块文档；本专题只保留剩余决定和缺口，全部关闭后删除，不再积累逐轮运行流水。

### Cleanup实施中新发现的执行授权缺口（已确认）

只读核对确认：现有HTTP入口核验当前User的`system.cleanup.execute`，但Owner消费原Redis请求时没有排队后的原用户权限复核。既有任务授权主体只支持Orchestrator／Transfer；execution authorization的audience及派生来源也没有Cleanup。Tenant Service Token要求Tenant active，IAM的closed Tenant不能按自报`tenant.deleted`上下文取得历史租户清理权。父子持久记录、lease和读取Owner scope均不能替代这项权限。

用户已确认以下首期窄方案；先同步正式IAM契约，再实施：

1. 复用System现有执行授权框架，新增明确的Cleanup目的绑定，不复用数据读写或Ontology内部任务授权。受理时绑定当前User／Tenant／Membership、请求与确认摘要、准确父子UUID、Owner、action／mode和原范围；不保存User Bearer，普通用户入口仍只要求现有Cleanup权限。
2. 为已登记Cleanup Owner增加仅可调用Cleanup授权复核的Tenant Service权限`system.cleanup_authorization.execute`，固定客户端与Owner对应关系，只授予这十个Owner的内置runtime角色，不授予用户角色。该Permission仅允许核验自己的准确子执行，不授予任意资源删除或跨Tenant权；服务端复核原用户当前成员资格、授权版本及Cleanup权限、请求未被替换、当前attempt／lease和有效期。实际效果前复核，任何失败不派发删除；临时授权不可用与已撤权分别记录。
3. 首期限定active Tenant的用户来源请求及其已确认Engine删除派生流程。`tenant.deleted`／closed Tenant和没有可信原用户来源的生命周期请求继续失败关闭，不复活Membership、不使用Platform Token跨域执行。历史Tenant需要独立的目的限定治理授权，另议。
4. 新授权只约束既定Owner范围，不扩大可清理资源、不自动重试旧执行、不重跑Hosted。已确认的持久队列、P→C锁序、标准回执及安全读取投影可继续独立完成。

此补充改变执行授权的目的绑定与Service Permission，根AGENTS.md最高优先级第0条要求确认；本项现已单独获得用户确认，历史Tenant仍另议。影响门禁为System IAM授权正反例、Cleanup实际领取／效果前拒绝、Owner固定client/audience/Tenant与撤权反例；由原System IAM及新增System Cleanup标准入口覆盖，不新增旁路授权机制。

### 2026-10-29 公共准入与窄授权实施记录

已实现公共 Cleanup 冻结请求、父子预建、同父同 Owner 唯一约束、P→C 准入／未领取拒绝、类型化完成回执和纯值安全投影。读取 Owner 裁决的 HTTP 实现从 Monitor 提取至 Common，仍采用当前请求内的 User Bearer、五秒总预算、最多四并发、禁止重定向和失败关闭；System 后续复用同一路径。

System 的 Cleanup 目的授权复用既有 execution authorization，签发／附着／审计同事务，固定十个 Owner runtime；领取前和效果前分别复核，角色、成员、授权版本及租约变化均拒绝。数据库迁移同时发布先前 Graph `graph.graph.reconcile` 的权限目录事实，不给任何内置角色默认授予该权限。该阶段只提供公共原语和授权能力，**System 持久协调器、十个 Owner 的新消费路径及 System／Monitor 展示尚未接入，不计为 Cleanup 全链路完成**。

实际通过的本地标准门禁：`make test-go`、`make test-authorization`、`make test-monitor-postgres`、Common `--test execution-lifecycle`，以及 System IAM 的 `cleanup-authorization`、`internal-task-authorization`、`execution-subject-roles` 和 migration 包的 `execution-authorization-lease-boundary`。System 前端另有 179 项单元测试、250 项受控浏览器测试及构建通过。这些分别是 Go、协议／权限、真实 PostgreSQL 与受控前端证据，不是 Hosted 或环境切换证据。

Common CI 在 `single_claim_and_terminal_clock` 的创建时间断言曾失败。确定性复现表明旧测试比较的是纳秒精度 Go 插入输入和微秒精度 PG 值；现固定带亚微秒的夹具，以领取前独立读取的持久化值作为基线，领取与终态仍逐值严格相等，不放宽容差、不修改业务写路径。原失败与新本地通过分别保留，新 SHA 自然 CI 尚需核验。完整 `make test-platform` 的本地重跑仍在栅格 runner 的 Node 版本检查报 `25 != 24`，不计为通过；本阶段没有据此扩展栅格修改。

### Cleanup 专业结果读取出口的补充决定（已确认）

2026-10-29 主责与“租户日志改造”会话完成十个 Owner 的只读核对：现有 `execution-read-scope` 都只裁决公共概览；Asset、Develop、Manager、Meta、Model、Quality、Service、Standard、Transfer 没有可复用的 Cleanup 专业结果接口。Graph 的旧构建执行详情只按 module 过滤，会误包含 Cleanup，属于需要修复的范围缺口，不能视为合法专业出口。System 现有页面会直接展示 Redis 中 `statistics` 与自由文本 `errors` 的原始 JSON；十个生产 executor 当前没有填写独立 `details` 对象。

因此，删除旧 Redis、专业结果归 Owner、System 不跨读私表、保留用户可见专业统计这四项要求，无法仅靠已批准的公共概览协议同时实现。本轮安全展示确认明确没有新设专业端点或扩大权限，不能把它解释为专业读取授权。用户已于 2026-10-29 确认以下唯一路线；接口／权限与页面同步实施，持久受理／协调、PG 门禁及 Graph 读取范围修复继续：

1. 十个 Owner 各自持久保存本模块 Cleanup 专业结果，绑定准确子 execution、Tenant、attempt 和请求 digest，与合法子终态／公共回执同事务提交；数据库效果也在该事务边界内。外部未决操作证据仍属于各自窄控制事实，不被结果接口或成功状态替代。
2. 增加 `GET /api/v1/{owner}/cleanup-executions/{execution_id}/result`，只接受准确 ID，不接受任意表、路径或存储参数。增加各 Owner 的 `<owner>.cleanup_result.read` 窄 Permission，不默认授予任何角色。读取同时要求该 Permission、当前 Tenant User、公共 Cleanup 子读取范围及本人一次性执行归属；不能用 Graph 构建权限或 System 后台身份替代。首期与 Owner 范围协议一致，仅第一方 unrestricted User 可读专业结果；其他合法父入口凭据仍可读取原已授权父事实。
3. System 页面保留标准摘要，并为当前可读子提供按需“模块专业结果”入口，由浏览器经 Gateway 使用当前 User 直接向该 Owner 请求，不由 System 聚合专业正文。Common 前端统一承载请求失效、错误处理和结果容器；每个 Owner 只提供闭合专业字段。切换身份、租户、任务或新请求失败时立即清除旧结果。
4. 保留现有实际使用的模块统计计数；错误改为 Owner 定义的安全稳定类别和计数。删除任意原始 JSON／SQL／对象路径／凭据／供应商错误透传；没有已有实际用途的任意 `details` 字段不迁移为公开扩展口。当前结果是单个有界统计对象，无明细列表，不新增假分页；将来需要逐对象清单时另定授权字段和分页合同。
5. 私有专业结果首期不启用自动物理删除，不套用执行事件 30 天或概览 180 天只读评估期限；旧 Redis 结果不导入为新可信记录。受影响门禁包括十 Owner 的权限／同租户他人／错误 task_type／未知 ID／私有字段泄露反例、结果与终态原子性、System 按需访问及请求代际失效。新的 Hosted 仍单独确认。

新增专业 API、窄 Permission 及可见错误从原文改为安全类别这三项产品／权限边界均已获确认；不默认授予新权限，专业字段迁移与页面同次实施。

用户进一步明确 ADDP 仍处于研发阶段，本次改造涉及的旧执行、Cleanup 协调及旧协议记录允许清理，不要求兼容或迁移为新可信记录。实施时先盘点引用关系、活动运行体及外部未决效果，再按准确范围提供一次性清理入口；删除记录不构成外部运行已停止的证明，不自动重放删除。该授权不扩展为新记录的周期保留清理，也不替代新的 Hosted 或服务重启授权。

### 2026-10-29 接线、验证与剩余范围

当前工作区已接入 System 的 PG 持久协调器、原请求幂等、固定截止快照和 Engine 删除续办；Asset、Develop、Graph、Model、Quality、Service、Standard 共七个 Owner 已切换公共 runner 与私有专业结果。System／Monitor 复用 Common 展示组件，保留真实 pending／failed、父子分别授权、读取一致快照、身份及请求代际失效。十个 Owner 的专业 GET／Permission／Swagger 已接入，不默认授予角色。Manager、Meta、Transfer 的外部效果主路径尚未收口，整组产品接线仍未提交，不能部署半套消费者。

当前输入实际通过的本地门禁：`make test-go`、`make test-swagger test-authorization`；Common `execution-lifecycle` 真实 PG（专业结果／回执／终态原子性）；`make test-system-cleanup`（持久受理、协调及 Engine 工作流）；System IAM `cleanup-authorization`（十个专业 Permission 无角色默认授权、原用户当前 Engine 删除权限及锁等待过期）；Common `graph` 真 PG（原 repository 覆盖及两个 Owner 实例竞争、未领取拒绝、未知外部效果阻断、最终事务回滚）；`make test-monitor-postgres`（同一只读快照与零写入）；`make test-standard-postgres`（现有 PG 与临时 MinIO 门禁通过、门禁自有容器／网络／卷／镜像零残留；不据此宣称历史 Tenant 进入新执行路径）。Standard 移除 Redis 直接使用后首次 `test-go` 的 tidy 检查失败，修正依赖为 indirect 后全 Go 入口通过，未跳过依赖检查。

前端证据分层：Common 298 项单元通过；Monitor 131 项单元、512 项受控浏览器及构建通过，另新增 Cleanup 关闭说明／读取拒绝清除测试通过；System 原全套 179 项单元、250 项受控浏览器及构建通过，随后重跑的 180 项 System 单元及构建通过，新增的五项 Cleanup 浏览器测试通过，覆盖真实 pending 无开始时间、分页、Owner 不可用／非法证据清除、专业结果按需读取／迟到响应失效、同 UUID 重试及身份切换。新增测试首次发现翻译键层级和分页总数提前清零问题，均已修复并重验。以上不是固定 SHA 的 CI、真实 Gateway 或新的 Hosted 证据。

只读盘点本地共享 `addp`：`cleanup`、`cleanup_executor`、`kg_build` 三种类型中查到旧 Cleanup 父记录 9 条、子记录 63 条，均为 success，均没有新协议字段或有效 lease；该次查询未查到 kg_build。生产 schema 尚无新 `system.cleanup_requests`。后续同日只读复核仍为上述 72 条 success；Engine 删除 scan／execute 字段对这批 execution 的引用数为 0，数据库声明的关联外键来自 common.execution_events。Redis 有 11 个 cleanup 相关键（9 hash、1 stream、1 list），request stream 有 9 项，十个 Owner 消费组 pending=0、lag=0。未输出事件正文、令牌或凭据。这些均是盘点时点事实，不证明仍存活的旧消费者不会再受理或所有外部请求已经终止。尚未删除记录、修改共享服务或运行新的 Hosted；旧记录直接清理而不兼容，清理执行须和停止旧受理／运行体、精确引用核验及单次切换相衔接。

### 外部效果剩余方案：已确认机制与索引治理边界

主责与“租户日志改造”会话只读核对确认：Manager 的对象／物化表删除、Meta 的索引删除委托、Transfer 的连续任务停止和 CDC／DLQ 删除，都把外部请求与独立 PG 提交混在旧函数中。外部成功后 PG 失败不能当作“没有删除”，lease 过期也不能当作“旧运行体已停止”。旧记录可清理不改变这些新执行的正确性要求。

**按既有授权继续实施的机制：**

- Manager：复用产物的准确 ID、Tenant、Task、原 execution 和 StorageRef／物理表目标，补本域操作事实并在短事务内登记；锁外调用，再凭当前 lease 提交结果。GLB 生成回收的 pending／garbage 状态不充当 Cleanup 完成凭据；手动删除、重新生成和后台回收必须遵守同一目标的未决操作围栏，不能另起一次删除绕过未知结果。
- Transfer：复用 CaptureResource 的 generation、source fingerprint、Connector／Topic／Group 和 ResourceVersion，精确记录每个删除操作；实际停止后才释放任务私有状态。新增绑定原 runtime instance／generation 的停止确认，由运行体退出并等待在途操作结束后持久提交；没有确认的失联状态保留阻断，不能仅由 lease TTL 放行。Kafka DeleteTopics／DeleteGroups 缺少准确目标响应的漏洞已修复并通过 Go 测试；明确 ACK 仍不等于生产者已停或物理删除已最终完成。
- 各 Owner：外部调用不持有 PG 锁；效果前复核原用户授权；结果未知不自动重发；失权旧实例至多记录绑定原操作的可信结束事实，不能修改 execution 终态或业务结果。纯 PG 改动和私有结果／回执／终态保持原子提交。专业 GET 不承担核清或状态推进。

**Meta／Manager 范围调整（用户已确认，实施中）：**

原 Meta 的 execute 在本域 PG 删除后调用 Manager 的内容索引删除接口；该隐式调用现已从工作区删除，Meta Go 门禁通过。接口只有完成响应，没有可供 Cleanup 绑定的 operation ID；Manager 后台投递也未绑定 Meta 请求的原用户和子 lease。Manager 已有 `content_index_deliveries` 的 endpoint／epoch、correlation、TaskUID 和 unknown 状态，但不能凭现有通用机器权限获得另一 Owner 的 Cleanup 执行权。

已确认删除 Cleanup 内 Meta→Manager 的隐式效果委托：Meta 只处理本域 PG；索引清理由 Manager 自己的 C 负责，使用 Manager runtime 的既有窄授权。复用 Manager 投递事实并增加 C／attempt／精确目标绑定，Cleanup 来源禁止自动创建新操作重试；以可信原 TaskUID 和目标证据核验结果。Manager 独立治理“自己索引中失效 Engine 的投影”，从本域索引目标及 System 权威 Engine 状态确定范围，不跨读 Meta 私表，不复制 Meta 内部规则。这是明确的 Owner 治理语义调整，**不是原 Meta 候选集合的等价搬迁**：旧 Meta 以本域节点／ScanTask 的 Engine 并集为来源；Manager 会覆盖索引中仍残留、但 Meta 已无节点／任务的投影，也不会伪造一次针对空索引的已删除统计。

相关会话已完成只读复核，补充了以下实施约束：

- Manager 当前的持久 delivery 只保留 DocumentID、过滤条件和任务凭据，不能直接作为完整 Engine 索引账本；已有普通搜索／单文档读取也不保证全量枚举。必须新增有界、完整性可判定的本 Tenant 索引身份发现，仅取 ID／Tenant／Engine 等必要字段；分页截断、代次漂移、服务不可用都失败关闭，不能用第一页、估计命中数或默认 facet 上限声称完整。
- scan 仍只读，必须明确给出索引候选事实与安全计数；execute 在效果前核对同一确认范围及出口代次。已存在 submitting／unknown 写入可能在枚举之后落入索引，必须等待原操作的可信结束证据或保留阻断；保留现有“有未决写入则不派发 DELETE”的约束，不能用空枚举或 C 终结制造安全证明。目标范围变化继续拒绝，不能自动扩大删除。
- System 的默认 Engine 列表只返回 active，列表缺失不等于已删除。必须通过既有合法读取获取准确生命周期；401／403／超时均不是不存在证明。Engine 删除流程已有精确 Tenant＋Engine，可直接限定本域索引目标，仍须处理未决投递。
- Manager 当前 `ActivateIfSettled` 会在没有未终态 delivery 时自动解除 isolated，恢复循环会调用它；因此不能只调用 Fence 后假定隔离持久。Cleanup 的 C／attempt／目标绑定控制事实必须同时约束 Open、ActivateIfSettled 和 ClaimMaintenance，未决效果不得被启动或恢复流程绕过。epoch 是启动代次，EndpointID 是地址标识，都不充当索引数据 revision。
- 当前 Manager scan 尚无索引候选统计，必须同次补齐。仅在 expected_modules 中出现 Manager 不足以构成索引已评估或已清理的证据。

产品侧建议要求 manual execute 包含 Meta 时，同一次 scan 和确认必须显式同时包含 Manager；仅 Meta 的 scan 仍可评估，但不能据其结果隐式删除 Manager 索引，应提示按 Meta＋Manager 重评估。Engine 删除本已冻结全部十个 Owner，无需扩大其 scope。System 不创建隐藏子项、不替用户勾选、不把“已排队”计作已清除。

用户已明确同意独立治理与显式配对，本项职责和手动范围调整现已授权；不重复征求确认。按正式契约继续 Meta／Manager 接线及其余 Owner 外部效果围栏。所需最小验证为：Meta-only execute 拒绝且零效果；显式双 Owner、原用户撤权及固定 runtime；未知外部结果不重放；Manager 候选范围完整／范围变化拒绝；索引投递成功而结果事务失败时保留可核验事实。测试通过后仍需整体固定 SHA CI；新的 Hosted 单独确认。

本次配对实施验证：System 的 Meta-only execute 返回明确 409／cleanup_manager_scope_required，前端提示显式选择 Meta＋Manager 重评估；新增真 PG 反例验证父评估成功之后仍拒绝越界执行，执行记录总数、请求记录和授权／审计事实均不增加。System 与 Meta Go 门禁、System 180 项单元及构建、Swagger 路由覆盖通过。首次 Meta 新夹具缺少 scan_tasks 表而失败，补齐真实所需表后通过。上述产品改动仍在工作区，Manager 索引执行与其他外部效果尚未接齐；不据此推送半套消费路径。

Manager 纯读索引枚举已写入工作区：通过 SDK documents/fetch，仅读取 id/document_id/tenant_id/engine_id，显式每页 256、最多 100000 个身份、总预算 30 秒；超过预算返回未完成，绝不以截断结果计作完整候选。逐页核对 Offset/Limit/Total、身份唯一性及准确整数；两次独立数据库观察检查出口代次/地址、全部未终态投递和已登记总数。以首期投递账本不删除为前提捕获扫描期间快速完成的变更；将来删除账本前必须替换为持久单调版本。摘要绑定物理索引名、Tenant 和规范排序的完整身份集合。该纯读能力尚未接入 Cleanup 删除，不代替持久 hold、生命周期资格、确认范围复核或效果前原用户授权。

纯读索引能力的本地验证已通过：Manager Go 全模块及标准 `make test-manager-postgres`；后者在真实 PostgreSQL＋门禁自有 Meilisearch 的既有批量投递用例中，验证准确身份枚举、另一 Tenant 空集与摘要绑定、读取期间投递账本零写入。确定性反例另覆盖重复身份、跨 Tenant、非整数 Engine ID、分页缺失／总数变化和两次观察间已快速完成的投递；大于 JavaScript 安全整数的 Engine ID 保持准确。日志为 `/tmp/addp-cleanup-manager-inventory-final.log`、`/tmp/addp-cleanup-manager-native-inventory.log`。本组是工作区本地验证，未提交，不能称为 Cleanup 物理删除／持久 hold 或 Hosted 通过。

本轮继续实施：Meta 已切公共 runner，删除旧 Redis 消费；Engine 范围传播到孤儿、过期、重复指纹扫描及软删除，准确 Engine 事实采用本域节点／item／ScanTask 引用全集逐一 GetEngine，只有 404 判不存在，其他授权／网络／非法能力事实拒绝。原用户撤权、同 Tenant 跨 Engine／跨 Tenant 隔离、专业结果写入失败整体回滚均经标准 Meta Go 与真实 PG 门禁通过。首次 PG 夹具在同一 sql.Tx 并发轮询出现 conn busy，改为同事务同步运行 Owner 后重新通过；保留 `/tmp/addp-cleanup-meta-runtime-pg.log` 原失败和 `/tmp/addp-cleanup-meta-runtime-pg-final.log` 通过记录，不作为 Hosted 证据。

Manager 索引已补持久 `cleanup_hold_id`、C／attempt 绑定操作及准确 Tenant＋Engine 写入阻断目标；初始化、Open 后普通恢复、ActivateIfSettled、ClaimMaintenance 均不得清除或绕过 hold。成功原生 TaskUID 仅记录效果事实，hold 解除须和最终结果事务一起提交；目标记录仍保留阻断旧请求的新投递。新增真实 PG 反例验证重启仍隔离、旧代不能派发、结果事务失败仍 hold、迟到同目标写入被拒绝。标准 Manager PG 通过日志 `/tmp/addp-cleanup-manager-hold-pg.log`；完整产物／索引 Cleanup runtime 尚在集成。


2026-10-29 本批收口进度（工作区，尚未统一提交）：

- Transfer 公共 Runner 接线及真实停止证明已落地。标准 `make test-transfer-postgres` 通过，新增 Owner 结果原子性覆盖正常完成、效果前撤权、专业结果提交失败回滚和跨 Tenant／Engine 隔离；另覆盖真实停止回执、旧运行体迟到确认、未知外部操作不重发、新运行体准入阻断。通过日志 `/tmp/addp-cleanup-transfer-owner-pg.log`。旧 PG 夹具只有 lease、缺失 invocation 事实而失败的原记录保留，修正为协议完整夹具后重验通过，未放宽停止证明。
- `make test-module MODULE=common GO_ONLY=1` 和 `make test-authorization` 通过；后者包含权限清单／生成常量及 Swagger 路由覆盖。此项不等于整体 Owner 门禁或真实 Gateway 授权验收。
- Manager 已移除 Redis Cleanup 消费及无 C 的旧 ExecuteCleanup 路线，接入 scan 冻结、execute 复核、索引与产物持久操作和最终事务。原生成／写入／手动删除共用目标守卫的收口及 Manager 门禁仍在进行，不将临时编译失败或待验证项计为通过。
- 本轮没有启动新的 Hosted、重启服务或删除旧环境记录。


2026-10-29 Manager 组合 PG 收口：`make test-manager-postgres` 最终通过（`/tmp/addp-cleanup-manager-combined-pg-v4.log`）。包括产物未知写入持久阻断、原调用结束后禁止再派发、结果回滚保留隔离、目录／对象并发互斥，以及 Owner 七场景（完成、效果前撤权、结果回滚、扫描目标改变、索引完成、索引成功但结果回滚、索引未知响应）。索引 HTTP 采用受控服务，既有内容快照用例使用门禁自有 Meilisearch；不将受控响应算作 Hosted。真实 PG 揭示并修复了 `jsonb` 重排导致操作快照摘要不一致的问题，持久快照改为原字节文本，查询解析仅在表达式中转换。旧 GLB 测试不再将失败终态视为停止证据，不再期待对未知上传重复删除。此前失败日志保留，未调整超时或放宽停止证明。

本轮完整 Monitor 前端门禁为失败（493 个浏览器用例通过、20 个资源／告警等用例失败）；Cleanup admission 用例通过，131 项单元及随后独立构建通过。System 前端 255 项浏览器用例及构建通过，共享前端 298 项通过。工作区 `make test-changed` 在前置环境检查停止，尚未形成全量 T0-T3 通过证明；平台 Transfer 来源快照需在真实源码提交后更新修订。新的 Hosted 未运行。


产品接线统一提交为 `138ef83ba657f5554576aed9594e17f88949336a`。平台来源核验的唯一漂移是 Transfer 委托创建测试增加独立 Cleanup Handler 构造参数，委托路由／权限／断言语义未变；经本体会话只读复核，将 Transfer 平台修订从 37 提升至 38，冻结该真实源码提交及文件摘要，未新增 Cleanup Tool 或扩大委托权限。来源快照必须引用已提交源码，因此该登记使用后续提交收口。

来源快照更新后，全仓 `make test-go` 通过（`/tmp/addp-cleanup-all-go-postcommit.log`）；`make test-swagger test-authorization` 通过（`/tmp/addp-cleanup-api-last.log`）。


2026-10-29 自然 CI 首轮及测试断言收口：包含产品提交的 `944c4bcf74272a568045a8c066da34878f4c7fa6` 在 [Release and T2 gates 40877071516](https://github.com/pampa0629/addp/actions/runs/40877071516) 中，System Cleanup、Common PG、Manager PG/PostGIS/Meilisearch、Meta、Transfer、Asset、Develop、Model、Quality、Service、Standard PG 及 Graph 引擎契约通过；System IAM 和 Monitor PG 失败，整轮不计通过。

- System IAM 的 OAuth 测试严格角色清单遗漏已确认的 `system.cleanup_authorization.execute`；补齐精确单项，保留完整列表相等断言。标准 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package api --test oauth-client-credentials"` 本地通过，日志 `/tmp/addp-cleanup-iam-ci-fix-pg.log`。
- Monitor 新增 GET 零写入用例把内存纳秒插入值与 PG 微秒读回值直接序列化比较。用固定亚微秒创建时间确定性复现，仅 created_at/updated_at 的精度与时区表示不同。改为首次 GET 前读取完整持久化基线，与全部 GET 后独立读回严格相等；保留固定亚微秒夹具、并发父关闭快照及权限反例，不修改产品写入。完整 `make test-monitor-postgres` 本地通过，失败复现与修正日志分别为 `/tmp/addp-cleanup-monitor-precision-repro.log`、`/tmp/addp-cleanup-monitor-ci-fix-pg.log`。两项沿原有 T2 自动发现，无需新增入口。
- 来源快照提交 `13bf7cd13b08eccc16af1a8bb79130a594af6426` 的 [Platform CI 40878025884](https://github.com/pampa0629/addp/actions/runs/40878025884) 中，Go、产品构建和受影响前端已通过，含 Monitor 全前端；本地 Monitor 的 20 个失败记录仍保留，不能倒写为本地通过。该提交的 T2 汇总虽绿，System IAM 实验作业及大部分 Owner 作业实际跳过，不能用汇总绿覆盖上一轮失败。上述两项修正提交后的自然 CI 仍待核验。
- 本地完整 `make test-platform` 仍在执行；新 Hosted、真实 Gateway／Worker 验收和旧环境记录清理均未进行。最终清单汇报稿已准备，但会话消息工具当前不可用，未送达维护会话，也未据此更新工作簿。


2026-10-29 IAM 迁移目录断言继续收口：`66d0321de4a19afbc9e8c1474a563931f58f129c` 的 [T2 作业 40880761833](https://github.com/pampa0629/addp/actions/runs/40880761833) 第一次在仓库检出阶段达到既有 5 分钟上限而取消；仅重试选择作业后，Monitor PG 通过，具体 CleanupObservationSnapshot 用例实际通过，未跳过。IAM 的 OAuth 权限清单修正通过，但完整 IAM 在后续迁移总测试失败。现按已确认契约补齐 Standard 专业结果权限数量（35→36）、六个已有严格 Runtime 列表中的 Cleanup 窄权限、目的绑定触发器数量（5→6）及 Cleanup audience 检查；不修改产品授权或放宽精确断言。

本地完整 IAM 标准入口另在主体角色用例报“authenticated time must not be in the future”，尚未进入迁移包；该组在上述 CI 中已通过。本地失败日志 `/tmp/addp-cleanup-iam-full-final.log` 保留，不计本地全量通过，不调整认证时间规则。迁移包使用同一标准入口 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration"` 单独验证。

本地 `make test-platform` 最终失败于既有 raster runner 的 Node 主版本检查（25 != 24）；来源快照提交的 Platform consistency CI 已取消。保留平台总门禁未通过事实，已通过的来源核验、Go、构建、Swagger／权限、相关 PG 和前端分别计证，不扩展本轮去修改栅格门禁或预算。新 Hosted、真实 Gateway／Worker、旧记录删除及环境切换仍未运行。

本地完整 migration 包标准门禁最终通过（`/tmp/addp-cleanup-iam-migration-v2.log`），覆盖完整空库迁移、重入／篡改拒绝、当前目录及历史前向迁移。此前触发器数量遗漏的失败日志 `/tmp/addp-cleanup-iam-migration-final.log` 保留；本次仅测试与证据文档变更。修正提交后的 IAM 自然 CI 尚待核验。


2026-10-29 最终固定提交证据：测试修正提交 `af28a3fc750287d20ee9620ce2e0088b033b4db1` 的 [System IAM 完整 PostgreSQL 作业](https://github.com/pampa0629/addp/actions/runs/40885300963/job/123487644831) 及 IAM 汇总门禁均成功；日志核实主体角色、OAuth Client Credentials 和完整迁移 Runner 用例均实际通过，后续包及容器清理同作业成功。全仓 Go CI 亦成功。Monitor 修正的 [PostgreSQL 作业](https://github.com/pampa0629/addp/actions/runs/40880761833/job/123475011425) 成功，明确包含 CleanupObservationSnapshot。

本轮 T2 工作流整体仍为 failure：Redis 原生门禁在拉取镜像时被注册表返回 `toomanyrequests: Data limit exceeded`，尚未执行测试，不记为本轮通过。此前包含同一产品实现的 `944c4bcf74272a568045a8c066da34878f4c7fa6` Redis 作业已成功；本次只修正 PG 测试断言，未修改 Redis 产品路径。不改变镜像来源、预算或测试范围，不因环境拉取失败覆盖原通过或原失败记录。待拉取额度恢复后，仅该门禁仍可补充新提交的 CI 证据。

交付边界：首期十 Owner 接线、持久隔离、迟到写入阻断及专业结果读取已实现并提交；本地与固定提交专项结果以上述记录分别为准。本地全量 `test-changed` 未执行到测试、`test-platform`／全量 IAM／Monitor 前端各自的失败仍保留，不能称全仓本地门禁全绿。新的 Hosted、真实 Gateway／Worker 故障注入及旧环境切换未验收。清单汇报稿位于 `/tmp/addp-cleanup-p0-handoff-20261029.txt`，会话消息工具不可用，未送达维护会话、未由本会话回填工作簿。


### 2026-10-29 Manager GLB 补充复核与修复

CI 协调会话补充两条此前门禁未覆盖的源码缺口：Freeze 将保留的已清理输入审计一律视为未结工作；输出完成／输入登记存在产物 → execution／Task 的反向锁序。它们属于既定方案内的实现问题，不改变权限或模块职责。原通过证据仍有效，但不覆盖这两条新增边界。

已先同步正式 Cleanup 契约，再复用 Manager GLB 生命周期解析及逐项输入身份校验，接受合法 Cleaned 审计清单，保持 pending／garbage／未释放／Watch／未清理／非法对象拒绝。scan 仍可只读展示未决操作事实；unknown hold 继续阻断执行准备和重复效果。生成准备、输入登记／结束、输出完成统一 execution → Task → 产物，并在领域锁后复核 lease；无 execution 的后台路径不反向获取 execution／Task。没有跨远程调用持锁、删除审计或新建公共协议。

实际通过：`GOMAXPROCS=4 GOFLAGS=-p=2 make test-module MODULE=manager GO_ONLY=1`（`/tmp/addp-cleanup-glb-followup-go-final.log`）；完整 `make test-manager-postgres`（`/tmp/addp-cleanup-glb-followup-pg-final.log`）。新增九个 PG 场景覆盖准确物理目标删除及审计保留、未释放／Watch／未清理／非法清单、unknown 不重放、准备与完成／输入结束竞争，以及先 scan 后启动生成时的 Cleanup 与完成竞争。通过 pg_blocking_pids 建立真实锁等待，再执行竞争入口，未出现 40P01，正常终态与产物同事务提交，终态后输入写入拒绝。外部删除为受控回调；不宣称真实对象存储、Gateway／Worker 或 Hosted 验收。新测试由既有 Manager PG 名称前缀及 CI 路径自动发现，无新增入口／服务依赖。

本轮未修改 Common、其他 Owner、API 或权限；其他 Owner 的有效门禁不重跑。首次 Go 编译发现测试可空状态字段比较错误，修正后重验通过；最终 PG 输入另补强审计保留与竞争时序断言后再次完整通过。补充修复提交后的固定 CI 待核对。当前仍无可用的会话发送工具，协调回报记录尚未送达。

GLB 补充修复固定提交 `0fc6299d8150ec5e5cf3ae4cd72657cd759ea969` 的 [Manager PostgreSQL／PostGIS／Meilisearch CI](https://github.com/pampa0629/addp/actions/runs/40889932763/job/123503994261) 已成功，日志逐项确认上述九个新增场景实际执行并通过，没有跳过；同提交全仓 Go CI 已成功。日志 `/tmp/addp-cleanup-glb-followup-ci.log`，本地及 CI 摘要 `/tmp/addp-cleanup-glb-followup-evidence.json`。这两条新增源码缺口及对应确定性／PG 门禁现已收口；新的 Hosted 和真实外部系统故障注入边界不变。

## 八、运行验收准备与协作收尾（2026-10-29）

**当前结论。** 已确认产品路径已落地，证据见前节；专项运行验收尚未完成。此次继续工作先补齐可审查的验收方案及 runner，不启动新的 Hosted、不操作共享环境切换。旧记录可清理的授权继续有效；实际执行仍受停止旧受理、准确归属和外部效果核清约束。

### 8.1 现有入口与真实缺口

只读检查 `.github/workflows/online-t4-gates.yml`、根 Makefile 和当前 runner 后确认：

| 范围 | 现有能力 | 剩余工作 |
| --- | --- | --- |
| Quality bounded recovery | `quality-dynamic-binding` 的 `quality_case=bounded-recovery`；两个自有 Worker、真实 30 秒租约、暂停／崩溃及补回；`make test-quality-online-runner` | 冻结待验收源码并核对受影响输入；历史 Hosted 成功不替代本次改造后的运行证据 |
| Graph | `graph-protection`；FalkorDB／Neo4j；现有 case 为 full、endpoint-geometry、endpoint-same-color；`make test-graph-online-runner` | 缺少本次取消／失联／待核清／reconcile／reopen 的专项 case 与真实故障断言，不能用既有浏览／审核 suite 冒充 |
| Cleanup | 各 Owner 与 System／Monitor 的 T1、PG T2 和受控前端已有分层证据 | 尚无覆盖此次父子协调及外部未知效果的完整 Hosted 编排；需补专项场景、身份夹具、登记和退出清理验证 |
| 旧环境切换 | 已有历史只读盘点，研发旧记录允许删除 | 尚无此次准确清理清单及已验证的一次性执行入口；旧快照不能直接用于删除 |

因此不能只触发一次现有 Quality suite 就宣称整个执行管理体系完成 T4。新增用例复用现有 Hosted 生命周期和业务夹具，避免建立另一套故障平台；新增入口必须同次完成 Make、影响选择、workflow 登记和确定性失败反例。

### 8.2 专项验收矩阵与操作边界

| 批次 | 必需断言 | 证据与限制 |
| --- | --- | --- |
| Q：公共 bounded 恢复 | 崩溃补回及暂停失联后真实等待租约到期；恢复 pending 的 started_at 为空；再次领取时间／attempt／token 更新；旧 heartbeat、进度和终态拒绝；Monitor 显示当前 attempt | 复用 Quality 专项；私有只读核验 token 变化，公开报告仅记录布尔断言，不归档 token；当前 workflow 上限 60 分钟 |
| G：Graph 两种原生引擎 | 实际 Backend 取消与监督器恢复；未 claim 撤销及运行中取消意图；外部操作未决时持久关闭准入；服务端证据不足拒绝 reconcile／reopen；核清后只允许同物理目标显式 reopen，不自动重跑；旧运行体不能回写 | FalkorDB、Neo4j 各自独立一次性部署，分别出结论；先补专项 runner；沿现有 Graph Job 120 分钟上限，不自动扩预算或重跑 |
| C：Cleanup 协调及专业结果 | 十 Owner 逐一证明正式 Gateway 受理、公共领取、准确原用户权限复核、原子回执与私有结果；父关闭／从未开始的真实状态与安全说明；同参幂等、跨用户拒绝、Owner 不可用失败关闭、GET 零状态推进 | 活跃临时 Tenant、普通请求用户、分别授权的观察用户和对应 Owner runtime；只授予临时角色所需窄权限，不改默认角色；每个 Owner 独立记录已执行／失败／未执行 |
| C 的外部效果场景 | Manager 准确对象／索引目标持久隔离、GLB Cleaned 审计保留与迟到写入拒绝；Meta＋Manager 显式配对且 Meta-only 零效果；Transfer 真实运行体结束证明、准确代际 Capture／DLQ、未知结果不重放；Graph 的领域阻断与父关闭组合 | 真实自有对象存储、索引和运行体；只中断本轮进程或其专属连接。无法证明请求确已发送／响应丢失时，该未知效果场景记为未证明，不能拿模拟 HTTP 成功替代 |

C 批次的具体 suite 归属、依赖清单、单场景超时及总预算须在 runner 实现时与 CI 单一负责人核对，冻结到最终运行申请；本表不授权提前启动一套未登记的部署。Q、G 和 C 分批审查与申请运行，不把某一批批准解释为其余批次或重试授权。

统一前置条件：源码与 runner 均已提交；核对固定 SHA 包含产品及后续修复；相关确定性门禁通过，且没有被跳过的必需断言；运行清单明确准确身份、临时资源、场景、预算和退出步骤。只用 GitHub Hosted 独占 Linux 部署，不复用个人开发环境或本地共享 PG。进程动作前核验本轮归属及进程身份，禁止凭端口或名称批量终止。

退出顺序：恢复本轮暂停的进程以便受控退出，停止本轮应用／运行体，删除本轮业务夹具，再销毁本轮 Infra、卷和私有凭据；失败仍执行退出。报告保留源码、场景、阶段、安全错误类别及资源清点；清理失败单列失败，不覆盖原场景失败。任何归属不明、越权、外部效果无法核清或清理残留都阻断成功。未知效果不能靠删除公共执行记录消除。

### 8.3 共享开发环境的一次性切换顺序

1. **刷新只读盘点。** 按准确 task_type、Tenant、execution／request ID 生成候选清单，核对公共事件、Engine 删除引用、Owner 私有结果、旧 Redis 键及消费者；另查存活运行体与外部操作事实。此前 9 父＋63 子及 11 个 Redis 键仅为历史快照，不作为本轮删除输入。
2. **准备入口与演练。** 主责补标准一次性入口，默认只读；应用阶段要求准确清单及其摘要，执行前重读拒绝范围漂移。使用标准测试库和自有夹具验证引用顺序、活动／未知阻断、幂等清理以及中途失败后的准确剩余清单；不创建第三个本地 database，不运行泛化 TRUNCATE 或模糊键删除。
3. **申请一次切换窗口。** 给出已演练入口、固定源码、受影响模块、候选统计、核清证据及预计窗口，再确认实际操作范围。由用户在自己的终端按标准生命周期停止旧受理／旧运行体；不将“无 pending”当作运行体已停止证明，不由 AI 重启服务。
4. **准确清理并启动新路线。** 再核对没有活动或未知外部效果后，按已核验引用顺序清理获准的旧协议记录；保留与本次无关的数据。旧 Redis 消费者与新 PG 监督器不得并行。用户按标准脚本启动已确认的新版本；失败保持停止受理，不重新启用旧协议兼容路线。
5. **切换后验证。** 只对本次自有任务核验新请求、领取、结束、专业结果及 Monitor；确认旧入口／消费者不再运行、无旧协议写入，再解除窗口。共享环境切换成功和隔离 Hosted 成功分别记录，不相互替代。

### 8.4 协作会话的后续安排

用户已授权主责直接联系既有协作会话。本次检查未发现可用的跨会话发送／状态工具；原生应用访问也被 Computer Use 明确拒绝。以下为基于已提交证据的工作分配建议，**不是本次已发出消息或已收到会话确认**；不会通过修改会话存储绕过限制。

| 会话 | 本任务剩余职责 | 是否需要继续占用 |
| --- | --- | --- |
| 主责「任务执行体系改造-0」 | Graph／Cleanup 场景与夹具、运行申请、一次性切换入口、最终证据整合 | 继续承担 |
| CI修复-持续进行 | 后续专项 Make／workflow／影响选择和登记；接收 GLB 两项缺口已在 `0fc6299d8` 及对应 CI 收口的回报 | 仍有专项入口协作项；消息尚未送达，未授权主责并改其共享编排文件 |
| 全面支持falkordb | 正式契约与 Graph 私有路径已交接主责；没有已知未交付产品项 | 就本任务可安排其他工作，后续仅按具体新发现协作 |
| 租户日志改造 | 父子／权限／Owner 结果／Meta＋Manager 边界只读复核已纳入实现 | 就本任务可安排其他工作；新跨身份运行场景由主责负责 |
| ontology模块-系统本体（后续名称 ontology模块-3） | Transfer 来源只读复核已纳入 `13bf7cd13`，没有本任务剩余实施项 | 就本任务可安排其他工作 |
| 三维快显改进 | 依赖交接已提交；GLB 补充缺口由主责修复并通过固定 CI | 就本任务可安排其他工作，不占用其后续点云／预览工作 |
| 文档清理 | 主责承担最终规范核对，不再等待早期可选只读回复 | 就本任务可安排其他工作 |
| p0-hunan功能清单维护 | 接收实际实现／PG／CI 与 Hosted 未验收的分层回报，再核对清单 | 仅维护职责，无产品实施任务；汇报稿仍未送达，主责不代改工作簿 |

Security、p19、p12 等未承担本轮核心实施，不应因本专题占用。上述安排仅针对本任务，不判定各会话在其他任务上的空闲状态。工具恢复后优先发送 CI 精确协作范围及 p0 实际进展；在无法确认共享编排文件归属前，主责继续独立场景准备，不假定消息已送达。

### 8.5 专项 runner 实施与 CI 交接清单

用户已继续授权主责补 runner 和相关门禁，CI 协调事项先整理，新的 Hosted 与环境切换仍单独确认。本轮后续已实际收到 Graph、租户日志、本体及 Security 会话的回报：均确认本任务无剩余产品实施项，可以安排其他工作；更新 8.4 中这四个会话“尚未确认”的时点状态。出站跨会话消息工具仍不可用，不能据此声称本会话已向 CI 或清单维护发送消息。

**新增场景代码。** [Graph runner](../../scripts/test/graph-execution-online.py)复用现有 Graph 原生业务夹具及受控抽取器：先创建准确材料和两个实际运行，利用两条同步监督器占用证明第三条请求确实尚未领取；核对 pending 取消零 attempt／零运行事件；暂停拥有者后向另一个固定 Backend 提交取消，必须保持“running、取消请求已受理”；第二个运行体真实等待 30 秒 lease 到期，核对失联失败、不重放、旧进程准确 execution 的 lease 拒绝、终态未变、unknown 仍阻断 reconcile／reopen，并在准确容器进程崩溃补回后重验持久阻断。固定 Backend 的直接请求与普通 Gateway 读写分别记录，不能把随机 Gateway 分流推定为跨实例。抽取是受控传输，不能称真实模型推理。

[Cleanup runner](../../scripts/test/cleanup-execution-online.py)覆盖十 Owner 的显式 scan＋logical execute、同 UUID 同参幂等／异参拒绝、父子准确绑定、回执与专业结果一致、同 Tenant 他人和无专业权限拒绝、父可读子不可读不输出隐藏节点／统计、终态 GET 不改公共执行／事件、Owner 暂停时聚合 503 而不是成功空集合；Meta-only execute 必须以 `cleanup_manager_scope_required` 拒绝且没有私有请求或公共执行。另暂停唯一 Asset Owner，等待原 30 秒 scan 截止，核对未领取子零开始时间／零过程事件，恢复后由 Owner 真实拒绝；单独的原用户撤权场景只撤销准确临时角色分配，必须在父截止前恢复 Owner，并观察 attempt 0 的 `cleanup_authorization_denied`，不能将截止拒绝冒充撤权拒绝。

[共享控制](../../scripts/test/execution-online-support.py)仅允许本轮专属 Hosted 环境、loopback 地址、准确 checkout／run ID 和固定 Docker ID／镜像／启动时刻。只可暂停 Graph 主／副 Backend 与 Asset Backend，不可暂停 PG。暂停意图先写私有 0600 恢复文件，异常退出由外层生命周期调用 `python3 -m scripts.test.execution-online-support resume` 后销毁部署；不凭容器名称处置替换实例。私有 PG 查询强制 READ ONLY、5 秒 statement timeout，不修改业务状态或 lease；仅在内存比较安全状态摘要，不上传 token、原始日志或结果内容。Graph 的取消失败日志补入 execution UUID，以便准确证明旧实例遭 lease 拒绝；没有改变产品状态机或公开 API。

临时身份由既有 `system/backend/cmd/online-test-fixture` 准备。Graph 新角色与原 protection preparer／reader 分离；Cleanup 请求用户、同权限他人、只读用户及被撤权用户分离，IAM 控制用户只处理本轮准确分配。权限集合在 [runner 身份契约](../../scripts/test/execution-online-contract.json) 与 Go 夹具间交叉校验，不更改默认角色。旧 Graph review runner 同时迁移到 202 入队、准确 execution／attempt 的取消，以及带当前 version 的 Graph 删除；不保留 200 同步构建兼容分支。

**标准门禁覆盖。** 新增 Python 回归放入既有 `graph-review-online_test.py`，由 `make test-graph-online-runner` 实际执行，不新增游离测试入口；该入口已有 `test-security-online-runner` → `test-online-runner` 上层登记。身份夹具新增 Go 用例名称含 `GraphProtection`，同时命中该入口和夹具完整包测试。受影响最小命令为 `GOMAXPROCS=4 GOFLAGS=-p=2 make test-graph-online-runner`、`make test-module MODULE=graph GO_ONLY=1`；共享身份夹具完整包回归复用 `make test-quality-online-runner`，不触发 Quality Hosted。新公开接口／Swagger 没有变化，本轮无需重新生成产品 Swagger。

**CI 单一负责人的接入状态。** CI 已接收并核对两个有限 profile 的输入、报告和恢复协议；用户已确认按有限方案实施共享编排、选择、登记及确定性反例。共享接入已提交 `577aca240`，合并主责 runner 后标准门禁通过，实际命令与结果见 8.6；未运行新的 Hosted。Owner runner／公共控制由主责独占，共享 Make／workflow／登记由 CI 单一负责人维护，本专题按双方约定由主责统一提交。

1. Graph 既有 suite 增加 `execution-lifecycle` case，将该 case 分发给新 Graph runner；FalkorDB、Neo4j 分别运行。现有 `online-hosted-security-gate.sh`、`graph_case`、online dispatcher 和注册校验必须同次接入；共享分发已接入该 case，并保持两个引擎分别选择；确定性结果与实际 Hosted 结果分别记录，本轮没有发起 Hosted。
2. Graph 一次性 Compose profile 复用相同镜像，增加独立 `graph-backend-peer`，不同 module runtime instance identity、独立 loopback 端口和 readiness；两个进程均显式 `GRAPH_BUILD_LEASE_SECONDS=30`、`GRAPH_BUILD_HEARTBEAT_SECONDS=5`，不沿用默认 120 秒。副实例 URL 传入 `ADDP_ONLINE_GRAPH_PEER_URL`；抽取器传入 `ADDP_ONLINE_GRAPH_CASE=execution-lifecycle`。所有被控制容器必须含当次 `ADDP_ONLINE_TEST_RUN_ID`／`ADDP_ONLINE_HOSTED=1`，不能复用个人环境容器。
3. Cleanup 以单独 `cleanup-execution` profile 接入原有 Hosted 生命周期；System／Gateway、十 Owner Backend、所需公共 Infra 和各自正式 runtime 授权必须齐全。该 profile 的 Graph 和 Asset 各只有一个 Owner 实例，否则暂停一个实例不能证明 Owner 不可用／子未领取。调用既有身份工具 `--suite cleanup-execution`，只把其私有输出传给新 runner；fixture 代码已识别此名称，Online dispatcher／workflow 已按有限协议登记，入口仍要求准确 Hosted 部署、私有身份和全部 Owner 预检；登记本身不构成实际运行授权。
4. 两个 profile 的失败／超时／信号退出均先恢复准确暂停实例，再停止应用并销毁本轮业务夹具／Infra／卷／凭据。Graph 故意保留的 unknown 证据不经 API 强删；由整套一次性部署销毁回收。场景 `result=passed` 与最终零残留必须同时成立，不能仅消费子 runner 结果。恢复记录异常、容器被替换、故障控制失败、场景失败、清理失败及缺报告均须有确定性失败反例。
5. 既有 T1 自动入口已覆盖本轮代码；新增 T4 case／profile 的影响选择、身份／服务前置检查、CI 注册断言、报告消费和文档由 CI 同次接入；使用 `make test-execution-online-ci-runner` 验证共享编排，既有 `test-graph-online-runner` → `test-security-online-runner` → `test-online-runner` 包含该子门禁。禁止静默跳过未实现场景、放宽预算或自动重跑。具体提交和自然 CI 结果由 CI 负责人核验后回报，不把代码登记算成专项 T4 通过。

**仍待补齐的实际缺口。** 此批 runner 不证明 Manager 原生对象／Meilisearch 迟到投递、Transfer 原生 Capture／DLQ 的未知外部效果故障，也不证明真实浏览器刷新／翻页中撤权后的旧值和迟到响应清除；已有 PG／受控浏览器证据仍按原层级保留，后续需在对应自有业务夹具上补场景。Graph 的 unknown 来源是受控抽取调用，尚不证明原生写图请求已发出但回包丢失的场景；崩溃发生在终态后以检验持久阻断，不能替代“运行中终止进程”的独立证据。Graph `Reopen` 当前始终因 Provider 原物理目标无法核验而拒绝，positive reopen 不是已实现能力；unknown reconcile 也不会凭迟到本地返回放行。新 runner 对拒绝路径给出断言，并明确该能力缺口，不能据此将 8.2 全矩阵或整个执行体系判为结项。Security 会话补充的 `d86049faf` 后 PG 报告与固定提交关联待统一证据清单核对，不因缺索引重跑其有效门禁，也不扩大此批 Hosted 范围。

本批实际本地验证通过：`make test-graph-online-runner` 的两组 Python 测试分别为 72／31 项，加 GraphProtection 身份夹具 Go 用例（`/tmp/addp-execution-runner-verified.log`）；`make test-module MODULE=graph GO_ONLY=1`（`/tmp/addp-execution-graph-go.log`）；共享夹具完整包及 Quality runner 48 项回归（`/tmp/addp-execution-fixture-final.log`）。命令均设置 `GOMAXPROCS=4 GOFLAGS=-p=2`。文档链接和差异检查通过。这些是工作区确定性门禁，不是实际容器故障、原生服务、固定提交 CI 或 Hosted 通过；本轮未运行 PG T2、Hosted 或环境切换，PG 状态机未改，仅新增只读验收探针。新场景的 Hosted 编排仍按上述清单由 CI 单一负责人接入。

首批场景及身份夹具已提交推送 `410a5d253030555e89e21f0ea2b1da90fbc32d72`。继续检索受影响调用后，修正既有 Graph protection／dense／endpoint runner 的公开状态修改及退出删除：每次从准确 Graph 的 GET 读取当前正整数 version，再提交变更；版本冲突直接失败，不刷新重试掩盖并发。review 删除复用同一函数，删除无版本旧路径。新增缺失／非法 version 和冲突不重试反例；最终 `GOMAXPROCS=4 GOFLAGS=-p=2 make test-graph-online-runner` 通过 74／31 项 Python 测试及 GraphProtection 身份夹具 Go 用例（`/tmp/addp-execution-runner-versioned-final.log`）。中间测试替身曾错误匹配 browse-snapshot 请求，已收窄到 Graph 详情路径并重验；不更改产品或验收断言。

Security 证据索引已补：固定源码 `0c8b006a3748d70d31db4775c9fed4f68422bae7` 的 [Security PostgreSQL 作业](https://github.com/pampa0629/addp/actions/runs/40832834357/job/123310943654) 实际成功，重新只读核验 job 和执行步骤均为 success；日志明确包含 `TestDiscoveryExecutionEventsAgainstPostgres` 的 claim 回滚、旧 lease 拒绝、retry 的真实 attempt、结果／事件事务及 expired lease 写入阻断。`git diff --quiet d86049faf 0c8b006a3 -- <六个 Security 文件>` 确认六个交接文件完全相同，日志保存在 `/tmp/addp-execution-security-pg-index.log`。这关闭上段的既有 PG 证据索引待核对项；本轮没有重跑 PG，也不证明真实 Worker／Hosted 故障场景。首批新 runner 的固定提交 CI 截至本次查询仍 queued，不记作通过，不触发重跑。


### 8.6 剩余故障场景的增量实现（2026-10-30）

用户要求继续补齐主责 runner 并协调 CI 接入；新的 Hosted、环境切换和模块／权限边界变化仍单独确认。CI 单一负责人继续编辑共享编排／登记，主责维护场景与控制。以下是实施范围，验证结果在实施后单独补记，不能据此视为已经运行通过。

Graph 增加独立的运行中进程终止断言，复用现有原生夹具的主图目标（与原 lifecycle 的 review 图目标分离）、已有普通用户和固定 peer；不新增 Engine、产品权限或故障控制入口。在主实例暂停时，只将新任务交给固定 peer，看到准确材料已进入受控抽取且公共 execution 为 attempt 1 running 后，恢复主实例，再对该 peer 的固定 Docker ID 执行既有 kill/start 控制。等待真实 lease 过期，由监督器失败收口；核验未决 operation、closed admission、相同 execution/attempt、拒绝 rerun 及受控抽取只进入一次。随后释放远端抽取返回，重验终态不变与不重放。抽取器最大独立 barrier 从 3 个调整为 4 个，仍不允许同 key 重放。新增报告 running_crash 及必需 check running_process_crash_without_replay，须与 CI 消费断言同次协调；不把该证据标为原生写图回包丢失。

最小门禁为既有 `GOMAXPROCS=4 GOFLAGS=-p=2 make test-graph-online-runner`，包含抽取器上限、准确 running 身份、失联失败／不重放、未知效果及退出恢复反例。仅修改 runner 时不重复产品 PG 或已有效的 Go 门禁；涉及共享编排的确定性门禁由 CI 负责人按实际差异执行。

Cleanup 浏览器增量复用现有同权限 other User，不新增用户、角色权限或产品接口；身份夹具仅补导出该用户已有口令及准确角色分配 ID（全部私有）。完成原协议与 queued revokee 场景后，由 other User 通过正式 API 创建 11 条 Asset scan 历史，保证真实第二页和本人专业结果可读。两个独立浏览器上下文均通过 Console 登录，先读取真实分页，再分别通过“刷新历史”和“下一页”触发撤权后的读取。专业结果仅使用 route.fetch 获得的真实 200 原响应延迟交付，不构造响应或修改产品状态；测试控制进程通过准确 IAM 分配执行一次撤权，先观察真实 401 和旧内容清除，再释放原响应并核对不能恢复显示。IAM 凭据不得进入页面或产品 runtime；浏览器原始日志、截图／trace／失败附件留在临时私有目录，不归档。对请求用户、Tenant、12 项权限、准确角色分配、请求／子执行、真实分页、两种刷新路径和迟到响应分别给断言。

此增量需要 Cleanup profile 的 Console/System frontend、Chromium 和三个私有环境输出 `ADDP_ONLINE_CLEANUP_OTHER_USERNAME`／`OTHER_PASSWORD`／`OTHER_ASSIGNMENT_ID`，复用已有 IAM 控制 token。外层必须消费 `cleanup-browser.json` 的精确身份及两个阶段通过报告，新增 check `browser_revocation_refresh_pagination_late_response`；不得将缺浏览器、缺报告或未执行记为跳过成功。这里只准备代码，不启动 Hosted。新增 Go 导出涉及共享夹具，使用既有 Graph runner 入口及 Quality runner 完整夹具包回归；CI 同步 JS 语法和报告反例登记。


**原生效果场景的新增身份／目的边界（2026-10-30 用户已确认）。** 现有 Cleanup 临时角色严格只有 12 项清理及专业结果读取 Permission，不能创建真实 Manager 产物或 Transfer Capture／DLQ；现有 IAM 控制会话只用于撤销本轮准确角色分配，不能扩大为业务准备管理员。Transfer 普通手动 scan 没有 Engine 生命周期 context 时明确返回空候选（`transfer/backend/internal/service/cleanup_service.go:listCandidateTaskDefinitions`），所以往空 Tenant 填任务再执行普通 Cleanup 仍不能证明 Capture／DLQ 删除。Engine 生命周期部分必须从已确认的 System Engine 删除评估／确认链路进入，不能给普通 scan 新增隐藏 context，也不能 SQL 伪造执行状态；DLQ 删除入口的进一步核对见下文。

已确认身份方案：在独立原生效果 case 中复用现有 Manager artifact 及 Transfer continuous 两组准确普通业务准备 Permission（以 `system/backend/cmd/online-test-fixture/main.go` 的 `managerArtifactPermissions`、`transferContinuousPermissions` 为唯一来源，各 10 项，身份彼此分离），沿已有真实扫描、产物生成／同步任务和显式源数据授权夹具创建自有数据；另建仅供本轮临时 Tenant 的 Engine 生命周期 User，只授予 `system.engine.create`／`system.engine.read`／`system.engine.delete`、`system.cleanup.read`、`manager.cleanup_result.read`、`transfer.cleanup_result.read` 共 6 项，不授 tenant.administrator，不改变现有 12 项 Cleanup 角色或默认角色。该 User 只可在 runner 的准确本轮 Engine ID 清单上执行 deletion-assessments／确认删除（含原确认 digest 和物理产物 delete 策略）；原生 Manager／Transfer 结果仍走已确认的本人专业结果窄权限方案，两项专业读取 Permission 与 Engine 删除操作 User 绑定，不能代读他人的私有结果。只处理 active Tenant 和本轮 Engine，历史 Tenant 不进入。

请求发出但回包丢失由本轮专属连接故障控制实现，独立原生只读探针确认对象／索引／Capture／DLQ 的真实状态；没有真实发送和原生效果证据时必须失败，不能由客户端超时推定成功或 unknown 已成立。控制不增加产品测试端点，不修改公共 execution/lease，不扩现有场景预算。用户已确认上述临时窄身份及目的绑定，不再重复申请；这不授权新的 Hosted 或环境切换。Graph positive reopen 还需要单独完成 Provider 原物理身份的规范方案，不能把名称／端口／晚到本地回包当作证明，此处不申请变更。


**原生场景入口复核及已确认的操作范围修订（2026-10-30）。** 已批准身份不等于 Engine 工作流会删除所有 Transfer 资源。正式 Cleanup 规范的 Transfer 节要求 Engine 生命周期保留、禁用用户任务，直接删除任务使用独立当前用户授权；`transfer/backend/internal/service/cleanup_service.go:executeCleanup` 明确以 `physical := mode == physical_cleanup && !engineDeletion` 区分，因此 Engine execute 保留 DLQ 与私有任务事实。`docs/spec/tasks/Transfer-Kafka与DLQ.md` 第 19 条规定用户直接删除任务才走 Capture／DLQ 与私有状态的删除入口。先前计划把 Engine 清理当作 DLQ 删除证明，覆盖范围判断有误，不能据此改产品删除语义或伪造历史 Tenant context。

用户确认保留现行业务契约，将原生验收明确分为两个入口：

1. Engine 生命周期 case 由已批准的六项权限 User 评估／确认本轮准确 Engine，验证 Manager 自有产物／索引清理和 Transfer 任务禁用、定义与 DLQ 保留；Capture 的准确代际清理由该工作流既有路径验证。CDC 先通过已有 pause 停止数据运行体并保留 capture，再检查停止证明，不能先调用会删除 capture 的 Stop 把测试对象清空。
2. DLQ 删除及未知响应 case 由已批准的 Transfer 普通业务 User 使用其已有 `transfer.task.delete`，显式删除本轮自有任务；验证准确 DLQ topic、外部响应未知后保留任务与索引、不重发及新 producer 被隔离。该证据标明是 Transfer 直接删除入口，不能冒充 System Cleanup 父子闭环；不扩大六项 Engine User、不使用 IAM 管理员、不启用历史 Tenant。

用户已确认此修订，两个入口的身份权限保持不变；另以独立原生 case 接入，不静默扩大已冻结的两个 profile。Graph 运行中崩溃、Cleanup 浏览器及已冻结 CI 接入继续完成。Graph positive reopen 的 Provider 原物理身份方案仍是独立缺口。


**本轮实际验证与提交边界。** 合并 CI 的 `577aca240` 后，`GOMAXPROCS=4 GOFLAGS=-p=2 make test-graph-online-runner` 实际退出 0，80 项场景测试、142 项接入／预检／报告消费测试及 GraphProtection 身份夹具 Go 测试通过；日志 `/tmp/addp-execution-runner-final-20261030.log`。共享身份夹具的完整包另由 `GOMAXPROCS=4 GOFLAGS=-p=2 make test-quality-online-runner` 验证，48 项 Python 及 Go 完整包通过，日志 `/tmp/addp-execution-fixture-incremental-20261030.log`，其后该 Go 输入未再改变。早先两次 Graph 入口遭遇共享接入中间态失败，保留原日志；最终未跳过用例、未放宽断言。JS 仅完成语法与 runner 报告契约验证，尚无真实浏览器执行结果。没有新的 Hosted、PG、容器故障运行或环境切换；原生未知效果及 positive reopen 仍未完成，不把本地 runner 测试当作运行验收。

用户同时授权：在既定大方向、原则和模块功能边界内，实施细节由主责自行推进并动态报告；新的 Hosted 和环境切换仍单独确认。原生 case 采用独立 suite `cleanup-native-effects`，避免把原生效果证据混入已冻结协议／浏览器 profile。

### 8.7 双入口原生故障验收接线（2026-10-30，实施中）

用户已确认 8.6 的两个入口，且允许主责在既定方向、原则和模块边界内自行推进实现细节。保留现有 Engine 删除语义；不增加产品权限、不启用历史 Tenant，不启动新的 Hosted 或切换环境。

独立 `cleanup-native-effects` suite 每次显式选择一个 `ADDP_ONLINE_NATIVE_CASE`，不一次复用已被 unknown 隔离的物理索引或运行环境。六个有限 case 为 `manager-success`、`manager-object-unknown`、`manager-index-unknown`、`transfer-engine`、`transfer-dlq-unknown`、`transfer-capture-unknown`；准确必需断言唯一存于 `scripts/test/execution-online-contract.json:native_case_checks`，Owner 和 CI 同读，不各维护另一份成功条件。

- Manager 由现有 10 项普通业务权限 User 沿正式 Meta 扫描、准确组成源 Grant、OBJ→GLB Model3D Runtime 生成真实产物；Engine User 沿原评估、名称／版本确认、权威复扫与十 Owner 流程清理。以直连 Infra MinIO 和 Meilisearch 原生读取核验效果，保留 Cleaned 输入审计与目标隔离；清理前后都按现有固定哈希核对 Business MinIO 的 OBJ／MTL／纹理未变。Manager 普通来源授权复用既有 `ModelSourceAuthorization`，显式传入已有 16 项初始化 Permission；该身份不需要 Deny 创建／释放权，其他已有调用仍使用其既有 Permission 集合。
- Transfer 由独立现有 10 项普通业务权限 User 创建真实 CDC／Kafka continuous 任务。首先观察真实目标业务行提交，再 pause 并取得 `runtime_sessions.stopped_at`，保留 capture 供 Engine 工作流清理。准确 generation 的 connector／slot／publication 由原生探针读取，不能以 lease 过期代替运行体停止。Engine case 验证任务与 DLQ 保留，再以普通用户显式 task DELETE 验证独立删除入口；DLQ unknown case 在 active Engine 上独立执行，核对外部 topic 已消失而任务／控制索引／原操作仍保留，重复 DELETE 不重发，新 producer 不准入，目标业务行保留。
- 请求及实际回包丢失由独占容器中的 `native-effect-proxy.py` 实现。每实例固定 upstream，控制仅容器 loopback；准确 run／UUID key 只可绑定一次。HTTP 以方法、路径、body SHA 绑定，Meili 另限定 canonical UUID `customMetadata`；Kafka 按当前固定 kmsg v0–v6 DeleteTopics 解码准确 task-owned DLQ 名并匹配 response correlation。收到真实 upstream 响应后才丢 downstream 回包，后续准确同目标重发只阻断、不再转发。独立 native probe 继续直达原生服务；仅修改 Kafka seed 地址不够，部署启动前必须固定 advertised listener 经 proxy。请求发送一次、实际 upstream 响应、原生效果与持久隔离缺一不可，不能用客户端超时推断成功。
- 临时业务／Engine 三用户准确 Permission 集合及私有环境导出键在同一机器契约中登记。修复了先前 `writeEnvironmentFile` 漏导出 Cleanup 新字段及 Graph lifecycle token 的问题；新增真正写出文件的 0600、字段完整性、shell 引号和任意 secret 拒绝反例。既有 Graph 其他专用字段原本已登记，不重复导出。

主责拥有 Owner runner、proxy、身份夹具与专业断言；CI 会话拥有独立一次性 profile、最小真实 Runtime／业务夹具、私有 manifest、标准 Make、选择／预检／报告消费／恢复销毁／登记与 workflow。同次完成接线和标准门禁后再提交。原生探针用的 `cleanup-native-fixture.json` 必须位于当次私有目录且为 0600，绑定 run／case、准确容器 ID／image／StartedAt、真实源描述与直达 native 连接；只读 PG 使用当次独占 `addp_online`，不操作本地共享生产库。所有 raw native／浏览器内容、凭据和私有运行信息不归档。

**已发现的既有产品表现及证据边界。** `StartContinuousExecution` 遇到 `ErrCleanupOperationUnresolved` 会阻断准入，但当前 Transfer handler 没有该错误的专门映射，返回 HTTP 500，安全错误为 `start continuous execution: transfer external cleanup operation is unresolved`。DLQ unknown case 只接受这个准确原因，并同时要求原 task／DLQ 计数、准确 operation 摘要及待执行／运行数量不变、真实发送数仍为 1；报告显式保存 `producer_refusal_http_status=500`。它验证安全围栏，不把现有 API 错误分类缺口掩盖成合理的冲突响应。此次不为适配测试而改变产品返回语义。

六 case 报告显式保留 `native_late_writer_acceptance=not_run`、`graph_native_write_acceptance=not_run`、`positive_reopen_acceptance=not_run`：并发旧 writer 真正迟到写入、Graph 原生写图回包丢失和正向 reopen 仍需自己的独立场景／物理证据，不能被本批结果覆盖。当前只有代码及确定性开发检查，真实 Hosted 未运行，尚不记作原生效果验收通过。最终标准门禁及固定提交 CI 结果在完成后补记。

**本批实际门禁。** CI 接线与 Owner 输入合并后的 `GOMAXPROCS=4 GOFLAGS=-p=2 make test-execution-online-ci-runner` 实际通过：59 项 native／transport／业务夹具／Hosted 生命周期测试，以及 143 项共享预检、报告消费与登记测试；日志 `/tmp/addp-native-ci-final-20261030.log`。其中 Owner 的 14 项测试实际覆盖 Engine 确认身份／版本／十 Owner 完整性、Grant 先于源下载、Manager native 效果缺失与审计／hold 损坏、双入口顺序及 DLQ 保留、原生存储授权失败不能当作对象不存在、实际停止证明、精确 HTTP／Kafka 传输与回包丢失及拒绝重发。此前独立标准 `make test-cleanup-native-online-runner` 57 项通过；新增两个 Owner 反例已包含在上述最终 59 项中，不把旧计数当作最终覆盖。

共享身份夹具 `GOMAXPROCS=4 GOFLAGS=-p=2 make test-quality-online-runner` 48 项 Python 与完整 Go 包通过（`/tmp/addp-native-identity-final-20261030.log`）；源 Grant helper 的既有 `make test-ftp-online-runner` 98 项、JS／Shell／登记和完整身份 Go 包通过（`/tmp/addp-native-grant-helper-20261030.log`）。CI 负责人另完成 workflow security 标准门禁及实际 Compose 只读合成，准确十三个 Backend loopback、Graph／Asset 单例及代理出口的证据位于 `/tmp/addp-native-actual-compose-proof.json`。这些都是确定性／配置证据，未启动容器故障场景或新的 Hosted。

上一批 `5f7876931` 的自然 Platform CI 原始失败为接入测试把 secret 放在临时 checkout 内；CI 负责人以 `117d7f892` 修正测试目录与规范化，未放宽生产准入。修复后的自然 Platform Job `123835886996` 中 142 项与登记实际通过，关联 Run `40985638920`，不据此将整轮或新原生 Hosted 计为通过。本批 Owner／CI 按依赖顺序分别提交，再统一推送和跟进本次影响门禁。

### 8.8 Manager 旧 Runtime 真正迟到写入专项（2026-10-30，实施前范围）

按用户继续指令和既定边界内自主推进授权，新增独立 `manager-late-writer` case；身份仍为既有 Manager 10／Transfer 10／Engine 6 与准确源 Grant 初始化角色，不改变产品删除语义，不启动新的 Hosted 或切换环境。该 case 只验证 Model3D Runtime 的旧执行迟到上传，不泛化为 Meilisearch 旧投递、Transfer 旧 producer 或 Graph 原生写图的完整证明。

先暂停本轮固定 Model3D Runtime，普通 Manager User 通过正式 quick-view action 受理 OBJ→GLB；从当次私有只读 PG 取得准确 execution／attempt 及 pending StorageRef，准确对象在 native store 尚不存在。只对该对象的 PUT 预置私有 `http-write` barrier；恢复同一 Runtime，必须看到真实请求完整进入且尚未发送上游，再终止并补回固定 ID／image 的本轮 Manager Backend。该 case 的 lease／heartbeat 明确为 30s／5s，真实等待旧 lease 过期及 attempt 1 失败收口，不修改公共 execution／lease 或伪造停止证明。

Engine User 沿既有 deletion-assessments 只读评估，该未决 writer 必须阻断评估，Engine 保持 active，不能转入 DELETE 或把请求失败当作对象不存在。随后释放原准确 PUT：proxy 至多发送一次，必须取得真实 native 回包；独立 S3 读取确认迟到 GLB 确实出现。旧执行仍保留失败、原 UUID／attempt／start，不能发布 ready 成果或由新进程接收迟到结果；原 pending／未决审计身份保留，新 generation 被拒绝，原业务 OBJ／MTL／纹理哈希不变。Native store 的迟到对象存在不等于业务成果可见或清理可执行，必须继续隔离。

故障控制只针对本轮 Model3D pause/resume 与 Manager kill/start，均使用准确容器 ID／image／StartedAt 和 run 环境身份；退出先恢复本轮暂停实例并释放本轮 barrier，未知事实不通过产品 DELETE 强清，最终仍由一次性 Hosted 生命周期销毁。`http-write` binding 不拦其他对象，不复制原始 body 到报告；未知、未收到请求、未真实发送、无 native 对象或缺少原 attempt 围栏证据均失败关闭。

必需门禁仍由 `make test-cleanup-native-online-runner` 及上层接入门禁执行，补精确 PUT／不可重放、错误对象／跨 run／重复绑定／等待失效、旧 Owner 与 late native 状态反例，并同步 CI 显式 case／报告／恢复契约。只有该 case 完整成功时才可报告 `native_late_writer_acceptance=passed`，且 scope 固定 `manager_glb_upload_after_owner_crash`；现有六 case 继续 not_run，Graph 原生回包与正向 reopen 仍不在此 case 范围。代码与确定性门禁不能计为真实 Hosted 已通过。

**本批实施及实际验证。** 新 case 的普通业务准备复用原 Manager 单一路径。Model3D 输出 access-plan 的 endpoint 实际来自 Manager MinIO 配置，故该真实 Runtime PUT 会经过准确 object proxy；原生读取继续直达 MinIO。新增 `http-write` barrier 在完整 PUT 进入后才等待，放行前不连接 upstream，放行后最多发送一次并保留原响应；等待过期后不允许迟到放行或重放。控制意图在发出 `/arm` 前登记，即使响应丢失，退出仍按原 key 恢复。pending GLB 读取使用有界零／一行观察，Worker 尚未建立结果时等待，重复身份拒绝；不把零行误当作 JSON 协议错误。迟到对象按原生 S3 读取并复用现有 OBJ→GLB 完整校验器，不仅检查非空。

最终实际通过 `GOMAXPROCS=4 GOFLAGS=-p=2 make test-graph-online-runner`：80 项 Graph／控制回归、68 项 native／transport／夹具／恢复测试、143 项共享预检／报告／登记及 GraphProtection Go 用例，退出 0，日志 `/tmp/addp-late-writer-graph-final-20261030.log`。其后仅修正上述 pending 零行竞态并加强迟到 GLB 原生内容校验；聚焦 20 项测试通过，再以 `make test-cleanup-native-online-runner` 完整复验最终输入，69 项通过、退出 0，日志 `/tmp/addp-late-writer-native-final-20261030.log`。共享报告／选择契约未在该增量改变，不重复稳定的共享门禁。所有结果仍是确定性、真实 loopback 传输或配置验证，没有运行新的 Hosted、共享环境切换或本地 PostgreSQL 门禁。

现有六 case 的 late-writer 标记保持 not_run；新增 case 也只有在后续实际 Hosted 的全部断言及最终销毁均通过时，才能形成这一有限范围的真实运行证据。Meilisearch 真正迟到投递、Transfer 旧 producer、Graph 原生写图未知效果、positive reopen 与 Transfer 围栏错误的 HTTP 500 分类仍按原缺口保留。
