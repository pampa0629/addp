package neo4j

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Neo4jPlugin Neo4j 图数据库插件
type Neo4jPlugin struct{}

// init 函数在包被导入时自动注册插件
func init() {
	plugin.Register(&Neo4jPlugin{})
}

// Type 返回数据库类型标识
func (p *Neo4jPlugin) Type() string {
	return "neo4j"
}

// DisplayName 返回显示名称
func (p *Neo4jPlugin) DisplayName() string {
	return "Neo4j"
}

// EngineCategory 返回引擎分类
func (p *Neo4jPlugin) EngineCategory() string {
	return "standard"
}

// DefaultPort 返回默认端口（Bolt 协议）
func (p *Neo4jPlugin) DefaultPort() int {
	return 7687
}

// RequiredFields 返回必填字段列表
func (p *Neo4jPlugin) RequiredFields() []string {
	return []string{"host", "user", "password"}
}

// SensitiveFields 返回敏感字段列表
func (p *Neo4jPlugin) SensitiveFields() []string {
	return []string{"password"}
}

// GenerateCapabilities 生成资源能力描述
func (p *Neo4jPlugin) GenerateCapabilities() string {
	return `{"storage":[{"type":"graph_db","engine":"neo4j","supports_query":true}],"compute":[{"dev_modes":["query"],"description":"图数据库查询（Cypher）","features":["graph_algorithms","knowledge_graph","cypher_query","property_graph"]}]}`
}

// ValidateConnectionInfo 验证连接信息
func (p *Neo4jPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// BuildConnectionString 构建 Bolt 连接字符串
func (p *Neo4jPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	if port == 0 {
		port = p.DefaultPort()
	}

	if host == "" {
		return "", fmt.Errorf("missing required Neo4j connection info: host")
	}

	return fmt.Sprintf("bolt://%s:%d", host, port), nil
}

// TestConnection 测试 Neo4j 连接
func (p *Neo4jPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	driver, err := p.createDriver(ctx, connInfo)
	if err != nil {
		return err
	}
	defer driver.Close(ctx)

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 执行需要认证的 Cypher 查询，而不是仅做连接性检查（VerifyConnectivity 不验证凭据）
	session := driver.NewSession(testCtx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: getDatabase(connInfo),
	})
	defer session.Close(testCtx)

	_, err = session.Run(testCtx, "RETURN 1 AS n", nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Neo4j: %w", err)
	}

	return nil
}

// SupportsMetadataQuery 实现 StoragePlugin 接口
func (p *Neo4jPlugin) SupportsMetadataQuery() bool {
	return true
}

// createDriver 创建 Neo4j driver（内部辅助方法）
func (p *Neo4jPlugin) createDriver(ctx context.Context, connInfo plugin.ConnectionInfo) (neo4jdriver.DriverWithContext, error) {
	boltURI, err := p.BuildConnectionString(connInfo)
	if err != nil {
		return nil, err
	}

	user := plugin.GetString(connInfo, "user")
	if user == "" {
		user = plugin.GetString(connInfo, "username")
	}
	password := plugin.GetString(connInfo, "password")

	auth := neo4jdriver.BasicAuth(user, password, "")
	driver, err := neo4jdriver.NewDriverWithContext(boltURI, auth)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	return driver, nil
}

// getDatabase 获取目标数据库名称，默认为 "neo4j"
func getDatabase(connInfo plugin.ConnectionInfo) string {
	db := plugin.GetString(connInfo, "database")
	if db == "" {
		return "neo4j"
	}
	return db
}

// ListDatabases 实现 NoSQLPlugin 接口 - 列出所有数据库
// Neo4j CE 只有一个默认数据库 "neo4j"
func (p *Neo4jPlugin) ListDatabases(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.DatabaseInfo, error) {
	driver, err := p.createDriver(ctx, connInfo)
	if err != nil {
		return nil, err
	}
	defer driver.Close(ctx)

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode: neo4jdriver.AccessModeRead,
	})
	defer session.Close(ctx)

	// 尝试 SHOW DATABASES（企业版支持），CE 版会返回单个数据库
	result, err := session.Run(ctx, "SHOW DATABASES YIELD name, currentStatus WHERE currentStatus = 'online' RETURN name", nil)
	if err != nil {
		// CE 版可能不支持 SHOW DATABASES，直接返回默认数据库
		dbName := getDatabase(connInfo)
		return []plugin.DatabaseInfo{{Name: dbName}}, nil
	}

	records, err := result.Collect(ctx)
	if err != nil || len(records) == 0 {
		dbName := getDatabase(connInfo)
		return []plugin.DatabaseInfo{{Name: dbName}}, nil
	}

	databases := make([]plugin.DatabaseInfo, 0, len(records))
	for _, record := range records {
		name, ok := record.Get("name")
		if !ok {
			continue
		}
		dbName, ok := name.(string)
		if !ok || p.IsSystemDatabase(dbName) {
			continue
		}
		databases = append(databases, plugin.DatabaseInfo{Name: dbName})
	}

	if len(databases) == 0 {
		dbName := getDatabase(connInfo)
		databases = []plugin.DatabaseInfo{{Name: dbName}}
	}

	return databases, nil
}

