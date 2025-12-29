# MVT 预缓存系统 (Pre-Cache)

## 概述

ADDP 的 MVT 预缓存系统是针对 PostgreSQL 空间数据的高性能瓦片缓存解决方案，通过智能算法和流水线并行架构，实现了空间数据的快速加载和浏览。

## 核心特性

### 1. 智能 MaxZoom 计算

系统根据数据规模自动计算最佳的最大缩放层级，避免过度或不足缓存。

**算法原理**:
- **目标密度**: 每瓦片 1000 条记录（可配置）
- **计算逻辑**: 从低层级向高层级迭代，找到使平均记录密度接近目标值的层级
- **边界检测**: 使用交集检测确保边界瓦片被正确生成

**实现位置**: [`common/spatial/zoom_calculator.go`](../common/spatial/zoom_calculator.go)

```go
func CalculateMaxZoomByRecordCount(
    recordCount int64,
    targetRecordsPerTile int,
    extent [4]float64,
    srid int,
) int {
    // 计算每个 zoom 层级的瓦片数量和平均密度
    for z := minZoom to 20 {
        tiles := calculateIntersectingTiles(z, extent)
        avgDensity := recordCount / len(tiles)
        if avgDensity < targetRecordsPerTile {
            return z
        }
    }
}
```

### 2. 三层 Zoom 范围处理

| Zoom 范围 | 行为 | 缓存策略 | 用户体验 |
|----------|------|---------|---------|
| **z < minZoom** | 返回空瓦片 | 不缓存 | 前端提示"数据可能不可见" |
| **minZoom ≤ z ≤ maxZoom** | 正常处理 | 生成时间 > 100ms **或** 大小 > 50KB | 最佳性能（预缓存范围） |
| **z > maxZoom** | 允许但降低优先级 | 生成时间 > 200ms **且** 大小 > 100KB | 前端提示"加载可能较慢" |

**实现位置**: [`manager/backend/internal/service/unified_mvt_service.go`](../manager/backend/internal/service/unified_mvt_service.go)

### 3. 流水线并行瓦片生成

传统逐层串行生成存在 worker 空闲问题，流水线并行模式通过跨层级复用 worker，显著提升生成效率。

**架构对比**:

```
传统串行模式:
┌──────────────┐
│   z=6 层     │ Worker 利用率: 100%
└──────────────┘
    ↓ 层间切换时 worker 空闲
┌──────────────┐
│   z=7 层     │ Worker 利用率: 60%
└──────────────┘
    ↓
...

流水线并行模式:
┌──────────────────────────────────┐
│  优先级队列 (Min Heap by Zoom)   │
│  [z=6] [z=7] [z=8] ...          │
└────────────┬─────────────────────┘
             │ 动态任务投递
             ▼
┌──────────────────────────────────┐
│  Worker Pool (跨层级共享)        │
│  Worker 1  Worker 2  ...  Worker N│ Worker 利用率: 95%
└──────────────────────────────────┘
```

**关键机制**:
1. **优先级队列**: 使用 Go `container/heap` 实现，低 zoom 优先
2. **动态任务提交**: 空闲 worker ≥ 50% 时启动下一层
3. **背压控制**: 限制待处理任务数，避免内存爆炸

**实现位置**: [`manager/backend/internal/mvt/pipeline_tile_generator.go`](../manager/backend/internal/mvt/pipeline_tile_generator.go)

**性能提升**:
- Worker 利用率: 60% → 95%
- 生成速度: 提升约 40%
- 内存稳定: 通过 `MaxPendingTasks` 限制

### 4. 配置参数化

所有预缓存参数均可通过环境变量配置，支持灵活调优。

**配置项**:

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `PRE_CACHE_TARGET_RECORDS` | 1000 | 每瓦片目标记录数（用于计算 maxZoom） |
| `PRE_CACHE_MIN_DURATION_MS` | 100 | 缓存耗时阈值（毫秒） |
| `PRE_CACHE_MIN_SIZE_KB` | 50 | 缓存大小阈值（KB） |
| `PRE_CACHE_MAX_ZOOM` | 18 | 全局最大 zoom 层级 |
| `PRE_CACHE_CONCURRENCY` | 10 | 并发协程数 |

**配置结构**: [`manager/backend/internal/config/config.go`](../manager/backend/internal/config/config.go)

```go
type PreCacheConfig struct {
    TargetRecordsPerTile  int  // 每瓦片目标记录数
    MinDurationForCacheMS int  // 生成耗时阈值（毫秒）
    MinSizeForCacheKB     int  // 瓦片大小阈值（KB）
    MaxZoom               int  // 全局最大层级
    Concurrency           int  // 并发协程数
}
```

## API 路由

### 新路由（推荐使用）

