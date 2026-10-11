# Ontology 模块

Ontology 管理租户领域本体的形式化语义，不接管 Standard、Model、Catalog 的事实所有权，也不读取业务数据库。总体边界见 [最小设计契约](../docs/next/ADDP%20Ontology最小设计契约.md)。

## 已确认的设计与依赖边界

2026-10-03 明确三层：平台能力语义辅助 Agent 选择功能，租户领域语义说明业务概念与规则，运行时业务事实支撑具体对象判断。当前覆盖原生 Tenant 定义、手工假设性试算、首个平台 Transfer 能力定义的部署发布和 PG 消费，以及平台用户只读核实；Tenant 专业来源引用、本体数据绑定和可信业务事实求值仍未实现。

Ontology 自身 PG、FalkorDB 和 System 当前注册构成 Ready 前置；除 System 外，不假定任何业务模块存在或被用户使用。Meta、Standard、Model、Quality、Catalog、Manager、Develop、Service、Agent 等只在使用其能力的请求中经正式 Client SDK/API 协作，不进入启动/Ready 检查。远端不可达、无权限、没有内容和版本变化按 owner 契约明确表达，不自动换源、不把错误当作无数据。

本体数据绑定目标由 Ontology 唯一管理，固定本体修订与类/属性、读取 owner 来源及输出契约、对象身份、类型化路径、转换语义和绑定版本；Catalog 提供可选企业身份、责任与治理关联，不维护第二份可编辑绑定。Catalog StandardMapping 与 Model 概念实现映射仍属于各自 owner。绑定不持有业务凭据、不执行查询、不代表事实已观察；用户业务数据继续由数据读取 owner 授权访问。首个真实切片为单条 Outdoor 活动的有效性判断，正式契约见设计文档 2.3、6.2 与 10 节，不能把当前试算接口当作真实求值入口。

## 当前交付范围

平台 User 的 Console `/ontology/platform/definitions` 与 `/:capability` 只读核实当前 active 定义，展示概念关系、条件/效果/排除效果、事实入口、选定覆盖范围及逐项冻结依据。目录/详情 API 为 `GET /platform/definitions` 与 `/{capability}`，详情从一次 PG active 读取恢复同一快照的 context/review，不读仓库文件或业务实例。独立 `ontology.platform_definition.read` 仅 Platform Scope、不可委托，System 向前迁移 000203 默认组合进平台系统管理员；不扩张 Tenant 或机器 Runtime。原 Agent 精简消费契约及权限不变。前端直接复用 common-frontend 的 OntologyView 和锁定的 G6 4.8.24，模块仅适配已发布成员；无在线编辑或任意图查询。已有 Go、IAM PostgreSQL、Ontology PostgreSQL、共享前端、Console 和 Ontology 前端标准入口覆盖 API/迁移/鉴权及确定性浏览器；CI 自动发现覆盖同一入口，无新增 Job。个人环境须由用户重启 System/Ontology 才加载权限及 API，不把受控 T3 当作正式 T4。

平台能力目录 `GET /platform/capabilities` / `platform.capabilities.list` 从同一只读 PG 快照列出完整核验的 active 定义，按 capability 升序，最多 32 项和 128 KiB；未激活不列出，空目录为成功空数组，损坏或超限整次失败，不截断/跳过/换源。沿用 `ontology.semantic.read` 和精确委托，拒绝 query、不缓存、不证明业务操作权限。Agent 以目录语义选择能力，再核对 Skill/Tool 装配；当前仅 Transfer 创建，真实短输入验收待重启后执行。门禁沿用 Go、Ontology PostgreSQL、Agent eval、授权与 Swagger 标准入口，无新增依赖或 CI Job。详见设计契约 1.12。

