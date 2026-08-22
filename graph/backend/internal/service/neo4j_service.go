package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
)

const (
	defaultGraphNodeColor = "#5B8FF9"
	defaultGraphEdgeColor = "#C0C0C0"

	DefaultExpandDepth             = 1
	MaxExpandDepth                 = 3
	DefaultExpandNodeLimit         = 200
	MaxExpandNodeLimit             = 500
	DefaultExpandRelationshipLimit = 400
	MaxExpandRelationshipLimit     = 1000
	maxAggregateSeedCount          = 20
)

// Neo4jService 提供面向知识图谱的 Neo4j 查询能力
type Neo4jService struct {
	graphRepo    *repository.KnowledgeGraphRepository
	ontologyRepo *repository.OntologyRepository
	systemClient *commonClient.SystemServiceClient
}

func NewNeo4jService(
	graphRepo *repository.KnowledgeGraphRepository,
	ontologyRepo *repository.OntologyRepository,
	systemClient *commonClient.SystemServiceClient,
) *Neo4jService {
	return &Neo4jService{
		graphRepo:    graphRepo,
		ontologyRepo: ontologyRepo,
		systemClient: systemClient,
	}
}

// getGraphAndEngine 获取 KnowledgeGraph 并构建针对该 database 的 Engine 副本
func (s *Neo4jService) getGraphAndEngine(graphID, tenantID uint) (*models.KnowledgeGraph, *commonmodels.Engine, error) {
	kg, err := s.graphRepo.GetByID(graphID, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("knowledge graph not found: %w", err)
	}
	engine, err := s.systemClient.WithTenantID(tenantID).GetEngine(context.Background(), kg.EngineID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get engine (id=%d): %w", kg.EngineID, err)
	}
	if !engineselection.IsAvailableForComputeEntrypoint(engine, "query") {
		return nil, nil, fmt.Errorf("engine (id=%d) is not currently available", kg.EngineID)
	}
	// 构建以图谱指定 database 为目标的 engine 副本
	engineCopy := *engine
	connCopy := make(commonmodels.ConnectionInfo, len(engine.ConnectionInfo))
	for k, v := range engine.ConnectionInfo {
		connCopy[k] = v
	}
	if kg.Database != "" {
		connCopy["database"] = kg.Database
	}
	engineCopy.ConnectionInfo = connCopy
	return kg, &engineCopy, nil
}

// GetBrowseSnapshot 从同一组图事实派生 Schema、统计和聚合概览。
func (s *Neo4jService) GetBrowseSnapshot(ctx context.Context, graphID, tenantID uint) (*models.BrowseSnapshot, error) {
	kg, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	ontology, _ := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	nodeColors, edgeColors, edgeDirections := buildVisualMaps(ontology)
	nodeResult, relationshipResult, err := queryBrowseFacts(ctx, engine)
	if err != nil {
		return nil, err
	}
	return buildBrowseSnapshot(nodeResult, relationshipResult, nodeColors, edgeColors, edgeDirections), nil
}

func queryBrowseFacts(ctx context.Context, engine *commonmodels.Engine) (*plugin.GraphQueryResult, *plugin.GraphQueryResult, error) {
	nodeResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"MATCH (n) WHERE "+businessNodePredicate("n")+
			" RETURN labels(n) AS labels, count(n) AS cnt ORDER BY cnt DESC")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get browse node facts: %w", err)
	}
	relationshipResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"MATCH (n)-[r]->(m) WHERE "+businessRelationshipPredicate("r", "n", "m")+
			" RETURN labels(n) AS source_labels, type(r) AS rel_type, labels(m) AS target_labels, count(r) AS cnt ORDER BY rel_type, cnt DESC")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get browse relationship facts: %w", err)
	}
	return nodeResult, relationshipResult, nil
}

