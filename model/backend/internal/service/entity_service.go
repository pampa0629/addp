package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"

	commonClient "github.com/addp/common/client"
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
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
	if err := validateCreateEntityRequest(req); err != nil {
		return nil, err
	}
	if err := s.validateReferences(tenantID, req.DomainID, nil); err != nil {
		return nil, err
	}
	entity := &models.Entity{
		TenantID:    tenantID,
		DomainID:    req.DomainID,
		Name:        req.Name,
		Code:        req.Code,
		Description: req.Description,
		Status:      "draft",
		Version:     1,
		CreatedBy:   userID,
	}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, standardReference(models.StandardResourceDomain, req.DomainID)); err != nil {
			return err
		}
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		txRepo := repository.NewEntityRepository(tx)
		exists, err := txRepo.ExistsByCode(req.Code, tenantID, 0)
		if err != nil {
			return err
		}
		if exists {
			return apperrors.Conflict("entity_code_conflict", i18n.MsgEntityCodeConflict)
		}
		if err := txRepo.Create(entity); err != nil {
			return modelResourceError(err, "entity_code", i18n.MsgEntityCodeConflict)
		}
		_, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
	if err != nil {
		return nil, err
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
	if !validOptionalID(opts.DomainID) || !validListStatus(opts.Status) {
		return nil, 0, invalidRequest()
	}
	return s.repo.List(tenantID, opts)
}

func (s *EntityService) UpdateEntity(id, tenantID, userID int64, req *models.UpdateEntityRequest) (*models.Entity, error) {
	if req == nil || !validOptionalID(req.DomainID) || !validRequiredString(req.Name, 200) {
		return nil, apperrors.Validation("invalid_request", i18n.MsgValidationFailed)
	}

	if err := s.validateReferences(tenantID, req.DomainID, nil); err != nil {
		return nil, err
	}
	var entity *models.Entity
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, standardReference(models.StandardResourceDomain, req.DomainID)); err != nil {
			return err
		}
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		entity, err = repository.LockEntity(tx, id, tenantID)
		if err != nil {
			return modelResourceError(err, "entity_not_found", i18n.MsgEntityNotFound)
		}
		if err := requireVersion(entity.Version, req.Version); err != nil {
			return err
		}
		if entity.Status != "draft" {
			return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
		}
		entity.Name = req.Name
		entity.DomainID = req.DomainID
		entity.Description = req.Description
		entity.UpdatedBy = &userID
		if err := repository.NewEntityRepository(tx).Update(entity); err != nil {
			return err
		}
		_, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
	if err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *EntityService) DeleteEntity(id, tenantID, version int64) error {
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		entity, err := repository.LockEntity(tx, id, tenantID)
		if err != nil {
			return modelResourceError(err, "entity_not_found", i18n.MsgEntityNotFound)
		}
		if err := requireVersion(entity.Version, version); err != nil {
			return err
		}
		if entity.Status != "draft" {
			return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
		}
		relationRepo := repository.NewEntityRelationRepository(tx)
		relations, err := relationRepo.GetByEntityID(tenantID, id)
		if err != nil {
			return err
		}
		for _, relation := range relations {
			otherEntityID := relation.SourceEntity
			if otherEntityID == id {
				otherEntityID = relation.TargetEntity
			}
			otherEntity, err := repository.LockEntity(tx, otherEntityID, tenantID)
			if err != nil {
				return err
			}
			if otherEntity.Status != "draft" {
				return apperrors.Conflict("entity_relation_state_conflict", i18n.MsgRelationStateConflict)
			}
		}
		if err := repository.NewEntityRepository(tx).Delete(id, tenantID, version); err != nil {
			return err
		}
		_, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
}

func (s *EntityService) ApproveEntity(id, tenantID, userID, version int64) (*models.Entity, error) {
	return s.updateEntityStatus(id, tenantID, userID, version, "draft", "approved", true)
}

func (s *EntityService) ReopenEntity(id, tenantID, userID, version int64) (*models.Entity, error) {
	return s.updateEntityStatus(id, tenantID, userID, version, "approved", "draft", false)
}

