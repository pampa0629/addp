package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

// DatabaseTablePreviewProvider 通用数据库表预览 Provider
// 自动支持所有实现 SQLQueryRuntimeProvider 的表格型数据库。
type DatabaseTablePreviewProvider struct {
	metadataRepo *repository.MetadataRepository
	metaClient   *commonClient.MetaClient
}

// NewDatabaseTablePreviewProvider 创建通用数据库表预览 Provider
func NewDatabaseTablePreviewProvider(metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient) PreviewProvider {
	return &DatabaseTablePreviewProvider{
		metadataRepo: metadataRepo,
		metaClient:   metaClient,
	}
}

func (p *DatabaseTablePreviewProvider) Name() string {
	return "builtin:database-table"
}

func (p *DatabaseTablePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	const maxRows = 50

	plug, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}
	sqlRuntime, ok := plug.(plugin.SQLQueryRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement SQLQueryRuntimeProvider", req.Engine.EngineType)
	}
	metadataProvider, _ := plug.(plugin.ItemMetadataProvider)
	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)

	// 1. 处理表名可能包含 schema 前缀的情况
	tableName := req.Table
	if strings.HasPrefix(req.Table, req.Schema+".") {
		tableName = strings.TrimPrefix(req.Table, req.Schema+".")
	}

	// 2. 从 ItemMetadataProvider 获取字段和统计信息。
	itemMetadata, columns, err := p.describeDatabaseTable(ctx, metadataProvider, connInfo, req.Engine.ID, plug, req.Schema, tableName)
	if err != nil {
		if p.isTableNotFoundError(err) {
			return nil, &TableNotFoundError{
				Schema: req.Schema,
				Table:  tableName,
			}
		}
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}

	// 3. 尝试从 Meta 获取列元数据（优先用于展示，包含更准确的几何类型）。
	columnMetadata, geometryColumns, srid, extent, metaErr := p.getColumnMetadataFromMeta(ctx, req.TenantID, req.Engine.ID, req.Schema, tableName)
	var columnNames []string

	if metaErr == nil && len(columnMetadata) > 0 {
		// Meta 元数据可用，使用 Meta 的数据
		columnNames = make([]string, len(columnMetadata))
		for i, meta := range columnMetadata {
			columnNames[i] = meta.ColumnName
		}
	} else {
		// Meta 不可用或无数据，回退到 ItemMetadataProvider。
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

	// 4. 获取总行数，优先使用 ItemMetadataProvider stats，缺失时通过 SQL runtime 查询。
	totalCount, err := p.resolveTableRowCount(ctx, sqlRuntime, connInfo, req.Engine.ID, req.Engine.EngineType, req.Schema, tableName, itemMetadata)
	if err != nil && p.isTableNotFoundError(err) {
		return nil, &TableNotFoundError{
			Schema: req.Schema,
			Table:  tableName,
		}
	}
	if err != nil {
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
	rows, err := p.queryData(ctx, sqlRuntime, connInfo, req.Engine.ID, req.Engine.EngineType, req.Schema, tableName, offset, limit, columns)
	if err != nil {
		return nil, fmt.Errorf("failed to query data: %w", err)
	}

	return &models.TablePreview{
		Mode:                  PreviewModeTable,
		Columns:               columnNames,
		ColumnMetadata:        columnMetadata,
		Rows:                  rows,
		Total:                 int(totalCount),
		Page:                  page,
		PageSize:              pageSize,
		GeometryColumns:       geometryColumns,
		RenderGeometryColumns: buildDatabaseRenderGeometryColumns(geometryColumns, rows),
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
	sqlRuntime plugin.SQLQueryRuntimeProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	engineType, schema, table string,
	offset, limit int,
	columns []plugin.ColumnInfo,
) ([]map[string]interface{}, error) {
	// 根据引擎类型构建查询
	var query string
	switch strings.ToLower(engineType) {
	case "postgresql":
		// PostgreSQL: 处理空间字段（ST_AsText 转为 WKT 格式，更易读）
		selectColumns := make([]string, 0, len(columns)*2)
		for _, col := range columns {
			if p.isSpatialType(col.DataType) {
				// 空间字段：转换为 WKT 格式（如 MULTIPOLYGON (((120.175...）））
				selectColumns = append(selectColumns, fmt.Sprintf("%s AS \"%s\"", databasePreviewWKTExpr(col), col.ColumnName))
				selectColumns = append(selectColumns, fmt.Sprintf("%s AS \"%s\"", databasePreviewRenderExpr(col), renderGeometryColumnName(col.ColumnName)))
			} else {
				// 普通字段：列名加双引号，确保大小写敏感
				selectColumns = append(selectColumns, fmt.Sprintf("\"%s\"", col.ColumnName))
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

	result, err := sqlRuntime.ExecuteSQL(ctx, connInfo, query, plugin.QueryOptions{
		EngineID:   engineID,
		EngineType: engineType,
		Limit:      limit,
		ReadOnly:   true,
	})
	if err != nil {
		return nil, err
	}
	return result.Rows, nil
}

func renderGeometryColumnName(column string) string {
	return "__render_geojson_" + column
}

func databasePreviewWKTExpr(col plugin.ColumnInfo) string {
	if isGeographyType(col.DataType) {
		return fmt.Sprintf("ST_AsText(\"%s\"::geometry)", col.ColumnName)
	}
	return fmt.Sprintf("ST_AsText(\"%s\")", col.ColumnName)
}

func databasePreviewRenderExpr(col plugin.ColumnInfo) string {
	if isGeographyType(col.DataType) {
		return fmt.Sprintf("CASE WHEN \"%s\" IS NULL THEN NULL ELSE ST_AsGeoJSON(\"%s\"::geometry) END", col.ColumnName, col.ColumnName)
	}
	return fmt.Sprintf("CASE WHEN \"%s\" IS NULL THEN NULL ELSE ST_AsGeoJSON(ST_Transform(\"%s\", 4326)) END", col.ColumnName, col.ColumnName)
}

func buildDatabaseRenderGeometryColumns(geometryColumns []string, rows []map[string]interface{}) map[string]string {
	if len(geometryColumns) == 0 {
		return nil
	}

	result := make(map[string]string, len(geometryColumns))
	for _, column := range geometryColumns {
		renderColumn := renderGeometryColumnName(column)
		if rowsContainColumn(rows, renderColumn) {
			result[column] = renderColumn
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func rowsContainColumn(rows []map[string]interface{}, column string) bool {
	for _, row := range rows {
		value, exists := row[column]
		if !exists || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			var payload interface{}
			if json.Unmarshal([]byte(typed), &payload) == nil {
				return true
			}
		case map[string]interface{}:
			return true
		}
	}
	return false
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

func isGeographyType(dataType string) bool {
	dataTypeLower := strings.ToLower(dataType)
	return dataTypeLower == "geography" || strings.HasPrefix(dataTypeLower, "geography(")
}

func (p *DatabaseTablePreviewProvider) describeDatabaseTable(
	ctx context.Context,
	metadataProvider plugin.ItemMetadataProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	plug plugin.EnginePlugin,
	schema, table string,
) (*plugin.ItemMetadata, []plugin.ColumnInfo, error) {
	if metadataProvider == nil {
		return nil, nil, fmt.Errorf("engine %s does not implement ItemMetadataProvider", plug.Type())
	}
	itemMetadata, err := metadataProvider.DescribeItem(ctx, connInfo, databaseTableCatalogPath(engineID, plug, schema, table), plugin.MetadataOptions{
		IncludeStatistics: true,
		IncludeIndexes:    true,
	})
	if err != nil {
		return nil, nil, err
	}
	columns := make([]plugin.ColumnInfo, 0, len(itemMetadata.Fields))
	for _, field := range itemMetadata.Fields {
		dataType := field.NativeType
		if dataType == "" {
			dataType = field.Type
		}
		columns = append(columns, plugin.ColumnInfo{
			ColumnName:   field.Name,
			DataType:     dataType,
			IsNullable:   field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
		})
	}
	return itemMetadata, columns, nil
}

func databaseTableCatalogPath(engineID uint, plug plugin.EnginePlugin, schema, table string) plugin.CatalogPath {
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []plugin.CatalogSegment{
			{Term: databaseNamespaceTerm(plug), Kind: plugin.CatalogKindNamespace, Name: schema},
			{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: table},
		},
	}
}

func databaseNamespaceTerm(plug plugin.EnginePlugin) string {
	if modelProvider, ok := plug.(plugin.CatalogModelProvider); ok {
		model := modelProvider.CatalogModel()
		if len(model.Levels) > 0 && model.Levels[0].Term != "" {
			return model.Levels[0].Term
		}
	}
	return plugin.CatalogTermDatabase
}

func (p *DatabaseTablePreviewProvider) resolveTableRowCount(
	ctx context.Context,
	sqlRuntime plugin.SQLQueryRuntimeProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	engineType, schema, table string,
	itemMetadata *plugin.ItemMetadata,
) (int64, error) {
	if itemMetadata != nil {
		if rowCount, ok := databaseInt64Stat(itemMetadata.Stats, "row_count"); ok {
			return rowCount, nil
		}
		if rowCount, ok := databaseInt64Stat(itemMetadata.Stats, "document_count"); ok {
			return rowCount, nil
		}
	}
	query := databasePreviewCountQuery(engineType, schema, table)
	result, err := sqlRuntime.ExecuteSQL(ctx, connInfo, query, plugin.QueryOptions{
		EngineID:   engineID,
		EngineType: engineType,
		ReadOnly:   true,
	})
	if err != nil {
		return 0, err
	}
	if len(result.Rows) == 0 {
		return 0, nil
	}
	for _, value := range result.Rows[0] {
		return numericToInt64(value), nil
	}
	return 0, nil
}

func databasePreviewCountQuery(engineType, schema, table string) string {
	switch strings.ToLower(engineType) {
	case "mysql", "doris", "clickhouse":
		return fmt.Sprintf("SELECT COUNT(*) AS total FROM `%s`.`%s`", schema, table)
	default:
		return fmt.Sprintf("SELECT COUNT(*) AS total FROM \"%s\".\"%s\"", schema, table)
	}
}

func databaseInt64Stat(stats map[string]interface{}, key string) (int64, bool) {
	if stats == nil {
		return 0, false
	}
	value, ok := stats[key]
	if !ok {
		return 0, false
	}
	return numericToInt64(value), true
}

func numericToInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int16:
		return int64(typed)
	case int8:
		return int64(typed)
	case uint:
		return int64(typed)
	case uint64:
		return int64(typed)
	case uint32:
		return int64(typed)
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case []byte:
		var parsed int64
		if _, err := fmt.Sscan(string(typed), &parsed); err == nil {
			return parsed
		}
	case string:
		var parsed int64
		if _, err := fmt.Sscan(typed, &parsed); err == nil {
			return parsed
		}
	}
	return 0
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
	spatialMeta, err := p.metaClient.GetItemSpatialMetadata(engineID, schema, table)
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
			IsNullable:   true, // Meta 当前不存储 nullable 信息，默认为 true
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
