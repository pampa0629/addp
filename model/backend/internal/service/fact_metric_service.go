package service

import (
	"fmt"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

type FactMetricService struct {
	factMetricRepo *repository.FactMetricRepository
	tableRepo      *repository.LogicalTableRepository
}

func NewFactMetricService(factMetricRepo *repository.FactMetricRepository, tableRepo *repository.LogicalTableRepository) *FactMetricService {
	return &FactMetricService{
		factMetricRepo: factMetricRepo,
		tableRepo:      tableRepo,
	}
}

// ListMetrics 获取事实表关联的指标列表
func (s *FactMetricService) ListMetrics(factTableID, tenantID int64) ([]models.FactMetricMapping, error) {
	// 验证事实表存在且属于该租户
	if _, err := s.tableRepo.GetByID(factTableID, tenantID); err != nil {
		return nil, fmt.Errorf("逻辑表不存在")
	}
	return s.factMetricRepo.ListByFactTable(factTableID, tenantID)
}

// AddMetric 为事实表添加指标关联
func (s *FactMetricService) AddMetric(factTableID, tenantID, userID int64, req *models.CreateFactMetricMappingRequest) (*models.FactMetricMapping, error) {
	// 验证事实表存在且属于该租户
	if _, err := s.tableRepo.GetByID(factTableID, tenantID); err != nil {
		return nil, fmt.Errorf("逻辑表不存在")
	}

	// 防止重复关联
	exists, err := s.factMetricRepo.Exists(factTableID, req.MetricID, tenantID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("该指标已关联到此事实表")
	}

	m := &models.FactMetricMapping{
		TenantID:    tenantID,
		FactTableID: factTableID,
		MetricID:    req.MetricID,
		FieldID:     req.FieldID,
		Note:        req.Note,
		CreatedBy:   userID,
	}
	if err := s.factMetricRepo.Create(m); err != nil {
		return nil, err
	}
	return m, nil
}

// RemoveMetric 移除事实表的指标关联
func (s *FactMetricService) RemoveMetric(mappingID, factTableID, tenantID int64) error {
	return s.factMetricRepo.Delete(mappingID, factTableID, tenantID)
}
