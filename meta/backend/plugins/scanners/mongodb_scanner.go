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
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDBScanner 执行 MongoDB 元数据扫描
// 使用原生 mongo-driver 进行元数据查询
type MongoDBScanner struct {
	resource *commonModels.Engine
	client   *mongo.Client
	// defaultDB 是连接字符串中指定的默认数据库
	defaultDB string
}

const (
	// 每个Collection采样的文档数量
	DefaultSampleSize = 100
	// 字段类型推断的阈值（出现频率超过此值才认为是主要类型）
	TypeInferenceThreshold = 0.8
)

// NewMongoDBScanner 根据资源信息创建 MongoDB 扫描器
func NewMongoDBScanner(resource *commonModels.Engine) (*MongoDBScanner, error) {
	// 从插件系统构建连接字符串
	pluginResource := &plugin.Resource{
		ID:             resource.ID,
		EngineType:   resource.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(resource.ConnectionInfo),
	}
	connStr, err := plugin.BuildConnectionString(pluginResource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 获取默认数据库名称
	defaultDB := ""
	if db, ok := resource.ConnectionInfo["database"].(string); ok {
		defaultDB = db
	}

	// 创建MongoDB客户端选项
	// 使用较短的超时时间（3秒）以快速检测离线状态，避免页面长时间等待
	clientOptions := options.Client().ApplyURI(connStr)
	clientOptions.SetConnectTimeout(3 * time.Second)
	clientOptions.SetServerSelectionTimeout(3 * time.Second)

	// 连接MongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// 测试连接
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(context.Background())
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	return &MongoDBScanner{
		resource:  resource,
		client:    client,
		defaultDB: defaultDB,
	}, nil
}

// ListSchemas 列出所有数据库
// 在MongoDB中，Schema对应数据库（Database）
func (s *MongoDBScanner) ListSchemas() ([]format.SchemaInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 列出所有数据库
	databases, err := s.client.ListDatabaseNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}

	var schemas []format.SchemaInfo
	for _, dbName := range databases {
		// 过滤系统数据库
		if dbName == "admin" || dbName == "local" || dbName == "config" {
			continue
		}

		// 获取数据库的Collection数量
		db := s.client.Database(dbName)
		collections, err := db.ListCollectionNames(ctx, bson.M{})
		if err != nil {
			// 忽略无权访问的数据库
			continue
		}

		schemas = append(schemas, format.SchemaInfo{
			Name:           dbName,
			TableCount:     len(collections),
			TotalSizeBytes: 0, // MongoDB不容易获取数据库总大小，设为0
		})
	}

	return schemas, nil
}

// ScanTables 扫描指定数据库的所有Collection
// 在MongoDB中，Table对应Collection
func (s *MongoDBScanner) ScanTables(schemaName string) ([]format.TableInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := s.client.Database(schemaName)

	// 列出所有Collection
	collections, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}

	var tables []format.TableInfo
	for _, collName := range collections {
		// 跳过系统Collection
		if strings.HasPrefix(collName, "system.") {
			continue
		}

		// 获取Collection统计信息
		var stats bson.M
		err := db.RunCommand(ctx, bson.D{{Key: "collStats", Value: collName}}).Decode(&stats)

		rowCount := int64(0)
		sizeBytes := int64(0)

		if err == nil {
			// 提取统计信息
			if count, ok := stats["count"].(int64); ok {
				rowCount = count
			} else if count, ok := stats["count"].(int32); ok {
				rowCount = int64(count)
			}

			if size, ok := stats["size"].(int64); ok {
				sizeBytes = size
			} else if size, ok := stats["size"].(int32); ok {
				sizeBytes = int64(size)
			}
		}

		tables = append(tables, format.TableInfo{
			Name:      collName,
			Type:      "COLLECTION", // MongoDB使用Collection而不是Table
			Comment:   "",           // MongoDB Collection级别一般没有注释
			RowCount:  rowCount,
			SizeBytes: sizeBytes,
		})
	}

	return tables, nil
}

