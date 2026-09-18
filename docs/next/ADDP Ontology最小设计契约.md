# ADDP Ontology 最小设计契约

更新日期：2026-09-18。

状态：独立 Ontology 的原生语义内核、PG 修订/发布服务、FalkorDB 投影适配和单机 Infra、System 内部任务授权、投影执行/激活及失败批次显式重建、常驻 Backend、管理 HTTP 入口与 Console 建模页面已实现，范围与标准验证入口见 [模块说明](../../ontology/CLAUDE.md)。两个原生定义只读 Agent Tool 已交付，并于 2026-09-18 完成本地真实用户的建模、发布激活和 Agent 读取联调；这不是业务实例查询验收，也不替代隔离部署的正式 T4。真实 T4 首跑被公共 Infra 的 MinIO 镜像拉取失败阻塞，未进入本体断言；该验收暂缓，不算通过。当前不调整 Graph，后续另行讨论其职责迁移。本文的目标设计不代表功能均已实现。

## 1. 目标与范围

### 当前建模页面切片

Console 数据治理组提供独立 Ontology 入口，不改动 Graph。原生类型、属性、关系、规则使用同一份表单草稿，保存仍由现有 Backend 校验、编译和生成摘要；浏览器不执行 CEL。页面只消费当前 Tenant 的正式管理 API，不访问 PG 或 FalkorDB。

模块内公开路由为 `/ontologies`、`/ontologies/new`、`/ontologies/:ontology_id`、`/ontologies/:ontology_id/revisions/:revision`。列表分页使用 `page`（默认 1 省略）；编辑器页签使用 `tab=properties|relations|rules`（默认 classes 省略）。新本体创建 revision 1；新修订从已保存定义复制并使用 head.last_revision+1。保存和状态流转携带精确 version；409 不自动重试、不覆盖本地编辑。发布/重建的 202 或含 intent 的 502 不表示激活，必须重新读取修订、最新投影与 active 指针；传输结果不确定时锁定写操作，要求显式重新加载后再决定下一步。投影 ready 也仅在 head.active_generation 匹配时显示为当前生效。

首轮页面提供创建、保存、提交审核、退回草稿、发布、撤回和失败投影显式重建。操作同时受状态与 Permission 限制，发布/重建额外要求执行授权权限。离页保护、认证、语言、主题和 Console 导航复用 common-frontend。T0 检查生命周期和 CI 登记，T1 验证状态/请求契约，T3 使用独立端口与受控 API 夹具验证交互；不借此声称真实 T4 或 Agent 已通过。

目标是让 Agent 使用用户明确建设的领域概念、关系、规则和数据映射，减少猜测，并对结论给出可核对的依据。不是用图数据库替换 Skill + Tool，也不是把业务数据整体搬进图数据库。

首版路线：

- 独立 `ontology` 业务模块，后端 Go + Gin + GORM，前端 Vue 3，沿用 ADDP 共享能力。
- Infra PostgreSQL 的 `ontology` owner schema 保存本模块权威定义、修订和发布控制事实；FalkorDB 保存可重建的已发布语义关系投影。
- CEL-Go 执行有限、确定性的分类条件；关系语义由 Ontology 服务明确实现，不交给 LLM 自行推断。
- 用户业务数据留在原业务库，访问仍走拥有该能力的正式 owner。
- Agent 保留现有 Skill、Tool Manifest、ToolExecutor、Python SDK、Gateway 和委托身份链路。
- 首版服务 Tenant 领域本体。平台术语、文档、Tool 描述仍沿现有规范和 Skill 管理，不把导入全平台语义作为前置条件。

不纳入首版：完整 RDF/OWL 推理、任意递归规则、任意 CEL 到 SQL/Cypher 的自动编译、通用 GraphRAG、全量业务实例复制、另建 Python/Java 本体服务，以及新的中心 Tool 服务。

## 2. 术语与唯一事实所有权

新增术语见[术语表](../concepts/addp术语表.md)的“领域本体（目标设计）”。以下职责是目标边界，不表示现有接口已经支持。

| 事实 | 唯一 owner | Ontology 的使用方式 |
| --- | --- | --- |
| 业务域、术语、数据元、码值、单位、指标定义 | Standard | 引用确定修订或带版本/摘要的只读来源捕获，不提供可编辑副本 |
| 业务实体、属性、实体关系、逻辑模型、维度层级、指标实现及概念实现映射 | Model | 引用并增加本体专有语义，不复制实体基础定义或指标计算 |
| 实际字段/组件与标准的映射 | Catalog | 消费 Catalog 权威映射，不另存一份字段落标关系 |
| 实际字段/组件与本体属性的直接映射 | Catalog（待扩展） | 无 Standard/Model 时也可显式绑定；不是 Ontology 私有映射表 |
| 本体自有类、形式化关系特征、分类规则、来源对齐、修订与发布 | Ontology | 权威管理、校验及解释 |
| 本体类到业务图节点标签、属性名、边类型的执行映射 | Graph | 绑定 Ontology 修订；这是图构建执行配置，不是第二份本体定义 |
| 图谱实例、构建任务、实例抽取审核、图分析 | Graph | 不接管这些业务能力 |
| 数据结构、字段路径与结构覆盖率 | Meta | 经正式 owner 能力读取，不按名称或 locator 猜测字段 |
| 数据质量检查、评分、问题治理 | Quality | 本体分类不符合不等于质量不合格；不重复建设质量执行器 |
| 身份、授权、敏感数据保护 | System / Security / 数据出口 owner | 本体不能扩大权限，也不能绕过脱敏或明文授权 |
| 数据读取、查询与计算 | Manager / Develop / Service 等执行 owner | Ontology 不保存业务连接凭据，不另建 SQL/MQL 执行入口 |

### 2.1 有 Model 与没有 Model

已经存在的 Model Entity 以来源引用进入本体视图，其名称、属性和基础关系仍在 Model 修改。Ontology 只保存身份对齐和本体专有的继承、逆关系、分类条件等扩展。

未建设 Model 时，允许在 Ontology 创建自有类与属性，不强制先建设数仓、逻辑表或 Standard。对同一个受治理的概念，当前基础定义必须只有一个 owner；来源不能同时为“自有”与“Model 引用”。