平台能力的 PG 内部记录使用独立的 `platform_capabilities`、`platform_revisions`、`platform_revision_events`，迁移 004。严格比较最后修订基线，允许代码部署跳过修订号；完整规范化 TEXT 快照、摘要和平台主体审计同事务存入，修订/审计不可更新或删除。记录本身不是授权或发布入口，不使用 Tenant Actor。迁移 005 增加同步平台发布投影及审计、机器发布 generation fencing 和 active 指针；迁移 006/schema 6 要求新记录使用包含来源证据的 v2 快照，保留旧字节但不支持旧格式消费。测试复用现有 PostgreSQL 门禁及 from 1/2/3/4/5 升级测试，详见设计契约 1.6–1.11。

`internal/platform` 唯一管理代码发布的 Transfer 能力定义，包含概念、关系、前置条件、效果、修订与摘要。2026-10-08 确认先不提供平台管理员在线修订。人工整理的 `transfer.json` 与仅供溯源的 `transfer.review.json` 经唯一 `Compile` 路径严格校验，形成绑定 `addp.platform-definition/v2` 与编译器版本的不可变快照；`Restore` 拒绝损坏、非规范及旧版本内容，不自动升级或退回源文件。Transfer 修订 6 的摘要同时绑定定义、来源与覆盖声明；每个成员均须关联依据，来源 owner/类别/路径/锚点/提交/文件 SHA-256 固定，标准 T1 检测漂移。修订 6 捕获创建 Tool v3 的标准目标类型与快照 replace/append 约束，并引入 Transfer 正式支持矩阵和 Planner 依据；删除快照 upsert 及 keys 的错误路径。输入枚举只由 Manifest 声明，不在 Ontology 复制；配置通过不证明实际值转换或执行成功。覆盖只声明当前创建切片及选定未建模范围，不宣称全模块完整。来源和覆盖保存在同一个 PG payload，FalkorDB 仍只投影概念及关系；Agent 上下文 v1 不携带来源全文。快照不是平台发布授权、PG published 或图 ready 凭证。`platform.capability.context` 通过 `GET /platform/capabilities/transfer.task.create` 返回 `knowledge_kind=platform_definition`；不伪造 Tenant 本体或运行可用性，不查询业务数据。现有 `ontology.semantic.read` 与精确 Tool scope 控制读取。定义中的 Tool/Skill 引用由 Manifest 和根 Skill 校验，不复制权限、HTTP 地址或凭据。

`skills/transfer-generation` 先消费平台语义，再经事实 owner 确认引擎、源结构与目标父节点，信息不足要求澄清。新增 `transfer.task.create` 仅创建 bounded/snapshot/native table、无计划、未启用且不自动扫描的任务定义，返回 idle/stopped；不能运行任务。System 迁移 181 只开放已有创建权限的委托标志，不给角色增加权限。确定性 Agent 场景 `platform-transfer-create`、Common Python HTTP 契约、Transfer owner 拒绝与零 execution 断言沿现有标准门禁执行；真实用户/LLM 对话验收待新服务加载后进行。

`backend/internal/semantic` 是正式 Go 语义内核；`internal/service`、`internal/repository` 管理原生定义的 PG 修订生命周期。`cmd/server` 已装配统一 Lifecycle、管理 HTTP API 和投影 Supervisor；FalkorDB 是私有 Infra。`frontend` 提供 Console 原生定义建模与修订管理页面；两个只读 Agent Tool 提供激活定义消费，不影响 Graph 的当前功能。2026-09-18 已在用户启动的个人开发环境完成真实用户的建模、发布激活和 Agent 定义读取联调；不把本地手工联调或 T1/T2/T3 当作正式隔离 T4 上线验收。

只读消费使用 `GET /ontologies/{ontology_id}/semantic/classes` 和 `GET /ontologies/{ontology_id}/semantic/classes/{class_id}`。后者必须绑定目录返回的 revision、generation、activation_version，激活变化返回 409；仅从同一 PG 只读快照核对过的 published/ready 定义读取，不读取草稿或实例。返回 `knowledge_kind=native_definition`，结果超过 128 KiB 直接拒绝，不截断。独立的 Tenant `ontology.semantic.read` 可委托给精确 Tool scope，管理权限仍不可委托。迁移 154 只登记权限，不给既有角色扩权。根目录 `skills/ontology-exploration` 组合目录和上下文工具，要求身份澄清、版本一致和定义/事实区分。

