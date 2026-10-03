# System IAM 数据模型与迁移规范

更新日期：2026-10-03

状态：System 模块正式实现规范。本文定义已投入运行的 IAM PostgreSQL 模型、事务边界、迁移路径和安全约束。

## 一、事实边界

System 是 ADDP 唯一 IAM 逻辑权威，负责 Principal、账号、认证方式、Tenant Membership、组织关系、Role/Permission、会话、OAuth 协议事实和 IAM 安全审计。

业务资源和资源级授权事实仍归对应 owner。System 不复制全平台业务资源，也不建立中央资源 ACL 大表。

System 自身拥有引擎访问控制领域。该领域的 `engine_access_grants` 是精确源数据共享的不可变签发历史：以原办理编号唯一引用 `engine_access_fulfillment_outcomes`，参数仍由已提交的不可变受理回执提供，不复制 Catalog 责任、业务决定或 IAM Role/Permission。迁移 000182 不自动签发、不新增权限或角色分配、不推进 IAM 授权版本；签发不改变 AuthContext 的功能 Permission。内部签发与高风险审计同事务，插入触发器按数据库墙钟检查原 accepted 窗口并拒绝客户端回填时间。当前没有公开签发或执行侧消费入口；签发历史不能代替后续到期、撤销、Deny 和当前接收主体的访问裁决。

迁移 000183 增加 `engine_access_grant_revocations`：以原办理 UUID 唯一引用 Grant，追加撤销者 Principal／Tenant Membership、数据库墙钟与必填原因；不复制或改写原目标、接收主体、动作和期限。行及整表历史不可 UPDATE／DELETE／TRUNCATE，插入核验撤销者为原 Tenant 的 User 成员。真实撤销命令发布独立 `system.engine_access_grant.revoke`，不默认分配 Role 或账号，不推进既有授权版本；服务核验当前账号、Token、Permission 和引擎管理委派，并同事务写入高风险撤销审计。引擎停用或失去实时目录能力仍允许收回旧授权，账号或委派失效则拒绝；不修改新授予的启用条件。撤销历史不是 Explicit Deny，也不表示执行侧数据权限已经接通。

IAM 权威表位于 PostgreSQL `system` schema，只允许 `system/backend/internal/migration/sql/*.up.sql` 单向迁移。IAM 表不得进入 GORM `AutoMigrate`，运行时不得根据表存在性补列、建表或写兼容种子。

## 二、统一字段规则

- 主键使用 PostgreSQL bigint，API 中以十进制字符串表达 IAM ID；
- 时间使用 `timestamptz` 和数据库时间；
- 浏览器认证、MFA、Context Selection／Switch、Token Family 签发及刷新统一通过 IAM Repository 读取数据库 `statement_timestamp()`，不默认读取应用时钟；读取失败必须回滚，不回退应用时间。取锁后再读取当前语句时间，不能使用等待锁之前的时间或固定事务开始时间判断过期。AuthContext 快照在同一查询中读取该时间；一次判定复用同一个时间值。登录认证时间来自成功认证事务，跨事务传递时保留原事实；MFA 与上下文转换不延长已有 Family 最终期限。TOTP 计数与过期判断使用同一数据库时间，保留既有防重放规则。服务的时间依赖显式包含 Context 与 Repository，仅测试可注入受控时间读取器，正式装配使用唯一数据库读取实现。
- 生命周期使用显式状态，不使用可产生歧义的通用 `is_active` 替代完整状态机；
- Principal 授权事实变化必须推进 `authorization_version`；
- IAM 安全事实和对应审计事件必须在同一数据库事务提交；
- Token、Code、一次性 Secret 只保存不可逆 Hash；密码保存自适应 Hash；MFA Secret 使用独立密钥加密。

