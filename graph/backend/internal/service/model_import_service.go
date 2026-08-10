package service

import (
	"context"
	commonClient "github.com/addp/common/client"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
)

// ModelImportService 从 Model 模块导入本体
type ModelImportService struct {
	modelClient  *commonClient.ModelClient
	ontologySvc  *OntologyService
	entityRepo   *repository.EntityTypeRepository
	relationRepo *repository.RelationTypeRepository
}

func NewModelImportService(
	modelClient *commonClient.ModelClient,
	ontologySvc *OntologyService,
	entityRepo *repository.EntityTypeRepository,
	relationRepo *repository.RelationTypeRepository,
) *ModelImportService {
	return &ModelImportService{
		modelClient:  modelClient,
		ontologySvc:  ontologySvc,
		entityRepo:   entityRepo,
		relationRepo: relationRepo,
	}
}

// ModelImportPreview 预览可导入的实体和关系（不写库）
type ModelImportPreview struct {
	Entities  []ModelEntityPreview   `json:"entities"`
	Relations []ModelRelationPreview `json:"relations"`
}

type ModelEntityPreview struct {
	ModelID    uint                        `json:"model_id"`
	Name       string                      `json:"name"`  // 将成为本体 EntityType.name 概念标识
	Label      string                      `json:"label"` // 将成为 EntityType.label 显示名称
	Properties []models.PropertyDefinition `json:"properties"`
	Exists     bool                        `json:"exists"` // 本体中已存在同名实体类型
}

type ModelRelationPreview struct {
	ModelID        uint   `json:"model_id"`
	Name           string `json:"name"`
	SourceEntityID uint   `json:"source_entity_id"` // model entity id
	TargetEntityID uint   `json:"target_entity_id"` // model entity id
	SourceName     string `json:"source_name"`
	TargetName     string `json:"target_name"`
	Directed       bool   `json:"directed"`
	Exists         bool   `json:"exists"`
}

// GetImportPreview 获取从 Model 导入的预览数据
func (s *ModelImportService) GetImportPreview(tenantID uint) (*ModelImportPreview, error) {
	entities, err := s.modelClient.WithTenantID(tenantID).ListEntities(context.Background())
	if err != nil {
		return nil, err
	}
	relations, err := s.modelClient.WithTenantID(tenantID).ListEntityRelations(context.Background())
	if err != nil {
		return nil, err
	}

	// 构建 modelEntityID → code 的映射（用于 relation 展示）
	entityCodeMap := make(map[uint]string)
	entityNameMap := make(map[uint]string)
	for _, e := range entities {
		entityCodeMap[e.ID] = e.Code
		entityNameMap[e.ID] = e.Name
	}

	preview := &ModelImportPreview{}

	for _, e := range entities {
		// 获取属性（需单独请求含属性的详情）
		detailed, err := s.modelClient.WithTenantID(tenantID).GetEntityWithAttributes(context.Background(), e.ID)
		if err != nil || detailed == nil {
			detailed = &e
		}
		ep := ModelEntityPreview{
			ModelID:    e.ID,
			Name:       e.Code, // code 作为本体概念标识
			Label:      e.Name, // name 作为显示名称
			Properties: mapModelAttributes(detailed.Attributes),
		}
		preview.Entities = append(preview.Entities, ep)
	}

	for _, r := range relations {
		rp := ModelRelationPreview{
			ModelID:        r.ID,
			Name:           r.Name,
			SourceEntityID: r.SourceEntity,
			TargetEntityID: r.TargetEntity,
			SourceName:     entityCodeMap[r.SourceEntity],
			TargetName:     entityCodeMap[r.TargetEntity],
			Directed:       r.RelationType == "one_to_many" || r.RelationType == "one_to_one",
		}
		preview.Relations = append(preview.Relations, rp)
	}
	return preview, nil
}

// ImportRequest 从 Model 导入到本体的请求参数
type ImportFromModelRequest struct {
	EntityModelIDs   []uint `json:"entity_ids"`   // 选中的 Model entity id 列表
	RelationModelIDs []uint `json:"relation_ids"` // 选中的 Model relation id 列表
	Conflict         string `json:"conflict"`     // "skip" | "overwrite"
}

