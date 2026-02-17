# ADDP 数据引擎扩展指南

本文档提供 ADDP 平台数据引擎扩展的完整指南，包括三层插件系统架构、实施步骤和参考实现。

---

## 📋 快速概览

添加新数据引擎支持需要以下步骤：

1. **创建引擎插件**（在 [common/engine/plugins/](../common/engine/plugins/)，100-400 行代码）
2. **注册插件**（在 [common/dbbridge/bridge.go](../common/dbbridge/bridge.go)，1 行导入）
3. **测试验证**（构建测试和功能验证）

**总工作量：2-6 小时**

---

## 🏗️ 架构概览

ADDP 数据引擎基于三层插件化架构：

```
┌──────────────────────────────────────────────────────────┐
│   应用层 (System/Manager/Meta/Transfer/Develop)         │
│   - 通过 common/dbbridge 或直接调用 plugin 接口         │
└────────────┬──────────────────────────────────────────────┘
             │
┌────────────▼──────────────────────────────────────────────┐
│   DBBridge Facade (common/dbbridge/)                      │
│   - 引擎连接测试、元数据查询入口                          │
│   - 自动导入所有引擎插件                                  │
└────────────┬──────────────────────────────────────────────┘
             │
┌────────────▼──────────────────────────────────────────────┐
│   三层接口架构 (common/engine/plugin/)                    │
│                                                            │
│   第一层：EnginePlugin（所有引擎的基础接口）               │
│   ├─ Type(), DisplayName(), EngineCategory()             │
│   ├─ TestConnection(), BuildConnectionString()           │
│   └─ DefaultPort(), RequiredFields(), SensitiveFields()  │
│                                                            │
│   第二层：按功能分类的标记接口                             │
│   ├─ StoragePlugin（存储引擎）                            │
│   │   └─ SupportsMetadataQuery()                         │
│   ├─ ComputePlugin（计算引擎）                            │
│   │   └─ GetSupportedOperators(), HealthCheckEndpoint()  │
│   └─ NoSQLPlugin（NoSQL 引擎）⭐新增                      │
│       ├─ ListDatabases(), ListCollections()             │
│       ├─ GetCollectionStats(), IsSystemDatabase()       │
│       └─ CreateClient(), CloseClient()                  │
│                                                            │
│   第三层：按存储类型细分的功能接口                         │
│   ├─ RelationalDBPlugin（关系型数据库）                   │
│   │   ├─ ListSchemas(), ListTables(), ListColumns()      │
│   │   ├─ GetTableRowCount(), IsSystemSchema()            │
│   │   └─ ConnectionPoolPlugin（连接池管理）              │
│   └─ ObjectStoragePlugin（对象存储）                      │
│       ├─ ListBuckets(), ListObjects()                    │
│       ├─ GetObjectMetadata(), InferContentType()         │
│       └─ DefaultBucket(), SupportsSSL()                  │
└───────────────────────────────────────────────────────────┘
             │
┌────────────▼──────────────────────────────────────────────┐
│   具体引擎插件 (common/engine/plugins/*)                  │
│   - PostgreSQL、MySQL、Doris、ClickHouse（关系型）        │
│   - MongoDB（NoSQL）⭐                                     │
│   - MinIO、S3（对象存储）                                 │
│   - Python Workflow、Spark Workflow、Math Workflow（计算）│
└──────────────────────────────────────────────────────────┘
```

**关键设计理念**：
- **三层渐进**：第一层必须实现，第二三层按需选择
- **接口组合**：一个插件可以实现多个接口（如 PostgreSQL 同时实现 StoragePlugin 和 RelationalDBPlugin）
- **自动注册**：插件通过 `init()` 函数自动注册，零配置使用
- **解耦合**：Meta、Manager、Transfer 共享 common/engine，无需重复实现

---

## 🗂️ 当前支持的数据引擎

截至 **v0.0.20** (2025-12-30)，ADDP 平台支持 **12 种**数据引擎：

### 关系型数据库 (OLTP/OLAP)

