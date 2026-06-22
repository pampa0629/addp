package neo4j

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/datatype"
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

func (p *Neo4jPlugin) graphCatalogCallbacks() plugin.GraphCatalogCallbacks {
	return plugin.GraphCatalogCallbacks{
		ListNamespacesFunc:   p.listDatabases,
		DescribeGraphFunc:    p.describeGraph,
		IsSystemDatabaseFunc: p.IsSystemDatabase,
	}
}

func (p *Neo4jPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	return plugin.ListGraphCatalogChildren(ctx, p.graphCatalogCallbacks(), parent.EngineID, connInfo, parent, opts)
}

func (p *Neo4jPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return plugin.ResolveGraphCatalogPath(ctx, p.graphCatalogCallbacks(), path.EngineID, connInfo, path)
}

func (p *Neo4jPlugin) DescribeCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	return plugin.DescribeGraphCatalogFacts(ctx, p.graphCatalogCallbacks(), path.EngineID, connInfo, path, opts)
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

func (p *Neo4jPlugin) ExecuteGraphQuery(ctx context.Context, connInfo plugin.ConnectionInfo, cypher string, opts plugin.QueryOptions) (*plugin.GraphQueryResult, error) {
	return p.executeGraphQuery(ctx, connInfo, cypher)
}

func (p *Neo4jPlugin) SampleGraph(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.GraphSampleOptions) (*plugin.GraphData, error) {
	database := getDatabase(connInfo)
	segments := plugin.CatalogPathWithoutRoot(path).Segments
	if len(segments) > 0 && segments[0].Name != "" {
		database = segments[0].Name
	}
	sampleConn := cloneConnectionInfo(connInfo)
	sampleConn["database"] = database
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := sampleGraphQuery(opts.Filter, limit)
	result, err := p.executeGraphQuery(ctx, sampleConn,
		query)
	if err != nil {
		return nil, err
	}
	if result.GraphData != nil && (len(result.GraphData.Nodes) > 0 || len(result.GraphData.Relationships) > 0) {
		return result.GraphData, nil
	}
	if isFilteredGraphSample(opts.Filter) {
		return &plugin.GraphData{}, nil
	}
	result, err = p.executeGraphQuery(ctx, sampleConn, sampleGraphNodeFallbackQuery(limit))
	if err != nil {
		return nil, err
	}
	if result.GraphData == nil {
		return &plugin.GraphData{}, nil
	}
	return result.GraphData, nil
}

func sampleGraphQuery(filter plugin.GraphSampleFilter, limit int) string {
	filter = filter.Clone()
	switch filter.Kind {
	case plugin.GraphSampleKindNodeShape:
		labels := filter.Labels
		if isInternalNodeLabelSet(labels) {
			return fmt.Sprintf("MATCH (n) WHERE false RETURN n LIMIT %d", limit)
		}
		return fmt.Sprintf("MATCH (n%s) RETURN n LIMIT %d", cypherNodeLabels(labels), limit)
	case plugin.GraphSampleKindRelationshipShape:
		relType := filter.RelationshipType
		if relType == "" || isInternalRelationshipType(relType) {
			return fmt.Sprintf("MATCH (n)-[r]->(m) WHERE false RETURN n, r, m LIMIT %d", limit)
		}
		return fmt.Sprintf("MATCH (n%s)-[r:%s]->(m%s) RETURN n, r, m LIMIT %d",
			cypherNodeLabels(filter.FromLabels),
			cypherIdentifier(relType),
			cypherNodeLabels(filter.ToLabels),
			limit)
	default:
		return fmt.Sprintf("MATCH (n)-[r]->(m) WHERE NOT type(r) IN ['RTREE_METADATA', 'RTREE_REFERENCE', 'RTREE_ROOT'] RETURN n, r, m LIMIT %d", limit)
	}
}

func sampleGraphNodeFallbackQuery(limit int) string {
	return fmt.Sprintf("MATCH (n) WHERE NOT %s RETURN n LIMIT %d", cypherInternalNodePredicate("n"), limit)
}

func isFilteredGraphSample(filter plugin.GraphSampleFilter) bool {
	return !filter.IsZero()
}

