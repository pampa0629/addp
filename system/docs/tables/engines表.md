# Engines 表结构和 API 说明

## 一、表结构概览

`system.engines` 表是 ADDP 平台的核心表，用于管理所有类型的数据引擎（数据库、对象存储、计算引擎等）。采用多租户隔离设计，支持灵活的能力声明和扩展机制。

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 主键，自增ID |
| `tenant_id` | INTEGER | NULLABLE, INDEX | 租户ID，NULL表示系统级引擎（仅SuperAdmin可见） |
| `name` | VARCHAR(255) | NOT NULL, INDEX | 显示名称（中文或英文） |
| `engine_type` | VARCHAR(255) | NOT NULL, INDEX | 引擎类型（postgresql/mysql/python_workflow等） |
| `engine_origin` | VARCHAR(50) | NOT NULL, DEFAULT 'general' | 引擎来源：general（通用）/extension（扩展） |
| `connection_info` | JSON | NOT NULL | 连接信息（敏感字段加密） |
| `description` | TEXT | | 描述信息 |
| `is_active` | BOOLEAN | DEFAULT true | 是否激活 |
| `created_by` | INTEGER | | 创建者ID |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |

### 2.2 扩展引擎字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `is_builtin` | BOOLEAN | DEFAULT false, INDEX | 是否为内置引擎（内置引擎不可删除） |
| `capabilities` | JSONB | | 引擎自身 native / provider 能力声明 |

**注意**：
- `extension_api_config` 和 `health_check_config` 字段已废弃（数据库字段保留但不使用）
- 工作流引擎的 API 配置已标准化到代码中（见 `common/models/workflow_standards.go`）

### 2.3 连接状态缓存字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `connection_status` | VARCHAR(20) | DEFAULT 'unknown', INDEX | 连接状态：online/offline/unknown/checking |
| `last_check_at` | TIMESTAMP | | 上次检测时间 |
| `check_message` | TEXT | | 检测结果消息 |

### 2.4 数据库索引

| 索引名 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `idx_engines_name` | `name` | 普通索引 | 按名称查询 |
| `idx_engines_type` | `engine_type` | 普通索引 | 按类型查询 |
| `idx_engines_tenant` | `tenant_id` | 普通索引 | 租户隔离 |
| `idx_engines_origin` | `engine_origin` | 普通索引 | 按来源查询 |
| `idx_engines_builtin` | `is_builtin` | 普通索引 | 查询内置引擎 |
| `idx_engines_connection_status` | `connection_status` | 普通索引 | 按连接状态查询 |
| `idx_builtin_engine_type` | `engine_type` | 唯一索引（WHERE is_builtin=true） | 内置引擎类型唯一 |

---

## 三、引擎来源说明

### 3.1 General 引擎（通用引擎）

**定义**：直接连接的数据库和存储引擎

**特点**：
- 通过驱动程序直接连接（JDBC、Go driver等）
- `connection_info` 包含主机、端口、用户名、密码
- 不需要 `extension_api_config`

**典型类型**：
- 数据库：`postgresql`、`mysql`、`doris`、`clickhouse`、`mongodb`、`spark_sql`
- 对象存储：`minio`、`s3`

**示例**：
```json
{
  "name": "生产PostgreSQL",
  "engine_type": "postgresql",
  "engine_origin": "general",
  "connection_info": {
    "host": "localhost",
    "port": 5432,
    "username": "admin",
    "password": "[ENCRYPTED]",
    "database": "production"
  }
}
```

### 3.2 Extension 引擎（扩展引擎）

**定义**：通过 HTTP API 调用的外部服务引擎

**特点**：
- 通过 HTTP API 调用
- `connection_info` 包含 `protocol`、`host`、`port`
- API 端点配置已标准化到代码中（`common/models/workflow_standards.go`）

**典型类型**：
- 工作流引擎：`python_workflow`、`spark_workflow`
- Notebook引擎：`jupyter`

**示例**：
```json
{
  "name": "Python工作流引擎",
  "engine_type": "python_workflow",
  "engine_origin": "extension",
  "connection_info": {
    "protocol": "http",
    "host": "localhost",
    "port": 8099
  },
  "capabilities": {
    "schema_version": "engine.capabilities/v1",
    "engine_type": "python_workflow",
    "engine_family": "workflow",
    "compute": {
      "workflow": {
        "supported": true,
        "runtime_api": "addp.workflow/v1",
        "dynamic_operators": true
      }
    }
  }
}
```

