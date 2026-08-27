package repository

import (
	"context"
	"strings"

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

func (r *ReferenceResolutionRepository) ResolveElements(ctx context.Context, tenantID int64, ids []int64) ([]models.Element, error) {
	result := make([]models.Element, 0)
	if len(ids) == 0 {
		return result, nil
	}
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&result).Error
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

func (r *ReferenceResolutionRepository) ListElementCandidates(ctx context.Context, tenantID int64, search string, page, pageSize int) ([]models.Element, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.Element{}).
		Where("tenant_id = ? AND status = ? AND lifecycle_state = ?", tenantID, "approved", "active")
	if search = strings.TrimSpace(search); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("name ILIKE ? OR code ILIKE ? OR definition ILIKE ?", pattern, pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapDBError(err)
	}
	items := make([]models.Element, 0)
	err := query.Order("LOWER(name) ASC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, wrapDBError(err)
}
