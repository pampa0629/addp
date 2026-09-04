package repository

import (
	"context"
	"errors"
	"fmt"

	commonapi "github.com/addp/common/api"
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

func (r *CatalogResourceRepository) ListMetrics(ctx context.Context, tenantID int64, ids []int64) ([]models.MetricDefinitionAggregate, error) {
	rows := make([]models.MetricDefinitionAggregate, 0, len(ids))
	if len(ids) == 0 {
		return rows, nil
	}
	metricRepo := NewMetricRepository(r.db.WithContext(ctx))
	for _, id := range ids {
		item, err := metricRepo.GetAggregate(id, tenantID)
		if errors.Is(err, commonapi.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("resolve Standard catalog metric %d: %w", id, err)
		}
		rows = append(rows, *item)
	}
	return rows, nil
}
