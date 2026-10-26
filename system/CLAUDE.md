Graph 保护投影 Runtime（000218）：只向既有 `tenant.graph_runtime` 发布 `security.protection_projection.read/update`，向 `platform.graph_runtime` 发布 `platform.tenant.read` 以发现待同步租户；不新增用户授权或 Role Assignment，不调整源数据访问授权。绑定变更沿现有 IAM 触发器推进服务主体授权版本。验证使用 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test graph-protection-runtime"`，完整迁移门禁与既有 System CI 自动发现。

# CLAUDE.md

Meta basic 采集资格由 System 唯一保存及裁决，首期限定 PG entire-engine `ScanTask`，绑定真实任务功能摘要、引擎版本、用途和明确期限。独立批准／读取／撤销 Permission 与当前引擎管理资格取交集；Meta Runtime 的当次消费同时核对当前 Common execution／lease 和 Meta 事实，不签发普通源数据 Grant。000234 发布最小审批与双向机器事实读取职责，不自动分配账号、不批准旧任务、不改变现有 Grant／Deny。标准定向验证：`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package engineaccess --test meta-collection"` 及 `--package migration --test meta-collection`，默认 T2 同样自动发现。审批工作区由 `EngineMetaCollectionQualifications.vue` 唯一拥有，入口为引擎授权 `tab=collection-qualifications`，只在 PG 存储引擎及独立 Permission 下提供。新增批准先读任务事实，原命令恢复不重签，撤销冲突必须显式刷新再确认；记录状态不等于执行 Allow。前端使用既有 `make test-system-frontend`，无需新路由或 CI 入口。详情见 `system/docs/IAM数据模型与迁移规范.md` 和 `docs/spec/addp授权上下文规范.md`。正式 API／Worker 及采集资格审批页真实登录、核对、批准、同命令恢复和撤销的限定 PG basic 跨服务 T4 已通过，具体证据及未验范围见[验收证据索引](../docs/spec/testing/验收证据索引.md)；后续固定 `0bd708dc9` 的真实 T4 还验证资格到期前执行成功、过期命令重放不续期或复活、期限后下一次执行拒绝，以及同一凭据／资格历史不变和零残留。固定 `d6a7f555d` 的真实 T4 还验证 PG basic 原一分钟 Cron 的未批准拒绝、同资格两次定时成功及撤销后定时拒绝，重放原命令不复活；结束关闭调度，资格／任务和源规则零残留。非预期致命失败清理仍以确定性反例为证，不宣称 Hosted 已注入该故障。不以此替代排队／运行中变更及在途到期／撤销竞态、deep、其他 selector／引擎或生产容量验收。

数据授权办理员统一角色：`tenant.source_data_authorizer` 同时组合直接 Grant 创建与 Catalog 人类办理 Permission，并包含 Catalog 条目读取。两种流程继续按当前目标批准方式、引擎管理范围及业务确认分别核验；不增加确认、Deny、模式切换、机器 execute 或数据读取权限。000217 仅扩展该既有角色，推进已分配主体授权版本并撤销旧会话，不新增账号分配、委派或 Grant，不修改租户自定义角色。下文 000178／000197 的描述记录原迁移范围，当前模板以 `system/authorization/builtin_roles.yaml` 为准。

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

批准方式显式切换（2026-10-08）：`PUT /api/v1/system/engines/:id/access_approval_requirements/:requirement_id` 是已有精确目标安排的唯一更新入口，只接受正整数 `version`、`mode` 和原因，目标及当次承接人从原记录和当前用户派生。独立 `.update` 功能权限与当前引擎管理资格取交集；000200 默认仅加入引擎授权管理员模板，并推进已分配人员的授权版本、撤销旧会话。普通数据授权办理员不默认获此权限。切到直接批准还核验本人 `.engine_access_grant.create`，记录本人为当次承接人，但不授予读取权、不成为永久唯一审批人。事务复用身份→引擎／委派→精确目标锁；等待及审计后按数据库墙钟重核资格，版本冲突返回 409，同模式不递增版本或追加切换审计。跨租户／引擎返回 404。切换不改既有 Grant、撤销关系、旧业务决定或已受理原请求。前端已有表的批准方式旁提供切换、原因及确认；失败保留输入，须显式只读刷新核对后再确认，不自动补版本或重试。下文“已有要求不可覆盖”仅限定首次初始化，不能阻止此显式更新。

> IAM 概念与平台契约以 `docs/concepts/addp账号与权限体系图.md`、`docs/spec/addp授权上下文规范.md`、`docs/spec/addp权限与角色发布规范.md` 和 `docs/spec/addp OAuth授权规范.md` 为准；System 数据与协议实现以 `system/docs/IAM数据模型与迁移规范.md` 和 `system/docs/OAuth与Fosite实现说明.md` 为准。当前实现已切换为 Principal、Tenant Membership、Role/Permission、Token Family 和 `addp.auth_context/v1`，不得恢复旧账号分级或平行认证路径。

Standard 平台计量目录：000212 将既有 `standard.unit.*` Permission 扩展至 Platform Scope，并只给 `platform.system_administrator` 增加平台目录读写权限；现有 Tenant 角色保留原权限。角色权限触发器推进授权版本，迁移撤销平台系统管理员的旧 Token Family。目录与译名仍归 Standard，System 不保存单位副本；精确回归入口为 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test standard-platform-units"`。

> Enterprise Catalog（企业资源目录）由独立 Catalog 模块拥有。System 只提供 Tenant、Department、Project Group、User、成员关系、AuthContext、模块注册和 `addp-catalog` 服务身份，不保存 CatalogEntry、业务语义关联、责任关系或企业目录搜索投影。

> 源数据授权职责：System 的引擎访问控制领域唯一维护以 Meta DataItem 为目标的源数据规则；PG 普通表及 FTP IFC／3DS 精确文件组成的已验收范围、其他入口和格式的剩余边界统一见 `docs/spec/addp授权上下文规范.md` 第九节及其验收证据索引。System IAM 不接管所有业务对象 ACL，不建立可独立编辑的 Catalog 责任副本。Catalog 与 System 的办理入口应使用同一规则写路径；各访问模块执行同一权威规则。业务确认／办理分工、共享期限及责任移交以该规范 5.5 为准，不能把现有 Engine ID + read/write/ddl 执行范围视为表级授权已经实现。

PG 新成果初始授权的内部准备消费 Transfer 不可变实际 ReadSet，逐项核验来源／目标引擎版本及 Meta 完整边界；当前源规则、执行 IAM 和目标批准／办理／Grant／Deny 历史在同一只读快照观察，不签发或冻结期限，不把未知来源、已解除限制或空历史解释为首次新建证明。正式签发在准备之后沿唯一 Grant 写入链执行，只以已证明的新建提交为资格，不要求跨库保持原物理对象。标准验证为 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package engineaccess --test production-sources"`；新增 exercise 也由默认 engineaccess 门禁自动发现，使用独立操作预算，不延长已有测试的资格期限。

Manager 剖析结果的固定同步源检查 `POST /engine-access/read-checks/manager-profile-result` 只接受当前普通第一方／OAuth API User，核验固定 `manager.data_item.read` 与结果实际历史完整 ReadSet 的现行源规则；与预览共享只读快照底座，但不开放委托、Service 或 Resource Ticket。仅返回不可缓存的当次观察时刻，不授予剖析执行权限，不替代 Manager 本地 Security；缺失历史来源由 Manager 拒绝，不用当前视图依赖补造。

Manager 正式剖析执行授权由 `iam.ManagerProfileAuthorizationService` 与同事务的 `engineaccess.VerifyExecutionSourceRead` 组合。000192 在既有 Execution Authorization 上加入不可变 `source_read_scope`，只接受精确 pending、普通 API User、单引擎只读的 `data-profile-config/v6`，冻结全配置摘要及完整 ReadSet；消费复核当前 Manager Runtime、真实 running claim、发起人功能及全部源规则，审计后再次检查到期。仅给内置 Manager Runtime 增加已有机器消费 Permission，不给用户或源资源补权。固定 HTTP 签发入口 `/auth/execution-authorizations/manager-profiles` 仅接受 execution UUID／有效期，核验两项 Manager 功能 Permission；消费入口 `/execution-authorizations/:id/manager-profile-accesses` 仅接受固定 `addp-manager` Tenant Service 和真实执行租约，返回不可缓存的当次观察时刻，不返回连接或访问租约。范围类型及纯值校验唯一归 `common/execution`，共享客户端不自动重试或跟随重定向。禁止通过 engine-accesses、Notebook 或服务定义绕过范围；Manager 生产采样仍未接通，继续明确拒绝无正式来源授权的剖析。

> 正式受理归属（2026-10-01 已确认，分步实施）：Catalog 管业务批准及当前责任核验，System 的引擎访问控制领域持久保存最小正式受理回执。回执与批准要求变更、Grant 写入在同一精确目标边界排序，不复制业务责任或业务决定正文；不是访问凭据或 Grant。原 5 分钟窗口不因重试刷新，退出不取消退出前已受理的同次、同参办理。迁移 000167 和内部仓储提供同次受理／关闭互斥、不可变结果及事务审计底座；000168 增加精确目标的版本化批准要求，新受理消费当前 `catalog` 模式及版本，退出／重新启用与受理共享目标锁。承接人仅用于当次资格核验及交接审计，不是永久唯一审批人。正式准备、可信 owner 反查、首次受理及受理后的 Runtime Grant 签发／历史查询已接通；Catalog 通过唯一续办路径自动签发，PG 普通表 single 经 Manager 真实读取和收权的闭环已由 `pg-source-authorization` Hosted T4 验收；其他引擎及消费入口仍须独立接入。不以 Catalog 本地锁或独立时钟冒充先后证明。

> 内部核清读取：`engineaccess.Repository.readFulfillment` 使用独立只读事务，与仲裁共用完整请求绑定匹配，不取仲裁锁；未找到不等于关闭，退出或过期不改写原回执。拒绝在受理写事务中暴露自身未提交结果。该仓储不替代可信跨模块核清接口、当前资格判断或实际 Grant 消费。

> 内部目标管理底线：首次受理在 IAM 身份锁之后、请求／目标锁之前，以共享行锁读取本租户目标引擎及原操作账号 Membership 的有效管理委派，业务核验后再次按数据库墙钟检查。不同目标可并行，提交前的引擎状态变更和委派撤销须等待；失效资格不能产生新回执，但不阻断关闭或历史核清。管理委派不授予内容读取，不代替独立办理 Permission、可信业务决定或 Grant 写入时的当前核验，正式 Runtime 首次受理已消费这些核验，但不写入 Grant。

人类办理范围观察（2026-10-02）：`GET /engines/:id/access_handling_scope` 使用当前 Tenant User 身份、独立 `system.engine_access_fulfillment.create` 与有效引擎管理委派；按身份→引擎／委派共享锁核验当前 IAM、Token 及到期，等待后按数据库墙钟复核。000178 只登记该 Tenant Scope、高风险、不可委托、可租户定制的权限，不默认授予角色，不新增 Assignment 或 Grant。观察可供 Catalog 的人类候选摘要读取，不是可复用凭据，不授予确认或内容访问，不依赖 Catalog 在线，也不是首次受理接口。

