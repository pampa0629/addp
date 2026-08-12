package service

import (
	"context"
	"encoding/json"
	"fmt"
	commonClient "github.com/addp/common/client"
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EntityService struct {
	repo         *repository.EntityRepository
	relationRepo *repository.EntityRelationRepository
	standard     *commonClient.StandardClient
}

func (s *EntityService) SetStandardClient(client *commonClient.StandardClient) { s.standard = client }

func (s *EntityService) validateReferences(tenantID int64, domainID, elementID *int64) error {
	if s.standard == nil {
		return nil
	}
	client := s.standard.WithTenantID(uint(tenantID))
	if domainID != nil && *domainID > 0 {
		if err := client.ValidateDomain(context.Background(), *domainID); err != nil {
			return standardReferenceError(err, "domain_not_found")
		}
	}
	if elementID != nil && *elementID > 0 {
		if err := client.ValidateElement(context.Background(), *elementID); err != nil {
			return standardReferenceError(err, "element_not_found")
		}
	}
	return nil
}

func NewEntityService(repo *repository.EntityRepository, relationRepo *repository.EntityRelationRepository) *EntityService {
	return &EntityService{
		repo: repo, relationRepo: relationRepo,
	}
}

func (s *EntityService) CreateEntity(req *models.CreateEntityRequest, tenantID, userID int64) (*models.Entity, error) {
	if err := s.validateReferences(tenantID, req.DomainID, nil); err != nil {
		return nil, err
	}
	exists, err := s.repo.ExistsByCode(req.Code, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperrors.Conflict("entity_code_conflict", i18n.MsgEntityCodeConflict)
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
		return nil, modelResourceError(err, "entity_code", i18n.MsgEntityCodeConflict)
	}
	return entity, nil
}

func (s *EntityService) GetEntity(id, tenantID int64) (*models.Entity, error) {
	entity, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, modelResourceError(err, "entity_not_found", i18n.MsgEntityNotFound)
	}
	return entity, nil
}

func (s *EntityService) ListEntities(tenantID int64, opts repository.ListEntityOptions) ([]models.Entity, int64, error) {
	return s.repo.List(tenantID, opts)
}

func (s *EntityService) UpdateEntity(id, tenantID, userID int64, req *models.UpdateEntityRequest) (*models.Entity, error) {
	entity, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, modelResourceError(err, "entity_not_found", i18n.MsgEntityNotFound)
	}
	if entity.Status != "draft" {
		return nil, apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
	}

	if req.Name != "" {
		entity.Name = req.Name
	}
	if req.DomainID != nil {
		if err := s.validateReferences(tenantID, req.DomainID, nil); err != nil {
			return nil, err
		}
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
	entity, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return modelResourceError(err, "entity_not_found", i18n.MsgEntityNotFound)
	}
	if entity.Status != "draft" {
		return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
	}
	relations, err := s.relationRepo.GetByEntityID(tenantID, id)
	if err != nil {
		return err
	}
	for _, relation := range relations {
		otherEntityID := relation.SourceEntity
		if otherEntityID == id {
			otherEntityID = relation.TargetEntity
		}
		otherEntity, err := s.repo.GetByID(otherEntityID, tenantID)
		if err != nil {
			return err
		}
		if otherEntity.Status != "draft" {
			return apperrors.Conflict("entity_relation_state_conflict", i18n.MsgRelationStateConflict)
		}
	}
	return s.repo.Delete(id, tenantID)
}

func (s *EntityService) ApproveEntity(id, tenantID, userID int64) error {
	entity, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return modelResourceError(err, "entity_not_found", i18n.MsgEntityNotFound)
	}
	if entity.Status != "draft" {
		return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
	}
	attributes, err := s.repo.GetAttributes(id)
	if err != nil {
		return err
	}
	if len(attributes) == 0 {
		return apperrors.Validation("entity_approval_invalid", i18n.MsgValidationFailed)
	}
	hasPrimaryKey := false
	for _, attribute := range attributes {
		if attribute.IsPK {
			hasPrimaryKey = true
		}
		if attribute.ColumnName == "" || attribute.DataType == "" {
			return apperrors.Validation("entity_approval_invalid", i18n.MsgValidationFailed)
		}
	}
	if !hasPrimaryKey {
		return apperrors.Validation("entity_approval_invalid", i18n.MsgValidationFailed)
	}
	return s.repo.UpdateStatus(id, tenantID, "approved", userID)
}

func (s *EntityService) ReopenEntity(id, tenantID, userID int64) error {
	entity, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return modelResourceError(err, "entity_not_found", i18n.MsgEntityNotFound)
	}
	if entity.Status != "approved" {
		return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
	}
	return s.repo.UpdateStatus(id, tenantID, "draft", userID)
}

// GetAttributes 获取实体属性列表
func (s *EntityService) GetAttributes(entityID, tenantID int64) ([]models.EntityAttribute, error) {
	// 验证实体属于当前租户
	_, err := s.repo.GetByID(entityID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
	}
	return s.repo.GetAttributes(entityID)
}

