package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ListDatabases 实现 NoSQLPlugin 接口 - 列出所有数据库
func (p *MongoDBPlugin) ListDatabases(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.DatabaseInfo, error) {
	client, err := p.CreateClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer p.CloseClient(ctx, client)

	mongoClient := client.(*mongo.Client)

	// 列出所有数据库
	databases, err := mongoClient.ListDatabases(ctx, bson.M{})
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

// ListCollections 实现 NoSQLPlugin 接口 - 列出指定数据库下的所有集合
func (p *MongoDBPlugin) ListCollections(ctx context.Context, connInfo plugin.ConnectionInfo, database string) ([]plugin.CollectionInfo, error) {
	client, err := p.CreateClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer p.CloseClient(ctx, client)

	mongoClient := client.(*mongo.Client)
	db := mongoClient.Database(database)

	// 列出所有集合
	collections, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}

	// 转换为 plugin.CollectionInfo
	result := make([]plugin.CollectionInfo, 0, len(collections))
	for _, collName := range collections {
		// 获取集合统计信息
		stats, err := p.GetCollectionStats(ctx, connInfo, database, collName)
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

// GetCollectionStats 实现 NoSQLPlugin 接口 - 获取集合统计信息
func (p *MongoDBPlugin) GetCollectionStats(ctx context.Context, connInfo plugin.ConnectionInfo, database, collection string) (*plugin.CollectionStats, error) {
	client, err := p.CreateClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer p.CloseClient(ctx, client)

	mongoClient := client.(*mongo.Client)
	coll := mongoClient.Database(database).Collection(collection)

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
			AvgDocSize:    0,
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
		AvgDocSize:    statsDoc.StorageStats.AvgObjSize,
		Indexes:       indexes,
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

// IsSystemDatabase 实现 NoSQLPlugin 接口 - 判断是否为系统数据库
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

// CreateClient 实现 NoSQLPlugin 接口 - 创建数据库客户端
func (p *MongoDBPlugin) CreateClient(ctx context.Context, connInfo plugin.ConnectionInfo) (interface{}, error) {
	// 构建连接字符串
	connStr, err := p.BuildConnectionString(connInfo)
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

// CloseClient 实现 NoSQLPlugin 接口 - 关闭数据库客户端
func (p *MongoDBPlugin) CloseClient(ctx context.Context, client interface{}) error {
	if client == nil {
		return nil
	}

	mongoClient, ok := client.(*mongo.Client)
	if !ok {
		return fmt.Errorf("invalid client type: expected *mongo.Client")
	}

	if err := mongoClient.Disconnect(ctx); err != nil {
		return fmt.Errorf("failed to disconnect MongoDB client: %w", err)
	}

	return nil
}
