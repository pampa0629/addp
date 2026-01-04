# Meta 模块说明

## 核心职责

Meta 模块是 ADDP 平台的**元数据管理中枢**，负责以下核心功能：

1. **元数据扫描** - 自动扫描存储引擎（数据库、对象存储）的元数据信息（Schema、Table、Object），构建数据目录树
2. **定时调度** - 支持定时扫描任务（基于 Cron 表达式），自动更新元数据
3. **元数据存储** - 将扫描结果存储到 `metadata.meta_node`（层级节点）和 `metadata.meta_item`（数据项）表
4. **全文检索索引** - 将元数据同步到 Meilisearch，支持全文搜索
5. **文档向量化** - 支持文档内容的多模态向量化（文本、图片、音频、视频），存储到 pgvector（实验性功能）
6. **扫描事件发布** - 扫描完成后发布 Redis 事件，通知其他模块（如 Manager）刷新缓存

## 关键架构

### 元数据扫描架构

```
扫描任务触发
  ├─ 手动触发（Manual）- API 调用
  ├─ 定时触发（Scheduled）- Cron 调度
  └─ API 触发（API）- 其他模块调用
  ↓
ScanTaskService (任务管理)
  ├─ 创建 ScanTaskRun（运行记录）
  ├─ 任务队列（Asynq Worker 或本地 goroutine）
  └─ 调用 ScanServiceNew.ExecuteScan()
  ↓
ScanServiceNew (扫描执行器)
  ├─ 验证租户权限（verifyResourceAccess）
  ├─ 判断存储引擎类型（数据库 vs 对象存储）
  ├─ 数据库扫描流程（DatabaseScanService）
  │  ├─ 🔌 获取插件: plugin.Get(engineType) → RelationalDBPlugin
  │  ├─ 创建连接池: plugin.GetOrCreatePoolFromFactory()
  │  ├─ 列出 Schema: relPlugin.ListSchemas(ctx, db)
  │  ├─ 过滤系统 Schema: relPlugin.IsSystemSchema(schemaName)
  │  ├─ 创建/更新 MetaNode（Schema 节点）
  │  ├─ 列出表: relPlugin.ListTables(ctx, db, schemaName)
  │  ├─ 创建/更新 MetaItem（Table 数据项）
  │  ├─ 列出字段: relPlugin.ListColumns(ctx, db, schemaName, tableName)
  │  ├─ 提取表 Schema（字段、类型、注释）
  │  ├─ 空间元数据提取（ST_Extent、SRID、几何类型）
  │  └─ 索引到 Meilisearch
  └─ 对象存储扫描流程（ObjectStorageScanService）
     ├─ 🔌 获取插件: plugin.Get(engineType) → ObjectStoragePlugin
     ├─ 列出 Bucket: objPlugin.ListBuckets(ctx, connInfo)
     ├─ 创建/更新 MetaNode（Bucket）
     ├─ 列出对象: objPlugin.ListObjects(ctx, connInfo, bucket, prefix, recursive)
     ├─ 创建/更新 MetaItem（Object）
     ├─ 提取文件元数据（扩展名、MIME、文件大小）
     ├─ 文档内容向量化（可选，需配置 Embedding Service）
     └─ 索引到 Meilisearch
  ↓
元数据持久化（PostgreSQL metadata schema）
  ├─ meta_node 表（层级节点）
  └─ meta_item 表（数据项）
  ↓
全文检索索引（Meilisearch）
  ├─ metadata_nodes_index
  └─ metadata_items_index
  ↓
向量化存储（PgVector, 可选）
  └─ metadata.document_embeddings
  ↓
事件发布（Redis）
  └─ meta:events:scan_completed
```

### 插件系统架构（v0.0.20 重构）

Meta 模块通过 `common/database/plugin` 三层插件架构直接访问存储引擎：

```
Meta 模块 (internal/service)
  ↓ 调用
plugin.Get(engineType) → EnginePlugin
  ↓ 类型断言
RelationalDBPlugin (数据库)          ObjectStoragePlugin (对象存储)
  ├─ ListSchemas()                   ├─ ListBuckets()
  ├─ ListTables()                    ├─ ListObjects()
  ├─ ListColumns()                   └─ GetObjectMetadata()
  ├─ IsSystemSchema()
  └─ GetTableMetadata()
  ↓ 实现
PostgreSQL / MySQL / Doris          MinIO / S3
ClickHouse / MongoDB
```