凭据校验的内部失败诊断必须从实际消费的快照取证，不以失败后重新查询的时间代替原始校验时间。诊断仅保留固定条件码、数据库时间、凭据创建／认证时间及相关过期边界，不保留 Token、Hash、完整快照或身份详情。诊断随内部错误返回给测试排查，不加入 HTTP 错误正文；未授权错误语义和现有有效性判断保持一致。该证据只证明一次校验的触发条件，不能单凭创建时间较晚就断言宿主机或数据库的时钟同步故障。

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

Department 和 Project Group Scope 必须引用当前 Tenant 的可用组织对象，且目标 Tenant Membership 必须已具有对应的有效组织成员关系。后端在创建 Assignment 的同一事务内复核 Department 为 `active`、Project Group 不为 `closed`，并锁定对应的 Department Membership 或 Project Group Membership；跨 Tenant 或不存在返回 `404`，不可用或缺少有效组织成员关系返回 `409`。前端只允许通过可搜索选择器选择组织名称和 Code，提交时保持 `department_id` / `project_group_id` 稳定协议；列表主要展示名称和 Code，只在历史对象无法解析时回退展示 ID。

租户 Role 管理列表必须保持单行可浏览：主列表只展示 Role 名称、标识、类型、适用对象与 Scope、Permission 数量及模块摘要；Role 描述和完整 Permission 必须统一放在详情视图，Permission 按命名空间、资源与动作分组，不得用长描述或逗号拼接文本撑高整张表。角色定义首先按 `allowed_principal_types` 分为“用户账号角色”和“机器身份角色”，默认展示用户账号角色，并允许查看全部角色；分类不得依赖 Role Key 后缀或展示名称。列表还必须支持名称或标识搜索、Role 类型和允许 Scope 过滤。

Role Assignment 列表支持按 Membership 与 `principal_type=user|service_principal` 组合过滤，成员列必须显式展示成员类型；“当前账号”只通过 AuthContext 的 `tenant_membership_id` 识别，不按用户名、邮箱或展示名称猜测。持久 `status` 只表达 `active` / `revoked` 生命周期，读取接口使用数据库时间派生 `effective_state=scheduled|effective|expired|revoked`，默认只返回 `effective`。只有 `scheduled` 和 `effective` Assignment 可撤销；`expired` 只读，后端与数据库必须拒绝将其改写为 `revoked`。Role 选择器把兼容 Role 分为“可分配角色”和“已分配角色”，只以同一 Membership、同一 Scope 下的 `effective` Assignment 禁止重复选择，历史撤销或到期记录不得阻止重新分配。数据库对同一 Principal、Role 和 Scope 使用不重叠的半开授权区间 `[valid_from, valid_until)` 约束重复，不再以所有持久 `active` 记录共用一个唯一位置。

`role_assignments.grant_reason` 保存授予原因；手工创建必须非空。撤销时必须原子写入 `revoked_by_principal_id`、`revoked_at` 和非空 `revoked_reason`，撤销后的 Assignment 不可恢复或修改。授权详情由 `GET /api/v1/system/tenant/role_assignments/:id` 读取，返回授予与撤销操作者的可读 Principal 引用、完整时间和原因；已撤销记录还派生同一 Principal、Role、Scope 下的 `same_scope_active_assignment_id`，但不把重新授权持久化为旧 Assignment 的替代关系。授权生命周期详情沿用 `iam.tenant_role_assignment.read`，不扩大 Tenant 审计权限。

Tenant 审计的成员筛选以所选 Tenant Membership 的 `principal_id` 查询既有审计协议，并支持按 `principal_type=user|service_principal` 过滤操作者类型；成员列必须显式展示操作者类型。模块筛选使用 `module_name` 的稳定协议值。界面必须以本地化模块名称作为主标签、稳定 `module_name` 作为辅助标识，不得把中文名称写入审计事实或新增兼容字段。审计导出必须覆盖当前筛选条件下的全部事件，不得复用普通列表的单页上限静默截断；后端使用稳定排序的分批读取和有界内存文件生成，响应通过 `X-ADDP-Export-Count` 明确返回实际导出条数。

