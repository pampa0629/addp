package service

import (
	"encoding/json"
	"fmt"

	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/datatypes"
)

type OntologyService struct {
	ontologyRepo      *repository.OntologyRepository
	entityTypeRepo    *repository.EntityTypeRepository
	relationTypeRepo  *repository.RelationTypeRepository
	versionRepo       *repository.OntologyVersionRepository
}

func NewOntologyService(
	ontologyRepo *repository.OntologyRepository,
	entityTypeRepo *repository.EntityTypeRepository,
	relationTypeRepo *repository.RelationTypeRepository,
	versionRepo *repository.OntologyVersionRepository,
) *OntologyService {
	return &OntologyService{
		ontologyRepo:     ontologyRepo,
		entityTypeRepo:   entityTypeRepo,
		relationTypeRepo: relationTypeRepo,
		versionRepo:      versionRepo,
	}
}

// --- Ontology CRUD ---

func (s *OntologyService) List(tenantID uint) ([]models.Ontology, error) {
	return s.ontologyRepo.List(tenantID)
}

func (s *OntologyService) GetByID(id, tenantID uint) (*models.Ontology, error) {
	return s.ontologyRepo.GetByID(id, tenantID)
}

func (s *OntologyService) GetDetail(id, tenantID uint) (*models.Ontology, error) {
	return s.ontologyRepo.GetDetail(id, tenantID)
}

func (s *OntologyService) Create(tenantID uint, req *models.CreateOntologyRequest) (*models.Ontology, error) {
	meta, _ := json.Marshal(req.Metadata)
	ontology := &models.Ontology{
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		Status:      "active",
		Metadata:    datatypes.JSON(meta),
	}
	if err := s.ontologyRepo.Create(ontology); err != nil {
		return nil, fmt.Errorf("failed to create ontology: %w", err)
	}
	return ontology, nil
}

func (s *OntologyService) Update(id, tenantID uint, req *models.UpdateOntologyRequest) (*models.Ontology, error) {
	ontology, err := s.ontologyRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		ontology.Name = req.Name
	}
	if req.Description != "" {
		ontology.Description = req.Description
	}
	if req.Status != "" {
		ontology.Status = req.Status
	}
	if req.Metadata != nil {
		meta, _ := json.Marshal(req.Metadata)
		ontology.Metadata = datatypes.JSON(meta)
	}
	return ontology, s.ontologyRepo.Update(ontology)
}

func (s *OntologyService) Delete(id, tenantID uint) error {
	return s.ontologyRepo.Delete(id, tenantID)
}

// --- EntityType CRUD ---

func (s *OntologyService) ListEntityTypes(ontologyID, tenantID uint) ([]models.EntityType, error) {
	return s.entityTypeRepo.ListByOntology(ontologyID, tenantID)
}

func (s *OntologyService) CreateEntityType(ontologyID, tenantID uint, req *models.CreateEntityTypeRequest) (*models.EntityType, error) {
	// 验证本体存在
	if _, err := s.ontologyRepo.GetByID(ontologyID, tenantID); err != nil {
		return nil, fmt.Errorf("ontology not found: %w", err)
	}

	props, _ := json.Marshal(req.Properties)
	constraints, _ := json.Marshal(req.Constraints)
	color := req.Color
	if color == "" {
		color = "#5B8FF9"
	}

	var spatialConfig datatypes.JSON
	if req.IsSpatialLayer && req.SpatialLayerConfig != nil {
		spatialConfig, _ = json.Marshal(req.SpatialLayerConfig)
	} else {
		spatialConfig = datatypes.JSON("{}")
	}

	et := &models.EntityType{
		OntologyID:         ontologyID,
		TenantID:           tenantID,
		Name:               req.Name,
		Label:              req.Label,
		Description:        req.Description,
		Color:              color,
		Icon:               req.Icon,
		ParentID:           req.ParentID,
		Properties:         datatypes.JSON(props),
		Constraints:        datatypes.JSON(constraints),
		IsSpatialLayer:     req.IsSpatialLayer,
		SpatialLayerConfig: spatialConfig,
		SortOrder:          req.SortOrder,
	}
	if err := s.entityTypeRepo.Create(et); err != nil {
		return nil, fmt.Errorf("failed to create entity type: %w", err)
	}
	return et, nil
}