// ImportFromModel 将选中的 Model 实体/关系导入到指定本体
func (s *ModelImportService) ImportFromModel(ontologyID, tenantID uint, req *ImportFromModelRequest) (*ImportResult, error) {
	// 获取所有预览数据（含属性）
	preview, err := s.GetImportPreview(tenantID)
	if err != nil {
		return nil, err
	}

	// 构建 modelEntityID → (code, preview index) 的映射
	entityIDMap := make(map[uint]int) // modelID → preview index
	for i, e := range preview.Entities {
		entityIDMap[e.ModelID] = i
	}

	// 选中的 entity model id set
	selectedEntityIDs := make(map[uint]bool)
	for _, id := range req.EntityModelIDs {
		selectedEntityIDs[id] = true
	}
	selectedRelationIDs := make(map[uint]bool)
	for _, id := range req.RelationModelIDs {
		selectedRelationIDs[id] = true
	}

	result := &ImportResult{}

	// 已有实体类型 name set
	existingEntityTypes, _ := s.ontologySvc.ListEntityTypes(ontologyID, tenantID)
	existingNames := make(map[string]*models.EntityType)
	for i, et := range existingEntityTypes {
		existingNames[et.Name] = &existingEntityTypes[i]
	}

	// model entity id → graph EntityType id（用于关系导入时关联）
	modelToGraphEntityID := make(map[uint]uint)

	// 导入实体类型
	for _, modelID := range req.EntityModelIDs {
		idx, ok := entityIDMap[modelID]
		if !ok {
			continue
		}
		ep := preview.Entities[idx]
		if existing, exists := existingNames[ep.Name]; exists {
			if req.Conflict == "skip" {
				result.Skipped++
				modelToGraphEntityID[modelID] = existing.ID
				continue
			}
			// overwrite: 更新属性
			updateReq := &models.UpdateEntityTypeRequest{
				Label:           ep.Label,
				DisplayProperty: existing.DisplayProperty,
				Properties:      ep.Properties,
			}
			if _, err := s.ontologySvc.UpdateEntityType(existing.ID, ontologyID, tenantID, updateReq); err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			modelToGraphEntityID[modelID] = existing.ID
			result.Updated++
		} else {
			createReq := &models.CreateEntityTypeRequest{
				Name:       ep.Name,
				Label:      ep.Label,
				Properties: ep.Properties,
			}
			et, err := s.ontologySvc.CreateEntityType(ontologyID, tenantID, createReq)
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			modelToGraphEntityID[modelID] = et.ID
			result.Created++
		}
	}

	// 获取已有关系类型
	existingRelTypes, _ := s.ontologySvc.ListRelationTypes(ontologyID, tenantID)
	existingRelNames := make(map[string]*models.RelationType)
	for i, rt := range existingRelTypes {
		existingRelNames[rt.Name] = &existingRelTypes[i]
	}

	// 导入关系类型
	for _, modelID := range req.RelationModelIDs {
		var rp *ModelRelationPreview
		for i, r := range preview.Relations {
			if r.ModelID == modelID {
				rp = &preview.Relations[i]
				break
			}
		}
		if rp == nil {
			continue
		}

		sourceGraphID := modelToGraphEntityID[rp.SourceEntityID]
		targetGraphID := modelToGraphEntityID[rp.TargetEntityID]

		if existing, exists := existingRelNames[rp.Name]; exists {
			if req.Conflict == "skip" {
				result.Skipped++
				continue
			}
			// overwrite
			directed := rp.Directed
			updateReq := &models.UpdateRelationTypeRequest{
				Label:        rp.Name,
				SourceTypeID: uintPtr(sourceGraphID),
				TargetTypeID: uintPtr(targetGraphID),
				Directed:     &directed,
			}
			if _, err := s.ontologySvc.UpdateRelationType(existing.ID, ontologyID, tenantID, updateReq); err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			result.Updated++
		} else {
			directed := rp.Directed
			createReq := &models.CreateRelationTypeRequest{
				Name:     rp.Name,
				Label:    rp.Name,
				Directed: &directed,
			}
			if sourceGraphID > 0 {
				createReq.SourceTypeID = uintPtr(sourceGraphID)
			}
			if targetGraphID > 0 {
				createReq.TargetTypeID = uintPtr(targetGraphID)
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

// ImportResult 导入结果摘要
type ImportResult struct {
	Created int      `json:"created"`
	Updated int      `json:"updated"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors,omitempty"`
}

// mapModelAttributes 将 Model 实体属性映射为 PropertyDefinition
func mapModelAttributes(attrs []commonClient.ModelEntityAttribute) []models.PropertyDefinition {
	props := make([]models.PropertyDefinition, 0, len(attrs))
	for _, a := range attrs {
		if a.IsPK {
			continue // 主键不导入为属性
		}
		dataType := mapDataType(a.DataType)
		prop := models.PropertyDefinition{
			Name:       a.ColumnName,
			Label:      a.Name,
			DataType:   dataType,
			Required:   !a.Nullable,
			Searchable: dataType == "string",
		}
		props = append(props, prop)
	}
	return props
}

// mapDataType 将 Model 数据类型映射为 Graph 数据类型
func mapDataType(modelType string) string {
	switch modelType {
	case "int", "bigint", "smallint", "integer":
		return "integer"
	case "float", "double", "decimal", "numeric":
		return "float"
	case "bool", "boolean":
		return "boolean"
	case "date":
		return "date"
	case "datetime", "timestamp", "timestamptz":
		return "datetime"
	default:
		return "string"
	}
}

func uintPtr(v uint) *uint {
	return &v
}
