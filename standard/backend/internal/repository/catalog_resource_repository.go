package repository

import (
	"context"
	"fmt"

	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

type CatalogResourceRepository struct{ db *gorm.DB }

func NewCatalogResourceRepository(db *gorm.DB) *CatalogResourceRepository {
	return &CatalogResourceRepository{db: db}
}

func (r *CatalogResourceRepository) ListChanges(ctx context.Context, tenantID, afterID int64, limit int) ([]models.CatalogResourceChangeRow, error) {
	var rows []models.CatalogResourceChangeRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, tenant_id, source_type, source_identity, operation, resource_version, snapshot, observed_at
		FROM standard.catalog_resource_changes
		WHERE tenant_id = ? AND id > ?
		ORDER BY id ASC
		LIMIT ?
	`, tenantID, afterID, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list Standard catalog resource changes: %w", err)
	}
	return rows, nil
}

func (r *CatalogResourceRepository) ListMetrics(ctx context.Context, tenantID int64, ids []int64) ([]models.Metric, error) {
	var rows []models.Metric
	if len(ids) == 0 {
		return rows, nil
	}
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve Standard catalog metrics: %w", err)
	}
	return rows, nil
}
