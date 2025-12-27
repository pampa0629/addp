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
	if req == nil || req.Resource == nil {
		return false
	}
	if req.Schema == "" || req.Table == "" {
		return false
	}

	// 支持 MongoDB
	resourceType := sanitizeResourceType(req.Resource.ResourceType)
	return resourceType == "mongodb"
}

func (p *mongodbPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	// 转换Resource类型到plugin models
	pluginResource := &plugin.Resource{
		ID:             req.Resource.ID,
		ResourceType:   req.Resource.ResourceType,
		ConnectionInfo: plugin.ConnectionInfo(req.Resource.ConnectionInfo),
	}

	// 使用插件系统构建连接字符串
	connStr, err := plugin.BuildConnectionString(pluginResource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 创建MongoDB客户端
	clientOptions := options.Client().ApplyURI(connStr)
	clientOptions.SetConnectTimeout(10 * time.Second)
	clientOptions.SetServerSelectionTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer func() {
		if err := client.Disconnect(ctx); err != nil {
			// 忽略断开连接错误
		}
	}()

	// 测试连接
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	// 获取collection
	// req.Schema 是数据库名，req.Table 格式是 "database.collection"
	// 需要从 req.Table 中提取实际的 collection 名称
	database := client.Database(req.Schema)

	// 去掉 "database." 前缀，只保留 collection 名称
	collectionName := req.Table
	if strings.HasPrefix(req.Table, req.Schema+".") {
		collectionName = strings.TrimPrefix(req.Table, req.Schema+".")
	}

	collection := database.Collection(collectionName)

	fmt.Printf("[MongoDB Preview] Querying database=%s, collection=%s (original table=%s)\n",
		req.Schema, collectionName, req.Table)

	// 计算总文档数
	totalCount, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to count documents: %w", err)
	}

	// 计算分页参数
	skip := (req.Page - 1) * req.PageSize
	limit := req.PageSize

	// 限制最大返回行数
	const maxRows = 50
	if limit > maxRows {
		limit = maxRows
	}

	// 查询文档
	findOptions := options.Find().SetSkip(int64(skip)).SetLimit(int64(limit))
	cursor, err := collection.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to query collection: %w", err)
	}
	defer cursor.Close(ctx)

	// 收集所有文档
	var documents []bson.M
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, fmt.Errorf("failed to decode documents: %w", err)
	}

	// 如果没有文档，返回空预览
	if len(documents) == 0 {
		fmt.Printf("[MongoDB Preview] No documents found in collection %s.%s (total=%d)\n", req.Schema, req.Table, totalCount)
		return &models.TablePreview{
			Mode:         PreviewModeTable,
			Columns:      []string{},
			Rows:         []map[string]interface{}{},
			Total:        0,
			Page:         req.Page,
			PageSize:     req.PageSize,
			ResourceID:   req.Resource.ID,
			Schema:       req.Schema,
			Table:        req.Table,
			ResourceType: req.Resource.ResourceType,
		}, nil
	}

	fmt.Printf("[MongoDB Preview] Found %d documents in collection %s.%s (total=%d, page=%d, pageSize=%d)\n",
		len(documents), req.Schema, req.Table, totalCount, req.Page, req.PageSize)

	// 提取所有字段名（从所有文档中收集）
	columnsSet := make(map[string]bool)
	for _, doc := range documents {
		for key := range doc {
			columnsSet[key] = true
		}
	}

	// 将字段名转换为有序列表（_id放在最前面）
	columns := make([]string, 0, len(columnsSet))
	if columnsSet["_id"] {
		columns = append(columns, "_id")
		delete(columnsSet, "_id")
	}
	for col := range columnsSet {
		columns = append(columns, col)
	}

	// 转换文档为行数据（map格式）
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
		Mode:         PreviewModeTable,
		Columns:      columns,
		Rows:         rows,
		Total:        int(totalCount),
		Page:         req.Page,
		PageSize:     req.PageSize,
		ResourceID:   req.Resource.ID,
		Schema:       req.Schema,
		Table:        req.Table,
		ResourceType: req.Resource.ResourceType,
	}, nil
}

// formatMongoDBValue 格式化MongoDB值为JSON可序列化的格式
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
