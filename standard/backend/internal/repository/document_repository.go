package repository

import (
	"fmt"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

// DocumentRepository 标准文档仓库
type DocumentRepository struct {
	db *gorm.DB
}

func NewDocumentRepository(db *gorm.DB) *DocumentRepository {
	return &DocumentRepository{db: db}
}

type ListDocumentOptions struct {
	DocType  string
	Keyword  string
	Page     int
	PageSize int
}

func (r *DocumentRepository) List(tenantID int64, opts ListDocumentOptions) ([]models.Document, int64, error) {
	query := r.db.Model(&models.Document{}).Where("tenant_id = ?", tenantID)
	if opts.DocType != "" {
		query = query.Where("doc_type = ?", opts.DocType)
	}
	if opts.Keyword != "" {
		query = query.Where("name ILIKE ? OR source_org ILIKE ?",
			"%"+opts.Keyword+"%", "%"+opts.Keyword+"%")
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

	var docs []models.Document
	err := query.Order("created_at DESC").Offset(offset).Limit(opts.PageSize).Find(&docs).Error
	return docs, total, err
}

func (r *DocumentRepository) GetByID(id, tenantID int64) (*models.Document, error) {
	var doc models.Document
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&doc).Error
	return &doc, commonrepo.WrapDBError(err)
}

func (r *DocumentRepository) Create(doc *models.Document) error {
	return r.db.Create(doc).Error
}

func (r *DocumentRepository) Update(doc *models.Document) error {
	return r.db.Save(doc).Error
}

func (r *DocumentRepository) Delete(id, tenantID int64) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Document{}).Error
}

// GetElementMappings 获取文档关联的数据元
func (r *DocumentRepository) GetElementMappings(docID, tenantID int64) ([]models.DocumentElementMapping, error) {
	var mappings []models.DocumentElementMapping
	err := r.db.Model(&models.DocumentElementMapping{}).
		Select("standard.document_element_mappings.*, e.name").
		Joins("JOIN standard.elements e ON e.id = standard.document_element_mappings.element_id AND e.tenant_id = ?", tenantID).
		Where("standard.document_element_mappings.document_id = ?", docID).
		Find(&mappings).Error
	return mappings, err
}

// GetGlossaryMappings 获取文档关联的术语
func (r *DocumentRepository) GetGlossaryMappings(docID, tenantID int64) ([]models.DocumentGlossaryMapping, error) {
	var mappings []models.DocumentGlossaryMapping
	err := r.db.Model(&models.DocumentGlossaryMapping{}).
		Select("standard.document_glossary_mappings.*, g.name").
		Joins("JOIN standard.glossaries g ON g.id = standard.document_glossary_mappings.glossary_id AND g.tenant_id = ?", tenantID).
		Where("standard.document_glossary_mappings.document_id = ?", docID).
		Find(&mappings).Error
	return mappings, err
}

// GetMetricMappings 获取文档关联的指标
func (r *DocumentRepository) GetMetricMappings(docID, tenantID int64) ([]models.DocumentMetricMapping, error) {
	var mappings []models.DocumentMetricMapping
	err := r.db.Model(&models.DocumentMetricMapping{}).
		Select("standard.document_metric_mappings.*, m.name").
		Joins("JOIN standard.metrics m ON m.id = standard.document_metric_mappings.metric_id AND m.tenant_id = ?", tenantID).
		Where("standard.document_metric_mappings.document_id = ?", docID).
		Find(&mappings).Error
	return mappings, err
}

// SetElementMappings 设置文档关联数据元（全量替换）
func (r *DocumentRepository) SetElementMappings(docID int64, elementIDs []int64, locations map[string]string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", docID).Delete(&models.DocumentElementMapping{}).Error; err != nil {
			return err
		}
		for _, eid := range elementIDs {
			key := fmt.Sprintf("element_%d", eid)
			m := models.DocumentElementMapping{
				DocumentID:        docID,
				ElementID:         eid,
				ReferenceLocation: locations[key],
			}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// SetGlossaryMappings 设置文档关联术语（全量替换）
func (r *DocumentRepository) SetGlossaryMappings(docID int64, glossaryIDs []int64, locations map[string]string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", docID).Delete(&models.DocumentGlossaryMapping{}).Error; err != nil {
			return err
		}
		for _, gid := range glossaryIDs {
			key := fmt.Sprintf("glossary_%d", gid)
			m := models.DocumentGlossaryMapping{
				DocumentID:        docID,
				GlossaryID:        gid,
				ReferenceLocation: locations[key],
			}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// SetMetricMappings 设置文档关联指标（全量替换）
func (r *DocumentRepository) SetMetricMappings(docID int64, metricIDs []int64, locations map[string]string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", docID).Delete(&models.DocumentMetricMapping{}).Error; err != nil {
			return err
		}
		for _, mid := range metricIDs {
			key := fmt.Sprintf("metric_%d", mid)
			m := models.DocumentMetricMapping{
				DocumentID:        docID,
				MetricID:          mid,
				ReferenceLocation: locations[key],
			}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *DocumentRepository) SetMappings(docID int64, elementIDs, glossaryIDs, metricIDs []int64, locations map[string]string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, model := range []interface{}{
			&models.DocumentElementMapping{},
			&models.DocumentGlossaryMapping{},
			&models.DocumentMetricMapping{},
		} {
			if err := tx.Where("document_id = ?", docID).Delete(model).Error; err != nil {
				return err
			}
		}
		for _, elementID := range elementIDs {
			mapping := models.DocumentElementMapping{DocumentID: docID, ElementID: elementID, ReferenceLocation: locations[fmt.Sprintf("element_%d", elementID)]}
			if err := tx.Create(&mapping).Error; err != nil {
				return err
			}
		}
		for _, glossaryID := range glossaryIDs {
			mapping := models.DocumentGlossaryMapping{DocumentID: docID, GlossaryID: glossaryID, ReferenceLocation: locations[fmt.Sprintf("glossary_%d", glossaryID)]}
			if err := tx.Create(&mapping).Error; err != nil {
				return err
			}
		}
		for _, metricID := range metricIDs {
			mapping := models.DocumentMetricMapping{DocumentID: docID, MetricID: metricID, ReferenceLocation: locations[fmt.Sprintf("metric_%d", metricID)]}
			if err := tx.Create(&mapping).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *DocumentRepository) CreateWithMapping(doc *models.Document, mapping interface{}) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(doc).Error; err != nil {
			return err
		}
		switch value := mapping.(type) {
		case *models.DocumentElementMapping:
			value.DocumentID = doc.ID
		case *models.DocumentGlossaryMapping:
			value.DocumentID = doc.ID
		case *models.DocumentMetricMapping:
			value.DocumentID = doc.ID
		default:
			return fmt.Errorf("unsupported document mapping type %T", mapping)
		}
		return tx.Create(mapping).Error
	})
}

