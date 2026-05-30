package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/spatial"
	"github.com/addp/common/sqldialect"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

// DatabaseTablePreviewProvider 通用数据库表预览 Provider。
// 自动支持所有实现 BatchReadableProvider 的表格型数据库。
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
	batchReader, ok := plug.(plugin.BatchReadableProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement BatchReadableProvider", req.Engine.EngineType)
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
		if len(geometryColumns) == 0 {
			geometryColumns = p.detectGeometryColumns(req.Engine.EngineType, columns)
		}
	} else {
		// Meta 不可用或无数据，回退到 ItemMetadataProvider。
		columnNames = make([]string, len(columns))
		for i, col := range columns {
			columnNames[i] = col.Name
		}

		// 检测几何列
		geometryColumns = p.detectGeometryColumns(req.Engine.EngineType, columns)

		// 转换列元数据
		columnMetadata = make([]models.ColumnMetadata, len(columns))
		for i, col := range columns {
			columnMetadata[i] = models.ColumnMetadata{
				ColumnName:   col.Name,
				Type:         databaseFieldNativeType(col),
				IsNullable:   col.Nullable,
				IsPrimaryKey: col.PrimaryKey,
				Comment:      col.Comment,
			}
		}
	}

	// 4. 获取总行数，优先使用 Meta / ItemMetadata 中已知的估算值。
	totalCount := p.resolveTableRowCount(req, itemMetadata)

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
	rows, err := p.queryData(ctx, batchReader, connInfo, req.Engine.ID, plug, req.Engine.EngineType, req.Schema, tableName, offset, limit, columns)
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
	batchReader plugin.BatchReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	plug plugin.EnginePlugin,
	engineType, schema, table string,
	offset, limit int,
	columns []datatype.FieldInfo,
) ([]map[string]interface{}, error) {
	dialect := sqldialect.ForEngine(engineType)
	selectExpr := "*"
	query := ""
	if dialect.IsPostgreSQL() {
		primaryKeys := databasePrimaryKeyColumns(columns)
		if len(primaryKeys) > 0 {
			selectExpr = databasePreviewSelectExpr(dialect, columns, databasePreviewSourceAlias)
			query = databasePreviewPostgreSQLPrimaryKeyPageQuery(dialect, selectExpr, schema, table, primaryKeys, limit, offset)
		} else {
			selectExpr = databasePreviewSelectExpr(dialect, columns, "")
		}
	}

	if query == "" {
		query = dialect.SelectTableSQL(selectExpr, schema, table, "", "", limit, offset)
	}
	result, err := batchReader.ReadBatch(ctx, connInfo, databaseTableCatalogPath(engineID, plug, schema, table), plugin.BatchReadOptions{
		Query: query,
	})
	if err != nil {
		return nil, err
	}
	return result.Rows, nil
}

const (
	databasePreviewSourceAlias = "__addp_src"
	databasePreviewKeyCTEAlias = "__addp_page_keys"
	databasePreviewKeyAlias    = "__addp_keys"
)

func databaseFieldNativeType(field datatype.FieldInfo) string {
	nativeType := strings.TrimSpace(field.NativeType)
	if nativeType != "" {
		return nativeType
	}
	return strings.TrimSpace(string(field.Type))
}

func databasePrimaryKeyColumns(columns []datatype.FieldInfo) []string {
	primaryKeys := make([]string, 0)
	for _, col := range columns {
		if col.PrimaryKey && strings.TrimSpace(col.Name) != "" {
			primaryKeys = append(primaryKeys, col.Name)
		}
	}
	return primaryKeys
}

