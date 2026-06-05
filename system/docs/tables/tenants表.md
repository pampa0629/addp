# tenants 表结构和 API 说明

## 一、表结构概览

`system.tenants` 表是 ADDP 平台的多租户管理核心表,负责存储租户(组织)信息。每个租户代表一个独立的组织或企业,拥有独立的用户、资源和数据空间,实现数据隔离和权限控制。

### 核心功能

- **租户管理**:创建、查询、更新、删除租户
- **数据隔离**:每个租户的用户、引擎、数据完全隔离
- **租户激活控制**:支持启用/禁用租户
- **权限控制**:仅 SuperAdmin 可管理租户
- **自动创建管理员**:创建租户时自动创建 TenantAdmin 用户

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 租户唯一标识 |
| `name` | VARCHAR(255) | NOT NULL, UNIQUE | 租户名称(显示名称) |
| `description` | TEXT | | 租户描述信息 |
| `is_active` | BOOLEAN | DEFAULT true | 是否激活 |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `idx_tenants_name` | `name` | 唯一索引 | 租户名称唯一性约束 |

### 2.3 外键关系

**被引用表**:
- `system.users.tenant_id` → `tenants.id`
- `system.engines.tenant_id` → `tenants.id`
- `system.audit_logs.tenant_id` → `tenants.id`

---

## 三、Go 模型定义

### 3.1 Tenant 模型

```go
package models

type Tenant struct {
    ID          uint      `gorm:"primaryKey" json:"id"`
    Name        string    `gorm:"not null;unique" json:"name"`
    Description string    `json:"description"`
    IsActive    bool      `gorm:"default:true" json:"is_active"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

### 3.2 请求 DTO

```go
// 创建租户请求
type TenantCreateRequest struct {
    Name        string `json:"name" binding:"required"`
    Description string `json:"description"`
    AdminUsername string `json:"admin_username" binding:"required"`
    AdminPassword string `json:"admin_password" binding:"required,min=6"`
    AdminEmail    string `json:"admin_email"`
    AdminFullName string `json:"admin_full_name"`
}

// 更新租户请求
type TenantUpdateRequest struct {
    Name        *string `json:"name"`
    Description *string `json:"description"`
    IsActive    *bool   `json:"is_active"`
}
```

---

## 四、API 端点说明

**重要**:所有租户管理 API 仅 SuperAdmin 可访问

### 4.1 POST /api/tenants - 创建租户

**权限**:SuperAdmin

**请求头**:

```
Authorization: Bearer <jwt_token>
Content-Type: application/json
```

**请求体**:

```json
{
  "name": "新租户",
  "description": "租户描述信息",
  "admin_username": "tenant_admin",
  "admin_password": "admin123",
  "admin_email": "admin@newtenant.com",
  "admin_full_name": "租户管理员"
}
```

**响应**(201 Created):

```json
{
  "tenant": {
    "id": 2,
    "name": "新租户",
    "description": "租户描述信息",
    "is_active": true,
    "created_at": "2026-01-01T10:00:00Z",
    "updated_at": "2026-01-01T10:00:00Z"
  },
  "admin_user": {
    "id": 10,
    "username": "tenant_admin",
    "email": "admin@newtenant.com",
    "full_name": "租户管理员",
    "is_active": true,
    "user_type": "tenant_admin",
    "tenant_id": 2,
    "created_at": "2026-01-01T10:00:00Z"
  }
}
```

**业务逻辑**:
1. 创建租户记录
2. 自动创建 TenantAdmin 用户
3. 关联用户到新租户
4. 事务保证原子性(任一失败则回滚)

**响应**(400 Bad Request):

```json
{
  "error": "租户名称已存在"
}
```

---

### 4.2 GET /api/tenants - 列出租户

**权限**:SuperAdmin

**查询参数**:
- `page`(可选):页码,默认 1
- `page_size`(可选):每页条数,默认 10

**响应**(200 OK):

