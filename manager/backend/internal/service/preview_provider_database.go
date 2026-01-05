package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/gorm"
)

// DatabaseTablePreviewProvider 通用数据库表预览 Provider
// 自动支持所有实现了 RelationalDBPlugin 的数据库（PostgreSQL、MySQL、ClickHouse、Doris、MongoDB 等）
type DatabaseTablePreviewProvider struct {
	metadataRepo *repository.MetadataRepository
	priority     int
}

// NewDatabaseTablePreviewProvider 创建通用数据库表预览 Provider
func NewDatabaseTablePreviewProvider(metadataRepo *repository.MetadataRepository) PreviewProvider {
	return &DatabaseTablePreviewProvider{
		metadataRepo: metadataRepo,
		priority:     100, // 高优先级
	}
}

func (p *DatabaseTablePreviewProvider) Name() string {
	return "builtin:database-table"
}

func (p *DatabaseTablePreviewProvider) Priority() int {
	return p.priority
}

func (p *DatabaseTablePreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Engine == nil {
		return false
	}
	if req.Schema == "" || req.Table == "" {
		return false
	}

	// 检查是否为表预览模式
	if req.Mode() != PreviewModeTable {
		return false
	}

	// 使用 dbbridge 检查是否支持连接池（即是否为关系型数据库）
	return dbbridge.SupportsConnectionPool(req.Engine.EngineType)
}

func (p *DatabaseTablePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	const maxRows = 50

	// 转换 Engine 类型到 common models
	commonResource := &commonModels.Engine{
		ID:             req.Engine.ID,
		EngineType:     req.Engine.EngineType,
		ConnectionInfo: commonModels.ConnectionInfo(req.Engine.ConnectionInfo),
	}

	// 1. 获取或创建连接池（自动使用正确的 plugin）
	db, err := dbbridge.GetOrCreatePool(commonResource, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get connection pool: %w", err)
	}

	// 2. 处理表名可能包含 schema 前缀的情况
	tableName := req.Table
	if strings.HasPrefix(req.Table, req.Schema+".") {
		tableName = strings.TrimPrefix(req.Table, req.Schema+".")
	}

	// 3. 获取列信息（自动使用正确的 plugin）
	columns, err := dbbridge.ListColumns(ctx, commonResource, db, req.Schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}

	// 提取列名
	columnNames := make([]string, len(columns))
	for i, col := range columns {
		columnNames[i] = col.ColumnName
	}

	// 4. 获取总行数（自动使用正确的 plugin）
	totalCount, err := dbbridge.GetTableRowCount(ctx, commonResource, db, req.Schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get row count: %w", err)
	}

	// 限制最大行数
	if totalCount > int64(maxRows) {
		totalCount = int64(maxRows)
	}

	// 5. 计算分页参数
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > maxRows {
		pageSize = maxRows
	}

	offset := (page - 1) * pageSize
	limit := pageSize

	// 6. 执行分页查询
	rows, err := p.queryData(ctx, db, req.Engine.EngineType, req.Schema, tableName, offset, limit, columns)
	if err != nil {
		return nil, fmt.Errorf("failed to query data: %w", err)
	}

	// 7. 检测几何列（仅 PostgreSQL + PostGIS）
	geometryColumns := p.detectGeometryColumns(req.Engine.EngineType, columns)

	return &models.TablePreview{
		Mode:            PreviewModeTable,
		Columns:         columnNames,
		Rows:            rows,
		Total:           int(totalCount),
		Page:            page,
		PageSize:        pageSize,
		GeometryColumns: geometryColumns,
		// MVT preview metadata (for frontend decision-making)
		EngineID:   req.Engine.ID,
		Schema:     req.Schema,
		Table:      req.Table,
		EngineType: req.Engine.EngineType,
	}, nil
}

// queryData 执行分页查询
func (p *DatabaseTablePreviewProvider) queryData(
	ctx context.Context,
	db interface{},
	engineType, schema, table string,
	offset, limit int,
	columns []plugin.ColumnInfo,
) ([]map[string]interface{}, error) {
	// 获取 GORM DB 实例
	gormDB, ok := db.(*gorm.DB)
	if !ok {
		return nil, fmt.Errorf("invalid database connection type")
	}

	// 根据引擎类型构建查询
	var query string
	switch strings.ToLower(engineType) {
	case "postgresql":
		// PostgreSQL: 处理空间字段（ST_AsGeoJSON）
		selectColumns := make([]string, len(columns))
		for i, col := range columns {
			if p.isSpatialType(col.DataType) {
				// 空间字段：转换为 GeoJSON
				selectColumns[i] = fmt.Sprintf("ST_AsGeoJSON(%s) AS %s", col.ColumnName, col.ColumnName)
			} else {
				selectColumns[i] = col.ColumnName
			}
		}
		query = fmt.Sprintf("SELECT %s FROM \"%s\".\"%s\" LIMIT %d OFFSET %d",
			strings.Join(selectColumns, ", "), schema, table, limit, offset)

	case "mysql", "doris":
		// MySQL/Doris: 使用反引号
		query = fmt.Sprintf("SELECT * FROM `%s`.`%s` LIMIT %d OFFSET %d",
			schema, table, limit, offset)

	case "clickhouse":
		// ClickHouse: 使用反引号
		query = fmt.Sprintf("SELECT * FROM `%s`.`%s` LIMIT %d OFFSET %d",
			schema, table, limit, offset)

	default:
		// 默认：使用双引号
		query = fmt.Sprintf("SELECT * FROM \"%s\".\"%s\" LIMIT %d OFFSET %d",
			schema, table, limit, offset)
	}

	// 执行查询
	var rows []map[string]interface{}
	if err := gormDB.WithContext(ctx).Raw(query).Scan(&rows).Error; err != nil {
		return nil, err
	}

	return rows, nil
}

// detectGeometryColumns 检测几何列（仅 PostgreSQL + PostGIS）
func (p *DatabaseTablePreviewProvider) detectGeometryColumns(engineType string, columns []plugin.ColumnInfo) []string {
	if strings.ToLower(engineType) != "postgresql" {
		return []string{}
	}

	geometryColumns := []string{}
	for _, col := range columns {
		if p.isSpatialType(col.DataType) {
			geometryColumns = append(geometryColumns, col.ColumnName)
		}
	}

	return geometryColumns
}

// isSpatialType 判断是否为空间类型
func (p *DatabaseTablePreviewProvider) isSpatialType(dataType string) bool {
	switch strings.ToLower(dataType) {
	case "geometry", "geography":
		return true
	default:
		return false
	}
}