func (s *EntityService) updateEntityStatus(id, tenantID, userID, version int64, from, to string, validateApproval bool) (*models.Entity, error) {
	var entity *models.Entity
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		entity, err = repository.LockEntity(tx, id, tenantID)
		if err != nil {
			return modelResourceError(err, "entity_not_found", i18n.MsgEntityNotFound)
		}
		if err := requireVersion(entity.Version, version); err != nil {
			return err
		}
		if entity.Status != from {
			return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
		}
		if validateApproval {
			attributes, err := repository.NewEntityRepository(tx).GetAttributes(id)
			if err != nil {
				return err
			}
			if len(attributes) == 0 {
				return apperrors.Validation("entity_approval_attributes_required", i18n.MsgEntityAttributesRequired)
			}
			hasPrimaryKey := false
			for _, attribute := range attributes {
				hasPrimaryKey = hasPrimaryKey || attribute.IsPK
				if attribute.ColumnName == "" || attribute.DataType == "" {
					return apperrors.Validation("entity_approval_attribute_invalid", i18n.MsgEntityAttributeInvalid)
				}
			}
			if !hasPrimaryKey {
				return apperrors.Validation("entity_approval_primary_key_required", i18n.MsgEntityPrimaryKeyRequired)
			}
		}
		if err := repository.NewEntityRepository(tx).UpdateStatus(id, tenantID, version, to, userID); err != nil {
			return err
		}
		entity.Status = to
		entity.Version++
		entity.UpdatedBy = &userID
		_, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
	return entity, err
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
func (s *EntityService) CreateAttribute(entityID, tenantID int64, req *models.CreateEntityAttributeRequest) (*models.EntityAttributeMutationResponse, error) {
	if req == nil {
		return nil, apperrors.Validation("invalid_request", i18n.MsgValidationFailed)
	}
	if err := validateCreateEntityAttributeRequest(req); err != nil {
		return nil, err
	}
	if err := s.validateReferences(tenantID, nil, req.ElementID); err != nil {
		return nil, err
	}

	attr := models.EntityAttribute{
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

	response := &models.EntityAttributeMutationResponse{Attribute: attr}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, standardReference(models.StandardResourceElement, req.ElementID)); err != nil {
			return err
		}
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		entity, err := repository.LockEntity(tx, entityID, tenantID)
		if err != nil {
			return apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
		}
		if err := requireVersion(entity.Version, req.Version); err != nil {
			return err
		}
		if entity.Status != "draft" {
			return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
		}
		txRepo := repository.NewEntityRepository(tx)
		if err := txRepo.CreateAttribute(&response.Attribute); err != nil {
			return modelResourceError(err, "entity_attribute_column", i18n.MsgAttributeColumnConflict)
		}
		response.Version, err = repository.AdvanceEntityVersion(tx, entityID, tenantID, req.Version)
		if err != nil {
			return err
		}
		_, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

// UpdateAttribute 更新实体属性
func (s *EntityService) UpdateAttribute(attrID, entityID, tenantID int64, req *models.UpdateEntityAttributeRequest) (*models.EntityAttributeMutationResponse, error) {
	if req == nil {
		return nil, invalidRequest()
	}
	if err := s.validateReferences(tenantID, nil, req.ElementID); err != nil {
		return nil, err
	}

	if !validRequiredString(req.Name, 200) || !modelCodePattern.MatchString(req.ColumnName) || utf8.RuneCountInString(req.ColumnName) > 200 ||
		!validValue(req.DataType, modelDataTypes...) || !validOptionalID(req.ElementID) ||
		req.IsPK == nil || req.Nullable == nil || req.SortOrder == nil || *req.SortOrder < 0 {
		return nil, apperrors.Validation("invalid_request", i18n.MsgValidationFailed)
	}

	response := &models.EntityAttributeMutationResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, standardReference(models.StandardResourceElement, req.ElementID)); err != nil {
			return err
		}
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		entity, err := repository.LockEntity(tx, entityID, tenantID)
		if err != nil {
			return apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
		}
		if err := requireVersion(entity.Version, req.Version); err != nil {
			return err
		}
		if entity.Status != "draft" {
			return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
		}
		txRepo := repository.NewEntityRepository(tx)
		attr, err := txRepo.GetAttributeByID(attrID, entityID)
		if err != nil {
			return apperrors.NotFound("attribute_not_found", i18n.MsgAttributeNotFound)
		}
		attr.Name = req.Name
		attr.ColumnName = req.ColumnName
		attr.DataType = req.DataType
		attr.ElementID = req.ElementID
		attr.IsPK = *req.IsPK
		attr.Nullable = *req.Nullable
		attr.Description = req.Description
		attr.SortOrder = *req.SortOrder
		if err := txRepo.UpdateAttribute(attr); err != nil {
			return modelResourceError(err, "entity_attribute_column", i18n.MsgAttributeColumnConflict)
		}
		response.Attribute = *attr
		response.Version, err = repository.AdvanceEntityVersion(tx, entityID, tenantID, req.Version)
		if err != nil {
			return err
		}
		_, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

// DeleteAttribute 删除实体属性
func (s *EntityService) DeleteAttribute(attrID, entityID, tenantID, version int64) (*models.VersionResponse, error) {
	response := &models.VersionResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		entity, err := repository.LockEntity(tx, entityID, tenantID)
		if err != nil {
			return apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
		}
		if err := requireVersion(entity.Version, version); err != nil {
			return err
		}
		if entity.Status != "draft" {
			return apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
		}
		if err := repository.NewEntityRepository(tx).DeleteAttribute(attrID, entityID); err != nil {
			return modelResourceError(err, "attribute_not_found", i18n.MsgAttributeNotFound)
		}
		response.Version, err = repository.AdvanceEntityVersion(tx, entityID, tenantID, version)
		if err != nil {
			return err
		}
		_, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
	return response, err
}

// ImportFromMermaid 从 Mermaid ER 图全量替换当前租户的实体模型。
func (s *EntityService) ImportFromMermaid(tenantID, userID int64, req *models.MermaidImportRequest) (*models.MermaidImportResult, error) {
	if req == nil || req.Revision <= 0 {
		return nil, invalidRequest()
	}
	result := &models.MermaidImportResult{}
	parsed, err := ParseMermaidER(req.MermaidCode)
	if err != nil {
		return nil, apperrors.Wrap(apperrors.KindValidation, "mermaid_invalid", i18n.MsgValidationFailed, err)
	}
	for _, entity := range parsed.Entities {
		if err := s.validateReferences(tenantID, entity.DomainID, nil); err != nil {
			return nil, err
		}
		for _, attribute := range entity.Attributes {
			if err := s.validateReferences(tenantID, nil, attribute.ElementID); err != nil {
				return nil, err
			}
		}
	}
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		references := make([]models.StandardReference, 0, len(parsed.Entities)*2)
		for _, entity := range parsed.Entities {
			references = append(references, standardReference(models.StandardResourceDomain, entity.DomainID))
			for _, attribute := range entity.Attributes {
				references = append(references, standardReference(models.StandardResourceElement, attribute.ElementID))
			}
		}
		if err := lockStandardReferences(tx, tenantID, references...); err != nil {
			return err
		}
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		if err := requireVersion(revision.Revision, req.Revision); err != nil {
			return err
		}
		var existingEntities []models.Entity
		if err := tx.Where("tenant_id = ?", tenantID).Find(&existingEntities).Error; err != nil {
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
			entity := &models.Entity{
				TenantID: tenantID, DomainID: definition.DomainID, Name: definition.DisplayName,
				Code: definition.Name, Description: definition.Description, Status: "draft", Version: 1, CreatedBy: userID,
			}
			if err := entityRepo.Create(entity); err != nil {
				return fmt.Errorf("创建实体 %s: %w", definition.Name, err)
			}
			entityIDs[definition.Name] = entity.ID
			result.CreatedEntities++
			for _, definition := range definition.Attributes {
				attribute := &models.EntityAttribute{
					EntityID: entity.ID, ElementID: definition.ElementID, Name: definition.DisplayName,
					ColumnName: definition.Name, DataType: definition.Type, IsPK: definition.IsPK,
					Nullable: definition.Nullable, Description: definition.Description, SortOrder: definition.SortOrder,
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
				Description: definition.Description, Version: 1,
			}
			if err := relationRepo.Create(relation); err != nil {
				return fmt.Errorf("创建关系 %s -> %s: %w", definition.Source, definition.Target, err)
			}
			result.CreatedRelations++
		}
		result.Revision, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ExportToMermaid 导出实体和关系为Mermaid代码
func (s *EntityService) ExportToMermaid(tenantID int64) (*models.MermaidExportResponse, error) {
	response := &models.MermaidExportResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		entityRepo := repository.NewEntityRepository(tx)
		relationRepo := repository.NewEntityRelationRepository(tx)
		entities, err := entityRepo.ListByTenantID(tenantID)
		if err != nil {
			return err
		}
		relations, err := relationRepo.ListByTenantID(tenantID)
		if err != nil {
			return err
		}
		code := "erDiagram\n"

		// 实体定义
		for _, entity := range entities {
			metadata, _ := json.Marshal(mermaidEntityMetadata{
				Code: entity.Code, Name: entity.Name, DomainID: entity.DomainID, Description: entity.Description,
			})
			code += "  %% addp:entity " + string(metadata) + "\n"
			code += "  " + entity.Code + " {\n"

			attributes, err := entityRepo.GetAttributes(entity.ID)
			if err != nil {
				return err
			}
			for _, attr := range attributes {
				metadata, _ := json.Marshal(mermaidAttributeMetadata{
					Entity: entity.Code, Column: attr.ColumnName, Name: attr.Name, Nullable: attr.Nullable,
					ElementID: attr.ElementID, Description: attr.Description, SortOrder: attr.SortOrder,
				})
				code += "    %% addp:attribute " + string(metadata) + "\n"
				pk := ""
				if attr.IsPK {
					pk = " PK"
				}
				code += "    " + attr.DataType + " " + attr.ColumnName + pk + "\n"
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
				metadata, _ := json.Marshal(mermaidRelationMetadata{
					Source: sourceEntity.Code, Target: targetEntity.Code, RelationType: relation.RelationType,
					Name: relation.Name, Description: relation.Description,
				})
				code += "  %% addp:relation " + string(metadata) + "\n"
				code += "  " + sourceEntity.Code + " " + ConvertToMermaidSymbol(relation.RelationType) + " " + targetEntity.Code + " : " + strconv.Quote(relation.Name) + "\n"
			}
		}
		response.MermaidCode = code
		response.Revision = revision.Revision
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}
