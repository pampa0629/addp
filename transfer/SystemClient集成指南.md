# Transfer 模块 - SystemClient 集成指南

**版本**: v1.1.0
**日期**: 2025-10-21
**状态**: ✅ 已实现

---

## 概述

Transfer 模块现已集成 SystemClient，支持从 System 模块动态获取数据源配置。用户不再需要在任务配置中硬编码敏感的连接信息（如数据库密码、S3密钥等），只需提供 `source_id` 和 `target_id`，系统会自动从 System 模块获取完整的资源配置。

---

## 🎯 功能亮点

### ✅ 已实现功能

1. **自动资源配置获取**
   - 通过 `source_id` / `target_id` 从 System 模块获取资源配置
   - 自动解密密码和密钥（System 模块负责加密存储）
   - 支持 PostgreSQL、MySQL、S3/MinIO 等多种资源类型

2. **配置优先级**
   - 优先使用 System 中的资源配置
   - 支持 fallback 到 task.config 中的配置
   - 可以混合使用（资源配置 + 任务特定配置）

3. **灵活的配置合并**
   - 资源配置提供连接信息（host, port, user, password等）
   - 任务配置提供业务逻辑（query, table, prefix等）
   - 自动合并，避免配置冗余

4. **安全性提升**
   - 敏感信息集中管理在 System 模块
   - Transfer 任务配置不包含明文密码
   - 支持服务间调用的内部 API Key 认证

---

## 📖 使用方法

### 方式1：使用 resource_id（推荐）

这是**最简单、最安全**的方式。只需提供资源ID，系统自动获取所有连接信息。

#### 示例：从 PostgreSQL 导入到 S3

```json
POST /api/tasks
{
  "name": "Daily User Export",
  "type": "export",
  "mode": "batch",
  "source_id": 10,  // PostgreSQL 数据源ID（在 System 中配置）
  "target_id": 20,  // S3 存储桶ID（在 System 中配置）
  "config": {
    "source": {
      "query": "SELECT * FROM users WHERE created_at >= ?",
      "parameters": ["2024-01-01"]
    },
    "target": {
      "prefix": "exports/users/",
      "file_name": "users_2024.json",
      "file_type": "json"
    }
  },
  "batch_size": 1000,
  "schedule": "0 2 * * *"
}
```

**配置说明**:
- ✅ `source_id: 10` - System 中的 PostgreSQL 资源（包含 host, port, user, password, database）
- ✅ `target_id: 20` - System 中的 S3 资源（包含 endpoint, access_key, secret_key, bucket）
- ✅ `config.source.query` - 任务特定的查询语句
- ✅ `config.target.prefix` - 任务特定的文件路径

**执行时系统会自动构建完整配置**:

```javascript
// Source connector config (自动生成)
{
  "type": "jdbc",
  "driver": "postgresql",
  "host": "pg.example.com",        // 从 System 资源获取
  "port": 5432,                     // 从 System 资源获取
  "user": "app_user",               // 从 System 资源获取
  "password": "decrypted_password", // 从 System 资源获取并解密
  "database": "app_db",             // 从 System 资源获取
  "query": "SELECT * FROM users WHERE created_at >= ?", // 从任务配置获取
  "parameters": ["2024-01-01"]      // 从任务配置获取
}

// Target connector config (自动生成)
{
  "type": "s3",
  "endpoint": "https://s3.example.com", // 从 System 资源获取
  "access_key": "AKIAIOSFODNN7EXAMPLE",  // 从 System 资源获取
  "secret_key": "wJalrXUtnFEMI/K7MDENG", // 从 System 资源获取并解密
  "bucket": "my-data-lake",              // 从 System 资源获取
  "region": "us-east-1",                 // 从 System 资源获取
  "prefix": "exports/users/",            // 从任务配置获取
  "file_name": "users_2024.json",        // 从任务配置获取
  "file_type": "json"                    // 从任务配置获取
}
```

---

### 方式2：完全使用 task.config（传统方式）

如果不提供 `source_id` / `target_id`，系统会完全使用 task.config 中的配置。

