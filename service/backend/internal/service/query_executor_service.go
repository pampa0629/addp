package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/duckdb"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/sqldialect"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"gorm.io/gorm"
)

// QueryExecutorService 查询执行服务
type QueryExecutorService struct {
	queryRepo    *repository.QueryServiceRepository
	systemClient *client.SystemClient
	metaClient   *client.MetaClient
}

// NewQueryExecutorService 创建查询执行服务
func NewQueryExecutorService(
	queryRepo *repository.QueryServiceRepository,
	systemClient *client.SystemClient,
	metaClient *client.MetaClient,
) *QueryExecutorService {
	return &QueryExecutorService{
		queryRepo:    queryRepo,
		systemClient: systemClient,
		metaClient:   metaClient,
	}
}

// QueryParams 查询参数
type QueryParams struct {
	// 通用参数
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
	Format   string `form:"format"` // json, csv, geojson

	// Table 模式参数
	Filter  string `form:"filter"`  // WHERE 条件
	Fields  string `form:"fields"`  // 返回字段列表（逗号分隔）
	OrderBy string `form:"orderBy"` // 排序
}

// QueryResult 查询结果
type QueryResult struct {
	Data       interface{} `json:"data"`
	Pagination *Pagination `json:"pagination,omitempty"`
}

// Pagination 分页信息
type Pagination struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

// ExecuteQuery 执行查询
func (s *QueryExecutorService) ExecuteQuery(
	ctx context.Context,
	service *models.QueryService,
	params *QueryParams,
) (*QueryResult, error) {
	// 设置分页参数
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 50
	}
	maxFeatures := service.MaxFeatures
	if maxFeatures <= 0 {
		maxFeatures = 1000
	}
	if params.PageSize > maxFeatures {
		params.PageSize = maxFeatures
	}

	// sql 模式 + engine_id IS NULL → DuckDB 联邦查询执行路径
	if service.IsDuckDBSQL() {
		return s.executeDuckDBSQLQuery(ctx, service, params)
	}

	// table 模式 + MinIO/S3 引擎 → DuckDB 执行路径
	if service.ConfigType == "table" && service.IsLakeTable() {
		return s.executeLakeTableQuery(ctx, service, params)
	}

	// 1. 获取存储引擎信息
	engine, err := s.systemClient.GetEngine(service.GetEngineID())
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 转换为 common models
	commonEngine := &commonModels.Engine{
		ID:             engine.ID,
		EngineType:     engine.EngineType,
		ConnectionInfo: commonModels.ConnectionInfo(engine.ConnectionInfo),
	}

	// 2. 获取或创建连接池
	gormDB, err := dbbridge.GetOrCreatePool(commonEngine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get connection pool: %w", err)
	}

	// 3. 根据配置类型执行查询
	var rows []map[string]interface{}
	var total int64

	if service.ConfigType == "table" {
		rows, total, err = s.executeTableQuery(ctx, gormDB, engine.EngineType, service, params)
	} else {
		rows, total, err = s.executeSQLQuery(ctx, gormDB, service, params)
	}

	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Data: rows,
		Pagination: &Pagination{
			Page:       params.Page,
			PageSize:   params.PageSize,
			Total:      total,
			TotalPages: int((total + int64(params.PageSize) - 1) / int64(params.PageSize)),
		},
	}, nil
}