```
POST   /api/engines/:id/spatial/:schema/:table/pre-cache
GET    /api/engines/:id/spatial/:schema/:table/pre-cache/status
DELETE /api/engines/:id/spatial/:schema/:table/pre-cache

GET    /api/pre-cache/tasks
GET    /api/pre-cache/statistics
```

### 旧路由（向后兼容，保留作为别名）

```
POST   /api/engines/:id/spatial/:schema/:table/quick-view
GET    /api/engines/:id/spatial/:schema/:table/quick-view/status
DELETE /api/engines/:id/spatial/:schema/:table/quick-view

GET    /api/quick-view/tasks
GET    /api/quick-view/statistics
```

**迁移建议**: 新代码应使用 `/pre-cache` 路由，旧路由将在未来版本中标记为废弃。

## 使用示例

### 1. 触发预缓存生成

**请求**:
```bash
curl -X POST "http://localhost:8081/api/engines/2/spatial/public/dltb/pre-cache" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "max_zoom": 18,
    "concurrency": 10,
    "priority": "default"
  }'
```

**响应**:
```json
{
  "message": "Pre-cache generation started",
  "task_id": "uuid-here"
}
```

### 2. 查询预缓存状态

**请求**:
```bash
curl "http://localhost:8081/api/engines/2/spatial/public/dltb/pre-cache/status" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

**响应**:
```json
{
  "status": "completed",
  "fingerprint": "abc123def456",
  "min_zoom": 6,
  "max_zoom": 14,
  "extent": [120.0, 30.0, 121.0, 31.0],
  "total_tiles": 12345,
  "cached_tiles": 12000,
  "generation_sec": 123.45,
  "created_at": "2025-01-01T12:00:00Z"
}
```

### 3. 获取瓦片配置

前端在初始化地图时调用此接口获取 minZoom/maxZoom：

**请求**:
```bash
curl "http://localhost:8081/api/engines/2/spatial/public/dltb/tile-config" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

**响应**:
```json
{
  "min_zoom": 6,
  "max_zoom": 14,
  "extent": [120.0, 30.0, 121.0, 31.0],
  "srid": 4326
}
```

## 前端集成

### VectorTilePreview 组件

预缓存系统与前端地图组件无缝集成：

**特性**:
1. **自动获取 tile-config**: 组件初始化时自动查询 minZoom/maxZoom
2. **软限制**: 不限制用户缩放范围（1-20），仅显示提示消息
3. **智能提示**:
   - `z < minZoom`: "当前层级低于建议范围，数据可能不可见"
   - `z > maxZoom`: "当前层级超出预缓存范围，加载可能较慢"

**实现位置**: [`manager/frontend/src/components/map/VectorTilePreview.vue`](../manager/frontend/src/components/map/VectorTilePreview.vue)

```javascript
// 获取瓦片配置
async function fetchTileConfig() {
  const url = `/engines/${engineId}/spatial/${schema}/${table}/tile-config`
  const response = await client.get(url)
  tileConfig.value = response.data
}

// 监听 zoom 变化，显示友好提示
map.getView().on('change:resolution', () => {
  const currentZoom = Math.round(map.getView().getZoom())

  if (currentZoom < tileConfig.value.min_zoom) {
    ElMessage.warning('当前层级低于建议范围，数据可能不可见')
  } else if (currentZoom > tileConfig.value.max_zoom) {
    ElMessage.info('当前层级超出预缓存范围，加载可能较慢')
  }
})
```

## 性能优化建议

### 1. 数据规模 vs 配置

| 数据规模 | 推荐 Concurrency | 推荐 TargetRecords |
|---------|-----------------|-------------------|
| < 10 万条 | 5 | 500 |
| 10-100 万条 | 10 | 1000 |
| 100-1000 万条 | 20 | 2000 |
| > 1000 万条 | 30+ | 3000+ |

### 2. 缓存策略调优

**场景 1: 高频访问，快速响应**
```bash
PRE_CACHE_MIN_DURATION_MS=50    # 更激进的缓存
PRE_CACHE_MIN_SIZE_KB=20
```

**场景 2: 低频访问，节省存储**
```bash
PRE_CACHE_MIN_DURATION_MS=200   # 更保守的缓存
PRE_CACHE_MIN_SIZE_KB=100
```

### 3. 流水线并行调优

**高并发场景**:
```bash
PRE_CACHE_CONCURRENCY=30        # 更多 worker
IDLE_THRESHOLD=15               # 50% 空闲时启动下一层
MAX_PENDING_TASKS=300           # 更大的任务队列
```

**低资源场景**:
```bash
PRE_CACHE_CONCURRENCY=5         # 较少 worker
IDLE_THRESHOLD=2
MAX_PENDING_TASKS=50
```

## 监控和调试

### 查看预缓存任务列表

```bash
curl "http://localhost:8081/api/pre-cache/tasks" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 查看预缓存统计

```bash
curl "http://localhost:8081/api/pre-cache/statistics" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

