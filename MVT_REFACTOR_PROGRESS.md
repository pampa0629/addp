# MVT快显重构 - 工作进度报告

**开始时间**: 2025-11-24
**状态**: 进行中 (约30%完成)

## 📋 重构目标

将MVT瓦片缓存功能从Meta模块移到Manager模块，并实现以下目标：
1. 默认开启立即扫描（基础）和定时扫描（深度），用户可手动关闭
2. MVT瓦片缓存能力从存储引擎注册移除，放到Manager模块
3. Manager增加独立worker，接替Meta worker的MVT缓存功能
4. Manager前端增加"快显"开关入口

---

## ✅ 已完成工作

### Phase 1: 数据库准备 (100%)

**文件**: `scripts/init-db.sql`

创建了 `manager.quick_view` 表，包含以下字段：
- 基本信息：tenant_id, resource_id, schema_name, table_name
- 状态：status ('none', 'generating', 'ready', 'failed'), error_message
- 缓存配置：min_zoom (自动计算), max_zoom (默认18), actual_max_zoom
- 统计信息：total_tiles, cached_tiles, last_zoom_avg_time_ms, last_zoom_avg_size_kb
- 停止条件：stop_threshold_time_ms (默认300), stop_threshold_size_kb (默认100)
- 指纹和范围：fingerprint, extent (JSONB)
- 时间戳：started_at, completed_at, created_at, updated_at

**索引**:
- 唯一约束：(tenant_id, resource_id, schema_name, table_name)
- idx_quick_view_status
- idx_quick_view_fingerprint

**触发器**: update_quick_view_updated_at (自动更新 updated_at)

---

### Phase 2: Manager Worker 实现 (80%)

#### 2.1 Worker入口 (100%)
**文件**: `manager/backend/cmd/worker/main.go`

- 完整的worker主程序
- 数据库连接
- Asynq服务器配置（3个优先级队列）
- 优雅关闭机制
- 队列配置：
  - manager:critical (优先级6)
  - manager:default (优先级3)
  - manager:low (优先级1)

#### 2.2 MVT核心代码移植 (100%)
**目录**: `manager/backend/internal/mvt/`

**已创建文件**:
1. `models.go` - 数据模型
   - TileCoord (瓦片坐标)
   - ZoomLevelStats (缩放级别统计)
   - QuickViewMetadata (快显元数据)
   - SpatialMetadata (空间表元数据)
   - QuickViewProgress (快显进度)

2. `utils.go` - 工具函数
   - `lonLatToTileXY()` - 经纬度转瓦片坐标
   - `tileXYToLonLat()` - 瓦片坐标转经纬度
   - `calculateTileBounds()` - 计算瓦片边界
   - `calculateTotalTiles()` - 计算瓦片总数
   - **`CalculateOptimalMinZoom()`** - 根据extent自动计算MinZoom ⭐
   - `gzipCompress/Decompress()` - 压缩/解压缩
   - 辅助转换函数

3. `tile_generator.go` - 瓦片生成器
   - 简化版（不依赖meta models）
   - `TileGenerationParams` 结构体传参
   - `GenerateTile()` - 生成单个MVT瓦片
   - `GetSpatialExtent()` - 获取空间范围
   - 使用 common/spatial 统一MVT查询

4. `quick_view_service.go` - 快显服务 (核心)
   - `QuickViewService` 结构体
   - `Generate()` - 批量生成瓦片主流程
   - `processZoomLevel()` - 处理单个zoom level
   - `processTile()` - 处理单个瓦片
   - **自适应停止逻辑**: 平均时间 <= 300ms 且 大小 <= 100KB ⭐
   - MinIO集成（存储/读取瓦片和元数据）
   - Worker Pool 并发生成

**MinZoom 自动计算规则**:
```
maxSpan >= 50度  → zoom 2  (大陆级)
maxSpan >= 10度  → zoom 4  (国家级)
maxSpan >= 1度   → zoom 6  (省级)
maxSpan >= 0.1度 → zoom 8  (城市级)
maxSpan >= 0.01度 → zoom 10 (区县级)
maxSpan >= 0.001度 → zoom 12 (街道级)
< 0.001度        → zoom 14 (小范围)
```

#### 2.3 Worker任务处理 (100%)
**目录**: `manager/backend/internal/worker/`

1. `queue.go` - 任务队列管理
   - `TaskQueue` 结构体
   - `QuickViewTaskPayload` 任务载荷定义
   - `EnqueueQuickViewTask()` - 入队
   - `EnqueueQuickViewTaskWithPriority()` - 优先级入队