func cypherNodeLabels(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	return ":" + strings.Join(escapeCypherLabels(labels), ":")
}

func cypherIdentifier(name string) string {
	return "`" + escapeCypherLabel(name) + "`"
}

func cloneConnectionInfo(connInfo plugin.ConnectionInfo) plugin.ConnectionInfo {
	cloned := make(plugin.ConnectionInfo, len(connInfo))
	for key, value := range connInfo {
		cloned[key] = value
	}
	return cloned
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

// listDatabases lists databases for the catalog callbacks.
// Neo4j CE 只有一个默认数据库 "neo4j"
func (p *Neo4jPlugin) listDatabases(ctx context.Context, connInfo plugin.ConnectionInfo, root plugin.CatalogPath) ([]plugin.CatalogEntry, error) {
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
		return []plugin.CatalogEntry{neo4jDatabaseCatalogEntry(root, dbName)}, nil
	}

	records, err := result.Collect(ctx)
	if err != nil || len(records) == 0 {
		dbName := getDatabase(connInfo)
		return []plugin.CatalogEntry{neo4jDatabaseCatalogEntry(root, dbName)}, nil
	}

	databases := make([]plugin.CatalogEntry, 0, len(records))
	for _, record := range records {
		name, ok := record.Get("name")
		if !ok {
			continue
		}
		dbName, ok := name.(string)
		if !ok || p.IsSystemDatabase(dbName) {
			continue
		}
		databases = append(databases, neo4jDatabaseCatalogEntry(root, dbName))
	}

	if len(databases) == 0 {
		dbName := getDatabase(connInfo)
		databases = []plugin.CatalogEntry{neo4jDatabaseCatalogEntry(root, dbName)}
	}

	return databases, nil
}

func neo4jDatabaseCatalogEntry(root plugin.CatalogPath, name string) plugin.CatalogEntry {
	return plugin.CatalogEntry{
		Name: name,
		Path: plugin.CatalogPath{
			Version:  root.Version,
			EngineID: root.EngineID,
			Segments: append(append([]plugin.CatalogSegment(nil), root.Segments...), plugin.CatalogSegment{
				Term: plugin.CatalogTermDatabase,
				Kind: plugin.CatalogKindNamespace,
				Name: name,
			}),
		},
		Term: plugin.CatalogTermDatabase,
		Kind: plugin.CatalogKindNamespace,
		Role: plugin.CatalogRoleBranch,
	}
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

func (p *Neo4jPlugin) describeGraph(ctx context.Context, connInfo plugin.ConnectionInfo, database string, opts plugin.CatalogFactsOptions) (*datatype.GraphInfo, error) {
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

	nodeShapes, nodeCount, err := p.describeNodeShapes(ctx, session)
	if err != nil {
		return nil, err
	}
	nodeCount, err = p.countNodes(ctx, session)
	if err != nil {
		return nil, err
	}
	relationshipShapes, err := p.describeRelationshipShapes(ctx, session)
	if err != nil {
		return nil, err
	}
	relationshipCount, err := p.countRelationships(ctx, session)
	if err != nil {
		return nil, err
	}
	directed := true
	return &datatype.GraphInfo{
		Model:              datatype.GraphModelPropertyGraph,
		Directed:           &directed,
		NodeCount:          &nodeCount,
		RelationshipCount:  &relationshipCount,
		NodeShapes:         nodeShapes,
		RelationshipShapes: relationshipShapes,
	}, nil
}

// describeNodeShapes lists observed node label sets as node shapes.
func (p *Neo4jPlugin) describeNodeShapes(ctx context.Context, session neo4jdriver.SessionWithContext) ([]datatype.GraphNodeShapeInfo, int64, error) {
	result, err := session.Run(ctx,
		fmt.Sprintf(`MATCH (n)
WITH labels(n) AS labels
WHERE NOT %s
RETURN labels, count(*) AS count ORDER BY count DESC LIMIT 500`, cypherInternalLabelsPredicate("labels")),
		nil,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list Neo4j node shapes: %w", err)
	}

	records, err := result.Collect(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to collect Neo4j node shapes: %w", err)
	}

	nodeShapes := make([]datatype.GraphNodeShapeInfo, 0, len(records))
	var total int64
	for _, record := range records {
		labelsVal, ok := record.Get("labels")
		if !ok {
			continue
		}
		labels := neo4jStringSlice(labelsVal)
		if len(labels) == 0 || isInternalNodeLabelSet(labels) {
			continue
		}
		countVal, _ := record.Get("count")
		var count int64
		if v, ok := countVal.(int64); ok {
			count = v
		}

		properties, err := p.describeNodeShapeProperties(ctx, session, labels)
		if err != nil {
			return nil, 0, err
		}

		total += count
		nodeShapes = append(nodeShapes, datatype.GraphNodeShapeInfo{
			Name:       graphEndpointShapeName(labels),
			Kind:       graphNodeShapeKind(labels),
			Labels:     labels,
			Properties: properties,
			Count:      &count,
		})
	}

	return nodeShapes, total, nil
}

func graphNodeShapeKind(labels []string) string {
	if len(labels) == 1 {
		return datatype.GraphNodeShapeKindLabel
	}
	return datatype.GraphNodeShapeKindLabelSet
}

func (p *Neo4jPlugin) countNodes(ctx context.Context, session neo4jdriver.SessionWithContext) (int64, error) {
	result, err := session.Run(ctx, fmt.Sprintf("MATCH (n) WHERE NOT %s RETURN count(n) AS count", cypherInternalNodePredicate("n")), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to count Neo4j nodes: %w", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to collect Neo4j node count: %w", err)
	}
	countVal, _ := record.Get("count")
	count, _ := countVal.(int64)
	return count, nil
}

