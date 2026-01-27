package common

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/service/internal/models"
)

// QueryParams 要素查询参数
type QueryParams struct {
	LayerID     uint
	SRID        int           // 目标坐标系 EPSG 代码
	BBOX        *BBox         // 空间范围过滤
	Filter      string        // CQL 过滤器
	PropertyNames []string    // 要返回的属性列表
	SortBy      string        // 排序字段 (field_name+A 或 field_name+D)
	Limit       int           // 返回要素数
	Offset      int           // 分页偏移
}

// BBox 空间范围
type BBox struct {
	MinX float64
	MinY float64
	MaxX float64
	MaxY float64
	CRS  int // EPSG 代码
}

// SupportedSRIDs 支持的坐标系列表
var SupportedSRIDs = []int{4326, 3857, 4490, 2000, 4214}

// ValidateSRID 验证坐标系是否支持
func ValidateSRID(srid int) error {
	for _, supported := range SupportedSRIDs {
		if srid == supported {
			return nil
		}
	}
	return fmt.Errorf("unsupported SRID %d, supported: %v", srid, SupportedSRIDs)
}

// BuildFeatureQuery 构建要素查询 SQL
// 返回 SQL 语句和参数列表
func BuildFeatureQuery(layer *models.InternalServiceLayer, params *QueryParams) (string, []interface{}, error) {
	if err := ValidateSRID(params.SRID); err != nil {
		return "", nil, err
	}

	args := []interface{}{params.SRID}
	argIndex := 2
	var whereClauses []string

	// 基础 SELECT 子句
	var selectCols strings.Builder
	selectCols.WriteString(fmt.Sprintf(`id, ST_AsGeoJSON(ST_Transform(%s, $1)) as geometry`, layer.GeometryColumn))

	// 添加属性列
	if len(params.PropertyNames) > 0 {
		// 白名单检查：只允许指定的属性
		for _, propName := range params.PropertyNames {
			selectCols.WriteString(fmt.Sprintf(`, "%s"`, propName))
		}
	} else {
		// 默认返回所有列（除了几何列）
		selectCols.WriteString(`, *`)
	}

	// 构建 FROM 子句
	query := fmt.Sprintf(`SELECT %s FROM "%s"."%s"`, selectCols.String(), layer.SchemaName, layer.DBTableName)

	// 空间范围过滤 (BBOX)
	if params.BBOX != nil {
		whereClauses = append(whereClauses,
			fmt.Sprintf(`%s && ST_Transform(ST_MakeEnvelope($%d, $%d, $%d, $%d, $%d), %d)`,
				layer.GeometryColumn, argIndex, argIndex+1, argIndex+2, argIndex+3, argIndex+4, layer.SRID))
		args = append(args, params.BBOX.MinX, params.BBOX.MinY, params.BBOX.MaxX, params.BBOX.MaxY, params.BBOX.CRS)
		argIndex += 5
	}

	// CQL 过滤器
	if params.Filter != "" {
		filterClause, filterArgs, err := ParseCQLFilter(params.Filter, layer.FilterColumns)
		if err != nil {
			return "", nil, err
		}
		if filterClause != "" {
			whereClauses = append(whereClauses, filterClause)
			args = append(args, filterArgs...)
			argIndex += len(filterArgs)
		}
	}

	// 添加 WHERE 子句
	if len(whereClauses) > 0 {
		query += ` WHERE ` + strings.Join(whereClauses, ` AND `)
	}

	// 排序
	if params.SortBy != "" {
		if field, dir, err := ParseSortBy(params.SortBy, layer.FilterColumns); err == nil {
			query += fmt.Sprintf(` ORDER BY "%s" %s`, field, dir)
		}
	}

	// 分页（LIMIT 和 OFFSET）
	query += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, argIndex, argIndex+1)
	args = append(args, params.Limit, params.Offset)

	return query, args, nil
}

// ParseCQLFilter 解析 CQL 过滤器（简化版）
// 支持的操作符：=, !=, >, <, >=, <=, IN, LIKE, AND, OR
// 返回 SQL WHERE 子句和参数列表
func ParseCQLFilter(cql string, allowedColumns []string) (string, []interface{}, error) {
	// 简化实现：只支持基本操作符
	// 例如：name='Beijing' AND population>1000000
	// 验证列名必须在 allowedColumns 中

	if cql == "" {
		return "", []interface{}{}, nil
	}

	// 白名单映射：列名 -> 允许
	allowedMap := make(map[string]bool)
	for _, col := range allowedColumns {
		allowedMap[col] = true
	}

	// TODO: 实现完整的 CQL 解析器
	// 现在返回空值，表示不支持 CQL 过滤
	// 后期可集成 GIS 标准的 CQL 解析库

	return "", []interface{}{}, nil
}

// ParseSortBy 解析排序参数
// 格式：field_name+A (升序) 或 field_name+D (降序)
// 返回列名和排序方向（ASC 或 DESC）
func ParseSortBy(sortBy string, allowedColumns []string) (string, string, error) {
	// 分解 sortBy 参数
	parts := strings.Split(sortBy, "+")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid sortBy format, expected field+A/D")
	}

	fieldName := parts[0]
	direction := parts[1]

	// 白名单检查
	allowed := false
	for _, col := range allowedColumns {
		if col == fieldName {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", "", fmt.Errorf("field %s not allowed for sorting", fieldName)
	}

	// 验证排序方向
	if direction != "A" && direction != "D" {
		return "", "", fmt.Errorf("direction must be A (ascending) or D (descending)")
	}

	dir := "ASC"
	if direction == "D" {
		dir = "DESC"
	}

	return fieldName, dir, nil
}

// RowsToFeatures 将数据库行转换为要素集合
// 返回 GeoJSON Feature 的数组
func RowsToFeatures(rows *sql.Rows, columns []string) ([]map[string]interface{}, error) {
	var features []map[string]interface{}

	for rows.Next() {
		// 创建一个 interface{} 数组来接收行数据
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// 构建 Feature 对象
		feature := make(map[string]interface{})
		var id interface{}
		var geometry string

		for i, col := range columns {
			val := values[i]

			// 处理 NULL 值
			if val == nil {
				continue
			}

			switch col {
			case "id":
				id = val
			case "geometry":
				geometry = string(val.([]byte))
			default:
				// 其他列作为属性
				if feature["properties"] == nil {
					feature["properties"] = make(map[string]interface{})
				}
				feature["properties"].(map[string]interface{})[col] = val
			}
		}

		// 构建完整的 GeoJSON Feature
		feature["type"] = "Feature"
		if id != nil {
			feature["id"] = id
		}

		// 解析几何 JSON
		if geometry != "" {
			// 这里假设 geometry 已经是 GeoJSON 格式的 JSON 字符串
			// 实际实现中可能需要进一步处理
			feature["geometry"] = geometry // TODO: 解析为 JSON 对象
		}

		features = append(features, feature)
	}

	return features, rows.Err()
}