**API 标准配置**：工作流引擎的 API 端点在 `common/models/workflow_standards.go` 中定义，包括：
- `execute`: `POST /api/workflows/execute`
- `status`: `GET /api/workflows/status/{id}`
- `logs`: `GET /api/workflows/logs/{id}`
- `cancel`: `POST /api/workflows/cancel/{id}`
- 健康检查: `GET /health`

---

## 四、JSON 字段详细结构

### 4.1 ConnectionInfo（连接信息）

**安全特性**：敏感字段使用 **AES-256-GCM** 加密存储

#### 加密字段（自动处理）
- `password` - 数据库密码
- `access_key` - MinIO/S3 访问密钥
- `secret_key` - MinIO/S3 密钥
- `token` - API Token
- `api_key` - API 密钥

#### PostgreSQL 示例
```json
{
  "host": "localhost",
  "port": 5432,
  "username": "user",
  "password": "[ENCRYPTED]",
  "database": "mydb"
}
```

#### MinIO/S3 示例
```json
{
  "endpoint": "http://localhost:9000",
  "access_key": "[ENCRYPTED]",
  "secret_key": "[ENCRYPTED]",
  "bucket": "my-bucket",
  "use_ssl": false
}
```

#### Extension 引擎示例
```json
{
  "protocol": "http",
  "host": "localhost",
  "port": 8099
}
```

**注意**：工作流引擎的 API 端点和健康检查配置已标准化到代码中（`common/models/workflow_standards.go`），无需在数据库中配置。

---

### 4.2 Capabilities（能力声明）

**类型**：JSONB
**作用**：声明引擎自身具备、且可由 ADDP 统一消费的 native / provider 能力。

`system.engines.capabilities` 使用 `engine.capabilities/v1` 结构。内置引擎的事实来源是引擎插件的 `Capabilities()` 方法；System 服务启动时会基于当前插件体系刷新已注册引擎能力。Engine API 创建或更新引擎时，如果调用方未提供能力声明，System 会按 `engine_type` 生成当前结构；Registry 注册接口提交能力时也必须使用该结构。

**边界**：
- 该字段只表达引擎自身能力，例如 catalog、facts、store、query、workflow、script。
- 不表达 Transfer、Manager Preview、Meta 等 ADDP 模块对某个引擎的适配状态。
- 上层模块可以读取该字段形成自己的执行策略，但模块适配情况不写回该字段。
- 旧版 `storage[]` / `compute[]`、`dev_modes`、`supported_sources` 结构已不再兼容，发现后应刷新为当前结构。

#### 数据结构

```go
type EngineCapabilities struct {
    SchemaVersion string                 `json:"schema_version"`
    EngineType    string                 `json:"engine_type"`
    EngineFamily  string                 `json:"engine_family"`
    Storage       *StorageCapabilities   `json:"storage,omitempty"`
    Compute       *ComputeCapabilities   `json:"compute,omitempty"`
    Limits        map[string]interface{} `json:"limits,omitempty"`
    Extensions    map[string]interface{} `json:"extensions,omitempty"`
}

type StorageCapabilities struct {
    CatalogModel *CatalogModelSpec   `json:"catalog_model,omitempty"`
    Catalog      *CatalogCapability  `json:"catalog,omitempty"`
    Facts        *CatalogFactsCapability `json:"facts,omitempty"`
    Store        *StoreCapability    `json:"store,omitempty"`
    Semantics    []string            `json:"semantics,omitempty"`
    NotSupported []string            `json:"not_supported,omitempty"`
}

type ComputeCapabilities struct {
    Query    *QueryCapability    `json:"query,omitempty"`
    Workflow *WorkflowCapability `json:"workflow,omitempty"`
    Script   *ScriptCapability   `json:"script,omitempty"`
}
```

#### 顶层字段

| 字段 | 说明 |
|---|---|
| `schema_version` | 固定为 `engine.capabilities/v1` |
| `engine_type` | 引擎类型，如 `postgresql`、`mysql`、`python_workflow` |
| `engine_family` | 粗粒度引擎族，如 `tabular`、`object`、`file`、`dynamic_schema`、`graph`、`workflow`、`script` |
| `storage` | 存储、目录、catalog facts、内容访问能力 |
| `compute` | 查询、工作流、脚本或 Notebook 运行能力 |
| `limits` | 跨能力限制，有真实调用方时使用 |
| `extensions` | 引擎特有补充信息，不得替代核心字段 |