// executeLakeTableQuery 通过 DuckDB 执行湖表查询（MinIO/S3 上的 Parquet）
func (s *QueryExecutorService) executeLakeTableQuery(
	ctx context.Context,
	service *models.QueryService,
	params *QueryParams,
) (*QueryResult, error) {
	// 获取引擎连接信息
	engine, err := s.systemClient.GetEngine(service.GetEngineID())
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	commonEngine := commonModels.Engine{
		ID:             engine.ID,
		Name:           engine.Name,
		EngineType:     engine.EngineType,
		ConnectionInfo: commonModels.ConnectionInfo(engine.ConnectionInfo),
	}

	// 从 ConnectionInfo 中获取 bucket；MinIO 引擎没有 bucket 字段，用 schema_name 作为 bucket
	bucket := ""
	if b, ok := engine.ConnectionInfo["bucket"].(string); ok {
		bucket = b
	}
	schemaForPath := service.SchemaName
	if bucket == "" && service.SchemaName != "" {
		// MinIO 引擎：schema_name 即 bucket 名，路径中不再重复 schema
		bucket = service.SchemaName
		schemaForPath = ""
	}

	// 构建 S3 路径
	s3Path := duckdb.BuildLakeTableS3Path(bucket, schemaForPath, service.TargetTable, service.GetLakeMode())

	// 打开 DuckDB 连接
	db, err := duckdb.OpenDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	conn, err := db.Conn(execCtx)
	if err != nil {
		return nil, fmt.Errorf("获取 DuckDB 连接失败: %w", err)
	}
	defer conn.Close()

	// 挂载 S3 引擎
	if err := duckdb.MountObjectStorage(execCtx, conn, commonEngine); err != nil {
		return nil, fmt.Errorf("挂载 S3 引擎失败: %w", err)
	}

	// 构建 SELECT 字段
	selectFields := "*"
	if params.Fields != "" {
		selectFields = params.Fields
	}

	// 构建 WHERE 条件
	whereClause := ""
	if params.Filter != "" {
		whereClause = " WHERE " + params.Filter
	}

	// 构建 ORDER BY
	orderByClause := ""
	if params.OrderBy != "" {
		orderByClause = " ORDER BY " + params.OrderBy
	}

	// 获取总数
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet('%s')%s", s3Path, whereClause)
	var total int64
	row := conn.QueryRowContext(execCtx, countSQL)
	if err := row.Scan(&total); err != nil {
		return nil, fmt.Errorf("获取总数失败: %w", err)
	}

	// 分页查询
	offset := (params.Page - 1) * params.PageSize
	dataSQL := fmt.Sprintf("SELECT %s FROM read_parquet('%s')%s%s LIMIT %d OFFSET %d",
		selectFields, s3Path, whereClause, orderByClause, params.PageSize, offset)

	result, err := duckdb.ExecuteQuery(execCtx, conn, dataSQL)
	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Data: result.Rows,
		Pagination: &Pagination{
			Page:       params.Page,
			PageSize:   params.PageSize,
			Total:      total,
			TotalPages: int((total + int64(params.PageSize) - 1) / int64(params.PageSize)),
		},
	}, nil
}

// executeDuckDBSQLQuery 通过 DuckDB 执行联邦 SQL 查询（engine_id IS NULL 时）
func (s *QueryExecutorService) executeDuckDBSQLQuery(
	ctx context.Context,
	service *models.QueryService,
	params *QueryParams,
) (*QueryResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	session, err := duckdb.PrepareFederatedQuery(execCtx, service.TenantID, service.SqlQuery, s.systemClient, s.metaClient)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	// 获取总数
	countSQL := sqldialect.CountSubquerySQL(session.RewrittenSQL, "_q")
	var total int64
	row := session.Conn.QueryRowContext(execCtx, countSQL)
	if err := row.Scan(&total); err != nil {
		return nil, fmt.Errorf("获取总数失败: %w", err)
	}

	// 分页查询
	offset := (params.Page - 1) * params.PageSize
	paginatedSQL := sqldialect.PaginateQuerySQL(session.RewrittenSQL, params.PageSize, offset)
	result, err := duckdb.ExecuteQuery(execCtx, session.Conn, paginatedSQL)
	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Data: result.Rows,
		Pagination: &Pagination{
			Page:       params.Page,
			PageSize:   params.PageSize,
			Total:      total,
			TotalPages: int((total + int64(params.PageSize) - 1) / int64(params.PageSize)),
		},
	}, nil
}

