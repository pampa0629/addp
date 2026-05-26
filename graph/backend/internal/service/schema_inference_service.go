package service

import (
	"context"
	"fmt"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
)

// SchemaInferenceService 从 Neo4j 现有数据推导本体
type SchemaInferenceService struct {
	graphRepo    *repository.KnowledgeGraphRepository
	ontologyRepo *repository.OntologyRepository
	neo4jSvc     *Neo4jService
	ontologySvc  *OntologyService
	systemClient *commonClient.SystemClient
}

func NewSchemaInferenceService(
	graphRepo *repository.KnowledgeGraphRepository,
	ontologyRepo *repository.OntologyRepository,
	neo4jSvc *Neo4jService,
	ontologySvc *OntologyService,
	systemClient *commonClient.SystemClient,
) *SchemaInferenceService {
	return &SchemaInferenceService{
		graphRepo:    graphRepo,
		ontologyRepo: ontologyRepo,
		neo4jSvc:     neo4jSvc,
		ontologySvc:  ontologySvc,
		systemClient: systemClient,
	}
}

// InferredEntityType 推导出的实体类型预览
type InferredEntityType struct {
	Label      string                      `json:"label"`      // Neo4j 标签名
	Properties []models.PropertyDefinition `json:"properties"` // 采样的属性
	Count      int64                       `json:"count"`      // 节点数量
	Exists     bool                        `json:"exists"`     // 本体中是否已存在
}

// InferredRelationType 推导出的关系类型预览
type InferredRelationType struct {
	Name        string `json:"name"`         // 关系类型名
	SourceLabel string `json:"source_label"` // 来源标签
	TargetLabel string `json:"target_label"` // 目标标签
	Count       int64  `json:"count"`
	Exists      bool   `json:"exists"`
}

// SchemaInferencePreview 推导结果预览
type SchemaInferencePreview struct {
	EntityTypes   []InferredEntityType   `json:"entity_types"`
	RelationTypes []InferredRelationType `json:"relation_types"`
}

// ApplyInferredSchemaRequest 应用推导结果到本体（从 graph_id）
type ApplyInferredSchemaRequest struct {
	OntologyID        uint     `json:"ontology_id" binding:"required"`
	EntityTypeLabels  []string `json:"entity_type_names"`
	RelationTypeNames []string `json:"relation_type_names"`
	Conflict          string   `json:"conflict"` // "skip" | "overwrite"
}

// ApplyInferredSchemaFromEngineRequest 应用推导结果到本体（从 engine_id）
type ApplyInferredSchemaFromEngineRequest struct {
	EngineID          uint     `json:"engine_id" binding:"required"`
	EntityTypeLabels  []string `json:"entity_type_names"`
	RelationTypeNames []string `json:"relation_type_names"`
	Conflict          string   `json:"conflict"` // "skip" | "overwrite"
}

// ListNeo4jEngines 返回当前租户下所有 Neo4j 引擎
func (s *SchemaInferenceService) ListNeo4jEngines(tenantID uint) ([]commonModels.Engine, error) {
	engines, err := s.systemClient.ListEngines("neo4j", tenantID)
	if err != nil {
		return nil, fmt.Errorf("list neo4j engines: %w", err)
	}
	return engines, nil
}

// InferSchema 从指定知识图谱推导 Schema（不写库，仅预览）
func (s *SchemaInferenceService) InferSchema(ctx context.Context, graphID, tenantID uint, ontologyID *uint) (*SchemaInferencePreview, error) {
	_, engine, err := s.neo4jSvc.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	return s.inferWithEngine(ctx, engine, tenantID, ontologyID)
}

// InferSchemaFromEngine 直接从 Neo4j 引擎推导 Schema（不依赖知识图谱）
func (s *SchemaInferenceService) InferSchemaFromEngine(ctx context.Context, engineID, tenantID uint, ontologyID *uint) (*SchemaInferencePreview, error) {
	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("get engine %d: %w", engineID, err)
	}
	return s.inferWithEngine(ctx, engine, tenantID, ontologyID)
}

