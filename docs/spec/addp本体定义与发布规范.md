# ADDP 本体定义与发布规范

本文维护 Ontology 已实现的 Tenant 原生定义、修订发布、假设性试算与平台能力发布消费契约。未实施的专业来源引用、本体数据绑定、可信业务求值和 Graph 职责迁移见[后续设计](../next/ADDP%20Ontology最小设计契约.md)。本文不将关系声明或手工试算解释为业务实例事实。

## 一、语义层次与依赖

| 层次 | 回答的问题 | 权威来源与当前边界 |
| --- | --- | --- |
| 平台能力语义 | 用户意图对应 ADDP 的哪些功能、Skill 和 Tool？ | 平台概念文档、模块接口契约、Skill 与 Tool Manifest；结合 System 当前模块登记、实际可用性和用户权限判断。Transfer 创建切片由 PG 选择权威 active，再经同版 FalkorDB 查询依赖并补全（见平台能力消费）；平台用户只读核实入口已实现，真实部署消费仍待验收。不把 Tenant 定义当作平台定义，不以平台本体建设为领域建模前置。 |
| 租户领域语义 | 业务对象、关系、口径和规则是什么？ | Ontology 自有定义与明确引用的专业 owner。Standard、Model 可提供业务定义；Meta 提供结构事实，Quality 提供规则与检查结果，Catalog 提供企业身份、责任及治理关联。各类来源按专业含义消费，不自动转换为可编辑本体定义。 |
| 运行时业务事实 | 本次授权读取中，具体对象处于什么状态？ | 数据读取 owner 的实际结果，绑定对象身份、时点、范围和完整性。业务数据留在原库；事实随读取变化，不写入本体定义包，也不要求复制到 FalkorDB。 |

Agent 将三层组合：平台能力语义辅助选择合法操作，领域语义说明业务含义与规则，运行时事实支撑具体判断。静态能力定义不证明对应模块已部署、Tool 可执行或用户有权限；本体关系声明也不证明实例关系已经存在。

Ontology 不假设除 System 之外的任何业务模块已经部署或被用户使用，Agent 本身也不是 Ontology 的运行前置。自身 PG、FalkorDB 是必需 Infra；统一生命周期下 Alive 不要求 System 当时可达，Ready 要求自身 Infra 正常及 System 当前注册成功。其他业务模块不可达不影响 Ontology 启动、Ready、自有定义编辑发布或手工试算。

跨模块协作按明确操作通过正式 Client SDK/API 发起，不跨 Schema、不保存业务连接凭据，不因 SDK 可导入就认为远端能力存在。依赖只约束使用它的请求：引用某个 Standard 修订或读取某个来源时，该 owner 缺席、不可达或拒绝访问应明确阻断本次操作，不能阻断无关本体功能。稳定 owner 引用和已发布内容不得静默改绑其他来源。

客户端必须区分模块未提供、暂不可达、身份或权限拒绝、来源不存在、内容尚未建设、版本变化和观察不完整；可使用的公开分类以对应 owner 契约为准，404 等隐藏资源响应不得用于探测跨 Tenant 事实。上述情况不能统一解释为无数据、false 或空集合，不自动切换 owner，也不由 LLM 补齐未观察事实。Catalog 治理信息缺失不影响合法的独立本体操作，但显式选择的 Catalog 关联不可用时不能伪造其当前责任或审核状态。

## 二、原生定义与语义内核

一个 Tenant 内的 Ontology 是定义聚合；成员以同一修订发布，草稿并发 version、业务 revision 和投影 generation 不可混用。当前只支持原生类、属性、继承、关系声明及受限分类条件；专业 owner 来源引用和本体数据绑定尚未实现。

- 类、属性、关系和规则具有聚合内稳定身份。编译拒绝继承环、悬空引用、继承属性歧义、不一致逆关系及类型错误；声明传递性或逆关系不表示已执行实例推理。
- 确定修订形成不可变快照；规范化 JSON 和 SHA-256 包含 Tenant、本体、修订、内核契约与编译器版本，快照本身不是发布成功或激活证明。
- CEL 只支持布尔/字符串、等值、逻辑运算和字符串字面量有限集合成员判断。禁止宏、算术、动态类型、时间、正则、任意函数、网络/文件/数据库与隐式随机输入；有限枚举同时检查输入值与表达式字面量。
- 分类条件只有一份规范表达式；扩展数值、日期、单位或集合运算前必须先明确精度、时区、溢出、来源与成本边界。指标聚合、连接、去重和空间计算仍归相应执行 owner。

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

## 三、修订、投影与激活

PG 保存 Ontology 身份、工作修订、不可变发布内容、发布审计、投影构建状态及当前激活指针。FalkorDB 保存由确定修订构建的关系投影与最小查询属性；图中不保存业务连接凭据。

首版每个 Tenant、Ontology、发布修订及构建批次使用服务端生成的独立 graph key。调用方只提供业务身份，不能直接选择 graph key 或执行任意 Cypher。物理分图有助于隔离，但不能替代服务端 Tenant 和资源授权。

规则求值从 PG 的确定修订加载权威定义；图查询返回成员身份和关系，必须能核对修订和摘要。禁止在 PG 与图中分别编辑规则，也不维护两个“当前版本”指针。

定义生命周期为 `draft → in_review → published → withdrawn`，只有草稿可改；已发布内容冻结，published 不等于投影 ready 或当前 active。

