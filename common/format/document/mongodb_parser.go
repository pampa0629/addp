package document

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	mongoOptions "go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDBCollectionParser MongoDB 集合解析器
type MongoDBCollectionParser struct{}

// init 自动注册解析器
func init() {
	format.RegisterDocCollectionParser(&MongoDBCollectionParser{})
}

// ParseTableInfo 实现 DocCollectionParser 接口 - 从 Collection 中提取 TableInfo
func (p *MongoDBCollectionParser) ParseTableInfo(
	ctx context.Context,
	client interface{},
	database, collection string,
	options *format.ParseOptions,
) (*format.TableInfo, error) {

	mongoClient, ok := client.(*mongo.Client)
	if !ok {
		return nil, fmt.Errorf("invalid client type: expected *mongo.Client")
	}

	coll := mongoClient.Database(database).Collection(collection)

	// 获取采样大小（默认 100）
	sampleSize := 100
	if options != nil && options.SampleSize > 0 {
		sampleSize = options.SampleSize
	}

	// 采样文档
	findOptions := mongoOptions.Find().SetLimit(int64(sampleSize))
	cursor, err := coll.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to find documents: %w", err)
	}
	defer cursor.Close(ctx)

	var documents []bson.M
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, fmt.Errorf("failed to decode documents: %w", err)
	}

	// 如果集合为空
	if len(documents) == 0 {
		totalDocs, _ := coll.EstimatedDocumentCount(ctx)
		return &format.TableInfo{
			Name:       collection,
			RowCount:   &totalDocs,
			Fields:     []format.FieldInfo{},
			PrimaryKey: []string{"_id"},
			Extensions: []format.ExtensionInfo{
				&format.DocCollectionInfo{
					IsSampled:      true,
					SampleSize:     0,
					SchemaType:     "dynamic",
					Indexes:        []plugin.IndexInfo{},
					TotalDocuments: totalDocs,
				},
			},
		}, nil
	}

	// 统计字段类型
	fieldStats := make(map[string]*fieldStat)
	for _, doc := range documents {
		for key, value := range doc {
			stat := ensureFieldStat(fieldStats, key)
			stat.Count++

			typeStr := detectBSONType(value)
			if stat.Type == "" {
				stat.Type = typeStr
			} else if stat.Type != typeStr && typeStr != "null" {
				// 如果类型不一致且不是 null，标记为 mixed
				stat.Type = "mixed"
			}
		}
	}

	// 转换为 FieldInfo
	fields := make([]format.FieldInfo, 0, len(fieldStats))
	for name, stat := range fieldStats {
		fields = append(fields, format.FieldInfo{
			Name:           name,
			Type:           mapToFieldType(stat.Type),
			OriginalType:   stat.Type,
			Nullable:       true, // MongoDB 字段都是可选的
			IsPrimaryKey:   name == "_id",
			OccurrenceRate: float64(stat.Count) / float64(len(documents)),
		})
	}

	// 排序：_id 优先，然后按出现率降序
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Name == "_id" {
			return true
		}
		if fields[j].Name == "_id" {
			return false
		}
		return fields[i].OccurrenceRate > fields[j].OccurrenceRate
	})

	// 获取索引信息
	indexes, err := p.getIndexes(ctx, coll)
	if err != nil {
		// 索引获取失败不影响主要功能
		indexes = []plugin.IndexInfo{}
	}

	// 获取文档总数
	totalDocs, _ := coll.EstimatedDocumentCount(ctx)

	return &format.TableInfo{
		Name:       collection,
		RowCount:   &totalDocs,
		Fields:     fields,
		PrimaryKey: []string{"_id"},
		Extensions: []format.ExtensionInfo{
			&format.DocCollectionInfo{
				IsSampled:      true,
				SampleSize:     len(documents),
				SchemaType:     "dynamic",
				Indexes:        indexes,
				TotalDocuments: totalDocs,
			},
		},
	}, nil
}

// ReadPreview 实现 DocCollectionParser 接口 - 读取 Collection 的预览数据
func (p *MongoDBCollectionParser) ReadPreview(
	ctx context.Context,
	client interface{},
	database, collection string,
	offset, limit int64,
	options *format.ParseOptions,
) ([]map[string]interface{}, error) {

	mongoClient, ok := client.(*mongo.Client)
	if !ok {
		return nil, fmt.Errorf("invalid client type: expected *mongo.Client")
	}

	coll := mongoClient.Database(database).Collection(collection)

	// 查询文档
	findOptions := mongoOptions.Find().SetSkip(offset).SetLimit(limit)
	cursor, err := coll.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to find documents: %w", err)
	}
	defer cursor.Close(ctx)

	var documents []bson.M
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, fmt.Errorf("failed to decode documents: %w", err)
	}

	// 转换为 map[string]interface{}
	result := make([]map[string]interface{}, 0, len(documents))
	for _, doc := range documents {
		record := make(map[string]interface{})
		for k, v := range doc {
			record[k] = convertBSONValue(v)
		}
		result = append(result, record)
	}

	return result, nil
}

