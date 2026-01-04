# Meta 模块插件系统重构总结

## 概述

**重构版本**: v0.0.20
**重构时间**: 2025-01-04
**重构目标**: 简化 Meta 模块的扫描架构，消除冗余的 Scanner 适配层，直接使用 `common/database/plugin` 三层插件架构

## 重构背景

### 重构前的问题

在 v0.0.19 及之前版本中，Meta 模块的扫描架构存在以下问题：

1. **调用链冗长**（5 层）：
   ```
   ScanService → plugins.NewScanner() → Scanner 适配器 → plugin.Get() → Plugin → 数据库
   ```

2. **代码重复**：
   - `meta/backend/plugins/scanners/` 中的 Scanner 实现（8 个文件，1200+ 行代码）
   - 与 `common/database/plugin` 中的插件实现功能重复
   - 每次新增存储引擎需要同时维护两套代码

3. **维护成本高**：
   - 接口变更需要同步修改 Scanner 适配层
   - 类型转换繁琐（plugin 类型 → Scanner 类型 → Meta 内部类型）

### 重构目标

- ✅ **简化调用链**: 从 5 层缩减为 2 层
- ✅ **消除代码重复**: 删除 Scanner 适配层，直接使用 common 插件
- ✅ **统一插件接口**: Meta、Manager、Transfer 等模块使用相同的插件接口
- ✅ **降低维护成本**: 新增存储引擎只需实现一次插件接口

## 重构内容

### 阶段1: 设计和验证插件接口

**目标**: 确认 `common/database/plugin` 的三层插件架构能满足 Meta 模块的扫描需求

**实现**:
1. 验证 `RelationalDBPlugin` 接口方法：
   - `ListSchemas(ctx, db) -> []SchemaInfo`
   - `ListTables(ctx, db, schema) -> []TableInfo`
   - `ListColumns(ctx, db, schema, table) -> []ColumnInfo`
   - `IsSystemSchema(schemaName) -> bool`

2. 验证 `ObjectStoragePlugin` 接口方法：
   - `ListBuckets(ctx, connInfo) -> []BucketInfo`
   - `ListObjects(ctx, connInfo, bucket, prefix, recursive) -> []ObjectInfo`
   - `GetObjectMetadata(ctx, connInfo, bucket, key) -> *ObjectInfo`

3. 创建集成测试脚本 `common/database/plugin/integration/test_plugins.sh`

**结果**:
- ✅ 所有插件接口验证通过
- ✅ 测试覆盖 PostgreSQL、MySQL、MinIO、S3 四种引擎

### 阶段2: 重构 DatabaseScanService

**文件修改**: `meta/backend/internal/service/scan_database_service.go`

**关键变更**:
1. 移除 Scanner 参数，直接使用 `plugin.RelationalDBPlugin`：
   ```go
   // 旧版本
   func (s *DatabaseScanService) ScanSchema(
       scan plugins.Scanner,
       resource *commonModels.Engine,
       ...
   )

   // 新版本
   func (s *DatabaseScanService) ScanSchema(
       ctx context.Context,
       resource *commonModels.Engine,
       ...
   ) {
       p, _ := plugin.Get(resource.EngineType)
       relPlugin := p.(plugin.RelationalDBPlugin)
       db, _ := plugin.GetOrCreatePoolFromFactory(...)

       tables, _ := relPlugin.ListTables(ctx, db, schemaName)
       columns, _ := relPlugin.ListColumns(ctx, db, schemaName, tableName)
   }
   ```

2. 类型转换优化：
   ```go
   // plugin.TableInfo → plugins.TableInfo
   tables := make([]plugins.TableInfo, len(pluginTables))
   for i, t := range pluginTables {
       tables[i] = plugins.TableInfo{
           Name:       t.Name,
           Type:       t.Type,
           RowCount:   t.RowCount,
           SizeBytes:  t.SizeBytes,
           Comment:    t.Comment,
       }
   }
   ```