平台系统管理员、安全管理员和审计管理员是三个互斥 User Role，不存在全权合并角色。平台高权限身份变化使用 `privileged_change_requests` 和 `privileged_change_approvals`，申请人与审批人必须满足职责分离要求。

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

### 引擎访问控制领域的管理委派

`system.engine_access_delegations` 由 System 引擎访问控制领域独立维护，不是 Role Assignment 或全平台中央 ACL。它绑定同 Tenant 的 Engine 和 User Membership，保存显式到期时间、授予原因／操作者／时间、版本，以及撤销原因／操作者／时间；无源端写入、无默认角色授权、无 Catalog 副本。只允许创建、读取和版本化撤销；授权范围及接口以 `docs/spec/addp授权上下文规范.md` 5.5.2 为准。

同一 Engine、同一 Membership 的有效区间不得重叠；数据库拒绝身份、范围、期限、授予事实的修改，以及撤销历史的恢复／删除。写入与安全审计在同一事务完成，并通过既有 IAM Repository 推进接收主体授权版本、撤销旧会话。读取派生 `effective/expired/unavailable/revoked`，不得把持久 `active` 等同于当前有效管理资格；尚未实施的源读取规则不能消费此表绕过最终资源授权。

### 源授权办理协调底座

正式准备→可信反查→首次受理（2026-10-03）：Catalog 人类入口从可信 AuthContext 派生操作人，只接收请求／决定编号和批准要求版本；本地完整待核清绑定提交后才发送。System `POST /runtime/engine-access-fulfillments/:request_id/accept` 限定 `addp-catalog` 当前 Tenant Service 身份及 `.execute`；使用独立 `addp-system` Tenant Service OAuth 在事务锁外反查 Catalog 精确待核清依据。返回后对机器主体、办理人、确认人及接收账号去重升序锁定，事务内核验当前 IAM、独立人类办理权限、管理委派、接收主体及精确批准要求版本。确认人历史授权版本仅供审计，办理人版本必须与请求一致；等待后按数据库墙钟复核权限及期限。反查期间先提交的原受理／关闭结果优先按完整绑定核清，不由旧人员状态阻断历史恢复。

迁移 `000180_catalog_fulfillment_basis_runtime` 登记不可委托、不可租户定制的 `catalog.sharing_fulfillment.read`，只授予内置 `tenant.system_runtime`；为存量已初始化 Tenant 和未来新 Tenant 接入 `addp-system`。OAuth Client 初始停用且 Secret 为空，SQL 不保存明文或借用其他服务 Secret。`SYSTEM_SERVICE_CLIENT_SECRET` 是可选独立配置：缺失不阻断 System Ready，新受理明确返回 503 `fulfillment_capability_unavailable`，历史核清／关闭及同参结果恢复仍可用；移除配置通过既有 Provisioner 停用 Client 并撤销活动 Token Family，不回退独立批准或推断 Catalog 退出。未运行真实 OAuth 双服务 Online T4，不以受控 owner Transport 夹具替代其验收。

源授权办理协调底座使用 `system.engine_access_fulfillment_outcomes` 保存同次请求唯一、不可改写的 `accepted` 或 `closed` 结果。它不是 Grant、批准要求或访问凭据；没有结果与已关闭严格不同。请求编号绑定租户、机器主体、精确结构化路径、决定引用、批准要求版本及参数；System 数据库墙钟产生受理时间及不超过原授权到期的 5 分钟截止时间。结果与最小审计同事务，重试只返回原结果。正式新受理已由专用 Runtime 接口消费该底座，真实业务决定与当前资格独立核验；受理接口本身不写入 Grant，内部签发另由 000182 对应路径落实。受理回执不能代替内容访问裁决。

核清读取使用独立只读事务，拒绝在受理写事务中读取自身未提交结果；与仲裁共用唯一的完整绑定匹配路径，不取请求或目标仲裁锁。不存在返回未找到，参数不同返回绑定冲突，数据库错误原样保留；查询均不创建关闭结果、不追加办理审计。退出、更换批准要求版本或原窗口到期不改变历史查询结果，也不因此重新授予或续期。

