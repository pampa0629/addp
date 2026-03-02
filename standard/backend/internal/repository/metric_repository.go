package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

// MetricCategoryRepository 指标目录仓库
type MetricCategoryRepository struct {
	db *gorm.DB
}

func NewMetricCategoryRepository(db *gorm.DB) *MetricCategoryRepository {
	return &MetricCategoryRepository{db: db}
}

func (r *MetricCategoryRepository) List(tenantID int64) ([]models.MetricCategory, error) {
	var list []models.MetricCategory
	err := r.db.Where("tenant_id = ?", tenantID).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

func (r *MetricCategoryRepository) GetByID(id, tenantID int64) (*models.MetricCategory, error) {
	var c models.MetricCategory
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&c).Error
	return &c, commonrepo.WrapDBError(err)
}

func (r *MetricCategoryRepository) Create(c *models.MetricCategory) error {
	return r.db.Create(c).Error
}

func (r *MetricCategoryRepository) Update(c *models.MetricCategory) error {
	return r.db.Save(c).Error
}

func (r *MetricCategoryRepository) Delete(id, tenantID int64) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.MetricCategory{}).Error
}

// MetricRepository 指标仓库
type MetricRepository struct {
	db *gorm.DB
}

func NewMetricRepository(db *gorm.DB) *MetricRepository {
	return &MetricRepository{db: db}
}

type ListMetricOptions struct {
	CategoryID *int64
	Type       string
	Status     string
	Keyword    string
	Page       int
	PageSize   int
}

func (r *MetricRepository) List(tenantID int64, opts ListMetricOptions) ([]models.Metric, int64, error) {
	query := r.db.Model(&models.Metric{}).Where("tenant_id = ?", tenantID)
	if opts.CategoryID != nil {
		query = query.Where("category_id = ?", *opts.CategoryID)
	}
	if opts.Type != "" {
		query = query.Where("type = ?", opts.Type)
	}
	if opts.Status != "" {
		query = query.Where("status = ?", opts.Status)
	}
	if opts.Keyword != "" {
		query = query.Where("name ILIKE ? OR code ILIKE ? OR definition ILIKE ?",
			"%"+opts.Keyword+"%", "%"+opts.Keyword+"%", "%"+opts.Keyword+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	offset := (opts.Page - 1) * opts.PageSize

	var metrics []models.Metric
	err := query.Order("created_at DESC").Offset(offset).Limit(opts.PageSize).Find(&metrics).Error
	return metrics, total, err
}

func (r *MetricRepository) GetByID(id, tenantID int64) (*models.Metric, error) {
	var m models.Metric
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&m).Error
	return &m, commonrepo.WrapDBError(err)
}

func (r *MetricRepository) Create(m *models.Metric) error {
	return r.db.Create(m).Error
}

func (r *MetricRepository) Update(m *models.Metric) error {
	return r.db.Save(m).Error
}

func (r *MetricRepository) Delete(id, tenantID int64) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Metric{}).Error
}

func (r *MetricRepository) UpdateStatus(id, tenantID int64, status string, updatedBy int64) error {
	return r.db.Model(&models.Metric{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{"status": status, "updated_by": updatedBy}).Error
}

func (r *MetricRepository) ExistsByCode(code string, tenantID int64, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.Metric{}).Where("code = ? AND tenant_id = ?", code, tenantID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}

// GetElementMappings 获取指标关联的数据元
func (r *MetricRepository) GetElementMappings(metricID int64) ([]models.MetricElementMapping, error) {
	var mappings []models.MetricElementMapping
	err := r.db.Where("metric_id = ?", metricID).Find(&mappings).Error
	return mappings, err
}

// SetElementMappings 设置指标关联数据元（全量替换）
func (r *MetricRepository) SetElementMappings(metricID int64, elementIDs []int64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("metric_id = ?", metricID).Delete(&models.MetricElementMapping{}).Error; err != nil {
			return err
		}
		for _, eid := range elementIDs {
			m := models.MetricElementMapping{MetricID: metricID, ElementID: eid}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetDependencies 获取复合指标依赖的指标列表
func (r *MetricRepository) GetDependencies(metricID int64) ([]models.MetricDependency, error) {
	var deps []models.MetricDependency
	err := r.db.Where("from_metric_id = ?", metricID).Find(&deps).Error
	return deps, err
}

// SetDependencies 设置复合指标依赖（全量替换）
func (r *MetricRepository) SetDependencies(metricID int64, depIDs []int64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("from_metric_id = ?", metricID).Delete(&models.MetricDependency{}).Error; err != nil {
			return err
		}
		for _, did := range depIDs {
			d := models.MetricDependency{FromMetricID: metricID, ToMetricID: did}
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
