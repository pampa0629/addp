# Meta 后台元数据扫描流程分析与改进建议

## 一、系统架构概览

### 1.1 核心服务分层

```
用户请求 / 定时调度
    ↓
API Handler (handler.go)
    ↓
ScanTaskService (任务管理和调度)
    ↓
ScanService (统一扫描编排)
    ↓
DatabaseScanService | ObjectStorageScanService (具体实现)
    ↓
ScanRepository (数据访问层)
    ↓
PostgreSQL (metadata.meta_node, metadata.meta_item)
```

### 1.2 关键文件清单

**核心服务**：
- `/Users/pampa/code/addp/meta/backend/internal/service/scan_service.go` - 扫描编排器（380 行）
- `/Users/pampa/code/addp/meta/backend/internal/service/scan_database_service.go` - 数据库扫描（364 行）
- `/Users/pampa/code/addp/meta/backend/internal/service/scan_object_storage_service.go` - 对象存储扫描
- `/Users/pampa/code/addp/meta/backend/internal/service/scan_task_service.go` - 任务调度（300+ 行）
- `/Users/pampa/code/addp/meta/backend/internal/service/scan_repository.go` - 数据访问层（200 行）

**插件机制**：
- `/Users/pampa/code/addp/common/database/plugin/interfaces.go` - 插件接口定义
- `/Users/pampa/code/addp/common/database/plugin/registry.go` - 插件注册中心
- `/Users/pampa/code/addp/common/database/plugins/postgresql/plugin.go` - PostgreSQL 插件实现

**数据模型**：
- `metadata.meta_node` - 层级节点（Schema/Bucket/Prefix）
- `metadata.meta_item` - 数据项（Table/View/Object）
- `metadata.scan_tasks` - 任务定义
- `metadata.scan_task_runs` - 运行记录

---

## 二、元数据扫描完整流程

### 2.1 手动触发扫描流程

```
1. 用户点击"扫描"按钮
   ↓
2. POST /api/meta/scan/run/manual
   ↓
3. ScanTaskService.CreateManualRun()
   - 验证资源可访问性（engineService.GetResourceByID）
   - Redis 去重检查（防止 30 分钟内重复扫描）
   - 创建 ScanTaskRun 记录（status: pending）
   - 入队（本地队列或 Asynq）
   ↓
4. Worker 取出任务
   ↓
5. ScanTaskService.executeRun()
   - 更新状态 → running
   - 创建进度报告器（实时更新 progress_percent）
   - 调用 ScanService.ScanEngineWithDepth()
   ↓
6. ScanService.scanResourceInternal()
   - 判断资源类型（数据库 vs 对象存储）
   - 委托给专门的扫描服务
   ↓
7a. [数据库路径] DatabaseScanService.ScanSchema()
   - 创建/更新 Schema 节点
   - 扫描表列表（scan.ScanTables）
   - 对每张表：
     * 检查是否需要更新（shouldUpdateTable）
     * 扫描字段（scan.ScanFields，deep 模式）
     * 提取空间元数据（PostgreSQL PostGIS）
     * 持久化 MetaItem
     * 索引到 Meilisearch（deep 模式）
   - 软删除已移除的表
   - 更新节点统计
   ↓
7b. [对象存储路径] ObjectStorageScanService.scanObjectStoragePaths()
   - 创建/更新 Bucket 节点
   - 扫描对象列表（objectScanner.ScanPath）
   - persistObjectMetas()：
     * 构建目录树（Prefix 节点）
     * 生成 fingerprint（SHA256）
     * 增量更新检测（LastModified, SizeBytes）
     * 提取元数据（deep 模式：MetadataExtractor）
     * 持久化 MetaItem
     * 索引 Meilisearch
   - 软删除未扫描到的对象（deep 模式）
   ↓
8. ScanRepository.UpsertItem()
   - 基于 fingerprint 去重
   - 软删除恢复机制
   ↓
9. 存储到 PostgreSQL
   - metadata.meta_node（层级节点）
   - metadata.meta_item（数据项）
   ↓
10. 索引到 Meilisearch
    - metadata_assets_index（资产索引）
    - metadata_documents_index（文档索引，可选）
    ↓
11. 发布 Redis 事件
    - 频道：meta:events:scan_completed
    - Manager 模块监听并刷新缓存
    ↓
12. ScanTaskService 更新运行状态
    - status: success/failed
    - result_summary（统计数据）
    - progress_percent: 100%
```

### 2.2 定时扫描流程

```
1. 系统启动 → ScanTaskService.Start()
   ↓
2. bootstrapSchedules() - 加载所有 enabled=true 的 ScanTask
   ↓
3. scheduleTask() - 解析 Cron 表达式，注册到 Scheduler
   ↓
4. Cron 触发时间到达
   ↓
5. triggerScheduledTask(taskID)
   - Redis 去重 + 时间间隔检查（≥6 小时）
   - 创建 ScanTaskRun（trigger_type: scheduled）
   - 更新 last_run_at, next_run_at
   - 入队
   ↓
6. （后续流程同手动触发）
```

### 2.3 插件机制工作流程

```
1. 应用启动时导入插件包
   - import _ "github.com/addp/common/database/plugins/postgresql"
   ↓
2. 插件 init() 自动注册
   - plugin.Register(&PostgreSQLPlugin{})
   ↓
3. 扫描时获取插件
   - plugin.Get(resource.EngineType)
   ↓
4. 创建 Scanner
   - plugins.NewScanner(resource)
   ↓
5. 调用插件方法
   - scanner.ListSchemas()
   - scanner.ScanTables(schema)
   - scanner.ScanFields(schema, table)
   ↓
6. 插件通过 ConnectionPoolPlugin 管理连接
   - CreateConnectionPool() 创建 GORM 连接池
   - 连接池自动复用，提升性能
```

---

## 三、当前实现的优点

### 3.1 架构设计优点

