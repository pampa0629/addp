package wfs

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/ogc/common"
	"github.com/addp/service/internal/repository"
	"gorm.io/gorm"
)

// GetFeatureHandler WFS GetFeature 请求处理器
type GetFeatureHandler struct {
	repo *repository.InternalServiceRepository
	db   *gorm.DB
}

// NewGetFeatureHandler 创建新的 GetFeature 处理器
func NewGetFeatureHandler(repo *repository.InternalServiceRepository, db *gorm.DB) *GetFeatureHandler {
	return &GetFeatureHandler{
		repo: repo,
		db:   db,
	}
}

// GetFeatureRequest WFS GetFeature 请求参数
type GetFeatureRequest struct {
	Service       string
	Version       string
	Request       string
	TypeName      string    // 图层名称
	OutputFormat  string    // 输出格式：application/json (GeoJSON) 或 application/gml+xml
	Count         int       // maxFeatures 别名
	MaxFeatures   int       // 最大返回要素数
	StartIndex    int       // 分页起始索引
	BBox          *common.BBox // 空间范围
	SrsName       string    // 坐标系 URN
	PropertyName  string    // 属性列表（逗号分隔）
	SortBy        string    // 排序字段
	CQL_Filter    string    // CQL 过滤器
}

// ParseGetFeatureRequest 从 URL 查询参数解析 GetFeature 请求
func ParseGetFeatureRequest(query url.Values, layer *models.InternalServiceLayer) (*GetFeatureRequest, error) {
	req := &GetFeatureRequest{
		Service:      query.Get("service"),
		Version:      query.Get("version"),
		Request:      query.Get("request"),
		TypeName:     query.Get("typeName"),
		OutputFormat: query.Get("outputFormat"),
		CQL_Filter:   query.Get("cql_filter"),
		PropertyName: query.Get("propertyName"),
		SortBy:       query.Get("sortBy"),
		SrsName:      query.Get("srsName"),
	}

	// 解析 maxFeatures / count
	maxFeatures := 100 // 默认值
	if mf := query.Get("maxFeatures"); mf != "" {
		if parsed, err := strconv.Atoi(mf); err == nil {
			maxFeatures = parsed
		}
	}
	if count := query.Get("count"); count != "" {
		if parsed, err := strconv.Atoi(count); err == nil {
			maxFeatures = parsed
		}
	}
	// 限制最大值
	if maxFeatures > 10000 {
		maxFeatures = 10000
	}
	req.MaxFeatures = maxFeatures
	req.Count = maxFeatures

	// 解析 startIndex
	startIndex := 0
	if si := query.Get("startIndex"); si != "" {
		if parsed, err := strconv.Atoi(si); err == nil {
			startIndex = parsed
		}
	}
	req.StartIndex = startIndex

	// 解析 BBOX
	if bboxStr := query.Get("bbox"); bboxStr != "" {
		if bbox, err := parseBBox(bboxStr); err == nil {
			req.BBox = bbox
		}
	}

	// 解析坐标系（从 srsName 中提取 EPSG 代码）
	srid := layer.SRID
	if req.SrsName != "" {
		if epsg, err := extractEPSGFromURN(req.SrsName); err == nil {
			srid = epsg
		}
	}

	// 解析输出格式
	if req.OutputFormat == "" {
		req.OutputFormat = "application/json" // 默认 GeoJSON
	}

	return req, nil
}