**重构优势**（v0.0.20）：
- ✅ **调用链简化**: 从 5 层缩减为 2 层（ScanService → Plugin → 数据库）
- ✅ **代码复用**: DatabaseScanService 和 ObjectStorageScanService 直接复用 common 插件
- ✅ **减少维护成本**: 删除 1200+ 行冗余 Scanner 适配层代码
- ✅ **统一接口**: Meta、Manager、Transfer 等模块使用相同的插件接口
- ✅ **扩展性强**: 新增存储引擎只需实现 common 插件接口

## 数据库文档

**遇到以下场景时,主动阅读对应文档**:

| 场景 | 必读文档 | 触发关键词 |
|------|---------|----------|
| 数据库表结构查询 | 对应单表文档 | 字段定义、索引、约束 |
| 表之间关系 | 数据库架构.md | 外键、关联、数据流 |
| API端点详情 | 对应单表文档 | API、接口、请求响应 |
| 扫描任务配置 | scan_tasks表.md | 定时任务、Cron表达式 |
| 元数据层级组织 | meta_node表.md | 树形结构、节点类型 |

### 架构说明
- [数据库架构](docs/数据库架构.md) - 表关系、数据流向、扫描流程设计

### 单表文档

详细的表结构和API说明文档：

- [meta_node表](docs/tables/meta_node表.md) - 元数据层级节点（Engine/Schema/Bucket/Prefix）
- [meta_item表](docs/tables/meta_item表.md) - 数据项（Table/View/Object/File）
- [scan_tasks表](docs/tables/scan_tasks表.md) - 扫描任务配置（定时调度、手动触发）
- [scan_task_runs表](docs/tables/scan_task_runs表.md) - 任务运行记录（状态、进度、结果摘要）
- [scan_logs表](docs/tables/scan_logs表.md) - 扫描日志（INFO/WARN/ERROR）

**重要**：修改表结构或API时，必须同步更新对应的单表文档。

### 数据模型设计

**MetaNode（层级节点）**：
- 代表数据库 Schema、对象存储 Bucket/Prefix（目录）
- 支持树形结构（parent_node_id）
- 聚合统计（item_count、total_size_bytes）
- 扫描状态追踪（scan_status、scanned_at）
- 扫描配置（scan_config JSONB）- 合并了 auto_enabled、cron、next_scan_at、error_message 等配置字段

**MetaItem（数据项）**：
- 代表数据库 Table/View、对象存储 Object（文件）
- 关联到 MetaNode（node_id 外键）
- 数据指纹（fingerprint）- 基于 engine_id + 路径的 SHA256 哈希，用于去重和数据血缘追踪
- 时间字段区分：
  - `data_updated_at` - 被扫描数据的最后更新时间（源数据修改时间）
  - `scanned_at` - Meta 模块扫描该数据项的时间
- 扩展属性（attributes JSONB）- 存储表 Schema、空间元数据、文件元数据等

**数据模型层次关系**：
```
Engine (system.engines)
  ↓
MetaNode (Bucket/Schema, depth=1)
  ↓
MetaNode (Prefix/Schema, depth=2+) [可选，对象存储支持多层目录]
  ↓
MetaItem (Object/Table)
```

### 定时调度架构

```
ScanTaskService 启动
  ↓
加载所有启用的 ScanTask（enabled=true）
  ↓
解析 Schedule（Cron 表达式）
  ↓
注册到 common/scheduler（基于 robfig/cron）
  ↓
定时触发 → 创建 ScanTaskRun → 入队 → 执行扫描
```

**支持的调度类型**：
- `manual` - 手动触发
- `cron` - Cron 表达式（如 `0 2 * * *` 每天凌晨 2 点）
- `once` - 一次性任务
- `daily` - 每日任务（指定时间）
- `weekly` - 每周任务（指定星期几和时间）

### 文档向量化架构（实验性）