精确目标办理要求观察（2026-10-03）：`POST /engines/:id/access_handling_requirement` 使用同一当前 User 办理权限及委派核验，仅接受完整路径的 `version`／`segments`，Engine ID 唯一来自路径参数。结构路径可能超过安全 URL 长度，故采用只读 POST；仅返回精确叶子的 `mode` 及字符串 `requirement_version`，不继承父要求、不枚举配置、不初始化缺失要求（404），不创建 Grant、待核清或批准要求变更审计。正式准备仍核验原版本，不因观察而扩大配置 `.read` 权限；HTTP 请求审计不记录正文。Catalog 人类办理页已消费此接口，刷新原请求仍只用 GET。

> 内部操作来源底线：完整请求不可变绑定 User Principal、Tenant Membership 和授权版本；首次受理按身份 → 请求 → 目标顺序，在 IAM 共享行锁下核验当前身份、成员关系、租户及成员到期时间，业务核验后再查数据库墙钟。共享锁与审计外键引用兼容，不阻塞不同目标的独立读取核验；现有管理委派写入仍保留原写锁。关闭与只读历史核清不要求原操作人仍有效，不由历史结果恢复新办理资格。身份引用本身并非可信来源证明；正式受理另通过 `addp-system` 反查 Catalog 已提交待核清事实，并独立核验办理 Permission、管理委派及当前确认人 IAM；签发仅消费已提交受理回执，不接受浏览器自报批准。

> 普通只读共享有效期：每次显式选择 `at_time`（未来绝对到期时间）或 `until_revoked`（无到期日期，直至撤销）；遗漏模式或矛盾参数拒绝，模式与日期均绑定不可变请求。自动办理窗口仍为原受理时间起 5 分钟；限时共享另受授权到期时间截断，长期有效不延长办理窗口。000172 在排他锁与同一事务内将历史有限期回执无损标为 `at_time`、补齐绑定并恢复不可变触发器，不创建 Grant 或改变历史时间。临时接入、管理委派和敏感操作规则不变。

平台节点指标 Hosted T4 的 `cmd/online-test-fixture --suite platform-node-metrics` 使用正式三员 Bootstrap，独立准备临时系统/安全管理员登录及 TOTP 凭据，不直接签发平台 User Token。两个不同的非默认 Tenant User 仅授予 `monitor.execution.read`，用于验证平台入口越权拒绝。凭据只写入 owner-only 临时文件，身份随当次 Hosted Infra 销毁；真实登录/MFA、节点/目标管理与独立 Prometheus 发现身份由同名 T4 suite 验证，身份数据库回归纳入既有 Online fixture PostgreSQL 门禁。

## 项目概述

Meta 授权资源事实核对使用 `GET /api/v1/system/engines/:id/data-items/:fingerprint/authorization-facts`：当前 User 须同时具备 `system.engine_access_grant.read` 及本引擎管理资格。System 在 IAM 事务外以独立 `addp-system` Tenant Service 读取 Meta，返回后重核 User 资格及 Engine；只读、不签发 Grant。迁移 000205 仅向 `tenant.system_runtime` 增加 `meta.authorization_facts.read`，复用可选 `SYSTEM_SERVICE_CLIENT_SECRET`；缺失或依赖故障明确 503，不影响 Ready。当前实施和端到端验收聚焦 PG schema 普通表 single，批准与 Manager 的源规则核验绑定数据项指纹及边界摘要。共享协议与数据库身份约束可表达 MySQL database 普通单表及 MongoDB database 单集合，但不表示这些引擎的 Manager 实际读取已接入；不将协议或仓储测试通过解释为其他引擎的数据授权闭环。000207 清理已获准丢弃的旧源授权测试事实，不清 IAM、角色、委派或通用审计；Grant 匹配身份与摘要，Deny 仅按同 Tenant／Engine 的稳定身份持续约束。新请求必须提交预期边界摘要，服务端独立重核，历史恢复不自动重批或续期；共享协议和未完成范围见平台《元数据扫描机制规范》的“授权资源描述契约”。

Hosted Security T4 的一次性 IAM 夹具复用 `backend/cmd/online-test-fixture` 正式 IAM Service 路径；验收 User、引擎登记 User 与治理初始化 User 分别授权。Security 分类、等级和检测绑定的创建权限仅授予准备用户，不进入四出口验收身份；凭据由 Hosted 生命周期在业务验收前移除，完整身份随当次 Infra 销毁。

正式 Runtime 首次受理（2026-10-03）：`POST /runtime/engine-access-fulfillments/:request_id/accept` 限定当前 `addp-catalog` Tenant Service 身份及 `.execute`。System 在本地锁外使用独立 `addp-system` Tenant Service OAuth 反查 Catalog 精确待核清依据，再按稳定 IAM 锁顺序核验原办理人、确认人、接收主体、独立办理权限／委派及批准要求版本；结果和审计原子提交。000180 仅给 `tenant.system_runtime` 最小 Catalog 依据读取权。可选 `SYSTEM_SERVICE_CLIENT_SECRET` 缺失不影响 Ready，只阻断新受理并返回明确 503；历史结果恢复和关闭不依赖它。移除配置停用 Client 并撤销原 Token Family，不借用其他模块凭据。受理回执本身不是 Grant 或实际读取许可；PG 的自动签发、Manager 真实读取、Deny 与收权另由 `pg-source-authorization` Hosted T4 验收，不把该结果推广到其他引擎或消费入口。

幂等 Grant 签发：000195 将 `system.engine_access_grants` 收敛为精确源规则的唯一不可变存储，保存目标、接收主体、动作、期限和操作者来源；批准来源由 `approval_mode` 区分。Catalog 签发以同编号 `catalog_request_id` 引用 accepted 回执并由数据库提取规则，原窗口及历史恢复契约保持；独立来源不伪造回执，匹配当前独立批准要求版本。已有签发、受理与撤销保留原编号和时间，不重新授予。`engineaccess.IssueFulfillmentGrant` 经 `writeAcceptedGrant` 核验当前机器、原办理人版本／权限、引擎管理委派与接收主体，按身份→引擎／委派→原请求→精确目标排序；唯一 Grant 插入归 `insertSourceGrant`，与审计原子提交。锁等待和审计等待跨过原窗口都回滚；已有同参重试只恢复原 Catalog 历史，不要求原操作人仍有效、不延长期限、不重新调用 Catalog 或用后续批准要求否定已经受理的同次请求。Runtime 的签发／历史查询仅消费 Catalog 来源，不把独立签发投影为 Catalog 办理。读取与撤销直接消费同一 Grant 规则，不再拼接受理回执。关系撤销保留独立 Permission、管理资格、到期拒绝和原参恢复语义，同次收回所有仍有效的重复记录。000196 发布独立 `.engine_access_grant.create/read`，不默认分配角色或账号。公开 `POST /api/v1/system/engines/:id/access_grants` 对已显式配置独立批准的精确普通表或集合叶子签发 read Grant，`GET` 同路径读取当前授权关系，`GET /access_grants/history` 读取两类来源的完整签发及撤销历史；均要求当前 User 对应功能权限及管理资格（当前租户授权管理员或本引擎有效受托办理人）。源端通过 Provider ResolvePath 精确核验，在 IAM 锁外完成；MongoDB 普通集合仅查询名称和原生类型，不读取文档样本，正式写入再核验引擎版本、批准要求和接收主体；Grant 与审计原子提交。编号原参重试仅恢复历史，不续期、不恢复撤销，也不依赖 Catalog。000202 拒绝有效关系重复签发，保留存量重复及审计；当前关系为派生管理视图，不是实际访问裁决。

精确目标批准要求初始化（2026-10-07）：User 在当前 Tenant Context 中通过 `POST /api/v1/system/engines/:id/access_approval_requirements` 显式建立版本 1 的要求；只接受完整结构化路径、必填 `mode=catalog|independent` 和原因，不接受版本、承接人或操作者身份。独立 `system.engine_access_approval_requirement.initialize` 与管理资格取交集；读取使用独立 `.read` 与管理资格。两项权限由 000174 登记，000197 仅组合进专门的源数据授权办理员模板，不扩张其他角色或自动分配账号。身份 → 引擎／委派 → 精确目标锁，等待后按数据库墙钟再核验资格，配置和审计同事务。重复初始化返回 409，不覆盖既有模式，也不创建 Grant 或授予独立批准 Permission。首次配置不同于已有要求的交接；保存 Catalog 业务负责人不切换批准方式。可信跨模块首次受理已接通，不存在批准要求不能视为独立批准。API 不依赖 Catalog 在线；Catalog 前端复用已选有效共享确认的完整目标并明确提交 `catalog` 模式，单独填写原因显式初始化，不根据观察失败自动写入；成功后重新只读观察版本，不将初始化响应当作 Grant。System 引擎列表“授权”入口的“数据授权”分区已提供要求分页读取和首次显式配置；通过共享 ResourceTreePicker 的 System Engine Catalog 适配器浏览实时普通表，不依赖 Meta 扫描或 Catalog 编目，不读取表内数据。选择、加载、刷新和 URL 恢复均不写配置。选择 schema/node 后展开当前已有普通表为固定清单，最多 200 项，可逐项移除，不递归授权、不包括未来新增表或视图。直接批准首次使用唯一 Grant 命令的 `initialize_approval=true`，同时核验首次配置权限，批准要求与 Grant 原子提交；逐表显示结果，部分失败仅重试原编号原参数的失败项。已有要求不可覆盖；业务批准只初始化要求并提供引擎与表名筛选的 Catalog 跳转，不绕过业务确认。初始化请求序列化由 common-frontend 单一实现持有，保留 int64 十进制精度。同页提供独立批准表的“授予读取权限”和统一授权历史、撤销入口：接收账号／部门／项目组来自有权查询的当前有效 IAM 候选，显式选择指定期限或直到撤销并填写原因；确认摘要后才提交。结果未知保留原命令原参数，用户明确重试，不自动重发新授权。表已有 Catalog 批准安排时不显示独立签发入口；签发历史不代表当前访问许可。

2026-10-08：000198 将现有委派管理员模板完善为“引擎授权管理员”，显式组合初始化、Grant 创建／读取／撤销和候选读取权限。当前租户有效的 `system.engine_access_delegation.create` 表达管理员管理范围，管理员无需委托自己；具体功能仍逐项核验，不能仅凭角色名称放行。角色扩展不创建委托或读取 Grant；已分配账号的授权版本推进、旧会话撤销，需重新登录。000199 仅保存不可变初始化意图，不改变历史授权事实。

引擎列表的“授权”操作与“详情”并列，打开独立的引擎授权窗口；详情只展示基本信息、连接配置和能力声明，不再包含授权页签。授权窗口复用原有组件，按独立功能权限显示“数据授权”和“委托授权”分区：前者配置精确表批准方式、签发或撤销读取 Grant，后者通过引擎授权管理委派指定谁能办理，两者互不授予或替代对方的权限。授权入口优先打开有权使用的数据授权分区，否则打开委托授权；仅有引擎读取权限时不显示入口。