```json
{
  "tenants": [
    {
      "id": 1,
      "name": "默认租户",
      "description": "系统默认租户",
      "is_active": true,
      "created_at": "2025-12-01T00:00:00Z",
      "updated_at": "2025-12-01T00:00:00Z"
    },
    {
      "id": 2,
      "name": "新租户",
      "description": "租户描述信息",
      "is_active": true,
      "created_at": "2026-01-01T10:00:00Z",
      "updated_at": "2026-01-01T10:00:00Z"
    }
  ],
  "total": 2,
  "page": 1,
  "page_size": 10
}
```

---

### 4.3 GET /api/tenants/:id - 获取指定租户

**权限**:SuperAdmin

**响应**(200 OK):

```json
{
  "id": 2,
  "name": "新租户",
  "description": "租户描述信息",
  "is_active": true,
  "created_at": "2026-01-01T10:00:00Z",
  "updated_at": "2026-01-01T10:00:00Z"
}
```

**响应**(404 Not Found):

```json
{
  "error": "租户不存在"
}
```

---

### 4.4 PUT /api/tenants/:id - 更新租户

**权限**:SuperAdmin

**请求体**:

```json
{
  "name": "更新后的名称",
  "description": "新的描述信息",
  "is_active": false
}
```

**响应**(200 OK):

```json
{
  "id": 2,
  "name": "更新后的名称",
  "description": "新的描述信息",
  "is_active": false,
  "created_at": "2026-01-01T10:00:00Z",
  "updated_at": "2026-01-01T10:30:00Z"
}
```

**业务规则**:
- 租户名称必须唯一
- 禁用租户(`is_active=false`)不影响已存在的用户和数据,但用户无法登录
- 更新租户不影响关联的用户和资源

---

### 4.5 DELETE /api/tenants/:id - 删除租户

**权限**:SuperAdmin

**响应**(200 OK):

```json
{
  "message": "租户删除成功"
}
```

**响应**(400 Bad Request):

```json
{
  "error": "无法删除有关联用户的租户"
}
```

**删除策略**:
- **软删除**(推荐):禁用租户而非真实删除
- **硬删除**(危险):级联删除租户下的所有用户、引擎、数据
- 当前实现:硬删除(需要确保租户下无关联资源)

**注意事项**:
- 删除租户前应先删除或迁移所有关联用户
- 删除租户前应先删除或迁移所有关联引擎
- 删除操作不可恢复

---

## 五、权限控制

### 5.1 访问权限

| 操作 | SuperAdmin | TenantAdmin | User |
|------|------------|-------------|------|
| 创建租户 | ✅ | ❌ | ❌ |
| 查看租户列表 | ✅ | ❌ | ❌ |
| 查看租户详情 | ✅ | ❌ | ❌ |
| 更新租户 | ✅ | ❌ | ❌ |
| 删除租户 | ✅ | ❌ | ❌ |

**说明**:
- 只有 SuperAdmin 可以查看和管理租户
- TenantAdmin 和 User 无法访问租户管理 API
- 租户管理操作记录到审计日志

### 5.2 中间件验证

```go
// 示例:租户管理 API 权限检查
func (h *TenantHandler) Create(c *gin.Context) {
    // 从 JWT 中提取 user_type
    userType := c.GetString("user_type")

    // 验证是否为 SuperAdmin
    if userType != "super_admin" {
        c.JSON(403, gin.H{"error": "仅 SuperAdmin 可管理租户"})
        return
    }

    // 继续处理...
}
```

---

## 六、数据隔离

### 6.1 租户数据隔离范围

每个租户拥有独立的:

| 资源类型 | 隔离方式 | 说明 |
|---------|---------|------|
| 用户 | `users.tenant_id` | 租户下的用户列表 |
| 引擎 | `engines.tenant_id` | 租户下的数据库/存储引擎配置 |
| 审计日志 | `audit_logs.tenant_id` | 租户下的操作记录 |
| Manager 资源 | `manager.*.tenant_id` | 上传目录、权限配置 |
| Meta 资源 | `meta.*.tenant_id` | 元数据扫描任务、节点 |
| Transfer 任务 | `transfer.*.tenant_id` | 数据导入导出任务 |
| Develop 项目 | `develop.*.tenant_id` | SQL 项目、工作流 |

