package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/propertygraph"
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

// GraphDataService 组合本体与业务图操作，原生差异由 Common Provider 承担。
type GraphDataService struct {
	graphRepo    *repository.KnowledgeGraphRepository
	ontologyRepo *repository.OntologyRepository
	systemClient *commonClient.SystemServiceClient
	protection   *GraphProtectionGate
}

func NewGraphDataService(
	graphRepo *repository.KnowledgeGraphRepository,
	ontologyRepo *repository.OntologyRepository,
	systemClient *commonClient.SystemServiceClient,
	protection *GraphProtectionGate,
) *GraphDataService {
	return &GraphDataService{
		graphRepo:    graphRepo,
		ontologyRepo: ontologyRepo,
		systemClient: systemClient,
		protection:   protection,
	}
}

// getGraphAndEngine binds a domain operation to its explicit catalog target.
func (s *GraphDataService) getGraphAndEngine(ctx context.Context, graphID, tenantID uint) (*models.KnowledgeGraph, *graphTarget, error) {
	kg, err := s.graphRepo.GetByID(graphID, tenantID)
	if err != nil {
		return nil, nil, err
	}
	if kg.IndexManagement != models.GraphIndexesManaged && kg.IndexManagement != models.GraphIndexesExternal {
		return nil, nil, ErrGraphTargetInvalid
	}
	target, err := s.resolveTarget(ctx, tenantID, kg.EngineID, kg.CatalogPath)
	if err != nil {
		return nil, nil, err
	}
	target.managed = kg.IndexManagement == models.GraphIndexesManaged
	return kg, target, nil
}

// GetBrowseSnapshot 从同一组图事实派生 Schema、统计和聚合概览。
func (s *GraphDataService) GetBrowseSnapshot(ctx context.Context, graphID, tenantID uint) (*models.BrowseSnapshot, error) {
	kg, engine, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return nil, err
	}
	defer engine.close()
	ontology, _ := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	nodeColors, edgeColors, edgeDirections := buildVisualMaps(ontology)
	nodeResult, relationshipResult, err := queryBrowseFacts(ctx, engine)
	if err != nil {
		return nil, err
	}
	return buildBrowseSnapshot(nodeResult, relationshipResult, nodeColors, edgeColors, edgeDirections), nil
}