引擎授权窗口的 `tab=delegations`（委托授权）提供管理委派分页查询、创建和撤销，分别要求 `system.engine_access_delegation.read/create/revoke`；进入引擎详情或授权窗口仍需 `system.engine.read`。接收账号复用 `TenantMemberSelect` 和有权读取的有效 Tenant Membership 候选，不手填 ID；创建要求显式未来到期时间及原因，不自动分配角色或数据读取权。管理委派变更会撤销接收账号的旧会话；撤销管理委派不撤销既有源数据 Grant。无权限的 Tab 规范化为基础信息，草稿不写入 URL 或浏览器存储。

委托授权使用互斥的“新增委托／已有委托”视图，有读取权限时默认展示已有记录及其有效、到期、撤销状态，不与新增表单混排；只在进入新增视图时查询账号候选。切换视图不写委托，取消清空草稿，创建成功后回到第一页已有记录；无读取权限的创建者保留在新增视图，不因创建成功获得查询权。失败保留输入，不自动重试；提交或撤销期间锁定视图切换。

引擎授权委派管理员内置角色只包含委派读取／创建／撤销、`system.engine.read` 与 `iam.tenant_membership.read`，仅允许 Tenant Scope 的 User；通过正常 IAM 角色分配显式授予，不回填已有账号、不改写其他管理员角色。角色本身不授予数据访问，也不自动生成委派。

源数据授权办理员内置角色 `tenant.source_data_authorizer` 由 000197 发布，组合批准要求读取／初始化、独立只读 Grant 创建／历史读取／撤销及必要的引擎结构和 IAM 接收方候选读取。仅 Tenant Scope 的 User，通过正常角色分配显式安排；没有当前租户授权管理员资格时，仍另须目标引擎的有效管理委派，不提供委派管理、Catalog 业务确认／履约或内容读取。角色发布不改变已有账号、会话、委派或 Grant；已有角色分配权限的管理员给自己分配时沿用 MFA 增强认证，不新增自批旁路。

PG 任务产出初始授权沿同一源 Grant 表与读取／撤销链管理，来源为 `task_production`。迁移 000225 发布仅 `tenant.transfer_runtime` 持有的不可委派、不可自定义 `system.engine_access_initial_grant.execute`，固定 Tenant Service `addp-transfer` 调用 `GET/POST /runtime/initial_production_grants/transfer/:execution_id`。无请求体或自报身份／目标；System 以自身统一 client 反查 Transfer v3 成功产出、完整实际 ReadSet 与 Meta DataItem 边界，锁内重核当前人类来源、执行功能权限、来源 Grant/Deny、期限和目标全部授权历史。成功与唯一 Grant、审计原子提交，拒绝只保存不可变终态；暂时依赖故障返回安全 503、不生成终态。相同原执行只恢复原签发或拒绝，不续期、不复活撤权，历史查询不依赖 Meta 或原人类仍有资格。人类来源与机器签发者分别留痕，不伪造人工批准要求；Transfer 后台续办与 Meta 限定基础补扫已接入，对应 T1／PostgreSQL T2 及 PG 普通表新建成果的 Hosted T4 已通过，涵盖唯一初始 Grant、Manager 实际读取及源／成果分别撤权；不证明 Develop、其他引擎或 multi／whole 成果初始授权，见[验收证据索引](../docs/spec/testing/验收证据索引.md)。

内部业务确认资格（2026-10-02）：首次受理还须从 owner 业务决定获取原确认人的身份引用，与办理人及接收账号共同去重升序共享锁定。确认人的历史授权版本只作审计，实时核验原 Tenant Membership、当前身份及 `catalog.entry.read`／`catalog.sharing_decision.create` 的有效 Tenant Scope 授权；原办理人仍严格匹配请求版本。权限和成员自然到期在业务核验后按数据库墙钟复核。历史核清及未受理关闭不重新要求确认资格；内部入参不构成可信 owner 证明；正式 Runtime 受理在数据库锁外通过独立 `addp-system` Tenant Service OAuth 反查 Catalog 原责任依据，事务内核验独立办理 Permission。后续签发接口只消费本域已经受理的精确请求，不提供新的独立批准入口。

**全域数据平台 (All Domain Data Platform)** 是企业级数据平台的核心能力模块，提供基础系统功能：
- 统一 IAM（全局 User、Tenant Membership、组织、角色、权限和平台三员分立）
- 日志管理（审计日志存储和查询、统计分析、导出）
- 引擎管理（通用引擎连接、扩展运行时注册与能力展示，含 Schema/表枚举）
- API 消费方（外部数据面 API 调用方登记、服务授权与 API 消费凭据管理）
- 资源回收管理（跨模块评估和执行资源回收）
- 模块注册与发现（供 Gateway 动态路由）
- 平台节点台账：`/platform/host_nodes` 创建/分页读取/版本更新；独立 `platform.host_node.*`，Platform User、默认平台系统管理员。模块实例通过部署 `ADDP_HOST_NODE_ID` 声明，System 从真实 Client 校验节点允许集合；首次声明不可改挂，拒绝/撤销关联不影响登记与租约。节点基础不含 SSH 或采集；引擎部署另走独立登记与显式承载声明，见平台运行观测规范 4.1、4.4。
- 平台引擎部署：`/platform/engine_deployments` 管理 Infra／Business PostgreSQL 来源快照与明确的单主机承载声明；稳定部署 UUID 与租户 Engine 分开。独立平台权限、版本 CAS、数据库时间声明区间和审计同事务，不探测 SQL/Docker、不授予业务数据读取。System 前端“平台引擎部署”提供自动发现、批量确认及详情的最近来源展示；T2 精确入口 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package repository --test engine-deployments"`，权限迁移用同入口 `--package migration`，均由既有门禁自动发现。
- PostgreSQL 专业观测沿唯一私有观测投影增加已纳管、主机关联有效的 Infra／Business 指标 HTTPS 端点与当前数据库容器 ID／启动时刻。部署发现 v2 的专业来源字段仅在该投影使用，不进入普通浏览器 DTO、管理员确认摘要或部署声明；后台身份不代替当前用户读取授权。
- TaskProvider 模块角色声明与动态发现（供 Orchestrator 查询调用）
- 数据存储在 PostgreSQL 数据库（system schema）

技术栈：
- **后端**: Go + Gin + GORM + PostgreSQL
- **前端**: Vue 3 + Vite + Pinia + Vue Router
- **部署**: Docker + Docker Compose

## 多租户架构

### 统一身份与上下文

- User 是全局自然人身份，Local Account 和 External Identity 是登录凭据或身份绑定；
- 一个 User 可拥有多个 Tenant Membership，但一个 Tenant Context 只绑定一个 Membership；
- Platform Context 与 Tenant Context 互斥，平台角色不自动获得租户业务数据权限；
- 平台最高管理职责拆分为系统管理员、安全管理员和审计管理员；三员 Role Assignment 两两互斥，职责与日常写权限分离；管理员治理按目标岗位矩阵申请／复核，审计员只有限复核系统／安全管理员请求，不获得普通账号写权限；
- 所有管理和业务操作使用 AuthContext 中的 Role Assignment 与 Permission，不根据账号名或身份类别放行。
- 平台安全管理员创建普通 User 和凭据；平台系统管理员创建/初始化 Tenant 时必须指定该 User 为首位 `tenant.administrator`。Tenant、首个 Membership、首个 Assignment 和审计同事务，平台三员不得成为首位 Tenant Administrator。

### 数据隔离

**租户级隔离**: 所有功能和数据按租户隔离，包括：
- 存储引擎连接配置
- 审计日志记录
- 数据管理（Manager模块）
- 元数据信息（Meta模块）
- 数据传输任务（Transfer模块）

**隔离实现**:
- Tenant Context 的 Tenant 和 Membership 由 AuthContext 提供，客户端不得自报；
- 引擎、日志等业务事实关联 Tenant ID；
- API 先执行 Context 与 Permission Guard，再由 owner 查询和资源策略完成最终隔离。

## 平台模块运行日志

- 模块管理的服务实例页提供正文查询，日志链路页只提供来源健康、策略和活动事件数，通过带来源上下文的链接进入 Monitor `/platform-alerts`。确认、抑制、通知及失败重投仅由该 Monitor 页面提供。正文读取与来源诊断、告警管理权限独立，不由页面推断租户执行事实。
- 服务实例资源观测使用既有实例身份和租约：迁移 000204 保存不可变私有 `process_metrics` 声明，只有 Common SDK 实际启动的来源随正式注册发布；普通 DTO 仅公开 `process_metrics_declared`。平台实例列表支持最多 100 个 `ids` 精确过滤，仍按当前 User 的 `platform.module.read` 裁决。专用后台投影可发送有效未绑定主机的进程来源，主机关系失效只移除关系，不伪造承载事实；Monitor 查询仍先执行用户实例授权读取。设计及分层验收见平台运行监控设计第 6 节。
- Monitor 通知投递列表显示当前目标名称/版本、原事件类别/主体/类型/发生时间和告警当前状态。最终失败重投同时需要 `monitor.platform_alert_notification.read/update`；事件查看与处理使用 `monitor.platform_alert.read/update`，均限定非委托 Platform User。迁移 000209 只向既有平台系统管理员发布四项权限并停用旧日志通知键，不改变租户授权。
- Monitor 主机资源规则的四项平台权限 `monitor.platform_resource_rule.read/create/update/delete` 由正式迁移 000211 发布，仅授予既有平台系统管理员；角色变更推进授权版本并撤销旧刷新族。创建/修改规则仍通过当前用户核验 System 主机，不把后台最小身份投影用于用户授权。
- Monitor 重投确认包含历史恢复提示，提交仅带预期人工重投次数与目标版本，保持原投递身份；当前目标仍须订阅原事件类别。成功表示已重新入队，刷新当前页；冲突保留失败信息并要求刷新。累计、本轮和人工次数分别展示，不把累计领取次数解释为实际网络请求次数。

## 资源观测的身份投影

平台实例列表 `GET /platform/module-instances` 可按 `process_metrics_declared` 查询正式进程采集声明是否存在。只接受单个规范 `true` 或 `false`，省略不限制；空值、重复及其他值返回 400。与模块、角色、有效在线状态、ID 等条件取交集，在计数与分页之前执行；声明存在不表示采集成功。仍使用原 Platform User 与 `platform.module.read`，不向普通 DTO 暴露来源端点，不更改登记、租约或后台发现。现有 System PostgreSQL repository 门禁覆盖筛选、计数、分页及离线交集。