后续若把自有概念纳入 Model，应经人工确认明确转移基础定义所有权、升级引用并重新发布；历史修订保留当时来源快照，但不保留两套当前可编辑定义。禁止按同名自动合并或双向同步。

### 2.2 数据映射不能复制

Catalog 的既有 StandardMapping 仍只表示组件到确定 ElementRevision 的映射，不把它改成任意关系容器。直接连接本体属性时，需要 Catalog-owned、类型化的本体属性映射，固定目标本体修订、属性身份和来源组件，并拥有审核与版本事实。

Model 已有概念实现映射仍由 Model 提供；Ontology 只保存或返回 owner 引用，不再复制表、字段、连接条件。两类映射按明确来源类型解析，不在缺失时互相猜测或回退。

语义定义发布不要求已经绑定业务数据。必须先有可引用的本体修订，再由映射 owner 建立映射，避免“本体发布依赖映射、映射发布又依赖本体”的循环。没有有效映射时仍可解释概念，但不能声称已能查询或判定真实业务数据。

## 3. 最小定义模型

以一个 Tenant 内的 Ontology 为定义聚合：稳定身份、稳定编码、可选的 Standard Domain 引用和资源并发版本。未建设 Domain 时允许租户范围建模，不在本模块复制业务域。

类、属性、关系和规则有聚合内稳定身份，作为同一 Ontology 修订的成员统一发布；首版不为每个成员再建立一套独立发布流程。

| 成员 | 必须明确的内容 |
| --- | --- |
| 类 | 稳定身份、语义名称、来源所有权、直接父类；可引用 Model Entity |
| 属性 | 所属类、逻辑类型、含义、可选的标准修订/单位引用；基础字段规则不覆盖来源 owner |
| 关系 | 两端允许的类、方向、显式逆关系及传递性声明 |
| 分类规则 | 目标类、布尔条件、类型化输入、缺失/未知策略、语义依据 |
| 来源对齐 | owner、来源类型、稳定身份、确定修订或版本、内容摘要及只读捕获 |

一个 OntologyRevision 冻结完整成员集合、依赖清单、规则语言/编译器版本和规范化内容摘要。草稿并发 `version`、业务 `revision_no` 和图投影构建批次不可混用。

外部 owner 如果只有可变资源版本而无不可变修订，应通过其正式接口读取一致的聚合内容并核对版本，捕获只读快照及摘要。没有可靠一致性读取契约时拒绝发布该引用，不以多次无版本 GET 拼成伪快照。

来源快照仅供历史解释与确定性执行，不接受单独编辑。源定义更新必须显式更新依赖、重新审核发布；不得后台改写已发布修订。源对象删除/撤回、权限撤销和安全策略收紧必须遵守原 owner 的约束，历史快照不是继续访问业务数据的授权。

## 4. 第一版语义边界

### 4.1 关系与约束

- 继承：首版拒绝继承环，计算有界祖先关系；同名属性的类型或来源冲突在发布前拒绝，不静默覆盖。
- 逆关系：只按明确声明生成反向语义，不从关系名称猜测。
- 传递关系：只有显式声明的关系才允许闭包推导，执行必须有深度、规模及成本上限。输出因上限不完整时必须标记，不能将未遍历到解释为不存在。
- 两端类型、属性类型与基数约束用于定义一致性及有完整事实时的符合性判断；成员集合不完整时不能证明“恰好一个”“没有关联对象”等否定结论。
- 首版不把本体约束自动变成业务库 DDL 或 Quality 检查方案。正式质量检查仍归 Quality，图引擎约束同步仍归 Graph。

FalkorDB 中主要是类型、规则、依赖和来源引用之间的语义关系，不是全部业务实例。声明“地点位于行政区”不代表数据库已经知道每个活动地点的行政归属。

### 4.2 CEL 分类条件

分类条件只有一份规范表达式。可视化编辑器可以帮助生成，但不能同时保存一份能独立修改的条件 JSON 与 CEL。发布时类型检查并冻结表达式及其编译环境；编译结果是可重建产物。

首个真实切片只需要布尔值、字符串、有限枚举集合与逻辑运算。扩展数值、日期、单位换算或集合运算前，必须先定义精度、时区、溢出、单位来源及成本边界；不得把泛化 `dyn` 或任意宿主函数作为绕过类型契约的入口。

禁止规则读取网络、文件、数据库、隐式当前时间或随机值；所需时点必须是显式输入。限制表达式长度、输入大小和执行成本，禁止未经审查的宏、递归和自定义执行函数。

CEL 负责有限事实上的条件求值，不负责读取业务表、构建 SQL 或证明任意本体公理。指标聚合、去重、连接和空间计算仍由相应执行 owner 完成。

### 4.3 未知不等于 false

区分输入事实状态与规则输出状态：

| 层级 | 状态 | 含义 |
| --- | --- | --- |
| 输入事实 | 已知值 | owner 提供了可信且类型正确的值 |
| 输入事实 | 已确认缺失 | owner 已完整观察该字段/关系，确认没有值；按具体业务规则处理 |
| 输入事实 | 未知 | 未读取、无权限、映射缺失、抽样不完整或尚未确认；不能擅自补零/false |
| 输入事实 | 无效 | 类型错误、来源矛盾或证据无法校验；不能作为正常值继续求值 |
| 分类输出 | matched / not_matched | 证据足以确定条件成立/不成立 |
| 分类输出 | unknown | 当前证据不足以判定 |
| 分类输出 | error | 类型、执行、授权或依赖等错误导致未完成求值；不是业务不符合 |

如果其他已知事实已足以决定布尔结果，可以在保留缺失证据的同时返回确定结果。例如活动已取消，即使日期尚未读取，也足以排除统计有效性。

“内部使用”不是把所有关系都当成完整闭世界的理由。完整性必须限定到本次查询范围、权限范围、字段覆盖和分页状态；完整地读到一个空集合与只读到第一页是不同事实。

## 5. PostgreSQL、FalkorDB 与发布

### 5.1 存储职责

PG 保存 Ontology 身份、工作修订、不可变发布内容、依赖捕获、发布审计、投影构建状态及当前激活指针。FalkorDB 保存由确定修订构建的关系投影与最小查询属性；图中不保存业务连接凭据。

首版每个 Tenant、Ontology、发布修订及构建批次使用服务端生成的独立 graph key。调用方只提供业务身份，不能直接选择 graph key 或执行任意 Cypher。物理分图有助于隔离，但不能替代服务端 Tenant 和资源授权。