| 数据库 | 类型 | 默认端口 | 接口实现 | 用途 |
|--------|------|----------|----------|------|
| **PostgreSQL** | OLTP | 5432 | RelationalDBPlugin + ConnectionPoolPlugin | 系统主数据库，支持 PostGIS 空间扩展 |
| **MySQL** | OLTP | 3306 | RelationalDBPlugin + ConnectionPoolPlugin | 通用关系型数据库 |
| **Apache Doris** | HTAP | 9030 | RelationalDBPlugin + ConnectionPoolPlugin | 实时分析数据库，支持 OLAP 查询 |
| **ClickHouse** | OLAP | 9000 | RelationalDBPlugin + ConnectionPoolPlugin | 高性能列式存储，适合大数据量分析 |

### NoSQL 数据库 ⭐

| 数据库 | 类型 | 默认端口 | 接口实现 | 用途 |
|--------|------|----------|----------|------|
| **MongoDB** | 文档型 | 27017 | NoSQLPlugin | 文档型数据库，灵活 Schema，支持采样推断 |

### 对象存储

| 存储 | 类型 | 默认端口 | 接口实现 | 用途 |
|------|------|----------|----------|------|
| **MinIO** | 对象存储 | 9000 | ObjectStoragePlugin | S3 兼容对象存储，存储文件和二进制数据 |
| **Amazon S3** | 对象存储 | 443 | ObjectStoragePlugin | AWS 云对象存储 |

### 计算引擎

| 引擎 | 类型 | 默认端口 | 接口实现 | 用途 |
|------|------|----------|----------|------|
| **Python Workflow** | 工作流引擎 | 8099 | ComputePlugin | 基于 GeoPandas 的空间工作流计算 |
| **Spark Workflow** | 工作流引擎 | 8098 | ComputePlugin | 分布式空间工作流计算 |
| **Spark SQL** | SQL 引擎 | 10000 | RelationalDBPlugin | 大数据分布式查询引擎（Thrift Server，兼容 JDBC/SQL） |
| **Math Workflow** | 计算引擎 | 8089 | ComputePlugin | 数学计算工作流 |
| **Jupyter** | Notebook 引擎 | 8097 | EnginePlugin | 交互式 Notebook 开发（Python/Shell） |

---

## 🚀 实施步骤

### 步骤 1：创建引擎插件 (Common 模块)

#### 1.1 选择接口组合

根据引擎类型，选择需要实现的接口：

**关系型数据库**（PostgreSQL、MySQL、Doris、ClickHouse）：
```
EnginePlugin（必须）
  + StoragePlugin（必须）
  + RelationalDBPlugin（必须）
  + ConnectionPoolPlugin（必须）
```

**NoSQL 数据库**（MongoDB）⭐：
```
EnginePlugin（必须）
  + StoragePlugin（必须）
  + NoSQLPlugin（必须）
```

**对象存储**（MinIO、S3）：
```
EnginePlugin（必须）
  + StoragePlugin（必须）
  + ObjectStoragePlugin（必须）
```

**计算引擎**（Python Workflow、Spark Workflow）：
```
EnginePlugin（必须）
  + ComputePlugin（必须）
```

#### 1.2 创建插件目录和文件

```bash
mkdir -p common/engine/plugins/<enginetype>/
cd common/engine/plugins/<enginetype>/
touch plugin.go
```

#### 1.3 实现插件接口

创建 `plugin.go`，实现选定的接口。

**核心接口清单**（详见 [common/engine/plugin/interfaces.go](../common/engine/plugin/interfaces.go)）：

**EnginePlugin 接口**（所有引擎必须实现）：
- `Type() string` - 引擎类型标识（如 "postgresql", "mongodb"）
- `DisplayName() string` - 显示名称（如 "PostgreSQL", "MongoDB"）
- `EngineCategory() string` - 引擎分类：`"standard"` 或 `"extension"`
- `DefaultPort() int` - 默认端口
- `RequiredFields() []string` - 必填字段列表（如 ["host", "user", "database"]）
- `SensitiveFields() []string` - 敏感字段列表（如 ["password"]，需加密）
- `GenerateCapabilities() string` - 能力声明（JSON 格式）
- `ValidateConnectionInfo(connInfo) error` - 验证连接信息
- `BuildConnectionString(connInfo) (string, error)` - 构建连接字符串
- `TestConnection(ctx, connInfo) error` - 测试连接