```
对象存储扫描
  ↓
检测文档类型（通过 plugins/extractors）
  ↓
插件提取内容（TextExtractor/ImageExtractor/AudioExtractor/VideoExtractor）
  ↓
调用 Embedding Service（OpenAI-compatible API）
  ├─ 文本 Embedding（text-embedding-3-small）
  ├─ 图片 Embedding（clip-vit-base-patch32）
  ├─ 音频 Embedding（whisper + text embedding）
  └─ 视频 Embedding（frame sampling + image embedding）
  ↓
存储到 PgVector（metadata.document_embeddings）
  ├─ object_key（文件路径）
  ├─ embedding（向量，1536 维）
  ├─ modality（模态类型：text/image/audio/video）
  └─ metadata（提取的元数据 JSONB）
```

**向量化配置**（需在 .env 中启用）：
```bash
META_EMBEDDING_SERVICE_BASE_URL=http://embedding-service:8080
META_EMBEDDING_SERVICE_API_KEY=your_api_key
META_VECTOR_DB_HOST=postgres
META_VECTOR_DB_SCHEMA=metadata
META_VECTOR_DB_TABLE=document_embeddings
```

### 依赖的其他模块

- **System 模块** (`common/client/system.go`) - 获取存储引擎列表和连接信息，验证租户权限
- **Redis** - 三种用途：
  - 任务队列（Asynq，扫描任务异步执行）
  - 事件发布（scan_completed 事件）
  - 扫描去重（防止重复扫描同一资源）
- **Meilisearch** - 元数据全文检索索引
- **PostgreSQL** - 元数据存储 + 向量存储（pgvector 扩展）

### 使用的中间件资源

- **PostgreSQL Schema**: `metadata`
  - `meta_node` 表（层级节点）
  - `meta_item` 表（数据项）
  - `scan_task` 表（扫描任务定义）
  - `scan_task_run` 表（任务运行记录）
  - `document_embeddings` 表（向量嵌入，可选）
- **Redis Key 前缀**:
  - `meta:cache:engine:{id}` - 存储引擎缓存
  - `meta:events:scan_completed` - 扫描完成事件频道
  - `meta:scan_dedup:{engine_id}` - 扫描去重锁
- **Asynq Queue**: `meta:scan` (扫描任务队列)
- **Meilisearch Index**:
  - `metadata_nodes_index` - 节点索引
  - `metadata_items_index` - 数据项索引

## 重要文件位置

### 核心服务文件

- [scan_service_new.go](backend/internal/service/scan_service_new.go) - **统一扫描服务**（数据库 + 对象存储扫描核心逻辑）
- [scan_task_service.go](backend/internal/service/scan_task_service.go) - **扫描任务管理**（任务调度、队列管理、运行记录）
- [engine_service.go](backend/internal/service/engine_service.go) - **存储引擎服务**（缓存引擎列表，租户权限验证）
- [scan_spatial.go](backend/internal/service/scan_spatial.go) - **空间元数据提取**（ST_Extent、SRID、几何类型检测）
- [scan_dedup_service.go](backend/internal/service/scan_dedup_service.go) - **扫描去重服务**（基于 Redis 锁防止重复扫描）

### 插件系统文件

**元数据提取器插件** (plugins/extractors/):
- [text_extractor.go](../plugins/extractors/text_extractor.go) - 文本提取（txt、json、csv、md）
- [image_extractor.go](../plugins/extractors/image_extractor.go) - 图片元数据提取（EXIF、尺寸）
- [audio_extractor.go](../plugins/extractors/audio_extractor.go) - 音频元数据提取（时长、采样率）
- [video_extractor.go](../plugins/extractors/video_extractor.go) - 视频元数据提取（分辨率、时长、编码）
- [pdf_extractor.go](../plugins/extractors/pdf_extractor.go) - PDF 元数据提取（页数、作者、创建时间）

**插件注册机制**：
```go
// plugins/extractors/init.go
func init() {
    plugins.Register(&TextExtractor{})
    plugins.Register(&ImageExtractor{})
    // ...
}

// 在 scan_service_new.go 中自动导入：
_ "github.com/addp/meta/plugins/extractors"
```

### 数据模型文件

- [node.go](backend/internal/models/node.go) - MetaNode 模型（层级节点）
- [item.go](backend/internal/models/item.go) - MetaItem 模型（数据项）
- [scan_task.go](backend/internal/models/scan_task.go) - ScanTask 和 ScanTaskRun 模型（任务定义和运行记录）
- [dictionary.go](backend/internal/models/dictionary.go) - 数据字典模型（存储表 Schema）

### API 路由文件

