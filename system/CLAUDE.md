# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

**全域数据平台 (All Domain Data Platform)** 是企业级数据平台的核心能力模块，提供基础系统功能：
- 多租户账号管理（超级管理员、租户管理员、普通用户）
- 日志管理（审计日志存储和查询、统计分析、导出）
- 引擎管理（标准库连接、扩展引擎连接等，含 Schema/表枚举）
- 应用管理（外部应用注册、API Key 管理）
- 垃圾数据清理管理（跨模块扫描和执行清理）
- 模块注册与发现（供 Gateway 动态路由）
- 任务提供者注册（供 Orchestrator 查询调用）
- 数据存储在 PostgreSQL 数据库（system schema）

技术栈：
- **后端**: Go + Gin + GORM + PostgreSQL
- **前端**: Vue 3 + Vite + Pinia + Vue Router
- **部署**: Docker + Docker Compose

## 多租户架构

### 用户体系（三级权限）

1. **超级管理员 (SuperAdmin)**
   - 系统唯一，默认账号 `SuperAdmin`，默认密码 `20251001#SuperAdmin`
   - 权限: 创建和管理租户 ✅
   - 限制: 不能直接创建、查看和管理普通用户 ❌
   - 不可删除

2. **租户管理员 (Tenant Admin)**
   - 由超级管理员创建租户时设置
   - 权限: 创建和管理本租户内的普通用户 ✅
   - 限制: 不能查看/修改超级管理员 ❌，不能跨租户访问 ❌

3. **普通用户 (User)**
   - 由租户管理员创建
   - 权限: 查看和修改自己的账号信息 ✅
   - 限制: 不能创建、查看和管理其他任何用户 ❌

### 数据隔离

**租户级隔离**: 所有功能和数据按租户隔离，包括：
- 存储引擎连接配置
- 审计日志记录
- 数据管理（Manager模块）
- 元数据信息（Meta模块）
- 数据传输任务（Transfer模块）

**隔离实现**:
- 每个用户关联到特定租户 (`users.tenant_id`)
- 引擎、日志等数据关联租户ID
- API查询自动过滤：只返回当前用户所属租户的数据

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
│   │   ├── cleanup_handler.go         # 垃圾数据清理
│   │   ├── module_registry_handler.go # 模块注册与发现
│   │   ├── task_provider_handler.go   # 任务提供者注册
│   │   ├── registry_handler.go        # 能力注册（计算引擎能力）
│   │   ├── config_handler.go          # 共享配置（内部 API）
│   │   └── internal_handler.go        # API Key 验证（内部 API）
│   ├── config/         # 配置管理
│   ├── middleware/     # 中间件（认证、日志等）
│   ├── models/         # 数据模型和请求/响应结构
│   │   ├── user.go
│   │   ├── tenant.go
│   │   ├── log.go
│   │   ├── engine.go
│   │   ├── application.go     # 应用 + APIKey 模型
│   │   ├── cleanup.go         # 清理任务模型
│   │   ├── module_registry.go # 模块注册表模型
│   │   └── task_provider.go   # 任务提供者模型
│   ├── repository/     # 数据访问层
│   └── service/        # 业务逻辑层
└── pkg/                # 可对外暴露的工具包
    └── utils/          # 工具函数（JWT、密码加密等）
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
│   ├── cleanup.js        # 清理管理 API
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
│   ├── useUserManagement.js       # 用户管理业务逻辑
│   └── useEngineManagement.js     # 引擎管理业务逻辑
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
│   ├── CleanupManager.vue # 垃圾数据清理管理
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
| users | system | 用户表，含认证和权限管理 |
| tenants | system | 租户表，多租户隔离 |
| audit_logs | system | 审计日志表 |
| engines | system | 引擎配置表，含加密连接信息 |
| applications | system | 外部应用表 |
| api_keys | system | 应用 API Key 表（存储 SHA256 hash） |
| module_registry | (public) | 模块注册表，供 Gateway 动态路由 |
| task_providers | (public) | 任务提供者表，供 Orchestrator 调用 |

### 单表文档

详细的表结构和 API 说明文档:

- [users表](docs/tables/users表.md) - 用户表,认证和权限管理
- [tenants表](docs/tables/tenants表.md) - 租户表,多租户隔离
- [audit_logs表](docs/tables/audit_logs表.md) - 审计日志表,操作审计和追溯
- [engines表](docs/tables/engines表.md) - 引擎配置表,引擎连接管理

**重要**:修改表结构或 API 时,必须同步更新对应的单表文档。

## 核心功能实现

### 认证流程