func buildBrowseSnapshot(
	nodeResult, relationshipResult *plugin.GraphQueryResult,
	nodeColors, edgeColors map[string]string,
	edgeDirections map[string]bool,
) *models.BrowseSnapshot {
	snapshot := &models.BrowseSnapshot{
		Schema: models.BrowseSchema{
			NodeShapes:         []models.NodeShapeDTO{},
			RelationshipShapes: []models.RelationshipShapeDTO{},
		},
		Stats: models.BrowseStats{ByLabel: make(map[string]int64)},
	}
	snapshot.Overview = *buildAggregatedOverview(nodeResult, relationshipResult, nodeColors, edgeColors, edgeDirections)

	if nodeResult != nil {
		for _, row := range nodeResult.Rows {
			labels := normalizedStringSet(interfaceToStringSlice(row["labels"]))
			if len(labels) == 0 || isInternalNodeLabelSet(labels) {
				continue
			}
			count := toInt64(row["cnt"])
			shapeName := endpointShapeName(labels)
			color := nodeColors[shapeName]
			if color == "" {
				color = defaultGraphNodeColor
			}
			snapshot.Schema.NodeShapes = append(snapshot.Schema.NodeShapes, models.NodeShapeDTO{
				Name: shapeName, Kind: "label_set", Labels: labels, Color: color, Count: &count,
			})
			snapshot.Stats.NodeCount += count
			for _, label := range labels {
				snapshot.Stats.ByLabel[label] += count
			}
		}
	}

	relationshipShapeByType := make(map[string]int)
	if relationshipResult != nil {
		for _, row := range relationshipResult.Rows {
			relType := fmt.Sprintf("%v", row["rel_type"])
			fromLabels := normalizedStringSet(interfaceToStringSlice(row["source_labels"]))
			toLabels := normalizedStringSet(interfaceToStringSlice(row["target_labels"]))
			if relType == "" || relType == "<nil>" || isInternalRelationshipType(relType) || isInternalNodeLabelSet(fromLabels) || isInternalNodeLabelSet(toLabels) {
				continue
			}
			count := toInt64(row["cnt"])
			snapshot.Stats.RelationshipCount += count
			index, ok := relationshipShapeByType[relType]
			if !ok {
				color := edgeColors[relType]
				if color == "" {
					color = defaultGraphEdgeColor
				}
				directed, hasDirection := edgeDirections[relType]
				if !hasDirection {
					directed = true
				}
				index = len(snapshot.Schema.RelationshipShapes)
				snapshot.Schema.RelationshipShapes = append(snapshot.Schema.RelationshipShapes, models.RelationshipShapeDTO{
					Type: relType, Color: color, Directed: directed,
				})
				relationshipShapeByType[relType] = index
			}
			shape := &snapshot.Schema.RelationshipShapes[index]
			shape.Count = addInt64Ptr(shape.Count, count)
			shape.Patterns = append(shape.Patterns, models.RelationshipPatternDTO{
				From:  models.GraphEndpointDTO{ShapeName: endpointShapeName(fromLabels), Labels: fromLabels},
				To:    models.GraphEndpointDTO{ShapeName: endpointShapeName(toLabels), Labels: toLabels},
				Count: &count,
			})
		}
	}
	return snapshot
}

func buildAggregatedOverview(
	nodeResult, relationshipResult *plugin.GraphQueryResult,
	nodeColors, edgeColors map[string]string,
	edgeDirections map[string]bool,
) *models.SubgraphResult {
	out := &models.SubgraphResult{Nodes: []models.GraphNodeDTO{}, Edges: []models.GraphEdgeDTO{}}
	if nodeResult == nil {
		return out
	}

	bucketIDs := make(map[string]string, len(nodeResult.Rows))
	for _, row := range nodeResult.Rows {
		labels := normalizedStringSet(interfaceToStringSlice(row["labels"]))
		if len(labels) == 0 || isInternalNodeLabelSet(labels) {
			continue
		}
		shapeName := endpointShapeName(labels)
		bucketID := aggregateNodeID(labels)
		bucketIDs[nodeLabelsKey(labels)] = bucketID
		color := nodeColors[shapeName]
		if color == "" {
			color = defaultGraphNodeColor
		}
		out.Nodes = append(out.Nodes, models.GraphNodeDTO{
			ID:          bucketID,
			Kind:        "aggregate",
			Labels:      labels,
			EntityType:  shapeName,
			Color:       color,
			DisplayName: shapeName,
			MemberCount: toInt64(row["cnt"]),
		})
	}

	if relationshipResult == nil {
		return out
	}
	for _, row := range relationshipResult.Rows {
		sourceLabels := normalizedStringSet(interfaceToStringSlice(row["source_labels"]))
		targetLabels := normalizedStringSet(interfaceToStringSlice(row["target_labels"]))
		relType := fmt.Sprintf("%v", row["rel_type"])
		sourceID := bucketIDs[nodeLabelsKey(sourceLabels)]
		targetID := bucketIDs[nodeLabelsKey(targetLabels)]
		if sourceID == "" || targetID == "" || relType == "" || isInternalRelationshipType(relType) {
			continue
		}
		color := edgeColors[relType]
		if color == "" {
			color = defaultGraphEdgeColor
		}
		directed, ok := edgeDirections[relType]
		if !ok {
			directed = true
		}
		out.Edges = append(out.Edges, models.GraphEdgeDTO{
			ID:           aggregateEdgeID(sourceID, relType, targetID),
			Kind:         "aggregate",
			Type:         relType,
			RelationType: relType,
			Color:        color,
			Directed:     directed,
			Source:       sourceID,
			Target:       targetID,
			Count:        toInt64(row["cnt"]),
		})
	}
	return out
}

