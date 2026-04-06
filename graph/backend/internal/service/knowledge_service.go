package service

import (
	"context"
	"fmt"

	"github.com/addp/common/dbbridge"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
)

// KnowledgeService 知识服务：向外暴露知识图谱的语义化查询能力
type KnowledgeService struct {
	neo4jSvc     *Neo4jService
	ontologyRepo *repository.OntologyRepository
	graphRepo    *repository.KnowledgeGraphRepository
}

func NewKnowledgeService(
	neo4jSvc *Neo4jService,
	ontologyRepo *repository.OntologyRepository,
	graphRepo *repository.KnowledgeGraphRepository,
) *KnowledgeService {
	return &KnowledgeService{
		neo4jSvc:     neo4jSvc,
		ontologyRepo: ontologyRepo,
		graphRepo:    graphRepo,
	}
}

// GetGraphPublic 不限租户获取图谱（用于 is_public 场景）
func (s *KnowledgeService) GetGraphPublic(graphID uint) (*models.KnowledgeGraph, error) {
	return s.graphRepo.GetByIDGlobal(graphID)
}

// GetGraph 按租户获取图谱
func (s *KnowledgeService) GetGraph(graphID, tenantID uint) (*models.KnowledgeGraph, error) {
	return s.graphRepo.GetByID(graphID, tenantID)
}

// ListEntitiesByType 按本体类型查询实体列表（分页）
func (s *KnowledgeService) ListEntitiesByType(
	ctx context.Context,
	graphID, tenantID uint,
	entityTypeName string,
	page, pageSize int,
) ([]models.KSEntity, int64, error) {
	kg, engine, err := s.neo4jSvc.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, 0, err
	}

	countResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		fmt.Sprintf("MATCH (n:`%s`) RETURN count(n) AS total", escapeCypher(entityTypeName)))
	if err != nil {
		return nil, 0, fmt.Errorf("count failed: %w", err)
	}
	var total int64
	if len(countResult.Rows) > 0 {
		total = toInt64(countResult.Rows[0]["total"])
	}
	if total == 0 {
		return []models.KSEntity{}, 0, nil
	}

	skip := (page - 1) * pageSize
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		fmt.Sprintf("MATCH (n:`%s`) RETURN n SKIP %d LIMIT %d",
			escapeCypher(entityTypeName), skip, pageSize))
	if err != nil {
		return nil, 0, fmt.Errorf("list failed: %w", err)
	}

	typeLabel := s.resolveEntityTypeLabel(kg.OntologyID, tenantID, entityTypeName)
	entities := make([]models.KSEntity, 0)
	if result.GraphData != nil {
		for _, n := range result.GraphData.Nodes {
			entities = append(entities, models.KSEntity{
				ID:         n.ElementId,
				Type:       entityTypeName,
				TypeLabel:  typeLabel,
				Properties: n.Properties,
			})
		}
	}
	return entities, total, nil
}

// GetEntityDetail 获取实体详情（全属性）
func (s *KnowledgeService) GetEntityDetail(
	ctx context.Context,
	graphID, tenantID uint,
	nodeID string,
) (*models.KSEntity, error) {
	kg, engine, err := s.neo4jSvc.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		fmt.Sprintf("MATCH (n) WHERE elementId(n) = '%s' RETURN n", escapeCypher(nodeID)))
	if err != nil {
		return nil, fmt.Errorf("get entity failed: %w", err)
	}
	if result.GraphData == nil || len(result.GraphData.Nodes) == 0 {
		return nil, fmt.Errorf("entity not found: %s", nodeID)
	}

	n := result.GraphData.Nodes[0]
	entityType, typeLabel := s.enrichEntityType(kg.OntologyID, tenantID, n.Labels)
	return &models.KSEntity{
		ID:         n.ElementId,
		Type:       entityType,
		TypeLabel:  typeLabel,
		Properties: n.Properties,
	}, nil
}

