# System IAM 数据模型与迁移规范

更新日期：2026-08-01

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

## 七、Bootstrap、密码重置与灾难恢复

### 7.1 首批三员 Bootstrap

系统不创建默认管理员。空 User 系统只允许使用与 System 同版本发布的离线 `iam-bootstrap prepare/apply`：

- `prepare` 生成一次性随机 Secret，只保存 Hash；
- `apply` 通过 TTY 收集三名管理员各自密码和 TOTP 验证；
- 三个 User、凭据、互斥 Role Assignment、审计和永久完成状态在单一事务提交；
- 任一 User 已存在时拒绝 Bootstrap，不提供 HTTP、弱密码或默认账号旁路。

### 7.2 普通 User 密码重置

平台安全管理员只能为不持有有效 Platform Role 的普通 User 执行受控重置。事务必须替换 Password Hash、清除临时锁定、推进授权版本、撤销全部 Token Family、终止未消费 Ticket/Challenge，并写入高风险审计。

### 7.3 三员灾难恢复

三员凭据全部不可用时，只允许离线 `iam-recovery prepare/apply`。恢复不删除或重建 Bootstrap 状态，不通过 SQL 直接替换 Hash，不开放 HTTP API。

恢复一次性重建三员密码和 TOTP，撤销旧会话并保留完整审计。Secret、密码、TOTP Secret 和验证码不得进入参数、环境变量、日志或审计详情。

## 八、OAuth 协议表

OAuth Client、Authorization Request、PKCE、Authorization Code、Device Authorization 和 Token Family 等协议事实由同一个 Fosite Storage Adapter 访问。具体表映射、锁顺序和 Provider 组合见 `system/docs/OAuth与Fosite实现说明.md`。

OIDC 表字段是未来协议启用所需的受控预留，不表示 OIDC 已对外启用。

## 九、迁移 Runner

System 启动顺序固定为：

1. 使用启动期专用单连接池连接 PostgreSQL；
2. 读取嵌入 migration 目录并校验版本连续性；
3. 获取 PostgreSQL session advisory lock；
4. 拒绝 dirty、数据库版本超前和 legacy IAM schema；
5. 按版本分别在事务中执行向前 migration；
6. 确认数据库版本等于嵌入最新版本并释放连接；
7. 打开 GORM 运行时连接并启动 HTTP 服务。

等待锁的实例必须在获得锁后重新读取版本。migration 失败会保留 dirty 状态并阻止 System 启动，不允许自动回退、跳过版本或运行时兜底。

当前版本由 `system/backend/internal/migration/sql` 和 `internal/migration/catalog_test.go` 共同约束；本文不固定抄写版本号。

## 十、迁移演进

- 只增加新的 `NNNNNN_name.up.sql`，不修改已发布 migration；
- Permission/Role Catalog 变化由聚合器生成确定性输入，再进入新的向前 migration；
- 破坏性模型切换不保留旧字段、双写、双读或兼容 query；
- 需要保留的外部数据必须另行批准离线导入方案，不进入 System Runtime；
- migration 内不得访问 Redis、HTTP、外部 IdP、密钥服务或其他模块数据库。

## 十一、验证

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