func aggregateNodeID(labels []string) string {
	sum := sha256.Sum256([]byte(nodeLabelsKey(labels)))
	return fmt.Sprintf("aggregate:%x", sum[:8])
}

func aggregateEdgeID(sourceID, relType, targetID string) string {
	sum := sha256.Sum256([]byte(sourceID + "\x00" + relType + "\x00" + targetID))
	return fmt.Sprintf("aggregate-edge:%x", sum[:8])
}

// SearchNodes 使用本体声明的实体全文索引搜索节点。
func (s *Neo4jService) SearchNodes(ctx context.Context, graphID, tenantID uint, query string, limit int) (*models.SubgraphResult, error) {
	kg, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	ontology, err := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get search ontology: %w", err)
	}
	nodeColors, edgeColors, edgeDirections := buildVisualMaps(ontology)
	if limit <= 0 || limit > 50 {
		limit = 30
	}
	definitions := buildSearchIndexDefinitions(graphID, ontology)
	if strings.TrimSpace(query) == "" {
		return &models.SubgraphResult{Nodes: []models.GraphNodeDTO{}, Edges: []models.GraphEdgeDTO{}}, nil
	}
	if err := syncSearchIndexes(ctx, engine, graphID, definitions); err != nil {
		return nil, fmt.Errorf("failed to sync search indexes: %w", err)
	}
	if len(definitions) == 0 {
		return &models.SubgraphResult{Nodes: []models.GraphNodeDTO{}, Edges: []models.GraphEdgeDTO{}}, nil
	}
	cypher := fmt.Sprintf(
		"%s WITH node, max(score) AS score WHERE %s RETURN node ORDER BY score DESC, elementId(node) LIMIT %d",
		fulltextSearchSubquery(definitions, query), businessNodePredicate("node"), limit,
	)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	return buildSubgraph(result, nodeColors, edgeColors, edgeDirections, newOntologySemantics(ontology)), nil
}

func searchIndexPrefix(graphID uint) string {
	return fmt.Sprintf("addp_graph_%d_search_", graphID)
}

func searchIndexName(graphID uint, entityType string) string {
	sum := sha256.Sum256([]byte(entityType))
	return fmt.Sprintf("%s%x", searchIndexPrefix(graphID), sum[:8])
}

func syncSearchIndexes(ctx context.Context, engine *commonmodels.Engine, graphID uint, definitions []searchIndexDefinition) error {
	prefix := searchIndexPrefix(graphID)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, fmt.Sprintf(
		"SHOW FULLTEXT INDEXES YIELD name, labelsOrTypes, properties, state WHERE name STARTS WITH '%s' RETURN name, labelsOrTypes, properties, state",
		escapeCypher(prefix),
	))
	if err != nil {
		return err
	}

	expected := make(map[string]searchIndexDefinition, len(definitions))
	for _, definition := range definitions {
		expected[definition.Name] = definition
	}
	existing := make(map[string]bool, len(result.Rows))
	needsAwait := false
	for _, row := range result.Rows {
		name := fmt.Sprintf("%v", row["name"])
		definition, ok := expected[name]
		matches := ok && sameStringSet(interfaceToStringSlice(row["labelsOrTypes"]), []string{definition.Labels[0]}) &&
			sameStringSet(interfaceToStringSlice(row["properties"]), definition.Properties)
		if matches {
			existing[name] = true
			needsAwait = needsAwait || fmt.Sprintf("%v", row["state"]) != "ONLINE"
			continue
		}
		if _, err := dbbridge.ExecuteGraphQuery(ctx, engine,
			fmt.Sprintf("DROP INDEX `%s` IF EXISTS", escapeCypherIdentifier(name))); err != nil {
			return err
		}
		needsAwait = true
	}

	for name, definition := range expected {
		if !existing[name] {
			properties := make([]string, 0, len(definition.Properties))
			for _, property := range definition.Properties {
				properties = append(properties, fmt.Sprintf("n.`%s`", escapeCypherIdentifier(property)))
			}
			createQuery := fmt.Sprintf(
				"CREATE FULLTEXT INDEX `%s` IF NOT EXISTS FOR (n:`%s`) ON EACH [%s]",
				escapeCypherIdentifier(name), escapeCypherIdentifier(definition.Labels[0]), strings.Join(properties, ", "),
			)
			if _, err := dbbridge.ExecuteGraphQuery(ctx, engine, createQuery); err != nil {
				return err
			}
			needsAwait = true
		}
	}
	if needsAwait && len(expected) > 0 {
		if _, err := dbbridge.ExecuteGraphQuery(ctx, engine, "CALL db.awaitIndexes(30)"); err != nil {
			return err
		}
	}
	return nil
}

