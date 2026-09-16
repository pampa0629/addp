package service

import (
	"sort"

	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

// ConceptMappingService manages the realization contract between an approved
// conceptual entity model and one logical-table aggregate.
type ConceptMappingService struct {
	repo      *repository.ConceptMappingRepository
	tableRepo *repository.LogicalTableRepository
}

func NewConceptMappingService(
	repo *repository.ConceptMappingRepository,
	tableRepo *repository.LogicalTableRepository,
) *ConceptMappingService {
	return &ConceptMappingService{repo: repo, tableRepo: tableRepo}
}

func (s *ConceptMappingService) Get(tableID, tenantID int64) (*models.ConceptMappingsResponse, error) {
	table, err := s.tableRepo.GetByID(tableID, tenantID)
	if err != nil {
		return nil, modelResourceError(err, "logical_table_not_found", i18n.MsgTableNotFound)
	}
	return loadConceptMappings(s.repo, table.ID, tenantID, table.Version)
}

func (s *ConceptMappingService) Replace(
	tableID, tenantID int64,
	req *models.ReplaceConceptMappingsRequest,
) (*models.ConceptMappingsResponse, error) {
	if err := validateConceptMappingRequest(req); err != nil {
		return nil, err
	}

	var version int64
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		tableRelations, err := loadRequestedTableRelations(tx, tableID, tenantID, req.RelationMappings)
		if err != nil {
			return err
		}
		tableIDs := []int64{tableID}
		for _, relation := range tableRelations {
			tableIDs = append(tableIDs, relation.TargetTable)
		}
		tables, err := lockLogicalTables(tx, tenantID, tableIDs...)
		if err != nil {
			return err
		}
		table := tables[tableID]
		if err := requireVersion(table.Version, req.Version); err != nil {
			return err
		}
		if table.Status != "draft" {
			return apperrors.Conflict("concept_mapping_state_conflict", i18n.MsgConceptMappingStateConflict)
		}

		// Re-read after locking the source logical table. Table-relation mutations
		// use that same aggregate lock, so the requested relations are now stable.
		tableRelations, err = loadRequestedTableRelations(tx, tableID, tenantID, req.RelationMappings)
		if err != nil {
			return err
		}

		entityRelations, err := lockRequestedEntityRelations(tx, tenantID, req.RelationMappings)
		if err != nil {
			return err
		}
		entityIDs := make([]int64, 0, len(req.TableMappings)+len(entityRelations)*2)
		for _, mapping := range req.TableMappings {
			entityIDs = append(entityIDs, mapping.EntityID)
		}
		for _, relation := range entityRelations {
			entityIDs = append(entityIDs, relation.SourceEntity, relation.TargetEntity)
		}
		entities, err := lockApprovedEntities(tx, tenantID, entityIDs...)
		if err != nil {
			return err
		}

		tableMappings, mappedEntities := buildTableEntityMappings(tableID, tenantID, req.TableMappings, entities)
		fieldMappings, err := buildFieldAttributeMappings(tx, tableID, tenantID, req.FieldMappings, mappedEntities)
		if err != nil {
			return err
		}
		relationMappings, err := buildRelationConceptMappings(
			tx,
			tableID,
			tenantID,
			req.RelationMappings,
			tableRelations,
			entityRelations,
			mappedEntities,
			entities,
		)
		if err != nil {
			return err
		}

		txRepo := repository.NewConceptMappingRepository(tx)
		if err := txRepo.Replace(tableID, tenantID, tableMappings, fieldMappings, relationMappings); err != nil {
			return modelResourceError(err, "concept_mapping_invalid", i18n.MsgConceptMappingInvalid)
		}
		version, err = repository.AdvanceLogicalTableVersion(tx, tableID, tenantID, req.Version)
		return err
	})
	if err != nil {
		return nil, err
	}
	return loadConceptMappings(s.repo, tableID, tenantID, version)
}