- `ontology.ontologies` 协调修订序号和唯一激活指针。创建使用 last_revision+1，不能同时存在其他 draft/in_review；保存、提交、退回、发布和撤回都携带精确 version，冲突不自动重试。
- `ontology.revisions` 保存规范化包、摘要、状态和 version；`revision_events` 与业务变更同事务，已发布内容及审计不可改写。owner 迁移由 `common/schema.Migrate` 管理，不由 Ontology 初始化共享表。
- 发布同事务冻结内容并创建 `ontology / semantic_projection` bounded execution、generation 及 activation_version 基线。用户主体取自 System AuthContext；internal_task 授权绑定 task_type/resource_id/revision/digest/generation，与 Engine Access Scope 互斥，不授予业务 Engine 访问权。
- 意图提交后在同一请求栈签发并附加 System Execution Authorization；未准入不得领取。Supervisor 仅 Ready 时使用 Common claim/lease/fencing；锁顺序为本体头、修订、投影、execution，claim 单独提交后才进入 owner 事务。
- 构建前恢复冻结快照并消费内部授权，在新 graph key 中构建并全量核验。激活前再次消费授权，并复核授权期限、lease、撤回和 activation_version；ready、active 指针、owner 激活审计与 execution success 原子提交。PG 行锁内不访问 System 或 FalkorDB。
- 每次切换或撤回当前版本递增 activation_version。失败、取消、失权和过期租约不激活部分图，不自动重放写入，保留原 active；撤回当前修订清空 active，不自动回退历史修订。

- 只重建仍为 published 修订的 failed 投影，且旧 execution 已终结并释放租约。请求必须携带修订 version、失败 generation 和明确确认的 activation_version；过期基线或重复重建冲突，不隐式重试。
- 同一失败 generation 只能产生一个后继；同一修订至多有一个 pending/building 投影。后继再失败时须明确选择该后继重建，不允许从历史祖先分叉。ready 投影的主动替换不属于当前机制。
- 新投影、新 execution 和重建请求审计同事务保存，保留 predecessor generation；不改写冻结定义、原 execution、旧授权或旧图。修订记录上的 generation/build_execution_id 只证明首次发布时的构建身份，不是“当前投影”指针；后续准入和执行一律以 owner 投影记录为准。
- 首次构建和重建共用唯一准入与执行路径。准入显式指定 generation，以本次请求的 User Token 为新的 execution 签发授权；主体是本次已获发布权限的请求人，不借用原发布者身份。当前 Actor 必须与新 execution 中冻结的请求主体一致。
- 重建再次恢复同一冻结快照并校验摘要，不读取最新来源定义。只有新图完整校验、授权复核及激活基线检查均通过才切换；失败继续保留原激活版本。
- 撤回检查该修订的全部投影，原子取消尚未领取的重建，并阻断已领取投影激活；旧 generation 的迟到授权、终态回写不能影响新 generation。
- 重建 HTTP 入口见下节管理契约；不提供 Agent 重建入口或历史图清理。


图适配唯一使用 go-redis；每请求独占连接，取消关闭连接并拒绝迟到结果。已提交查询带非零服务端 timeout，最多 2 秒、每适配器最多 4 个在途请求，容量满立即拒绝；断连不表示写入未发生，不自动重放。FalkorDB 是私有 Infra，独立凭据和资源边界，不注册业务 Engine，不复用业务 Redis。历史图自动清理、ready 投影主动替换及未准入 pending 的自动超时收敛尚未交付。

## 四、管理入口与建模页面

Backend 使用 Go/Gin，端口 8195，唯一 API 前缀 `/api/v1/ontology`，不改 Graph。管理路由仅接受当前 Tenant 的 User Access Token；主体、成员、租户和授权版本从 System AuthContext 取得，禁止请求体自报。只有下节的两个只读定义入口允许精确 Tool 的 Delegated Token；不开放 Service Token 或任意 Cypher。

`ontology.revision.read` 读取本体头、确定修订与确定 generation；`ontology.revision.update` 创建/保存草稿、提交审核、退回；`ontology.revision.publish` 发布/撤回，发布和失败重建同时要求 `system.execution_authorization.create`。Permission 均为 Tenant scope，自定义角色显式分配，不扩张既有用户角色。`platform.ontology_runtime` 用于模块注册及下文平台机器发布；不借用其权限执行 Tenant 发布。

- `GET /ontologies/{ontology_id}`：返回 last_revision、activation_version、active_revision/generation，不把 published 当作 active。
- `GET /ontologies`：当前 Tenant 的本体头分页列表，固定按 ontology_id 升序；不按名称猜测或跨租户发现本体。
- `GET /ontologies/{ontology_id}/revisions`：当前 Tenant 下确定本体的修订摘要分页列表，固定按 revision 降序。返回身份、version、status、digest、首次发布 generation/execution 与时间，不加载或返回定义 payload；完整内容仍由确定修订详情接口读取。不存在或属于其他 Tenant 的本体统一返回 404。
- 两个管理列表仅接受 `page`、`page_size`，默认 1/20，每页最多 100；非规范正整数、重复/未知参数及超过有符号 32 位 OFFSET 范围的请求返回 400，不静默改写参数。响应复用 `data/total/page/page_size/total_pages`，越界页为 `data: []`；每次请求在同一只读 PG 快照内读取 total 和当前页，不承诺多个请求之间冻结列表。沿用 `ontology.revision.read`，不开放 Delegated Token 或增加新的权限。
- `POST /ontologies/{ontology_id}/revisions`、`GET/PUT /ontologies/{ontology_id}/revisions/{revision}`：请求只提交原生定义成员，Scope 由路径、修订号和认证事实组成；保存携带精确 version。
- 修订下 `POST /submit`、`/return`、`/publish`、`/withdraw`：精确 version，不接受内容；`POST /rebuild` 另带 failed_generation 和 activation_version。
- `GET /ontologies/{ontology_id}/projections/{generation}`：仅返回当前 Tenant/本体的投影身份、状态、前驱和激活基线，不暴露授权、lease token 或图物理 key。
- `GET /ontologies/{ontology_id}/revisions/{revision}/projection`：按重建前驱链读取该修订最新一次投影（不是 active 指针），便于发布/重建响应丢失后找回任务身份；没有投影返回 404。按无后继节点确定，不按客户端时间猜测或回退首次 generation。