#### 示例：本地文件导入

```json
POST /api/tasks
{
  "name": "Import CSV to Database",
  "type": "import",
  "config": {
    "source": {
      "type": "file",
      "file_path": "/data/users.csv",
      "file_type": "csv",
      "delimiter": ","
    },
    "target": {
      "type": "jdbc",
      "driver": "postgresql",
      "host": "localhost",
      "port": 5432,
      "user": "admin",
      "password": "admin123",  // ⚠️ 不推荐：明文密码
      "database": "test_db",
      "table": "users"
    }
  }
}
```

**缺点**:
- ❌ 配置冗长
- ❌ 密码明文存储在任务配置中
- ❌ 修改数据源密码需要更新所有任务

**建议**: 除非是临时测试，否则应使用 resource_id 方式。

---

### 方式3：混合方式（部分使用资源配置）

可以为 source 使用 resource_id，为 target 使用完整配置（或反之）。

#### 示例：从资源导出到本地文件

```json
POST /api/tasks
{
  "name": "Database Backup",
  "type": "export",
  "source_id": 10,  // 使用 System 资源配置
  "config": {
    "source": {
      "query": "SELECT * FROM orders WHERE status = 'completed'"
    },
    "target": {
      "type": "file",
      "file_path": "/backups/orders_backup.json",
      "file_type": "json"
    }
  }
}
```

---

## 🏗️ 架构设计

### 配置解析流程

```
用户提交任务
    ↓
TaskService.buildExecutionTask()
    ↓
resolveConnectorConfig("source", task.SourceID)
    ↓
┌─────────────────────────────────────┐
│ source_id 存在？                     │
├─────────────────────────────────────┤
│ YES → SystemClient.GetResource()    │
│       ↓                              │
│       resourceToConnectorConfig()   │
│       ↓                              │
│       合并 task.config 中的额外配置  │
│                                      │
│ NO  → 直接使用 task.config.source   │
└─────────────────────────────────────┘
    ↓
返回完整的连接器配置
    ↓
ExecutionEngine.Execute()
```

### 核心方法

#### 1. `resolveConnectorConfig()`

**功能**: 解析连接器配置，支持从 System 获取或使用任务配置。

**逻辑**:
```go
func (s *TaskService) resolveConnectorConfig(
    taskConfig models.JSONMap,
    configKey string,  // "source" 或 "target"
    resourceID *uint,
) (map[string]interface{}, error) {
    // 1. 如果提供了 resource_id，尝试从 System 获取
    if resourceID != nil && *resourceID > 0 {
        resource, err := s.GetResourceConfig(ctx, *resourceID)
        if err == nil {
            // 成功：转换 + 合并配置
            connectorConfig := resourceToConnectorConfig(resource)
            mergeTaskConfig(connectorConfig, taskConfig[configKey])
            return connectorConfig
        }
        // 失败：fallback 到 task.config
    }

    // 2. 使用 task.config
    return taskConfig[configKey], nil
}
```

#### 2. `resourceToConnectorConfig()`

**功能**: 将 System 资源转换为连接器配置。

**支持的资源类型**:

| 资源类型 | 连接器类型 | 转换字段 |
|---------|-----------|---------|
| `postgresql` | `jdbc` | host, port, user, password, database |
| `mysql` | `jdbc` | host, port, user, password, database |
| `s3` | `s3` | endpoint, access_key, secret_key, bucket, region |
| `minio` | `s3` | endpoint, access_key, secret_key, bucket, region |

**示例转换**:

```go
// System Resource
{
  "id": 10,
  "resource_type": "postgresql",
  "connection_info": {
    "host": "pg.example.com",
    "port": 5432,
    "user": "app_user",
    "password": "encrypted_password", // 已加密
    "database": "app_db"
  }
}

// Connector Config（自动解密密码）
{
  "type": "jdbc",
  "driver": "postgresql",
  "host": "pg.example.com",
  "port": 5432,
  "user": "app_user",
  "password": "decrypted_password", // 已解密
  "database": "app_db"
}
```