详细规范见 [ADDP 引擎能力声明规范](../../../docs/spec/addp引擎能力声明规范.md)。

#### PostgreSQL 示例
```json
{
  "schema_version": "engine.capabilities/v1",
  "engine_type": "postgresql",
  "engine_family": "tabular",
  "storage": {
    "catalog_model": {
      "path_version": "catalog.path/v1",
      "root_term": "server",
      "levels": [
        {"term": "schema", "kinds": ["schema"], "container": true, "i18n_key": "engine.term.schema"},
        {"term": "table", "kinds": ["table", "view", "materialized_view", "external_table"], "item": true, "i18n_key": "engine.term.table"}
      ]
    },
    "catalog": {"supported": true, "real_time": true, "system_filtering": true},
    "facts": {"supported": true, "field_info": true, "statistics": true, "indexes": true, "constraints": true, "spatial_facts": true, "native_facts": true},
    "store": {"batch_read": true, "table_read_session": true, "batch_write": true, "table_write_session": true, "table_write_prepare": true, "delete": true}
  },
  "compute": {
    "query": {
      "supported": true,
      "languages": ["sql"],
      "default_language": "sql",
      "result_kinds": ["table", "scalar"],
      "supports_explain": true,
      "supports_cancel": true
    }
  }
}
```

#### Python Workflow 示例
```json
{
  "schema_version": "engine.capabilities/v1",
  "engine_type": "python_workflow",
  "engine_family": "workflow",
  "compute": {
    "workflow": {
      "supported": true,
      "runtime_api": "addp.workflow/v1",
      "dynamic_operators": true
    }
  }
}
```

---

### ~~4.3 ExtensionAPIConfig（扩展引擎 API 配置）~~

**⚠️ 已废弃**：此字段已从数据库中删除。

**新方案**：工作流引擎的 API 端点配置已标准化到代码中（`common/models/workflow_standards.go`）。

**标准配置示例**（Python Workflow / Spark Workflow）：
```go
WorkflowStandards = map[string]WorkflowStandard{
    "python_workflow": {
        Endpoints: map[string]WorkflowEndpoint{
            "execute": {Method: "POST", Path: "/api/workflows/execute", Timeout: 300},
            "status":  {Method: "GET",  Path: "/api/workflows/status/{id}", Timeout: 10},
            "logs":    {Method: "GET",  Path: "/api/workflows/logs/{id}", Timeout: 10},
            "cancel":  {Method: "POST", Path: "/api/workflows/cancel/{id}", Timeout: 30},
        },
        HealthCheck: {Endpoint: "/health", Timeout: 5, Interval: 60},
    },
}
```

---

### 4.4 元数据扫描策略边界

System engine 表不保存元数据扫描策略。注册引擎时的扫描计划由 Console 调用 Meta 创建或更新 `meta.scan_tasks`；System 只保存 engine 身份、连接、能力、租户和生命周期等自身事实。

---

### ~~4.5 HealthCheckConfig（健康检查配置）~~

**⚠️ 已废弃**：此字段已从数据库中删除。

**新方案**：健康检查配置已标准化到代码中（`common/models/workflow_standards.go`），所有工作流引擎统一使用 `/health` 端点。

---

## 五、API 端点说明

### 5.1 外部 API（需要认证）

#### 列表查询
```
GET /api/engines?engine_type={type}&is_active={true|false}
```

**查询参数**：
- `engine_type`（可选）- 按类型过滤
- `is_active`（可选）- 按激活状态过滤

**响应示例**：
```json
{
  "code": 200,
  "data": [
    {
      "id": 1,
      "tenant_id": 1,
      "name": "生产PostgreSQL",
      "engine_type": "postgresql",
      "engine_origin": "general",
      "connection_info": {
        "host": "localhost",
        "port": 5432,
        "username": "admin",
        "password": "******",
        "database": "production"
      },
      "is_active": true,
      "connection_status": "online",
      "last_check_at": "2026-01-01T10:00:00Z"
    }
  ]
}
```