- [backend/internal/api/router.go](backend/internal/api/router.go) - HTTP 路由定义
- [backend/internal/api/scan_handler.go](backend/internal/api/scan_handler.go) - 扫描 API（手动触发、查询结果）
- [backend/internal/api/task_handler.go](backend/internal/api/task_handler.go) - 任务管理 API（创建/更新/删除任务）
- [backend/internal/api/metadata_handler.go](backend/internal/api/metadata_handler.go) - 元数据查询 API（节点/数据项）

### 搜索索引文件

- [internal/search/indexer.go](backend/internal/search/indexer.go) - Meilisearch 索引器（元数据同步）
- [internal/search/config.go](backend/internal/search/config.go) - 索引配置（字段映射、排序规则）

### 配置文件

- [backend/internal/config/config.go](backend/internal/config/config.go) - 配置加载逻辑
- [.env](../.env) - 环境变量（`META_*` 前缀）

## 常见开发场景

### 场景 1：添加新的文档类型支持

**需求示例**：支持扫描 Excel 文件（.xlsx）的元数据

**步骤**：

1. **创建提取器插件**：
   ```bash
   # 在 plugins/extractors/ 创建新文件
   touch plugins/extractors/excel_extractor.go
   ```

2. **实现 `MetadataExtractor` 接口**：
   ```go
   package extractors

   import "github.com/addp/meta/plugins"

   type ExcelExtractor struct{}

   func (e *ExcelExtractor) Name() string {
       return "excel"
   }

   func (e *ExcelExtractor) Supports(contentType, extension string) bool {
       return extension == ".xlsx" || extension == ".xls"
   }

   func (e *ExcelExtractor) Extract(ctx context.Context, reader io.Reader, metadata map[string]interface{}) error {
       // 使用 excelize 库读取 Excel
       // 提取工作表数量、行数、列数等
       metadata["sheet_count"] = 3
       metadata["total_rows"] = 1000
       return nil
   }

   func init() {
       plugins.Register(&ExcelExtractor{})
   }
   ```

3. **重启 Meta 服务**：
   ```bash
   bash scripts/dev/restart.sh -meta
   ```

4. **测试扫描**：
   - 上传 .xlsx 文件到 MinIO
   - 触发对象存储扫描
   - 查看 `metadata.meta_item` 表的 `attributes` 字段，确认提取的元数据

**相关文件**：
- [plugins/extractors/text_extractor.go](../plugins/extractors/text_extractor.go) - 参考实现
- [scan_service_new.go:500-800](backend/internal/service/scan_service_new.go) - 对象扫描调用提取器的逻辑

### 场景 2：创建定时扫描任务

**需求示例**：每天凌晨 2 点自动扫描 PostgreSQL 数据库的所有 Schema

**步骤**：

1. **通过 API 创建扫描任务**：
   ```bash
   curl -X POST http://localhost:8082/api/v1/scan-tasks \
     -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d '{
       "name": "每日全量扫描",
       "description": "扫描所有 Schema 的元数据",
       "engine_id": 1,
       "schedule_type": "cron",
       "schedule": "0 2 * * *",
       "enabled": true,
       "parameters": {
         "schemas": ["public", "gis", "business"]
       }
     }'
   ```

2. **验证任务创建**：
   ```bash
   # 查看任务列表
   curl -H "Authorization: Bearer <token>" \
     http://localhost:8082/api/v1/scan-tasks

   # 查看任务详情（包含 next_run_at 预计执行时间）
   curl -H "Authorization: Bearer <token>" \
     http://localhost:8082/api/v1/scan-tasks/<task_id>
   ```

3. **手动触发测试**（不等到凌晨 2 点）：
   ```bash
   curl -X POST http://localhost:8082/api/v1/scan-tasks/<task_id>/trigger \
     -H "Authorization: Bearer <token>"
   ```

4. **查看运行历史**：
   ```bash
   # 查看所有运行记录
   curl -H "Authorization: Bearer <token>" \
     "http://localhost:8082/api/v1/scan-runs?task_id=<task_id>"

   # 查看单次运行详情（包含进度和错误信息）
   curl -H "Authorization: Bearer <token>" \
     http://localhost:8082/api/v1/scan-runs/<run_id>
   ```

**关键字段说明**：
- `schedule_type`: `cron` | `daily` | `weekly` | `once` | `manual`
- `schedule`: Cron 表达式（仅当 `schedule_type=cron` 时需要）
- `parameters.schemas`: 要扫描的 Schema 列表（空数组表示扫描所有）
- `enabled`: 是否启用定时调度

