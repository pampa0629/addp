# Develop 模块数据结构和 API 文档

## 目录

- [1. 模块概述](#1-模块概述)
- [2. 数据库结构](#2-数据库结构)
- [3. API 端点清单](#3-api-端点清单)
- [4. 服务层架构](#4-服务层架构)
- [5. 配置参数](#5-配置参数)

---

## 1. 模块概述

Develop 模块是 ADDP 平台的开发工具模块，提供以下功能：

- **SQL 查询执行**：交互式 SQL 查询界面（类似 DBeaver）
- **连接测试**：数据库连接验证
- **脚本管理**（Phase 2 计划中）：SQL 脚本保存、版本控制、依赖管理
- **执行历史**（Phase 2 计划中）：查询历史记录和结果缓存
- **Monaco Editor 集成**：提供代码高亮、智能提示、格式化

### 端口配置

- **开发端口**: 8085
- **生产端口**: 8085
- **数据库 Schema**: `develop`
- **依赖**: PostgreSQL, System 模块, Manager 模块（数据源配置）

### 模块依赖关系

```
System（资源配置、认证服务）
  ↓
Manager（数据源元数据）
  ↓
Develop（SQL 开发工具）
```

---

## 2. 数据库结构

### 2.1 PostgreSQL Schema: develop

Develop 模块使用 `develop` schema，包含 4 张核心表（Phase 2 计划中）。

#### 表 1: scripts - SQL 脚本表（计划中）

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 脚本唯一标识 |
| `tenant_id` | BIGINT | NOT NULL, INDEXED | 租户 ID |
| `name` | VARCHAR(255) | NOT NULL | 脚本名称 |
| `description` | TEXT | | 脚本描述 |
| `resource_id` | BIGINT | FK → system.resources | 关联数据源 |
| `script_type` | VARCHAR(50) | NOT NULL | 脚本类型：query/ddl/dml/procedure |
| `content` | TEXT | NOT NULL | SQL 脚本内容 |
| `current_version` | INTEGER | DEFAULT 1 | 当前版本号 |
| `is_shared` | BOOLEAN | DEFAULT false | 是否共享给其他用户 |
| `created_by` | BIGINT | FK → system.users | 创建者 ID |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 更新时间 |
| `deleted_at` | TIMESTAMP | | 软删除时间戳 |

**索引**:
- `idx_scripts_tenant` - 租户索引
- `idx_scripts_resource` - 数据源索引
- `idx_scripts_created_by` - 创建者索引

**Go 模型** (`internal/models/script.go`):

```go
type Script struct {
    ID             uint
    TenantID       uint
    Name           string
    Description    string
    ResourceID     uint
    ScriptType     string
    Content        string
    CurrentVersion int
    IsShared       bool
    CreatedBy      uint
    CreatedAt      time.Time
    UpdatedAt      time.Time
    DeletedAt      gorm.DeletedAt
}
```

---

#### 表 2: script_versions - 脚本版本表（计划中）

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|---------|
| `id` | BIGSERIAL | PRIMARY KEY | 版本记录 ID |
| `script_id` | BIGINT | NOT NULL, FK, INDEXED | 关联脚本 ID |
| `version_number` | INTEGER | NOT NULL | 版本号 |
| `content` | TEXT | NOT NULL | 脚本内容快照 |
| `change_log` | TEXT | | 变更日志 |
| `created_by` | BIGINT | FK → system.users | 创建者 ID |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

**索引**:
- `idx_versions_script` - 脚本索引
- UNIQUE(script_id, version_number) - 唯一版本约束

**Go 模型** (`internal/models/script.go`):

```go
type ScriptVersion struct {
    ID            uint
    ScriptID      uint
    VersionNumber int
    Content       string
    ChangeLog     string
    CreatedBy     uint
    CreatedAt     time.Time
}
```

---

#### 表 3: executions - 执行历史表（计划中）

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 执行记录 ID |
| `tenant_id` | BIGINT | NOT NULL, INDEXED | 租户 ID |
| `script_id` | BIGINT | FK, INDEXED (nullable) | 关联脚本 ID（临时查询为 NULL） |
| `resource_id` | BIGINT | NOT NULL, FK | 关联数据源 |
| `sql_content` | TEXT | NOT NULL | 执行的 SQL 内容 |
| `execution_type` | VARCHAR(50) | NOT NULL | 执行类型：query/ddl/dml |
| `status` | VARCHAR(20) | NOT NULL | 状态：running/success/failed |
| `rows_affected` | INTEGER | | 影响行数 |
| `execution_time_ms` | INTEGER | | 执行时长（毫秒） |
| `result_preview` | JSONB | | 结果预览（前 100 行） |
| `result_cache_key` | VARCHAR(255) | | Redis 缓存键（完整结果） |
| `error_message` | TEXT | | 错误信息 |
| `executed_by` | BIGINT | FK → system.users | 执行者 ID |
| `executed_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 执行时间 |

**索引**:
- `idx_executions_tenant` - 租户索引
- `idx_executions_script` - 脚本索引
- `idx_executions_executed_by` - 执行者索引
- `idx_executions_executed_at` - 执行时间索引

**Go 模型** (`internal/models/execution.go`):

```go
type Execution struct {
    ID               uint
    TenantID         uint
    ScriptID         *uint
    ResourceID       uint
    SQLContent       string
    ExecutionType    string
    Status           string
    RowsAffected     *int
    ExecutionTimeMs  *int
    ResultPreview    JSONMap
    ResultCacheKey   string
    ErrorMessage     string
    ExecutedBy       uint
    ExecutedAt       time.Time
}
```

---

#### 表 4: script_dependencies - 脚本依赖表（计划中）

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 依赖记录 ID |
| `script_id` | BIGINT | NOT NULL, FK | 脚本 ID |
| `depends_on_script_id` | BIGINT | NOT NULL, FK | 依赖的脚本 ID |
| `dependency_type` | VARCHAR(50) | NOT NULL | 依赖类型：requires/before/after |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

**索引**:
- `idx_dependencies_script` - 脚本索引
- UNIQUE(script_id, depends_on_script_id) - 防止重复依赖

**Go 模型** (`internal/models/script.go`):

```go
type ScriptDependency struct {
    ID                 uint
    ScriptID           uint
    DependsOnScriptID  uint
    DependencyType     string
    CreatedAt          time.Time
}
```

---

### 2.2 数据表关系图

```
system.resources (来自 System 模块)
    ↓
develop.scripts (脚本定义)
    ↓ 1:N
develop.script_versions (版本历史)
    ↓ 1:N
develop.executions (执行记录)

develop.scripts
    ↓ N:N
develop.script_dependencies (脚本依赖关系)
```

---

## 3. API 端点清单

### 3.1 SQL 执行 API

#### POST /api/sql/execute - 执行 SQL 查询

**请求体**:

```json
{
  "resource_id": 1,
  "sql": "SELECT * FROM public.users LIMIT 10",
  "save_to_history": true
}
```

**响应** (200 OK):

```json
{
  "columns": ["id", "username", "email", "created_at"],
  "rows": [
    {"id": 1, "username": "admin", "email": "admin@example.com", "created_at": "2025-12-11T10:00:00Z"},
    {"id": 2, "username": "user1", "email": "user1@example.com", "created_at": "2025-12-11T11:00:00Z"}
  ],
  "total_rows": 2,
  "execution_time_ms": 45,
  "execution_id": 100
}
```

**响应** (400 Bad Request):

```json
{
  "error": "SQL syntax error",
  "details": "ERROR: syntax error at or near \"SELEC\""
}
```

---

#### POST /api/sql/test-connection - 测试数据库连接

**请求体**:

```json
{
  "resource_id": 1
}
```

**响应** (200 OK):

```json
{
  "success": true,
  "message": "连接测试成功",
  "database_version": "PostgreSQL 15.3",
  "connection_time_ms": 120
}
```

**响应** (400 Bad Request):

```json
{
  "success": false,
  "error": "连接失败",
  "details": "dial tcp: connection refused"
}
```

---

#### POST /api/sql/format - 格式化 SQL

**请求体**:

```json
{
  "sql": "select * from users where id=1"
}
```

**响应** (200 OK):

```json
{
  "formatted_sql": "SELECT *\nFROM users\nWHERE id = 1"
}
```

---

### 3.2 脚本管理 API（Phase 2 计划中）

#### POST /api/scripts - 创建脚本

**请求体**:

```json
{
  "name": "用户统计查询",
  "description": "统计每日新增用户数",
  "resource_id": 1,
  "script_type": "query",
  "content": "SELECT DATE(created_at), COUNT(*) FROM users GROUP BY DATE(created_at)",
  "is_shared": false
}
```

**响应** (201 Created): 返回 Script 对象

---

#### GET /api/scripts - 列出脚本

**查询参数**:
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 10）
- `resource_id`: 按数据源过滤
- `script_type`: 按类型过滤

**响应** (200 OK):

```json
{
  "scripts": [
    {
      "id": 1,
      "name": "用户统计查询",
      "description": "统计每日新增用户数",
      "resource_id": 1,
      "script_type": "query",
      "current_version": 3,
      "is_shared": false,
      "created_at": "2025-12-11T10:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 10
}
```

---

#### GET /api/scripts/:id - 获取脚本详情

**响应** (200 OK): 返回 Script 对象（包含完整内容）

---

#### PUT /api/scripts/:id - 更新脚本

**请求体**:

```json
{
  "name": "用户统计查询 v2",
  "content": "SELECT DATE(created_at), COUNT(*) FROM users WHERE deleted_at IS NULL GROUP BY DATE(created_at)",
  "change_log": "添加软删除过滤条件"
}
```

**响应** (200 OK): 返回更新后的 Script 对象

**说明**: 自动创建新版本记录

---

#### DELETE /api/scripts/:id - 删除脚本

**响应** (200 OK):

```json
{
  "message": "脚本删除成功"
}
```

**说明**: 软删除，保留历史版本

---

#### GET /api/scripts/:id/versions - 获取脚本版本历史

**响应** (200 OK):

```json
{
  "versions": [
    {
      "id": 10,
      "version_number": 3,
      "change_log": "添加软删除过滤条件",
      "created_by": 2,
      "created_at": "2025-12-11T15:00:00Z"
    },
    {
      "id": 9,
      "version_number": 2,
      "change_log": "优化 GROUP BY 性能",
      "created_by": 2,
      "created_at": "2025-12-11T14:00:00Z"
    }
  ],
  "total": 3
}
```

---

#### POST /api/scripts/:id/rollback - 回滚到指定版本

**请求体**:

```json
{
  "version_number": 2
}
```

**响应** (200 OK):

```json
{
  "message": "已回滚到版本 2",
  "current_version": 4
}
```

**说明**: 回滚操作会创建新版本（版本 4），内容为版本 2 的快照

---

### 3.3 执行历史 API（Phase 2 计划中）

#### GET /api/executions - 列出执行历史

**查询参数**:
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 20）
- `resource_id`: 按数据源过滤
- `script_id`: 按脚本过滤
- `status`: 按状态过滤

**响应** (200 OK):

```json
{
  "executions": [
    {
      "id": 100,
      "script_id": 1,
      "resource_id": 1,
      "execution_type": "query",
      "status": "success",
      "rows_affected": 50,
      "execution_time_ms": 45,
      "executed_by": 2,
      "executed_at": "2025-12-11T16:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

---

#### GET /api/executions/:id - 获取执行详情

**响应** (200 OK):

```json
{
  "id": 100,
  "script_id": 1,
  "resource_id": 1,
  "sql_content": "SELECT * FROM users LIMIT 10",
  "execution_type": "query",
  "status": "success",
  "rows_affected": 10,
  "execution_time_ms": 45,
  "result_preview": {
    "columns": ["id", "username", "email"],
    "rows": [{"id": 1, "username": "admin", "email": "admin@example.com"}]
  },
  "executed_by": 2,
  "executed_at": "2025-12-11T16:00:00Z"
}
```

---

#### GET /api/executions/:id/download - 下载完整结果

**查询参数**:
- `format`: 下载格式（csv/json/excel）

**响应** (200 OK):
- Content-Type: `text/csv` 或 `application/json` 或 `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`
- Body: 导出文件

**说明**: 从 Redis 缓存或重新执行获取完整结果

---

#### DELETE /api/executions/:id - 删除执行记录

**响应** (200 OK):

```json
{
  "message": "执行记录删除成功"
}
```

---

### 3.4 基础设施 API

#### GET /health - 健康检查

**响应** (200 OK):

```json
{
  "status": "ok"
}
```

---

## 4. 服务层架构

### 4.1 核心 Service 类

#### SQLExecutor - SQL 执行服务

**文件**: `internal/service/sql_executor.go`

```go
type SQLExecutor struct {
    systemClient *commonClient.SystemClient
    connPool     *ConnectionPool
    resultCache  *ResultCache
}

// ExecuteSQL 执行 SQL 查询
func (s *SQLExecutor) ExecuteSQL(ctx context.Context, req *ExecuteSQLRequest) (*ExecutionResult, error)

// TestConnection 测试数据库连接
func (s *SQLExecutor) TestConnection(ctx context.Context, resourceID uint) (*ConnectionTestResult, error)

// FormatSQL 格式化 SQL
func (s *SQLExecutor) FormatSQL(sql string) (string, error)
```

**核心功能**:

1. **动态连接管理**:
   - 从 System 模块获取数据源配置
   - 按 resource_id 缓存连接池（每个数据源独立连接池）
   - 连接超时自动关闭（5 分钟空闲）

2. **查询执行**:
   - 支持 SELECT/INSERT/UPDATE/DELETE/DDL
   - 自动检测查询类型（query/dml/ddl）
   - 限制 SELECT 结果最大行数（默认 10000 行）
   - 执行超时保护（默认 30 秒）

3. **结果缓存**:
   - 完整结果存储到 Redis（TTL: 1 小时）
   - 响应只返回前 100 行预览
   - 缓存键格式: `develop:execution:{execution_id}`

4. **错误处理**:
   - 捕获 SQL 语法错误
   - 捕获权限错误
   - 捕获连接错误

---

#### ConnectionPool - 连接池管理

**文件**: `internal/service/connection_pool.go`

```go
type ConnectionPool struct {
    pools      map[uint]*sql.DB
    mu         sync.RWMutex
    systemClient *commonClient.SystemClient
}

// GetConnection 获取或创建数据库连接
func (p *ConnectionPool) GetConnection(resourceID uint) (*sql.DB, error)

// CloseConnection 关闭指定资源的连接
func (p *ConnectionPool) CloseConnection(resourceID uint)

// CloseAll 关闭所有连接
func (p *ConnectionPool) CloseAll()
```

**连接池配置**:
- 最大空闲连接: 5
- 最大打开连接: 10
- 连接最大生命周期: 5 分钟
- 连接最大空闲时间: 5 分钟

---

#### ResultCache - 结果缓存服务（计划中）

**文件**: `internal/service/result_cache.go`

```go
type ResultCache struct {
    redisClient *redis.Client
}

// CacheResult 缓存查询结果
func (r *ResultCache) CacheResult(executionID uint, result interface{}) error

// GetResult 获取缓存的结果
func (r *ResultCache) GetResult(executionID uint) (interface{}, error)

// DeleteResult 删除缓存结果
func (r *ResultCache) DeleteResult(executionID uint) error
```

---

#### ScriptService - 脚本管理服务（Phase 2 计划中）

**文件**: `internal/service/script_service.go`

```go
type ScriptService struct {
    repo *ScriptRepository
}

// CreateScript 创建脚本
func (s *ScriptService) CreateScript(ctx context.Context, req *CreateScriptRequest) (*Script, error)

// UpdateScript 更新脚本（自动创建版本）
func (s *ScriptService) UpdateScript(ctx context.Context, scriptID uint, req *UpdateScriptRequest) (*Script, error)

// RollbackScript 回滚到指定版本
func (s *ScriptService) RollbackScript(ctx context.Context, scriptID uint, versionNumber int) (*Script, error)
```

---

### 4.2 SQL 执行流程

```
前端 Monaco Editor
  ↓
POST /api/sql/execute
  ↓
SQLExecutor.ExecuteSQL()
  ↓
├─ 获取数据源配置 (SystemClient → System API)
├─ 获取或创建连接 (ConnectionPool)
├─ 解析 SQL 类型 (query/dml/ddl)
├─ 执行 SQL (带超时保护)
├─ 限制结果集大小 (最大 10000 行)
├─ 缓存完整结果到 Redis (ResultCache)
├─ 创建执行记录 (ExecutionRepository, Phase 2)
└─ 返回结果预览 (前 100 行)
  ↓
响应返回前端
```

---

### 4.3 前端架构

**Monaco Editor 集成**:

- **组件**: `develop/frontend/src/components/SQLEditor.vue`
- **功能**:
  - SQL 语法高亮
  - 代码自动补全（关键词、表名、字段名）
  - 快捷键支持（Ctrl+Enter 执行、Ctrl+K Ctrl+F 格式化）
  - 主题切换（亮色/暗色）
- **库**: `monaco-editor` (4.0.0+)

**结果展示**:

- **组件**: `develop/frontend/src/components/ResultTable.vue`
- **功能**:
  - 虚拟滚动（支持大量数据）
  - 列宽自适应
  - 复制单元格/整行
  - 导出 CSV/JSON/Excel
- **库**: `element-plus` Table 组件 + `xlsx` 导出

**连接管理**:

- **组件**: `develop/frontend/src/components/ConnectionSelector.vue`
- **功能**:
  - 从 System 获取数据源列表
  - 连接状态指示
  - 快速切换数据源

---

## 5. 配置参数

### 5.1 环境变量清单

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `PORT` | 8085 | 服务端口 |
| `DB_HOST` | localhost | PostgreSQL 主机 |
| `DB_PORT` | 5432 | PostgreSQL 端口 |
| `DB_USER` | addp | 数据库用户 |
| `DB_PASSWORD` | addp_password | 数据库密码 |
| `DB_NAME` | addp | 数据库名 |
| `DB_SCHEMA` | develop | Develop schema 名 |
| `REDIS_ADDR` | localhost:6379 | Redis 地址（结果缓存） |
| `REDIS_PASSWORD` | - | Redis 密码（可选） |
| `SYSTEM_SERVICE_URL` | http://localhost:8080 | System 服务 URL |
| `SQL_EXECUTION_TIMEOUT` | 30 | SQL 执行超时（秒） |
| `MAX_RESULT_ROWS` | 10000 | 最大结果行数 |
| `RESULT_CACHE_TTL` | 3600 | 结果缓存 TTL（秒） |
| `CONNECTION_MAX_LIFETIME` | 300 | 连接最大生命周期（秒） |
| `CONNECTION_MAX_IDLE_TIME` | 300 | 连接最大空闲时间（秒） |

---

### 5.2 连接池配置

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `MaxIdleConns` | 5 | 最大空闲连接数 |
| `MaxOpenConns` | 10 | 最大打开连接数 |
| `ConnMaxLifetime` | 5m | 连接最大生命周期 |
| `ConnMaxIdleTime` | 5m | 连接最大空闲时间 |

---

### 5.3 SQL 执行限制

| 限制类型 | 默认值 | 说明 |
|---------|--------|------|
| 执行超时 | 30 秒 | 单次查询最大执行时间 |
| 结果行数 | 10000 | SELECT 查询最大返回行数 |
| 结果预览 | 100 | 响应中返回的预览行数 |
| 缓存 TTL | 1 小时 | Redis 结果缓存有效期 |

---

## 6. 关键文件路径

| 文件 | 路径 | 说明 |
|------|------|------|
| 路由配置 | `develop/backend/internal/api/router.go` | API 端点定义 |
| SQL 执行服务 | `develop/backend/internal/service/sql_executor.go` | 核心执行逻辑 |
| 连接池管理 | `develop/backend/internal/service/connection_pool.go` | 数据库连接池 |
| 结果缓存 | `develop/backend/internal/service/result_cache.go` | Redis 结果缓存 |
| 脚本模型 | `develop/backend/internal/models/script.go` | 脚本和版本模型 |
| 执行模型 | `develop/backend/internal/models/execution.go` | 执行记录模型 |
| Monaco 编辑器 | `develop/frontend/src/components/SQLEditor.vue` | SQL 编辑器组件 |
| 结果展示 | `develop/frontend/src/components/ResultTable.vue` | 结果表格组件 |

---

## 7. 关键特性和限制

### 7.1 已实现特性

- ✅ **SQL 查询执行**: 支持 SELECT/INSERT/UPDATE/DELETE/DDL
- ✅ **连接测试**: 验证数据库连接有效性
- ✅ **动态连接管理**: 自动从 System 获取数据源配置
- ✅ **连接池**: 每个数据源独立连接池，自动超时清理
- ✅ **Monaco Editor**: 语法高亮、代码补全、格式化
- ✅ **结果限制**: 防止大结果集导致内存溢出
- ✅ **超时保护**: 防止长时间运行的查询阻塞

### 7.2 计划中的特性（Phase 2）

- 📋 **脚本保存**: SQL 脚本持久化存储
- 📋 **版本控制**: 脚本版本历史和回滚
- 📋 **执行历史**: 查询历史记录和结果缓存
- 📋 **脚本依赖**: 脚本间依赖关系管理
- 📋 **结果导出**: CSV/JSON/Excel 格式导出
- 📋 **智能补全**: 基于数据库元数据的代码补全
- 📋 **执行计划**: EXPLAIN 查询性能分析
- 📋 **事务支持**: BEGIN/COMMIT/ROLLBACK 事务控制

### 7.3 已知限制

- ⚠️ **单租户连接**: 每个数据源只维护一个连接池（不区分租户）
- ⚠️ **无权限控制**: Phase 1 未实现表级、列级权限控制
- ⚠️ **无审计日志**: 未记录 SQL 执行审计（计划在 Phase 2 实现）
- ⚠️ **结果大小限制**: 最大 10000 行，超出会被截断
- ⚠️ **无 DDL 保护**: 允许执行 DROP/TRUNCATE 等危险操作（需前端警告）

---

## 8. 相关文档

- [ADDP 平台架构文档](../CLAUDE.md)
- [Develop 模块详细文档](README.md)
- [System 模块数据结构文档](../system/DATA_STRUCTURES.md)
- [Manager 模块数据结构文档](../manager/DATA_STRUCTURES.md)