// GetEntityNeighbors 获取跨类型邻居（含关系方向和类型）
func (s *KnowledgeService) GetEntityNeighbors(
	ctx context.Context,
	graphID, tenantID uint,
	nodeID string,
	limit int,
) (*models.KSNeighborsResponse, error) {
	kg, engine, err := s.neo4jSvc.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	// 获取中心节点
	nodeResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		fmt.Sprintf("MATCH (n) WHERE elementId(n) = '%s' RETURN n", escapeCypher(nodeID)))
	if err != nil || nodeResult.GraphData == nil || len(nodeResult.GraphData.Nodes) == 0 {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}
	centerNode := nodeResult.GraphData.Nodes[0]
	centerType, centerTypeLabel := s.enrichEntityType(kg.OntologyID, tenantID, centerNode.Labels)
	center := models.KSEntity{
		ID:         centerNode.ElementId,
		Type:       centerType,
		TypeLabel:  centerTypeLabel,
		Properties: centerNode.Properties,
	}

	// 查询邻居：显式返回 elementId(m) 以便对齐
	cypher := fmt.Sprintf(
		"MATCH (n)-[r]-(m) WHERE elementId(n) = '%s' "+
			"RETURN m, type(r) AS rel_type, (startNode(r) = n) AS is_out, elementId(m) AS m_eid "+
			"LIMIT %d",
		escapeCypher(nodeID), limit,
	)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("neighbors failed: %w", err)
	}

	// 构建 elementId → GraphNode 索引
	nodeIndex := make(map[string]models.KSEntity)
	if result.GraphData != nil {
		for _, n := range result.GraphData.Nodes {
			eType, eTypeLabel := s.enrichEntityType(kg.OntologyID, tenantID, n.Labels)
			nodeIndex[n.ElementId] = models.KSEntity{
				ID:         n.ElementId,
				Type:       eType,
				TypeLabel:  eTypeLabel,
				Properties: n.Properties,
			}
		}
	}

	relTypeLabels := s.resolveRelationTypeLabels(kg.OntologyID, tenantID)
	neighbors := make([]models.KSNeighborItem, 0, len(result.Rows))
	for _, row := range result.Rows {
		mEid := fmt.Sprintf("%v", row["m_eid"])
		entity, ok := nodeIndex[mEid]
		if !ok {
			continue
		}
		relType := fmt.Sprintf("%v", row["rel_type"])
		relLabel := relTypeLabels[relType]
		if relLabel == "" {
			relLabel = relType
		}
		direction := "out"
		if isOut, ok := row["is_out"]; ok {
			if b, ok2 := isOut.(bool); ok2 && !b {
				direction = "in"
			}
		}
		neighbors = append(neighbors, models.KSNeighborItem{
			Node:          entity,
			RelationType:  relType,
			RelationLabel: relLabel,
			Direction:     direction,
		})
	}

	return &models.KSNeighborsResponse{Node: center, Neighbors: neighbors}, nil
}

// SearchEntities 全文搜索（支持类型过滤，分页）
func (s *KnowledgeService) SearchEntities(
	ctx context.Context,
	graphID, tenantID uint,
	query, entityType string,
	page, pageSize int,
) ([]models.KSEntity, int64, error) {
	kg, engine, err := s.neo4jSvc.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, 0, err
	}

	typeFilter := ""
	if entityType != "" {
		typeFilter = fmt.Sprintf(" AND '%s' IN labels(n)", escapeCypher(entityType))
	}

	countCypher := fmt.Sprintf(
		"MATCH (n) WHERE any(key IN keys(n) WHERE toLower(toString(n[key])) CONTAINS toLower('%s'))%s "+
			"RETURN count(n) AS total",
		escapeCypher(query), typeFilter,
	)
	countResult, err := dbbridge.ExecuteGraphQuery(ctx, engine, countCypher)
	if err != nil {
		return nil, 0, fmt.Errorf("search count failed: %w", err)
	}
	var total int64
	if len(countResult.Rows) > 0 {
		total = toInt64(countResult.Rows[0]["total"])
	}
	if total == 0 {
		return []models.KSEntity{}, 0, nil
	}

	skip := (page - 1) * pageSize
	cypher := fmt.Sprintf(
		"MATCH (n) WHERE any(key IN keys(n) WHERE toLower(toString(n[key])) CONTAINS toLower('%s'))%s "+
			"RETURN n SKIP %d LIMIT %d",
		escapeCypher(query), typeFilter, skip, pageSize,
	)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, 0, fmt.Errorf("search failed: %w", err)
	}

	entities := make([]models.KSEntity, 0)
	if result.GraphData != nil {
		for _, n := range result.GraphData.Nodes {
			eType, eTypeLabel := s.enrichEntityType(kg.OntologyID, tenantID, n.Labels)
			entities = append(entities, models.KSEntity{
				ID:         n.ElementId,
				Type:       eType,
				TypeLabel:  eTypeLabel,
				Properties: n.Properties,
			})
		}
	}
	return entities, total, nil
}

// FindPaths 路径查找（复用现有 neo4jSvc.FindPath）
func (s *KnowledgeService) FindPaths(
	ctx context.Context,
	graphID, tenantID uint,
	req *models.KSPathRequest,
) (*models.SubgraphResult, error) {
	return s.neo4jSvc.FindPath(ctx, graphID, tenantID, req.SourceNodeID, req.TargetNodeID)
}

// GetSubgraph 实体中心子图（可配置深度，最大 3）
func (s *KnowledgeService) GetSubgraph(
	ctx context.Context,
	graphID, tenantID uint,
	req *models.KSSubgraphRequest,
) (*models.SubgraphResult, error) {
	kg, engine, err := s.neo4jSvc.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	nodeColors, edgeColors := s.neo4jSvc.buildColorMaps(kg.OntologyID, tenantID)

	depth := req.Depth
	if depth <= 0 {
		depth = 2
	}
	if depth > 3 {
		depth = 3
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// 收集 N 跳范围内节点，再查边
	cypher := fmt.Sprintf(
		"MATCH (n)-[r*1..%d]-(m) WHERE elementId(n) = '%s' "+
			"RETURN DISTINCT n, r, m LIMIT %d",
		depth, escapeCypher(req.NodeID), limit,
	)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		// 回退到简单展开
		return s.neo4jSvc.ExpandNode(ctx, graphID, tenantID, req.NodeID, limit)
	}
	return buildSubgraph(result, nodeColors, edgeColors), nil
}

