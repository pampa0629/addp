package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type clickhousePreviewProvider struct {
	metadataRepo *repository.MetadataRepository
	priority     int
}

func NewClickHousePreviewProvider(metadataRepo *repository.MetadataRepository) PreviewProvider {
	return &clickhousePreviewProvider{
		metadataRepo: metadataRepo,
		priority:     100,
	}
}

func (p *clickhousePreviewProvider) Name() string {
	return "builtin:clickhouse-table"
}

func (p *clickhousePreviewProvider) Priority() int {
	return p.priority
}

func (p *clickhousePreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Resource == nil {
		return false
	}
	if req.Schema == "" || req.Table == "" {
		return false
	}

	// 支持 ClickHouse
	resourceType := sanitizeResourceType(req.Resource.ResourceType)
	return resourceType == "clickhouse"
}

func (p *clickhousePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	const maxRows = 50

	// 转换 Resource 类型到 common models
	commonResource := &commonModels.Resource{
		ID:             req.Resource.ID,
		ResourceType:   req.Resource.ResourceType,
		ConnectionInfo: commonModels.ConnectionInfo(req.Resource.ConnectionInfo),
	}

	// 获取或创建连接池
	db, err := dbbridge.GetOrCreatePool(commonResource, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get connection pool: %w", err)
	}

	// 处理表名可能包含 schema 前缀的情况
	tableName := req.Table
	if strings.HasPrefix(req.Table, req.Schema+".") {
		tableName = strings.TrimPrefix(req.Table, req.Schema+".")
	}

	fmt.Printf("[ClickHouse Preview] Querying database=%s, table=%s (original table=%s)\n",
		req.Schema, tableName, req.Table)

	// 获取列信息
	columns, err := dbbridge.ListColumns(ctx, commonResource, db, req.Schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}

	// 提取列名
	columnNames := make([]string, len(columns))
	for i, col := range columns {
		columnNames[i] = col.ColumnName
	}

	// 获取总行数
	totalCount, err := dbbridge.GetTableRowCount(ctx, commonResource, db, req.Schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get row count: %w", err)
	}

	// 限制最大行数
	if totalCount > int64(maxRows) {
		totalCount = int64(maxRows)
	}

	// 计算分页参数
	offset := (req.Page - 1) * req.PageSize
	limit := req.PageSize
	if limit > maxRows {
		limit = maxRows
	}

	// 构建查询语句
	query := fmt.Sprintf("SELECT * FROM `%s`.`%s` LIMIT %d OFFSET %d",
		req.Schema, tableName, limit, offset)

	// 执行查询
	rows := []map[string]interface{}{}
	if err := db.WithContext(ctx).Raw(query).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to query data: %w", err)
	}

	fmt.Printf("[ClickHouse Preview] Found %d rows in table %s.%s (total=%d, page=%d, pageSize=%d)\n",
		len(rows), req.Schema, tableName, totalCount, req.Page, req.PageSize)

	return &models.TablePreview{
		Mode:            PreviewModeTable,
		Columns:         columnNames,
		Rows:            rows,
		Total:           int(totalCount),
		Page:            req.Page,
		PageSize:        req.PageSize,
		GeometryColumns: []string{}, // ClickHouse 不支持几何列
		// Populate MVT preview metadata for frontend decision-making
		ResourceID:   req.Resource.ID,
		Schema:       req.Schema,
		Table:        req.Table,
		ResourceType: req.Resource.ResourceType,
	}, nil
}
