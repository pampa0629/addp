# ADDP 任务执行管理统一收口专题

更新时间：2026-10-29

状态：诊断与实施方案待确认。已完成范围梳理和文档记录，尚未启动整体重构，也未向协作会话派发实施任务。

## 一、范围与结论

本专题处理任务体系中的执行管理：一次 execution 如何创建、领取、运行、续租、恢复和终结，以及这些事实如何被 Owner、Monitor 和测试消费。它也包含没有持久任务定义的一次性执行与运维 execution，不能把所有 execution 都当作可调度任务。

当前已有统一执行记录和 bounded 租约协议，主要问题是公共字段与状态转换尚未完全由唯一入口强制维护，部分正确性依赖各 Owner 自行补齐。目标是收口公共执行事实及其写入约束，同时保留 Owner 对业务授权、外部副作用、提交和安全重放的裁决。

任务定义、Cron、触发去重和 DAG 依赖属于相邻范围。[调度与任务编排统一改造专题](ADDP调度与任务编排统一改造专题.md)讨论“何时、为什么创建执行”；本专题讨论“创建之后如何可靠管理执行”。两个专题分别确认和实施，本次不引入统一 Trigger，不迁移 owner scheduler。

正式契约继续以以下文档为准；本专题的建议尚不替代现行规范：

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
| pending → 拒绝或其他合法终态 | 明确 pending 条件；不伪造开始时间、运行耗时或领取次数 | 执行前拒绝原因；取消须满足现行真实运行体契约 |
| 人工重试 | 新 execution，保留原执行终态和前驱关联 | 当前权限及新的冻结输入；不复用旧授权 |

同一 attempt 的 `started_at` 不可变。终态耗时由已持久化的开始时间计算，不能由 Owner 重新取起点；无开始时间的终态耗时为空。恢复后原创建时间至当前开始时间的差值不等于纯排队耗时之和，不在本次引入 attempt 历史表或累计时长实体。

过程事件沿现有安全字段、预算和保留规范。已接入事件的状态转换须维持同事务；尚未接入的 Owner 需要明确接入范围，不能把所有内部动作强制扩展为新产品事件。Trace、进程心跳和日志均不代替执行所有权。

## 四、建议实现路线

### 4.1 公共入口收口

1. 在现有 `common/execution` 内提供语义明确的创建、领取、进度、续租、恢复、pending 终结和当前 attempt 终结入口。使用明确参数或闭合字段集合，不再接受可任意修改生命周期字段的 map。
2. 统一时间与耗时生成。生产 lease 的权威时钟、锁后复核及到期等号边界由真实 PostgreSQL 测试确定；测试时钟必须隔离于生产路径。
3. 授权附加、异常无租约记录收敛等使用明确前置条件，不把它们塞回万能更新入口。Common 不读取 System 或 Owner 私表来猜授权和业务资格。
4. 公共原语支持 Owner 的外层事务和既有锁顺序；Owner 摘要、结果、必要事件失败时一起回滚。外部提交另按 Provider 协议确认，不将 PG 事务当作外部系统事务。
5. 调用方逐项切换后删除旧 `Save`、无条件状态写入和隐式 lease fallback。采用原接口原位收紧或完整切换调用方的提交，不提交永久兼容包装或两条生产写路线。

### 4.2 Owner 接入顺序

先迁移已使用公共 claim 的 9 个 Owner，保持各自的恢复资格不变。优先修正 Security 和 Ontology 的公共时间字段，再检查其他 Owner 的 pending 失败、过期收敛、摘要事务、授权和专业回执。

Graph 推荐改为 Backend 内嵌监督器，API 只持久入队，由公共 claim 取得唯一所有权；保留材料、审核和图写入规则，失联默认失败收敛，不自动重放已发生的图写入。该运行机制调整须先确认并修订 Graph 契约。

Cleanup 必须先确认持久执行身份、协调事实与结果事实的唯一来源。建议保留 System coordinator 和各 Owner executor 的职责，把重复事件准入、执行领取、结果写回和失联终结做成可核验的持久协议。Redis 协调状态与 PostgreSQL execution 不能假装处于同一数据库事务；需要明确唯一权威状态和幂等消费／结果收敛协议后再实现。不默认自动重试物理删除，不在本轮擅自扩大清理范围。

Transfer continuous 保持独立 runtime 所有权，公共字段通过明确的受控适配写入。禁止用“没有 bounded lease 就直接写”的方式识别 continuous，也不能在消费者未迁移完时破坏其取消、恢复和 position 提交。

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

涉及的现有 PostgreSQL 入口包括 `test-common-postgres`、`test-quality-postgres`、`test-security-postgres`、`test-meta-postgres`、`test-transfer-postgres`、`test-develop-postgres`、`test-manager-postgres`、`test-model-postgres`、`test-orchestrator-postgres`、`test-ontology-postgres`、`test-monitor-postgres`；具体按实施影响与当前注册选择。Ontology 投影激活与图联动还须覆盖 `test-ontology-falkor`，不能只验 PG 修订。Graph／Cleanup 如新增依赖或测试入口，须同次完成 Make、影响选择、CI 和文档，不能沿用旧 Go 通过记录冒充新协议 T2。