规则求值从 PG 的确定修订加载权威定义；图查询返回成员身份和关系，必须能核对修订和摘要。禁止在 PG 与图中分别编辑规则，也不维护两个“当前版本”指针。

### 5.2 发布主路径

定义生命周期沿用 `draft → in_review → published → withdrawn`；只有草稿可改，退回审核恢复草稿。`published` 表示内容已冻结，不等于其图投影已经可用。

1. 校验聚合并发版本、引用权限、依赖一致性、继承/关系约束及 CEL 类型。
2. 在 PG 事务中冻结发布修订，并创建可恢复的投影构建 execution。发布记录与构建意图必须原子保存，不能先写图再补 PG。
3. Ontology Backend 的有界 Supervisor 通过现有 `common.task_executions` claim、lease 和 fencing 执行构建；首版不另起消息队列或第二套 Worker 路线。
4. 在新 graph key 中构建完整投影，校验成员数量、关系引用、修订摘要和确定性探针。投影状态为 `pending / building / ready / failed`。
5. 仅在投影 ready、发布未撤回、预期激活基线未变化且依赖准入仍满足时，在 PG 事务中切换 `active_revision_id + projection_generation`。并发发布不能由完成顺序决定谁覆盖谁；激活基线冲突必须显式重新确认。

本体当前激活指针是 Ontology 自己的运行时发布机制，不改变 Standard 按生效区间解析修订的规则。首版不引入预约生效或按请求隐式选择最新 published 修订。

### 5.3 故障、重建与撤回

- 新投影失败时，既有激活指针保持不变；请求明确指定失败的新修订时返回不可用，不能悄悄改用旧版。
- 同一修订重建只能使用其已冻结内容，新 generation 校验后切换；不能顺便读取最新 Standard/Model 定义改变语义。
- 请求第一次解析即固定修订和 generation。后续激活新版不使同一个请求混用版本。
- 所需图投影不可用时明确失败，不新增“FalkorDB 失败就走另一套 PG 图查询”的隐式旁路。
- 撤回修订后禁止新的执行消费，撤回当前修订时清除当前激活指针，不自动选一个历史版本。历史审计读取与重新执行分开授权。
- 已运行请求在输出结论或触发下游动作前复核撤回与授权；失效则停止，不把旧授权冻结为永久权利。
- 图清理只删除本 owner、明确身份且已无有效引用的 generation；保留必要的历史定义及审计，不清理用户业务库。Tenant 删除沿现有 Cleanup owner 机制处理。

### 5.4 PG 修订阶段的实施边界

本节记录 PG 修订内部切片的边界；其上的首次投影执行见 5.5，当前正式 HTTP 装配见 10.1：

- `ontology.ontologies` 协调同一本体的修订序号和激活指针；每次创建必须为上次序号加一，且不能存在另一份 draft / in_review。调用方明确提交目标修订号，冲突不自动重试。
- `ontology.revisions` 保存唯一的规范化定义包、摘要、状态和乐观锁 version。编辑、提交审核、退回、发布、撤回都要求精确 version；已发布内容不能修改，只能创建下一修订。
- `ontology.revision_events` 保存每次变更的主体、状态、版本及定义摘要，与业务写入同事务提交；数据库拒绝改写发布内容和既有审计。
- 发布原子创建 `common.task_executions` 的 `ontology / semantic_projection` bounded execution，绑定确定修订、摘要和初始 generation。只保存已认证调用方的 actor 事实，不伪造 System Execution Authorization；未获得后续授权准入的 execution 不得领取，不提供任意后台执行入口。
- 撤回取消尚未领取的构建意图，清空指向当前修订的激活指针并保留审计。已在运行的执行不能仅靠数据库改状态冒充取消；执行器在激活前复核撤回。当前没有图查询接口，不能把 pending 当作可用；失败投影重建见 5.6。
- 内部服务的 Actor 参数只承载调用方已核实的主体/租户/授权版本，不是鉴权器。用户入口必须经过 10.1 的正式 API 与 execution 授权链路，不直接暴露内部服务。
- 迁移由 `common/schema.Migrate` 协调 owner 版本化 SQL；仅只读要求已初始化的 common schema，不由 Ontology 初始化共享表。T2 夹具通过 common 的正式初始化能力准备共享执行存储。

首次图构建、激活按 5.5 实施；失败批次的新 generation 重建按 5.6 实施；更广泛的 ready 投影维护及历史清理仍按 5.2、5.3 另行实施。

### 5.5 首次投影执行与激活切片

本节记录首次执行内部切片：连接已授权的首次发布意图、Common 租约、FalkorDB 构建与 PG 激活。重建见 5.6，HTTP 装配见 10.1；历史图清理与 Agent 消费入口仍不在已交付范围。

- 发布事务创建 owner 投影记录，保存 generation、execution、摘要以及当时的 `activation_version` 基线。本体头只保存一个 active revision/generation；每次切换或撤回当前版本均递增 activation_version，避免空指针的 ABA 冲突。
- 有界 Supervisor 只在调用方明确报告 Ready 时领取；复用 Common claim/lease/终态原语。锁顺序统一为本体头、修订、投影、execution；claim 只锁 execution，提交后再进入 owner 事务，禁止倒置。
- 执行前恢复并核对冻结快照，消费 System 内部任务授权；只允许 pending 投影进入 building 一次。完成完整图校验后再次消费授权，提交事务复核授权响应期限、当前 lease、撤回状态和激活基线，原子保存 ready、激活指针、owner 审计及 execution success。
- 网络调用不在持有 PG 行锁时执行，避免 System 租约复核与 owner 终态事务互相等待。授权响应只是本次即时复核结果，不缓存为永久许可。
- 本切片不自动重放图写入。失败、取消、授权失效和过期租约都收敛为失败投影；保留原激活指针，部分图不删除、不激活。需要新 generation 的显式重建另行实现，不能重置当前 execution 或沿用旧授权来重试。
- owner 激活审计独立于定义修订审计，记录准确 generation、attempt 和发布主体，不复制用户 Token。撤回当前修订同步清空指针，不回退旧版本。

### 5.6 失败投影的显式重建

