package service

import (
	"fmt"

	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

type EntityRelationService struct {
	relationRepo *repository.EntityRelationRepository
	entityRepo   *repository.EntityRepository
}

func (s *EntityRelationService) requireDraftEntities(tenantID, sourceID, targetID int64) (*models.Entity, *models.Entity, error) {
	sourceEntity, err := s.entityRepo.GetByID(sourceID, tenantID)
	if err != nil {
		return nil, nil, apperrors.NotFound("source_entity_not_found", i18n.MsgRelationTargetNotFound)
	}
	targetEntity, err := s.entityRepo.GetByID(targetID, tenantID)
	if err != nil {
		return nil, nil, apperrors.NotFound("target_entity_not_found", i18n.MsgRelationTargetNotFound)
	}
	if sourceEntity.Status != "draft" || targetEntity.Status != "draft" {
		return nil, nil, apperrors.Conflict("entity_relation_state_conflict", i18n.MsgRelationStateConflict)
	}
	return sourceEntity, targetEntity, nil
}

func NewEntityRelationService(relationRepo *repository.EntityRelationRepository, entityRepo *repository.EntityRepository) *EntityRelationService {
	return &EntityRelationService{
		relationRepo: relationRepo,
		entityRepo:   entityRepo,
	}
}

// Create 创建实体关系
func (s *EntityRelationService) Create(tenantID int64, req *models.CreateEntityRelationRequest) (*models.EntityRelation, error) {
	sourceEntity, targetEntity, err := s.requireDraftEntities(tenantID, req.SourceEntity, req.TargetEntity)
	if err != nil {
		return nil, err
	}

	// 不允许自关联
	if req.SourceEntity == req.TargetEntity {
		return nil, apperrors.Conflict("entity_relation_self_conflict", i18n.MsgRelationSelfConflict)
	}

	relation := &models.EntityRelation{
		TenantID:     tenantID,
		SourceEntity: req.SourceEntity,
		TargetEntity: req.TargetEntity,
		RelationType: req.RelationType,
		Name:         req.Name,
		Description:  req.Description,
	}

	// 如果Name为空，生成默认名称
	if relation.Name == "" {
		relation.Name = fmt.Sprintf("%s_%s", sourceEntity.Code, targetEntity.Code)
	}

	if err := s.relationRepo.Create(relation); err != nil {
		return nil, err
	}

	return relation, nil
}

// GetByID 根据ID获取实体关系
func (s *EntityRelationService) GetByID(id, tenantID int64) (*models.EntityRelation, error) {
	relation, err := s.relationRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, modelResourceError(err, "entity_relation_not_found", i18n.MsgRelationNotFound)
	}
	return relation, nil
}

// GetByEntityID 获取某个实体的所有关系
func (s *EntityRelationService) GetByEntityID(tenantID, entityID int64) ([]models.EntityRelation, error) {
	// 验证实体存在且属于当前租户
	if _, err := s.entityRepo.GetByID(entityID, tenantID); err != nil {
		return nil, apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound)
	}

	return s.relationRepo.GetByEntityID(tenantID, entityID)
}

// ListByTenantID 列出租户的所有实体关系
func (s *EntityRelationService) ListByTenantID(tenantID int64) ([]models.EntityRelation, error) {
	return s.relationRepo.ListByTenantID(tenantID)
}

// Update 更新实体关系
func (s *EntityRelationService) Update(id, tenantID int64, req *models.UpdateEntityRelationRequest) (*models.EntityRelation, error) {
	// 获取现有关系
	relation, err := s.relationRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("entity_relation_not_found", i18n.MsgRelationNotFound)
	}
	if _, _, err := s.requireDraftEntities(tenantID, relation.SourceEntity, relation.TargetEntity); err != nil {
		return nil, err
	}

	// 更新字段
	if req.RelationType != "" {
		relation.RelationType = req.RelationType
	}
	if req.Name != "" {
		relation.Name = req.Name
	}
	relation.Description = req.Description

	if err := s.relationRepo.Update(relation); err != nil {
		return nil, err
	}

	return relation, nil
}

// Delete 删除实体关系
func (s *EntityRelationService) Delete(id, tenantID int64) error {
	// 验证关系存在且属于当前租户
	relation, err := s.relationRepo.GetByID(id, tenantID)
	if err != nil {
		return apperrors.NotFound("entity_relation_not_found", i18n.MsgRelationNotFound)
	}
	if _, _, err := s.requireDraftEntities(tenantID, relation.SourceEntity, relation.TargetEntity); err != nil {
		return err
	}

	if err := s.relationRepo.Delete(id, tenantID); err != nil {
		return modelResourceError(err, "entity_relation_not_found", i18n.MsgRelationNotFound)
	}
	return nil
}
