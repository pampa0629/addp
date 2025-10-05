package api

import (
	"net/http"
	"strconv"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type ResourceHandler struct {
	resourceService      *service.ResourceService
	storageEngineService *service.StorageEngineService
}

func NewResourceHandler(resourceService *service.ResourceService) *ResourceHandler {
	return &ResourceHandler{
		resourceService:      resourceService,
		storageEngineService: service.NewStorageEngineService(),
	}
}

func (h *ResourceHandler) Create(c *gin.Context) {
	var req models.ResourceCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetUint("user_id")
	resource, err := h.resourceService.Create(&req, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, resource)
}

func (h *ResourceHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	resourceType := c.Query("resource_type")

	// 获取当前用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	resources, err := h.resourceService.List(page, pageSize, resourceType, userID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resources)
}

func (h *ResourceHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	resource, err := h.resourceService.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "资源不存在"})
		return
	}

	c.JSON(http.StatusOK, resource)
}

func (h *ResourceHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	var req models.ResourceUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resource, err := h.resourceService.Update(uint(id), &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resource)
}

func (h *ResourceHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	if err := h.resourceService.Delete(uint(id)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// TestConnection 测试存储引擎连接
func (h *ResourceHandler) TestConnection(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	resource, err := h.resourceService.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "资源不存在"})
		return
	}

	// 测试连接
	if err := h.storageEngineService.TestConnection(resource); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "连接失败",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "连接成功",
	})
}

// TestConnectionBeforeCreate 创建前测试连接
func (h *ResourceHandler) TestConnectionBeforeCreate(c *gin.Context) {
	var req models.ResourceCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 构建临时资源对象用于测试
	resource := &models.Resource{
		ResourceType:   req.ResourceType,
		ConnectionInfo: req.ConnectionInfo,
	}

	// 测试连接
	if err := h.storageEngineService.TestConnection(resource); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "连接失败",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "连接成功",
	})
}

// ============ 内部 API（服务间调用）============

// ListInternal 内部资源列表查询（无需用户认证，用于服务间调用）
func (h *ResourceHandler) ListInternal(c *gin.Context) {
	resourceType := c.Query("resource_type")
	tenantID := c.Query("tenant_id") // 可选，按租户过滤

	// 内部调用返回所有资源（或按 tenant_id 过滤）
	var tenantIDUint uint
	if tenantID != "" {
		id, err := strconv.ParseUint(tenantID, 10, 32)
		if err == nil {
			tenantIDUint = uint(id)
		}
	}

	// 调用服务层的内部列表方法（不做租户隔离检查）
	resources, err := h.resourceService.ListInternal(resourceType, tenantIDUint)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resources)
}

// GetByIDInternal 内部资源详情查询（无需用户认证，用于服务间调用）
func (h *ResourceHandler) GetByIDInternal(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的资源ID"})
		return
	}

	resource, err := h.resourceService.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "资源不存在"})
		return
	}

	c.JSON(http.StatusOK, resource)
}