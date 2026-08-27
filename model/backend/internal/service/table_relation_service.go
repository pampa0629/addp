package service

import (
	"sort"

	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

type TableRelationService struct {
	repo           *repository.TableRelationRepository
	tableRepo      *repository.LogicalTableRepository
	entityRepo     *repository.EntityRepository
	factMetricRepo *repository.FactMetricRepository
}

func (s *TableRelationService) SetProfessionalRelationSources(entityRepo *repository.EntityRepository, factMetricRepo *repository.FactMetricRepository) {
	s.entityRepo = entityRepo
	s.factMetricRepo = factMetricRepo
}

func lockLogicalTables(tx *gorm.DB, tenantID int64, ids ...int64) (map[int64]*models.LogicalTable, error) {
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
	tables := make(map[int64]*models.LogicalTable, len(ordered))
	for _, id := range ordered {
		table, err := repository.LockLogicalTable(tx, id, tenantID)
		if err != nil {
			return nil, apperrors.NotFound("logical_table_not_found", i18n.MsgTableRelationTargetNotFound)
		}
		tables[id] = table
	}
	return tables, nil
}

func NewTableRelationService(repo *repository.TableRelationRepository, tableRepo *repository.LogicalTableRepository) *TableRelationService {
	return &TableRelationService{repo: repo, tableRepo: tableRepo}
}

// ListDimensionRelations 获取事实表关联的维度表列表（含字段信息）
func (s *TableRelationService) ListDimensionRelations(factTableID, tenantID int64) ([]models.TableRelationDetail, error) {
	table, err := s.tableRepo.GetByID(factTableID, tenantID)
	if err != nil {
		return nil, apperrors.NotFound("logical_table_not_found", i18n.MsgTableNotFound)
	}
	if table.TableType != "fact" {
		return nil, apperrors.Validation("fact_table_required", i18n.MsgValidationFailed)
	}
	return s.repo.ListByFactTable(factTableID, tenantID)
}

// AddDimensionRelation 为事实表添加维度表关联
func (s *TableRelationService) AddDimensionRelation(factTableID, tenantID int64, req *models.CreateTableRelationRequest) (*models.TableRelationMutationResponse, error) {
	if err := validateCreateTableRelationRequest(req); err != nil {
		return nil, err
	}
	relationType := req.RelationType
	if relationType == "" {
		relationType = "fk"
	}
	rel := models.TableRelation{
		TenantID:     tenantID,
		SourceTable:  factTableID,
		SourceField:  req.SourceField,
		TargetTable:  req.TargetTable,
		TargetField:  req.TargetField,
		RelationType: relationType,
	}
	response := &models.TableRelationMutationResponse{Relation: rel}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		tables, err := lockLogicalTables(tx, tenantID, factTableID, req.TargetTable)
		if err != nil {
			return err
		}
		factTable := tables[factTableID]
		dimensionTable := tables[req.TargetTable]
		if err := requireVersion(factTable.Version, req.Version); err != nil {
			return err
		}
		if factTable.Status != "draft" || dimensionTable.Status != "draft" {
			return apperrors.Conflict("table_relation_state_conflict", i18n.MsgTableRelationStateConflict)
		}
		if factTable.TableType != "fact" || dimensionTable.TableType != "dimension" {
			return apperrors.Validation("table_relation_invalid", i18n.MsgValidationFailed)
		}
		tableRepo := repository.NewLogicalTableRepository(tx)
		sourceField, err := tableRepo.GetFieldByID(req.SourceField, factTableID)
		if err != nil {
			return apperrors.NotFound("source_field_not_found", i18n.MsgTableRelationTargetNotFound)
		}
		targetField, err := tableRepo.GetFieldByID(req.TargetField, req.TargetTable)
		if err != nil {
			return apperrors.NotFound("target_field_not_found", i18n.MsgTableRelationTargetNotFound)
		}
		if relationType == "fk" && !targetField.IsPK || sourceField.DataType != targetField.DataType {
			return apperrors.Validation("table_relation_invalid", i18n.MsgValidationFailed)
		}
		txRepo := repository.NewTableRelationRepository(tx)
		exists, err := txRepo.Exists(factTableID, req.SourceField, req.TargetTable, req.TargetField, tenantID)
		if err != nil {
			return err
		}
		if exists {
			return apperrors.Conflict("table_relation_conflict", i18n.MsgTableRelationConflict)
		}
		if err := txRepo.Create(&response.Relation); err != nil {
			return modelResourceError(err, "table_relation", i18n.MsgTableRelationConflict)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, factTableID, tenantID, req.Version)
		return err
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

// RemoveDimensionRelation 移除关联
func (s *TableRelationService) RemoveDimensionRelation(relationID, factTableID, tenantID, version int64) (*models.VersionResponse, error) {
	response := &models.VersionResponse{}
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewTableRelationRepository(tx)
		relation, err := txRepo.GetByID(relationID, factTableID, tenantID)
		if err != nil {
			return apperrors.NotFound("table_relation_not_found", i18n.MsgInvalidRelationID)
		}
		tables, err := lockLogicalTables(tx, tenantID, factTableID, relation.TargetTable)
		if err != nil {
			return err
		}
		if err := requireVersion(tables[factTableID].Version, version); err != nil {
			return err
		}
		if tables[factTableID].Status != "draft" || tables[relation.TargetTable].Status != "draft" {
			return apperrors.Conflict("table_relation_state_conflict", i18n.MsgTableRelationStateConflict)
		}
		if err := txRepo.Delete(relationID, factTableID, tenantID); err != nil {
			return modelResourceError(err, "table_relation_not_found", i18n.MsgInvalidRelationID)
		}
		response.Version, err = repository.AdvanceLogicalTableVersion(tx, factTableID, tenantID, version)
		return err
	})
	return response, err
}