生产核清通过 `POST /runtime/engine-access-fulfillments/:request_id/resolve|close` 接入：限定 `addp-catalog` 当前 Tenant Service 身份及 `system.engine_access_fulfillment.execute`，Tenant 从规范 AuthContext 派生，请求中的原调用主体须匹配认证主体。服务当前 OAuth Client、Service Principal、Tenant Membership、授权版本、Permission 与 Token 期限在本地事务核验，关闭等待请求／目标锁后再次核验期限再提交；原操作人或接收方后来失效不阻断历史核清。明确未找到只是查询结果，关闭与原受理竞争，返回先提交的原结果。迁移 000176 仅向 `tenant.catalog_runtime` 增加此不可委托、不可租户定制的 Tenant Permission，受影响的活跃角色分配主体递增授权版本并撤销 Refresh Family；不新增 User Role 授权、Assignment、新受理或 Grant。Catalog 后台使用独立 Service 客户端消费结果，不持 System 事务锁跨模块调用，通信故障保留待核清保护，不影响服务 Ready。

首次受理须在同一事务通过 IAM 仓储核验接收 User 及本 Tenant Membership，或本 Tenant 的 Department／Project Group。共享账号锁先去重并按 ID 升序取得，再读取成员、Tenant 与组织；共享锁保护生命周期至事务结束，业务核验后按数据库墙钟再次检查成员期限。接收组织不要求非空，接收账号不绑定当前授权版本快照；这不代替实际 Grant 写入和访问时的当前核验。历史受理／关闭结果的核清不受后来接收主体失效影响，不产生新授权。

完整 `binding` 同时绑定原操作人的 User Principal、Tenant Membership 和授权版本，以十进制字符串保留 bigint 精度，不保存 User Token 或 Role／Permission／责任副本。首次受理通过 IAM Repository 的事务内 `LockUserAuthorizationSource` 按 Principal → Membership → Tenant 取得共享行锁，再进入请求和目标边界；核验当前对象类型、状态、成员身份范围、授权版本和有效期，业务核验后重新检查数据库墙钟。共享锁既阻止 IAM 状态修改，又兼容审计外键引用检查，避免身份写锁与目标锁的循环等待；原管理委派写事务不改用共享锁。原操作人失效不阻止同参历史核清或持久关闭，但不得据历史结果新建受理。此底线本身不证明这些字段确实来自该用户操作；正式消费者通过 Catalog 专用 owner 接口核对原操作人持久来源，并独立核验当前功能 Permission 与管理委派，不保存或透传 User Token。

`system.engine_access_approval_requirements` 保存精确源目标的 `catalog/independent` 要求及并发版本，既不是 Grant，也不保存 Catalog 责任副本或审批人白名单。初始化必须显式执行；缺失不能推导为允许独立批准。要求变更与新受理使用同一目标事务锁，新 Catalog 受理必须匹配当前模式和版本；退出不改变之前已受理的原结果，重新启用也不能复用旧版本新建受理。承接账号只写入当次交接审计，后续批准仍须核验当前独立批准 Permission 与有效管理委派。迁移 000168 不自动初始化存量目标、不创建 Permission 或真实 Grant。迁移 000174 随真实初始化／读取 API 发布两个 Tenant Scope、不可委托且允许租户自定义的独立 Permission，不默认分配角色或修改主体授权版本。初始化固定建立版本 1 的 `catalog` 要求，IAM 当前权限与管理委派在同一事务核验，等待目标锁后再次检查期限并写入配置及审计；已有要求返回冲突，不覆盖独立模式。读取同时按当前 Tenant、Engine 及当前委派隔离。退出／重新启用与 Grant 仍待贯通；跨模块首次受理已接通，内部交接核验回调不视为生产资格证明。