2. `handler.go` - 任务处理器
   - `TaskHandler` 结构体
   - `HandleQuickViewTask()` - 处理快显任务
   - `updateQuickViewStatus()` - 更新数据库状态
   - `resourceServiceAdapter` - SystemClient适配器

**任务载荷字段**:
```go
type QuickViewTaskPayload struct {
    TenantID, ResourceID uint
    SchemaName, TableName string
    GeomColumn string
    SRID int
    PrimaryKey string
    Extent []float64
    MinZoom, MaxZoom int
    Concurrency int
    StopThresholdMs, StopThresholdKB float64
    Fingerprint string
}
```

#### 2.4 数据模型 (100%)
**文件**: `manager/backend/internal/models/quick_view.go`

- `QuickView` GORM模型
- `JSONFloatArray` 自定义类型（存储extent到JSONB）
- 实现 sql.Scanner 和 driver.Valuer 接口
- 完整的字段映射（对应数据库表）

---

## 🚧 待完成工作

### Phase 3: Manager API层 (0%)

#### 3.1 Repository层 (未开始)
**需创建**: `manager/backend/internal/repository/quick_view_repository.go`

需实现方法:
- `Create(qv *QuickView) error`
- `GetByTable(tenantID, resourceID, schema, table) (*QuickView, error)`
- `Update(qv *QuickView) error`
- `UpdateStatus(id, status, errorMsg) error`
- `UpdateResult(id, result) error`
- `ListAll(tenantID, pagination) ([]QuickView, int64, error)`
- `GetStatistics(tenantID) (*Statistics, error)`
- `Delete(id) error`

#### 3.2 Service层 (未开始)
**需创建**: `manager/backend/internal/service/quick_view_service.go`

需实现方法:
- `TriggerQuickView(ctx, params) error` - 触发快显
  - 检查并发（不允许同一表重复生成）
  - 从Meta获取spatial_metadata
  - 自动计算MinZoom（如果未指定）
  - 入队QuickViewTask
- `GetStatus(ctx, resourceID, schema, table) (*QuickView, error)`
- `ListAll(ctx, tenantID, params) ([]QuickView, int64, error)`
- `GetStatistics(ctx, tenantID) (*Statistics, error)`
- `ClearQuickView(ctx, id) error` - 删除缓存和数据库记录

#### 3.3 API Handler (未开始)
**需创建**: `manager/backend/internal/api/quick_view_handler.go`

需实现端点:
```
POST   /api/resources/:id/spatial/:schema/:table/quick-view
GET    /api/resources/:id/spatial/:schema/:table/quick-view/status
DELETE /api/resources/:id/spatial/:schema/:table/quick-view
GET    /api/quick-view/tasks
GET    /api/quick-view/statistics
```

#### 3.4 路由注册 (未开始)
**需修改**: `manager/backend/internal/api/router.go`

添加快显相关路由

#### 3.5 UnifiedMVTService自动缓存 (未开始)
**需修改**: `manager/backend/internal/service/unified_mvt_service.go`

在 `GetTile()` 方法中添加逻辑:
- 实时生成瓦片后，检查生成时间和大小
- 如果 `duration > 300ms || size > 100kb`，异步存储到MinIO
- 更新 quick_view 表的统计数据

---

### Phase 4: Meta模块清理 (0%)

**需删除的目录**:
- `meta/backend/internal/mvt/` (整个目录)

**需删除的文件**:
- `meta/backend/internal/service/preprocess_service.go`

**需修改的文件**:
1. `meta/backend/cmd/worker/main.go`
   - 移除 `preprocessService` 初始化
   - 移除 `taskHandler` 参数中的 `preprocessService`

2. `meta/backend/internal/worker/handler.go`
   - 删除 `HandlePreprocessTask` 函数
   - 移除 `preprocessService` 字段

3. `meta/backend/internal/worker/queue.go`
   - 删除 `TypePreprocessTask` 常量
   - 删除 `PreprocessTaskPayload` 结构体
   - 删除 `EnqueuePreprocessTask*` 相关方法

4. `meta/backend/internal/service/scan_service_new.go`
   - 移除扫描完成后调用 `EnqueuePreprocessTask` 的代码
   - 删除 `taskQueue` 参数传递

5. `meta/backend/internal/models/scan_task.go`
   - 移除 `preprocess_*` 相关字段（如有）

---

### Phase 5: System模块简化 (0%)

#### 5.1 简化ScanConfig (未开始)
**需修改**: `common/models/resource.go`

