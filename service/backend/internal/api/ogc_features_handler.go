package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

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
// @Summary OGC API Features Landing Page | OGC API Features Landing Page
// @Tags OGC Features
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Success 200 {object} map[string]interface{} "Landing Page | Landing Page"
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 501 {object} map[string]string
// @x-addp-auth-mode "public"
// @Router /ogc/features/{serviceName} [get]
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
// @Summary OGC API Features Conformance | OGC API Features Conformance
// @Tags OGC Features
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Success 200 {object} map[string]interface{} "Conformance declaration | Conformance declaration"
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 501 {object} map[string]string
// @x-addp-auth-mode "public"
// @Router /ogc/features/{serviceName}/conformance [get]
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
// @Summary OGC API Features Collections | OGC API Features Collections
// @Tags OGC Features
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Success 200 {object} map[string]interface{} "Collections | Collections"
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 501 {object} map[string]string
// @x-addp-auth-mode "public"
// @Router /ogc/features/{serviceName}/collections [get]
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

	// collection extent 直接来自已发布 SpatialInfo，保持源 CRS 语义。
	if spatialInfo := service.GetSpatialInfo(); spatialInfo != nil && spatialInfo.Extent != nil {
		if crs := ogcCRSURI(spatialInfo.PrimaryCRSRef()); crs != "" {
			extent := *spatialInfo.Extent
			collection["extent"] = gin.H{"spatial": gin.H{
				"bbox": [][]float64{{extent[0], extent[1], extent[2], extent[3]}},
				"crs":  crs,
			}}
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
// @Summary OGC API Features Items | OGC API Features Items
// @Tags OGC Features
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Param collectionId path string true "集合ID | Collection ID"
// @Param limit query int false "返回数量 | Limit" default(10)
// @Param offset query int false "偏移量 | Offset" default(0)
// @Param bbox query string false "空间范围 | Bounding box"
// @Success 200 {object} map[string]interface{} "GeoJSON FeatureCollection | GeoJSON FeatureCollection"
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Failure 501 {object} map[string]string
// @x-addp-auth-mode "public"
// @Router /ogc/features/{serviceName}/collections/{collectionId}/items [get]
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
// @Summary OGC API Features Item | OGC API Features Item
// @Tags OGC Features
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Param collectionId path string true "集合ID | Collection ID"
// @Param featureId path string true "要素ID | Feature ID"
// @Success 200 {object} map[string]interface{} "GeoJSON Feature | GeoJSON Feature"
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Failure 501 {object} map[string]string
// @x-addp-auth-mode "public"
// @Router /ogc/features/{serviceName}/collections/{collectionId}/items/{featureId} [get]
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

	primaryKey := service.GetPrimaryKey()
	if primaryKey == "" {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "Published output contract does not define a primary key"})
		return
	}

	// 单要素查询使用已发布主键，不假设字段名为 id。
	params := svc.QueryParams{
		Filter:   fmt.Sprintf("%s='%s'", primaryKey, strings.ReplaceAll(featureID, "'", "''")),
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

func ogcCRSURI(crsRef string) string {
	parts := strings.Split(strings.TrimSpace(crsRef), ":")
	if len(parts) == 2 && strings.EqualFold(parts[0], "EPSG") && parts[1] != "" {
		return fmt.Sprintf("http://www.opengis.net/def/crs/EPSG/0/%s", parts[1])
	}
	return ""
}

// getAndValidateService 通用服务查找和权限验证逻辑
func (h *OGCFeaturesHandler) getAndValidateService(c *gin.Context, serviceName string) (*models.QueryService, error) {
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
	if !requireJSONServiceAccess(c, service.PublicAccess, service.TenantID) {
		return nil, errors.New("service access denied")
	}

	return service, nil
}