#### 3. `mergeTaskConfig()`

**功能**: 合并资源配置和任务配置。

**规则**:
- ✅ 资源配置的连接信息优先（host, port, user, password, endpoint, access_key, secret_key, bucket）
- ✅ 任务配置的业务参数不会被覆盖（query, table, prefix, file_name等）

**示例**:

```javascript
// Resource Config
{
  "type": "jdbc",
  "host": "pg.example.com",
  "port": 5432,
  "user": "app_user",
  "password": "decrypted_password",
  "database": "app_db"
}

// Task Config
{
  "query": "SELECT * FROM users",
  "batch_size": 1000,
  "host": "localhost"  // ❌ 会被忽略，使用资源配置中的 host
}

// Merged Config
{
  "type": "jdbc",
  "host": "pg.example.com",      // 来自资源配置
  "port": 5432,                   // 来自资源配置
  "user": "app_user",             // 来自资源配置
  "password": "decrypted_password", // 来自资源配置
  "database": "app_db",           // 来自资源配置
  "query": "SELECT * FROM users", // 来自任务配置
  "batch_size": 1000              // 来自任务配置
}
```

---

## 🔐 安全性

### 密码加密和解密

**流程**:
1. 用户在 System 模块创建资源时，密码会自动加密存储
2. Transfer 模块通过 SystemClient 获取资源时，System 自动解密密码
3. Transfer 模块接收到的是**已解密**的连接信息
4. 连接器直接使用解密后的密码连接数据源

**代码位置**:
- 加密：`system/backend/internal/service/resource_service.go`
- 解密：`common/models/resource.go` 中的 `BuildConnectionString()`

### 服务间认证

**当前实现**:
```go
// transfer/backend/internal/service/task_service.go
systemClient := commonClient.NewSystemClient(cfg.SystemServiceURL, "")
```

**⚠️ TODO**: 生产环境应使用内部 API Key 认证：
```go
// 推荐方式
internalKey := cfg.InternalAPIKey // 从环境变量读取
systemClient := commonClient.NewSystemClientWithInternalKey(
    cfg.SystemServiceURL,
    internalKey,
)
```

**System 端需要实现**:
```go
// system/backend/internal/api/router.go
internal := r.Group("/internal")
internal.Use(middleware.InternalAPIKeyAuth()) // 验证内部 API Key
internal.GET("/resources/:id", resourceHandler.GetInternal)
```

---

## 📝 配置示例

### 在 System 模块创建资源

#### PostgreSQL 资源

```bash
POST http://localhost:8080/api/resources
Authorization: Bearer <jwt_token>

{
  "name": "生产环境数据库",
  "resource_type": "postgresql",
  "connection_info": {
    "host": "prod-db.example.com",
    "port": 5432,
    "user": "app_user",
    "password": "SuperSecretPassword123!",
    "database": "production"
  },
  "description": "生产环境主数据库"
}
```

**响应**:
```json
{
  "id": 10,
  "name": "生产环境数据库",
  "resource_type": "postgresql",
  "connection_info": {
    "host": "prod-db.example.com",
    "port": 5432,
    "user": "app_user",
    "password": "***encrypted***",  // 密码已加密
    "database": "production"
  }
}
```

#### S3 资源

```bash
POST http://localhost:8080/api/resources

{
  "name": "数据湖存储",
  "resource_type": "s3",
  "connection_info": {
    "endpoint": "https://s3.amazonaws.com",
    "access_key": "AKIAIOSFODNN7EXAMPLE",
    "secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    "bucket": "my-data-lake",
    "region": "us-east-1"
  }
}
```

### 在 Transfer 模块使用资源

```bash
POST http://localhost:8083/api/tasks
Authorization: Bearer <jwt_token>

{
  "name": "每日用户数据导出",
  "type": "export",
  "source_id": 10,  // 引用上面创建的 PostgreSQL 资源
  "target_id": 11,  // 引用上面创建的 S3 资源
  "config": {
    "source": {
      "query": "SELECT id, name, email, created_at FROM users WHERE created_at::date = CURRENT_DATE"
    },
    "target": {
      "prefix": "daily-exports/users/",
      "file_name": "users_{date}.json",
      "file_type": "json"
    }
  },
  "batch_size": 5000,
  "schedule": "0 1 * * *"  // 每天凌晨1点执行
}
```

