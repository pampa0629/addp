# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

**全域数据平台 (All Domain Data Platform)** 是企业级数据平台的核心能力模块，提供基础系统功能：
- 多租户账号管理（超级管理员、租户管理员、普通用户）
- 日志管理（审计日志存储和查询）
- 引擎管理（标准库连接、扩展引擎连接等）
- API 文档中心（ADDP 平台所有模块的 REST API 接口文档）
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

### 后端开发

```bash
# 进入后端目录
cd backend

# 下载依赖
go mod download

# 开发模式运行
go run cmd/server/main.go

# 编译（输出到统一 dist）
GOOS=$(go env GOOS) GOARCH=$(go env GOARCH) \
  go build -ldflags "-s -w" \
  -o ../../dist/release/backend/system/${GOOS}-${GOARCH}/system cmd/server/main.go

# 运行测试
go test ./...
```

### 前端开发

```bash
# 进入前端目录
cd frontend

# 安装依赖
npm install

# 开发模式运行（默认端口 5173）
npm run dev

# 构建生产版本
npm run build

# 预览生产版本
npm run preview
```

### Docker 部署

```bash
# 构建镜像
make docker-build
# 或
docker-compose build

# 启动服务
make docker-up
# 或
docker-compose up -d

# 停止服务
make docker-down
# 或
docker-compose down

# 查看日志
docker-compose logs -f
```

## 项目结构

### 后端架构（Go）

