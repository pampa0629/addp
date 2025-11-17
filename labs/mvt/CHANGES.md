# 缓存策略修改说明

## 修改时间
2025-01-17

## 修改内容

### 1. 预热阶段缓存策略
**修改前**：预热瓦片根据生成耗时（≥3s）或大小（≥100KB）决定是否持久化到 PG
**修改后**：预热阶段所有瓦片**强制持久化到 PG**，无大小或时间限制

**影响的文件**：
- `backend/internal/service/prewarm_service.go`
  - 移除了 `cfg.CachePolicy` 的阈值判断
  - `SetTileWithOptions` 的 `PersistToPG` 参数固定为 `true`
  - 移除了生成超时 `GenerateTimeout`（使用 `context.Background()`，无超时）
  - 移除了缓存超时 `CacheTimeout`（使用 `context.Background()`，无超时）
  - 添加详细的日志输出，包括成功和失败的情况

### 2. 配置结构调整
**修改前**：`PrewarmConfig` 包含 `GenerateTimeout` 和 `CacheTimeout` 字段
**修改后**：移除这两个超时字段，简化配置

**影响的文件**：
- `backend/internal/config/config.go`
  - 移除 `PrewarmConfig.GenerateTimeout`
  - 移除 `PrewarmConfig.CacheTimeout`
  - 移除相关环境变量支持 (`PREWARM_GENERATE_TIMEOUT`, `PREWARM_CACHE_TIMEOUT`)
  - 更新默认配置

- `backend/config/app.yaml`
  - 移除 `prewarm.generate_timeout`
  - 移除 `prewarm.cache_timeout`
  - 添加注释说明预热阶段强制持久化

- `backend/cmd/server/main.go`
  - 移除启动日志中的超时信息

### 3. 浏览请求缓存策略（保持不变）
**行为**：浏览生成的 MVT 仍然根据配置的阈值决定是否持久化
**条件**：生成耗时 ≥ `persist_min_duration` **或** 大小 ≥ `persist_min_raw_kb`（满足任意一个）

**相关代码**：`backend/internal/api/handler.go:78-97`（未修改）

## 配置示例

### 修改后的 app.yaml 配置

```yaml
# 缓存策略：控制浏览请求何时将瓦片持久化写入 Postgres
# 注意：预热阶段的瓦片始终持久化，不受此配置影响
cache_policy:
  persist_min_duration: 3s    # 仅影响浏览请求
  persist_min_raw_kb: 100     # 仅影响浏览请求

# 预热：启动后自动生成低缩放层级的瓦片并缓存到 PG
# 预热阶段：所有生成的瓦片强制持久化，无大小或时间限制
prewarm:
  enabled: true
  max_zoom: 8          # 预热 z=0~8 层
  concurrency: 10      # 并发数
```

## 行为对比

| 场景 | 修改前 | 修改后 |
|------|--------|--------|
| **预热：小瓦片（<100KB，<3s）** | 可能不持久化到 PG | **强制持久化到 PG** |
| **预热：大瓦片（≥100KB 或 ≥3s）** | 持久化到 PG | **强制持久化到 PG** |
| **预热：生成超时** | 200s 后取消 | **无超时限制** |
| **预热：缓存写入超时** | 20s 后取消 | **无超时限制** |
| **浏览：小瓦片（<100KB，<3s）** | 仅写入内存+Redis | **仅写入内存+Redis**（不变） |
| **浏览：大瓦片（≥100KB 或 ≥3s）** | 持久化到 PG | **持久化到 PG**（不变） |

## 日志示例

### 预热成功日志
```
[PREWARM] Start prewarming 2 datasources, z=0..8, concurrency=10
[PREWARM] Cached buildings_test z=5 x=423 y=203 (size=12345 bytes, took=1.234s)
[PREWARM] Cached roads_test z=6 x=846 y=406 (size=45678 bytes, took=5.678s)
[PREWARM] Done
```

### 预热失败日志
```
[PREWARM] Generate failed for buildings_test z=7 x=1692 y=812: query timeout
[PREWARM] Gzip failed for roads_test z=6 x=846 y=407: unexpected EOF
[PREWARM] Cache failed for buildings_test z=5 x=424 y=204: connection refused
```

## 测试验证

### 编译验证
```bash
cd backend
go build -o /tmp/mvt-test ./cmd/server/main.go
# 编译成功，无语法错误
```

### 运行验证
```bash
# 启动服务
make dev-backend

# 观察预热日志
# 应该看到所有瓦片都输出 "[PREWARM] Cached ..." 日志

# 检查 PG cache 表
psql -h localhost -p 5433 -U business -d business -c \
  "SELECT datasource, z, COUNT(*) FROM mvt_cache GROUP BY datasource, z ORDER BY z;"
```

### 预期结果
- 预热阶段生成的所有瓦片（z=0~max_zoom）都存在于 `mvt_cache` 表
- 包括小瓦片（<100KB 且生成时间 <3s）也会被持久化
- 浏览请求生成的瓦片仍然按照原有策略（大小或时间阈值）决定是否持久化

## 性能影响

### 优势
- ✅ 预热完成后，低层级瓦片完全缓存，响应极快
- ✅ 无需重复预热，服务重启后 PG cache 仍然有效
- ✅ 避免了因阈值判断导致的小瓦片缺失

### 注意事项
- ⚠️ 预热阶段会对 PostgreSQL 产生较大写入压力
- ⚠️ `mvt_cache` 表体积会增大（建议监控表大小）
- ⚠️ 预热时间可能延长（所有瓦片都要写 PG）
- ⚠️ 建议根据服务器性能调整 `concurrency` 参数（默认 10）

## 回滚方案

如需恢复到原来的行为，执行以下操作：

```bash
git diff HEAD backend/internal/service/prewarm_service.go
git diff HEAD backend/internal/config/config.go
git diff HEAD backend/config/app.yaml

# 回滚
git checkout HEAD -- backend/internal/service/prewarm_service.go
git checkout HEAD -- backend/internal/config/config.go
git checkout HEAD -- backend/config/app.yaml
git checkout HEAD -- backend/cmd/server/main.go
```

## 相关文档
- [CLAUDE.md](CLAUDE.md) - 项目架构说明
- [START.md](START.md) - 快速开始指南