func (s *OntologyService) UpdateEntityType(id, ontologyID, tenantID uint, req *models.UpdateEntityTypeRequest) (*models.EntityType, error) {
	et, err := s.entityTypeRepo.GetByID(id, ontologyID, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		et.Name = req.Name
	}
	if req.Label != "" {
		et.Label = req.Label
	}
	if req.Description != "" {
		et.Description = req.Description
	}
	if req.Color != "" {
		et.Color = req.Color
	}
	if req.Icon != "" {
		et.Icon = req.Icon
	}
	et.ParentID = req.ParentID
	if req.Properties != nil {
		props, _ := json.Marshal(req.Properties)
		et.Properties = datatypes.JSON(props)
	}
	if req.Constraints != nil {
		constraints, _ := json.Marshal(req.Constraints)
		et.Constraints = datatypes.JSON(constraints)
	}
	et.IsSpatialLayer = req.IsSpatialLayer
	if req.IsSpatialLayer && req.SpatialLayerConfig != nil {
		spatialConfig, _ := json.Marshal(req.SpatialLayerConfig)
		et.SpatialLayerConfig = datatypes.JSON(spatialConfig)
	} else if !req.IsSpatialLayer {
		et.SpatialLayerConfig = datatypes.JSON("{}")
	}
	et.SortOrder = req.SortOrder
	return et, s.entityTypeRepo.Update(et)
}

func (s *OntologyService) DeleteEntityType(id, ontologyID, tenantID uint) error {
	return s.entityTypeRepo.Delete(id, ontologyID, tenantID)
}

func (s *OntologyService) GetEntityType(id, ontologyID, tenantID uint) (*models.EntityType, error) {
	return s.entityTypeRepo.GetByID(id, ontologyID, tenantID)
}

// GetSpatialEntityTypes 返回本体中所有直接定义 is_spatial_layer=true 的 EntityType
func (s *OntologyService) GetSpatialEntityTypes(ontologyID, tenantID uint) ([]models.EntityType, error) {
	all, err := s.entityTypeRepo.ListByOntology(ontologyID, tenantID)
	if err != nil {
		return nil, err
	}
	var result []models.EntityType
	for _, et := range all {
		if et.IsSpatialLayer {
			result = append(result, et)
		}
	}
	return result, nil
}

// BuildSpatialLayerLookup 构建 label名 → SpatialLayerConfig 的查找表（含继承关系）
// 遍历本体内所有 EntityType，对每个类型沿 ParentID 上溯直到找到 is_spatial_layer=true 的祖先
// 返回：map[labelName]*SpatialLayerConfig（仅含有空间祖先的 label）
func (s *OntologyService) BuildSpatialLayerLookup(ontologyID, tenantID uint) (map[string]*models.SpatialLayerConfig, error) {
	all, err := s.entityTypeRepo.ListByOntology(ontologyID, tenantID)
	if err != nil {
		return nil, err
	}

	// 构建 id→EntityType 索引
	byID := make(map[uint]*models.EntityType, len(all))
	for i := range all {
		byID[all[i].ID] = &all[i]
	}

	result := make(map[string]*models.SpatialLayerConfig)
	for _, et := range all {
		cfg := findSpatialAncestorConfig(&et, byID, 0)
		if cfg != nil {
			// 每个实体类型使用自身名称作为 Neo4j 空间图层名，而不是祖先的名称
			cfgCopy := *cfg
			cfgCopy.LayerName = et.Name
			result[et.Name] = &cfgCopy
		}
	}
	return result, nil
}