迁移 `000170_catalog_sharing_decision_permission` 随 Catalog 真实业务确认 API 发布独立 `catalog.sharing_decision.create`：仅 Tenant Scope、high risk、不可委托、允许租户定制。它只登记功能权限，不默认分配给内置管理员或任何 Role，不新增 Assignment 或数据 Grant、不修改已有主体授权版本。Catalog 仍须以该权限与当前业务负责人资格取交集；System 新受理消费者已接通，实际源数据规则尚未贯通。前向升级和重复执行由既有 System IAM PostgreSQL 门禁覆盖，不操作开发业务库的迁移状态。

迁移 `000178_engine_access_fulfillment_handling` 随 System 当前 User 办理范围观察和 Catalog 人类候选摘要读取登记独立 `system.engine_access_fulfillment.create`：仅 Tenant Scope、high risk、不可委托、允许租户定制。没有默认 Role Permission、Assignment 或 Grant，不修改现有主体授权版本。业务确认权、编目权、管理委派或机器 `.execute` 都不能替代它；有效管理委派仍须另外核验。前向迁移与重复运行并入既有 `engine-access-coordination` PostgreSQL 分组和完整迁移门禁。该观察接口本身不是受理或内容授权入口；正式准备与首次受理另通过专用入口消费同一独立 Permission。

普通只读共享显式选择 `expiry_mode=at_time|until_revoked`，受理结果和完整 binding 都保存该模式，`grant_expires_at` 在长期有效时为 NULL。指定到期仍须为未来时间；长期有效的自动办理窗口仍严格为原受理时间后 5 分钟。迁移 `000172_engine_access_fulfillment_expiry_mode` 在排他表锁及同一事务内暂时撤下该表 UPDATE/DELETE 不可变保护，将存量限时记录无损标记为 `at_time`、为 binding 补齐模式，再恢复保护及完整日期／截止时间约束；原到期、受理时间、截止时间、身份和审计不变。不改写已执行 167，不增加实际 Grant、默认权限或授权版本变化。长期有效不改变临时接入规则和管理委派的强制到期契约。

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

### 6.3 Notebook Session Authorization

`notebook_session_authorizations` 保存由当前 Tenant User Access Token 派生、绑定唯一 Notebook Session 和 Task 的短期授权事实。它不是 Token，不保存 Token Hash、Engine 列表或连接信息，也不新增 AuthContext 类型；身份边界通过 User Principal、Tenant Membership、Token Family 和签发时 `authorization_version` 固定。它只允许实时 Catalog 发现，以及为每次 Notebook 只读查询/扫描派生独立 Execution Authorization。派生记录必须通过 `execution_authorizations.source_notebook_session_authorization_id` 保存唯一来源，并继承 Session 的身份、有效期和撤销边界。

签发、消费、撤销必须使用版本化 SQL 表和事务审计。每次消费实时回查 Principal、Tenant、Membership、Token Family、授权版本和当前 Permission；Family 或 Session 撤销必须在同一事务联动撤销 Session Authorization 及其派生且仍有效的 Execution Authorization。标准 Engine Access 租约复核必须沿来源外键重复校验这条生命周期链。详细契约以 `docs/spec/addp授权上下文规范.md` 和 `docs/spec/addp登录认证的统一要求.md` 为准。

## 七、Bootstrap、密码重置与灾难恢复

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

### 7.4 三员灾难恢复

三员凭据全部不可用时，只允许离线 `iam-recovery prepare/apply`。恢复不删除或重建 Bootstrap 状态，不通过 SQL 直接替换 Hash，不开放 HTTP API。

恢复一次性重建三员密码和 TOTP，撤销旧会话并保留完整审计。Secret、密码、TOTP Secret 和验证码不得进入参数、环境变量、日志或审计详情。

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

等待锁的实例必须在获得锁后重新读取版本。migration 失败会保留 dirty 状态并阻止 System 启动，不允许自动回退、跳过版本或运行时兜底。