发布/重建先提交 PG 意图，再在同一请求栈内签发并附加授权，成功返回 202（仅表示已准入，不表示 active）。准入失败返回 502 和稳定 `projection_admission_failed`，同时返回已提交的 revision/version/generation/execution_id；调用方必须按身份重新查询，不得盲重试发布。原修订不会因准入失败解冻，后续遵循显式失败重建。请求/进程中断产生的未准入 pending 不被领取，当前不自动补签或重放，可撤回修订；无授权 pending 的自动超时收敛不在当前机制范围。

Backend 使用统一 Lifecycle，绑定监听后异步注册；Ready 同时要求 PG、带非零查询上限的 FalkorDB 和 System 注册有效。Supervisor 只在 Ready 时领取；退出停止领取、取消并等待在途执行，限时关闭 HTTP，再等待注销。部署仅使用独立 `ONTOLOGY_SERVICE_CLIENT_SECRET`、`INFRA_FALKORDB_PASSWORD` 与部署地址，不注册 Business Engine。

最小门禁为 T0 权限/Swagger/构建与生命周期登记、T1 正式路由鉴权与严格 DTO、T2 PG/FalkorDB 原有状态机及读取隔离。真实 System/Gateway 跨进程体验另需 T4，不以进程内认证夹具冒充已上线。

Console 数据治理组提供独立 Ontology 入口，不改动 Graph。原生类型、属性、关系、规则使用同一份表单草稿，保存仍由现有 Backend 校验、编译和生成摘要；浏览器不执行 CEL。页面只消费当前 Tenant 的正式管理 API，不访问 PG 或 FalkorDB。

模块内公开路由为 `/ontologies`、`/ontologies/new`、`/ontologies/:ontology_id`、`/ontologies/:ontology_id/revisions/:revision`。列表分页使用 `page`（默认 1 省略）；编辑器页签使用 `tab=properties|relations|rules`（默认 classes 省略）。新本体创建 revision 1；新修订从已保存定义复制并使用 head.last_revision+1。保存和状态流转携带精确 version；409 不自动重试、不覆盖本地编辑。发布/重建的 202 或含 intent 的 502 不表示激活，必须重新读取修订、最新投影与 active 指针；传输结果不确定时锁定写操作，要求显式重新加载后再决定下一步。投影 ready 也仅在 head.active_generation 匹配时显示为当前生效。

首轮页面提供创建、保存、提交审核、退回草稿、发布、撤回和失败投影显式重建。操作同时受状态与 Permission 限制，发布/重建额外要求执行授权权限。离页保护、认证、语言、主题和 Console 导航复用 common-frontend。T0 检查生命周期和 CI 登记，T1 验证状态/请求契约，T3 使用独立端口与受控 API 夹具验证交互；不借此声称真实 T4 或 Agent 已通过。

## 五、原生定义消费与假设性试算

当前提供两个 Tool：`ontology.classes.list` 枚举用户明确指定本体在读取时已激活的类；`ontology.class.context` 使用前者返回的 revision、generation、activation_version 和明确 class_id 读取该类上下文。不跨本体猜测身份，不从名称推断业务实例。正式路径为 `GET /ontologies/{ontology_id}/semantic/classes` 与 `GET /ontologies/{ontology_id}/semantic/classes/{class_id}`，后者三个版本 query 必填；未知或重复 query 拒绝。

两个入口只接受 Tenant User 或精确对应 Tool 的 Delegated Token，使用独立且可委托的 `ontology.semantic.read`，不沿用管理修订权限。管理路由仍拒绝委托令牌；不自动给已有角色增加权限。查询在同一 PG 只读快照中核对 head、published 修订与 ready 投影的身份/摘要，再从权威原生定义构造有界结果；不把 PG 定义读取描述成 FalkorDB 实例查询。输出固定本体、修订、generation、activation_version、digest 与 `knowledge_kind=native_definition`。未激活返回 409，固定版本已变化返回 409，跨 Tenant/不存在统一 404；读取时的绑定不保证后续调用仍激活。

类上下文包含该类、完整祖先、可用属性、以该类为显式端点的关系和直接绑定该类的规则；规则不隐式继承，关系不是实例边，不执行 CEL。无来源映射与业务事实时不宣称已验证真实数据。类目录最多 64 项，目录和上下文紧凑 JSON 最大 128 KiB，超限明确失败而非截断。SDK、Manifest、精确委托、Skill 与离线评测同次接入；真实身份 Online 验收仍单独报告。

Console 在本体详情提供 `/ontologies/:ontology_id/trial` 页面，使用现有类目录与类上下文选择规则，不读取草稿、历史修订或业务数据。`POST /ontologies/{ontology_id}/semantic/rules/{rule_id}/trial` 仅接受 Tenant User 和 `ontology.semantic.read`；这是无持久化、无业务效果的定义消费，不扩展已有 Delegated Tool 的 scope，也不新增后台任务。

