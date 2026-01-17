package service

import (
	"context"
	"fmt"
	"strings"

	commonClient "github.com/addp/common/client"
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
	metaClient   *commonClient.MetaClient
	priority     int
}

// NewDatabaseTablePreviewProvider 创建通用数据库表预览 Provider
func NewDatabaseTablePreviewProvider(metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient) PreviewProvider {
	return &DatabaseTablePreviewProvider{
		metadataRepo: metadataRepo,
		metaClient:   metaClient,
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

	// 3. 尝试从 Meta 获取列元数据（优先使用 Meta，包含更准确的几何类型）
	columnMetadata, geometryColumns, srid, extent, metaErr := p.getColumnMetadataFromMeta(ctx, req.TenantID, req.Engine.ID, req.Schema, tableName)
	var columnNames []string
	var columns []plugin.ColumnInfo

	if metaErr == nil && len(columnMetadata) > 0 {
		// Meta 元数据可用，使用 Meta 的数据
		columnNames = make([]string, len(columnMetadata))
		for i, meta := range columnMetadata {
			columnNames[i] = meta.ColumnName
		}
		// 仍需要从数据库获取列信息用于查询构建（必须获取，否则 PostgreSQL 查询会失败）
		columns, err = dbbridge.ListColumns(ctx, commonResource, db, req.Schema, tableName)
		if err != nil {
			// 检查是否为表不存在错误
			if p.isTableNotFoundError(err) {
				return nil, &TableNotFoundError{
					Schema: req.Schema,
					Table:  tableName,
				}
			}
			return nil, fmt.Errorf("failed to list columns for query construction: %w", err)
		}
	} else {
		// Meta 不可用或无数据，回退到数据库插件
		columns, err = dbbridge.ListColumns(ctx, commonResource, db, req.Schema, tableName)
		if err != nil {
			// 检查是否为表不存在错误
			if p.isTableNotFoundError(err) {
				return nil, &TableNotFoundError{
					Schema: req.Schema,
					Table:  tableName,
				}
			}
			return nil, fmt.Errorf("failed to list columns: %w", err)
		}

		// 提取列名
		columnNames = make([]string, len(columns))
		for i, col := range columns {
			columnNames[i] = col.ColumnName
		}

		// 检测几何列
		geometryColumns = p.detectGeometryColumns(req.Engine.EngineType, columns)

		// 转换列元数据
		columnMetadata = make([]models.ColumnMetadata, len(columns))
		for i, col := range columns {
			columnMetadata[i] = models.ColumnMetadata{
				ColumnName:   col.ColumnName,
				DataType:     col.DataType,
				IsNullable:   col.IsNullable,
				IsPrimaryKey: col.IsPrimaryKey,
				Comment:      col.Comment,
			}
		}
	}

	// 4. 获取总行数（自动使用正确的 plugin）
	totalCount, err := dbbridge.GetTableRowCount(ctx, commonResource, db, req.Schema, tableName)
	if err != nil {
		// 检查是否为表不存在错误
		if p.isTableNotFoundError(err) {
			return nil, &TableNotFoundError{
				Schema: req.Schema,
				Table:  tableName,
			}
		}
		return nil, fmt.Errorf("failed to get row count: %w", err)
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

	return &models.TablePreview{
		Mode:            PreviewModeTable,
		Columns:         columnNames,
		ColumnMetadata:  columnMetadata,
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
		// Spatial metadata (for spatial data preview)
		SRID:   srid,
		Extent: extent,
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
		// PostgreSQL: 处理空间字段（ST_AsText 转为 WKT 格式，更易读）
		selectColumns := make([]string, len(columns))
		for i, col := range columns {
			if p.isSpatialType(col.DataType) {
				// 空间字段：转换为 WKT 格式（如 MULTIPOLYGON (((120.175...）））
				selectColumns[i] = fmt.Sprintf("ST_AsText(\"%s\") AS \"%s\"", col.ColumnName, col.ColumnName)
				// 调试日志：记录空间字段转换
				fmt.Printf("[DEBUG] 检测到空间列: %s, 类型: %s, 使用 ST_AsText 转换\n", col.ColumnName, col.DataType)
			} else {
				// 普通字段：列名加双引号，确保大小写敏感
				selectColumns[i] = fmt.Sprintf("\"%s\"", col.ColumnName)
			}
		}
		query = fmt.Sprintf("SELECT %s FROM \"%s\".\"%s\" LIMIT %d OFFSET %d",
			strings.Join(selectColumns, ", "), schema, table, limit, offset)
		// 调试日志：记录生成的 SQL 查询
		fmt.Printf("[DEBUG] PostgreSQL 查询: %s\n", query)

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
	dataTypeLower := strings.ToLower(dataType)
	// 支持完整匹配和前缀匹配
	// 例如: "geometry", "geography", "GEOMETRY(MULTIPOLYGON, 4326)", "USER-DEFINED"
	return dataTypeLower == "geometry" ||
		dataTypeLower == "geography" ||
		strings.HasPrefix(dataTypeLower, "geometry(") ||
		strings.HasPrefix(dataTypeLower, "geography(") ||
		dataTypeLower == "user-defined" // PostGIS 几何类型有时显示为 USER-DEFINED
}

// getColumnMetadataFromMeta 从 Meta 服务获取列元数据（包含准确的几何类型）
func (p *DatabaseTablePreviewProvider) getColumnMetadataFromMeta(
	ctx context.Context,
	tenantID *uint,
	engineID uint,
	schema, table string,
) ([]models.ColumnMetadata, []string, int, []float64, error) {
	// 检查 MetaClient 是否可用
	if p.metaClient == nil {
		return nil, nil, 0, nil, fmt.Errorf("meta client not available")
	}

	// 设置租户 ID（用于服务间调用时的租户隔离）
	p.metaClient.SetTenantID(tenantID)

	// 调用 Meta API 获取表的空间元数据（包含字段列表和几何信息）
	spatialMeta, err := p.metaClient.GetTableSpatialMetadata(engineID, schema, table)
	if err != nil {
		return nil, nil, 0, nil, fmt.Errorf("failed to get spatial metadata from Meta: %w", err)
	}

	// 如果没有字段信息，返回错误
	if len(spatialMeta.Fields) == 0 {
		return nil, nil, 0, nil, fmt.Errorf("no field metadata in Meta")
	}

	// 转换字段信息为 ColumnMetadata
	columnMetadata := make([]models.ColumnMetadata, len(spatialMeta.Fields))
	geometryColumns := []string{}

	for i, field := range spatialMeta.Fields {
		dataType := field.DataType

		// 对于几何列，使用 spatial_metadata 中的几何类型信息来丰富 data_type
		if field.Name == spatialMeta.GeometryColumn && spatialMeta.GeometryColumn != "" {
			// 将几何列添加到列表
			geometryColumns = append(geometryColumns, field.Name)

			// 使用更精确的几何类型描述
			// 例如: "GEOMETRY(MULTIPOLYGON, 4326)" 而不是 "USER-DEFINED"
			if len(spatialMeta.Extent) > 0 {
				// 尝试从 geometry_types 中提取具体的几何类型
				geomType := ""
				if len(spatialMeta.GeometryTypes) > 0 {
					// Meta 存储格式如 "ST_MultiPolygon"，需要转换为 "MULTIPOLYGON"
					rawType := spatialMeta.GeometryTypes[0]
					if strings.HasPrefix(rawType, "ST_") {
						geomType = strings.ToUpper(strings.TrimPrefix(rawType, "ST_"))
					} else {
						geomType = strings.ToUpper(rawType)
					}
				}

				// 构建完整的几何类型描述
				if geomType != "" {
					dataType = fmt.Sprintf("GEOMETRY(%s, %d)", geomType, spatialMeta.SRID)
				} else {
					dataType = fmt.Sprintf("GEOMETRY(SRID %d)", spatialMeta.SRID)
				}
			}
		}

		columnMetadata[i] = models.ColumnMetadata{
			ColumnName:   field.Name,
			DataType:     dataType,
			IsNullable:   true,  // Meta 当前不存储 nullable 信息，默认为 true
			IsPrimaryKey: field.IsPrimaryKey,
			Comment:      "", // Meta 当前不存储 comment 信息
		}
	}

	// 返回 SRID 和 Extent（用于前端显示）
	return columnMetadata, geometryColumns, spatialMeta.SRID, spatialMeta.Extent, nil
}

// TableNotFoundError 表不存在错误
type TableNotFoundError struct {
	Schema string
	Table  string
}

func (e *TableNotFoundError) Error() string {
	return fmt.Sprintf("表 '%s.%s' 不存在或已被删除，请点击刷新按钮同步最新状态", e.Schema, e.Table)
}

// isTableNotFoundError 检查错误是否为表不存在错误
func (p *DatabaseTablePreviewProvider) isTableNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())
	// 检查常见的"表不存在"错误信息
	return strings.Contains(errMsg, "does not exist") ||
		strings.Contains(errMsg, "doesn't exist") ||
		strings.Contains(errMsg, "not exist") ||
		strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "unknown table") ||
		strings.Contains(errMsg, "no such table") ||
		strings.Contains(errMsg, "relation") && strings.Contains(errMsg, "does not exist")
}
