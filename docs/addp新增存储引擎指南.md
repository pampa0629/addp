# ADDP 新增存储引擎指南

本文档详细说明如何在 ADDP 平台中添加新的数据库/存储引擎类型支持。

## 📋 概述

ADDP 采用插件化架构支持多种数据库。添加新数据库类型需要在 3 个层面实现：

1. **数据库插件层** (Common) - 连接管理、DSN 构建
2. **元数据扫描层** (Meta) - Schema/表/字段信息提取
3. **数据预览层** (Manager) - 数据查询和显示

## 🎯 实现步骤

### 步骤 1: 创建数据库插件 (Common 模块)

**位置**: `common/database/plugins/<dbtype>/plugin.go`

**必须实现的接口**:
- `DatabasePlugin` - 基础插件接口（必需）
- `ConnectionPoolPlugin` - 连接池接口（SQL 数据库推荐）
- `MetadataPlugin` - 元数据查询接口（支持 GORM 的数据库推荐）

#### 示例：MongoDB 插件

```go
package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/database/plugin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type MongoDBPlugin struct{}

// init 函数在包被导入时自动注册插件
func init() {
	plugin.Register(&MongoDBPlugin{})
}

// Type 返回数据库类型标识（小写，与 resource_type 一致）
func (p *MongoDBPlugin) Type() string {
	return "mongodb"
}

// DisplayName 返回显示名称
func (p *MongoDBPlugin) DisplayName() string {
	return "MongoDB"
}

// ConnectionCategory 返回连接类别
func (p *MongoDBPlugin) ConnectionCategory() string {
	return "nosql_db" // 分类: sql_db, nosql_db, object_storage, data_warehouse
}

// DefaultPort 返回默认端口
func (p *MongoDBPlugin) DefaultPort() int {
	return 27017
}

// RequiredFields 返回必填字段列表
func (p *MongoDBPlugin) RequiredFields() []string {
	return []string{"host", "database"}
}

// SensitiveFields 返回敏感字段列表（需要加密存储）
func (p *MongoDBPlugin) SensitiveFields() []string {
	return []string{"password"}
}

// BuildConnectionString 构建连接字符串
func (p *MongoDBPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	host := connInfo.GetString("host")
	port := connInfo.GetInt("port", p.DefaultPort())
	database := connInfo.GetString("database")
	username := connInfo.GetString("username")
	password := connInfo.GetString("password")

	if host == "" || database == "" {
		return "", fmt.Errorf("host and database are required")
	}

	// 构建连接字符串
	if username != "" && password != "" {
		return fmt.Sprintf("mongodb://%s:%s@%s:%d/%s?authSource=admin",
			username, password, host, port, database), nil
	}
	return fmt.Sprintf("mongodb://%s:%d/%s", host, port, database), nil
}

// TestConnection 测试连接
func (p *MongoDBPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	connStr, err := p.BuildConnectionString(connInfo)
	if err != nil {
		return err
	}

	clientOptions := options.Client().ApplyURI(connStr)
	clientOptions.SetConnectTimeout(10 * time.Second)
	clientOptions.SetServerSelectionTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Disconnect(ctx)

	return client.Ping(ctx, readpref.Primary())
}

// ValidateConnectionInfo 验证连接信息
func (p *MongoDBPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	if connInfo.GetString("host") == "" {
		return fmt.Errorf("host is required")
	}
	if connInfo.GetString("database") == "" {
		return fmt.Errorf("database is required")
	}
	return nil
}

// GenerateCapabilities 生成能力描述
func (p *MongoDBPlugin) GenerateCapabilities() string {
	return "NoSQL document database, supports flexible schema"
}
```

#### 关键要点：

1. **类型标识一致性**：`Type()` 返回的字符串必须与 System 模块中 `engine_type` 字段完全一致（小写）
2. **自动注册**：必须在 `init()` 函数中调用 `plugin.Register()`
3. **连接字符串安全**：处理 localhost 别名、端口格式化等边界情况
4. **错误处理**：提供清晰的错误信息，帮助用户诊断问题

---

### 步骤 2: 创建元数据扫描器 (Meta 模块)

**位置**: `meta/backend/plugins/scanners/<dbtype>_scanner.go`

**必须实现的接口**: `format.Scanner`

#### 两种实现策略：

##### 策略 A: 委托给 MetadataPlugin（推荐用于 SQL 数据库）