// ListCollections 实现 NoSQLPlugin 接口 - 列出所有 Node Label（相当于"表"）
func (p *Neo4jPlugin) ListCollections(ctx context.Context, connInfo plugin.ConnectionInfo, database string) ([]plugin.CollectionInfo, error) {
	driver, err := p.createDriver(ctx, connInfo)
	if err != nil {
		return nil, err
	}
	defer driver.Close(ctx)

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: database,
	})
	defer session.Close(ctx)

	// 获取所有 Node Label
	result, err := session.Run(ctx, "CALL db.labels() YIELD label RETURN label ORDER BY label", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list Neo4j labels: %w", err)
	}

	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect Neo4j labels: %w", err)
	}

	collections := make([]plugin.CollectionInfo, 0, len(records))
	for _, record := range records {
		labelVal, ok := record.Get("label")
		if !ok {
			continue
		}
		label, ok := labelVal.(string)
		if !ok {
			continue
		}

		// 获取该 label 的节点数量
		stats, err := p.GetCollectionStats(ctx, connInfo, database, label)
		if err != nil {
			collections = append(collections, plugin.CollectionInfo{
				Database:      database,
				Name:          label,
				DocumentCount: 0,
			})
			continue
		}

		collections = append(collections, plugin.CollectionInfo{
			Database:      database,
			Name:          label,
			DocumentCount: stats.DocumentCount,
			SizeBytes:     stats.SizeBytes,
		})
	}

	return collections, nil
}

// GetCollectionStats 实现 NoSQLPlugin 接口 - 获取指定 Label 的统计信息
func (p *Neo4jPlugin) GetCollectionStats(ctx context.Context, connInfo plugin.ConnectionInfo, database, label string) (*plugin.CollectionStats, error) {
	driver, err := p.createDriver(ctx, connInfo)
	if err != nil {
		return nil, err
	}
	defer driver.Close(ctx)

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: database,
	})
	defer session.Close(ctx)

	// 统计该 Label 节点数量
	result, err := session.Run(ctx,
		fmt.Sprintf("MATCH (n:`%s`) RETURN count(n) AS count", label),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to count nodes for label %s: %w", label, err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return &plugin.CollectionStats{DocumentCount: 0, Indexes: []plugin.IndexInfo{}}, nil
	}

	countVal, _ := record.Get("count")
	count, _ := countVal.(int64)

	return &plugin.CollectionStats{
		DocumentCount: count,
		SizeBytes:     0,
		IndexCount:    0,
		AvgDocSize:    0,
		Indexes:       []plugin.IndexInfo{},
	}, nil
}

// IsSystemDatabase 实现 NoSQLPlugin 接口 - 判断是否为系统数据库
func (p *Neo4jPlugin) IsSystemDatabase(databaseName string) bool {
	// Neo4j 系统数据库
	systemDatabases := []string{"system"}
	for _, sysDB := range systemDatabases {
		if databaseName == sysDB {
			return true
		}
	}
	return false
}

// ============ GraphDBPlugin 接口实现 ============

// ListNodeLabels 实现 GraphDBPlugin 接口 - 列出所有节点标签
func (p *Neo4jPlugin) ListNodeLabels(ctx context.Context, connInfo plugin.ConnectionInfo, database string) ([]plugin.NodeLabelInfo, error) {
	collections, err := p.ListCollections(ctx, connInfo, database)
	if err != nil {
		return nil, err
	}
	labels := make([]plugin.NodeLabelInfo, len(collections))
	for i, c := range collections {
		labels[i] = plugin.NodeLabelInfo{Name: c.Name, Count: c.DocumentCount}
	}
	return labels, nil
}

