# MVT 空间数据快显实现总结

## 概述

本实现将 labs/mvt 的 MVT (Mapbox Vector Tiles) 瓦片预缓存功能完整集成到 ADDP 平台的 meta 模块中，实现了空间数据的快速渲染。

## 核心设计

### 1. 指纹机制
- **不可变指纹**: `SHA256(resID:schema.table)` - 仅基于表物理位置，不依赖数据内容
- **目录结构**: MinIO 路径 `mvt-tiles/{fingerprint}/tiles/z{z}/{x}_{y}.mvt.gz`
- **无历史版本**: 数据更新时直接删除旧瓦片并重新生成

### 2. 自适应缩放停止策略
```go
// 停止条件（同时满足）:
stats.AvgGenTimeMs < 3000 AND stats.AvgSizeKB < 50
```
- 自动检测最优缩放级别，避免过度预处理
- 平均生成时间 < 3s 且平均瓦片大小 < 50KB 时停止

### 3. 增量更新策略

**变更检测** (使用 `pg_stat_user_tables`):
```sql
SELECT n_tup_ins + n_tup_upd + n_tup_del as total_changes,
       n_live_tup, n_dead_tup
FROM pg_stat_user_tables
WHERE schemaname = $1 AND relname = $2
```

**精确区域检测** (可选，需要 `updated_at` 字段):
```sql
SELECT ST_Extent(ST_Transform(geom, 4326))
FROM schema.table
WHERE updated_at > $last_preprocess_time
```

- 只重新生成受影响的瓦片
- 未变更的瓦片保持不变

## 实现阶段

### Phase 1: 基础架构 ✅

**1.1 指纹计算模块** ([common/models/fingerprint.go](../common/models/fingerprint.go))
```go
// 提炼到 common 模块，供所有服务使用
func GenerateTableFingerprint(resID uint, schema, tableName string) string
func GenerateObjectFingerprint(resID uint, bucket, objectPath string) string
func GenerateItemFingerprint(resID uint, identifier string) string
```

**1.2 预处理配置扩展** ([system/backend/internal/models/resource.go](../system/backend/internal/models/resource.go))
```go
type PreprocessingConfig struct {
    Enabled     bool              `json:"enabled"`
    AutoTrigger bool              `json:"auto_trigger"`
    Types       []string          `json:"types"` // ["mvt_tiles", "vector_embedding"]
    MVTConfig   *MVTPreprocessConfig `json:"mvt_config,omitempty"`
}

type MVTPreprocessConfig struct {
    MaxZoom          int     `json:"max_zoom"`           // 0-18, default 18
    Concurrency      int     `json:"concurrency"`        // 1-20, default 10
    StopThresholdSec float64 `json:"stop_threshold_sec"` // default 3.0
    StopThresholdKB  float64 `json:"stop_threshold_kb"`  // default 50.0
}
```

**1.3 MinIO 初始化脚本** ([scripts/setup/init-minio-mvt.sh](../scripts/setup/init-minio-mvt.sh))
```bash
#!/bin/bash
# 创建 mvt-tiles bucket
# 设置公开读权限（前端直接访问）
mc mb addp-minio/mvt-tiles
mc anonymous set download addp-minio/mvt-tiles
```

### Phase 2: MVT 核心实现 ✅

**2.1 数据模型** ([meta/backend/internal/mvt/models.go](../meta/backend/internal/mvt/models.go))
```go
type TileCoord struct {
    Z int `json:"z"`
    X int `json:"x"`
    Y int `json:"y"`
}

type SpatialMetadata struct {
    GeometryColumn  string
    SRID            int
    Extent          []float64  // [minLng, minLat, maxLng, maxLat]
    GeometryTypes   []string
    HasSpatialIndex bool
    IndexName       string
    HasUpdatedAt    bool
    UpdatedAtColumn string
}

type PreprocessMetadata struct {
    Fingerprint     string
    StartedAt       time.Time
    CompletedAt     time.Time
    TotalTiles      int
    GeneratedTiles  int
    EmptyTiles      int
    MaxZoom         int
    StopReason      string // "reached_max_zoom" | "adaptive_threshold"
    DurationMs      int64
    AvgTileSizeKB   float64
    StorageSizeMB   float64
    TableStats      *TableStats
}
```