新增消费链路沿用 `make test-module MODULE=ontology` 的 T1/T2、`make test-agent-eval`（含 Common Python）、`make test-copilot` 与 System IAM `--package migration --test ontology-backend` 标准门禁。Skill/eval/SDK 路径由现有共享影响矩阵和 CI 自动发现；无新增外部依赖或 CI Job。本地真实身份联调已通过，正式 T4 仍未通过。

`internal/falkor` 提供唯一的 go-redis 薄适配及确定性图投影构建/全量校验；投影执行器连接首次发布/失败重建准入、Common 租约、构建前/激活前 System 授权消费和 PG 激活指针。Backend 绑定 HTTP 后异步注册，Ready 同时验证 PG、FalkorDB 非零查询预算及 System 注册，只有 Ready 才领取；退出等待在途执行与注销。

- 自有类、属性、继承和关系声明的严格校验；拒绝环、悬空引用、继承属性歧义及不一致逆关系。
- 确定修订的不可变内存快照；规范化 JSON 和 SHA-256 摘要包含租户、本体、修订、内核契约与编译器版本。它不是数据库发布成功的凭证。
- 受限 CEL 分类：布尔/字符串、等值、逻辑运算、字符串字面量有限集合成员判断；禁止宏、算术、动态类型、时间、正则和任意函数。
- 有限枚举同时校验输入值和直接比较/集合条件中的字面量，拼错的码值不能发布为永远不匹配的规则。未知状态应传 `unknown`，不将不认识的已知码值自动视为未知或 false。
- 已知、已确认缺失、未知、无效四种输入；输出 matched、not_matched、unknown、error。只有输入声明明确允许的布尔属性，才将已确认缺失映射为 false；未读取不适用该策略。
- 解释保留规则、输入状态与缺口，但仅为 `hypothetical` 试算，不认证业务事实。原始事实值不回显。内部错误不是 HTTP 错误格式，由试算 API owner 翻译及授权过滤。

Console `/ontology/ontologies/:ontology_id/trial` 提供当前激活规则试算，复用唯一 CEL 内核与共享参数输入组件。`POST /ontologies/{ontology_id}/semantic/rules/{rule_id}/trial` 仅允许 Tenant User 的 `ontology.semantic.read`，不对 Agent/Delegated 开放。手工输入四态及已知值，必须绑定 revision/generation/activation_version；执行前后检查 active，版本变化返回 409。输入最多 16 项、请求 96 KiB；不读取业务库、不持久化、不回显原始值。编辑输入会取消在途请求并清除结果，冲突保留输入且要求重新加载。现有 Ontology T1/T2/T3 自动发现覆盖 DTO/权限拒绝、四态求值、租户/版本隔离、撤回拒绝、页面与迟到响应；无新增依赖或 CI Job，未将此切片宣称为真实业务事实求值或正式 T4 验收。

关系声明目前只校验，不执行传递/逆关系的实例推理。仅支持原生定义；未来来源引用须遵守 owner 快照契约，不能加可编辑来源副本。

试算页不再把令牌轮换时短暂的 AuthContext 空值当作重载定义：重取期间隐藏内容、取消在途试算并清除结果，同一 User/Tenant/Membership 且语义读取权限仍有效时保留内存输入；身份变化、退出、失权或重取失败则清空。继续使用共享认证实现，页面不解析 Token、不保存凭据、不自动重发试算，也不解除已有版本冲突锁。T3 使用真实共享 Auth Store 与受控轮换令牌夹具验证刷新保留、身份隔离、失权/失败清空和迟到响应；沿现有前端与模块 CI 自动发现执行。

## PG 修订与发布