1. 用户通过 `POST /api/system/login` 登录，提交用户名和密码
2. 后端验证凭证，生成 JWT Token（使用 HS256 算法）
3. 前端存储 Token 到 localStorage
4. 后续请求通过 `Authorization: Bearer <token>` 头部携带 Token
5. 后端中间件 `AuthMiddleware` 验证 Token 并提取用户信息
6. Token 过期后可通过 `POST /api/system/refresh` 刷新

### 数据库设计

**system.users 表**:
- 用户基本信息、密码 Hash、用户类型、租户ID
- 字段: `id`, `username`, `email`, `password_hash`, `user_type`, `tenant_id`, `is_active`
- 密码使用 **bcrypt** 加密存储 (不可逆)
- 用户类型: `super_admin` / `tenant_admin` / `user`

**system.tenants 表**:
- 租户信息
- 字段: `id`, `name`, `description`, `is_active`, `created_at`

**system.audit_logs 表**:
- 记录所有非 GET 请求的操作日志
- 字段: `id`, `user_id`, `tenant_id`, `action`, `method`, `path`, `ip_address`, `created_at`
- 自动关联租户，支持按租户过滤日志

**system.engines 表**:
- 存储各类引擎连接信息 (数据库、对象存储等)
- 字段: `id`, `name`, `engine_type`, `connection_info`, `tenant_id`, `created_by`, `is_active`
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

**module_registry 表**（public schema）:
- 模块注册表，供 Gateway 动态路由查询
- 字段: `id`, `module_name`, `module_url`, `route_prefix`, `health_check_url`, `status`, `last_heartbeat`, `metadata`

**task_providers 表**（public schema）:
- 任务提供者注册，供 Orchestrator 查询和调用
- 字段: `id`, `module_name`, `display_name`, `base_url`, 各端点 URL, `capabilities`, `is_enabled`

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
   - 在 `internal/api/router.go` 注册路由（注意路由前缀为 `/api/system/`）

2. **数据库迁移**:
   - 修改 `internal/models/` 中的模型结构
   - 在 `repository/database.go` 的 `AutoMigrate` 中添加新模型
   - 重启应用自动执行迁移

3. **前端添加新页面**:
   - 在 `src/views/` 创建 Vue 组件
   - 在 `src/api/` 添加 API 调用函数（注意 URL 前缀为 `/api/system/`）
   - 在 `src/router/` 注册路由

4. **端口配置**:
   - 后端默认: 8180
   - 前端开发: 5173
   - 前端生产（Nginx）: 8090

## 安全机制

### 密码安全

1. **用户密码加密** (system.users.password_hash)
   - 算法: **bcrypt** (cost factor 10)
   - 不可逆哈希,自动加盐
   - 验证: `CheckPassword(plaintext, hash)`

2. **引擎连接密码加密** (system.engines.connection_info)
   - 算法: **AES-256-GCM** (对称加密 + 认证)
   - 密钥管理:
     - 开发环境: 默认32字节密钥
     - 生产环境: 环境变量 `ENCRYPTION_KEY` (Base64编码)
   - 加密字段: `password`, `access_key`, `secret_key`, `token`, `api_key`
   - 自动加密: 创建/更新引擎时自动加密敏感字段
   - 自动解密: 查询引擎时自动解密返回

3. **API Key 安全** (system.api_keys)
   - 存储：仅存 SHA256 hash，明文仅在创建时返回一次
   - 验证：`GET /api/internal/api-keys/validate` 供 Gateway 调用

### 访问控制

**权限矩阵**:

| 操作 | 超级管理员 | 租户管理员 | 普通用户 |
|------|-----------|-----------|---------|
| 创建租户 | ✅ | ❌ | ❌ |
| 查看租户列表 | ✅ | ❌ | ❌ |
| 创建用户 | ❌ | ✅ (本租户) | ❌ |
| 查看用户列表 | ❌ | ✅ (本租户) | ❌ |
| 查看自己信息 | ✅ | ✅ | ✅ |
| 修改自己密码 | ✅ | ✅ | ✅ |
| 查看引擎列表 | ✅ (所有) | ✅ (本租户) | ✅ (本租户) |
| 查看日志 | ✅ (所有) | ✅ (本租户) | ✅ (本租户) |
| 管理应用/API Key | ✅ | ✅ (本租户) | ❌ |
| 垃圾数据清理 | ❌ | ✅ (本租户) | ❌ |

## API 端点

> 所有对外 API 均以 `/api/system` 为前缀。内部服务间 API 以 `/api/internal` 为前缀（需 `X-Internal-API-Key` 认证）。

### 认证（无需认证）
- `POST /api/system/login` - 用户登录
- `POST /api/system/register` - 用户注册（仅限初始化）
- `POST /api/system/refresh` - Token 刷新