func loadConceptMappings(
	repo *repository.ConceptMappingRepository,
	tableID, tenantID, version int64,
) (*models.ConceptMappingsResponse, error) {
	tableMappings, err := repo.ListTableMappingViews(tableID, tenantID)
	if err != nil {
		return nil, err
	}
	fieldMappings, err := repo.ListFieldMappingViews(tableID, tenantID)
	if err != nil {
		return nil, err
	}
	relationMappings, err := repo.ListRelationMappingViews(tableID, tenantID)
	if err != nil {
		return nil, err
	}
	return &models.ConceptMappingsResponse{
		Version:          version,
		TableMappings:    tableMappings,
		FieldMappings:    fieldMappings,
		RelationMappings: relationMappings,
	}, nil
}

func validateConceptMappingRequest(req *models.ReplaceConceptMappingsRequest) error {
	if req == nil || req.Version <= 0 {
		return conceptMappingInvalid()
	}
	tableKeys := make(map[int64]struct{}, len(req.TableMappings))
	for _, mapping := range req.TableMappings {
		if mapping.EntityID <= 0 || !validValue(mapping.MappingRole, "represents", "derives_from") {
			return conceptMappingInvalid()
		}
		if _, exists := tableKeys[mapping.EntityID]; exists {
			return conceptMappingInvalid()
		}
		tableKeys[mapping.EntityID] = struct{}{}
	}
	fieldKeys := make(map[[2]int64]struct{}, len(req.FieldMappings))
	for _, mapping := range req.FieldMappings {
		if mapping.FieldID <= 0 || mapping.EntityAttributeID <= 0 || !validValue(mapping.MappingRole, "direct", "derived") {
			return conceptMappingInvalid()
		}
		key := [2]int64{mapping.FieldID, mapping.EntityAttributeID}
		if _, exists := fieldKeys[key]; exists {
			return conceptMappingInvalid()
		}
		fieldKeys[key] = struct{}{}
	}
	relationKeys := make(map[[2]int64]struct{}, len(req.RelationMappings))
	for _, mapping := range req.RelationMappings {
		if mapping.TableRelationID <= 0 || mapping.EntityRelationID <= 0 || !validValue(mapping.Orientation, "same", "inverse") {
			return conceptMappingInvalid()
		}
		key := [2]int64{mapping.TableRelationID, mapping.EntityRelationID}
		if _, exists := relationKeys[key]; exists {
			return conceptMappingInvalid()
		}
		relationKeys[key] = struct{}{}
	}
	return nil
}

func loadRequestedTableRelations(
	tx *gorm.DB,
	tableID, tenantID int64,
	inputs []models.RelationConceptMappingInput,
) (map[int64]models.TableRelation, error) {
	ids := uniqueRelationMappingIDs(inputs, func(input models.RelationConceptMappingInput) int64 {
		return input.TableRelationID
	})
	if len(ids) == 0 {
		return map[int64]models.TableRelation{}, nil
	}
	var relations []models.TableRelation
	if err := tx.Where("tenant_id = ? AND source_table = ? AND id IN ?", tenantID, tableID, ids).
		Order("id ASC").Find(&relations).Error; err != nil {
		return nil, err
	}
	if len(relations) != len(ids) {
		return nil, conceptMappingInvalid()
	}
	result := make(map[int64]models.TableRelation, len(relations))
	for _, relation := range relations {
		result[relation.ID] = relation
	}
	return result, nil
}

func lockRequestedEntityRelations(
	tx *gorm.DB,
	tenantID int64,
	inputs []models.RelationConceptMappingInput,
) (map[int64]*models.EntityRelation, error) {
	ids := uniqueRelationMappingIDs(inputs, func(input models.RelationConceptMappingInput) int64 {
		return input.EntityRelationID
	})
	result := make(map[int64]*models.EntityRelation, len(ids))
	for _, id := range ids {
		relation, err := repository.LockEntityRelation(tx, id, tenantID)
		if err != nil {
			return nil, conceptMappingInvalid()
		}
		result[id] = relation
	}
	return result, nil
}