// ScanFields 扫描指定Collection的字段
// MongoDB是无Schema的，需要通过采样文档推断字段结构
func (s *MongoDBScanner) ScanFields(schemaName, tableName string) ([]format.FieldInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	collection := s.client.Database(schemaName).Collection(tableName)

	// 采样文档
	cursor, err := collection.Find(ctx, bson.M{}, options.Find().SetLimit(DefaultSampleSize))
	if err != nil {
		return nil, fmt.Errorf("failed to sample documents: %w", err)
	}
	defer cursor.Close(ctx)

	// 统计字段及其类型
	fieldStats := make(map[string]*fieldTypeStats)
	totalDocs := 0

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		totalDocs++

		// 分析文档中的每个字段
		s.analyzeDocument(doc, "", fieldStats)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	// 将统计结果转换为FieldInfo列表
	var fields []format.FieldInfo
	position := 1
	for fieldName, stats := range fieldStats {
		// 推断主要类型
		mainType := stats.inferMainType()

		// 判断是否可为空（不是所有文档都有此字段）
		isNullable := stats.count < totalDocs

		// 检查是否是_id字段（MongoDB的主键）
		isPrimaryKey := fieldName == "_id"

		fields = append(fields, format.FieldInfo{
			Name:            fieldName,
			OrdinalPosition: position,
			DataType:        mainType,
			ColumnType:      mainType,
			IsNullable:      isNullable,
			DefaultValue:    "",
			Comment:         fmt.Sprintf("出现在 %d/%d 文档中", stats.count, totalDocs),
			IsPrimaryKey:    isPrimaryKey,
			IsUniqueKey:     isPrimaryKey,
			CharacterSet:    "",
			Collation:       "",
		})
		position++
	}

	return fields, nil
}

// Close 关闭MongoDB连接
func (s *MongoDBScanner) Close() error {
	if s.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.client.Disconnect(ctx)
	}
	return nil
}

// === 辅助类型和方法 ===

// fieldTypeStats 字段类型统计
type fieldTypeStats struct {
	count int                // 出现次数
	types map[string]int     // 各类型出现次数
}

// inferMainType 推断主要类型
func (s *fieldTypeStats) inferMainType() string {
	if len(s.types) == 0 {
		return "unknown"
	}

	// 找出现次数最多的类型
	maxCount := 0
	mainType := "unknown"
	for typeName, count := range s.types {
		if count > maxCount {
			maxCount = count
			mainType = typeName
		}
	}

	// 如果类型一致性低于阈值，标记为mixed
	if float64(maxCount) / float64(s.count) < TypeInferenceThreshold {
		// 列出所有类型
		var types []string
		for typeName := range s.types {
			types = append(types, typeName)
		}
		return "mixed(" + strings.Join(types, "|") + ")"
	}

	return mainType
}

// analyzeDocument 递归分析文档字段
func (s *MongoDBScanner) analyzeDocument(doc bson.M, prefix string, stats map[string]*fieldTypeStats) {
	for key, value := range doc {
		fieldName := key
		if prefix != "" {
			fieldName = prefix + "." + key
		}

		// 初始化统计
		if stats[fieldName] == nil {
			stats[fieldName] = &fieldTypeStats{
				types: make(map[string]int),
			}
		}

		stats[fieldName].count++

		// 推断类型
		typeName := inferBSONType(value)
		stats[fieldName].types[typeName]++

		// 递归处理嵌套文档（最多2层，避免过深）
		if prefix == "" && typeName == "object" {
			if nestedDoc, ok := value.(bson.M); ok {
				s.analyzeDocument(nestedDoc, fieldName, stats)
			} else if nestedDoc, ok := value.(map[string]interface{}); ok {
				s.analyzeDocument(nestedDoc, fieldName, stats)
			}
		}
	}
}

// inferBSONType 推断BSON值的类型
func inferBSONType(value interface{}) string {
	if value == nil {
		return "null"
	}

	switch v := value.(type) {
	case string:
		return "string"
	case int, int32, int64:
		return "integer"
	case float32, float64:
		return "double"
	case bool:
		return "boolean"
	case time.Time:
		return "datetime"
	case primitive.DateTime:
		return "datetime"
	case primitive.ObjectID:
		return "objectid"
	case primitive.Binary:
		return "binary"
	case primitive.Decimal128:
		return "decimal"
	case []interface{}:
		// 数组：尝试推断元素类型
		if len(v) > 0 {
			elemType := inferBSONType(v[0])
			return "array<" + elemType + ">"
		}
		return "array"
	case bson.M, map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}
