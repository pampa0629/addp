package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
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
	entity, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, modelResourceError(err, "entity_not_found", i18n.MsgEntityNotFound)
	}
	if err := requireVersion(entity.Version, version); err != nil {
		return nil, err
	}
	if entity.Status != "draft" {
		return nil, apperrors.Conflict("entity_state_conflict", i18n.MsgEntityStateConflict)
	}
	attributes, err := s.repo.GetAttributes(id)
	if err != nil {
		return nil, err
	}
	elementIDs := make([]int64, 0, len(attributes))
	for _, attribute := range attributes {
		if attribute.ElementID != nil {
			elementIDs = append(elementIDs, *attribute.ElementID)
		}
	}
	bindings, err := resolveElementRevisionSnapshot(s.standard, tenantID, elementIDs, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return s.updateEntityStatus(id, tenantID, userID, version, "draft", "approved", true, bindings)
}

func (s *EntityService) ReopenEntity(id, tenantID, userID, version int64) (*models.Entity, error) {
	return s.updateEntityStatus(id, tenantID, userID, version, "approved", "draft", false, nil)
}

func (s *EntityService) updateEntityStatus(id, tenantID, userID, version int64, from, to string, validateApproval bool, elementRevisions map[int64]int64) (*models.Entity, error) {
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
		txRepo := repository.NewEntityRepository(tx)
		if validateApproval {
			attributes, err := txRepo.GetAttributes(id)
			if err != nil {
				return err
			}
			if len(attributes) == 0 {
				return apperrors.Validation("entity_approval_attributes_required", i18n.MsgEntityAttributesRequired)
			}
			hasPrimaryKey := false
			references := make([]models.StandardReference, 0, len(attributes))
			for _, attribute := range attributes {
				hasPrimaryKey = hasPrimaryKey || attribute.IsPK
				if attribute.ColumnName == "" || attribute.DataType == "" {
					return apperrors.Validation("entity_approval_attribute_invalid", i18n.MsgEntityAttributeInvalid)
				}
				if attribute.ElementID != nil {
					if elementRevisions[*attribute.ElementID] <= 0 {
						return apperrors.NotFound("element_revision_not_found", i18n.MsgReferenceNotFound)
					}
					references = append(references, requiredStandardReference(models.StandardResourceElement, *attribute.ElementID))
				}
			}
			if !hasPrimaryKey {
				return apperrors.Validation("entity_approval_primary_key_required", i18n.MsgEntityPrimaryKeyRequired)
			}
			if err := lockStandardReferences(tx, tenantID, references...); err != nil {
				return err
			}
			if err := txRepo.FreezeAttributeElementRevisions(id, elementRevisions); err != nil {
				return err
			}
		}
		if err := txRepo.UpdateStatus(id, tenantID, version, to, userID); err != nil {
			return err
		}
		if to == "draft" {
			if err := txRepo.ClearAttributeElementRevisions(id); err != nil {
				return err
			}
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

type mermaidImportPlan struct {
	preview      models.MermaidImportPreview
	newEntities  []EntityDefinition
	newRelations []RelationDefinition
	entityIDs    map[string]int64
}

func (s *EntityService) parseMermaidImport(tenantID int64, markdown string) (*MermaidERParser, []models.MermaidResolvedDomain, error) {
	parsed, err := ParseMermaidER(markdown)
	if err != nil {
		return nil, nil, apperrors.Wrap(apperrors.KindValidation, "mermaid_invalid", i18n.MsgValidationFailed, err)
	}
	resolvedDomains, err := s.resolveMermaidImportReferences(tenantID, parsed)
	if err != nil {
		return nil, nil, err
	}
	return parsed, resolvedDomains, nil
}

func (s *EntityService) resolveMermaidImportReferences(tenantID int64, parsed *MermaidERParser) ([]models.MermaidResolvedDomain, error) {
	references := make([]commonClient.StandardCodeReference, 0, len(parsed.Entities)*2+1)
	seen := map[string]struct{}{}
	appendReference := func(objectType string, code *string) {
		if code == nil {
			return
		}
		key := objectType + ":" + *code
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		references = append(references, commonClient.StandardCodeReference{ObjectType: objectType, Code: *code})
	}
	appendReference("domain", parsed.Document.DomainCode)
	for index := range parsed.Entities {
		entity := &parsed.Entities[index]
		appendReference("domain", entity.DomainCode)
		for _, attribute := range entity.Attributes {
			appendReference("element", attribute.ElementCode)
		}
	}
	if len(references) == 0 {
		return []models.MermaidResolvedDomain{}, nil
	}
	if s.standard == nil {
		return nil, standardReferenceError(errors.New("standard client is required to resolve Mermaid references"), "domain_not_found")
	}
	resolved := make(map[string]commonClient.StandardReferenceResolution, len(references))
	client := s.standard.WithTenantID(uint(tenantID))
	for offset := 0; offset < len(references); offset += 200 {
		end := offset + 200
		if end > len(references) {
			end = len(references)
		}
		batch, resolveErr := client.ResolveReferencesByCode(context.Background(), references[offset:end])
		if resolveErr != nil {
			return nil, standardReferenceError(resolveErr, "domain_not_found")
		}
		for _, resolution := range batch {
			if !resolution.Found || !resolution.Referenceable {
				if resolution.Found && resolution.LifecycleState == "deleting" {
					return nil, standardReferenceError(commonClient.ErrStandardReferenceDeleting, resolution.ObjectType+"_not_found")
				}
				return nil, standardReferenceError(commonClient.ErrTenantReferenceNotFound, resolution.ObjectType+"_not_found")
			}
			resolved[resolution.ObjectType+":"+resolution.Code] = resolution
		}
	}
	domainSummaries := map[string]models.MermaidResolvedDomain{}
	for index := range parsed.Entities {
		entity := &parsed.Entities[index]
		if entity.DomainCode != nil {
			resolution := resolved["domain:"+*entity.DomainCode]
			id := resolution.ID
			entity.DomainID = &id
			domainSummaries[resolution.Code] = models.MermaidResolvedDomain{Code: resolution.Code, Name: resolution.Name}
		}
		for attributeIndex := range entity.Attributes {
			attribute := &entity.Attributes[attributeIndex]
			if attribute.ElementCode != nil {
				resolution := resolved["element:"+*attribute.ElementCode]
				id := resolution.ID
				attribute.ElementID = &id
			}
		}
	}
	if parsed.Document.DomainCode != nil {
		resolution := resolved["domain:"+*parsed.Document.DomainCode]
		domainSummaries[resolution.Code] = models.MermaidResolvedDomain{Code: resolution.Code, Name: resolution.Name}
	}
	summaries := make([]models.MermaidResolvedDomain, 0, len(domainSummaries))
	for _, summary := range domainSummaries {
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Code < summaries[j].Code })
	return summaries, nil
}

func sameOptionalID(left, right *int64) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func sameEntityDefinition(entity models.Entity, attributes []models.EntityAttribute, definition EntityDefinition) bool {
	if !sameOptionalID(entity.DomainID, definition.DomainID) || entity.Name != definition.DisplayName || entity.Description != definition.Description || len(attributes) != len(definition.Attributes) {
		return false
	}
	byColumn := make(map[string]models.EntityAttribute, len(attributes))
	for _, attribute := range attributes {
		byColumn[attribute.ColumnName] = attribute
	}
	for _, definition := range definition.Attributes {
		attribute, ok := byColumn[definition.Name]
		if !ok || attribute.Name != definition.DisplayName || attribute.DataType != definition.Type ||
			attribute.IsPK != definition.IsPK || attribute.Nullable != definition.Nullable ||
			!sameOptionalID(attribute.ElementID, definition.ElementID) || attribute.Description != definition.Description ||
			attribute.SortOrder != definition.SortOrder {
			return false
		}
	}
	return true
}

func (s *EntityService) buildMermaidImportPlan(tx *gorm.DB, tenantID, revision int64, parsed *MermaidERParser) (*mermaidImportPlan, error) {
	plan := &mermaidImportPlan{
		preview: models.MermaidImportPreview{
			Revision: revision, Scope: parsed.Document.Scope, DomainCode: parsed.Document.DomainCode,
			Conflicts: []models.MermaidImportConflict{},
		},
		entityIDs: make(map[string]int64, len(parsed.Entities)),
	}
	entityRepo := repository.NewEntityRepository(tx)
	existingEntities, err := entityRepo.ListByTenantID(tenantID)
	if err != nil {
		return nil, err
	}
	existingByCode := make(map[string]models.Entity, len(existingEntities))
	for _, entity := range existingEntities {
		existingByCode[entity.Code] = entity
	}
	for _, definition := range parsed.Entities {
		entity, exists := existingByCode[definition.Name]
		if !exists {
			plan.newEntities = append(plan.newEntities, definition)
			plan.preview.CreatedEntities++
			continue
		}
		plan.entityIDs[definition.Name] = entity.ID
		attributes, err := entityRepo.GetAttributes(entity.ID)
		if err != nil {
			return nil, err
		}
		if !sameEntityDefinition(entity, attributes, definition) {
			plan.preview.Conflicts = append(plan.preview.Conflicts, models.MermaidImportConflict{
				ResourceType: "entity", Key: definition.Name, Reason: "definition_mismatch",
			})
			continue
		}
		plan.preview.UnchangedEntities++
	}

	relations, err := repository.NewEntityRelationRepository(tx).ListByTenantID(tenantID)
	if err != nil {
		return nil, err
	}
	codeByID := make(map[int64]string, len(existingEntities))
	for _, entity := range existingEntities {
		codeByID[entity.ID] = entity.Code
	}
	existingRelations := make(map[string]models.EntityRelation, len(relations))
	for _, relation := range relations {
		key := mermaidRelationKey(codeByID[relation.SourceEntity], codeByID[relation.TargetEntity], relation.RelationType, relation.Name)
		existingRelations[key] = relation
	}
	for _, definition := range parsed.Relations {
		relationType := ConvertRelationType(definition.Symbol)
		key := mermaidRelationKey(definition.Source, definition.Target, relationType, definition.Label)
		if relation, exists := existingRelations[key]; exists {
			if relation.Description != definition.Description {
				plan.preview.Conflicts = append(plan.preview.Conflicts, models.MermaidImportConflict{
					ResourceType: "relation", Key: key, Reason: "definition_mismatch",
				})
			} else {
				plan.preview.UnchangedRelations++
			}
			continue
		}
		source, sourceExists := existingByCode[definition.Source]
		target, targetExists := existingByCode[definition.Target]
		if (sourceExists && source.Status != "draft") || (targetExists && target.Status != "draft") {
			plan.preview.Conflicts = append(plan.preview.Conflicts, models.MermaidImportConflict{
				ResourceType: "relation", Key: key, Reason: "entity_state_conflict",
			})
			continue
		}
		plan.newRelations = append(plan.newRelations, definition)
		plan.preview.CreatedRelations++
	}
	return plan, nil
}

func standardReferencesFromMermaid(parsed *MermaidERParser) []models.StandardReference {
	references := make([]models.StandardReference, 0, len(parsed.Entities)*2)
	for _, entity := range parsed.Entities {
		references = append(references, standardReference(models.StandardResourceDomain, entity.DomainID))
		for _, attribute := range entity.Attributes {
			references = append(references, standardReference(models.StandardResourceElement, attribute.ElementID))
		}
	}
	return references
}

func (s *EntityService) PreviewMermaidImport(tenantID int64, req *models.MermaidImportPreviewRequest) (*models.MermaidImportPreview, error) {
	if req == nil || req.Markdown == "" {
		return nil, invalidRequest()
	}
	parsed, resolvedDomains, err := s.parseMermaidImport(tenantID, req.Markdown)
	if err != nil {
		return nil, err
	}
	var preview *models.MermaidImportPreview
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		plan, err := s.buildMermaidImportPlan(tx, tenantID, revision.Revision, parsed)
		if err != nil {
			return err
		}
		preview = &plan.preview
		preview.ResolvedDomains = resolvedDomains
		return nil
	})
	return preview, err
}

