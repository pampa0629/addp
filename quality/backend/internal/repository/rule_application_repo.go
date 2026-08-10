package repository

import (
	commonAPI "github.com/addp/common/api"
	commonRepository "github.com/addp/common/repository"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

type RuleApplicationRepository struct {
	db *gorm.DB
}

func NewRuleApplicationRepository(db *gorm.DB) *RuleApplicationRepository {
	return &RuleApplicationRepository{db: db}
}

type RuleApplicationListOptions struct {
	TenantID   int64
	EngineID   int64
	SchemaName string
	TableName  string
	Page       int
	PageSize   int
}

func (r *RuleApplicationRepository) List(opts RuleApplicationListOptions) ([]models.RuleApplication, int64, error) {
	var items []models.RuleApplication
	q := r.db.Where("tenant_id = ?", opts.TenantID)
	if opts.EngineID > 0 {
		q = q.Where("engine_id = ?", opts.EngineID)
	}
	if opts.SchemaName != "" {
		q = q.Where("schema_name = ?", opts.SchemaName)
	}
	if opts.TableName != "" {
		q = q.Where("table_name = ?", opts.TableName)
	}
	var total int64
	if err := q.Model(&models.RuleApplication{}).Count(&total).Error; err != nil {
		return nil, 0, commonRepository.WrapDBError(err)
	}
	page, pageSize := normalizePage(opts.Page, opts.PageSize)
	err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, commonRepository.WrapDBError(err)
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func (r *RuleApplicationRepository) Get(id, tenantID int64) (*models.RuleApplication, error) {
	var item models.RuleApplication
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	if err != nil {
		return nil, commonRepository.WrapDBError(err)
	}
	return &item, nil
}

func (r *RuleApplicationRepository) Create(item *models.RuleApplication) error {
	return commonRepository.WrapDBError(r.db.Create(item).Error)
}

func (r *RuleApplicationRepository) Update(item *models.RuleApplication) error {
	return commonRepository.WrapDBError(r.db.Save(item).Error)
}

func (r *RuleApplicationRepository) Delete(id, tenantID int64) error {
	result := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.RuleApplication{})
	if result.Error != nil {
		return commonRepository.WrapDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonAPI.ErrNotFound
	}
	return nil
}