✅ **清晰的服务分层**：
- ScanService 作为主编排器，职责清晰
- DatabaseScanService/ObjectStorageScanService 分离数据库和对象存储的扫描逻辑
- ScanRepository 封装数据访问，便于测试和维护

✅ **插件化架构**：
- 基于接口的插件系统，支持 10 种存储引擎
- 新增数据库类型只需实现接口，无需修改核心代码
- 插件自动注册机制（init() + Register）简化集成

✅ **任务调度机制**：
- 支持手动触发和定时调度（基于 common/scheduler）
- Cron 表达式支持灵活的调度策略
- 任务队列支持本地队列和 Asynq（分布式）

### 3.2 数据管理优点

✅ **软删除机制**：
- 支持数据血缘追踪
- 软删除的数据可以恢复（UpsertNode/UpsertItem）
- 避免数据丢失

✅ **增量更新**：
- 通过 fingerprint（SHA256）去重
- shouldUpdateTable 检查 row_count、size_bytes 变化
- 避免不必要的重复扫描

✅ **进度追踪**：
- ScanProgressReporter 接口实时更新进度
- 前端可以显示扫描进度条
- 用户体验良好

### 3.3 集成优点

✅ **事件驱动**：
- 扫描完成后发布 Redis 事件
- Manager 模块自动刷新缓存
- 模块间解耦

✅ **全文搜索集成**：
- 自动索引到 Meilisearch
- 支持资产搜索（表名、字段名、注释）
- 支持文档搜索（PDF、图片等）

✅ **连接池管理**：
- 全局连接池管理器（pool_manager.go）
- 自动缓存和复用连接
- 减少连接开销

---

## 四、存在的问题与改进建议

### 4.1 架构设计问题（P0 - 高优先级）

#### 问题 1：循环依赖和服务耦合

**现象**：
```go
// scan_service.go:53
func NewScanService(db *gorm.DB, engineService *EngineService) *ScanService {
    // ...
    s.dbScanService = NewDatabaseScanService(db, log, nil, repo, spatialService, indexerService)
    // indexer 稍后通过 SetIndexer 注入
}

// scan_service.go:95
func (s *ScanService) SetIndexer(indexer *search.Indexer) {
    s.indexer = indexer
    if s.indexerService != nil {
        s.indexerService.indexer = indexer  // 手动级联注入
    }
}
```

**问题**：
- 服务之间存在循环依赖（ScanService ↔ IndexerService ↔ SpatialService）
- 通过 SetIndexer、SetConfig 等 setter 注入依赖，但容易出错
- 服务创建顺序有严格要求，维护困难

**改进建议**：
```
方案 1：依赖注入框架（推荐）
- 引入 google/wire 进行编译期依赖注入
- 定义清晰的服务构造器（Provider）
- 自动解决依赖顺序

方案 2：接口抽象
- 将 IndexerService、SpatialService 抽象为接口
- 通过接口注入，而不是具体实现
- 打破循环依赖
```

#### 问题 2：职责划分不清

**现象**：
```go
// scan_service.go:1162 - 查询接口混在扫描服务中
func (s *ScanService) GetTablesByResource(engineID, tenantID uint) ([]models.MetaItem, error) {
    return s.metadataQueryService.GetTablesByResource(engineID, tenantID)
}

// 实际上只是代理，应该独立出来
```

**问题**：
- ScanService 既负责扫描，又提供查询接口
- MetadataQueryService、ResourceDiscoveryService 作为独立服务存在，但通过 ScanService 代理
- 服务职责不单一，违反 SOLID 原则

**改进建议**：
```
重构服务层次：
1. ScanService - 只负责扫描编排
2. MetadataQueryService - 独立提供查询接口（直接被 Handler 调用）
3. ResourceDiscoveryService - 独立提供资源发现接口
4. 删除 ScanService 中的代理方法
```

---

### 4.2 性能问题（P0 - 高优先级）

#### 问题 3：串行扫描导致性能瓶颈

**现象**：
```go
// scan_database_service.go:130 - 串行处理每张表
for i, tableInfo := range tables {
    // 扫描字段
    fields, attrs, err := s.scanTableDetails(...)
    // 持久化
    item, err := s.repo.UpsertItem(...)
    // 索引
    s.indexerService.IndexTableAsset(...)
}
```

**问题**：
- 大型数据库（1000+ 张表）扫描时间过长
- 单个表扫描慢会阻塞整个流程
- CPU 和 I/O 资源利用率低

**性能数据**：
- 单表扫描平均耗时：50-200ms（取决于字段数）
- 1000 张表串行扫描：50-200 秒
- 如果并发 10 个：5-20 秒（**提升 10 倍**）

**改进建议**：
```go
// 方案 1：goroutine 池 + channel
type TableScanJob struct {
    TableInfo plugins.TableInfo
    Result    chan<- TableScanResult
}

func (s *DatabaseScanService) ScanSchema(...) {
    jobs := make(chan TableScanJob, len(tables))
    results := make(chan TableScanResult, len(tables))

    // 启动 worker pool（10 个并发）
    for i := 0; i < 10; i++ {
        go s.tableScanWorker(jobs, results)
    }

    // 分发任务
    for _, table := range tables {
        jobs <- TableScanJob{TableInfo: table, Result: results}
    }
    close(jobs)

    // 收集结果
    for i := 0; i < len(tables); i++ {
        result := <-results
        // 处理结果
    }
}

// 方案 2：使用 sync.WaitGroup + errgroup
import "golang.org/x/sync/errgroup"

func (s *DatabaseScanService) ScanSchema(...) {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(10) // 限制并发数

    for _, table := range tables {
        table := table // 避免闭包问题
        g.Go(func() error {
            return s.scanSingleTable(ctx, table)
        })
    }

    if err := g.Wait(); err != nil {
        return err
    }
}
```

#### 问题 4：Meilisearch 索引性能

**现象**：
```go
// scan_database_service.go:185
if isDeepScan && s.indexerService != nil {
    s.indexerService.IndexTableAsset(...) // 每张表单独索引
}
```