// ===== 反向查询：按标准项 ID 查询关联文档 =====

// ListByElementID 查询某数据元关联的所有文档
func (r *DocumentRepository) ListByElementID(tenantID, elementID int64) ([]models.Document, error) {
	var docs []models.Document
	err := r.db.
		Joins("JOIN standard.document_element_mappings dem ON dem.document_id = standard.documents.id").
		Where("standard.documents.tenant_id = ? AND dem.element_id = ?", tenantID, elementID).
		Order("standard.documents.created_at DESC").
		Find(&docs).Error
	return docs, err
}

// ListByGlossaryID 查询某业务术语关联的所有文档
func (r *DocumentRepository) ListByGlossaryID(tenantID, glossaryID int64) ([]models.Document, error) {
	var docs []models.Document
	err := r.db.
		Joins("JOIN standard.document_glossary_mappings dgm ON dgm.document_id = standard.documents.id").
		Where("standard.documents.tenant_id = ? AND dgm.glossary_id = ?", tenantID, glossaryID).
		Order("standard.documents.created_at DESC").
		Find(&docs).Error
	return docs, err
}

// ListByMetricID 查询某指标关联的所有文档
func (r *DocumentRepository) ListByMetricID(tenantID, metricID int64) ([]models.Document, error) {
	var docs []models.Document
	err := r.db.
		Joins("JOIN standard.document_metric_mappings dmm ON dmm.document_id = standard.documents.id").
		Where("standard.documents.tenant_id = ? AND dmm.metric_id = ?", tenantID, metricID).
		Order("standard.documents.created_at DESC").
		Find(&docs).Error
	return docs, err
}

// ===== 增量关联 / 解关联（单条操作，与全量替换互补） =====

// AddElementMapping 增量添加文档与数据元的关联
func (r *DocumentRepository) AddElementMapping(docID, elementID int64) error {
	m := models.DocumentElementMapping{DocumentID: docID, ElementID: elementID}
	return r.db.Where("document_id = ? AND element_id = ?", docID, elementID).
		FirstOrCreate(&m).Error
}

// RemoveElementMapping 解除文档与数据元的关联
func (r *DocumentRepository) RemoveElementMapping(docID, elementID int64) error {
	return r.db.Where("document_id = ? AND element_id = ?", docID, elementID).
		Delete(&models.DocumentElementMapping{}).Error
}

// AddGlossaryMapping 增量添加文档与业务术语的关联
func (r *DocumentRepository) AddGlossaryMapping(docID, glossaryID int64) error {
	m := models.DocumentGlossaryMapping{DocumentID: docID, GlossaryID: glossaryID}
	return r.db.Where("document_id = ? AND glossary_id = ?", docID, glossaryID).
		FirstOrCreate(&m).Error
}

// RemoveGlossaryMapping 解除文档与业务术语的关联
func (r *DocumentRepository) RemoveGlossaryMapping(docID, glossaryID int64) error {
	return r.db.Where("document_id = ? AND glossary_id = ?", docID, glossaryID).
		Delete(&models.DocumentGlossaryMapping{}).Error
}

// AddMetricMapping 增量添加文档与指标的关联
func (r *DocumentRepository) AddMetricMapping(docID, metricID int64) error {
	m := models.DocumentMetricMapping{DocumentID: docID, MetricID: metricID}
	return r.db.Where("document_id = ? AND metric_id = ?", docID, metricID).
		FirstOrCreate(&m).Error
}

// RemoveMetricMapping 解除文档与指标的关联
func (r *DocumentRepository) RemoveMetricMapping(docID, metricID int64) error {
	return r.db.Where("document_id = ? AND metric_id = ?", docID, metricID).
		Delete(&models.DocumentMetricMapping{}).Error
}