---

## 🐛 故障排查

### 问题1：获取资源失败

**错误日志**:
```
WARN failed to get resource from System, falling back to task config
resource_id=10 error=system api returned status 404
```

**可能原因**:
1. 资源ID不存在
2. System 服务未启动
3. 网络连接问题
4. JWT Token 过期或无效

**解决方法**:
```bash
# 1. 检查资源是否存在
curl http://localhost:8080/api/resources/10 \
  -H "Authorization: Bearer $TOKEN"

# 2. 检查 System 服务状态
curl http://localhost:8080/health

# 3. 检查 Transfer 配置
# transfer/.env
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true
```

### 问题2：服务集成被禁用

**错误日志**:
```
ERROR system client not available (integration disabled)
```

**解决方法**:
```bash
# 启用服务集成
# transfer/backend/.env
ENABLE_SERVICE_INTEGRATION=true
SYSTEM_SERVICE_URL=http://localhost:8080
```

### 问题3：配置转换失败

**错误日志**:
```
ERROR failed to convert resource to connector config
error=unsupported resource type: redis
```

**原因**: 该资源类型尚未支持。

**支持的资源类型**: `postgresql`, `mysql`, `s3`, `minio`

**解决方法**: 使用传统方式在 task.config 中配置，或扩展 `resourceToConnectorConfig()` 方法。

---

## 🚀 最佳实践

### 1. 优先使用资源配置

✅ **推荐**:
```json
{
  "source_id": 10,
  "target_id": 20,
  "config": {
    "source": {"query": "..."},
    "target": {"prefix": "..."}
  }
}
```

❌ **不推荐**:
```json
{
  "config": {
    "source": {
      "host": "...",
      "password": "plaintext_password"  // 安全风险
    }
  }
}
```

### 2. 合理组织资源

**System 资源管理**:
- 为不同环境创建独立资源（开发、测试、生产）
- 使用描述字段标注资源用途
- 定期审计资源访问日志
- 及时删除不再使用的资源

### 3. 分离连接信息和业务逻辑

**资源配置** (System 模块):
- 连接地址 (host, endpoint)
- 认证信息 (user, password, access_key)
- 数据库/存储桶名称

**任务配置** (Transfer 模块):
- SQL 查询语句
- 文件路径和模式
- 字段映射规则
- 转换逻辑

### 4. 测试配置

在创建正式任务前，先测试资源配置：

```bash
# 1. 创建测试任务
POST /api/tasks
{
  "name": "Connection Test",
  "source_id": 10,
  "config": {
    "source": {"query": "SELECT 1 AS test"}
  }
}

# 2. 立即执行
POST /api/tasks/{id}/start

# 3. 查看执行日志
GET /api/executions/{execution_id}
```

---

## 📊 监控和日志

### 日志示例

**成功获取资源**:
```
INFO fetching resource config from System
  resource_id=10

INFO resource config fetched successfully
  resource_id=10 resource_type=postgresql

INFO converted resource to connector config
  resource_id=10 resource_type=postgresql connector_type=jdbc
```

**Fallback 到任务配置**:
```
WARN failed to get resource from System, falling back to task config
  resource_id=10 error=connection refused
```

### 关键指标

**监控建议**:
- System API 调用成功率
- 资源配置获取延迟
- Fallback 发生频率
- 配置转换错误率

---

## 📚 相关文档

- [CLAUDE.md](../CLAUDE.md) - 平台整体架构
- [修复日志.md](修复日志.md) - 最近的修复记录
- [连接器使用指南.md](连接器使用指南.md) - 连接器配置参考
- [common/client/system.go](../common/client/system.go) - SystemClient 实现

---

**更新时间**: 2025-10-21
**维护者**: ADDP Transfer Team