- `CreateDraft` 只接受同一本体的下一修订号；同一时刻最多一个 draft / in_review。
- `SaveDraft` 和 `Transition` 必须携带精确 version，状态沿 draft → in_review → published → withdrawn，允许审核退回草稿。错误版本不自动重试。
- `semantic.Restore` 严格核对规范化内容、摘要、内核契约和编译器版本；读取异常快照失败关闭，不隐式升级历史定义。
- 发布在同一事务内冻结修订、写审计、创建 `common.task_executions` 的 pending 构建意图，绑定修订、摘要和 generation；审计失败时全部回滚。数据库触发器保护审核/发布内容和追加式审计。
- 发布同时保存 pending 投影及激活基线。构建通过全量校验后，ready、active revision/generation、激活审计与 execution success 同事务提交；并发完成只允许基线匹配者激活。每次切换或清空指针都递增 activation_version，阻止空指针 ABA。
- 撤回与 pending 构建意图取消、当前激活指针清空同事务；不伪装取消已运行工作，也不回退旧版本。执行器在激活前再次核实撤回、授权、当前 attempt/token/租约及基线，不把 published 当作可运行。
- 单槽 Supervisor 仅在显式 Ready 时领取，使用 Common 队列、30 秒租约、5 秒续租和 25 秒执行预算。过期租约收敛失败，不重放结果不确定的图写入；失败保留原激活版本，不删除部分图。历史图清理尚未实现。
- `RebuildProjection` 必须显式指定仍为 published 的修订 version、failed generation 和当前 activation_version；旧 execution 必须终结且无租约。新 generation、新 execution、前驱关联及请求审计原子创建，同一失败批次至多一个后继，同一修订至多一个 pending/building 投影。重复/过期请求拒绝，不回放旧写入、不改原定义、不复用旧授权。ready 投影的主动替换不在当前范围。
- 修订的 generation/build_execution_id 是首次发布的不可变历史身份，不是当前投影指针。首次构建和重建共用 owner 投影记录、准入及执行路径；撤回会取消该修订尚未领取的后继投影，并阻断所有在途投影激活。
- Actor 只承载调用方已核实的租户/主体/成员/授权版本。创建投影先保存无授权的 pending 意图，唯一准入方法 `AdmitProjection` 显式接收 generation，用当前请求的 User Token 调用 System，核对该 execution 的请求主体及全部范围后原子附加授权；领取必须要求授权。重建可由当前已获发布权限的用户请求，不借用原发布者身份。签发或附加失败只关闭目标 generation 仍未授权的 pending 意图，撤回优先于迟到授权。不保存或交给 Worker 用户 Token，不得把内部服务直接暴露为未鉴权 API。
- `repository.Migrate` 使用 `common/schema.Migrate` 协调不可变的 `001_revisions.sql`、`002_projection_runtime.sql`、`003_projection_rebuild.sql`、`004_platform_revisions.sql`、`005_platform_publication.sql` 与向前的 `006_platform_review.sql`，重复同版本不执行。历史快照不重写、不自动解释为新格式；必须部署新修订激活后才可消费。生产 Ontology 只用 `schema.Require` 检查 common，不创建共享执行表。

## 代码与测试

### FalkorDB 适配边界

- `scripts/infra/falkordb.yml` 是 Infra Compose 与 T2 共用的数据库定义。常驻服务使用独立密码与卷，仅开放回环 `16479`；T2 使用随机密码/端口及退出必删的独占卷，并覆盖图快照在容器重建后的恢复。数据库启动就绪不等于本体投影 ready。

