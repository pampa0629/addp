# users 表结构和 API 说明

> 本文记录当前表和 API 实现。IAM 目标模型以 `docs/concepts/addp账号与权限体系图.md` 为准；`user_type` 和 User 单一 `tenant_id` 将由 User、Tenant Membership、Role Assignment 等目标模型一次性替换，不得继续扩展旧权限分支。

## 一、表结构概览

`system.users` 表是 ADDP 平台的用户管理核心表,负责存储用户认证信息和基本资料。支持三种用户类型(SuperAdmin、TenantAdmin、User),实现多租户隔离和基于角色的权限控制。

### 核心功能

- **用户认证**:支持用户名/密码登录,使用 bcrypt 加密存储密码
- **多租户隔离**:TenantAdmin 和 User 必须关联租户,SuperAdmin 无租户限制
- **角色权限**:基于 user_type 的三级权限体系
- **用户管理**:支持创建、查询、更新、删除用户
- **密码管理**:用户可修改自己的密码
- **审计日志**:所有用户操作自动记录到 audit_logs 表

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 用户唯一标识 |
| `username` | VARCHAR(255) | NOT NULL, UNIQUE | 用户名(登录用) |
| `email` | VARCHAR(255) | UNIQUE | 邮箱地址 |
| `password_hash` | VARCHAR(255) | NOT NULL | bcrypt 加密的密码(cost=10) |
| `full_name` | VARCHAR(255) | | 用户全名 |
| `is_active` | BOOLEAN | DEFAULT true | 是否激活 |
| `user_type` | VARCHAR(20) | NOT NULL, DEFAULT 'user' | 用户类型:super_admin/tenant_admin/user |
| `tenant_id` | INTEGER | FK → tenants.id, NULLABLE, INDEXED | 租户 ID(SuperAdmin 为 NULL) |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `idx_users_username` | `username` | 唯一索引 | 用户名唯一性约束 |
| `idx_users_email` | `email` | 唯一索引 | 邮箱唯一性约束 |
| `idx_users_tenant` | `tenant_id` | 普通索引 | 租户隔离查询优化 |

### 2.3 外键关系

| 字段 | 引用表 | 说明 |
|------|--------|------|
| `tenant_id` | `system.tenants.id` | 用户所属租户(SuperAdmin 为 NULL) |

---

## 三、用户类型说明

### 3.1 UserType 枚举

| 类型 | 值 | tenant_id | 权限范围 |
|------|---|-----------|---------|
| SuperAdmin | `super_admin` | NULL | 全局权限,管理租户、查看系统级日志 |
| TenantAdmin | `tenant_admin` | 有值 | 租户级权限,管理本租户用户和资源 |
| User | `user` | 有值 | 用户级权限,仅查看自己的信息 |

### 3.2 权限矩阵

| 操作 | SuperAdmin | TenantAdmin | User |
|------|------------|-------------|------|
| 创建租户 | ✅ | ❌ | ❌ |
| 管理租户 | ✅(所有) | ❌ | ❌ |
| 创建用户 | ✅(任意租户) | ✅(本租户) | ❌ |
| 查看用户列表 | ✅(所有) | ✅(本租户) | ❌ |
| 查看用户详情 | ✅(所有) | ✅(本租户) | ✅(自己) |
| 修改用户信息 | ✅(所有) | ✅(本租户) | ✅(自己) |
| 删除用户 | ✅(非 SuperAdmin) | ✅(本租户) | ❌ |
| 修改密码 | ✅(自己) | ✅(自己) | ✅(自己) |
| 查看审计日志 | ✅(所有) | ✅(本租户) | ❌ |

---

## 四、Go 模型定义

### 4.1 User 模型

```go
package models

type UserType string

const (
    UserTypeSuperAdmin  UserType = "super_admin"  // 超级管理员
    UserTypeTenantAdmin UserType = "tenant_admin" // 租户管理员
    UserTypeUser        UserType = "user"         // 普通用户
)

type User struct {
    ID           uint      `gorm:"primaryKey" json:"id"`
    Username     string    `gorm:"not null;unique" json:"username"`
    Email        string    `gorm:"unique" json:"email"`
    PasswordHash string    `gorm:"not null" json:"-"`
    FullName     string    `json:"full_name"`
    IsActive     bool      `gorm:"default:true" json:"is_active"`
    UserType     UserType  `gorm:"type:varchar(20);default:'user';not null" json:"user_type"`
    TenantID     *uint     `gorm:"index" json:"tenant_id"`
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
}
```

### 4.2 请求 DTO

```go
// 创建用户请求
type UserCreateRequest struct {
    Username string   `json:"username" binding:"required"`
    Email    string   `json:"email"`
    Password string   `json:"password" binding:"required,min=6"`
    FullName string   `json:"full_name"`
    UserType UserType `json:"user_type"`
}

// 更新用户请求
type UserUpdateRequest struct {
    Email    *string   `json:"email"`
    FullName *string   `json:"full_name"`
    Password *string   `json:"password"`
    IsActive *bool     `json:"is_active"`
    UserType *UserType `json:"user_type"`
}

// 修改密码请求
type ChangePasswordRequest struct {
    OldPassword string `json:"old_password" binding:"required"`
    NewPassword string `json:"new_password" binding:"required,min=6"`
}
```

