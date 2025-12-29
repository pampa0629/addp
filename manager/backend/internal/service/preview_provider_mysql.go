package service

import (
	"context"
	"fmt"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type mysqlPreviewProvider struct {
	metadataRepo *repository.MetadataRepository
	priority     int
}

func NewMySQLPreviewProvider(metadataRepo *repository.MetadataRepository) PreviewProvider {
	return &mysqlPreviewProvider{
		metadataRepo: metadataRepo,
		priority:     100,
	}
}

func (p *mysqlPreviewProvider) Name() string {
	return "builtin:mysql-table"
}

func (p *mysqlPreviewProvider) Priority() int {
	return p.priority
}

func (p *mysqlPreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Engine == nil {
		return false
	}
	if req.Schema == "" || req.Table == "" {
		return false
	}

	// 支持 MySQL 和 Doris (Doris 使用 MySQL 协议)
	resourceType := sanitizeResourceType(req.Engine.EngineType)
	return resourceType == "mysql" || resourceType == "doris"
}

func (p *mysqlPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	_ = ctx

	const maxRows = 50

	// MySQL/Doris 使用专用的查询方法 (基于 MySQL 协议)
	columns, rows, total, geometryColumns, err := p.metadataRepo.QueryMySQLTablePreview(
		req.Engine,
		req.Schema,
		req.Table,
		req.Page,
		req.PageSize,
		maxRows,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql/doris preview query failed: %w", err)
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
		EngineID:   req.Engine.ID,
		Schema:       req.Schema,
		Table:        req.Table,
		EngineType: req.Engine.EngineType,
	}, nil
}
