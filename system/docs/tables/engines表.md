# Engines 表结构和 API 说明

> 本文记录当前表和 API 实现。IAM 目标授权边界以 `docs/concepts/addp账号与权限体系图.md` 为准；文中的 SuperAdmin 跨 Tenant 访问语义属于待替换实现差距，平台角色不自动获得 Tenant 引擎访问权。

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
| `engine_type` | VARCHAR(255) | NOT NULL, INDEX | 引擎类型（postgresql/mysql/acme_geo_workflow等） |
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
- 历史 `unique_identifier`、`extension_api_config` 和 `health_check_config` 字段已废弃，并由 System 启动迁移删除。
- 工作流和脚本运行时通过 `common/engine` Provider 调用，System 不保存运行时端点配置
- API 响应中的 `capabilities_view` 是 System 后端根据 `capabilities` 派生的展示模型，定义在 `common/models` 供各模块客户端复用；它不是 `system.engines` 表字段，也不写入数据库。
- `capabilities.extensions.spatial_workspaces` 用于承载数据库实例中可识别的厂商空间工作区事实，例如 SuperMap `sdx+` 或 ArcGIS `sde`；System 应自动探测并在详情页展示，高危启用入口应基于这一事实自动收口。
- System 提供显性的高危操作入口 `POST /api/v1/system/engines/{id}/spatial-workspaces/{ecosystem}/{kind}/enable`，当前第一版对 `supermap/sdx+` 由已绑定的 `supermap_workflow` 运行时执行 direct-only 启用算子，后续可扩展到其他厂商空间工作区。

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

**普通 Engine API 示例**：
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
- API 端点由对应 `WorkflowRuntimeProvider` / `ScriptRuntimeProvider` 实现消费

**典型类型**：
- 工作流运行时：`geopython_workflow`、`spark_workflow`、`model3d_workflow`、`pointcloud_workflow`、`supermap_workflow`，以及手动注册的参考实现 `math_workflow`
- 内置 Notebook 运行时示例：`jupyter`
- 用户也可以按 ADDP 扩展引擎规范实现自研 `engine_type`，例如 `acme_geo_workflow`

**注册入口**：System 前端“注册扩展引擎”表单用于手动注册 `addp.workflow/v1` 工作流运行时。表单会按 `engine_type` 生成默认 `engine.capabilities/v1`，支持填入 SuperMap Workflow 和 Math Workflow 示例值，并提供“检查服务”只读探测：System 后端会访问 `/health` 和 `/api/operators`，确认运行时服务可达且算子 `engine_type` 与注册值一致。内置插件类型保存时以插件能力声明为准，前端提交的默认 capabilities 不作为最终事实源。Math Workflow 在开发环境中可自动启动服务，但不会自动写入本表；SuperMap Workflow 需要先按 `engines/supermap-workflow/README.md` 绑定 SuperMap SDK、GPA/SPS libs 和许可并启动运行时。

**示例**：
```json
{
  "name": "Acme Geo Workflow",
  "engine_type": "acme_geo_workflow",
  "engine_origin": "extension",
  "connection_info": {
    "protocol": "http",
    "host": "localhost",
    "port": 8100
  }
}
```

**运行时调用规则**：上层模块不直接读取或拼接工作流引擎 HTTP 端点。Develop 等调用方通过 `common/engine` 获取 `WorkflowRuntimeProvider`，由 provider 按 `addp.workflow/v1` HTTP 协议调用 `GET /api/operators`、`POST /api/workflow`、`GET /api/executions/{id}` 等运行时入口。连接测试统一由插件调用 `/health`。

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

**注意**：工作流引擎的运行时 API 由 `WorkflowRuntimeProvider` 封装，脚本 / Notebook 运行时由 `ScriptRuntimeProvider` 封装；数据库中无需保存端点配置。

---

### 4.2 Capabilities（能力声明）

**类型**：JSONB
**作用**：声明引擎自身具备、且可由 ADDP 统一消费的 native / provider 能力。

`system.engines.capabilities` 使用 `engine.capabilities/v1` 结构。已注册插件引擎的插件 `Capabilities()` 方法只提供 Provider 能力模板；System 服务启动、Engine API 创建或更新插件引擎时，会忽略调用方提交的插件引擎 `capabilities`，并按 `engine_type` 使用插件模板和可选实例能力解析结果生成落库能力。实例能力解析只允许做只读探测，例如检查数据库扩展、版本或函数是否可用。Registry 能力注册接口中，非插件引擎必须提交标准 `engine.capabilities/v1`；插件引擎即使提交 `capabilities` 也会被 System 忽略，并改用插件模板和实例解析结果生成落库能力。内置扩展引擎通过 `/api/v1/internal/engines/register` 自注册时不提交 `capabilities`。

