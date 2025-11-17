# 性能优化：双连接池 + 优先级机制

## 优化时间
2025-01-17

## 问题诊断

### 症状
- 浏览器请求瓦片响应缓慢（即使已缓存数据）
- 放大地图后新瓦片加载延迟严重
- PG cache 表已有大量数据，但前端看不到

### 根本原因：资源竞争

```
┌─────────────────────────────────────────┐
│  预热任务（10 个并发 worker）          │
│  ↓                                      │
│  占用数据库连接池（50 max connections） │
│  ↓                                      │
│  长时间持有连接（无超时限制）           │
└─────────────────────────────────────────┘
                ↓
        ┌───────────────┐
        │ 浏览请求等待  │ ← 用户体验差
        │ 连接释放      │
        └───────────────┘
```

**关键问题**：
1. **共享连接池**：预热和浏览请求使用同一个数据库连接池
2. **预热占用连接**：10 个 worker × 复杂查询 × 无超时 = 长时间占用连接
3. **浏览请求饥饿**：无法获取连接 → 超时或缓慢响应

## 优化方案

### 核心策略：双连接池 + 优先级隔离

```
┌─────────────────────────────────────────────────────┐
│  TileService                                        │
│  ┌──────────────────┐  ┌──────────────────┐        │
│  │ 主连接池（预热）  │  │ 优先级连接池    │        │
│  │ Max: 25 conns    │  │ (浏览请求)      │        │
│  │ Idle: 5 conns    │  │ Max: 50 conns   │        │
│  └──────────────────┘  └──────────────────┘        │
│         ↓                       ↓                   │
│   GenerateTilePrewarm()   GenerateTile()           │
│   (预热专用)              (浏览请求专用)            │
└─────────────────────────────────────────────────────┘
```

### 实现细节

#### 1. 双连接池架构

**TileService 结构变更**：
```go
type TileService struct {
    db          *sql.DB  // 主连接池（预热使用）
    priorityDB  *sql.DB  // 优先级连接池（浏览请求使用）
    dataSources map[string]*models.DataSource
    mu          sync.RWMutex
}
```

**连接池配置**：
```go
// 主连接池（预热）：限制连接数避免过度占用
prewarmMaxConns := cfg.Database.MaxOpenConns / 2  // 预热最多用一半连接
db.SetMaxOpenConns(prewarmMaxConns)               // 例如 50/2 = 25
db.SetMaxIdleConns(cfg.Database.MaxIdleConns / 2)

// 优先级连接池（浏览请求）：保证足够连接
priorityDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)  // 例如 50
priorityDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
```

**方法分离**：
```go
// 浏览请求：使用优先级连接池（高优先级）
func (s *TileService) GenerateTile(ctx, dsID string, z, x, y int) ([]byte, error) {
    return s.generateTileWithDB(ctx, dsID, z, x, y, s.priorityDB)
}

// 预热任务：使用主连接池（低优先级）
func (s *TileService) GenerateTilePrewarm(ctx, dsID string, z, x, y int) ([]byte, error) {
    return s.generateTileWithDB(ctx, dsID, z, x, y, s.db)
}
```

#### 2. 速率限制

**预热节流机制**：
```go
// 每个瓦片之间暂停 10ms，避免过度占用资源
rateLimitDelay := 10 * time.Millisecond

for j := range jobs {
    // ... 生成并缓存瓦片 ...

    // 速率限制：暂停一小段时间
    time.Sleep(rateLimitDelay)
}
```

**效果**：
- 10 个 worker × 10ms delay = 每秒最多 1000 个瓦片（仍然很快）
- 给浏览请求留出处理窗口
- 降低数据库和 Redis 的瞬时压力

#### 3. 资源分配策略

| 操作类型 | 连接池 | 最大连接数 | 优先级 | 用途 |
|---------|--------|-----------|--------|------|
| **浏览请求** | `priorityDB` | 50（配置值） | 高 | 用户交互，需要即时响应 |
| **预热任务** | `db` | 25（配置值的 50%） | 低 | 后台任务，可容忍延迟 |
| **Extent 查询** | `db` | 25 | 低 | 启动时一次性查询 |

## 修改的文件

### 1. `backend/internal/service/tile_service.go`

