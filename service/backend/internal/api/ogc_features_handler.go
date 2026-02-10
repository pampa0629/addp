package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/addp/service/internal/models"
	svc "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// OGCFeaturesHandler 处理 OGC API Features 标准端点
type OGCFeaturesHandler struct {
	svc         *svc.QueryServiceService
	executorSvc *svc.QueryExecutorService
	baseURL     string
}

// NewOGCFeaturesHandler 创建新的 OGC Features 处理器
func NewOGCFeaturesHandler(s *svc.QueryServiceService, executorSvc *svc.QueryExecutorService, baseURL string) *OGCFeaturesHandler {
	return &OGCFeaturesHandler{
		svc:         s,
		executorSvc: executorSvc,
		baseURL:     baseURL,
	}
}

// GetLandingPage 获取 OGC API Features Landing Page
// GET /ogc/features/:serviceName
func (h *OGCFeaturesHandler) GetLandingPage(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 查找服务
	service, err := h.getAndValidateService(c, serviceName)
	if err != nil {
		return // 错误已在函数内处理
	}

	// 检查 OGC Features 是否启用
	if !service.IsOGCFeaturesEnabled() {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	// 构建 Landing Page 响应
	baseURL := fmt.Sprintf("%s/ogc/features/%s", h.baseURL, serviceName)
	landingPage := gin.H{
		"title":       service.Title,
		"description": service.Description,
		"links": []gin.H{
			{
				"href":  baseURL,
				"rel":   "self",
				"type":  "application/json",
				"title": "This document",
			},
			{
				"href":  fmt.Sprintf("%s/conformance", baseURL),
				"rel":   "conformance",
				"type":  "application/json",
				"title": "Conformance Declaration",
			},
			{
				"href":  fmt.Sprintf("%s/collections", baseURL),
				"rel":   "data",
				"type":  "application/json",
				"title": "Collections",
			},
			{
				"href":  "https://www.opengis.net/doc/IS/ogcapi-features-1/1.0",
				"rel":   "service-desc",
				"type":  "text/html",
				"title": "OGC API Features specification",
			},
		},
	}

	c.JSON(http.StatusOK, landingPage)
}

// GetConformance 获取 OGC API Features Conformance 声明
// GET /ogc/features/:serviceName/conformance
func (h *OGCFeaturesHandler) GetConformance(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 查找服务
	service, err := h.getAndValidateService(c, serviceName)
	if err != nil {
		return // 错误已在函数内处理
	}

	// 检查 OGC Features 是否启用
	if !service.IsOGCFeaturesEnabled() {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	// 符合的规范类别
	conformsTo := []string{
		"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core",
		"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/oas30",
		"http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/geojson",
	}

	c.JSON(http.StatusOK, gin.H{
		"conformsTo": conformsTo,
	})
}

// GetCollections 获取 Collections 列表
// GET /ogc/features/:serviceName/collections
func (h *OGCFeaturesHandler) GetCollections(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 查找服务
	service, err := h.getAndValidateService(c, serviceName)
	if err != nil {
		return // 错误已在函数内处理
	}

	// 检查 OGC Features 是否启用
	if !service.IsOGCFeaturesEnabled() {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	// 构建 collection 信息
	baseURL := fmt.Sprintf("%s/ogc/features/%s", h.baseURL, serviceName)
	collectionID := service.ServiceName

	collection := gin.H{
		"id":          collectionID,
		"title":       service.Title,
		"description": service.Description,
		"links": []gin.H{
			{
				"href":  fmt.Sprintf("%s/collections/%s/items", baseURL, collectionID),
				"rel":   "items",
				"type":  "application/geo+json",
				"title": "Items",
			},
		},
	}

	// 如果有空间信息，添加 extent
	if geomConfig, ok := service.DataConfig["geometry"].(map[string]interface{}); ok {
		if hasGeometry, ok := geomConfig["has_geometry"].(bool); ok && hasGeometry {
			if extent, ok := geomConfig["extent"].(map[string]interface{}); ok {
				collection["extent"] = gin.H{
					"spatial": gin.H{
						"bbox": [][]float64{
							{
								extent["minX"].(float64),
								extent["minY"].(float64),
								extent["maxX"].(float64),
								extent["maxY"].(float64),
							},
						},
						"crs": fmt.Sprintf("http://www.opengis.net/def/crs/EPSG/0/%v", geomConfig["srid"]),
					},
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"links": []gin.H{
			{
				"href":  fmt.Sprintf("%s/collections", baseURL),
				"rel":   "self",
				"type":  "application/json",
				"title": "This document",
			},
		},
		"collections": []gin.H{collection},
	})
}

// GetItems 获取 Collection 中的 Items（即查询数据）
// GET /ogc/features/:serviceName/collections/:collectionId/items
func (h *OGCFeaturesHandler) GetItems(c *gin.Context) {
	serviceName := c.Param("serviceName")
	collectionID := c.Param("collectionId")

	// 查找服务
	service, err := h.getAndValidateService(c, serviceName)
	if err != nil {
		return // 错误已在函数内处理
	}

	// 验证 collectionId（目前一个服务对应一个 collection）
	if collectionID != service.ServiceName {
		c.JSON(http.StatusNotFound, gin.H{"error": "Collection not found"})
		return
	}

	// 检查 OGC Features 是否启用
	if !service.IsOGCFeaturesEnabled() {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	// 解析查询参数（OGC 标准参数）
	var params svc.QueryParams

	// 分页参数（OGC 标准使用 limit 和 offset）
	if limitStr := c.Query("limit"); limitStr != "" {
		var limit int
		fmt.Sscanf(limitStr, "%d", &limit)
		if limit > 0 && limit <= 10000 {
			params.PageSize = limit
		}
	}
	if params.PageSize == 0 {
		params.PageSize = 10 // OGC 默认 10 条
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		var offset int
		fmt.Sscanf(offsetStr, "%d", &offset)
		if offset >= 0 {
			params.Page = (offset / params.PageSize) + 1
		}
	}
	if params.Page == 0 {
		params.Page = 1
	}

	// bbox 参数（空间过滤）
	if bbox := c.Query("bbox"); bbox != "" {
		params.Filter = fmt.Sprintf("bbox:%s", bbox)
	}

	// 其他标准参数
	params.Format = "geojson" // OGC Features 默认返回 GeoJSON

	// 执行查询
	result, err := h.executorSvc.ExecuteQuery(c.Request.Context(), service, &params)
	if err != nil {
		log.Printf("[OGCFeatures] Query execution failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query execution failed: " + err.Error()})
		return
	}

	// 转换为 GeoJSON
	rows, ok := result.Data.([]map[string]interface{})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid data format"})
		return
	}

	geojsonData, err := h.executorSvc.FormatAsGeoJSON(rows, service)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to format as GeoJSON: " + err.Error()})
		return
	}

	// OGC Features 响应需要包含 links
	baseURL := fmt.Sprintf("%s/ogc/features/%s/collections/%s/items", h.baseURL, serviceName, collectionID)

	// 解析 GeoJSON 并添加 links
	var geojson map[string]interface{}
	if err := json.Unmarshal(geojsonData, &geojson); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse GeoJSON"})
		return
	}

	// 添加分页链接
	links := []gin.H{
		{
			"href": baseURL,
			"rel":  "self",
			"type": "application/geo+json",
		},
	}

	// 如果有下一页，添加 next 链接
	if result.Pagination.Page < result.Pagination.TotalPages {
		nextOffset := result.Pagination.Page * params.PageSize
		links = append(links, gin.H{
			"href": fmt.Sprintf("%s?limit=%d&offset=%d", baseURL, params.PageSize, nextOffset),
			"rel":  "next",
			"type": "application/geo+json",
		})
	}

	geojson["links"] = links
	geojson["numberMatched"] = result.Pagination.Total
	geojson["numberReturned"] = len(rows)

	c.Header("Content-Type", "application/geo+json")
	c.JSON(http.StatusOK, geojson)
}

// GetItem 获取单个 Feature
// GET /ogc/features/:serviceName/collections/:collectionId/items/:featureId
func (h *OGCFeaturesHandler) GetItem(c *gin.Context) {
	serviceName := c.Param("serviceName")
	collectionID := c.Param("collectionId")
	featureID := c.Param("featureId")

	// 查找服务
	service, err := h.getAndValidateService(c, serviceName)
	if err != nil {
		return // 错误已在函数内处理
	}

	// 验证 collectionId
	if collectionID != service.ServiceName {
		c.JSON(http.StatusNotFound, gin.H{"error": "Collection not found"})
		return
	}

	// 检查 OGC Features 是否启用
	if !service.IsOGCFeaturesEnabled() {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "OGC API Features is not enabled for this service"})
		return
	}

	// 构建 ID 过滤条件（假设主键为 id）
	params := svc.QueryParams{
		Filter:   fmt.Sprintf("id=%s", featureID),
		Page:     1,
		PageSize: 1,
		Format:   "geojson",
	}

	// 执行查询
	result, err := h.executorSvc.ExecuteQuery(c.Request.Context(), service, &params)
	if err != nil {
		log.Printf("[OGCFeatures] Query execution failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query execution failed: " + err.Error()})
		return
	}

	rows, ok := result.Data.([]map[string]interface{})
	if !ok || len(rows) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
		return
	}

	// 转换为 GeoJSON Feature
	geojsonData, err := h.executorSvc.FormatAsGeoJSON(rows, service)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to format as GeoJSON: " + err.Error()})
		return
	}

	c.Header("Content-Type", "application/geo+json")
	c.Data(http.StatusOK, "application/geo+json", geojsonData)
}

// getAndValidateService 通用服务查找和权限验证逻辑
func (h *OGCFeaturesHandler) getAndValidateService(c *gin.Context, serviceName string) (*models.QueryService, error) {
	// 从 JWT token 中获取租户 ID（如果是公开服务则可能没有）
	tenantID := c.GetUint("tenant_id")

	// 先通过服务名称查找服务(不过滤租户)
	service, err := h.svc.GetServiceModelByNameOnly(serviceName)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get service: " + err.Error()})
		}
		return nil, err
	}

	// 检查服务状态
	if service.Status != "active" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service is not active"})
		return nil, errors.New("service not active")
	}

	// 检查访问权限
	if !service.PublicAccess {
		// 非公开服务需要认证且租户匹配
		if tenantID == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
			return nil, errors.New("authentication required")
		}
		if service.TenantID != tenantID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return nil, errors.New("access denied")
		}
	}

	return service, nil
}