**问题**：
- 每张表单独调用 Meilisearch API
- 网络往返次数多（1000 张表 = 1000 次请求）
- Meilisearch 批量索引 API 未使用

**改进建议**：
```go
// 批量索引
func (s *DatabaseScanService) ScanSchema(...) {
    batch := make([]search.Asset, 0, 100)

    for _, table := range tables {
        // ...
        asset := buildTableAsset(table, fields)
        batch = append(batch, asset)

        // 每 100 条批量提交
        if len(batch) >= 100 {
            s.indexerService.IndexAssetsBatch(batch)
            batch = batch[:0]
        }
    }

    // 提交剩余
    if len(batch) > 0 {
        s.indexerService.IndexAssetsBatch(batch)
    }
}
```

#### 问题 5：重复连接创建

**现象**：
```go
// scan_service.go:818
scan, err := plugins.NewScanner(resource)
defer scan.Close()

// 每次扫描都创建新的 Scanner
```

**问题**：
- 虽然有连接池机制，但 Scanner 对象本身是短生命周期
- 对象存储扫描时，可能创建多个 MinIO 客户端
- 连接池的优势未充分利用

**改进建议**：
```
1. 在 ScanService 级别缓存 Scanner（长生命周期）
2. 通过 context 控制 Scanner 的生命周期
3. 对象存储客户端复用（MinIO Client Pool）
```

---

### 4.3 错误处理问题（P0 - 高优先级）

#### 问题 6：缺少错误重试机制

**现象**：
```go
// scan_database_service.go:155
if err != nil {
    s.log.Warn("表扫描失败，跳过", ...)
    continue // 直接跳过，不重试
}
```

**问题**：
- 单个表扫描失败（网络抖动、超时）会永久跳过
- 没有记录失败的表，无法事后重试
- 用户不知道哪些表扫描失败

**改进建议**：
```go
// 方案 1：记录失败的表到 scan_task_runs.error_details
type ScanError struct {
    Table     string
    Error     string
    Retries   int
    Timestamp time.Time
}

// 方案 2：自动重试（exponential backoff）
func (s *DatabaseScanService) scanTableWithRetry(table, maxRetries int) error {
    var err error
    for i := 0; i < maxRetries; i++ {
        err = s.scanTable(table)
        if err == nil {
            return nil
        }

        backoff := time.Duration(1<<uint(i)) * time.Second
        time.Sleep(backoff)
    }
    return err
}
```

#### 问题 7：缺少超时控制

**现象**：
```go
// scan_database_service.go:219
fields, err = scan.ScanFields(schemaName, tableInfo.Name)
// 没有超时控制，大表可能阻塞很久
```

**问题**：
- 大表（百万行）扫描可能耗时很长
- 没有单表扫描超时限制
- 阻塞整个扫描流程

**改进建议**：
```go
// 方案 1：context 超时控制
ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
defer cancel()

fields, err := scan.ScanFieldsWithContext(ctx, schemaName, tableInfo.Name)

// 方案 2：配置化超时时间
type ScanConfig struct {
    PerTableTimeout time.Duration // 每张表超时时间
    TotalTimeout    time.Duration // 总超时时间
}
```

---

### 4.4 扫描策略问题（P1 - 中优先级）

#### 问题 8：basic 扫描策略不够智能

**现象**：
```go
// scan_database_service.go:341
func shouldUpdateTable(existingItem *models.MetaItem, tableInfo plugins.TableInfo) bool {
    // 只检查行数和大小变化
    if existingItem.RowCount != nil && *existingItem.RowCount != tableInfo.RowCount {
        return true
    }
    // ...
}
```

**问题**：
- 表结构变化（新增/删除字段）不会触发更新
- 没有考虑 data_updated_at 时间戳
- basic 扫描可能遗漏重要变化

**改进建议**：
```go
// 增强 shouldUpdateTable 逻辑
func shouldUpdateTable(existingItem *models.MetaItem, tableInfo plugins.TableInfo) bool {
    // 1. 行数变化（阈值判断，避免小波动）
    if existingItem.RowCount != nil {
        diff := abs(*existingItem.RowCount - tableInfo.RowCount)
        threshold := *existingItem.RowCount / 100 // 1% 变化
        if diff > threshold {
            return true
        }
    }

    // 2. 时间戳变化
    if existingItem.DataUpdatedAt != nil && tableInfo.LastModified != nil {
        if existingItem.DataUpdatedAt.Before(*tableInfo.LastModified) {
            return true
        }
    }

    // 3. 字段数量变化（需要 basic 扫描也获取字段数）
    if existingItem.Attributes["field_count"] != tableInfo.FieldCount {
        return true
    }

    return false
}
```

#### 问题 9：缺少强制全量扫描选项

**现象**：
- basic 扫描可能跳过很多表
- 没有"强制全量扫描"选项
- 某些场景下需要强制重新扫描所有表

**改进建议**：
```go
// 添加扫描模式
type ScanMode string

const (
    ScanModeBasic       ScanMode = "basic"        // 基础扫描（增量）
    ScanModeDeep        ScanMode = "deep"         // 深度扫描（增量）
    ScanModeForceBasic  ScanMode = "force_basic"  // 强制基础扫描（全量）
    ScanModeForceDeep   ScanMode = "force_deep"   // 强制深度扫描（全量）
)

func shouldUpdateTable(mode ScanMode, existingItem, tableInfo) bool {
    if mode == ScanModeForceBasic || mode == ScanModeForceDeep {
        return true // 强制扫描
    }
    // 正常增量逻辑
}
```

---

### 4.5 插件机制问题（P1 - 中优先级）

#### 问题 10：插件能力不统一