// ImportFromMermaid 在预览基线上增量创建缺失的实体和关系。
func (s *EntityService) ImportFromMermaid(tenantID, userID int64, req *models.MermaidImportRequest) (*models.MermaidImportResult, error) {
	if req == nil || req.Revision <= 0 || req.Markdown == "" {
		return nil, invalidRequest()
	}
	parsed, _, err := s.parseMermaidImport(tenantID, req.Markdown)
	if err != nil {
		return nil, err
	}
	result := &models.MermaidImportResult{}
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockStandardReferences(tx, tenantID, standardReferencesFromMermaid(parsed)...); err != nil {
			return err
		}
		revision, err := repository.LockEntityModelRevision(tx, tenantID)
		if err != nil {
			return err
		}
		if err := requireVersion(revision.Revision, req.Revision); err != nil {
			return err
		}
		plan, err := s.buildMermaidImportPlan(tx, tenantID, revision.Revision, parsed)
		if err != nil {
			return err
		}
		if len(plan.preview.Conflicts) > 0 {
			return apperrors.Conflict("mermaid_import_conflict", i18n.MsgMermaidImportConflict)
		}
		entityRepo := repository.NewEntityRepository(tx)
		for _, definition := range plan.newEntities {
			entity := &models.Entity{
				TenantID: tenantID, DomainID: definition.DomainID, Name: definition.DisplayName,
				Code: definition.Name, Description: definition.Description, Status: "draft", Version: 1, CreatedBy: userID,
			}
			if err := entityRepo.Create(entity); err != nil {
				return modelResourceError(err, "entity_code", i18n.MsgEntityCodeConflict)
			}
			plan.entityIDs[definition.Name] = entity.ID
			for _, definition := range definition.Attributes {
				attribute := &models.EntityAttribute{
					EntityID: entity.ID, ElementID: definition.ElementID, Name: definition.DisplayName,
					ColumnName: definition.Name, DataType: definition.Type, IsPK: definition.IsPK,
					Nullable: definition.Nullable, Description: definition.Description, SortOrder: definition.SortOrder,
				}
				if err := entityRepo.CreateAttribute(attribute); err != nil {
					return modelResourceError(err, "entity_attribute_column", i18n.MsgAttributeColumnConflict)
				}
			}
		}
		relationRepo := repository.NewEntityRelationRepository(tx)
		for _, definition := range plan.newRelations {
			relation := &models.EntityRelation{
				TenantID: tenantID, SourceEntity: plan.entityIDs[definition.Source], TargetEntity: plan.entityIDs[definition.Target],
				RelationType: ConvertRelationType(definition.Symbol), Name: definition.Label,
				Description: definition.Description, Version: 1,
			}
			if err := relationRepo.Create(relation); err != nil {
				return modelResourceError(err, "entity_relation", i18n.MsgRelationConflict)
			}
		}
		result.CreatedEntities = plan.preview.CreatedEntities
		result.UnchangedEntities = plan.preview.UnchangedEntities
		result.CreatedRelations = plan.preview.CreatedRelations
		result.UnchangedRelations = plan.preview.UnchangedRelations
		result.Revision = revision.Revision
		if result.CreatedEntities > 0 || result.CreatedRelations > 0 {
			result.Revision, err = repository.AdvanceEntityModelRevision(tx, tenantID, revision.Revision)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type mermaidExportEntity struct {
	Entity     models.Entity
	Attributes []models.EntityAttribute
}

func (s *EntityService) resolveMermaidExportReferences(tenantID int64, domainID *int64, entities []mermaidExportEntity) (map[int64]commonClient.StandardReferenceResolution, map[int64]commonClient.StandardReferenceResolution, error) {
	references := make([]commonClient.StandardReference, 0, len(entities)*2+1)
	seen := map[string]struct{}{}
	appendReference := func(objectType string, id *int64) {
		if id == nil {
			return
		}
		key := objectType + ":" + strconv.FormatInt(*id, 10)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		references = append(references, commonClient.StandardReference{ObjectType: objectType, ID: *id})
	}
	appendReference("domain", domainID)
	for _, snapshot := range entities {
		appendReference("domain", snapshot.Entity.DomainID)
		for _, attribute := range snapshot.Attributes {
			appendReference("element", attribute.ElementID)
		}
	}
	domains := map[int64]commonClient.StandardReferenceResolution{}
	elements := map[int64]commonClient.StandardReferenceResolution{}
	if len(references) == 0 {
		return domains, elements, nil
	}
	if s.standard == nil {
		return nil, nil, standardReferenceError(errors.New("standard client is required to export Mermaid references"), "domain_not_found")
	}
	client := s.standard.WithTenantID(uint(tenantID))
	for offset := 0; offset < len(references); offset += 200 {
		end := offset + 200
		if end > len(references) {
			end = len(references)
		}
		batch, err := client.ResolveReferences(context.Background(), references[offset:end])
		if err != nil {
			return nil, nil, standardReferenceError(err, "domain_not_found")
		}
		for _, resolution := range batch {
			if !resolution.Found || !resolution.Referenceable {
				if resolution.Found && resolution.LifecycleState == "deleting" {
					return nil, nil, standardReferenceError(commonClient.ErrStandardReferenceDeleting, resolution.ObjectType+"_not_found")
				}
				return nil, nil, standardReferenceError(commonClient.ErrTenantReferenceNotFound, resolution.ObjectType+"_not_found")
			}
			if resolution.ObjectType == "domain" {
				domains[resolution.ID] = resolution
			} else {
				elements[resolution.ID] = resolution
			}
		}
	}
	return domains, elements, nil
}

// ExportToMermaid 按可选业务域导出 Markdown Mermaid 文档。
func (s *EntityService) ExportToMermaid(tenantID int64, domainID *int64) (*models.MermaidExportResponse, error) {
	if !validOptionalID(domainID) {
		return nil, invalidRequest()
	}
	var entities []mermaidExportEntity
	var relations []models.EntityRelation
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if _, err := repository.LockEntityModelRevision(tx, tenantID); err != nil {
			return err
		}
		entityRepo := repository.NewEntityRepository(tx)
		allEntities, err := entityRepo.ListByTenantID(tenantID)
		if err != nil {
			return err
		}
		for _, entity := range allEntities {
			if domainID != nil && (entity.DomainID == nil || *entity.DomainID != *domainID) {
				continue
			}
			attributes, err := entityRepo.GetAttributes(entity.ID)
			if err != nil {
				return err
			}
			entities = append(entities, mermaidExportEntity{Entity: entity, Attributes: attributes})
		}
		relations, err = repository.NewEntityRelationRepository(tx).ListByTenantID(tenantID)
		return err
	})
	if err != nil {
		return nil, err
	}
	domainReferences, elementReferences, err := s.resolveMermaidExportReferences(tenantID, domainID, entities)
	if err != nil {
		return nil, err
	}
	document := MermaidDocumentMetadata{Format: mermaidDocumentFormat, Scope: "all"}
	if domainID != nil {
		document.Scope = "domain"
		code := domainReferences[*domainID].Code
		document.DomainCode = &code
	}
	documentJSON, _ := json.Marshal(document)
	var code strings.Builder
	code.WriteString("erDiagram\n  %% addp:document ")
	code.Write(documentJSON)
	code.WriteByte('\n')
	entityByID := make(map[int64]models.Entity, len(entities))
	for _, snapshot := range entities {
		entity := snapshot.Entity
		entityByID[entity.ID] = entity
		var domainCode *string
		if entity.DomainID != nil {
			value := domainReferences[*entity.DomainID].Code
			domainCode = &value
		}
		metadata, _ := json.Marshal(mermaidEntityMetadata{
			Code: entity.Code, Name: entity.Name, DomainCode: domainCode, Description: entity.Description,
		})
		code.WriteString("  %% addp:entity ")
		code.Write(metadata)
		code.WriteString("\n  " + entity.Code + " {\n")
		for _, attribute := range snapshot.Attributes {
			var elementCode *string
			if attribute.ElementID != nil {
				value := elementReferences[*attribute.ElementID].Code
				elementCode = &value
			}
			metadata, _ := json.Marshal(mermaidAttributeMetadata{
				Entity: entity.Code, Column: attribute.ColumnName, Name: attribute.Name, Nullable: attribute.Nullable,
				ElementCode: elementCode, Description: attribute.Description, SortOrder: attribute.SortOrder,
			})
			code.WriteString("    %% addp:attribute ")
			code.Write(metadata)
			code.WriteString("\n    " + attribute.DataType + " " + attribute.ColumnName)
			if attribute.IsPK {
				code.WriteString(" PK")
			}
			code.WriteByte('\n')
		}
		code.WriteString("  }\n")
	}
	for _, relation := range relations {
		sourceEntity, sourceExists := entityByID[relation.SourceEntity]
		targetEntity, targetExists := entityByID[relation.TargetEntity]
		if !sourceExists || !targetExists {
			continue
		}
		metadata, _ := json.Marshal(mermaidRelationMetadata{
			Source: sourceEntity.Code, Target: targetEntity.Code, RelationType: relation.RelationType,
			Name: relation.Name, Description: relation.Description,
		})
		code.WriteString("  %% addp:relation ")
		code.Write(metadata)
		code.WriteString("\n  " + sourceEntity.Code + " " + ConvertToMermaidSymbol(relation.RelationType) + " " + targetEntity.Code + " : " + strconv.Quote(relation.Name) + "\n")
	}
	return &models.MermaidExportResponse{
		Markdown: "# ADDP Entity Relationship Diagram\n\n```mermaid\n" + code.String() + "```\n",
		Scope:    document.Scope, DomainCode: document.DomainCode,
	}, nil
}
