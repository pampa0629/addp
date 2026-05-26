package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
)

// Neo4jService 提供面向知识图谱的 Neo4j 查询能力
type Neo4jService struct {
	graphRepo    *repository.KnowledgeGraphRepository
	ontologyRepo *repository.OntologyRepository
	systemClient *commonClient.SystemClient
}

func NewNeo4jService(
	graphRepo *repository.KnowledgeGraphRepository,
	ontologyRepo *repository.OntologyRepository,
	systemClient *commonClient.SystemClient,
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
	engine, err := s.systemClient.GetEngine(kg.EngineID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get engine (id=%d): %w", kg.EngineID, err)
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

// buildColorMaps 从本体中构建 node shape→颜色 和 relType→颜色 的映射
func (s *Neo4jService) buildColorMaps(ontologyID, tenantID uint) (nodeColors, edgeColors map[string]string) {
	nodeColors = make(map[string]string)
	edgeColors = make(map[string]string)
	ontology, err := s.ontologyRepo.GetDetail(ontologyID, tenantID)
	if err != nil {
		return
	}
	byID := entityTypeByID(ontology.EntityTypes)
	for _, et := range ontology.EntityTypes {
		if et.Color != "" {
			nodeColors[et.Name] = et.Color
			if labels := effectiveNodeLabels(&et, byID); len(labels) > 0 {
				nodeColors[endpointShapeName(labels)] = et.Color
			}
		}
	}
	for _, rt := range ontology.RelationTypes {
		if rt.Color != "" {
			edgeColors[rt.Name] = rt.Color
		}
	}
	return
}

// GetSchema 获取图谱的 Schema（节点形状 + 关系类型）
func (s *Neo4jService) GetSchema(ctx context.Context, graphID, tenantID uint) (*models.BrowseSchema, error) {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}

	nodeShapeResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"MATCH (n) RETURN labels(n) AS labels, count(n) AS cnt ORDER BY cnt DESC LIMIT 500")
	if err != nil {
		return nil, fmt.Errorf("failed to get node shapes: %w", err)
	}
	nodeShapes := make([]models.NodeShapeDTO, 0, len(nodeShapeResult.Rows))
	for _, row := range nodeShapeResult.Rows {
		labels := interfaceToStringSlice(row["labels"])
		if len(labels) == 0 {
			continue
		}
		count := toInt64(row["cnt"])
		nodeShapes = append(nodeShapes, models.NodeShapeDTO{
			Name:   endpointShapeName(labels),
			Kind:   "label_set",
			Labels: labels,
			Count:  &count,
		})
	}

	relResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		`MATCH (a)-[r]->(b)
		WITH type(r) AS relType, labels(a) AS fromLabels, labels(b) AS toLabels, count(r) AS cnt
		WHERE NOT (relType IN `+internalRelationshipTypeList+`)
		RETURN relType, fromLabels, toLabels, cnt
		ORDER BY relType, cnt DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to get relationship types: %w", err)
	}
	relationshipShapes := make([]models.RelationshipShapeDTO, 0)
	relationshipShapeByType := make(map[string]int)
	for _, row := range relResult.Rows {
		relType := fmt.Sprintf("%v", row["relType"])
		if relType == "" || relType == "<nil>" || isInternalRelationshipType(relType) {
			continue
		}
		index, ok := relationshipShapeByType[relType]
		if !ok {
			index = len(relationshipShapes)
			relationshipShapes = append(relationshipShapes, models.RelationshipShapeDTO{Type: relType})
			relationshipShapeByType[relType] = index
		}
		count := toInt64(row["cnt"])
		relationshipShapes[index].Count = addInt64Ptr(relationshipShapes[index].Count, count)
		fromLabels := interfaceToStringSlice(row["fromLabels"])
		toLabels := interfaceToStringSlice(row["toLabels"])
		if len(fromLabels) > 0 || len(toLabels) > 0 {
			relationshipShapes[index].Patterns = append(relationshipShapes[index].Patterns, models.RelationshipPatternDTO{
				From:  models.GraphEndpointDTO{ShapeName: endpointShapeName(fromLabels), Labels: fromLabels},
				To:    models.GraphEndpointDTO{ShapeName: endpointShapeName(toLabels), Labels: toLabels},
				Count: &count,
			})
		}
	}

	return &models.BrowseSchema{
		NodeShapes:         nodeShapes,
		RelationshipShapes: relationshipShapes,
	}, nil
}

// GetStats 获取图谱统计（总节点数、总关系数、按标签分组节点数）
func (s *Neo4jService) GetStats(ctx context.Context, graphID, tenantID uint) (*models.BrowseStats, error) {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	stats := &models.BrowseStats{ByLabel: make(map[string]int64)}

	nodeResult, err := dbbridge.ExecuteGraphQuery(ctx, engine, "MATCH (n) RETURN count(n) AS total")
	if err != nil {
		return nil, fmt.Errorf("failed to count nodes: %w", err)
	}
	if len(nodeResult.Rows) > 0 {
		stats.NodeCount = toInt64(nodeResult.Rows[0]["total"])
	}

	edgeResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"MATCH ()-[r]->() WHERE NOT ("+internalRelationshipTypePredicate+") RETURN count(r) AS total")
	if err != nil {
		return nil, fmt.Errorf("failed to count edges: %w", err)
	}
	if len(edgeResult.Rows) > 0 {
		stats.EdgeCount = toInt64(edgeResult.Rows[0]["total"])
	}

	labelResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"MATCH (n) UNWIND labels(n) AS lbl RETURN lbl, count(n) AS cnt")
	if err == nil {
		for _, row := range labelResult.Rows {
			if lbl, ok := row["lbl"]; ok {
				if cnt, ok2 := row["cnt"]; ok2 {
					stats.ByLabel[fmt.Sprintf("%v", lbl)] = toInt64(cnt)
				}
			}
		}
	}
	return stats, nil
}

// GetOverview 获取图谱概览子图（采样关系，若无则退回到采样节点）
func (s *Neo4jService) GetOverview(ctx context.Context, graphID, tenantID uint) (*models.SubgraphResult, error) {
	kg, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	nodeColors, edgeColors := s.buildColorMaps(kg.OntologyID, tenantID)

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"MATCH (n)-[r]->(m) WHERE NOT ("+internalRelationshipTypePredicate+") RETURN n, r, m LIMIT 100")
	if err != nil {
		return nil, fmt.Errorf("failed to get overview: %w", err)
	}

	// 无关系则退回到仅节点
	if result.GraphData == nil || len(result.GraphData.Nodes) == 0 {
		result, err = dbbridge.ExecuteGraphQuery(ctx, engine, "MATCH (n) RETURN n LIMIT 50")
		if err != nil {
			return nil, fmt.Errorf("failed to get overview nodes: %w", err)
		}
	}

	return buildSubgraph(result, nodeColors, edgeColors), nil
}

// SearchNodes 全文搜索：在所有节点属性中匹配关键词
func (s *Neo4jService) SearchNodes(ctx context.Context, graphID, tenantID uint, query string, limit int) (*models.SubgraphResult, error) {
	kg, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	nodeColors, edgeColors := s.buildColorMaps(kg.OntologyID, tenantID)

	if limit <= 0 || limit > 50 {
		limit = 30
	}
	// 只对字符串属性搜索，避免数组类型传入 toString 时触发 Neo4j TypeError
	cypher := fmt.Sprintf(
		"MATCH (n) WHERE ANY(key IN keys(n) WHERE valueType(n[key]) STARTS WITH 'STRING' AND n[key] CONTAINS '%s') RETURN n LIMIT %d",
		escapeCypher(query), limit,
	)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	return buildSubgraph(result, nodeColors, edgeColors), nil
}

// ExpandNode 展开节点的 1 跳邻居
func (s *Neo4jService) ExpandNode(ctx context.Context, graphID, tenantID uint, nodeID string, limit int) (*models.SubgraphResult, error) {
	kg, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	nodeColors, edgeColors := s.buildColorMaps(kg.OntologyID, tenantID)

	if limit <= 0 || limit > 200 {
		limit = 100
	}
	cypher := fmt.Sprintf(
		"MATCH (n)-[r]-(m) WHERE elementId(n) = '%s' AND NOT ("+internalRelationshipTypePredicate+") RETURN n, r, m LIMIT %d",
		escapeCypher(nodeID), limit,
	)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("expand failed: %w", err)
	}
	return buildSubgraph(result, nodeColors, edgeColors), nil
}

// FindPath 查找两节点间的最短路径（最多 10 跳）
func (s *Neo4jService) FindPath(ctx context.Context, graphID, tenantID uint, sourceID, targetID string) (*models.SubgraphResult, error) {
	kg, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	nodeColors, edgeColors := s.buildColorMaps(kg.OntologyID, tenantID)

	cypher := fmt.Sprintf(
		"MATCH p = shortestPath((a)-[*..10]-(b)) WHERE elementId(a) = '%s' AND elementId(b) = '%s' AND NONE(rel IN relationships(p) WHERE type(rel) IN "+internalRelationshipTypeList+") RETURN p",
		escapeCypher(sourceID), escapeCypher(targetID),
	)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("path query failed: %w", err)
	}
	return buildSubgraph(result, nodeColors, edgeColors), nil
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

// buildSubgraph 将 GraphQueryResult 转换为 SubgraphResult（含本体着色）
func buildSubgraph(result *plugin.GraphQueryResult, nodeColors, edgeColors map[string]string) *models.SubgraphResult {
	out := &models.SubgraphResult{
		Nodes: []models.GraphNodeDTO{},
		Edges: []models.GraphEdgeDTO{},
	}
	if result == nil || result.GraphData == nil {
		return out
	}

	for _, n := range result.GraphData.Nodes {
		dto := models.GraphNodeDTO{
			ID:         n.ElementId,
			Labels:     n.Labels,
			Color:      "#5B8FF9", // default
			Properties: n.Properties,
		}
		// 优先使用完整 label set 对应的 node shape，单 label 作为兼容兜底。
		if len(n.Labels) > 0 {
			shapeName := endpointShapeName(n.Labels)
			dto.EntityType = shapeName
			if color, ok := nodeColors[shapeName]; ok {
				dto.Color = color
			} else if color, ok := nodeColors[n.Labels[0]]; ok {
				dto.Color = color
			}
		}
		// 从属性中提取展示名
		for _, key := range []string{"name", "title", "label", "id"} {
			if v, ok := n.Properties[key]; ok && v != nil {
				dto.DisplayName = fmt.Sprintf("%v", v)
				break
			}
		}
		if dto.DisplayName == "" && len(n.Labels) > 0 {
			dto.DisplayName = n.Labels[0]
		}
		out.Nodes = append(out.Nodes, dto)
	}

	for _, r := range result.GraphData.Relationships {
		if isInternalRelationshipType(r.Type) {
			continue
		}
		dto := models.GraphEdgeDTO{
			ID:           r.ElementId,
			Type:         r.Type,
			RelationType: r.Type,
			Color:        "#C0C0C0", // default
			Source:       r.StartNodeId,
			Target:       r.EndNodeId,
			Properties:   r.Properties,
		}
		if color, ok := edgeColors[r.Type]; ok {
			dto.Color = color
		}
		out.Edges = append(out.Edges, dto)
	}
	return out
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