---

## 五、API 端点说明

### 5.1 认证 API

#### POST /api/v1/system/login - 用户登录

**请求体**:

```json
{
  "username": "admin",
  "password": "123456"
}
```

**响应**(200 OK):

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer"
}
```

**响应**(401 Unauthorized):

```json
{
  "error": "用户名或密码错误"
}
```

**JWT Payload**:

```json
{
  "user_id": 2,
  "username": "admin",
  "tenant_id": 1,
  "exp": 1735833600,
  "iat": 1735747200
}
```

`user_type`、用户激活状态和租户激活状态由 `GET /api/v1/system/auth/context` 回查当前数据后返回，不从 JWT 字符串推断。

---

#### POST /api/v1/system/register - 用户注册

**权限**:需要配置 `ALLOW_PUBLIC_REGISTRATION=true`(默认关闭)

**请求体**:

```json
{
  "username": "newuser",
  "email": "user@example.com",
  "password": "password123",
  "full_name": "张三"
}
```

**响应**(201 Created):

```json
{
  "id": 5,
  "username": "newuser",
  "email": "user@example.com",
  "full_name": "张三",
  "is_active": true,
  "user_type": "user",
  "tenant_id": 1,
  "created_at": "2026-01-01T10:30:00Z"
}
```

**说明**:
- 注册用户自动关联到默认租户
- 默认 user_type 为 `user`
- 密码使用 bcrypt 加密存储

---

### 5.2 用户管理 API(需要认证)

#### POST /api/v1/system/users - 创建用户

**权限**:TenantAdmin(本租户) 或 SuperAdmin

**请求头**:

```
Authorization: Bearer <jwt_token>
Content-Type: application/json
```

**请求体**:

```json
{
  "username": "newuser",
  "email": "user@example.com",
  "password": "password123",
  "full_name": "李四",
  "user_type": "user"
}
```

**响应**(201 Created):

```json
{
  "id": 6,
  "username": "newuser",
  "email": "user@example.com",
  "full_name": "李四",
  "is_active": true,
  "user_type": "user",
  "tenant_id": 1,
  "created_at": "2026-01-01T11:00:00Z",
  "updated_at": "2026-01-01T11:00:00Z"
}
```

**业务规则**:
- TenantAdmin 创建的用户自动关联到自己的租户
- SuperAdmin 可指定 tenant_id
- 用户名和邮箱必须全局唯一
- 密码最少 6 位

---

#### GET /api/v1/system/users - 列出用户

**权限**:SuperAdmin(查看所有) 或 TenantAdmin(查看本租户)

**查询参数**:
- `page`(可选):页码,默认 1
- `page_size`(可选):每页条数,默认 10

**响应**(200 OK):

```json
{
  "users": [
    {
      "id": 2,
      "username": "admin",
      "email": "admin@example.com",
      "full_name": "管理员",
      "is_active": true,
      "user_type": "tenant_admin",
      "tenant_id": 1,
      "created_at": "2025-12-01T00:00:00Z",
      "updated_at": "2025-12-01T00:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 10
}
```

**说明**:
- TenantAdmin 自动过滤 `WHERE tenant_id = <当前租户>`
- SuperAdmin 可查看所有用户(包括其他租户和 SuperAdmin)

---

#### GET /api/v1/system/users/me - 获取当前用户信息

**权限**:已认证用户

**响应**(200 OK):

```json
{
  "id": 2,
  "username": "admin",
  "email": "admin@example.com",
  "full_name": "管理员",
  "is_active": true,
  "user_type": "tenant_admin",
  "tenant_id": 1,
  "created_at": "2025-12-01T00:00:00Z",
  "updated_at": "2025-12-01T00:00:00Z"
}
```

---

#### GET /api/v1/system/users/:id - 获取指定用户

**权限**:用户本人 / TenantAdmin(本租户) / SuperAdmin

**响应**(200 OK):返回 User 对象

**响应**(403 Forbidden):

```json
{
  "error": "无权限访问此用户"
}
```

---

#### PUT /api/v1/system/users/:id - 更新用户信息

**权限**:用户本人 / TenantAdmin(本租户) / SuperAdmin

**请求体**:

```json
{
  "email": "newemail@example.com",
  "full_name": "新名字",
  "is_active": true,
  "user_type": "user"
}
```

**响应**(200 OK):返回更新后的 User 对象

**业务规则**:
- 普通用户只能修改自己的 email 和 full_name
- TenantAdmin 可修改本租户用户的 is_active 和 user_type
- SuperAdmin 可修改所有字段(除 SuperAdmin 用户的 user_type)
- 不能修改 username

---

#### PUT /api/v1/system/users/me/password - 修改当前用户密码

**权限**:用户本人

**请求体**:

```json
{
  "old_password": "oldpass123",
  "new_password": "newpass456"
}
```

**响应**(200 OK):

```json
{
  "message": "密码修改成功"
}
```

**响应**(400 Bad Request):

```json
{
  "error": "旧密码错误"
}
```

**业务规则**:
- 必须验证旧密码
- 新密码最少 6 位
- 密码使用 bcrypt 加密存储(cost=10)

---

#### DELETE /api/v1/system/users/:id - 删除用户

**权限**:TenantAdmin(本租户) / SuperAdmin

**响应**(200 OK):

```json
{
  "message": "用户删除成功"
}
```

**响应**(403 Forbidden):

```json
{
  "error": "不能删除 SuperAdmin 用户"
}
```

**业务规则**:
- 不能删除 SuperAdmin 用户
- TenantAdmin 只能删除本租户用户
- 删除用户不会级联删除审计日志

---

## 六、权限控制

### 6.1 认证机制

**用户访问令牌**:
- 格式:`addp_at_` opaque Access Token，只保存 SHA-256 Hash
- Token 位置:`Authorization: Bearer <token>`
- 身份字段由 `system.access_tokens` 关联用户并通过 AuthContext 返回

**中间件**:`middleware.AuthMiddleware`
- 验证 opaque Access Token Hash、有效期和撤销状态
- 回查当前用户、租户和激活状态，生成 AuthContext
- 仅在 System 内部解析 Token；业务模块通过 `/api/v1/system/auth/context` 消费结果
- 处理 Token 过期和无效情况

### 6.2 租户隔离

**自动隔离**:
- TenantAdmin 和 User 的所有查询自动添加 `WHERE tenant_id = <当前租户>`
- SuperAdmin 查询不受租户限制(tenant_id IS NULL)

**创建规则**:
- TenantAdmin 创建用户自动关联到自己的租户
- SuperAdmin 可手动指定 tenant_id

---

## 七、数据安全

### 7.1 密码加密

**算法**:bcrypt
**Cost**:10(2^10 = 1024 次迭代)
**实现**:

```go
import "golang.org/x/crypto/bcrypt"

// 加密
hash, _ := bcrypt.GenerateFromPassword([]byte(password), 10)

// 验证
err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
```

### 7.2 敏感字段保护

**不返回的字段**:
- `password_hash`:JSON 序列化时自动隐藏(`json:"-"`)

**响应脱敏**:
- 用户列表和详情接口不返回密码哈希

---

## 八、使用示例

### 8.1 登录获取 Token

```bash
curl -X POST http://localhost:8180/api/v1/system/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "123456"
  }'
```

**响应**:

```json
{
  "access_token": "<signed-user-access-token>",
  "token_type": "Bearer"
}
```

---

### 8.2 创建用户

```bash
TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."

curl -X POST http://localhost:8180/api/v1/system/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "newuser",
    "email": "newuser@example.com",
    "password": "password123",
    "full_name": "新用户",
    "user_type": "user"
  }'
