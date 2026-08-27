package repository

import (
	"context"
	"fmt"

	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

type CatalogResourceRepository struct{ db *gorm.DB }

func NewCatalogResourceRepository(db *gorm.DB) *CatalogResourceRepository {
	return &CatalogResourceRepository{db: db}
}

func (r *CatalogResourceRepository) ListChanges(ctx context.Context, tenantID, afterID int64, limit int) ([]models.CatalogResourceChangeRow, error) {
	var rows []models.CatalogResourceChangeRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, tenant_id, source_type, source_identity, operation, snapshot, observed_at
		FROM develop.catalog_resource_changes
		WHERE tenant_id = ? AND id > ?
		ORDER BY id ASC
		LIMIT ?
	`, tenantID, afterID, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list Develop catalog resource changes: %w", err)
	}
	return rows, nil
}

func (r *CatalogResourceRepository) ListReusableDevTasks(ctx context.Context, tenantID int64, ids []int64) ([]models.DevTask, error) {
	var rows []models.DevTask
	if len(ids) == 0 {
		return rows, nil
	}
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id IN ? AND dev_type IN ?", tenantID, ids, []string{"query", "workflow"}).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve Develop catalog DevTasks: %w", err)
	}
	return rows, nil
}

func (r *CatalogResourceRepository) LatestChangeVersions(ctx context.Context, tenantID int64, ids []int64) (map[int64]int64, error) {
	versions := make(map[int64]int64, len(ids))
	if len(ids) == 0 {
		return versions, nil
	}
	var rows []struct {
		SourceIdentity int64
		Version        int64
	}
	if err := r.db.WithContext(ctx).Raw(`
		SELECT source_identity, MAX(id) AS version
		FROM develop.catalog_resource_changes
		WHERE tenant_id = ? AND source_type = ? AND source_identity IN ?
		GROUP BY source_identity
	`, tenantID, models.CatalogSourceTypeDevTask, ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve Develop catalog DevTask versions: %w", err)
	}
	for _, row := range rows {
		versions[row.SourceIdentity] = row.Version
	}
	return versions, nil
}