- 只重建仍为 published 修订的 failed 投影，且旧 execution 已终结并释放租约。请求必须携带修订 version、失败 generation 和明确确认的 activation_version；过期基线或重复重建冲突，不隐式重试。
- 同一失败 generation 只能产生一个后继；同一修订至多有一个 pending/building 投影。后继再失败时须明确选择该后继重建，不允许从历史祖先分叉。ready 投影的主动替换不属于本切片。
- 新投影、新 execution 和重建请求审计同事务保存，保留 predecessor generation；不改写冻结定义、原 execution、旧授权或旧图。修订记录上的 generation/build_execution_id 只证明首次发布时的构建身份，不是“当前投影”指针；后续准入和执行一律以 owner 投影记录为准。
- 首次构建和重建共用唯一准入与执行路径。准入显式指定 generation，以本次请求的 User Token 为新的 execution 签发授权；主体是本次已获发布权限的请求人，不借用原发布者身份。当前 Actor 必须与新 execution 中冻结的请求主体一致。
- 重建再次恢复同一冻结快照并校验摘要，不读取最新来源定义。只有新图完整校验、授权复核及激活基线检查均通过才切换；失败继续保留原激活版本。
- 撤回检查该修订的全部投影，原子取消尚未领取的重建，并阻断已领取投影激活；旧 generation 的迟到授权、终态回写不能影响新 generation。
- 本节仅定义内部重建能力，HTTP 入口见 10.1；仍不提供 Agent 入口或历史图清理。沿已有 Ontology T1、PG T2 和真实 PG/FalkorDB 联动 T2 验证，无新增外部依赖或 CI Job。

## 6. Agent 如何消费

### 发布运行时前置：内部执行授权（准入和首次执行已接入）

语义投影只读写 Ontology 的 Infra 存储，不访问用户业务 Engine。后续复用 System Execution Authorization 的签发、有效期、主体版本复核和审计，以及 Common 的 execution/lease/fencing；不新增 Token、登录方式或第二套 Worker。内部任务必须有明确且不可变的执行与操作范围，不能把空 Engine 列表解释为任意授权，也不能把 Infra FalkorDB 伪装成业务 Engine。

System 负责当前 User、Tenant Membership、授权版本、功能 Permission 及唯一 Runtime 消费身份；Ontology 负责本体资源权限、修订/摘要、发布/撤回和激活基线。签发成功并原子附加完整授权引用后才允许领取，构建前与激活前均复核；业务数据访问仍使用原有逐 Engine Access Scope。实施时必须同步 System 数据库不可变约束、API/Swagger、Common 客户端、权限登记和真实 IAM PostgreSQL 门禁；完整投影运行时仍须验证构建前与激活前的授权消费，不能用 Infra 或准入测试代替。

当前已增加互斥的 `internal_task` 范围、System 151/152 向前迁移、现有签发 API 的内部任务分支、精确租约消费 API、Common 客户端和 Ontology `AdmitProjection`。范围固定为 task_type/resource_id/revision/digest/generation，System 只核对 common execution 与 IAM，Ontology 核对自有修订及撤回。失败不自动重签或复用已关闭 execution；未附加授权不能领取。用户发布权限通过自定义角色显式授予，不自动扩大既有用户角色。首次执行器已接入构建前/激活前消费与 PG 激活指针；正式 HTTP 发布入口与常驻服务已按 10.1 装配，真实 T4 尚未通过。

### 6.1 不改变 Tool 架构

语义能力仍走现有主路径：Agent Runtime → Tool Adapter → ToolExecutor → Python SDK → Gateway / Ontology 正式 API。Graph Tool 不是一种替代 HTTP 的通信协议，也不是向模型公开数据库连接。

首版按三个职责组织能力，具体 Tool 名称、HTTP DTO、Scope 和输出 Schema 在 owner API 实施时同步定稿，本轮不向 Manifest 登记占位 Tool：

1. 解析概念：从当前用户可见本体中找候选、返回含义和来源；歧义返回候选供澄清，不自动选同名概念。
2. 获取语义上下文：固定修订，返回有界的相关类、关系、规则、所需属性和映射 owner 引用；不返回整张图。
3. 求值与解释：按确定规则和有界的已确认事实求值，返回结论、依据及缺口；不在模型上下文中执行任意表达式。

Skill 指导何时调用这些能力，现有业务 Tool 继续承担数据读取、查询和任务执行。LLM 不能自行生成 tenant_id、字段映射、规则阈值或未观察到的来源身份。

### 6.2 语义上下文与事实来源

首个只读消费切片固定两个 Tool：`ontology.classes.list` 枚举用户明确指定本体在读取时已激活的类；`ontology.class.context` 使用前者返回的 revision、generation、activation_version 和明确 class_id 读取该类上下文。不跨本体猜测身份，不从名称推断业务实例。正式路径为 `GET /ontologies/{ontology_id}/semantic/classes` 与 `GET /ontologies/{ontology_id}/semantic/classes/{class_id}`，后者三个版本 query 必填；未知或重复 query 拒绝。

两个入口只接受 Tenant User 或精确对应 Tool 的 Delegated Token，使用独立且可委托的 `ontology.semantic.read`，不沿用管理修订权限。管理路由仍拒绝委托令牌；不自动给已有角色增加权限。查询在同一 PG 只读快照中核对 head、published 修订与 ready 投影的身份/摘要，再从权威原生定义构造有界结果；不把 PG 定义读取描述成 FalkorDB 实例查询。输出固定本体、修订、generation、activation_version、digest 与 `knowledge_kind=native_definition`。未激活返回 409，固定版本已变化返回 409，跨 Tenant/不存在统一 404；读取时的绑定不保证后续调用仍激活。

类上下文包含该类、完整祖先、可用属性、以该类为显式端点的关系和直接绑定该类的规则；规则不隐式继承，关系不是实例边，不执行 CEL。无来源映射与业务事实时不宣称已验证真实数据。类目录最多 64 项，目录和上下文紧凑 JSON 最大 128 KiB，超限明确失败而非截断。SDK、Manifest、精确委托、Skill 与离线评测同次接入；真实身份 Online 验收仍单独报告。

一次语义上下文至少固定：Ontology 身份及修订、投影 generation、相关定义摘要、原 owner 依赖修订/版本，以及本次采用的数据映射修订/版本。映射由对应 owner 在运行时解析并固定，不反向修改 Ontology 发布包。