// GetOntologyDescription 本体感知的图谱描述（PostgreSQL 本体 + Neo4j 统计）
func (s *KnowledgeService) GetOntologyDescription(
	ctx context.Context,
	graphID, tenantID uint,
) (*models.KSOntologyResponse, error) {
	kg, engine, err := s.neo4jSvc.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}

	ontology, err := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get ontology failed: %w", err)
	}

	// 各 label 节点数量
	labelCounts := make(map[string]int64)
	labelCountResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"MATCH (n) UNWIND labels(n) AS lbl RETURN lbl, count(n) AS cnt")
	if err == nil {
		for _, row := range labelCountResult.Rows {
			if lbl, ok := row["lbl"]; ok {
				if cnt, ok2 := row["cnt"]; ok2 {
					labelCounts[fmt.Sprintf("%v", lbl)] = toInt64(cnt)
				}
			}
		}
	}

	// 各关系类型数量
	relCounts := make(map[string]int64)
	relCountResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"MATCH ()-[r]->() RETURN type(r) AS rel_type, count(r) AS cnt")
	if err == nil {
		for _, row := range relCountResult.Rows {
			if rt, ok := row["rel_type"]; ok {
				if cnt, ok2 := row["cnt"]; ok2 {
					relCounts[fmt.Sprintf("%v", rt)] = toInt64(cnt)
				}
			}
		}
	}

	entityTypes := make([]models.KSEntityType, 0, len(ontology.EntityTypes))
	for _, et := range ontology.EntityTypes {
		props := make([]models.KSPropertyInfo, 0)
		if parsed, parseErr := et.ParsedProperties(); parseErr == nil {
			for _, p := range parsed {
				props = append(props, models.KSPropertyInfo{
					Name:     p.Name,
					DataType: p.DataType,
					Unique:   p.Unique,
					Required: p.Required,
				})
			}
		}
		entityTypes = append(entityTypes, models.KSEntityType{
			Name:       et.Name,
			Label:      et.Label,
			Properties: props,
			Count:      labelCounts[et.Name],
		})
	}

	relationTypes := make([]models.KSRelationType, 0, len(ontology.RelationTypes))
	for _, rt := range ontology.RelationTypes {
		sourceTypeName := ""
		targetTypeName := ""
		if rt.SourceType != nil {
			sourceTypeName = rt.SourceType.Name
		}
		if rt.TargetType != nil {
			targetTypeName = rt.TargetType.Name
		}
		relationTypes = append(relationTypes, models.KSRelationType{
			Name:       rt.Name,
			Label:      rt.Label,
			SourceType: sourceTypeName,
			TargetType: targetTypeName,
			Count:      relCounts[rt.Name],
		})
	}

	return &models.KSOntologyResponse{
		GraphName:     kg.Name,
		OntologyName:  ontology.Name,
		EntityTypes:   entityTypes,
		RelationTypes: relationTypes,
	}, nil
}

// GetStats 复用 neo4jSvc.GetStats
func (s *KnowledgeService) GetStats(ctx context.Context, graphID, tenantID uint) (*models.BrowseStats, error) {
	return s.neo4jSvc.GetStats(ctx, graphID, tenantID)
}

// ─── 内部辅助 ───────────────────────────────────────────

func (s *KnowledgeService) enrichEntityType(ontologyID, tenantID uint, labels []string) (string, string) {
	if len(labels) == 0 {
		return "", ""
	}
	ontology, err := s.ontologyRepo.GetDetail(ontologyID, tenantID)
	if err != nil {
		return labels[0], labels[0]
	}
	for _, lbl := range labels {
		for _, et := range ontology.EntityTypes {
			if et.Name == lbl {
				label := et.Label
				if label == "" {
					label = et.Name
				}
				return et.Name, label
			}
		}
	}
	return labels[0], labels[0]
}

func (s *KnowledgeService) resolveEntityTypeLabel(ontologyID, tenantID uint, entityTypeName string) string {
	ontology, err := s.ontologyRepo.GetDetail(ontologyID, tenantID)
	if err != nil {
		return entityTypeName
	}
	for _, et := range ontology.EntityTypes {
		if et.Name == entityTypeName {
			if et.Label != "" {
				return et.Label
			}
			return et.Name
		}
	}
	return entityTypeName
}

func (s *KnowledgeService) resolveRelationTypeLabels(ontologyID, tenantID uint) map[string]string {
	m := make(map[string]string)
	ontology, err := s.ontologyRepo.GetDetail(ontologyID, tenantID)
	if err != nil {
		return m
	}
	for _, rt := range ontology.RelationTypes {
		label := rt.Label
		if label == "" {
			label = rt.Name
		}
		m[rt.Name] = label
	}
	return m
}
