package service

import (
	"fmt"

	commonClient "github.com/addp/common/client"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

type EntityService struct {
	repo           *repository.EntityRepository
	relationRepo   *repository.EntityRelationRepository
	standardClient *commonClient.StandardClient
}

func NewEntityService(repo *repository.EntityRepository, relationRepo *repository.EntityRelationRepository, standardURL, internalKey string) *EntityService {
	var standardClient *commonClient.StandardClient
	if standardURL != "" {
		standardClient = commonClient.NewStandardClientWithInternalKey(standardURL, internalKey)
	}
	return &EntityService{
		repo:           repo,
		relationRepo:   relationRepo,
		standardClient: standardClient,
	}
}

func (s *EntityService) CreateEntity(req *models.CreateEntityRequest, tenantID, userID int64) (*models.Entity, error) {
	exists, err := s.repo.ExistsByCode(req.Code, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("实体编码 '%s' 已存在", req.Code)
	}

	entity := &models.Entity{
		TenantID:    tenantID,
		DomainID:    req.DomainID,
		Name:        req.Name,
		Code:        req.Code,
		Description: req.Description,
		Status:      "draft",
		CreatedBy:   userID,
	}

	if err := s.repo.Create(entity); err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *EntityService) GetEntity(id, tenantID int64) (*models.Entity, error) {
	return s.repo.GetByID(id, tenantID)
}

func (s *EntityService) ListEntities(tenantID int64, opts repository.ListEntityOptions) ([]models.Entity, int64, error) {
	return s.repo.List(tenantID, opts)
}

func (s *EntityService) UpdateEntity(id, tenantID, userID int64, req *models.UpdateEntityRequest) (*models.Entity, error) {
	entity, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		entity.Name = req.Name
	}
	if req.DomainID != nil {
		entity.DomainID = req.DomainID
	}
	entity.Description = req.Description
	entity.UpdatedBy = &userID

	if err := s.repo.Update(entity); err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *EntityService) DeleteEntity(id, tenantID int64) error {
	return s.repo.Delete(id, tenantID)
}

func (s *EntityService) ApproveEntity(id, tenantID, userID int64) error {
	return s.repo.UpdateStatus(id, tenantID, "approved", userID)
}

// GetAttributes 获取实体属性列表
func (s *EntityService) GetAttributes(entityID, tenantID int64) ([]models.EntityAttribute, error) {
	// 验证实体属于当前租户
	if _, err := s.repo.GetByID(entityID, tenantID); err != nil {
		return nil, fmt.Errorf("实体不存在")
	}
	return s.repo.GetAttributes(entityID)
}

// CreateAttribute 创建实体属性
func (s *EntityService) CreateAttribute(entityID, tenantID int64, req *models.CreateEntityAttributeRequest) (*models.EntityAttribute, error) {
	if _, err := s.repo.GetByID(entityID, tenantID); err != nil {
		return nil, fmt.Errorf("实体不存在")
	}

	attr := &models.EntityAttribute{
		EntityID:    entityID,
		ElementID:   req.ElementID,
		Name:        req.Name,
		IsPK:        req.IsPK,
		Nullable:    req.Nullable,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	}

	if err := s.repo.CreateAttribute(attr); err != nil {
		return nil, err
	}
	return attr, nil
}

// UpdateAttribute 更新实体属性
func (s *EntityService) UpdateAttribute(attrID, entityID, tenantID int64, req *models.UpdateEntityAttributeRequest) (*models.EntityAttribute, error) {
	if _, err := s.repo.GetByID(entityID, tenantID); err != nil {
		return nil, fmt.Errorf("实体不存在")
	}

	attr, err := s.repo.GetAttributeByID(attrID, entityID)
	if err != nil {
		return nil, fmt.Errorf("属性不存在")
	}

	if req.Name != "" {
		attr.Name = req.Name
	}
	attr.ElementID = req.ElementID
	if req.IsPK != nil {
		attr.IsPK = *req.IsPK
	}
	if req.Nullable != nil {
		attr.Nullable = *req.Nullable
	}
	attr.Description = req.Description
	if req.SortOrder != nil {
		attr.SortOrder = *req.SortOrder
	}

	if err := s.repo.UpdateAttribute(attr); err != nil {
		return nil, err
	}
	return attr, nil
}

// DeleteAttribute 删除实体属性
func (s *EntityService) DeleteAttribute(attrID, entityID, tenantID int64) error {
	if _, err := s.repo.GetByID(entityID, tenantID); err != nil {
		return fmt.Errorf("实体不存在")
	}
	return s.repo.DeleteAttribute(attrID, entityID)
}

