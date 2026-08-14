package service

import (
	"fmt"
	"sort"

	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

type EntityRelationService struct {
	relationRepo *repository.EntityRelationRepository
	entityRepo   *repository.EntityRepository
}

func lockDraftEntities(tx *gorm.DB, tenantID int64, ids ...int64) (map[int64]*models.Entity, error) {
	unique := make(map[int64]struct{}, len(ids))
	ordered := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := unique[id]; ok {
			continue
		}
		unique[id] = struct{}{}
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	entities := make(map[int64]*models.Entity, len(ordered))
	for _, id := range ordered {
		entity, err := repository.LockEntity(tx, id, tenantID)
		if err != nil {
			return nil, apperrors.NotFound("entity_relation_target_not_found", i18n.MsgRelationTargetNotFound)
		}
		if entity.Status != "draft" {
			return nil, apperrors.Conflict("entity_relation_state_conflict", i18n.MsgRelationStateConflict)
		}
		entities[id] = entity
	}
	return entities, nil
}

func NewEntityRelationService(relationRepo *repository.EntityRelationRepository, entityRepo *repository.EntityRepository) *EntityRelationService {
	return &EntityRelationService{
		relationRepo: relationRepo,
		entityRepo:   entityRepo,
	}
}

// Create 创建实体关系
func (s *EntityRelationService) Create(tenantID int64, req *models.CreateEntityRelationRequest) (*models.EntityRelation, error) {
	if err := validateCreateEntityRelationRequest(req); err != nil {
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
		Version:      1,
	}
	err := s.relationRepo.DB().Transaction(func(tx *gorm.DB) error {
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		entities, err := lockDraftEntities(tx, tenantID, req.SourceEntity, req.TargetEntity)
		if err != nil {
			return err
		}
		if relation.Name == "" {
			relation.Name = fmt.Sprintf("%s_%s", entities[req.SourceEntity].Code, entities[req.TargetEntity].Code)
		}
		if !validOptionalStringLength(relation.Name, 200) {
			return invalidRequest()
		}
		if err := repository.NewEntityRelationRepository(tx).Create(relation); err != nil {
			return modelResourceError(err, "entity_relation", i18n.MsgRelationConflict)
		}
		_, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
	if err != nil {
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
	if req == nil || (req.RelationType != "one_to_one" && req.RelationType != "one_to_many" && req.RelationType != "many_to_many") ||
		req.SourceEntity <= 0 || req.TargetEntity <= 0 || req.SourceEntity == req.TargetEntity || !validOptionalStringLength(req.Name, 200) {
		return nil, apperrors.Validation("invalid_request", i18n.MsgValidationFailed)
	}
	var relation *models.EntityRelation
	err := s.relationRepo.DB().Transaction(func(tx *gorm.DB) error {
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		relation, err = repository.LockEntityRelation(tx, id, tenantID)
		if err != nil {
			return apperrors.NotFound("entity_relation_not_found", i18n.MsgRelationNotFound)
		}
		if err := requireVersion(relation.Version, req.Version); err != nil {
			return err
		}
		if _, err := lockDraftEntities(tx, tenantID, relation.SourceEntity, relation.TargetEntity, req.SourceEntity, req.TargetEntity); err != nil {
			return err
		}
		relation.SourceEntity = req.SourceEntity
		relation.TargetEntity = req.TargetEntity
		relation.RelationType = req.RelationType
		relation.Name = req.Name
		relation.Description = req.Description
		if err := repository.NewEntityRelationRepository(tx).Update(relation); err != nil {
			return modelResourceError(err, "entity_relation", i18n.MsgRelationConflict)
		}
		_, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
	if err != nil {
		return nil, err
	}

	return relation, nil
}

// Delete 删除实体关系
func (s *EntityRelationService) Delete(id, tenantID, version int64) error {
	return s.relationRepo.DB().Transaction(func(tx *gorm.DB) error {
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		relation, err := repository.LockEntityRelation(tx, id, tenantID)
		if err != nil {
			return apperrors.NotFound("entity_relation_not_found", i18n.MsgRelationNotFound)
		}
		if err := requireVersion(relation.Version, version); err != nil {
			return err
		}
		if _, err := lockDraftEntities(tx, tenantID, relation.SourceEntity, relation.TargetEntity); err != nil {
			return err
		}
		if err := repository.NewEntityRelationRepository(tx).Delete(id, tenantID, version); err != nil {
			return modelResourceError(err, "entity_relation_not_found", i18n.MsgRelationNotFound)
		}
		_, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		return err
	})
}