// inferWithEngine 核心推导逻辑，从给定 engine 执行 Cypher 推导
func (s *SchemaInferenceService) inferWithEngine(ctx context.Context, engine *commonModels.Engine, tenantID uint, ontologyID *uint) (*SchemaInferencePreview, error) {
	preview := &SchemaInferencePreview{}

	// 构建已有本体的 name set（用于标注 exists）
	existingEntityNames := make(map[string]bool)
	existingRelNames := make(map[string]bool)
	if ontologyID != nil {
		ets, _ := s.ontologySvc.ListEntityTypes(*ontologyID, tenantID)
		for _, et := range ets {
			existingEntityNames[et.Name] = true
		}
		rts, _ := s.ontologySvc.ListRelationTypes(*ontologyID, tenantID)
		for _, rt := range rts {
			existingRelNames[rt.Name] = true
		}
	}

	// 获取所有标签
	labelsResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"CALL db.labels() YIELD label RETURN label ORDER BY label")
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}

	// 对每个标签提取属性和节点数
	for _, row := range labelsResult.Rows {
		labelVal, ok := row["label"]
		if !ok {
			continue
		}
		label := fmt.Sprintf("%v", labelVal)

		et := InferredEntityType{
			Label:  label,
			Exists: existingEntityNames[label],
		}

		// 采样节点数
		countResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
			fmt.Sprintf("MATCH (n:`%s`) RETURN count(n) AS cnt", label))
		if err == nil && len(countResult.Rows) > 0 {
			et.Count = toInt64(countResult.Rows[0]["cnt"])
		}

		// 采样属性 key（LIMIT 1000 防止大库慢查询）
		propResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
			fmt.Sprintf("MATCH (n:`%s`) UNWIND keys(n) AS k RETURN DISTINCT k LIMIT 1000", label))
		if err == nil {
			for _, r := range propResult.Rows {
				if k, ok := r["k"]; ok {
					et.Properties = append(et.Properties, models.PropertyDefinition{
						Name:     fmt.Sprintf("%v", k),
						Label:    fmt.Sprintf("%v", k),
						DataType: "string", // 默认 string，Neo4j 无法可靠推断类型
					})
				}
			}
		}

		preview.EntityTypes = append(preview.EntityTypes, et)
	}

	// 提取关系模式（来源标签 + 关系类型 + 目标标签）
	relResult, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"MATCH (a)-[r]->(b) WHERE NOT ("+internalRelationshipTypePredicate+") RETURN DISTINCT labels(a)[0] AS src, type(r) AS rel, labels(b)[0] AS tgt, count(r) AS cnt LIMIT 500")
	if err != nil {
		return preview, nil // 关系推导失败不影响实体推导结果
	}
	for _, row := range relResult.Rows {
		relName := fmt.Sprintf("%v", row["rel"])
		if isInternalRelationshipType(relName) {
			continue
		}
		rt := InferredRelationType{
			Name:        relName,
			SourceLabel: fmt.Sprintf("%v", row["src"]),
			TargetLabel: fmt.Sprintf("%v", row["tgt"]),
			Count:       toInt64(row["cnt"]),
			Exists:      existingRelNames[relName],
		}
		preview.RelationTypes = append(preview.RelationTypes, rt)
	}

	return preview, nil
}

// ApplyInferredSchema 将选中的推导结果（来自图谱）应用到本体
func (s *SchemaInferenceService) ApplyInferredSchema(ctx context.Context, graphID, tenantID uint, req *ApplyInferredSchemaRequest) (*ImportResult, error) {
	preview, err := s.InferSchema(ctx, graphID, tenantID, &req.OntologyID)
	if err != nil {
		return nil, err
	}
	return s.applyPreview(tenantID, req.OntologyID, preview, req.EntityTypeLabels, req.RelationTypeNames, req.Conflict)
}

