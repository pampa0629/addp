# System IAM 数据模型与迁移规范

更新日期：2026-08-28

状态：System 模块正式实现规范。本文定义已投入运行的 IAM PostgreSQL 模型、事务边界、迁移路径和安全约束。

## 一、事实边界

System 是 ADDP 唯一 IAM 逻辑权威，负责 Principal、账号、认证方式、Tenant Membership、组织关系、Role/Permission、会话、OAuth 协议事实和 IAM 安全审计。

业务资源和资源级授权事实仍归对应 owner。System 不复制全平台业务资源，也不建立中央资源 ACL 大表。

IAM 权威表位于 PostgreSQL `system` schema，只允许 `system/backend/internal/migration/sql/*.up.sql` 单向迁移。IAM 表不得进入 GORM `AutoMigrate`，运行时不得根据表存在性补列、建表或写兼容种子。

## 二、统一字段规则

- 主键使用 PostgreSQL bigint，API 中以十进制字符串表达 IAM ID；
- 时间使用 `timestamptz` 和数据库时间；
- 生命周期使用显式状态，不使用可产生歧义的通用 `is_active` 替代完整状态机；
- Principal 授权事实变化必须推进 `authorization_version`；
- IAM 安全事实和对应审计事件必须在同一数据库事务提交；
- Token、Code、一次性 Secret 只保存不可逆 Hash；密码保存自适应 Hash；MFA Secret 使用独立密钥加密。

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

Department 使用 `active` / `disabled` 生命周期。Department 是可被 Catalog 等 owner 软引用的稳定组织身份，不提供物理删除 API；停用后不允许建立新的成员关系或责任引用，既有历史仍可解析，允许管理员显式恢复。Department code 在创建后不可修改，层级更新必须继续满足同 Tenant、无循环约束。部门结构变更必须持有当前 Tenant 的结构级事务锁，生命周期变更与成员关系写入必须锁定同一 Department 聚合根，避免并发创建循环或在停用过程中新增成员。

Project Group 使用 `planned` / `active` / `closed` 生命周期。`planned` 可以推进为 `active`，`planned` 或 `active` 可以关闭；`closed` 是不可逆终态。关闭后不允许新增或恢复成员关系，既有临时授权按授权上下文规则失效。`starts_at` / `ends_at` 只表达计划周期，关闭动作不得用实际关闭时间覆盖 `ends_at`；实际关闭时间和原因保存在审计事实中。Project Group code 在创建后不可修改，第一阶段不支持嵌套。生命周期变更与成员关系写入必须锁定同一 Project Group 聚合根。

Department Membership 的 `membership_type` 使用 `primary` / `additional`，`relation_role` 使用 `member` / `leader`；同一 Tenant Membership 最多一个有效主部门。Project Group Membership 的 `relation_role` 使用 `member` / `leader` / `coordinator`。两类成员关系都使用 `active` / `ended`，`ended` 是历史终态；成员身份、所属组织和 Tenant Membership 创建后不可变，需要调整时必须结束旧关系并创建新关系。

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

## 五、Permission、Role 与高权限治理

`permissions`、`roles`、`role_permissions`、`role_assignments` 和 `role_conflicts` 保存运行时 RBAC 事实。Permission 和内置 Role 的发布规则见 `docs/spec/addp权限与角色发布规范.md`。

平台系统管理员、安全管理员和审计管理员是三个互斥 User Role，不存在全权合并角色。平台高权限身份变化使用 `privileged_change_requests` 和 `privileged_change_approvals`，申请人与审批人必须满足职责分离要求。

Role、Assignment、Membership、组织关系或 Principal 状态变化时，数据库和 Service 必须在同一事务中：

- 锁定受影响 Principal；
- 修改权威事实；
- 推进授权版本；
- 撤销或转换受影响 Token Family；
- 写入唯一安全审计事实。

## 六、会话与令牌

### 6.1 Context Selection

`context_selection_tickets` 保存登录后的短期上下文候选快照。Ticket 只使用一次，消费时必须重新校验 Principal、Membership、Platform Assignment、认证强度和授权版本。

消费 Ticket 和创建新 Token Family 必须在同一事务；并发消费只能成功一次。

### 6.2 Token Family

`refresh_token_families` 固定保存 Principal、Context、Client、认证方法、AAL、认证时间、授权版本和有效期。Family 创建后这些身份事实不可变。

`access_tokens`、`refresh_tokens`、`resource_access_tickets` 和 `delegated_access_tokens` 都只保存 Hash，并绑定 Family 或源 Token。AuthContext 解析必须回查当前 Principal 和授权版本，不依赖 Token 内自包含 claims。

Refresh 采用轮换和重用检测。并发 Refresh、logout、context switch、MFA 转换按统一 Principal 和 Family 锁顺序竞争；发现已消费旧 Refresh Token 时撤销整个 Family，但正常并发失败不能误报为重用攻击。

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

## 十一、迁移演进

- 只增加新的 `NNNNNN_name.up.sql`，不修改已发布 migration；
- 已执行 migration 的版本号、文件名和内容摘要必须保持不变；概念收敛或方案重做也必须使用新版本向前迁移；
- Permission/Role Catalog 变化由聚合器生成确定性输入，再进入新的向前 migration；
- 破坏性模型切换不保留旧字段、双写、双读或兼容 query；
- 需要保留的外部数据必须另行批准离线导入方案，不进入 System Runtime；
- migration 内不得访问 Redis、HTTP、外部 IdP、密钥服务或其他模块数据库。
- 已登记的定向恢复只有 75 号历史不可变 audience 冲突、113 号 Security 首次失败迁移的完整回滚状态，以及 130 号 Security 原值访问申请权限迁移的完整回滚状态；只能使用 `cmd/iam-migration-repair --migration <75|113|130> --apply`。75 号必须通过精确状态、checksum、约束和触发器校验；113 号必须通过 `(113, dirty)`、checksum 恰好到 112、Security 目标事实零落地与 Standard 原分类权限未变更校验；130 号必须通过 `(130, dirty)`、checksum 恰好到 129、新申请权限零落地、旧豁免变更权限与内置角色绑定仍完整的校验。三者都不得扩展为通用 dirty force、跳过版本或 checksum 改写能力。

## 十二、验证

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