- 每条命令使用独占连接、无自动重试、RESP2 标量结果；取消关闭本请求连接并拒绝迟到结果，不承诺服务端立即硬中止。最多 4 条本地在途命令，满额立即拒绝。
- 查询必须携带 1–2000 ms 服务端预算，且不超过调用方剩余 deadline。服务端必须同时启用非零 `TIMEOUT_MAX/TIMEOUT_DEFAULT`；写超时回滚可能额外耗时。取消/网络错误后的写入结果不确定，禁止原地自动重放。
- `Plan` 只从已校验的不可变快照和 owner generation 派生物理 graph key。图内只保存类型、属性、关系、规则的成员身份/名称及结构引用；CEL、规则依据与业务事实不复制。传递/逆关系实例推理未实现。
- `Build` 原子保留空 generation，拒绝覆盖已有图；分阶段写入后由 `Verify` 比较摘要、完整成员/边集合及总数。失败可能留下部分图，不能据此激活；清理必须由后续 PG owner 核实引用与 lease 后协调，不在取消路径直接删除仍可能写入的图。
- 适配不是授权器。执行器在构建前核实授权和租约，在激活前再次核实取消、撤回、授权、lease/fencing 和激活基线。网络授权调用不能置于 PG 行锁事务中；owner 统一按本体头、修订、投影、execution 加锁，最后终态写入再次核实数据库时间。当前不提供任意 Cypher API，也没有跳过准入直接图写入的旁路。

System 的 `ontology` 内部执行授权固定绑定 semantic_projection、修订、摘要与 generation；`addp-ontology` 的 Tenant Runtime 只有 `system.execution_authorization.execute`，Platform Runtime 只有自身模块注册及固定平台定义发布权限。执行授权消费接口验证当前 running attempt/token 和 User 授权版本，不能消费引擎连接。平台定义发布不借用 Tenant 执行授权。System 启动需要独立 `ONTOLOGY_SERVICE_CLIENT_SECRET`。

### 管理 API 与部署

- 唯一前缀 `/api/v1/ontology`，Backend 端口 `8195`，Swagger `/swagger/index.html`。路由和 DTO 详见 `backend/docs/swagger.json` 及设计契约 10.1。
- `ontology.revision.read` 读取本体头、确定修订、确定投影；`ontology.revision.update` 创建/保存/提交/退回；`ontology.revision.publish` 发布/撤回，发布与重建另需 `system.execution_authorization.create`。全部由 Tenant 自定义角色显式分配，不自动扩张既有角色。
- Tenant 管理 API 只接受当前 Tenant 的 User Token。拒绝 Service、Delegated、资源票据以及非 Tenant 作用域的 Permission 候选；身份来自 AuthContext，请求体不能提交 scope、tenant_id 或 Actor。平台核实 API 独立要求 Platform User 和 Platform Scope 的读取权限，不开放 Tenant 管理操作。
- 发布/重建成功为 202，仅代表已准入。准入失败为 502 `projection_admission_failed`，响应 `intent` 保留已提交 revision/version/generation/execution_id，不能盲重发发布。重新读取确认后显式重建 failed generation；进程中断留下的未准入 pending 不自动补签，可撤回修订。
- `revision.initial_generation/initial_execution_id` 是首次发布身份；当前激活事实只从本体头读取，具体投影从 generation 读取。不回显执行授权、租约 token、图 key 或底层错误。
- 修订下 `GET /projection` 沿重建前驱链读取最新尝试，响应丢失时可找回身份；它不表示 active，没有投影返回 404，不依赖墙钟排序。
- 管理浏览使用 `GET /ontologies` 与 `GET /ontologies/{ontology_id}/revisions`，分别固定按本体 ID 升序和修订号降序。仅接受 page/page_size（默认 1/20，最多 100），拒绝重复、未知、非规范参数及 OFFSET 溢出；沿用 Tenant User 的 `ontology.revision.read`。同一只读 PG 快照内计数和取页，列表只给本体头/修订摘要，不加载定义 payload，不把 published 当作 active。详情仍由确定修订接口提供。T1 覆盖分页与路由拒绝，T2 的 `TestPostgresRevisionLifecycle/management_lists` 覆盖租户隔离、顺序、空页、首次发布身份和取消；现有模块/CI 入口直接覆盖，无新增数据库、迁移或 Job。
- `INFRA_FALKORDB_ADDRESS` 宿主默认 `127.0.0.1:16479`，容器为 `falkordb:6379`；只读取独立 `INFRA_FALKORDB_PASSWORD`。构建、Compose、`start.sh -ontology`、`restart.sh -ontology` 已登记；前端开发端口 `5192`、Docker `8123:80`，Console 数据治理组按上下文展示 Tenant `/ontology/ontologies` 或 Platform `/ontology/platform/definitions`。
- 若本机已有忽略提交的 `go.work`，使用 `go work use ./ontology/backend` 纳入模块后再生成 Swagger/开发构建；T1 同时校验生成文档中的原生定义字段，拒绝只保留路由但请求类型变成空对象的假覆盖。
- System 向前迁移 `000153` 登记 read/update 与 Platform Runtime，`000201` 仅为 `platform.ontology_runtime` 增加 `ontology.platform_definition.publish`；既有角色绑定触发器推进授权版本，不修改已发布迁移。Ontology owner 使用 schema v6。
- 平台机器发布核验复用 Common System Client SDK，只接受已编译能力修订摘要；System 固定 `addp-ontology` Platform Service 凭据和发布权限，不接受 User/Tenant/委托凭据，不缓存授权观察。启动发布器已装配该 Authorizer，管理员无发布权限。详见设计契约 1.9、1.11。
- `PlatformPublisher.Publish` 是最多 45 秒的内部同步机器入口：记录前、构图前、激活前均重新核验，同身份修订仅复用精确字节/摘要。每次使用新 generation；新尝试取代旧 building fence，旧尝试不能迟到激活；只激活最后记录修订，阻止旧部署回滚。复用唯一 Falkor Build/Verify，平台图使用 `ontology:p:`，包含概念、多语言名称及显式关系；完整操作契约仍在 PG。ready、active 和审计同事务提交，失败保留旧版本，取消不后台补写；没有 Tenant execution、自动重放或取消路径删图。PG `platform_publication` 子测试与 Falkor 门禁的 `platform_publication_real_graph` 自动覆盖，详见 1.10。
- 启动先绑定 HTTP 并发起 System 注册，独立且退出时等待的 `PublishOnReady` 编译部署包、等待既有 Ready 后只发布一次；失败只记安全日志，不阻断 Tenant 功能，不自动重放。`GET /platform/capabilities/{capability}` 唯一消费 PG active：只读可重复读事务核对头、ready generation、摘要及激活基线，再 Restore；没有记录 404，尚未激活 409，损坏 500。响应字段及 32 KiB 限制不变，成功不可缓存，删除运行读取源 JSON 的旧方法。真实 System/Gateway/Agent 验收仍待加载新服务后进行，详见 1.11。

