# Manager 模块说明

## 最近更新 (2026-01-14)

### 🚀 前端性能优化

**问题背景**：
- 懒加载每次展开目录重新加载整个引擎树（性能差 5-10倍）
- 客户端全量过滤搜索，大型树卡顿
- 组件过大（DataExplorer.vue 647行），维护困难

**优化方案**：

#### 1. 后端 API 新增

**增量加载 API**：
```
GET /api/manager/tree/:engine_id/node
参数：
  - locator: ResourceLocator URI（必填）
  - expand_depth: 展开深度（默认1）
响应：{ parent_locator, children: [...] }
```

**搜索 API**：
```
GET /api/manager/tree/:engine_id/search
参数：
  - q: 搜索关键词（最少2字符）
  - node_types: 节点类型过滤（可选）
  - limit: 返回数量限制（默认50）
响应：{ keyword, total, results: [{ node, path, match_type, score }] }
```

**实现位置**：
- Handler: [explorer_handler.go:189-300](manager/backend/internal/api/explorer_handler.go#L189-L300)
- Service: [explorer_service.go](manager/backend/internal/service/explorer_service.go) - `GetNodeChildren()` 和 `SearchNodes()` 方法

#### 2. 前端优化

**Store 层增强** ([explorer.js](manager/frontend/src/stores/explorer.js)):
- 新增节点级缓存：5分钟TTL
- `loadNodeChildren(locator, expandDepth)` - 增量加载替代全量重载
- `searchNodes(engineId, keyword, nodeTypes)` - 后端搜索替代客户端过滤
- `updateTreeNode(tree, locator, updates)` - 增量更新树节点

**组件拆分**：
- **ExplorerTree.vue** (~290行) - 封装树交互逻辑，使用增量加载
- **ExplorerSearch.vue** (~230行) - 封装搜索逻辑，500ms防抖
- **DataExplorer.vue** (~300行) - 简化为布局协调组件（原647行，减少53%）

**清理**：
- 删除重复的 `datasources.js`（37行）

#### 3. 性能提升

| 操作 | 优化前 | 优化后 | 提升 |
|------|-------|-------|------|
| 展开目录 | ~3-3.5s (全量重载) | <0.3s (增量加载) | **10-11x** |
| 搜索响应 | ~2.5s (客户端过滤) | <0.5s (后端搜索) | **5x** |
| 代码行数 | 647行 | 300行 | **-53%** |
| 缓存命中率 | 0% | ~90% (5分钟TTL) | - |

**关键改进**：
- ✅ 懒加载不再重新加载整个树，只加载当前节点的子节点
- ✅ 搜索从客户端O(n)遍历改为后端索引查询
- ✅ 5分钟节点缓存减少重复请求
- ✅ 组件职责清晰，代码更易维护

---

## 核心职责

Manager 模块是 ADDP 平台的**数据管理中枢**，负责以下核心功能：

1. **存储引擎管理** - 管理多类型数据库和对象存储的连接配置（8种引擎：PostgreSQL、MySQL、Doris、ClickHouse、MongoDB、Apache Spark、MinIO、S3）
2. **数据探查与预览** - 提供统一的数据预览能力，支持结构化数据、空间数据、文档、图片等多种格式
3. **元数据查询与展示** - 与 Meta 模块集成，展示数据目录树、表结构、文件属性等元数据信息
4. **数据检索** - 基于 Meilisearch + 向量数据库的混合检索（全文检索 + 语义检索），支持数据表、对象存储文件等
5. **空间数据可视化** - 提供 MVT（矢量瓦片）服务，支持大规模空间数据的地图渲染（含三层缓存架构）

## 关键架构

### 数据流架构

```
前端请求
  ↓
API Handler (internal/api/)
  ↓
Service 层 (internal/service/)
  ├─ MetadataService (数据探查协调)
  │  ├─ PreviewRegistry (预览插件注册表)
  │  │  ├─ PostgreSQL Provider (数据库表预览)
  │  │  ├─ MySQL/MongoDB Provider
  │  │  ├─ ObjectStorage Provider (文件/对象预览)
  │  │  └─ 外部插件 (preview/ 目录动态加载)
  │  └─ ObjectContentRegistry (内容处理器)
  │     ├─ Shapefile Handler (多文件复合流式处理)
  │     ├─ GeoJSON Handler
  │     ├─ Image/PDF Handler
  │     └─ Text/JSON Handler
  ├─ MVTService (空间瓦片生成)
  ├─ UnifiedMVTService (三层缓存穿透)
  ├─ QuickViewService (快速预览缓存)
  └─ SearchService (Meilisearch 集成)
  ↓
Repository 层 (internal/repository/)
  ├─ EngineRepository (system.engines 表访问)
  ├─ MetadataRepository (调用 System/Meta 客户端)
  └─ SearchHistoryRepository (manager.search_histories)
  ↓
外部依赖
  ├─ System 模块 (获取解密的存储引擎连接信息)
  ├─ Meta 模块 (查询元数据节点和数据项)
  ├─ Meilisearch (全文搜索引擎)
  ├─ Redis (缓存 + 任务队列 + 事件订阅)
  └─ 目标数据库/对象存储 (实时连接查询数据)
```

### 预览插件架构（核心设计）

Manager 采用 **插件化预览架构**，通过优先级链式调用实现多数据源适配：

```go
// 预览请求 → PreviewRegistry.Preview()
// 1. 按优先级排序所有插件
// 2. 逐个调用 provider.Supports(req)
// 3. 第一个支持的插件执行预览

// 插件类型示例：
- PostgreSQL Provider (priority: 90) → 处理 PostgreSQL 表/视图预览
- MongoDB Provider (priority: 88) → 处理 MongoDB 集合预览
- ObjectStorage Provider (priority: 95) → 处理 MinIO/S3 对象预览
- 外部插件 (priority: 自定义) → 动态加载自定义预览逻辑
```

**内容处理器架构（三种模式）**：
- `ContentHandler` - 传统字节数组处理（小文件，如 JSON/文本）
- `StreamableContentHandler` - 单文件流式处理（大文件，如图片/PDF）
- `CompositeStreamableContentHandler` - 多文件复合流式处理（如 Shapefile = .shp + .dbf + .shx）

### 空间数据 MVT 三层缓存架构

```
前端地图请求 → UnifiedMVTService
  ↓
Layer 1: Quick View 缓存 (PostgreSQL manager.quick_views)
  - 存储完整瓦片数据（支持离线使用）
  - 缓存键：(engine_id, schema, table, z, x, y)
  ↓ (Miss)
Layer 2: Spatial Preview 缓存 (Redis)
  - 存储 fingerprint → 瓦片 ID 映射
  - TTL: 1小时（热数据）
  ↓ (Miss)
Layer 3: 实时生成 (MVTService)
  - 连接数据库执行 ST_AsMVT() 查询
  - 生成结果后写入 Layer 2
```

### 数据库连接池管理

Manager 需要连接用户配置的多个数据库，采用 **动态连接池** 管理：

```go
// 数据库连接通过 common/database 插件系统统一管理
// 支持的插件类型：
- PostgreSQL (github.com/addp/common/database/plugins/postgresql)
- MySQL (github.com/addp/common/database/plugins/mysql)
- MongoDB (github.com/addp/common/database/plugins/mongodb)
- ClickHouse (github.com/addp/common/database/plugins/clickhouse)
- Doris (github.com/addp/common/database/plugins/doris)
- Apache Spark (github.com/addp/common/database/plugins/spark_sql)
- MinIO (github.com/addp/common/database/plugins/minio)
- S3 (github.com/addp/common/database/plugins/s3)

// 连接流程：
// 1. MetadataRepository.DecryptConnectionInfo() 解密存储引擎配置
// 2. 通过插件系统创建数据库客户端
// 3. 执行查询（表预览/数据采样/MVT 生成）
// 4. 连接池自动管理（由插件内部实现）
```

### 依赖的其他模块

- **System 模块** (`common/client/system.go`) - 获取解密的存储引擎连接信息，验证用户权限
- **Meta 模块** (`common/client/meta.go`) - 查询元数据节点（Node）和数据项（Item），获取扫描任务状态
- **Redis** - 三种用途：
  - 缓存层（空间预览 fingerprint）
  - 任务队列（Quick View 批量生成，基于 Asynq）
  - 事件订阅（监听 Meta 扫描完成事件，自动刷新缓存）

### 使用的中间件资源

- **PostgreSQL Schema**: `manager` (存储搜索历史 search_histories、Quick View 缓存 quick_views)
- **Redis Key 前缀**:
  - `manager:cache:engine:{id}` - 存储引擎缓存
  - `manager:spatial_preview:{fingerprint}` - 空间预览缓存
  - `manager:events:scan_completed` - 扫描完成事件频道
- **MinIO Bucket**: 无（Manager 不存储数据，只连接用户配置的存储引擎）
- **Asynq Queue**: `manager:quick_view` (Quick View 批量缓存生成任务)

## 数据库文档

**遇到以下场景时,主动阅读对应文档**:

| 场景 | 必读文档 | 触发关键词 |
|------|---------|----------|
| 数据库表结构查询 | 对应单表文档 | 字段定义、索引、约束 |
| 表之间关系 | 数据库架构.md | 外键、关联、数据流 |
| API端点详情 | 对应单表文档 | API、接口、请求响应 |
| 目录树管理 | directories表 | 文件夹、路径、目录结构 |
| 搜索历史 | search_history表 | 数据检索、混合检索、历史记录 |
| 空间快显 | quick_view表 | 瓦片缓存、MVT、预缓存 |

### 架构说明
- [数据库架构](docs/数据库架构.md) - 表关系、数据流向、设计决策

### 单表文档

详细的表结构和API说明文档：

- [directories表](docs/tables/directories表.md) - 目录结构表,文件系统风格的树形结构
- [search_history表](docs/tables/search_history表.md) - 搜索历史表,记录用户数据检索历史（全文检索 + 向量检索）
- [quick_view表](docs/tables/quick_view表.md) - 快显任务表,空间数据瓦片预缓存

**重要**：修改表结构或API时，必须同步更新对应的单表文档。

## 重要文件位置

### 核心服务文件

- [metadata_service.go](backend/internal/service/metadata_service.go) - **数据探查总协调器**（调度预览插件，整合 Meta 元数据）
- [object_preview.go](backend/internal/service/object_preview.go) - **对象存储预览插件**（MinIO/S3 文件/目录预览，支持多格式内容处理）
- [preview_registry.go](backend/internal/service/preview_registry.go) - **预览插件注册表**（按优先级链式调用插件）
- [mvt_service.go](backend/internal/service/mvt_service.go) - **MVT 瓦片生成服务**（实时执行 ST_AsMVT 查询）
- [unified_mvt_service.go](backend/internal/service/unified_mvt_service.go) - **统一 MVT 服务**（三层缓存穿透架构）
- [quick_view_service.go](backend/internal/service/quick_view_service.go) - **快速预览缓存服务**（批量生成/持久化存储）
- [search_service.go](backend/internal/service/search_service.go) - **混合检索服务**（Meilisearch + 向量数据库）
- [engine_cache.go](backend/internal/service/engine_cache.go) - **存储引擎缓存服务**（Redis 事件订阅自动刷新）

### 预览插件文件

**内置插件** (backend/internal/service/):
- [preview_provider_database.go](backend/internal/service/preview_provider_database.go) - **通用关系型数据库预览**（自动支持 PostgreSQL、MySQL、ClickHouse、Doris 等）
- [preview_provider_file.go](backend/internal/service/preview_provider_file.go) - **通用文件表预览**（支持 CSV、Excel、Shapefile、GeoJSON 等）
- [preview_provider_mongodb.go](backend/internal/service/preview_provider_mongodb.go) - MongoDB 集合预览（NoSQL 特殊处理）
- [preview_provider_schema.go](backend/internal/service/preview_provider_schema.go) - Schema 节点预览
- [builtin/init.go](backend/internal/service/builtin/init.go) - **插件工厂注册**（启动时自动注册所有内置插件）

**插件配置** (backend/plugins/):
- [providers/](backend/plugins/providers/) - 预览提供程序 JSON 配置（3 个文件）
- [content/](backend/plugins/content/) - 内容处理器 JSON 配置（13 个文件）
- [README.md](backend/plugins/README.md) - 插件系统架构说明

**架构演进**：系统已从"每种数据库/格式独立实现"升级为"通用实现 + 声明式配置 + 工厂模式"，提高了可扩展性和维护性。

**内容处理器** (backend/internal/service/):
- [object_content_shapefile.go](backend/internal/service/object_content_shapefile.go) - Shapefile 多文件复合处理（.shp + .dbf + .shx）
- [object_content_plugin.go](backend/internal/service/object_content_plugin.go) - 内容处理器接口定义
- [object_content_plugin_loader.go](backend/internal/service/object_content_plugin_loader.go) - 外部插件动态加载

### API 路由文件

- [backend/internal/api/handler.go](backend/internal/api/handler.go) - HTTP 路由定义
- [backend/internal/api/middleware.go](backend/internal/api/middleware.go) - 认证中间件（JWT 验证）
- [backend/internal/api/preview.go](backend/internal/api/preview.go) - 数据预览 API
- [backend/internal/api/mvt.go](backend/internal/api/mvt.go) - MVT 瓦片 API

### 数据模型文件

- [internal/models/models.go](backend/internal/models/models.go) - 核心数据模型（Engine, Directory, TablePreview, ObjectPreview）
- [internal/models/quick_view.go](backend/internal/models/quick_view.go) - Quick View 缓存模型

### 前端视图文件

- [frontend/src/views/DataExplorer.vue](frontend/src/views/DataExplorer.vue) - **数据探查主界面**（树形目录 + 数据预览）
- [frontend/src/views/Preview.vue](frontend/src/views/Preview.vue) - 数据预览组件
- [frontend/src/views/DataSources.vue](frontend/src/views/DataSources.vue) - 存储引擎管理界面
- [frontend/src/views/Metadata.vue](frontend/src/views/Metadata.vue) - 元数据查看界面
- [frontend/src/views/DataRetrieval.vue](frontend/src/views/DataRetrieval.vue) - 数据检索界面（混合检索：全文 + 向量）
- [frontend/src/views/SpatialPreview.vue](frontend/src/views/SpatialPreview.vue) - 空间数据地图预览

### 配置文件

- [backend/internal/config/config.go](backend/internal/config/config.go) - 配置加载逻辑
- [.env](../.env) - 环境变量（`MANAGER_*` 前缀）
- [docker-compose.yml](../docker-compose.yml) - 服务定义（manager-backend, manager-frontend）

## 常见开发场景

### 场景 1：添加新的数据格式预览支持

**需求示例**：支持预览 Parquet 文件

**步骤**：

1. **创建内容处理器**（如果需要特殊格式解析）：
   ```bash
   # 在 backend/internal/service/ 创建新文件
   touch backend/internal/service/object_content_parquet.go
   ```

2. **实现 `ContentHandler` 或 `StreamableContentHandler` 接口**：
   ```go
   type ParquetContentHandler struct{}

   func (h *ParquetContentHandler) Supports(req *ObjectContentRequest) bool {
       return req.Extension == ".parquet"
   }

   func (h *ParquetContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectFetcher) (*ObjectPreviewContent, bool, error) {
       // 解析 Parquet 文件逻辑
   }
   ```

3. **注册到内容注册表**（在 `main.go` 或 `init()` 函数中）：
   ```go
   contentRegistry.Register(&ParquetContentHandler{})
   ```

4. **测试预览**：
   - 上传 Parquet 文件到 MinIO
   - 访问 `/api/v1/preview?engineId=X&schema=bucket&table=file.parquet`
   - 验证返回的 JSON 结构

**相关文件**：
- [object_content_plugin.go:15-50](backend/internal/service/object_content_plugin.go) - 内容处理器接口定义
- [object_preview.go:325-404](backend/internal/service/object_preview.go) - 内容处理器调用逻辑

### 场景 2：添加新的数据库类型预览插件

**需求示例**：支持 Elasticsearch 数据预览

**步骤**：

1. **创建预览插件文件**：
   ```bash
   touch backend/internal/service/preview_provider_elasticsearch.go
   ```

2. **实现 `PreviewProvider` 接口**：
   ```go
   type ElasticsearchPreviewProvider struct {
       metadataRepo *repository.MetadataRepository
       priority     int
   }

   func (p *ElasticsearchPreviewProvider) Name() string {
       return "builtin:elasticsearch"
   }

   func (p *ElasticsearchPreviewProvider) Priority() int {
       return 85 // 优先级低于 PostgreSQL (90)
   }

   func (p *ElasticsearchPreviewProvider) Supports(req *PreviewRequest) bool {
       return req.Engine != nil &&
              strings.EqualFold(req.Engine.EngineType, "elasticsearch")
   }

   func (p *ElasticsearchPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
       // 1. 解密连接信息
       // 2. 连接 Elasticsearch
       // 3. 查询数据
       // 4. 返回 TablePreview 结构
   }
   ```

3. **注册插件**（在 `backend/internal/service/builtin/register.go`）：
   ```go
   func Register(registry *service.PreviewRegistry, metadataRepo *repository.MetadataRepository) error {
       registry.Register(NewElasticsearchPreviewProvider(metadataRepo))
       return nil
   }
   ```

4. **测试预览**：
   - 在 System 模块创建 Elasticsearch 存储引擎
   - 访问 `/api/v1/preview?engineId=X&schema=index_name`

**相关文件**：
- [preview_registry.go:1-120](backend/internal/service/preview_registry.go) - 预览注册表实现
- [preview_provider_database.go:1-223](backend/internal/service/preview_provider_database.go) - 通用数据库插件参考实现
- [builtin/init.go](backend/internal/service/builtin/init.go) - 插件工厂注册示例

### 场景 3：调试数据预览失败问题

**常见错误类型**：

1. **"failed to decrypt connection info"** → 检查 System 模块的 `ENCRYPTION_KEY` 是否与 Manager 模块一致
2. **"failed to init minio client"** → 验证存储引擎的连接信息（endpoint/access_key/secret_key）
3. **"no preview provider supports this request"** → 检查预览插件注册和优先级

**调试步骤**：

```bash
# 1. 查看 Manager 后端日志
tail -f logs/manager-backend.log

# 2. 检查预览插件注册情况（启动日志）
grep "数据预览" logs/manager-backend.log
# 输出示例：数据预览: 已激活预览插件 providers=[builtin:postgres builtin:mysql ...]

# 3. 测试存储引擎连接（通过 API）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8081/api/v1/engines/<engine_id>/test

# 4. 检查 Meta 模块元数据（是否已扫描）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8082/api/v1/metadata/nodes?engine_id=<id>
```

**关键日志位置**：
- [manager-backend.log](../logs/manager-backend.log) - Manager 后端日志
- [metadata_service.go:200-250](backend/internal/service/metadata_service.go) - 预览调用入口（有详细日志）

### 场景 4：优化空间数据地图加载速度

**问题描述**：空间数据表有 100 万条记录，地图加载很慢

**优化方案**：

1. **启用 Quick View 批量缓存**（推荐）：
   ```bash
   # 调用 API 触发批量缓存生成
   curl -X POST -H "Authorization: Bearer <token>" \
     http://localhost:8081/api/v1/quick-views/batch \
     -d '{
       "engine_id": 1,
       "schema": "public",
       "table": "large_table",
       "zoom_levels": [0, 1, 2, 3, 4, 5]
     }'

   # 后台任务队列会异步生成瓦片缓存
   # 查看任务进度（Redis Asynq）
   docker exec -it addp-infra-redis redis-cli
   > LLEN asynq:queues:manager:quick_view
   ```

2. **检查数据库空间索引**：
   ```sql
   -- 确保几何字段有空间索引
   SELECT tablename, indexname
   FROM pg_indexes
   WHERE schemaname = 'public'
     AND tablename = 'large_table'
     AND indexdef LIKE '%USING gist%';

   -- 如果没有，创建索引
   CREATE INDEX idx_geom ON large_table USING GIST(geom);
   ```

3. **调整 MVT 查询参数**（在 MVTService 中）：
   - 减少 `simplify_tolerance`（降低简化程度）
   - 调整 `buffer_size`（减少缓冲区）
   - 限制 `feature_limit`（每个瓦片最大要素数）

**相关文件**：
- [quick_view_service.go:1-300](backend/internal/service/quick_view_service.go) - Quick View 缓存生成逻辑
- [mvt_service.go:50-150](backend/internal/service/mvt_service.go) - MVT 查询参数配置
- [unified_mvt_service.go:1-200](backend/internal/service/unified_mvt_service.go) - 三层缓存调用逻辑

### 场景 5：集成外部预览插件

**需求示例**：加载第三方开发的 HDF5 文件预览插件

**目录结构**：
```
manager/
├── backend/
│   └── plugins/
│       └── preview/
│           └── hdf5/
│               ├── plugin.so (编译的 Go 插件)
│               └── config.yaml (插件配置)
```

**插件开发指南**：

1. **编写插件代码** (`hdf5_plugin.go`)：
   ```go
   package main

   import "github.com/addp/manager/internal/service"

   type HDF5PreviewProvider struct{}

   func (p *HDF5PreviewProvider) Name() string { return "external:hdf5" }
   func (p *HDF5PreviewProvider) Priority() int { return 80 }
   func (p *HDF5PreviewProvider) Supports(req *service.PreviewRequest) bool {
       // 支持逻辑
   }
   func (p *HDF5PreviewProvider) Preview(ctx context.Context, req *service.PreviewRequest) (*models.TablePreview, error) {
       // 预览逻辑
   }

   var Provider service.PreviewProvider = &HDF5PreviewProvider{}
   ```

2. **编译为插件**：
   ```bash
   go build -buildmode=plugin -o manager/backend/plugins/preview/hdf5/plugin.so hdf5_plugin.go
   ```

3. **配置环境变量**：
   ```bash
   # .env 文件
   MANAGER_PREVIEW_PLUGIN_DIR=/app/manager/backend/plugins/preview
   ```

4. **重启 Manager 服务**：
   ```bash
   bash scripts/dev/restart.sh -manager

   # 查看日志确认插件加载
   grep "外部预览插件" logs/manager-backend.log
   ```

**相关文件**：
- [preview_plugin_loader.go:1-150](backend/internal/service/preview_plugin_loader.go) - 外部插件加载逻辑
- [plugin_config.go:1-50](backend/internal/service/plugin_config.go) - 插件配置解析

## 注意事项

### 1. 数据库连接管理

- Manager 需要连接**用户配置的多个数据库**，不是自己的数据库
- 连接信息加密存储在 `system.engines` 表中，解密需要统一的 `ENCRYPTION_KEY`
- 所有数据库连接必须通过 `common/database` 插件系统统一管理，不要直接使用 GORM/SQL
- **重要**：预览服务不做数据缓存，每次请求都实时连接目标数据库（MVT 除外，有三层缓存）

### 2. 预览插件优先级设计

预览插件按 **优先级从高到低** 依次调用，第一个 `Supports()` 返回 true 的插件执行预览：

```go
// 推荐的优先级范围：
// 100-90: 高优先级（对象存储，MinIO/S3）
// 89-80:  中优先级（关系型数据库 PostgreSQL/MySQL）
// 79-70:  低优先级（NoSQL 数据库 MongoDB/ClickHouse）
// 69-60:  外部插件（自定义格式）
```

**陷阱**：如果两个插件都返回 `Supports() = true`，只有高优先级的会执行（低优先级被跳过）

### 3. 与 Meta 模块的交互要点

- **Manager 不做元数据扫描**，只负责展示 Meta 模块扫描的结果
- **元数据缓存刷新机制**：Manager 通过 Redis 订阅 `meta:events:scan_completed` 事件，自动清理对应存储引擎的缓存
- **用户权限隔离**：Manager 调用 Meta API 时，必须携带用户的 JWT token（租户隔离）

**错误示例**：
```go
// ❌ 错误：使用 Manager 的 InternalAPIKey 调用 Meta
metaClient := commonClient.NewMetaClientWithInternalKey(metaURL, internalKey)

// ✅ 正确：从 Gin context 提取用户 token
if ginCtx, ok := ctx.(*gin.Context); ok {
    authHeader := ginCtx.GetHeader("Authorization")
    parts := strings.SplitN(authHeader, " ", 2)
    if len(parts) == 2 {
        metaClient = commonClient.NewMetaClient(metaURL, parts[1])
    }
}
```

**相关文件**：[object_preview.go:150-168](backend/internal/service/object_preview.go)

### 4. MVT 服务性能优化要点

**三层缓存穿透顺序**（必须严格遵守）：
1. Quick View (PostgreSQL) - 持久化存储，支持离线使用
2. Spatial Preview (Redis) - 热数据缓存，TTL 1小时
3. 实时生成 (MVTService) - 数据库查询 ST_AsMVT()

**缓存键设计**：
- Quick View: `(engine_id, schema, table, z, x, y)` - 精确匹配
- Spatial Preview: `fingerprint` → `tile_id` 映射 - 需要先查询 fingerprint

**性能陷阱**：
- ❌ 不要在 `ST_AsMVT()` 查询中使用 `ORDER BY`（会导致全表扫描）
- ❌ 不要在 `WHERE` 子句中使用复杂的几何计算（用空间索引）
- ✅ 优先生成 Quick View 缓存（批量任务，避免实时查询）

### 5. 模块间依赖关系

```
Manager 模块依赖：
├─ System 模块 (必须) - 获取解密的存储引擎连接信息
├─ Meta 模块 (可选) - 查询元数据节点和数据项
├─ Redis (必须) - 缓存 + 任务队列 + 事件订阅
└─ Meilisearch (可选) - 混合检索功能（全文检索 + 向量检索）

被依赖：
├─ Console 模块 - 提供数据探查 iframe 入口
└─ Orchestrator 模块 - 注册为 TaskProvider（提供数据预览任务）
```

**启动顺序要求**：
1. 基础设施（PostgreSQL、Redis、MinIO、Meilisearch）
2. System 模块（提供认证和存储引擎管理）
3. Meta 模块（可选，提供元数据查询）
4. Manager 模块

### 6. 前端组件复用

Manager 前端大量使用 `common-frontend` 共享组件：

**基础组件** (`common-frontend/basic`):
- `StorageEngineForm` - 存储引擎表单（8种数据库统一配置界面）
- `ImagePreview` - 图片预览
- `formatters` - 数据格式化工具（文件大小、时间戳等）

**地图组件** (`common-frontend/map`):
- `GeoJsonPreview` - GeoJSON 地图预览
- `ShapefilePreview` - Shapefile 地图预览
- `TablePreview` - 表格数据预览（带几何字段识别）

**重要**：修改这些共享组件会影响所有使用它们的模块（Manager、Meta、Transfer 等）

**相关文档**：[common-frontend/README.md](../common-frontend/README.md)

## 典型开发工作流

### 修改 Manager 后端代码后

```bash
# 1. 重启 Manager 后端服务（会自动重新编译）
bash scripts/dev/restart.sh -manager

# 2. 查看启动日志（确认编译成功）
tail -f logs/manager-backend.log

# 3. 测试 API（使用 Console 登录获取 token）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8081/api/v1/engines
```

### 修改 Manager 前端代码后

```bash
# 前端是 Vite 热更新模式，保存代码会自动刷新浏览器
# 如果遇到问题，手动重启：
bash scripts/dev/restart.sh -manager

# 访问 Console 查看效果
open http://localhost:5170
```

### 添加新的预览插件后

```bash
# 1. 编写插件代码（参考场景 2）
# 2. 注册插件到 PreviewRegistry
# 3. 重启服务
bash scripts/dev/restart.sh -manager

# 4. 验证插件加载（查看日志）
grep "数据预览: 已激活预览插件" logs/manager-backend.log

# 5. 测试预览 API
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8081/api/v1/preview?engineId=1&schema=test&table=table1"
```

## 相关文档

- **存储引擎插件系统** - [docs/数据库插件系统.md](../docs/数据库插件系统.md)
- **共享组件使用指南** - [common-frontend/README.md](../common-frontend/README.md)
- **System 模块说明** - [system/CLAUDE.md](../system/CLAUDE.md)
- **Meta 模块说明** - [meta/CLAUDE.md](../meta/CLAUDE.md)（待生成）
- **API 文档** - [backend/docs/api.md](backend/docs/api.md)（如果有）
