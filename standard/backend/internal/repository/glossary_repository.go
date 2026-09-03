package repository

import (
	"time"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

type GlossaryRepository struct {
	db *gorm.DB
}

func NewGlossaryRepository(db *gorm.DB) *GlossaryRepository {
	return &GlossaryRepository{db: db}
}

func (r *GlossaryRepository) Create(glossary *models.Glossary) error {
	return wrapDBError(r.db.Create(glossary).Error)
}

func (r *GlossaryRepository) GetByID(id, tenantID int64) (*models.Glossary, error) {
	var glossary models.Glossary
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&glossary).Error
	return &glossary, commonrepo.WrapDBError(err)
}

type ListGlossaryOptions struct {
	DomainID *int64
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

func (r *GlossaryRepository) List(tenantID int64, opts ListGlossaryOptions) ([]models.Glossary, int64, error) {
	query := r.db.Model(&models.Glossary{}).Where("tenant_id = ?", tenantID)

	if opts.DomainID != nil {
		query = query.Where("domain_id = ?", *opts.DomainID)
	}
	if opts.Status != "" {
		query = query.Where("status = ?", opts.Status)
	}
	if opts.Keyword != "" {
		query = query.Where("name ILIKE ? OR definition ILIKE ?", "%"+opts.Keyword+"%", "%"+opts.Keyword+"%")
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

	var glossaries []models.Glossary
	err := query.Order("created_at DESC").Offset(offset).Limit(opts.PageSize).Find(&glossaries).Error
	return glossaries, total, err
}

func (r *GlossaryRepository) UpdateWithRelations(glossary *models.Glossary, elementIDs []int64, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, glossary, glossary.ID, glossary.TenantID, expectedVersion, map[string]interface{}{
			"domain_id": glossary.DomainID, "name": glossary.Name, "alias": glossary.Alias,
			"definition": glossary.Definition, "example": glossary.Example, "note": glossary.Note,
			"steward_id": glossary.StewardID, "tags": glossary.Tags, "updated_by": glossary.UpdatedBy,
		}); err != nil {
			return err
		}
		if elementIDs != nil {
			if err := tx.Where("glossary_id = ?", glossary.ID).Delete(&models.GlossaryElementMapping{}).Error; err != nil {
				return err
			}
			for _, elementID := range uniqueInt64s(elementIDs) {
				if err := tx.Create(&models.GlossaryElementMapping{GlossaryID: glossary.ID, ElementID: elementID}).Error; err != nil {
					return err
				}
			}
		}
		glossary.Version = expectedVersion + 1
		return nil
	}))
}

func (r *GlossaryRepository) Delete(id, tenantID int64) error {
	return deleteInTransaction(r.db, &models.Glossary{}, "id = ? AND tenant_id = ?", id, tenantID)
}

func (r *GlossaryRepository) UpdateStatus(id, tenantID, expectedVersion int64, status string, updatedBy int64) error {
	return updateVersioned(r.db, &models.Glossary{}, id, tenantID, expectedVersion, map[string]interface{}{
		"status": status, "updated_by": updatedBy,
	})
}

// GetMappedElements 获取术语关联的完整数据元列表
func (r *GlossaryRepository) GetMappedElements(glossaryID, tenantID int64) ([]models.PublishedElementReference, error) {
	var elements []models.PublishedElementReference
	asOf := time.Now().UTC()
	err := r.db.Raw(`
		SELECT e.id, e.tenant_id, e.scope_type, e.owner_domain_id, e.code, e.lifecycle_state, e.version,
			er.id AS revision_id, er.revision_no, er.name, er.status
		FROM standard.elements e
		INNER JOIN standard.glossary_element_mappings gem ON gem.element_id = e.id
		INNER JOIN standard.element_revisions er ON er.element_id = e.id
			AND er.status = 'published'
			AND er.effective_from <= ?
			AND (er.effective_to IS NULL OR er.effective_to > ?)
		WHERE gem.glossary_id = ? AND e.tenant_id = ? AND e.lifecycle_state = 'active'
	`, asOf, asOf, glossaryID, tenantID).Scan(&elements).Error
	return elements, err
}

// GetGlossariesByElementID 根据数据元ID反查关联的术语列表
func (r *GlossaryRepository) GetGlossariesByElementID(elementID, tenantID int64) ([]models.Glossary, error) {
	var glossaries []models.Glossary
	err := r.db.Raw(`
		SELECT g.* FROM standard.glossaries g
		INNER JOIN standard.glossary_element_mappings gem ON gem.glossary_id = g.id
		WHERE gem.element_id = ? AND g.tenant_id = ?
	`, elementID, tenantID).Scan(&glossaries).Error
	return glossaries, err
}
