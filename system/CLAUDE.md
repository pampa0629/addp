# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> IAM 概念与平台契约以 `docs/concepts/addp账号与权限体系图.md`、`docs/spec/addp授权上下文规范.md`、`docs/spec/addp权限与角色发布规范.md` 和 `docs/spec/addp OAuth授权规范.md` 为准；System 数据与协议实现以 `system/docs/IAM数据模型与迁移规范.md` 和 `system/docs/OAuth与Fosite实现说明.md` 为准。当前实现已切换为 Principal、Tenant Membership、Role/Permission、Token Family 和 `addp.auth_context/v1`，不得恢复旧账号分级或平行认证路径。

## 项目概述

**全域数据平台 (All Domain Data Platform)** 是企业级数据平台的核心能力模块，提供基础系统功能：
- 统一 IAM（全局 User、Tenant Membership、组织、角色、权限和平台三员分立）
- 日志管理（审计日志存储和查询、统计分析、导出）
- 引擎管理（通用引擎连接、扩展运行时注册与能力展示，含 Schema/表枚举）
- 应用管理（外部应用注册、API Key 管理）
- 资源回收管理（跨模块评估和执行资源回收）
- 模块注册与发现（供 Gateway 动态路由）
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
- 平台最高管理职责拆分为系统管理员、安全管理员和审计管理员；三员 Role Assignment 两两互斥，职责与写权限分离，允许共享必要的只读监督 Permission；
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

## 常用命令

> **注意**: 开发状态下，不得通过 `go run` 直接运行，改用重启脚本验证修改结果。

```bash
# 重启 system 服务（修改后端代码后）
./scripts/dev/restart.sh -system

# 重启所有服务（修改 common 代码后）
./scripts/dev/restart.sh -all

# 前端仅修改时无需重启后端，热更新自动生效
```

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
│   │   ├── application_handler.go     # 应用与 API Key 管理
│   │   ├── cleanup_handler.go         # 资源回收
│   │   ├── module_registry_handler.go # 模块注册与发现
│   │   ├── task_provider_handler.go   # TaskProvider 读取投影
│   │   ├── registry_handler.go        # 引擎能力注册与发现
│   │   └── internal_handler.go        # API Key 验证（内部 API）
│   ├── config/         # 配置管理
│   ├── middleware/     # 中间件（认证、日志等）
│   ├── models/         # 数据模型和请求/响应结构
│   │   ├── user.go
│   │   ├── tenant.go
│   │   ├── log.go
│   │   ├── engine.go
│   │   ├── application.go     # 应用 + APIKey 模型
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
│   ├── applications.js   # 应用管理 API
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
│   ├── Applications.vue   # 应用与 API Key 管理
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

### 架构说明

- [数据库架构](docs/数据库架构.md) - 表关系、数据流向、设计决策

### 数据库表概览

| 表名 | Schema | 说明 |
|------|--------|------|
| principals / users / local_accounts | system | 授权主体、自然人资料与本地登录凭据 |
| tenants | system | 租户表，多租户隔离 |
| audit_logs | system | 审计日志表 |
| engines | system | 引擎配置表，含加密连接信息 |
| applications | system | 外部应用表 |
| api_keys | system | 应用 API Key 表（存储 SHA256 hash） |
| oauth_authorization_requests | system | 浏览器授权前的短期已校验请求、取消凭据 Hash 和一次性状态 |
| iam_recovery_attempts | system | 三员整体凭据恢复尝试，仅保存一次性 Secret Hash 与终态事实 |
| refresh_token_families | system | 浏览器和 OAuth Refresh Token Family |
| refresh_tokens | system | 轮换 Refresh Token Hash |
| access_tokens | system | 短期 User Access Token Hash |
| delegated_access_tokens | system | Agent Tool 短期受委托访问令牌 Hash 与审计绑定 |
| notebook_session_authorizations | system | Notebook Session 绑定的用户派生短期只读 Catalog 授权事实 |
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
- `connection_info` 为 JSONB 类型，灵活存储不同类型的连接配置
- 敏感字段 (password, access_key 等) 使用 **AES-256-GCM** 加密存储

**system.applications 表**:
- 外部应用注册信息，用于管理第三方应用的 API Key
- 字段: `id`, `name`, `description`, `tenant_id`, `allowed_services`, `rate_limit_per_minute`, `status`
- 软删除支持（`deleted_at`）

**system.api_keys 表**:
- API Key 存储（仅存 SHA256 hash，明文仅在创建时返回一次）
- 字段: `id`, `application_id`, `key_prefix`, `key_hash`, `name`, `last_used_at`, `expires_at`, `status`
- Key 格式：`addp_live_` 前缀 + 随机字符串

