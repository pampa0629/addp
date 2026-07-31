package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/datatypes"
)

var (
	ErrDisplayPropertyNotFound  = errors.New("display property does not exist")
	ErrDisplayPropertyNotString = errors.New("display property must be a string")
)

type OntologyService struct {
	ontologyRepo     *repository.OntologyRepository
	entityTypeRepo   *repository.EntityTypeRepository
	relationTypeRepo *repository.RelationTypeRepository
	versionRepo      *repository.OntologyVersionRepository
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
	nodeLabels, _ := json.Marshal(req.NodeLabels)
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
		NodeLabels:         datatypes.JSON(nodeLabels),
		Color:              color,
		Icon:               req.Icon,
		ParentID:           req.ParentID,
		DisplayProperty:    strings.TrimSpace(req.DisplayProperty),
		Properties:         datatypes.JSON(props),
		Constraints:        datatypes.JSON(constraints),
		IsSpatialLayer:     req.IsSpatialLayer,
		SpatialLayerConfig: spatialConfig,
		SortOrder:          req.SortOrder,
	}
	entityTypes, err := s.entityTypeRepo.ListByOntology(ontologyID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := normalizeEntityTypeDisplayProperty(et, entityTypeByID(entityTypes)); err != nil {
		return nil, err
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
	if req.NodeLabels != nil {
		nodeLabels, _ := json.Marshal(req.NodeLabels)
		et.NodeLabels = datatypes.JSON(nodeLabels)
	}
	if req.Color != "" {
		et.Color = req.Color
	}
	if req.Icon != "" {
		et.Icon = req.Icon
	}
	et.ParentID = req.ParentID
	et.DisplayProperty = strings.TrimSpace(req.DisplayProperty)
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
	entityTypes, err := s.entityTypeRepo.ListByOntology(ontologyID, tenantID)
	if err != nil {
		return nil, err
	}
	byID := entityTypeByID(entityTypes)
	byID[et.ID] = et
	if err := normalizeEntityTypeDisplayProperty(et, byID); err != nil {
		return nil, err
	}
	return et, s.entityTypeRepo.Update(et)
}

func normalizeEntityTypeDisplayProperty(et *models.EntityType, byID map[uint]*models.EntityType) error {
	displayProperty := strings.TrimSpace(et.DisplayProperty)
	et.DisplayProperty = displayProperty
	if displayProperty == "" {
		return nil
	}

	var selected *models.PropertyDefinition
	for _, property := range collectInheritedProperties(et, byID) {
		if strings.TrimSpace(property.Name) == displayProperty {
			propertyCopy := property
			selected = &propertyCopy
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("%w: %s", ErrDisplayPropertyNotFound, displayProperty)
	}
	if selected.DataType != "string" {
		return fmt.Errorf("%w: %s", ErrDisplayPropertyNotString, displayProperty)
	}

	properties, err := et.ParsedProperties()
	if err != nil {
		return err
	}
	for index := range properties {
		if strings.TrimSpace(properties[index].Name) == displayProperty {
			properties[index].Searchable = true
		}
	}
	encoded, err := json.Marshal(properties)
	if err != nil {
		return err
	}
	et.Properties = datatypes.JSON(encoded)
	return nil
}

func (s *OntologyService) DeleteEntityType(id, ontologyID, tenantID uint) error {
	return s.entityTypeRepo.Delete(id, ontologyID, tenantID)
}

func (s *OntologyService) GetEntityType(id, ontologyID, tenantID uint) (*models.EntityType, error) {
	return s.entityTypeRepo.GetByID(id, ontologyID, tenantID)
}

func (s *OntologyService) EntityTypeNodeLabels(ontologyID, tenantID uint, entityTypeName string) []string {
	ontology, err := s.ontologyRepo.GetDetail(ontologyID, tenantID)
	if err != nil {
		return entityTypeLabels(entityTypeName)
	}
	return entityTypeNodeLabels(ontology, entityTypeName)
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

// BuildSpatialLayerLookup 构建本体实体类型名 → SpatialLayerConfig 的查找表（含继承关系）
// 遍历本体内所有 EntityType，对每个类型沿 ParentID 上溯直到找到 is_spatial_layer=true 的祖先
// 返回：map[entityTypeName]SpatialLayerMapping（仅含有空间祖先的实体类型）
func (s *OntologyService) BuildSpatialLayerLookup(ontologyID, tenantID uint) (map[string]SpatialLayerMapping, error) {
	all, err := s.entityTypeRepo.ListByOntology(ontologyID, tenantID)
	if err != nil {
		return nil, err
	}

	return buildSpatialLayerMappingsByEntityType(all), nil
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