func databasePreviewSelectExpr(dialect sqldialect.Dialect, columns []datatype.FieldInfo, tableAlias string) string {
	if len(columns) == 0 {
		if tableAlias != "" {
			return dialect.QuoteIdentifier(tableAlias) + ".*"
		}
		return "*"
	}

	selectColumns := make([]string, 0, len(columns)*2)
	for _, col := range columns {
		nativeType := databaseFieldNativeType(col)
		columnRef := databasePreviewColumnRef(dialect, tableAlias, col.Name)
		if spatial.IsPostGISSpatialType(nativeType) {
			selectColumns = append(selectColumns, fmt.Sprintf("%s AS %s",
				databasePreviewWKTExpr(columnRef, nativeType),
				dialect.QuoteIdentifier(col.Name),
			))
			selectColumns = append(selectColumns, fmt.Sprintf("%s AS %s",
				databasePreviewRenderExpr(columnRef, nativeType),
				dialect.QuoteIdentifier(renderGeometryColumnName(col.Name)),
			))
			continue
		}
		selectColumns = append(selectColumns, fmt.Sprintf("%s AS %s", columnRef, dialect.QuoteIdentifier(col.Name)))
	}
	return strings.Join(selectColumns, ", ")
}

func databasePreviewColumnRef(dialect sqldialect.Dialect, tableAlias, column string) string {
	quotedColumn := dialect.QuoteIdentifier(column)
	if tableAlias == "" {
		return quotedColumn
	}
	return dialect.QuoteIdentifier(tableAlias) + "." + quotedColumn
}

func databasePreviewPostgreSQLPrimaryKeyPageQuery(
	dialect sqldialect.Dialect,
	selectExpr, schema, table string,
	primaryKeys []string,
	limit, offset int,
) string {
	qualifiedTable := dialect.QualifiedTable(schema, table)
	sourceAlias := dialect.QuoteIdentifier(databasePreviewSourceAlias)
	sourceOrderBy := databasePreviewOrderByClause(dialect, databasePreviewSourceAlias, primaryKeys)
	limitClause := databasePreviewLimitOffsetClause(limit, offset)
	if offset <= 0 {
		return fmt.Sprintf("SELECT %s FROM %s AS %s ORDER BY %s%s", selectExpr, qualifiedTable, sourceAlias, sourceOrderBy, limitClause)
	}

	keySelect := databasePreviewKeyColumnList(dialect, primaryKeys)
	keyOrderBy := databasePreviewOrderByClause(dialect, "", primaryKeys)
	keyCTE := dialect.QuoteIdentifier(databasePreviewKeyCTEAlias)
	keyAlias := dialect.QuoteIdentifier(databasePreviewKeyAlias)
	joinClause := databasePreviewPrimaryKeyJoinClause(dialect, primaryKeys)
	return fmt.Sprintf(
		"WITH %s AS (SELECT %s FROM %s ORDER BY %s%s) SELECT %s FROM %s AS %s JOIN %s AS %s ON %s ORDER BY %s",
		keyCTE,
		keySelect,
		qualifiedTable,
		keyOrderBy,
		limitClause,
		selectExpr,
		qualifiedTable,
		sourceAlias,
		keyCTE,
		keyAlias,
		joinClause,
		sourceOrderBy,
	)
}

func databasePreviewKeyColumnList(dialect sqldialect.Dialect, primaryKeys []string) string {
	parts := make([]string, 0, len(primaryKeys))
	for _, column := range primaryKeys {
		parts = append(parts, dialect.QuoteIdentifier(column))
	}
	return strings.Join(parts, ", ")
}

func databasePreviewOrderByClause(dialect sqldialect.Dialect, tableAlias string, columns []string) string {
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		parts = append(parts, databasePreviewColumnRef(dialect, tableAlias, column))
	}
	return strings.Join(parts, ", ")
}

func databasePreviewPrimaryKeyJoinClause(dialect sqldialect.Dialect, primaryKeys []string) string {
	parts := make([]string, 0, len(primaryKeys))
	for _, column := range primaryKeys {
		parts = append(parts, fmt.Sprintf(
			"%s = %s",
			databasePreviewColumnRef(dialect, databasePreviewSourceAlias, column),
			databasePreviewColumnRef(dialect, databasePreviewKeyAlias, column),
		))
	}
	return strings.Join(parts, " AND ")
}