- `GET /api/v1/system/runtime/observability-identities` 只允许固定 `addp-monitor` 的非委托 Platform Service Access Token 与 `system.observability_identity.read`，不允许 User、Gateway、日志观测器或 Tenant Context。该 Permission 只授予 `platform.monitor_runtime`。
- 同一私有响应的 `service_groups` 子域只增加标准部署入口声明的模块／Backend或Worker角色／用途、预期副本数、运行意图、变更时间和正式租约计数；不返回实例Metadata、端点、凭据或租户内容。System 使用既有受管发现目录只读核验 `service-expectations.json`，前后重读拒绝来源变化；缺失、非法、用途不明及超量仅使该子域不可判定，不把零在线解释为告警或恢复。协议、预算和状态见《addp平台运行观测规范》4.8；不增加身份、权限或普通用户DTO。
- 只读可重复读事务返回启用节点的 UUID/版本与有效节点绑定、启用模块、UP 且未过期的 Backend/Worker/Scheduler/Ingress 实例身份和租约时间；另含启用且有效主机绑定的平台引擎部署 UUID/版本、Infra/Business 作用域、引擎类型及 Docker 引擎／项目／服务来源键，不含名称、地址、端口、业务 URL、Metadata、Engine Connection、租户引擎或凭据。不持久化新副本，不续租，不参与业务 Ready。
- 节点上限 1,000、带节点声明的当前候选实例上限 10,000、启用引擎部署候选上限 1,000、响应上限 4 MiB、总超时 5 秒。超限/控制面失败返回 503，超时 504；无成功空快照兜底或截断。真实空数组为 200，所有投影响应禁止缓存。Common 的唯一服务 Client 校验完整性、时间、重复身份、节点引用与响应体预算。

部署容器证明沿相同受管文件目录只读消费，不调用 Docker。迁移 000240 只扩展进程声明的严格 JSONB CHECK，允许选中的真实 cgroup 内核身份；不变更不可变触发器、既有数据或 IAM。当前实例的不可变声明附带真实 cgroup 内核身份；仅与标准单应用容器的完整层级证明、module／role、启动边界及有效租约精确匹配时，向既有 Monitor 后台投影增加私有 ContainerProof。重复证明、共享边界、缺失／过期／不可读证明均不关联，不推断承载主机、不进入浏览器实例 DTO，也不新增权限。证明字段与时限见平台运行观测规范 6.1；System T1／既有进程登记 PostgreSQL 门禁覆盖。

## 平台本体机器发布核验

- `POST /api/v1/system/runtime/platform-definition-publication-checks` 复用 IAM 认证、Service 凭据、Platform、固定 `addp-ontology` Client 和 `ontology.platform_definition.publish` 守卫；Permission 由 Ontology Manifest 拥有，迁移 `000201` 仅扩展 `platform.ontology_runtime`，不扩张管理员或 Tenant Role。
- 请求只包含 `capability`、正数 BIGINT 字符串 `revision` 和小写 SHA-256 `digest`，正文最大 2 KiB；拒绝 query、未知字段、重复键、大小写别名、Tenant/主体/定义载荷。成功只返回同一绑定及当前机器主体/授权版本，禁止缓存，不签发票据或执行凭据，不记录定义副本，不表示发布或激活成功。
- 生产发布器仍需在各阶段重新核验并完成 Ontology 的内容、并发与激活检查；本切片未装配自动发布器。Common SDK 与 Ontology Authorizer 的 T1 由既有 Go 门禁发现，真实前向迁移与角色范围回归由 `system-iam-postgres-gate.sh --package migration` 自动发现，不新增 CI 路径。

## 常用命令

> **注意**: 开发状态下，不得通过 `go run` 直接运行，改用重启脚本验证修改结果。

```bash
# 重启 system 服务（修改后端代码后）
./scripts/dev/restart.sh -system

# 重启所有服务（修改 common 代码后）
./scripts/dev/restart.sh -all

# 前端仅修改时无需重启后端，热更新自动生效

# System 前端单元测试、浏览器 E2E 与生产构建
make test-system-frontend
```

System 前端 HMR 复用实际 HTTP 监听端口。浏览器测试经共享启动器分配当轮独立回环端口和结果目录，地址统一由 `browserTestOrigin('system')` 获取；设置 `ADDP_E2E=1` 并使用共享 `withFrontendTestIsolation` 关闭 HMR，将依赖缓存与 PID 记录隔离到当轮临时运行目录，退出时清理。不得占用开发端口、复用开发服务或覆盖开发缓存。

实例登记地址的展示直接复用 `common-frontend/basic/src/utils/moduleRuntimeEndpoint.mjs` 的唯一 `getRegisteredEndpoint`；旧私有解析器已移除，与 Monitor 服务监控保持同一契约。共享端点单测归 `make test-common-frontend`，System 只验证组合与自身运行时长口径，既有 System/Console 前端门禁覆盖引用及页面，不新增 API 或依赖。

部署清单统一由 Console 中 `/system/host-admission` 接入，独立模块先进入 Console 后接受文件；仅 Platform User、非委托、既有主机读取/创建权限。v2 清单可省略采集，选择已有主机必须当前读取并核对启用、类型与允许服务，不能覆盖原配置；新建结果冻结并 replace 保存稳定 `node_id`，部署参数只供实际宿主上的新进程使用。需采集时沿共享一次性内存导航草稿到 Monitor，来源不进 URL 或浏览器存储；System 不保存 Monitor 配置。旧 Monitor 导入编排删除；共享、System、Monitor、Console 和 Infra 生命周期及原 Hosted 专项同批验证，实际新流程 T4 未通过前保留缺口。

## 项目结构

### 后端架构（Go）

```
backend/
├── cmd/server/          # 应用入口
│   └── main.go
├── internal/            # 内部包（不对外暴露）
│   ├── api/            # HTTP 处理层
│   │   ├── router.go                  # 路由配置
│   │   ├── auth_handler.go            # 认证：登录/注册/刷新
│   │   ├── user_handler.go            # 用户管理
│   │   ├── tenant_handler.go          # 租户管理
│   │   ├── log_handler.go             # 日志管理
│   │   ├── engine_handler.go          # 引擎管理
│   │   ├── api_consumer_handler.go    # API 消费方与消费凭据管理
│   │   ├── cleanup_handler.go         # 资源回收
│   │   ├── module_registry_handler.go # 模块注册与发现
│   │   ├── task_provider_handler.go   # TaskProvider 读取投影
│   │   └── internal_handler.go        # API 消费凭据验证（Runtime API）
│   ├── config/         # 配置管理
│   ├── middleware/     # 中间件（认证、日志等）
│   ├── models/         # 数据模型和请求/响应结构
│   │   ├── user.go
│   │   ├── tenant.go
│   │   ├── log.go
│   │   ├── engine.go
│   │   ├── api_consumer.go    # API 消费方、精确服务授权与凭据模型
│   │   ├── cleanup.go         # 资源回收任务模型
│   │   └── module_registry.go # 模块定义、运行实例与角色声明模型
│   ├── repository/     # 数据访问层
│   └── service/        # 业务逻辑层
└── pkg/                # 可对外暴露的工具包
    └── utils/          # 密码等通用工具
```

**分层设计**:
- **API Layer**: 处理 HTTP 请求、参数验证、响应格式化
- **Service Layer**: 实现业务逻辑、事务处理
- **Repository Layer**: 数据库操作、CRUD 接口
- **Model Layer**: 定义数据结构、数据库表映射

### 前端架构（Vue 3）

```
frontend/src/
├── api/              # API 请求封装
│   ├── client.js         # Axios 实例配置（拦截器、认证）
│   ├── auth.js           # 认证 API
│   ├── users.js          # 用户管理 API
│   ├── tenant.js         # 租户管理 API
│   ├── engines.js        # 引擎管理 API
│   ├── logs.js           # 日志管理 API
│   ├── apiConsumers.js   # API 消费方管理 API
│   ├── cleanup.js        # 资源回收 API
│   └── manager.js        # 外部 Manager 模块 API（预览等）
├── components/       # 可复用组件
│   ├── Layout.vue        # 通用布局
│   ├── users/            # 用户管理子组件
│   │   ├── UserList.vue
│   │   ├── UserFormDialog.vue
│   │   └── PasswordDialog.vue
│   └── engines/          # 引擎管理子组件
│       ├── EngineList.vue
│       └── EngineFilterBar.vue
├── composables/      # Vue Composables
│   ├── usePagination.js           # 分页逻辑复用
│   ├── useFormDialog.js           # 对话框状态管理
│   └── useUserManagement.js       # 用户管理业务逻辑
├── store/            # Pinia 状态管理
│   └── auth.js           # 认证状态
├── views/            # 页面组件
│   ├── Login.vue          # 登录页
│   ├── Home.vue           # 首页（导航入口）
│   ├── Dashboard.vue      # 仪表盘
│   ├── SystemLayout.vue   # 系统布局框架
│   ├── Users.vue          # 用户管理
│   ├── Tenants.vue        # 租户管理
│   ├── Logs.vue           # 日志管理
│   ├── Engines.vue        # 引擎管理
│   ├── IAMCategoryPage.vue # IAM 分类页；应用接入组合 API 消费方与 OAuth
│   ├── CleanupManager.vue # 资源回收管理
│   └── Developer.vue      # 开发者工具页面
└── router/           # 路由配置
```

## 数据库文档

**遇到以下场景时,主动阅读对应文档**:

| 场景 | 必读文档 | 触发关键词 |
|------|---------|----------|
| 数据库表结构查询 | 对应单表文档 | 字段定义、索引、约束 |
| 表之间关系 | 数据库架构.md | 外键、关联、数据流 |
| API端点详情 | 对应单表文档 | API、接口、请求响应 |
| 权限控制规则 | 对应单表文档 | 权限、访问控制、租户隔离 |
| 数据加密机制 | engines表、数据库架构 | 加密、AES、bcrypt |
| 平台审计归档 | [审计归档专题导航](#审计归档专题导航) | 归档、COMPLIANCE、可信根、保留、只读计划 |

### 架构说明

- [数据库架构](docs/数据库架构.md) - 表关系、数据流向、设计决策

### 数据库表概览

| 表名 | Schema | 说明 |
|------|--------|------|
| principals / users / local_accounts | system | 授权主体、自然人资料与本地登录凭据 |
| tenants | system | 租户表，多租户隔离 |
| audit_logs | system | 审计日志表 |
| engines | system | 引擎配置表，含加密连接信息 |
| api_consumers | system | Tenant 数据面 API 消费方 |
| api_consumer_service_grants | system | 消费方可访问的精确服务引用 |
| api_consumer_credentials | system | API 消费凭据（仅保存 SHA256 hash） |
| oauth_authorization_requests | system | 浏览器授权前的短期已校验请求、取消凭据 Hash 和一次性状态 |
| iam_recovery_attempts | system | 离线管理员凭据恢复尝试，仅保存一次性 Secret Hash 与终态事实 |
| administrator_credential_recoveries | system | 特权请求的线上一次性凭据登记，保存 Proof Hash、短期加密 TOTP、失败次数和消费事实 |
| refresh_token_families | system | 浏览器和 OAuth Refresh Token Family |
| refresh_tokens | system | 轮换 Refresh Token Hash |
| access_tokens | system | 短期 User Access Token Hash |
| delegated_access_tokens | system | Agent Tool 短期受委托访问令牌 Hash 与审计绑定 |
| notebook_session_authorizations | system | Notebook Session 绑定的用户派生短期只读 Engine Catalog 授权事实 |
| resource_access_tickets | system | Owner Path 浏览器资源票据 Hash |
| iam_security_policy | system | System IAM 平台安全策略及已应用版本 |
| module_definitions | system | 持久模块身份、路由、管理员启用状态和配置入口声明 |
| module_runtime_instances | system | Backend、Worker、Scheduler 进程实例及短期租约 |
| module_definitions.task_provider | system | 模块 TaskProvider 能力声明，供 Orchestrator 与 Monitor 动态解析 |

### 单表文档

详细的表结构和 API 说明文档:

- [users表](docs/tables/users表.md) - 用户表,认证和权限管理
- [tenants表](docs/tables/tenants表.md) - 租户表,多租户隔离
- [audit_logs表](docs/tables/audit_logs表.md) - 审计日志表,操作审计和追溯
- [engines表](docs/tables/engines表.md) - 引擎配置表,引擎连接管理
- [resource_access_tickets表](docs/tables/resource_access_tickets表.md) - 浏览器原生资源访问票据
- [delegated_access_tokens表](docs/tables/delegated_access_tokens表.md) - Agent Tool 短期受委托访问令牌
- [notebook_session_authorizations表](docs/tables/notebook_session_authorizations表.md) - Notebook Session 会话授权事实
- [iam_security_policy表](docs/tables/iam_security_policy表.md) - IAM 平台安全策略与重启生效版本

**重要**:修改表结构或 API 时,必须同步更新对应的单表文档。

### 审计归档专题导航

事务审计与对象归档分别验收，完整契约统一见[平台审计归档部署契约](docs/tables/audit_logs表.md#八平台审计归档部署契约)。

- **权限与受控操作**：[在线查询授权](docs/tables/audit_logs表.md#六查询授权)、[三员权限与保留](docs/tables/audit_logs表.md#81-权限与保留)、[构建及私有配置](docs/tables/audit_logs表.md#86-构建与受控操作入口)。归档原始凭据不发放给 Tenant；正文至少保护 365 天，根与封段检查点自存储创建起至少保护 20 个 UTC 日历年，期限与 COMPLIANCE 要求按专题核验。
- **协议与故障边界**：[基线、提交与故障](docs/tables/audit_logs表.md#82-基线提交与故障)、[v2 流式核验与只读 plan](docs/tables/audit_logs表.md#84-分段流式核验与只读到期计划已确认)。plan 不授权删除，实际清理、双人批准和回执尚未实现。
- **验证与独立接收端**：[验收与实际边界](docs/tables/audit_logs表.md#83-验收与实际边界)、[B 的限定协议](docs/tables/audit_logs表.md#85-独立日志接收端-b-的限定协议非首期部署要求)。B 不是平台桶部署前提，也不随开发环境启动。隔离 T2 通过不证明真实 Infra 已部署、生产 WORM、最高管理员物理防篡改或全量访问日志；历史失败与后继通过分别保留。

## 核心功能实现

### 认证流程

1. 用户通过 `POST /api/v1/system/login` 登录，提交用户名和密码
2. 后端验证凭证，创建 Refresh Token Family、opaque Access Token、Refresh Token 和 Owner Resource Access Ticket
3. Access Token 只返回给 Browser AuthSession 内存；Refresh Token 和 Resource Ticket 只通过 HttpOnly Cookie 下发
4. 普通 API 通过 `Authorization: Bearer <token>` 携带 Access Token
5. 原生图片、媒体、下载和三维资源使用干净 URL，由浏览器携带 Owner Path Resource Ticket Cookie
6. System `/auth/context` 解析 Token 并回查当前用户、租户状态，Owner 模块继续执行租户和资源权限校验
7. Access Token 到期前通过 `POST /api/v1/system/refresh` 静默轮换；退出或 Refresh Token 重用时撤销整个 Family

### 数据库设计

**身份与成员关系表**:
- `system.principals` 保存主体类型、状态和授权版本；
- `system.users` 保存自然人资料，不保存登录凭据和 Tenant 归属；
- `system.local_accounts` 保存本地用户名与不可逆密码 Hash；
- `system.tenant_memberships` 保存主体进入 Tenant 的有效关系；
- `system.roles`、`system.role_permissions`、`system.role_assignments` 保存 RBAC 事实。

**system.tenants 表**:
- 租户信息
- 字段: `id`, `code`, `name`, `description`, `status`, `created_at`, `updated_at`

**system.audit_logs 表**:
- IAM、OAuth 和通用操作审计的唯一追加式事实表
- 当前字段使用 `principal_id`, `principal_type`, `context_type`, `tenant_id`, `event_name`, `result`, `risk_level`, `details`, `created_at` 等
- 身份、授权、Token 撤销和 OAuth 状态转换必须与权威事实同事务写入；普通运行时路径不得 UPDATE / DELETE / TRUNCATE

**system.engines 表**:
- 存储各类引擎连接信息 (数据库、对象存储等)
- 字段: `id`, `name`, `engine_type`, `connection_info`, `tenant_id`, `created_by`, `lifecycle_state` 及删除工作流状态
- `version` 只表示 Engine Instance 聚合根的管理员编辑基线；后台连接观测更新 `connection_status`、`last_check_at`、`check_message`，插件成功返回原生产品版本时同次更新只读 `server_version` 与独立 `server_version_checked_at`；失败或未提供版本时保留最后一次成功事实。这些观测不得递增 `version` 或修改 `updated_at`
- `connection_info` 为 JSONB 类型，灵活存储不同类型的连接配置
- 连接敏感字段的声明、存储与返回规则见[引擎连接安全](docs/tables/engines表.md#71-敏感字段加密)。

**system.api_consumers / system.api_consumer_service_grants / system.api_consumer_credentials**:
- API Consumer 只表达外部系统对已发布数据面 API 的机器调用，不生成 Principal 或 AuthContext；
- Service Grant 使用 `(service_type, service_id)` 精确引用 owner 服务，首期只允许 `query`；
- Credential 以 `addp_api_` 开头，System 只保存 SHA256 hash，明文仅在创建时返回一次；
- Gateway 只在正式数据面入口识别该凭据，`/api/v1/*` 控制面必须显式拒绝；Service owner 再按 Tenant 与精确服务引用完成最终授权。

**system.module_definitions / system.module_runtime_instances 表**:
- `module_definitions` 按稳定 `module_name` 保存持久定义和管理员 `enabled` 状态，进程离线不删除定义
- `module_definitions.version` 是聚合根乐观并发版本；心跳不得递增它，幂等重复注册保持版本不变，只有 owner 模块级声明实际变化时才原子递增且不得覆盖管理员 `enabled`
- System Backend 自身注册生命周期使用进程信号 Context，退出前完成实例注销、等待心跳与清理任务结束；开发停止顺序为其他模块和 Runtime 先退出、System 最后退出，正常注销记录 `graceful`，未完成注销的强制终止或失联实例仍按租约超时判断。
- `module_runtime_instances` 按 `(module_definition_id, instance_id)` 保存进程角色、端点、运行环境主机名 `runtime_hostname`、部署注入的宿主节点名 `host_node_name`、宿主节点 IP 集合 `host_node_ips`、元数据、心跳和租约
- 心跳只续租实例；只有 `enabled + backend + up + lease valid` 的实例可供 Gateway 路由
- `configuration_management` 只保存版本化配置管理入口声明（owner、scope、前端路由和读写 Permission），不保存模块配置键、配置值或 Secret

**TaskProvider 模块角色**:
- Provider 声明保存在 `system.module_definitions.task_provider`，不建立独立注册实体或独立启用状态
- Provider ID 复用模块定义 ID，重复相同声明保持模块版本不变；声明变化递增模块定义版本
- 有效端点池只在读取时从当前 Backend 租约解析，不写入模块定义；模块离线时声明保留但 `available=false`，System 不固定选择单个 Backend

### 日志中间件

`LoggerMiddleware` 自动记录非 GET 请求，以及模块实例运行日志的 GET 读取审计。运行日志读取审计只记录操作者、实例、时间范围和结果，不保存关键字和日志正文。其他审计包括：
- 用户身份（如果已认证）
- 请求方法和路径
- 客户端 IP 地址
- 请求时间

### 安全机制

**登录限流**: 15 分钟内最多 5 次尝试（基于 Redis 的 Rate Limit 中间件）

**CORS 白名单**: 仅允许 `cfg.AllowedOrigins` 中的 origin，拒绝其他跨域请求

## 开发注意事项

1. **添加新的 API 端点**:
   - 分层职责、DTO 与共享能力遵循[API 实施规范](../docs/spec/api/文档与实施.md#后端实现分层与共享能力)，具体步骤见[新接口开发](../docs/spec/api/文档与实施.md#新接口开发)。
   - System API 组在[路由入口](backend/internal/api/router.go)声明，前缀为 `/api/v1/system`；契约同步、生成与覆盖检查统一遵循[Swagger 强制同步要求](../docs/spec/addp-Swagger集成指南.md#强制同步要求)。

2. **数据库迁移**:
   - 表归属、初始化顺序与失败处理统一见[System 迁移 Runner](docs/IAM数据模型与迁移规范.md#十system-统一迁移-runner)：IAM 及其约束依赖的基础资源表（含 `system.engines`）使用版本化 SQL，不进入 `AutoMigrate`；GORM 仅在迁移成功后初始化尚未迁入 Runner 的非基础业务表。失败不得以表存在性或默认数据兜底。
   - 版本超前、checksum 不一致和 dirty 状态见[数据库迁移故障排查](../docs/guide/troubleshooting/数据库迁移.md#system-backend-因数据库迁移版本超前而启动超时)；75、113、130、240 号迁移的精确前置条件与允许修改范围见[定向恢复清单](docs/IAM数据模型与迁移规范.md#定向恢复)。
   - 实际开发库恢复须由用户自行执行或取得本次明确授权，流程见[共同操作与验收边界](../docs/guide/troubleshooting/数据库迁移.md#定向恢复的共同操作)。构建、隔离测试与只读核对不等于已恢复，恢复成功也不等于后续迁移和业务验收通过。

3. **前端添加新页面**:
   - 页面、布局、Console iframe 与共享认证遵循[页面接入规范](../docs/spec/addp新模块开发指南.md#添加前端页面)。System 页面位于 `frontend/src/views/`，API 封装位于 `frontend/src/api/`；受保护页面沿用[现有路由与权限守卫](frontend/src/router/index.js)。
   - API 复用[模块 Client](frontend/src/api/client.js)：共享 `baseURL` 为 `/api/v1`，System API 调用参数使用 `/system/...`，最终路径为 `/api/v1/system/...`；拼接规则见[前后端连接说明](../docs/spec/addp新模块开发指南.md#开发中的前端-后端连接)。

4. **端口配置**:
   - 开发首选端口与容器内端口以[端口规范](../docs/spec/addp端口分配.md#端口分配)为准；开发实际端口取标准启动输出及 `.dev-state/ports.env`，不从固定示例推断。
   - 浏览器入口遵循[公开路由规范](../docs/spec/addp前端路由与可恢复状态规范.md#二公开路由事实源)：Console 使用 `/system/...`，模块 iframe 与 standalone 使用同 origin 的 `/module-ui/system/...`；模块开发监听端口用于内部服务，直接访问会跳转到正式入口。
   - 根 Compose 部署仅发布 Nginx 统一入口，System 前后端使用容器网络与固定内部端口；部署端口冲突时失败，不按开发模式自动避让。公开 origin 按[部署配置](../docs/spec/configuration/环境变量参考.md#端口分配)设置 `ADDP_PUBLIC_ORIGIN`；域名、HTTPS 或上级反向代理须使用用户实际访问的 origin。模块单独部署仍按对应部署配置确定入口。

## 安全机制

### 密码安全

1. **用户密码 Hash** (`system.local_accounts.password_hash`)
   - 算法: **bcrypt** (cost factor 10)
   - 不可逆哈希,自动加盐
   - 验证: `CheckPassword(plaintext, hash)`

2. **引擎连接敏感字段加密** (`system.engines.connection_info`)
   - 字段由当前插件 `ConnectionSpec` 的 `sensitive=true` 唯一声明，`SensitiveFields()` 必须从中派生。
   - AES-256-GCM 持久化处理、更新占位符、密钥加载与按调用路径返回的规则统一见[引擎连接安全](docs/tables/engines表.md#71-敏感字段加密)；声明与静态检查不替代持久化及接口验收。

3. **API 消费凭据安全** (`system.api_consumer_credentials`)
   - 存储：仅存 SHA256 hash，明文仅在创建时返回一次
   - 验证：`GET /api/v1/system/runtime/api-consumer-credentials/validate` 只供持有 `iam.api_consumer_runtime.read` 的 Gateway 与 Service Platform Service Principal 调用
   - 边界：凭据只用于发布的数据面 API，不能访问控制面 API

### 访问控制

访问控制由 AuthContext 的当前 Context、Token Scope、Role Permission 与 owner 资源策略共同决定。平台三员和 Tenant 内置 Role 的精确 Permission 集合以 `system/authorization/builtin_roles.yaml` 为唯一发布源，API 路由必须使用精确 Permission Guard，不允许角色名判断或隐式继承。

## API 端点

> 所有公开和服务身份 API 均以 `/api/v1/system` 为前缀并使用 Bearer Token。Tenant Runtime 通过 `tenant_id` 换取 Tenant Service Access Token，平台控制面通过 `context_type=platform` 换取 Platform Service Access Token。

### 认证
- `POST /api/v1/system/login` - 用户登录
- `POST /api/v1/system/auth/mfa-verifications` - TOTP 验证
- `POST /api/v1/system/auth/context-selections` - 登录时选择上下文
- `POST /api/v1/system/refresh` - Token 刷新
- `POST /api/v1/system/logout` - 撤销当前 Browser Token Family

当前精确源数据规则观察的唯一底座是 `engineaccess` 私有仓储方法：非空完整目标集合在自有只读事务的一条 SQL 快照中核验当前主体及 Grant／Deny；无 Grant、有效 Deny、失效主体、停用引擎或缺失结果均不满足覆盖。不提供自报账号的裁决 API，不替代可信凭据、功能权限、执行范围或 Security 条件，也不代表 Manager／Develop 已贯通。

可信同步 User 消费组合复用 IAM `ResolveUserAccessToken`，在同一自有只读 Repeatable Read 事务中派生当前 Tenant、Principal、Membership 和授权版本，再调用唯一精确规则查询；不接受自报身份或外部 AuthContext。查询后按数据库墙钟复核 Token 自然到期，不保存、记录或返回凭据。该私有方法只覆盖真实身份与源规则，不授予 owner 功能／Client Scope、不替代 Execution／Security，不接收 Service、Resource Ticket 或 Delegated 凭据，不新增 HTTP 入口。

正式同步检查首个入口为 `POST /engine-access/read-checks/manager-preview`：接收当前 Tenant 普通 User Bearer，或 IAM 核验的精确 `manager` audience、唯一 `data.preview` Scope 委托凭据，以及 1–200 个完整目标。固定复核 `manager.data_item.read`，分别核验 API Client 或固定 Tool 委托边界，再用同一快照观察全部源规则；任一未覆盖即 403，不公开逐条 Grant／Deny 或主体详情。委托凭据不进入私有普通 User 路径，也不获准调用其它 System API。成功只返回当次 `observed_at` 并禁止缓存，不是完整 Allow 或访问令牌。此入口不访问源端、Catalog 或 Security；Manager 的完整 Provider ReadSet、同一 PreparedQuery 实际执行及本地字段保护仍须单独贯通，不能用检查通过声称实际预览已放行。

### 授权上下文（需认证）
- `GET /api/v1/system/auth/context` - 验证当前访问令牌，回查用户和租户状态，返回权威 AuthContext

`/auth/context` 是 Go/Python 业务模块消费用户身份的唯一接口。`/users/me` 只返回用户资料，不用于 Token 验证。

### 当前用户（需认证）
- `GET /api/v1/system/users/me` - 获取当前用户信息
- `PUT /api/v1/system/users/me/password` - 修改当前用户密码

### Platform 管理（Platform Context + 精确 Permission）
- `/api/v1/system/platform/tenants` - Tenant 查询、创建、更新、暂停、恢复和关闭；
- `/api/v1/system/platform/users` - 全局 User 查询、创建、更新、暂停和重新激活；
- `/api/v1/system/platform/administrator_changes` - 平台身份变更申请、复核和监督；
- `/api/v1/system/platform/audit/events` - 平台审计查询、汇总、趋势和导出。
- `/api/v1/system/platform/modules` - 模块定义、有界当前运行实例投影和带版本的启用状态管理；不得携带全部实例历史。
- `/api/v1/system/platform/module-instances` - 跨模块运行实例记录的唯一只读分页查询，支持模块名、登记主机、节点、role、当前有效 status、`stop_reason=graceful|lease_expired` 与时间范围组合过滤。`time_basis=registered|offline` 选择登记时间或离线判定时间（默认登记），统一使用 `time_from` / `time_to`，范围下界含、上界不含，按选定时间倒序及 ID 倒序稳定分页；旧的登记时间专属 query 删除。原因筛选与其他条件取交集，不自动修改状态或时间依据；离线时间及原因查询仅展示仍保留离线观测的实例，包括尚未被扫描落库的租约过期实例；恢复后退出结果，不代表完整的历史离线事件或历史可用率。

### Tenant IAM 管理（Tenant Context + 精确 Permission）
- `/api/v1/system/tenant/memberships` - 当前 Tenant Membership 查询、有效期和生命周期；
- `/api/v1/system/tenant/invitations` - 当前 Tenant 邀请查询、创建与撤销；
- `/api/v1/system/tenant/audit/events` - 当前 Tenant 审计查询、汇总、趋势和导出。

### 引擎管理（需认证）
- `POST /api/v1/system/engines` - 创建引擎（自动关联当前用户租户）
- `GET /api/v1/system/engines` - 获取当前 Tenant 的完整过滤后引擎数组；System 管理页面固定读取全部生命周期的可见引擎（包含内置），在前端筛选后分页，不随勾选条件查询后台。
- `GET /api/v1/system/engine-types` - 获取插件声明的可注册引擎类型、连接表单、能力和 Catalog Model
- `GET /api/v1/system/engines/:id` - 获取指定引擎
- `PUT /api/v1/system/engines/:id` - 更新引擎（敏感字段自动重新加密）
- `DELETE /api/v1/system/engines/:id` - 删除引擎
- `POST /api/v1/system/engines/:id/test` - 测试已有引擎连接
- `POST /api/v1/system/engines/test-connection` - 创建前测试连接
- `POST /api/v1/system/engines/:id/catalog/children` - 统一列出实时 Engine Catalog 子节点，支持数据库、对象存储、文件系统和图数据库等多层目录发现；路由中的 `catalog` 已由 Engine 上下文限定
- `POST /api/v1/system/engines/:id/catalog/facts` - 按结构化 EngineCatalogPath 读取单个叶子的实时结构事实；列表省略的字段等详情从这里按需读取
- `POST /api/v1/system/engines/:id/access_grants` - 独立批准普通表的正式只读授予，要求创建权限和管理资格，明确接收主体、期限与原因；已有有效关系拒绝重复签发，不依赖 Catalog。
- `GET /api/v1/system/engines/:id/access_grants` - 按精确目标、接收方和动作聚合当前有效授权，合并存量重复并展示数量；不代表最终读取裁决。
- `GET /api/v1/system/engines/:id/access_grants/history` - 使用同一读取权限查看所有签发、到期与撤销历史，不提供历史单份撤销入口。
- `POST /api/v1/system/engines/:id/access_grants/:request_id/revoke` - 当前 Tenant User 的独立撤销 Permission 与管理资格共同核验，定位并原子收回整条关系的全部有效 Grant；停用引擎仍可撤销，不依赖 Catalog 在线，不收回其他个人／组织关系。原因必填，编号集合、历史及审计同事务追加，同参重试仅恢复原结果，不影响后来重新授予。
- `POST /api/v1/system/engines/:id/access_denies` - 当前 Tenant User 的独立 `system.engine_access_deny.create` 与有效引擎管理委派共同核验，绑定精确叶子、当前有效 User／Department／Project Group 和 `read`。期限必须显式选择 `at_time` 或 `until_revoked`；编号同参重试只恢复不可变历史，不能续期。停用引擎仍允许限制，不连接源端或 Catalog；规则及高风险审计同事务，提交前复核资格和数据库墙钟。000185 不默认给任何角色赋权；PG Manager 读取与 System 授权工作区已接入该规则，其他引擎及消费入口仍须独立验证。
- `POST /api/v1/system/engines/:id/access_denies/:deny_id/release` - 独立 `system.engine_access_deny.release` 与当前有效引擎管理委派共同核验，正文仅接受原因。000186 保存不可变解除事实及同事务高风险审计，不修改原 Deny 或 Grant。自然到期后首次解除返回 409、无新事实；到期前已解除的原参重试仍可恢复历史，但当前资格必须有效。停用引擎、失效接收主体及 Catalog 不可用均不阻断合格操作者解除；解除不等于自动允许访问。

`GET /engines` 对 User 和 Service Principal 都返回脱敏列表。`GET /engines/:id` 对 User 返回脱敏连接信息；具有 `system.engine.read` 的 Tenant Service Principal 返回同 Tenant 的解密连接信息，跨 Tenant 返回 403。

扩展引擎提供独立“停用”入口（含内置扩展引擎），只消费既有 `system.engine.update` 权限并提交观察到的 `version` 与 `lifecycle_state=disabled`；不开放名称、连接配置等登记字段编辑，不停止 Runtime 进程。版本冲突或请求结果不确定时保留确认框，禁止自动重试或补版本；操作者须显式重新读取并核对，再次确认。成功提示以列表及详情的只读读回均为停用为准，旧轮询先完成再读回，保留当前筛选。实际管理范围仍由后端校验。

引擎管理筛选分为能力（存储／计算）、来源（通用／扩展）、注册方式（全部／注册／内置）和生命周期（启用／停用／清理中／已删除）四个独立维度。多选维度内取并集，维度间取交集，任一多选维度空选即无结果；能力以实例 `capabilities.storage/compute` 声明是否存在为准，同一实例可同时匹配两类，不按引擎名称或注册分组猜测。默认选择全部能力、全部来源、全部注册方式及除已删除以外的生命周期。首次加载、手动刷新、既有状态轮询及操作成功后的刷新均读取完整清单，保留当前筛选；筛选变更回到第一页，刷新后按筛选结果收敛页码，总数取筛选结果长度。共享 API 的后台过滤能力继续服务其他消费者，本页只使用上述固定读取范围。

### Service Runtime（Service Access Token + 精确 Permission）
- `POST /api/v1/system/runtime/modules` - Platform Service Principal 注册自身模块；
- `POST /api/v1/system/runtime/modules/heartbeat` - Platform Service Principal 更新自身心跳；
- `GET /api/v1/system/runtime/modules` - Gateway Platform Service Principal 查询模块注册表；
- `GET /api/v1/system/runtime/modules/:module_name` - Gateway Platform Service Principal 查询模块详情；
- `GET /api/v1/system/runtime/task-providers` - Platform Service Principal 读取模块定义中的 TaskProvider 声明及当前动态可用性；
- `POST /api/v1/system/runtime/engines` - Workflow Runtime Platform Service Principal 注册自身内置 Runtime；
- `GET /api/v1/system/runtime/api-consumer-credentials/validate` - Gateway 或 Service Platform Service Principal 验证 API Consumer Credential Hash；
- `GET /api/v1/system/runtime/engine-descriptors` - Tenant Service Principal 列出当前 Tenant 可见的脱敏 Engine Runtime Descriptor；
- `GET /api/v1/system/runtime/engine-descriptors/:id` - Tenant Service Principal 读取当前 Tenant 可见的单个脱敏 Engine Runtime Descriptor；
- `POST /api/v1/system/tenant/audit/events` - Tenant Service Principal 追加当前 Tenant 审计事件。

Module Name 必须与 OAuth Client `addp-<module>` 一致；Principal、Context 和 Tenant 只从 AuthContext 获取。所有服务间调用统一使用上述 Bearer 路由。

Engine Runtime Descriptor 不包含 `connection_info`。只有工作流或脚本 Runtime 可投影非密密的 `protocol/host/port`；数据引擎连接信息必须继续通过 Execution Authorization 或已明确授权的详情路由获取。

### API 消费方（Tenant User + 精确 Permission）
- `POST /api/v1/system/tenant/api-consumers` - 创建 API 消费方并绑定精确服务引用
- `GET /api/v1/system/tenant/api-consumers` - 获取当前 Tenant 的 API 消费方列表
- `GET /api/v1/system/tenant/api-consumers/:id` - 获取 API 消费方详情
- `PUT /api/v1/system/tenant/api-consumers/:id` - 更新名称、说明、服务授权和速率限制
- `DELETE /api/v1/system/tenant/api-consumers/:id` - 删除 API 消费方并使全部凭据失效
- `POST /api/v1/system/tenant/api-consumers/:id/credentials` - 生成 API 消费凭据
- `GET /api/v1/system/tenant/api-consumers/:id/credentials` - 列出凭据元数据
- `DELETE /api/v1/system/tenant/api-consumers/:id/credentials/:credential_id` - 撤销凭据

API 消费方不是 Principal，不能分配 Role。首期只绑定 Service Consumer Catalog 中的 Query Service；Gateway 只在 `/api/query/:serviceName/query` 数据面路由接受 `X-API-Key`，`/api/v1/*` 控制面必须显式拒绝该 Header。Service owner 根据当前 Query Service 的 Tenant、ID、状态和发布状态执行最终授权。

### 资源回收（Tenant User + 精确 Permission）

入口要求 User 凭据与 AuthContext 中的当前 Tenant，权限和任务租户归属由后端校验，不按“管理员”称谓放行。

- `POST /api/v1/system/admin/cleanup/scan` - 创建评估任务，需 `system.cleanup.execute`
- `GET /api/v1/system/admin/cleanup/tasks/:task_id` - 查询本租户任务状态，需 `system.cleanup.read`
- `POST /api/v1/system/admin/cleanup/execute` - 创建执行任务，需 `system.cleanup.execute`
- `GET /api/v1/system/admin/cleanup/history` - 查询本租户临时任务历史，需 `system.cleanup.read`

execute 必须引用本租户已 `completed` 的 scan 并提交 `confirmed=true`；高风险或物理回收另需确认文本 `CONFIRM`，物理回收须显式选择 `external_artifact_policy=delete|abandon`，逻辑回收不接受该策略。参数与确认规则统一见[scan 与 execute](../docs/spec/addp-cleanup体系规范.md#六scan-与-execute)；确认字段不授予权限。

能力范围见[executor 参与范围](../docs/spec/addp-cleanup体系规范.md#executor-参与范围)、[结果与状态](../docs/spec/addp-cleanup体系规范.md#八结果模型)及[System 页面与 API](../docs/spec/addp-cleanup体系规范.md#system-页面与任务-api)。[Monitor](../docs/spec/addp-cleanup体系规范.md#1-monitor)独立执行 Owner 读取裁决与安全投影；引擎删除须走[专用工作流](../docs/spec/addp-cleanup体系规范.md#engine-删除工作流)，不能用通用 execute 替代。

验证要求与未覆盖项统一见[运行态验收](../docs/spec/addp-cleanup体系规范.md#十三运行态验收)：完整 Cleanup suite 尚未接入，接口存在、任务终态或页面可展示均不代表完整验收通过；未运行、失败或跳过不计为通过。

### 前端公开路由

- 平台主机管理唯一使用 `/host-nodes` 和 `/host-nodes/:node_id`，仅 Platform User、`platform.host_node.read`；创建与更新另需对应权限。检索和分页使用 `search/node_kind/enabled/page/page_size`，类型为 physical/virtual，纳管状态为 true/false，全部时省略；默认第 1 页、每页 20 条省略，共享导航桥同步 Console。类型、状态及名称/地址在服务端计数与分页前组合筛选；筛选变化回第一页，详情返回保留条件，重置保留每页数量。详情新读取版本后完整提交，409 保留草稿，不自动重试；实例节点展示共用 `ModuleInstanceNode`，只对当前有效绑定提供节点详情入口。纳管状态不表示主机电源状态，具体实现与验证见平台运行监控设计第 2、4 节。
- 主机台账列表的计数与页数据来自同一只读可重复读事务，供 Monitor 在现有分页上限内识别完整当前 User 授权集合；不增加权限或后台身份投影。超出完整范围或资源预算时由 Monitor 整份拒绝，不把单页称作平台 TOP5。System repository PostgreSQL 门禁覆盖计数后并发插入不会混入同页。
- 主机管理在当前 User 同时拥有监测目标读取/创建权时，提供“从部署清单接入”导航到 Monitor 原新建页。System 继续唯一创建主机身份；导入、预览和跨 owner 保存由该页面组合既有公开 API，不新增 System 导入接口、存储或后台身份。新建主机仍另需 `platform.host_node.create`，详情见平台运行监控设计 4.2 节。
- IAM 左侧导航按业务大类固定为 `/iam/organization`、`/iam/accounts`、`/iam/roles`、`/iam/application-access`、`/iam/security` 五个页面；具体管理对象使用页内稳定 `tab`，默认 Tab 省略，无权限或无效 Tab 规范化为该分类下的首个可用值。
- `/iam/organization` 承载租户、部门和项目组；`/iam/accounts` 承载用户账号、用户邀请和平台身份变更；`/iam/roles` 承载角色定义和用户账号角色分配；`/iam/application-access` 分别承载 API 消费方、外部 OAuth 应用、租户服务账号和平台运行账号，其中平台运行账号只读、租户服务账号可管理且 Service Principal 角色入口只存在于此；`/iam/security` 承载 IAM 平台安全策略以及当前 Context 审计。当前 User 由右上角“个人中心”进入唯一 `/account` 页面，分类查看基本信息、当前租户下本人组织归属与账号安全，不属于任一 IAM 管理页 Tab；`GET /users/me/organization` 是只读自服务，不要求组织管理权限，不枚举他人或跨租户组织。
- 引擎对象唯一使用 `/engines/:id`；详情稳定子视图使用 `tab=connection|capabilities`，默认基础信息省略；独立授权窗口使用 `tab=data-authorization|delegations`，按对应权限显示数据授权或委托授权。通过同一对象路由的稳定子视图区分页面职责，不新增同一功能的双轨路由；刷新、关闭和无权限规范化继续使用共享导航桥。
- 审计入口唯一使用 `/iam/security?tab=audit`，审计范围由当前 Platform 或 Tenant Context 决定，并支持 `module_name`、`principal_id`、`principal_type`、`entity_type`、`entity_id` 稳定筛选；资源回收不再跳转不存在的 `Logs` route。
- 模块管理唯一使用 `/modules`；页面只对持有 `platform.module.read` 的 Platform User 显示，启停还要求 `platform.module.update`。
- 服务实例使用 `tab=instances`，组合筛选、时间依据、时段及分页使用 `docs/spec/addp前端路由与可恢复状态规范.md` 中的唯一 query 契约；默认 UP 和登记时间依据省略，全部状态显式为 `status=all`。时间依据选择后立即查询，保留其余筛选和时段并回到第 1 页；重置恢复 UP、登记时间、全部时间和默认分页。筛选和分页由现有 System 导航桥 replace 到 Console 或 standalone URL，刷新、分享和历史导航从 URL 恢复；未应用输入不写入地址栏。时间控件、UTC 规范化、预设目录与每请求边界换算统一复用 Common Frontend 的 `RuntimeInstanceTimeFilter`／`runtimeInstanceTime`，不在 System 保留平行实现；日志页面继续复用同一预设目录。离线原因控件、枚举规范化及译名统一复用 Common Frontend 的 `RuntimeInstanceStopReasonSelect`／`runtimeInstanceStopReason`，概览、实例列表与日志诊断不保留旧词条或平行目录。System 前端门禁覆盖 standalone，Console 前端门禁加载真实 System 页面覆盖 iframe 同步与刷新。
- 使用离线判定时间查询时，列表将离线判定时间紧接运行状态展示，便于同时查看状态与故障时间；使用登记时间查询时仍可在实例诊断中查看离线判定时间。
- 服务实例页沿用模块概览的 10 秒刷新间隔，重新读取当前已应用的组合条件与分页，更新心跳、租约及运行时长；后台刷新不显示整表加载遮罩。有请求进行中、文本防抖未结束、自定义时间范围不完整或浏览器页面隐藏时跳过后台刷新，旧响应不得覆盖新查询；离开服务实例页停止该页轮询。该行为由既有 `make test-system-frontend` 和 System 前端 CI Job 的浏览器回归覆盖。
- 服务实例列表显示当前查询最后一次成功获取数据的本地时间，包括成功返回空列表的情况；该时间只表示列表获取时间，不代表实例心跳时间。刷新失败立即提示数据已过期；两个刷新周期（20 秒）没有成功更新时同样提示，保留同一查询的上次结果供查看。重试完成前保留失败及过期提示，只有当前查询成功响应才清除。切换筛选或分页、重置及未完成自定义时间范围时清除上一查询的结果和获取时间；旧响应不得更新获取时间。页面恢复可见时立即检查数据时效并尝试刷新。获取时间和过期状态不写入 URL。
- 同一实例的租约过期及恢复由确定性浏览器用例连续验证：全部状态列表自动从 UP 更新为 DOWN；已离线实例不出现在 UP 筛选中，恢复实例自动退出 DOWN 筛选；组合条件与 URL 保持不变。租约超时只说明失联，离线时不推断进程持续运行时长；相同进程续租恢复后继续按原启动时间计算。真实 System/Gateway 注册和恢复链路另由隔离部署中的 T4 `module-registry-recovery` 验证，不以受控 API 夹具替代。


签发结果只读找回（2026-10-03）：`engineaccess.ResolveFulfillmentGrant` 经唯一 `resolveAcceptedGrant` 路径只观察已提交签发历史，原完整绑定匹配和当前 Runtime 资格均须通过；未找到不是关闭或授权许可。受理与签发历史共用自有只读事务边界，拒绝暴露调用事务自身未提交记录，不取仲裁锁，先结束历史读取再开始资格事务。签发入口复用此找回路径，但新签发仍在权威写事务内查重与重新核验。HTTP 查询未找到返回 `found=false` 且省略 `grant`；找到仅返回原编号和签发时刻，不能把历史当作执行侧当前访问裁决。

授权记录的接收方展示：稳定账号／组织编号仍是 Grant 绑定依据，页面通过当前 Tenant 的 IAM 读取接口解析名称，账号复用 `TenantMemberIdentity`，部门与项目组展示名称和编码。历史解析不限定为有效候选，不能把已停用账号误当作不存在；授予表单仍只接受当前有效候选。名称读取遵守独立成员／组织查询 Permission，缺少权限、查询失败或查无对象时明确提示并保留类型与编号供核对；不隐藏授权记录、不扩大读取权限。授权列表与撤销摘要复用同一展示，切换身份、租户、权限或引擎后清除旧名称。

授权列表使用当前与历史的唯一 GET 入口，接受 table_search、catalog_path、recipient_type、recipient_id。table_search 保留字面量子串规则；catalog_path 为最多 4096 字节的完整规范叶子 EngineCatalogPath JSON，必须属于当前引擎，与 table_search 互斥，按 jsonb 完整结构精确匹配。非法、重复及未知参数返回 400。当前授权选择具体用户时，自动按表展开其个人、当前有效部门和项目组来源；无具体用户时按直接接收方关系展示。两种表条件均在来源展开、计数和分页前执行，账号不存在或跨租户返回 404。授权历史包含全部签发、到期与撤销记录，接收方条件只匹配直接记录，不展开今天的组织成员关系。切换当前／历史保留条件，选项立即查询、表名防抖 300 毫秒；条件、身份和引擎变化隔离旧响应。名称继续遵守各类独立 IAM 读取权限。

新增授权、当前授权、全部授权记录使用始终可见的同一互斥导航，由父工作区单一状态控制；新增视图不另设返回导航，未完成的签发批次禁止切换。完整记录仍使用既有 `/access_grants/history`，可选 `state=active|revoked|expired`，省略为全部；当前关系入口拒绝状态参数。状态与表及接收方在分页前共同筛选，以当次数据库墙钟判定，撤销优先于到期；有效记录不等于实际可读取。状态只在完整记录视图生效，往返切换保留其选择。数据表筛选使用一个搜索框，右侧内嵌树形下拉入口；下拉复用共享 ResourceTreePicker 与现有 System Engine Catalog 适配器选择普通表，立即按完整路径精确查询；修改文本切回模糊搜索，清空恢复全部表。切换当前／全部记录保留筛选条件；新增草稿取消后清除草稿，不提交写操作。树读取仍要求 engine_catalog.read 及引擎可用，无此能力时仍可文字查询授权，不依赖 Meta 或企业 Catalog，也不授予数据读取权。

授权窗口默认展示当前授权，新增授权进入独立办理视图；账号核查不再是单独页面或 HTTP 入口。选择具体用户后，每张表显示源规则状态、批准方式和合并期限，展开查看各来源的接收方、期限及记录数量。源规则观察与实际读取复用唯一 SQL 谓词，在同一快照和时刻查询；inspection 不是实际访问 Allow，不读取源库、不授予权限，不暴露 Deny 正文，响应 no-store。撤销只针对展开后的具体来源；撤销个人来源不影响部门／项目组授权。无匹配结果只表示筛选没有当前来源。新增授权取消返回列表；成功按本次接收方、单表时按表定位；未完成批次保持原命令和逐表结果。T1 查询契约、T2 PostgreSQL 当前成员关系／拒绝优先／来源撤销／分页，T3 自动筛选和来源展开均由现有 System 标准门禁与 CI 覆盖，无新增入口。

具体账号的当前授权同时保留 Deny-only 数据项，展开显示 `inspection.denies` 的最小摘要（规则编号、接收主体、期限），不返回建立原因、操作者或历史正文。个人账号暂时失效时仍保留其直接拒绝的解除锚点，但规则状态继续为 `source_unavailable`。页面“禁止读取”固定对选定账号及单个 DataItem，明确选择期限和原因，复用既有 `POST /access_denies`；已有个人拒绝不重复提交新命令。“解除禁止”针对具体来源，组织规则说明完整影响范围，复用 `POST /access_denies/:id/release`。两者分别依赖独立创建／解除 Permission，不扩展任何内置角色；结果未知时保留原编号、参数及到期时间供显式重试。解除不隐含新建、恢复或续期 Grant。测试沿用 System 前端与 engineaccess PostgreSQL 标准门禁。

## 模块实例运行日志

System 平台的“模块管理 → 服务实例”提供已登记 UP/DOWN 实例的日志抽屉。独立 Permission `platform.module_log.read` 默认只授予平台系统管理员；租户、Runtime Service Principal 和仅有模块读取权限的用户不能读取正文。

API 为 `GET /api/v1/system/platform/modules/{module_name}/instances/{instance_id}/logs`，参数 `from/to/level/keyword/limit/cursor`。固定实例、字面量关键字、有界时间窗口；每批默认 200 条，最多 1000 条且响应不超过 2 MiB。历史分页固定首批窗口，不提供虚构总数；游标绑定当前 Platform User 与筛选，30 分钟有效。正文按接收时间和精确 uint64 序号排序。同时间戳候选达到 Loki 1001 条单次安全限额时返回 422，不能悄悄跳过。抽屉提供更早、返回和回到最新，历史浏览暂停跟随，翻页失败保留当前批；筛选变化清除旧结果，迟到记录刷新补查。本次查询不是存储快照，末页不表示采集完整。System 只访问受控 Loki 查询代理，不接收客户端文件路径、LogQL 或远端地址。失败返回明确错误，采集完整性始终标为未知。

正文由 Alloy/Loki/MinIO 保存，不写入 IAM 审计表。标准开发和容器启动通过 `common/cmd/runtime-log` 生成并传递进程身份。重启产生新身份，原有 DOWN 登记事实保留，便于查询集中保留期内的旧日志。尚未登记的进程使用可信来源目录提供日志入口，不虚构登记实例或启动失败状态。

标准验证：`make test-module MODULE=system`（显式注入允许的 IAM 测试 DSN），`make test-system-runtime-log`。正文、来源及查询限制见 [模块服务运行日志规范](../docs/spec/addp模块服务运行日志规范.md)，设施运维见 [Infra 指南](../scripts/infra/README.md)。

### 未登记实例的运行日志

System 唯一拥有 `module_log_sources` 保留目录。Infra `addp-log-observer` 使用独立 `system.module_log_source.create` Permission 与绑定节点上报有界元数据；Monitor 健康接收器列表不承载历史目录。Platform User 的 `platform.module_log.read` 同时用于目录和单实例正文。GET `/platform/module-log-sources` 排除当前登记关联；既有正文路由同时验证登记身份或保留来源，按可信节点筛选、绑定分页游标。未登记不是 DOWN 或启动失败，采集时间不代表业务启动时间。目录按最后来源观测加实际集中保留时长过期，不因重复扫描延长。数据库门禁新增 repository 分组，由 `scripts/test/system-iam-postgres-gate.sh --package repository` 运行，纳入既有 System IAM CI。

`cmd/online-test-fixture` 支持 Redis 的 `redis-consumer-flow` Hosted T4 身份准备：非默认 Tenant 的普通用户仅授予 `system.engine_catalog.read`、Meta 目录/扫描和 Manager 阅读权限，输出浏览器登录凭据到 owner-only Secret 文件；独立 Engine provisioner 经正式 API 登记后由生命周期移除其凭据，不把基础设施管理权限加入消费用户。

ES Hosted T4 的 System-owned `cmd/online-test-fixture --suite elasticsearch-consumer-flow` 仅建立非默认 Tenant、最小权限消费 User 和独立 Engine Provisioner。消费 User 读取 System 目录而不读取 Engine 控制面；身份和登录凭据仅写入 owner-only 临时目录。引擎由正式 API 登记，所有身份随 Hosted 平台库销毁。

平台指标发现身份（2026-10-05）：迁移 000191 发布独立 `addp-prometheus` Platform Service Principal，唯一权限 `monitor.metrics_discovery.read`，没有 Tenant Runtime 绑定。`PROMETHEUS_SERVICE_CLIENT_SECRET` 可选且不可与业务/日志观测凭据复用；指标选择关闭或凭据非法时只禁用该 Client，授权版本和旧 Token Family 随凭据关闭撤销，不阻断业务必需客户端。节点监测目标 CRUD 权限仅默认授予平台系统管理员，身份事实仍由 System 当前节点接口与观测投影各自裁决。精确 IAM/OAuth 回归通过既有 `test-system-iam-postgres` 选择，完整 CI 自动发现新增测试，设计见平台运行监控文档第 3.3、4.3 节；生产采集与 Prometheus 接线另行验收。

任务授权主体沿用唯一 `POST /auth/task-authorization-subjects` 和 Runtime resolve 入口，支持严格的 `orchestrator/orchestration → addp-orchestrator`、`transfer/sync → addp-transfer` 绑定。User 入口要求 `system.execution_authorization.create`，再按 owner/type 检查对应模块执行 Permission；System 在事务内重核当前 User、Membership、Tenant、授权版本与两项 Permission。000216 只放开上述二元组约束并向 `tenant.transfer_runtime` 增授不可委派的 `system.task_authorization.execute`，不授予普通 User，不创建 Assignment。resolve 拒绝记录安全审计；主体不包含 Token，也不授予引擎数据访问。Transfer 定义摘要和领取事务由 Transfer 拥有，System 不跨读其私表。确定性与 PostgreSQL 测试沿现有模块门禁及 `AgainstPostgres` 自动发现。

本人角色自查：`GET /api/v1/system/auth/subject-roles` 仅消费当前第一方 Tenant User Bearer，不接收主体、租户、角色筛选或正文，不增加功能权限或读取其他人角色的能力。复用执行角色核验的同一有效 Assignment 查询，包含没有功能权限的业务角色；AuthContext v1 仍只投影功能权限角色，不扩张认证协议。身份锁后与角色读取后复核当前 Token Family、Principal、Membership、授权版本及数据库墙钟，期限取令牌、Family、Membership 和返回角色的最早期限。响应 `no-store`；Common 客户端不缓存、不跟随重定向、不隐藏重试、不回退 Service 凭据，并与当前 AuthContext 精确比对主体、租户、成员和版本。异步执行仍必须走已有 execution/attempt/lease 绑定接口。真实 PostgreSQL 用例与 `execution-subject-roles` 标准 IAM 子组共入口执行，覆盖纯业务角色、授权版本、自然到期、登出与非本人凭据拒绝。
