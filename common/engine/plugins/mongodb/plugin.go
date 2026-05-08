package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDBPlugin MongoDB 数据库插件
type MongoDBPlugin struct{}

// init 函数在包被导入时自动注册插件
func init() {
	plugin.Register(&MongoDBPlugin{})
}

// Type 返回数据库类型标识
func (p *MongoDBPlugin) Type() string {
	return "mongodb"
}

// DisplayName 返回显示名称
func (p *MongoDBPlugin) DisplayName() string {
	return "MongoDB"
}

// EngineOrigin 返回引擎分类
func (p *MongoDBPlugin) EngineOrigin() string {
	return "general"
}

// DefaultPort 返回默认端口
func (p *MongoDBPlugin) DefaultPort() int {
	return 27017
}

// RequiredFields 返回必填字段列表
func (p *MongoDBPlugin) RequiredFields() []string {
	return []string{"host", "database"}
}

// SensitiveFields 返回敏感字段列表
func (p *MongoDBPlugin) SensitiveFields() []string {
	return []string{"password"}
}

func (p *MongoDBPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewDocumentCapabilities(p.Type())
}

func (p *MongoDBPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.DocumentCatalogModel()
}

func (p *MongoDBPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *MongoDBPlugin) documentCatalogAdapter() plugin.DocumentCatalogAdapter {
	return plugin.DocumentCatalogAdapter{
		Plugin:               p,
		ListDatabasesFunc:    p.listDatabases,
		ListCollectionsFunc:  p.listCollections,
		IsSystemDatabaseFunc: p.IsSystemDatabase,
	}
}

func (p *MongoDBPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return plugin.ListDocumentCatalogChildren(ctx, p.documentCatalogAdapter(), parent.EngineID, connInfo, parent, opts)
}

func (p *MongoDBPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return plugin.ResolveDocumentCatalogPath(ctx, p.documentCatalogAdapter(), path.EngineID, connInfo, path)
}

func (p *MongoDBPlugin) DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	return plugin.DescribeDocumentItem(ctx, p, path.EngineID, connInfo, path, opts)
}

func (p *MongoDBPlugin) QueryLanguages() []string {
	return []string{"mql"}
}

func (p *MongoDBPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return p.generateSampleQuery(ctx, connInfo)
}

func (p *MongoDBPlugin) ExecuteRuntimeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return p.executeQuery(ctx, connInfo, req.Query)
}

func (p *MongoDBPlugin) ExecuteDocumentQuery(ctx context.Context, connInfo plugin.ConnectionInfo, command string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return p.executeQuery(ctx, connInfo, command)
}

func (p *MongoDBPlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	query := opts.Query
	if query == "" {
		collection := ""
		if len(path.Segments) > 0 {
			collection = path.Segments[len(path.Segments)-1].Name
		}
		if collection == "" {
			return nil, fmt.Errorf("MongoDB batch read requires collection path or query")
		}
		limit := opts.Limit
		if limit <= 0 {
			limit = 1000
		}
		query = fmt.Sprintf(`{"find": "%s", "filter": {}, "limit": %d}`, collection, limit)
	}
	result, err := p.ExecuteDocumentQuery(ctx, connInfo, query, plugin.QueryOptions{Limit: opts.Limit})
	if err != nil {
		return nil, err
	}
	return plugin.QueryResultToBatchData(result, opts.Offset), nil
}