3. 上下文传播：为所有扫描方法添加 `context.Context` 参数

**影响范围**:
- `ScanSchema()` 方法
- `scanTables()` 方法
- `scanTableDetails()` 方法
- `scanSpatialMetadata()` 方法

### 阶段3: 重构 ObjectStorageScanService

**文件修改**:
- `meta/backend/internal/service/scan_object_storage_service.go`
- `meta/backend/internal/service/scan_service.go`

**关键变更**:
1. 直接使用 `plugin.ObjectStoragePlugin`：
   ```go
   // 旧版本
   scan, _ := plugins.NewScanner(resource)
   objectScanner := scan.(plugins.ObjectStorageScanner)
   objects, _ := objectScanner.ScanPath(bucketPath)

   // 新版本
   p, _ := plugin.Get(resource.EngineType)
   objPlugin := p.(plugin.ObjectStoragePlugin)
   objects, _ := objPlugin.ListObjects(
       ctx,
       plugin.ConnectionInfo(resource.ConnectionInfo),
       bucket,
       prefix,
       recursive,
   )
   ```

2. 创建新方法 `scanObjectStoragePathsWithPlugin()`：
   - 替代旧的 Scanner 适配器方法
   - 直接调用 `objPlugin.ListObjects()`
   - 类型转换: `plugin.ObjectInfo` → `plugins.ObjectMetadata`

3. 创建辅助方法：
   - `convertToObjectMetadata()` - 对象元数据转换
   - `splitObjectPath()` - 路径解析（bucket/prefix）

**影响范围**:
- `ScanPaths()` 方法
- `scanObjectStoragePathsWithPlugin()` 新增方法
- `scanResource()` 对象存储扫描逻辑
- `scanObjectStorageResourceWithReporter()` 方法

### 阶段4: 删除冗余代码

**删除文件**（共 8 个文件，约 1200 行代码）:
```
meta/backend/plugins/scanners/
├── clickhouse_scanner.go
├── doris_scanner.go
├── factory.go
├── mongodb_scanner.go
├── mysql_scanner.go
├── postgres_scanner.go
├── register.go
└── s3_scanner.go
```

**清理代码**:
1. 更新 `meta/backend/plugins/types.go`：
   - 删除 `NewScanner()` 函数
   - 保留类型别名用于兼容（标记为"已废弃"）
   - 移除对 `meta/plugins/scanners` 包的引用

2. 更新 `resource_discovery_service.go`：
   - `ListAvailableSchemas()` - 使用 `RelationalDBPlugin`
   - `ListObjectStorageNodes()` - 使用 `ObjectStoragePlugin`

**代码统计**:
```
23 files changed, 1077 insertions(+), 2020 deletions(-)
```
- **删除代码**: 2020 行
- **新增代码**: 1077 行
- **净减少**: **943 行代码**

### 阶段5: 更新文档

**更新文档**:
1. `meta/CLAUDE.md` - Meta 模块主文档
   - 添加"插件系统架构（v0.0.20 重构）"章节
   - 更新"元数据扫描架构"流程图
   - 说明重构优势

2. `docs/meta模块插件系统重构总结.md` - 重构总结文档（本文档）
   - 记录重构背景、过程、结果
   - 提供迁移指南和最佳实践

### 阶段6: 测试和验证

**编译验证**:
```bash
✅ go build ./cmd/server  # 编译成功
✅ bash scripts/dev/restart.sh -meta  # 服务重启成功
✅ curl http://localhost:8082/health  # 健康检查通过
✅ tail logs/meta-backend.log  # 无错误日志
```

**功能测试**（见下节"测试验证"）

## 重构成果

### 架构优化

**调用链简化**:
```
重构前（5 层）:
ScanService → plugins.NewScanner() → Scanner 适配器 → plugin.Get() → Plugin → 数据库

重构后（2 层）:
ScanService → plugin.Get() → Plugin → 数据库
```