// executeTableQuery 执行表配置模式的查询
func (s *QueryExecutorService) executeTableQuery(
	ctx context.Context,
	db *gorm.DB,
	engineType string,
	service *models.QueryService,
	params *QueryParams,
) ([]map[string]interface{}, int64, error) {
	schema := service.SchemaName
	table := service.TargetTable
	dialect := sqldialect.ForEngine(engineType)

	// 1. 构建 SELECT 字段列表
	selectFields := "*"
	geomColumn := ""

	if params.Fields != "" {
		// 用户指定了字段列表
		fields := strings.Split(params.Fields, ",")
		quotedFields := make([]string, len(fields))
		for i, field := range fields {
			field = strings.TrimSpace(field)
			quotedFields[i] = dialect.QuoteIdentifier(field)
		}
		selectFields = strings.Join(quotedFields, ", ")
	} else {
		// 使用默认字段
		defaultFields := service.GetDefaultFields()
		if len(defaultFields) > 0 {
			quotedFields := make([]string, len(defaultFields))
			for i, field := range defaultFields {
				quotedFields[i] = dialect.QuoteIdentifier(field)
			}
			selectFields = strings.Join(quotedFields, ", ")
		}
	}

	// 2. 处理空间字段（如果有）
	if service.HasGeometry() {
		geomColumn = service.GetGeometryColumn()
		if dialect.IsPostgreSQL() && geomColumn != "" {
			// 如果是 SELECT *，需要先获取所有列名，然后显式构建字段列表
			if strings.TrimSpace(selectFields) == "*" {
				selectFields = s.buildSelectFieldsWithGeometry(ctx, db, schema, table, geomColumn, engineType)
			} else {
				// 替换几何列为 ST_AsGeoJSON 格式
				selectFields = s.replaceGeometryColumn(selectFields, geomColumn, engineType)
			}
		}
	}

	// 4. 构建 WHERE 条件
	var whereClause string
	if params.Filter != "" {
		// 验证过滤字段（如果配置了 filterable_fields）
		filterableFields := service.GetFilterableFields()
		if len(filterableFields) > 0 {
			// TODO: 验证 filter 中使用的字段是否在 filterable_fields 中
			// 当前简化处理：直接使用用户提供的 filter
		}
		whereClause = " WHERE " + params.Filter
	}

	// 5. 构建 ORDER BY
	var orderByClause string
	if params.OrderBy != "" {
		orderByClause = " ORDER BY " + params.OrderBy
	}

	// 6. 获取总数（不带 LIMIT）
	countQuery := dialect.CountTableSQL(schema, table, params.Filter)
	var total int64
	if err := db.WithContext(ctx).Raw(countQuery).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// 7. 执行分页查询
	offset := (params.Page - 1) * params.PageSize
	limit := params.PageSize

	query := dialect.SelectTableSQL(selectFields, schema, table, whereClause, orderByClause, limit, offset)

	var rows []map[string]interface{}
	if err := db.WithContext(ctx).Raw(query).Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to query data: %w", err)
	}

	return rows, total, nil
}

// executeSQLQuery 执行 SQL 配置模式的查询
func (s *QueryExecutorService) executeSQLQuery(
	ctx context.Context,
	db *gorm.DB,
	service *models.QueryService,
	params *QueryParams,
) ([]map[string]interface{}, int64, error) {
	sqlQuery := service.SqlQuery

	// 1. 获取总数（使用子查询）
	countQuery := sqldialect.CountSubquerySQL(sqlQuery, "subquery")
	var total int64
	if err := db.WithContext(ctx).Raw(countQuery).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// 2. 执行分页查询
	offset := (params.Page - 1) * params.PageSize
	limit := params.PageSize

	paginatedQuery := sqldialect.PaginateQuerySQL(sqlQuery, limit, offset)

	var rows []map[string]interface{}
	if err := db.WithContext(ctx).Raw(paginatedQuery).Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to query data: %w", err)
	}

	return rows, total, nil
}