// ListRelationshipTypes 实现 GraphDBPlugin 接口 - 列出所有关系类型及连接统计
func (p *Neo4jPlugin) ListRelationshipTypes(ctx context.Context, connInfo plugin.ConnectionInfo, database string) ([]plugin.RelationshipTypeInfo, error) {
	driver, err := p.createDriver(ctx, connInfo)
	if err != nil {
		return nil, err
	}
	defer driver.Close(ctx) //nolint:errcheck

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: database,
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "CALL db.relationshipTypes() YIELD relationshipType RETURN relationshipType ORDER BY relationshipType", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list Neo4j relationship types: %w", err)
	}

	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect Neo4j relationship types: %w", err)
	}

	relTypes := make([]plugin.RelationshipTypeInfo, 0, len(records))
	for _, record := range records {
		relTypeVal, ok := record.Get("relationshipType")
		if !ok {
			continue
		}
		relType, ok := relTypeVal.(string)
		if !ok {
			continue
		}

		info := plugin.RelationshipTypeInfo{Name: relType}

		// 查询该关系类型的起始/终止标签和总数（最多取5种组合）
		statsResult, err := session.Run(ctx,
			fmt.Sprintf(`MATCH (a)-[r:%s]->(b)
WITH labels(a)[0] AS from, labels(b)[0] AS to, count(r) AS cnt
RETURN from, to, cnt ORDER BY cnt DESC LIMIT 5`, escapeCypherLabel(relType)),
			nil,
		)
		if err == nil {
			statsRecords, _ := statsResult.Collect(ctx)
			fromSet := make(map[string]struct{})
			toSet := make(map[string]struct{})
			for _, sr := range statsRecords {
				fromVal, _ := sr.Get("from")
				toVal, _ := sr.Get("to")
				cntVal, _ := sr.Get("cnt")
				if from, ok := fromVal.(string); ok && from != "" {
					fromSet[from] = struct{}{}
				}
				if to, ok := toVal.(string); ok && to != "" {
					toSet[to] = struct{}{}
				}
				if cnt, ok := cntVal.(int64); ok {
					info.Count += cnt
				}
			}
			for from := range fromSet {
				info.FromLabels = append(info.FromLabels, from)
			}
			for to := range toSet {
				info.ToLabels = append(info.ToLabels, to)
			}
		}

		relTypes = append(relTypes, info)
	}

	return relTypes, nil
}

// GetGraphSchema 实现 GraphDBPlugin 接口 - 获取图数据库完整 Schema
func (p *Neo4jPlugin) GetGraphSchema(ctx context.Context, connInfo plugin.ConnectionInfo, database string) (*plugin.GraphSchema, error) {
	labels, err := p.ListNodeLabels(ctx, connInfo, database)
	if err != nil {
		return nil, fmt.Errorf("failed to get node labels: %w", err)
	}
	rels, err := p.ListRelationshipTypes(ctx, connInfo, database)
	if err != nil {
		return nil, fmt.Errorf("failed to get relationship types: %w", err)
	}
	return &plugin.GraphSchema{NodeLabels: labels, Relationships: rels}, nil
}

// ============ GraphQueryPlugin 接口实现 ============

// ExecuteGraphQuery 实现 GraphQueryPlugin 接口 - 执行 Cypher 查询并提取图数据
func (p *Neo4jPlugin) ExecuteGraphQuery(ctx context.Context, connInfo plugin.ConnectionInfo, query string) (*plugin.GraphQueryResult, error) {
	driver, err := p.createDriver(ctx, connInfo)
	if err != nil {
		return nil, err
	}
	defer driver.Close(ctx) //nolint:errcheck

	routing := neo4jdriver.ExecuteQueryWithReadersRouting()
	if isCypherWriteQuery(query) {
		routing = neo4jdriver.ExecuteQueryWithWritersRouting()
	}

	result, err := neo4jdriver.ExecuteQuery(
		ctx,
		driver,
		query,
		nil,
		neo4jdriver.EagerResultTransformer,
		neo4jdriver.ExecuteQueryWithDatabase(getDatabase(connInfo)),
		routing,
	)
	if err != nil {
		return nil, fmt.Errorf("执行 Cypher 失败：%w", err)
	}

	if len(result.Records) == 0 {
		cols := []string{"nodes_created", "nodes_deleted", "relationships_created", "relationships_deleted", "properties_set"}
		row := map[string]interface{}{
			"nodes_created":         result.Summary.Counters().NodesCreated(),
			"nodes_deleted":         result.Summary.Counters().NodesDeleted(),
			"relationships_created": result.Summary.Counters().RelationshipsCreated(),
			"relationships_deleted": result.Summary.Counters().RelationshipsDeleted(),
			"properties_set":        result.Summary.Counters().PropertiesSet(),
		}
		return &plugin.GraphQueryResult{
			QueryResult: plugin.QueryResult{Columns: cols, Rows: []map[string]interface{}{row}},
		}, nil
	}

	// 同时收集图数据和表格数据
	nodeMap := make(map[string]plugin.GraphNode)
	relMap := make(map[string]plugin.GraphRelationship)

	columns := result.Records[0].Keys
	rows := make([]map[string]interface{}, len(result.Records))
	for i, record := range result.Records {
		row := make(map[string]interface{}, len(columns))
		for _, col := range columns {
			val, _ := record.Get(col)
			extractGraphElements(val, nodeMap, relMap)
			row[col] = convertNeo4jValue(val)
		}
		rows[i] = row
	}

	var graphData *plugin.GraphData
	if len(nodeMap) > 0 || len(relMap) > 0 {
		nodes := make([]plugin.GraphNode, 0, len(nodeMap))
		for _, n := range nodeMap {
			nodes = append(nodes, n)
		}
		rels := make([]plugin.GraphRelationship, 0, len(relMap))
		for _, r := range relMap {
			rels = append(rels, r)
		}
		graphData = &plugin.GraphData{Nodes: nodes, Relationships: rels}
	}

	return &plugin.GraphQueryResult{
		QueryResult: plugin.QueryResult{Columns: columns, Rows: rows},
		GraphData:   graphData,
	}, nil
}