### 标准门禁

`make test-platform-review` 是不依赖外部服务的平台来源漂移门禁，已纳入 `test-platform`，因此其他 owner 的标准模块门禁同样检查已捕获文件 SHA-256 与锚点。Go T1 另核验完整成员绑定、覆盖状态、摘要绑定、来源隔离和拒绝旧格式；来源变化须审查后更新依据及新发布修订，不能盲目刷新摘要。现有 Go/PG/Falkor CI 自动发现覆盖新增测试，未增加运行依赖或另起 CI 路线。

平台 Transfer 切片另用 `ADDP_SYSTEM_POSTGRES_TEST_DSN=<已核实端口的 addp_iam_test PostgreSQL URL> bash scripts/test/system-iam-postgres-gate.sh --package migration --test transfer-task-create` 精确验证迁移 181；默认 System IAM 门禁仍由 `AgainstPostgres$` 自动发现此测试，不将精确选择器视为全量通过。`make test-system-iam-runner` 验证选择器、默认发现和跨进程测试库互斥。

前端复用共享认证、主题、国际化、导航和离页保护；类型/属性/关系/规则编辑同一原生 definition，服务端独占校验与 CEL 编译。页面提供列表、修订历史、创建/保存/审核/发布/撤回及失败投影重建。精确 version 冲突或结果不确定时锁定后续写入且不覆盖编辑，显式重新加载后才解除；发布受理与当前 active 指针分开显示。原生成员移除只修改本地草稿，不级联删除引用，保存时由后端拒绝悬空引用。