func queryBrowseFacts(ctx context.Context, engine *graphTarget) (*plugin.GraphQueryResult, *plugin.GraphQueryResult, error) {
	nodeResult, err := executeGraphQuery(ctx, engine,
		"MATCH (n) WHERE "+propertygraph.NodePredicate(engine.provider.GraphDialect(), "n")+
			" RETURN labels(n) AS labels, count(n) AS cnt ORDER BY cnt DESC")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get browse node facts: %w", err)
	}
	relationshipResult, err := executeGraphQuery(ctx, engine,
		"MATCH (n)-[r]->(m) WHERE "+propertygraph.RelationshipPredicate(engine.provider.GraphDialect(), "r", "n", "m")+
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
			if len(labels) == 0 {
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
			if relType == "" || relType == "<nil>" {
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
		if len(labels) == 0 {
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
		if sourceID == "" || targetID == "" || relType == "" {
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
func (s *GraphDataService) SearchNodes(ctx context.Context, graphID, tenantID uint, query string, limit int) (*models.SubgraphResult, error) {
	kg, engine, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return nil, err
	}
	defer engine.close()
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
	indexes, err := syncSearchIndexes(ctx, engine, graphID, definitions)
	if err != nil {
		return nil, err
	}
	result, _, err := propertygraph.Search(ctx, engine.provider, engine.conn, engine.path, indexes, query, propertygraph.NodePredicate(engine.provider.GraphDialect(), "node"), 0, limit)
	if err != nil {
		return nil, err
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

func syncSearchIndexes(ctx context.Context, target *graphTarget, graphID uint, definitions []searchIndexDefinition) ([]plugin.GraphSearchIndex, error) {
	indexes := make([]plugin.GraphSearchIndex, 0, len(definitions))
	for _, d := range definitions {
		indexes = append(indexes, plugin.GraphSearchIndex{Name: d.Name, Labels: d.Labels, Properties: d.Properties})
	}
	return target.provider.SyncGraphSearchIndexes(ctx, target.conn, target.path, plugin.GraphSearchSchema{OwnerPrefix: searchIndexPrefix(graphID), Managed: target.managed, Indexes: indexes})
}

// Expand 使用节点/关系双预算展开聚合桶或真实实体。
func (s *GraphDataService) Expand(ctx context.Context, graphID, tenantID uint, req *models.ExpandRequest) (*models.SubgraphResult, error) {
	kg, engine, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return nil, err
	}
	defer engine.close()
	ontology, _ := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	nodeColors, edgeColors, edgeDirections := buildVisualMaps(ontology)
	semantics := newOntologySemantics(ontology)

	depth, nodeLimit, relationshipLimit := normalizeExpandBudget(req)
	var seedResult *plugin.GraphQueryResult
	switch req.Target.Kind {
	case "aggregate":
		seedLimit := min(maxAggregateSeedCount, nodeLimit)
		seedResult, err = executeGraphQuery(ctx, engine, aggregateSeedQuery(engine.provider.GraphDialect(), req.Target.Labels, seedLimit))
	case "entity":
		seedResult, err = executeGraphQuery(ctx, engine, fmt.Sprintf(
			"MATCH (n) WHERE %s = '%s' AND %s RETURN n",
			engine.provider.GraphDialect().ID("n"), escapeCypher(req.Target.ID), propertygraph.NodePredicate(engine.provider.GraphDialect(), "n"),
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

func aggregateSeedQuery(dialect plugin.PropertyGraphDialect, labels []string, limit int) string {
	labels = normalizedStringSet(labels)
	shapePredicate := fmt.Sprintf(
		"size(labels(n)) = %d AND all(label IN %s WHERE label IN labels(n))",
		len(labels), cypherStringList(labels),
	)
	return fmt.Sprintf(
		"MATCH (n) WHERE %s AND %s WITH n, %s AS degree RETURN n ORDER BY degree DESC, %s LIMIT %d",
		propertygraph.NodePredicate(dialect, "n"), shapePredicate, fmt.Sprintf(dialect.DegreeTemplate, "n"), dialect.ID("n"), limit,
	)
}

func (s *GraphDataService) expandFromSeeds(
	ctx context.Context,
	engine *graphTarget,
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
		result, err := executeGraphQuery(ctx, engine, expandFrontierQuery(engine.provider.GraphDialect(), frontier, seenIDs(edgeSeen), remainingRelationships))
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

func expandFrontierQuery(dialect plugin.PropertyGraphDialect, frontier, seenRelationshipIDs []string, relationshipLimit int) string {
	seenPredicate := ""
	if len(seenRelationshipIDs) > 0 {
		seenPredicate = " AND NOT " + dialect.ID("r") + " IN " + cypherStringList(seenRelationshipIDs)
	}
	return fmt.Sprintf(
		"MATCH (n)-[r]-(m) WHERE %s IN %s AND %s%s WITH DISTINCT r RETURN startNode(r) AS source, r, endNode(r) AS target ORDER BY %s LIMIT %d",
		dialect.ID("n"), cypherStringList(frontier),
		propertygraph.RelationshipPredicate(dialect, "r", "n", "m"),
		seenPredicate, dialect.ID("r"),
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
func (s *GraphDataService) FindPath(ctx context.Context, graphID, tenantID uint, sourceID, targetID string) (*models.SubgraphResult, error) {
	kg, engine, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return nil, err
	}
	defer engine.close()
	ontology, _ := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	nodeColors, edgeColors, edgeDirections := buildVisualMaps(ontology)

	result, err := propertygraph.ShortestPath(ctx, engine.provider, engine.conn, engine.path, sourceID, targetID, propertygraph.NodePredicate(engine.provider.GraphDialect(), "n"), propertygraph.RelationshipPredicate(engine.provider.GraphDialect(), "r", "n", "m"))

	if err != nil {
		return nil, fmt.Errorf("path query failed: %w", err)
	}
	return buildSubgraph(result, nodeColors, edgeColors, edgeDirections, newOntologySemantics(ontology)), nil
}

// SyncConstraints applies native uniqueness constraints; creation must be operational before success.
func (s *GraphDataService) SyncConstraints(ctx context.Context, graphID, tenantID uint, entityTypeName string, nodeLabels []string, props []models.PropertyDefinition) error {
	_, target, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return err
	}
	defer target.close()
	if len(nodeLabels) == 0 {
		nodeLabels = []string{entityTypeName}
	}
	for _, prop := range props {
		if !prop.Unique {
			continue
		}
		if err := target.provider.EnsureGraphUniqueConstraint(ctx, target.conn, target.path, plugin.GraphConstraint{Name: fmt.Sprintf("graph_%d_%s_%s_unique", graphID, entityTypeName, prop.Name), Type: "UNIQUE", Label: nodeLabels[0], Properties: []string{prop.Name}}); err != nil {
			return err
		}
	}
	return nil
}
func (s *GraphDataService) GetConstraints(ctx context.Context, graphID, tenantID uint) ([]models.ConstraintInfo, error) {
	_, target, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return nil, err
	}
	defer target.close()
	constraints, err := target.provider.ListGraphConstraints(ctx, target.conn, target.path)
	if err != nil {
		return nil, err
	}
	out := make([]models.ConstraintInfo, 0, len(constraints))
	for _, d := range constraints {
		out = append(out, models.ConstraintInfo{Name: d.Name, Type: d.Type, EntityType: d.Label, Field: strings.Join(d.Properties, ",")})
	}
	return out, nil
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

// MergeEntity 将实体写入图数据库（MERGE 幂等写入）
// entityLabels: 图节点标签执行映射（如 ["Company", "POI"]）
// uniqueField: 唯一标识属性名（来自本体 PropertyDefinition.Unique=true）
// uniqueValue: 唯一标识属性值
// properties: 所有属性
// 返回 elementId
func (s *GraphDataService) MergeEntity(ctx context.Context, graphID, tenantID uint, entityLabels []string, uniqueField string, uniqueValue interface{}, properties map[string]interface{}) (string, error) {
	_, engine, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return "", err
	}
	defer engine.close()

	labelSelector := nodeLabelsPattern(entityLabels)
	if labelSelector == "" {
		return "", fmt.Errorf("entityLabels must not be empty")
	}

	params := map[string]interface{}{"key": uniqueValue, "props": nonEmptyProperties(properties)}
	cypher := fmt.Sprintf("MERGE (n%s {%s: $key}) ON CREATE SET n += $props, n._created_at = timestamp() ON MATCH SET n += $props, n._updated_at = timestamp() RETURN %s AS eid", labelSelector, propertygraph.Identifier(uniqueField), engine.provider.GraphDialect().ID("n"))
	result, err := engine.query(ctx, cypher, params)
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

// MergeRelation 将关系写入图数据库（MERGE 幂等写入）
// relType: 关系类型名
// srcLabels/tgtLabels: 源/目标节点的 图节点标签执行映射
// srcUniqueField/tgtUniqueField: 源/目标节点的唯一标识属性名
// srcUniqueVal/tgtUniqueVal: 源/目标节点的唯一标识属性值
// properties: 关系属性
// 返回 elementId（找不到节点时返回空字符串）
func (s *GraphDataService) MergeRelation(
	ctx context.Context,
	graphID, tenantID uint,
	relType string,
	srcLabels []string, srcUniqueField string, srcUniqueVal interface{},
	tgtLabels []string, tgtUniqueField string, tgtUniqueVal interface{},
	properties map[string]interface{},
) (string, error) {
	_, engine, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return "", err
	}
	defer engine.close()

	srcSelector, tgtSelector := nodeLabelsPattern(srcLabels), nodeLabelsPattern(tgtLabels)
	if srcSelector == "" || tgtSelector == "" {
		return "", fmt.Errorf("source and target labels must not be empty")
	}
	cypher := fmt.Sprintf("MATCH (a%s {%s: $source}), (b%s {%s: $target}) MERGE (a)-[r:%s]->(b) ON CREATE SET r += $props RETURN %s AS eid", srcSelector, propertygraph.Identifier(srcUniqueField), tgtSelector, propertygraph.Identifier(tgtUniqueField), propertygraph.Identifier(relType), engine.provider.GraphDialect().ID("r"))
	result, err := engine.query(ctx, cypher, map[string]interface{}{"source": srcUniqueVal, "target": tgtUniqueVal, "props": nonEmptyProperties(properties)})
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
func (s *GraphDataService) SyncSpatialLayer(ctx context.Context, graphID, tenantID uint, et models.EntityType) error {
	if !et.IsSpatialLayer {
		return fmt.Errorf("entity type %q is not a spatial layer type", et.Name)
	}
	_, engine, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return err
	}
	defer engine.close()

	if !engine.provider.GraphDialect().Spatial {
		return fmt.Errorf("target graph does not support spatial layers")
	}
	// 检查 spatial 插件是否可用
	checkResult, err := executeGraphQuery(ctx, engine,
		"CALL spatial.procedures() YIELD signature RETURN count(signature) AS cnt")
	if err != nil || len(checkResult.Rows) == 0 || toInt64(checkResult.Rows[0]["cnt"]) == 0 {
		return fmt.Errorf("目标 Neo4j 未安装 spatial 插件")
	}

	cfg := et.ParsedSpatialLayerConfig()
	layerName := cfg.LayerName

	// 查询已有图层
	layersResult, _ := executeGraphQuery(ctx, engine, "CALL spatial.layers() YIELD name")
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

	if _, err := executeGraphQuery(ctx, engine, createCypher); err != nil {
		return fmt.Errorf("创建空间图层 %q 失败: %w", layerName, err)
	}
	return nil
}

// SyncSpatialLayerByConfig 根据 SpatialLayerConfig 在 Neo4j 中创建空间图层（幂等，不要求 IsSpatialLayer=true）
func (s *GraphDataService) SyncSpatialLayerByConfig(ctx context.Context, graphID, tenantID uint, cfg *models.SpatialLayerConfig) error {
	if cfg == nil || cfg.LayerName == "" {
		return nil
	}
	_, engine, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return err
	}
	defer engine.close()

	if !engine.provider.GraphDialect().Spatial {
		return fmt.Errorf("target graph does not support spatial layers")
	}
	// 查询已有图层，幂等
	layersResult, _ := executeGraphQuery(ctx, engine, "CALL spatial.layers() YIELD name")
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
	if _, err := executeGraphQuery(ctx, engine, createCypher); err != nil {
		return fmt.Errorf("创建空间图层 %q 失败: %w", cfg.LayerName, err)
	}
	return nil
}

// AddNodeToSpatialLayer 将已写入的节点注册到空间图层（通过 elementId 定位节点）
func (s *GraphDataService) AddNodeToSpatialLayer(ctx context.Context, graphID, tenantID uint, elementId, layerName string) error {
	_, engine, err := s.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return err
	}
	defer engine.close()
	cypher := fmt.Sprintf(
		"MATCH (n) WHERE %s = '%s' CALL spatial.addNode('%s', n) YIELD node RETURN node",
		engine.provider.GraphDialect().ID("n"), escapeCypher(elementId), escapeCypher(layerName),
	)
	if _, err := executeGraphQuery(ctx, engine, cypher); err != nil {
		return fmt.Errorf("addNodeToSpatialLayer(%q, %q) failed: %w", elementId, layerName, err)
	}
	return nil
}

func nonEmptyProperties(props map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range props {
		if v == nil {
			continue
		}
		if text, ok := v.(string); ok && text == "" {
			continue
		}
		out[k] = v
	}
	return out
}