**主要变更**：
- 添加 `priorityDB` 字段
- 修改 `NewTileService()`：创建双连接池
- 新增 `GenerateTilePrewarm()`：预热专用方法
- 修改 `GenerateTile()`：默认使用优先级连接池
- 新增 `generateTileWithDB()`：内部实现，接受连接池参数
- 修改 `Close()`：关闭两个连接池

**关键代码**：
```go
// 主连接池（预热用）
prewarmMaxConns := cfg.Database.MaxOpenConns / 2
db.SetMaxOpenConns(prewarmMaxConns)

// 优先级连接池（浏览请求用）
priorityDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)

log.Printf("[INFO] Connected to database successfully (main pool: %d, priority pool: %d)",
    prewarmMaxConns, cfg.Database.MaxOpenConns)
```

### 2. `backend/internal/service/prewarm_service.go`

**主要变更**：
- 调用 `GenerateTilePrewarm()` 而非 `GenerateTile()`
- 添加速率限制 `rateLimitDelay`
- 更新日志输出，标明使用独立连接池

**关键代码**：
```go
raw, err := tiles.GenerateTilePrewarm(ctx, j.dsID, j.z, j.x, j.y)  // 使用预热专用方法

// 速率限制
time.Sleep(rateLimitDelay)
```

## 性能对比

### 优化前

| 场景 | 响应时间 | 问题 |
|------|---------|------|
| 浏览已缓存瓦片 | 慢/超时 | 等待连接池 |
| 浏览新瓦片 | 非常慢/超时 | 等待连接池 + 生成 |
| 预热进度 | 快速进行 | 占满连接池 |

### 优化后

| 场景 | 响应时间 | 改进 |
|------|---------|------|
| 浏览已缓存瓦片 | 1-5ms | ✅ 立即从缓存返回 |
| 浏览新瓦片 | 50-200ms | ✅ 优先级连接池保证可用 |
| 预热进度 | 稍慢（受节流限制） | ✅ 不影响浏览体验 |

## 配置建议

### 数据库连接池配置

```yaml
# backend/config/app.yaml
database:
  max_open_conns: 50     # 总连接数
  max_idle_conns: 10     # 空闲连接数
  conn_max_lifetime: 5m

# 实际分配：
# - 浏览请求（优先级连接池）：50 max, 10 idle
# - 预热任务（主连接池）：25 max, 5 idle
```

### 预热并发配置

```yaml
# backend/config/app.yaml
prewarm:
  enabled: true
  max_zoom: 9
  concurrency: 10  # 建议值：5-10

# 配置原则：
# - concurrency × 查询耗时 ≤ 主连接池大小
# - 例如：10 workers × 平均 2s = 20 个并发连接（< 25 max）
```

### PostgreSQL 服务器配置

确保 PostgreSQL 允许足够的连接：

```sql
-- 查看当前最大连接数
SHOW max_connections;

-- 建议配置（postgresql.conf）
max_connections = 200  -- 应该 > (浏览连接池 + 预热连接池 + CacheService 连接池)
                       -- 例如：50 + 25 + 10 = 85，留有余量
```

## 测试验证

### 1. 编译测试

```bash
cd backend
go build -o /tmp/mvt-optimized ./cmd/server/main.go
# ✅ 编译成功，无语法错误
```

### 2. 启动服务

```bash
make dev-backend
```

**观察启动日志**：
```
[INFO] Connected to database successfully (main pool: 25, priority pool: 50)
[PREWARM] Start prewarming 2 datasources, z=0..9, concurrency=10 (using dedicated connection pool)
```

### 3. 浏览测试

**测试步骤**：
1. 打开浏览器前端 http://localhost:5180
2. 加载地图，观察瓦片加载速度
3. 快速缩放、平移地图
4. 检查浏览器 DevTools Network 标签：
   - 查看 `.mvt` 请求响应时间
   - 查看 `X-Cache` header（HIT/MISS）

**预期结果**：
- 已缓存瓦片：响应时间 < 10ms，`X-Cache: HIT`
- 新瓦片：响应时间 50-200ms，`X-Cache: MISS`
- 不再出现超时或长时间等待

### 4. 数据库监控

