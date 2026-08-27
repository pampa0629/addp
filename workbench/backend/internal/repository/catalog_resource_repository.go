package repository

import (
	"context"
	"fmt"

	"github.com/addp/workbench/internal/models"
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
		FROM workbench.catalog_resource_changes
		WHERE tenant_id = ? AND id > ?
		ORDER BY id ASC
		LIMIT ?
	`, tenantID, afterID, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list Workbench catalog resource changes: %w", err)
	}
	return rows, nil
}

func (r *CatalogResourceRepository) ListDataApplications(ctx context.Context, tenantID int64, ids []string) ([]models.CatalogDataApplicationRecord, error) {
	rows := make([]models.CatalogDataApplicationRecord, 0)
	if len(ids) == 0 {
		return rows, nil
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT application.id,
		       application.publication_status,
		       application.current_revision_number,
		       revision.name AS revision_name,
		       revision.description AS revision_description,
		       revision.published_at
		FROM workbench.data_applications AS application
		JOIN workbench.data_application_revisions AS revision
		  ON revision.tenant_id = application.tenant_id
		 AND revision.application_id = application.id
		 AND revision.revision_number = application.current_revision_number
		WHERE application.tenant_id = ?
		  AND application.current_revision_number IS NOT NULL
		  AND application.id IN ?
	`, tenantID, ids).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("resolve Workbench catalog Data Applications: %w", err)
	}
	return rows, nil
}

func (r *CatalogResourceRepository) LatestChangeVersions(ctx context.Context, tenantID int64, ids []string) (map[string]int64, error) {
	versions := make(map[string]int64, len(ids))
	if len(ids) == 0 {
		return versions, nil
	}
	var rows []struct {
		SourceIdentity string
		Version        int64
	}
	if err := r.db.WithContext(ctx).Raw(`
		SELECT CAST(source_identity AS TEXT) AS source_identity, MAX(id) AS version
		FROM workbench.catalog_resource_changes
		WHERE tenant_id = ? AND source_type = ? AND source_identity IN ?
		GROUP BY source_identity
	`, tenantID, models.CatalogSourceTypeDataApplication, ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve Workbench catalog Data Application versions: %w", err)
	}
	for _, row := range rows {
		versions[row.SourceIdentity] = row.Version
	}
	return versions, nil
}
