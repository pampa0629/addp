package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"gorm.io/gorm"
)

// QueryExecutorService 查询执行服务
type QueryExecutorService struct {
	queryRepo   *repository.QueryServiceRepository
	systemClient *client.SystemClient
}

// NewQueryExecutorService 创建查询执行服务
func NewQueryExecutorService(
	queryRepo *repository.QueryServiceRepository,
	systemClient *client.SystemClient,
) *QueryExecutorService {
	return &QueryExecutorService{
		queryRepo:   queryRepo,
		systemClient: systemClient,
	}
}

// QueryParams 查询参数
type QueryParams struct {
	// 通用参数
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
	Format   string `form:"format"` // json, csv, geojson

	// Table 模式参数
	Filter  string `form:"filter"`   // WHERE 条件
	Fields  string `form:"fields"`   // 返回字段列表（逗号分隔）
	OrderBy string `form:"orderBy"`  // 排序
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
	// 1. 获取存储引擎信息
	engine, err := s.systemClient.GetEngine(service.EngineID)
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

	// 3. 设置分页参数
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

	// 4. 根据配置类型执行查询
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

	// 5. 格式化输出
	result := &QueryResult{
		Data: rows,
		Pagination: &Pagination{
			Page:       params.Page,
			PageSize:   params.PageSize,
			Total:      total,
			TotalPages: int((total + int64(params.PageSize) - 1) / int64(params.PageSize)),
		},
	}

	return result, nil
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

	// 1. 构建 SELECT 字段列表
	selectFields := "*"
	if params.Fields != "" {
		// 用户指定了字段列表
		fields := strings.Split(params.Fields, ",")
		quotedFields := make([]string, len(fields))
		for i, field := range fields {
			field = strings.TrimSpace(field)
			quotedFields[i] = s.quoteIdentifier(engineType, field)
		}
		selectFields = strings.Join(quotedFields, ", ")
	} else {
		// 使用默认字段
		defaultFields := service.GetDefaultFields()
		if len(defaultFields) > 0 {
			quotedFields := make([]string, len(defaultFields))
			for i, field := range defaultFields {
				quotedFields[i] = s.quoteIdentifier(engineType, field)
			}
			selectFields = strings.Join(quotedFields, ", ")
		}
	}

	// 2. 处理空间字段（如果有）
	if service.HasGeometry() {
		geomColumn := service.GetGeometryColumn()
		if strings.ToLower(engineType) == "postgresql" && geomColumn != "" {
			// 替换几何列为 ST_AsGeoJSON 格式
			selectFields = s.replaceGeometryColumn(selectFields, geomColumn, engineType)
		}
	}

	// 3. 构建查询表达式
	tableName := s.quoteIdentifier(engineType, schema) + "." + s.quoteIdentifier(engineType, table)

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
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", tableName, whereClause)
	var total int64
	if err := db.WithContext(ctx).Raw(countQuery).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// 7. 执行分页查询
	offset := (params.Page - 1) * params.PageSize
	limit := params.PageSize

	query := fmt.Sprintf("SELECT %s FROM %s%s%s LIMIT %d OFFSET %d",
		selectFields, tableName, whereClause, orderByClause, limit, offset)

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
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM (%s) AS subquery", sqlQuery)
	var total int64
	if err := db.WithContext(ctx).Raw(countQuery).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// 2. 执行分页查询
	offset := (params.Page - 1) * params.PageSize
	limit := params.PageSize

	paginatedQuery := fmt.Sprintf("%s LIMIT %d OFFSET %d", sqlQuery, limit, offset)

	var rows []map[string]interface{}
	if err := db.WithContext(ctx).Raw(paginatedQuery).Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to query data: %w", err)
	}

	return rows, total, nil
}

// quoteIdentifier 根据数据库类型引用标识符
func (s *QueryExecutorService) quoteIdentifier(engineType, identifier string) string {
	engineTypeLower := strings.ToLower(engineType)
	switch engineTypeLower {
	case "mysql", "doris", "clickhouse":
		return "`" + identifier + "`"
	default: // postgresql 等
		return "\"" + identifier + "\""
	}
}

// replaceGeometryColumn 替换几何列为空间函数
func (s *QueryExecutorService) replaceGeometryColumn(selectFields, geomColumn, engineType string) string {
	if strings.ToLower(engineType) != "postgresql" {
		return selectFields
	}

	// 如果是 SELECT *，需要特殊处理
	if strings.TrimSpace(selectFields) == "*" {
		return selectFields // 保持 SELECT *，让数据库返回原始几何数据
	}

	// 替换几何列为 ST_AsGeoJSON
	quotedGeomColumn := s.quoteIdentifier(engineType, geomColumn)
	replacement := fmt.Sprintf("ST_AsGeoJSON(%s) AS %s", quotedGeomColumn, quotedGeomColumn)

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
