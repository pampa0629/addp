package service

import (
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

type TableRelationService struct {
	repo      *repository.TableRelationRepository
	tableRepo *repository.LogicalTableRepository
}

func (s *TableRelationService) requireDraftTables(tenantID, factTableID, dimensionTableID int64) (*models.LogicalTable, *models.LogicalTable, error) {
	factTable, err := s.tableRepo.GetByID(factTableID, tenantID)
	if err != nil {
		return nil, nil, apperrors.NotFound("fact_table_not_found", i18n.MsgTableRelationTargetNotFound)
	}
	dimensionTable, err := s.tableRepo.GetByID(dimensionTableID, tenantID)
	if err != nil {
		return nil, nil, apperrors.NotFound("dimension_table_not_found", i18n.MsgTableRelationTargetNotFound)
	}
	if factTable.Status != "draft" || dimensionTable.Status != "draft" {
		return nil, nil, apperrors.Conflict("table_relation_state_conflict", i18n.MsgTableRelationStateConflict)
	}
	return factTable, dimensionTable, nil
}

func NewTableRelationService(repo *repository.TableRelationRepository, tableRepo *repository.LogicalTableRepository) *TableRelationService {
	return &TableRelationService{repo: repo, tableRepo: tableRepo}
}

// ListDimensionRelations 获取事实表关联的维度表列表（含字段信息）
func (s *TableRelationService) ListDimensionRelations(factTableID, tenantID int64) ([]repository.TableRelationDetail, error) {
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
func (s *TableRelationService) AddDimensionRelation(factTableID, tenantID int64, req *models.CreateTableRelationRequest) (*models.TableRelation, error) {
	if err := validateCreateTableRelationRequest(req); err != nil {
		return nil, err
	}
	factTable, dimensionTable, err := s.requireDraftTables(tenantID, factTableID, req.TargetTable)
	if err != nil {
		return nil, err
	}
	if factTable.TableType != "fact" {
		return nil, apperrors.Validation("fact_table_required", i18n.MsgValidationFailed)
	}
	if dimensionTable.TableType != "dimension" {
		return nil, apperrors.Validation("dimension_table_required", i18n.MsgValidationFailed)
	}
	sourceField, err := s.tableRepo.GetFieldByID(req.SourceField, factTableID)
	if err != nil {
		return nil, apperrors.NotFound("source_field_not_found", i18n.MsgTableRelationTargetNotFound)
	}
	targetField, err := s.tableRepo.GetFieldByID(req.TargetField, req.TargetTable)
	if err != nil {
		return nil, apperrors.NotFound("target_field_not_found", i18n.MsgTableRelationTargetNotFound)
	}
	relationType := req.RelationType
	if relationType == "" {
		relationType = "fk"
	}
	if relationType == "fk" && !targetField.IsPK {
		return nil, apperrors.Validation("table_relation_invalid", i18n.MsgValidationFailed)
	}
	if sourceField.DataType != targetField.DataType {
		return nil, apperrors.Validation("table_relation_invalid", i18n.MsgValidationFailed)
	}

	exists, err := s.repo.Exists(factTableID, req.SourceField, req.TargetTable, req.TargetField, tenantID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperrors.Conflict("table_relation_conflict", i18n.MsgTableRelationConflict)
	}

	rel := &models.TableRelation{
		TenantID:     tenantID,
		SourceTable:  factTableID,
		SourceField:  req.SourceField,
		TargetTable:  req.TargetTable,
		TargetField:  req.TargetField,
		RelationType: relationType,
	}
	if err := s.repo.Create(rel); err != nil {
		return nil, modelResourceError(err, "table_relation", i18n.MsgTableRelationConflict)
	}
	return rel, nil
}

// RemoveDimensionRelation 移除关联
func (s *TableRelationService) RemoveDimensionRelation(relationID, factTableID, tenantID int64) error {
	relation, err := s.repo.GetByID(relationID, factTableID, tenantID)
	if err != nil {
		return apperrors.NotFound("table_relation_not_found", i18n.MsgInvalidRelationID)
	}
	if _, _, err := s.requireDraftTables(tenantID, factTableID, relation.TargetTable); err != nil {
		return err
	}
	if err := s.repo.Delete(relationID, factTableID, tenantID); err != nil {
		return modelResourceError(err, "table_relation_not_found", i18n.MsgInvalidRelationID)
	}
	return nil
}
