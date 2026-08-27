package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
)

type CatalogResourceChangeRow struct {
	ID             int64
	SourceType     string
	SourceIdentity int64
	Operation      string
	Snapshot       models.JSONB `gorm:"type:jsonb"`
	ObservedAt     time.Time
}

type CatalogResourceRepository struct {
	db *gorm.DB
}

func NewCatalogResourceRepository(db *gorm.DB) *CatalogResourceRepository {
	return &CatalogResourceRepository{db: db}
}

func (r *CatalogResourceRepository) ListChanges(ctx context.Context, tenantID, afterID int64, limit int) ([]CatalogResourceChangeRow, error) {
	var rows []CatalogResourceChangeRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, source_type, source_identity, operation, snapshot, observed_at
		FROM model.catalog_resource_changes
		WHERE tenant_id = ? AND id > ?
		ORDER BY id ASC
		LIMIT ?
	`, tenantID, afterID, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list Model catalog resource changes: %w", err)
	}
	return rows, nil
}

func (r *CatalogResourceRepository) ListEntities(ctx context.Context, tenantID int64, ids []int64) ([]models.Entity, error) {
	var rows []models.Entity
	if len(ids) == 0 {
		return rows, nil
	}
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve Model catalog entities: %w", err)
	}
	return rows, nil
}

func (r *CatalogResourceRepository) ListLogicalTables(ctx context.Context, tenantID int64, ids []int64) ([]models.LogicalTable, error) {
	var rows []models.LogicalTable
	if len(ids) == 0 {
		return rows, nil
	}
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve Model catalog logical tables: %w", err)
	}
	return rows, nil
}
