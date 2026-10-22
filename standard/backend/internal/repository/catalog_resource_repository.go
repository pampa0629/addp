package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"

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

// ListResources uses the same aggregate readers as Standard's professional pages.
func (r *CatalogResourceRepository) ListResources(ctx context.Context, tenantID int64, references []models.CatalogReference) ([]models.CatalogReferenceResolution, error) {
	results := make([]models.CatalogReferenceResolution, 0, len(references))
	db := r.db.WithContext(ctx)
	for _, reference := range references {
		id, err := strconv.ParseInt(reference.SourceIdentity, 10, 64)
		if err != nil {
			return nil, err
		}
		var item any
		switch reference.SourceType {
		case "metric":
			item, err = NewMetricRepository(db).GetAggregate(id, tenantID)
		case "glossary":
			item, err = NewGlossaryRepository(db).GetAggregate(id, tenantID)
		case "element":
			item, err = NewElementRepository(db).GetAggregate(id, tenantID)
		case "code_set":
			item, err = NewCodeSetRepository(db).GetAggregate(id, tenantID)
		case "document":
			item, err = NewDocumentRepository(db).GetAggregate(id, tenantID)
		default:
			return nil, fmt.Errorf("unsupported Standard catalog type")
		}
		if errors.Is(err, commonapi.ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result, err := models.ProjectCatalogResource(reference.SourceType, item)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}