func fulltextSearchSubquery(definitions []searchIndexDefinition, query string) string {
	parts := make([]string, 0, len(definitions))
	queryLiteral := escapeCypher(fulltextPhrase(query))
	for _, definition := range definitions {
		parts = append(parts, fmt.Sprintf(
			"CALL db.index.fulltext.queryNodes('%s', '%s') YIELD node, score WHERE all(label IN %s WHERE label IN labels(node)) RETURN node, score",
			escapeCypher(definition.Name), queryLiteral, cypherStringList(definition.Labels),
		))
	}
	return "CALL { " + strings.Join(parts, " UNION ALL ") + " }"
}

func fulltextPhrase(query string) string {
	query = strings.TrimSpace(query)
	query = strings.ReplaceAll(query, `\`, `\\`)
	query = strings.ReplaceAll(query, `"`, `\"`)
	return `"` + query + `"`
}

// Expand 使用节点/关系双预算展开聚合桶或真实实体。
func (s *Neo4jService) Expand(ctx context.Context, graphID, tenantID uint, req *models.ExpandRequest) (*models.SubgraphResult, error) {
	kg, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	ontology, _ := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	nodeColors, edgeColors, edgeDirections := buildVisualMaps(ontology)
	semantics := newOntologySemantics(ontology)

	depth, nodeLimit, relationshipLimit := normalizeExpandBudget(req)
	var seedResult *plugin.GraphQueryResult
	switch req.Target.Kind {
	case "aggregate":
		seedLimit := min(maxAggregateSeedCount, nodeLimit)
		seedResult, err = dbbridge.ExecuteGraphQuery(ctx, engine, aggregateSeedQuery(req.Target.Labels, seedLimit))
	case "entity":
		seedResult, err = dbbridge.ExecuteGraphQuery(ctx, engine, fmt.Sprintf(
			"MATCH (n) WHERE elementId(n) = '%s' AND %s RETURN n",
			escapeCypher(req.Target.ID), businessNodePredicate("n"),
		))
	default:
		return nil, fmt.Errorf("unsupported expand target kind %q", req.Target.Kind)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get expand seeds: %w", err)
	}
	return s.expandFromSeeds(ctx, engine, seedResult, depth, nodeLimit, relationshipLimit, nodeColors, edgeColors, edgeDirections, semantics)
}

func normalizeExpandBudget(req *models.ExpandRequest) (depth, nodeLimit, relationshipLimit int) {
	depth = req.Depth
	if depth <= 0 {
		depth = DefaultExpandDepth
	}
	nodeLimit = req.NodeLimit
	if nodeLimit <= 0 {
		nodeLimit = DefaultExpandNodeLimit
	}
	relationshipLimit = req.RelationshipLimit
	if relationshipLimit <= 0 {
		relationshipLimit = DefaultExpandRelationshipLimit
	}
	return
}

func aggregateSeedQuery(labels []string, limit int) string {
	labels = normalizedStringSet(labels)
	shapePredicate := fmt.Sprintf(
		"size(labels(n)) = %d AND all(label IN %s WHERE label IN labels(n))",
		len(labels), cypherStringList(labels),
	)
	return fmt.Sprintf(
		"MATCH (n) WHERE %s AND %s WITH n, COUNT { (n)--() } AS degree RETURN n ORDER BY degree DESC, elementId(n) LIMIT %d",
		businessNodePredicate("n"), shapePredicate, limit,
	)
}

func (s *Neo4jService) expandFromSeeds(
	ctx context.Context,
	engine *commonmodels.Engine,
	seedResult *plugin.GraphQueryResult,
	depth, nodeLimit, relationshipLimit int,
	nodeColors, edgeColors map[string]string,
	edgeDirections map[string]bool,
	semantics *ontologySemantics,
) (*models.SubgraphResult, error) {
	out := &models.SubgraphResult{Nodes: []models.GraphNodeDTO{}, Edges: []models.GraphEdgeDTO{}}
	nodeSeen := make(map[string]bool, nodeLimit)
	edgeSeen := make(map[string]bool, relationshipLimit)
	frontier := mergeExpandedSubgraph(out, buildSubgraph(seedResult, nodeColors, edgeColors, edgeDirections, semantics), nodeSeen, edgeSeen, nodeLimit, relationshipLimit)

	for hop := 0; hop < depth && len(frontier) > 0 && len(out.Nodes) < nodeLimit && len(out.Edges) < relationshipLimit; hop++ {
		remainingRelationships := relationshipLimit - len(out.Edges)
		result, err := dbbridge.ExecuteGraphQuery(ctx, engine, expandFrontierQuery(frontier, seenIDs(edgeSeen), remainingRelationships))
		if err != nil {
			return nil, fmt.Errorf("expand hop %d failed: %w", hop+1, err)
		}
		frontier = mergeExpandedSubgraph(
			out,
			buildSubgraph(result, nodeColors, edgeColors, edgeDirections, semantics),
			nodeSeen,
			edgeSeen,
			nodeLimit,
			relationshipLimit,
		)
	}
	return out, nil
}

func expandFrontierQuery(frontier, seenRelationshipIDs []string, relationshipLimit int) string {
	seenPredicate := ""
	if len(seenRelationshipIDs) > 0 {
		seenPredicate = " AND NOT elementId(r) IN " + cypherStringList(seenRelationshipIDs)
	}
	return fmt.Sprintf(
		"MATCH (n)-[r]-(m) WHERE elementId(n) IN %s AND %s%s WITH DISTINCT r RETURN startNode(r) AS source, r, endNode(r) AS target ORDER BY elementId(r) LIMIT %d",
		cypherStringList(frontier),
		businessRelationshipPredicate("r", "n", "m"),
		seenPredicate,
		relationshipLimit,
	)
}

func seenIDs(seen map[string]bool) []string {
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func mergeExpandedSubgraph(
	out, incoming *models.SubgraphResult,
	nodeSeen, edgeSeen map[string]bool,
	nodeLimit, relationshipLimit int,
) []string {
	if incoming == nil {
		return nil
	}
	nodeByID := make(map[string]models.GraphNodeDTO, len(incoming.Nodes))
	for _, node := range incoming.Nodes {
		nodeByID[node.ID] = node
	}
	newNodeIDs := make([]string, 0)
	addNode := func(id string) bool {
		if nodeSeen[id] {
			return true
		}
		if len(out.Nodes) >= nodeLimit {
			return false
		}
		node, ok := nodeByID[id]
		if !ok {
			return false
		}
		nodeSeen[id] = true
		out.Nodes = append(out.Nodes, node)
		newNodeIDs = append(newNodeIDs, id)
		return true
	}

	for _, node := range incoming.Nodes {
		if len(incoming.Edges) > 0 {
			break
		}
		addNode(node.ID)
	}
	for _, edge := range incoming.Edges {
		if len(out.Edges) >= relationshipLimit || edgeSeen[edge.ID] {
			continue
		}
		if !addNode(edge.Source) || !addNode(edge.Target) {
			continue
		}
		edgeSeen[edge.ID] = true
		out.Edges = append(out.Edges, edge)
	}
	return newNodeIDs
}

// FindPath 查找两节点间的最短路径（最多 10 跳）
func (s *Neo4jService) FindPath(ctx context.Context, graphID, tenantID uint, sourceID, targetID string) (*models.SubgraphResult, error) {
	kg, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	ontology, _ := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	nodeColors, edgeColors, edgeDirections := buildVisualMaps(ontology)

	cypher := fmt.Sprintf(
		"MATCH p = shortestPath((a)-[*..10]-(b)) WHERE elementId(a) = '%s' AND elementId(b) = '%s' AND NONE(rel IN relationships(p) WHERE type(rel) IN "+internalRelationshipTypeList+") RETURN p",
		escapeCypher(sourceID), escapeCypher(targetID),
	)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("path query failed: %w", err)
	}
	return buildSubgraph(result, nodeColors, edgeColors, edgeDirections, newOntologySemantics(ontology)), nil
}

// SyncConstraints 将实体类型的唯一属性约束同步到 Neo4j（幂等，使用 IF NOT EXISTS）
// 约束命名规则：graph_{graphID}_{entityTypeName}_{fieldName}_unique
// entityTypeName 是本体概念标识；Neo4j 执行 label 来自 nodeLabels。
// 注意：NOT NULL 约束需 Neo4j 企业版，此处仅同步 UNIQUE 约束
func (s *Neo4jService) SyncConstraints(ctx context.Context, graphID, tenantID uint, entityTypeName string, nodeLabels []string, props []models.PropertyDefinition) error {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return err
	}
	if len(nodeLabels) == 0 {
		nodeLabels = []string{entityTypeName}
	}
	for _, prop := range props {
		if !prop.Unique {
			continue
		}
		constraintName := fmt.Sprintf("graph_%d_%s_%s_unique", graphID, entityTypeName, prop.Name)
		// Neo4j 约束语法只支持单 label；label-set 类型取第一个执行 label 创建约束。
		cypher := fmt.Sprintf(
			"CREATE CONSTRAINT %s IF NOT EXISTS FOR (n:%s) REQUIRE n.%s IS UNIQUE",
			constraintName, escapeCypherIdentifier(nodeLabels[0]), escapeCypherIdentifier(prop.Name),
		)
		if _, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher); err != nil {
			return fmt.Errorf("failed to create constraint %s: %w", constraintName, err)
		}
	}
	return nil
}