适用于支持 GORM 的数据库（PostgreSQL, MySQL, ClickHouse, Doris 等）。

```go
package scanners

import (
	"context"
	"fmt"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
)

type ClickHouseScanner struct {
	engine *commonModels.Engine
	db     interface{}
}

func NewClickHouseScanner(engine *commonModels.Engine) (*ClickHouseScanner, error) {
	db, err := dbbridge.GetOrCreatePool(engine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get connection pool: %w", err)
	}
	return &ClickHouseScanner{engine: engine, db: db}, nil
}

func (s *ClickHouseScanner) ScanSchemas() ([]format.SchemaInfo, error) {
	return dbbridge.ScanSchemas(s.db, s.engine)
}

func (s *ClickHouseScanner) ScanTables(schemaName string) ([]format.TableInfo, error) {
	return dbbridge.ScanTables(s.db, s.engine, schemaName)
}

func (s *ClickHouseScanner) ScanFields(schemaName, tableName string) ([]format.FieldInfo, error) {
	return dbbridge.ScanFields(s.db, s.engine, schemaName, tableName)
}
```

##### 策略 B: 原生驱动实现（用于 NoSQL 数据库）

适用于不支持 GORM 的数据库（MongoDB, Redis 等），需要使用原生驱动。

```go
package scanners

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/database/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const DefaultSampleSize = 100 // 采样文档数量

type MongoDBScanner struct {
	engine *commonModels.Engine
	client *mongo.Client
}

func NewMongoDBScanner(engine *commonModels.Engine) (*MongoDBScanner, error) {
	// 使用插件系统构建连接字符串
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:     engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}
	connStr, err := plugin.BuildConnectionString(pluginEngine)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 连接 MongoDB
	clientOptions := options.Client().ApplyURI(connStr)
	clientOptions.SetConnectTimeout(10 * time.Second)

	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	return &MongoDBScanner{engine: engine, client: client}, nil
}

func (s *MongoDBScanner) ScanSchemas() ([]format.SchemaInfo, error) {
	ctx := context.Background()

	// MongoDB 的 database 对应 ADDP 的 schema
	databases, err := s.client.ListDatabaseNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}

	schemas := make([]format.SchemaInfo, 0, len(databases))
	for _, dbName := range databases {
		// 跳过系统数据库
		if dbName == "admin" || dbName == "config" || dbName == "local" {
			continue
		}
		schemas = append(schemas, format.SchemaInfo{
			SchemaName: dbName,
			SchemaType: "database",
		})
	}

	return schemas, nil
}

func (s *MongoDBScanner) ScanTables(schemaName string) ([]format.TableInfo, error) {
	ctx := context.Background()
	db := s.client.Database(schemaName)

	// 列出所有集合
	collections, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}

	tables := make([]format.TableInfo, 0, len(collections))
	for _, collName := range collections {
		// 跳过系统集合
		if strings.HasPrefix(collName, "system.") {
			continue
		}

		// 获取集合统计信息
		collection := db.Collection(collName)
		count, _ := collection.CountDocuments(ctx, bson.M{})

		tables = append(tables, format.TableInfo{
			TableName: collName,
			TableType: "collection",
			RowCount:  int(count),
		})
	}

	return tables, nil
}

func (s *MongoDBScanner) ScanFields(schemaName, tableName string) ([]format.FieldInfo, error) {
	ctx := context.Background()
	db := s.client.Database(schemaName)
	collection := db.Collection(tableName)

	// 采样文档推断 schema
	cursor, err := collection.Find(ctx, bson.M{}, options.Find().SetLimit(DefaultSampleSize))
	if err != nil {
		return nil, fmt.Errorf("failed to sample documents: %w", err)
	}
	defer cursor.Close(ctx)

	// 收集所有字段及其类型频率
	fieldTypes := make(map[string]map[string]int)
	docCount := 0

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		docCount++

		// 分析文档中的每个字段
		for key, value := range doc {
			if fieldTypes[key] == nil {
				fieldTypes[key] = make(map[string]int)
			}
			bsonType := inferBSONType(value)
			fieldTypes[key][bsonType]++
		}
	}

	if docCount == 0 {
		return []format.FieldInfo{}, nil
	}

	// 生成字段列表（类型取出现频率最高的）
	fields := make([]format.FieldInfo, 0, len(fieldTypes))
	for fieldName, types := range fieldTypes {
		// 找出最常见的类型
		mostCommonType := ""
		maxCount := 0
		for typeName, count := range types {
			if count > maxCount {
				mostCommonType = typeName
				maxCount = count
			}
		}

		// 计算字段出现率
		frequency := float64(maxCount) / float64(docCount)

		fields = append(fields, format.FieldInfo{
			FieldName: fieldName,
			FieldType: mostCommonType,
			Comment:   fmt.Sprintf("Appears in %.0f%% of documents", frequency*100),
		})
	}

	return fields, nil
}

// inferBSONType 推断 BSON 值的类型
func inferBSONType(value interface{}) string {
	switch value.(type) {
	case string:
		return "string"
	case int, int32, int64:
		return "int"
	case float32, float64:
		return "double"
	case bool:
		return "bool"
	case bson.M, map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case primitive.ObjectID:
		return "objectId"
	case primitive.DateTime, time.Time:
		return "date"
	case primitive.Binary:
		return "binary"
	default:
		return "unknown"
	}
}
```

