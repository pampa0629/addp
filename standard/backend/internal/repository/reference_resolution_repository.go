package repository

import (
	"context"
	"strings"
	"time"

	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

type ReferenceResolutionRepository struct {
	db *gorm.DB
}

func NewReferenceResolutionRepository(db *gorm.DB) *ReferenceResolutionRepository {
	return &ReferenceResolutionRepository{db: db}
}

func (r *ReferenceResolutionRepository) ResolveDomains(ctx context.Context, tenantID int64, ids []int64) ([]models.Domain, error) {
	result := make([]models.Domain, 0)
	if len(ids) == 0 {
		return result, nil
	}
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&result).Error
	return result, wrapDBError(err)
}

func (r *ReferenceResolutionRepository) ResolveGlossaries(ctx context.Context, tenantID int64, ids []int64) ([]models.Glossary, error) {
	result := make([]models.Glossary, 0)
	if len(ids) == 0 {
		return result, nil
	}
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&result).Error
	return result, wrapDBError(err)
}

func (r *ReferenceResolutionRepository) ResolveElements(ctx context.Context, tenantID int64, ids []int64) ([]models.PublishedElementReference, error) {
	result := make([]models.PublishedElementReference, 0)
	if len(ids) == 0 {
		return result, nil
	}
	asOf := time.Now().UTC()
	err := r.db.WithContext(ctx).Table("standard.elements AS e").
		Select("e.id, e.tenant_id, e.scope_type, e.owner_domain_id, e.code, e.lifecycle_state, e.version, er.id AS revision_id, er.revision_no, er.name, er.status").
		Joins("JOIN standard.element_revisions er ON er.element_id = e.id AND er.status = ? AND er.effective_from <= ? AND (er.effective_to IS NULL OR er.effective_to > ?)", models.RevisionStatusPublished, asOf, asOf).
		Where("e.tenant_id = ? AND e.lifecycle_state = ? AND e.id IN ?", tenantID, "active", ids).Scan(&result).Error
	return result, wrapDBError(err)
}

func (r *ReferenceResolutionRepository) ListDomainCandidates(ctx context.Context, tenantID int64, search string, page, pageSize int) ([]models.Domain, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.Domain{}).
		Where("tenant_id = ? AND lifecycle_state = ?", tenantID, "active")
	if search = strings.TrimSpace(search); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("name ILIKE ? OR code ILIKE ?", pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapDBError(err)
	}
	items := make([]models.Domain, 0)
	err := query.Order("LOWER(name) ASC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, wrapDBError(err)
}

func (r *ReferenceResolutionRepository) ListGlossaryCandidates(ctx context.Context, tenantID int64, search string, page, pageSize int) ([]models.Glossary, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.Glossary{}).
		Where("tenant_id = ? AND status = ?", tenantID, "approved")
	if search = strings.TrimSpace(search); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("name ILIKE ? OR definition ILIKE ?", pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapDBError(err)
	}
	items := make([]models.Glossary, 0)
	err := query.Order("LOWER(name) ASC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, wrapDBError(err)
}

func (r *ReferenceResolutionRepository) ListElementCandidates(ctx context.Context, tenantID int64, search string, page, pageSize int) ([]models.PublishedElementReference, int64, error) {
	asOf := time.Now().UTC()
	query := r.db.WithContext(ctx).Table("standard.elements AS e").
		Joins("JOIN standard.element_revisions er ON er.element_id = e.id AND er.status = ? AND er.effective_from <= ? AND (er.effective_to IS NULL OR er.effective_to > ?)", models.RevisionStatusPublished, asOf, asOf).
		Where("e.tenant_id = ? AND e.lifecycle_state = ?", tenantID, "active")
	if search = strings.TrimSpace(search); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("er.name ILIKE ? OR e.code ILIKE ? OR er.definition ILIKE ?", pattern, pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapDBError(err)
	}
	items := make([]models.PublishedElementReference, 0)
	err := query.Select("e.id, e.tenant_id, e.scope_type, e.owner_domain_id, e.code, e.lifecycle_state, e.version, er.id AS revision_id, er.revision_no, er.name, er.status").
		Order("LOWER(er.name) ASC, e.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error
	return items, total, wrapDBError(err)
}
