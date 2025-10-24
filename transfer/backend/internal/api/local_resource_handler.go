package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

// LocalResourceHandler 提供本地存储引擎管理 API
type LocalResourceHandler struct {
	service *service.LocalResourceService
}

// NewLocalResourceHandler 构造函数
func NewLocalResourceHandler(service *service.LocalResourceService) *LocalResourceHandler {
	return &LocalResourceHandler{service: service}
}

// LocalResourceRequest 创建或更新请求体
type LocalResourceRequest struct {
	Name           string         `json:"name" binding:"required"`
	ResourceType   string         `json:"resource_type" binding:"required"`
	Description    string         `json:"description"`
	IsActive       bool           `json:"is_active"`
	ConnectionInfo models.JSONMap `json:"connection_info" binding:"required"`
}

// LocalResourceUpdateRequest 更新请求
type LocalResourceUpdateRequest struct {
	Name           *string         `json:"name"`
	Description    *string         `json:"description"`
	IsActive       *bool           `json:"is_active"`
	ConnectionInfo *models.JSONMap `json:"connection_info"`
}

// List 返回当前租户的资源列表
func (h *LocalResourceHandler) List(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	resourceType := c.Query("resource_type")

	resources, err := h.service.List(tenantID, resourceType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resources)
}

// ListSystemResources 返回 System 模块的存储引擎
func (h *LocalResourceHandler) ListSystemResources(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	resourceType := c.Query("resource_type")

	resources, err := h.service.ListSystemResources(resourceType, tenantID)
	if err != nil {
		if errors.Is(err, service.ErrSystemIntegrationDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "system integration not available"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resources)
}

// Create 新建资源
func (h *LocalResourceHandler) Create(c *gin.Context) {
	var req LocalResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	resource := &models.LocalResource{
		TenantID:       tenantID,
		Name:           req.Name,
		ResourceType:   req.ResourceType,
		Description:    req.Description,
		IsActive:       req.IsActive,
		ConnectionInfo: req.ConnectionInfo,
		CreatedBy:      &userID,
	}

	if err := h.service.Create(resource); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, resource)
}

// Update 更新资源
func (h *LocalResourceHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	var req LocalResourceUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")

	resource, err := h.service.Get(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return
	}

	if req.Name != nil {
		resource.Name = *req.Name
	}
	if req.Description != nil {
		resource.Description = *req.Description
	}
	if req.IsActive != nil {
		resource.IsActive = *req.IsActive
	}
	if req.ConnectionInfo != nil {
		resource.ConnectionInfo = *req.ConnectionInfo
	}

	if err := h.service.Update(resource); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resource)
}

// Delete 删除资源
func (h *LocalResourceHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.service.Delete(uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// TestBeforeCreate 创建前测试连接
func (h *LocalResourceHandler) TestBeforeCreate(c *gin.Context) {
	var req LocalResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.service.TestConnectionBeforeCreate(req.ResourceType, req.ConnectionInfo); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// TestExisting 测试已有资源
func (h *LocalResourceHandler) TestExisting(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.service.TestConnection(uint(id), tenantID); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Sync 推送到 System
func (h *LocalResourceHandler) Sync(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	resource, err := h.service.Get(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return
	}

	result, err := h.service.SyncToSystem(resource)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
