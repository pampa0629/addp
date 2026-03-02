package service

import (
	"fmt"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

// MetricService 指标服务
type MetricService struct {
	catRepo    *repository.MetricCategoryRepository
	metricRepo *repository.MetricRepository
}

func NewMetricService(catRepo *repository.MetricCategoryRepository, metricRepo *repository.MetricRepository) *MetricService {
	return &MetricService{catRepo: catRepo, metricRepo: metricRepo}
}

// --- 指标目录 ---

func (s *MetricService) ListCategories(tenantID int64) ([]models.MetricCategory, error) {
	return s.catRepo.List(tenantID)
}

func (s *MetricService) CreateCategory(req *models.CreateMetricCategoryRequest, tenantID, userID int64) (*models.MetricCategory, error) {
	c := &models.MetricCategory{
		TenantID:    tenantID,
		Name:        req.Name,
		Code:        req.Code,
		Description: req.Description,
		ParentID:    req.ParentID,
		SortOrder:   req.SortOrder,
		CreatedBy:   userID,
	}
	if err := s.catRepo.Create(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *MetricService) UpdateCategory(id, tenantID, userID int64, req *models.UpdateMetricCategoryRequest) (*models.MetricCategory, error) {
	c, err := s.catRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		c.Name = req.Name
	}
	c.Description = req.Description
	c.ParentID = req.ParentID
	c.SortOrder = req.SortOrder
	c.UpdatedBy = &userID
	if err := s.catRepo.Update(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *MetricService) DeleteCategory(id, tenantID int64) error {
	return s.catRepo.Delete(id, tenantID)
}

// --- 指标定义 ---

func (s *MetricService) ListMetrics(tenantID int64, opts repository.ListMetricOptions) ([]models.Metric, int64, error) {
	return s.metricRepo.List(tenantID, opts)
}

func (s *MetricService) GetMetric(id, tenantID int64) (*models.Metric, error) {
	return s.metricRepo.GetByID(id, tenantID)
}

func (s *MetricService) CreateMetric(req *models.CreateMetricRequest, tenantID, userID int64) (*models.Metric, error) {
	// 检查 code 唯一性
	exists, err := s.metricRepo.ExistsByCode(req.Code, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("指标编码 '%s' 已存在", req.Code)
	}

	metric := &models.Metric{
		TenantID:         tenantID,
		CategoryID:       req.CategoryID,
		DomainID:         req.DomainID,
		Name:             req.Name,
		Code:             req.Code,
		Type:             req.Type,
		Definition:       req.Definition,
		Formula:          req.Formula,
		UnitID:           req.UnitID,
		BaseMetricID:     req.BaseMetricID,
		DerivationConfig: req.DerivationConfig,
		Status:           "draft",
		StewardID:        req.StewardID,
		Tags:             req.Tags,
		CreatedBy:        userID,
	}

	if err := s.metricRepo.Create(metric); err != nil {
		return nil, err
	}

	// 设置关联数据元（原子指标）
	if len(req.ElementIDs) > 0 {
		if err := s.metricRepo.SetElementMappings(metric.ID, req.ElementIDs); err != nil {
			return nil, err
		}
	}

	// 设置依赖指标（复合指标）
	if len(req.DependencyIDs) > 0 {
		if err := s.metricRepo.SetDependencies(metric.ID, req.DependencyIDs); err != nil {
			return nil, err
		}
	}

	return metric, nil
}

func (s *MetricService) UpdateMetric(id, tenantID, userID int64, req *models.UpdateMetricRequest) (*models.Metric, error) {
	metric, err := s.metricRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		metric.Name = req.Name
	}
	if req.Type != "" {
		metric.Type = req.Type
	}
	metric.CategoryID = req.CategoryID
	metric.DomainID = req.DomainID
	metric.Definition = req.Definition
	metric.Formula = req.Formula
	metric.UnitID = req.UnitID
	metric.BaseMetricID = req.BaseMetricID
	if req.DerivationConfig != nil {
		metric.DerivationConfig = req.DerivationConfig
	}
	metric.StewardID = req.StewardID
	if req.Tags != nil {
		metric.Tags = req.Tags
	}
	metric.UpdatedBy = &userID

	if err := s.metricRepo.Update(metric); err != nil {
		return nil, err
	}

	// 更新关联数据元
	if req.ElementIDs != nil {
		if err := s.metricRepo.SetElementMappings(metric.ID, req.ElementIDs); err != nil {
			return nil, err
		}
	}

	// 更新依赖指标
	if req.DependencyIDs != nil {
		if err := s.metricRepo.SetDependencies(metric.ID, req.DependencyIDs); err != nil {
			return nil, err
		}
	}

	return metric, nil
}

func (s *MetricService) DeleteMetric(id, tenantID int64) error {
	return s.metricRepo.Delete(id, tenantID)
}

func (s *MetricService) ApproveMetric(id, tenantID, userID int64) error {
	return s.metricRepo.UpdateStatus(id, tenantID, "approved", userID)
}

func (s *MetricService) DeprecateMetric(id, tenantID, userID int64) error {
	return s.metricRepo.UpdateStatus(id, tenantID, "deprecated", userID)
}

func (s *MetricService) GetElementMappings(metricID int64) ([]models.MetricElementMapping, error) {
	return s.metricRepo.GetElementMappings(metricID)
}

func (s *MetricService) GetDependencies(metricID int64) ([]models.MetricDependency, error) {
	return s.metricRepo.GetDependencies(metricID)
}
