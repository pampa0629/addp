package mongodb

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// listDatabases lists all non-system databases for the catalog callbacks.
func (p *MongoDBPlugin) listDatabases(ctx context.Context, connInfo plugin.ConnectionInfo, root plugin.CatalogPath) ([]plugin.CatalogEntry, error) {
	client, err := p.openClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	// MongoDB 返回当前认证主体被授权访问的数据库，不能把服务器上的
	// 其他数据库泄露到 ADDP Catalog。
	databases, err := client.ListDatabases(ctx, bson.M{}, options.ListDatabases().
		SetNameOnly(true).
		SetAuthorizedDatabases(true))
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}

	result := make([]plugin.CatalogEntry, 0, len(databases.Databases))
	for _, db := range databases.Databases {
		// 过滤系统数据库
		if p.IsSystemDatabase(db.Name) {
			continue
		}

		sizeBytes := db.SizeOnDisk
		result = append(result, plugin.CatalogEntry{
			Name: db.Name,
			Path: plugin.CatalogPath{
				Version:  root.Version,
				EngineID: root.EngineID,
				Segments: append(append([]plugin.CatalogSegment(nil), root.Segments...), plugin.CatalogSegment{
					Term: plugin.CatalogTermDatabase,
					Kind: plugin.CatalogKindNamespace,
					Name: db.Name,
				}),
			},
			Term: plugin.CatalogTermDatabase,
			Kind: plugin.CatalogKindNamespace,
			Role: plugin.CatalogRoleBranch,
			Storage: &plugin.CatalogStorageFacts{
				SizeBytes: &sizeBytes,
			},
		})
	}

	return result, nil
}

// listCollections lists collections for the dynamic schema catalog callbacks.
func (p *MongoDBPlugin) listCollections(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, database string) ([]plugin.CatalogEntry, error) {
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

	result := make([]plugin.CatalogEntry, 0, len(collections))
	for _, collName := range collections {
		// 获取 collection 的 engine 原生事实。
		stats, err := p.describeCollectionFacts(ctx, connInfo, database, collName)
		if err != nil {
			// 如果获取统计失败，使用默认值
			result = append(result, plugin.DynamicCollectionCatalogEntry(parent, database, collName, plugin.DynamicCollectionFacts{}))
			continue
		}

		result = append(result, plugin.DynamicCollectionCatalogEntry(parent, database, collName, *stats))
	}

	return result, nil
}

// describeCollectionFacts returns engine-native facts for a MongoDB collection catalog leaf.
func (p *MongoDBPlugin) describeCollectionFacts(ctx context.Context, connInfo plugin.ConnectionInfo, database, collection string) (*plugin.DynamicCollectionFacts, error) {
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
		count, countErr := coll.EstimatedDocumentCount(ctx)
		if countErr != nil {
			return nil, fmt.Errorf("failed to estimate collection documents: %w", countErr)
		}
		return &plugin.DynamicCollectionFacts{
			DocumentCount: &count,
			SizeBytes:     0,
			IndexCount:    0,
			AvgRecordSize: 0,
			Indexes:       []plugin.IndexFacts{},
		}, nil
	}

	// 获取索引信息
	indexes, err := p.getIndexes(ctx, coll)
	if err != nil {
		// 索引获取失败不影响主要统计信息
		indexes = []plugin.IndexFacts{}
	}

	estimatedDocumentCount := statsDoc.StorageStats.Count
	return &plugin.DynamicCollectionFacts{
		DocumentCount: &estimatedDocumentCount,
		SizeBytes:     statsDoc.StorageStats.Size,
		IndexCount:    statsDoc.StorageStats.NIndexes,
		AvgRecordSize: statsDoc.StorageStats.AvgObjSize,
		Indexes:       indexes,
	}, nil
}