func (p *Neo4jPlugin) describeNodeShapeProperties(ctx context.Context, session neo4jdriver.SessionWithContext, labels []string) ([]datatype.FieldInfo, error) {
	result, err := session.Run(ctx,
		fmt.Sprintf(`MATCH (n:%s)
UNWIND keys(n) AS property
WITH property, head([value IN collect(n[property]) WHERE value IS NOT NULL]) AS sample
RETURN property, valueType(sample) AS native_type
ORDER BY property`, strings.Join(escapeCypherLabels(labels), ":")),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list Neo4j node properties for shape %s: %w", graphEndpointShapeName(labels), err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect Neo4j node properties for shape %s: %w", graphEndpointShapeName(labels), err)
	}
	properties := make([]datatype.FieldInfo, 0, len(records))
	for i, record := range records {
		nameVal, _ := record.Get("property")
		name, _ := nameVal.(string)
		if name == "" {
			continue
		}
		nativeVal, _ := record.Get("native_type")
		nativeType, _ := nativeVal.(string)
		properties = append(properties, datatype.FieldInfo{
			Name:            name,
			Type:            neo4jValueTypeToFieldType(nativeType),
			NativeType:      nativeType,
			OrdinalPosition: i + 1,
		})
	}
	return properties, nil
}

func neo4jValueTypeToFieldType(nativeType string) datatype.FieldType {
	switch strings.ToUpper(strings.TrimSpace(nativeType)) {
	case "BOOLEAN":
		return datatype.FieldTypeBool
	case "INTEGER":
		return datatype.FieldTypeBigInt
	case "FLOAT":
		return datatype.FieldTypeDouble
	case "STRING":
		return datatype.FieldTypeString
	case "BYTES":
		return datatype.FieldTypeBytes
	case "DATE":
		return datatype.FieldTypeDate
	case "LOCAL TIME", "ZONED TIME":
		return datatype.FieldTypeTime
	case "LOCAL DATETIME", "ZONED DATETIME":
		return datatype.FieldTypeTimestamp
	case "LIST":
		return datatype.FieldTypeArray
	case "MAP":
		return datatype.FieldTypeJSON
	case "POINT":
		return datatype.FieldTypeGeometry
	default:
		return datatype.FieldTypeUnknown
	}
}