**响应示例**:
```json
{
  "total_tasks": 10,
  "completed": 8,
  "generating": 1,
  "failed": 1,
  "total_tiles_cached": 98765,
  "total_storage_mb": 1234.56
}
```

### 日志分析

查看 Manager backend 日志以分析预缓存性能：

```bash
tail -f logs/manager-backend.log | grep -E "流水线|Pre-Cache|MVT"
```

**关键日志指标**:
- `pipeline_efficiency`: 流水线效率（0.0-1.0，越接近 1.0 越好）
- `generation_sec`: 总生成时长
- `cached_tiles`: 成功缓存的瓦片数

## 故障排查

### 问题 1: 预缓存生成失败

**症状**: 状态一直为 `generating` 或变为 `failed`

**排查步骤**:
1. 查看后端日志: `logs/manager-backend.log`
2. 检查 PostgreSQL 连接: 资源配置是否正确
3. 检查 MinIO 连接: 存储桶是否存在且可访问
4. 检查数据库权限: 用户是否有读取空间数据权限

### 问题 2: 前端地图加载慢

**症状**: 拖动地图时瓦片加载缓慢

**排查步骤**:
1. 检查是否启用了预缓存: `GET /pre-cache/status`
2. 检查当前 zoom 是否在预缓存范围内
3. 检查 Redis/MinIO 缓存是否正常工作
4. 检查网络延迟: 前端到后端的 RTT

### 问题 3: 预缓存占用过多存储

**症状**: MinIO 存储空间快速增长

**解决方案**:
1. 调整缓存策略阈值（提高 `MIN_DURATION_MS` 和 `MIN_SIZE_KB`）
2. 降低 `MAX_ZOOM`（减少高层级瓦片数量）
3. 定期清理不再使用的预缓存: `DELETE /pre-cache`

## 技术架构

### 数据流

```
┌──────────────┐
│   前端地图    │
└──────┬───────┘
       │ GET /tiles/{z}/{x}/{y}
       ▼
┌──────────────────────────────────────────────┐
│         UnifiedMVTService                    │
│  ┌────────────────────────────────────────┐ │
│  │ 1. Zoom 范围验证（返回空瓦片 or 继续） │ │
│  └────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────┐ │
│  │ 2. 三层缓存穿透                        │ │
│  │    内存 LRU → Redis → MinIO           │ │
│  └────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────┐ │
│  │ 3. 缓存未命中，实时 PG 生成            │ │
│  └────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────┐ │
│  │ 4. 缓存策略判断（基于 zoom 范围）      │ │
│  └────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────┐ │
│  │ 5. 异步持久化到 MinIO + 回填缓存       │ │
│  └────────────────────────────────────────┘ │
└──────────────────────────────────────────────┘
       │
       ▼
┌──────────────┐
│   返回瓦片    │
└──────────────┘
```

### 预缓存生成流程

```
触发预缓存
   ↓
┌────────────────────────────────────────┐
│  PipelineTileGenerator                 │
│                                        │
│  1. 初始化优先级队列（Min Heap）      │
│  2. 启动固定大小的 Worker Pool        │
│  3. 动态任务提交协程                  │
│     - 监控空闲 worker 数量            │
│     - 空闲 ≥ 阈值时投递下一层任务     │
│  4. Worker 从队列获取任务             │
│     - 按 zoom 优先级处理              │
│     - 生成瓦片并上传 MinIO            │
│  5. 结果收集与统计                    │
└────────────────────────────────────────┘
   ↓
返回生成结果（包含效率指标）
```

## 相关文件

### 后端

- **核心服务**: `manager/backend/internal/service/unified_mvt_service.go`
- **流水线生成器**: `manager/backend/internal/mvt/pipeline_tile_generator.go`
- **智能计算**: `common/spatial/zoom_calculator.go`
- **配置管理**: `manager/backend/internal/config/config.go`
- **API 路由**: `manager/backend/internal/api/router.go`
- **瓦片配置**: `manager/backend/internal/api/tile_config_handler.go`

### 前端

- **地图组件**: `manager/frontend/src/components/map/VectorTilePreview.vue`
- **API 客户端**: `manager/frontend/src/api/quickView.js`
- **预览组件**: `manager/frontend/src/components/previews/TablePreview.vue`

## 未来优化方向

1. **自适应并发**: 根据系统负载动态调整 worker 数量
2. **智能预热**: 基于用户访问热力图预测性预缓存
3. **增量更新**: 数据变更时仅重新生成受影响的瓦片
4. **多数据源支持**: 扩展到非 PostgreSQL 数据源（如 Shapefile、GeoJSON）
5. **分布式生成**: 跨多节点并行生成大规模数据集的瓦片

## 贡献

欢迎提交 Issue 或 Pull Request 以改进预缓存系统！

**相关讨论**: 详见 [`CLAUDE.md`](../CLAUDE.md) 中的 MVT 预缓存章节。