// ValidateConnectionInfo 验证连接信息
func (p *MongoDBPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// BuildDSN 构建连接字符串
func (p *MongoDBPlugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	parts := plugin.ParseDriverConnInfo(connInfo, p.DefaultPort(), "")
	authSource := plugin.GetString(connInfo, "auth_source")

	if parts.Host == "" {
		return "", fmt.Errorf("missing required MongoDB connection info: host")
	}

	return plugin.MongoDBStyleDSN(parts.User, parts.Password, parts.Host, parts.Port, parts.Database, authSource), nil
}

// TestConnection 测试数据库连接
func (p *MongoDBPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	client, err := p.createClient(ctx, connInfo)
	if err != nil {
		return err
	}
	defer func() {
		if err := client.Disconnect(ctx); err != nil {
			// 忽略断开连接错误
		}
	}()

	testCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 用 listDatabases 而不是 ping，因为 ping 不需要认证，无法验证凭据
	if _, err := client.ListDatabaseNames(testCtx, bson.M{}); err != nil {
		return fmt.Errorf("failed to authenticate with MongoDB: %w", err)
	}

	return nil
}

// createClient 创建 MongoDB 客户端（内部辅助方法）
func (p *MongoDBPlugin) createClient(ctx context.Context, connInfo plugin.ConnectionInfo) (*mongo.Client, error) {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	clientOptions := options.Client().ApplyURI(connStr)
	clientOptions.SetConnectTimeout(10 * time.Second)
	clientOptions.SetServerSelectionTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	return client, nil
}

// 查询数据库中第一个集合名称，生成可执行的 find 命令
func (p *MongoDBPlugin) generateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo) (string, string) {
	const fallback = `{"find": "collection_name", "filter": {}, "limit": 10}`

	client, err := p.createClient(ctx, connInfo)
	if err != nil {
		return fallback, "mql"
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	database := plugin.GetString(connInfo, "database")
	names, err := client.Database(database).ListCollectionNames(ctx, bson.M{})
	if err != nil || len(names) == 0 {
		return fallback, "mql"
	}

	return fmt.Sprintf(`{"find": "%s", "filter": {}, "limit": 10}`, names[0]), "mql"
}

// query 为 JSON 命令字符串，支持 find/aggregate/count/distinct，其他命令走 RunCommand 通用路径
// 示例：{"find":"users","filter":{"age":{"$gt":18}},"limit":10}
func (p *MongoDBPlugin) executeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, query string) (*plugin.QueryResult, error) {
	// 解析 JSON 命令
	var cmd map[string]interface{}
	if err := json.Unmarshal([]byte(query), &cmd); err != nil {
		return nil, fmt.Errorf("无效的 MQL 格式，请输入 JSON 命令对象，示例：{\"find\":\"collection\",\"filter\":{},\"limit\":10}：%w", err)
	}

	// 建立连接
	client, err := p.createClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("连接 MongoDB 失败：%w", err)
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	database := plugin.GetString(connInfo, "database")
	db := client.Database(database)

	// 根据命令类型分发执行
	var docs []bson.M

	if collName, ok := getStringKey(cmd, "find"); ok {
		docs, err = p.execFind(ctx, db, collName, cmd)
	} else if collName, ok := getStringKey(cmd, "aggregate"); ok {
		docs, err = p.execAggregate(ctx, db, collName, cmd)
	} else if collName, ok := getStringKey(cmd, "count"); ok {
		count, countErr := p.execCount(ctx, db, collName, cmd)
		if countErr != nil {
			return nil, countErr
		}
		return &plugin.QueryResult{
			Columns: []string{"count"},
			Rows:    []map[string]interface{}{{"count": count}},
		}, nil
	} else if collName, ok := getStringKey(cmd, "distinct"); ok {
		values, distinctErr := p.execDistinct(ctx, db, collName, cmd)
		if distinctErr != nil {
			return nil, distinctErr
		}
		rows := make([]map[string]interface{}, len(values))
		for i, v := range values {
			rows[i] = map[string]interface{}{"value": v}
		}
		return &plugin.QueryResult{Columns: []string{"value"}, Rows: rows}, nil
	} else {
		// 通用 RunCommand 路径
		docs, err = p.execRunCommand(ctx, db, cmd)
	}

	if err != nil {
		return nil, err
	}

	return bsonDocsToQueryResult(docs), nil
}

// execFind 执行 find 命令
func (p *MongoDBPlugin) execFind(ctx context.Context, db *mongo.Database, collName string, cmd map[string]interface{}) ([]bson.M, error) {
	coll := db.Collection(collName)

	findOptions := options.Find()

	// 默认 limit 防止结果集过大
	limit := int64(1000)
	if l, ok := cmd["limit"]; ok {
		if lf, ok := toInt64(l); ok {
			limit = lf
		}
	}
	findOptions.SetLimit(limit)

	if skip, ok := cmd["skip"]; ok {
		if sf, ok := toInt64(skip); ok {
			findOptions.SetSkip(sf)
		}
	}

	if sort, ok := cmd["sort"]; ok {
		if sortDoc, ok := sort.(map[string]interface{}); ok {
			findOptions.SetSort(sortDoc)
		}
	}

	if proj, ok := cmd["projection"]; ok {
		if projDoc, ok := proj.(map[string]interface{}); ok {
			findOptions.SetProjection(projDoc)
		}
	}

	filter := bson.M{}
	if f, ok := cmd["filter"]; ok {
		if fDoc, ok := f.(map[string]interface{}); ok {
			filter = bson.M(fDoc)
		}
	}

	cursor, err := coll.Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("执行 find 失败：%w", err)
	}
	defer cursor.Close(ctx) //nolint:errcheck

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("读取结果失败：%w", err)
	}
	return results, nil
}

// execAggregate 执行 aggregate 命令
func (p *MongoDBPlugin) execAggregate(ctx context.Context, db *mongo.Database, collName string, cmd map[string]interface{}) ([]bson.M, error) {
	coll := db.Collection(collName)

	var pipeline []interface{}
	if pl, ok := cmd["pipeline"]; ok {
		if plArr, ok := pl.([]interface{}); ok {
			pipeline = plArr
		}
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("执行 aggregate 失败：%w", err)
	}
	defer cursor.Close(ctx) //nolint:errcheck

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("读取聚合结果失败：%w", err)
	}
	return results, nil
}

