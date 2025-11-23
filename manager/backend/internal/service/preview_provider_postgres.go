package service

import (
	"context"
	"fmt"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type postgresPreviewProvider struct {
	metadataRepo *repository.MetadataRepository
	priority     int
}

func NewPostgresPreviewProvider(metadataRepo *repository.MetadataRepository) PreviewProvider {
	return &postgresPreviewProvider{
		metadataRepo: metadataRepo,
		priority:     100,
	}
}

func (p *postgresPreviewProvider) Name() string {
	return "builtin:postgresql-table"
}

func (p *postgresPreviewProvider) Priority() int {
	return p.priority
}

func (p *postgresPreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Resource == nil {
		return false
	}
	if req.Schema == "" || req.Table == "" {
		return false
	}

	return sanitizeResourceType(req.Resource.ResourceType) == "postgresql"
}

func (p *postgresPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	_ = ctx

	const maxRows = 50

	columns, rows, total, geometryColumns, err := p.metadataRepo.QueryTablePreview(
		req.Resource,
		req.Schema,
		req.Table,
		req.Page,
		req.PageSize,
		maxRows,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres preview query failed: %w", err)
	}

	return &models.TablePreview{
		Mode:            PreviewModeTable,
		Columns:         columns,
		Rows:            rows,
		Total:           total,
		Page:            req.Page,
		PageSize:        req.PageSize,
		GeometryColumns: geometryColumns,
		// Populate MVT preview metadata for frontend decision-making
		ResourceID:   req.Resource.ID,
		Schema:       req.Schema,
		Table:        req.Table,
		ResourceType: req.Resource.ResourceType,
	}, nil
}
