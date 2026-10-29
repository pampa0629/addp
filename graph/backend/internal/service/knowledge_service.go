package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/propertygraph"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
)

// KnowledgeService 知识服务：向外暴露知识图谱的语义化查询能力
type KnowledgeService struct {
	graphDataSvc *GraphDataService
	ontologyRepo *repository.OntologyRepository
	graphRepo    *repository.KnowledgeGraphRepository
}

func NewKnowledgeService(
	graphDataSvc *GraphDataService,
	ontologyRepo *repository.OntologyRepository,
	graphRepo *repository.KnowledgeGraphRepository,
) *KnowledgeService {
	return &KnowledgeService{
		graphDataSvc: graphDataSvc,
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
	kg, engine, err := s.graphDataSvc.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return nil, 0, err
	}
	defer engine.close()

	ontology, _ := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	semantics := newOntologySemantics(ontology)
	entityLabels := entityTypeNodeLabels(ontology, entityTypeName)
	countResult, err := executeGraphQuery(ctx, engine,
		fmt.Sprintf("MATCH (n%s) WHERE %s RETURN count(n) AS total", nodeLabelsPattern(entityLabels), propertygraph.NodePredicate(engine.provider.GraphDialect(), "n")))
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
	result, err := executeGraphQuery(ctx, engine,
		fmt.Sprintf("MATCH (n%s) WHERE %s RETURN n SKIP %d LIMIT %d",
			nodeLabelsPattern(entityLabels), propertygraph.NodePredicate(engine.provider.GraphDialect(), "n"), skip, pageSize))
	if err != nil {
		return nil, 0, fmt.Errorf("list failed: %w", err)
	}

	typeLabel := entityTypeName
	if entityType, ok := semantics.entityType(entityTypeName); ok {
		typeLabel = entityType.Label
	}
	entities := make([]models.KSEntity, 0)
	if result.GraphData != nil {
		for _, n := range result.GraphData.Nodes {
			entities = append(entities, models.KSEntity{
				ID:          n.ElementId,
				DisplayName: semantics.displayName(n.Labels, n.Properties, n.ElementId),
				Type:        entityTypeName,
				TypeLabel:   typeLabel,
				Properties:  n.Properties,
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
	kg, engine, err := s.graphDataSvc.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return nil, err
	}
	defer engine.close()

	result, err := executeGraphQuery(ctx, engine,
		fmt.Sprintf("MATCH (n) WHERE %s = '%s' AND %s RETURN n", engine.provider.GraphDialect().ID("n"), escapeCypher(nodeID), propertygraph.NodePredicate(engine.provider.GraphDialect(), "n")))
	if err != nil {
		return nil, fmt.Errorf("get entity failed: %w", err)
	}
	if result.GraphData == nil || len(result.GraphData.Nodes) == 0 {
		return nil, fmt.Errorf("entity not found: %s", nodeID)
	}

	n := result.GraphData.Nodes[0]
	ontology, _ := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	semantics := newOntologySemantics(ontology)
	entityType, typeLabel := semantics.resolveEntityType(n.Labels)
	return &models.KSEntity{
		ID:          n.ElementId,
		DisplayName: semantics.displayName(n.Labels, n.Properties, n.ElementId),
		Type:        entityType,
		TypeLabel:   typeLabel,
		Properties:  n.Properties,
	}, nil
}

// GetEntityNeighbors 获取跨类型邻居（含关系方向和类型）
func (s *KnowledgeService) GetEntityNeighbors(
	ctx context.Context,
	graphID, tenantID uint,
	nodeID string,
	limit int,
) (*models.KSNeighborsResponse, error) {
	kg, engine, err := s.graphDataSvc.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return nil, err
	}
	defer engine.close()
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	ontology, _ := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	semantics := newOntologySemantics(ontology)

	// 获取中心节点
	nodeResult, err := executeGraphQuery(ctx, engine,
		fmt.Sprintf("MATCH (n) WHERE %s = '%s' AND %s RETURN n", engine.provider.GraphDialect().ID("n"), escapeCypher(nodeID), propertygraph.NodePredicate(engine.provider.GraphDialect(), "n")))
	if err != nil || nodeResult.GraphData == nil || len(nodeResult.GraphData.Nodes) == 0 {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}
	centerNode := nodeResult.GraphData.Nodes[0]
	centerType, centerTypeLabel := semantics.resolveEntityType(centerNode.Labels)
	center := models.KSEntity{
		ID:          centerNode.ElementId,
		DisplayName: semantics.displayName(centerNode.Labels, centerNode.Properties, centerNode.ElementId),
		Type:        centerType,
		TypeLabel:   centerTypeLabel,
		Properties:  centerNode.Properties,
	}

	// 查询邻居：显式返回 elementId(m) 以便对齐
	cypher := fmt.Sprintf(
		"MATCH (n)-[r]-(m) WHERE %s = '%s' AND "+propertygraph.RelationshipPredicate(engine.provider.GraphDialect(), "r", "n", "m")+" "+
			"RETURN m, type(r) AS rel_type, (startNode(r) = n) AS is_out, %s AS m_eid "+
			"LIMIT %d",
		engine.provider.GraphDialect().ID("n"), escapeCypher(nodeID), engine.provider.GraphDialect().ID("m"), limit,
	)
	result, err := executeGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("neighbors failed: %w", err)
	}

	// 构建 elementId → GraphNode 索引
	nodeIndex := make(map[string]models.KSEntity)
	if result.GraphData != nil {
		for _, n := range result.GraphData.Nodes {
			eType, eTypeLabel := semantics.resolveEntityType(n.Labels)
			nodeIndex[n.ElementId] = models.KSEntity{
				ID:          n.ElementId,
				DisplayName: semantics.displayName(n.Labels, n.Properties, n.ElementId),
				Type:        eType,
				TypeLabel:   eTypeLabel,
				Properties:  n.Properties,
			}
		}
	}

	neighbors := make([]models.KSNeighborItem, 0, len(result.Rows))
	for _, row := range result.Rows {
		mEid := fmt.Sprintf("%v", row["m_eid"])
		entity, ok := nodeIndex[mEid]
		if !ok {
			continue
		}
		relType := fmt.Sprintf("%v", row["rel_type"])
		relLabel := semantics.relationLabel[relType]
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
	kg, engine, err := s.graphDataSvc.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return nil, 0, err
	}
	defer engine.close()

	ontology, err := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("get search ontology failed: %w", err)
	}
	semantics := newOntologySemantics(ontology)
	allDefinitions := buildSearchIndexDefinitions(graphID, ontology)
	if strings.TrimSpace(query) == "" {
		return []models.KSEntity{}, 0, nil
	}
	indexes, err := s.graphDataSvc.syncSearchIndexes(ctx, engine, kg, allDefinitions)
	if err != nil {
		return nil, 0, err
	}
	for i := range allDefinitions {
		allDefinitions[i].Name = indexes[i].Name
	}
	definitions := allDefinitions
	if entityType != "" {
		filtered := definitions[:0]
		for _, definition := range definitions {
			if definition.EntityType == entityType {
				filtered = append(filtered, definition)
			}
		}
		definitions = filtered
	}
	if len(definitions) == 0 {
		return []models.KSEntity{}, 0, nil
	}
	selected := make([]plugin.GraphSearchIndex, 0, len(definitions))
	for _, d := range definitions {
		selected = append(selected, plugin.GraphSearchIndex{Name: d.Name, Labels: d.Labels, Properties: d.Properties})
	}
	result, total, err := propertygraph.Search(ctx, engine.provider, engine.conn, engine.path, selected, query, propertygraph.NodePredicate(engine.provider.GraphDialect(), "node"), (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, err
	}

	entities := make([]models.KSEntity, 0)
	if result.GraphData != nil {
		for _, n := range result.GraphData.Nodes {
			eType, eTypeLabel := semantics.resolveEntityType(n.Labels)
			entities = append(entities, models.KSEntity{
				ID:          n.ElementId,
				DisplayName: semantics.displayName(n.Labels, n.Properties, n.ElementId),
				Type:        eType,
				TypeLabel:   eTypeLabel,
				Properties:  n.Properties,
			})
		}
	}
	return entities, total, nil
}

// FindPaths 路径查找（复用现有 graphDataSvc.FindPath）
func (s *KnowledgeService) FindPaths(
	ctx context.Context,
	graphID, tenantID uint,
	req *models.KSPathRequest,
) (*models.SubgraphResult, error) {
	return s.graphDataSvc.FindPath(ctx, graphID, tenantID, req.SourceNodeID, req.TargetNodeID)
}

// GetSubgraph 实体中心子图（可配置深度，最大 3）
func (s *KnowledgeService) GetSubgraph(
	ctx context.Context,
	graphID, tenantID uint,
	req *models.KSSubgraphRequest,
) (*models.SubgraphResult, error) {
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

	return s.graphDataSvc.Expand(ctx, graphID, tenantID, &models.ExpandRequest{
		Target:            models.ExpandTarget{Kind: "entity", ID: req.NodeID},
		Depth:             depth,
		NodeLimit:         limit,
		RelationshipLimit: min(limit*2, MaxExpandRelationshipLimit),
	})
}

// GetOntologyDescription 本体感知的图谱描述（PostgreSQL 本体 + Neo4j 统计）
func (s *KnowledgeService) GetOntologyDescription(
	ctx context.Context,
	graphID, tenantID uint,
) (*models.KSOntologyResponse, error) {
	kg, err := s.graphRepo.GetByID(graphID, tenantID)
	if err != nil {
		return nil, err
	}

	ontology, err := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get ontology failed: %w", err)
	}

	snapshot, err := s.graphDataSvc.GetBrowseSnapshot(ctx, graphID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get browse snapshot failed: %w", err)
	}
	nodeCounts := make(map[string]int64, len(snapshot.Schema.NodeShapes))
	for _, shape := range snapshot.Schema.NodeShapes {
		if shape.Count != nil {
			nodeCounts[nodeLabelsKey(shape.Labels)] = *shape.Count
		}
	}
	relCounts := make(map[string]int64, len(snapshot.Schema.RelationshipShapes))
	for _, shape := range snapshot.Schema.RelationshipShapes {
		if shape.Count != nil {
			relCounts[shape.Type] = *shape.Count
		}
	}

	entityTypesByID := entityTypeByID(ontology.EntityTypes)
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
			Name:            et.Name,
			Label:           et.Label,
			DisplayProperty: et.DisplayProperty,
			Properties:      props,
			Count:           nodeCounts[nodeLabelsKey(effectiveNodeLabels(&et, entityTypesByID))],
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

// GetStats 复用统一浏览快照。
func (s *KnowledgeService) GetStats(ctx context.Context, graphID, tenantID uint) (*models.BrowseStats, error) {
	snapshot, err := s.graphDataSvc.GetBrowseSnapshot(ctx, graphID, tenantID)
	if err != nil {
		return nil, err
	}
	return &snapshot.Stats, nil
}