请求体固定为类目录返回的 `revision`、`generation`、`activation_version` 与按规则变量名索引的 `inputs`。每项包含 `state=known|absent|unknown|invalid`；非 known 的 `value` 必须省略或为 null。输入最多 16 项，请求最多 96 KiB；不接受表达式、租户、来源凭证或任意 query。未提交变量按 unknown 处理；未知变量返回 400，未知规则返回 404。已知值类型或枚举错误、显式 invalid 输入属于试算结果 error，不当作“不匹配”。

服务端复用唯一 CEL 内核，执行前后核对同一 published/ready 激活绑定；未激活或版本变化返回 409，不回退历史版本。HTTP 200 只表示试算得到结果，响应包含激活绑定、摘要及 `decision.mode=hypothetical`、规则依据、四态结果、输入状态和未决变量，不回显原始值，不持久化输入或输出。页面修改输入、规则、本体或重新加载时清除旧结果；冲突后保留输入但禁止继续试算，显式重新加载建立新基线。试算不是可信业务事实，也不是人数统计或质量检测。

令牌轮换后的 AuthContext 重取不是本体或规则切换。重取期间隐藏定义和输入、取消在途试算并清除结果；只有同一 User、Tenant 和 Tenant Membership 经权威上下文重新确认且仍有语义读取权限时，才恢复同一路由下的内存输入，不自动重新提交试算。换主体、换租户/成员身份、退出、权限撤销或重取失败均清空旧数据；仍由试算接口验证原激活绑定，409 锁定不会被普通令牌刷新解除。输入不写入浏览器持久存储。

## 六、平台定义来源与编译

平台定义由开发者在 Git 中维护，经部署编译发布到 PG 后不可独立编辑；管理员只读查看，不接收用户上传或在线修订。平台定义与 Tenant 定义分离，不使用 Tenant=0，不复制具体任务或业务实例。

`transfer.json` 仍是人工整理的精简语义定义，不从数据库或 Swagger 自动生成；同目录 `transfer.review.json` 只记录依据，不是第二份语义定义。依据包含 owner、类别、仓库路径、稳定定位锚点、Git 提交及文件 SHA-256，逐项绑定概念、关系、条件、效果、排除效果和事实入口。覆盖声明明确已建模能力及选定的未建模范围，不宣称枚举了 Transfer 的全部功能。

编译器将定义和依据共同冻结到 `addp.platform-definition/v2` 快照；摘要绑定两者，PG 仍只保存同一不可变 payload，没有第二张来源权威表或运行时仓库文件读取。Agent 上下文契约保持 v1，不附加来源全文。来源内容变化由平台公共 T0 门禁及 Ontology T1 检测文件摘要及锚点漂移，并核对所引用 Git 提交中的文件是否具有同一摘要与锚点；工作区文件匹配不能代替提交证据，缺失提交或内容不符必须拒绝，不联网补取或跳过。此核验只在开发与 CI 门禁读取 Git，本体服务的编译、发布与运行消费不依赖 Git。来源代码先提交，再以真实提交身份冻结依据，不能以旧提交配新摘要。来源变化须核实后更新依据和发布修订；测试只能证明引用完整、版本一致及已测试行为，不能自动证明本体含义完整或正确。来源引用不授予权限，也不证明当前模块在线。

平台核实入口为 Console `/ontology/platform/definitions` 及 `/:capability`，只查看当前 active 定义，不编辑或发布。平台 User 持有独立、不可委托的 `ontology.platform_definition.read` Permission（Platform Scope）；默认组合进平台系统管理员，不授予 Tenant 角色或机器 Runtime。公开 `GET /platform/definitions` 沿用有界目录，详情 `GET /platform/definitions/{capability}` 从一次 PG active 读取恢复同一快照的 `context` 和 `review`，不分别读取导致版本混合。语义消费 API 保持原 Tenant/精确 Tool 委托边界。页面复用共享本体图组件，只将已发布概念和关系可视化，不查询业务实例或任意 Cypher；所有条件、效果、事实入口和成员依据均可查看，覆盖范围不是整个模块的完整度分数。认证重取期间隐藏内容、取消请求；身份变化、撤权或失败清空，不接受迟到响应。

Ontology schema 6 向前调整新记录的快照约束；保留历史字节和审计，不重写旧记录，不增加 v1 Restore 分支。旧快照不能由新编译器消费，在部署新修订激活前相关读取失败关闭，不回退 JSON。来源/覆盖说明保留在 PG，FalkorDB 的投影及正式消费范围按下节的能力依赖消费规则投影；不开放任意图查询或实例推理。