// CreateAttribute 创建实体属性
func (s *EntityService) CreateAttribute(entityID, tenantID int64, req *models.CreateEntityAttributeRequest) (*models.EntityAttribute, error) {
	entity, err := s.repo.GetByID(entityID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
	}
	if entity.Status != "draft" {
		return nil, apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
	}
	if err := s.validateReferences(tenantID, nil, req.ElementID); err != nil {
		return nil, err
	}

	attr := &models.EntityAttribute{
		EntityID:    entityID,
		ElementID:   req.ElementID,
		Name:        req.Name,
		ColumnName:  req.ColumnName,
		DataType:    req.DataType,
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
	entity, err := s.repo.GetByID(entityID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
	}
	if entity.Status != "draft" {
		return nil, apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
	}
	if err := s.validateReferences(tenantID, nil, req.ElementID); err != nil {
		return nil, err
	}

	attr, err := s.repo.GetAttributeByID(attrID, entityID)
	if err != nil {
		return nil, apperrors.NotFound("attribute_not_found", i18n.MsgAttributeNotFound)
	}

	if req.Name != "" {
		attr.Name = req.Name
	}
	if req.ColumnName != "" {
		attr.ColumnName = req.ColumnName
	}
	if req.DataType != "" {
		attr.DataType = req.DataType
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
	entity, err := s.repo.GetByID(entityID, tenantID)
	if err != nil {
		return apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
	}
	if entity.Status != "draft" {
		return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
	}
	if err := s.repo.DeleteAttribute(attrID, entityID); err != nil {
		return modelResourceError(err, "attribute_not_found", i18n.MsgAttributeNotFound)
	}
	return nil
}

// ImportFromMermaid 从 Mermaid ER 图全量替换当前租户的实体模型。
func (s *EntityService) ImportFromMermaid(tenantID, userID int64, req *models.MermaidImportRequest) (*models.MermaidImportResult, error) {
	result := &models.MermaidImportResult{}
	parsed, err := ParseMermaidER(req.MermaidCode)
	if err != nil {
		return nil, apperrors.Wrap(apperrors.KindValidation, "mermaid_invalid", i18n.MsgValidationFailed, err)
	}
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		var existingEntities []models.Entity
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ?", tenantID).Find(&existingEntities).Error; err != nil {
			return err
		}
		for _, entity := range existingEntities {
			if entity.Status != "draft" {
				return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
			}
		}
		entityRepo := repository.NewEntityRepository(tx)
		relationRepo := repository.NewEntityRelationRepository(tx)
		if err := tx.Where("tenant_id = ?", tenantID).Delete(&models.EntityRelation{}).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ?", tenantID).Delete(&models.Entity{}).Error; err != nil {
			return err
		}

		entityIDs := make(map[string]int64, len(parsed.Entities))
		for _, definition := range parsed.Entities {
			entity := &models.Entity{TenantID: tenantID, Name: definition.DisplayName, Code: definition.Name, Status: "draft", CreatedBy: userID}
			if err := entityRepo.Create(entity); err != nil {
				return fmt.Errorf("创建实体 %s: %w", definition.Name, err)
			}
			entityIDs[definition.Name] = entity.ID
			result.CreatedEntities++
			for index, definition := range definition.Attributes {
				attribute := &models.EntityAttribute{
					EntityID: entity.ID, Name: definition.DisplayName, ColumnName: definition.Name,
					DataType: definition.Type, IsPK: definition.IsPK, Nullable: definition.Nullable, SortOrder: index,
				}
				if err := entityRepo.CreateAttribute(attribute); err != nil {
					return fmt.Errorf("创建实体 %s 的属性 %s: %w", entity.Code, definition.Name, err)
				}
			}
		}
		for _, definition := range parsed.Relations {
			relation := &models.EntityRelation{
				TenantID: tenantID, SourceEntity: entityIDs[definition.Source], TargetEntity: entityIDs[definition.Target],
				RelationType: ConvertRelationType(definition.Symbol), Name: definition.Label,
			}
			if err := relationRepo.Create(relation); err != nil {
				return fmt.Errorf("创建关系 %s -> %s: %w", definition.Source, definition.Target, err)
			}
			result.CreatedRelations++
		}
		return nil
	})
	if err != nil {
		return nil, err
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
		metadata, _ := json.Marshal(mermaidEntityMetadata{Code: entity.Code, Name: entity.Name})
		code += "  %% addp:entity " + string(metadata) + "\n"
		code += "  " + entity.Code + " {\n"

		// 查询属性
		attributes, err := s.repo.GetAttributes(entity.ID)
		if err != nil {
			return "", err
		}
		for _, attr := range attributes {
			metadata, _ := json.Marshal(mermaidAttributeMetadata{Entity: entity.Code, Column: attr.ColumnName, Name: attr.Name, Nullable: attr.Nullable})
			code += "    %% addp:attribute " + string(metadata) + "\n"
			dataType := attr.DataType
			pk := ""
			if attr.IsPK {
				pk = " PK"
			}
			code += "    " + dataType + " " + attr.ColumnName + pk + "\n"
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
