package repository

import (
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
	return r.db.Create(glossary).Error
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

func (r *GlossaryRepository) Update(glossary *models.Glossary) error {
	return r.db.Save(glossary).Error
}

func (r *GlossaryRepository) Delete(id, tenantID int64) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Glossary{}).Error
}

func (r *GlossaryRepository) UpdateStatus(id, tenantID int64, status string, updatedBy int64) error {
	return r.db.Model(&models.Glossary{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{"status": status, "updated_by": updatedBy}).Error
}

// GetMappedElements 获取术语关联的完整数据元列表
func (r *GlossaryRepository) GetMappedElements(glossaryID, tenantID int64) ([]models.Element, error) {
	var elements []models.Element
	err := r.db.Raw(`
		SELECT e.* FROM standard.elements e
		INNER JOIN standard.glossary_element_mappings gem ON gem.element_id = e.id
		WHERE gem.glossary_id = ? AND e.tenant_id = ?
	`, glossaryID, tenantID).Scan(&elements).Error
	return elements, err
}

// SetElementMappings 批量替换术语的数据元映射（事务：先删全部，再插入）
func (r *GlossaryRepository) SetElementMappings(glossaryID, tenantID int64, elementIDs []int64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("glossary_id = ?", glossaryID).Delete(&models.GlossaryElementMapping{}).Error; err != nil {
			return err
		}
		if len(elementIDs) == 0 {
			return nil
		}
		mappings := make([]models.GlossaryElementMapping, len(elementIDs))
		for i, eid := range elementIDs {
			mappings[i] = models.GlossaryElementMapping{GlossaryID: glossaryID, ElementID: eid}
		}
		return tx.Create(&mappings).Error
	})
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