// ExecuteGetFeature 执行 GetFeature 查询
func (h *GetFeatureHandler) ExecuteGetFeature(serviceName string, layerName string, req *GetFeatureRequest, db *sql.DB) (interface{}, error) {
	// 1. 查询服务和图层
	service, err := h.repo.GetByName(serviceName)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	layer, err := h.repo.GetLayerByName(service.ID, layerName)
	if err != nil {
		return nil, fmt.Errorf("layer not found: %w", err)
	}

	if !layer.Enabled {
		return nil, fmt.Errorf("layer is disabled")
	}

	// 2. 构建查询参数
	queryParams := &common.QueryParams{
		LayerID:  layer.ID,
		SRID:     service.DefaultSRID,
		BBOX:     req.BBox,
		Filter:   req.CQL_Filter,
		SortBy:   req.SortBy,
		Limit:    req.MaxFeatures,
		Offset:   req.StartIndex,
	}

	// 处理属性列表
	if req.PropertyName != "" {
		queryParams.PropertyNames = strings.Split(req.PropertyName, ",")
		// 白名单过滤
		var filtered []string
		for _, prop := range queryParams.PropertyNames {
			prop = strings.TrimSpace(prop)
			// 检查是否在 FilterColumns 中
			allowed := false
			for _, fc := range layer.FilterColumns {
				if fc == prop {
					allowed = true
					break
				}
			}
			if allowed || prop == "id" {
				filtered = append(filtered, prop)
			}
		}
		queryParams.PropertyNames = filtered
	}

	// 3. 构建并执行 SQL 查询
	sqlQuery, args, err := common.BuildFeatureQuery(layer, queryParams)
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Columns()

	// 4. 获取列名
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// 5. 序列化结果
	switch req.OutputFormat {
	case "application/json", "application/geo+json":
		// GeoJSON 格式
		geojson, err := common.RowsToGeoJSON(rows, columns)
		if err != nil {
			return nil, fmt.Errorf("failed to convert to GeoJSON: %w", err)
		}
		return geojson, nil

	case "application/gml+xml":
		// GML 格式
		gml, err := RowsToGML(rows, columns, layer)
		if err != nil {
			return nil, fmt.Errorf("failed to convert to GML: %w", err)
		}
		return gml, nil

	default:
		return nil, fmt.Errorf("unsupported output format: %s", req.OutputFormat)
	}
}

// parseBBox 解析 BBOX 字符串
// 格式：minx,miny,maxx,maxy[,crs]
func parseBBox(bboxStr string) (*common.BBox, error) {
	parts := strings.Split(bboxStr, ",")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid bbox format")
	}

	minX, _ := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	minY, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	maxX, _ := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	maxY, _ := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)

	bbox := &common.BBox{
		MinX: minX,
		MinY: minY,
		MaxX: maxX,
		MaxY: maxY,
		CRS:  4326, // 默认 WGS84
	}

	// 如果提供了 CRS，解析它
	if len(parts) > 4 {
		crsStr := strings.TrimSpace(parts[4])
		if epsg, err := extractEPSGFromURN(crsStr); err == nil {
			bbox.CRS = epsg
		}
	}

	return bbox, nil
}

// extractEPSGFromURN 从 OGC CRS URN 中提取 EPSG 代码
// 格式：urn:ogc:def:crs:EPSG::4326
func extractEPSGFromURN(urn string) (int, error) {
	parts := strings.Split(urn, ":")
	if len(parts) > 0 {
		if code, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return code, nil
		}
	}
	return 0, fmt.Errorf("invalid CRS URN: %s", urn)
}

// RowsToGML 将行数据转换为 GML FeatureCollection（简化版）
func RowsToGML(rows *sql.Rows, columns []string, layer *models.InternalServiceLayer) (string, error) {
	// TODO: 实现完整的 GML 序列化
	// 这是一个简化的实现，实际 GML 生成需要更复杂的逻辑

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<wfs:FeatureCollection xmlns:wfs="http://www.opengis.net/wfs/2.0" xmlns:gml="http://www.opengis.net/gml/3.2">`)

	count := 0
	for rows.Next() {
		count++
		// TODO: 扫描并转换为 GML
		sb.WriteString(`<!-- TODO: GML feature member -->`)
	}

	sb.WriteString(`</wfs:FeatureCollection>`)
	return sb.String(), rows.Err()
}
