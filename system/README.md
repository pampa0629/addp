# System 系统核心模块

> 全域数据平台的账号管理、认证和基础配置服务

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含架构设计、开发指南、API 详解和安全机制

## 🎯 核心功能

- **统一 IAM**: Principal、User、Local Account、Tenant Membership、Role / Permission 和平台三员分立
- **账号认证**: 密码 + TOTP、opaque Access Token、Refresh Token Family 与 AuthContext
- **OAuth 2.0**: 受控 Fosite 唯一主路径，支持 Authorization Code + PKCE、RFC 8252 loopback 和 Device Flow
- **权限治理**: 平台/租户上下文、Role Assignment、Permission Manifest 与 owner 资源策略
- **引擎管理**: 数据库/存储引擎连接配置（8种引擎，AES-256-GCM 加密）
- **审计日志**: 自动记录操作日志，支持租户隔离

## 🚀 快速开始

### 方式 1: 开发模式（推荐）

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 System 模块
bash scripts/dev/start.sh -system
```

- 后端: http://localhost:8180
- 前端: http://localhost:5173

### 方式 2: Docker 部署

```bash
cd system
docker-compose up -d
```

### IAM 首次初始化

System 不创建默认全权管理员、默认租户或共享弱密码账号。首次平台系统管理员、安全管理员和审计管理员只能通过一次性离线 Bootstrap 建立；平台三员相互独立、角色互斥。

租户内权限由 Tenant Membership、Role Assignment 和 owner 模块资源授权共同决定，不使用旧的三级 `user_type` 分支。

新 Tenant 必须由平台系统管理员指定一个已由平台安全管理员创建的有效普通 User 作为首位 `tenant.administrator`。Tenant、首个 Membership、首个 Role Assignment 和审计在一个事务中完成；Tenant 不保存独立管理员密码。切换前已存在且没有任何 Membership/Assignment 的 Tenant 只能执行一次正式 Initialization，不能通过 SQL 或第二套授权接口补角色。

普通 User 遗失本地密码时，由平台安全管理员通过用户管理中的受控密码重置入口处理。该操作替换 Password Hash、递增授权版本、撤销既有会话并写入高风险审计；有效平台角色持有人不显示该操作，也不能调用对应 API。平台三员可本人维护凭据，遗失凭据时通过管理员治理申请和复核后，由目标本人登记新密码与 TOTP；无法形成合格的申请／复核组合时使用离线凭据恢复，不与普通 User 重置混用。

## 📡 主要 API 端点

```text
认证与上下文: /api/v1/system/login、/refresh、/logout、/auth/*
OAuth 2.0:       /api/v1/system/oauth/*
平台 IAM:       /api/v1/system/platform/tenants、/platform/users、/platform/administrator_changes
租户 IAM:       /api/v1/system/tenant/memberships、/tenant/invitations、/tenant/roles、/tenant/role_assignments
审计:            /api/v1/system/platform/audit/events、/tenant/audit/events
引擎管理:       /api/v1/system/engines
```

不存在公开 `/register`、旧 `/users` CRUD、物理删除 `/tenants` 或混合语义 `/logs` 路径。

完整 API 文档请查看 [CLAUDE.md#API端点](./CLAUDE.md#api-端点)

## 🔐 安全特性

- **用户密码**: bcrypt 自适应 Hash
- **引擎密码**: AES-256-GCM 加密
- **认证方式**: opaque Access Token + Refresh Token Family

详细安全机制请查看 [CLAUDE.md#安全机制](./CLAUDE.md#安全机制)

## 单会话应急撤销

这是平台级应急治理能力，授权效果仅限一个指定租户的一个普通用户、一个 `addp-web` 第一方 Web Token Family。申请人与执行人为同一普通 User；目标和执行人均不得是有效平台角色持有人。系统管理员与安全管理员分别在 Platform Context 批准，两次批准必须来自不同 User，且均不能是执行人或目标。审计管理员独立读取，不参与批准或执行。不会产生应急 Token、管理员角色或通用数据权限。

首期通过 System 现有 HTTP 管理入口办理，Gateway 继续转发 `/api/v1/system`，不增加第二套入口：

| Context | API | 用途 |
| --- | --- | --- |
| Tenant | `POST /tenant/emergency_session_requests` | 提交 `id`（新 UUID）、`target_principal_id`、`target_family_id`、`reason`、`incident_reference`；租户及执行人从当前身份取得 |
| Platform / Tenant | `GET /{context}/emergency_session_requests`、`GET /{context}/emergency_session_requests/{id}` | 平台审核／审计查询；Tenant 仅查询本人申请及原执行回执 |
| Platform | `POST /platform/emergency_session_requests/{id}/approve`、`reject`、`revoke` | `version`（十进制字符串）、`reason`；批准／拒绝／撤回未消费授权 |
| Tenant | `POST /tenant/emergency_session_requests/{id}/cancel` | 本人放弃，提交 `version`、`reason` |
| Tenant | `POST /tenant/emergency_session_requests/{id}/execute` | 本人单次执行，提交 `version`、新的 `execution_id`（UUID） |

现有 Tenant 自定义 Role 可显式配置 `iam.emergency_session_revocation.create/read/cancel/execute` 入口权限，默认不授予任何租户内置角色；入口权限本身不能代替两次批准。目标 Family 编号来自已核实事件记录，不提供跨用户会话枚举。返回租户、执行人、目标与批准人的可读名称供复核；所有 BIGINT 均使用十进制字符串。批准与执行需要现有 MFA Step-up，不能靠复用旧 MFA 事实绕过。`409 resource_version_conflict` 要求重新读取原请求；超时、断连等结果不明时先 `GET` 原 UUID 核清，同一 `execution_id` 的已执行命令只返回原回执，其他编号不能再次消费。客户端不得自动重放写请求。

审批自数据库申请时刻起10分钟，第二次批准记录的数据库时刻起15分钟内执行，事务提交后生效；等待锁或审计写入跨越期限时回滚。以实时数据库时钟判定，不依赖定时任务或后台清理。撤销仅影响目标 Family 及现有派生授权，不停止外部引擎已经运行的作业，也不改变全局授权版本或同用户其他会话。目标此前已撤销／已到期时回执明确为 `already_revoked`／`already_expired`，不能记为本次撤销成功。

创建、两次批准、拒绝、放弃、撤回和执行写高风险业务审计；执行回执、授权消费、Family 和派生授权撤销与审计处于同一事务，审计失败整体回滚。拒绝／失败的 HTTP 尝试沿用现有请求审计，不记录请求 Body、Bearer 或会话秘密。追加式记录与三员职责分离不等于独立审计存储或数据库运维防篡改；这部分部署边界另验。

最小验证为 `make test-module MODULE=system GO_ONLY=1`、`make test-system-frontend`、`make test-authorization`、Swagger 生成／覆盖检查；真实 PostgreSQL 使用已有 `make test-system-iam-postgres`，自动发现 `TestEmergencySessionRevocationAgainstPostgres`，只在独占 Hosted Service 验证，不连接本地数据库。既有 `platform-administrator-governance` Hosted 套件还通过真实 Gateway 调用验证 MFA 执行人、两员批准、10／15分钟窗口、单 Family 失效、同用户其他会话保留、回执重放和独立审计；不新增 Workflow 或本地部署入口。未执行的 Hosted／浏览器／独立审计部署验收不得计为通过。

## 三员治理验证

标准 PostgreSQL 入口使用隔离测试库，不操作开发库：

```bash
make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package iam --test administrator-governance"
make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package api --test administrator-governance"
make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test administrator-governance"
make test-module MODULE=system
```

前两项覆盖事务治理及真实密码、TOTP、Access Token 与 API 授权链路；浏览器门禁使用隔离页面夹具。它们不代替真实部署的跨进程运行验收。

`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package iam"` 自动运行 `TestRecoveryServiceAgainstPostgres`。该 T2 用例以系统岗两人、安全及审计各一人的四名持有人验证正式恢复事务：强制最后的完成审计写入失败，逐表比较全部身份、账户、MFA、角色、会话、待消费认证事实、恢复凭据及审计的原始状态。移除故障后，两次调用以同一 Secret 和不同的新凭据重试，观察到两者均等待真实 PostgreSQL 恢复表锁后再放行，核验仅一次成功、另一方因 Secret 无效被拒绝；四人最终凭据全部属于同一胜者，授权版本及恢复审计只增加一轮。它不替代真实 CLI 中断的 T4 验收。

同一 T2 还在完成审计的 `AFTER INSERT` 触发器中确认四人的替换审计和恢复完成状态均已写入，再用 `pg_terminate_backend(pg_backend_pid())` 终止执行该事务的测试连接。用例要求收到 PostgreSQL 的 `FATAL/57P01`，十组持久状态完整回滚，原 Secret 仍可验证，并在后续并发重试中成功消费。此项覆盖提交前的连接终止，不覆盖发出 `COMMIT` 后确认丢失时的结果不确定性，也不替代跨进程 CLI 故障验收。

离线恢复唯一协议为 `iam-recovery prepare`、`iam-recovery apply <attempt-id>` 和 `iam-recovery status <attempt-id> <submission-id>`。prepare 显示非敏感恢复编号，apply 在收集前核验原编号与 Secret，并在正式提交前显示新的非敏感提交编号。人员、岗位分配、状态或授权版本在收集期间变化时拒绝写入，不能自动刷新目标。完成状态及包含成功提交编号的审计回执与凭据替换一同提交，仍清空 Secret Hash。

COMMIT 确认丢失时不按错误或退出码推定回滚，不自动重试。使用记录的两个编号运行 status，经原受控运维身份连接可信权威写库；它在最多五秒的 READ COMMITTED 短事务中先等待恢复表锁，再返回 `completed_this_submission`、`completed_other_submission`、`prepared`、`expired` 或 `unknown`。核清不修改业务数据或审计，不把旧 Secret 当历史查询凭据，不开放 HTTP。completed 是历史回执，不证明当前还能登录；prepared 只是允许重新确认目标、重新验证新鲜 TOTP 后，以原编号和 Secret 显式提交。再次提交生成新编号，仍在事务内竞争一次消费；过期不续期。等锁超时、连接错误、查无或回执矛盾保持 unknown。数据库主库历史不明或分叉不在此保证范围内。

`TestRecoveryServiceAgainstPostgres` 同时验证真实 PostgreSQL 返回 `CommandComplete(COMMIT)` 后，在测试专属连接截断确认，Apply 报错而独立连接核清成功；四人凭据与版本落地一次，重试不改变十组状态，其他提交不能冒认胜者，之后身份变化不改写历史回执。此协议测试由现有 `AgainstPostgres$` 自动发现；本地仅运行 `make test-module MODULE=system GO_ONLY=1` 和 `make test-platform-administrator-online-runner`，真实数据库测试由 GitHub Hosted 独占 PG15 执行，不操作本地服务。

三员跨进程验收使用 `platform-administrator-governance`，只允许 GitHub Hosted 人工触发；不在本机运行生命周期。同一套件通过正式治理审批增授第二名系统管理员，按 Principal ID 和账号覆盖四名持有人；先在前三人完成凭据收集、第四人输入密码前终止私有 CLI，核验原凭据／会话／角色／授权版本未变及仅有准备审计，再使用同一 Secret 完整恢复，最后批准撤销新增岗位并核验原三员保留；通过正式 CLI 的真实伪终端和连续两个 TOTP 时间窗口验收离线恢复，核验旧凭据与会话失效、原暂停状态与角色分配保留、Secret 不可复用及完整审计；CLI 新凭据只在场景进程内存中流转。`make test-platform-administrator-online-runner` 仅验证准入、身份断言、伪终端协议、清理、浏览器语法与 CI 登记，不连接开发服务或数据库；新身份夹具的 T2 由既有 System PostgreSQL `--package online-fixture` 门禁自动发现，真实 T4 结果单独报告。

同一 Hosted 套件在原 CLI 退出后以新进程按 Attempt／Submission 编号核清，覆盖中断后 prepared、错编号 unknown、本／其他提交判别、四人历史回执，以及后来恢复暂停和撤任不改变回执、重复核清不写恢复审计或撤销当前会话。status 不输入任何恢复凭据，报告只保存布尔证明。它验证跨进程核清，不能替代 PostgreSQL COMMIT 确认截断的 T2，也不能外推跨进程网络确认丢失故障。

## 🐛 常见问题

### 平台管理员无法登录？

不得直接修改 `system.users` 或 `system.local_accounts` 绕过 IAM 审计。账号恢复和平台高权限身份变更必须走受控恢复及双人审批流程。

### 如何生成加密密钥？

```bash
openssl rand -base64 32
```

更多问题请查看 [CLAUDE.md](./CLAUDE.md)

## Online 验收身份夹具

独占 Hosted 部署通过 `cmd/online-test-fixture` 创建验收租户和普通用户；仅允许 `addp_online`，不得在本地共享 Infra 调用。依赖保护投影的场景传入 `--wait-protection-owners develop,manager`，在导出身份前等待该新租户的 Owner 持久初始化完成，最多 60 秒。夹具只复用 Common 的 `projectionstore.Open` 与 `Initialized` 读取现有存储，不迁移或写入 checkpoint，不重试业务工作流；读取失败或超时直接失败并交给 Hosted 统一退出清理。服务进程就绪不代表稍后新建租户已完成首次同步。

栅格入口已使用该前置；验证命令为根目录 `make test-raster-online-runner`，涵盖等待、租户与 Owner 范围、错误保留和退出清理。真实数据、浏览器与成果清理仍须通过原 `raster-workflow` Hosted suite，不能用夹具回归代替。

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（架构、开发指南、API 详解）
- **[IAM 数据模型与迁移规范](./docs/IAM数据模型与迁移规范.md)** - IAM PostgreSQL 模型、事务和迁移边界
- **[OAuth 与 Fosite 实现说明](./docs/OAuth与Fosite实现说明.md)** - OAuth 协议引擎、Provider 和 Storage 边界
- **[权限与角色发布规范](../docs/spec/addp权限与角色发布规范.md)** - Permission、Role、Manifest 和发布门禁
- **[../docs/spec/addp技术栈规约.md](../docs/spec/addp技术栈规约.md)** - 技术栈和依赖版本
- **[../docs/spec/addp配置介绍.md](../docs/spec/addp配置介绍.md)** - 配置分层与管理能力规范

---

Copyright © 2025 ADDP Team