// SupportedEngineTypes 实现 DocCollectionParser 接口 - 返回支持的引擎类型
func (p *MongoDBCollectionParser) SupportedEngineTypes() []string {
	return []string{"mongodb"}
}

// ========== 内部辅助方法 ==========

// fieldStat 字段统计信息
type fieldStat struct {
	Count int    // 出现次数
	Type  string // 类型
}

// ensureFieldStat 确保字段统计存在
func ensureFieldStat(stats map[string]*fieldStat, fieldName string) *fieldStat {
	if stat, exists := stats[fieldName]; exists {
		return stat
	}
	stat := &fieldStat{Count: 0, Type: ""}
	stats[fieldName] = stat
	return stat
}

// detectBSONType 检测 BSON 值的类型
func detectBSONType(value interface{}) string {
	if value == nil {
		return "null"
	}

	switch v := value.(type) {
	case string:
		return "string"
	case int32, int64, int:
		return "int64"
	case float32, float64:
		return "float64"
	case bool:
		return "bool"
	case primitive.ObjectID:
		return "objectid"
	case primitive.DateTime, time.Time:
		return "datetime"
	case []interface{}, primitive.A:
		return "array"
	case bson.M, map[string]interface{}:
		return "object"
	case primitive.Binary:
		return "binary"
	case primitive.Decimal128:
		return "decimal"
	default:
		return fmt.Sprintf("unknown(%T)", v)
	}
}

// mapToFieldType 将 BSON 类型映射到标准 FieldType
func mapToFieldType(bsonType string) format.FieldType {
	switch bsonType {
	case "string":
		return format.FieldTypeString
	case "int64":
		return format.FieldTypeInt
	case "float64":
		return format.FieldTypeFloat
	case "bool":
		return format.FieldTypeBool
	case "datetime":
		return format.FieldTypeTimestamp
	case "objectid":
		return format.FieldTypeString // ObjectID 映射为 string
	case "array":
		return format.FieldTypeArray
	case "object":
		return format.FieldTypeJSON
	case "binary":
		return format.FieldTypeBytes
	case "decimal":
		return format.FieldTypeDecimal
	case "mixed":
		return format.FieldTypeMixed
	case "null":
		return format.FieldTypeUnknown
	default:
		return format.FieldTypeUnknown
	}
}

// convertBSONValue 转换 BSON 值为可序列化的值
func convertBSONValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case primitive.ObjectID:
		return v.Hex()
	case primitive.DateTime:
		return v.Time()
	case time.Time:
		return v
	case bson.M:
		// 递归转换嵌套对象
		result := make(map[string]interface{})
		for k, val := range v {
			result[k] = convertBSONValue(val)
		}
		return result
	case []interface{}:
		// 递归转换数组
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = convertBSONValue(val)
		}
		return result
	case primitive.A:
		// 递归转换 BSON 数组
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = convertBSONValue(val)
		}
		return result
	case primitive.Binary:
		return v.Data
	case primitive.Decimal128:
		return v.String()
	default:
		return v
	}
}

// getIndexes 获取集合的索引信息
func (p *MongoDBCollectionParser) getIndexes(ctx context.Context, coll *mongo.Collection) ([]plugin.IndexInfo, error) {
	cursor, err := coll.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var indexes []plugin.IndexInfo
	for cursor.Next(ctx) {
		var indexDoc struct {
			Name   string                 `bson:"name"`
			Key    map[string]interface{} `bson:"key"`
			Unique bool                   `bson:"unique"`
		}

		if err := cursor.Decode(&indexDoc); err != nil {
			continue
		}

		// 提取索引字段
		fields := make([]string, 0, len(indexDoc.Key))
		for field := range indexDoc.Key {
			fields = append(fields, field)
		}

		indexes = append(indexes, plugin.IndexInfo{
			Name:      indexDoc.Name,
			Fields:    fields,
			IsUnique:  indexDoc.Unique,
			IndexType: "btree", // MongoDB 默认使用 B-tree 索引
		})
	}

	return indexes, nil
}