已执行 migration 的摘要记录在 `system.schema_migration_checksums`；该表只由 Migration Runner 维护，不是手工修改版本或跳过迁移的通道。当前版本由 `system/backend/internal/migration/sql` 和 `internal/migration/catalog_test.go` 共同约束；本文不固定抄写版本号。

## 十一、管理端信息架构

System IAM 管理端只按稳定业务大类提供五个左侧页面，不能把每个管理对象平铺为一个左侧入口，也不能继续把全部对象放入单个工作台：

| 页面 | Platform Context | Tenant Context |
| --- | --- | --- |
| 组织管理 `/iam/organization` | 租户管理 | 部门管理、项目组管理 |
| 账号管理 `/iam/accounts` | 用户账号、身份变更审批 | 用户账号、用户邀请 |
| 角色管理 `/iam/roles` | 无可用对象时隐藏 | 角色定义、角色分配 |
| 应用接入 `/iam/application-access` | 无可用对象时隐藏 | API 消费方、外部应用（OAuth）、租户服务账号、平台运行账号 |
| 审计管理 `/iam/security` | IAM 安全策略、平台审计 | 租户审计 |

页面表达业务大类，页内 `tab` 表达该类中的具体管理对象或流程。Tab 必须继续按当前 AuthContext 类型和 Permission 过滤；某个页面在当前上下文中没有任何可用 Tab 时，Console 左侧入口和 System standalone 导航都必须隐藏，直接访问也不得绕过 Context 与 Permission Guard。

当前 User 的凭据与 MFA 是跨 Tenant Context 的全局自服务对象，不是租户账号管理对象。它的唯一页面为 `/account/security`，由 Console 和 System standalone 右上角“我的账号”入口打开，不得再作为 `/iam/accounts` 内的 Tab。页面标题使用“我的账号”，当前功能分组使用“安全设置”，TOTP 功能使用“多因素认证”。

IAM 管理页页头必须用业务语言表达当前作用范围和会话认证状态：Tenant Context 显示“当前租户：名称（编码）”，Platform Context 显示“当前管理范围：平台”；不得向用户展示 `租户上下文 #<id>` 等内部标识。AAL 按“基础认证”“多因素认证”等语义展示，原始协议值只作为辅助说明。Tenant 名称和编码使用现有 `context-options` 权威结果，不得根据 Tenant ID 猜测或建立第二套查询。

“用户账号”、“租户服务账号”和“平台运行账号”是三类管理责任不同的对象，不得混排在同一主列表中。用户账号页只展示 User Membership；租户服务账号页只展示租户可管理的 Tenant-owned Service Principal；平台运行账号页只展示已加入当前 Tenant 的 Platform-owned Runtime Service Principal。角色管理中的角色分配页只展示 User；Service Principal 的角色入口分别归入“租户服务账号”和“平台运行账号”页；租户审计仍覆盖两类 Principal，并在选择器或过滤器内按“账号类型”分组并显式标记。

Tenant-owned 服务账号由 Service Principal、唯一 Tenant Membership 和一对一 Confidential OAuth Client 组成，三者生命周期由同一个服务账号 API 原子管理；内部 OAuth Client 不得出现在“外部应用（OAuth）”列表。Platform-owned Runtime Service Principal 在独立的“平台运行账号”页中只读，不向 Tenant 管理员暴露凭据轮换、定义修改或生命周期操作。两类机器身份的角色展示均复用 Role Assignment API；只有 Tenant-owned 服务账号允许 Tenant 管理员创建或撤销兼容的 Runtime Role Assignment。外部应用是代表 User 执行 Authorization Code + PKCE 的 Public OAuth Client，不是账号，也不接受 Client Credentials。

“应用接入”页签按 Tenant 管理员的常见工作频率排列：“API 消费方”、“外部应用（OAuth）”、“租户服务账号”、“平台运行账号”。前两项是对外接入的主要工作流；租户服务账号是高级自动化能力；平台运行账号只用于运行时角色查看与故障排查。该顺序是唯一默认顺序，不保留旧“机器身份”混合页签或其 query 兼容路径。

