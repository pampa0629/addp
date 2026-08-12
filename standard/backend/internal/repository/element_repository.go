package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

type ElementRepository struct {
	db *gorm.DB
}

func NewElementRepository(db *gorm.DB) *ElementRepository {
	return &ElementRepository{db: db}
}

func (r *ElementRepository) Create(element *models.Element) error {
	return wrapDBError(r.db.Create(element).Error)
}

func (r *ElementRepository) GetByID(id, tenantID int64) (*models.Element, error) {
	var element models.Element
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&element).Error
	return &element, commonrepo.WrapDBError(err)
}

type ListElementOptions struct {
	DomainID *int64
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

func (r *ElementRepository) List(tenantID int64, opts ListElementOptions) ([]models.Element, int64, error) {
	query := r.db.Model(&models.Element{}).Where("tenant_id = ?", tenantID)

	if opts.DomainID != nil {
		query = query.Where("domain_id = ?", *opts.DomainID)
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

	var elements []models.Element
	err := query.Order("created_at DESC").Offset(offset).Limit(opts.PageSize).Find(&elements).Error
	return elements, total, err
}

func (r *ElementRepository) Update(element *models.Element) error {
	return wrapDBError(r.db.Save(element).Error)
}

func (r *ElementRepository) Delete(id, tenantID int64) error {
	return deleteInTransaction(r.db, &models.Element{}, "id = ? AND tenant_id = ?", id, tenantID)
}

func (r *ElementRepository) UpdateStatus(id, tenantID int64, status string, updatedBy int64) error {
	return requireAffectedRow(r.db.Model(&models.Element{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{"status": status, "updated_by": updatedBy}))
}

func (r *ElementRepository) ExistsByCode(code string, tenantID int64, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.Element{}).Where("code = ? AND tenant_id = ?", code, tenantID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}
