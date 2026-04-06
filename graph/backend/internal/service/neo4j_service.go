package service

import (
	"context"
	"fmt"
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

// buildColorMaps 从本体中构建 label→颜色 和 relType→颜色 的映射
func (s *Neo4jService) buildColorMaps(ontologyID, tenantID uint) (nodeColors, edgeColors map[string]string) {
	nodeColors = make(map[string]string)
	edgeColors = make(map[string]string)
	ontology, err := s.ontologyRepo.GetDetail(ontologyID, tenantID)
	if err != nil {
		return
	}
	for _, et := range ontology.EntityTypes {
		if et.Color != "" {
			nodeColors[et.Name] = et.Color
		}
	}
	for _, rt := range ontology.RelationTypes {
		if rt.Color != "" {
			edgeColors[rt.Name] = rt.Color
		}
	}
	return
}

// GetSchema 获取图谱的 Schema（节点标签 + 关系类型）
func (s *Neo4jService) GetSchema(ctx context.Context, graphID, tenantID uint) (*models.BrowseSchema, error) {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}

	labelsResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"CALL db.labels() YIELD label RETURN label ORDER BY label")
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}
	labels := make([]string, 0, len(labelsResult.Rows))
	for _, row := range labelsResult.Rows {
		if v, ok := row["label"]; ok {
			labels = append(labels, fmt.Sprintf("%v", v))
		}
	}

	relResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"CALL db.relationshipTypes() YIELD relationshipType RETURN relationshipType ORDER BY relationshipType")
	if err != nil {
		return nil, fmt.Errorf("failed to get relationship types: %w", err)
	}
	relTypes := make([]string, 0, len(relResult.Rows))
	for _, row := range relResult.Rows {
		if v, ok := row["relationshipType"]; ok {
			relTypes = append(relTypes, fmt.Sprintf("%v", v))
		}
	}

	return &models.BrowseSchema{Labels: labels, RelTypes: relTypes}, nil
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

	edgeResult, err := dbbridge.ExecuteGraphQuery(ctx, engine, "MATCH ()-[r]->() RETURN count(r) AS total")
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
		"MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 100")
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
	cypher := fmt.Sprintf(
		"MATCH (n) WHERE ANY(key IN keys(n) WHERE toString(n[key]) CONTAINS '%s') RETURN n LIMIT %d",
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
		"MATCH (n)-[r]-(m) WHERE elementId(n) = '%s' RETURN n, r, m LIMIT %d",
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
		"MATCH p = shortestPath((a)-[*..10]-(b)) WHERE elementId(a) = '%s' AND elementId(b) = '%s' RETURN p",
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
// 注意：NOT NULL 约束需 Neo4j 企业版，此处仅同步 UNIQUE 约束
func (s *Neo4jService) SyncConstraints(ctx context.Context, graphID, tenantID uint, entityTypeName string, props []models.PropertyDefinition) error {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return err
	}
	for _, prop := range props {
		if !prop.Unique {
			continue
		}
		constraintName := fmt.Sprintf("graph_%d_%s_%s_unique", graphID, entityTypeName, prop.Name)
		cypher := fmt.Sprintf(
			"CREATE CONSTRAINT %s IF NOT EXISTS FOR (n:%s) REQUIRE n.%s IS UNIQUE",
			constraintName, entityTypeName, prop.Name,
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
		// 取第一个 label 的显示名作为节点标签
		if len(n.Labels) > 0 {
			dto.EntityType = n.Labels[0]
			if color, ok := nodeColors[n.Labels[0]]; ok {
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
		parts = append(parts, fmt.Sprintf("%s.%s = %s", alias, escapeCypher(k), cypherValue(v)))
	}
	return strings.Join(parts, ", ")
}

// MergeEntity 将实体写入 Neo4j（MERGE 幂等写入）
// entityType: Neo4j 标签（对应本体实体类型 Name）
// uniqueField: 唯一标识属性名（来自本体 PropertyDefinition.Unique=true）
// uniqueValue: 唯一标识属性值
// properties: 所有属性
// 返回 elementId
func (s *Neo4jService) MergeEntity(ctx context.Context, graphID, tenantID uint, entityType, uniqueField string, uniqueValue interface{}, properties map[string]interface{}) (string, error) {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return "", err
	}

	propSet := buildSetClause("n", properties)
	var setPart string
	if propSet != "" {
		setPart = fmt.Sprintf("ON CREATE SET %s, n._created_at = timestamp()\nON MATCH  SET %s, n._updated_at = timestamp()", propSet, propSet)
	} else {
		setPart = "ON CREATE SET n._created_at = timestamp()\nON MATCH SET n._updated_at = timestamp()"
	}

	cypher := fmt.Sprintf(
		"MERGE (n:%s {%s: %s})\n%s\nRETURN elementId(n) AS eid",
		entityType, escapeCypher(uniqueField), cypherValue(uniqueValue), setPart,
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
// srcType/tgtType: 源/目标节点的 Neo4j 标签
// srcUniqueField/tgtUniqueField: 源/目标节点的唯一标识属性名
// srcUniqueVal/tgtUniqueVal: 源/目标节点的唯一标识属性值
// properties: 关系属性
// 返回 elementId（找不到节点时返回空字符串）
func (s *Neo4jService) MergeRelation(
	ctx context.Context,
	graphID, tenantID uint,
	relType string,
	srcType, srcUniqueField string, srcUniqueVal interface{},
	tgtType, tgtUniqueField string, tgtUniqueVal interface{},
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

	cypher := fmt.Sprintf(
		"MATCH (a:%s {%s: %s}), (b:%s {%s: %s})\nMERGE (a)-[r:%s]->(b)\n%s\nRETURN elementId(r) AS eid",
		srcType, escapeCypher(srcUniqueField), cypherValue(srcUniqueVal),
		tgtType, escapeCypher(tgtUniqueField), cypherValue(tgtUniqueVal),
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