- 每个重要概念、关系、操作条件和效果关联来源 owner、来源类别、稳定身份与引用位置、用途、确定版本和内容摘要。Git 来源固定提交及文件/章节；公开契约固定契约身份和确定版本/摘要。行号或可变路径只能辅助定位，不能作为唯一版本证据。
- 按需捕获最小、只读的语义依据和公开结构，不保存业务实例、凭据或私有运行数据。来源捕获不是另一份可编辑定义，也不替代 owner 当前授权。平台语义来源引用与 Tenant 本体数据绑定的模型、授权及用途不同，不能混为一套字段映射。
- 文档证明意图和正式含义，公开契约证明交互边界，实现与测试证明实际行为；没有一类来源可以覆盖所有事实。发生矛盾时先由 owner 核清并同步规范与实现，阻断依赖该冲突的发布，不让模型静默选择或拼接。
- Transfer 改变公开操作、配置语义、条件、效果、支持范围或保证时，同次更新相关规范、平台定义及来源引用、实际受影响的 Skill/Tool 和验收。当前采用整文件摘要，已捕获文件的内部重构也会要求人工复核；核实无语义变化时不改变概念或条件，只更新来源版本，完整快照因此改变仍须新发布修订。未触及已捕获依据或公开语义的内部变化不强制新增修订。表/代码变化检测不能自动判定业务含义或发布未经确认的定义。
- 平台定义由平台开发者在 Git 中维护，发布到 PG 后不可独立编辑；Tenant 原生定义继续由 Tenant 管理入口编辑发布。复用可适用的摘要、投影、审计及激活机制，不强行把操作前置条件塞进现有 Tenant CEL 分类模型，也不在 Transfer 保存第二份平台本体。
- 运行消费区分“语义已知”“owner 当前支持”“本次授权可执行”。平台定义绑定所依据的公开契约；消费依赖该能力的请求时核实当前 owner 与契约适用性，未匹配或无法核实不能推测执行，也不能自动改用旧定义。其他业务模块缺席不影响 Ontology 自有功能的 Ready。

平台能力定义使用独立的编译与恢复入口，不复用要求 Tenant 身份的原生 CEL 内核。编译只校验静态定义，不读取其他模块、业务数据或 IAM，不授予发布权限，也不证明定义与当前 owner 契约一致。

- 来源 JSON 必须是单个对象，拒绝未知字段、重复键、非法 UTF-8、尾随值、超限嵌套和超限内容。概念身份唯一、名称包含中英文；关系必须引用已声明概念且不能重复；操作 Tool 必须与能力身份一致；输入、效果、排除效果和要求身份不得重复，效果与排除效果不得重叠。引用真实 Tool/owner/Skill 的跨 owner 核验继续由既有 Manifest 契约测试负责，不能由纯编译器猜测。
- `addp.platform-definition/v2` 快照固定平台编译器版本、规范化定义及来源/覆盖依据，来源定义中的 `digest` 必须为空，由编译器计算整个快照的 SHA-256。调用方不能提交 Tenant、Actor、凭据、运行可用性或自报摘要。成员数组保留声明顺序，JSON 对象键规范化；改变定义、依据或编译契约需发布新修订，不能重解释历史修订。
- 来源最多 64 KiB、JSON 嵌套最多 32 层；能力上下文最多 32 KiB、32 个概念、64 条关系、16 项要求，每项操作输入/效果/排除效果及每项要求的 Tool 最多 32 个。普通身份和 Skill 最多 64 字节，点分能力/Tool/效果身份最多 128 字节；每个概念名称最多 16 种语言，每个名称和语言键最多 512 字节且非空；修订必须为正数且不超过 PG BIGINT 正数上界。限制由平台编译契约固定，不通过用户参数放宽。
- 恢复只接受当前快照契约/编译器版本、完全一致的规范化内容和摘要；损坏、非规范内容或旧编译器版本明确拒绝，不自动升级，不退回文件重算。输出与序列化内容无可变别名，调用方修改上下文不能改变快照。
- 部署来源经过同一编译入口，删除直接反序列化并返回未经校验定义的路径；运行消费仅 Restore PG active，见下文。快照不是 published、图 ready 或平台发布授权凭证；PG 内部记录、机器准入与投影发布内核见下文。

## 七、平台记录、机器发布与激活

- 独立 `platform_capabilities`、`platform_revisions`、`platform_revision_events` 管理平台聚合，不含 Tenant/Membership。存入接受已编译快照、可信平台主体及精确 last_revision 基线；新修订严格递增而不要求连续。头、规范化 TEXT 原字节、SHA-256 和审计同事务保存，修订/审计不可 UPDATE/DELETE；读取必须 Restore 检查身份、契约、规范化字节和摘要。
- 固定 `addp-ontology` Platform Service 使用不可委托的 `ontology.platform_definition.publish`。System 的 `POST /api/v1/system/runtime/platform-definition-publication-checks` 核验 IAM、Service 凭据、Platform Context、Permission 和固定 Client；只接受能力、正数 BIGINT 修订字符串和摘要，不接受定义、Tenant、图地址或自报主体。
- 发布是有界同步操作；存入、构图、激活前分别重新核验机器权限，结果不缓存为票据。已存在的相同修订只有字节与摘要完全一致才可复用；同身份不同内容拒绝。
- 每次尝试分配新 generation，`platform_projections` 保存修订、摘要、激活基线和 building/ready/failed/superseded。能力头保存当前发布 generation、active 修订/generation 和递增 activation_version；新的 building 尝试取代旧尝试，迟到结果不能激活。
- `ontology:p:` 命名空间构建独立投影，复用唯一 Build/Verify 并全量核验。激活按能力头、不可变修订、投影加锁，比较最后修订、当前 generation、building、摘要与基线；仅最新记录修订可激活，ready、active、机器审计同事务提交。网络调用不持有 PG 行锁。
- 任一构图、核验、撤权、取消、并发或审计失败均保留旧 active。中断不自动重放或删除图，暂留 building 由下一次部署尝试取代；历史图清理另行实施。

Backend 启动绑定 HTTP、发起 System 注册后，在独立且退出时等待的 goroutine 中编译随部署交付的定义；仅当既有 Lifecycle Ready（自身 PG/FalkorDB 正常、System 注册成功）时进行一次平台发布。当前部署包只有 Transfer 创建能力。等待阶段可取消；发布仍走上述机器准入与 generation fencing，失败不在后台自动重放，后续部署/重启才产生新的尝试。日志只给稳定结果分类，不泄露原始错误、定义、凭据或图 key。平台发布失败不改变 Backend 的通用 Ready 或 Tenant Supervisor；已有平台 active 继续可读，首次失败则没有可消费定义。

