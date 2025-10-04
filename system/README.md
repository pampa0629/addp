# System 系统核心模块

> 全域数据平台的账号管理、认证和基础配置服务

## 🎯 核心功能

- **多租户管理**: 支持超级管理员、租户管理员、普通用户三级权限体系
- **账号认证**: 基于 JWT 的用户登录和权限验证
- **资源管理**: 管理各类数据库、存储引擎等资源连接配置
- **审计日志**: 自动记录所有操作日志，支持按租户隔离查询

## 🚀 快速开始

### 前置要求

- Go 1.21+
- PostgreSQL 15+
- Node.js 18+ (前端开发)

### 运行后端

```bash
cd backend
go mod download
go run cmd/server/main.go
```

访问: http://localhost:8080

### 运行前端

```bash
cd frontend
npm install
npm run dev
```

访问: http://localhost:5173

### Docker 部署

```bash
cd system
docker-compose up -d
```

## 👥 默认账号

首次启动时会自动创建超级管理员账号:

- **用户名**: `SuperAdmin`
- **密码**: `20251001#SuperAdmin`

⚠️ **生产环境请立即修改默认密码!**

## 🏗️ 用户体系

### 三级权限模型

| 用户类型 | 创建方式 | 权限范围 |
|---------|---------|---------|
| **超级管理员** | 系统初始化 | 创建/管理租户 ✅<br>查看所有数据 ✅<br>管理普通用户 ❌ |
| **租户管理员** | 超级管理员创建租户时设置 | 管理本租户用户 ✅<br>查看本租户数据 ✅<br>跨租户访问 ❌ |
| **普通用户** | 租户管理员创建 | 查看/修改自己信息 ✅<br>查看本租户数据 ✅<br>管理其他用户 ❌ |

### 数据隔离

所有功能和数据按租户隔离:
- 资源配置
- 审计日志
- 数据管理 (Manager模块)
- 元数据信息 (Meta模块)
- 传输任务 (Transfer模块)

## 🔐 安全机制

### 密码加密

- **用户密码**: bcrypt 算法加密存储 (cost factor 10)
- **资源连接密码**: AES-256-GCM 对称加密存储
- 加密密钥通过环境变量 `ENCRYPTION_KEY` 配置

### 认证流程

1. 用户登录 → 验证用户名密码
2. 生成 JWT Token (HS256算法)
3. Token 存储在前端 localStorage
4. 后续请求携带 `Authorization: Bearer <token>` 头部
5. 后端中间件验证 Token 并注入用户信息

## 📡 主要 API 端点

### 认证
- `POST /api/auth/login` - 用户登录
- `POST /api/auth/register` - 用户注册 (仅首次初始化)

### 租户管理 (仅超级管理员)
- `POST /api/tenants` - 创建租户 (同时创建租户管理员)
- `GET /api/tenants` - 获取租户列表
- `PUT /api/tenants/:id` - 更新租户
- `DELETE /api/tenants/:id` - 删除租户

### 用户管理
- `GET /api/users/me` - 获取当前用户信息
- `POST /api/users` - 创建用户 (租户管理员创建本租户用户)
- `GET /api/users` - 获取用户列表 (自动过滤租户)
- `PUT /api/users/:id` - 更新用户
- `DELETE /api/users/:id` - 删除用户 (SuperAdmin不可删除)

### 资源管理
- `POST /api/resources` - 创建资源 (密码自动加密)
- `GET /api/resources` - 获取资源列表 (自动过滤租户)
- `PUT /api/resources/:id` - 更新资源 (密码重新加密)
- `POST /api/resources/:id/test` - 测试资源连接

### 日志管理
- `GET /api/logs` - 获取审计日志 (自动过滤租户)

## ⚙️ 环境配置

### 后端配置 (.env)

```bash
# 数据库配置
DB_HOST=localhost
DB_PORT=5432
DB_NAME=addp
DB_USER=addp
DB_PASSWORD=addp_password

# JWT 配置
JWT_SECRET=your-secret-key-change-in-production  # 生产环境必须修改!

# 加密密钥 (AES-256,32字节Base64编码)
ENCRYPTION_KEY=your-base64-encoded-32-byte-key   # 可选,未设置使用默认密钥

# 服务端口
PORT=8080
```

### 端口说明

- **后端**: 8080 (开发) / 8080 (Docker)
- **前端**: 5173 (开发) / 8090 (Docker)

## 📊 数据库表结构

- `system.users` - 用户账号 (username, password_hash, user_type, tenant_id)
- `system.tenants` - 租户信息
- `system.audit_logs` - 审计日志
- `system.resources` - 资源连接配置 (connection_info 加密存储)

## 🔗 与其他模块集成

System 模块提供统一认证服务,其他模块通过 JWT 验证用户身份:

```
Client → Gateway:8000 → Manager:8081 ──┐
                      ↓                 ├→ System:8080 (验证JWT)
                   Meta:8082    ────────┤
                      ↓                 │
                Transfer:8083 ──────────┘
```

## 📚 更多文档

- 详细技术文档: [CLAUDE.md](./CLAUDE.md)
- API 详细说明: [CLAUDE.md#API端点](./CLAUDE.md)
- 开发规范: [CLAUDE.md#开发注意事项](./CLAUDE.md)

## 🐛 常见问题

### 1. 忘记 SuperAdmin 密码怎么办?

连接数据库直接重置:
```sql
UPDATE system.users
SET password_hash = '$2a$10$UJvKh/XXObz7YPQpQvkDTuBYD8J4R3zoDWrV1v9RRf1f2.FEOaer2'  -- admin123
WHERE username = 'SuperAdmin';
```

### 2. 如何生成生产环境的加密密钥?

```bash
# 使用 openssl 生成32字节随机密钥并 Base64 编码
openssl rand -base64 32
```

### 3. 多个租户如何隔离数据?

所有查询 API 自动根据当前用户的 `tenant_id` 过滤数据,无需手动处理。

## 📄 License

Copyright © 2025 ADDP Team