**代码结构**:
```
重构前:
meta/backend/
├── internal/service/
│   ├── scan_database_service.go  (使用 Scanner)
│   └── scan_object_storage_service.go  (使用 Scanner)
└── plugins/scanners/  ❌ 冗余层（1200+ 行）
    ├── factory.go
    ├── postgres_scanner.go
    ├── mysql_scanner.go
    ├── s3_scanner.go
    └── ...

重构后:
meta/backend/
├── internal/service/
│   ├── scan_database_service.go  ✅ 直接使用 plugin.RelationalDBPlugin
│   └── scan_object_storage_service.go  ✅ 直接使用 plugin.ObjectStoragePlugin
└── plugins/
    └── types.go  (仅保留类型别名，用于兼容)
```

### 代码质量

- ✅ **减少代码量**: 净减少 943 行代码
- ✅ **消除重复**: 删除与 common 插件重复的实现
- ✅ **统一接口**: 所有模块使用相同的插件接口
- ✅ **类型安全**: 通过接口类型断言保证安全性
- ✅ **上下文传播**: 所有扫描方法支持 `context.Context`

### 可维护性

- ✅ **新增引擎简单**: 只需在 `common/database/plugin` 实现接口
- ✅ **接口变更集中**: 修改插件接口后，所有模块自动生效
- ✅ **测试覆盖完整**: 集成测试脚本验证所有插件功能

## 测试验证

### 编译测试
```bash
cd meta/backend
go build ./cmd/server
# ✅ 编译成功，无错误
```

### 服务启动测试
```bash
bash scripts/dev/restart.sh -meta
# ✅ Meta Backend 启动成功 (PID: 20863)
# ✅ 健康检查通过: {"status":"healthy"}
```

### 功能测试

#### 1. 数据库扫描测试
```bash
# 测试 PostgreSQL 数据库扫描
TOKEN="your_jwt_token"
ENGINE_ID=1  # PostgreSQL 引擎 ID

curl -X POST "http://localhost:8082/api/engines/$ENGINE_ID/scan" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "schemas": ["public"],
    "scan_depth": "deep"
  }'

# 预期结果: 扫描成功，返回扫描统计
```

#### 2. 对象存储扫描测试
```bash
# 测试 MinIO 对象存储扫描
ENGINE_ID=2  # MinIO 引擎 ID

curl -X POST "http://localhost:8082/api/engines/$ENGINE_ID/scan" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "paths": ["test-bucket"],
    "scan_depth": "deep"
  }'

# 预期结果: 扫描成功，返回对象统计
```

#### 3. Schema 列表查询测试
```bash
# 测试实时查询可用 Schema
curl -X GET "http://localhost:8082/api/engines/$ENGINE_ID/schemas" \
  -H "Authorization: Bearer $TOKEN"

# 预期结果: 返回 Schema 列表
```

#### 4. 对象浏览测试
```bash
# 测试对象存储目录浏览
curl -X GET "http://localhost:8082/api/engines/$ENGINE_ID/nodes?path=test-bucket" \
  -H "Authorization: Bearer $TOKEN"

# 预期结果: 返回对象列表
```

### 日志验证
```bash
# 检查扫描日志，确保无错误
tail -f logs/meta-backend.log | grep -E "ERROR|WARN|扫描"
```

## 迁移指南

如果你的代码直接使用了 `meta/backend/plugins/scanners/` 包，需要进行以下迁移：

### 1. Scanner 创建

**旧代码**:
```go
import "github.com/addp/meta/plugins"

scan, err := plugins.NewScanner(resource)
if err != nil {
    return err
}
defer scan.Close()

// 使用 Scanner
schemas, err := scan.ListSchemas()
```

**新代码**:
```go
import "github.com/addp/common/database/plugin"

// 获取插件
p, err := plugin.Get(resource.EngineType)
if err != nil {
    return err
}

// 类型断言
relPlugin, ok := p.(plugin.RelationalDBPlugin)
if !ok {
    return fmt.Errorf("不支持的数据库类型")
}

// 获取连接池
db, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{
    ID:             resource.ID,
    EngineType:     resource.EngineType,
    ConnectionInfo: plugin.ConnectionInfo(resource.ConnectionInfo),
}, nil)
if err != nil {
    return err
}

// 使用插件
schemas, err := relPlugin.ListSchemas(context.Background(), db)
```