**2.2 瓦片生成器** ([meta/backend/internal/mvt/tile_generator.go](../meta/backend/internal/mvt/tile_generator.go:1))
```go
func (g *TileGenerator) GenerateTile(
    ctx context.Context,
    item *metaModels.MetaItem,
    tenantID uint,
    z, x, y int,
) ([]byte, error)
```
- 使用 PostGIS `ST_AsMVT()` 生成瓦片
- Web Mercator 投影 (EPSG:3857)
- 4096 extent, 64 buffer

**2.3 缓存服务** ([meta/backend/internal/mvt/cache_service.go](../meta/backend/internal/mvt/cache_service.go:1))
```go
// MinIO 持久化存储
func (s *CacheService) PutTile(ctx, fingerprint string, z, x, y int, data []byte) error
func (s *CacheService) GetTile(ctx, fingerprint string, z, x, y int) ([]byte, error)
func (s *CacheService) DeleteTile(ctx, fingerprint string, z, x, y int) error
func (s *CacheService) PutMetadata(ctx, fingerprint string, metadata *PreprocessMetadata) error
```

**2.4 预处理服务** ([meta/backend/internal/mvt/preprocess_service.go](../meta/backend/internal/mvt/preprocess_service.go:1))
```go
func (s *PreprocessService) StartPreprocess(
    ctx context.Context,
    item *metaModels.MetaItem,
    tenantID uint,
    cfg PreprocessConfig,
) (*PreprocessMetadata, error)
```
- 批量生成瓦片（并发控制）
- 自适应停止逻辑
- 跳过空瓦片
- 进度追踪

**2.5 增量更新服务** ([meta/backend/internal/mvt/incremental_updater.go](../meta/backend/internal/mvt/incremental_updater.go:1))
```go
func (u *IncrementalUpdater) DetectChanges(...) (changed bool, newStats *TableStats, err error)
func (u *IncrementalUpdater) GetChangedExtent(...) ([]float64, int, error)
func (u *IncrementalUpdater) CalculateAffectedTiles(...) []TileCoord
func (u *IncrementalUpdater) PerformIncrementalUpdate(...) error
```

**2.6 空间元数据提取** ([meta/backend/internal/service/scan_spatial.go](../meta/backend/internal/service/scan_spatial.go:1))
```go
// 在深度扫描时自动检测空间表
func (s *ScanServiceNew) scanSpatialMetadata(...) (*mvt.SpatialMetadata, error)
func (s *ScanServiceNew) detectGeometryColumn(...) (string, error)
func (s *ScanServiceNew) querySRID(...) (int, error)
func (s *ScanServiceNew) calculateExtent(...) ([]float64, error)
func (s *ScanServiceNew) checkSpatialIndex(...) (bool, string, error)
```

### Phase 3: 任务集成 ✅

**3.1 Worker 任务类型扩展** ([meta/backend/internal/worker/queue.go](../meta/backend/internal/worker/queue.go:1))
```go
const (
    TypeScanTask       = "meta:scan"
    TypePreprocessTask = "meta:preprocess" // 新增
)

type PreprocessTaskPayload struct {
    ItemID   uint   `json:"item_id"`
    TenantID uint   `json:"tenant_id"`
    Type     string `json:"type"` // "mvt_tiles", "vector_embedding"
}

func (q *TaskQueue) EnqueuePreprocessTask(...) error
func (q *TaskQueue) EnqueuePreprocessTaskWithPriority(...) error
```

**3.2 预处理任务处理器** ([meta/backend/internal/worker/handler.go](../meta/backend/internal/worker/handler.go:1))
```go
func (h *TaskHandler) HandlePreprocessTask(ctx context.Context, t *asynq.Task) error {
    // 1. 解析 payload
    // 2. 查询 meta_item
    // 3. 执行预处理
    // 4. 更新 meta_item.attributes.preprocess_metadata
}
```