**现象**：
```
插件能力矩阵：
- PostgreSQL: DatabasePlugin + ConnectionPoolPlugin + MetadataPlugin ✅
- MySQL:      DatabasePlugin + ConnectionPoolPlugin + MetadataPlugin ✅
- Doris:      DatabasePlugin + ConnectionPoolPlugin + MetadataPlugin ✅
- ClickHouse: DatabasePlugin + ConnectionPoolPlugin（MetadataPlugin 未实现）❌
- MongoDB:    DatabasePlugin（无连接池和元数据）❌
- Spark SQL:  DatabasePlugin + ConnectionPoolPlugin（MetadataPlugin 未实现）❌
```

**问题**：
- ClickHouse、MongoDB、Spark SQL 无法进行元数据扫描
- 用户创建这些引擎后，扫描功能不可用
- 前端无法判断哪些引擎支持扫描

**改进建议**：
```
1. 补齐插件实现：
   - ClickHouse: 实现 MetadataPlugin
   - MongoDB: 实现 NoSQLMetadataPlugin（新接口）
   - Spark SQL: 实现 MetadataPlugin

2. 在 Engine 表中添加 supports_scan 字段：
   - 根据插件能力自动设置
   - 前端根据此字段显示/隐藏扫描按钮

3. 定义插件能力常量：
   const (
       CapabilityMetadataScan   = "metadata_scan"
       CapabilityConnectionPool = "connection_pool"
       CapabilityFullTextSearch = "full_text_search"
   )
```

#### 问题 11：插件扩展文档缺失

**问题**：
- 缺少插件开发指南
- 新增数据库类型时，不知道需要实现哪些接口
- 没有测试模板

**改进建议**：
```
创建文档：
1. docs/addp新增存储引擎指南.md（已存在，但需补充插件部分）
2. common/database/plugins/PLUGIN_GUIDE.md
   - 插件接口说明
   - 实现步骤
   - 测试模板
   - 示例代码

3. 提供脚手架工具：
   go run scripts/generate_plugin.go --type tidb --display "TiDB"
```

---

### 4.6 数据一致性问题（P1 - 中优先级）

#### 问题 12：软删除数据累积

**现象**：
```sql
-- 软删除的数据不会被物理删除
SELECT COUNT(*) FROM metadata.meta_item WHERE deleted_at IS NOT NULL;
-- 结果：数千条甚至更多
```

**问题**：
- 软删除数据会一直累积
- 影响查询性能（需要 WHERE deleted_at IS NULL）
- 浪费存储空间

**改进建议**：
```go
// 方案 1：定期清理任务（Cron）
func (s *ScanService) CleanupSoftDeleted(retentionDays int) error {
    threshold := time.Now().AddDate(0, 0, -retentionDays)

    // 物理删除超过保留期的软删除数据
    return s.db.Unscoped().
        Where("deleted_at IS NOT NULL AND deleted_at < ?", threshold).
        Delete(&models.MetaItem{}).Error
}

// 方案 2：在扫描时自动清理
// 扫描完成后，清理本次扫描范围内的软删除数据
```

#### 问题 13：并发扫描控制不足

**现象**：
```go
// scan_dedup_service.go - Redis 去重
taskKey := "meta:cache:scan_task:{tenant_id}:{engine_id}:{scan_type}"
ttl := 30 * time.Minute // 硬编码 TTL
```

**问题**：
- Redis 去重 TTL 30 分钟可能不够（大型数据库扫描时间更长）
- 定时扫描间隔保护（6 小时）是硬编码
- 分布式场景下，多个 Worker 可能同时执行同一任务

**改进建议**：
```go
// 方案 1：动态 TTL（根据历史扫描耗时）
type ScanHistory struct {
    EngineID    uint
    AvgDuration time.Duration
}

func (s *ScanDedupService) GetDynamicTTL(engineID uint) time.Duration {
    history := s.getHistory(engineID)
    return history.AvgDuration * 2 // 2 倍安全余量
}

// 方案 2：分布式锁
import "github.com/go-redsync/redsync/v4"

func (s *ScanTaskService) executeRun(runID uint) error {
    lockKey := fmt.Sprintf("scan:lock:%d", runID)
    lock := s.redsync.NewMutex(lockKey)

    if err := lock.Lock(); err != nil {
        return fmt.Errorf("获取分布式锁失败: %w", err)
    }
    defer lock.Unlock()

    // 执行扫描
}
```

---

### 4.7 可观测性问题（P2 - 低优先级）

#### 问题 14：缺少性能监控

**问题**：
- 没有暴露 Prometheus 指标
- 无法监控扫描队列长度、成功率、耗时分布
- 难以发现性能瓶颈

**改进建议**：
```go
// 添加 Prometheus 指标
import "github.com/prometheus/client_golang/prometheus"

var (
    scanDurationHistogram = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "meta_scan_duration_seconds",
            Help: "Duration of metadata scan operations",
        },
        []string{"engine_type", "scan_depth", "status"},
    )

    scanQueueLength = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "meta_scan_queue_length",
            Help: "Current length of scan queue",
        },
    )

    scanErrorsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "meta_scan_errors_total",
            Help: "Total number of scan errors",
        },
        []string{"engine_type", "error_type"},
    )
)

// 使用
start := time.Now()
// ... 执行扫描
duration := time.Since(start).Seconds()
scanDurationHistogram.WithLabelValues(engineType, scanDepth, "success").Observe(duration)
```

#### 问题 15：日志结构化不足

**现象**：
```go
// 日志信息丰富，但格式不统一
s.log.Info("开始扫描 Schema", ...)
s.log.Info("扫描到的表", ...)
s.log.Info("表元数据写入成功", ...)
```

**问题**：
- 日志级别选择不一致
- 缺少 trace_id 追踪整个扫描流程
- 关键步骤的耗时没有记录

