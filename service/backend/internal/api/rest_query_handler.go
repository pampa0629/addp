package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/logger"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RestQueryHandler REST 查询 API 处理器（简化版，不遵循 OGC 标准）
type RestQueryHandler struct {
	repo *repository.InternalServiceRepository
	db   *gorm.DB
}

// NewRestQueryHandler 创建新的 REST 查询处理器
func NewRestQueryHandler(repo *repository.InternalServiceRepository, db *gorm.DB) *RestQueryHandler {
	return &RestQueryHandler{
		repo: repo,
		db:   db,
	}
}

// Query 处理查询请求
// GET /api/query/:serviceName/:layerName
func (h *RestQueryHandler) Query(c *gin.Context) {
	serviceName := c.Param("serviceName")
	layerName := c.Param("layerName")

	// 验证服务和图层
	service, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	if !service.EnabledRestQuery {
		c.JSON(http.StatusForbidden, gin.H{"error": "REST Query API is not enabled for this service"})
		return
	}

	layer, err := h.repo.GetLayerByName(service.ID, layerName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Layer not found"})
		return
	}

	if !layer.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "Layer is disabled"})
		return
	}

	// 解析查询参数
	filter := c.Query("filter")        // WHERE条件：population > 1000000
	fields := c.Query("fields")        // 字段列表：id,name,population
	orderBy := c.Query("orderBy")      // 排序：name ASC, population DESC
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	format := c.DefaultQuery("format", "json")  // json, csv, geojson

	// 参数验证
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}

	maxFeatures := service.MaxFeatures
	if layer.MaxFeatures != nil && *layer.MaxFeatures > 0 {
		maxFeatures = *layer.MaxFeatures
	}
	if pageSize > maxFeatures {
		pageSize = maxFeatures
	}

	// 解析字段列表
	var selectFields []string
	if fields != "" {
		selectFields = strings.Split(fields, ",")
		for i, f := range selectFields {
			selectFields[i] = strings.TrimSpace(f)
		}
	}

	// 构建 SQL 查询
	query, args, err := h.buildQuery(layer, filter, selectFields, orderBy, page, pageSize)
	if err != nil {
		logger.L().Error("Failed to build query", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid query parameters", "details": err.Error()})
		return
	}

	// 执行查询
	rows, err := h.db.Raw(query, args...).Rows()
	if err != nil {
		logger.L().Error("Failed to execute query", "error", err, "sql", query)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query data"})
		return
	}
	defer rows.Close()

	// 获取列名
	columns, err := rows.Columns()
	if err != nil {
		logger.L().Error("Failed to get columns", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process query results"})
		return
	}

	// 读取数据
	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			logger.L().Error("Failed to scan row", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process query results"})
			return
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if val != nil {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		logger.L().Error("Query error", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process query results"})
		return
	}

	// 根据格式返回数据
	switch format {
	case "csv":
		h.respondCSV(c, columns, results)
	case "geojson":
		h.respondGeoJSON(c, layer, results)
	default:
		h.respondJSON(c, results, page, pageSize)
	}

	logger.L().Debug("REST Query returned",
		"service", serviceName,
		"layer", layerName,
		"page", page,
		"pageSize", pageSize,
		"format", format,
		"count", len(results))
}

// buildQuery 构建 SQL 查询
func (h *RestQueryHandler) buildQuery(
	layer *models.InternalServiceLayer,
	filter string,
	selectFields []string,
	orderBy string,
	page, pageSize int,
) (string, []interface{}, error) {
	var args []interface{}
	argIndex := 1

	// 构建 SELECT 子句
	var selectCols string
	if len(selectFields) > 0 {
		// 白名单检查
		allowedMap := make(map[string]bool)
		for _, col := range layer.FilterColumns {
			allowedMap[col] = true
		}
		allowedMap["id"] = true
		if layer.GeometryColumn != "" {
			allowedMap[layer.GeometryColumn] = true
		}

		var validFields []string
		for _, field := range selectFields {
			if allowedMap[field] {
				validFields = append(validFields, fmt.Sprintf(`"%s"`, field))
			}
		}

		if len(validFields) == 0 {
			selectCols = "id"
		} else {
			selectCols = strings.Join(validFields, ", ")
		}
	} else {
		// 默认返回所有列
		selectCols = "*"
	}

	// 构建 FROM 子句
	query := fmt.Sprintf(`SELECT %s FROM "%s"."%s"`, selectCols, layer.SchemaName, layer.DBTableName)

	// WHERE 子句（基础过滤）
	if filter != "" {
		// 简单的 SQL 注入防护
		dangerousKeywords := []string{"DROP", "DELETE", "INSERT", "UPDATE", "EXEC", "EXECUTE", "--", ";"}
		upperFilter := strings.ToUpper(filter)
		for _, keyword := range dangerousKeywords {
			if strings.Contains(upperFilter, keyword) {
				return "", nil, fmt.Errorf("filter contains forbidden keyword: %s", keyword)
			}
		}

		query += fmt.Sprintf(" WHERE %s", filter)
	}

	// ORDER BY 子句
	if orderBy != "" {
		// 简单验证（防止 SQL 注入）
		if !strings.Contains(strings.ToUpper(orderBy), "DROP") &&
			!strings.Contains(strings.ToUpper(orderBy), "DELETE") {
			query += fmt.Sprintf(" ORDER BY %s", orderBy)
		}
	}

	// 分页
	offset := (page - 1) * pageSize
	query += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, argIndex, argIndex+1)
	args = append(args, pageSize, offset)

	return query, args, nil
}

// respondJSON 返回 JSON 格式
func (h *RestQueryHandler) respondJSON(c *gin.Context, results []map[string]interface{}, page, pageSize int) {
	response := gin.H{
		"page":      page,
		"page_size": pageSize,
		"count":     len(results),
		"data":      results,
	}
	c.JSON(http.StatusOK, response)
}

// respondCSV 返回 CSV 格式
func (h *RestQueryHandler) respondCSV(c *gin.Context, columns []string, results []map[string]interface{}) {
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment; filename=query_result.csv")

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// 写入表头
	if err := writer.Write(columns); err != nil {
		logger.L().Error("Failed to write CSV header", "error", err)
		return
	}

	// 写入数据
	for _, row := range results {
		record := make([]string, len(columns))
		for i, col := range columns {
			if val, ok := row[col]; ok && val != nil {
				record[i] = fmt.Sprintf("%v", val)
			}
		}
		if err := writer.Write(record); err != nil {
			logger.L().Error("Failed to write CSV row", "error", err)
			return
		}
	}
}

// respondGeoJSON 返回 GeoJSON 格式（如果有几何列）
func (h *RestQueryHandler) respondGeoJSON(c *gin.Context, layer *models.InternalServiceLayer, results []map[string]interface{}) {
	if layer.GeometryColumn == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Layer does not have geometry column, cannot return GeoJSON"})
		return
	}

	features := make([]gin.H, 0, len(results))
	for _, row := range results {
		var id interface{}
		if idVal, ok := row["id"]; ok {
			id = idVal
		}

		// 分离属性和几何
		properties := make(map[string]interface{})
		var geometryJSON interface{}

		for key, val := range row {
			if key == "id" {
				continue
			} else if key == layer.GeometryColumn {
				// 如果几何列是 JSON 字符串，解析它
				if strVal, ok := val.(string); ok {
					var geom interface{}
					if err := json.Unmarshal([]byte(strVal), &geom); err == nil {
						geometryJSON = geom
					}
				} else if bytesVal, ok := val.([]byte); ok {
					var geom interface{}
					if err := json.Unmarshal(bytesVal, &geom); err == nil {
						geometryJSON = geom
					}
				}
			} else {
				properties[key] = val
			}
		}

		feature := gin.H{
			"type":       "Feature",
			"id":         id,
			"properties": properties,
			"geometry":   geometryJSON,
		}
		features = append(features, feature)
	}

	response := gin.H{
		"type":     "FeatureCollection",
		"features": features,
	}

	c.JSON(http.StatusOK, response)
}
