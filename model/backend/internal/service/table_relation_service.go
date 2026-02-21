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

func NewTableRelationService(repo *repository.TableRelationRepository, tableRepo *repository.LogicalTableRepository) *TableRelationService {
	return &TableRelationService{repo: repo, tableRepo: tableRepo}
}

// ListDimensionRelations 获取事实表关联的维度表列表（含字段信息）
func (s *TableRelationService) ListDimensionRelations(factTableID, tenantID int64) ([]repository.TableRelationDetail, error) {
	if _, err := s.tableRepo.GetByID(factTableID, tenantID); err != nil {
		return nil, fmt.Errorf("逻辑表不存在")
	}
	return s.repo.ListByFactTable(factTableID, tenantID)
}

// AddDimensionRelation 为事实表添加维度表关联
func (s *TableRelationService) AddDimensionRelation(factTableID, tenantID int64, req *models.CreateTableRelationRequest) (*models.TableRelation, error) {
	if _, err := s.tableRepo.GetByID(factTableID, tenantID); err != nil {
		return nil, fmt.Errorf("事实表不存在")
	}
	if _, err := s.tableRepo.GetByID(req.TargetTable, tenantID); err != nil {
		return nil, fmt.Errorf("维度表不存在")
	}

	exists, err := s.repo.Exists(factTableID, req.SourceField, req.TargetTable, req.TargetField, tenantID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("该字段关联已存在")
	}

	relationType := req.RelationType
	if relationType == "" {
		relationType = "fk"
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
	return s.repo.Delete(relationID, factTableID, tenantID)
}