### 6.2 SuperAdmin 特殊性

**SuperAdmin 不属于任何租户**:
- `user_type = 'super_admin'`
- `tenant_id = NULL`
- 可查看和管理所有租户的资源
- 不受租户隔离限制

---

## 七、使用示例

### 7.1 创建租户

```bash
# SuperAdmin 登录获取 Token
TOKEN=$(curl -X POST http://localhost:8180/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "SuperAdmin", "password": "20251001#SuperAdmin"}' \
  | jq -r '.access_token')

# 创建新租户
curl -X POST http://localhost:8180/api/tenants \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "测试企业",
    "description": "用于测试的企业租户",
    "admin_username": "test_admin",
    "admin_password": "admin123",
    "admin_email": "admin@test.com",
    "admin_full_name": "测试管理员"
  }'
```

**响应**:

```json
{
  "tenant": {
    "id": 3,
    "name": "测试企业",
    "description": "用于测试的企业租户",
    "is_active": true,
    "created_at": "2026-01-01T11:00:00Z",
    "updated_at": "2026-01-01T11:00:00Z"
  },
  "admin_user": {
    "id": 15,
    "username": "test_admin",
    "email": "admin@test.com",
    "full_name": "测试管理员",
    "is_active": true,
    "user_type": "tenant_admin",
    "tenant_id": 3,
    "created_at": "2026-01-01T11:00:00Z"
  }
}
```

---

### 7.2 查询租户列表

```bash
curl http://localhost:8180/api/tenants?page=1&page_size=20 \
  -H "Authorization: Bearer $TOKEN"
```

---

### 7.3 更新租户信息

```bash
curl -X PUT http://localhost:8180/api/tenants/3 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "description": "更新后的描述",
    "is_active": true
  }'
```

---

### 7.4 禁用租户

```bash
curl -X PUT http://localhost:8180/api/tenants/3 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "is_active": false
  }'
```

**效果**:
- 租户下的用户无法登录
- 现有 Token 仍然有效(直到过期)
- 租户数据保留,可随时重新激活

---

### 7.5 删除租户

```bash
curl -X DELETE http://localhost:8180/api/tenants/3 \
  -H "Authorization: Bearer $TOKEN"
```

**警告**:删除租户将级联删除所有关联资源,操作不可恢复!

---

## 八、重要说明

### 8.1 默认租户

**创建方式**:
- 设置环境变量 `ENABLE_DEFAULT_TENANT=true`
- 服务启动时自动创建

**配置参数**:
- `DEFAULT_TENANT_NAME`:租户名称(默认:"默认租户")
- `DEFAULT_ADMIN_USERNAME`:管理员用户名(默认:"admin")
- `DEFAULT_ADMIN_PASSWORD`:管理员密码(默认:"123456")
- `DEFAULT_ADMIN_EMAIL`:管理员邮箱(默认:"admin@addp.com")

**生产环境限制**:
- `ENV=production` 时,即使设置 `ENABLE_DEFAULT_TENANT=true` 也不会创建
- 生产环境必须通过 SuperAdmin 手动创建租户

### 8.2 租户命名规则

- 必须全局唯一
- 建议使用企业/组织名称
- 支持中文
- 不建议使用特殊字符

### 8.3 租户删除策略

**推荐方案**:
1. 禁用租户(`is_active=false`)而非删除
2. 保留租户数据用于审计
3. 需要时可重新激活

**删除前检查**:
1. 确认租户下无活跃用户
2. 确认租户下无重要数据
3. 备份租户关联的审计日志

---

## 九、相关文档

- [users 表](./users表.md) - 用户表,关联 tenant_id
- [audit_logs 表](./audit_logs表.md) - 审计日志表,记录租户操作
- [engines 表](./engines表.md) - 引擎配置表,按租户隔离
- [数据库架构](../数据库架构.md) - System 模块整体架构
- [System 模块说明](../../system/CLAUDE.md) - 模块整体架构和设计理念
