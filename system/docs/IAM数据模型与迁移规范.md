# System IAM 数据模型与迁移规范

更新日期：2026-10-28

状态：System 模块正式实现规范。本文定义已投入运行的 IAM PostgreSQL 模型、事务边界、迁移路径和安全约束。

租户功能权限收敛：Role Assignment 只支持 Platform / Tenant，租户角色与分配请求不接收组织范围字段。旧部门／项目组分配先保留完整审计再退役，不升格为租户授权；范围目录、AuthContext 与数据库约束同步收敛。部门、项目组、部门层级、账号与全部组织成员关系保持原事实，源数据 Grant 与 Catalog 业务授权不在此次 IAM 收敛中改写。

## 直接授权许可迁移计划（2026-10-28）

状态：直接许可持久模型、签发校验、API、独立 Tab、部署登记和一次性历史退役已实现，互斥模式运行路径已删除。迁移及受影响授权链路的本地 PostgreSQL 门禁通过，System／Catalog 前端标准门禁通过；完整 IAM 包另有两个未改动用例因认证时间在未来失败，不计为整包通过。2026-10-28 用户整栈重启后，只读确认开发库 System=248/dirty=false、Catalog=1、Transfer=7，旧授权及办理历史归零。固定 `c8977cecb` 的 PG Hosted T4 已通过 93 项检查及三段浏览器，清理零残留；开发浏览器的当前／历史归零、直接许可过滤及选中项许可展示也已只读核实，未提交新的许可或 Grant。见[本轮验收记录](../../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#直接授权许可模型替换--pg-hosted-t4-与开发库迁移)。以下定义唯一新协议，旧模式仅作为前向迁移输入。

1. **前置清点。** 只读核对旧目标身份、模式、版本与审计来源、现有直接／业务 Grant、撤销和 Deny、未核清业务请求及正式受理窗口，以及旧配置权限在内置与自定义角色中的分布。旧 `independent` 记录可能由普通办理员首次建立，不能据模式推断管理员曾批准。迁移只走新的前向迁移，不改旧 migration checksum，不在运行中的个人开发库手工清理授权。
2. **已确认的存量映射。** 旧 `independent` 配置全部转为“允许直接授权”，旧 `catalog` 配置转为“未允许直接授权”；无配置目标不创建显式记录，按 Catalog 持久启用状态计算默认值。旧 `catalog` 迁移的显式禁止仍优先于未启用 Catalog 时的默认允许。用户已明确接受旧直接配置的延续，包括可能由普通办理员首次建立的配置。迁移记录真实的存量映射依据，保留配置身份、版本及既有通用审计，不伪造一次管理员开启操作。迁移后所有新的开启／关闭仍仅允许引擎授权管理员。2026-10-28 用户进一步确认：旧授权历史可以统一清理，包括此前两项持续授权；本次一次性清理不保留这两项例外，不自动重新授予。
3. **事实模型。** 新可变事实仅表达精确 DataItem 的显式直接授权许可及并发版本；记录缺失表示使用部署默认，不另存逐项默认值副本；禁止继续以 `catalog/independent` 二选一表示许可。迁移须保持 DataItem 身份无损，不根据表名猜测身份，不把目录／schema 扩展成许可。新 Grant 的批准来源和命令仍是不可变事实；不得把旧配置映射伪造成管理员操作或新 Grant。清理旧历史后直接移除旧批准要求版本与首次初始化字段，不保留历史兼容 DTO。
4. **角色与权限。** 直接授权、业务批准后办理、许可读取和许可管理分别校验。许可管理默认只授予引擎授权管理员，并在后端同时核验管理员资格；仅组合配置 Permission 或持有引擎委托不能绕过。普通办理员移除旧首次配置能力；不能把其旧 `.initialize` 自动升级为新许可管理权。自定义角色旧配置权限也不得自动升格。沿既有 Manifest／前向迁移／授权版本及会话失效机制发布，不新增真实账号或角色分配。
5. **业务协议收敛。** 新 Catalog 确认、准备及 System 新受理不再依赖目标批准要求模式与版本；删除 Catalog 的模式观察、初始化和模式冲突处理，以及统一 client、DTO、Swagger、前端中对应新命令字段。原受理、核清、签发、审计和不可变参数约束继续由各 owner 负责。本次开发阶段升级在停止相关写入及后台续办后，由各 owner 的一次性迁移清理旧办理记录及依赖；不得让旧进程继续签发。清理不伪造一次正常核清或业务撤销，不重新发起旧请求。
6. **已确认的开发历史清理。** 清理旧数据 Grant（含两项持续授权）、撤销、Deny／解除、旧业务办理及受理记录；不重新签发、不保留旧字段兼容。配置映射仍按第 2 项执行。System 只修改自身领域表，Catalog 只清理自身旧授权办理／批准记录，不跨 schema 操作；IAM 账号、角色分配、组织成员、引擎委托、Meta 采集资格、目录条目／责任／来源绑定、通用审计及源数据不在清理范围。任务产出的物理成功与成果数据保持不变；旧初始授权续办须同时退役，不能因 Grant 记录消失再次签发。
7. **页面及查询。** 独立“直接授权许可”Tab 使用 DataItem 树，复用共享 ResourceTree 渲染。既有身份清单附加 Meta 权威祖先节点链（仅节点 ID、名称、类型，不含属性或内容），由同一只读快照核对租户、引擎、父子关系和删除状态；缺失／循环父链明确失败，不靠 full_name 解析。System 原读取权限和机器身份不变。前端完整分页收集（上限 20,000 项）后渲染树并支持显示筛选；当前节点可仅直属或递归选择。提交沿精确 DataItem 既有 PUT，逐项独立并显示部分结果，冲突／未知须显式复核后再确认；原有逐行设置弹窗退出。新增授权仍仅显示选中项许可，查询和批量设置不产生 Grant，不改变已有 Grant／Deny。
8. **部署事实及初始化。** 已确认复用 System `module_definitions` 的 Catalog 持久 `enabled`，沿现有平台模块管理权限及并发版本更新，不新增第二套部署开关。新部署初始化显式登记启用／未启用，升级保留已有持久状态；缺少记录保持未知，不能因未注册、网络故障、租约过期或 URL 缺失推导未启用。部署登记前不能按默认允许签发。平台模块停用操作必须清楚提示未配置目标的默认许可变化；首次注册与重注册不得覆盖明确的部署意图。持久启用状态变更与默认许可签发须在数据库中排序，适配现有注册、初始化、平台模块页面与相关门禁。
9. **一次性交付范围。** 涉及 System 数据迁移／授权事务／模块部署登记及管理／Manifest／API／前端，Catalog 办理协议／前端，Common 统一 client、协议绑定及相关消费者、原 PG Online 夹具与场景规范。逐一核对实际引用，不扩展新的引擎适配，不修改 Provider、Security 或审计方式。实施前确认根 Make 与既有 CI 路径登记覆盖；接口变更同次更新生成常量及双语 Swagger。

### 许可解析、部署登记与事务顺序

| 精确 DataItem 显式值 | Catalog 持久状态 | 有效直接授权许可 | 展示来源 |
| --- | --- | --- | --- |
| 允许 | 启用／未启用／尚未登记 | 允许 | 显式设置 |
| 禁止 | 启用／未启用／尚未登记 | 不允许 | 显式设置 |
| 无记录 | 启用 | 不允许 | 部署默认 |
| 无记录 | 明确未启用 | 允许 | 部署默认 |
| 无记录 | 尚未登记或读取失败 | 未知，拒绝新直接签发 | 部署状态未知 |

已知显式设置优先，不要求 Catalog 在线；获取显式设置自身失败也必须拒绝，不能当作“无记录”继续用部署默认。两类版本分别表达 DataItem 配置和模块定义，不能把默认允许伪造为许可版本 1。精确观察返回有效状态、来源及实际参与裁决的版本；签发须在事务内重新读取并核对，观察结果不是授权凭据。

部署登记沿现有 System 平台模块管理资源完成；首次登记是创建操作（初始持久版本为 1），不得把更新版本 0 或省略版本作为隐式创建。已登记后沿原带版本的启停操作更新。重复创建、与首次服务注册撞名均返回冲突并要求重新读取，不能覆盖已有启用意图；未启动的 Catalog 定义不伪造 Runtime 实例、地址、心跳或 Ready。复用原 `platform.module.update` 管理资格，不给 Tenant 办理员新增平台权限。注册与重注册保留已登记启用意图；没有模块记录时，查询只返回未知，不通过读操作补建默认值。

精确签发裁决优先读取显式记录，无记录时才以 Catalog 持久状态为依据；列表在同一数据库语句快照读取显式集合与部署事实。新直接签发持有精确目标锁，并对参与默认值的 Catalog 定义加共享锁，持有至授权事务提交，防止状态改变后仍按旧默认值写入。模块启停使用现有定义行更新锁；首次登记通过唯一模块名约束排序。未知状态不签发，不能通过锁不存在的记录证明未部署。许可变更与签发统一保持 IAM／引擎／精确目标的既有锁顺序，不在持锁时请求 Catalog HTTP。

配置映射与一次性历史清理是两个独立步骤：先从旧配置读取精确身份与允许值，再退出旧模式字段；不从 Grant 推导许可。历史清理只用于本次开发阶段升级，不成为运行时清理 API 或授权捷径。旧进程必须退出后才升级，数据库 owner 迁移完成后才能恢复签发与后台续办。清理完成后旧请求编号不得从旧 Catalog 或 Transfer 待处理记录中自动重放。

### 开发历史退役的 owner 执行边界

System 前向迁移 `000246` 在同一事务清理旧 Grant／撤销、Deny／解除及业务办理受理记录，包含原两项持续授权。只在本次迁移事务内暂停目标表的领域不可变触发器，保持外键核验并在提交前恢复保护。旧任务产出签发终态保留执行标识作为拒绝重放依据，去掉已清理 Grant 的引用；不能因删除历史重新获得首次签发机会。

Catalog 复用共享启动迁移检查点，revision 1 一次性清理自身 `sharing_decisions` 与 `fulfillment_checks`；后续启动不再清理新批准。Transfer 启动版本 7 将旧成功执行的初始授权续办标记终止，移除旧 Grant 引用；后台不再领取，产出回执接口拒绝以该旧执行继续办理。执行物理结果、成果数据、其他元数据与既有来源证据保持不变。页面将该状态显示为“旧初始读取授权已清理”，不误报成首次未签发，也不自动重跑任务。

以上是新许可模型发布时的一次性升级步骤，不提供人工清理 API，也不是周期清理。三个 owner 的旧写入／续办进程全部退出后才能统一升级；不得单独重启某个新 owner，同时保留其他旧签发进程运行。本地隔离库已验证清理、重复启动及非授权事实保持；开发库尚未执行，真实升级／跨服务 T4 未验收。

### Catalog 业务办理的协议切换

新业务办理只提交不可变的 `request_id` 与 `decision_id`，不再接受 `requirement_version`；统一绑定和公开办理历史也移除该字段。Catalog 页面删除模式观察及首次配置入口，仍核查候选、已有有效授权、当前办理资格和原参恢复。System 受理保留当前人员、引擎范围、业务依据、Meta 边界、接收主体与期限校验，不读取直接配置。迁移 `000247` 在升级中间阶段将 Catalog Grant 的旧版本列固定为 0；`000248` 随后移除该列和首次初始化字段，改为仅直接来源保存 `direct_permission_source/version`。Catalog 与任务产出来源的这两个字段固定为空字符串／0，不能成为业务准入条件。

前置 Catalog 解耦验证曾覆盖无配置及旧直接／业务配置下均可受理，相关 Go、PostgreSQL、前端和 runner 结果仅属于当时输入。新许可模型完成后必须复核缺失／关闭／开启许可下 Catalog 均可办理，并保持直接签发、同参恢复、去重和清理断言；以本节后续标准门禁的新结果为准。

### 有效许可列表的跨服务身份查询

2026-10-28 已确认独立机器权限 `meta.data_item_identity.read`，默认仅授予 `tenant.system_runtime`；Meta 同时限定 `addp-system` 的当前 Tenant Service 身份。人员、Catalog 运行身份及原 `meta.authorization_facts.read` 不因该变化获得枚举能力。

Meta 的唯一最小清单接口为 `POST /api/v1/meta/runtime/data-item-identities/query`，经统一 `MetaClient` 调用。复杂查询在请求体携带 `engine_id`、`search`、`fingerprint_filter`（`include`／`exclude`）及 `fingerprints`、`page`／`page_size`；不存在 Tenant 覆盖字段。无集合过滤时省略过滤字段；显式 include 空集合产生空结果，不能变成查询全部。集合只承载本次查询的身份谓词，不保存 System 许可，不接受权限、角色或 Grant。单次最多 20,000 个唯一指纹，服务端拒绝超限或畸形请求，不能截断后声称完整；System 必须显式报告查询无法完成，不能遗漏默认允许或显式禁止的项。

结果只包含当前未删除 DataItem 的 `item_fingerprint`、`engine_id`、`name`、`full_name`、`item_type` 与标准管理分页信息。Meta 在同一只读快照中先过滤再计数和分页，按指纹稳定排序；不读取源、不扫描、不返回样本、字段、完整 attributes 或凭据。身份清单不证明可授权边界；实际许可配置与 Grant 签发仍通过既有精确授权事实接口核验。

System 读取当前显式许可与部署默认，按最终状态推导包含／排除的身份集合；无状态过滤时查询全部，再对当前页组合相同观察的许可。跨服务查询期间不持有 IAM／许可事务锁；返回前重新核对资格与本次配置观察，变化则报告冲突，不能混用不同配置版本。Meta 不参与许可裁决；System 不持久复制 DataItem 清单。名单查询失败不能被解释为空列表。

### 一次性清理与新写协议的落地检查

- 旧可变批准要求迁移为精确 DataItem 显式许可，保留稳定身份、指纹、版本；旧互斥模式只作为迁移输入，不保留为当前配置字段。
- 旧 Grant／撤销、Deny／解除、System 受理历史按依赖顺序在同一 owner 迁移事务清理，正常不可变保护随后恢复。禁止 `CASCADE` 扩散到未授权对象；循环外键必须显式核对并使用原可延迟约束，不关闭全库参照完整性。
- Catalog 在自己的迁移中清理旧批准和办理队列，删除旧请求版本字段及模式初始化入口；不删除条目、责任、来源、编目信息或通用审计。新 Catalog 命令只根据正式业务依据办理，不查询直接许可。
- 任务产出初始授权需要单独核对 System 终态与 Transfer 待处理标记：已成功生成的表、任务与执行事实不删除，不重跑计算；旧授权终态不能因清理变成可再次签发的首次请求。保留必要的拒绝重放事实不等于保留旧 Grant 或旧协议。具体清理语句须经依赖核查与隔离测试，不能仅清空 Grant 表。
- 新直接命令必须提交实际观察的许可来源及版本；正式命令在事务内复核当前 IAM／引擎范围、接收主体及已核验 Meta 边界，数据库 INSERT guard 同步核验许可来源／版本、操作人和接收主体的结构约束，不通过旧批准要求字段旁路。
- T2 必须覆盖旧配置映射、旧授权及续办清理、重复启动不再清理新记录、旧请求不恢复授权，以及未纳入清理的 IAM／组织／委托／采集资格等事实保持不变；不在个人开发库试迁移。

当前本地验证：`make test-authorization` 及 Common／Meta／System／Catalog／Transfer 受影响 Go 门禁通过；标准 PostgreSQL 门禁覆盖 System migration、engineaccess、repository、api、online-test-fixture、Meta authorization-facts、Catalog 与 Transfer，均通过。包含许可关闭／默认值变化与签发的四个真实并发顺序、迁移映射与不可重放、权限发布和最小身份清单隔离。System 前端 175 项单测、245 项隔离浏览器及构建通过；Console 207 项单测、239 项隔离浏览器及构建通过，两个宿主授权夹具已删除旧协议。Catalog 270 项测试及构建、共享前端 283 项测试、Transfer 受影响前端及构建通过。完整 IAM PostgreSQL 包的两个既有当前角色用例报“authenticated time must not be in the future”，相关文件本轮未改；本轮涉及的 MFA／会话清理用例通过，但该整包结果仍为失败。以上为本地及隔离数据库／模拟 API 浏览器证据，不替代新协议真实跨服务 T4 或开发库升级。
主链路验证范围与标准入口：

- T0/T1：`make test-authorization`、System 与 Catalog 的标准模块门禁、Common 受影响客户端及协议门禁；前端覆盖独立 Tab、状态筛选、精确查询、空选择零全量读取、未知状态拒绝提交及旧入口删除。纯规范阶段先运行 `make test-changed`，不能把文档门禁计为功能验收。
- T2：既有 System IAM PostgreSQL migration／engineaccess 与 Catalog PostgreSQL 标准入口，覆盖旧 Grant／Deny 清理且重复启动不删除新记录、旧普通办理员不获开启权、管理员开启关闭、业务流程在许可缺失／关闭／开启三态均可办理、Catalog 持久启用／未启用与显式允许／禁止／未配置的组合、未知及临时故障不放开、启用变化只影响未配置目标、部署默认值变更与签发并发排序、并发关闭与直接签发顺序、版本冲突、同命令恢复与重复关系拒绝、旧协议退出及续办退役。测试仅使用标准入口隔离库。
- T3：System、Catalog 前端构建及 Swagger 路由覆盖；Common 契约变更按依赖扩散。复用根 Make 和现有自动发现，不另建临时测试入口。
- T4：扩展既有 `pg-source-authorization` 隔离验收，验证新 Tab 的真实筛选及选择后状态、未许可直接授权拒绝、管理员许可后办理、许可关闭后拒绝新增、Catalog 两种许可状态均可业务签发、原 Grant 在关闭许可后仍可读、撤销与失败清理。先运行 `make test-pg-authorization-online-runner`，本地通过后才派发一次必要固定版本验收；已获准清理的两项旧持续授权不再作为保留例外，但仍不使用真实账号作验收夹具，不新增定时跟进。

## 一、事实边界

System 是 ADDP 唯一 IAM 逻辑权威，负责 Principal、账号、认证方式、Tenant Membership、组织关系、Role/Permission、会话、OAuth 协议事实和 IAM 安全审计。

业务资源和资源级授权事实仍归对应 owner。System 不复制全平台业务资源，也不建立中央资源 ACL 大表。

DataItem 授权资源事实由 Meta 的 Runtime API 提供，System 不跨 schema 查询。迁移 000205 登记不可委托、不可租户定制的 `meta.authorization_facts.read` 并关联 `tenant.system_runtime`；前向迁移 000206 将同一读取 Permission 关联 `tenant.catalog_runtime`，两者均用 `common/client.MetaClient.ReadAuthorizationFacts` 及自身 Tenant Service 凭据消费唯一契约。不新增人员 Role Assignment 或数据 Grant；现有 Role Permission 触发器推进受影响服务主体的授权版本，迁移显式撤销其旧 Token Family，不重复手工推进。管理员核对、新批准及正式读取消费在 IAM 锁外读取 Meta，随后在本地事务中重新核验当前资格。000207 将首期 schema 普通表 single 的 Grant 改为 `item_fingerprint + boundary_hash` 绑定；`catalog_path` 仅保留实际定位与批准时边界／审计坐标，不是独立 ACL 键。

System 自身拥有引擎访问控制领域，`engine_access_grants` 是精确源数据规则的唯一不可变签发存储。`request_id` 唯一标识命令；`approval_mode` 仅记录 `catalog/independent/task_production` 签发来源，不表达 DataItem 互斥模式。Catalog 来源以同编号 `catalog_request_id` 引用 accepted 回执并提取规则，核验原窗口；直接来源不伪造业务回执，以 `direct_permission_source/version` 记录许可观察并在新签发事务内复核；任务产出沿独立已确认规则办理。读取及撤销只消费 Grant，回执与许可配置均不能直接授予访问。正式 Catalog 签发／反查使用固定 Runtime 端点，直接授予使用 `POST /api/v1/system/engines/:id/access_grants`，当前关系和完整历史分别用 `GET /access_grants` 与 `/access_grants/history`。创建、读取和撤销分别校验独立功能权限及当前引擎管理资格；Meta 边界／源结构 RPC 不跨 IAM 锁，事务内重核资格并原子写入 Grant 与审计。原编号同参只恢复原历史，不续期、不恢复撤销；历史签发不替代后续到期、Deny、接收主体及 Security 访问裁决。

当前源数据读取规则由 `engineaccess` 私有只读批量观察方法复用上述不可变历史，不新增 ACL 表或可编辑副本。该方法拥有独立只读事务，拒绝嵌入调用方写事务；整批目标在一条 SQL、一个已提交快照和一个数据库时刻下核验当前主体、直接组织成员、引擎与 Grant／Deny。仅全部精确目标都有有效 Grant 且没有有效 Deny 时返回规则覆盖，不读取来源数据库、不获取仲裁或 IAM 写锁、不产生规则或审计副作用。规则观察本身不独立认证，也不是完整访问许可；正式同步入口按下述契约组合可信凭据与固定功能／Client 条件。适用的执行范围与 Security 仍由实际读取 owner 校验，主体 ID／成员关系 ID／授权版本引用自身不是认证证明。

可信同步 User 凭据组合复用 IAM 唯一 `ResolveUserAccessToken`：自有只读事务统一使用 Repeatable Read，凭据投影和精确规则查询共享已提交快照。Tenant／Principal／Membership／授权版本只由真实当前凭据派生，查询后另以数据库墙钟复核 Token 的自然到期；不保存或输出凭据、不复制 IAM 核验算法。底座本身保持私有，不取代 owner 功能／Client Scope、Execution 或 Security，不新增迁移或 Permission。

首个正式同步检查为 `POST /api/v1/system/engine-access/read-checks/manager-preview`，接收当前 Tenant 普通 User Bearer，或精确 `manager` audience、唯一 `data.preview` Scope 的真实委托凭据，与 1–200 个完整目标；固定消费 Manager 已声明的 `manager.data_item.read`。委托凭据由 IAM 核验来源 Token Family、当前用户／成员／授权版本及 Client、Run／Tool 绑定，不降格成普通 User，也不开放其它 System API。功能／Client 条件、真实凭据和全部源规则在同一只读快照中核验；任一目标无有效 Grant 或命中 Deny 时整体拒绝。成功只返回不可缓存的当次 `observed_at`，不返回完整 Allow、规则详情或执行凭据。领域检查不写授权审计或请求副本；通用 HTTP 元信息审计仍由现有中间件执行，不记录 Token 或正文。Manager 的完整 Provider ReadSet、同一 PreparedQuery 实际执行及 Security 尚须接入，不能把此接口完成当作真实内容访问闭环完成。

迁移 000183 增加 `engine_access_grant_revocations`：以原办理 UUID 唯一引用 Grant，追加撤销者 Principal／Tenant Membership、数据库墙钟与必填原因；不复制或改写原目标、接收主体、动作和期限。行及整表历史不可 UPDATE／DELETE／TRUNCATE，插入核验撤销者为原 Tenant 的 User 成员。真实撤销命令发布独立 `system.engine_access_grant.revoke`，不默认分配 Role 或账号，不推进既有授权版本；服务核验当前账号、Token、Permission 和引擎管理委派，并同事务写入高风险撤销审计。引擎停用或失去实时目录能力仍允许收回旧授权，账号或委派失效则拒绝；不修改新授予的启用条件。撤销历史不是 Explicit Deny，也不表示执行侧数据权限已经接通。

迁移 000184 只前向收紧撤销插入触发器：按数据库墙钟拒绝原 `at_time` Grant 到期后的首次撤销，不回填时间、不修改既有撤销／受理／签发历史，不新增或分配权限。服务写入前和审计后提交前同样检查授权期限，失败同时回滚撤销与审计；已成功撤销的原参重试优先读取历史，仅重新核验当前操作者资格，不受原授权后续到期或五分钟自动办理窗口限制。`until_revoked` 保持可主动撤销。

IAM 权威表位于 PostgreSQL `system` schema，只允许 `system/backend/internal/migration/sql/*.up.sql` 单向迁移。IAM 表不得进入 GORM `AutoMigrate`，运行时不得根据表存在性补列、建表或写兼容种子。

## 二、统一字段规则

- 主键使用 PostgreSQL bigint，API 中以十进制字符串表达 IAM ID；
- 时间使用 `timestamptz` 和数据库时间；
- 浏览器认证、MFA、Context Selection／Switch、Token Family 签发及刷新统一通过 IAM Repository 读取数据库 `statement_timestamp()`，不默认读取应用时钟；读取失败必须回滚，不回退应用时间。取锁后再读取当前语句时间，不能使用等待锁之前的时间或固定事务开始时间判断过期。AuthContext 快照在同一查询中读取该时间；一次判定复用同一个时间值。登录认证时间来自成功认证事务，跨事务传递时保留原事实；MFA 与上下文转换不延长已有 Family 最终期限。TOTP 计数与过期判断使用同一数据库时间，保留既有防重放规则。服务的时间依赖显式包含 Context 与 Repository，仅测试可注入受控时间读取器，正式装配使用唯一数据库读取实现。
- 生命周期使用显式状态，不使用可产生歧义的通用 `is_active` 替代完整状态机；
- Principal 授权事实变化必须推进 `authorization_version`；
- IAM 安全事实和对应审计事件必须在同一数据库事务提交；
- Token、Code、一次性 Secret 只保存不可逆 Hash；密码保存自适应 Hash；MFA Secret 使用独立密钥加密。

凭据校验的内部失败诊断必须从实际消费的快照取证，不以失败后重新查询的时间代替原始校验时间。诊断只保留固定条件码、数据库时间、凭据创建／认证时间及相关过期边界，以及已查到凭据的记录类别、内部记录 ID 和 Family ID，不保留 Token、Hash、完整快照或身份详情。记录类别区分 `access_token`、`resource_access_ticket` 和 `delegated_access_token`，避免不同表的相同 ID 被混用；格式错误或查无记录时不得伪造 ID。诊断随内部错误传递，System 的 AuthContext 拒绝运行日志只输出固定原因和内部记录引用，契约见[登录认证统一要求](../../docs/spec/addp登录认证的统一要求.md#24-认证凭据日志边界)。内部原因和引用不加入 HTTP 错误正文；未授权错误语义和现有有效性判断保持一致。该证据只证明一次校验的触发条件，不能单凭创建时间较晚就断言宿主机或数据库的时钟同步故障。

数据库约束中与上述事实相关的取时必须同步：稳定 Tenant Administrator 和 OAuth 身份上下文的有效期判断，以及 Authorization Request 完成、OIDC 认证和 Device 决定的未来时间拒绝，使用 `statement_timestamp()`。同事务写入已经发生的语句时间事实，不得被早于该事实的事务开始时间误判为未来或尚未生效；真正晚于当前语句时间的事件、未生效／已过期授权、非法身份和不可变事实修改仍须拒绝。修订通过前向迁移替换原函数，不改写已应用迁移，不重建触发器、不改写历史数据，不保留旧取时分支。

## 三、身份与凭据

### 3.1 Principal

`system.principals` 是 User 和 Service Principal 的统一授权主体，保存主体类型、状态和授权版本。业务模块跨服务只引用 Principal ID，不复制用户名、邮箱、密码或 Role。

### 3.2 User 与 Local Account

`system.users` 保存自然人资料，不保存 Tenant 归属、Role 或密码。`system.local_accounts` 保存本地登录标识和 Password Hash。

旧 `users.user_type`、`users.tenant_id` 和默认 SuperAdmin 已删除，不是兼容字段或迁移输入。

### 3.3 Service Principal

`system.service_principals` 保存工作负载身份。Service Principal 通过独立 Confidential OAuth Client 和 Client Credentials 获取短期 Access Token，不能使用用户密码、平台三员 Role 或共享 Internal API Key 模拟 User。

### 3.4 MFA

`mfa_credentials`、`mfa_challenges` 和 `mfa_enrollments` 保存 TOTP 凭据、一次性验证和登记状态。

TOTP 登记完成和 Browser step-up 都创建新的 AAL2 Token Family，再撤销原 Family；不得原地修改既有 Family 的认证事实。登记、验证、Family 转换、Token 签发、旧 Family 撤销和安全审计必须处于同一事务。

## 四、Tenant 与组织

`tenants` 是业务隔离边界；`tenant_memberships` 表达 Principal 进入 Tenant 的有效关系。一个 User 可拥有多个 Membership，但一个 AuthContext 只能选择一个 Tenant。

新 Tenant 必须原子创建：

1. 创建 Tenant；
2. 为指定普通 User 创建首个 Membership；
3. 创建首个 `tenant.administrator` Assignment；
4. 写入初始化事实和安全审计。

初始化后的非关闭 Tenant 必须始终保留至少一名稳定有效的 Tenant Administrator。约束延迟到事务提交前检查，禁止通过分步 API 或直接 SQL 留下无管理员 Tenant。

`departments` 和 `project_groups` 都严格属于单个 Tenant。Department 是稳定组织结构，Project Group 是跨部门协作集合，二者不得互相替代。成员关系变化必须推进受影响 Principal 的授权版本。

### 4.1 组织管理聚合与公开 API

Department、Project Group、Department Membership 和 Project Group Membership 都是可独立读取、具有独立生命周期的主资源，统一使用非空正整数 `version` 作为乐观并发版本。所有更新、停用、关闭和成员关系结束操作必须在同一事务中按 `tenant_id + id + version` 原子校验、写入并递增版本；版本冲突不得产生组织事实、授权版本或审计副作用。

Department 使用 `active` / `disabled` 生命周期。Department 是可被 Catalog 等 owner 软引用的稳定组织身份，不提供物理删除 API；停用后不允许建立新的成员关系或责任引用，既有历史仍可解析，允许管理员显式恢复。创建时管理员必须自行填写 code，界面明确提示必填且创建后不可修改；System 校验当前 Tenant 内 Department code 唯一。层级更新必须继续满足同 Tenant、无循环约束。部门结构变更必须持有当前 Tenant 的结构级事务锁，生命周期变更与成员关系写入必须锁定同一 Department 聚合根，避免并发创建循环或在停用过程中新增成员。

Project Group 创建即为 `active`，可以关闭为不可逆的 `closed`。关闭后不允许新增或恢复成员关系，既有临时授权按授权上下文规则失效；实际关闭时间和原因保存在审计事实中。创建时管理员必须自行填写 code，界面明确提示必填且创建后不可修改；System 校验当前 Tenant 内 Project Group code 唯一。描述在创建后按需编辑；项目组不保存计划起止时间，不以日期控制组织授权。第一阶段不支持嵌套。生命周期变更与成员关系写入必须锁定同一 Project Group 聚合根。

Department Membership 的 `membership_type` 使用 `primary` / `additional`，`relation_role` 使用 `member` / `leader`；同一 Tenant Membership 最多一个有效主部门。Project Group Membership 的 `relation_role` 使用 `member` / `leader` / `coordinator`。两类组织成员关系都只接受 `principal_type=user` 的 Tenant Membership；Service Principal 是机器身份，不得加入 Department 或 Project Group。两类成员关系都使用 `active` / `ended`，`ended` 是历史终态；成员身份、所属组织和 Tenant Membership 创建后不可变，需要调整时必须结束旧关系并创建新关系。

公开管理 API 只存在于当前 Tenant Context，固定使用以下单一路由，不接受 `tenant_id`：

| Method | Path | 语义 |
| --- | --- | --- |
| GET / POST | `/api/v1/system/tenant/departments` | 分页查询或创建 Department |
| GET / PUT | `/api/v1/system/tenant/departments/:id` | 读取或完整更新 Department |
| POST | `/api/v1/system/tenant/departments/:id/disable` | 停用 Department |
| POST | `/api/v1/system/tenant/departments/:id/restore` | 恢复 Department |
| GET / POST | `/api/v1/system/tenant/departments/:id/memberships` | 查询或创建 Department Membership |
| PUT | `/api/v1/system/tenant/departments/:id/memberships/:membership_id` | 更新成员关系类型和组织角色 |
| POST | `/api/v1/system/tenant/departments/:id/memberships/:membership_id/close` | 结束 Department Membership |
| GET / POST | `/api/v1/system/tenant/project_groups` | 分页查询或创建 Project Group |
| GET / PUT | `/api/v1/system/tenant/project_groups/:id` | 读取或完整更新 Project Group |
| POST | `/api/v1/system/tenant/project_groups/:id/close` | 关闭 Project Group |
| GET / POST | `/api/v1/system/tenant/project_groups/:id/memberships` | 查询或创建 Project Group Membership |
| PUT | `/api/v1/system/tenant/project_groups/:id/memberships/:membership_id` | 更新项目组内角色 |
| POST | `/api/v1/system/tenant/project_groups/:id/memberships/:membership_id/close` | 结束 Project Group Membership |

列表和详情只返回当前 Tenant 内对象，跨 Tenant ID 与不存在统一返回 `404`。成员创建只接受当前 Tenant 的 `tenant_membership_id`，不能由前端提交 User ID 猜测 Membership。所有写接口使用具体请求 DTO，并返回更新后的完整资源；生命周期动作必须携带 `version` 和非空 `reason`。

`GET /api/v1/system/tenant/memberships` 支持 `search`、`status` 和 `principal_type=user|service_principal` 组合过滤。管理界面将 `principal_type` 呈现为“账号类型”，值显示为“用户账号”或“服务账号”，不得改名或新增平行协议字段。“角色管理 > 角色分配”固定查询 User Membership；“应用接入 > 租户服务账号”和“应用接入 > 平台运行账号”分别通过每行的 Membership 管理或查看 Service Principal 角色；Tenant 审计复用同一 Tenant Membership 选择控件并按“当前账号、用户账号、服务账号”分组。角色分配只允许从有效 Membership 创建授权，历史审计允许选择全部生命周期 Membership。

## 五、Permission、Role 与高权限治理

`permissions`、`roles`、`role_permissions`、`role_assignments` 和 `role_conflicts` 保存运行时 RBAC 事实。Permission 和内置 Role 的发布规则见 `docs/spec/addp权限与角色发布规范.md`。

当管理员在同一 Membership、Scope、有效期和授权原因下分配多个 Role 时，`POST /api/v1/system/tenant/role_assignments` 只接受 `role_ids` 显式列表，并在单一事务内创建全部 Assignment 与审计事实。任一 Role 不存在、不兼容、不允许指定 Scope、重复分配或需要增强认证时，整批不得产生部分授权事实。

角色分配只允许 Platform / Tenant。租户分配始终绑定当前 Tenant，不接收组织范围或组织编号；组织归属单独由部门／项目组成员关系维护。000223 前向退役旧组织范围分配，先写完整不可变审计，再移除旧分配与组织范围字段，停用原仅支持组织范围的角色并失效旧会话；不转换为租户授权，不改写组织、成员或数据访问规则。

租户 Role 管理列表必须保持单行可浏览：主列表只展示 Role 名称、标识、类型、适用对象、Permission 数量及模块摘要；Role 描述和完整 Permission 必须统一放在详情视图，Permission 按命名空间、资源与动作分组，不得用长描述或逗号拼接文本撑高整张表。角色定义首先按 `allowed_principal_types` 分为“用户账号角色”和“机器身份角色”，默认展示用户账号角色，并允许查看全部角色；分类不得依赖 Role Key 后缀或展示名称。列表支持名称或标识搜索和 Role 类型过滤；租户角色的范围固定为当前 Tenant，不提供 Scope 选择或过滤。

Role Assignment 列表支持按 Membership 与 `principal_type=user|service_principal` 组合过滤，成员列必须显式展示成员类型；“当前账号”只通过 AuthContext 的 `tenant_membership_id` 识别，不按用户名、邮箱或展示名称猜测。持久 `status` 只表达 `active` / `revoked` 生命周期，读取接口使用数据库时间派生 `effective_state=scheduled|effective|expired|revoked`，默认只返回 `effective`。只有 `scheduled` 和 `effective` Assignment 可撤销；`expired` 只读，后端与数据库必须拒绝将其改写为 `revoked`。Role 选择器把兼容 Role 分为“可分配角色”和“已分配角色”，只以同一 Membership、同一 Scope 下的 `effective` Assignment 禁止重复选择，历史撤销或到期记录不得阻止重新分配。数据库对同一 Principal、Role 和 Scope 使用不重叠的半开授权区间 `[valid_from, valid_until)` 约束重复，不再以所有持久 `active` 记录共用一个唯一位置。

`role_assignments.grant_reason` 保存授予原因；手工创建必须非空。撤销时必须原子写入 `revoked_by_principal_id`、`revoked_at` 和非空 `revoked_reason`，撤销后的 Assignment 不可恢复或修改。授权详情由 `GET /api/v1/system/tenant/role_assignments/:id` 读取，返回授予与撤销操作者的可读 Principal 引用、完整时间和原因；已撤销记录还派生同一 Principal、Role、Scope 下的 `same_scope_active_assignment_id`，但不把重新授权持久化为旧 Assignment 的替代关系。授权生命周期详情沿用 `iam.tenant_role_assignment.read`，不扩大 Tenant 审计权限。

Tenant 审计的成员筛选以所选 Tenant Membership 的 `principal_id` 查询既有审计协议，并支持按 `principal_type=user|service_principal` 过滤操作者类型；成员列必须显式展示操作者类型。模块筛选使用 `module_name` 的稳定协议值。界面必须以本地化模块名称作为主标签、稳定 `module_name` 作为辅助标识，不得把中文名称写入审计事实或新增兼容字段。审计导出必须覆盖当前筛选条件下的全部事件，不得复用普通列表的单页上限静默截断；后端使用稳定排序的分批读取和有界内存文件生成，响应通过 `X-ADDP-Export-Count` 明确返回实际导出条数。

平台系统管理员、安全管理员和审计管理员是三个互斥 User Role，不存在全权合并角色。平台高权限身份变化使用 `privileged_change_requests` 和 `privileged_change_approvals`，申请人与审批人必须满足权限发布规范的目标角色矩阵；批准与状态／角色变更在同一事务内提交，不再通过普通账号接口二次执行。请求保存不可变的目标与申请人授权版本、目标角色、原分配、接任人、到期时间及独立 version，复核对版本和当前事实进行比较。

Role、Assignment、Membership、组织关系或 Principal 状态变化时，数据库和 Service 必须在同一事务中：

- 锁定受影响 Principal；
- 修改权威事实；
- 推进授权版本；
- 撤销或转换受影响 Token Family；
- 写入唯一安全审计事实。

### 5.1 授权事实的事务取锁顺序

角色分配／撤销、角色定义变更、租户成员维护、组织成员维护及租户／组织生命周期操作，必须先取得本次受影响 Principal 的写锁，再取得 Membership、Tenant、组织、Role 或 Assignment 等下层事实锁。涉及多个 Principal（包括事务需要锁定的操作者）时，去重后按 ID 升序取锁。数据库触发器推进授权版本所涉及的 Principal 同样属于受影响集合，不能持有下层锁后才由触发器反向取得账号锁。

需要从 Membership 或 Assignment 找到 Principal 时，只读预查仅用于定位不可变身份；取得账号锁后，必须重新读取并核对当前对象的租户与身份关联，原有状态、版本和有效期校验仍在锁内执行。角色定义、部门停用、项目组关闭和租户生命周期等批量操作先读取受影响账号集合并有序加锁，再锁聚合根并重新查询集合；集合变化返回事务冲突，整笔回滚，不在下层锁内补锁账号或绕过核验。集合核对不替代原有权限检查。

内部源授权受理的身份核验继续使用 Principal → Membership → Tenant 共享锁，再进入精确请求／目标边界。此顺序与上述 IAM 写事务相协调；不新增全局互斥锁、权限副本或死锁后无限重试。真实 PostgreSQL 并发回归纳入既有 System IAM 门禁，验证角色、成员和生命周期写事务与核验竞争时均可完成，且批量集合变化安全拒绝。

## 六、会话与令牌

源数据当前授权关系从 `engine_access_grants` 与不可变撤销事实实时派生，按 Tenant、Engine、DataItem 身份、接收方类型／编号及动作聚合，不新增可变 ACL 表。边界版本不拆分管理关系：当前列表及来源展开保留全部未撤销、未到期的来源，关系撤销在同一数据项事务锁内收回全部有效记录，每条撤销保存同次收回的编号集合。新签发在唯一持久路径拒绝有效关系重复，Catalog 与直接批准共用该约束。账号核查是对已存批准边界的源规则观察，不声称已经复核 Meta 当前边界；最终读取必须重取可信事实并匹配批准摘要。当前与历史列表均不是最终读取裁决。契约见《addp授权上下文规范》5.5.6。

迁移 `000202_source_grant_relations`、`000207_source_dataitem_boundary_bindings` 与 `000208_source_dataitem_boundary_identity` 的前向升级和重复运行由既有 System IAM PostgreSQL 门禁自动发现；`source-grant-relations` 分组还覆盖 `000210_source_single_record_targets`，验证三类单项目标和既有授权历史保留。000207 保持已执行的原始摘要，只清理经确认可丢弃的旧源授权测试事实及依赖记录，不修改 IAM、角色、管理委派和通用审计。000208 前向修正 schema 段的 `kind=namespace` 和结构根空名称校验，并重新验证四处既有边界约束；不改写规则、历史或 checksum，存在不符合约束的记录时明确失败。000210 在同一 `valid_source_single_identity` 函数内扩展 MySQL 单表和 MongoDB 单集合，保留既有 Grant、批准要求、Deny、受理回执、IAM 与审计记录，不复制授权、不改历史迁移或 checksum。局部验证使用 `bash scripts/test/system-iam-postgres-gate.sh --package migration --test source-grant-relations`；仍须指定经过标准基础设施脚本验证的 `addp_iam_test` DSN，不使用开发数据库。

### 引擎访问控制领域的管理委派

`system.engine_access_delegations` 由 System 引擎访问控制领域独立维护，不是 Role Assignment 或全平台中央 ACL。它绑定同 Tenant 的 Engine 和 User Membership，保存显式到期时间、授予原因／操作者／时间、版本，以及撤销原因／操作者／时间；无源端写入、无默认角色授权、无 Catalog 副本。只允许创建、读取和版本化撤销；授权范围及接口以 `docs/spec/addp授权上下文规范.md` 5.5.2 为准。

同一 Engine、同一 Membership 的有效区间不得重叠；数据库拒绝身份、范围、期限、授予事实的修改，以及撤销历史的恢复／删除。写入与安全审计在同一事务完成，并通过既有 IAM Repository 推进接收主体授权版本、撤销旧会话。读取派生 `effective/expired/unavailable/revoked`，不得把持久 `active` 等同于当前有效管理资格；尚未实施的源读取规则不能消费此表绕过最终资源授权。

### 源授权办理协调底座

正式准备→可信反查→首次受理（2026-10-03）：Catalog 人类入口从可信 AuthContext 派生操作人，只接收请求／决定编号；本地完整待核清绑定提交后才发送。System `POST /runtime/engine-access-fulfillments/:request_id/accept` 限定 `addp-catalog` 当前 Tenant Service 身份及 `.execute`；使用独立 `addp-system` Tenant Service OAuth 在事务锁外反查 Catalog 精确待核清依据。返回后对机器主体、办理人、确认人及接收账号去重升序锁定，事务内核验当前 IAM、独立人类办理权限、管理委派、接收主体与业务依据。确认人历史授权版本仅供审计，办理人版本必须与请求一致；等待后按数据库墙钟复核权限及期限。反查期间先提交的原受理／关闭结果优先按完整绑定核清，不由旧人员状态阻断历史恢复。

迁移 `000180_catalog_fulfillment_basis_runtime` 登记不可委托、不可租户定制的 `catalog.sharing_fulfillment.read`，只授予内置 `tenant.system_runtime`；为存量已初始化 Tenant 和未来新 Tenant 接入 `addp-system`。OAuth Client 初始停用且 Secret 为空，SQL 不保存明文或借用其他服务 Secret。`SYSTEM_SERVICE_CLIENT_SECRET` 是可选独立配置：缺失不阻断 System Ready，新受理明确返回 503 `fulfillment_capability_unavailable`，历史核清／关闭及同参结果恢复仍可用；移除配置通过既有 Provisioner 停用 Client 并撤销活动 Token Family，不回退独立批准或推断 Catalog 退出。未运行真实 OAuth 双服务 Online T4，不以受控 owner Transport 夹具替代其验收。

源授权办理协调底座使用 `system.engine_access_fulfillment_outcomes` 保存同次请求唯一、不可改写的 `accepted` 或 `closed` 结果。它不是 Grant、直接授权许可或访问凭据；没有结果与已关闭严格不同。请求编号绑定租户、机器主体、精确结构化路径、决定引用及参数；System 数据库墙钟产生受理时间及不超过原授权到期的 5 分钟截止时间。结果与最小审计同事务，重试只返回原结果。正式新受理已由专用 Runtime 接口消费该底座，真实业务决定与当前资格独立核验；受理接口本身不写入 Grant，内部签发另由 000182 对应路径落实。受理回执不能代替内容访问裁决。

核清读取使用独立只读事务，拒绝在受理写事务中读取自身未提交结果；与仲裁共用唯一的完整绑定匹配路径，不取请求或目标仲裁锁。不存在返回未找到，参数不同返回绑定冲突，数据库错误原样保留；查询均不创建关闭结果、不追加办理审计。直接授权许可变化或原窗口到期不改变 Catalog 历史查询结果，也不因此重新授予或续期。

生产核清通过 `POST /runtime/engine-access-fulfillments/:request_id/resolve|close` 接入：限定 `addp-catalog` 当前 Tenant Service 身份及 `system.engine_access_fulfillment.execute`，Tenant 从规范 AuthContext 派生，请求中的原调用主体须匹配认证主体。服务当前 OAuth Client、Service Principal、Tenant Membership、授权版本、Permission 与 Token 期限在本地事务核验，关闭等待请求／目标锁后再次核验期限再提交；原操作人或接收方后来失效不阻断历史核清。明确未找到只是查询结果，关闭与原受理竞争，返回先提交的原结果。迁移 000176 仅向 `tenant.catalog_runtime` 增加此不可委托、不可租户定制的 Tenant Permission，受影响的活跃角色分配主体递增授权版本并撤销 Refresh Family；不新增 User Role 授权、Assignment、新受理或 Grant。Catalog 后台使用独立 Service 客户端消费结果，不持 System 事务锁跨模块调用，通信故障保留待核清保护，不影响服务 Ready。

首次受理须在同一事务通过 IAM 仓储核验接收 User 及本 Tenant Membership，或本 Tenant 的 Department／Project Group。共享账号锁先去重并按 ID 升序取得，再读取成员、Tenant 与组织；共享锁保护生命周期至事务结束，业务核验后按数据库墙钟再次检查成员期限。接收组织不要求非空，接收账号不绑定当前授权版本快照；这不代替实际 Grant 写入和访问时的当前核验。历史受理／关闭结果的核清不受后来接收主体失效影响，不产生新授权。

完整 `binding` 同时绑定原操作人的 User Principal、Tenant Membership 和授权版本，以十进制字符串保留 bigint 精度，不保存 User Token 或 Role／Permission／责任副本。首次受理通过 IAM Repository 的事务内 `LockUserAuthorizationSource` 按 Principal → Membership → Tenant 取得共享行锁，再进入请求和目标边界；核验当前对象类型、状态、成员身份范围、授权版本和有效期，业务核验后重新检查数据库墙钟。共享锁既阻止 IAM 状态修改，又兼容审计外键引用检查，避免身份写锁与目标锁的循环等待；原管理委派写事务不改用共享锁。原操作人失效不阻止同参历史核清或持久关闭，但不得据历史结果新建受理。此底线本身不证明这些字段确实来自该用户操作；正式消费者通过 Catalog 专用 owner 接口核对原操作人持久来源，并独立核验当前功能 Permission 与管理委派，不保存或透传 User Token。

`system.engine_access_direct_permissions` 保存 Tenant／Engine 内精确 DataItem 的显式 `allowed`、稳定指纹与并发版本。`000248` 从旧配置映射：旧 `independent` 为允许、旧 `catalog` 为禁止，保留配置身份／版本和审计历史；旧配置表、模式及目标摘要字段退出。缺失记录按持久部署默认计算，不补建默认记录。设置需要当前独立许可管理 Permission 与引擎授权管理员资格；新记录版本 1，修改只递增一次，同显式值保持原版本。身份不可变，不能删除记录以恢复部署默认；许可变更与新直接签发使用同一精确目标锁，等待与审计后重新核验当前资格。新 Catalog 业务受理不读取该配置。只读列表跨服务查询期间不持 IAM 锁，返回前复核资格和完整许可观察，变化返回冲突。详细 API、部署优先级与迁移清理见文首。

迁移 `000170_catalog_sharing_decision_permission` 随 Catalog 真实业务确认 API 发布独立 `catalog.sharing_decision.create`：仅 Tenant Scope、high risk、不可委托、允许租户定制。它只登记功能权限，不默认分配给内置管理员或任何 Role，不新增 Assignment 或数据 Grant、不修改已有主体授权版本。Catalog 仍须以该权限与当前业务负责人资格取交集；System 新受理消费者已接通，实际源数据规则尚未贯通。前向升级和重复执行由既有 System IAM PostgreSQL 门禁覆盖，不操作开发业务库的迁移状态。

迁移 `000178_engine_access_fulfillment_handling` 随 System 当前 User 办理范围观察和 Catalog 人类候选摘要读取登记独立 `system.engine_access_fulfillment.create`：仅 Tenant Scope、high risk、不可委托、允许租户定制。没有默认 Role Permission、Assignment 或 Grant，不修改现有主体授权版本。业务确认权、编目权、管理委派或机器 `.execute` 都不能替代它；有效管理委派仍须另外核验。前向迁移与重复运行并入既有 `engine-access-coordination` PostgreSQL 分组和完整迁移门禁。该观察接口本身不是受理或内容授权入口；正式准备与首次受理另通过专用入口消费同一独立 Permission。

普通只读共享显式选择 `expiry_mode=at_time|until_revoked`，受理结果和完整 binding 都保存该模式，`grant_expires_at` 在长期有效时为 NULL。指定到期仍须为未来时间；长期有效的自动办理窗口仍严格为原受理时间后 5 分钟。迁移 `000172_engine_access_fulfillment_expiry_mode` 在排他表锁及同一事务内暂时撤下该表 UPDATE/DELETE 不可变保护，将存量限时记录无损标记为 `at_time`、为 binding 补齐模式，再恢复保护及完整日期／截止时间约束；原到期、受理时间、截止时间、身份和审计不变。不改写已执行 167，不增加实际 Grant、默认权限或授权版本变化。长期有效不改变临时接入规则和管理委派的强制到期契约。

正式受理后的 Grant HTTP（2026-10-03）：`POST /runtime/engine-access-fulfillments/:request_id/grant` 及其 `/grant/resolve` 查询复用 000182 表和唯一生产签发／只读历史路径；无新迁移、实体或默认赋权。当前 `addp-catalog` Tenant Service 及既有 `.execute` 是机器入口资格，新签发仍独立检查原人类 `.create`、引擎管理委派和接收主体。原编号及 `granted_at` 是最小不可变签发历史；查询 `found=false` 不代表关闭，查询与重试不延长原窗口或恢复撤销。不涉及 Catalog 自动消费、Explicit Deny 或执行侧当前数据访问裁决，完整接口契约见《addp授权上下文规范》§5.5.3。

签发阻碍观察（2026-10-17）：上述只读查询在未签发时可附带 `unissued_reason=relation_exists|window_expired|closed`。原请求和原签发、原窗口及重复关系来自同一已提交只读快照，当前机器身份仍按既有 Runtime 守卫复核。已有原签发历史优先，原窗口结束优先于重复关系；无阻碍省略原因。重复关系只按同租户、引擎、DataItem、接收方及动作检查未撤销且未过期的规则，不查询 Meta、不返回其他 Grant 身份，不证明实际内容可读。观察不持仲裁锁、不保存失败副本、不写审计或 Grant。Catalog 原续办观察到重复关系时暂不重发签发，窗口结束后完成本地核清；没有新的状态存储或授权路径。

### 源读取显式拒绝

DataItem 边界绑定区别 Allow 与 Deny：Grant 按稳定数据项身份及批准边界摘要匹配，Deny 按同 Tenant／Engine 的数据项身份、接收主体和动作匹配，不以当前摘要等于建立时摘要为条件。对同一数据项重扫、扩大边界或签发新 Grant 均不能绕过有效拒绝；解除和到期仍按原有不可变事实处理。当前端到端实施与验收聚焦 PG 的 server → schema → 普通 table。共享协议与数据库约束另外能表达 server → database → 普通 table（MySQL）和 server → database → collection（MongoDB），其持久化回归只证明身份约束与规则存储，不代表这些引擎的实际读取已接入；不扩大本轮 PG 实施范围。三个目标形态共用数据库身份约束；namespace 与 leaf 类型必须成对合法，不能把 schema 下的 collection 或 view 当作已支持目标。

切换回归须覆盖：同一数据项边界改变后旧 Grant 不覆盖、新 Grant 仍被原有效 Deny 阻断；正常内容更新及 whole 合法成员增长不改变摘要；解除 Deny 不复活无效 Grant；其他 Tenant／Engine／数据项不误命中；Meta 不可用不回退原生 ACL，且不阻止对已存规则的合法撤销／解除。Grant、Deny、当前来源查询与正式读取消费必须同次切换，不保留按路径独立放行的第二路线。

`system.engine_access_denies` 是 System 引擎访问控制领域的不可变拒绝建立事实，不是 IAM 全平台中央 ACL。它保存独立 UUID、Tenant／Engine、DataItem 指纹、建立时原生坐标、User／Department／Project Group 主体、`read` 动作、期限模式、原操作者引用、数据库建立时刻和原因，不复制 Catalog 责任或专业定义。指定到期必须有限且晚于真实建立时刻；长期有效必须显式为 `until_revoked` 且到期字段为空。插入触发器覆盖调用方时间，拒绝回填时间绕过到期；UPDATE、DELETE 和 TRUNCATE 都禁止。

唯一建立命令 `POST /engines/:id/access_denies` 通过独立 `.create` Permission 与当前有效引擎管理委派取交集。Permission 仅 Tenant Scope、high risk、不可委托、允许租户定制，不默认给任何 Role 赋权、不增加 Assignment 或推进已有主体版本。完整账号集合先去重升序锁定，再读取成员／Tenant、当前权限、接收组织、引擎及委派，最后按命令编号与精确目标排序；规则和高风险审计同事务，锁等待后及审计后提交前复核当前资格、接收主体和数据库期限。停用引擎仍可建立限制，不访问源端或 Catalog。

同编号、同租户／引擎、同原操作者及成员关系、同规范化参数重试返回原历史，即使规则后来到期或接收主体失效也不续期、不重复审计；当前操作者权限和委派仍须有效。同范围异参返回冲突，跨 Tenant／Engine 返回未找到。已存本地身份允许离线建立限制，新身份须先取得 Meta 证明；撤销与解除不依赖 Meta。创建记录本身不是访问裁决，Manager 上述只读入口逐次消费同一规则。

迁移 000186 随真实解除命令发布独立 `.release` Permission，不默认分配角色或推进已有主体授权版本。`engine_access_deny_releases` 以原 Deny UUID 为主键及外键，仅追加解除者 Principal／Tenant Membership、数据库墙钟和必填原因；原规则继续保存目标、主体及期限。行与整表历史不可 UPDATE／DELETE／TRUNCATE。插入触发器核验解除者是原 Tenant 的 User 成员，覆盖调用方时间，并拒绝 `at_time` 原规则到期后的首次解除。服务复用当前引擎管理资格及精确目标锁，解除和高风险审计同事务，等待后和提交前核验资格与数据库期限；首次到期不写事实，已解除的原参重试优先恢复历史。历史接收主体失效或引擎停用不妨碍解除，不依赖 Catalog、不修改任何 Grant、不表示当前访问已允许。

### AuthContext 解析诊断

身份解析成功但耗时达到 100ms 时，System 沿既有 IAM 内部诊断路径记录 `iam.auth_context.resolve_slow`，包含 request_id、固定阶段墙钟耗时、连接池当前值及本次解析时间窗内的池等待累计增量。阶段耗时包含排队与执行；池增量属于同进程共享连接池，不能直接当作该请求的独占等待时间，等待时长累计只包含已结束的等待，嵌套解析也不得重复相加。快请求不输出慢诊断，失败和拒绝保留原诊断路径；不记录凭据、SQL、完整 AuthContext 或组织内容，不改变 HTTP、审计及任何权限裁决。

Hosted 只读观察器的采集、资源预算与证据归因边界见[审计尾延迟诊断场景](../../docs/spec/testing/scenarios/授权与共享.md#审计尾延迟诊断)；串行与并发性能样本的统计口径及验收层级见[读取裁决性能采样约束](../../docs/spec/testing/scenarios/授权与共享.md#读取裁决性能采样约束)。实现诊断、确定性回归和真实 T4 分别认定，不因日志存在而认定历史峰值已修复。

### 6.1 Context Selection

`context_selection_tickets` 保存登录后的短期上下文候选快照。Ticket 只使用一次，消费时必须重新校验 Principal、Membership、Platform Assignment、认证强度和授权版本。

消费 Ticket 和创建新 Token Family 必须在同一事务；并发消费只能成功一次。

### 6.2 Token Family

`refresh_token_families` 固定保存 Principal、Context、Client、认证方法、AAL、认证时间、授权版本和有效期。Family 创建后这些身份事实不可变。

`access_tokens`、`refresh_tokens`、`resource_access_tickets` 和 `delegated_access_tokens` 都只保存 Hash，并绑定 Family 或源 Token。AuthContext 解析必须回查当前 Principal 和授权版本，不依赖 Token 内自包含 claims。

Refresh 采用轮换和重用检测。并发 Refresh、logout、context switch、MFA 转换按统一 Principal 和 Family 锁顺序竞争；发现已消费旧 Refresh Token 时撤销整个 Family，但正常并发失败不能误报为重用攻击。

### 6.2a Internal Task Execution Authorization

`execution_authorizations.internal_task` 为与逐引擎子表互斥的 JSONB 范围。151 号迁移要求 ontology/user 来源、精确内部字段与规范类型、当前 pending common execution 的租户/发布主体/任务配置；签发后边界不可修改。内部授权不能插入 engine access，必须沿原 seal/撤销/有效期机制消费。152 号迁移登记 `ontology.revision.publish`、最小 `tenant.ontology_runtime` 和 `addp-ontology` Client；凭据仍由既有 provisioner 配置，不在 SQL 存 Secret，不给用户角色隐式加权。

消费时使用 Principal → Membership/Tenant → Authorization → execution 的锁顺序，检查当前 running lease、完整授权引用、当前功能权限和唯一服务身份。返回期限不超过授权、租约及相关 Membership 到期时间；租约等待后使用数据库墙钟再次检查，不能把事务开始时间当作当前有效时间。System 不读取 Ontology owner 表，也不返回 Infra 连接信息。

### 6.2b Manager 剖析源读取执行范围

000192 在既有 `execution_authorizations` 增加 `source_read_scope` JSONB，而非另建授权实体。该范围仅用于 `audience=manager` 的普通 User 手工剖析，保存全配置 SHA-256 摘要和规范完整 ReadSet；它补充逐引擎只读范围，不与 `internal_task`、Notebook 来源或动态子执行来源共存。其他 audience 的历史记录保持空值，不补造来源，也不修改原授权。插入必须匹配已提交的精确 pending `common.task_executions` 身份、任务类型及 v6 配置，数据库保护范围不可变并拒绝其他引擎或效果。

签发与消费在 IAM 事务中组合引擎访问控制领域的现行全来源规则，不保存 Grant 副本或缓存 Allow。消费还绑定当前 Manager Service Principal、Tenant Membership、机器消费 Permission、真实 running attempt／lease、授权引用及配置摘要；审计失败或等待跨过资格、授权或租约期限均回滚。000192 只向内置 `tenant.manager_runtime` 加入已有 `system.execution_authorization.execute`，与内置角色 Manifest 115 同步；由已有 Role Permission 触发器推进受影响机器主体的授权版本，迁移不再次手工推进，并撤销旧 Token Family。不增加用户角色授权、Assignment 或源 Grant。固定 HTTP 签发／消费接口已接入 System 路由，契约见授权上下文规范 5.2.2；签发不接收调用方声明的来源范围，消费只返回当次观察时刻。共享 wire 范围唯一归 `common/execution`，不是另一授权权威。Manager 实际采样接入仍须独立完成和验证。

### 6.3 Notebook Session Authorization

`notebook_session_authorizations` 保存由当前 Tenant User Access Token 派生、绑定唯一 Notebook Session 和 Task 的短期授权事实。它不是 Token，不保存 Token Hash、Engine 列表或连接信息，也不新增 AuthContext 类型；身份边界通过 User Principal、Tenant Membership、Token Family 和签发时 `authorization_version` 固定。它只允许实时 Catalog 发现，以及为每次 Notebook 只读查询/扫描派生独立 Execution Authorization。派生记录必须通过 `execution_authorizations.source_notebook_session_authorization_id` 保存唯一来源，并继承 Session 的身份、有效期和撤销边界。

签发、消费、撤销必须使用版本化 SQL 表和事务审计。每次消费实时回查 Principal、Tenant、Membership、Token Family、授权版本和当前 Permission；Family 或 Session 撤销必须在同一事务联动撤销 Session Authorization 及其派生且仍有效的 Execution Authorization。标准 Engine Access 租约复核必须沿来源外键重复校验这条生命周期链。详细契约以 `docs/spec/addp授权上下文规范.md` 和 `docs/spec/addp登录认证的统一要求.md` 为准。

## 七、Bootstrap、密码重置与灾难恢复

### 7.0 紧急会话撤销

System 唯一拥有首期 Break-glass 申请、两个独立批准及单次执行回执。平台侧负责审批和监督，Tenant 侧只允许当前申请人读取、放弃或消费本人申请；Tenant ID 和申请人来自可信 AuthContext，不接受调用方自报。平台读回投影提供明确租户、执行人与目标的可读引用，不给系统管理员附带普通账号管理或会话批量枚举权限。

申请绑定客户端随机 UUID、执行人／Membership／授权版本、目标普通 User／Membership／授权版本、一个第一方 Web Token Family、原因、事件编号和数据库审批截止时刻。两条批准分别绑定 `platform.system_administrator` 与 `platform.security_administrator`、批准人、授权版本、理由与时间；同人、同岗重复及审计管理员批准均拒绝。第一次批准不产生可消费授权，第二次批准在同事务生成15分钟执行截止时刻。不可变目标不得因版本冲突、过期或重试重新解释。

申请及每次可变操作使用正整数 BIGINT `version`；创建不传版本，批准／拒绝／撤销／放弃／执行均提交原版本。申请 UUID 同参重试恢复原记录，不重置10分钟审批期限。消费使用独立执行 UUID；原成功或明确未执行的同编号同参重试只返回不可变回执，不再撤销或追加执行审计；其他执行编号不能冒认原结果。读取已完成回执不把后来的人员变更解释为历史改写，也不恢复当前操作资格。

锁顺序为所有相关 Principal 按 ID 升序、Membership 按 ID 升序／Tenant、当前凭据 Family／Token、应急请求、目标 Family；审批集合变化须返回冲突，不在下层锁后补取 Principal。操作在锁内复用唯一 AuthContext 凭据核验及精确 Permission，批准／消费额外要求当前有效 step-up。审批人资格和版本、执行人及目标事实、数据库墙钟在等待后及审计后提交前重核。消费、单个 Family 撤销和已有派生授权失效触发器、回执及审计在同一 System 事务；审计失败整体回滚。有效期只决定是否能消费，不使已撤销会话复活。

执行入口不签发新 Token、不授予三员 Role Assignment、不复用三员一申请一复核流程假装双批准；`role_assignments.source_type=break_glass` 的历史枚举不作为本能力的授权依据。独立入口 Permission 不默认加入租户角色，只能通过现有受控角色配置赋予资格，且不能代替两个平台批准。

最小验证由现有 System Go T1、IAM／API／Migration `AgainstPostgres$` 自动发现及 System 前端门禁覆盖；真实双人批准、单次消费和会话拒绝使用既有平台三员 Hosted T4 的独占部署补验。不得连接本地数据库或接管开发服务，独立审计职责与独立存储保护分别记录结果。

### 7.1 首批三员 Bootstrap

系统不创建默认管理员。空 User 系统只允许使用与 System 同版本发布的离线 `iam-bootstrap prepare/apply`：

- `prepare` 生成一次性随机 Secret，只保存 Hash；
- `apply` 通过 TTY 收集三名管理员各自密码和 TOTP 验证；
- 三个 User、凭据、互斥 Role Assignment、审计和永久完成状态在单一事务提交；
- 任一 User 已存在时拒绝 Bootstrap，不提供 HTTP、弱密码或默认账号旁路。

### 7.2 普通 User 密码重置

平台安全管理员只能为不持有有效 Platform Role 的普通 User 执行受控重置。事务必须替换 Password Hash、清除临时锁定、推进授权版本、撤销全部 Token Family、终止未消费 Ticket/Challenge，并写入高风险审计。

### 7.3 普通 User MFA 重置

平台安全管理员只能为不持有有效 Platform Role、具有可用 Local Account 和唯一 active TOTP Credential 的普通 User 执行受控 MFA 重置。事务必须废止旧 Credential、推进授权版本、撤销全部 Token Family、终止未消费 Ticket/Challenge/Enrollment，并写入高风险审计；不得恢复或返回旧 TOTP Secret，也不得修改密码、Membership 或 Role Assignment。目标 User 随后通过唯一的当前用户 TOTP 自助登记路径建立新 Credential。

### 7.4 三员凭据恢复

线上凭据恢复复用三员特权治理请求：另外两类管理员分别申请、复核，目标和任何接任人均不得参与。批准只签发一次性短期登记凭据，明文仅返回一次，不进入查询响应、日志和审计。登记凭据以 Hash 保存，绑定请求、目标和授权版本；目标自行设置新密码、验证新 TOTP，在同一事务替换凭据、撤销旧会话和待消费认证事实、消费恢复凭据并写审计。拒绝过期、复用和事实变化；登记前不开放任何平台权限。

### 7.5 三员离线凭据恢复

无法形成符合目标角色矩阵的线上申请／复核组合时，使用离线 `iam-recovery prepare/apply`。恢复不删除或重建 Bootstrap 状态，不通过 SQL 直接替换 Hash，不开放 HTTP API。

恢复按明确的 Principal 身份重建当前三员持有人的密码和 TOTP，撤销旧会话并保留完整审计；不解除 Principal 暂停，不修改角色分配。Secret、密码、TOTP Secret 和验证码不得进入参数、环境变量、日志或审计详情。

离线恢复的提交确认与核清遵循以下唯一协议（本轮针对已取得恢复编号的 Apply；不以查询“最新一条”猜测 prepare 确认丢失的归属）：

- `prepare` 返回非敏感的 Attempt ID；`apply <attempt-id>` 必须绑定原编号和原 Secret。验证后、收集凭据前显示编号；每次正式提交生成新的随机 Submission ID，并在调用前显示，不能复用编号替换参数或自动重发写请求。
- 完成回执复用不可变恢复 Attempt 及同事务审计：`iam.recovery.completed` 保存成功的 `submission_id` 和人数，逐人替换审计保存 Principal、岗位及提交时授权版本。完成状态、Secret 清空、全部凭据与撤销事实、回执在同一事务提交。不保存密码、验证码、TOTP Secret 或其额外指纹。
- `status <attempt-id> <submission-id>` 只通过既有受控离线运维身份核清；编号不是权限，不开放 HTTP。核清不消费 Secret、不修改业务数据、不追加恢复审计。连接必须是可信权威写库；复制延迟、主库历史不明或分叉不作成功／失败推断。
- 核清在有界短事务内使用 READ COMMITTED，先取得与 Apply 相同的恢复表排他锁，再读取 Attempt 与关联审计。等待原事务结束后才分类，不能把普通 MVCC 查询所见的 prepared 当作回滚证明。未能获取锁、连接错误、查无记录或完成审计不完整时保持 unknown。
- completed 且提交编号匹配，表示该次提交的历史成功；编号不同表示其他提交成功。两者均禁止再次写入。该历史不证明凭据现在仍可使用，也不因后续正常改密、任免或版本变化而失效。
- prepared 且未过期只表示观察时尚无已提交的恢复结果，允许操作者显式重试；不保证原调用已回滚或不会稍后竞争。重试仍绑定原 Attempt 和 Secret，重新核验人员、岗位分配、状态、授权版本及新鲜的连续两个 TOTP 证明，正式写入在同一恢复锁内再裁决。变化必须重新确认，不自动替换目标。过期只允许另行准备，不续期或恢复旧 Secret。
- COMMIT 发出后连接错误统一按“结果可能未知、请按编号核清”处理，不将 ErrUnauthorized、超时、退出码或登录失败当作提交结果。不为重试缓存凭据文件；进程退出后重新交互收集。

验收必须区分提交前断连与提交确认丢失：后者须观察 PostgreSQL 已产生 COMMIT 成功响应，再截断客户端确认，并用独立连接核对原回执、完整四人事实及再次提交拒绝；仅让驱动在成功 Commit 后返回伪造错误不计为网络确认丢失 T2。还须覆盖锁等待／超时、其他提交获胜、过期、目标变化及已完成历史不受后来身份变化影响。真实数据库验收仅使用 Hosted 独占 PostgreSQL，不触碰当前本地环境。

## 八、OAuth 协议表

OAuth Client、Authorization Request、PKCE、Authorization Code、Device Authorization 和 Token Family 等协议事实由同一个 Fosite Storage Adapter 访问。具体表映射、锁顺序和 Provider 组合见 `system/docs/OAuth与Fosite实现说明.md`。

`oauth_clients` 同时保存 Platform 内置 Client 和 Tenant 外部 Client。`owner_scope` 固定为 `platform|tenant`，`owner_tenant_id` 只在 Tenant Client 上存在；`client_id`、管理归属、创建人和创建时间不可修改，`version` 用于所有管理写操作的乐观并发控制。Tenant Client 的协议字段固定为公共 Authorization Code + PKCE 与 Refresh Token，不配置 Secret 或 Service Principal；管理端只允许维护显示名称、redirect URI 和 `active|disabled` 生命周期。

Tenant Client 管理 API 只使用当前 Tenant AuthContext 下的 `/api/v1/system/tenant/oauth_clients` 单一路由，不接受 `tenant_id`。停用 Client 必须在同一事务中递增版本、取消 pending Authorization Request、撤销全部有效 Token Family 并写入安全审计；恢复只允许后续重新授权，不恢复历史会话。读取或批准 Tenant Client 的 Authorization Request 时必须复核 AuthContext Tenant 与 Client owner Tenant 一致。

OIDC 表字段是未来协议启用所需的受控预留，不表示 OIDC 已对外启用。

## 九、IAM 安全策略

`iam_security_policy` 是 System IAM 的平台级单例安全策略，保存 Token、OAuth Device Flow、Tenant Invitation 和 OAuth 限流的普通数值策略。该表不保存 Pepper、MFA 加密密钥、Service Client Secret 或其他 Secret。

策略使用 `version` 做乐观并发控制，使用 `applied_version` 表达当前 IAM Runtime 已装配的版本。System 启动时读取并校验唯一记录，然后将该版本标记为已应用；运行期间更新只产生新的待重启版本，不修改已装配 Runtime，也不读取环境变量 fallback。

策略更新和 `iam.security_policy.updated` 安全审计必须在同一事务提交。只有 Platform Context 中持有 `iam.security_policy.read/update` 的 User 可以访问，由 `platform.security_administrator` 承担该职责。

## 十、System 统一迁移 Runner

`system/backend/internal/migration/sql` 是 System schema 的唯一版本化结构事实源，不再只管理 IAM 表。IAM 约束可以引用 System-owned 的 Engine 等资源事实，因此被引用的基础资源表必须在首次引用它的 migration 之前创建。`system.engines` 由首个 System migration 创建，后续不得再由 GORM `AutoMigrate` 建表或补列。

System 启动顺序固定为：

1. 使用启动期专用单连接池连接 PostgreSQL；
2. 读取嵌入 migration 目录并校验版本连续性；
3. 拒绝 dirty、数据库版本超前和 legacy IAM schema，并校验已执行 migration 的文件名和 SHA-256 摘要；
4. 获取 PostgreSQL session advisory lock，在锁内重新读取版本和 dirty 状态；
5. 按版本分别在事务中执行向前 migration；
6. 确认数据库版本等于嵌入最新版本，并记录新执行 migration 的文件名和摘要；
7. 释放启动期连接，打开 GORM 运行时连接并启动 HTTP 服务；GORM 只管理尚未迁入统一 runner 的非基础业务表，不得管理 `system.engines`。

等待锁的实例必须在获得锁后重新读取版本。migration 失败会保留 dirty 状态并阻止 System 启动；处理规则见[通用规则](#通用规则)。

已执行 migration 的摘要记录在 `system.schema_migration_checksums`，只由 Migration Runner 维护。当前版本由 `system/backend/internal/migration/sql` 和 `internal/migration/catalog_test.go` 共同约束；本文不固定抄写版本号。

`000231`、`000232` 的索引约束与回归边界见[具体迁移约束](#具体迁移约束)。

## 十一、管理端信息架构

### Meta basic 采集资格

`000234_meta_collection_qualifications` 新增独立的 `system.meta_collection_qualifications`。它不是源数据 Grant 或第二套 Meta ACL：同 Tenant／Engine／ScanTask 的批准冻结任务功能摘要和引擎版本，保存用途、明确期限、审批人及授权版本；配置及原期限不可变，只允许当前有效记录显式撤销，保留历史，不物理删除。同一任务的有效时间段不能重叠，服务与存储约束共用事务级任务锁，不依赖额外 PostgreSQL 扩展。

迁移只向既有引擎授权管理员角色发布 `system.meta_collection_qualification.approve/read/revoke`，向既有 Meta Runtime 发布不可委托且不可租户定制的 `execute`，向既有 System Runtime 增授 `meta.task_provider.read` 以读取固定的 Meta 任务／执行事实接口（TaskProvider 原入口仍只接受 Orchestrator，不能因此被 System 冒用）；不新增账号角色分配，不批准旧任务，不触碰源 Grant／Deny。角色发布沿既有触发器推进授权版本并撤销受影响 Refresh Token Family。外键具备前导索引。

人类入口统一为 `GET /engines/{id}/meta_collection_task_facts/{task_id}`、`GET/POST /engines/{id}/meta_collection_qualifications` 和 `DELETE /engines/{id}/meta_collection_qualifications/{qualification_id}`；各自 Permission 与当前引擎管理范围取交集。机器只通过 `POST /runtime/meta_collection_checks` 消费当次观察，不接受自报任务摘要或主体作为事实。所有路径相对 `/api/v1/system`，请求／结果均不可缓存。首期只支持 PG entire-engine basic，不为 deep 或未支持 selector 兜底。标准定向存储验证为 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package engineaccess --test meta-collection"`；原默认门禁同样自动发现。

System IAM 管理端只按稳定业务大类提供五个左侧页面，不能把每个管理对象平铺为一个左侧入口，也不能继续把全部对象放入单个工作台：

| 页面 | Platform Context | Tenant Context |
| --- | --- | --- |
| 组织管理 `/iam/organization` | 租户管理 | 部门管理、项目组管理 |
| 账号管理 `/iam/accounts` | 用户账号、身份变更审批 | 用户账号、用户邀请 |
| 角色管理 `/iam/roles` | 无可用对象时隐藏 | 角色定义、角色分配 |
| 应用接入 `/iam/application-access` | 无可用对象时隐藏 | API 消费方、外部应用（OAuth）、租户服务账号、平台运行账号 |
| 审计管理 `/iam/security` | IAM 安全策略、平台审计 | 租户审计 |

页面表达业务大类，页内 `tab` 表达该类中的具体管理对象或流程。Tab 必须继续按当前 AuthContext 类型和 Permission 过滤；某个页面在当前上下文中没有任何可用 Tab 时，Console 左侧入口和 System standalone 导航都必须隐藏，直接访问也不得绕过 Context 与 Permission Guard。

当前 User 的个人自服务唯一页面为 `/account`，由 Console 和 System standalone 右上角“个人中心”入口打开，不作为任何 IAM 管理页的 Tab。页面分为“基本信息”“组织归属”“账号安全”；默认基本信息省略 query，其他分类分别使用 `?tab=organization`、`?tab=security`，刷新和历史导航恢复当前分类。基本信息展示账号和当前租户，顶部摘要可进入组织归属。凭据与 MFA 是跨 Tenant Context 的全局自服务对象；组织归属只表达当前 Tenant 下本人的有效成员关系。组织角色不授予管理功能或数据权限。

`GET /api/v1/system/users/me/organization` 是第一方当前 User 的只读自服务接口，复用浏览器会话权威验证和有效组织成员关系查询，不要求组织管理 Permission，不接受 User、Tenant、Membership 或组织 ID 参数。响应直接返回 `tenant`（最小 ID、名称、编码；Platform Context 为 null）、`departments` 和 `project_groups` 数组。部门包含最小 ID、名称、编码、由根到本部门的名称路径、主／附加关系与本人组织角色；项目组包含最小 ID、名称、编码与本人组内角色。查询在同一只读可重复读事务中验证会话、解析当前 Tenant Membership 和显示名称，不复制组织事实、不将名称加入 Token 或 AuthContext。仅返回有效直接部门／项目组关系，停用部门、关闭项目组与已结束关系不进入当前归属；祖先仅用于路径展示，不推导成员资格。平台上下文返回空数组，不枚举租户组织。响应禁止缓存；失效会话、机器或 OAuth 身份拒绝，依赖故障明确报错，不能伪装为无归属。

个人中心的页面测试覆盖无管理权限用户的入口、组织显示、空状态／错误重试、分类刷新与历史、语言切换及身份变更后迟到响应隔离。后端契约与 PostgreSQL 成员隔离／生命周期回归由现有 System 门禁自动发现，Console 宿主导航及共享路由权限门禁同步覆盖新单一路由；旧 `/account/security` 路由删除。

IAM 管理页页头必须用业务语言表达当前作用范围和会话认证状态：Tenant Context 显示“当前租户：名称（编码）”，Platform Context 显示“当前管理范围：平台”；不得向用户展示 `租户上下文 #<id>` 等内部标识。AAL 按“基础认证”“多因素认证”等语义展示，原始协议值只作为辅助说明。Tenant 名称和编码使用现有 `context-options` 权威结果，不得根据 Tenant ID 猜测或建立第二套查询。

“用户账号”、“租户服务账号”和“平台运行账号”是三类管理责任不同的对象，不得混排在同一主列表中。用户账号页只展示 User Membership；租户服务账号页只展示租户可管理的 Tenant-owned Service Principal；平台运行账号页只展示已加入当前 Tenant 的 Platform-owned Runtime Service Principal。角色管理中的角色分配页只展示 User；Service Principal 的角色入口分别归入“租户服务账号”和“平台运行账号”页；租户审计仍覆盖两类 Principal，并在选择器或过滤器内按“账号类型”分组并显式标记。

Tenant-owned 服务账号由 Service Principal、唯一 Tenant Membership 和一对一 Confidential OAuth Client 组成，三者生命周期由同一个服务账号 API 原子管理；内部 OAuth Client 不得出现在“外部应用（OAuth）”列表。Platform-owned Runtime Service Principal 在独立的“平台运行账号”页中只读，不向 Tenant 管理员暴露凭据轮换、定义修改或生命周期操作。两类机器身份的角色展示均复用 Role Assignment API；只有 Tenant-owned 服务账号允许 Tenant 管理员创建或撤销兼容的 Runtime Role Assignment。外部应用是代表 User 执行 Authorization Code + PKCE 的 Public OAuth Client，不是账号，也不接受 Client Credentials。

“应用接入”页签按 Tenant 管理员的常见工作频率排列：“API 消费方”、“外部应用（OAuth）”、“租户服务账号”、“平台运行账号”。前两项是对外接入的主要工作流；租户服务账号是高级自动化能力；平台运行账号只用于运行时角色查看与故障排查。该顺序是唯一默认顺序，不保留旧“机器身份”混合页签或其 query 兼容路径。

租户服务账号和平台运行账号的角色列表与选择器必须优先显示角色的本地化业务名称，稳定 `role_key` 只作为技术识别辅助展示；所有内置 Tenant Role 的中英文名称和描述必须随 Role Catalog 同步发布并由清单覆盖测试约束。

API 消费方只用于外部系统消费已发布的数据面 API，不是 Principal，不建立 Tenant Membership、Role Assignment 或 OAuth Client。System 保存 API Consumer、一次性展示的 API Consumer Credential Hash，以及精确的 `ConsumerServiceReference(service_type, service_id)`；首期只允许绑定 Service 已正式发布到服务消费目录的 Query Service。Gateway 只在显式数据面路由验证凭据、限流和记录调用，Service owner 必须再次验证 Credential，并按当前 Tenant、精确 Service Reference、服务状态和发布状态完成最终授权。`X-API-Key` 不得进入 `/api/v1/*` 控制面；控制面继续只接受其路由声明的 Bearer Credential。

Console 公开路由与 System standalone 路由使用同一模块内 path 和 query 契约。旧 `/iam?tab=...` 工作台、`/iam/identity`、`/iam/access` 和 `/settings/security-policy` 路径不保留重定向或兼容读取；审计唯一使用 `/iam/security?tab=audit`，具体 Platform 或 Tenant 范围只从当前 AuthContext 推导。

## 十二、迁移演进

### 通用规则

- 仅通过新的 `NNNNNN_name.up.sql` 向前迁移。已发布或已执行 migration 的版本号、文件名和内容摘要必须保持不变；概念收敛或方案重做也不得改写历史迁移。
- migration 失败不允许自动回退、跳过版本或运行时兜底。
- Permission/Role Catalog 变化由聚合器生成确定性输入，再进入新的向前 migration；
- 破坏性模型切换不保留旧字段、双写、双读或兼容 query；
- 需要保留的外部数据必须另行批准离线导入方案，不进入 System Runtime；
- migration 内不得访问 Redis、HTTP、外部 IdP、密钥服务或其他模块数据库。

### 具体迁移约束

- 标准成员清单能力退出后，迁移 `000140` 停用其六项 Permission、删除全部 Role 关联并撤销受影响会话，并撤销 `tenant.standard_runtime` 的人员目录读取授权。Catalog 的组织引用继续只允许 `addp-catalog` 调用，由 Catalog 专用解析方法处理。
- Quality 域引用权限的已执行 `000149` 保持原始摘要；后续域读取授权与服务身份刷新使用 `000150`。回归测试须从原始 149 已执行状态升级，校验历史摘要保持不变及重复启动幂等，不能只验证空库迁移。
- `000193_resource_observation_read.up.sql` 发布资源观测权限：`monitor.resource_observation.read` 仅允许 Platform、不可委托及租户定制，产品目录只授予 `platform.system_administrator`；内置角色 Manifest 116 同步。迁移推进受影响主体授权版本并撤销旧 Refresh Token Family，不扩大三员其他角色、Prometheus 采集身份或 Tenant Runtime 角色。预算配置沿用 Monitor 已有模块级平台配置 Permission。
- `000194` 发布专职 `tenant.engine_access_delegation_administrator` 模板，只允许 Tenant Scope 的 User，并组合委派读取／创建／撤销、引擎管理读取与租户账号候选读取。角色 Manifest 117 同步；不改写旧角色、不新增任何 Assignment、不生成管理委派或数据 Grant。账号赋权仍走正常 IAM API 并执行原有授权版本与会话撤销规则。
- 主机资源告警规则迁移 `000211_platform_resource_alert_rules` 新增 Monitor 的四项平台规则权限，仅绑定既有 `platform.system_administrator`；沿角色绑定触发器推进授权版本，并撤销该角色活动用户的旧刷新令牌族。保留 IAM 内核、其他角色、用户角色分配、租户权限和服务身份。专项命令与覆盖范围见[000211 专项验证](#000211-专项验证)。
- DataItem 边界绑定切换时，经确认可丢弃的旧原生目标测试授权不自动转换为新 Grant；通过前向迁移清理相关源规则及依赖记录，重新核验 Meta 边界后显式授权。清理不扩大到 IAM 账号、角色、功能权限或引擎管理委派，也不删除通用审计历史。切换完成后，旧原生目标记录不能作为读取依据，不保留兼容放行路径。
- `000229` 为现有不可变 `engine_access_grants` 增加 nullable `file_composition` 最小批准快照；原生表／集合行保持 NULL，文件行必须有严格完整组成，不回填或升级旧主文件许可。组成只取可信 Meta 事实，指纹仍是主项身份，源规则依同项／同边界摘要匹配 Grant、依同项身份匹配 Deny。原有 `(tenant_id,engine_id,item_fingerprint,boundary_hash)` 索引继续服务点查，不为组成快照建立独立 ACL 或无查询需求的 JSONB 索引。受管 FTP 模型结果使用同一次普通 User 裁决绑定 `expected_source` 完整组成和当前 Engine 版本，不依赖历史任务仍存在或终态 EA。正式接入及验证分片记录，迁移／共享类型存在不表示 FTP 真实内容链已验收。
- `000230` 在既有唯一 `source_read_scope` 列中封闭表达 Manager 剖析或 FTP 模型 GLB 两种用途，二者互斥，不新增通用引擎许可。Manager 共用签发／消费内核核验当前 User、功能权限、精确 pending 或 running claim、期限和审计；用途各自核验完整冻结 Scope。GLB 的任务标识／版本来自 Manager 创建事务已冻结的统一执行事实，System 不跨读 Manager 私有任务表；同 UUID 的有限核清只返回原授权及原期限。消费在同一当前源规则核验中锁定原版本 FTP Engine，只解密该快照，不再查询另一个连接。新增前向约束保留剖析原 wire，不改角色、Assignment 或源规则；运行接线、负例门禁和真实 FTP T4 分别验收。
- 应急会话撤销的 `000231` 已发布，保持文件与摘要不变。`000232` 只补充其申请和批准表的外键前导索引：已有按 Tenant／申请编号开头的复合索引继续服务列表和请求查询，单列外键的父记录关联检查另有有效的 B-tree 前导索引。升级不改申请、两人批准、执行回执、审计、角色、权限、授权版本或会话事实；沿同一 Runner 向前升级并验证重复运行，保留完整外键索引门禁。新增 `AgainstPostgres$` 回归由既有 `make test-system-iam-postgres` 自动发现，不增加入口或 Workflow，也不连接本地数据库。
- `000236` 将现有精确模型组成扩展至 MinIO IFC/3DS 的原生 `service → bucket → prefix → object` 目标；对象主项和每条依赖必须同 bucket，依赖限定在主项所在目录范围，Grant 保存完整组成，目录／bucket／prefix 不成为授权目标。复用现有 `file_composition` 和 `source_read_scope`，不增加另一套对象许可，不回填旧成果的来源事实。FTP/NFS 既有格式范围保持不变，MinIO DAE 未纳入本次迁移。

### 定向恢复

当前仅登记以下四个精确案例；标准构建、调用与停服／重启步骤统一见[定向恢复的共同操作](../../docs/guide/troubleshooting/数据库迁移.md#定向恢复的共同操作)，各案例的故障现象见对应专题。

| 迁移 | 精确前置条件 | 允许的恢复范围 |
| --- | --- | --- |
| [75：历史不可变 audience 冲突](../../docs/guide/troubleshooting/数据库迁移.md#75-号迁移的定向恢复) | 精确 `(75, dirty)`；checksum 恰好到 74；旧约束与不可变触发器均存在，且至少一条历史 `addp-quality`。 | 串行化事务内暂停触发器、规范化历史 audience 后立即恢复触发器，将状态恢复为 `74/clean`；不修改 checksum、不跳过 75。 |
| [113：Security 首次失败迁移](../../docs/guide/troubleshooting/数据库迁移.md#113-号迁移的定向恢复) | 精确 `(113, dirty)`；checksum 恰好到 112；Security 目标事实零落地，Standard 原分类权限未变更。 | 仅将状态恢复为 `112/clean`；不删除 System 事实、不修改 checksum、不标记 113 已成功。 |
| [130：Security 原值访问申请权限迁移](../../docs/guide/troubleshooting/数据库迁移.md#130-号迁移的定向恢复) | 精确 `(130, dirty)`；checksum 恰好到 129；新申请权限零落地，旧豁免变更权限与内置角色绑定仍完整。 | 仅将状态恢复为 `129/clean`；不修改 Permission、RolePermission 或 checksum，不标记 130 已成功。 |
| [240：进程内核身份声明迁移](../../docs/guide/troubleshooting/数据库迁移.md#240-号进程内核身份声明迁移的定向恢复) | 精确 `(240, dirty)`；1—239 的文件名／SHA-256 逐条匹配且无 240 checksum 记录；旧声明 CHECK 已验证且定义未变，不可变触发器保持启用且函数未变。 | 串行化事务内锁定迁移状态、checksum 和实例表，仅将 `240/dirty` 恢复为 `239/clean`；不改声明行、约束、角色、权限或 checksum。 |

任一前置事实不匹配即拒绝；四者均不得扩展为通用 dirty force、跳过版本或 checksum 改写能力。实际开发库恢复须由用户自行执行或取得本次明确授权。恢复后由唯一 Migration Runner 重跑对应迁移及后续版本，240 号保留专题中的全量重启要求。构建、隔离测试与只读现场核对不等于实际恢复，恢复成功也不等于后续迁移和业务验收通过。


## 十三、验证

测试分层与入口以[测试与验收规范](../../docs/spec/addp测试与验收规范.md#三标准入口)为准，以下命令均在仓库根目录执行，按验证目的选择：

| 目的 | 标准入口 | 范围 |
| --- | --- | --- |
| Go 定位检查 | `make test-module MODULE=system GO_ONLY=1` | 仅 System Go T1，不替代完整 owner 门禁。 |
| System 变更验证 | `make test-module MODULE=system` | 已登记的 T0–T3 owner 门禁。 |
| IAM、Fosite Storage、API 与 migration PostgreSQL 回归 | `make test-system-iam-postgres` | 真实 PostgreSQL T2；分组选择沿用 `SYSTEM_IAM_POSTGRES_TEST_ARGS`。 |

数据库连接准备统一见[PostgreSQL 测试库与安全连接](../../scripts/infra/docs/PostgreSQL与测试库.md#postgresql-database-清单)及[T2 隔离规范](../../docs/spec/testing/T0-T3测试规范.md#t2)：显式提供已核实的 `ADDP_SYSTEM_POSTGRES_TEST_DSN`，本地仅使用 `addp_iam_test`，CI 使用 Job 独占且随 Job 销毁的数据库。门禁会重建测试库的 `system` 和 `common` Schema，禁止指向开发库或生产库，分包运行也须串行。

下文专项分组只证明所选用例覆盖，不替代完整 T2 或 owner 门禁。未运行、失败、跳过项须分别记录，不计为通过；Go T1／PostgreSQL T2 不证明开发库已恢复，也不替代跨模块 Online 或业务运行验收。

### 000211 专项验证

迁移事实与权限边界见[具体迁移约束](#具体迁移约束)中的 `000211`；连接准备、分组覆盖与未运行／失败／跳过项的判定沿用[本章验证边界](#十三验证)。

`000211` 专项验证使用 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test resource-alert-rules"`，覆盖新键作用域/绑定、其他目录与身份不变、授权版本、刷新撤销、重复执行及完整迁移 Runner。