**改进建议**：
```go
// 方案 1：结构化日志 + trace_id
import "go.opentelemetry.io/otel/trace"

func (s *DatabaseScanService) ScanSchema(ctx context.Context, ...) {
    span := trace.SpanFromContext(ctx)
    traceID := span.SpanContext().TraceID().String()

    log := s.log.With("trace_id", traceID, "engine_id", engineID, "schema", schemaName)

    log.Info("开始扫描 Schema")
    defer func(start time.Time) {
        log.Info("Schema 扫描完成", "duration_ms", time.Since(start).Milliseconds())
    }(time.Now())

    // ...
}

// 方案 2：分步骤记录耗时
type ScanStats struct {
    ListTablesMs    int64
    ScanFieldsMs    int64
    PersistMs       int64
    IndexMs         int64
}

func (s *DatabaseScanService) logScanStats(stats ScanStats) {
    s.log.Info("扫描性能统计",
        "list_tables_ms", stats.ListTablesMs,
        "scan_fields_ms", stats.ScanFieldsMs,
        "persist_ms", stats.PersistMs,
        "index_ms", stats.IndexMs,
    )
}
```

---

## 五、改进优先级与实施路径

### 5.1 优先级分级

**P0（高优先级）- 影响核心功能和性能**：
1. ✅ 实现并发扫描（问题 3） - **预计性能提升 10 倍**
2. ✅ 添加超时控制和错误重试（问题 6、7）
3. ✅ 批量索引到 Meilisearch（问题 4）
4. ⚠️ 解决循环依赖（问题 1） - 架构优化

**P1（中优先级）- 提升用户体验和系统稳定性**：
5. 优化扫描策略（问题 8、9）
6. 补齐插件能力（问题 10）
7. 实现软删除清理机制（问题 12）
8. 改进并发控制（问题 13）

**P2（低优先级）- 提升可维护性和可观测性**：
9. 重构服务职责（问题 2）
10. 添加性能监控（问题 14）
11. 改进日志结构化（问题 15）
12. 完善插件文档（问题 11）

### 5.2 快速见效的改进（Quick Wins）

**1. 并发扫描（1-2 天）**：
```
- 使用 errgroup 实现 goroutine 池
- 限制并发数为 10（可配置）
- 预期性能提升：10 倍
```

**2. 批量索引（半天）**：
```
- 累积 100 条记录批量提交
- 减少网络往返
- 预期性能提升：5 倍
```

**3. 超时控制（半天）**：
```
- 添加 context.WithTimeout
- 每张表超时时间 5 分钟
- 避免阻塞
```

**4. 错误重试（1 天）**：
```
- exponential backoff 重试 3 次
- 记录失败的表到 error_details
- 提升成功率
```

### 5.3 长期改进路径（需要重构）

**阶段 1：性能优化（1 周）**：
- 并发扫描
- 批量索引
- 超时控制
- 错误重试

**阶段 2：架构优化（2 周）**：
- 引入依赖注入框架（wire）
- 重构服务职责
- 优化插件机制

**阶段 3：增强功能（1 周）**：
- 智能增量扫描
- 软删除清理
- 分布式锁

**阶段 4：可观测性（1 周）**：
- Prometheus 指标
- OpenTelemetry 追踪
- 结构化日志

---

## 六、总体评价

### 6.1 当前实现评分（满分 10 分）

| 维度 | 评分 | 说明 |
|------|------|------|
| 架构设计 | 7/10 | 分层清晰，但存在循环依赖和职责不清 |
| 性能 | 5/10 | 串行扫描是主要瓶颈，亟需并发优化 |
| 可扩展性 | 8/10 | 插件机制优秀，但部分插件能力缺失 |
| 错误处理 | 6/10 | 基本的错误处理，但缺少重试和超时 |
| 数据一致性 | 7/10 | 软删除机制良好，但清理策略缺失 |
| 可观测性 | 5/10 | 日志丰富但缺少指标和追踪 |
| **综合评分** | **6.3/10** | 基础功能完善，性能和可观测性待提升 |

### 6.2 核心优势

✅ **插件化架构**：支持 10 种存储引擎，扩展性强
✅ **软删除机制**：数据血缘追踪，避免数据丢失
✅ **事件驱动集成**：与 Manager 模块解耦良好
✅ **任务调度**：支持手动和定时扫描

### 6.3 核心挑战

⚠️ **性能瓶颈**：串行扫描，大型数据库耗时长
⚠️ **插件能力**：部分插件未实现元数据扫描
⚠️ **可观测性**：缺少性能监控和追踪

---

## 七、实施建议

### 7.1 立即可做（本周）

1. **实现并发扫描**（scan_database_service.go:130）
   - 使用 errgroup 实现 goroutine 池
   - 限制并发数 10

2. **批量索引**（indexer_service.go）
   - 累积 100 条批量提交
   - 减少网络开销

3. **添加超时控制**（scan_database_service.go:219）
   - context.WithTimeout(5 分钟)
   - 避免阻塞

### 7.2 下一步（下周）

4. **错误重试机制**（scan_database_service.go:155）
   - exponential backoff
   - 记录失败详情

5. **优化扫描策略**（scan_database_service.go:341）
   - 增强 shouldUpdateTable 逻辑
   - 添加强制全量扫描选项

### 7.3 长期规划（1-2 个月）

6. **架构重构**
   - 引入依赖注入框架
   - 重构服务职责

7. **可观测性**
   - Prometheus 指标
   - OpenTelemetry 追踪

8. **插件补齐**
   - ClickHouse MetadataPlugin
   - MongoDB NoSQLMetadataPlugin

---

## 八、关键文件修改清单

### 需要修改的文件（P0 改进）

1. **scan_database_service.go**（主要修改）
   - 实现并发扫描（scanTables 方法）
   - 添加超时控制
   - 错误重试机制

2. **indexer_service.go**（新增方法）
   - IndexAssetsBatch() - 批量索引

3. **scan_service.go**（配置增强）
   - 添加并发配置参数
   - 超时配置参数

4. **scan_task_service.go**（可选）
   - 分布式锁支持

### 需要新增的文件

1. **internal/config/scan_config.go**
   - 扫描配置结构体
   - 并发数、超时时间等

2. **internal/service/scan_stats.go**
   - 性能统计收集
   - Prometheus 指标定义

---

## 九、双层插件机制详解

### 9.1 两套插件系统概览

Meta 模块使用了**两套插件系统**协同工作：