**system.module_definitions / system.module_runtime_instances 表**:
- `module_definitions` 按稳定 `module_name` 保存持久定义和管理员 `enabled` 状态，进程离线不删除定义
- `module_definitions.version` 是聚合根乐观并发版本；心跳不得递增它，幂等重复注册保持版本不变，只有 owner 模块级声明实际变化时才原子递增且不得覆盖管理员 `enabled`
- `module_runtime_instances` 按 `(module_definition_id, instance_id)` 保存进程角色、端点、元数据、心跳和租约
- 心跳只续租实例；只有 `enabled + backend + up + lease valid` 的实例可供 Gateway 路由
- `configuration_management` 只保存版本化配置管理入口声明（owner、scope、前端路由和读写 Permission），不保存模块配置键、配置值或 Secret

**TaskProvider 模块角色**:
- Provider 声明保存在 `system.module_definitions.task_provider`，不建立独立注册实体或独立启用状态
- Provider ID 复用模块定义 ID，重复相同声明保持模块版本不变；声明变化递增模块定义版本
- 有效端点池只在读取时从当前 Backend 租约解析，不写入模块定义；模块离线时声明保留但 `available=false`，System 不固定选择单个 Backend

### 日志中间件

`LoggerMiddleware` 自动记录所有非 GET 请求的审计日志，包括：
- 用户身份（如果已认证）
- 请求方法和路径
- 客户端 IP 地址
- 请求时间

### 安全机制

**登录限流**: 15 分钟内最多 5 次尝试（基于 Redis 的 Rate Limit 中间件）

**CORS 白名单**: 仅允许 `cfg.AllowedOrigins` 中的 origin，拒绝其他跨域请求

## 开发注意事项

1. **添加新的 API 端点**:
   - 在 `internal/models/` 定义请求/响应结构
   - 在 `internal/repository/` 添加数据访问方法
   - 在`internal/service/` 实现业务逻辑
   - 在 `internal/api/` 创建 HTTP 处理器
   - 在 `internal/api/router.go` 注册路由（注意路由前缀为 `/api/v1/system/`）

2. **数据库迁移**:
   - IAM 表以 `system/docs/IAM数据模型与迁移规范.md` 为准，必须使用显式版本化 SQL，不得加入 `AutoMigrate`。
   - System 统一 migration runner 同时管理 IAM 表和 IAM 约束依赖的基础资源表；`system.engines` 不得再进入 `AutoMigrate`。
   - 迁移 runner 成功后才允许执行剩余非基础业务表初始化，不能在启动过程中用表存在性或默认数据兜底。

3. **前端添加新页面**:
   - 在 `src/views/` 创建 Vue 组件
   - 在 `src/api/` 添加 API 调用函数（注意 URL 前缀为 `/api/v1/system/`）
   - 在 `src/router/` 注册路由

4. **端口配置**:
   - 后端默认: 8180
   - 前端开发: 5173
   - 前端生产（Nginx）: 8090

## 安全机制

### 密码安全

1. **用户密码 Hash** (`system.local_accounts.password_hash`)
   - 算法: **bcrypt** (cost factor 10)
   - 不可逆哈希,自动加盐
   - 验证: `CheckPassword(plaintext, hash)`

2. **引擎连接密码加密** (system.engines.connection_info)
   - 算法: **AES-256-GCM** (对称加密 + 认证)
   - 密钥管理:
     - 开发环境: 默认32字节密钥
     - 生产环境: 环境变量 `ENCRYPTION_KEY` (Base64编码)
   - 加密字段: 由当前引擎插件的 `SensitiveFields()` 唯一声明
   - 自动加密: 创建/更新引擎时自动加密敏感字段
   - 自动解密: 查询引擎时自动解密返回

3. **API Key 安全** (system.api_keys)
   - 存储：仅存 SHA256 hash，明文仅在创建时返回一次
   - 验证：`GET /api/v1/system/runtime/api-keys/validate` 只供持有 `system.api_key.read` 的 Gateway Platform Service Principal 调用

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

### 授权上下文（需认证）
- `GET /api/v1/system/auth/context` - 验证当前访问令牌，回查用户和租户状态，返回权威 AuthContext

`/auth/context` 是 Go/Python 业务模块消费用户身份的唯一接口。`/users/me` 只返回用户资料，不用于 Token 验证。

### 当前用户（需认证）
- `GET /api/v1/system/users/me` - 获取当前用户信息
- `PUT /api/v1/system/users/me/password` - 修改当前用户密码

### Platform 管理（Platform Context + 精确 Permission）
- `/api/v1/system/platform/tenants` - Tenant 查询、创建、更新、暂停、恢复和关闭；
- `/api/v1/system/platform/users` - 全局 User 查询、创建、更新、暂停和重新激活；
- `/api/v1/system/platform/identity_changes` - 平台身份变更申请、复核和监督；
- `/api/v1/system/platform/audit/events` - 平台审计查询、汇总、趋势和导出。
- `/api/v1/system/platform/modules` - 模块定义、运行实例观测和带版本的启用状态管理。

### Tenant IAM 管理（Tenant Context + 精确 Permission）
- `/api/v1/system/tenant/memberships` - 当前 Tenant Membership 查询、有效期和生命周期；
- `/api/v1/system/tenant/invitations` - 当前 Tenant 邀请查询、创建与撤销；
- `/api/v1/system/tenant/audit/events` - 当前 Tenant 审计查询、汇总、趋势和导出。