// replaceGeometryColumn 替换几何列为空间函数
func (s *QueryExecutorService) replaceGeometryColumn(selectFields, geomColumn, engineType string) string {
	dialect := sqldialect.ForEngine(engineType)
	if !dialect.IsPostgreSQL() {
		return selectFields
	}

	// 如果是 SELECT *，需要特殊处理
	if strings.TrimSpace(selectFields) == "*" {
		return selectFields // 保持 SELECT *，让数据库返回原始几何数据
	}

	// 替换几何列为 ST_AsGeoJSON
	quotedGeomColumn := dialect.QuoteIdentifier(geomColumn)
	replacement := sqldialect.PostGISGeoJSONSelectExpression(geomColumn)

	// 简单替换（实际应该更智能地解析 SQL）
	selectFields = strings.ReplaceAll(selectFields, quotedGeomColumn, replacement)

	return selectFields
}

// FormatAsCSV 格式化为 CSV
func (s *QueryExecutorService) FormatAsCSV(data []map[string]interface{}) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	// 获取列名（从第一行）
	var columns []string
	for col := range data[0] {
		columns = append(columns, col)
	}

	// 构建 CSV
	var buf strings.Builder
	writer := csv.NewWriter(&buf)

	// 写入表头
	if err := writer.Write(columns); err != nil {
		return nil, err
	}

	// 写入数据行
	for _, row := range data {
		record := make([]string, len(columns))
		for i, col := range columns {
			value := row[col]
			record[i] = fmt.Sprintf("%v", value)
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}

	return []byte(buf.String()), nil
}

// FormatAsGeoJSON 格式化为 GeoJSON
func (s *QueryExecutorService) FormatAsGeoJSON(
	data []map[string]interface{},
	service *models.QueryService,
) ([]byte, error) {
	if !service.HasGeometry() {
		return nil, fmt.Errorf("service does not have geometry column")
	}

	geomColumn := service.GetGeometryColumn()

	// 构建 GeoJSON FeatureCollection
	features := make([]map[string]interface{}, len(data))

	for i, row := range data {
		// 提取几何数据
		geomData, ok := row[geomColumn]
		if !ok {
			return nil, fmt.Errorf("geometry column %s not found in row", geomColumn)
		}

		// 解析 GeoJSON 几何（假设已经是 GeoJSON 格式字符串）
		var geometry map[string]interface{}
		if geomStr, ok := geomData.(string); ok {
			if err := json.Unmarshal([]byte(geomStr), &geometry); err != nil {
				return nil, fmt.Errorf("failed to parse geometry: %w", err)
			}
		} else {
			return nil, fmt.Errorf("geometry data is not string")
		}

		// 构建属性（排除几何列）
		properties := make(map[string]interface{})
		for key, value := range row {
			if key != geomColumn {
				properties[key] = value
			}
		}

		// 构建 Feature
		features[i] = map[string]interface{}{
			"type":       "Feature",
			"geometry":   geometry,
			"properties": properties,
		}
	}

	featureCollection := map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
	}

	return json.Marshal(featureCollection)
}

// buildSelectFieldsWithGeometry 为包含几何列的表构建显式字段列表
// 用于替代 SELECT * 以确保几何列被正确转换为 GeoJSON
func (s *QueryExecutorService) buildSelectFieldsWithGeometry(
	ctx context.Context,
	db *gorm.DB,
	schema string,
	table string,
	geomColumn string,
	engineType string,
) string {
	// 查询表的所有列名
	var columns []struct {
		ColumnName string `gorm:"column:column_name"`
	}

	query := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position
	`

	if err := db.WithContext(ctx).Raw(query, schema, table).Scan(&columns).Error; err != nil {
		// 查询失败，返回 *
		return "*"
	}

	if len(columns) == 0 {
		return "*"
	}

	// 构建字段列表
	dialect := sqldialect.ForEngine(engineType)
	fieldList := make([]string, len(columns))
	for i, col := range columns {
		quotedCol := dialect.QuoteIdentifier(col.ColumnName)

		// 如果是几何列，使用 ST_AsGeoJSON 转换
		if col.ColumnName == geomColumn {
			fieldList[i] = sqldialect.PostGISGeoJSONSelectExpression(col.ColumnName)
		} else {
			fieldList[i] = quotedCol
		}
	}

	return strings.Join(fieldList, ", ")
}