### 用户管理（需认证）
- `GET /api/system/users/me` - 获取当前用户信息
- `POST /api/system/users` - 创建用户（租户管理员创建本租户用户）
- `GET /api/system/users` - 获取用户列表（租户管理员仅看本租户）
- `GET /api/system/users/:id` - 获取指定用户
- `PUT /api/system/users/:id` - 更新用户
- `PUT /api/system/users/:id/change-password` - 修改密码
- `DELETE /api/system/users/:id` - 删除用户（SuperAdmin 不可删除）

### 租户管理（仅超级管理员）
- `POST /api/system/tenants` - 创建租户（同时创建租户管理员）
- `GET /api/system/tenants` - 获取租户列表
- `GET /api/system/tenants/:id` - 获取指定租户
- `PUT /api/system/tenants/:id` - 更新租户
- `DELETE /api/system/tenants/:id` - 删除租户

### 日志管理（需认证）
- `GET /api/system/logs` - 获取日志列表（自动过滤本租户，支持 user_id 过滤）
- `GET /api/system/logs/stats` - 获取日志统计数据
- `GET /api/system/logs/trends` - 获取日志时间趋势
- `GET /api/system/logs/export` - 导出日志
- `GET /api/system/logs/:id` - 获取指定日志

### 引擎管理（需认证）
- `POST /api/system/engines` - 创建引擎（自动关联当前用户租户）
- `GET /api/system/engines` - 获取引擎列表（自动过滤本租户，支持 engine_type 过滤）
- `GET /api/system/engines/:id` - 获取指定引擎
- `PUT /api/system/engines/:id` - 更新引擎（敏感字段自动重新加密）
- `DELETE /api/system/engines/:id` - 删除引擎
- `POST /api/system/engines/:id/test` - 测试已有引擎连接
- `POST /api/system/engines/test-connection` - 创建前测试连接
- `GET /api/system/engines/:id/schemas` - 列出数据库 schemas
- `GET /api/system/engines/:id/tables` - 列出数据库表

### 应用管理（需认证）
- `POST /api/system/applications` - 创建应用
- `GET /api/system/applications` - 获取应用列表
- `GET /api/system/applications/:id` - 获取指定应用
- `PUT /api/system/applications/:id` - 更新应用
- `DELETE /api/system/applications/:id` - 删除应用
- `POST /api/system/applications/:id/keys` - 为应用生成 API Key
- `GET /api/system/applications/:id/keys` - 列出应用的 API Key
- `DELETE /api/system/applications/:id/keys/:key_id` - 撤销 API Key

### 垃圾数据清理（仅租户管理员）
- `POST /api/system/admin/cleanup/scan` - 创建扫描任务
- `GET /api/system/admin/cleanup/tasks/:task_id` - 获取任务状态
- `POST /api/system/admin/cleanup/execute` - 创建执行清理任务
- `GET /api/system/admin/cleanup/history` - 获取任务历史

### 内部 API（`/api/internal`，X-Internal-API-Key 认证）

**配置**:
- `GET /api/internal/config` - 获取共享配置

**引擎**:
- `GET /api/internal/engines` - 列出所有引擎（内部使用）
- `GET /api/internal/engines/:id` - 获取引擎详情（内部使用）
- `POST /api/internal/engines` - 创建引擎（内部使用）
- `POST /api/internal/engines/register` - 引擎自注册
- `PUT /api/internal/engines/:id/connection-status` - 更新连接状态
- `POST /api/internal/engines/:id/check-connection` - 触发异步连接检测

**能力注册**:
- `POST /api/internal/registry/capabilities` - 注册计算能力
- `GET /api/internal/registry/capabilities` - 列出所有能力
- `GET /api/internal/registry/capabilities/:identifier` - 按标识符查询能力
- `GET /api/internal/registry/compute-engines` - 列出计算引擎

**任务提供者**:
- `POST /api/internal/task-providers/register` - 模块注册为任务提供者
- `GET /api/internal/task-providers` - 列出所有任务提供者
- `GET /api/internal/task-providers/:module_name` - 获取指定模块信息

**审计日志**:
- `POST /api/internal/audit-logs` - 其他模块写入审计日志

**API Key 验证**:
- `GET /api/internal/api-keys/validate` - 验证 API Key（Gateway 调用）
- `GET /api/internal/api-keys/bulk` - 批量获取 API Key 信息

**模块注册与发现**:
- `POST /api/internal/modules/register` - 模块注册
- `POST /api/internal/modules/heartbeat` - 心跳更新
- `GET /api/internal/modules` - 列出所有已注册模块
- `GET /api/internal/modules/:name` - 获取指定模块信息
- `DELETE /api/internal/modules/:name` - 注销模块