**RelationalDBPlugin 接口**（关系型数据库实现）：
- `ListSchemas(ctx, db) ([]SchemaInfo, error)` - 列出所有 Schema/Database
- `ListTables(ctx, db, schema) ([]TableInfo, error)` - 列出指定 Schema 下的所有表
- `ListColumns(ctx, db, schema, table) ([]ColumnInfo, error)` - 列出表的所有列
- `GetTableRowCount(ctx, db, schema, table) (int64, error)` - 获取表行数
- `IsSystemSchema(schemaName) bool` - 判断是否为系统 Schema

**NoSQLPlugin 接口**（NoSQL 数据库实现）⭐：
- `ListDatabases(ctx, connInfo) ([]DatabaseInfo, error)` - 列出所有 Database
- `ListCollections(ctx, connInfo, database) ([]CollectionInfo, error)` - 列出 Collection
- `GetCollectionStats(ctx, connInfo, database, collection) (*CollectionStats, error)` - 获取统计信息
- `IsSystemDatabase(databaseName) bool` - 判断是否为系统 Database
- `CreateClient(ctx, connInfo) (interface{}, error)` - 创建客户端
- `CloseClient(ctx, client) error` - 关闭客户端

**ObjectStoragePlugin 接口**（对象存储实现）：
- `ListBuckets(ctx, connInfo) ([]BucketInfo, error)` - 列出所有 Bucket
- `ListObjects(ctx, connInfo, bucket, prefix, recursive) ([]ObjectInfo, error)` - 列出对象
- `GetObjectMetadata(ctx, connInfo, bucket, key) (*ObjectInfo, error)` - 获取对象元数据
- `InferContentType(objectKey) string` - 推断 MIME 类型
- `DefaultBucket() string` - 默认 Bucket 名称
- `SupportsSSL() bool` - 是否支持 SSL

**ConnectionPoolPlugin 接口**（关系型数据库连接池）：
- `CreateConnectionPool(connInfo, poolConfig) (*gorm.DB, error)` - 创建 GORM 连接池
- `GetDialect() string` - 返回 GORM 方言名称

**ComputePlugin 接口**（计算引擎实现）：
- `GetSupportedOperators() []string` - 返回支持的算子列表
- `HealthCheckEndpoint() string` - 健康检查端点

#### 1.4 自动注册

在 `plugin.go` 中添加 `init()` 函数，自动注册插件：

```go
package <enginetype>

import (
    "github.com/addp/common/engine/plugin"
)

type <EngineType>Plugin struct{}

// init 函数在包被导入时自动注册插件
func init() {
    plugin.Register(&<EngineType>Plugin{})
}

// ... 实现接口方法 ...
```

#### 1.5 实现示例参考

**简单示例：MySQL**
- 文件：[common/engine/plugins/mysql/plugin.go](../common/engine/plugins/mysql/plugin.go)
- 实现接口：EnginePlugin + StoragePlugin + RelationalDBPlugin + ConnectionPoolPlugin
- 代码量：~290 行

**复杂示例：MongoDB** ⭐
- 文件：
  - [common/engine/plugins/mongodb/plugin.go](../common/engine/plugins/mongodb/plugin.go)（基础接口）
  - [common/engine/plugins/mongodb/nosql.go](../common/engine/plugins/mongodb/nosql.go)（NoSQLPlugin 实现）
- 实现接口：EnginePlugin + StoragePlugin + NoSQLPlugin
- 代码量：~380 行

**对象存储示例：MinIO**
- 文件：[common/engine/plugins/minio/plugin.go](../common/engine/plugins/minio/plugin.go)
- 实现接口：EnginePlugin + StoragePlugin + ObjectStoragePlugin
- 代码量：~350 行

**计算引擎示例：Python Workflow**
- 文件：[common/engine/plugins/python_workflow/plugin.go](../common/engine/plugins/python_workflow/plugin.go)
- 实现接口：EnginePlugin + ComputePlugin
- 代码量：~150 行