func databasePreviewLimitOffsetClause(limit, offset int) string {
	var sb strings.Builder
	if limit > 0 {
		sb.WriteString(" LIMIT ")
		sb.WriteString(strconv.Itoa(limit))
	}
	if offset > 0 {
		sb.WriteString(" OFFSET ")
		sb.WriteString(strconv.Itoa(offset))
	}
	return sb.String()
}

func renderGeometryColumnName(column string) string {
	return "__render_geojson_" + column
}

func databasePreviewWKTExpr(columnRef, dataType string) string {
	if spatial.IsPostGISGeographyType(dataType) {
		return fmt.Sprintf("ST_AsText(%s::geometry)", columnRef)
	}
	return fmt.Sprintf("ST_AsText(%s)", columnRef)
}

func databasePreviewRenderExpr(columnRef, dataType string) string {
	if spatial.IsPostGISGeographyType(dataType) {
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE ST_AsGeoJSON(%s::geometry) END", columnRef, columnRef)
	}
	return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL WHEN ST_SRID(%s) IN (0, 4326) THEN ST_AsGeoJSON(%s) ELSE ST_AsGeoJSON(ST_Transform(%s, 4326)) END", columnRef, columnRef, columnRef, columnRef)
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
func (p *DatabaseTablePreviewProvider) detectGeometryColumns(engineType string, columns []datatype.FieldInfo) []string {
	if !sqldialect.ForEngine(engineType).IsPostgreSQL() {
		return []string{}
	}

	geometryColumns := []string{}
	for _, col := range columns {
		if p.isSpatialType(databaseFieldNativeType(col)) {
			geometryColumns = append(geometryColumns, col.Name)
		}
	}

	return geometryColumns
}

// isSpatialType 判断是否为空间类型
func (p *DatabaseTablePreviewProvider) isSpatialType(dataType string) bool {
	return spatial.IsPostGISSpatialType(dataType)
}

func (p *DatabaseTablePreviewProvider) describeDatabaseTable(
	ctx context.Context,
	metadataProvider plugin.ItemMetadataProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	plug plugin.EnginePlugin,
	schema, table string,
) (*plugin.ItemMetadata, []datatype.FieldInfo, error) {
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
	return itemMetadata, plugin.ItemMetadataFields(itemMetadata), nil
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
	req *PreviewRequest,
	itemMetadata *plugin.ItemMetadata,
) int64 {
	if req != nil && req.ItemRowCount != nil && *req.ItemRowCount > 0 {
		return *req.ItemRowCount
	}
	if req != nil {
		tableInfo := tableInfoFromMetaAttributes(req.Attributes, "")
		if tableInfo != nil && tableInfo.RowCount != nil && *tableInfo.RowCount > 0 {
			return *tableInfo.RowCount
		}
	}
	if itemMetadata != nil {
		if tableInfo := plugin.ItemMetadataTableInfo(itemMetadata); tableInfo != nil && tableInfo.RowCount != nil && *tableInfo.RowCount > 0 {
			return *tableInfo.RowCount
		}
		if rowCount, ok := databaseInt64Stat(itemMetadata.Stats, "row_count"); ok && rowCount > 0 {
			return rowCount
		}
		if rowCount, ok := databaseInt64Stat(itemMetadata.Stats, "document_count"); ok && rowCount > 0 {
			return rowCount
		}
	}
	return 0
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
	spatialMeta, err := p.metaClient.GetItemSpatialMetadataByCatalogPath(engineID, fmt.Sprintf("%s.%s", schema, table))
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
		dataType := string(field.Type)

		// 对于几何列，使用 Meta 返回的标准空间能力信息来丰富 type
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
			Type:         dataType,
			IsNullable:   true, // Meta 当前不存储 nullable 信息，默认为 true
			IsPrimaryKey: field.PrimaryKey,
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