// describeRelationshipShapes 列出所有关系类型及连接模式。
func (p *Neo4jPlugin) describeRelationshipShapes(ctx context.Context, session neo4jdriver.SessionWithContext) ([]datatype.GraphRelationshipShapeInfo, error) {
	result, err := session.Run(ctx, "CALL db.relationshipTypes() YIELD relationshipType RETURN relationshipType ORDER BY relationshipType", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list Neo4j relationship types: %w", err)
	}

	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect Neo4j relationship types: %w", err)
	}

	relationshipShapes := make([]datatype.GraphRelationshipShapeInfo, 0, len(records))
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

		shape := datatype.GraphRelationshipShapeInfo{Type: relType}
		shapeCount, err := p.countRelationshipType(ctx, session, relType)
		if err != nil {
			return nil, err
		}
		shape.Count = &shapeCount

		// 查询该关系类型的起始/终止 label set 组合（最多取 20 种组合）。
		statsResult, err := session.Run(ctx,
			fmt.Sprintf(`MATCH (a)-[r:%s]->(b)
WITH labels(a) AS from, labels(b) AS to, count(r) AS cnt
RETURN from, to, cnt ORDER BY cnt DESC LIMIT 20`, cypherIdentifier(relType)),
			nil,
		)
		if err == nil {
			statsRecords, _ := statsResult.Collect(ctx)
			for _, sr := range statsRecords {
				fromVal, _ := sr.Get("from")
				toVal, _ := sr.Get("to")
				cntVal, _ := sr.Get("cnt")
				count, _ := cntVal.(int64)
				fromLabels := neo4jStringSlice(fromVal)
				toLabels := neo4jStringSlice(toVal)
				shape.Patterns = append(shape.Patterns, datatype.GraphRelationshipPatternInfo{
					From:  datatype.GraphEndpointInfo{ShapeName: graphEndpointShapeName(fromLabels), Labels: fromLabels},
					To:    datatype.GraphEndpointInfo{ShapeName: graphEndpointShapeName(toLabels), Labels: toLabels},
					Count: &count,
				})
			}
		}

		relationshipShapes = append(relationshipShapes, shape)
	}

	return relationshipShapes, nil
}

func (p *Neo4jPlugin) countRelationships(ctx context.Context, session neo4jdriver.SessionWithContext) (int64, error) {
	result, err := session.Run(ctx, "MATCH ()-[r]->() WHERE NOT type(r) IN ['RTREE_METADATA', 'RTREE_REFERENCE', 'RTREE_ROOT'] RETURN count(r) AS count", nil)
	if err != nil {
		return 0, fmt.Errorf("failed to count Neo4j relationships: %w", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to collect Neo4j relationship count: %w", err)
	}
	countVal, _ := record.Get("count")
	count, _ := countVal.(int64)
	return count, nil
}

func (p *Neo4jPlugin) countRelationshipType(ctx context.Context, session neo4jdriver.SessionWithContext, relType string) (int64, error) {
	result, err := session.Run(ctx, fmt.Sprintf("MATCH ()-[r:%s]->() RETURN count(r) AS count", cypherIdentifier(relType)), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to count Neo4j relationship type %s: %w", relType, err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to collect Neo4j relationship type %s count: %w", relType, err)
	}
	countVal, _ := record.Get("count")
	count, _ := countVal.(int64)
	return count, nil
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

func escapeCypherLabels(labels []string) []string {
	escaped := make([]string, 0, len(labels))
	for _, label := range labels {
		escaped = append(escaped, cypherIdentifier(label))
	}
	return escaped
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

func isInternalNodeLabelSet(labels []string) bool {
	for _, label := range labels {
		if isInternalNodeLabel(label) {
			return true
		}
	}
	return false
}

func isInternalNodeLabel(label string) bool {
	switch strings.ToUpper(strings.TrimSpace(label)) {
	case "SPATIALLAYER":
		return true
	default:
		return false
	}
}

func cypherInternalNodePredicate(nodeAlias string) string {
	return cypherInternalLabelsPredicate(fmt.Sprintf("labels(%s)", nodeAlias))
}

func cypherInternalLabelsPredicate(labelsExpr string) string {
	return fmt.Sprintf("any(label IN %s WHERE toUpper(label) IN ['SPATIALLAYER'])", labelsExpr)
}

func neo4jStringSlice(value interface{}) []string {
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...)
	case []interface{}:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if str, ok := value.(string); ok && str != "" {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

func graphEndpointShapeName(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	normalized := append([]string(nil), labels...)
	sort.Strings(normalized)
	return strings.Join(normalized, "+")
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