**相关文件**：
- [scan_task_service.go:180-250](backend/internal/service/scan_task_service.go) - 调度器初始化逻辑
- [scan_task_constants.go](backend/internal/service/scan_task_constants.go) - 任务状态常量

### 场景 3：调试扫描失败问题

**常见错误类型**：

1. **"engine access denied"** → 租户权限不足，检查用户的 tenant_id 是否与存储引擎的 tenant_id 匹配
2. **"failed to connect to database"** → 存储引擎连接信息错误，检查 `system.engines` 表的 `connection_info`
3. **"scan already in progress"** → 扫描去重锁未释放（Redis `meta:scan_dedup:{engine_id}` 键存在）

**调试步骤**：

```bash
# 1. 查看 Meta 后端日志
tail -f logs/meta-backend.log

# 2. 检查扫描任务运行记录（包含详细错误信息）
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8082/api/v1/scan-runs?status=failed&limit=10"

# 3. 查看具体运行详情
curl -H "Authorization: Bearer <token>" \
  http://localhost:8082/api/v1/scan-runs/<run_id>
# 关键字段：error_message（错误原因）、result_summary（扫描统计）

# 4. 检查 Redis 扫描锁（如果提示 "scan already in progress"）
docker exec -it addp-infra-redis redis-cli
> KEYS meta:scan_dedup:*
> TTL meta:scan_dedup:1  # 查看锁过期时间
> DEL meta:scan_dedup:1  # 手动删除锁（仅在确认没有扫描进程时）

# 5. 检查存储引擎缓存（如果提示 "engine not found"）
docker exec -it addp-infra-redis redis-cli
> GET meta:cache:engine:1
> DEL meta:cache:engine:1  # 清除缓存，强制重新加载

# 6. 测试存储引擎连接（通过 System 模块）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8080/api/v1/engines/1/test-connection
```

**关键日志位置**：
- [meta-backend.log](../logs/meta-backend.log) - Meta 后端日志
- [scan_service_new.go:100-200](backend/internal/service/scan_service_new.go) - 权限验证日志
- [scan_service_new.go:300-500](backend/internal/service/scan_service_new.go) - 数据库扫描详细日志
- [scan_service_new.go:700-900](backend/internal/service/scan_service_new.go) - 对象存储扫描详细日志

### 场景 4：启用文档向量化功能

**需求描述**：对上传到对象存储的文档（PDF、图片、文本）进行向量化，支持语义搜索

**前置条件**：
- 部署 OpenAI-compatible Embedding Service（如 text-embeddings-inference）
- PostgreSQL 已安装 pgvector 扩展
- 确保 Meta 后端有足够内存处理向量计算

**配置步骤**：

1. **配置 Embedding Service**（在 `.env` 中）：
   ```bash
   # Embedding Service 配置
   META_EMBEDDING_SERVICE_BASE_URL=http://embedding-service:8080
   META_EMBEDDING_SERVICE_API_KEY=your_api_key_here
   META_EMBEDDING_SERVICE_TIMEOUT=30s

   # 向量模型配置
   META_EMBEDDING_SERVICE_TEXT_MODEL=text-embedding-3-small
   META_EMBEDDING_SERVICE_IMAGE_MODEL=clip-vit-base-patch32
   META_EMBEDDING_SERVICE_AUDIO_MODEL=whisper-1
   META_EMBEDDING_SERVICE_VIDEO_MODEL=clip-vit-base-patch32

   # 向量数据库配置
   META_VECTOR_DB_HOST=postgres
   META_VECTOR_DB_PORT=15432
   META_VECTOR_DB_USER=addp
   META_VECTOR_DB_PASSWORD=your_password
   META_VECTOR_DB_DBNAME=addp
   META_VECTOR_DB_SCHEMA=metadata
   META_VECTOR_DB_TABLE=document_embeddings
   META_VECTOR_DB_DIMENSION=1536
   ```