**边界**：
- 该字段只表达引擎自身能力，例如 catalog、facts、store、change stream read、query、workflow、script。
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
| `engine_type` | 引擎类型，如 `postgresql`、`mysql`、`acme_geo_workflow` |
| `engine_family` | 粗粒度引擎族，如 `tabular`、`object`、`file`、`dynamic_schema`、`graph`、`event_stream`、`workflow`、`script` |
| `storage` | 存储、目录、catalog facts、内容访问能力 |
| `compute` | 查询、工作流、脚本或 Notebook 运行能力 |
| `limits` | 跨能力限制，有真实调用方时使用 |
| `extensions` | 引擎特有补充信息，不得替代核心字段 |

详细规范见 [ADDP 引擎能力声明规范](../../../docs/spec/addp引擎能力声明规范.md)。

业务 Kafka Engine 插件已实现 `engine_family=event_stream`、`service -> topic` catalog 和 `storage.store.change_stream_read`，System 后端可按普通 Engine 注册；Console 配置表单与 Transfer continuous runtime 尚未开放。Infra Kafka 属于 ADDP 部署配置，不写入 `system.engines`，也不产生本表 capabilities 记录。

Kafka `connection_info` 第一版字段：

| 字段 | 说明 |
|---|---|
| `bootstrap_servers` | 必填，逗号分隔的 broker 地址。 |
| `client_id` | 可选客户端标识。 |
| `security_protocol` | `plaintext` / `ssl` / `sasl_plaintext` / `sasl_ssl`，默认 `plaintext`。 |
| `sasl_mechanism` | SASL 时必填：`plain` / `scram-sha-256` / `scram-sha-512`。 |
| `username` / `password` | SASL 凭据；`password` 属于敏感字段。 |
| `tls_ca_cert` | 可选 PEM CA。 |
| `tls_client_cert` / `tls_client_key` | 可选 mTLS 证书与私钥，必须同时提供；私钥属于敏感字段。 |
| `tls_insecure_skip_verify` | 可选显式跳过服务端证书校验，默认 false。 |

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
        {"term": "schema", "kinds": ["schema"], "role": "branch", "i18n_key": "engine.term.schema"},
        {"term": "table", "kinds": ["table", "view", "materialized_view", "external_table"], "role": "leaf", "i18n_key": "engine.term.table"}
      ]
    },
    "catalog": {"supported": true, "real_time": true, "system_filtering": true},
    "facts": {"supported": true, "field_info": true, "statistics": true, "indexes": true, "constraints": true, "spatial_facts": true, "native_facts": true},
    "store": {
      "batch_read": true,
      "table_read_session": true,
      "batch_write": true,
      "table_write_session": true,
      "table_write_prepare": true,
      "bounded_watermark_read": true,
      "table_upsert": {"supported": true, "idempotent": true},
      "partitioned_table_change_apply": {
        "supported": true,
        "atomic_position_commit": true,
        "monotonic": true,
        "position_types": ["kafka_offset/v1"],
        "operations": ["upsert"]
      },
      "delete": true
    }
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

#### 自研 Workflow 示例
```json
{
  "schema_version": "engine.capabilities/v1",
  "engine_type": "acme_geo_workflow",
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

**新方案**：运行时端点不作为 System 表字段或独立配置事实源。工作流引擎通过 `WorkflowRuntimeProvider` 调用，当前 `addp.workflow/v1` HTTP 协议入口由 `common/engine/plugin/http_runtime.go` 封装。

**标准调用入口**（已注册的工作流运行时，例如 GeoPython Workflow / Spark Workflow / SuperMap Workflow / Math Workflow）：
```go
WorkflowRuntimeProvider.ListOperators()     // HTTP GET  /api/operators
WorkflowRuntimeProvider.ExecuteWorkflow()  // HTTP POST /api/workflow
EnginePlugin.TestConnection()              // HTTP GET  /health
```

---

### 4.4 元数据扫描策略边界

System engine 表不保存元数据扫描策略。注册引擎时的扫描计划由 Console 调用 Meta 创建或更新 `meta.scan_tasks`；System 只保存 engine 身份、连接、能力、租户和生命周期等自身事实。

---

### ~~4.5 HealthCheckConfig（健康检查配置）~~

**⚠️ 已废弃**：此字段已从数据库中删除。

**新方案**：健康检查由 `EnginePlugin.TestConnection()` 执行。工作流和脚本扩展引擎统一使用 `/health` 作为最小只读真实检查入口。

---

## 五、API 端点说明

### 5.1 外部 API（需要认证）

#### 列表查询
```
GET /api/v1/system/engines?page=1&page_size=10&capability_groups=storage,compute&engine_origins=general,extension&include_builtin=true
```

**查询参数**：
- `page`（可选，默认 `1`）- 页码
- `page_size`（可选，默认 `10`）- 每页数量
- `engine_type`（可选）- 按类型过滤
- `capability_groups`（可选）- 按能力分组过滤，支持 `storage`、`compute`，多个值以逗号分隔
- `engine_origins`（可选）- 按来源过滤，支持 `general`、`extension`，多个值以逗号分隔
- `include_builtin`（可选，默认 `true`）- 是否包含内置引擎

过滤在分页前执行，响应中的 `data`、`total` 和 `total_pages` 均以过滤后的结果集为准。

**响应示例**：
```json
{
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
  ],
  "total": 1,
  "page": 1,
  "page_size": 10,
  "total_pages": 1
}
```

#### 获取单个引擎
```
GET /api/v1/system/engines/:id
```

#### 创建引擎
```
POST /api/v1/system/engines
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
PUT /api/v1/system/engines/:id
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
DELETE /api/v1/system/engines/:id
```

**限制**：
- 内置引擎（`is_builtin=true`）不可删除
- 只有管理员可以删除

#### 测试连接（创建前）
```
POST /api/v1/system/engines/test-connection
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
POST /api/v1/system/engines/:id/test
```

请求体可选。编辑页面可传入当前表单中的 `connection_info`，用于测试尚未保存的连接配置；接口仍会同步更新该引擎的 `connection_status`、`last_check_at` 和 `check_message`。

---

### 5.2 内部 API（服务间调用）

#### 内部列表查询（解密）
```
GET /api/v1/internal/engines?engine_type={type}&tenant_id={id}
```

**特性**：
- 自动解密 `connection_info`
- 无需用户认证
- 支持跨租户查询（SuperAdmin）
- 响应为引擎数组；公开 `GET /api/v1/system/engines` 使用分页对象 `{data,total,page,page_size,total_pages}`。

#### 内部获取单个（解密）
```
GET /api/v1/internal/engines/:id
```

#### 注册能力
```
POST /api/v1/internal/registry/capabilities
Content-Type: application/json

