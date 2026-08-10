package service

import (
	"fmt"

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
		return nil, nil, fmt.Errorf("事实表不存在")
	}
	dimensionTable, err := s.tableRepo.GetByID(dimensionTableID, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("维度表不存在")
	}
	if factTable.Status != "draft" || dimensionTable.Status != "draft" {
		return nil, nil, fmt.Errorf("表关系两端都必须处于草稿状态")
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
		return nil, fmt.Errorf("逻辑表不存在")
	}
	if table.TableType != "fact" {
		return nil, fmt.Errorf("源表必须是事实表")
	}
	return s.repo.ListByFactTable(factTableID, tenantID)
}

// AddDimensionRelation 为事实表添加维度表关联
func (s *TableRelationService) AddDimensionRelation(factTableID, tenantID int64, req *models.CreateTableRelationRequest) (*models.TableRelation, error) {
	factTable, dimensionTable, err := s.requireDraftTables(tenantID, factTableID, req.TargetTable)
	if err != nil {
		return nil, err
	}
	if factTable.TableType != "fact" {
		return nil, fmt.Errorf("源表必须是事实表")
	}
	if dimensionTable.TableType != "dimension" {
		return nil, fmt.Errorf("目标表必须是维度表")
	}
	sourceField, err := s.tableRepo.GetFieldByID(req.SourceField, factTableID)
	if err != nil {
		return nil, fmt.Errorf("事实表字段不存在")
	}
	targetField, err := s.tableRepo.GetFieldByID(req.TargetField, req.TargetTable)
	if err != nil {
		return nil, fmt.Errorf("维度表字段不存在")
	}
	relationType := req.RelationType
	if relationType == "" {
		relationType = "fk"
	}
	if relationType == "fk" && !targetField.IsPK {
		return nil, fmt.Errorf("外键关系的目标字段必须是主键")
	}
	if sourceField.DataType != targetField.DataType {
		return nil, fmt.Errorf("关联字段数据类型必须一致")
	}

	exists, err := s.repo.Exists(factTableID, req.SourceField, req.TargetTable, req.TargetField, tenantID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("该字段关联已存在")
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
		return nil, err
	}
	return rel, nil
}

// RemoveDimensionRelation 移除关联
func (s *TableRelationService) RemoveDimensionRelation(relationID, factTableID, tenantID int64) error {
	relation, err := s.repo.GetByID(relationID, factTableID, tenantID)
	if err != nil {
		return fmt.Errorf("表关系不存在")
	}
	if _, _, err := s.requireDraftTables(tenantID, factTableID, relation.TargetTable); err != nil {
		return err
	}
	return s.repo.Delete(relationID, factTableID, tenantID)
}
