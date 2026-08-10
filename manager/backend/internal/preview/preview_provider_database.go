package preview

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/profilefilter"
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
	const maxRows = 2000

	plug := req.EnginePlugin
	if plug == nil {
		return nil, fmt.Errorf("resolved engine provider is required for database table preview")
	}
	batchReader, ok := plug.(plugin.BatchReadableProvider)
	sessionReader, hasSession := plug.(plugin.TableReadSessionProvider)
	if !ok && !hasSession {
		return nil, fmt.Errorf("engine %s does not implement a table read provider", req.Engine.EngineType)
	}
	catalogFactsProvider, _ := plug.(plugin.CatalogFactsProvider)
	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)

	// 1. 处理表名可能包含 schema 前缀的情况
	tableName := req.Table
	if strings.HasPrefix(req.Table, req.Schema+".") {
		tableName = strings.TrimPrefix(req.Table, req.Schema+".")
	}

	// 2. 从 CatalogFactsProvider 获取字段和统计信息。
	catalogFacts, columns, err := p.describeDatabaseTable(ctx, catalogFactsProvider, connInfo, plug, req.ProviderPath)
	if err != nil {
		if p.isTableNotFoundError(err) {
			return nil, &TableNotFoundError{
				Schema: req.Schema,
				Table:  tableName,
			}
		}
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	catalogSpatial := plugin.CatalogFactsSpatialInfo(catalogFacts)
	profileFields := append([]datatype.FieldInfo(nil), columns...)
	if metaTable := tableInfoFromMetaAttributes(req.Attributes, tableName); metaTable != nil && len(metaTable.Fields) > 0 {
		profileFields = append([]datatype.FieldInfo(nil), metaTable.Fields...)
	}

	// 3. 尝试从 Meta 获取列元数据（优先用于展示，包含更准确的几何类型）。
	columnMetadata, geometryColumns, srid, sourceCRS, sourceCRSDefinition, extent, metaErr := p.getColumnMetadataFromMeta(ctx, req.TenantID, req.Engine.ID, req.Schema, tableName)
	var columnNames []string

	if metaErr == nil && len(columnMetadata) > 0 {
		// Meta 元数据可用，使用 Meta 的数据
		columnNames = make([]string, len(columnMetadata))
		for i, meta := range columnMetadata {
			columnNames[i] = meta.ColumnName
		}
		if len(geometryColumns) == 0 {
			geometryColumns = databaseGeometryColumns(catalogSpatial, columns)
		}
	} else {
		// Meta 不可用或无数据，回退到 CatalogFactsProvider。
		columnNames = make([]string, len(columns))
		for i, col := range columns {
			columnNames[i] = col.Name
		}

		// 检测几何列
		geometryColumns = databaseGeometryColumns(catalogSpatial, columns)

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

	// 4. 获取总行数，优先使用 Meta / CatalogFacts 中已知的估算值。
	totalCount := p.resolveTableRowCount(req, catalogFacts)
	if req.DataScope.Kind == "condition" {
		totalCount = -1
	}
	if srid == 0 {
		srid = databasePreviewSourceSRID(columns, geometryColumns)
		if srid == 0 && catalogSpatial != nil {
			srid = catalogSpatial.PrimarySRIDValue()
		}
	}
	if sourceCRS == "" && catalogSpatial != nil {
		sourceCRS = catalogSpatial.PrimaryCRSRef()
	}
	if sourceCRSDefinition == nil && catalogSpatial != nil {
		sourceCRSDefinition = catalogSpatial.CRSDefinitionByID(sourceCRS)
	}
	spatialContract := tablePreviewSpatialCRSContract(geometryColumns, srid, sourceCRS, sourceCRSDefinition)

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
	rows, err := p.queryData(ctx, batchReader, sessionReader, connInfo, req.ProviderPath, req.Engine.EngineType, req.Schema, tableName, offset, limit, columns, req.DataScope)
	if err != nil {
		return nil, fmt.Errorf("failed to query data: %w", err)
	}

	return &models.TablePreview{
		Mode:                PreviewModeTable,
		Columns:             columnNames,
		Fields:              profileFields,
		ColumnMetadata:      columnMetadata,
		Rows:                rows,
		Total:               int(totalCount),
		Page:                page,
		PageSize:            pageSize,
		GeometryColumns:     geometryColumns,
		GeometryColumn:      spatialContract.GeometryColumn,
		SourceSRID:          spatialContract.SourceSRID,
		SourceCRS:           spatialContract.SourceCRS,
		SourceCRSDefinition: spatialContract.SourceCRSDefinition,
		TransformStatus:     spatialContract.TransformStatus,
		PreviewHint:         spatialContract.PreviewHint,
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
	sessionReader plugin.TableReadSessionProvider,
	connInfo plugin.ConnectionInfo,
	providerPath plugin.CatalogPath,
	engineType, schema, table string,
	offset, limit int,
	columns []datatype.FieldInfo,
	dataScope dataprofile.DataScope,
) ([]map[string]interface{}, error) {
	if batchReader == nil {
		if req := strings.TrimSpace(string(dataScope.Kind)); req != "" && req != "all" {
			return nil, fmt.Errorf("table preview conditions are not supported by the resolved table provider")
		}
		session, err := sessionReader.OpenTableReadSession(ctx, connInfo, providerPath, plugin.TableReadSessionOptions{
			Hints: map[string]interface{}{
				plugin.TableReadHintGeometryEncoding: "ewkb",
			},
		})
		if err != nil {
			return nil, err
		}
		defer session.Close(ctx)
		return readSessionPage(ctx, session, offset, limit)
	}
	dialect := commonquery.ForEngine(engineType)
	whereClause, args, err := profilefilter.SQL(dataScope, dialect, "")
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(strings.TrimSpace(engineType), "mysql") && strings.TrimSpace(whereClause) == "" {
		result, err := batchReader.ReadBatch(ctx, connInfo, providerPath, plugin.BatchReadOptions{
			Limit:  limit,
			Offset: int64(offset),
			Hints: map[string]interface{}{
				plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingGeoJSON),
			},
		})
		if err != nil {
			return nil, err
		}
		return result.Rows, nil
	}
	selectExpr := "*"
	query := ""
	if dialect.IsPostgreSQL() {
		primaryKeys := databasePrimaryKeyColumns(columns)
		if len(primaryKeys) > 0 {
			selectExpr = databasePreviewSelectExpr(dialect, columns, databasePreviewSourceAlias)
			query = databasePreviewPostgreSQLPrimaryKeyPageQuery(dialect, selectExpr, schema, table, primaryKeys, whereClause, limit, offset)
		} else {
			selectExpr = databasePreviewSelectExpr(dialect, columns, "")
		}
	}

	if query == "" {
		query = dialect.SelectTableSQL(selectExpr, schema, table, whereClause, "", limit, offset)
	}
	result, err := batchReader.ReadBatch(ctx, connInfo, providerPath, plugin.BatchReadOptions{
		Query: query,
		Args:  args,
	})
	if err != nil {
		return nil, err
	}
	return result.Rows, nil
}

func readSessionPage(ctx context.Context, session plugin.TableReadSession, offset, limit int) ([]map[string]interface{}, error) {
	if session == nil {
		return nil, fmt.Errorf("table read session is required")
	}
	if limit <= 0 {
		return []map[string]interface{}{}, nil
	}
	rows := make([]map[string]interface{}, 0, limit)
	discard := offset
	for len(rows) < limit {
		batch, err := session.ReadBatch(ctx, maxReadBatch(limit, 256))
		if err != nil {
			return nil, err
		}
		if batch == nil || len(batch.Rows) == 0 {
			break
		}
		for _, row := range batch.Rows {
			if discard > 0 {
				discard--
				continue
			}
			rows = append(rows, row)
			if len(rows) == limit {
				break
			}
		}
	}
	return rows, nil
}

func maxReadBatch(left, right int) int {
	if left > right {
		return left
	}
	return right
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

func databasePreviewSelectExpr(dialect commonquery.Dialect, columns []datatype.FieldInfo, tableAlias string) string {
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
			continue
		}
		selectColumns = append(selectColumns, fmt.Sprintf("%s AS %s", columnRef, dialect.QuoteIdentifier(col.Name)))
	}
	return strings.Join(selectColumns, ", ")
}