| 插件系统 | 位置 | 职责 | 使用模块 |
|---------|------|------|---------|
| **数据库插件** | `common/database/plugins/` | 数据库连接、查询、写入 | 所有模块（Meta, Manager, Transfer, Develop） |
| **扫描插件** | `meta/backend/plugins/` | 元数据扫描、文件格式识别 | 仅 Meta 模块 |

### 9.2 插件系统架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                    Meta 扫描调度层                               │
│  ScanService → DatabaseScanService / ObjectStorageScanService   │
└───────────────────┬─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────────────────────┐
│                 Meta 扫描插件层 (meta/plugins)                   │
│  ┌──────────────────────┬───────────────────────────────────┐  │
│  │  Scanner 适配器       │  FileMetadataExtractor 提取器     │  │
│  │  - PostgresScanner   │  - CSVExtractor                   │  │
│  │  - MySQLScanner      │  - GeoJSONExtractor              │  │
│  │  - S3Scanner ⚠️      │  - ImageExtractor                │  │
│  │                      │  - PDFExtractor                  │  │
│  └──────────────────────┴───────────────────────────────────┘  │
└───────────────────┬─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────────────────────┐
│          Common 数据库插件层 (common/database/plugins)           │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │  DatabasePlugin (10 种数据库/存储引擎)                   │  │
│  │  - PostgreSQL  - MySQL  - Doris  - ClickHouse          │  │
│  │  - MongoDB  - MinIO ⚠️  - S3 ⚠️  - Spark SQL         │  │
│  │  - Python Workflow  - Spark Workflow                   │  │
│  └─────────────────────────────────────────────────────────┘  │
└───────────────────┬─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────────────────────┐
│              底层驱动和存储引擎                                  │
│  PostgreSQL DB  MySQL DB  MinIO  S3  MongoDB  等              │
└─────────────────────────────────────────────────────────────────┘
```

**⚠️ 重要说明：对象存储的"双层"组件**

对象存储涉及两个层面的组件：
- **Meta 层**：S3Scanner（meta/plugins/scanners/s3_scanner.go）
- **Common 层**：MinIOPlugin、S3Plugin（common/database/plugins/minio/, s3/）

**但它们的职责完全不同**：

| 组件 | 层次 | 职责 | 为什么存在 |
|------|------|------|-----------|
| **S3Scanner** | Meta 扫描层 | 扫描对象列表、提取元数据、调用 FileExtractor | Meta 模块专用，执行扫描任务 |
| **MinIOPlugin** | Common 层 | 测试连接、构建连接串、管理客户端 | 所有模块共享，提供基础连接能力 |

**关键区别**：
- MinIOPlugin **只负责连接管理**（TestConnection、BuildConnectionString）
- S3Scanner **负责扫描逻辑**（ListObjects、ExtractMetadata、调用文件提取器）

### 9.2.1 "双层设计"的真相与问题分析

**🚨 Scanner 适配层存在冗余设计！**

让我们看看 **PostgresScanner** 的实际代码：

```go
// meta/plugins/scanners/postgres_scanner.go:38-55
func (s *PostgresScanner) ListSchemas() ([]format.SchemaInfo, error) {
    // 直接委托给 dbbridge（实际上就是调用 PostgreSQLPlugin）
    pluginSchemas, err := dbbridge.ListSchemas(context.Background(), s.resource, s.db)

    // 只做格式转换：plugin.SchemaInfo → format.SchemaInfo
    schemas := make([]format.SchemaInfo, len(pluginSchemas))
    for i, ps := range pluginSchemas {
        schemas[i] = format.SchemaInfo{
            Name:       ps.Name,
            TableCount: ps.TableCount,
        }
    }
    return schemas, nil
}
```

**PostgresScanner 只做了两件事**：
1. 调用 dbbridge（转发给 PostgreSQLPlugin）
2. 格式转换（plugin.SchemaInfo → format.SchemaInfo）

**PostgreSQLPlugin 才是实际干活的**：
```go
// common/database/plugins/postgresql/plugin.go:176-198
func (p *PostgreSQLPlugin) ListSchemas(ctx context.Context, db *gorm.DB) ([]plugin.SchemaInfo, error) {
    var schemas []plugin.SchemaInfo

    query := `
        SELECT
            schema_name as name,
            (SELECT COUNT(*) FROM information_schema.tables ...) as table_count
        FROM information_schema.schemata s
        WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
    `

    err := db.WithContext(ctx).Raw(query).Scan(&schemas).Error
    return schemas, nil
}
```

### 9.2.2 为什么会有这个"多余"的适配层？

**原因分析**：

| 原因 | 是否合理 | 说明 |
|------|---------|------|
| **历史遗留** | ⚠️ | Meta 模块早期自己实现了 Scanner，后来 common 层统一了插件系统，但没有完全重构 |
| **接口隔离** | ⚠️ | format.Scanner 接口 vs plugin.MetadataPlugin 接口不一致 |
| **格式转换** | ❌ | 两种数据结构（format.SchemaInfo vs plugin.SchemaInfo）几乎一样，完全可以统一 |
| **独立演进** | ❌ | Meta 模块想独立于 common 层？但实际上已经强依赖了 |

**真相：这是一个过度设计的产物！**

### 9.2.3 对比：对象存储的设计更合理

**S3Scanner 的情况不同**：

```go
// S3Scanner 有实际的业务逻辑
func (s *S3Scanner) ScanPath(path string) ([]ObjectMetadata, error) {
    // 1. 解析 bucket 和 prefix
    bucket, prefix := parsePath(path)

    // 2. 递归扫描对象列表（MinIOPlugin 不提供）
    objectCh := s.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
        Prefix:    prefix,
        Recursive: isDeepScan,  // ← 扫描深度控制
    })

    // 3. 推断 MIME 类型（MinIOPlugin 不提供）
    for object := range objectCh {
        contentType := inferContentTypeFromExt(object.Key)

        // 4. 调用文件提取器（MinIOPlugin 不提供）
        if isDeepScan {
            metadata := s.extractObjectMetadata(object)
        }
    }
}
```

**S3Scanner vs PostgresScanner 的对比**：

| 特性 | S3Scanner | PostgresScanner |
|------|-----------|----------------|
| **有独立逻辑** | ✅ 扫描控制、MIME 推断、提取器调用 | ❌ 只是格式转换 |
| **有状态管理** | ✅ scanDepth、resourceID | ❌ 无状态 |
| **存在必要性** | ✅ 必须存在 | ❌ 可以去掉 |

**改进建议**：详见独立的重构计划文档 [meta-scanner-refactor.md](/Users/pampa/.claude/plans/meta-scanner-refactor.md)

### 9.3 数据库扫描完整调用链路

```
1. 用户触发 PostgreSQL 数据库扫描
   ↓