**3.3 预处理 API** ([meta/backend/internal/api/preprocess_handler.go](../meta/backend/internal/api/preprocess_handler.go:1))
```go
// POST /api/meta/preprocess/items/:item_id
func (h *PreprocessHandler) TriggerPreprocess(c *gin.Context)

// GET /api/meta/preprocess/items/:item_id/status
func (h *PreprocessHandler) GetPreprocessStatus(c *gin.Context)

// DELETE /api/meta/preprocess/items/:item_id/cache
func (h *PreprocessHandler) ClearCache(c *gin.Context)
```

**3.4 扫描完成后自动触发** ([meta/backend/internal/service/scan_service_new.go](../meta/backend/internal/service/scan_service_new.go:632))
```go
// 在 scanResourceInternal 完成后调用
if err := s.triggerPreprocessingIfEnabled(ctx, resource, tenantID); err != nil {
    s.log.Warn("预处理任务触发失败", "resource_id", resource.ID, "error", err)
}
```

### Phase 4: Manager 模块预览 ✅

**4.1 空间预览服务** ([manager/backend/internal/service/spatial_preview_service.go](../manager/backend/internal/service/spatial_preview_service.go:1))
```go
func (s *SpatialPreviewService) GetTileMetadata(ctx, fingerprint string) (map[string]interface{}, error)
func (s *SpatialPreviewService) GetTile(ctx, fingerprint string, z, x, y int) ([]byte, error)
func (s *SpatialPreviewService) CheckTileExists(ctx, fingerprint string, z, x, y int) (bool, error)
```
- 从 MinIO `mvt-tiles` 读取预缓存瓦片
- 直接返回压缩的 MVT 数据

**4.2 API 端点** ([manager/backend/internal/api/spatial_preview_handler.go](../manager/backend/internal/api/spatial_preview_handler.go:1))
```go
// GET /api/spatial/:fingerprint/metadata
func (h *SpatialPreviewHandler) GetTileMetadata(c *gin.Context)

// GET /api/spatial/:fingerprint/tiles/:z/:x/:y.mvt
func (h *SpatialPreviewHandler) GetTile(c *gin.Context)

// HEAD /api/spatial/:fingerprint/tiles/:z/:x/:y.mvt
func (h *SpatialPreviewHandler) CheckTileExists(c *gin.Context)
```

## 数据流程

### 1. 扫描触发流程
```
1. System 前端: 用户注册存储引擎，配置 scan_config.preprocessing.enabled = true
2. Meta Backend: 定时或手动触发扫描
3. Scan Service: 检测空间表，提取 spatial_metadata
4. Scan Service: 扫描完成后调用 triggerPreprocessingIfEnabled()
5. Task Queue: 入队预处理任务（meta:preprocess）
6. Meta Worker: 消费任务，执行预处理
7. Preprocess Service: 批量生成瓦片，存储到 MinIO
8. Meta Backend: 更新 meta_item.attributes.preprocess_metadata
```

### 2. 前端渲染流程
```
1. Manager 前端: 用户浏览空间表
2. Manager Backend: 查询 meta_item，获取 fingerprint
3. Manager Frontend: 使用 MapLibre GL 加载地图
4. MapLibre GL: 请求瓦片 /api/spatial/{fingerprint}/tiles/{z}/{x}/{y}.mvt
5. Manager Backend: 从 MinIO 读取预缓存瓦片
6. Manager Backend: 返回 gzip 压缩的 MVT 数据
7. MapLibre GL: 解析并渲染瓦片
```

## MinIO 目录结构

```
mvt-tiles/
├── README.txt                                    # 说明文档
├── {fingerprint_1}/                              # SHA256(resID:schema.table)
│   ├── metadata.json                             # 预处理元数据
│   └── tiles/
│       ├── z0/
│       │   └── 0_0.mvt.gz                        # z=0, x=0, y=0
│       ├── z1/
│       │   ├── 0_0.mvt.gz
│       │   ├── 0_1.mvt.gz
│       │   ├── 1_0.mvt.gz
│       │   └── 1_1.mvt.gz
│       ├── z2/
│       │   └── ...
│       └── z{max}/
│           └── ...
└── {fingerprint_2}/
    └── ...
```

## 配置示例

