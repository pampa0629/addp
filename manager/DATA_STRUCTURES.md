# Manager 模块数据结构和 API 文档

## 目录

- [1. 模块概述](#1-模块概述)
- [2. 数据库结构](#2-数据库结构)
- [3. API 端点清单](#3-api-端点清单)
- [4. 基础设施使用](#4-基础设施使用)
- [5. 服务层架构](#5-服务层架构)
- [6. 配置参数](#6-配置参数)

---

## 1. 模块概述

Manager 模块负责数据管理、预览和空间数据处理，提供以下功能：

- **数据源管理**：连接和管理各类数据源
- **数据探查**：浏览数据库表、对象存储文件
- **数据预览**：表数据、GeoJSON、Shapefile、图片、PDF、Office 文档预览
- **MVT 瓦片服务**：矢量瓦片生成和缓存（三层缓存架构）
- **快显/预缓存**：空间数据瓦片预生成
- **全文搜索**：基于 Meilisearch 的资产搜索
- **地图配置**：高德/天地图 API 配置

### 端口配置

- **开发端口**: 8081
- **生产端口**: 8081
- **数据库 Schema**: `manager`
- **依赖**: PostgreSQL, Redis, MinIO, Meilisearch, System 模块, Meta 模块

### 模块依赖关系

```
System（资源配置）
  ↓
Manager（数据管理、MVT 瓦片）
  ↓ (提供元数据)
Meta（元数据扫描）
```

---

## 2. 数据库结构

### 2.1 PostgreSQL Schema: manager

Manager 模块使用 `manager` schema，包含 5 张核心表。

#### 表 1: data_sources - 数据源管理表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 数据源唯一标识 |
| `name` | VARCHAR(255) | NOT NULL | 数据源名称 |
| `type` | VARCHAR(50) | NOT NULL | 类型（postgresql/minio 等） |
| `connection_info` | JSONB | NOT NULL | 连接信息（JSON 格式，加密存储） |
| `status` | VARCHAR(20) | DEFAULT 'active' | 状态 |
| `created_by` | INTEGER | | 创建者 ID |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 更新时间 |

**Go 模型** (`internal/models/resource.go`):

```go
type Resource struct {
    ID             uint
    Name           string
    ResourceType   string
    ConnectionInfo ConnectionInfo
    Description    string
    CreatedBy      *uint
    TenantID       *uint
    IsActive       bool
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

---

#### 表 2: directories - 目录树管理表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 目录唯一标识 |
| `name` | TEXT | | 目录/文件名 |
| `parent_id` | BIGINT | FK, UNIQUE(parent_id, name) | 父目录 ID |
| `path` | TEXT | INDEXED | 完整路径 |
| `type` | TEXT | | 类型：folder/file |
| `size` | BIGINT | | 大小（字节） |
| `mime_type` | TEXT | | MIME 类型 |
| `storage_id` | BIGINT | | 存储引擎 ID |
| `created_by` | BIGINT | | 创建者 ID |
| `created_at` | TIMESTAMP | | 创建时间 |
| `updated_at` | TIMESTAMP | | 更新时间 |

**索引**:
- `idx_directories_parent` - 父目录索引
- `idx_directories_path` - 路径索引

---

#### 表 3: quick_view - 快显任务表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 任务唯一标识 |
| `tenant_id` | INT | NOT NULL | 租户 ID |
| `resource_id` | INT | NOT NULL | 数据源 ID（关联 system.resources） |
| `schema_name` | VARCHAR(255) | NOT NULL | 数据库 schema 名称 |
| `table_name` | VARCHAR(255) | NOT NULL | 表名 |
| `status` | VARCHAR(50) | DEFAULT 'none' | 状态：none/generating/ready/failed |
| `error_message` | TEXT | | 错误信息 |
| `min_zoom` | INT | | 最小缩放级别（自动计算） |
| `max_zoom` | INT | DEFAULT 18 | 最大缩放级别 |
| `actual_max_zoom` | INT | | 实际生成到的最高层级 |
| `total_tiles` | INT | DEFAULT 0 | 瓦片总数 |
| `cached_tiles` | INT | DEFAULT 0 | 已缓存的瓦片数 |
| `last_zoom_avg_time_ms` | FLOAT | | 最后一层平均生成时间（毫秒） |
| `last_zoom_avg_size_kb` | FLOAT | | 最后一层平均瓦片大小（KB） |
| `stop_threshold_time_ms` | FLOAT | DEFAULT 300 | 停止条件：时间阈值（毫秒） |
| `stop_threshold_size_kb` | FLOAT | DEFAULT 100 | 停止条件：大小阈值（KB） |
| `fingerprint` | VARCHAR(64) | NOT NULL, UNIQUE | MinIO 路径指纹（资源+schema+表） |
| `extent` | JSONB | | 地理范围 [minLng, minLat, maxLng, maxLat] |
| `extent_srid` | INT | DEFAULT 4326 | 坐标系统（默认 WGS84） |
| `started_at` | TIMESTAMP | | 开始时间 |
| `completed_at` | TIMESTAMP | | 完成时间 |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 更新时间 |

**约束**:
- UNIQUE(tenant_id, resource_id, schema_name, table_name)

**索引**:
- `idx_quick_view_status` - 状态索引
- `idx_quick_view_fingerprint` - 指纹索引

**Fingerprint 计算**:
```go
fingerprint = MD5(fmt.Sprintf("%d:%s:%s", resourceID, schemaName, tableName))
```

**Go 模型** (`internal/models/quick_view.go`):

```go
type QuickView struct {
    ID                  uint
    TenantID            uint
    ResourceID          uint
    SchemaName          string
    Table               string
    Status              string
    ErrorMessage        string
    MinZoom             *int
    MaxZoom             int
    ActualMaxZoom       *int
    TotalTiles          int
    CachedTiles         int
    LastZoomAvgTimeMs   *float64
    LastZoomAvgSizeKB   *float64
    StopThresholdTimeMs float64
    StopThresholdSizeKB float64
    Fingerprint         string
    Extent              JSONFloatArray
    ExtentSRID          int
    StartedAt           *time.Time
    CompletedAt         *time.Time
    CreatedAt           time.Time
    UpdatedAt           time.Time
}
```

---

#### 表 4: data_source_permissions - 数据源权限表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 权限记录 ID |
| `data_source_id` | INTEGER | FK CASCADE | 数据源 ID |
| `user_id` | INTEGER | | 用户 ID |
| `group_id` | INTEGER | | 群组 ID |
| `permission` | VARCHAR(20) | NOT NULL | 权限：none/read/write/admin |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

---

#### 表 5: directory_permissions - 目录权限表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 权限记录 ID |
| `directory_id` | INTEGER | FK CASCADE | 目录 ID |
| `user_id` | INTEGER | | 用户 ID |
| `group_id` | INTEGER | | 群组 ID |
| `permission` | VARCHAR(20) | NOT NULL | 权限：none/read/write/admin |
| `inherited` | BOOLEAN | DEFAULT false | 是否继承自父目录 |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

---

### 2.2 数据表关系图

```
system.resources (来自 System 模块)
    ↓
manager.quick_view (快显任务)
    ↓ (fingerprint)
MinIO: manager/mvt-tiles/{fingerprint}/{z}/{x}/{y}.pbf

manager.data_sources
    ↓ 1:N
manager.data_source_permissions

manager.directories
    ↓ 1:N (自引用)
manager.directories (parent_id)
    ↓ 1:N
manager.directory_permissions
```

---

## 3. API 端点清单

### 3.1 基础端点

| 方法 | 端点 | 认证 | 说明 |
|------|------|------|------|
| GET | `/health` | 否 | 健康检查 |
| GET | `/` | 否 | 服务信息 |

---

### 3.2 资源管理 API

#### GET /api/resources - 列出数据源

**查询参数**:
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 10）
- `resource_type`: 按类型过滤

**响应** (200 OK):

```json
{
  "resources": [
    {
      "id": 1,
      "name": "PostgreSQL-生产库",
      "resource_type": "postgresql",
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

---

#### GET /api/resources/:id - 获取单个数据源

**响应** (200 OK): 返回 Resource 对象

---

### 3.3 数据探查 API

#### GET /api/data-explorer/tree - 获取所有资源树（兼容旧接口）

**响应** (200 OK): 返回资源树结构

---

#### GET /api/data-explorer/resources - 列出可用的存储引擎

**响应** (200 OK): 返回资源列表

---

#### GET /api/data-explorer/resources/:id/tree - 获取指定资源的 schema/表树

**响应** (200 OK):

```json
{
  "resource_id": 1,
  "resource_name": "PostgreSQL-生产库",
  "schemas": [
    {
      "name": "public",
      "tables": [
        {
          "name": "users",
          "type": "table",
          "row_count": 1000,
          "size_bytes": 102400
        }
      ]
    }
  ]
}
```

---

#### POST /api/data-explorer/resources/:id/refresh - 触发 Meta 服务刷新节点

**请求体**:

```json
{
  "node_id": 123
}
```

**响应** (200 OK):

```json
{
  "message": "刷新请求已提交"
}
```

---

#### GET /api/data-explorer/preview - 预览表/对象数据

**查询参数**:
- `resource_id`: 资源 ID（必填）
- `schema`: Schema 名称（数据库必填）
- `table`: 表名（数据库必填）
- `bucket`: Bucket 名称（对象存储必填）
- `path`: 对象路径（对象存储必填）
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 10）

**响应** (200 OK - 表数据):

```json
{
  "mode": "table",
  "columns": ["id", "name", "created_at"],
  "rows": [
    {"id": 1, "name": "test", "created_at": "2025-12-11T10:30:00Z"},
    {"id": 2, "name": "demo", "created_at": "2025-12-11T11:00:00Z"}
  ],
  "total": 2,
  "page": 1,
  "page_size": 10,
  "geometry_columns": ["geom"]
}
```

**响应** (200 OK - 对象预览):

```json
{
  "mode": "object",
  "object": {
    "name": "file.json",
    "size": 1024,
    "content_type": "application/json",
    "content": "{\"key\": \"value\"}"
  }
}
```

---

#### GET /api/data-explorer/video-stream - 视频流服务

**查询参数**:
- `resource_id`: 资源 ID
- `bucket`: Bucket 名称
- `path`: 视频路径

**响应** (200 OK): 返回视频流（Content-Type: video/mp4）

---

### 3.4 元数据扫描 API

#### POST /api/resources/:id/scan - 扫描资源元数据

**请求体**:

```json
{
  "schema_names": ["public", "data"],
  "scan_depth": "basic"
}
```

**响应** (202 Accepted):

```json
{
  "message": "扫描任务已提交",
  "scan_id": 42
}
```

---

#### GET /api/resources/:id/tables - 获取资源的表列表

**响应** (200 OK):

```json
{
  "tables": [
    {
      "schema": "public",
      "table": "users",
      "row_count": 1000,
      "size_bytes": 102400,
      "has_geometry": true
    }
  ]
}
```

---

#### GET /api/resources/:id/scan-tasks - 列出扫描任务

**响应** (200 OK): 返回扫描任务列表

---

#### POST /api/resources/:id/scan-tasks - 创建扫描任务

**请求体**:

```json
{
  "name": "每日扫描",
  "schedule_type": "daily",
  "schedule_time": "02:00"
}
```

**响应** (201 Created): 返回任务对象

---

#### PUT /api/resources/:id/scan-tasks/:task_id - 更新扫描任务

**请求体**: 同创建任务

**响应** (200 OK): 返回更新后的任务

---

#### DELETE /api/resources/:id/scan-tasks/:task_id - 删除扫描任务

**响应** (200 OK):

```json
{
  "message": "任务删除成功"
}
```

---

#### POST /api/resources/:id/scan-tasks/:task_id/trigger - 立即触发扫描

**响应** (202 Accepted):

```json
{
  "message": "扫描已触发",
  "run_id": 123
}
```

---

#### GET /api/resources/:id/scan-runs - 列出扫描运行记录

**查询参数**:
- `task_id`: 按任务过滤
- `status`: 按状态过滤
- `limit`: 记录数（默认 20）
- `offset`: 偏移量（默认 0）

**响应** (200 OK): 返回运行记录列表

---

#### GET /api/resources/:id/scan-runs/:run_id - 获取扫描运行详情

**响应** (200 OK): 返回运行详情

---

#### POST /api/resources/:id/scan-runs/manual - 发起即时扫描

**请求体**:

```json
{
  "schema_names": ["public"],
  "scan_depth": "deep"
}
```

**响应** (202 Accepted):

```json
{
  "run_id": 124,
  "message": "扫描已开始"
}
```

---

#### POST /api/tables/:id/manage - 纳入表管理

**请求体**:

```json
{
  "tags": ["重要", "生产"]
}
```

**响应** (200 OK):

```json
{
  "message": "已纳入管理"
}
```

---

#### POST /api/tables/:id/unmanage - 取消表管理

**响应** (200 OK):

```json
{
  "message": "已取消管理"
}
```

---

### 3.5 快显/预缓存 API

#### POST /api/resources/:id/spatial/:schema/:table/pre-cache - 触发预缓存任务

**请求体**:

```json
{
  "min_zoom": 6,
  "max_zoom": 18,
  "concurrency": 20,
  "priority": "default"
}
```

**响应** (202 Accepted):

```json
{
  "message": "预缓存任务已启动",
  "task_id": 10,
  "fingerprint": "9d471f70796ad82037b77f2e4439d0eb59dc762a"
}
```

---

#### GET /api/resources/:id/spatial/:schema/:table/pre-cache/status - 获取预缓存状态

**响应** (200 OK):

```json
{
  "id": 10,
  "status": "generating",
  "min_zoom": 6,
  "max_zoom": 18,
  "actual_max_zoom": 12,
  "total_tiles": 50000,
  "cached_tiles": 25000,
  "progress": 50.0,
  "started_at": "2025-12-11T10:00:00Z"
}
```

---

#### DELETE /api/resources/:id/spatial/:schema/:table/pre-cache - 清除预缓存

**响应** (200 OK):

```json
{
  "message": "预缓存已清除",
  "deleted_tiles": 50000
}
```

---

#### GET /api/pre-cache/tasks - 列出所有预缓存任务

**响应** (200 OK):

```json
{
  "tasks": [
    {
      "id": 10,
      "resource_id": 1,
      "schema_name": "public",
      "table_name": "cities",
      "status": "ready",
      "cached_tiles": 50000,
      "completed_at": "2025-12-11T12:00:00Z"
    }
  ],
  "total": 1
}
```

---

#### GET /api/pre-cache/statistics - 获取预缓存统计

**响应** (200 OK):

```json
{
  "total_tasks": 10,
  "ready": 8,
  "generating": 1,
  "failed": 1,
  "total_tiles": 500000,
  "total_size_mb": 250.5
}
```

---

### 3.6 MVT 瓦片 API

#### GET /api/resources/:id/spatial/tiles/:schema/:table/:z/:x/:y - 获取 MVT 瓦片

**查询参数**:
- `geom`: 几何列名（默认 "geom"）
- `srid`: 坐标系（默认 4326）
- `cols`: 返回列（逗号分隔，最多 8 列）

**响应** (200 OK):
- Content-Type: `application/x-protobuf`
- Body: MVT 二进制数据（gzip 压缩）

**缓存机制**: 三层缓存（LRU→Redis→MinIO）

---

#### GET /api/resources/:id/spatial/:schema/:table/tile-config - 获取瓦片配置

**响应** (200 OK):

```json
{
  "min_zoom": 6,
  "max_zoom": 18,
  "extent": [120.1, 30.2, 121.5, 31.8],
  "srid": 4326
}
```

---

### 3.7 全文搜索 API

#### GET /api/search/fulltext - 全文搜索

**查询参数**:
- `q`: 搜索关键词（必填）
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 10）

**响应** (200 OK):

```json
{
  "hits": [
    {
      "id": "table_1",
      "resource_name": "PostgreSQL-生产库",
      "schema": "public",
      "table": "users",
      "row_count": 1000,
      "score": 0.95
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 10
}
```

---

#### GET /api/search/history - 获取搜索历史

**响应** (200 OK):

```json
{
  "history": [
    {
      "id": 1,
      "query": "用户表",
      "created_at": "2025-12-11T10:00:00Z"
    }
  ]
}
```

---

#### DELETE /api/search/history/:id - 删除单条历史

**响应** (200 OK):

```json
{
  "message": "历史记录已删除"
}
```

---

#### DELETE /api/search/history - 清空搜索历史

**响应** (200 OK):

```json
{
  "message": "搜索历史已清空"
}
```

---

### 3.8 配置 API

#### GET /api/config/map - 获取地图配置

**响应** (200 OK):

```json
{
  "amap_key": "your_amap_key",
  "amap_security_js_code": "xxx",
  "tdt_key": "your_tdt_key"
}
```

---

### 3.9 要素 API

#### GET /api/resources/:id/features/:feature_id/centroid - 获取要素中心点坐标

**查询参数**:
- `schema`: schema 名称（必填）
- `table`: 表名（必填）
- `geom`: 几何列名（默认 "geom"）
- `primary_key`: 主键列名（默认 "id"）

**响应** (200 OK):

```json
{
  "lon": 120.5,
  "lat": 30.8
}
```

---

## 4. 基础设施使用

### 4.1 Redis 缓存

**MVT 瓦片缓存键模式**:

```
manager:cache:mvt:spatial:{fingerprint}:{z}:{x}:{y}
```

**缓存策略**:
- TTL: 24 小时
- 容量: 无限制（由 Redis maxmemory 控制）

**清理模式**:

```bash
# 清理指定资源的所有瓦片缓存
SCAN "manager:cache:mvt:*:resource:{resource_id}:*" 100
```

**内存 LRU 缓存**:
- 容量: 8192 条目（可配置 `MVT_CACHE_MEMORY_SIZE`）
- TTL: 5 分钟（可配置 `MVT_CACHE_MEMORY_TTL`）
- 用途: 热点瓦片快速访问

---

### 4.2 MinIO 存储

**Bucket**: `manager` (系统 MinIO，端口 9000-9001)

**目录结构**:

```
manager/
├── mvt-tiles/                          # MVT 瓦片存储目录
│   ├── {fingerprint}/                  # 指纹目录
│   │   ├── tiles/
│   │   │   └── z{z}/
│   │   │       └── {x}/
│   │   │           └── {y}.pbf        # 瓦片文件（Protobuf + gzip）
│   │   └── metadata.json              # 瓦片元数据
│   └── ...
└── preview-cache/                      # 预览缓存（可选）
    ├── tables/
    └── objects/
```

**瓦片文件**:
- 路径: `manager/mvt-tiles/{fingerprint}/tiles/z{z}/{x}/{y}.pbf`
- 格式: Protobuf（MVT 规范）
- 压缩: gzip
- 大小: 通常 2-50KB

---

### 4.3 Meilisearch 索引

**索引名**: `manager:files`

**文档结构**:

```json
{
  "id": "file_123",
  "resource_id": 1,
  "bucket": "mybucket",
  "path": "/data/file.csv",
  "name": "file.csv",
  "size": 1024,
  "mime_type": "text/csv",
  "created_at": "2025-12-11T10:00:00Z"
}
```

**可搜索字段**:
- `name` - 文件名
- `path` - 完整路径
- `mime_type` - 内容类型

---

## 5. 服务层架构

### 5.1 核心 Service 类

| Service 类 | 主要职责 |
|-----------|---------|
| `ResourceService` | 资源列表、单个资源查询 |
| `MetadataService` | 元数据扫描管理、任务管理 |
| `QuickViewService` | 快显任务管理 |
| `UnifiedMVTService` | 统一 MVT 瓦片获取（三层缓存） |
| `SpatialPreviewService` | MVT 缓存穿透 |
| `MVTService` | 实时 MVT 瓦片生成 |
| `FullTextSearchService` | 全文搜索 |
| `SearchHistoryService` | 搜索历史管理 |

---

### 5.2 缓存层次结构

```
请求 GET /api/resources/1/spatial/tiles/public/cities/10/512/384
  ↓
[1] 内存 LRU 缓存 (8192 items, 5 min TTL)
  ↓ (MISS)
[2] Redis 缓存 (24h TTL)
  key: manager:cache:mvt:spatial:{fingerprint}:10:512:384
  ↓ (MISS)
[3] MinIO (快显预生成的瓦片)
  path: manager/mvt-tiles/{fingerprint}/tiles/z10/512/384.pbf
  ↓ (MISS)
[4] 实时 PG 生成
  SQL: SELECT ST_AsMVT(...) FROM public.cities WHERE ...
  ↓
  回写 MinIO (可选) → 回写 Redis → 回写内存 → 返回
```

---

### 5.3 快显预缓存工作流程

1. 前端调用 POST /api/resources/:id/spatial/:schema/:table/pre-cache
2. TriggerQuickView 验证：
   - 检查是否已在生成中
   - 从 Meta 获取空间元数据（extent、SRID）
   - 验证 min_zoom/max_zoom 参数
3. 创建 quick_view 记录（状态：generating）
4. 计算 fingerprint：MD5(resource_id:schema:table)
5. 入队 Asynq 任务（优先级：critical/default/low）
6. Worker 处理：
   - Zoom 循环（min_zoom 到 max_zoom）
   - 每层生成瓦片 (x,y) 坐标范围
   - 实时监控：平均生成时间、瓦片大小
   - 动态停止：如果超过阈值，停止预生成
7. 写入 MinIO：manager/mvt-tiles/{fingerprint}/tiles/z{z}/{x}/{y}.pbf
8. 更新 quick_view：status=ready，统计信息

---

## 6. 配置参数

### 6.1 环境变量清单

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `PORT` | 8081 | 服务端口 |
| `DB_HOST` | localhost | PostgreSQL 主机 |
| `DB_PORT` | 5432 | PostgreSQL 端口 |
| `DB_USER` | addp | 数据库用户 |
| `DB_PASSWORD` | addp_password | 数据库密码 |
| `DB_NAME` | addp | 数据库名 |
| `DB_SCHEMA` | manager | Manager schema 名 |
| `REDIS_ADDR` | localhost:6379 | Redis 地址 |
| `REDIS_PASSWORD` | - | Redis 密码（可选） |
| `MINIO_ROOT_USER` | minioadmin | MinIO 访问键 |
| `MINIO_ROOT_PASSWORD` | minioadmin | MinIO 密钥 |
| `MVT_CACHE_MEMORY_SIZE` | 8192 | 内存 LRU 容量 |
| `MVT_CACHE_MEMORY_TTL` | 5m | 内存缓存 TTL |
| `SYSTEM_SERVICE_URL` | http://localhost:8080 | System 服务 URL |
| `AMAP_KEY` | - | 高德地图 API Key |
| `TDT_KEY` | - | 天地图 API Key |

---

## 7. 关键文件路径

| 文件 | 路径 | 说明 |
|------|------|------|
| 路由配置 | `manager/backend/internal/api/router.go` | 所有 API 端点定义 |
| 快显模型 | `manager/backend/internal/models/quick_view.go` | 快显任务模型 |
| 预览服务 | `manager/backend/internal/service/object_preview.go` | 预览插件系统 |
| MVT 服务 | `manager/backend/internal/service/unified_mvt_service.go` | 统一瓦片服务 |
| 快显服务 | `manager/backend/internal/service/quick_view_service.go` | 快显任务管理 |
| 前端预览 | `manager/frontend/src/components/previews/` | 预览组件 |
| 前端插件 | `manager/frontend/src/plugins/previews/index.js` | 预览插件注册 |

---

## 8. 相关文档

- [ADDP 平台架构文档](../CLAUDE.md)
- [Manager 模块详细文档](README.md)
- [System 模块数据结构文档](../system/DATA_STRUCTURES.md)
- [Meta 模块数据结构文档](../meta/DATA_STRUCTURES.md)