`make test-ontology-frontend` 执行 T1 状态/请求契约、T3 当轮独立回环端口的 Playwright 受控 API 场景以及生产构建，不调用个人 Gateway。CI 的 Platform frontend matrix 已登记同一入口并安装 Chromium；模块门禁自动发现新前端。独立登录复用 AuthLoginFlow，支持 MFA 和上下文选择；平台上下文不提供建模权限。

后端模块：`github.com/addp/ontology`，Go 1.24.2。规则上限是版本化内核契约的一部分，不通过用户选项关闭；修改上限或表达式语义须升级契约版本。

v1 边界：64 类、256 属性、128 关系、64 规则；单类 8 个直接父类、继承深度 32；单规则 16 个输入、4096 字节表达式、256 个 AST 节点、深度 32、运行成本 256；字符串值最多 4096 字节，枚举/字面量集合最多 64 项，规范化前定义 JSON 最多 1 MiB。空属性枚举表示不限定字符串取值；字面量成员集合不允许空集合或重复值。属性不得覆盖继承链上的同名 key，菱形继承同一个属性不算冲突。v1 仅接受两端为同一类的传递关系声明；逆关系须双向显式引用，端点和传递性一致。

```bash
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  make test-module MODULE=ontology
# 并发安全与覆盖率仍走同一标准入口：
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  GOFLAGS='-race -count=1 -cover' make test-module MODULE=ontology
# 图适配及 PG/FalkorDB 联动门禁；无需传入个人图数据库地址或密码：
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  make test-ontology-falkor
# System 独立 IAM 测试库，只验证本切片的两步向前迁移：
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_iam_test?sslmode=disable' \
  bash scripts/test/system-iam-postgres-gate.sh --package migration --test ontology-backend
# 共享严格 JSON 绑定器与授权覆盖变更需扩散到 Go 消费方：
make test-go
```

标准模块门禁自动发现 `backend/go.mod`、PostgreSQL T2 和门禁自管的 disposable Compose T2，先预检连接条件，再执行 T0、T1、T2；T1 明确排除数据库连接变量与图集成开关。两个 T2 均进入根 `test-integration` 串行聚合、CI 独立 Job 和本地巡检的同一发现路径。

FalkorDB T2 固定 4.20.6 多架构镜像 digest，每轮独立 Compose Project、随机密码、动态回环端口、资源上限与非零服务端超时；不读取根 `.env`，不使用个人 Redis 或图实例。正常退出、失败和可捕获中断都删除自有容器/卷/网络并核验零残留。T2 覆盖投影往返/防覆盖/篡改检测/最大定义、参数转义与大整数、服务端读写超时/回滚、重复取消后的客户端关闭和服务端回收、并发结果隔离；同一门禁再连接 PG 验证发布、真实图构建、激活、失败保留旧版本及新 generation 重建恢复，使用可控授权夹具，不宣称正式 HTTP/IAM 端到端验收。PG owner 状态门禁另覆盖并发激活、ABA、授权失效、撤回、租约恢复、重建去重/祖先分叉拒绝、新主体授权、旧授权拒绝、审计回滚与 v1/v2 向前迁移。清理核对本轮修订及全部投影的 execution，不遗漏重建任务。服务端 TCP FIN 回收允许有界异步收敛，不以一次 CLIENT LIST 快照冒充泄漏判断。

本地只允许回环地址的 `addp_test`，CI 只允许 Job 独占的 `addp_ontology_test`。门禁拒绝清空已存在的 ontology schema；当次新建 schema 带随机 Run ID 所有权标记，正常退出、失败及可捕获中断都通过同一 owner 清理函数删除本轮 execution 与 schema 并检查残留，不清空 common。强制 SIGKILL 无法运行退出钩子，若残留需核对原 Run ID 后由 owner 门禁处理，不能直接删除未知 schema。