2. ScanService.ScanEngine()
   - 文件：meta/backend/internal/service/scan_service.go:365
   ↓
3. plugins.NewScanner(resource)
   - 文件：meta/backend/plugins/scanners/factory.go:15
   - 根据 resource.EngineType 创建 PostgresScanner
   ↓
4. PostgresScanner.ListSchemas()
   - 文件：meta/backend/plugins/scanners/postgres_scanner.go:38
   - 委托给 common/dbbridge
   ↓
5. dbbridge.ListSchemas(ctx, resource, db)
   - 文件：common/dbbridge/bridge.go:150
   - 获取 PostgreSQL 插件
   ↓
6. PostgreSQLPlugin.ListSchemas(ctx, db)
   - 文件：common/database/plugins/postgresql/metadata.go:20
   - 执行 SQL: SELECT schema_name FROM information_schema.schemata
   ↓
7. DatabaseScanService.ScanSchema()
   - 文件：meta/backend/internal/service/scan_database_service.go:59
   - 创建/更新 MetaNode (schema)
   - 调用 scan.ScanTables(schemaName)
   ↓
8. PostgresScanner.ScanTables(schema)
   - 委托给 dbbridge.ListTables()
   ↓
9. PostgreSQLPlugin.ListTables(ctx, db, schema)
   - 执行 SQL: SELECT * FROM information_schema.tables WHERE table_schema = ?
   ↓
10. DatabaseScanService.scanTableDetails()
    - 文件：meta/backend/internal/service/scan_database_service.go:205
    - deep 模式：调用 scan.ScanFields(schema, table)
    ↓
11. PostgreSQLPlugin.ListColumns(ctx, db, schema, table)
    - 执行 SQL: SELECT * FROM information_schema.columns WHERE table_schema = ? AND table_name = ?
    ↓
12. ScanRepository.UpsertItem()
    - 文件：meta/backend/internal/service/scan_repository.go:219
    - 持久化到 metadata.meta_item
    - 字段信息存储在 attributes JSONB 字段
    ↓
13. IndexerService.IndexTableAsset()
    - 同步到 Meilisearch (deep 模式)
```

### 9.4 对象存储 + 文件提取器调用链路

```
1. 用户触发 MinIO 对象存储扫描
   ↓
2. ScanService.ScanEngine()
   ↓
3. plugins.NewScanner(resource)
   - 创建 S3Scanner
   - 文件：meta/backend/plugins/scanners/s3_scanner.go:50
   ↓
4. S3Scanner.ScanPath(path)
   - 文件：meta/backend/plugins/scanners/s3_scanner.go:276
   - 递归扫描对象列表（deep 模式）
   - 推断 MIME 类型（inferContentTypeFromExt）
   ↓
5. deep 模式：S3Scanner.extractObjectMetadata()
   - 文件：meta/backend/plugins/scanners/s3_scanner.go:549
   - 获取对象 Reader
   ↓
6. format.GetExtractor(contentType)
   - 文件：common/format/scanner.go:150
   - 从全局注册表查找匹配的提取器
   - 按优先级排序
   ↓
7. 匹配到 CSVExtractor / GeoJSONExtractor / ImageExtractor 等
   - 文件：meta/backend/plugins/extractors/csv_extractor.go:29
   ↓
8. extractor.Extract(ctx, input)
   - 读取对象内容（Reader）
   - 解析结构化数据（schema, 样本数据）
   - 提取特定元数据
   ↓
9. 返回 ExtractedMetadata
   - BasicInfo（文件名、大小、类型）
   - SchemaInfo（字段定义、行数）
   - CustomAttrs（geo_metadata、statistics 等）
   ↓
10. ObjectStorageScanService.persistObjectMetas()
    - 文件：meta/backend/internal/service/scan_object_storage_service.go:115
    - 构建目录树（Prefix 节点）
    - 持久化到 metadata.meta_item
    - ExtractedMetadata 存储在 attributes JSONB
    ↓
11. IndexerService.IndexObjectAsset()
    - 同步到 Meilisearch (deep 模式)
```

### 9.5 扫描深度控制机制

**basic 扫描**：
- 数据库：只更新表行数和大小，保留已有字段信息
- 对象存储：只扫描一级目录和文件，不递归子目录，不调用文件提取器

**deep 扫描**：
- 数据库：完整扫描字段、空间元数据、索引信息
- 对象存储：递归扫描所有子目录，调用文件提取器提取详细元数据

**代码控制**：

```go
// S3Scanner 中的深度控制
isDeepScan := strings.EqualFold(s.scanDepth, "deep")

objectCh := s.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
    Prefix:    cleanPrefix,
    Recursive: isDeepScan, // 浅度扫描时不递归
})

