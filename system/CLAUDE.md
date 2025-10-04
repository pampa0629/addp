# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

**全域数据平台 (All Domain Data Platform)** 是企业级数据平台的核心能力模块，提供基础系统功能：
- 多租户账号管理（超级管理员、租户管理员、普通用户）
- 日志管理（审计日志存储和查询）
- 资源管理（数据库连接、计算引擎连接等）
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
- 资源、日志等数据关联租户ID
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

# 编译
go build -o ../bin/server cmd/server/main.go

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

### 前端架构（Vue 3）

```
frontend/src/
├── api/              # API 请求封装
│   ├── client.js    # Axios 实例配置（拦截器、认证）
│   └── *.js         # 各模块的 API 调用
├── components/       # 可复用组件
├── store/           # Pinia 状态管理
│   └── auth.js      # 认证状态
├── views/           # 页面组件
│   ├── Login.vue    # 登录页
│   ├── Dashboard.vue # 首页
│   ├── Users.vue    # 用户管理
│   ├── Logs.vue     # 日志管理
│   └── Resources.vue # 资源管理
└── router/          # 路由配置
```

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

**system.resources 表**:
- 存储各类资源连接信息 (数据库、对象存储等)
- 字段: `id`, `name`, `resource_type`, `connection_info`, `tenant_id`, `created_by`, `is_active`
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
   - 后端默认: 8080
   - 前端开发: 5173
   - 前端生产（Nginx）: 80

## 安全机制

### 密码安全

1. **用户密码加密** (system.users.password_hash)
   - 算法: **bcrypt** (cost factor 10)
   - 不可逆哈希,自动加盐
   - 验证: `CheckPassword(plaintext, hash)`

2. **资源连接密码加密** (system.resources.connection_info)
   - 算法: **AES-256-GCM** (对称加密 + 认证)
   - 密钥管理:
     - 开发环境: 默认32字节密钥
     - 生产环境: 环境变量 `ENCRYPTION_KEY` (Base64编码)
   - 加密字段: `password`, `access_key`, `secret_key`, `token`, `api_key`
   - 自动加密: 创建/更新资源时自动加密敏感字段
   - 自动解密: 查询资源时自动解密返回

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
| 查看资源列表 | ✅ (所有) | ✅ (本租户) | ✅ (本租户) |
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

### 资源管理（需认证）
- `POST /api/resources` - 创建资源（自动关联当前用户租户）
- `GET /api/resources` - 获取资源列表（自动过滤：仅返回本租户资源，支持 resource_type 过滤）
- `GET /api/resources/:id` - 获取指定资源
- `PUT /api/resources/:id` - 更新资源（敏感字段自动重新加密）
- `DELETE /api/resources/:id` - 删除资源
- `POST /api/resources/:id/test` - 测试资源连接
- `POST /api/resources/test-connection` - 创建前测试连接