```go
type ScanConfig struct {
    ImmediateScan  bool   `json:"immediate_scan"` // 默认 true
    ScheduledScan  bool   `json:"scheduled_scan"` // 默认 true
    ScheduleType   string `json:"schedule_type"`  // "daily"
    CronExpression string `json:"cron_expression,omitempty"`
    ScheduleTime   string `json:"schedule_time"`  // "00:00"
    ScheduleValue  []int  `json:"schedule_value,omitempty"`
    ScanDepth      string `json:"scan_depth"`     // "shallow" | "deep" (保留)

    // 删除整个 Preprocessing 配置块
    // Preprocessing PreprocessingConfig `json:"preprocessing"` // 删除
}

// 删除 PreprocessingConfig 结构体定义
```

#### 5.2 前端移除Preprocessing UI (未开始)
**需修改**: `system/frontend/src/views/Resources.vue`

移除 Preprocessing 配置相关的UI组件

---

### Phase 6: Manager前端实现 (0%)

#### 6.1 快显监控页面 (未开始)
**需创建文件**:
- `manager/frontend/src/views/QuickView/TaskMonitor.vue` - 任务监控仪表板
- `manager/frontend/src/views/QuickView/TaskList.vue` - 任务列表
- `manager/frontend/src/views/QuickView/TaskDetail.vue` - 任务详情

**功能需求**:
- **TaskMonitor**: 显示统计数据（总数、生成中、已完成、失败）+ 最近任务列表
- **TaskDetail**: 实时进度显示（5秒轮询），zoom level进度条，统计信息
- **轮询机制**: 参考 Transfer 模块实现（generating状态5秒轮询，完成后停止）

#### 6.2 快显开关 (未开始)
**需修改**: `manager/frontend/src/components/previews/TablePreview.vue`

添加功能:
- 快显开关（el-switch）
- 状态显示（generating, ready, failed）
- 组件加载时检查快显状态
- 开启时调用API触发快显，切换到MVT浏览模式
- 关闭时切换回普通表格预览

#### 6.3 API层 (未开始)
**需创建**: `manager/frontend/src/api/quickView.js`

```js
export const quickViewAPI = {
  trigger(resourceId, schema, table, config = {}),
  getStatus(resourceId, schema, table),
  clear(resourceId, schema, table),
  listAll(params),
  statistics()
}
```

#### 6.4 路由和导航 (未开始)
**需修改文件**:
- `manager/frontend/src/router/index.js` - 添加快显监控路由
- `manager/frontend/src/components/Layout.vue` - 添加"快显监控"菜单项

---

### Phase 7: 基础设施配置 (0%)

#### 7.1 Docker配置 (未开始)
**需创建**: `manager/backend/Dockerfile.worker`

参考 `meta/backend/Dockerfile.worker` 创建

**需修改**: `docker-compose.yml`

添加 manager-worker 服务:
```yaml
manager-worker:
  build:
    context: ./manager/backend
    dockerfile: Dockerfile.worker
  depends_on:
    - redis
    - minio
    - postgres
    - manager-backend
  environment:
    - PORT=8091
    - REDIS_ADDR=redis:6379
    - MINIO_ENDPOINT=minio:9000
    # ... 其他环境变量
  networks:
    - addp-network
  profiles:
    - full
  restart: unless-stopped
```

#### 7.2 开发脚本 (未开始)
**需修改**: `scripts/dev/start.sh`

在 Step 6 (启动Workers) 部分添加:
```bash
# Manager Worker
echo -e "${BLUE}Starting Manager Worker...${NC}"
cd "$PROJECT_ROOT/manager/backend"
nohup go run cmd/worker/main.go > "$LOG_DIR/manager-worker.log" 2>&1 &
echo $! > "$PID_DIR/manager-worker.pid"
echo -e "${GREEN}✓ Manager Worker started (PID: $!)${NC}"
```

**需修改**: `scripts/dev/stop.sh` 和 `scripts/dev/restart.sh`

确保Manager Worker被正确停止和重启

---

### Phase 8: 测试和文档 (0%)

#### 8.1 端到端测试 (未开始)
测试场景:
1. 注册空间数据源（验证无Preprocessing配置）
2. Meta扫描（验证不触发MVT生成）
3. 开启快显（验证自动计算MinZoom）
4. 监控任务进度（验证轮询更新）
5. MVT浏览（验证缓存优先级）
6. 按需生成自动缓存（验证300ms/100kb阈值）