#### 注册 Scanner

**位置**: `meta/backend/plugins/scanners/factory.go`

在 `CreateScanner` 函数的 switch 语句中添加新的 case：

```go
func CreateScanner(engine *commonModels.Engine) (format.Scanner, error) {
	dbType := strings.ToLower(engine.EngineType)

	switch dbType {
	case "postgresql", "postgres":
		return NewPostgresScanner(engine)
	case "mysql":
		return NewMySQLScanner(engine)
	case "doris":
		return NewDorisScanner(engine)
	case "clickhouse":  // ← 添加 ClickHouse
		return NewClickHouseScanner(engine)
	case "mongodb":     // ← 添加 MongoDB
		return NewMongoDBScanner(engine)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}
```

---

### 步骤 3: 创建数据预览提供者 (Manager 模块)

**位置**: `manager/backend/internal/service/preview_provider_<dbtype>.go`

**必须实现的接口**: `PreviewProvider`

#### 示例：MongoDB Preview Provider

```go
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/database/plugin"
	"github.com/addp/manager/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongodbPreviewProvider struct {
	priority int
}

func NewMongoDBPreviewProvider() PreviewProvider {
	return &mongodbPreviewProvider{
		priority: 100,
	}
}

func (p *mongodbPreviewProvider) Name() string {
	return "builtin:mongodb-table"
}

func (p *mongodbPreviewProvider) Priority() int {
	return p.priority
}

func (p *mongodbPreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Engine == nil {
		return false
	}
	if req.Schema == "" || req.Table == "" {
		return false
	}

	engineType := sanitizeEngineType(req.Engine.EngineType)
	return engineType == "mongodb"
}

func (p *mongodbPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	// 1. 使用插件系统构建连接字符串
	pluginEngine := &plugin.Engine{
		ID:             req.Engine.ID,
		EngineType:     req.Engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(req.Engine.ConnectionInfo),
	}
	connStr, err := plugin.BuildConnectionString(pluginEngine)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 2. 连接数据库
	clientOptions := options.Client().ApplyURI(connStr)
	clientOptions.SetConnectTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer client.Disconnect(ctx)

	// 3. 提取集合名（去掉 database 前缀）
	database := client.Database(req.Schema)
	collectionName := req.Table
	if strings.HasPrefix(req.Table, req.Schema+".") {
		collectionName = strings.TrimPrefix(req.Table, req.Schema+".")
	}
	collection := database.Collection(collectionName)

	// 4. 查询文档
	skip := (req.Page - 1) * req.PageSize
	limit := req.PageSize
	if limit > 50 {
		limit = 50 // 限制最大返回行数
	}

	totalCount, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to count documents: %w", err)
	}

	findOptions := options.Find().SetSkip(int64(skip)).SetLimit(int64(limit))
	cursor, err := collection.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to query collection: %w", err)
	}
	defer cursor.Close(ctx)

	var documents []bson.M
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, fmt.Errorf("failed to decode documents: %w", err)
	}

	// 5. 处理空结果
	if len(documents) == 0 {
		return &models.TablePreview{
			Mode:       PreviewModeTable,
			Columns:    []string{},
			Rows:       []map[string]interface{}{},
			Total:      0,
			Page:       req.Page,
			PageSize:   req.PageSize,
			EngineID:   req.Engine.ID,
			Schema:     req.Schema,
			Table:      req.Table,
			EngineType: req.Engine.EngineType,
		}, nil
	}

	// 6. 提取所有字段名
	columnsSet := make(map[string]bool)
	for _, doc := range documents {
		for key := range doc {
			columnsSet[key] = true
		}
	}

	// 7. 将字段名转换为有序列表（_id 放在最前面）
	columns := make([]string, 0, len(columnsSet))
	if columnsSet["_id"] {
		columns = append(columns, "_id")
		delete(columnsSet, "_id")
	}
	for col := range columnsSet {
		columns = append(columns, col)
	}

	// 8. 转换文档为行数据（map 格式）
	rows := make([]map[string]interface{}, 0, len(documents))
	for _, doc := range documents {
		row := make(map[string]interface{})
		for _, col := range columns {
			if val, ok := doc[col]; ok {
				row[col] = formatMongoDBValue(val)
			} else {
				row[col] = nil
			}
		}
		rows = append(rows, row)
	}

	return &models.TablePreview{
		Mode:       PreviewModeTable,
		Columns:    columns,
		Rows:       rows,
		Total:      int(totalCount),
		Page:       req.Page,
		PageSize:   req.PageSize,
		EngineID:   req.Engine.ID,
		Schema:     req.Schema,
		Table:      req.Table,
		EngineType: req.Engine.EngineType,
	}, nil
}

// formatMongoDBValue 格式化 MongoDB 值为 JSON 可序列化的格式
func formatMongoDBValue(val interface{}) interface{} {
	switch v := val.(type) {
	case primitive.ObjectID:
		return v.Hex()
	case primitive.DateTime:
		return v.Time().Format(time.RFC3339)
	case time.Time:
		return v.Format(time.RFC3339)
	case primitive.Binary:
		return fmt.Sprintf("<binary:%d bytes>", len(v.Data))
	case primitive.Decimal128:
		return v.String()
	case []interface{}:
		// 递归处理数组
		formatted := make([]interface{}, len(v))
		for i, item := range v {
			formatted[i] = formatMongoDBValue(item)
		}
		return formatted
	case bson.M:
		// 递归处理嵌套文档
		formatted := make(map[string]interface{})
		for key, value := range v {
			formatted[key] = formatMongoDBValue(value)
		}
		return formatted
	case map[string]interface{}:
		// 递归处理嵌套文档
		formatted := make(map[string]interface{})
		for key, value := range v {
			formatted[key] = formatMongoDBValue(value)
		}
		return formatted
	default:
		return v
	}
}
```