func databasePreviewColumnRef(dialect commonquery.Dialect, tableAlias, column string) string {
	quotedColumn := dialect.QuoteIdentifier(column)
	if tableAlias == "" {
		return quotedColumn
	}
	return dialect.QuoteIdentifier(tableAlias) + "." + quotedColumn
}

func databasePreviewPostgreSQLPrimaryKeyPageQuery(
	dialect commonquery.Dialect,
	selectExpr, schema, table string,
	primaryKeys []string,
	whereClause string,
	limit, offset int,
) string {
	qualifiedTable := dialect.QualifiedTable(schema, table)
	sourceAlias := dialect.QuoteIdentifier(databasePreviewSourceAlias)
	sourceOrderBy := databasePreviewOrderByClause(dialect, databasePreviewSourceAlias, primaryKeys)
	limitClause := databasePreviewLimitOffsetClause(limit, offset)
	whereSQL := ""
	if strings.TrimSpace(whereClause) != "" {
		whereSQL = " WHERE " + strings.TrimSpace(whereClause)
	}
	if offset <= 0 {
		return fmt.Sprintf("SELECT %s FROM %s AS %s%s ORDER BY %s%s", selectExpr, qualifiedTable, sourceAlias, whereSQL, sourceOrderBy, limitClause)
	}

	keySelect := databasePreviewKeyColumnList(dialect, primaryKeys)
	keyOrderBy := databasePreviewOrderByClause(dialect, "", primaryKeys)
	keyCTE := dialect.QuoteIdentifier(databasePreviewKeyCTEAlias)
	keyAlias := dialect.QuoteIdentifier(databasePreviewKeyAlias)
	joinClause := databasePreviewPrimaryKeyJoinClause(dialect, primaryKeys)
	return fmt.Sprintf(
		"WITH %s AS (SELECT %s FROM %s%s ORDER BY %s%s) SELECT %s FROM %s AS %s JOIN %s AS %s ON %s ORDER BY %s",
		keyCTE,
		keySelect,
		qualifiedTable,
		whereSQL,
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

func databasePreviewKeyColumnList(dialect commonquery.Dialect, primaryKeys []string) string {
	parts := make([]string, 0, len(primaryKeys))
	for _, column := range primaryKeys {
		parts = append(parts, dialect.QuoteIdentifier(column))
	}
	return strings.Join(parts, ", ")
}

func databasePreviewOrderByClause(dialect commonquery.Dialect, tableAlias string, columns []string) string {
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		parts = append(parts, databasePreviewColumnRef(dialect, tableAlias, column))
	}
	return strings.Join(parts, ", ")
}

func databasePreviewPrimaryKeyJoinClause(dialect commonquery.Dialect, primaryKeys []string) string {
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

func databasePreviewWKTExpr(columnRef, dataType string) string {
	if spatial.IsPostGISGeographyType(dataType) {
		return fmt.Sprintf("ST_AsText(%s::geometry)", columnRef)
	}
	return fmt.Sprintf("ST_AsText(%s)", columnRef)
}

func databaseGeometryColumns(spatialInfo *datatype.SpatialInfo, columns []datatype.FieldInfo) []string {
	if spatialInfo != nil {
		if names := spatialInfo.GeometryColumnNames(); len(names) > 0 {
			return names
		}
	}
	geometryColumns := make([]string, 0)
	for _, col := range columns {
		if datatype.IsSpatialFieldType(col.Type) || spatial.IsPostGISSpatialType(databaseFieldNativeType(col)) {
			geometryColumns = append(geometryColumns, col.Name)
		}
	}
	return geometryColumns
}

type tablePreviewCRSContract struct {
	GeometryColumn      string
	SourceSRID          int
	SourceCRS           string
	SourceCRSDefinition *datatype.CRSDefinition
	TransformStatus     string
	PreviewHint         string
}

func tablePreviewSpatialCRSContract(geometryColumns []string, srid int, sourceCRS string, sourceCRSDefinition *datatype.CRSDefinition) tablePreviewCRSContract {
	if len(geometryColumns) == 0 {
		return tablePreviewCRSContract{}
	}

	contract := tablePreviewCRSContract{
		GeometryColumn:      geometryColumns[0],
		SourceSRID:          srid,
		SourceCRS:           strings.TrimSpace(sourceCRS),
		SourceCRSDefinition: sourceCRSDefinition,
		PreviewHint:         "frontend_transform_required",
	}
	if contract.SourceCRS == "" && srid > 0 {
		contract.SourceCRS = datatype.EPSGCRSRef(srid)
	}
	if contract.SourceCRS != "" {
		contract.TransformStatus = "not_transformed"
		if srid == spatial.SRIDWGS84 {
			contract.PreviewHint = "direct_renderable"
		}
		return contract
	}

	contract.TransformStatus = "unknown_crs"
	contract.PreviewHint = "unknown_crs"
	return contract
}

func databasePreviewSourceSRID(columns []datatype.FieldInfo, geometryColumns []string) int {
	if len(geometryColumns) == 0 {
		return 0
	}
	geometrySet := make(map[string]struct{}, len(geometryColumns))
	for _, column := range geometryColumns {
		geometrySet[strings.ToLower(strings.TrimSpace(column))] = struct{}{}
	}
	for _, col := range columns {
		if _, ok := geometrySet[strings.ToLower(strings.TrimSpace(col.Name))]; !ok {
			continue
		}
		if srid := parsePostGISNativeSRID(databaseFieldNativeType(col)); srid > 0 {
			return srid
		}
	}
	return 0
}

func parsePostGISNativeSRID(nativeType string) int {
	trimmed := strings.TrimSpace(nativeType)
	if trimmed == "" {
		return 0
	}
	open := strings.LastIndex(trimmed, "(")
	close := strings.LastIndex(trimmed, ")")
	if open < 0 || close <= open {
		return 0
	}
	parts := strings.Split(trimmed[open+1:close], ",")
	if len(parts) == 0 {
		return 0
	}
	raw := strings.TrimSpace(parts[len(parts)-1])
	if raw == "" {
		return 0
	}
	srid, err := strconv.Atoi(raw)
	if err != nil || srid <= 0 {
		return 0
	}
	return srid
}

func (p *DatabaseTablePreviewProvider) describeDatabaseTable(
	ctx context.Context,
	catalogFactsProvider plugin.CatalogFactsProvider,
	connInfo plugin.ConnectionInfo,
	plug plugin.EnginePlugin,
	providerPath plugin.CatalogPath,
) (*plugin.CatalogFacts, []datatype.FieldInfo, error) {
	if catalogFactsProvider == nil {
		return nil, nil, fmt.Errorf("engine %s does not implement CatalogFactsProvider", plug.Type())
	}
	catalogFacts, err := catalogFactsProvider.DescribeCatalogFacts(ctx, connInfo, providerPath, plugin.CatalogFactsOptions{
		IncludeStatistics: true,
		IncludeIndexes:    true,
	})
	if err != nil {
		return nil, nil, err
	}
	tableInfo := plugin.CatalogFactsTableInfo(catalogFacts)
	if tableInfo == nil {
		return catalogFacts, nil, nil
	}
	return catalogFacts, append([]datatype.FieldInfo(nil), tableInfo.Fields...), nil
}

func (p *DatabaseTablePreviewProvider) resolveTableRowCount(
	req *PreviewRequest,
	catalogFacts *plugin.CatalogFacts,
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
	if catalogFacts != nil {
		if tableInfo := plugin.CatalogFactsTableInfo(catalogFacts); tableInfo != nil && tableInfo.RowCount != nil && *tableInfo.RowCount > 0 {
			return *tableInfo.RowCount
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
) ([]models.ColumnMetadata, []string, int, string, *datatype.CRSDefinition, []float64, error) {
	// 检查 MetaClient 是否可用
	if p.metaClient == nil {
		return nil, nil, 0, "", nil, nil, fmt.Errorf("meta client not available")
	}

	metaClient := p.metaClient
	if tenantID != nil {
		metaClient = metaClient.WithTenantID(*tenantID)
	}

	// 调用 Meta API 获取表的空间元数据（包含字段列表和几何信息）
	spatialMeta, err := metaClient.GetItemSpatialMetadataByCatalogPath(engineID, fmt.Sprintf("%s.%s", schema, table))
	if err != nil {
		return nil, nil, 0, "", nil, nil, fmt.Errorf("failed to get spatial metadata from Meta: %w", err)
	}

	// 如果没有字段信息，返回错误
	if len(spatialMeta.Fields) == 0 {
		return nil, nil, 0, "", nil, nil, fmt.Errorf("no field metadata in Meta")
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
				// geometry_types 已是标准 OGC 写法，例如 MultiPolygon
				geomType := ""
				if len(spatialMeta.GeometryTypes) > 0 {
					geomType = spatialMeta.GeometryTypes[0]
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
	return columnMetadata, geometryColumns, spatialMeta.SRID, spatialMeta.CRSRef, spatialMeta.CRSDefinition, spatialMeta.Extent, nil
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