语义上下文不等于业务数据快照。业务取数另行记录 owner、结果引用（如有）、采集时点、查询范围、字段覆盖与分页完整性；源数据变化时不能只靠相同本体修订宣称结果可重放。

LLM 自填字段值只能用于明确标注的假设性规则试算，不能产生“已验证真实数据”结论。正式 Agent 链路的事实必须绑定本轮实际 owner Tool 结果，由可信 Runtime/ToolExecutor 从已观察结果构造，不能接受模型重写值或伪造出处。没有已实现的可信事实交接契约时，该真实数据求值能力不得上线；不为绕过此条件先建通用证据数据库。

Ontology 委托令牌的 audience 不能被直接转发给 Manager、Develop 或 Service。各 owner 操作沿自己的精确 Tool Scope 和短期委托令牌执行，不能用 Ontology 的宽权限 Service Token 代替用户的数据权限。语义定义可读也不意味着其所有来源数据可读。

### 6.3 解释输出

#### 当前规则试算切片

Console 在本体详情提供 `/ontologies/:ontology_id/trial` 页面，使用现有类目录与类上下文选择规则，不读取草稿、历史修订或业务数据。`POST /ontologies/{ontology_id}/semantic/rules/{rule_id}/trial` 仅接受 Tenant User 和 `ontology.semantic.read`；这是无持久化、无业务效果的定义消费，不扩展已有 Delegated Tool 的 scope，也不新增后台任务。

请求体固定为类目录返回的 `revision`、`generation`、`activation_version` 与按规则变量名索引的 `inputs`。每项包含 `state=known|absent|unknown|invalid`；非 known 的 `value` 必须省略或为 null。输入最多 16 项，请求最多 96 KiB；不接受表达式、租户、来源凭证或任意 query。未提交变量按 unknown 处理；未知变量返回 400，未知规则返回 404。已知值类型或枚举错误、显式 invalid 输入属于试算结果 error，不当作“不匹配”。

服务端复用唯一 CEL 内核，执行前后核对同一 published/ready 激活绑定；未激活或版本变化返回 409，不回退历史版本。HTTP 200 只表示试算得到结果，响应包含激活绑定、摘要及 `decision.mode=hypothetical`、规则依据、四态结果、输入状态和未决变量，不回显原始值，不持久化输入或输出。页面修改输入、规则、本体或重新加载时清除旧结果；冲突后保留输入但禁止继续试算，显式重新加载建立新基线。试算不是可信业务事实，也不是人数统计或质量检测。

该切片复用现有 Ontology T0/T1、PG T2 与前端 T3 标准入口；Swagger、请求边界、租户/用户权限、撤回/版本变化、四态输出及页面迟到响应隔离同次验证。不新增运行依赖、数据库、CI Job 或 Agent Tool；正式隔离 T4 仍单独报告。

令牌轮换后的 AuthContext 重取不是本体或规则切换。重取期间隐藏定义和输入、取消在途试算并清除结果；只有同一 User、Tenant 和 Tenant Membership 经权威上下文重新确认且仍有语义读取权限时，才恢复同一路由下的内存输入，不自动重新提交试算。换主体、换租户/成员身份、退出、权限撤销或重取失败均清空旧数据；仍由试算接口验证原激活绑定，409 锁定不会被普通令牌刷新解除。输入不写入浏览器持久存储。

解释是可核对的规则与事实，不是模型隐藏推理。最小内容包括：

- 本体修订、规则身份及定义摘要；
- 条件与来源依据，必要时包括受保护的来源引用；
- 决定结论的输入、输入状态与来源时点；
- matched / not_matched / unknown，或明确的执行错误；
- 缺失/未确认事项、数据范围与完整性；
- 使用了哪条显式关系或分类规则，而不是只输出“系统推断”。

无权访问的字段和值不能出现在解释、缺口说明或可观察错误差异中。工具超限应明确拒绝或沿 owner 的正式分页契约继续读取，不截断后宣称完整；分页后再做本地规则过滤不能冒充完整筛选结果。

AgentRun 只保存现有规范允许的紧凑事实与版本引用，不复制业务明细或整张图。新能力未定义正式 ResultRef 类型前使用 Manifest 的 `none`，不能从任意 ID 猜测结果引用。

Agent 取消仍遵守现有规范：取消 Runtime 不自动取消已有 owner execution。Ontology 的同步图查询应传播请求取消并有服务端超时；发布 execution 的取消由 owner 生命周期契约单独管理。

## 7. Graph 暂不调整，后续另行决策

当前 Graph 仍拥有自己的本体 CRUD。本阶段不修改其代码、接口、权限、数据和页面，不把 Graph 迁移作为语义内核建设的前置条件。Ontology 使用独立的自有定义与测试夹具，不复制、同步或双写 Graph 的当前定义。

以下仅记录后续讨论的迁移候选，须在 Ontology 初步成果验收后重新确认范围，当前不执行：

1. 盘点 `graph.ontologies / entity_types / relation_types / ontology_versions` 及所有前端、构建、导入、推导和知识服务消费者。
2. 将类、属性、关系定义及来源对齐收敛到 Ontology；将 NodeLabels、边类型、图属性名、节点展示字段等执行/呈现配置留在 Graph 的类型化映射中。
3. Model 导入改为引用/对齐，不复制可编辑实体；由图实例结构推导出的结果只能是待确认候选，不能自动成为权威业务语义。
4. Graph 图谱固定引用 Ontology 已发布修订；抽取出的实例候选仍由 Graph 审核，不转移到 Ontology。
5. 在受控切换窗口冻结旧本体写入，核对稳定身份、版本、属性及执行映射，处理冲突后统一切换消费者。
6. 删除旧本体写表、API、Permission、编辑页面及调用路径；历史内容如有审计保留需要，只作为不可变历史事实，不再参与旧运行时。
7. 如确认迁移，须通过唯一所有权与消费者回归测试；不维持双写或双版本 API。

切换前现有 Graph 模块文档仍描述当前实现。未来如确认切换，须同批更新 Graph、Ontology、概念架构图、权限与路由文档；不能把当前的暂缓决定解释为允许同一概念保留两个可编辑 owner。

前端应先检索并复用 `common-frontend/graph` 已有本体展示能力，按职责提炼编辑组合，不复制第二份相同契约的图画布。