`GET /platform/capabilities/{capability}` 保留现有 Tenant User / 精确 Delegated Tool 与 `ontology.semantic.read` 边界，响应字段和 32 KiB 上限不变，不新增管理员写接口。同一只读可重复读 PG 事务核对 capability head、active 修订及 ready generation 的身份、摘要和激活基线，再 Restore 并按下节查询同版图依赖。平台定义是全局平台内容，读取不伪造 Platform Actor 或 Tenant=0，也不访问其他 owner schema。API 只接受规范能力身份及无 query 的请求；没有记录 404，无 active 409，损坏或图读取失败 500，不缓存、不回退。200 的 revision/digest 表示 PG 选择时点的 active 静态定义，不证明 Tool 可用、用户可执行或后续操作仍适用。

源 JSON 只供部署编译和静态测试；运行时唯一读取 PG active，不保留失败回退文件的路线。真实 System/Gateway/Agent T4 单独验收。

## 八、平台能力与图依赖消费

PG 是唯一完整定义与激活权威，FalkorDB 是正式依赖关系查询路径，而不是第二个定义库。当前创建能力切片不改变发布快照 v2 或 Agent context v1，不新增接口、权限或数据库迁移；只把已有字段确定性投影为 capability、owner、skill、tool、condition、requirement、effect 节点和有向关系。原概念、名称及显式关系继续保留。condition 对应 `inputs_required`；requirement 是声明的入口分组，经 `uses_tool` 连接 Tool，不代表每个 Tool 都必须调用。没有显式声明的条件—事实对应关系不猜测，辅助草稿入口不提升为强制前置步骤。效果与排除效果分别连接，均不是已经发生的实例事实。

目录和能力详情沿唯一链路：一次只读可重复读 PG 事务选择 active revision、digest、ready generation 和 activation_version → Restore 同一不可变 payload → 使用该 generation 的受控图查询展开能力的一跳依赖和入口组到 Tool 的两跳依赖 → 核对完整投影及依赖结果 → 用同一冻结定义补全名称说明并保持原响应顺序。图调用不持有 PG 事务或行锁。读取语义固定为 PG 选择时点的版本，不把请求中途新激活的 generation 拼进旧定义；读取成功不保证后续写操作仍适用，原 Runtime 的创建前平台上下文复核继续有效。

整个语义读取使用最多 5 秒预算，图命令沿既有 1–2000 ms 预算和取消规则。目录仍最多 32 项、128 KiB，任一依赖读取失败整次失败，不返回部分目录、不自动重试、不回退源 JSON 或 PG 关系遍历。图的摘要、成员、关系或依赖结果与 PG 确定修订不一致均拒绝；不接受用户 Cypher、任意深度、任意 graph key。平台管理员核实详情仍直接读取 PG 权威快照；核实目录复用正式能力目录，经过同版图查询。详情核实成功不宣称图运行消费成功。

投影结构变化不重写 PG 定义或旧 generation；部署发布器以同一冻结修订构建新的 generation 并核验后激活。旧结构不增加兼容读取分支，部署新服务后须完成一次成功发布。当前每能力独立图；尚无跨能力统一检索、自动规划、条件事实求值或性能优越性结论。验证沿既有 Go T1、Ontology PG/FalkorDB T2、Agent eval、Swagger 和平台一致性入口自动发现，无新依赖或 CI Job。

### 条件取证路径

平台定义复用已有概念、关系与入口组，不增加另一套事实表或 API 字段。`reads_via` 表示前置条件到正式事实读取入口组的显式关联，其目标概念身份必须对应 `requirements[].id`；`configured_by` 表示条件依赖任务配置，`confirmed_by` 表示条件依赖用户确认。三类关系的起点均须为本能力已声明的必需条件。未声明映射不能推断条件不需要事实或已经满足。

Transfer 定义将源身份与结构、目标父节点、行粒度及字段条件关联到既有源/目标读取入口；行粒度还依赖业务配置，执行边界、装载模式与写入策略属于配置，最终复核属于用户确认。入口组列出的是发现、定位和取证方法，不表示组内每个 Tool 都要调用，更不表示搜索候选等同于 `resource.facts.get` 的结构事实。Copilot 草稿辅助不充当事实入口。

FalkorDB 按显式关系投影并查询“能力 → 条件 → 读取入口组 → Tool”的固定三跳路径，核对完整结果后补全同版上下文中的 `reads_via` 关系；配置与确认关系仍来自同一冻结定义。保持现有上下文 v1 和快照 v2，定义及逐项来源绑定随不可变修订发布。查询没有调用这些 Tool，也没有判定条件满足；Agent 继续按 Skill 获取事实、澄清和复核，Runtime 保留正式观察、参数与确认指纹的裁决。不得由模型生成任意查询或自动执行读写计划。

用户只表达业务目标，例如“把 MongoDB 的 outdoor 传到 PostgreSQL，先建任务不要运行”；读取本体、发现真实资源、禁止猜测和确认写入策略由 Runtime、Skill 与 owner 契约承担，不要求用户写出内部流程或 API 字段。

平台能力正式读取按本节规则经过同版本 FalkorDB 依赖展开，再由 PG 冻结内容补全。下文“同一 PG 快照”限定权威版本选择，不表示图故障时可以只读 PG 返回语义。