---

### 步骤 2：注册插件到 DBBridge

编辑 [common/dbbridge/bridge.go](../common/dbbridge/bridge.go)，添加导入语句：

```go
import (
    _ "github.com/addp/common/engine/plugins/postgresql"
    _ "github.com/addp/common/engine/plugins/mysql"
    _ "github.com/addp/common/engine/plugins/doris"
    _ "github.com/addp/common/engine/plugins/clickhouse"
    _ "github.com/addp/common/engine/plugins/mongodb"      // ⭐ MongoDB
    _ "github.com/addp/common/engine/plugins/minio"
    _ "github.com/addp/common/engine/plugins/s3"
    _ "github.com/addp/common/engine/plugins/<enginetype>" // 👈 添加这一行
)
```

导入后，插件的 `init()` 函数会自动执行，完成注册。

---

### 步骤 3：测试验证

#### 3.1 构建测试

```bash
# 构建 common 模块
cd common
go build ./...

# 运行插件注册测试
go test ./engine/plugin/... -v
```

#### 3.2 功能验证

**1. 连接测试**：
```bash
# 在 System 模块添加新引擎，测试连接
cd system/backend
bash ../../scripts/dev/restart.sh -system

# 通过 API 测试连接
curl -X POST http://localhost:8180/api/engines/<engine_id>/test \
  -H "Authorization: Bearer <token>"
```

**2. 元数据扫描**（自动支持）：
- **关系型数据库**：Meta 模块自动使用 `RelationalDBPlugin.ListSchemas/ListTables/ListColumns`
- **NoSQL 数据库**：Meta 模块自动使用 `NoSQLPlugin.ListDatabases/ListCollections` ⭐
- **对象存储**：Meta 模块自动使用 `ObjectStoragePlugin.ListBuckets/ListObjects`

**3. 数据预览**（自动支持）：
- **关系型数据库**：Manager 模块自动使用 `RelationalDBPlugin` 查询数据
- **NoSQL 数据库**：Manager 模块自动使用 `NoSQLPlugin` + `DocCollectionParser` ⭐
- **对象存储**：Manager 模块自动使用 `ObjectStoragePlugin` + `FileTableParser`

**4. 跨模块集成**：
- Transfer 模块：自动支持新引擎作为数据源/目标
- Develop 模块：根据 `capabilities` 自动支持 SQL/工作流/Notebook 开发
- Service 模块：自动支持数据服务发布

---

## 🛠️ 常用工具函数

ADDP 提供了丰富的工具函数简化插件开发（详见 [common/engine/plugin/helpers.go](../common/engine/plugin/helpers.go)）：

### 1. 连接信息提取

```go
host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
port := plugin.GetInt(connInfo, "port")
user := plugin.GetString(connInfo, "user")
password := plugin.GetString(connInfo, "password")
database := plugin.GetString(connInfo, "database")
```

### 2. 字段验证

```go
err := plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
```

### 3. DSN 构建

```go
// PostgreSQL 风格
dsn := plugin.PostgreSQLStyleDSN(user, password, host, port, database, "disable")

// MySQL 风格（MySQL、Doris、TiDB）
dsn := plugin.MySQLStyleDSN(user, password, host, port, database, map[string]string{
    "parseTime": "true",
    "timeout":   "10s",
})

// MongoDB 风格
dsn := plugin.MongoDBStyleDSN(user, password, host, port, database)

// ClickHouse 风格
dsn := plugin.ClickHouseStyleDSN(user, password, host, port, database, map[string]string{
    "dial_timeout": "10s",
})
```

### 4. 类型映射

ADDP 提供了统一的类型映射工具，将各数据库的原生类型映射为标准类型（详见 [common/format/type_mapper.go](../common/format/type_mapper.go)）。

---

## 📊 模块自动集成说明

添加新引擎插件后，各模块会**自动支持**新引擎，无需额外开发：

### System 模块
- **引擎注册**：通过 API 创建引擎实例
- **连接测试**：调用 `EnginePlugin.TestConnection()`
- **能力发现**：读取 `GenerateCapabilities()` JSON