阶段 B 可在接口实现之前独立推进，但共享 Make／workflow 变更由单一负责人整合。C、D 的提交需维持每个已迁移调用方只有一条主路径；整体接口收口必须等待全部调用方闭环。阶段 E 不为“全覆盖”重复仍有效的场景；输入发生变化时才按影响复验，并逐 Owner 记录尚未验证项。

## 六、会话协作建议

采用“一个主责会话统一设计与整合，少量领域会话核对或承担独立文件范围”的方式。下表来自 2026-10-29 的会话状态与最近工作核对，是建议分工，尚未派发，也不改变各会话原授权。

| 会话原名与 threadId | 已核对的工作范围 | 建议职责 |
| --- | --- | --- |
| 修复 RetryExpired 的 attempt 开始时间清理；`01a1811b-d160-77c0-817c-286daeb9fca8` | 本次公共时间修复、Quality 恢复验收和整体审查 | 主责：规范、Common、调用方清单、核心迁移和最终整合；`common/execution/**` 只由本会话修改 |
| CI修复-持续进行；`01a115b5-90bc-7d11-9a68-ffd2af505611` | 当前持续核对 CI、去重和协调修复 | 核对独立 Job／required 汇总、路径选择及验证计划；若承担实施，Make／CI 编排由该会话单一整合，本会话不并改 |
| p145-security；`01a10003-a16c-76c3-b047-237c2317425d` | 最近完成保护算法展示与入口修正 | 核对 discovery 重放、Finding／保护投影事务与授权边界；公共接口稳定后可承担 Security 私有适配，不能改公共租约 |
| 租户日志改造；`01a0f7e1-50f2-7063-9df2-dd918416b75a` | 执行事件、安全诊断、分页／保留；后续关注持续写入 | 核对事件与状态同事务、attempt 展示和读取权限；Monitor／事件验收边界由其复核，公共事件代码仍由主责整合 |
| p19-接口质量与调用链；`01a1585e-ae3c-7e60-a4b6-4ecaadb2bc17` | HTTP 质量、调用链及采样证据 | 按需复核 execution 短阶段 trace 的事务提交语义，不承担本次生命周期主改造 |
| ontology模块-系统本体；`01a04725-c1d4-7261-b90c-f55c3849ec63` | Ontology 投影、PG 发布与 FalkorDB 联动 | 按需复核投影锁顺序、激活指针和授权／租约双重条件，避免公共抽象破坏既有提交协议 |
| 全面支持falkordb；`01a1476a-4a03-7fa2-99f2-cca6aa13a857` | Graph／图引擎相关工作；最近工作转为 Console 门禁 | Graph 运行机制确认时提供历史业务边界；不因会话名称直接派发监督器重构 |
| 文档清理；`01a148c8-5547-7810-b108-bbfbcd185654` | 概念／规范去重与职责表述整理 | 实施落地后复核规范唯一事实源和删除过期方案；不重复维护本专题 |

`p12-服务与主机监控` 正在处理主机／服务页面交互，执行实例观测不等于任务所有权，本批不要求其参与核心修改。栅格、拓扑、格式、数据权限等会话按实际受影响路径协作，不因共用 Common 就全部派发产品改造。`p0-hunan功能清单维护` 只接收实际实现和验证结果，方案确认不能记成功能完成。

不按“一个模块一个会话”拆散公共状态机。优先只请 CI、Security、租户日志三方核对契约；其他会话在出现其独有业务边界时介入。没有清晰活跃负责会话的 Owner 适配由主责承接，不能猜测归属或擅自新建任务。

共享工作区内，派发前必须再次读取最新进展，明确文件、接口版本、验收和合入顺序；已在处理的不重复派发。各会话只提交自有改动，实施前再次核对未经提交的同文件修改。消息、并行实施和运行验收分别以用户确认的范围为准。

## 七、待确认决定与关闭条件

下一步优先确认本专题的主责／协作方式和以下架构决定，再修订正式契约并实施：

1. Common 独占公共生命周期字段，Owner 保留业务提交和安全重放裁决；移除通用无条件写入口。
2. Graph 采用内嵌监督器和公共 lease；失联失败收敛，不自动重放图写入。
3. Cleanup 的唯一协调事实、持久执行身份、重复事件处理及结果收敛协议；该项需要专门设计，不将本专题的方向当作已完成方案。
4. 公共生产时钟、锁后到期判定、字段恢复规则和存量迁移范围；不新增 attempt 历史实体。
5. CI 独立结果与必需汇总保持覆盖，新的 Hosted 场景逐项明确范围。

完成时必须同时满足：运行路径有明确所有权；公共字段有唯一维护入口；旧路径删除；Owner 事务／副作用边界有证据；受影响标准门禁实际执行；真实验收按固定版本与范围通过，清理可核验。未运行、失败、跳过和未确认设计分别记录。

已实施的稳定规则迁入现有 concepts／spec 与模块文档；本专题只保留剩余决定和缺口，全部关闭后删除，不再积累逐轮运行流水。
