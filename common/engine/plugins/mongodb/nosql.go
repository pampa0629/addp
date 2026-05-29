package mongodb

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// listDatabases lists all non-system databases for the catalog callbacks.
func (p *MongoDBPlugin) listDatabases(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.DatabaseInfo, error) {
	client, err := p.openClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	// 列出所有数据库
	databases, err := client.ListDatabases(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}

	// 转换为 plugin.DatabaseInfo
	result := make([]plugin.DatabaseInfo, 0)
	for _, db := range databases.Databases {
		// 过滤系统数据库
		if p.IsSystemDatabase(db.Name) {
			continue
		}

		result = append(result, plugin.DatabaseInfo{
			Name:      db.Name,
			SizeBytes: db.SizeOnDisk,
		})
	}

	return result, nil
}

// listCollections lists collections for the catalog callbacks.
func (p *MongoDBPlugin) listCollections(ctx context.Context, connInfo plugin.ConnectionInfo, database string) ([]plugin.CollectionInfo, error) {
	client, err := p.openClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	db := client.Database(database)

	// 列出所有集合
	collections, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}

	// 转换为 plugin.CollectionInfo
	result := make([]plugin.CollectionInfo, 0, len(collections))
	for _, collName := range collections {
		// 获取集合统计信息
		stats, err := p.getCollectionStats(ctx, connInfo, database, collName)
		if err != nil {
			// 如果获取统计失败，使用默认值
			result = append(result, plugin.CollectionInfo{
				Database:      database,
				Name:          collName,
				DocumentCount: 0,
				SizeBytes:     0,
			})
			continue
		}

		result = append(result, plugin.CollectionInfo{
			Database:      database,
			Name:          collName,
			DocumentCount: stats.DocumentCount,
			SizeBytes:     stats.SizeBytes,
		})
	}

	return result, nil
}

// getCollectionStats returns collection statistics.
func (p *MongoDBPlugin) getCollectionStats(ctx context.Context, connInfo plugin.ConnectionInfo, database, collection string) (*plugin.CollectionStats, error) {
	client, err := p.openClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	coll := client.Database(database).Collection(collection)

	// 使用 $collStats 聚合管道获取统计信息
	pipeline := mongo.Pipeline{
		{{"$collStats", bson.M{"storageStats": bson.M{}}}},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection stats: %w", err)
	}
	defer cursor.Close(ctx)

	var statsDoc struct {
		StorageStats struct {
			Count      int64 `bson:"count"`
			Size       int64 `bson:"size"`
			AvgObjSize int64 `bson:"avgObjSize"`
			NIndexes   int   `bson:"nindexes"`
		} `bson:"storageStats"`
	}

	if cursor.Next(ctx) {
		if err := cursor.Decode(&statsDoc); err != nil {
			return nil, fmt.Errorf("failed to decode stats: %w", err)
		}
	} else {
		// 如果聚合失败，使用估算值
		count, _ := coll.EstimatedDocumentCount(ctx)
		return &plugin.CollectionStats{
			DocumentCount: int64(count),
			SizeBytes:     0,
			IndexCount:    0,
			AvgRecordSize: 0,
			Indexes:       []plugin.IndexInfo{},
		}, nil
	}

	// 获取索引信息
	indexes, err := p.getIndexes(ctx, coll)
	if err != nil {
		// 索引获取失败不影响主要统计信息
		indexes = []plugin.IndexInfo{}
	}

	return &plugin.CollectionStats{
		DocumentCount: statsDoc.StorageStats.Count,
		SizeBytes:     statsDoc.StorageStats.Size,
		IndexCount:    statsDoc.StorageStats.NIndexes,
		AvgRecordSize: statsDoc.StorageStats.AvgObjSize,
		Indexes:       indexes,
	}, nil
}