#### 获取单个引擎
```
GET /api/engines/:id
```

#### 创建引擎
```
POST /api/engines
Content-Type: application/json

{
  "name": "测试PostgreSQL",
  "engine_type": "postgresql",
  "connection_info": {
    "host": "localhost",
    "port": 5432,
    "username": "user",
    "password": "password",
    "database": "testdb"
  },
  "description": "测试环境数据库"
}
```

#### 更新引擎
```
PUT /api/engines/:id
Content-Type: application/json

{
  "name": "生产PostgreSQL（更新）",
  "description": "生产环境主数据库",
  "is_active": true
}
```

**注意**：
- 敏感字段如果传入 `"******"` 或 `"****"`，系统会保留原始加密值
- 内置引擎不允许修改核心配置（`name`、`connection_info`）

#### 删除引擎
```
DELETE /api/engines/:id
```

**限制**：
- 内置引擎（`is_builtin=true`）不可删除
- 只有管理员可以删除

#### 测试连接（创建前）
```
POST /api/engines/test-connection
Content-Type: application/json

{
  "engine_type": "postgresql",
  "connection_info": {
    "host": "localhost",
    "port": 5432,
    "username": "user",
    "password": "password",
    "database": "testdb"
  }
}
```

#### 测试连接（已创建）
```
POST /api/engines/:id/test
```

请求体可选。编辑页面可传入当前表单中的 `connection_info`，用于测试尚未保存的连接配置；接口仍会同步更新该引擎的 `connection_status`、`last_check_at` 和 `check_message`。

---

### 5.2 内部 API（服务间调用）

#### 内部列表查询（解密）
```
GET /internal/engines?engine_type={type}&tenant_id={id}
```

**特性**：
- 自动解密 `connection_info`
- 无需用户认证
- 支持跨租户查询（SuperAdmin）

#### 内部获取单个（解密）
```
GET /internal/engines/:id
```

#### 注册能力
```
POST /internal/registry/capabilities
Content-Type: application/json

{
  "name": "Python工作流引擎",
  "engine_type": "python_workflow",
  "is_builtin": true,
  "capabilities": {
    "schema_version": "engine.capabilities/v1",
    "engine_type": "python_workflow",
    "engine_family": "workflow",
    "compute": {
      "workflow": {
        "supported": true,
        "runtime_api": "addp.workflow/v1",
        "dynamic_operators": true
      }
    }
  },
  "description": "基于 Python 的工作流引擎",
  "connection_info": {
    "protocol": "http",
    "host": "localhost",
    "port": 8099
  }
}
```

**注意**：API 端点配置已标准化到代码中，注册时无需提供。

#### 查询能力
```
GET /internal/registry/capabilities?filter={key}={value}
```

#### 查询计算引擎
```
GET /internal/registry/compute-engines
```

---

## 六、权限控制

### 6.1 权限模型

| 角色 | 查看 | 创建 | 修改 | 删除 |
|------|------|------|------|------|
| SuperAdmin | 所有引擎 | 租户级/系统级 | 所有引擎 | 非内置引擎 |
| TenantAdmin | 本租户 | 租户级 | 本租户 | 本租户非内置 |
| 普通用户 | 本租户 | ❌ | ❌ | ❌ |

### 6.2 租户隔离

**系统级引擎**（`tenant_id = NULL`）：
- 仅 SuperAdmin 可见和管理
- 用于平台级共享资源

**租户级引擎**（`tenant_id` 有值）：
- 本租户所有用户可见
- 仅 TenantAdmin 可管理

---

## 七、数据安全

### 7.1 敏感字段加密

**加密算法**：AES-256-GCM

**加密时机**：
- 创建引擎时
- 更新连接信息时

**解密时机**：
- 内部服务调用（`GetByIDInternal`, `ListInternal`）
- 测试连接（`GetForConnection`）

**脱敏显示**：
- 外部 API 查询 - 敏感字段显示为 `"******"`

### 7.2 占位符保护

更新引擎时，如果敏感字段值为：
- `"******"` 或 `"****"` → 保留原始加密值
- 真实新值 → 加密并保存

---

## 八、连接状态管理

### 8.1 状态值

| 状态 | 含义 |
|------|------|
| `online` | 连接成功 |
| `offline` | 连接失败 |
| `unknown` | 未检测（新创建） |
| `checking` | 检测中（预留） |