新增 CEL 依赖同时由 T0 的 `make test-go-dependency-policy` 检查；该入口覆盖单行/块状 `require`、错误版本拒绝和注释/排除声明不误识别，不能靠固定 go.mod 排版才能识别依赖。

Outdoor 合成示例用于测试夹具及用户显式创建的本地演示本体，不自动供应到其他 Tenant。北京是示例背景，不从活动名称猜测行政区。核心代码不得硬编码业务状态、集合名、字段路径或地名。`beijing_outdoor_demo` 修订 1 已在个人开发环境激活，Agent 会话 76 已验证两个定义 Tool 的真实调用；其中继承关系及简化报名集合是演示内容，不是可直接绑定真实 Outdoor 数据的业务模型，详见最小设计契约第 9 节。

正式业务定义 `internal/semantic/testdata/outdoor_business.json` 按已确认的 Outdoor 口径区分活动、人员和活动成员关系，无相互继承；三个规则分别表达统计有效活动、成员报名状态和成员实际参加状态。`business_fixture_test.go` 验证完整状态矩阵、上下文隔离、缺失/未知和错误输入；沿现有 `make test-module MODULE=ontology` 及 CI Go 自动发现执行，无新增运行依赖。本轮按用户确认不接 Catalog、字段映射或业务取数，不向业务定义包注入映射，也不自动将该文件发布到 Tenant。日期存在性是有明确完整性要求的输入观察；成员分类不隐式关联活动或构成统计人数。正式业务定义与合成演示用途不同，不能相互作为运行时回退。

2026-09-18 已由真实用户页面创建、提交并发布个人开发环境 Tenant 1 的 `outdoor_business` 修订 1，投影已激活；持久化定义与上述测试夹具逐项核对一致（忽略规范化数组顺序）。Agent 会话 77 的运行 `6877c35e-1d9a-4ce1-a194-85b71394c47d` 完成目录及两个类上下文的三次只读 Tool 调用，正确区分报名/参加口径、成员关系与活动粒度、日期缺失与未知，以及定义与真实业务证据。`make test-agent-eval` 通过；此记录仅为本地真实用户联调，不替代下述正式隔离 T4。

### 正式 Online 验收入口

`ontology-revision-lifecycle` 已登记为仅手工触发的 GitHub Hosted T4 suite；入口为隔离部署中的 `make test-online ONLINE_SUITE=ontology-revision-lifecycle`，生命周期由 `scripts/test/online-hosted-ontology-gate.sh` 复用统一 Hosted owner 管理。[2026-09-18 首跑](https://github.com/pampa0629/addp/actions/runs/35304960321) 在公共 Infra 拉取 `minio/minio:latest` 时被拒绝，未进入本体业务断言；退出证据为 `cleanup=passed`、`infra_cleanup=zero_residuals`。该项按用户决定暂缓，仍未取得真实 T4 通过证据，脚本单测和本地开发服务 Ready 不能替代它，也不阻断独立的建模入口开发。

场景复用 `internal/semantic/testdata/beijing_outdoor_online.json` 的北京 Outdoor 合成本体；语义内核 T1 同时验证此定义可编译，并区分明确北京、明确其他与未知地点。Hosted helper 只给 User 分配本体 read/update/publish 与执行授权派生四项权限，不创建 Engine Provisioner 或业务 Engine。所有本体操作通过 Gateway，核对两次发布的不可变快照、generation/execution、ready 与 active 指针、旧版本冲突、最新尝试发现，以及撤回第二修订不自动回退第一修订。

没有业务实例查询或分类 API 的验收。修订、审计和图历史在场景中保留，退出时停止当次应用、销毁整个独占 Infra 及数据卷和临时凭据；生命周期另查容器/网络/卷零残留。发现旧 Infra 容器或卷时必须在启动前拒绝，不能误用或删除既有环境。确定性脚本验证由 `make test-online-runner` 纳入 T0，实际执行由 `.github/workflows/online-t4-gates.yml` 的 `ontology-hosted-t4` Job 完成。