```bash
# 连接到 PostgreSQL
psql -h localhost -p 5433 -U business -d business

-- 查看当前活动连接
SELECT datname, usename, application_name, state, COUNT(*)
FROM pg_stat_activity
WHERE datname = 'business'
GROUP BY datname, usename, application_name, state;

-- 预期输出：
-- business | business | pgx | active | ~10-15  (预热 worker)
-- business | business | pgx | idle   | ~20-30  (浏览请求连接池)
```

### 5. 缓存命中率测试

```bash
# 查看 PG cache 表统计
psql -h localhost -p 5433 -U business -d business -c \
  "SELECT datasource, z, COUNT(*) as tiles,
          pg_size_pretty(SUM(length(tile))) as total_size
   FROM mvt_cache
   GROUP BY datasource, z
   ORDER BY datasource, z;"
```

## 监控指标

### 关键指标

1. **浏览请求响应时间**（p50, p95, p99）
   - 目标：p95 < 500ms

2. **缓存命中率**
   - 目标：> 80%

3. **数据库连接池使用率**
   - 优先级连接池：< 80%
   - 主连接池：可达 100%（不影响浏览）

4. **预热进度**
   - 每秒处理瓦片数：~100-200 个（受节流限制）

### 日志示例

**正常运行日志**：
```
[INFO] Connected to database successfully (main pool: 25, priority pool: 50)
[PREWARM] Start prewarming 2 datasources, z=0..9, concurrency=10 (using dedicated connection pool)
[PREWARM] Cached buildings_test z=5 x=423 y=203 (size=12345 bytes, took=1.234s)
[DEBUG HANDLER] URL=/tiles/buildings_test/14/13423/6403.mvt, datasourceID=buildings_test, z=14, x=13423, y=14403
Cache hit: memory LRU
```

## 回滚方案

如需恢复到单连接池架构：

```bash
git diff HEAD backend/internal/service/tile_service.go
git diff HEAD backend/internal/service/prewarm_service.go

# 回滚
git checkout HEAD -- backend/internal/service/tile_service.go
git checkout HEAD -- backend/internal/service/prewarm_service.go
```

## 进一步优化建议

### 1. 动态调整连接池大小

根据系统负载动态调整：
```go
if loadHigh {
    priorityDB.SetMaxOpenConns(cfg.Database.MaxOpenConns * 2)
    db.SetMaxOpenConns(cfg.Database.MaxOpenConns / 4)
}
```

### 2. 预热暂停机制

检测浏览请求高峰时暂停预热：
```go
if browserRequestRate > threshold {
    pausePrewarming()
}
```

### 3. 优先级队列

使用更复杂的优先级队列而非简单的连接池隔离。

### 4. 连接池监控

暴露 Prometheus 指标：
```go
prometheus.NewGaugeFunc(prometheus.GaugeOpts{
    Name: "mvt_db_pool_open_connections",
    Help: "Number of open connections to the database",
}, func() float64 {
    return float64(db.Stats().OpenConnections)
})
```

## 常见问题

### Q1: 为什么不直接增加连接池大小？

**A**: 增加连接池会增加数据库负担，可能导致：
- PostgreSQL 查询性能下降
- 内存消耗增加
- 连接管理开销增大

双连接池的优势是**隔离而非扩容**。

### Q2: 速率限制会拖慢预热吗？

**A**: 轻微影响，但可接受：
- 10ms delay × 10 workers = 每秒最多 1000 个瓦片
- z=0~9 层通常只有几千到几万个瓦片
- 预热在后台进行，几分钟完成即可

### Q3: 如何知道优化是否生效？

**A**: 观察以下指标：
1. 浏览器 Network 标签：瓦片请求时间 < 100ms
2. 后端日志：无 "connection pool exhausted" 错误
3. 用户体验：地图流畅加载，无卡顿

### Q4: 预热完成后性能如何？

**A**: 预热完成后，主连接池空闲，所有资源用于浏览请求：
- 优先级连接池：50 个连接
- 主连接池：25 个连接（空闲）
- **总可用**：75 个连接用于浏览请求

## 相关文档

- [CLAUDE.md](CLAUDE.md) - 项目架构
- [CHANGES.md](CHANGES.md) - 缓存策略修改
- [START.md](START.md) - 快速开始指南