### Meta 模块
- **元数据扫描**：
  - 关系型数据库：调用 `RelationalDBPlugin.ListSchemas/ListTables/ListColumns`
  - NoSQL 数据库：调用 `NoSQLPlugin.ListDatabases/ListCollections` ⭐
  - 对象存储：调用 `ObjectStoragePlugin.ListBuckets/ListObjects`
- **索引同步**：自动同步到 Meilisearch 全文搜索引擎

### Manager 模块
- **数据预览**：
  - 关系型数据库：使用 `RelationalDBPlugin` 查询数据
  - NoSQL 数据库：使用 `NoSQLPlugin` + `DocCollectionParser` 采样推断 ⭐
  - 对象存储：使用 `ObjectStoragePlugin` + `FileTableParser` 解析文件
- **MVT 瓦片**：自动支持 PostGIS 空间数据的矢量瓦片生成

### Transfer 模块
- **数据导入/导出**：自动支持新引擎作为源或目标
- **连接池管理**：复用 `ConnectionPoolPlugin` 创建的连接池

### Develop 模块
- **SQL 查询**：根据 `dev_modes: ["query"]` 自动支持 SQL 工作台
- **工作流**：根据 `dev_modes: ["workflow"]` 自动支持工作流编排
- **Notebook**：根据 `dev_modes: ["notebook"]` 自动支持 Notebook 开发

---

## 📚 参考实现

### 关系型数据库：PostgreSQL
- **文件**：[common/engine/plugins/postgresql/plugin.go](../common/engine/plugins/postgresql/plugin.go)
- **实现接口**：EnginePlugin + StoragePlugin + RelationalDBPlugin + ConnectionPoolPlugin
- **特点**：
  - 使用 GORM 连接池管理
  - 查询 `information_schema` 获取元数据
  - 支持 PostGIS 空间扩展检测
  - 过滤系统 Schema（`pg_catalog`, `information_schema`）

### NoSQL 数据库：MongoDB ⭐
- **文件**：
  - [common/engine/plugins/mongodb/plugin.go](../common/engine/plugins/mongodb/plugin.go)
  - [common/engine/plugins/mongodb/nosql.go](../common/engine/plugins/mongodb/nosql.go)
- **实现接口**：EnginePlugin + StoragePlugin + NoSQLPlugin
- **特点**：
  - 使用 `mongo-driver` 官方驱动
  - 采样文档推断 Schema（由 `DocCollectionParser` 完成）
  - 过滤系统数据库（`admin`, `local`, `config`）
  - 支持索引信息查询

### 对象存储：MinIO
- **文件**：[common/engine/plugins/minio/plugin.go](../common/engine/plugins/minio/plugin.go)
- **实现接口**：EnginePlugin + StoragePlugin + ObjectStoragePlugin
- **特点**：
  - S3 兼容协议
  - 自动 MIME 类型推断（支持空间数据格式：shapefile、geojson）
  - localhost 规范化处理
  - 支持 SSL 配置

### 计算引擎：Python Workflow
- **文件**：[common/engine/plugins/python_workflow/plugin.go](../common/engine/plugins/python_workflow/plugin.go)
- **实现接口**：EnginePlugin + ComputePlugin
- **特点**：
  - HTTP 健康检查
  - 返回支持的空间/非空间算子列表
  - JSON 格式连接信息

---

## 🎯 常见数据库类型实现参考

### TiDB

```
驱动：github.com/go-sql-driver/mysql（与 MySQL 相同）
GORM 驱动：gorm.io/driver/mysql
默认端口：4000
DSN 格式：与 MySQL 相同

实现建议：复用 MySQL 插件代码，仅修改默认端口和名称
```

### SQLite

```
驱动：github.com/mattn/go-sqlite3
GORM 驱动：gorm.io/driver/sqlite
默认端口：无（本地文件）
DSN 格式：文件路径

特殊处理：BuildConnectionString() 返回文件路径，RequiredFields() 包含 "file_path"
```

### Redis（NoSQL 缓存数据库）