// execCount 执行 count 命令，返回文档数量
func (p *MongoDBPlugin) execCount(ctx context.Context, db *mongo.Database, collName string, cmd map[string]interface{}) (int64, error) {
	coll := db.Collection(collName)

	filter := bson.M{}
	if q, ok := cmd["query"]; ok {
		if qDoc, ok := q.(map[string]interface{}); ok {
			filter = bson.M(qDoc)
		}
	}

	count, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("执行 count 失败：%w", err)
	}
	return count, nil
}

// execDistinct 执行 distinct 命令，返回去重值列表
func (p *MongoDBPlugin) execDistinct(ctx context.Context, db *mongo.Database, collName string, cmd map[string]interface{}) ([]interface{}, error) {
	coll := db.Collection(collName)

	field, _ := getStringKey(cmd, "key")
	if field == "" {
		return nil, fmt.Errorf("distinct 命令缺少 key 字段")
	}

	filter := bson.M{}
	if q, ok := cmd["query"]; ok {
		if qDoc, ok := q.(map[string]interface{}); ok {
			filter = bson.M(qDoc)
		}
	}

	values, err := coll.Distinct(ctx, field, filter)
	if err != nil {
		return nil, fmt.Errorf("执行 distinct 失败：%w", err)
	}
	return values, nil
}

// execRunCommand 通用 RunCommand 路径
func (p *MongoDBPlugin) execRunCommand(ctx context.Context, db *mongo.Database, cmd map[string]interface{}) ([]bson.M, error) {
	var result bson.M
	if err := db.RunCommand(ctx, cmd).Decode(&result); err != nil {
		return nil, fmt.Errorf("执行命令失败：%w", err)
	}

	// 尝试从结果中提取文档列表
	for _, key := range []string{"cursor", "values", "documents", "result"} {
		if val, ok := result[key]; ok {
			if docs, ok := val.(primitive.A); ok {
				var bsonDocs []bson.M
				for _, d := range docs {
					if m, ok := d.(bson.M); ok {
						bsonDocs = append(bsonDocs, m)
					}
				}
				return bsonDocs, nil
			}
		}
	}

	// 返回整个结果作为单行
	return []bson.M{result}, nil
}

// bsonDocsToQueryResult 将 BSON 文档列表转换为列式 QueryResult
// - _id 排第一列
// - 其余列按首次出现顺序排列
// - ObjectID 转 hex string；time.Time 转 RFC3339 string
func bsonDocsToQueryResult(docs []bson.M) *plugin.QueryResult {
	if len(docs) == 0 {
		return &plugin.QueryResult{Columns: []string{}, Rows: []map[string]interface{}{}}
	}

	// 收集所有列名（union of keys），保持首次出现顺序
	seen := map[string]bool{}
	var columns []string
	for _, doc := range docs {
		// _id 优先
		if _, has := doc["_id"]; has && !seen["_id"] {
			columns = append(columns, "_id")
			seen["_id"] = true
		}
		// 其余 key 按 doc 内顺序（Go map 无序，用 sort 保持稳定）
		keys := make([]string, 0, len(doc))
		for k := range doc {
			if k != "_id" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !seen[k] {
				columns = append(columns, k)
				seen[k] = true
			}
		}
	}

	rows := make([]map[string]interface{}, len(docs))
	for i, doc := range docs {
		row := make(map[string]interface{}, len(columns))
		for _, col := range columns {
			row[col] = convertBSONValue(doc[col])
		}
		rows[i] = row
	}

	return &plugin.QueryResult{Columns: columns, Rows: rows}
}

// convertBSONValue 将 BSON 值转为 JSON 友好的 Go 类型
func convertBSONValue(v interface{}) interface{} {
	switch val := v.(type) {
	case primitive.ObjectID:
		return val.Hex()
	case primitive.DateTime:
		return val.Time().UTC().Format(time.RFC3339)
	case primitive.Timestamp:
		return val.T
	case primitive.Decimal128:
		return val.String()
	case primitive.Binary:
		return fmt.Sprintf("<Binary len=%d>", len(val.Data))
	case bson.M:
		converted := make(map[string]interface{}, len(val))
		for k, v2 := range val {
			converted[k] = convertBSONValue(v2)
		}
		return converted
	case primitive.A:
		arr := make([]interface{}, len(val))
		for i, elem := range val {
			arr[i] = convertBSONValue(elem)
		}
		return arr
	case []interface{}:
		arr := make([]interface{}, len(val))
		for i, elem := range val {
			arr[i] = convertBSONValue(elem)
		}
		return arr
	case time.Time:
		return val.UTC().Format(time.RFC3339)
	default:
		return val
	}
}

// getStringKey 从 map 中获取指定 key 的字符串值
func getStringKey(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// toInt64 将 interface{} 转为 int64
func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	case int32:
		return int64(n), true
	}
	return 0, false
}