// GetConstraints 查询 Neo4j 当前已有的约束
func (s *Neo4jService) GetConstraints(ctx context.Context, graphID, tenantID uint) ([]models.ConstraintInfo, error) {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, "SHOW CONSTRAINTS")
	if err != nil {
		return nil, fmt.Errorf("failed to list constraints: %w", err)
	}
	constraints := make([]models.ConstraintInfo, 0, len(result.Rows))
	for _, row := range result.Rows {
		info := models.ConstraintInfo{
			Name: fmt.Sprintf("%v", row["name"]),
			Type: fmt.Sprintf("%v", row["type"]),
		}
		// labelsOrTypes 字段包含节点标签
		if v, ok := row["labelsOrTypes"]; ok {
			info.EntityType = fmt.Sprintf("%v", v)
		}
		// properties 字段包含受约束的属性
		if v, ok := row["properties"]; ok {
			info.Field = fmt.Sprintf("%v", v)
		}
		constraints = append(constraints, info)
	}
	return constraints, nil
}

// ============ 内部辅助函数 ============

func buildVisualMapsFromOntology(ontologyRepo *repository.OntologyRepository, ontologyID, tenantID uint) (nodeColors, edgeColors map[string]string, edgeDirections map[string]bool) {
	ontology, err := ontologyRepo.GetDetail(ontologyID, tenantID)
	if err != nil {
		return buildVisualMaps(nil)
	}
	return buildVisualMaps(ontology)
}