```
驱动：github.com/go-redis/redis/v9
默认端口：6379
不支持 GORM，不实现 ConnectionPoolPlugin

实现建议：
- 实现 EnginePlugin + StoragePlugin + NoSQLPlugin
- ListDatabases() 返回 Redis 的 16 个默认数据库
- ListCollections() 使用 SCAN 命令列出 Key 前缀
- 注意：Redis 不支持标准 SQL，仅实现元数据扫描
```

---

## ⚠️ 注意事项

### 1. 安全性
- **敏感信息**：在 `SensitiveFields()` 中声明敏感字段（如 password），System 模块会自动加密存储
- **SQL 注入防护**：使用参数化查询，避免字符串拼接
- **日志脱敏**：不要在日志中打印密码等敏感信息

### 2. 错误处理
- **错误包装**：使用 `fmt.Errorf("...: %w", err)` 保留错误链
- **清晰消息**：提供用户友好的错误消息，帮助排查问题
- **超时设置**：连接和查询设置合理超时（推荐 10-30 秒）

### 3. 性能优化
- **统计信息优先**：获取行数时优先使用数据库统计信息（如 `pg_stat_user_tables`）
- **避免 SELECT \***：只查询需要的列
- **连接池配置**：合理设置 `MaxOpenConns`、`MaxIdleConns`、`ConnMaxLifetime`

### 4. 兼容性
- **字段名灵活性**：同时支持 `user` 和 `username`（通过 `GetString(connInfo, "user")` 或 `GetString(connInfo, "username")`）
- **空密码处理**：部分数据库（如 Doris）默认无密码，需处理空字符串
- **默认值**：提供合理的默认值（如数据库名、端口）

---

## 🆘 故障排查

### 问题 1：插件未注册

**症状**：`plugin.Get("<dbtype>")` 返回 nil

**解决方案**：
1. 确认 `init()` 函数调用了 `plugin.Register()`
2. 确认在 `common/dbbridge/bridge.go` 中添加了 `import _ "..."`
3. 重新编译：`cd common && go build ./...`

### 问题 2：连接测试失败

**症状**：`TestConnection` 返回超时或连接拒绝

**解决方案**：
1. 检查 DSN 格式是否正确（可打印到日志调试）
2. 确认数据库服务正在运行
3. 检查防火墙和网络配置
4. 增加连接超时时间

### 问题 3：元数据扫描失败

**症状**：Meta 模块扫描时报错 "plugin not found" 或 "method not implemented"

**解决方案**：
1. 确认插件实现了对应接口（`RelationalDBPlugin` 或 `NoSQLPlugin` 或 `ObjectStoragePlugin`）
2. 检查接口方法签名是否正确
3. 重启 Meta Worker：`bash scripts/dev/restart.sh -meta`

### 问题 4：预览不显示数据

**症状**：Manager 模块数据预览页面空白或报错

**解决方案**：
1. 确认插件实现了元数据查询接口
2. 检查是否正确返回了 Schema/Table/Column 信息
3. 查看浏览器控制台错误

---

## 🎓 学习资源

- **接口定义**：[common/engine/plugin/interfaces.go](../common/engine/plugin/interfaces.go)
- **工具函数**：[common/engine/plugin/helpers.go](../common/engine/plugin/helpers.go)
- **DSN 构建**：[common/engine/plugin/dsn_builder.go](../common/engine/plugin/dsn_builder.go)
- **完整示例**：[common/engine/plugins/postgresql/plugin.go](../common/engine/plugins/postgresql/plugin.go)
- **MongoDB 示例**：[common/engine/plugins/mongodb/](../common/engine/plugins/mongodb/) ⭐
- **类型映射**：[common/format/type_mapper.go](../common/format/type_mapper.go)

---

## 📧 需要帮助？

如果遇到问题，请：
1. 查看已有插件实现作为参考
2. 阅读 [docs/addp常见故障排查.md](addp常见故障排查.md)
3. 在项目仓库提交 Issue

---

**祝你成功添加新的数据引擎支持！** 🎉

**最后更新**：2025-12-30
**版本**：v0.0.20
**维护者**：ADDP 开发团队