平台能力目录唯一入口为 `GET /platform/capabilities`，Tool 为 `platform.capabilities.list`，沿用 Tenant User / 精确 Delegated Scope 与 `ontology.semantic.read`。仅列出同一 PG 只读可重复读快照中已激活且完整核验的定义，按 capability 升序；未激活定义不进入目录，空目录返回成功空数组，损坏定义导致整次失败，不能丢弃异常成员。最多 32 项、完整紧凑 JSON 128 KiB，超限明确失败，不分页、不截断、不访问源文件或业务库；拒绝任何 query，响应不缓存。目录包含概念、关系、操作条件与效果及 Skill/Tool 引用，不证明模块可用或用户具有业务操作权限。

Skill Runtime 装配用 `platform_routed=true` 声明该任务领域必须经平台语义选择，不在 Agent 维护另一套能力到 Skill 的映射。初始意图分类只决定是否进入该领域，不选择其中的 Skill 或能力；随后 Runtime 经正式 ToolExecutor 读取目录，LLM 根据用户短输入和返回语义选择一个 capability，Runtime 只接受目录中的身份，并核对定义所指 Skill 已装配、Tool 属于其白名单且 owner 与 Manifest 一致。空目录、无匹配或多义时解释限制或要求补充意图，不自动选第一项；目录读取失败终止当前相关请求，不能退回 Skill 描述路由。闲聊与其他尚未纳入平台语义的 Skill 不读取 Ontology，不改变 Ready 依赖。

能力选择通过后仍由领域 Skill 读取当前确定能力上下文、获取真实 owner 资源事实和确认配置；目录不是执行授权，也不是保证后续读取版本不变的票据。Interaction 恢复沿用既有 Skill 与已观察事实，不重新选择能力。当前只纳入 Transfer 创建切片，不据此宣称持续同步、启动、暂停或重试已有平台语义。参数草稿仍可由 LLM 基于事实提出，受控组装的进一步改造不在当前机制实现。

目录读取记录为现有 Tool step，选择后的 Skill 沿用现有 run state；当前机制不新增专门的能力选择审计或版本绑定。验证使用现有授权、Swagger、Go/Python T1、Ontology PostgreSQL T2 与 Agent 评测入口；不新增权限、基础设施或 CI Job。确定性短输入测试证明代码消费链路，真实 LLM 的短输入验收须另行记录，不能用脚本化模型替代。

## 九、Transfer 创建条件与复核

当前平台能力只覆盖创建 bounded/snapshot/native table、无计划、未启用且不自动扫描的任务定义，写入策略为 replace/append；创建不等于运行。输入 Schema、目标类型词汇由 Manifest 唯一声明，Ontology 不复制 Provider 物理规则。

Agent 在当前 Harness 调用内成功读取 `platform.capability.context` 后才允许复核或创建。具有 Transfer 创建 Tool 的 Harness 每次进入（包括 Interaction 恢复）时，由 Runtime 经白名单内的正式 Tool 主动读取当前能力，记录同一 AgentRun 的调用审计和检查点；不依赖模型记住刷新，不复用 checkpoint 中的旧定义。没有正式创建确认时再交给模型；确认恢复由 Runtime 按交互协议重读原参数、刷新源和目标事实并重新预检，匹配原指纹后沿原创建 Tool 提交，不再请求模型决策或收尾。稳定报告或事实变化要求同参数重新复核；条件不满足、读取失败或结果不明终止，不自动重试；新修订仍使旧复核指纹失效。Runtime 按返回的条件身份调用有限的条件适配器；未知条件明确拒绝，不交给模型自行忽略。核验源有正式结构事实、目标父节点由 owner 返回、边界/装载/写入策略符合条件，以及直接字段映射有依据。当前查询整形适配仅覆盖 Skill 已有的 MQL 显式直接投影和可选单次数组展开，拒绝复杂表达式或其他查询语言；它不替代 Provider 的正式查询分析和 Transfer 后端校验，不执行查询、不读取样本、不声称扫描结构完整覆盖业务值。

`request_clarification` 的 `transfer_create_review` 原因要求 `operation_review={tool,arguments}` 和 confirm/cancel 两个选项。Runtime 先校验条件及 Manifest，再追加确切参数的规范化 JSON 到复核提示中；仅保存参数与能力修订/摘要绑定的指纹，选项身份由服务端生成，模型不能通过普通候选伪造已复核状态。正式 Interaction 回答才写入 confirmed；创建前重新核验全部条件和指纹。任务参数、语义摘要或 owner 结构事实变化都要求重新复核。提交前消费确认并持久化检查点，响应丢失或失败不自动重用确认。此复核是配置确认，不是新的 Owner 审批或权限授予。

确认消费使用既有 checkpoint 事件的进程内提交回执：AG-UI 适配层完成数据库事务、再次检查客户端断开和运行取消后才回执，Harness 等待回执成功后调用写 Tool；持久化失败或回执前取消不调用 owner。回执后已发出的 owner 调用不承诺撤销。回执不进入 HTTP、数据库 payload 或事件回放。每次重新读取平台上下文前清空旧观察，刷新失败不能沿用同次 Harness 内较早的成功结果。

正式 ResourceFacts 刷新替换该资源原观察快照，不把新响应缺失的字段、查询名或旧搜索元数据合并回来；结构缺失明确补充事实，不能用旧结构通过创建条件。正式快照取得后只能由后续正式快照替换，搜索、目录与预览不能覆盖或补写它，从而保留完整事实来源。

