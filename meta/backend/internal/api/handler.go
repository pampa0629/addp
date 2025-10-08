package api

import (
	"net/http"
	"strconv"

	"github.com/addp/meta/internal/middleware"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	resourceService *service.ResourceService
	scanService     *service.ScanServiceNew
}

func NewHandler(resourceService *service.ResourceService, scanService *service.ScanServiceNew) *Handler {
	return &Handler{
		resourceService: resourceService,
		scanService:     scanService,
	}
}

// GetResources 获取资源列表及统计
// GET /api/meta/resources
func (h *Handler) GetResources(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resources, err := h.resourceService.GetResourcesWithStats(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": resources})
}

// GetSchemas 获取资源的Schema列表
// GET /api/meta/schemas/:resource_id
func (h *Handler) GetSchemas(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Param("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	schemas, err := h.scanService.GetSchemasByResource(uint(resourceID), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": schemas})
}

// ListAvailableSchemas 列出资源中可用的Schema（从数据库实时查询）
// GET /api/meta/schemas/:resource_id/available
func (h *Handler) ListAvailableSchemas(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Param("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	// 从请求头中提取JWT token，传递给System API
	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}
	// 去掉 "Bearer " 前缀
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	schemas, err := h.scanService.ListAvailableSchemas(uint(resourceID), tenantID, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": schemas})
}

// ListObjectStorageNodes 分级列出对象存储节点
func (h *Handler) ListObjectStorageNodes(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Param("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	path := c.Query("path")

	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	nodes, err := h.scanService.ListObjectStorageNodes(uint(resourceID), tenantID, path, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": nodes})
}

// AutoScan 自动扫描所有未扫描的资源
// POST /api/meta/scan/auto
func (h *Handler) AutoScan(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	result, err := h.scanService.AutoScanUnscanned(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ScanResource 扫描指定资源
// POST /api/meta/scan/resource
func (h *Handler) ScanResource(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req models.ScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 提取JWT token
	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
		return
	}
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	result, err := h.scanService.ScanResource(req.ResourceID, tenantID, req.SchemaNames, req.ObjectPaths, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
