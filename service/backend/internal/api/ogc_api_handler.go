package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
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

	// 验证是否启用 OGC API Features
	if !service.EnabledOGCAPI {
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

	if !service.EnabledOGCAPI {
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

	if !service.EnabledOGCAPI {
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
			// TODO: 正确解析 JSONB Extent4326
			collection["extent"] = gin.H{
				"spatial": gin.H{
					"bbox": []interface{}{[]float64{0, 0, 180, 90}},
				},
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

	if !service.EnabledOGCAPI {
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
		// TODO: 正确解析 JSONB Extent4326
		collection["extent"] = gin.H{
			"spatial": gin.H{
				"bbox": []interface{}{[]float64{0, 0, 180, 90}},
			},
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

	if !service.EnabledOGCAPI {
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

	// 解析查询参数
	limit := 10
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	offset := 0
	if off := c.Query("offset"); off != "" {
		if parsed, err := strconv.Atoi(off); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	baseURL := "http://" + c.Request.Host + "/ogc/api/" + serviceName

	// 构建响应（简化版，返回空要素集合）
	// TODO: 实现实际的要素查询逻辑
	response := gin.H{
		"type": "FeatureCollection",
		"features": []interface{}{},
		"links": []gin.H{
			{
				"href": c.Request.RequestURI,
				"rel":  "self",
				"type": "application/json",
			},
			{
				"href": fmt.Sprintf("%s/collections/%s", baseURL, collectionID),
				"rel":  "collection",
				"type": "application/json",
			},
		},
		"numberMatched": 0,
		"numberReturned": 0,
		"timeStamp": gin.H{},
	}

	logger.L().Debug("OGC API Features Items requested", "service", serviceName, "collection", collectionID, "limit", limit, "offset", offset)

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

	if !service.EnabledOGCAPI {
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

	logger.L().Debug("OGC API Features GetItem requested", "service", serviceName, "collection", collectionID, "feature", featureID)

	// TODO: 实现实际的要素查询逻辑
	c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
}