{
  "name": "Custom Workflow Runtime",
  "engine_type": "custom_workflow",
  "is_builtin": false,
  "capabilities": {
    "schema_version": "engine.capabilities/v1",
    "engine_type": "custom_workflow",
    "engine_family": "workflow",
    "compute": {
      "workflow": {
        "supported": true,
        "runtime_api": "addp.workflow/v1",
        "dynamic_operators": true
      }
    }
  },
  "description": "非内置工作流运行时",
  "connection_info": {
    "protocol": "http",
    "host": "localhost",
    "port": 19099
  }
}
```

**注意**：这是 Registry 能力注册接口。非插件引擎调用时必须提交标准 `engine.capabilities/v1`；插件引擎的能力事实源仍是插件 `Capabilities()`，System 会忽略请求体中的 `capabilities`。内置运行时启动自注册使用 `POST /api/v1/internal/engines/register`，只提交身份与连接信息，不提交 `capabilities`。

#### 查询能力
```
GET /api/v1/internal/registry/capabilities?engine_type=acme_geo_workflow&is_active=true
```

#### 查询计算引擎
```
GET /api/v1/internal/registry/compute-engines
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
   - API：`POST /api/v1/system/engines/:id/test`
   - 同步返回检测结果并更新状态

3. **不实施后台定时检测**
   - 原因：节省资源，避免频繁连接
   - 用户按需检测即可

---

## 九、使用示例

### 9.1 创建 Standard 引擎（PostgreSQL）

```bash
curl -X POST http://localhost:8180/api/v1/system/engines \
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

### 9.2 通过 Engine API 创建自研 Extension 工作流引擎

```bash
curl -X POST http://localhost:8180/api/v1/system/engines \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "Acme Geo Workflow",
    "engine_type": "acme_geo_workflow",
    "engine_origin": "extension",
    "connection_info": {
      "protocol": "http",
      "host": "localhost",
      "port": 8100
    },
    "capabilities": {
      "schema_version": "engine.capabilities/v1",
      "engine_type": "acme_geo_workflow",
      "engine_family": "workflow",
      "compute": {
        "workflow": {
          "supported": true,
          "runtime_api": "addp.workflow/v1",
          "dynamic_operators": true,
          "supported_operator_mode": ["workflow", "direct"]
        }
      }
    },
    "description": "自研 GIS 工作流运行时"
  }'
```

**注意**：这是用户自研扩展引擎创建示例，必须提交或由 System 注册表单生成标准 `engine.capabilities/v1`，且其中的 `engine_type` 必须与资源 `engine_type` 一致。声明 `compute.workflow.supported=true` 且 `runtime_api="addp.workflow/v1"` 时，System 保存前会按 `/health` 和 `/api/operators` 执行只读协议探测。生产内置运行时启动自注册不走该示例，可以不提交 `capabilities`，由 System 按已注册插件声明生成；Math Workflow 作为参考实现可随开发环境启动服务，但仍按手动注册路径处理。

### 9.3 测试连接

```bash
curl -X POST http://localhost:8180/api/v1/system/engines/1/test \
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
curl http://localhost:8180/api/v1/internal/registry/compute-engines
```

**响应示例**：
```json
{
  "code": 200,
  "data": [
    {
      "id": 2,
      "name": "Acme Geo Workflow",
      "engine_type": "acme_geo_workflow",
      "connection_info": {
        "protocol": "http",
        "host": "localhost",
        "port": 8099
      },
      "capabilities": {
        "schema_version": "engine.capabilities/v1",
        "engine_type": "acme_geo_workflow",
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

**注意**：运行时端点由 Provider 封装，API 响应不包含该信息。

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