2. **创建 pgvector 扩展和表**：
   ```sql
   -- 连接到 PostgreSQL
   psql -h localhost -p 15432 -U addp -d addp

   -- 创建 pgvector 扩展
   CREATE EXTENSION IF NOT EXISTS vector;

   -- 创建向量表（由 Meta 服务自动创建，也可手动创建）
   CREATE TABLE IF NOT EXISTS metadata.document_embeddings (
       id SERIAL PRIMARY KEY,
       object_key TEXT NOT NULL UNIQUE,
       embedding vector(1536) NOT NULL,
       modality VARCHAR(20) NOT NULL,
       metadata JSONB,
       created_at TIMESTAMP DEFAULT NOW()
   );

   -- 创建向量索引（加速相似度搜索）
   CREATE INDEX ON metadata.document_embeddings
   USING ivfflat (embedding vector_cosine_ops)
   WITH (lists = 100);
   ```

3. **重启 Meta 服务**：
   ```bash
   bash scripts/dev/restart.sh -meta

   # 查看日志确认向量化启用
   grep "VECTOR INIT" logs/meta-backend.log
   # 应输出：✅ [VECTOR INIT] Document vectorization ENABLED!
   ```

4. **触发对象存储扫描**（自动向量化）：
   ```bash
   curl -X POST http://localhost:8082/api/v1/manual-scan \
     -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d '{
       "engine_id": 2,
       "scan_type": "full",
       "scan_depth": "deep"
     }'
   ```

5. **验证向量化结果**：
   ```sql
   -- 查看向量化的文档数量
   SELECT COUNT(*) FROM metadata.document_embeddings;

   -- 查看具体向量记录
   SELECT object_key, modality, metadata
   FROM metadata.document_embeddings
   LIMIT 10;

   -- 测试语义搜索（查找与某个文档相似的文档）
   SELECT object_key,
          1 - (embedding <=> (SELECT embedding FROM metadata.document_embeddings WHERE object_key = 'test.pdf' LIMIT 1)) AS similarity
   FROM metadata.document_embeddings
   ORDER BY similarity DESC
   LIMIT 10;
   ```

**注意事项**：
- 向量化是 **CPU/内存密集** 操作，大量文档会消耗大量资源
- 默认只向量化 **可提取内容的文档**（插件支持的格式）
- 向量化失败不会阻塞扫描流程（只记录日志警告）

**相关文件**：
- [scan_service_new.go:86-100](backend/internal/service/scan_service_new.go) - 向量化启用入口
- [scan_service_new.go:800-1000](backend/internal/service/scan_service_new.go) - 对象向量化逻辑
- [common/embedding/](../common/embedding/) - Embedding 客户端实现
- [common/vectorstore/](../common/vectorstore/) - PgVector 存储实现

### 场景 5：优化大规模数据库扫描性能

**问题描述**：扫描包含 1000+ 个表的数据库非常慢

**优化方案**：

1. **并发扫描表** - Meta 默认串行扫描，可修改为并发：
   ```go
   // scan_service_new.go 中修改（需自行实现）
   // 使用 worker pool 并发扫描表
   var wg sync.WaitGroup
   semaphore := make(chan struct{}, 10) // 并发度 10
   for _, table := range tables {
       wg.Add(1)
       semaphore <- struct{}{}
       go func(t Table) {
           defer wg.Done()
           defer func() { <-semaphore }()
           s.scanTable(ctx, node, t)
       }(table)
   }
   wg.Wait()
   ```

2. **跳过大表采样** - 对超大表（如 > 1亿行）跳过数据采样：
   ```go
   // scan_service_new.go 中添加条件判断
   if rowCount > 100000000 {
       s.log.Warn("跳过超大表采样", "table", tableName, "row_count", rowCount)
       // 只扫描元数据，不提取 Schema 和采样数据
   }
   ```

3. **增量扫描** - 只扫描有变更的表（基于 `pg_stat_user_tables`）：
   ```sql
   -- 在 PostgreSQL 中查询有变更的表
   SELECT schemaname, tablename, n_tup_ins + n_tup_upd + n_tup_del AS total_changes
   FROM pg_stat_user_tables
   WHERE n_tup_ins + n_tup_upd + n_tup_del > 0
   ORDER BY total_changes DESC;
   ```