// extractGraphElements 从 Neo4j 值中递归提取节点和关系，用于图数据收集
func extractGraphElements(v interface{}, nodes map[string]plugin.GraphNode, rels map[string]plugin.GraphRelationship) {
	if v == nil {
		return
	}
	switch val := v.(type) {
	case neo4jdriver.Node:
		if _, exists := nodes[val.ElementId]; !exists {
			props := make(map[string]interface{}, len(val.Props))
			for k, p := range val.Props {
				props[k] = convertNeo4jValue(p)
			}
			nodes[val.ElementId] = plugin.GraphNode{
				ElementId:  val.ElementId,
				Labels:     val.Labels,
				Properties: props,
			}
		}
	case neo4jdriver.Relationship:
		if _, exists := rels[val.ElementId]; !exists {
			props := make(map[string]interface{}, len(val.Props))
			for k, p := range val.Props {
				props[k] = convertNeo4jValue(p)
			}
			rels[val.ElementId] = plugin.GraphRelationship{
				ElementId:   val.ElementId,
				Type:        val.Type,
				StartNodeId: val.StartElementId,
				EndNodeId:   val.EndElementId,
				Properties:  props,
			}
		}
	case neo4jdriver.Path:
		for _, n := range val.Nodes {
			extractGraphElements(n, nodes, rels)
		}
		for _, r := range val.Relationships {
			extractGraphElements(r, nodes, rels)
		}
	case []interface{}:
		for _, elem := range val {
			extractGraphElements(elem, nodes, rels)
		}
	}
}

// CreateClient 实现 NoSQLPlugin 接口 - 创建 Neo4j driver 客户端
func (p *Neo4jPlugin) CreateClient(ctx context.Context, connInfo plugin.ConnectionInfo) (interface{}, error) {
	driver, err := p.createDriver(ctx, connInfo)
	if err != nil {
		return nil, err
	}

	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := driver.VerifyConnectivity(verifyCtx); err != nil {
		driver.Close(ctx)
		return nil, fmt.Errorf("failed to verify Neo4j connectivity: %w", err)
	}

	return driver, nil
}

// CloseClient 实现 NoSQLPlugin 接口 - 关闭 Neo4j driver 客户端
func (p *Neo4jPlugin) CloseClient(ctx context.Context, client interface{}) error {
	if client == nil {
		return nil
	}

	driver, ok := client.(neo4jdriver.DriverWithContext)
	if !ok {
		return fmt.Errorf("invalid client type: expected neo4j.DriverWithContext")
	}

	return driver.Close(ctx)
}

