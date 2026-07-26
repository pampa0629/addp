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

- 后端: http://localhost:8180
- 前端: http://localhost:5173

### 方式 2: Docker 部署

```bash
cd system
docker-compose up -d
```

### IAM 首次初始化

System 不创建默认 SuperAdmin、默认租户或共享弱密码账号。首次平台系统管理员、安全管理员和审计管理员只能通过一次性离线 Bootstrap 建立；平台三员相互独立、角色互斥。

租户内权限由 Tenant Membership、Role Assignment 和 owner 模块资源授权共同决定，不使用旧的三级 `user_type` 分支。

## 📡 主要 API 端点

```
认证:     POST /api/v1/system/login
租户管理: GET/POST/PUT/DELETE /api/v1/system/tenants
用户管理: GET/POST/PUT/DELETE /api/v1/system/users
引擎管理: GET/POST/PUT/DELETE /api/v1/system/engines
日志管理: GET /api/v1/system/logs
```

完整 API 文档请查看 [CLAUDE.md#API端点](./CLAUDE.md#api-端点)

## 🔐 安全特性

- **用户密码**: bcrypt 加密
- **引擎密码**: AES-256-GCM 加密
- **认证方式**: opaque Access Token + Refresh Token Family

详细安全机制请查看 [CLAUDE.md#安全机制](./CLAUDE.md#安全机制)

## 🐛 常见问题

### 平台管理员无法登录？

不得直接修改 `system.users` 或 `system.local_accounts` 绕过 IAM 审计。账号恢复和平台高权限身份变更必须走受控恢复及双人审批流程。

### 如何生成加密密钥？

```bash
openssl rand -base64 32
```

更多问题请查看 [CLAUDE.md](./CLAUDE.md)

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（架构、开发指南、API 详解）
- **[../docs/spec/addp技术栈规约.md](../docs/spec/addp技术栈规约.md)** - 技术栈和依赖版本
- **[../docs/spec/addp配置介绍.md](../docs/spec/addp配置介绍.md)** - 配置中心说明

---

Copyright © 2025 ADDP Team