// ImportFromMermaid 从Mermaid代码导入实体和关系（合并模式）
func (s *EntityService) ImportFromMermaid(tenantID, userID int64, req *models.MermaidImportRequest) (*models.MermaidImportResult, error) {
	result := &models.MermaidImportResult{
		Errors: []string{},
	}

	// 1. 解析Mermaid代码
	parsed, err := ParseMermaidER(req.MermaidCode)
	if err != nil {
		return nil, fmt.Errorf("解析Mermaid代码失败: %v", err)
	}

	// 2. 处理实体（合并模式）
	entityMap := make(map[string]int64) // code -> id

	for _, entityDef := range parsed.Entities {
		// 检查实体是否已存在（按code匹配）
		existingEntity, _ := s.repo.GetByCode(tenantID, entityDef.Name)

		var entityID int64

		if existingEntity != nil {
			// 更新现有实体（仅更新名称，code不变）
			existingEntity.Name = entityDef.Name
			existingEntity.UpdatedBy = &userID
			if err := s.repo.Update(existingEntity); err != nil {
				result.Errors = append(result.Errors, "更新实体失败: "+entityDef.Name)
				continue
			}
			entityID = existingEntity.ID
			result.UpdatedEntities++
		} else {
			// 创建新实体
			entity := &models.Entity{
				TenantID:  tenantID,
				Name:      entityDef.Name,
				Code:      entityDef.Name,
				Status:    "draft",
				CreatedBy: userID,
			}
			if err := s.repo.Create(entity); err != nil {
				result.Errors = append(result.Errors, "创建实体失败: "+entityDef.Name)
				continue
			}
			entityID = entity.ID
			result.CreatedEntities++
		}

		entityMap[entityDef.Name] = entityID

		// 3. 处理属性（删除旧属性，创建新属性）
		s.repo.DeleteByEntityID(entityID)

		for _, attrDef := range entityDef.Attributes {
			attr := &models.EntityAttribute{
				EntityID:    entityID,
				Name:        attrDef.Name,
				IsPK:        attrDef.IsPK,
				Nullable:    !attrDef.IsPK,
				Description: "",
			}
			if err := s.repo.CreateAttribute(attr); err != nil {
				result.Errors = append(result.Errors, "创建属性失败: "+attrDef.Name)
			}
		}
	}

	// 4. 处理关系（删除旧关系，创建新关系）
	s.relationRepo.DeleteByTenantID(tenantID)

	for _, relDef := range parsed.Relations {
		sourceID, sourceExists := entityMap[relDef.Source]
		targetID, targetExists := entityMap[relDef.Target]

		if !sourceExists || !targetExists {
			result.Errors = append(result.Errors, "关系引用的实体不存在: "+relDef.Source+" -> "+relDef.Target)
			continue
		}

		relation := &models.EntityRelation{
			TenantID:     tenantID,
			SourceEntity: sourceID,
			TargetEntity: targetID,
			RelationType: ConvertRelationType(relDef.Symbol),
			Name:         relDef.Label,
		}

		if err := s.relationRepo.Create(relation); err != nil {
			result.Errors = append(result.Errors, "创建关系失败: "+relDef.Label)
			continue
		}

		result.CreatedRelations++
	}

	return result, nil
}

// ExportToMermaid 导出实体和关系为Mermaid代码
func (s *EntityService) ExportToMermaid(tenantID int64) (string, error) {
	// 1. 查询所有实体
	entities, err := s.repo.ListByTenantID(tenantID)
	if err != nil {
		return "", err
	}

	// 2. 查询所有关系
	relations, err := s.relationRepo.ListByTenantID(tenantID)
	if err != nil {
		return "", err
	}

	// 3. 生成Mermaid代码
	var code string
	code = "erDiagram\n"

	// 实体定义
	for _, entity := range entities {
		code += "  " + entity.Code + " {\n"

		// 查询属性
		attributes, _ := s.repo.GetAttributes(entity.ID)
		for _, attr := range attributes {
			dataType := "string" // 默认类型
			pk := ""
			if attr.IsPK {
				pk = " PK"
			}
			code += "    " + dataType + " " + attr.Name + pk + "\n"
		}

		code += "  }\n"
	}

	// 关系定义
	for _, relation := range relations {
		var sourceEntity, targetEntity *models.Entity
		for i := range entities {
			if entities[i].ID == relation.SourceEntity {
				sourceEntity = &entities[i]
			}
			if entities[i].ID == relation.TargetEntity {
				targetEntity = &entities[i]
			}
		}

		if sourceEntity != nil && targetEntity != nil {
			symbol := ConvertToMermaidSymbol(relation.RelationType)
			label := relation.Name
			if label == "" {
				label = "relates"
			}

			code += "  " + sourceEntity.Code + " " + symbol + " " + targetEntity.Code + " : \"" + label + "\"\n"
		}
	}

	return code, nil
}