```
backend/
├── cmd/server/          # 应用入口
│   └── main.go
├── internal/            # 内部包（不对外暴露）
│   ├── api/            # HTTP 处理层
│   │   ├── router.go   # 路由配置
│   │   └── *_handler.go # 各模块的 HTTP 处理器
│   ├── config/         # 配置管理
│   ├── middleware/     # 中间件（认证、日志等）
│   ├── models/         # 数据模型和请求/响应结构
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

**代码质量改进（v0.0.20）**:

System 模块已完成大规模重构（2026-01），显著提升代码质量、可维护性和安全性：

1. **消除代码重复**
   - 提取 `common/api/handler_helpers.go` - 消除 Handler 层 10+ 处 ID 解析、15+ 处用户获取重复
   - 提取 `common/service/auth_service.go` - 消除 Service 层 20+ 处权限检查重复
   - **Handler 层减少**: 912 → 789 行 (-13.5%)

2. **Repository 接口化**
   - 创建 `internal/repository/interfaces.go`
   - 支持依赖注入和 mock 测试
   - Repository 返回值标准化（添加 total 分页信息）

3. **安全增强**
   - ✅ CORS 白名单配置（移除不安全的 "*"）
   - ✅ Rate Limiting 中间件（登录接口 15 分钟内最多 5 次尝试）
   - ✅ 加密服务 (`common/service/crypto_service.go`) 统一处理敏感字段

4. **测试覆盖**
   - AuthService 单元测试：22 个测试用例全部通过
   - API Helpers 单元测试：30+ 个测试用例全部通过
   - common/api 覆盖率：27.1%
   - common/service 覆盖率：30.3%

参考：详细改进计划见 `~/.claude/plans/iridescent-yawning-newell.md`

### 前端架构（Vue 3）

```
frontend/src/
├── api/              # API 请求封装
│   ├── client.js    # Axios 实例配置（拦截器、认证）
│   └── *.js         # 各模块的 API 调用
├── components/       # 可复用组件
│   ├── users/       # 用户管理子组件（新）
│   │   ├── UserList.vue
│   │   ├── UserFormDialog.vue
│   │   └── PasswordDialog.vue
│   └── engines/     # 引擎管理子组件（新）
│       ├── EngineList.vue
│       └── EngineFilterBar.vue
├── composables/     # Vue Composables（新）
│   ├── usePagination.js           # 分页逻辑复用
│   ├── useFormDialog.js           # 对话框状态管理
│   ├── useUserManagement.js       # 用户管理业务逻辑
│   └── useEngineManagement.js     # 引擎管理业务逻辑
├── store/           # Pinia 状态管理
│   └── auth.js      # 认证状态
├── views/           # 页面组件
│   ├── Login.vue    # 登录页
│   ├── Dashboard.vue # 首页
│   ├── Users.vue    # 用户管理（重构：531 → 223 行，-58%）
│   ├── Logs.vue     # 日志管理
│   └── Engines.vue  # 引擎管理（重构：1052 → 131 行，-87.5%）
└── router/          # 路由配置
```

**代码质量改进（v0.0.20）**:

前端代码已完成组件化重构（2026-01），显著提升可维护性：

1. **组件拆分**
   - Users.vue：531 → 223 行（-58%）- 拆分为 UserList, UserFormDialog, PasswordDialog
   - Engines.vue：1052 → 131 行（-87.5%）- 拆分为 EngineList, EngineFilterBar
   - **总减少**: 1583 → 354 行（-77.6%）

2. **Composables 提取**
   - `usePagination.js` - 消除 5+ 处分页重复
   - `useFormDialog.js` - 消除 6+ 处对话框状态重复
   - `useUserManagement.js` - 用户 CRUD 业务逻辑（3 个 composables）
   - `useEngineManagement.js` - 引擎 CRUD 业务逻辑（2 个 composables）

3. **代码复用**
   - 移除重复的 formatDate 实现（5+ 处）
   - 改用 `@common-ui/utils/formatters`

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

### 单表文档

详细的表结构和 API 说明文档:

- [users表](docs/tables/users表.md) - 用户表,认证和权限管理
- [tenants表](docs/tables/tenants表.md) - 租户表,多租户隔离
- [audit_logs表](docs/tables/audit_logs表.md) - 审计日志表,操作审计和追溯
- [engines表](docs/tables/engines表.md) - 引擎配置表,引擎连接管理

**重要**:修改表结构或 API 时,必须同步更新对应的单表文档。

## 核心功能实现

### 认证流程

1. 用户通过 `/api/auth/login` 登录，提交用户名和密码
2. 后端验证凭证，生成 JWT Token（使用 HS256 算法）
3. 前端存储 Token 到 localStorage
4. 后续请求通过 `Authorization: Bearer <token>` 头部携带 Token
5. 后端中间件 `AuthMiddleware` 验证 Token 并提取用户信息

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

### 日志中间件

`LoggerMiddleware` 自动记录所有非 GET 请求的审计日志，包括：
- 用户身份（如果已认证）
- 请求方法和路径
- 客户端 IP 地址
- 请求时间

## 开发注意事项

1. **添加新的 API 端点**:
   - 在 `internal/models/` 定义请求/响应结构
   - 在 `internal/repository/` 添加数据访问方法
   - 在 `internal/service/` 实现业务逻辑
   - 在 `internal/api/` 创建 HTTP 处理器
   - 在 `internal/api/router.go` 注册路由

2. **数据库迁移**:
   - 修改 `internal/models/` 中的模型结构
   - 在 `repository/database.go` 的 `AutoMigrate` 中添加新模型
   - 重启应用自动执行迁移

3. **前端添加新页面**:
   - 在 `src/views/` 创建 Vue 组件
   - 在 `src/api/` 添加 API 调用函数
   - 在 `src/router/index.js` 注册路由
   - 根据需要在各页面的侧边栏添加导航链接

4. **环境配置**:
   - 复制 `backend/.env.example` 为 `.env`
   - 修改 JWT_SECRET 为随机字符串（生产环境必须修改）

5. **端口配置**:
   - 后端默认: 8180
   - 前端开发: 5173
   - 前端生产（Nginx）: 80

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

## API 端点

### 认证
- `POST /api/auth/login` - 用户登录
- `POST /api/auth/register` - 用户注册 (仅限首次初始化)

### 租户管理（仅超级管理员）
- `POST /api/tenants` - 创建租户（同时创建租户管理员）
- `GET /api/tenants` - 获取租户列表
- `GET /api/tenants/:id` - 获取指定租户
- `PUT /api/tenants/:id` - 更新租户
- `DELETE /api/tenants/:id` - 删除租户

### 用户管理（需认证）
- `GET /api/users/me` - 获取当前用户信息
- `POST /api/users` - 创建用户（租户管理员创建本租户用户）
- `GET /api/users` - 获取用户列表（自动过滤：租户管理员仅看本租户）
- `GET /api/users/:id` - 获取指定用户（需权限）
- `PUT /api/users/:id` - 更新用户（需权限）
- `DELETE /api/users/:id` - 删除用户（需权限，SuperAdmin不可删除）

### 日志管理（需认证）
- `GET /api/logs` - 获取日志列表（自动过滤：仅返回本租户日志，支持 user_id 过滤）
- `GET /api/logs/:id` - 获取指定日志

### 引擎管理（需认证）
- `POST /api/engines` - 创建引擎（自动关联当前用户租户）
- `GET /api/engines` - 获取引擎列表（自动过滤：仅返回本租户引擎，支持 engine_type 过滤）
- `GET /api/engines/:id` - 获取指定引擎
- `PUT /api/engines/:id` - 更新引擎（敏感字段自动重新加密）
- `DELETE /api/engines/:id` - 删除引擎
- `POST /api/engines/:id/test` - 测试引擎连接
- `POST /api/engines/test-connection` - 创建前测试连接