### 资源注册配置
```json
{
  "name": "业务数据库",
  "resource_type": "postgresql",
  "connection_info": {
    "host": "192.168.1.100",
    "port": 5432,
    "database": "business",
    "username": "admin",
    "password": "***"
  },
  "scan_config": {
    "enabled": true,
    "schedule_type": "daily",
    "schedule_time": "02:00",
    "scan_depth": "deep",
    "preprocessing": {
      "enabled": true,
      "auto_trigger": true,
      "types": ["mvt_tiles"],
      "mvt_config": {
        "max_zoom": 18,
        "concurrency": 10,
        "stop_threshold_sec": 3.0,
        "stop_threshold_kb": 50.0
      }
    }
  }
}
```

### 手动触发预处理
```bash
# 触发预处理
curl -X POST http://localhost:8082/api/meta/preprocess/items/123 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "mvt_tiles",
    "priority": "default"
  }'

# 查询状态
curl -X GET http://localhost:8082/api/meta/preprocess/items/123/status \
  -H "Authorization: Bearer $TOKEN"
```

## 性能优化

1. **并发控制**: 可配置并发数（默认 10）
2. **自适应停止**: 避免生成过多高缩放级别瓦片
3. **跳过空瓦片**: 不存储无数据的瓦片
4. **gzip 压缩**: 瓦片采用 gzip 压缩存储
5. **HTTP 缓存**: 设置 Cache-Control 头，浏览器缓存 1 天

## 已完成功能

- ✅ 指纹计算与目录结构设计
- ✅ 空间表元数据自动检测（几何列、SRID、Extent、索引）
- ✅ MVT 瓦片批量生成（PostGIS ST_AsMVT）
- ✅ MinIO 持久化存储
- ✅ 自适应缩放停止
- ✅ 变更检测（pg_stat_user_tables）
- ✅ Worker 任务队列集成
- ✅ 扫描完成后自动触发
- ✅ Manager 模块瓦片预览 API
- ✅ 手动触发预处理 API

## 待完成功能

### Phase 4.5: Manager 前端空间预览组件
- 创建 SpatialPreview.vue 组件
- 集成 MapLibre GL JS
- 实现瓦片图层加载
- 添加空间表预览入口

### Phase 5: System 前端配置界面
- 资源注册表单扩展
- 添加预处理配置 UI
- MVT 配置参数输入

### 未来增强
- 增量更新精确区域检测（基于 updated_at）
- 预处理进度实时追踪
- 瓦片统计和监控面板
- 其他预处理类型（vector_embedding）

## 部署步骤

### 1. 初始化 MinIO
```bash
./scripts/setup/init-minio-mvt.sh
```

### 2. 启动服务
```bash
# 启动所有服务
make dev-start

# 或分别启动
cd meta/backend && go run cmd/server/main.go
cd meta/backend && go run cmd/worker/main.go
cd manager/backend && go run cmd/server/main.go
```

### 3. 配置资源
- 在 System 前端注册存储引擎
- 启用 `scan_config.preprocessing.enabled = true`
- 配置自动触发 `auto_trigger = true`

### 4. 触发扫描
- 手动触发或等待定时任务
- 扫描完成后自动创建预处理任务
- Worker 并发生成瓦片

### 5. 验证
```bash
# 检查瓦片是否生成
mc ls addp-minio/mvt-tiles/

# 测试瓦片访问
curl -I http://localhost:8081/api/spatial/{fingerprint}/tiles/0/0/0.mvt \
  -H "Authorization: Bearer $TOKEN"
```

## 总结

本实现完整集成了 MVT 空间数据快显功能，核心特性包括：

1. **指纹不可变**: 基于表物理位置，数据更新不改变指纹
2. **自适应停止**: 智能检测最优缩放级别
3. **增量更新**: 只重新生成变更的瓦片
4. **异步处理**: 使用 Worker 队列，不阻塞扫描流程
5. **自动触发**: 扫描完成后自动创建预处理任务
6. **缓存优化**: MinIO 持久化 + HTTP 缓存

所有后端功能已完整实现并集成，前端预览组件和配置界面为下一步工作。
