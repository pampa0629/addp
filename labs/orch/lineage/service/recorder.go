package service

import (
	"context"
	"lineage/models"
)

// LineageRecorder 血缘记录接口（策略模式）
type LineageRecorder interface {
	// Record 记录单条血缘
	Record(ctx context.Context, input *models.LineageInput) (*models.DataLineage, error)

	// RecordBatch 批量记录血缘
	RecordBatch(ctx context.Context, inputs []*models.LineageInput) error

	// QueryUpstream 查询上游血缘（数据来源）
	QueryUpstream(ctx context.Context, itemID uint) ([]*models.DataLineage, error)

	// QueryDownstream 查询下游血缘（数据去向）
	QueryDownstream(ctx context.Context, itemID uint) ([]*models.DataLineage, error)

	// Query 灵活查询
	Query(ctx context.Context, query *models.LineageQuery) ([]*models.DataLineage, error)
}