// findSpatialAncestorConfig 沿 ParentID 链查找最近的空间图层祖先（最多 10 层防循环）
func findSpatialAncestorConfig(et *models.EntityType, byID map[uint]*models.EntityType, depth int) *models.SpatialLayerConfig {
	if depth > 10 {
		return nil
	}
	if et.IsSpatialLayer {
		return et.ParsedSpatialLayerConfig()
	}
	if et.ParentID == nil {
		return nil
	}
	parent, ok := byID[*et.ParentID]
	if !ok {
		return nil
	}
	return findSpatialAncestorConfig(parent, byID, depth+1)
}

// --- RelationType CRUD ---

func (s *OntologyService) ListRelationTypes(ontologyID, tenantID uint) ([]models.RelationType, error) {
	return s.relationTypeRepo.ListByOntology(ontologyID, tenantID)
}

func (s *OntologyService) CreateRelationType(ontologyID, tenantID uint, req *models.CreateRelationTypeRequest) (*models.RelationType, error) {
	if _, err := s.ontologyRepo.GetByID(ontologyID, tenantID); err != nil {
		return nil, fmt.Errorf("ontology not found: %w", err)
	}

	props, _ := json.Marshal(req.Properties)
	constraints, _ := json.Marshal(req.Constraints)
	color := req.Color
	if color == "" {
		color = "#8B8B8B"
	}
	directed := true
	if req.Directed != nil {
		directed = *req.Directed
	}

	rt := &models.RelationType{
		OntologyID:   ontologyID,
		TenantID:     tenantID,
		Name:         req.Name,
		Label:        req.Label,
		Description:  req.Description,
		SourceTypeID: req.SourceTypeID,
		TargetTypeID: req.TargetTypeID,
		Directed:     directed,
		Color:        color,
		Properties:   datatypes.JSON(props),
		Constraints:  datatypes.JSON(constraints),
		SortOrder:    req.SortOrder,
	}
	if err := s.relationTypeRepo.Create(rt); err != nil {
		return nil, fmt.Errorf("failed to create relation type: %w", err)
	}
	return rt, nil
}

func (s *OntologyService) UpdateRelationType(id, ontologyID, tenantID uint, req *models.UpdateRelationTypeRequest) (*models.RelationType, error) {
	rt, err := s.relationTypeRepo.GetByID(id, ontologyID, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		rt.Name = req.Name
	}
	if req.Label != "" {
		rt.Label = req.Label
	}
	if req.Description != "" {
		rt.Description = req.Description
	}
	if req.Color != "" {
		rt.Color = req.Color
	}
	if req.Directed != nil {
		rt.Directed = *req.Directed
	}
	rt.SourceTypeID = req.SourceTypeID
	rt.TargetTypeID = req.TargetTypeID
	if req.Properties != nil {
		props, _ := json.Marshal(req.Properties)
		rt.Properties = datatypes.JSON(props)
	}
	if req.Constraints != nil {
		constraints, _ := json.Marshal(req.Constraints)
		rt.Constraints = datatypes.JSON(constraints)
	}
	rt.SortOrder = req.SortOrder
	return rt, s.relationTypeRepo.Update(rt)
}

func (s *OntologyService) DeleteRelationType(id, ontologyID, tenantID uint) error {
	return s.relationTypeRepo.Delete(id, ontologyID, tenantID)
}

// --- OntologyVersion ---

func (s *OntologyService) ListVersions(ontologyID, tenantID uint) ([]models.OntologyVersion, error) {
	return s.versionRepo.ListByOntology(ontologyID, tenantID)
}

func (s *OntologyService) CreateVersion(ontologyID, tenantID, userID uint, req *models.CreateOntologyVersionRequest) (*models.OntologyVersion, error) {
	// 获取当前本体完整快照
	ontology, err := s.ontologyRepo.GetDetail(ontologyID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("ontology not found: %w", err)
	}

	snapshot, _ := json.Marshal(ontology)
	v := &models.OntologyVersion{
		OntologyID:  ontologyID,
		TenantID:    tenantID,
		Version:     req.Version,
		Description: req.Description,
		Snapshot:    datatypes.JSON(snapshot),
		CreatedBy:   userID,
	}
	return v, s.versionRepo.Create(v)
}