#### 8.2 文档更新 (未开始)
**需更新**:
- `CLAUDE.md` - 更新平台架构说明
- `manager/CLAUDE.md` - 创建或更新Manager模块文档
- `docs/QUICK_VIEW.md` - 快显功能使用指南（可选）

---

## 🔧 技术要点总结

### MinZoom 自动计算
- 位置: `manager/backend/internal/mvt/utils.go`
- 函数: `CalculateOptimalMinZoom(extent []float64) int`
- 策略: 根据extent的maxSpan（经纬度跨度）决定起始zoom level
- 范围: zoom 2-14

### 停止条件
- 平均生成时间 ≤ 300ms **且** 平均瓦片大小 ≤ 100KB
- 只统计有效瓦片（非空瓦片）
- 达到条件后停止，实际最大层级记录在 `actual_max_zoom`

### MVT缓存优先级
1. Memory LRU (5分钟TTL) - 由UnifiedMVTService管理
2. Redis (24小时TTL) - 由UnifiedMVTService管理
3. MinIO (持久化) - 由QuickViewService生成
4. PostgreSQL实时生成 - 回退方案

### 自动缓存触发
在UnifiedMVTService的GetTile()中:
```
实时生成瓦片 → 检查时间和大小
如果 duration > 300ms || size > 100kb:
  → 异步存储到MinIO
  → 更新quick_view统计
```

---

## 📝 下一步工作建议

按优先级排序:

1. **创建QuickView Repository和Service层** (Phase 3.1-3.2)
   - 这是API的基础，优先实现

2. **创建API Handler和路由** (Phase 3.3-3.4)
   - 提供REST接口供前端调用

3. **修改UnifiedMVTService** (Phase 3.5)
   - 实现按需生成时的自动缓存

4. **清理Meta模块** (Phase 4)
   - 删除MVT相关代码

5. **简化System模块** (Phase 5)
   - 删除Preprocessing配置

6. **基础设施配置** (Phase 7)
   - 使Worker能够运行

7. **前端实现** (Phase 6)
   - 最后实现UI

8. **测试和文档** (Phase 8)
   - 验证和记录

---

## ⚠️ 注意事项

1. **编译依赖**: Worker代码依赖common模块，确保 `go.mod` 正确配置
2. **MinIO Bucket**: 代码中硬编码使用 `mvt-tiles` bucket，需确保存在
3. **Fingerprint计算**: 使用 `SHA256(resourceID:schema.table)` 作为缓存key
4. **并发控制**: 同一张表不允许多个快显任务并发（通过status检查）
5. **Meta依赖**: 快显触发时需要从Meta读取spatial_metadata（geom_column, srid, primary_key）
6. **时区**: Worker已设置为Asia/Shanghai
7. **GORM自动迁移**: Manager server启动时需要添加 `AutoMigrate(&models.QuickView{})`

---

## 🐛 已知问题

1. handler.go中的 `StartedAt` 和 `CompletedAt` 字段类型可能需要调整（当前使用 `*gorm.DeletedAt`，应该用 `*time.Time`）
2. Worker中的resourceServiceAdapter需要正确传递tenantID（当前SystemClient.GetResource不支持tenantID参数）

---

## 📊 工作量估算

- ✅ 已完成: ~30%
- 🚧 待完成: ~70%

**预计剩余工作时间**: 8-10小时

---

## 🔗 相关文件索引

### 已创建的核心文件
1. `scripts/init-db.sql` - 数据库表定义
2. `manager/backend/cmd/worker/main.go` - Worker入口
3. `manager/backend/internal/mvt/models.go` - MVT数据模型
4. `manager/backend/internal/mvt/utils.go` - MVT工具函数（含MinZoom计算）
5. `manager/backend/internal/mvt/tile_generator.go` - 瓦片生成器
6. `manager/backend/internal/mvt/quick_view_service.go` - 快显服务
7. `manager/backend/internal/worker/queue.go` - 任务队列
8. `manager/backend/internal/worker/handler.go` - 任务处理器
9. `manager/backend/internal/models/quick_view.go` - QuickView模型

### 待创建的关键文件
1. `manager/backend/internal/repository/quick_view_repository.go`
2. `manager/backend/internal/service/quick_view_service.go`
3. `manager/backend/internal/api/quick_view_handler.go`
4. `manager/backend/Dockerfile.worker`
5. `manager/frontend/src/views/QuickView/*.vue`
6. `manager/frontend/src/api/quickView.js`

---

**报告生成时间**: 2025-11-24
**下次更新**: 继续Phase 3工作后