租户服务账号和平台运行账号的角色列表与选择器必须优先显示角色的本地化业务名称，稳定 `role_key` 只作为技术识别辅助展示；所有内置 Tenant Role 的中英文名称和描述必须随 Role Catalog 同步发布并由清单覆盖测试约束。

API 消费方只用于外部系统消费已发布的数据面 API，不是 Principal，不建立 Tenant Membership、Role Assignment 或 OAuth Client。System 保存 API Consumer、一次性展示的 API Consumer Credential Hash，以及精确的 `ConsumerServiceReference(service_type, service_id)`；首期只允许绑定 Service 已正式发布到服务消费目录的 Query Service。Gateway 只在显式数据面路由验证凭据、限流和记录调用，Service owner 必须再次验证 Credential，并按当前 Tenant、精确 Service Reference、服务状态和发布状态完成最终授权。`X-API-Key` 不得进入 `/api/v1/*` 控制面；控制面继续只接受其路由声明的 Bearer Credential。

Console 公开路由与 System standalone 路由使用同一模块内 path 和 query 契约。旧 `/iam?tab=...` 工作台、`/iam/identity`、`/iam/access` 和 `/settings/security-policy` 路径不保留重定向或兼容读取；审计唯一使用 `/iam/security?tab=audit`，具体 Platform 或 Tenant 范围只从当前 AuthContext 推导。

## 十二、迁移演进

- 只增加新的 `NNNNNN_name.up.sql`，不修改已发布 migration；
- 已执行 migration 的版本号、文件名和内容摘要必须保持不变；概念收敛或方案重做也必须使用新版本向前迁移；
- Quality 域引用权限的已执行 `000149` 保持原始摘要；后续域读取授权与服务身份刷新使用 `000150`。回归测试须从原始 149 已执行状态升级，校验历史摘要保持不变及重复启动幂等，不能只验证空库迁移。
- Permission/Role Catalog 变化由聚合器生成确定性输入，再进入新的向前 migration；
- 破坏性模型切换不保留旧字段、双写、双读或兼容 query；
- 需要保留的外部数据必须另行批准离线导入方案，不进入 System Runtime；
- migration 内不得访问 Redis、HTTP、外部 IdP、密钥服务或其他模块数据库。
- 已登记的定向恢复只有 75 号历史不可变 audience 冲突、113 号 Security 首次失败迁移的完整回滚状态，以及 130 号 Security 原值访问申请权限迁移的完整回滚状态；只能使用 `cmd/iam-migration-repair --migration <75|113|130> --apply`。75 号必须通过精确状态、checksum、约束和触发器校验；113 号必须通过 `(113, dirty)`、checksum 恰好到 112、Security 目标事实零落地与 Standard 原分类权限未变更校验；130 号必须通过 `(130, dirty)`、checksum 恰好到 129、新申请权限零落地、旧豁免变更权限与内置角色绑定仍完整的校验。三者都不得扩展为通用 dirty force、跳过版本或 checksum 改写能力。

## 十三、验证

非数据库单元测试：

```bash
cd system/backend
go test ./internal/authorization/... ./internal/migration ./internal/iam/...
```

完整 IAM、Fosite Storage、API 和 migration PostgreSQL 发布门必须使用专用一次性数据库：

```bash
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://.../addp_iam_test?...' make test-system-iam-postgres
```

该门禁会重建目标数据库的 `system` 和 `common` Schema，禁止指向开发库或生产库。

标准成员清单能力退出后，迁移 000140 停用其六项 Permission、删除全部 Role 关联并撤销受影响会话，并撤销 `tenant.standard_runtime` 的人员目录读取授权；已应用的历史迁移保持校验和不变。Catalog 的组织引用继续只允许 `addp-catalog` 调用，由 Catalog 专用解析方法处理。
