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

// EngineOrigin 返回引擎分类
func (p *Neo4jPlugin) EngineOrigin() string {
	return "general"
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

func (p *Neo4jPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewGraphCapabilities(p.Type())
}

func (p *Neo4jPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.GraphCatalogModel()
}

func (p *Neo4jPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *Neo4jPlugin) graphCatalogAdapter() plugin.GraphCatalogAdapter {
	return plugin.GraphCatalogAdapter{
		Plugin:                    p,
		ListDatabasesFunc:         p.listDatabases,
		ListNodeLabelsFunc:        p.listNodeLabels,
		ListRelationshipTypesFunc: p.listRelationshipTypes,
		IsSystemDatabaseFunc:      p.IsSystemDatabase,
	}
}

func (p *Neo4jPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return plugin.ListGraphCatalogChildren(ctx, p.graphCatalogAdapter(), parent.EngineID, connInfo, parent, opts)
}

func (p *Neo4jPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return plugin.ResolveGraphCatalogPath(ctx, p.graphCatalogAdapter(), path.EngineID, connInfo, path)
}

func (p *Neo4jPlugin) DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	return plugin.DescribeGraphItem(ctx, p, path.EngineID, connInfo, path, opts)
}

func (p *Neo4jPlugin) QueryLanguages() []string {
	return []string{"cypher"}
}

func (p *Neo4jPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return p.generateSampleQuery(ctx, connInfo)
}

func (p *Neo4jPlugin) ExecuteRuntimeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return p.executeQuery(ctx, connInfo, req.Query)
}

func (p *Neo4jPlugin) ExecuteRuntimeGraphQuery(ctx context.Context, connInfo plugin.ConnectionInfo, cypher string, opts plugin.QueryOptions) (*plugin.GraphQueryResult, error) {
	return p.executeGraphQuery(ctx, connInfo, cypher)
}

func (p *Neo4jPlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	query := opts.Query
	if query == "" {
		if len(path.Segments) == 0 {
			return nil, fmt.Errorf("Neo4j batch read requires item path or query")
		}
		label := escapeCypherLabel(path.Segments[len(path.Segments)-1].Name)
		limit := opts.Limit
		if limit <= 0 {
			limit = 1000
		}
		query = fmt.Sprintf("MATCH (n:`%s`) RETURN n LIMIT %d", label, limit)
	}
	result, err := p.executeQuery(ctx, connInfo, query)
	if err != nil {
		return nil, err
	}
	return plugin.QueryResultToBatchData(result, opts.Offset), nil
}

// ValidateConnectionInfo 验证连接信息
func (p *Neo4jPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// BuildDSN 构建 Bolt 连接字符串
func (p *Neo4jPlugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	parts := plugin.ParseDriverConnInfo(connInfo, p.DefaultPort(), "")
	if parts.Host == "" {
		return "", fmt.Errorf("missing required Neo4j connection info: host")
	}

	return fmt.Sprintf("bolt://%s:%d", parts.Host, parts.Port), nil
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

// createDriver 创建 Neo4j driver（内部辅助方法）
func (p *Neo4jPlugin) createDriver(ctx context.Context, connInfo plugin.ConnectionInfo) (neo4jdriver.DriverWithContext, error) {
	boltURI, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, err
	}

	parts := plugin.ParseDriverConnInfo(connInfo, p.DefaultPort(), "")

	auth := neo4jdriver.BasicAuth(parts.User, parts.Password, "")
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

// listDatabases lists databases for the catalog adapter.
// Neo4j CE 只有一个默认数据库 "neo4j"
func (p *Neo4jPlugin) listDatabases(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.DatabaseInfo, error) {
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

// IsSystemDatabase 判断是否为系统数据库
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

// listNodeLabels 列出所有节点标签。
func (p *Neo4jPlugin) listNodeLabels(ctx context.Context, connInfo plugin.ConnectionInfo, database string) ([]plugin.NodeLabelInfo, error) {
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

	result, err := session.Run(ctx, "CALL db.labels() YIELD label RETURN label ORDER BY label", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list Neo4j labels: %w", err)
	}

	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect Neo4j labels: %w", err)
	}

	labels := make([]plugin.NodeLabelInfo, 0, len(records))
	for _, record := range records {
		labelVal, ok := record.Get("label")
		if !ok {
			continue
		}
		label, ok := labelVal.(string)
		if !ok {
			continue
		}

		// 统计该 label 的节点数量
		countResult, err := session.Run(ctx,
			fmt.Sprintf("MATCH (n:`%s`) RETURN count(n) AS count", escapeCypherLabel(label)),
			nil,
		)
		var count int64
		if err == nil {
			if rec, err := countResult.Single(ctx); err == nil {
				if v, ok := rec.Get("count"); ok {
					count, _ = v.(int64)
				}
			}
		}

		labels = append(labels, plugin.NodeLabelInfo{Name: label, Count: count})
	}

	return labels, nil
}

// listRelationshipTypes 列出所有关系类型及连接统计。
func (p *Neo4jPlugin) listRelationshipTypes(ctx context.Context, connInfo plugin.ConnectionInfo, database string) ([]plugin.RelationshipTypeInfo, error) {
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

		if isInternalRelationshipType(relType) {
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
	labels, err := p.listNodeLabels(ctx, connInfo, database)
	if err != nil {
		return nil, fmt.Errorf("failed to get node labels: %w", err)
	}
	rels, err := p.listRelationshipTypes(ctx, connInfo, database)
	if err != nil {
		return nil, fmt.Errorf("failed to get relationship types: %w", err)
	}
	return &plugin.GraphSchema{NodeLabels: labels, Relationships: rels}, nil
}

// executeGraphQuery 执行 Cypher 查询并提取图数据。
func (p *Neo4jPlugin) executeGraphQuery(ctx context.Context, connInfo plugin.ConnectionInfo, query string) (*plugin.GraphQueryResult, error) {
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

// 查询数据库中第一个 Node Label，生成可执行的 Cypher 查询
func (p *Neo4jPlugin) generateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo) (string, string) {
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
func (p *Neo4jPlugin) executeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, query string) (*plugin.QueryResult, error) {
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
	for _, kw := range []string{"CREATE ", "MERGE ", "DELETE ", "SET ", "REMOVE ", "DROP ", "CALL "} {
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

func isInternalRelationshipType(relType string) bool {
	internalTypes := map[string]struct{}{
		"RTREE_METADATA":  {},
		"RTREE_REFERENCE": {},
		"RTREE_ROOT":      {},
	}
	_, ok := internalTypes[strings.ToUpper(relType)]
	return ok
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
