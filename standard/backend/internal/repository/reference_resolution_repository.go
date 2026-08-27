package repository

import (
	"context"

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