### 引擎管理（需认证）
- `POST /api/v1/system/engines` - 创建引擎（自动关联当前用户租户）
- `GET /api/v1/system/engines` - 获取当前 Tenant 的完整过滤后引擎数组；System 管理页面在前端分页
- `GET /api/v1/system/engines/:id` - 获取指定引擎
- `PUT /api/v1/system/engines/:id` - 更新引擎（敏感字段自动重新加密）
- `DELETE /api/v1/system/engines/:id` - 删除引擎
- `POST /api/v1/system/engines/:id/test` - 测试已有引擎连接
- `POST /api/v1/system/engines/test-connection` - 创建前测试连接
- `POST /api/v1/system/engines/:id/catalog/children` - 统一列出实时 catalog 子节点，支持数据库、对象存储、文件系统和图数据库等多层目录发现
- `POST /api/v1/system/engines/:id/catalog/facts` - 按结构化 CatalogPath 读取单个叶子的实时结构事实；列表省略的字段等详情从这里按需读取

`GET /engines` 对 User 和 Service Principal 都返回脱敏列表。`GET /engines/:id` 对 User 返回脱敏连接信息；具有 `system.engine.read` 的 Tenant Service Principal 返回同 Tenant 的解密连接信息，跨 Tenant 返回 403。

### Service Runtime（Service Access Token + 精确 Permission）
- `POST /api/v1/system/runtime/modules` - Platform Service Principal 注册自身模块；
- `POST /api/v1/system/runtime/modules/heartbeat` - Platform Service Principal 更新自身心跳；
- `GET /api/v1/system/runtime/modules` - Gateway Platform Service Principal 查询模块注册表；
- `GET /api/v1/system/runtime/modules/:module_name` - Gateway Platform Service Principal 查询模块详情；
- `GET /api/v1/system/runtime/task-providers` - Platform Service Principal 读取模块定义中的 TaskProvider 声明及当前动态可用性；
- `POST /api/v1/system/runtime/engines` - Workflow Runtime Platform Service Principal 注册自身内置 Runtime；
- `GET /api/v1/system/runtime/api-keys/validate` - Gateway Platform Service Principal 验证外部 API Key Hash；
- `GET /api/v1/system/runtime/engine-descriptors` - Tenant Service Principal 列出当前 Tenant 可见的脱敏 Engine Runtime Descriptor；
- `GET /api/v1/system/runtime/engine-descriptors/:id` - Tenant Service Principal 读取当前 Tenant 可见的单个脱敏 Engine Runtime Descriptor；
- `POST /api/v1/system/tenant/audit/events` - Tenant Service Principal 追加当前 Tenant 审计事件。

Module Name 必须与 OAuth Client `addp-<module>` 一致；Principal、Context 和 Tenant 只从 AuthContext 获取。所有服务间调用统一使用上述 Bearer 路由。

Engine Runtime Descriptor 不包含 `connection_info`。只有工作流或脚本 Runtime 可投影非密密的 `protocol/host/port`；数据引擎连接信息必须继续通过 Execution Authorization 或已明确授权的详情路由获取。

### 应用管理（需认证）
- `POST /api/v1/system/applications` - 创建应用
- `GET /api/v1/system/applications` - 获取应用列表
- `GET /api/v1/system/applications/:id` - 获取指定应用
- `PUT /api/v1/system/applications/:id` - 更新应用
- `DELETE /api/v1/system/applications/:id` - 删除应用
- `POST /api/v1/system/applications/:id/keys` - 为应用生成 API Key
- `GET /api/v1/system/applications/:id/keys` - 列出应用的 API Key
- `DELETE /api/v1/system/applications/:id/keys/:key_id` - 撤销 API Key

### 资源回收（仅租户管理员）
- `POST /api/v1/system/admin/cleanup/scan` - 创建资源回收评估任务
- `GET /api/v1/system/admin/cleanup/tasks/:task_id` - 获取资源回收任务状态
- `POST /api/v1/system/admin/cleanup/execute` - 创建资源回收执行任务
- `GET /api/v1/system/admin/cleanup/history` - 获取资源回收任务历史

### 前端公开路由

- IAM 使用 `/iam?tab=...` 恢复当前权限上下文下可用的稳定 Tab；默认 Tab 省略，无权限或无效 Tab 规范化为默认值。
- 引擎详情唯一使用 `/engines/:id`，详情稳定子视图使用 `tab=connection|capabilities`，默认基础信息省略。
- 审计入口使用 IAM 的 `platform-audit` 或 `tenant-audit` Tab，并支持 `module_name`、`entity_type`、`entity_id` 稳定筛选；资源回收不再跳转不存在的 `Logs` route。
- 模块管理唯一使用 `/modules`；页面只对持有 `platform.module.read` 的 Platform User 显示，启停还要求 `platform.module.update`。