// GenerateSampleQuery 实现 QueryablePlugin 接口
// 查询数据库中第一个 Node Label，生成可执行的 Cypher 查询
func (p *Neo4jPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo) (string, string) {
	const fallback = "MATCH (n)\nRETURN n\nLIMIT 10"

	driver, err := p.createDriver(ctx, connInfo)
	if err != nil {
		return fallback, "cypher"
	}
	defer driver.Close(ctx) //nolint:errcheck

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: getDatabase(connInfo),
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "CALL db.labels() YIELD label RETURN label LIMIT 1", nil)
	if err != nil {
		return fallback, "cypher"
	}

	records, err := result.Collect(ctx)
	if err != nil || len(records) == 0 {
		return fallback, "cypher"
	}

	labelVal, ok := records[0].Get("label")
	if !ok {
		return fallback, "cypher"
	}
	label, ok := labelVal.(string)
	if !ok {
		return fallback, "cypher"
	}

	return fmt.Sprintf("MATCH (n:%s)\nRETURN n\nLIMIT 10", label), "cypher"
}
// query 为 Cypher 字符串，如 MATCH (n:Person) RETURN n.name, n.age LIMIT 10
// 写操作（CREATE/MERGE/DELETE/SET/REMOVE/DROP）自动使用写路由，其余使用读路由
func (p *Neo4jPlugin) ExecuteQuery(ctx context.Context, connInfo plugin.ConnectionInfo, query string) (*plugin.QueryResult, error) {
	driver, err := p.createDriver(ctx, connInfo)
	if err != nil {
		return nil, err
	}
	defer driver.Close(ctx) //nolint:errcheck

	routing := neo4jdriver.ExecuteQueryWithReadersRouting()
	if isCypherWriteQuery(query) {
		routing = neo4jdriver.ExecuteQueryWithWritersRouting()
	}

	result, err := neo4jdriver.ExecuteQuery(
		ctx,
		driver,
		query,
		nil,
		neo4jdriver.EagerResultTransformer,
		neo4jdriver.ExecuteQueryWithDatabase(getDatabase(connInfo)),
		routing,
	)
	if err != nil {
		return nil, fmt.Errorf("执行 Cypher 失败：%w", err)
	}

	if len(result.Records) == 0 {
		// 写操作或空结果：返回影响计数
		cols := []string{"nodes_created", "nodes_deleted", "relationships_created", "relationships_deleted", "properties_set"}
		row := map[string]interface{}{
			"nodes_created":         result.Summary.Counters().NodesCreated(),
			"nodes_deleted":         result.Summary.Counters().NodesDeleted(),
			"relationships_created": result.Summary.Counters().RelationshipsCreated(),
			"relationships_deleted": result.Summary.Counters().RelationshipsDeleted(),
			"properties_set":        result.Summary.Counters().PropertiesSet(),
		}
		return &plugin.QueryResult{Columns: cols, Rows: []map[string]interface{}{row}}, nil
	}

	columns := result.Records[0].Keys
	rows := make([]map[string]interface{}, len(result.Records))
	for i, record := range result.Records {
		row := make(map[string]interface{}, len(columns))
		for _, col := range columns {
			val, _ := record.Get(col)
			row[col] = convertNeo4jValue(val)
		}
		rows[i] = row
	}

	return &plugin.QueryResult{Columns: columns, Rows: rows}, nil
}

// isCypherWriteQuery 判断 Cypher 是否包含写操作关键字
func isCypherWriteQuery(cypher string) bool {
	upper := strings.ToUpper(strings.TrimSpace(cypher))
	for _, kw := range []string{"CREATE ", "MERGE ", "DELETE ", "SET ", "REMOVE ", "DROP "} {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}

// escapeCypherLabel 转义 Cypher 标签/关系类型中的反引号，防止注入
func escapeCypherLabel(label string) string {
	return strings.ReplaceAll(label, "`", "``")
}

// convertNeo4jValue 将 Neo4j 值转为 JSON 友好的 Go 类型
func convertNeo4jValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case neo4jdriver.Node:
		props := make(map[string]interface{}, len(val.Props))
		for k, p := range val.Props {
			props[k] = convertNeo4jValue(p)
		}
		return map[string]interface{}{
			"id":         val.ElementId,
			"labels":     val.Labels,
			"properties": props,
		}
	case neo4jdriver.Relationship:
		props := make(map[string]interface{}, len(val.Props))
		for k, p := range val.Props {
			props[k] = convertNeo4jValue(p)
		}
		return map[string]interface{}{
			"id":         val.ElementId,
			"type":       val.Type,
			"properties": props,
		}
	case neo4jdriver.Path:
		nodes := make([]interface{}, len(val.Nodes))
		for i, n := range val.Nodes {
			nodes[i] = convertNeo4jValue(n)
		}
		rels := make([]interface{}, len(val.Relationships))
		for i, r := range val.Relationships {
			rels[i] = convertNeo4jValue(r)
		}
		return map[string]interface{}{
			"length":        len(val.Nodes) - 1,
			"nodes":         nodes,
			"relationships": rels,
		}
	case []interface{}:
		arr := make([]interface{}, len(val))
		for i, elem := range val {
			arr[i] = convertNeo4jValue(elem)
		}
		return arr
	case map[string]interface{}:
		converted := make(map[string]interface{}, len(val))
		for k, elem := range val {
			converted[k] = convertNeo4jValue(elem)
		}
		return converted
	default:
		return val
	}
}

