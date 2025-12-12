# Meta 模块数据结构和 API 文档

## 目录

- [1. 模块概述](#1-模块概述)
- [2. 数据库结构](#2-数据库结构)
- [3. API 端点清单](#3-api-端点清单)
- [4. 基础设施使用](#4-基础设施使用)
- [5. Worker 架构](#5-worker-架构)
- [6. 配置参数](#6-配置参数)

---

## 1. 模块概述

Meta 模块负责元数据管理和数据血缘追踪，提供以下功能：

- **元数据扫描**：自动扫描数据库（PostgreSQL/MySQL）和对象存储（MinIO/S3/OSS）
- **层级管理**：统一的 resource → node → item 层级模型
- **空间元数据**：PostGIS 空间数据的元数据提取（几何类型、SRID、范围）
- **定时调度**：Cron 表达式支持的自动扫描任务
- **事件驱动**：订阅 System 资源创建事件，自动触发扫描
- **全文索引**：集成 Meilisearch 的元数据资产搜索
- **向量化**：pgvector 支持的多模态向量检索

### 端口配置

- **开发端口**: 8082
- **生产端口**: 8082
- **数据库 Schema**: `metadata`
- **依赖**: PostgreSQL, Redis (Asynq), MinIO, Meilisearch, pgvector, System 模块

### 模块依赖关系

```
System（资源配置、事件发布）
  ↓
Meta（元数据扫描、存储）
  ↓ (提供元数据)
Manager、Transfer（元数据消费）
```

---

## 2. 数据库结构

### 2.1 PostgreSQL Schema: metadata

Meta 模块使用 `metadata` schema，包含 7 张核心表。

#### 表 1: meta_node - 层级节点表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 节点唯一标识 |
| `tenant_id` | BIGINT | NOT NULL | 租户 ID |
| `res_id` | BIGINT | NOT NULL, INDEXED | 资源 ID（关联 system.resources） |
| `parent_node_id` | BIGINT | FK, INDEXED | 父节点 ID（自引用） |
| `node_type` | VARCHAR(64) | NOT NULL, INDEXED | 节点类型（schema/prefix/bucket 等） |
| `name` | VARCHAR(255) | NOT NULL | 节点名称 |
| `depth` | INT | NOT NULL | 深度（0 为顶层） |
| `path` | TEXT | | ID 路径链 |
| `full_name` | TEXT | | 完整名称（包含前缀） |
| `status` | VARCHAR(32) | | 状态（active/inactive） |
| `scan_status` | VARCHAR(32) | | 扫描状态（未扫描/扫描中/已扫描） |
| `last_scan_at` | TIMESTAMP WITH TIME ZONE | | 最后扫描时间 |
| `auto_scan_enabled` | BOOLEAN | DEFAULT false | 是否启用自动扫描 |
| `auto_scan_cron` | VARCHAR(128) | | Cron 表达式 |
| `next_scan_at` | TIMESTAMP WITH TIME ZONE | | 下次扫描时间 |
| `item_count` | INT | DEFAULT 0 | 子项目数 |
| `total_size_bytes` | BIGINT | DEFAULT 0 | 总大小 |
| `error_message` | TEXT | | 错误信息 |
| `attributes` | JSONB | | 扩展属性（空间元数据、统计信息） |
| `sync_version` | BIGINT | DEFAULT 0 | 同步版本号 |
| `created_at` | TIMESTAMP WITH TIME ZONE | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP WITH TIME ZONE | DEFAULT CURRENT_TIMESTAMP | 更新时间 |
| `deleted_at` | TIMESTAMP WITH TIME ZONE | | 软删除时间戳 |

**约束**:
- UNIQUE(res_id, name, parent_node_id)

**索引**:
- `idx_meta_node_res` - 资源索引
- `idx_meta_node_parent` - 父节点索引
- `idx_meta_node_type` - 节点类型索引

**Go 模型** (`internal/models/meta_node.go`):

```go
type MetaNode struct {
    ID              uint
    TenantID        uint
    ResID           uint
    ParentNodeID    *uint
    NodeType        string
    Name            string
    Depth           int
    Path            string
    FullName        string
    Status          string
    ScanStatus      string
    LastScanAt      *time.Time
    AutoScanEnabled bool
    AutoScanCron    string
    NextScanAt      *time.Time
    ItemCount       int
    TotalSizeBytes  int64
    ErrorMessage    string
    Attributes      JSONMap
    SyncVersion     int64
    CreatedAt       time.Time
    UpdatedAt       time.Time
    DeletedAt       gorm.DeletedAt
}
```

---

#### 表 2: meta_item - 数据项表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 项目唯一标识 |
| `tenant_id` | BIGINT | NOT NULL | 租户 ID |
| `res_id` | BIGINT | NOT NULL | 资源 ID |
| `node_id` | BIGINT | FK, INDEXED | 所属节点 ID |
| `item_type` | VARCHAR(64) | NOT NULL, INDEXED | 项目类型（table/view/object 等） |
| `name` | VARCHAR(255) | NOT NULL | 项目名称 |
| `full_name` | TEXT | | 完整名称 |
| `fingerprint` | VARCHAR(64) | UNIQUE INDEX | 数据指纹（SHA256 哈希） |
| `status` | VARCHAR(32) | | 状态 |
| `meta_schema_version` | INT | DEFAULT 1 | 元数据架构版本 |
| `row_count` | BIGINT | | 行数（表） |
| `size_bytes` | BIGINT | | 大小（表） |
| `object_size_bytes` | BIGINT | | 对象大小 |
| `last_modified_at` | TIMESTAMP WITH TIME ZONE | | 最后修改时间 |
| `attributes` | JSONB | | 扩展属性（字段列表、空间元数据） |
| `sync_version` | BIGINT | DEFAULT 0 | 同步版本号 |
| `source` | VARCHAR(64) | | 来源类型 |
| `created_at` | TIMESTAMP WITH TIME ZONE | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP WITH TIME ZONE | DEFAULT CURRENT_TIMESTAMP | 更新时间 |
| `deleted_at` | TIMESTAMP WITH TIME ZONE | | 软删除时间戳 |

**索引**:
- `idx_meta_item_node` - 节点索引
- `idx_meta_item_type` - 项目类型索引
- `idx_meta_item_fingerprint` - 指纹唯一索引

**Attributes 字段示例**:

对于表：
```json
{
  "fields": [
    {"name": "id", "type": "integer", "nullable": false},
    {"name": "name", "type": "varchar(255)", "nullable": true}
  ],
  "spatial_metadata": {
    "geometry_column": "geom",
    "srid": 4326,
    "extent_srid": 4326,
    "extent": [120.1, 30.2, 121.5, 31.8],
    "geometry_types": ["POINT", "POLYGON"],
    "has_spatial_index": true,
    "index_name": "idx_geom_gist"
  }
}
```

**Go 模型** (`internal/models/meta_item.go`):

```go
type MetaItem struct {
    ID                uint
    TenantID          uint
    ResID             uint
    NodeID            uint
    ItemType          string
    Name              string
    FullName          string
    Fingerprint       string
    Status            string
    MetaSchemaVersion int
    RowCount          *int64
    SizeBytes         *int64
    ObjectSizeBytes   *int64
    LastModifiedAt    *time.Time
    Attributes        JSONMap
    SyncVersion       int64
    Source            string
    CreatedAt         time.Time
    UpdatedAt         time.Time
    DeletedAt         gorm.DeletedAt
}
```

---

#### 表 3: meta_node_type_dict - 节点类型字典

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `type_code` | VARCHAR(64) | PRIMARY KEY | 类型代码 |
| `category` | VARCHAR(64) | | 分类 |
| `description` | TEXT | | 描述 |
| `created_at` | TIMESTAMP WITH TIME ZONE | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

**预定义类型**:
- `schema` - 数据库 schema
- `prefix` - 对象存储前缀
- `bucket` - 对象存储 bucket
- `collection` - 集合

---

#### 表 4: meta_node_child_rule - 父子节点规则

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `parent_type` | VARCHAR(64) | FK, PRIMARY KEY | 父节点类型 |
| `child_type` | VARCHAR(64) | FK, PRIMARY KEY | 子节点类型 |

**用途**: 限定合法的节点层级关系

---

#### 表 5: meta_json_schema - JSON 属性 Schema 版本管理

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | Schema ID |
| `target` | VARCHAR(32) | NOT NULL | 目标（node/item） |
| `version` | INT | NOT NULL | Schema 版本 |
| `definition` | JSONB | | JSON Schema 定义 |
| `description` | TEXT | | 描述 |
| `created_at` | TIMESTAMP WITH TIME ZONE | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

**约束**:
- UNIQUE(target, version)

---

#### 表 6: meta_change_log - 元数据变更日志

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 日志 ID |
| `tenant_id` | BIGINT | | 租户 ID |
| `res_id` | BIGINT | | 资源 ID |
| `node_id` | BIGINT | FK | 节点 ID |
| `item_id` | BIGINT | FK | 项目 ID |
| `change_type` | VARCHAR(64) | | 变更类型（创建/更新/删除等） |
| `change_source` | VARCHAR(64) | | 变更来源（sync/manual/system） |
| `payload` | JSONB | | 变更详情 |
| `sync_version` | BIGINT | | 关联的同步版本 |
| `created_at` | TIMESTAMP WITH TIME ZONE | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

---

#### 表 7: scan_logs - 扫描日志表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 日志 ID |
| `resource_id` | BIGINT | INDEXED | 资源 ID |
| `schema_id` | BIGINT | | Schema ID（可选） |
| `tenant_id` | BIGINT | INDEXED | 租户 ID |
| `scan_type` | VARCHAR(50) | | 扫描类型（auto/manual/scheduled） |
| `scan_depth` | VARCHAR(20) | | 扫描深度（basic/deep/full） |
| `target_schemas` | TEXT | | JSON 数组：["schema1", "schema2"] |
| `status` | VARCHAR(20) | INDEXED | 状态（running/success/failed） |
| `error_message` | TEXT | | 错误信息 |
| `schemas_scanned` | INT | | 扫描的 Schema 数 |
| `tables_scanned` | INT | | 扫描的表数 |
| `fields_scanned` | INT | | 扫描的字段数 |
| `started_at` | TIMESTAMP WITH TIME ZONE | | 开始时间 |
| `completed_at` | TIMESTAMP WITH TIME ZONE | | 完成时间 |
| `duration_ms` | INT | | 耗时（毫秒） |
| `created_at` | TIMESTAMP WITH TIME ZONE | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

**索引**:
- `idx_scan_logs_resource` - 资源索引
- `idx_scan_logs_tenant` - 租户索引
- `idx_scan_logs_status` - 状态索引

---

### 2.2 数据表关系图

```
system.resources (来自 System 模块)
    ↓
metadata.meta_node (层级节点)
    ↓ 1:N (parent_node_id)
metadata.meta_node (子节点)
    ↓ 1:N
metadata.meta_item (数据项)

metadata.scan_logs (扫描日志)
    ↓ (关联)
metadata.meta_node

metadata.meta_node_type_dict
    ↓ (验证)
metadata.meta_node (node_type)

metadata.meta_change_log
    ↓ (关联)
metadata.meta_node / metadata.meta_item
```

---

## 3. API 端点清单

### 3.1 资源管理 API

#### GET /api/meta/resources - 获取资源列表及统计

**响应** (200 OK):

```json
{
  "resources": [
    {
      "id": 1,
      "name": "PostgreSQL-生产库",
      "resource_type": "postgresql",
      "scanned": true,
      "last_scan_at": "2025-12-11T10:00:00Z",
      "node_count": 5,
      "item_count": 100
    }
  ],
  "total": 1
}
```

---

#### GET /api/meta/schemas/:resource_id - 获取资源的 Schema 列表

**响应** (200 OK):

```json
{
  "schemas": [
    {
      "id": 10,
      "name": "public",
      "scan_status": "已扫描",
      "table_count": 50,
      "last_scan_at": "2025-12-11T10:00:00Z"
    }
  ]
}
```

---

#### GET /api/meta/schemas/:resource_id/available - 列出可用 Schema（实时查询）

**响应** (200 OK):

```json
{
  "schemas": [
    {"name": "public"},
    {"name": "data"},
    {"name": "analytics"}
  ]
}
```

---

#### GET /api/meta/object-storage/:resource_id/nodes - 分级列出对象存储节点

**查询参数**:
- `parent_id`: 父节点 ID（可选）
- `depth`: 深度（可选）

**响应** (200 OK):

```json
{
  "nodes": [
    {
      "id": 20,
      "name": "data/",
      "node_type": "prefix",
      "item_count": 100,
      "total_size_bytes": 1048576
    }
  ]
}
```

---

### 3.2 扫描相关 API

#### POST /api/meta/scan/auto - 自动扫描所有未扫描资源

**响应** (202 Accepted):

```json
{
  "message": "自动扫描已启动",
  "resources_to_scan": 3
}
```

---

#### POST /api/meta/scan/resource - 扫描指定资源

**请求体**:

```json
{
  "resource_id": 1,
  "schema_names": ["public", "data"],
  "scan_depth": "basic"
}
```

**响应** (202 Accepted):

```json
{
  "message": "扫描任务已提交",
  "run_id": 42
}
```

---

#### POST /api/meta/scan/run/manual - 创建异步扫描运行

**请求体**:

```json
{
  "resource_id": 1,
  "storage_type": "postgresql",
  "parameters": {
    "schema_names": ["public"],
    "object_paths": [],
    "scan_depth": "deep"
  }
}
```

**响应** (202 Accepted):

```json
{
  "run_id": 43,
  "status": "pending",
  "message": "扫描运行已创建"
}
```

---

#### GET /api/meta/scan/runs - 列出扫描运行记录

**查询参数**:
- `task_id`: 按任务过滤
- `status`: 按状态过滤（pending/running/success/failed）
- `storage_type`: 按存储类型过滤
- `limit`: 记录数（默认 20）
- `offset`: 偏移量（默认 0）

**响应** (200 OK):

```json
{
  "items": [
    {
      "id": 43,
      "task_id": 10,
      "resource_id": 1,
      "status": "success",
      "trigger_type": "manual",
      "started_at": "2025-12-11T10:00:00Z",
      "completed_at": "2025-12-11T10:05:00Z",
      "progress_percent": 100.0,
      "result_summary": {
        "schemas_scanned": 2,
        "tables_scanned": 50
      }
    }
  ],
  "total": 1
}
```

---

#### GET /api/meta/scan/runs/:run_id - 获取扫描运行详情

**响应** (200 OK): 返回运行详情对象

---

#### GET /api/meta/scan/tasks - 列出扫描任务台账

**响应** (200 OK):

```json
{
  "tasks": [
    {
      "id": 10,
      "name": "每日扫描",
      "resource_id": 1,
      "schedule_type": "daily",
      "cron_expression": "0 0 * * *",
      "enabled": true,
      "last_run_at": "2025-12-11T00:00:00Z",
      "next_run_at": "2025-12-12T00:00:00Z"
    }
  ]
}
```

---

#### POST /api/meta/scan/tasks - 创建扫描任务

**请求体**:

```json
{
  "name": "每日扫描",
  "description": "每天凌晨2点扫描",
  "resource_id": 1,
  "schedule_type": "daily",
  "schedule_time": "02:00",
  "parameters": {
    "schema_names": ["public"],
    "scan_depth": "basic"
  },
  "enabled": true
}
```

**响应** (201 Created): 返回任务对象

---

#### PUT /api/meta/scan/tasks/:task_id - 更新扫描任务

**请求体**: 同创建任务

**响应** (200 OK): 返回更新后的任务

---

#### DELETE /api/meta/scan/tasks/:task_id - 删除扫描任务

**响应** (200 OK):

```json
{
  "message": "任务删除成功"
}
```

---

#### POST /api/meta/scan/tasks/:task_id/trigger - 手动触发任务

**响应** (202 Accepted):

```json
{
  "message": "任务已触发",
  "run_id": 44
}
```

---

### 3.3 元数据查询 API

#### GET /api/meta/metadata/object - 获取对象元数据

**查询参数**:
- `resource_id`: 资源 ID（必填）
- `bucket`: Bucket 名称（必填）
- `path`: 对象路径（必填）

**响应** (200 OK):

```json
{
  "name": "file.csv",
  "size": 1024,
  "content_type": "text/csv",
  "last_modified": "2025-12-11T10:00:00Z",
  "metadata": {
    "columns": ["id", "name", "value"],
    "row_count": 100
  }
}
```

---

#### POST /api/meta/metadata/extract - 按需提取对象深度元数据

**请求体**:

```json
{
  "resource_id": 1,
  "bucket": "mybucket",
  "path": "/data/file.parquet"
}
```

**响应** (200 OK):

```json
{
  "format": "parquet",
  "schema": {
    "fields": [
      {"name": "id", "type": "int64"},
      {"name": "name", "type": "string"}
    ]
  },
  "row_count": 10000,
  "size_bytes": 102400
}
```

---

#### GET /api/meta/metadata/tables - 获取资源的表列表（Transfer 用）

**查询参数**:
- `resource_id`: 资源 ID（必填）
- `schema`: Schema 名称（可选）

**响应** (200 OK):

```json
{
  "tables": [
    {
      "schema": "public",
      "table": "users",
      "row_count": 1000,
      "size_bytes": 102400
    }
  ]
}
```

---

#### GET /api/meta/metadata/fields - 获取表的字段列表（带详情）

**查询参数**:
- `resource_id`: 资源 ID（必填）
- `schema`: Schema 名称（必填）
- `table`: 表名（必填）

**响应** (200 OK):

```json
{
  "fields": [
    {
      "name": "id",
      "type": "integer",
      "nullable": false,
      "is_primary_key": true
    },
    {
      "name": "name",
      "type": "varchar(255)",
      "nullable": true
    }
  ]
}
```

---

#### GET /api/meta/metadata/tables/spatial - 获取表的空间元数据（MVT 用）

**查询参数**:
- `resource_id`: 资源 ID（必填）
- `schema`: Schema 名称（必填）
- `table`: 表名（必填）

**响应** (200 OK):

```json
{
  "geometry_column": "geom",
  "srid": 4326,
  "extent_srid": 4326,
  "extent": [120.1, 30.2, 121.5, 31.8],
  "geometry_types": ["POINT", "POLYGON"],
  "has_spatial_index": true,
  "index_name": "idx_geom_gist"
}
```

---

### 3.4 Manager 集成接口

#### GET /api/meta/resources/:resource_id/tree - 获取资源元数据树

**响应** (200 OK):

```json
{
  "resource_id": 1,
  "resource_name": "PostgreSQL-生产库",
  "nodes": [
    {
      "id": 10,
      "name": "public",
      "node_type": "schema",
      "item_count": 50,
      "children": [
        {
          "id": 100,
          "name": "users",
          "item_type": "table",
          "row_count": 1000
        }
      ]
    }
  ]
}
```

---

#### GET /api/meta/nodes/:node_id - 获取节点详情

**响应** (200 OK): 返回 MetaNode 对象

---

#### GET /api/meta/nodes/:node_id/children - 获取子节点

**响应** (200 OK):

```json
{
  "children": [
    {
      "id": 11,
      "name": "data",
      "node_type": "schema",
      "item_count": 20
    }
  ]
}
```

---

#### GET /api/meta/nodes/:node_id/items - 获取节点下的项目

**响应** (200 OK):

```json
{
  "items": [
    {
      "id": 100,
      "name": "users",
      "item_type": "table",
      "row_count": 1000,
      "size_bytes": 102400
    }
  ]
}
```

---

#### GET /api/meta/nodes/by-path - 按路径查询节点

**查询参数**:
- `resource_id`: 资源 ID（必填）
- `path`: 节点路径（必填）

**响应** (200 OK): 返回 MetaNode 对象

---

#### GET /api/meta/items/by-path - 按路径查询项目

**查询参数**:
- `resource_id`: 资源 ID（必填）
- `node_path`: 节点路径（必填）
- `item_name`: 项目名称（必填）

**响应** (200 OK): 返回 MetaItem 对象

---

### 3.5 缓存管理 API

#### DELETE /api/meta/cache/resources/:resource_id - 清除资源缓存

**响应** (200 OK):

```json
{
  "message": "缓存已清除"
}
```

---

#### DELETE /api/meta/cache/resources/all - 清除所有缓存

**响应** (200 OK):

```json
{
  "message": "所有缓存已清除"
}
```

---

#### POST /api/meta/cache/refresh - 刷新资源缓存

**请求体**:

```json
{
  "resource_id": 1
}
```

**响应** (200 OK):

```json
{
  "message": "缓存刷新成功"
}
```

---

## 4. 基础设施使用

### 4.1 Redis Asynq 队列

**队列命名规范**:

```
meta:critical   高优先级队列（并发 6）
meta:default    默认队列（并发 3）
meta:low        低优先级队列（并发 1）
```

**任务类型**:

```
TypeScanTask = "meta:scan"
```

**任务载荷**:

```json
{
  "run_id": 43,
  "task_id": 10,
  "tenant_id": 1
}
```

**队列统计**:

```
asynq:meta:default:pending    等待处理的任务
asynq:meta:default:active     正在处理的任务
asynq:meta:default:scheduled  延迟执行的任务
asynq:meta:default:retry      重试队列
asynq:meta:default:archived   死信队列
```

---

### 4.2 MinIO 存储

**Bucket**: `meta` (系统 MinIO，端口 9000-9001)

**目录结构**:

```
meta/
├── scan-cache/              # 扫描缓存
│   └── {resource_id}/
│       └── {schema_name}/
│           └── metadata.json
├── metadata-exports/        # 元数据导出
│   └── {tenant_id}/
│       └── {export_id}.json
└── object-metadata/         # 对象元数据缓存
    └── {resource_id}/
        └── {object_path}/
            └── metadata.json
```

---

### 4.3 Meilisearch 索引

**索引名**: `meta:assets`

**文档结构**:

```json
{
  "id": "meta_item_100",
  "resource_id": 1,
  "node_id": 10,
  "item_type": "table",
  "name": "users",
  "full_name": "public.users",
  "content": "用户表 包含用户信息",
  "attributes": {
    "row_count": 1000,
    "size_bytes": 102400
  },
  "created_at": "2025-12-11T10:00:00Z"
}
```

**可搜索字段**:
- `name` - 项目名称
- `full_name` - 完整名称
- `content` - 可搜索的内容

---

### 4.4 事件驱动架构

**Redis Pub/Sub 事件**:

订阅事件：`system:resource:created`

```json
{
  "resource_id": 1,
  "resource_type": "postgresql",
  "tenant_id": 1
}
```

**处理逻辑**:
1. Meta 订阅资源创建事件
2. 根据 ScanConfig 决定是否自动扫描
3. 如果 `schedule_type == "immediate"`，创建 ScanTaskRun 并入队

---

## 5. Worker 架构

### 5.1 Worker 启动流程

位置：`meta/backend/cmd/worker/main.go`

1. 加载环境变量和配置
2. 连接 PostgreSQL 数据库
3. 创建日志记录器和扫描服务
4. 创建任务队列（连接 Redis）
5. 创建 Asynq Server 并配置并发参数
6. 注册任务处理器
7. 创建并启动定时调度器
8. 启动 Worker 并监听关闭信号

### 5.2 任务处理器

**HandleScanTask** (`internal/worker/handler.go`):

```go
func (h *TaskHandler) HandleScanTask(ctx context.Context, t *asynq.Task) error {
    // 1. 解析任务载荷
    var payload ScanTaskPayload
    json.Unmarshal(t.Payload(), &payload)

    // 2. 执行扫描
    err := h.taskService.ExecuteScanRun(ctx, payload.RunID)

    // 3. 返回结果
    return err
}
```

### 5.3 定时调度器

**Scheduler** (`internal/worker/scheduler.go`):

```go
type Scheduler struct {
    cron          *cron.Cron
    taskRepo      *TaskRepository
    taskQueue     *TaskQueue
    executionRepo *ExecutionRepository
}
```

**功能**:
- 使用 `robfig/cron/v3` 库
- 支持 manual/daily/weekly/monthly/cron 调度类型
- 自动转换为 Asynq 队列任务

---

## 6. 配置参数

### 6.1 环境变量清单

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `PORT` | 8082 | 服务端口 |
| `DB_HOST` | localhost | PostgreSQL 主机 |
| `DB_PORT` | 5432 | PostgreSQL 端口 |
| `DB_USER` | addp | 数据库用户 |
| `DB_PASSWORD` | addp_password | 数据库密码 |
| `DB_NAME` | addp | 数据库名 |
| `DB_SCHEMA` | metadata | Meta schema 名 |
| `REDIS_HOST` | localhost | Redis 主机 |
| `REDIS_PORT` | 6379 | Redis 端口 |
| `REDIS_PASSWORD` | - | Redis 密码（可选） |
| `SYSTEM_SERVICE_URL` | http://localhost:8080 | System 服务 URL |
| `ENABLE_SERVICE_INTEGRATION` | true | 启用服务间集成 |
| `MEILISEARCH_HOST` | http://localhost:7700 | Meilisearch 地址 |
| `MEILISEARCH_API_KEY` | - | Meilisearch API Key |

---

## 7. 关键文件路径

| 文件 | 路径 | 说明 |
|------|------|------|
| 路由配置 | `meta/backend/internal/api/router.go` | 所有 API 端点定义 |
| 节点模型 | `meta/backend/internal/models/meta_node.go` | 节点模型 |
| 项目模型 | `meta/backend/internal/models/meta_item.go` | 项目模型 |
| 扫描服务 | `meta/backend/internal/service/scan_service_new.go` | 扫描逻辑 |
| 任务服务 | `meta/backend/internal/service/scan_task_service.go` | 任务管理 |
| Worker 主程序 | `meta/backend/cmd/worker/main.go` | Worker 入口 |
| 任务处理器 | `meta/backend/internal/worker/handler.go` | 任务处理 |
| 调度器 | `meta/backend/internal/worker/scheduler.go` | 定时调度 |

---

## 8. 相关文档

- [ADDP 平台架构文档](../CLAUDE.md)
- [Meta 模块详细文档](README.md)
- [System 模块数据结构文档](../system/DATA_STRUCTURES.md)
- [Manager 模块数据结构文档](../manager/DATA_STRUCTURES.md)