// SampleDynamicSchema samples a collection and returns inferred dynamic field info.
func (p *MongoDBPlugin) SampleDynamicSchema(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	segments := plugin.CatalogPathWithoutRoot(path).Segments
	if len(segments) < 2 {
		return nil, fmt.Errorf("dynamic schema item path requires database and collection segments")
	}
	database := segments[0].Name
	collection := segments[len(segments)-1].Name

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
	// Dynamic collections often append newer document shapes after older
	// records. Include a deterministic tail sample so fields introduced later
	// are not invisible when the bounded head sample has a different schema.
	tailCursor, err := coll.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(sampleSize)))
	if err == nil {
		var tailDocuments []bson.M
		if decodeErr := tailCursor.All(ctx, &tailDocuments); decodeErr == nil {
			documents = append(documents, tailDocuments...)
		}
		_ = tailCursor.Close(ctx)
	}

	fieldStats := make(map[string]*mongoFieldStat)
	for _, doc := range documents {
		collectMongoDocumentFields(fieldStats, nil, map[string]interface{}(doc), 0)
	}

	fields := make([]datatype.FieldInfo, 0, len(fieldStats))
	for name, stat := range fieldStats {
		fields = append(fields, datatype.FieldInfo{
			Name:        name,
			Path:        append([]string(nil), stat.Path...),
			Type:        mapMongoBSONType(stat.Type),
			ElementType: mapMongoArrayElementType(stat.ElementType),
			NativeType:  stat.Type,
			Nullable:    true,
			PrimaryKey:  len(stat.Path) == 1 && name == "_id",
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

	stats, err := p.describeCollectionFacts(ctx, connInfo, database, collection)
	if err != nil {
		count, countErr := coll.EstimatedDocumentCount(ctx)
		if countErr != nil {
			return nil, fmt.Errorf("failed to estimate collection documents: %w", countErr)
		}
		stats = &plugin.DynamicCollectionFacts{DocumentCount: &count}
	}

	tableInfo := &datatype.TableInfo{
		Name:              collection,
		Kind:              plugin.CatalogKindCollection,
		Fields:            fields,
		EstimatedRowCount: stats.DocumentCount,
		SizeBytes:         &stats.SizeBytes,
		Native: map[string]interface{}{
			"database":        database,
			"collection":      collection,
			"sample_size":     len(documents),
			"schema_type":     "dynamic",
			"index_count":     stats.IndexCount,
			"avg_record_size": stats.AvgRecordSize,
		},
	}
	if opts.IncludeStatistics {
		count, err := coll.CountDocuments(ctx, bson.M{})
		if err != nil {
			return nil, fmt.Errorf("failed to count collection documents: %w", err)
		}
		tableInfo.RowCount = &count
	}

	return &plugin.CatalogFacts{
		Path:    path,
		Kind:    plugin.CatalogKindCollection,
		Table:   tableInfo,
		Indexes: append([]plugin.IndexFacts{}, stats.Indexes...),
	}, nil
}

// getIndexes 获取集合的索引信息（内部辅助方法）
func (p *MongoDBPlugin) getIndexes(ctx context.Context, coll *mongo.Collection) ([]plugin.IndexFacts, error) {
	cursor, err := coll.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var indexes []plugin.IndexFacts
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

		indexes = append(indexes, plugin.IndexFacts{
			Name:      indexDoc.Name,
			Fields:    fields,
			IsUnique:  indexDoc.Unique,
			IndexType: "btree", // MongoDB 默认使用 B-tree 索引
		})
	}

	return indexes, nil
}

type mongoFieldStat struct {
	Count       int
	Type        string
	ElementType string
	Path        []string
}

const (
	mongoSchemaMaxDepth         = 8
	mongoSchemaMaxFields        = 200
	mongoSchemaMaxArrayElements = 20
)

func ensureMongoFieldStat(stats map[string]*mongoFieldStat, path []string) *mongoFieldStat {
	fieldName := strings.Join(path, ".")
	if stat, exists := stats[fieldName]; exists {
		return stat
	}
	stat := &mongoFieldStat{Path: append([]string(nil), path...)}
	stats[fieldName] = stat
	return stat
}

func collectMongoFieldStats(stats map[string]*mongoFieldStat, path []string, value interface{}, depth int) {
	if len(path) == 0 || depth > mongoSchemaMaxDepth || len(stats) >= mongoSchemaMaxFields {
		return
	}
	stat := ensureMongoFieldStat(stats, path)
	stat.Count++
	typeStr := detectMongoBSONType(value)
	if typeStr != "null" {
		if stat.Type == "" || stat.Type == "null" {
			stat.Type = typeStr
		} else if stat.Type != typeStr {
			stat.Type = "mixed"
		}
	}
	if typeStr == "array" {
		elementType := detectMongoArrayElementType(value)
		if elementType != "" {
			if stat.ElementType == "" || stat.ElementType == "null" {
				stat.ElementType = elementType
			} else if stat.ElementType != elementType {
				stat.ElementType = "mixed"
			}
		}
	}
	if depth == mongoSchemaMaxDepth {
		return
	}
	collectMongoNestedFields(stats, path, value, depth)
}

func detectMongoArrayElementType(value interface{}) string {
	var values []interface{}
	switch typed := value.(type) {
	case primitive.A:
		values = []interface{}(typed)
	case []interface{}:
		values = typed
	default:
		return ""
	}
	limit := len(values)
	if limit > mongoSchemaMaxArrayElements {
		limit = mongoSchemaMaxArrayElements
	}
	elementType := ""
	for _, item := range values[:limit] {
		itemType := detectMongoBSONType(item)
		if itemType == "null" {
			continue
		}
		if elementType == "" {
			elementType = itemType
		} else if elementType != itemType {
			return "mixed"
		}
	}
	return elementType
}

func collectMongoNestedFields(stats map[string]*mongoFieldStat, path []string, value interface{}, depth int) {
	switch typed := value.(type) {
	case bson.M:
		collectMongoDocumentFields(stats, path, map[string]interface{}(typed), depth)
	case map[string]interface{}:
		collectMongoDocumentFields(stats, path, typed, depth)
	case primitive.D:
		children := append(primitive.D(nil), typed...)
		sort.SliceStable(children, func(i, j int) bool { return children[i].Key < children[j].Key })
		for _, child := range children {
			collectMongoFieldStats(stats, appendMongoPath(path, child.Key), child.Value, depth+1)
			if len(stats) >= mongoSchemaMaxFields {
				return
			}
		}
	case primitive.A:
		collectMongoArrayFields(stats, path, []interface{}(typed), depth)
	case []interface{}:
		collectMongoArrayFields(stats, path, typed, depth)
	}
}

func collectMongoDocumentFields(
	stats map[string]*mongoFieldStat,
	path []string,
	document map[string]interface{},
	depth int,
) {
	if len(path) == 0 {
		keys := make([]string, 0, len(document))
		for key := range document {
			if !looksLikeMongoDynamicKey(key) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			fieldPath := appendMongoPath(path, key)
			collectMongoFieldStats(stats, fieldPath, document[key], mongoSchemaMaxDepth)
		}
		for _, key := range keys {
			fieldPath := appendMongoPath(path, key)
			children := mongoDirectFields(fieldPath, document[key], 2)
			if len(children) > 0 && len(stats) < mongoSchemaMaxFields {
				collectMongoFieldStats(stats, children[0].path, children[0].value, mongoSchemaMaxDepth)
			}
		}
	}
	frontier := mongoDirectFields(path, map[string]interface{}(document), depth+1)
	for len(frontier) > 0 && len(stats) < mongoSchemaMaxFields {
		frontier = interleaveMongoSchemaFields(frontier)
		next := make([]mongoSchemaField, 0)
		for _, field := range frontier {
			if len(stats) >= mongoSchemaMaxFields {
				return
			}
			collectMongoFieldStats(stats, field.path, field.value, mongoSchemaMaxDepth)
			if field.depth < mongoSchemaMaxDepth {
				next = append(next, mongoDirectFields(field.path, field.value, field.depth+1)...)
			}
		}
		frontier = next
	}
}

func interleaveMongoSchemaFields(fields []mongoSchemaField) []mongoSchemaField {
	groups := make(map[string][]mongoSchemaField)
	for _, field := range fields {
		parent := strings.Join(field.path[:len(field.path)-1], ".")
		groups[parent] = append(groups[parent], field)
	}
	parents := make([]string, 0, len(groups))
	for parent := range groups {
		parents = append(parents, parent)
		sort.SliceStable(groups[parent], func(i, j int) bool {
			return strings.Join(groups[parent][i].path, ".") < strings.Join(groups[parent][j].path, ".")
		})
	}
	sort.Strings(parents)
	result := make([]mongoSchemaField, 0, len(fields))
	for index := 0; ; index++ {
		added := false
		for _, parent := range parents {
			if index < len(groups[parent]) {
				result = append(result, groups[parent][index])
				added = true
			}
		}
		if !added {
			return result
		}
	}
}

type mongoSchemaField struct {
	path  []string
	value interface{}
	depth int
}

func mongoDirectFields(path []string, value interface{}, depth int) []mongoSchemaField {
	fields := make([]mongoSchemaField, 0)
	appendField := func(key string, child interface{}) {
		if looksLikeMongoDynamicKey(key) {
			return
		}
		fields = append(fields, mongoSchemaField{path: appendMongoPath(path, key), value: child, depth: depth})
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			appendField(key, typed[key])
		}
	case bson.M:
		return mongoDirectFields(path, map[string]interface{}(typed), depth)
	case primitive.D:
		children := append(primitive.D(nil), typed...)
		sort.SliceStable(children, func(i, j int) bool { return children[i].Key < children[j].Key })
		for _, child := range children {
			appendField(child.Key, child.Value)
		}
	case primitive.A:
		limit := len(typed)
		if limit > mongoSchemaMaxArrayElements {
			limit = mongoSchemaMaxArrayElements
		}
		for _, item := range typed[:limit] {
			fields = append(fields, mongoDirectFields(path, item, depth)...)
		}
	case []interface{}:
		limit := len(typed)
		if limit > mongoSchemaMaxArrayElements {
			limit = mongoSchemaMaxArrayElements
		}
		for _, item := range typed[:limit] {
			fields = append(fields, mongoDirectFields(path, item, depth)...)
		}
	}
	return fields
}

func looksLikeMongoDynamicKey(key string) bool {
	if len(key) < 20 {
		return false
	}
	hasLetter := false
	hasDigit := false
	for _, char := range key {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z':
			hasLetter = true
		case char >= '0' && char <= '9':
			hasDigit = true
		case char == '-' || char == '_':
		default:
			return false
		}
	}
	return hasLetter && hasDigit
}

func collectMongoArrayFields(stats map[string]*mongoFieldStat, path []string, values []interface{}, depth int) {
	limit := len(values)
	if limit > mongoSchemaMaxArrayElements {
		limit = mongoSchemaMaxArrayElements
	}
	for _, value := range values[:limit] {
		collectMongoNestedFields(stats, path, value, depth)
		if len(stats) >= mongoSchemaMaxFields {
			return
		}
	}
}

func appendMongoPath(path []string, segment string) []string {
	result := make([]string, len(path), len(path)+1)
	copy(result, path)
	return append(result, segment)
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
	case bson.M, map[string]interface{}, primitive.D:
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

func mapMongoArrayElementType(bsonType string) datatype.FieldType {
	if bsonType == "" || bsonType == "null" {
		return ""
	}
	return mapMongoBSONType(bsonType)
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