### 2. 对象存储扫描

**旧代码**:
```go
scan, _ := plugins.NewScanner(resource)
objectScanner := scan.(plugins.ObjectStorageScanner)
objects, _ := objectScanner.ScanPath("bucket/path")
```

**新代码**:
```go
p, _ := plugin.Get(resource.EngineType)
objPlugin := p.(plugin.ObjectStoragePlugin)
objects, _ := objPlugin.ListObjects(
    context.Background(),
    plugin.ConnectionInfo(resource.ConnectionInfo),
    "bucket",
    "path",
    true, // recursive
)
```

### 3. 类型转换

如果你需要使用 Meta 内部的类型（`plugins.TableInfo`、`plugins.ObjectMetadata`），需要手动转换：

```go
// plugin.TableInfo → plugins.TableInfo
metaTables := make([]plugins.TableInfo, len(pluginTables))
for i, t := range pluginTables {
    metaTables[i] = plugins.TableInfo{
        Name:      t.Name,
        Type:      t.Type,
        RowCount:  t.RowCount,
        SizeBytes: t.SizeBytes,
        Comment:   t.Comment,
    }
}
```

## 最佳实践

### 1. 使用上下文传播
所有扫描操作都应传递 `context.Context`，支持超时和取消：

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

tables, err := relPlugin.ListTables(ctx, db, schemaName)
```

### 2. 连接池复用
使用 `plugin.GetOrCreatePoolFromFactory()` 获取连接池，避免重复创建：

```go
// ✅ 推荐：使用连接池
db, err := plugin.GetOrCreatePoolFromFactory(engineConfig, nil)

// ❌ 避免：直接创建连接
db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
```

### 3. 系统 Schema 过滤
使用插件提供的 `IsSystemSchema()` 方法，而不是硬编码列表：

```go
// ✅ 推荐：使用插件方法
if relPlugin.IsSystemSchema(schemaName) {
    continue
}

// ❌ 避免：硬编码
systemSchemas := []string{"information_schema", "pg_catalog"}
```

### 4. 错误处理
始终检查类型断言的结果：

```go
// ✅ 推荐：安全的类型断言
relPlugin, ok := p.(plugin.RelationalDBPlugin)
if !ok {
    return fmt.Errorf("engine %s does not implement RelationalDBPlugin", engineType)
}

// ❌ 避免：不安全的类型断言
relPlugin := p.(plugin.RelationalDBPlugin) // 可能 panic
```

## 后续优化建议

1. **性能优化**:
   - 考虑并发扫描多个 Schema/Bucket
   - 实现增量扫描（仅扫描变更的数据）
   - 添加扫描结果缓存

2. **功能增强**:
   - 支持扫描进度实时推送（WebSocket）
   - 支持扫描中断和恢复
   - 支持自定义扫描规则（过滤、采样）

3. **监控告警**:
   - 添加扫描性能指标（Prometheus）
   - 扫描失败自动重试
   - 异常扫描结果告警

## 相关文档

- [meta/CLAUDE.md](../meta/CLAUDE.md) - Meta 模块主文档
- [docs/数据库插件系统.md](数据库插件系统.md) - 插件系统架构说明
- [docs/addp新增存储引擎指南.md](addp新增存储引擎指南.md) - 如何新增存储引擎
- [common/database/plugin/README.md](../common/database/plugin/README.md) - 插件接口文档

## 版本历史

- **v0.0.20** (2025-01-04) - Meta 模块插件系统重构完成
  - 简化调用链从 5 层到 2 层
  - 删除 1200+ 行冗余 Scanner 适配层代码
  - 统一使用 `common/database/plugin` 三层插件架构
  - 更新文档和测试