## 8. 技术选型与实施准入

临时原型曾验证 Go 1.24.2、CEL-Go `v0.32.0`、FalkorDB Go 客户端 `v2.1.0` 和 FalkorDB `v4.20.6` ARM64 的有限场景。正式代码采用已锁定 CEL-Go 和基于 go-redis 的唯一 FalkorDB 薄适配，不引入原型 SDK。适配及定义图构建/校验由独占 T2 验证；单机 Infra 与 T2 共用固定版本服务定义，首次执行/激活组件已接入 PG/FalkorDB 联动 T2。当前已有 10.1 的管理 HTTP 入口、Console 建模页面，以及 6.2 的两个原生定义只读 Agent Tool。

正式实施必须满足：

- 锁定依赖版本及镜像 digest，与平台依赖规约协调；原型使用的客户端依赖 go-redis `v9.17.2`，与当前规约一致。
- 取消采用“请求停止使用结果 + 数据库有界终止”，不承诺即时硬中止。唯一适配基于平台已有 go-redis，逐请求独占连接；取消关闭该连接并拒绝返回迟到结果，不通过外层 goroutine 留下无人管理的查询。已经提交的读写查询始终带非零服务端 timeout，服务端同时启用 TIMEOUT_MAX / TIMEOUT_DEFAULT；超时回滚可能额外耗时，断连不等于写入未发生，禁止自动重放写操作。
- 首个适配切片将每条查询预算限制为最多 2 秒、每个适配器最多 4 个在途请求；容量满时立即拒绝。构建只能写未激活的独立 generation；取消、撤回、授权失效或租约丢失后禁止发布结果和切换 PG 激活指针。连接回收及超时只证明资源边界，不替代后续 PG fencing / IAM 准入。
- 验证所选唯一客户端路线的参数编码、并发安全、超时、连接回收及错误分类，再进入正式数据面；不在运行时保留多驱动回退。
- FalkorDB 作为 Ontology 的 Infra 私有依赖部署，单独配置凭据、网络边界、资源限额与健康检查；不复用现有 Redis 存储，也不自动注册为用户 Engine Instance。
- PG 定义为恢复权威；FalkorDB 可丢弃重建，但备份恢复必须同时保留定义、来源捕获、发布指针与重建能力。
- 注册、Ready、迁移、配置、端口和部署遵守现有模块规范。业务依赖 owner 不可达只阻断相应操作，不能要求 Standard/Model/Catalog 全部在线才能启动 Ontology。
- 对外错误、HTTP API、Swagger、Permission、SDK、Manifest 和前端调用在实施时一次同步；本轮不提前增加空模块或空路由。

## 9. Outdoor 首个验收切片

### 本地原生定义演示与真实业务的区别

2026-09-18 在个人开发环境通过正式页面创建并激活 `beijing_outdoor_demo` 修订 1；Agent 对话 76 的成功运行依次调用 `ontology.classes.list`、`ontology.class.context`，读取同一激活版本并解释继承属性、`registered` 条件和未知输入。该联调未读取业务数据库，未验证真实报名人数；当日 `make test-agent-eval` 离线门禁通过，不替代正式 T4。

演示定义来自合成夹具：`attendance` 表示“含成员状态的活动观察”，继承 `activity` 仅用于演示内核继承能力；它不能直接作为真实活动成员关系的业务模型。演示 `registered` 的三状态集合也不是下文真实 Outdoor 的完整报名口径。后续真实字段映射必须先明确活动、人员与成员关系的业务粒度，并采用已确认的业务口径；不得把合成定义直接绑定生产来源、猜测城市字段，或将演示结果描述成真实数据结论。

### 9.1 业务口径

以[Outdoor 领域理解](Outdoor领域理解.md)为依据：统计有效活动排除草稿、取消和无日期活动；报名与实际参加按已确认的成员状态分别计算。实际参加是现行业务统计定义，不额外声称存在签到证据。

2026-09-17 的仓库外只读聚合原型得到：

| 项目 | 数值 | 解释 |
| --- | ---: | --- |
| 活动总数 | 2383 | 只读业务快照 |
| 统计有效活动 | 681 | 与独立 MongoDB aggregation 一致 |
| 计入报名 | 5009 | 有效活动内的成员数组条目 |
| 计入实际参加 | 4886 | 有效活动内的成员数组条目 |
| 成员状态未知 | 15 | 有效活动内状态字段缺失，不猜测 |

这些是特定时间的数据观察，不是永久验收常量或正式发布指标。成员条目没有做人员-活动对去重；正式指标仍需要稳定身份、去重和 Model/Service 的完整执行契约。

业务数据以北京为主要背景，但本次统计未加地域过滤。“怀柔区位于北京”仅用明确标注的可丢弃夹具验证关系查询；北京、怀柔区是地点实例，不是两个本体类。真实活动行政归属要有可信地点映射，不能从数据集背景或标题猜测。

### 9.1.1 正式业务定义切片（不接 Catalog）

本轮按用户确认先忽略 Catalog，仅建设正式业务定义及其确定性规则用例；不实现字段映射、业务取数、聚合或可信事实交接。第 2 节的映射所有权保持不变，不在 Ontology 增加临时映射表。合成演示 `beijing_outdoor_demo` 保留；正式业务定义使用独立身份 `outdoor_business`，不把两者当作可互换版本，也不自动给 Tenant 创建本体。

- `activity`（户外活动）、`person`（户外参与者）、`activity_membership`（活动成员关系）是三个独立类，无相互继承。成员关系作为带状态的关联对象，通过 `membership_activity` 指向活动、`membership_person` 指向人员；声明仅约束类型端点，不证明已有实例关联、基数或唯一性。
- 活动具有字符串 `activity_status` 和布尔 `activity_date_present`。后者表达已经确认存在非空活动日期的观察，不是新增业务库字段；未来读取 owner 必须验证日期事实后构造，不能由 LLM、标题或字段名猜测。已知缺失、null 或空字符串对应“无日期”；未读取仍为 unknown，混合类型等无效事实不得当作 false。
- `valid_activity` 只绑定活动：有日期，且状态不是“拟定中”“已取消”。不把历史状态“报名截止”或“已结束”排除；活动状态不设置封闭枚举，以遵守已确认的“草稿/取消/其他”口径。
- 成员关系具有枚举 `member_status`：报名中、领队、领队组、替补中、占坑中、浏览中。`registered` 与 `participated` 只对该成员状态分类，分别采用第 9.1 节引用的五状态和三状态口径。缺失/未读状态为 unknown；未知码值或错误类型为 error，不静默转为 false。
- 成员规则不隐式遍历关系、不自动执行活动规则。统计消费必须另外证明关联活动满足 `valid_activity`，限定 `members[]` 范围并明确身份、去重与完整性；定义解释和假设性试算不能称为已核实报名、实际到场或统计人数。当前不扩展至 `addMembers[]`、`aaMembers[]`、签到证据或行政区判断。