#### 注册 Preview Provider

**位置**: `manager/backend/internal/service/builtin/init.go`

```go
func init() {
	// ... 其他 provider ...

	// 4. MongoDB 表预览
	service.RegisterPreviewProvider("mongodb-table", func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ string, _ *service.ObjectContentRegistry) (service.PreviewProvider, error) {
		return service.NewMongoDBPreviewProvider(), nil
	})
}
```

---

### 步骤 4: 导入数据库插件 (Manager 模块)

**位置**: `manager/backend/cmd/server/main.go`

在 Manager 的 main.go 中添加空白导入，触发插件的 `init()` 函数：

```go
import (
	// ... 其他导入 ...

	// 导入数据库插件以触发自动注册
	_ "github.com/addp/common/database/plugins/clickhouse"
	_ "github.com/addp/common/database/plugins/doris"
	_ "github.com/addp/common/database/plugins/minio"
	_ "github.com/addp/common/database/plugins/mongodb"  // ← 添加 MongoDB
	_ "github.com/addp/common/database/plugins/mysql"
	_ "github.com/addp/common/database/plugins/postgresql"
	_ "github.com/addp/common/database/plugins/s3"
	_ "github.com/addp/common/database/plugins/spark_sql"
)
```

**为什么需要这一步？**

- MongoDB Preview Provider 使用 `plugin.BuildConnectionString()` 构建连接字符串
- 这个函数会查找全局插件注册表
- 如果没有导入插件包，`init()` 函数不会执行，插件不会注册
- 导致 `unsupported database type: mongodb (available types: )` 错误

---

## ⚠️ 常见错误和注意事项

### 1. 连接字符串构建错误

**错误症状**:
```
failed to build connection string: unsupported engine type: mongodb (available types: )
```

**原因**: Manager Backend 没有导入数据库插件，导致全局插件注册表为空。