### 8.2 更新策略

**混合模式**：

1. **启动时检测**
   - System 服务启动时自动检测所有引擎
   - 更新 `connection_status`、`last_check_at`、`check_message`

2. **用户手动触发**
   - API：`POST /api/engines/:id/test`
   - 同步返回检测结果并更新状态

3. **不实施后台定时检测**
   - 原因：节省资源，避免频繁连接
   - 用户按需检测即可

---

## 九、使用示例

### 9.1 创建 Standard 引擎（PostgreSQL）

```bash
curl -X POST http://localhost:8180/api/engines \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "生产PostgreSQL",
    "engine_type": "postgresql",
    "connection_info": {
      "host": "localhost",
      "port": 5432,
      "username": "admin",
      "password": "admin123",
      "database": "production"
    },
    "description": "生产环境主数据库"
  }'
```

### 9.2 创建 Extension 引擎（Python Workflow）

```bash
curl -X POST http://localhost:8180/api/engines \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "Python工作流引擎",
    "engine_type": "python_workflow",
    "engine_origin": "extension",
    "connection_info": {
      "protocol": "http",
      "host": "localhost",
      "port": 8099
    },
    "capabilities": {
      "schema_version": "engine.capabilities/v1",
      "engine_type": "python_workflow",
      "engine_family": "workflow",
      "compute": {
        "workflow": {
          "supported": true,
          "runtime_api": "addp.workflow/v1",
          "dynamic_operators": true
        }
      }
    },
    "description": "基于 Python 的通用数据处理工作流引擎"
  }'
```

**注意**：API 端点配置已标准化到代码中（`common/models/workflow_standards.go`），创建时无需提供。

### 9.3 测试连接

```bash
curl -X POST http://localhost:8180/api/engines/1/test \
  -H "Authorization: Bearer $TOKEN"
```

**响应示例**：
```json
{
  "code": 200,
  "message": "连接成功",
  "data": {
    "status": "online",
    "message": "连接正常",
    "checked_at": "2026-01-01T10:00:00Z"
  }
}
```

### 9.4 查询计算引擎（内部 API）

```bash
curl http://localhost:8180/internal/registry/compute-engines
```

**响应示例**：
```json
{
  "code": 200,
  "data": [
    {
      "id": 2,
      "name": "Python工作流引擎",
      "engine_type": "python_workflow",
      "connection_info": {
        "protocol": "http",
        "host": "localhost",
        "port": 8099
      },
      "capabilities": {
        "schema_version": "engine.capabilities/v1",
        "engine_type": "python_workflow",
        "engine_family": "workflow",
        "compute": {
          "workflow": {
            "supported": true,
            "runtime_api": "addp.workflow/v1",
            "dynamic_operators": true
          }
        }
      }
    }
  ]
}
```

**注意**：API 端点配置在代码中（`common/models/workflow_standards.go`），API 响应不包含该信息。

---

## 十、重要说明

### 10.1 命名约定

1. **时间戳字段**使用 `_at` 后缀（如 `last_check_at`）
2. **配置时间**使用 `_time` 后缀（如 `schedule_time`）
3. **敏感字段**自动加密（`password`、`access_key`、`secret_key`、`token`、`api_key`）

### 10.2 元数据扫描策略

`scan_config` 不属于 System engine 表或 System API。需要注册扫描计划时，由 Console 调用 Meta 的 ScanTask API。

### 10.3 内置引擎保护

**创建时**：
- `is_builtin=true` 的引擎标记为内置
- 内置引擎有唯一索引（`engine_type` 唯一）

**修改限制**：
- 不允许修改 `engine_type`
- 不允许修改 `is_builtin`
- 只允许修改描述和激活状态

**删除保护**：
- 内置引擎不可删除

---

## 十一、相关文档

- **引擎插件接口规范**：[docs/spec/addp引擎插件接口规范.md](../../../docs/spec/addp引擎插件接口规范.md)
- **数据引擎扩展指南**：[docs/spec/addp数据引擎扩展指南.md](../../../docs/spec/addp数据引擎扩展指南.md)
- **各模块简要介绍**：[docs/concepts/addp各模块功能介绍.md](../../../docs/concepts/addp各模块功能介绍.md)
- **System模块说明**：[system/CLAUDE.md](../../CLAUDE.md)