```

---

### 8.3 查询用户列表

```bash
curl http://localhost:8180/api/v1/system/users?page=1&page_size=10 \
  -H "Authorization: Bearer $TOKEN"
```

---

### 8.4 获取当前用户信息

```bash
curl http://localhost:8180/api/v1/system/users/me \
  -H "Authorization: Bearer $TOKEN"
```

---

### 8.5 修改密码

```bash
curl -X PUT http://localhost:8180/api/v1/system/users/me/password \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "old_password": "123456",
    "new_password": "newpassword123"
  }'
```

---

## 九、重要说明

### 9.1 默认账户

**SuperAdmin**(总是启用):
- 用户名:`SuperAdmin`
- 密码:`20251001#SuperAdmin`(可通过环境变量配置)
- 用户类型:`super_admin`
- tenant_id:`NULL`

**默认租户管理员**(需启用):
- 用户名:`admin`(可配置)
- 密码:`123456`(可配置)
- 用户类型:`tenant_admin`
- tenant_id:默认租户 ID
- 启用方式:`ENABLE_DEFAULT_TENANT=true`

**生产环境限制**:
- `ENV=production` 时,即使设置 `ENABLE_DEFAULT_TENANT=true` 也不会创建默认租户和管理员
- SuperAdmin 始终创建,但应在生产环境修改默认密码

### 9.2 用户名规则

- 必须全局唯一
- 创建后不可修改
- 建议使用英文字母、数字、下划线

### 9.3 邮箱规则

- 全局唯一(可选填)
- 支持修改
- 未来可用于密码找回

---

## 十、相关文档

- [tenants 表](./tenants表.md) - 租户表,存储租户信息
- [audit_logs 表](./audit_logs表.md) - 审计日志表,记录用户操作
- [engines 表](./engines表.md) - 引擎配置表
- [数据库架构](../数据库架构.md) - System 模块整体架构
- [System 模块说明](../../system/CLAUDE.md) - 模块整体架构和设计理念