**解决方案**:
- 在 `manager/backend/cmd/server/main.go` 中添加空白导入
- 确保导入路径正确
- 重新编译并重启 Manager 模块

---

### 2. 表名/集合名处理错误

**错误症状**:
- MongoDB 查询 `business.business.products` 而不是 `products`
- 查询失败但没有错误提示

**原因**: 前端传递的 `table` 参数格式是 `schema.table` 或 `database.collection`，直接使用会导致重复前缀。

**解决方案**:
```go
// 从 req.Table 中提取实际的集合名
collectionName := req.Table
if strings.HasPrefix(req.Table, req.Schema+".") {
	collectionName = strings.TrimPrefix(req.Table, req.Schema+".")
}
collection := database.Collection(collectionName)
```

---

### 3. 数据类型转换错误

**错误症状**:
- 前端显示 `[object Object]`
- JSON 序列化失败

**原因**: MongoDB 等 NoSQL 数据库有特殊类型（ObjectID, DateTime, Binary），不能直接 JSON 序列化。

**解决方案**:
- 实现 `formatMongoDBValue()` 函数，递归转换特殊类型
- ObjectID → Hex 字符串
- DateTime → RFC3339 时间字符串
- Binary → 描述性字符串（如 `<binary:1024 bytes>`）
- 嵌套文档和数组递归处理

---

### 4. 数据格式错误

**错误症状**:
```
cannot use [][]interface{} as []map[string]interface{} value in struct literal
```

**原因**: `TablePreview.Rows` 的类型是 `[]map[string]interface{}`，不是 `[][]interface{}`。

**解决方案**:
```go
// 正确的格式
rows := make([]map[string]interface{}, 0, len(documents))
for _, doc := range documents {
	row := make(map[string]interface{})
	for _, col := range columns {
		row[col] = formatValue(doc[col])
	}
	rows = append(rows, row)
}
```

---

### 5. Scanner 连接字符串错误

**错误症状**:
```
failed to create scanner: failed to build connection string: unsupported engine type: mongodb
```

**原因**: Scanner 使用了硬编码的 `commonModels.BuildConnectionString()`，而不是插件系统的 `plugin.BuildConnectionString()`。

**解决方案**:
```go
// 错误的做法 ❌
connStr, err := commonModels.BuildConnectionString(engine)

// 正确的做法 ✅
pluginEngine := &plugin.Engine{
	ID:             engine.ID,
	EngineType:     engine.EngineType,
	ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
}
connStr, err := plugin.BuildConnectionString(pluginEngine)
```

---

### 6. 类型转换错误

**错误症状**:
```
cannot use req.Engine (variable of type *manager/models.Engine) as *common/models.Engine
```

**原因**: Manager 和 Common 模块有不同的 Engine 类型定义。

**解决方案**:
```go
// 转换 Manager Engine 到 Plugin Engine
pluginEngine := &plugin.Engine{
	ID:             req.Engine.ID,
	EngineType:     req.Engine.EngineType,
	ConnectionInfo: plugin.ConnectionInfo(req.Engine.ConnectionInfo),
}
```

---

## 🧪 测试验证

### 1. 测试连接

在 System 模块中添加新的计算引擎，点击"测试连接"。

### 2. 测试元数据扫描

在 Meta 模块中触发全量扫描或增量扫描，检查：
- Schema/Database 列表
- Table/Collection 列表
- Field/Column 信息

### 3. 测试数据预览

在 Manager 模块的数据探查页面：
- 展开引擎树
- 点击表/集合
- 验证数据正确显示
- 测试分页功能

### 4. 查看日志

```bash
# 查看 Meta 扫描日志
tail -f logs/meta-backend.log | grep -i mongodb

# 查看 Manager 预览日志
tail -f logs/manager-backend.log | grep -i mongodb
```

---

## 📝 完整检查清单

添加新数据库类型时，确保完成以下所有步骤：

- [ ] **Common 模块**
  - [ ] 创建 `common/database/plugins/<dbtype>/plugin.go`
  - [ ] 实现 `DatabasePlugin` 接口
  - [ ] 在 `init()` 中注册插件
  - [ ] 实现 `BuildConnectionString()`
  - [ ] 实现 `TestConnection()`
  - [ ] 可选：实现 `ConnectionPoolPlugin`（SQL 数据库）
  - [ ] 可选：实现 `MetadataPlugin`（支持 GORM 的数据库）