// SampleDynamicSchema samples a collection and returns inferred dynamic field info.
func (p *MongoDBPlugin) SampleDynamicSchema(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	if len(path.Segments) < 2 {
		return nil, fmt.Errorf("dynamic schema item path requires database and collection segments")
	}
	database := path.Segments[0].Name
	collection := path.Segments[len(path.Segments)-1].Name

	client, err := p.createClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	coll := client.Database(database).Collection(collection)
	sampleSize := opts.SampleSize
	if sampleSize <= 0 {
		sampleSize = 100
	}

	cursor, err := coll.Find(ctx, bson.M{}, options.Find().SetLimit(int64(sampleSize)))
	if err != nil {
		return nil, fmt.Errorf("failed to sample documents: %w", err)
	}
	defer cursor.Close(ctx) //nolint:errcheck

	var documents []bson.M
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, fmt.Errorf("failed to decode sampled documents: %w", err)
	}

	fieldStats := make(map[string]*mongoFieldStat)
	for _, doc := range documents {
		for key, value := range doc {
			stat := ensureMongoFieldStat(fieldStats, key)
			stat.Count++
			typeStr := detectMongoBSONType(value)
			if stat.Type == "" {
				stat.Type = typeStr
			} else if stat.Type != typeStr && typeStr != "null" {
				stat.Type = "mixed"
			}
		}
	}

	fields := make([]datatype.FieldInfo, 0, len(fieldStats))
	for name, stat := range fieldStats {
		fields = append(fields, datatype.FieldInfo{
			Name:       name,
			Type:       mapMongoBSONType(stat.Type),
			NativeType: stat.Type,
			Nullable:   true,
			PrimaryKey: name == "_id",
		})
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Name == "_id" {
			return true
		}
		if fields[j].Name == "_id" {
			return false
		}
		return fields[i].Name < fields[j].Name
	})

	stats, err := p.getCollectionStats(ctx, connInfo, database, collection)
	if err != nil {
		count, _ := coll.EstimatedDocumentCount(ctx)
		stats = &plugin.CollectionStats{DocumentCount: count}
	}

	attrs := map[string]interface{}{
		"database":        database,
		"collection":      collection,
		"is_sampled":      true,
		"sample_size":     len(documents),
		"schema_type":     "dynamic",
		"total_documents": stats.DocumentCount,
	}
	return &plugin.ItemMetadata{
		Path:    path,
		Kind:    plugin.CatalogKindCollection,
		Fields:  fields,
		Indexes: append([]plugin.IndexInfo{}, stats.Indexes...),
		Stats: map[string]interface{}{
			"document_count":  stats.DocumentCount,
			"size_bytes":      stats.SizeBytes,
			"index_count":     stats.IndexCount,
			"avg_record_size": stats.AvgRecordSize,
		},
		Attributes: attrs,
	}, nil
}

// getIndexes 获取集合的索引信息（内部辅助方法）
func (p *MongoDBPlugin) getIndexes(ctx context.Context, coll *mongo.Collection) ([]plugin.IndexInfo, error) {
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

type mongoFieldStat struct {
	Count int
	Type  string
}

func ensureMongoFieldStat(stats map[string]*mongoFieldStat, fieldName string) *mongoFieldStat {
	if stat, exists := stats[fieldName]; exists {
		return stat
	}
	stat := &mongoFieldStat{}
	stats[fieldName] = stat
	return stat
}

func detectMongoBSONType(value interface{}) string {
	if value == nil {
		return "null"
	}
	switch value.(type) {
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
		return fmt.Sprintf("unknown(%T)", value)
	}
}

func mapMongoBSONType(bsonType string) datatype.FieldType {
	switch bsonType {
	case "string", "objectid":
		return datatype.FieldTypeString
	case "int64":
		return datatype.FieldTypeBigInt
	case "float64":
		return datatype.FieldTypeDouble
	case "bool":
		return datatype.FieldTypeBool
	case "datetime":
		return datatype.FieldTypeTimestamp
	case "array":
		return datatype.FieldTypeArray
	case "object":
		return datatype.FieldTypeJSON
	case "binary":
		return datatype.FieldTypeBytes
	case "decimal":
		return datatype.FieldTypeDecimal
	case "mixed":
		return datatype.FieldTypeMixed
	case "null":
		return datatype.FieldTypeUnknown
	default:
		return datatype.FieldTypeUnknown
	}
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// IsSystemDatabase 判断是否为系统数据库
func (p *MongoDBPlugin) IsSystemDatabase(databaseName string) bool {
	// MongoDB 系统数据库：admin, local, config
	systemDatabases := []string{"admin", "local", "config"}
	for _, sysDB := range systemDatabases {
		if databaseName == sysDB {
			return true
		}
	}
	return false
}

func (p *MongoDBPlugin) openClient(ctx context.Context, connInfo plugin.ConnectionInfo) (*mongo.Client, error) {
	// 构建连接字符串
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 设置连接选项
	clientOptions := options.Client().ApplyURI(connStr)
	clientOptions.SetConnectTimeout(10 * time.Second)
	clientOptions.SetServerSelectionTimeout(10 * time.Second)

	// 连接数据库
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// 测试连接
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	return client, nil
}