正式定义用例位于 `ontology/backend/internal/semantic/testdata/outdoor_business.json`，语义内核 T1 检查类/关系粒度、完整状态矩阵、缺失与未知、无效输入及假设性解释。现有 Ontology Go 模块/CI 自动发现覆盖，无新增门禁或运行依赖；仍沿 `make test-module MODULE=ontology` 执行，不把定义用例当作真实数据闭环。

2026-09-18 已在个人开发环境通过正式页面完成 `outdoor_business` 修订 1 的创建、提交及发布，当前激活版本为 revision 1 / activation_version 2，generation 为 `34e81c3f-5e75-4451-9d48-70725a0e957c`。保存定义与测试夹具核对一致。Agent 会话 77 通过 `ontology.classes.list` 和两次 `ontology.class.context` 读取 `activity`、`activity_membership` 的同一激活版本，准确解释上述状态集合和证据边界；未调用业务查询工具。Ontology 模块门禁及 Agent 离线门禁通过。本记录是本地真实用户联调，不构成隔离 T4 或真实人数统计验收，也不代表其他 Tenant 已自动获得该本体。

### 9.2 验收问题

1. 为什么某条活动被纳入或排除统计有效活动？返回规则、状态与日期依据。
2. 为什么替补计入报名，但不计入实际参加？返回同一版本下两条规则及区别。
3. 缺少成员状态时如何回答？返回未知及缺口，不按数组位置猜测。
4. 没有读取日期与明确缺少日期有何差别？验证事实完整性和缺失策略。
5. 找不到可信的北京行政区映射时如何回答？要求补齐映射，不虚构地域结果。
6. 发布新版本、撤回或取消查询时，是否保持版本一致、权限有效和资源可回收？

已有原型只证明 Go/CEL 分类、图定义读取、有限关系、版本条件与超时可行；没有证明正式 PG 发布、IAM、可信事实交接、Agent 端到端效果或 FalkorDB 相对其他引擎的性能优势。

## 10. 实施切片与门禁

1a 语义内核、1b PG 修订/发布、1c-1 FalkorDB 投影适配及 1c-2 的首次执行/失败重建、Backend 管理入口已实施。首次发布和重建共用新 execution 的授权准入、Common 租约、确定性图构建、全量校验和 PG 原子激活，不存在绕过准入的构建旁路。常驻 Backend 位于 `ontology/backend/cmd/server`，管理路由位于 `internal/api`；端口、构建、部署、权限与开发生命周期同步登记。owner schema 由向前迁移管理；本轮未在个人开发库执行迁移或重启服务。FalkorDB 单机 Infra 与独占 T2 共用定义。原生定义的只读 Agent API/SDK/Tool/Skill 和离线评测已接入；真实 System/Gateway 的 T4、来源引用、关系实例推导、历史图清理及真实数据证据消费仍未完成。

内核输入属于假设性试算，不验证业务 owner 证据或 IAM；调用方传入的租户/修订身份匹配检查不等于授权。第 6.3 节的用户试算接口由 Ontology owner 统一鉴权，只接受手工假设，不认证业务事实。真实业务事实求值接口上线前仍必须满足第 6 节的可信事实交接与授权要求。

后续按以下依赖顺序实施，但不能把同一切片所需的 CI/测试登记留到以后：

| 切片 | 必须同时交付 | 最小充分验证 |
| --- | --- | --- |
| 1a. 语义内核（当前） | 原生定义、继承/关系声明校验、不可变快照与摘要、受限 CEL、四态事实与解释、依赖规约和模块文档 | `make test-module MODULE=ontology`：T0 一致性和自动发现；T1 类型/未知/继承/不可变性/规则预算/取消；现有 CI Go 测试自动发现 |
| 1b. PG 修订与发布（当前） | owner 迁移、乐观锁、审核/冻结/撤回、原子 pending execution、审计；标准 PG 门禁和 CI 登记 | T0 登记；T1 快照恢复与状态输入；T2 迁移、并发冲突、原子回滚、租户隔离、冻结保护与撤回 |
| 1c-1. 图适配准入 | go-redis 唯一适配、请求取消/服务端有界终止、原生定义投影构建与全量核验、独占 FalkorDB T2 和 CI 登记 | T0 自动发现与退出安全；T1 参数/身份/取消；T2 投影、读写超时/回滚、连接回收和并发 |
| 1c-Infra. 单机部署 | 正式/T2 共用定义、独立凭据与卷、回环端口、图健康检查、部署脚本与影响登记 | T0 配置一致、Secret 拒绝与门禁清理；T2 容器重建后的图快照恢复；不宣称生产 HA/TLS 认证 |
| 1c-2. 发布运行时 | 授权准入、lease/fencing、构建恢复/激活/重建；实际服务、Infra、权限和 CI 登记 | T0 登记一致性；T2 发布恢复、重建、引用撤回与取消；正式入口按 T4 验证 |
| 2. 建模入口及 Graph 边界确认 | Ontology 初步成果验收后讨论编辑界面、来源引用及 Graph 是否/如何切换；不预先改造 Graph | 按确认范围交付 owner 唯一性、迁移及消费者回归门禁 |
| 3. 真实数据与 Agent | Catalog 类型化本体映射/Model 来源解析、可信事实交接、owner API/SDK/Tool/Skill/评测同步 | T1 委托、错误与输出上限；T2 跨版本绑定；T4 正式用户授权与 Outdoor 对话闭环 |

1a 不对外开放编辑入口；1b 不以 Graph 改造为前提，但不能导入其定义形成双 owner。第三切片不得通过原型直连 MongoDB 或临时 Go 程序作为产品旁路。