// ApplyInferredSchemaFromEngine 将选中的推导结果（来自引擎）应用到指定本体
func (s *SchemaInferenceService) ApplyInferredSchemaFromEngine(ctx context.Context, engineID, ontologyID, tenantID uint, req *ApplyInferredSchemaFromEngineRequest) (*ImportResult, error) {
	preview, err := s.InferSchemaFromEngine(ctx, engineID, tenantID, &ontologyID)
	if err != nil {
		return nil, err
	}
	return s.applyPreview(tenantID, ontologyID, preview, req.EntityTypeLabels, req.RelationTypeNames, req.Conflict)
}

// applyPreview 将推导预览中选中的实体/关系类型写入本体（共享逻辑）
func (s *SchemaInferenceService) applyPreview(tenantID, ontologyID uint, preview *SchemaInferencePreview, entityLabels, relNames []string, conflict string) (*ImportResult, error) {
	selectedLabels := make(map[string]bool)
	for _, l := range entityLabels {
		selectedLabels[l] = true
	}
	selectedRels := make(map[string]bool)
	for _, r := range relNames {
		selectedRels[r] = true
	}

	result := &ImportResult{}
	if conflict == "" {
		conflict = "skip"
	}

	// 已有实体类型
	existingEntityTypes, _ := s.ontologySvc.ListEntityTypes(ontologyID, tenantID)
	existingNames := make(map[string]*models.EntityType)
	for i, et := range existingEntityTypes {
		existingNames[et.Name] = &existingEntityTypes[i]
	}

	// 导入实体类型
	for _, et := range preview.EntityTypes {
		if !selectedLabels[et.Label] {
			continue
		}
		if existing, exists := existingNames[et.Label]; exists {
			if conflict == "skip" {
				result.Skipped++
				continue
			}
			updateReq := &models.UpdateEntityTypeRequest{
				Label:      et.Label,
				Properties: et.Properties,
			}
			if _, err := s.ontologySvc.UpdateEntityType(existing.ID, ontologyID, tenantID, updateReq); err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			result.Updated++
		} else {
			createReq := &models.CreateEntityTypeRequest{
				Name:       et.Label,
				Label:      et.Label,
				Properties: et.Properties,
			}
			if _, err := s.ontologySvc.CreateEntityType(ontologyID, tenantID, createReq); err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			result.Created++
		}
	}

	// 重新获取实体类型 name→ID 映射（用于关系导入）
	updatedEntityTypes, _ := s.ontologySvc.ListEntityTypes(ontologyID, tenantID)
	entityNameToID := make(map[string]uint)
	for _, et := range updatedEntityTypes {
		entityNameToID[et.Name] = et.ID
	}

	// 已有关系类型
	existingRelTypes, _ := s.ontologySvc.ListRelationTypes(ontologyID, tenantID)
	existingRelNames := make(map[string]*models.RelationType)
	for i, rt := range existingRelTypes {
		existingRelNames[rt.Name] = &existingRelTypes[i]
	}

	// 导入关系类型
	for _, rt := range preview.RelationTypes {
		if !selectedRels[rt.Name] {
			continue
		}
		sourceID := entityNameToID[rt.SourceLabel]
		targetID := entityNameToID[rt.TargetLabel]

		if existing, exists := existingRelNames[rt.Name]; exists {
			if conflict == "skip" {
				result.Skipped++
				continue
			}
			directed := true
			updateReq := &models.UpdateRelationTypeRequest{
				Label:    rt.Name,
				Directed: &directed,
			}
			if sourceID > 0 {
				updateReq.SourceTypeID = uintPtr(sourceID)
			}
			if targetID > 0 {
				updateReq.TargetTypeID = uintPtr(targetID)
			}
			if _, err := s.ontologySvc.UpdateRelationType(existing.ID, ontologyID, tenantID, updateReq); err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			result.Updated++
		} else {
			directed := true
			createReq := &models.CreateRelationTypeRequest{
				Name:     rt.Name,
				Label:    rt.Name,
				Directed: &directed,
			}
			if sourceID > 0 {
				createReq.SourceTypeID = uintPtr(sourceID)
			}
			if targetID > 0 {
				createReq.TargetTypeID = uintPtr(targetID)
			}
			if _, err := s.ontologySvc.CreateRelationType(ontologyID, tenantID, createReq); err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			result.Created++
		}
	}

	return result, nil
}