最终创建复核采用 Runtime 生成的固定含义和本地化按钮，并附上绑定指纹的完整参数及正式预检结论。提示明确检查范围和未验证项；Transfer 与 Provider 裁决物理类型规则，Console 与 Agent 消费同一 owner 契约，不在 Agent 复制数据库规则或把 supported 称为执行保证。

正式只读预检纳入创建条件及 Skill。Runtime 在最终复核前和创建前，经同一白名单 Tool Adapter 调用 `transfer.task.config.check`，不依赖模型主动调用、不直接访问 SDK 或数据库；每次调用先清空旧报告并记录正式 Tool 审计。报告只绑定本次完整 Tool 参数摘要，owner 的 `config_hash` / `binding_hash` 原样保留，不在 Python 复制 Go JSON 哈希规则。复核指纹纳入报告稳定内容（含身份、配置摘要、结构事实、诊断及未检查项），仅排除报告的 `observed_at` 和包含该时间的 `binding_hash`；结构快照时刻变化仍属于事实变化。创建前重新预检，稳定内容变化使确认失效。unsupported 拒绝复核和创建；unknown 用 Runtime 固定本地化提示明确未检查项及运行时要求，取得绑定该报告的知情确认后才能保存未运行定义；supported 也不承诺执行成功。服务、授权或无效响应失败终止当前运行，不转换成 unknown 或自动重试。报告是当前 AgentRun 的受限观察，不进入平台静态图；不新增协议、权限、数据表或执行能力。

Runtime 自动预检的 `unsupported` 是定义拒绝，不是预检服务故障。拒绝复核或创建时，同一模型 ToolMessage 必须携带该次已校验报告的 `status/checked/not_checked/runtime_checks_required/diagnostics`，保留 owner 给出的字段位置与原因，使模型能够解释或修正草稿；不能只给条件错误码后要求模型猜测。此受限反馈不携带主体、事实摘要或绑定哈希，不新增 API 或持久化原始结果。完整报告仍由原检查点和正式预检步骤保存，拒绝时不生成确认选项、不调用创建 owner；报告损坏、权限或依赖失败仍终止，不走配置修正路径。模型只能基于 owner 诊断修改草稿，涉及业务含义变化先澄清，并对修改后的完整参数重新预检和复核，不在 Agent 复制 Provider 规则。

行粒度与字段保留范围必须分开解释和确认：一文档一行、不展开数组，并不授权删除数组字段。用户要求保留顶层字段时，已观察到的顶层数组与对象也进入候选映射；缺少目标表示或转换依据时交给正式预检或澄清，不能为规避 unsupported 而静默删字段。平台定义说明这一语义，Skill 组织相应交互；当前条件适配器不提供自然语言意图的确定性解析保证，完整配置仍需用户核对。

草稿与只读检查不要求进入创建复核。用户明确要求“检查配置，只给草稿，不创建、不运行”时，Skill 在参数完整后主动调用同一 `transfer.task.config.check`，按正式报告解释三态、字段诊断及剩余要求；不得反问用户是否需要已经要求的检查，也不调用 `transfer_create_review` 或创建 Tool。只要求草稿而没有要求检查时，不自称已预检。此分工不新增自动意图授权器、接口或权限，源事实和 Provider 仍各自拥有原有职责。

平台 Transfer 正式预检的事实展示由 Runtime 固定生成只读 Presentation：状态、检查时间、范围和完整诊断直接投影 owner 已验证响应，LLM 只解释，不负责构造报告卡片。Agent 与 Transfer 向导复用共享前端报告组件；不增加业务实体、ResultRef、审批或权限，不把运行报告写进静态本体。历史卡片只证明该次检查，创建条件、重新预检和绑定确认仍走原主路径；服务失败不产生 unknown 卡片。

报告后的默认短答由 Transfer Skill 单点维护，Runtime 不再追加逐项转述报告的领域指导。默认解释保留业务草稿概要、三态结论及影响当前决定的关键原因，完整范围和诊断由卡片展示；用户明确要求详解时可展开所请求部分。Tool 反馈和条件裁决继续消费完整报告，不裁剪诊断或增加前端文字过滤。提示词的受控回归只证明指导被注入及完整证据未丢失，实际简洁性仍须自由模型联调确认。

## 十、Outdoor 定义夹具与验证边界

正式业务定义 `ontology/backend/internal/semantic/testdata/outdoor_business.json` 使用三个独立类 activity/person/activity_membership，成员关系通过明确关系连接活动与人员。valid_activity 检查日期存在且不是草稿/取消，registered/participated 按[Outdoor 业务语义](../concepts/Outdoor业务语义.md)的成员状态集合分类；成员规则不隐式遍历活动，不等于人数统计或实际签到证据。

此定义的日期存在性分类范围与[已实现的 Outdoor 指标链路](Outdoor数据治理与指标实现规范.md)分开：它未实现日期不晚于观察日、事实绑定、取数、去重或指标计算，不能宣称与整条指标链路等价。合成北京演示夹具用于继承/未知输入测试，不作为真实成员关系模型或地域依据，也不自动供应到 Tenant。

标准入口为 `make test-module MODULE=ontology`、`make test-ontology-postgres`、`make test-ontology-falkor`、`make test-agent-eval` 及实际影响的授权、Swagger、平台来源门禁。具体目标以根 Makefile 和[测试与验收规范](addp测试与验收规范.md)为准；本地原生定义联调与 T1/T2/T3 不替代正式隔离 T4，当前未取得 ontology-revision-lifecycle 正式通过证据。新能力同次登记标准门禁与 CI，不把准入/编译成功视为真实业务求值成功。
