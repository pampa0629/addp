package service

import (
	"context"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

// MetricService 指标服务
type MetricService struct {
	catRepo    *repository.MetricCategoryRepository
	metricRepo *repository.MetricRepository
	refs       *repository.TenantReferenceRepository
	deletion   *StandardReferenceDeletionService
}

func NewMetricService(catRepo *repository.MetricCategoryRepository, metricRepo *repository.MetricRepository, refs *repository.TenantReferenceRepository, deletion *StandardReferenceDeletionService) *MetricService {
	return &MetricService{catRepo: catRepo, metricRepo: metricRepo, refs: refs, deletion: deletion}
}

// --- 指标目录 ---

func (s *MetricService) ListCategories(tenantID int64) ([]models.MetricCategory, error) {
	return s.catRepo.List(tenantID)
}

func (s *MetricService) CreateCategory(req *models.CreateMetricCategoryRequest, tenantID, userID int64) (*models.MetricCategory, error) {
	if err := s.refs.RequireMetricCategory(tenantID, req.ParentID); err != nil {
		return nil, err
	}
	exists, err := s.catRepo.ExistsByCode(req.Code, tenantID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
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
	if err := s.validateCategoryParent(id, tenantID, req.ParentID); err != nil {
		return nil, err
	}
	if req.Name != "" {
		c.Name = req.Name
	}
	c.Description = req.Description
	c.ParentID = req.ParentID
	c.SortOrder = req.SortOrder
	c.UpdatedBy = &userID
	if err := s.catRepo.Update(c, req.Version); err != nil {
		return nil, err
	}
	return s.catRepo.GetByID(id, tenantID)
}

func (s *MetricService) validateCategoryParent(id, tenantID int64, parentID *int64) error {
	if err := s.refs.RequireMetricCategory(tenantID, parentID); err != nil {
		return err
	}
	for current := parentID; current != nil; {
		if *current == id {
			return ErrMetricCategoryParentCycle
		}
		parent, err := s.catRepo.GetByID(*current, tenantID)
		if err != nil {
			return err
		}
		current = parent.ParentID
	}
	return nil
}

func (s *MetricService) DeleteCategory(id, tenantID int64) error {
	return mapDeleteConflict(s.catRepo.Delete(id, tenantID), ErrMetricCategoryReferenced)
}

// --- 指标定义 ---

func (s *MetricService) ListMetrics(tenantID int64, opts repository.ListMetricOptions) ([]models.Metric, int64, error) {
	return s.metricRepo.List(tenantID, opts)
}

func (s *MetricService) GetMetric(id, tenantID int64) (*models.Metric, error) {
	return s.metricRepo.GetByID(id, tenantID)
}

func (s *MetricService) CreateMetric(req *models.CreateMetricRequest, tenantID, userID int64) (*models.Metric, error) {
	if err := s.validateMetricReferences(tenantID, req.CategoryID, req.DomainID, req.UnitID, req.BaseMetricID, req.ElementIDs, req.DependencyIDs); err != nil {
		return nil, err
	}
	// 检查 code 唯一性
	exists, err := s.metricRepo.ExistsByCode(req.Code, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
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

	if err := s.metricRepo.CreateWithRelations(metric, req.ElementIDs, req.DependencyIDs); err != nil {
		return nil, err
	}

	return metric, nil
}

func (s *MetricService) UpdateMetric(id, tenantID, userID int64, req *models.UpdateMetricRequest) (*models.Metric, error) {
	metric, err := s.metricRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.validateMetricReferences(tenantID, req.CategoryID, req.DomainID, req.UnitID, req.BaseMetricID, req.ElementIDs, req.DependencyIDs); err != nil {
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

	if err := s.metricRepo.UpdateWithRelations(metric, req.ElementIDs, req.DependencyIDs, req.Version); err != nil {
		return nil, err
	}

	return s.metricRepo.GetByID(id, tenantID)
}

func (s *MetricService) DeleteMetric(ctx context.Context, id, tenantID int64) error {
	return s.deletion.Delete(ctx, tenantID, "metric", id, func() error {
		return mapDeleteConflict(s.metricRepo.Delete(id, tenantID), ErrMetricReferenced)
	})
}

func (s *MetricService) ApproveMetric(id, tenantID, userID, version int64) error {
	return s.metricRepo.UpdateStatus(id, tenantID, version, "approved", userID)
}

func (s *MetricService) DeprecateMetric(id, tenantID, userID, version int64) error {
	return s.metricRepo.UpdateStatus(id, tenantID, version, "deprecated", userID)
}

func (s *MetricService) GetElementMappings(metricID, tenantID int64) ([]models.MetricElementMapping, error) {
	return s.metricRepo.GetElementMappings(metricID, tenantID)
}

func (s *MetricService) GetDependencies(metricID, tenantID int64) ([]models.MetricDependency, error) {
	return s.metricRepo.GetDependencies(metricID, tenantID)
}

func (s *MetricService) validateMetricReferences(tenantID int64, categoryID, domainID, unitID, baseMetricID *int64, elementIDs, dependencyIDs []int64) error {
	for _, validate := range []func() error{
		func() error { return s.refs.RequireMetricCategory(tenantID, categoryID) },
		func() error { return s.refs.RequireDomain(tenantID, domainID) },
		func() error { return s.refs.RequireUnit(tenantID, unitID) },
		func() error { return s.refs.RequireMetric(tenantID, baseMetricID) },
		func() error { return s.refs.RequireElements(tenantID, elementIDs) },
		func() error { return s.refs.RequireMetrics(tenantID, dependencyIDs) },
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}