func buildVisualMaps(ontology *models.Ontology) (nodeColors, edgeColors map[string]string, edgeDirections map[string]bool) {
	nodeColors = make(map[string]string)
	edgeColors = make(map[string]string)
	edgeDirections = make(map[string]bool)
	if ontology == nil {
		return
	}
	byID := entityTypeByID(ontology.EntityTypes)
	for _, entityType := range ontology.EntityTypes {
		if entityType.Color == "" {
			continue
		}
		nodeColors[entityType.Name] = entityType.Color
		if labels := effectiveNodeLabels(&entityType, byID); len(labels) > 0 {
			nodeColors[endpointShapeName(labels)] = entityType.Color
		}
	}
	for _, relationType := range ontology.RelationTypes {
		if relationType.Color != "" {
			edgeColors[relationType.Name] = relationType.Color
		}
		edgeDirections[relationType.Name] = relationType.Directed
	}
	return
}

// buildSubgraph 将 GraphQueryResult 转换为 SubgraphResult（含本体着色）
func buildSubgraph(result *plugin.GraphQueryResult, nodeColors, edgeColors map[string]string, edgeDirections map[string]bool, semantics *ontologySemantics) *models.SubgraphResult {
	out := &models.SubgraphResult{
		Nodes: []models.GraphNodeDTO{},
		Edges: []models.GraphEdgeDTO{},
	}
	if result == nil || result.GraphData == nil {
		return out
	}

	for _, n := range result.GraphData.Nodes {
		dto := models.GraphNodeDTO{
			ID:          n.ElementId,
			Kind:        "entity",
			Labels:      n.Labels,
			Color:       defaultGraphNodeColor,
			DisplayName: n.ElementId,
			Properties:  displayGraphProperties(n.Properties),
		}
		// 节点类型以完整 Label Set 对应的 node shape 为唯一语义。
		if len(n.Labels) > 0 {
			shapeName := endpointShapeName(n.Labels)
			dto.EntityType = shapeName
			if color, ok := nodeColors[shapeName]; ok {
				dto.Color = color
			}
		}
		if semantics != nil {
			dto.DisplayName = semantics.displayName(n.Labels, n.Properties, n.ElementId)
		}
		out.Nodes = append(out.Nodes, dto)
	}

	for _, r := range result.GraphData.Relationships {
		if isInternalRelationshipType(r.Type) {
			continue
		}
		dto := models.GraphEdgeDTO{
			ID:           r.ElementId,
			Kind:         "entity",
			Type:         r.Type,
			RelationType: r.Type,
			Color:        defaultGraphEdgeColor,
			Directed:     true,
			Source:       r.StartNodeId,
			Target:       r.EndNodeId,
			Properties:   displayGraphProperties(r.Properties),
		}
		if color, ok := edgeColors[r.Type]; ok {
			dto.Color = color
		}
		if directed, ok := edgeDirections[r.Type]; ok {
			dto.Directed = directed
		}
		out.Edges = append(out.Edges, dto)
	}
	return out
}

func displayGraphProperties(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(input))
	for key, value := range input {
		if isTechnicalGraphProperty(key) {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isTechnicalGraphProperty(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "_created_at", "_updated_at", "_update_at", "_deleted_at":
		return true
	default:
		return false
	}
}

// escapeCypher 对 Cypher 字符串值进行转义，防止注入
func escapeCypher(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// escapeCypherIdentifier 转义 Cypher label/type/property 标识符中的反引号。
func escapeCypherIdentifier(s string) string {
	return strings.ReplaceAll(s, "`", "``")
}

// toInt64 将 interface{} 转换为 int64
func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	}
	return 0
}

