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
