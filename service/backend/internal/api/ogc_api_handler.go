package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
	"github.com/addp/common/spatial"
	"github.com/addp/service/internal/ogc/wfs"
	"github.com/addp/service/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// OGCAPIHandler OGC API Features 服务处理器
type OGCAPIHandler struct {
	repo *repository.InternalServiceRepository
	db   *gorm.DB
}

// NewOGCAPIHandler 创建新的 OGC API Features 处理器
func NewOGCAPIHandler(repo *repository.InternalServiceRepository, db *gorm.DB) *OGCAPIHandler {
	return &OGCAPIHandler{
		repo: repo,
		db:   db,
	}
}

// LandingPage OGC API Features 登陆页面
func (h *OGCAPIHandler) LandingPage(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 查询服务
	service, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// 检查是否为空间服务
	if !service.IsSpatialService() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OGC API Features only supports spatial services"})
		return
	}

	// 检查 OGC API 协议是否启用
	if !service.IsProtocolEnabled("ogc_api") {
		c.JSON(http.StatusForbidden, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	// 构建响应
	baseURL := "http://" + c.Request.Host + "/ogc/api/" + serviceName

	response := gin.H{
		"title":       service.Title,
		"description": service.Abstract,
		"links": []gin.H{
			{
				"href": baseURL + "/",
				"rel":  "self",
				"type": "application/json",
				"title": "This document",
			},
			{
				"href": baseURL + "/conformance",
				"rel":  "conformance",
				"type": "application/json",
				"title": "OGC API conformance classes",
			},
			{
				"href": baseURL + "/collections",
				"rel":  "data",
				"type": "application/json",
				"title": "Collections",
			},
		},
	}

	c.JSON(http.StatusOK, response)
}

// Conformance 返回符合性声明
func (h *OGCAPIHandler) Conformance(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 验证服务是否存在且启用 OGC API
	service, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	if !service.IsSpatialService() || !service.IsProtocolEnabled("ogc_api") {
		c.JSON(http.StatusForbidden, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	// 构建符合性声明
	conformsTo := []string{
		"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core",
		"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/oas30",
		"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/geojson",
	}

	c.JSON(http.StatusOK, gin.H{
		"conformsTo": conformsTo,
	})
}

// Collections 返回集合列表
func (h *OGCAPIHandler) Collections(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 查询服务和图层
	service, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	if !service.IsSpatialService() || !service.IsProtocolEnabled("ogc_api") {
		c.JSON(http.StatusForbidden, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	baseURL := "http://" + c.Request.Host + "/ogc/api/" + serviceName

	// 构建集合列表
	var collections []gin.H
	for _, layer := range service.Layers {
		if !layer.Enabled {
			continue
		}

		collection := gin.H{
			"id":          layer.LayerName,
			"title":       layer.Title,
			"description": layer.Abstract,
			"links": []gin.H{
				{
					"href": fmt.Sprintf("%s/collections/%s", baseURL, layer.LayerName),
					"rel":  "alternate",
					"type": "application/json",
				},
				{
					"href": fmt.Sprintf("%s/collections/%s/items", baseURL, layer.LayerName),
					"rel":  "items",
					"type": "application/json",
				},
			},
		}

		// 添加范围信息
		if layer.Extent4326 != nil {
			// Extent4326 存储为 JSONB，但实际是数组格式 [minLng, minLat, maxLng, maxLat]
			// 先 marshal 再 unmarshal 以正确解析
			if bboxBytes, err := json.Marshal(layer.Extent4326); err == nil {
				var bbox []float64
				if err := json.Unmarshal(bboxBytes, &bbox); err == nil && len(bbox) == 4 {
					collection["extent"] = gin.H{
						"spatial": gin.H{
							"bbox": []interface{}{bbox},
							"crs":  "http://www.opengis.net/def/crs/OGC/1.3/CRS84",
						},
					}
				}
			}
		}

		collections = append(collections, collection)
	}

	response := gin.H{
		"links": []gin.H{
			{
				"href": baseURL + "/collections",
				"rel":  "self",
				"type": "application/json",
			},
		},
		"collections": collections,
	}

	c.JSON(http.StatusOK, response)
}

// GetCollection 返回单个集合信息
func (h *OGCAPIHandler) GetCollection(c *gin.Context) {
	serviceName := c.Param("serviceName")
	collectionID := c.Param("collectionID")

	// 查询服务和图层
	service, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	if !service.IsSpatialService() || !service.IsProtocolEnabled("ogc_api") {
		c.JSON(http.StatusForbidden, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	// 查询图层
	layer, err := h.repo.GetLayerByName(service.ID, collectionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Collection not found"})
		return
	}

	if !layer.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "Collection is disabled"})
		return
	}

	baseURL := "http://" + c.Request.Host + "/ogc/api/" + serviceName

	// 构建集合详情
	collection := gin.H{
		"id":          layer.LayerName,
		"title":       layer.Title,
		"description": layer.Abstract,
		"itemType":    "feature",
		"crs": []string{
			"http://www.opengis.net/gml/srs/epsg.xml#4326",
			"http://www.opengis.net/gml/srs/epsg.xml#3857",
		},
		"links": []gin.H{
			{
				"href": fmt.Sprintf("%s/collections/%s", baseURL, layer.LayerName),
				"rel":  "self",
				"type": "application/json",
			},
			{
				"href": fmt.Sprintf("%s/collections/%s/items", baseURL, layer.LayerName),
				"rel":  "items",
				"type": "application/json",
			},
		},
	}

	// 添加范围信息
	if layer.Extent4326 != nil {
		// Extent4326 存储为 JSONB，但实际是数组格式 [minLng, minLat, maxLng, maxLat]
		if bboxBytes, err := json.Marshal(layer.Extent4326); err == nil {
			var bbox []float64
			if err := json.Unmarshal(bboxBytes, &bbox); err == nil && len(bbox) == 4 {
				collection["extent"] = gin.H{
					"spatial": gin.H{
						"bbox": []interface{}{bbox},
						"crs":  "http://www.opengis.net/def/crs/OGC/1.3/CRS84",
					},
				}
			}
		}
	}

	c.JSON(http.StatusOK, collection)
}

// Items 返回要素集合
func (h *OGCAPIHandler) Items(c *gin.Context) {
	serviceName := c.Param("serviceName")
	collectionID := c.Param("collectionID")

	// 查询服务和图层
	service, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	if !service.IsSpatialService() || !service.IsProtocolEnabled("ogc_api") {
		c.JSON(http.StatusForbidden, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	// 查询图层
	layer, err := h.repo.GetLayerByName(service.ID, collectionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Collection not found"})
		return
	}

	if !layer.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "Collection is disabled"})
		return
	}

	// 检查是否有几何列（非空间数据不支持 OGC API Features）
	if layer.GeometryColumn == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Collection does not contain spatial data",
			"hint":  "Use REST Query API for non-spatial data",
		})
		return
	}

	// 解析查询参数
	limit := 10
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 10000 {
			limit = parsed
		}
	}

	// 应用图层或服务的最大要素数限制
	maxFeatures := service.MaxFeatures
	if layer.MaxFeatures != nil && *layer.MaxFeatures > 0 {
		maxFeatures = *layer.MaxFeatures
	}
	if limit > maxFeatures {
		limit = maxFeatures
	}

	offset := 0
	if off := c.Query("offset"); off != "" {
		if parsed, err := strconv.Atoi(off); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// 解析 bbox 参数
	var bbox *spatial.BBox
	if bboxStr := c.Query("bbox"); bboxStr != "" {
		bbox, err = spatial.ParseBBox(bboxStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid bbox parameter", "details": err.Error()})
			return
		}
	}

	// 解析 filter 参数（CQL2）
	filter := c.Query("filter")

	// 解析坐标系参数（默认 4326）
	srid := service.DefaultSRID
	if crsParam := c.Query("crs"); crsParam != "" {
		// 支持格式：http://www.opengis.net/def/crs/EPSG/0/4326 或直接 4326
		if parsed, err := strconv.Atoi(crsParam); err == nil {
			srid = parsed
		}
	}

	// 构建查询参数
	queryParams := &wfs.QueryParams{
		LayerID: layer.ID,
		SRID:    srid,
		BBOX:    bbox,
		Filter:  filter,
		Limit:   limit,
		Offset:  offset,
	}

	// 构建查询 SQL
	sql, args, err := wfs.BuildFeatureQuery(layer, queryParams)
	if err != nil {
		logger.L().Error("Failed to build feature query", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid query parameters", "details": err.Error()})
		return
	}

	// 获取数据库连接（需要从 EngineID 获取）
	// TODO: 通过 EngineID 获取实际的数据库连接
	// 现在简化为使用系统默认的 DB 连接
	db := h.db

	// 执行查询
	rows, err := db.Raw(sql, args...).Rows()
	if err != nil {
		logger.L().Error("Failed to execute feature query", "error", err, "sql", sql)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query features"})
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

	// 转换为 GeoJSON Features
	features, err := wfs.RowsToFeatures(rows, columns)
	if err != nil {
		logger.L().Error("Failed to convert rows to features", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process features"})
		return
	}

	// 处理 geometry 字段中的 __rawJSON 标记
	processedFeatures := make([]map[string]interface{}, 0, len(features))
	for _, feature := range features {
		if geom, ok := feature["geometry"].(map[string]interface{}); ok {
			if rawJSON, ok := geom["__rawJSON"].(string); ok {
				// 解析 GeoJSON geometry
				var geometryObj interface{}
				if err := json.Unmarshal([]byte(rawJSON), &geometryObj); err == nil {
					feature["geometry"] = geometryObj
				}
			}
		}
		processedFeatures = append(processedFeatures, feature)
	}

	baseURL := "http://" + c.Request.Host + "/ogc/api/" + serviceName

	// 构建响应
	response := gin.H{
		"type":            "FeatureCollection",
		"features":        processedFeatures,
		"numberReturned":  len(processedFeatures),
		"timeStamp":       nil, // TODO: 添加时间戳
	}

	// 添加链接
	links := []gin.H{
		{
			"href":  c.Request.URL.String(),
			"rel":   "self",
			"type":  "application/geo+json",
			"title": "This document",
		},
		{
			"href":  fmt.Sprintf("%s/collections/%s", baseURL, collectionID),
			"rel":   "collection",
			"type":  "application/json",
			"title": "The collection",
		},
	}

	// 添加分页链接
	if len(processedFeatures) == limit {
		// 有下一页
		nextOffset := offset + limit
		nextURL := fmt.Sprintf("%s/collections/%s/items?limit=%d&offset=%d", baseURL, collectionID, limit, nextOffset)
		if bbox != nil {
			nextURL += fmt.Sprintf("&bbox=%.6f,%.6f,%.6f,%.6f", bbox.MinX, bbox.MinY, bbox.MaxX, bbox.MaxY)
		}
		if filter != "" {
			nextURL += "&filter=" + filter
		}
		links = append(links, gin.H{
			"href":  nextURL,
			"rel":   "next",
			"type":  "application/geo+json",
			"title": "Next page",
		})
	}

	if offset > 0 {
		// 有上一页
		prevOffset := offset - limit
		if prevOffset < 0 {
			prevOffset = 0
		}
		prevURL := fmt.Sprintf("%s/collections/%s/items?limit=%d&offset=%d", baseURL, collectionID, limit, prevOffset)
		if bbox != nil {
			prevURL += fmt.Sprintf("&bbox=%.6f,%.6f,%.6f,%.6f", bbox.MinX, bbox.MinY, bbox.MaxX, bbox.MaxY)
		}
		if filter != "" {
			prevURL += "&filter=" + filter
		}
		links = append(links, gin.H{
			"href":  prevURL,
			"rel":   "prev",
			"type":  "application/geo+json",
			"title": "Previous page",
		})
	}

	response["links"] = links

	logger.L().Debug("OGC API Features Items returned",
		"service", serviceName,
		"collection", collectionID,
		"limit", limit,
		"offset", offset,
		"returned", len(processedFeatures))

	c.JSON(http.StatusOK, response)
}

// GetItem 返回单个要素
func (h *OGCAPIHandler) GetItem(c *gin.Context) {
	serviceName := c.Param("serviceName")
	collectionID := c.Param("collectionID")
	featureID := c.Param("featureID")

	// 查询服务和图层
	service, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	if !service.IsSpatialService() || !service.IsProtocolEnabled("ogc_api") {
		c.JSON(http.StatusForbidden, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	// 查询图层
	layer, err := h.repo.GetLayerByName(service.ID, collectionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Collection not found"})
		return
	}

	if !layer.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "Collection is disabled"})
		return
	}

	// 检查是否有几何列
	if layer.GeometryColumn == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Collection does not contain spatial data",
		})
		return
	}

	// 解析坐标系参数
	srid := service.DefaultSRID
	if crsParam := c.Query("crs"); crsParam != "" {
		if parsed, err := strconv.Atoi(crsParam); err == nil {
			srid = parsed
		}
	}

	// 构建查询 SQL（查询单个要素）
	sql := fmt.Sprintf(`
		SELECT id, ST_AsGeoJSON(ST_Transform("%s", $1)) as geometry, *
		FROM "%s"."%s"
		WHERE id = $2
		LIMIT 1
	`, layer.GeometryColumn, layer.SchemaName, layer.DBTableName)

	args := []interface{}{srid, featureID}

	// 执行查询
	db := h.db
	rows, err := db.Raw(sql, args...).Rows()
	if err != nil {
		logger.L().Error("Failed to execute feature query", "error", err, "sql", sql)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query feature"})
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

	// 转换为 GeoJSON Features
	features, err := wfs.RowsToFeatures(rows, columns)
	if err != nil {
		logger.L().Error("Failed to convert rows to features", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process feature"})
		return
	}

	if len(features) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
		return
	}

	feature := features[0]

	// 处理 geometry 字段中的 __rawJSON 标记
	if geom, ok := feature["geometry"].(map[string]interface{}); ok {
		if rawJSON, ok := geom["__rawJSON"].(string); ok {
			// 解析 GeoJSON geometry
			var geometryObj interface{}
			if err := json.Unmarshal([]byte(rawJSON), &geometryObj); err == nil {
				feature["geometry"] = geometryObj
			}
		}
	}

	baseURL := "http://" + c.Request.Host + "/ogc/api/" + serviceName

	// 添加链接
	feature["links"] = []gin.H{
		{
			"href":  fmt.Sprintf("%s/collections/%s/items/%s", baseURL, collectionID, featureID),
			"rel":   "self",
			"type":  "application/geo+json",
			"title": "This document",
		},
		{
			"href":  fmt.Sprintf("%s/collections/%s", baseURL, collectionID),
			"rel":   "collection",
			"type":  "application/json",
			"title": "The collection",
		},
		{
			"href":  fmt.Sprintf("%s/collections/%s/items", baseURL, collectionID),
			"rel":   "items",
			"type":  "application/geo+json",
			"title": "Items in collection",
		},
	}

	logger.L().Debug("OGC API Features GetItem returned",
		"service", serviceName,
		"collection", collectionID,
		"feature", featureID)

	c.JSON(http.StatusOK, feature)
}