- [ ] **Meta 模块**
  - [ ] 创建 `meta/backend/plugins/scanners/<dbtype>_scanner.go`
  - [ ] 实现 `format.Scanner` 接口
  - [ ] 在 `factory.go` 的 switch 中添加 case
  - [ ] 处理 Schema/Table/Field 信息提取
  - [ ] NoSQL：实现 schema 推断逻辑

- [ ] **Manager 模块**
  - [ ] 创建 `manager/backend/internal/service/preview_provider_<dbtype>.go`
  - [ ] 实现 `PreviewProvider` 接口
  - [ ] 在 `builtin/init.go` 中注册
  - [ ] 在 `cmd/server/main.go` 中添加插件导入
  - [ ] 处理表名前缀问题
  - [ ] 实现数据类型格式化

- [ ] **测试验证**
  - [ ] 测试连接功能
  - [ ] 测试元数据扫描
  - [ ] 测试数据预览
  - [ ] 测试分页功能
  - [ ] 验证错误处理

---

## 🔍 调试技巧

### 1. 添加调试日志

在关键位置添加 `fmt.Printf` 或 `logger.L().Info`：

```go
fmt.Printf("[MongoDB Preview] Querying database=%s, collection=%s (original table=%s)\n",
	req.Schema, collectionName, req.Table)

fmt.Printf("[MongoDB Preview] Found %d documents in collection %s.%s (total=%d)\n",
	len(documents), req.Schema, collectionName, totalCount)
```

### 2. 使用重启脚本验证

```bash
# 修改代码后，立即重启验证
bash scripts/dev/restart.sh -manager
bash scripts/dev/restart.sh -meta
```

### 3. 检查插件注册状态

在代码中添加：

```go
import "github.com/addp/common/database/plugin"

func init() {
	// 打印已注册的插件
	fmt.Printf("Registered plugins: %v\n", plugin.List())
}
```

---

## 📚 参考实现

可参考以下已实现的数据库类型：

- **PostgreSQL**: 标准 SQL 数据库，使用 GORM + MetadataPlugin
- **MySQL**: 标准 SQL 数据库，使用 GORM + MetadataPlugin
- **ClickHouse**: 列式数据库，委托给 MetadataPlugin
- **MongoDB**: NoSQL 文档数据库，原生驱动 + schema 推断
- **MinIO/S3**: 对象存储，特殊处理（不是数据库）

---

## 🎓 最佳实践

1. **保持一致性**: 类型标识在各模块中必须完全一致（小写）
2. **错误处理**: 提供清晰的错误信息，帮助用户诊断问题
3. **性能优化**:
   - 限制单次查询的最大行数（如 50 行）
   - 使用连接池复用数据库连接
   - NoSQL 采样时限制采样文档数量
4. **安全性**:
   - 标记敏感字段（如 password）需要加密
   - 避免 SQL 注入（使用参数化查询）
   - 处理 localhost 别名（避免 Unix socket 问题）
5. **可维护性**:
   - 添加充分的注释
   - 保持代码结构清晰
   - 遵循现有的代码风格

---

## 💡 常见问题

**Q: Meta 模块为什么不需要导入数据库插件？**

A: Meta 模块使用 Scanner 接口直接操作数据库，Scanner 内部会调用插件系统。但 Scanner 创建时可能需要使用 `plugin.BuildConnectionString()`，所以还是推荐导入插件。

**Q: 如何支持多个别名（如 postgresql 和 postgres）？**

A: 在 Scanner Factory 的 switch 中使用多个 case：
```go
case "postgresql", "postgres":
	return NewPostgresScanner(engine)
```

**Q: NoSQL 数据库如何推断 schema？**

A: 采样一定数量的文档（如 100 个），统计每个字段的类型频率，选择出现频率最高的类型作为字段类型。

**Q: 如何处理嵌套文档和数组？**

A: 递归调用格式化函数，将嵌套结构转换为 JSON 可序列化的格式。

---

## 📮 获取帮助

如果遇到问题：

1. 查看 `docs/addp常见故障排查.md`
2. 检查日志文件：`logs/<module>-backend.log`
3. 参考已有的数据库实现
4. 在团队中寻求帮助

---

**文档版本**: v1.0
**最后更新**: 2025-12-26
**适用版本**: ADDP v0.0.18+