func uniqueRelationMappingIDs(
	inputs []models.RelationConceptMappingInput,
	selectID func(models.RelationConceptMappingInput) int64,
) []int64 {
	seen := make(map[int64]struct{}, len(inputs))
	ids := make([]int64, 0, len(inputs))
	for _, input := range inputs {
		id := selectID(input)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func lockApprovedEntities(tx *gorm.DB, tenantID int64, ids ...int64) (map[int64]*models.Entity, error) {
	unique := make(map[int64]struct{}, len(ids))
	ordered := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, exists := unique[id]; exists {
			continue
		}
		unique[id] = struct{}{}
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	entities := make(map[int64]*models.Entity, len(ordered))
	for _, id := range ordered {
		entity, err := repository.LockEntity(tx, id, tenantID)
		if err != nil || entity.Status != "approved" {
			return nil, conceptMappingInvalid()
		}
		entities[id] = entity
	}
	return entities, nil
}

func buildTableEntityMappings(
	tableID, tenantID int64,
	inputs []models.TableEntityMappingInput,
	entities map[int64]*models.Entity,
) ([]models.LogicalTableEntityMapping, map[int64]struct{}) {
	mappings := make([]models.LogicalTableEntityMapping, 0, len(inputs))
	mappedEntities := make(map[int64]struct{}, len(inputs))
	for _, input := range inputs {
		entity := entities[input.EntityID]
		mappings = append(mappings, models.LogicalTableEntityMapping{
			TenantID:      tenantID,
			TableID:       tableID,
			EntityID:      input.EntityID,
			MappingRole:   input.MappingRole,
			EntityVersion: entity.Version,
		})
		mappedEntities[input.EntityID] = struct{}{}
	}
	return mappings, mappedEntities
}

func buildFieldAttributeMappings(
	tx *gorm.DB,
	tableID, tenantID int64,
	inputs []models.FieldAttributeMappingInput,
	mappedEntities map[int64]struct{},
) ([]models.LogicalFieldAttributeMapping, error) {
	if len(inputs) == 0 {
		return []models.LogicalFieldAttributeMapping{}, nil
	}
	fieldIDs := make([]int64, 0, len(inputs))
	attributeIDs := make([]int64, 0, len(inputs))
	for _, input := range inputs {
		fieldIDs = append(fieldIDs, input.FieldID)
		attributeIDs = append(attributeIDs, input.EntityAttributeID)
	}
	var fields []models.LogicalField
	if err := tx.Where("table_id = ? AND id IN ?", tableID, fieldIDs).Find(&fields).Error; err != nil {
		return nil, err
	}
	validFields := make(map[int64]struct{}, len(fields))
	for _, field := range fields {
		validFields[field.ID] = struct{}{}
	}
	var attributes []models.EntityAttribute
	if err := tx.Where("id IN ?", attributeIDs).Find(&attributes).Error; err != nil {
		return nil, err
	}
	attributesByID := make(map[int64]models.EntityAttribute, len(attributes))
	for _, attribute := range attributes {
		attributesByID[attribute.ID] = attribute
	}
	if len(validFields) != len(uniqueInt64s(fieldIDs)) || len(attributesByID) != len(uniqueInt64s(attributeIDs)) {
		return nil, conceptMappingInvalid()
	}
	mappings := make([]models.LogicalFieldAttributeMapping, 0, len(inputs))
	for _, input := range inputs {
		attribute := attributesByID[input.EntityAttributeID]
		if _, mapped := mappedEntities[attribute.EntityID]; !mapped {
			return nil, conceptMappingInvalid()
		}
		mappings = append(mappings, models.LogicalFieldAttributeMapping{
			TenantID:          tenantID,
			TableID:           tableID,
			FieldID:           input.FieldID,
			EntityID:          attribute.EntityID,
			EntityAttributeID: input.EntityAttributeID,
			MappingRole:       input.MappingRole,
		})
	}
	return mappings, nil
}

func buildRelationConceptMappings(
	tx *gorm.DB,
	tableID, tenantID int64,
	inputs []models.RelationConceptMappingInput,
	tableRelations map[int64]models.TableRelation,
	entityRelations map[int64]*models.EntityRelation,
	sourceEntities map[int64]struct{},
	entities map[int64]*models.Entity,
) ([]models.TableRelationEntityRelationMapping, error) {
	if len(inputs) == 0 {
		return []models.TableRelationEntityRelationMapping{}, nil
	}
	targetMappingsByTable := make(map[int64]map[int64]struct{})
	mappings := make([]models.TableRelationEntityRelationMapping, 0, len(inputs))
	for _, input := range inputs {
		tableRelation := tableRelations[input.TableRelationID]
		targetEntities, exists := targetMappingsByTable[tableRelation.TargetTable]
		if !exists {
			targetMappings, err := repository.NewConceptMappingRepository(tx).ListTableMappings(tableRelation.TargetTable, tenantID)
			if err != nil {
				return nil, err
			}
			targetEntities = make(map[int64]struct{}, len(targetMappings))
			for _, targetMapping := range targetMappings {
				entity, ok := entities[targetMapping.EntityID]
				if !ok {
					locked, err := repository.LockEntity(tx, targetMapping.EntityID, tenantID)
					if err != nil {
						return nil, conceptMappingInvalid()
					}
					entity = locked
					entities[targetMapping.EntityID] = entity
				}
				if entity.Status != "approved" || entity.Version != targetMapping.EntityVersion {
					return nil, apperrors.Conflict("concept_mapping_drift", i18n.MsgConceptMappingDrift)
				}
				targetEntities[targetMapping.EntityID] = struct{}{}
			}
			targetMappingsByTable[tableRelation.TargetTable] = targetEntities
		}

		entityRelation := entityRelations[input.EntityRelationID]
		sourceEntityID := entityRelation.SourceEntity
		targetEntityID := entityRelation.TargetEntity
		if input.Orientation == "inverse" {
			sourceEntityID, targetEntityID = targetEntityID, sourceEntityID
		}
		if _, ok := sourceEntities[sourceEntityID]; !ok {
			return nil, conceptMappingInvalid()
		}
		if _, ok := targetEntities[targetEntityID]; !ok {
			return nil, conceptMappingInvalid()
		}
		mappings = append(mappings, models.TableRelationEntityRelationMapping{
			TenantID:              tenantID,
			TableID:               tableID,
			TableRelationID:       input.TableRelationID,
			EntityRelationID:      input.EntityRelationID,
			Orientation:           input.Orientation,
			EntityRelationVersion: entityRelation.Version,
		})
	}
	return mappings, nil
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func conceptMappingInvalid() error {
	return apperrors.Validation("concept_mapping_invalid", i18n.MsgConceptMappingInvalid)
}

func validateCurrentConceptMappings(tx *gorm.DB, tableID, tenantID int64) error {
	var driftCount int64
	if err := tx.Table("model.logical_table_entity_mappings AS mapping").
		Joins("JOIN model.entities AS entity ON entity.id = mapping.entity_id AND entity.tenant_id = mapping.tenant_id").
		Where("mapping.table_id = ? AND mapping.tenant_id = ?", tableID, tenantID).
		Where("entity.status <> 'approved' OR entity.version <> mapping.entity_version").
		Count(&driftCount).Error; err != nil {
		return err
	}
	if driftCount > 0 {
		return apperrors.Conflict("concept_mapping_drift", i18n.MsgConceptMappingDrift)
	}
	if err := tx.Table("model.table_relation_entity_relation_mappings AS mapping").
		Joins("JOIN model.entity_relations AS relation ON relation.id = mapping.entity_relation_id AND relation.tenant_id = mapping.tenant_id").
		Joins("JOIN model.entities AS source_entity ON source_entity.id = relation.source_entity AND source_entity.tenant_id = relation.tenant_id").
		Joins("JOIN model.entities AS target_entity ON target_entity.id = relation.target_entity AND target_entity.tenant_id = relation.tenant_id").
		Where("mapping.table_id = ? AND mapping.tenant_id = ?", tableID, tenantID).
		Where("relation.version <> mapping.entity_relation_version OR source_entity.status <> 'approved' OR target_entity.status <> 'approved'").
		Count(&driftCount).Error; err != nil {
		return err
	}
	if driftCount > 0 {
		return apperrors.Conflict("concept_mapping_drift", i18n.MsgConceptMappingDrift)
	}
	return nil
}
