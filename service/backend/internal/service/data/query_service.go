package data

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/service/internal/models"
)

type QueryService struct {
	systemClient *commonClient.SystemClient
	metaClient   *commonClient.MetaClient
}

type tableResourceRef struct {
	Locator    string
	EngineID   uint
	SchemaName string
	TableName  string
	ItemID     uint
}

var errInvalidResourceLocator = errors.New("invalid resource locator")

// IsInvalidResourceLocatorError 判断错误是否来自资源定位输入本身。
func IsInvalidResourceLocatorError(err error) bool {
	return errors.Is(err, errInvalidResourceLocator)
}

func NewQueryService(systemClient *commonClient.SystemClient, metaClient *commonClient.MetaClient) *QueryService {
	return &QueryService{
		systemClient: systemClient,
		metaClient:   metaClient,
	}
}

// Query 执行数据查询（数据服务核心功能）
func (s *QueryService) Query(ctx context.Context, req *models.DataQueryRequest) (*models.DataQueryResponse, error) {
	startTime := time.Now()
	tableRef, err := tableRefFromLocator(req.Locator)
	if err != nil {
		return nil, err
	}

	// 1. 获取引擎配置
	engine, err := s.systemClient.GetEngine(tableRef.EngineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 2. 获取数据库连接池
	db, err := dbbridge.GetOrCreatePool(engine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	// 3. 获取列信息
	metadata, err := describeTabularItemFacts(ctx, engine, tableRef.SchemaName, tableRef.TableName, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}
	columnsInfo := columnInfosFromMetadata(metadata)

	if len(columnsInfo) == 0 {
		return nil, fmt.Errorf("table %s.%s not found or has no columns", tableRef.SchemaName, tableRef.TableName)
	}

	// 4. 构建 SELECT 子句
	var selectCols []string
	if len(req.Columns) > 0 {
		// 用户指定列
		selectCols = req.Columns
	} else {
		// 查询所有列（需要处理大小写混合的列名）
		for _, col := range columnsInfo {
			// 检查列名是否包含大小写字母，如果有则加双引号
			if needsQuoting(col.ColumnName) {
				selectCols = append(selectCols, fmt.Sprintf(`"%s"`, col.ColumnName))
			} else {
				selectCols = append(selectCols, col.ColumnName)
			}
		}
	}

	// 5. 处理几何字段（如果需要）
	selectClause := strings.Join(selectCols, ", ")
	if req.Geometry && req.GeometryType != "" {
		// 查找几何列并转换格式
		for i, col := range selectCols {
			// 移除可能的双引号来匹配列信息
			colNameToMatch := strings.Trim(col, `"`)
			for _, colInfo := range columnsInfo {
				if colInfo.ColumnName == colNameToMatch && strings.Contains(strings.ToLower(colInfo.DataType), "geometry") {
					// 根据请求的几何格式转换（使用带引号的列名）
					quotedCol := col
					if !strings.HasPrefix(col, `"`) {
						quotedCol = fmt.Sprintf(`"%s"`, col)
					}
					switch strings.ToLower(req.GeometryType) {
					case "geojson":
						selectCols[i] = fmt.Sprintf("ST_AsGeoJSON(%s) as %s", quotedCol, quotedCol)
					case "wkt":
						selectCols[i] = fmt.Sprintf("ST_AsText(%s) as %s", quotedCol, quotedCol)
					case "wkb":
						selectCols[i] = fmt.Sprintf("ST_AsBinary(%s) as %s", quotedCol, quotedCol)
					}
					break
				}
			}
		}
		selectClause = strings.Join(selectCols, ", ")
	}

	// 6. 构建完整 SQL 查询
	tableName := fmt.Sprintf("%s.%s", tableRef.SchemaName, tableRef.TableName)
	sqlQuery := fmt.Sprintf("SELECT %s FROM %s", selectClause, tableName)

	// 添加 WHERE 条件
	if req.Filter != "" {
		sqlQuery += " WHERE " + req.Filter
	}

	// 添加 ORDER BY
	if req.OrderBy != "" {
		sqlQuery += " ORDER BY " + req.OrderBy
	}

	// 7. 查询总记录数（用于分页）
	var totalCount int64
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	if req.Filter != "" {
		countSQL += " WHERE " + req.Filter
	}
	if err := db.WithContext(ctx).Raw(countSQL).Scan(&totalCount).Error; err != nil {
		// 检查是否是列名不存在的错误
		if strings.Contains(err.Error(), "column") && strings.Contains(err.Error(), "does not exist") {
			return nil, fmt.Errorf("列名不存在或大小写不匹配。PostgreSQL 列名通常为小写，请检查过滤条件中的列名: %w", err)
		}
		return nil, fmt.Errorf("查询记录数失败: %w", err)
	}

	// 8. 添加分页
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 1000 {
		pageSize = 1000 // 最大限制
	}

	offset := (page - 1) * pageSize
	sqlQuery += fmt.Sprintf(" LIMIT %d OFFSET %d", pageSize, offset)

	log.Printf("[DataService] Executing query: %s", sqlQuery)

	// 9. 执行查询
	rows, err := db.WithContext(ctx).Raw(sqlQuery).Rows()
	if err != nil {
		// 检查是否是列名不存在的错误
		if strings.Contains(err.Error(), "column") && strings.Contains(err.Error(), "does not exist") {
			return nil, fmt.Errorf("列名不存在或大小写不匹配。PostgreSQL 列名通常为小写，请检查列名、过滤条件或排序字段: %w", err)
		}
		return nil, fmt.Errorf("执行查询失败: %w", err)
	}
	defer rows.Close()

	// 10. 获取列名
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// 11. 扫描结果
	var results [][]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// 处理特殊类型
		row := make([]interface{}, len(columns))
		for i, val := range values {
			if b, ok := val.([]byte); ok {
				row[i] = string(b)
			} else {
				row[i] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	// 12. 构建响应
	responseColumns := make([]models.ColumnInfo, len(columns))
	for i, colName := range columns {
		responseColumns[i] = models.ColumnInfo{
			Name:     colName,
			Type:     "VARCHAR", // 简化处理，实际可以从 columnsInfo 中获取
			Nullable: true,
		}
		// 匹配详细类型信息
		for _, colInfo := range columnsInfo {
			if colInfo.ColumnName == colName {
				responseColumns[i].Type = colInfo.DataType
				responseColumns[i].Nullable = colInfo.IsNullable
				break
			}
		}
	}

	duration := time.Since(startTime).Milliseconds()
	log.Printf("[DataService] Query completed in %dms, returned %d rows (total: %d)", duration, len(results), totalCount)

	return &models.DataQueryResponse{
		Columns:    responseColumns,
		Rows:       results,
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
		HasMore:    int64(page*pageSize) < totalCount,
	}, nil
}

// Aggregate 执行聚合查询
func (s *QueryService) Aggregate(ctx context.Context, req *models.AggregationRequest) (*models.AggregationResponse, error) {
	startTime := time.Now()
	tableRef, err := tableRefFromLocator(req.Locator)
	if err != nil {
		return nil, err
	}

	// 1. 获取引擎配置
	engine, err := s.systemClient.GetEngine(tableRef.EngineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 2. 获取数据库连接池
	db, err := dbbridge.GetOrCreatePool(engine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	// 3. 构建聚合查询 SQL
	var selectParts []string
	var columnInfos []models.ColumnInfo

	// 添加 GROUP BY 字段
	if len(req.GroupBy) > 0 {
		for _, col := range req.GroupBy {
			selectParts = append(selectParts, col)
			columnInfos = append(columnInfos, models.ColumnInfo{
				Name:     col,
				Type:     "VARCHAR",
				Nullable: true,
			})
		}
	}

	// 添加聚合函数
	for _, agg := range req.Aggregates {
		alias := agg.Alias
		if alias == "" {
			alias = fmt.Sprintf("%s_%s", strings.ToLower(agg.Function), agg.Column)
		}

		aggExpr := fmt.Sprintf("%s(%s) AS %s", strings.ToUpper(agg.Function), agg.Column, alias)
		selectParts = append(selectParts, aggExpr)

		columnInfos = append(columnInfos, models.ColumnInfo{
			Name:     alias,
			Type:     "NUMERIC",
			Nullable: false,
		})
	}

	if len(selectParts) == 0 {
		return nil, fmt.Errorf("no aggregation specified")
	}

	tableName := fmt.Sprintf("%s.%s", tableRef.SchemaName, tableRef.TableName)
	sqlQuery := fmt.Sprintf("SELECT %s FROM %s", strings.Join(selectParts, ", "), tableName)

	// 添加 WHERE 条件
	if req.Filter != "" {
		sqlQuery += " WHERE " + req.Filter
	}

	// 添加 GROUP BY
	if len(req.GroupBy) > 0 {
		sqlQuery += " GROUP BY " + strings.Join(req.GroupBy, ", ")
	}

	// 添加 HAVING 条件
	if req.Having != "" {
		sqlQuery += " HAVING " + req.Having
	}

	// 添加 ORDER BY
	if req.OrderBy != "" {
		sqlQuery += " ORDER BY " + req.OrderBy
	}

	// 添加 LIMIT
	if req.Limit > 0 {
		if req.Limit > 10000 {
			req.Limit = 10000 // 最大限制
		}
		sqlQuery += fmt.Sprintf(" LIMIT %d", req.Limit)
	}

	log.Printf("[DataService] Executing aggregation: %s", sqlQuery)

	// 4. 执行查询
	rows, err := db.WithContext(ctx).Raw(sqlQuery).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to execute aggregation: %w", err)
	}
	defer rows.Close()

	// 5. 扫描结果
	var results [][]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columnInfos))
		valuePtrs := make([]interface{}, len(columnInfos))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make([]interface{}, len(columnInfos))
		for i, val := range values {
			if b, ok := val.([]byte); ok {
				row[i] = string(b)
			} else {
				row[i] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	duration := time.Since(startTime).Milliseconds()
	log.Printf("[DataService] Aggregation completed in %dms, returned %d rows", duration, len(results))

	return &models.AggregationResponse{
		Columns: columnInfos,
		Rows:    results,
		Count:   len(results),
	}, nil
}

// GetTableStructure 获取表结构信息（列名、类型等）
func (s *QueryService) GetTableStructure(ctx context.Context, locator string) ([]models.ColumnInfo, error) {
	tableRef, err := tableRefFromLocator(locator)
	if err != nil {
		return nil, err
	}

	// 1. 获取引擎配置
	engine, err := s.systemClient.GetEngine(tableRef.EngineID)
	if err != nil {
		return nil, fmt.Errorf("获取引擎失败: %w", err)
	}

	// 2. 获取列信息：dbbridge 内部走 EngineCatalogFactsProvider。
	metadata, err := describeTabularItemFacts(ctx, engine, tableRef.SchemaName, tableRef.TableName, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("表 %s.%s 不存在", tableRef.SchemaName, tableRef.TableName)
		}
		return nil, fmt.Errorf("获取列信息失败: %w", err)
	}
	columnsInfo := columnInfosFromMetadata(metadata)

	if len(columnsInfo) == 0 {
		return nil, fmt.Errorf("表 %s.%s 没有列或不存在", tableRef.SchemaName, tableRef.TableName)
	}

	// 4. 转换为响应格式
	result := make([]models.ColumnInfo, len(columnsInfo))
	for i, col := range columnsInfo {
		result[i] = models.ColumnInfo{
			Name:     col.ColumnName,
			Type:     col.DataType,
			Nullable: col.IsNullable,
		}
	}

	log.Printf("[DataService] 获取表结构成功: %s.%s (%d 列)", tableRef.SchemaName, tableRef.TableName, len(result))
	return result, nil
}

func tableRefFromLocator(locator string) (*tableResourceRef, error) {
	if strings.TrimSpace(locator) == "" {
		return nil, fmt.Errorf("%w: locator is required", errInvalidResourceLocator)
	}
	loc, err := resourcetree.ParseURI(locator)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidResourceLocator, err)
	}
	if loc.Type != resourcetree.TypeTable {
		return nil, fmt.Errorf("%w: locator must reference a table resource, got %s", errInvalidResourceLocator, loc.Type)
	}
	if loc.ItemID == nil || *loc.ItemID == 0 {
		return nil, fmt.Errorf("%w: locator must include item_id", errInvalidResourceLocator)
	}
	if len(loc.Path) < 2 {
		return nil, fmt.Errorf("%w: locator path must include schema and table", errInvalidResourceLocator)
	}
	return &tableResourceRef{
		Locator:    locator,
		EngineID:   loc.EngineID,
		SchemaName: loc.Path[len(loc.Path)-2],
		TableName:  loc.Path[len(loc.Path)-1],
		ItemID:     *loc.ItemID,
	}, nil
}

// needsQuoting 判断 PostgreSQL 列名是否需要双引号
// 如果列名包含大写字母、特殊字符或是关键字，则需要加双引号
func needsQuoting(columnName string) bool {
	// 检查是否包含大写字母
	for _, c := range columnName {
		if c >= 'A' && c <= 'Z' {
			return true
		}
	}
	return false
}

type metadataColumnInfo struct {
	ColumnName   string
	DataType     string
	IsNullable   bool
	IsPrimaryKey bool
	Comment      string
}

func columnInfosFromMetadata(metadata *plugin.EngineCatalogFacts) []metadataColumnInfo {
	if metadata == nil {
		return nil
	}
	tableInfo := plugin.EngineCatalogFactsTableInfo(metadata)
	if tableInfo == nil {
		return nil
	}
	fields := tableInfo.Fields
	columns := make([]metadataColumnInfo, 0, len(fields))
	for _, field := range fields {
		dataType := field.NativeType
		if dataType == "" {
			dataType = string(field.Type)
		}
		columns = append(columns, metadataColumnInfo{
			ColumnName:   field.Name,
			DataType:     dataType,
			IsNullable:   field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
		})
	}
	return columns
}

func describeTabularItemFacts(ctx context.Context, engine *commonModels.Engine, namespace, table string, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	model, err := dbbridge.EngineCatalogModel(engine.EngineType)
	if err != nil {
		return nil, err
	}
	branchLevel, ok := plugin.EngineCatalogFirstBusinessBranch(model)
	if !ok || branchLevel.Term == "" {
		return nil, fmt.Errorf("catalog model for %s has no first business branch", engine.EngineType)
	}
	path := plugin.TabularItemPath(engine.ID, branchLevel.Term, namespace, table)
	return dbbridge.DescribeEngineCatalogFacts(ctx, engine, path, opts)
}

// Close 关闭所有数据库连接
func (s *QueryService) Close() error {
	// 连接池由 dbbridge 统一管理，无需手动关闭
	return nil
}