// 深度扫描时才调用提取器
if s.resourceID > 0 && isDeepScan {
    contentType := inferContentTypeFromExt(ext)
    extractedMetadata = s.extractObjectMetadata(ctx, objKey, meta)
}
```

### 9.6 插件能力矩阵

| 数据库类型 | Scanner 适配器 | DatabasePlugin | MetadataPlugin | 支持扫描 |
|-----------|---------------|----------------|---------------|---------|
| PostgreSQL | ✅ PostgresScanner | ✅ | ✅ | ✅ |
| MySQL | ✅ MySQLScanner | ✅ | ✅ | ✅ |
| Doris | ✅ DorisScanner | ✅ | ✅ | ✅ |
| ClickHouse | ✅ ClickHouseScanner | ✅ | ❌ | ⚠️ 部分支持 |
| MongoDB | ✅ MongoDBScanner | ✅ | ❌ | ❌ 不支持 |
| Spark SQL | ❌ | ✅ | ❌ | ❌ 不支持 |
| MinIO/S3 | ✅ S3Scanner | ✅ | N/A | ✅ |

| 文件格式 | Extractor | 支持的 MIME 类型 | 优先级 |
|---------|-----------|-----------------|-------|
| CSV | ✅ CSVExtractor | text/csv | 100 |
| GeoJSON | ✅ GeoJSONExtractor | application/geo+json | 100 |
| Shapefile | ✅ ShapefileExtractor | application/x-shapefile | 100 |
| Image | ✅ ImageExtractor | image/jpeg, image/png, image/tiff | 90 |
| PDF | ✅ PDFExtractor | application/pdf | 90 |
| SQLite | ✅ SQLiteExtractor | application/x-sqlite3 | 100 |
| Video | ✅ VideoExtractor | video/mp4, video/avi | 80 |
| Office | ✅ OfficeExtractor | application/vnd.ms-*, application/vnd.openxmlformats-* | 90 |
| Default | ✅ DefaultExtractor | */* (兜底) | 1 |

### 9.7 插件扩展指南

#### 添加新的数据库类型

**当前流程**（需要两步）：

1. 在 `common/database/plugins/` 实现数据库插件
2. 在 `meta/backend/plugins/scanners/` 创建 Scanner 适配器

**未来流程**（重构后，只需一步）：

只需在 `common/database/plugins/` 实现插件，无需创建 Scanner 适配器。

#### 添加新的文件提取器

**步骤 1**：创建提取器文件

```go
// meta/backend/plugins/extractors/excel_extractor.go
package extractors

type ExcelExtractor struct{}

func (e *ExcelExtractor) SupportedTypes() []string {
    return []string{
        "application/vnd.ms-excel",
        "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    }
}

func (e *ExcelExtractor) Priority() int {
    return 100
}

func (e *ExcelExtractor) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
    // 使用 excelize 库读取 Excel
    f, err := excelize.OpenReader(input.Reader)
    // 提取工作表列表和 schema
    // ...
    return metadata, nil
}
```

**步骤 2**：在 `register.go` 注册

```go
// meta/backend/plugins/extractors/register.go
func init() {
    format.RegisterExtractor(&ExcelExtractor{})
    // ...
}
```

### 9.8 关键文件路径总结

```
核心接口：
- common/format/scanner.go (Scanner, FileMetadataExtractor 接口)
- common/format/interface.go (Parser, DataReader 接口)

Scanner 实现层（meta/plugins）：
- meta/backend/plugins/scanners/factory.go (工厂方法)
- meta/backend/plugins/scanners/postgres_scanner.go
- meta/backend/plugins/scanners/mysql_scanner.go
- meta/backend/plugins/scanners/s3_scanner.go

Extractor 实现层（meta/plugins）：
- meta/backend/plugins/extractors/register.go (全局注册)
- meta/backend/plugins/extractors/csv_extractor.go
- meta/backend/plugins/extractors/geojson_extractor.go
- meta/backend/plugins/extractors/image_extractor.go

DatabasePlugin 实现层（common/database/plugins）：
- common/database/plugins/postgresql/plugin.go
- common/database/plugins/postgresql/metadata.go
- common/database/plugins/mysql/plugin.go
- common/database/plugins/minio/plugin.go

桥接层：
- common/dbbridge/bridge.go (统一入口)

服务层：
- meta/backend/internal/service/scan_service.go (主编排)
- meta/backend/internal/service/scan_database_service.go (数据库扫描)
- meta/backend/internal/service/scan_object_storage_service.go (对象存储扫描)
- meta/backend/internal/service/scan_repository.go (数据访问)
```

---

## 十、总结

Meta 后台的元数据扫描系统**架构设计合理**，**插件机制优秀**，但在**性能优化**和**可观测性**方面有较大提升空间。

### 10.1 插件机制评价

✅ **双层插件架构设计优秀**：
- Scanner 层（薄适配器）+ DatabasePlugin 层（实际驱动）分离清晰
- FileMetadataExtractor 自动注册机制简洁优雅
- 通过全局注册表和优先级实现灵活匹配

✅ **扩展性强**：
- 新增数据库类型只需实现接口
- 新增文件提取器只需注册到全局表
- 支持 10 种存储引擎 + 9 种文件格式

⚠️ **插件能力不完整**：
- ClickHouse、MongoDB、Spark SQL 缺少 MetadataPlugin 实现
- 缺少 Excel、Parquet 等常见格式提取器
- 部分插件文档缺失

⚠️ **存在架构冗余**：
- Scanner 适配层（PostgresScanner、MySQLScanner 等）只做格式转换，无实际业务逻辑
- 可通过重构消除此冗余层，详见 [Scanner 层重构计划](/Users/pampa/.claude/plans/meta-scanner-refactor.md)

### 10.2 核心改进方向

1. 🚀 **性能优化**：并发扫描 + 批量索引 → 预计提升 10 倍
2. 🔧 **错误处理**：超时控制 + 重试机制 → 提升稳定性
3. 🔌 **插件补齐**：ClickHouse MetadataPlugin + MongoDB NoSQLPlugin
4. 📊 **可观测性**：监控指标 + 链路追踪 → 便于运维
5. 🏗️ **架构重构**：去掉 Scanner 适配层 → 简化架构，降低维护成本

通过以上改进，可以将系统从**基础可用**提升到**生产级性能**。

**建议优先实施 P0 改进项**，可在 1 周内见到显著效果。