4. **调整扫描深度** - 对象存储扫描支持深度控制：
   ```bash
   # 浅度扫描（只扫描 Bucket，不扫描对象）
   curl -X POST http://localhost:8082/api/v1/manual-scan \
     -H "Authorization: Bearer <token>" \
     -d '{
       "engine_id": 2,
       "scan_type": "full",
       "scan_depth": "shallow"
     }'

   # 深度扫描（递归扫描所有子目录和对象）
   curl -X POST http://localhost:8082/api/v1/manual-scan \
     -H "Authorization: Bearer <token>" \
     -d '{
       "engine_id": 2,
       "scan_type": "full",
       "scan_depth": "deep"
     }'
   ```

5. **使用任务队列**（避免阻塞）：
   ```bash
   # 确保 .env 中配置了 Redis
   REDIS_HOST=redis
   REDIS_PORT=16379

   # Meta 服务会自动使用 Asynq Worker 队列执行扫描
   # 查看队列中的任务
   docker exec -it addp-infra-redis redis-cli
   > LLEN asynq:queues:meta:scan
   ```

**性能监控**：
```bash
# 查看扫描进度（通过运行记录）
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8082/api/v1/scan-runs/<run_id>" | jq '.progress_percent'

# 查看扫描统计
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8082/api/v1/scan-runs/<run_id>" | jq '.result_summary'
```

**相关文件**：
- [scan_service_new.go:300-600](backend/internal/service/scan_service_new.go) - 数据库扫描逻辑（可优化并发）
- [scan_task_service.go:100-150](backend/internal/service/scan_task_service.go) - 任务队列配置

## 注意事项

### 1. 租户隔离与权限验证

Meta 模块严格执行租户隔离：
- 所有扫描操作都会验证 `tenant_id` 是否与存储引擎的 `tenant_id` 匹配
- 超级管理员（`tenant_id = 0`）可以访问所有资源
- 普通用户只能访问自己租户的资源

**错误示例**：
```go
// ❌ 错误：直接查询 meta_node，跳过租户验证
var nodes []models.MetaNode
db.Where("engine_id = ?", engineID).Find(&nodes)

// ✅ 正确：通过 API 查询，自动应用租户过滤
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8082/api/v1/metadata/nodes?engine_id=1"
```

### 2. 扫描去重机制

为防止重复扫描同一资源（并发冲突），Meta 使用 **Redis 分布式锁**：

```go
// 扫描开始时获取锁
lockKey := fmt.Sprintf("meta:scan_dedup:%d", engineID)
acquired, err := dedupService.TryAcquire(ctx, engineID, 30*time.Minute)
if !acquired {
    return ErrScanAlreadyInProgress
}
defer dedupService.Release(ctx, engineID)
```

**陷阱**：
- 如果扫描进程异常退出（如 OOM），锁不会自动释放（需等待 TTL 过期，默认 30 分钟）
- 可手动删除 Redis 键强制释放锁：`DEL meta:scan_dedup:1`

### 3. 元数据更新策略

Meta 采用 **Upsert（存在则更新，不存在则插入）** 策略：

```go
// upsertNode 逻辑
// 1. 查询是否存在（包含软删除记录）
query := db.Unscoped().Where("engine_id = ? AND tenant_id = ? AND node_type = ? AND name = ?", ...)
err := query.First(&node).Error
if err == gorm.ErrRecordNotFound {
    // 2. 不存在 → 创建新节点
    db.Create(&node)
} else {
    // 3. 存在 → 更新节点（恢复软删除）
    updates := map[string]interface{}{"deleted_at": nil, ...}
    db.Unscoped().Model(&node).Updates(updates)
}
```

**重要**：
- 删除的节点不是物理删除，而是软删除（`deleted_at != NULL`）
- 重新扫描时会 **恢复** 软删除的节点
- 如果节点不再存在于源数据库，扫描完成后会标记为软删除

### 4. 空间元数据提取要点

对于 PostGIS 空间表，Meta 会自动提取：
- **几何列名** - 通过 `geometry_columns` 系统表查询
- **SRID** - 空间参考系 ID（如 4326 表示 WGS84）
- **Extent** - 数据边界范围（`ST_Extent`）
- **几何类型** - POINT、LINESTRING、POLYGON 等（`ST_GeometryType`）
- **空间索引** - 是否有 GiST 索引（查询 `pg_indexes`）

**性能警告**：
- `ST_Extent()` 是 **聚合函数**，对大表（> 百万行）可能很慢
- 可在扫描任务中配置 `skip_extent: true` 跳过 Extent 提取

**相关文件**：[scan_spatial.go](backend/internal/service/scan_spatial.go)