func interfaceToStringSlice(value interface{}) []string {
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

func endpointShapeName(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	normalized := append([]string(nil), labels...)
	sort.Strings(normalized)
	return strings.Join(normalized, "+")
}

func addInt64Ptr(value *int64, delta int64) *int64 {
	if value == nil {
		result := delta
		return &result
	}
	*value += delta
	return value
}

// cypherValue 将 Go 值序列化为 Cypher 字面量
func cypherValue(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("'%s'", escapeCypher(val))
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("'%s'", escapeCypher(fmt.Sprintf("%v", val)))
	}
}

// buildSetClause 将属性 map 转换为 Cypher SET 子句片段（如 n.name = 'Alice', n.age = 30）
func buildSetClause(alias string, props map[string]interface{}) string {
	if len(props) == 0 {
		return ""
	}
	parts := make([]string, 0, len(props))
	for k, v := range props {
		// 跳过 nil 和空字符串：避免"渐进叠加"场景下空值覆盖已有的有效数据
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s.%s = %s", alias, escapeCypher(k), cypherValue(v)))
	}
	return strings.Join(parts, ", ")
}

// MergeEntity 将实体写入 Neo4j（MERGE 幂等写入）
// entityLabels: Neo4j 节点标签执行映射（如 ["Company", "POI"]）
// uniqueField: 唯一标识属性名（来自本体 PropertyDefinition.Unique=true）
// uniqueValue: 唯一标识属性值
// properties: 所有属性
// 返回 elementId
func (s *Neo4jService) MergeEntity(ctx context.Context, graphID, tenantID uint, entityLabels []string, uniqueField string, uniqueValue interface{}, properties map[string]interface{}) (string, error) {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return "", err
	}

	labelSelector := nodeLabelsPattern(entityLabels)
	if labelSelector == "" {
		return "", fmt.Errorf("entityLabels must not be empty")
	}

	propSet := buildSetClause("n", properties)
	var setPart string
	if propSet != "" {
		setPart = fmt.Sprintf("ON CREATE SET %s, n._created_at = timestamp()\nON MATCH  SET %s, n._updated_at = timestamp()", propSet, propSet)
	} else {
		setPart = "ON CREATE SET n._created_at = timestamp()\nON MATCH SET n._updated_at = timestamp()"
	}

	cypher := fmt.Sprintf(
		"MERGE (n%s {%s: %s})\n%s\nRETURN elementId(n) AS eid",
		labelSelector, escapeCypher(uniqueField), cypherValue(uniqueValue), setPart,
	)

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return "", fmt.Errorf("merge entity failed: %w", err)
	}
	if len(result.Rows) > 0 {
		if eid, ok := result.Rows[0]["eid"]; ok {
			return fmt.Sprintf("%v", eid), nil
		}
	}
	return "", nil
}

// MergeRelation 将关系写入 Neo4j（MERGE 幂等写入）
// relType: 关系类型名
// srcLabels/tgtLabels: 源/目标节点的 Neo4j 节点标签执行映射
// srcUniqueField/tgtUniqueField: 源/目标节点的唯一标识属性名
// srcUniqueVal/tgtUniqueVal: 源/目标节点的唯一标识属性值
// properties: 关系属性
// 返回 elementId（找不到节点时返回空字符串）
func (s *Neo4jService) MergeRelation(
	ctx context.Context,
	graphID, tenantID uint,
	relType string,
	srcLabels []string, srcUniqueField string, srcUniqueVal interface{},
	tgtLabels []string, tgtUniqueField string, tgtUniqueVal interface{},
	properties map[string]interface{},
) (string, error) {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return "", err
	}

	var setPart string
	if propSet := buildSetClause("r", properties); propSet != "" {
		setPart = fmt.Sprintf("ON CREATE SET %s", propSet)
	}

	srcSelector := nodeLabelsPattern(srcLabels)
	tgtSelector := nodeLabelsPattern(tgtLabels)
	if srcSelector == "" || tgtSelector == "" {
		return "", fmt.Errorf("source and target labels must not be empty")
	}

	cypher := fmt.Sprintf(
		"MATCH (a%s {%s: %s}), (b%s {%s: %s})\nMERGE (a)-[r:%s]->(b)\n%s\nRETURN elementId(r) AS eid",
		srcSelector, escapeCypher(srcUniqueField), cypherValue(srcUniqueVal),
		tgtSelector, escapeCypher(tgtUniqueField), cypherValue(tgtUniqueVal),
		relType, setPart,
	)

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return "", fmt.Errorf("merge relation failed: %w", err)
	}
	if len(result.Rows) > 0 {
		if eid, ok := result.Rows[0]["eid"]; ok {
			return fmt.Sprintf("%v", eid), nil
		}
	}
	// MATCH 找不到节点时返回空（不报错，只是跳过）
	return "", nil
}

