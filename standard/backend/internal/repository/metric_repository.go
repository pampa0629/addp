package repository

import (
	"errors"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

var ErrMetricDependencyCycle = errors.New("metric dependency cycle")

const standardMetricDependencyLockBase int64 = 2026081100000000

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
	return wrapDBError(r.db.Create(c).Error)
}

func (r *MetricCategoryRepository) Update(c *models.MetricCategory) error {
	return wrapDBError(r.db.Save(c).Error)
}

func (r *MetricCategoryRepository) Delete(id, tenantID int64) error {
	return requireAffectedRow(r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.MetricCategory{}))
}

func (r *MetricCategoryRepository) ExistsByCode(code string, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Model(&models.MetricCategory{}).
		Where("code = ? AND tenant_id = ?", code, tenantID).
		Count(&count).Error
	return count > 0, err
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
	return wrapDBError(r.db.Create(m).Error)
}

func (r *MetricRepository) Update(m *models.Metric) error {
	return wrapDBError(r.db.Save(m).Error)
}

func (r *MetricRepository) Delete(id, tenantID int64) error {
	return requireAffectedRow(r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Metric{}))
}

func (r *MetricRepository) UpdateStatus(id, tenantID int64, status string, updatedBy int64) error {
	return requireAffectedRow(r.db.Model(&models.Metric{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{"status": status, "updated_by": updatedBy}))
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
func (r *MetricRepository) GetElementMappings(metricID, tenantID int64) ([]models.MetricElementMapping, error) {
	var mappings []models.MetricElementMapping
	err := r.db.Model(&models.MetricElementMapping{}).
		Joins("JOIN standard.elements e ON e.id = standard.metric_element_mappings.element_id AND e.tenant_id = ?", tenantID).
		Where("standard.metric_element_mappings.metric_id = ?", metricID).
		Find(&mappings).Error
	return mappings, err
}

// SetElementMappings 设置指标关联数据元（全量替换）
func (r *MetricRepository) SetElementMappings(metricID int64, elementIDs []int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
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
	}))
}

// GetDependencies 获取复合指标依赖的指标列表
func (r *MetricRepository) GetDependencies(metricID, tenantID int64) ([]models.MetricDependency, error) {
	var deps []models.MetricDependency
	err := r.db.Model(&models.MetricDependency{}).
		Joins("JOIN standard.metrics m ON m.id = standard.metric_dependencies.to_metric_id AND m.tenant_id = ?", tenantID).
		Where("standard.metric_dependencies.from_metric_id = ?", metricID).
		Find(&deps).Error
	return deps, err
}

// SetDependencies 设置复合指标依赖（全量替换）
func (r *MetricRepository) SetDependencies(metricID int64, depIDs []int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
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
	}))
}

func (r *MetricRepository) CreateWithRelations(metric *models.Metric, elementIDs, dependencyIDs []int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(metric).Error; err != nil {
			return err
		}
		if err := lockMetricDependencies(tx, metric.TenantID); err != nil {
			return err
		}
		if cycle, err := metricDependencyCycle(tx, metric.ID, metric.TenantID, dependencyIDs); err != nil {
			return err
		} else if cycle {
			return ErrMetricDependencyCycle
		}
		if err := replaceMetricElements(tx, metric.ID, elementIDs); err != nil {
			return err
		}
		return replaceMetricDependencies(tx, metric.ID, dependencyIDs)
	}))
}

func (r *MetricRepository) UpdateWithRelations(metric *models.Metric, elementIDs, dependencyIDs []int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if dependencyIDs != nil {
			if err := lockMetricDependencies(tx, metric.TenantID); err != nil {
				return err
			}
			cycle, err := metricDependencyCycle(tx, metric.ID, metric.TenantID, dependencyIDs)
			if err != nil {
				return err
			}
			if cycle {
				return ErrMetricDependencyCycle
			}
		}
		if err := tx.Save(metric).Error; err != nil {
			return err
		}
		if elementIDs != nil {
			if err := replaceMetricElements(tx, metric.ID, elementIDs); err != nil {
				return err
			}
		}
		if dependencyIDs != nil {
			return replaceMetricDependencies(tx, metric.ID, dependencyIDs)
		}
		return nil
	}))
}

func replaceMetricElements(tx *gorm.DB, metricID int64, elementIDs []int64) error {
	if err := tx.Where("metric_id = ?", metricID).Delete(&models.MetricElementMapping{}).Error; err != nil {
		return err
	}
	for _, elementID := range uniqueInt64s(elementIDs) {
		if err := tx.Create(&models.MetricElementMapping{MetricID: metricID, ElementID: elementID}).Error; err != nil {
			return err
		}
	}
	return nil
}

func replaceMetricDependencies(tx *gorm.DB, metricID int64, dependencyIDs []int64) error {
	if err := tx.Where("from_metric_id = ?", metricID).Delete(&models.MetricDependency{}).Error; err != nil {
		return err
	}
	for _, dependencyID := range uniqueInt64s(dependencyIDs) {
		if err := tx.Create(&models.MetricDependency{FromMetricID: metricID, ToMetricID: dependencyID}).Error; err != nil {
			return err
		}
	}
	return nil
}

func lockMetricDependencies(db *gorm.DB, tenantID int64) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	return db.Exec("SELECT pg_advisory_xact_lock(?)", standardMetricDependencyLockBase+tenantID).Error
}

func metricDependencyCycle(db *gorm.DB, metricID, tenantID int64, dependencyIDs []int64) (bool, error) {
	type edge struct {
		FromMetricID int64 `gorm:"column:from_metric_id"`
		ToMetricID   int64 `gorm:"column:to_metric_id"`
	}

	var edges []edge
	err := db.Model(&models.MetricDependency{}).
		Select("standard.metric_dependencies.from_metric_id, standard.metric_dependencies.to_metric_id").
		Joins("JOIN standard.metrics source ON source.id = standard.metric_dependencies.from_metric_id AND source.tenant_id = ?", tenantID).
		Joins("JOIN standard.metrics target ON target.id = standard.metric_dependencies.to_metric_id AND target.tenant_id = ?", tenantID).
		Where("standard.metric_dependencies.from_metric_id <> ?", metricID).
		Find(&edges).Error
	if err != nil {
		return false, err
	}

	graph := make(map[int64][]int64, len(edges)+1)
	for _, item := range edges {
		graph[item.FromMetricID] = append(graph[item.FromMetricID], item.ToMetricID)
	}
	graph[metricID] = uniqueInt64s(dependencyIDs)

	visiting := make(map[int64]bool)
	visited := make(map[int64]bool)
	var visit func(int64) bool
	visit = func(current int64) bool {
		if visiting[current] {
			return true
		}
		if visited[current] {
			return false
		}
		visiting[current] = true
		for _, next := range graph[current] {
			if visit(next) {
				return true
			}
		}
		visiting[current] = false
		visited[current] = true
		return false
	}
	return visit(metricID), nil
}
