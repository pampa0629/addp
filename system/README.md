# System 系统核心模块

> 全域数据平台的账号管理、认证和基础配置服务

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含架构设计、开发指南、API 详解和安全机制

## 🎯 核心功能

- **多租户管理**: 三级权限体系（超级管理员、租户管理员、普通用户）
- **账号认证**: JWT 认证 + bcrypt 密码加密
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

- 后端: http://localhost:8080
- 前端: http://localhost:5173

### 方式 2: Docker 部署

```bash
cd system
docker-compose up -d
```

### 默认账号

- **超级管理员**: `SuperAdmin` / `20251001#SuperAdmin`
- **租户管理员**: `admin` / `123456`

⚠️ 生产环境请立即修改默认密码！

## 🏗️ 三级权限体系

| 用户类型 | 权限范围 |
|---------|---------|
| **超级管理员** | 创建/管理租户、查看所有数据 |
| **租户管理员** | 管理本租户用户、查看本租户数据 |
| **普通用户** | 查看/修改自己信息、使用平台功能 |

所有数据按租户自动隔离（引擎配置、日志、元数据等）。

## 📡 主要 API 端点

```
认证:     POST /api/auth/login
租户管理: GET/POST/PUT/DELETE /api/tenants
用户管理: GET/POST/PUT/DELETE /api/users
引擎管理: GET/POST/PUT/DELETE /api/engines
日志管理: GET /api/logs
```

完整 API 文档请查看 [CLAUDE.md#API端点](./CLAUDE.md#api-端点)

## 🔐 安全特性

- **用户密码**: bcrypt 加密
- **引擎密码**: AES-256-GCM 加密
- **认证方式**: JWT (HS256)

详细安全机制请查看 [CLAUDE.md#安全机制](./CLAUDE.md#安全机制)

## 🐛 常见问题

### 忘记超级管理员密码？

```sql
-- 重置为 admin123
UPDATE system.users
SET password_hash = '$2a$10$UJvKh/XXObz7YPQpQvkDTuBYD8J4R3zoDWrV1v9RRf1f2.FEOaer2'
WHERE username = 'SuperAdmin';
```

### 如何生成加密密钥？

```bash
openssl rand -base64 32
```

更多问题请查看 [CLAUDE.md](./CLAUDE.md)

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（架构、开发指南、API 详解）
- **[../docs/addp技术栈规约.md](../docs/addp技术栈规约.md)** - 技术栈和依赖版本
- **[../docs/addp配置介绍.md](../docs/addp配置介绍.md)** - 配置中心说明

---

Copyright © 2025 ADDP Team