// SyncSpatialLayer 将 EntityType 的空间图层同步到 Neo4j（仅对 is_spatial_layer=true 的类型操作，幂等）
func (s *Neo4jService) SyncSpatialLayer(ctx context.Context, graphID, tenantID uint, et models.EntityType) error {
	if !et.IsSpatialLayer {
		return fmt.Errorf("entity type %q is not a spatial layer type", et.Name)
	}
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return err
	}

	// 检查 spatial 插件是否可用
	checkResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"CALL spatial.procedures() YIELD signature RETURN count(signature) AS cnt")
	if err != nil || len(checkResult.Rows) == 0 || toInt64(checkResult.Rows[0]["cnt"]) == 0 {
		return fmt.Errorf("目标 Neo4j 未安装 spatial 插件")
	}

	cfg := et.ParsedSpatialLayerConfig()
	layerName := cfg.LayerName

	// 查询已有图层
	layersResult, _ := dbbridge.ExecuteGraphQuery(ctx, engine, "CALL spatial.layers() YIELD name")
	existingLayers := make(map[string]bool)
	if layersResult != nil {
		for _, row := range layersResult.Rows {
			if v, ok := row["name"]; ok {
				existingLayers[fmt.Sprintf("%v", v)] = true
			}
		}
	}

	if existingLayers[layerName] {
		return nil // 幂等：图层已存在视为成功
	}

	// 按几何类型创建图层
	var createCypher string
	switch cfg.GeometryType {
	case "point":
		createCypher = fmt.Sprintf(
			"CALL spatial.addPointLayerXY('%s', '%s', '%s')",
			escapeCypher(layerName), escapeCypher(cfg.LonField), escapeCypher(cfg.LatField),
		)
	case "wkt":
		createCypher = fmt.Sprintf(
			"CALL spatial.addWKTLayer('%s', '%s')",
			escapeCypher(layerName), escapeCypher(cfg.GeomField),
		)
	default:
		return fmt.Errorf("unsupported geometry_type: %q (must be 'point' or 'wkt')", cfg.GeometryType)
	}

	if _, err := dbbridge.ExecuteGraphQuery(ctx, engine, createCypher); err != nil {
		return fmt.Errorf("创建空间图层 %q 失败: %w", layerName, err)
	}
	return nil
}

// SyncSpatialLayerByConfig 根据 SpatialLayerConfig 在 Neo4j 中创建空间图层（幂等，不要求 IsSpatialLayer=true）
func (s *Neo4jService) SyncSpatialLayerByConfig(ctx context.Context, graphID, tenantID uint, cfg *models.SpatialLayerConfig) error {
	if cfg == nil || cfg.LayerName == "" {
		return nil
	}
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return err
	}

	// 查询已有图层，幂等
	layersResult, _ := dbbridge.ExecuteGraphQuery(ctx, engine, "CALL spatial.layers() YIELD name")
	if layersResult != nil {
		for _, row := range layersResult.Rows {
			if name, ok := row["name"].(string); ok && name == cfg.LayerName {
				return nil // 已存在
			}
		}
	}

	var createCypher string
	switch cfg.GeometryType {
	case "wkt":
		createCypher = fmt.Sprintf(
			"CALL spatial.addWKTLayer('%s', '%s')",
			escapeCypher(cfg.LayerName), escapeCypher(cfg.GeomField),
		)
	default: // "point" 或未指定
		createCypher = fmt.Sprintf(
			"CALL spatial.addPointLayerXY('%s', '%s', '%s')",
			escapeCypher(cfg.LayerName), escapeCypher(cfg.LonField), escapeCypher(cfg.LatField),
		)
	}
	if _, err := dbbridge.ExecuteGraphQuery(ctx, engine, createCypher); err != nil {
		return fmt.Errorf("创建空间图层 %q 失败: %w", cfg.LayerName, err)
	}
	return nil
}

// AddNodeToSpatialLayer 将已写入的节点注册到空间图层（通过 elementId 定位节点）
func (s *Neo4jService) AddNodeToSpatialLayer(ctx context.Context, graphID, tenantID uint, elementId, layerName string) error {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return err
	}
	cypher := fmt.Sprintf(
		"MATCH (n) WHERE elementId(n) = '%s' CALL spatial.addNode('%s', n) YIELD node RETURN node",
		escapeCypher(elementId), escapeCypher(layerName),
	)
	if _, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher); err != nil {
		return fmt.Errorf("addNodeToSpatialLayer(%q, %q) failed: %w", elementId, layerName, err)
	}
	return nil
}
