# System 模块数据结构和 API 文档

## 目录

- [1. 模块概述](#1-模块概述)
- [2. 数据库结构](#2-数据库结构)
- [3. API 端点清单](#3-api-端点清单)
- [4. 基础设施使用](#4-基础设施使用)
- [5. 配置参数](#5-配置参数)
- [6. 安全机制](#6-安全机制)

---

## 1. 模块概述

System 模块是 ADDP 平台的核心基础模块，提供以下功能：

- **用户认证与授权**：JWT Token 签发、验证
- **用户管理**：用户 CRUD、密码管理、权限控制
- **租户管理**：多租户隔离、租户 CRUD
- **引擎管理**：存储引擎配置、连接信息加密存储
- **审计日志**：自动记录所有非 GET 操作
- **配置中心**：为其他模块提供共享配置（JWT Secret、加密密钥等）
- **能力注册**：支持 Orchestrator 动态发现和调用执行引擎

### 端口配置

- **开发端口**: 8080
- **生产端口**: 8080
- **数据库 Schema**: `system`
- **依赖**: PostgreSQL, Redis（可选）, MinIO（间接）

### 模块依赖关系

```
System（配置中心、认证服务）
  ↓ (被依赖)
Manager, Meta, Transfer, Orchestrator, Develop
```

---

## 2. 数据库结构

### 2.1 PostgreSQL Schema: system

System 模块使用 `system` schema，包含 4 张核心表。

#### 表 1: users - 用户表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 用户唯一标识 |
| `username` | VARCHAR(255) | NOT NULL, UNIQUE | 用户名（登录用） |
| `email` | VARCHAR(255) | UNIQUE | 邮箱地址 |
| `password_hash` | VARCHAR(255) | NOT NULL | bcrypt 加密的密码（cost=10） |
| `full_name` | VARCHAR(255) | | 用户全名 |
| `is_active` | BOOLEAN | DEFAULT true | 是否激活 |
| `user_type` | VARCHAR(50) | NOT NULL | 用户类型：super_admin/tenant_admin/user |
| `tenant_id` | INTEGER | FK → tenants.id | 租户 ID（super_admin 为 NULL） |
| `is_superuser` | BOOLEAN | DEFAULT false | 兼容旧代码的超级用户标志 |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 更新时间 |

**索引**:
- `idx_users_username` - 用户名唯一索引
- `idx_users_tenant` - 租户隔离查询索引
- `idx_users_email` - 邮箱唯一索引

**权限矩阵**:
- `super_admin`: 创建/管理租户，超级权限，tenant_id=NULL
- `tenant_admin`: 创建/管理本租户用户，租户级别管理权限
- `user`: 仅查看自己信息，普通用户权限

**Go 模型** (`internal/models/user.go`):

```go
type User struct {
    ID           uint           `gorm:"primaryKey" json:"id"`
    Username     string         `gorm:"not null;unique" json:"username"`
    Email        string         `gorm:"unique" json:"email"`
    PasswordHash string         `gorm:"not null" json:"-"`
    FullName     string         `json:"full_name"`
    IsActive     bool           `gorm:"default:true" json:"is_active"`
    UserType     string         `gorm:"not null" json:"user_type"`
    TenantID     *uint          `json:"tenant_id"`
    IsSuperuser  bool           `gorm:"default:false" json:"is_superuser"`
    CreatedAt    time.Time      `json:"created_at"`
    UpdatedAt    time.Time      `json:"updated_at"`
}
```

---

#### 表 2: tenants - 租户表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 租户唯一标识 |
| `name` | VARCHAR(255) | NOT NULL, UNIQUE | 租户名称 |
| `description` | TEXT | | 租户描述 |
| `is_active` | BOOLEAN | DEFAULT true | 是否激活 |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 更新时间 |

**索引**:
- `idx_tenants_name` - 租户名称唯一索引

**Go 模型** (`internal/models/tenant.go`):

```go
type Tenant struct {
    ID          uint      `gorm:"primaryKey" json:"id"`
    Name        string    `gorm:"not null;unique" json:"name"`
    Description string    `json:"description"`
    IsActive    bool      `gorm:"default:true" json:"is_active"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

---

#### 表 3: audit_logs - 审计日志表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 日志唯一标识 |
| `user_id` | INTEGER | FK → users.id (nullable) | 用户 ID |
| `username` | VARCHAR(255) | | 用户名快照 |
| `tenant_id` | INTEGER | FK → tenants.id (nullable) | 租户 ID（super_admin 操作为 NULL） |
| `action` | VARCHAR(255) | NOT NULL | 操作类型（HTTP方法+路径） |
| `entity_type` | VARCHAR(255) | | 操作对象类型（如：engine, user, tenant等） |
| `entity_id` | VARCHAR(255) | | 操作对象 ID |
| `details` | TEXT | | 操作详情（JSON 格式） |
| `ip_address` | VARCHAR(255) | | 客户端 IP |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP, INDEXED | 操作时间 |

**索引**:
- `idx_audit_logs_user` - 按用户查询
- `idx_audit_logs_tenant` - 按租户隔离
- `idx_audit_logs_created` - 按时间降序查询

**自动记录机制**:
- 由 `LoggerMiddleware` 自动捕获所有**非 GET 请求**
- 自动关联 tenant_id，按租户隔离日志
- 记录请求体、响应状态、客户端 IP

**Go 模型** (`internal/models/log.go`):

```go
type AuditLog struct {
    ID           uint      `gorm:"primaryKey" json:"id"`
    UserID       *uint     `json:"user_id"`
    Username     string    `json:"username"`
    TenantID     *uint     `json:"tenant_id"`
    Action       string    `gorm:"not null" json:"action"`
    EntityType   string    `json:"entity_type"`
    EntityID     string    `json:"entity_id"`
    Details      string    `gorm:"type:text" json:"details"`
    IPAddress    string    `json:"ip_address"`
    CreatedAt    time.Time `gorm:"index" json:"created_at"`
}
```

---

#### 表 4: engines - 引擎配置表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 引擎唯一标识 |
| `name` | VARCHAR(255) | NOT NULL, INDEXED | 引擎名称 |
| `engine_type` | VARCHAR(100) | NOT NULL | 引擎类型：database/compute_engine/object_storage |
| `connection_info` | JSONB | NOT NULL | 连接信息（敏感字段 AES-256-GCM 加密） |
| `description` | TEXT | | 引擎描述 |
| `scan_config` | JSONB | | 元数据扫描配置 |
| `unique_identifier` | VARCHAR(255) | UNIQUE INDEX | 能力唯一标识符（用于 Orchestrator） |
| `is_builtin` | BOOLEAN | DEFAULT false, INDEXED | 是否内置能力 |
| `capabilities` | JSONB | | 能力声明（支持的操作类型） |
| `task_api_config` | JSONB | | 任务 API 配置（端点、超时等） |
| `health_check_config` | JSONB | | 健康检查配置 |
| `created_by` | INTEGER | FK → users.id | 创建者 ID |
| `tenant_id` | INTEGER | FK → tenants.id (nullable) | 租户 ID（super_admin 引擎为 NULL） |
| `is_active` | BOOLEAN | DEFAULT true | 是否激活 |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 更新时间 |

**索引**:
- `idx_engines_name` - 引擎名称索引
- `idx_engines_type` - 按类型查询
- `idx_engines_tenant` - 租户隔离
- `idx_engines_identifier` - 能力标识符唯一索引
- `idx_engines_builtin` - 内置能力过滤

**加密策略（AES-256-GCM）**:

加密字段（在 `connection_info` JSON 中）：
- `password` - 数据库密码
- `access_key` - MinIO/S3 访问密钥
- `secret_key` - MinIO/S3 密钥
- `token` - API Token
- `api_key` - API 密钥

**扫描配置 (ScanConfig JSON)**:

```json
{
  "enabled": true,
  "immediate_scan": false,
  "immediate_depth": "basic",
  "scheduled_scan": true,
  "schedule_type": "daily",
  "cron_expression": "0 0 * * *",
  "schedule_time": "00:00",
  "schedule_value": [0],
  "scan_depth": "shallow",
  "preprocessing": {
    "enabled": true,
    "auto_trigger": false,
    "types": ["mvt_tiles", "vector_embedding"],
    "mvt_config": {
      "max_zoom": 18,
      "concurrency": 10,
      "stop_threshold_sec": 3.0,
      "stop_threshold_kb": 50.0
    }
  }
}
```

**Go 模型** (`internal/models/engine.go`):

```go
type Engine struct {
    ID                  uint                   `gorm:"primaryKey" json:"id"`
    Name                string                 `gorm:"not null;index" json:"name"`
    EngineType          string                 `gorm:"not null" json:"engine_type"`
    ConnectionInfo      map[string]interface{} `gorm:"type:jsonb" json:"connection_info"`
    Description         string                 `json:"description"`
    ScanConfig          *ScanConfig            `gorm:"type:jsonb" json:"scan_config,omitempty"`
    UniqueIdentifier    string                 `gorm:"unique;index" json:"unique_identifier,omitempty"`
    IsBuiltin           bool                   `gorm:"default:false;index" json:"is_builtin"`
    Capabilities        map[string]interface{} `gorm:"type:jsonb" json:"capabilities,omitempty"`
    TaskAPIConfig       map[string]interface{} `gorm:"type:jsonb" json:"task_api_config,omitempty"`
    HealthCheckConfig   map[string]interface{} `gorm:"type:jsonb" json:"health_check_config,omitempty"`
    CreatedBy           *uint                  `json:"created_by"`
    TenantID            *uint                  `json:"tenant_id"`
    IsActive            bool                   `gorm:"default:true" json:"is_active"`
    CreatedAt           time.Time              `json:"created_at"`
    UpdatedAt           time.Time              `json:"updated_at"`
}
```

---

### 2.2 数据表关系图

```
┌─────────────┐
│  tenants    │ (租户表)
│  - id (PK)  │
└──────┬──────┘
       │ 1:N
       ↓
┌─────────────┐
│   users     │ (用户表)
│  - id (PK)  │
│  - tenant_id│
└──────┬──────┘
       │ 1:N
       ↓
┌─────────────┐
│   engines   │ (引擎配置表)
│  - id (PK)  │
│  - created_by
│  - tenant_id│
└──────┬──────┘
       │
       ↓
┌─────────────┐
│ audit_logs  │ (审计日志表)
│  - id (PK)  │
│  - user_id  │
│  - tenant_id│
└─────────────┘
```

---

## 3. API 端点清单

### 3.1 基础端点

| 方法 | 端点 | 认证 | 说明 |
|------|------|------|------|
| GET | `/` | 否 | 项目信息 |
| GET | `/health` | 否 | 健康检查 |

**响应示例** (GET `/`):

```json
{
  "message": "全域数据平台",
  "name_en": "All Domain Data Platform"
}
```

---

### 3.2 认证 API

#### POST /api/auth/login - 用户登录

**请求体**:

```json
{
  "username": "admin",
  "password": "123456"
}
```

**响应** (200 OK):

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer"
}
```

**响应** (401 Unauthorized):

```json
{
  "error": "用户名或密码错误"
}
```

---

#### POST /api/auth/register - 用户注册

**请求体**:

```json
{
  "username": "newuser",
  "email": "user@example.com",
  "password": "password123",
  "full_name": "张三"
}
```

**响应** (201 Created):

```json
{
  "id": 5,
  "username": "newuser",
  "email": "user@example.com",
  "full_name": "张三",
  "is_active": true,
  "user_type": "user",
  "tenant_id": 1,
  "created_at": "2025-12-11T10:30:00Z"
}
```

**注意**: 此端点仅在 `ALLOW_PUBLIC_REGISTRATION=true` 时开放。

---

### 3.3 用户管理 API

所有用户管理端点需要 JWT 认证。

#### POST /api/users - 创建用户

**权限**: tenant_admin（本租户）或 super_admin

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

**响应** (201 Created): 返回 User 对象

---

#### GET /api/users - 列出用户

**权限**: 自动过滤（tenant_admin 仅看本租户，super_admin 看全部）

**查询参数**:
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 10）

**响应** (200 OK):

```json
{
  "users": [
    {
      "id": 1,
      "username": "admin",
      "email": "admin@example.com",
      "full_name": "管理员",
      "is_active": true,
      "user_type": "tenant_admin",
      "tenant_id": 1,
      "created_at": "2025-12-01T00:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 10
}
```

---

#### GET /api/users/me - 获取当前用户信息

**权限**: 已认证用户

**响应** (200 OK): 返回当前用户的 User 对象

---

#### GET /api/users/:id - 获取指定用户

**权限**: 用户本人 / 租户管理员（本租户）/ super_admin

**响应** (200 OK): 返回 User 对象

---

#### PUT /api/users/:id - 更新用户信息

**权限**: 用户本人 / 租户管理员（本租户）/ super_admin

**请求体**:

```json
{
  "email": "newemail@example.com",
  "full_name": "新名字",
  "is_active": true,
  "user_type": "user"
}
```

**响应** (200 OK): 返回更新后的 User 对象

---

#### PUT /api/users/:id/change-password - 修改密码

**权限**: 用户本人

**请求体**:

```json
{
  "old_password": "oldpass123",
  "new_password": "newpass456"
}
```

**响应** (200 OK):

```json
{
  "message": "密码修改成功"
}
```

---

#### DELETE /api/users/:id - 删除用户

**权限**: 租户管理员（本租户）/ super_admin

**限制**: SuperAdmin 不可删除

**响应** (200 OK):

```json
{
  "message": "用户删除成功"
}
```

---

### 3.4 日志管理 API

#### GET /api/logs - 查询审计日志

**权限**: 自动过滤（仅返回本租户日志，super_admin 可查看所有）

**查询参数**:
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 20）
- `user_id`: 按用户过滤

**响应** (200 OK):

```json
{
  "logs": [
    {
      "id": 100,
      "user_id": 2,
      "username": "admin",
      "tenant_id": 1,
      "action": "POST /api/engines",
      "entity_type": "engine",
      "entity_id": "5",
      "details": "{\"name\":\"test-db\"}",
      "ip_address": "127.0.0.1",
      "created_at": "2025-12-11T10:30:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

---

#### GET /api/logs/:id - 获取指定日志

**权限**: 本租户用户

**响应** (200 OK): 返回 AuditLog 对象

---

### 3.5 引擎管理 API

#### POST /api/engines - 创建引擎

**权限**: 本租户用户

**请求体**:

```json
{
  "name": "PostgreSQL-生产库",
  "engine_type": "postgresql",
  "connection_info": {
    "host": "localhost",
    "port": "5432",
    "user": "postgres",
    "password": "secret123",
    "database": "mydb"
  },
  "description": "生产数据库",
  "scan_config": {
    "enabled": true,
    "schedule_type": "daily",
    "schedule_time": "02:00"
  }
}
```

**响应** (201 Created): 返回 Engine 对象（connection_info 自动加密）

**事件发布**: 发布 Redis 事件 `system:engine:created`

---

#### GET /api/engines - 列出引擎

**权限**: 自动过滤本租户引擎

**查询参数**:
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 10）
- `engine_type`: 按类型过滤（postgresql/minio/s3 等）

**响应** (200 OK):

```json
{
  "engines": [
    {
      "id": 1,
      "name": "PostgreSQL-生产库",
      "engine_type": "postgresql",
      "connection_info": {
        "host": "localhost",
        "port": "5432",
        "user": "postgres",
        "password": "secret123",
        "database": "mydb"
      },
      "description": "生产数据库",
      "is_active": true,
      "created_at": "2025-12-11T10:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 10
}
```

**注意**: `connection_info` 自动解密返回

---

#### GET /api/engines/:id - 获取指定引擎

**权限**: 本租户用户

**响应** (200 OK): 返回 Engine 对象（connection_info 解密）

---

#### PUT /api/engines/:id - 更新引擎

**权限**: 本租户用户

**请求体**: 同创建引擎（可选字段）

**响应** (200 OK): 返回更新后的 Engine 对象

**事件发布**: 发布 Redis 事件 `system:engine:updated`

---

#### DELETE /api/engines/:id - 删除引擎

**权限**: 本租户用户

**响应** (200 OK):

```json
{
  "message": "引擎删除成功"
}
```

**事件发布**: 发布 Redis 事件 `system:engine:deleted`

---

#### POST /api/engines/:id/test - 测试已有引擎连接

**权限**: 本租户用户

**响应** (200 OK):

```json
{
  "success": true,
  "message": "连接测试成功"
}
```

**响应** (400 Bad Request):

```json
{
  "success": false,
  "message": "连接失败",
  "error": "dial tcp: connection refused"
}
```

**支持类型**: postgresql, minio, s3

---

#### POST /api/engines/test-connection - 创建前测试连接

**权限**: 本租户用户

**请求体**:

```json
{
  "engine_type": "postgresql",
  "connection_info": {
    "host": "localhost",
    "port": "5432",
    "user": "postgres",
    "password": "secret123",
    "database": "testdb"
  }
}
```

**响应**: 同上

---

### 3.6 租户管理 API

**权限**: 所有端点仅 super_admin 可访问

#### POST /api/tenants - 创建租户

**请求体**:

```json
{
  "name": "新租户",
  "description": "租户描述",
  "admin_username": "tenant_admin",
  "admin_password": "admin123",
  "admin_email": "admin@tenant.com",
  "admin_full_name": "租户管理员"
}
```

**响应** (201 Created):

```json
{
  "tenant": {
    "id": 2,
    "name": "新租户",
    "description": "租户描述",
    "is_active": true,
    "created_at": "2025-12-11T11:00:00Z"
  },
  "admin_user": {
    "id": 10,
    "username": "tenant_admin",
    "email": "admin@tenant.com",
    "full_name": "租户管理员",
    "user_type": "tenant_admin",
    "tenant_id": 2
  }
}
```

---

#### GET /api/tenants - 列出租户

**查询参数**:
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 10）

**响应** (200 OK): 返回租户列表

---

#### GET /api/tenants/:id - 获取指定租户

**响应** (200 OK): 返回 Tenant 对象

---

#### PUT /api/tenants/:id - 更新租户

**请求体**:

```json
{
  "name": "更新后的名称",
  "description": "新描述",
  "is_active": false
}
```

**响应** (200 OK): 返回更新后的 Tenant 对象

---

#### DELETE /api/tenants/:id - 删除租户

**响应** (200 OK):

```json
{
  "message": "租户删除成功"
}
```

**注意**: 级联删除该租户的所有用户和资源

---

### 3.7 内部 API（服务间调用）

**认证**: 需要 `X-Internal-API-Key` 请求头

#### GET /internal/config - 获取跨服务共享配置

**响应** (200 OK):

```json
{
  "project": {
    "name": "全域数据平台"
  },
  "jwt_secret": "your-jwt-secret",
  "encryption_key": "base64-encoded-key",
  "internal_api_key": "your-internal-api-key",
  "map": {
    "amap_key": "your-amap-key",
    "amap_security_js_code": "your-security-code",
    "tdt_key": "your-tdt-key"
  },
  "database": {
    "host": "localhost",
    "port": "5432",
    "user": "addp",
    "password": "addp_password",
    "name": "addp"
  }
}
```

---

#### GET /internal/engines - 获取引擎列表（无租户隔离）

**查询参数**:
- `engine_type`: 按类型过滤
- `tenant_id`: 按租户过滤

**响应** (200 OK): 返回引擎列表

---

#### GET /internal/engines/:id - 获取指定引擎

**响应** (200 OK): 返回 Engine 对象

---

#### POST /internal/engines - 创建引擎

**请求体**:

```json
{
  "name": "引擎名",
  "engine_type": "postgresql",
  "connection_info": {...},
  "tenant_id": 1,
  "created_by": 2
}
```

**响应** (201 Created): 返回 Engine 对象

---

### 3.8 能力注册 API

**认证**: 需要 `X-Internal-API-Key` 请求头

#### POST /internal/registry/capabilities - 注册能力

**请求体**:

```json
{
  "unique_identifier": "meta.scanner.default",
  "name": "Meta 元数据扫描器",
  "engine_type": "compute_engine",
  "is_builtin": true,
  "capabilities": {
    "scan": true,
    "schedule": true
  },
  "task_api_config": {
    "base_url": "http://localhost:8082",
    "endpoints": {
      "create": {
        "method": "POST",
        "path": "/api/scan/tasks"
      },
      "execute": {
        "method": "POST",
        "path": "/api/scan/tasks/{{.TaskID}}/execute"
      },
      "status": {
        "method": "GET",
        "path": "/api/scan/runs/{{.RunID}}"
      }
    }
  }
}
```

**响应** (201 Created): 返回 Engine 对象

---

#### GET /internal/registry/capabilities - 查询能力列表

**查询参数**:
- `engine_type`: 按类型过滤（compute_engine 等）
- `is_builtin`: 按是否内置过滤
- `is_active`: 按激活状态过滤

**响应** (200 OK): 返回 Engine 列表

---

#### GET /internal/registry/capabilities/:identifier - 按标识符查询

**响应** (200 OK): 返回 Engine 对象

---

#### GET /internal/registry/compute-engines - 查询所有计算引擎

**响应** (200 OK): 返回 Engine 列表（仅 is_builtin=true）

---

## 4. 基础设施使用

### 4.1 Redis

**配置**:
- Host: `REDIS_HOST` (默认: localhost)
- Port: `REDIS_PORT` (默认: 6379)
- Password: `REDIS_PASSWORD` (可选)
- DB: `REDIS_DB` (默认: 0)

**用途**: 引擎变更事件发布（Pub/Sub）

**事件消息格式**:

| 事件名 | 触发条件 | Payload |
|--------|---------|---------|
| `system:engine:created` | 创建引擎 | `{"engine_id": 1, "engine_type": "postgresql", "tenant_id": 1}` |
| `system:engine:updated` | 更新引擎 | 同上 |
| `system:engine:deleted` | 删除引擎 | 同上 |

**订阅方**:
- Meta 模块：订阅引擎创建事件，根据 ScanConfig 决定是否自动扫描
- Manager 模块：订阅引擎变更事件，刷新缓存

---

### 4.2 MinIO

System 模块**不直接使用** MinIO，但支持：
- 测试 MinIO/S3 连接（`TestConnection` 端点）
- 存储 MinIO 连接配置于 `engines.connection_info` 中

MinIO 的实际使用由其他模块负责：
- **系统 MinIO** (9000-9001): 存储系统文件（用户头像、系统配置等）
- **业务 MinIO** (9002-9003): 存储用户业务文件

---

## 5. 配置参数

### 5.1 环境变量清单

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `PORT` | 8080 | 服务端口 |
| `ENV` | development | 运行环境（development/production） |
| `JWT_SECRET` | - | JWT 签名密钥（**必须修改**） |
| `ENCRYPTION_KEY` | - | AES-256 加密密钥（Base64 编码，32 字节） |
| `INTERNAL_API_KEY` | - | 内部 API 认证密钥 |
| `POSTGRES_HOST` | localhost | PostgreSQL 主机 |
| `POSTGRES_PORT` | 5432 | PostgreSQL 端口 |
| `POSTGRES_USER` | addp | 数据库用户 |
| `POSTGRES_PASSWORD` | addp_password | 数据库密码 |
| `POSTGRES_DB` | addp | 数据库名 |
| `REDIS_HOST` | localhost | Redis 主机（可选） |
| `REDIS_PORT` | 6379 | Redis 端口 |
| `REDIS_PASSWORD` | - | Redis 密码（可选） |
| `REDIS_DB` | 0 | Redis 数据库编号 |
| `SUPER_ADMIN_USERNAME` | SuperAdmin | 超级管理员用户名 |
| `SUPER_ADMIN_PASSWORD` | 20251001#SuperAdmin | 超级管理员密码 |
| `ENABLE_DEFAULT_TENANT` | false | 是否启用默认租户（**生产环境禁止**） |
| `DEFAULT_TENANT_NAME` | 默认租户 | 默认租户名称 |
| `DEFAULT_ADMIN_USERNAME` | admin | 默认租户管理员用户名 |
| `DEFAULT_ADMIN_PASSWORD` | 123456 | 默认租户管理员密码 |
| `DEFAULT_ADMIN_EMAIL` | admin@addp.com | 默认租户管理员邮箱 |
| `ALLOW_PUBLIC_REGISTRATION` | false | 是否允许公开注册 |
| `AMAP_KEY` | - | 高德地图 API Key |
| `AMAP_SECURITY_KEY` | - | 高德地图安全密钥 |
| `TDT_KEY` | - | 天地图 API Key |

---

### 5.2 默认测试账户

**超级管理员**:
- 用户名: `SuperAdmin`
- 密码: `20251001#SuperAdmin`
- 用户类型: `super_admin`
- 权限: 管理租户、查看系统级日志
- 创建方式: 应用启动时自动创建（总是启用）

**默认租户管理员**（需启用）:
- 用户名: `admin`
- 密码: `123456`
- 用户类型: `tenant_admin`
- 权限: 管理默认租户下的用户、资源、数据
- 创建方式: 需要在 `.env` 中设置 `ENABLE_DEFAULT_TENANT=true`

**安全提示**:
- ⚠️ 仅用于开发和测试环境
- ⚠️ 生产环境强制禁用（即使设置 `ENABLE_DEFAULT_TENANT=true`，在 `ENV=production` 时也不会创建）
- ⚠️ 默认禁用（未设置 `ENABLE_DEFAULT_TENANT=true` 时不会创建）

---

## 6. 安全机制

### 6.1 密码加密

| 机制 | 算法 | 用途 |
|------|------|------|
| 用户密码 | bcrypt (cost: 10) | `users.password_hash` |
| 引擎敏感字段 | AES-256-GCM | `engines.connection_info` 中的 password/key/token |

---

### 6.2 JWT 认证

- **算法**: HS256
- **签名密钥**: `JWT_SECRET` 环境变量
- **Token 位置**: HTTP Header `Authorization: Bearer <token>`
- **Payload**: `{"user_id": 1, "username": "admin", "tenant_id": 1, "exp": 1234567890}`

---

### 6.3 内部 API 认证

- **机制**: `X-Internal-API-Key` 请求头
- **密钥来源**: `INTERNAL_API_KEY` 环境变量
- **用途**: 服务间调用认证（Manager/Meta/Transfer 调用 System）

---

### 6.4 多租户隔离

- 所有查询自动加上 `tenant_id` 过滤（中间件自动注入）
- `super_admin` 可跨租户查看（tenant_id=NULL）
- 审计日志按租户隔离记录

---

### 6.5 审计日志

- **触发**: 所有非 GET 操作（POST/PUT/DELETE）
- **记录内容**: 用户、租户、操作、资源、详情、IP
- **存储**: PostgreSQL `system.audit_logs` 表
- **查询**: 按租户隔离，支持按用户过滤

---

## 7. 关键文件路径

| 文件 | 路径 | 说明 |
|------|------|------|
| 路由配置 | `system/backend/internal/api/router.go` | 所有 API 端点定义 |
| 用户模型 | `system/backend/internal/models/user.go` | 用户字段和请求结构 |
| 引擎模型 | `system/backend/internal/models/engine.go` | 引擎和扫描配置 |
| 数据库初始化 | `system/backend/internal/repository/database.go` | AutoMigrate 和初始化逻辑 |
| 配置加载 | `system/backend/internal/config/config.go` | 环境变量和安全验证 |
| 认证中间件 | `system/backend/internal/middleware/auth.go` | JWT 验证和内部 API 认证 |
| 日志中间件 | `system/backend/internal/middleware/logger.go` | 审计日志记录 |
| 应用入口 | `system/backend/cmd/server/main.go` | 服务启动 |

---

## 8. 相关文档

- [ADDP 平台架构文档](../CLAUDE.md)
- [System 模块详细文档](README.md)
- [配置中心使用指南](../docs/CONFIG_CENTER.md)
- [Common 模块文档](../docs/COMMON_MODULE.md)
