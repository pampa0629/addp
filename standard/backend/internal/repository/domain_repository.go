package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

type DomainRepository struct {
	db *gorm.DB
}

func NewDomainRepository(db *gorm.DB) *DomainRepository {
	return &DomainRepository{db: db}
}

func (r *DomainRepository) Create(domain *models.Domain) error {
	return wrapDBError(r.db.Create(domain).Error)
}

func (r *DomainRepository) GetByID(id, tenantID int64) (*models.Domain, error) {
	var domain models.Domain
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&domain).Error
	return &domain, commonrepo.WrapDBError(err)
}

func (r *DomainRepository) List(tenantID int64) ([]models.Domain, error) {
	var domains []models.Domain
	err := r.db.Where("tenant_id = ?", tenantID).
		Order("sort_order ASC, id ASC").
		Find(&domains).Error
	return domains, err
}

func (r *DomainRepository) Update(domain *models.Domain) error {
	return wrapDBError(r.db.Save(domain).Error)
}

func (r *DomainRepository) Delete(id, tenantID int64) error {
	return requireAffectedRow(r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Domain{}))
}

func (r *DomainRepository) ExistsByCode(code string, tenantID int64, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.Domain{}).Where("code = ? AND tenant_id = ?", code, tenantID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}