### 5. 与 Manager 模块的交互要点

- **Manager 依赖 Meta** - Manager 通过 Meta API 查询元数据（不直接访问 `metadata` schema）
- **扫描完成通知** - Meta 扫描完成后发布 `meta:events:scan_completed` Redis 事件，Manager 订阅该事件自动刷新缓存
- **用户权限透传** - Manager 调用 Meta API 时，必须携带用户的 JWT token（租户隔离）

**事件发布代码**：
```go
// scan_service_new.go
event := events.ScanCompletedEvent{
    EngineID:  engineID,
    TenantID: tenantID,
    ScanType:  events.ScanTypeDatabase,
    Timestamp: time.Now(),
}
scanEventPublisher.PublishScanCompleted(ctx, event)
```

**Manager 订阅代码**（在 Manager 模块中）：
```go
// manager/backend/internal/service/scan_event_handler.go
pubsub := redisClient.Subscribe(ctx, "meta:events:scan_completed")
for msg := range pubsub.Channel() {
    var event events.ScanCompletedEvent
    json.Unmarshal([]byte(msg.Payload), &event)
    // 清除对应存储引擎的缓存
    cacheManager.ClearEngineCache(event.EngineID)
}
```

### 6. Meilisearch 索引更新

扫描完成后，Meta 会自动将元数据同步到 Meilisearch：

```go
// scan_service_new.go
if s.indexer != nil {
    s.indexer.IndexNode(node)    // 索引节点
    s.indexer.IndexItem(item)    // 索引数据项
}
```

**索引字段配置**（在 `internal/search/config.go` 中）：
- **可搜索字段**: `name`, `full_name`, `attributes.*`
- **可过滤字段**: `engine_id`, `tenant_id`, `node_type`, `item_type`
- **排序字段**: `created_at`, `updated_at`, `row_count`, `size_bytes`

**注意事项**：
- Meilisearch 索引是 **异步** 的（扫描不会等待索引完成）
- 索引失败不会阻塞扫描流程（只记录日志警告）
- 可通过 Manager 模块的全文检索 API 查询索引结果

## 典型开发工作流

### 修改 Meta 后端代码后

```bash
# 1. 重启 Meta 后端服务（会自动重新编译）
bash scripts/dev/restart.sh -meta

# 2. 查看启动日志（确认编译成功）
tail -f logs/meta-backend.log

# 3. 测试 API（使用 Portal 登录获取 token）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8082/api/v1/metadata/nodes?engine_id=1
```

### 添加新的提取器插件后

```bash
# 1. 编写插件代码（参考场景 1）
# 2. 在 plugins/extractors/init.go 中注册
# 3. 重启服务
bash scripts/dev/restart.sh -meta

# 4. 验证插件加载（查看日志）
grep "元数据提取器" logs/meta-backend.log

# 5. 触发扫描测试
curl -X POST http://localhost:8082/api/v1/manual-scan \
  -H "Authorization: Bearer <token>" \
  -d '{"engine_id": 2, "scan_type": "full"}'
```

### 调试扫描任务失败

```bash
# 1. 查看失败的运行记录
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8082/api/v1/scan-runs?status=failed&limit=10"

# 2. 查看具体错误信息
curl -H "Authorization: Bearer <token>" \
  http://localhost:8082/api/v1/scan-runs/<run_id> | jq '.error_message'

# 3. 查看 Meta 后端日志（搜索关键词）
grep "扫描失败" logs/meta-backend.log
grep "run_id=<run_id>" logs/meta-backend.log

# 4. 检查扫描锁状态
docker exec -it addp-infra-redis redis-cli KEYS "meta:scan_dedup:*"

# 5. 手动重试扫描
curl -X POST http://localhost:8082/api/v1/scan-runs/<run_id>/retry \
  -H "Authorization: Bearer <token>"
```

## 相关文档

- **存储引擎插件系统** - [docs/数据库插件系统.md](../docs/数据库插件系统.md)
- **System 模块说明** - [system/CLAUDE.md](../system/CLAUDE.md)
- **Manager 模块说明** - [manager/CLAUDE.md](../manager/CLAUDE.md)
- **扫描任务 API 文档** - [backend/docs/api.md](backend/docs/api.md)（如果有）
- **元数据提取器开发指南** - [plugins/extractors/README.md](../plugins/extractors/README.md)（如果有）