新模块必须验证根 `make test-module MODULE=ontology`、`make test-changed` 和平台自动发现能够命中。新增 PG/FalkorDB T2 必须同步登记标准入口、服务依赖、串行集成聚合和 CI Job；FalkorDB 是新的依赖类型，需要相应服务定义和一致性检查覆盖，不能只加测试文件。

本地共享 PostgreSQL 测试只允许 `addp_test` 与 `addp_iam_test`，通过标准入口隔离 owner schema，不创建一次性命名 database。FalkorDB 使用独占、固定镜像的 disposable 服务，失败和中断都清理并核验残留，不使用个人开发数据。

Agent 评测沿现有 `evals/agent-scenarios/` 和 `make test-agent-eval` 扩展；正式在线验证走已登记 T4，不新建第二套评测框架。用同一模型、同一数据快照与同一授权比较“Skill + Tool”与“增加领域语义”两组，至少覆盖正确率、无依据断言、澄清、版本依据和权限拒绝；不只看回答是否流畅。

### 10.1 Backend 管理入口

Backend 使用 Go/Gin，端口 8195，唯一 API 前缀 `/api/v1/ontology`，不改 Graph。管理路由仅接受当前 Tenant 的 User Access Token；主体、成员、租户和授权版本从 System AuthContext 取得，禁止请求体自报。只有 6.2 的两个只读定义入口允许精确 Tool 的 Delegated Token；不开放 Service Token 或任意 Cypher。

`ontology.revision.read` 读取本体头、确定修订与确定 generation；`ontology.revision.update` 创建/保存草稿、提交审核、退回；`ontology.revision.publish` 发布/撤回，发布和失败重建同时要求 `system.execution_authorization.create`。Permission 均为 Tenant scope，自定义角色显式分配，不扩张既有用户角色。`platform.ontology_runtime` 仅用于自身模块注册，Tenant Runtime 权限保持不变。

- `GET /ontologies/{ontology_id}`：返回 last_revision、activation_version、active_revision/generation，不把 published 当作 active。
- `GET /ontologies`：当前 Tenant 的本体头分页列表，固定按 ontology_id 升序；不按名称猜测或跨租户发现本体。
- `GET /ontologies/{ontology_id}/revisions`：当前 Tenant 下确定本体的修订摘要分页列表，固定按 revision 降序。返回身份、version、status、digest、首次发布 generation/execution 与时间，不加载或返回定义 payload；完整内容仍由确定修订详情接口读取。不存在或属于其他 Tenant 的本体统一返回 404。
- 两个管理列表仅接受 `page`、`page_size`，默认 1/20，每页最多 100；非规范正整数、重复/未知参数及超过有符号 32 位 OFFSET 范围的请求返回 400，不静默改写参数。响应复用 `data/total/page/page_size/total_pages`，越界页为 `data: []`；每次请求在同一只读 PG 快照内读取 total 和当前页，不承诺多个请求之间冻结列表。沿用 `ontology.revision.read`，不开放 Delegated Token 或增加新的权限。
- `POST /ontologies/{ontology_id}/revisions`、`GET/PUT /ontologies/{ontology_id}/revisions/{revision}`：请求只提交原生定义成员，Scope 由路径、修订号和认证事实组成；保存携带精确 version。
- 修订下 `POST /submit`、`/return`、`/publish`、`/withdraw`：精确 version，不接受内容；`POST /rebuild` 另带 failed_generation 和 activation_version。
- `GET /ontologies/{ontology_id}/projections/{generation}`：仅返回当前 Tenant/本体的投影身份、状态、前驱和激活基线，不暴露授权、lease token 或图物理 key。
- `GET /ontologies/{ontology_id}/revisions/{revision}/projection`：按重建前驱链读取该修订最新一次投影（不是 active 指针），便于发布/重建响应丢失后找回任务身份；没有投影返回 404。按无后继节点确定，不按客户端时间猜测或回退首次 generation。

发布/重建先提交 PG 意图，再在同一请求栈内签发并附加授权，成功返回 202（仅表示已准入，不表示 active）。准入失败返回 502 和稳定 `projection_admission_failed`，同时返回已提交的 revision/version/generation/execution_id；调用方必须按身份重新查询，不得盲重试发布。原修订不会因准入失败解冻，后续遵循显式失败重建。请求/进程中断产生的未准入 pending 不被领取，当前不自动补签或重放，可撤回修订；无授权 pending 的自动超时收敛不在本切片范围。

Backend 使用统一 Lifecycle，绑定监听后异步注册；Ready 同时要求 PG、带非零查询上限的 FalkorDB 和 System 注册有效。Supervisor 只在 Ready 时领取；退出停止领取、取消并等待在途执行，限时关闭 HTTP，再等待注销。部署仅使用独立 `ONTOLOGY_SERVICE_CLIENT_SECRET`、`INFRA_FALKORDB_PASSWORD` 与部署地址，不注册 Business Engine。

最小门禁为 T0 权限/Swagger/构建与生命周期登记、T1 正式路由鉴权与严格 DTO、T2 PG/FalkorDB 原有状态机及读取隔离。真实 System/Gateway 跨进程体验另需 T4，不以进程内认证夹具冒充已上线。

## 11. 相关事实源

- [术语表](../concepts/addp术语表.md)、[核心概念关系图](../concepts/addp核心概念关系图.md)、[模块架构图](../concepts/addp模块架构图.md)。
- [Standard 模块](../../standard/CLAUDE.md)、[Model 模块](../../model/CLAUDE.md)、[Catalog 模块](../../catalog/CLAUDE.md)、[Graph 模块](../../graph/CLAUDE.md)。
- [Agent 模块](../../agent/CLAUDE.md)、[Tool 开放规范](../spec/addp智能体Tool开放规范.md)、[智能体评测规范](../spec/addp智能体评测规范.md)。
- [技术栈规约](../spec/addp技术栈规约.md)、[开发原则](../spec/addp开发原则.md)、[测试与验收规范](../spec/addp测试与验收规范.md)、[新模块开发指南](../spec/addp新模块开发指南.md)。
- [FalkorDB Go 客户端 v2.1.0](https://github.com/FalkorDB/falkordb-go/tree/v2.1.0)、[CEL-Go v0.32.0](https://github.com/cel-expr/cel-go/tree/v0.32.0)。
