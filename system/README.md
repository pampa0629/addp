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

普通 User 遗失本地密码时，由平台安全管理员通过用户管理中的受控密码重置入口处理。该操作替换 Password Hash、递增授权版本、撤销既有会话并写入高风险审计；有效平台角色持有人不显示该操作，也不能调用对应 API。平台三员继续使用本人改密或离线三员灾难恢复，不与普通 User 重置混用。

## 📡 主要 API 端点

```text
认证与上下文: /api/v1/system/login、/refresh、/logout、/auth/*
OAuth 2.0:       /api/v1/system/oauth/*
平台 IAM:       /api/v1/system/platform/tenants、/platform/users、/platform/identity_changes
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
- **[IAM 数据模型与迁移规范](./docs/IAM数据模型与迁移规范.md)** - IAM PostgreSQL 模型、事务和迁移边界
- **[OAuth 与 Fosite 实现说明](./docs/OAuth与Fosite实现说明.md)** - OAuth 协议引擎、Provider 和 Storage 边界
- **[权限与角色发布规范](../docs/spec/addp权限与角色发布规范.md)** - Permission、Role、Manifest 和发布门禁
- **[../docs/spec/addp技术栈规约.md](../docs/spec/addp技术栈规约.md)** - 技术栈和依赖版本
- **[../docs/spec/addp配置介绍.md](../docs/spec/addp配置介绍.md)** - 配置分层与管理能力规范

---

Copyright © 2025 ADDP Team
