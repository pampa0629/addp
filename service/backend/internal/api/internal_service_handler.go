package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// InternalServiceHandler 处理内部服务相关的 HTTP 请求
type InternalServiceHandler struct {
	svc *service.InternalServiceService
}

// NewInternalServiceHandler 创建新的内部服务处理器
func NewInternalServiceHandler(svc *service.InternalServiceService) *InternalServiceHandler {
	return &InternalServiceHandler{
		svc: svc,
	}
}

// CreateService 创建新的内部服务
// POST /api/service/internal/services
func (h *InternalServiceHandler) CreateService(c *gin.Context) {
	var req models.CreateInternalServiceRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 从 JWT token 中获取租户 ID 和用户 ID
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	if tenantID == 0 || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id or user_id in token"})
		return
	}

	result, err := h.svc.CreateService(&req, tenantID, userID)
	if err != nil {
		// 区分不同的错误类型
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		} else if err.Error() == "at least one service type must be enabled" || err.Error() == "service name already exists" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create service: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusCreated, result)
}

// ListServices 列出租户下的所有服务
// GET /api/service/internal/services?page=1&limit=20&search=...
func (h *InternalServiceHandler) ListServices(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id in token"})
		return
	}

	// 分页参数
	page := 1
	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	limit := 20
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := (page - 1) * limit

	// 搜索参数
	search := c.Query("search")
	if search != "" {
		// 如果有搜索词，使用 SearchServices
		results, total, err := h.svc.SearchServices(tenantID, search, offset, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to search services: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"data":      results,
			"total":     total,
			"page":      page,
			"limit":     limit,
			"pages":     (total + int64(limit) - 1) / int64(limit),
		})
	} else {
		// 否则使用 ListServices
		results, total, err := h.svc.ListServices(tenantID, offset, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list services: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"data":      results,
			"total":     total,
			"page":      page,
			"limit":     limit,
			"pages":     (total + int64(limit) - 1) / int64(limit),
		})
	}
}

// GetService 获取服务详情
// GET /api/service/internal/services/:id
func (h *InternalServiceHandler) GetService(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	result, err := h.svc.GetService(uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get service: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// UpdateService 更新服务
// PUT /api/service/internal/services/:id
func (h *InternalServiceHandler) UpdateService(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	var req models.UpdateInternalServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	result, err := h.svc.UpdateService(uint(id), &req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else if err.Error() == "at least one service type must be enabled" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update service: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// DeleteService 删除服务
// DELETE /api/service/internal/services/:id
func (h *InternalServiceHandler) DeleteService(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	if err := h.svc.DeleteService(uint(id)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete service: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Service deleted successfully"})
}

// AddLayer 添加图层
// POST /api/service/internal/services/:id/layers
func (h *InternalServiceHandler) AddLayer(c *gin.Context) {
	idStr := c.Param("id")
	serviceID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	var req models.AddLayerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	result, err := h.svc.AddLayer(uint(serviceID), &req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add layer: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusCreated, result)
}

// UpdateLayer 更新图层
// PUT /api/service/internal/services/:id/layers/:layer_id
func (h *InternalServiceHandler) UpdateLayer(c *gin.Context) {
	idStr := c.Param("id")
	layerIDStr := c.Param("layer_id")

	// 验证 service ID（可选，主要用于权限检查）
	_, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// 解析 layer ID
	layerID, err := strconv.ParseUint(layerIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid layer ID"})
		return
	}

	var req models.UpdateLayerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	result, err := h.svc.UpdateLayer(uint(layerID), &req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Layer not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update layer: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// DeleteLayer 删除图层
// DELETE /api/service/internal/services/:id/layers/:layer_id
func (h *InternalServiceHandler) DeleteLayer(c *gin.Context) {
	idStr := c.Param("id")
	layerIDStr := c.Param("layer_id")

	// 验证 service ID
	_, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// 解析 layer ID
	layerID, err := strconv.ParseUint(layerIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid layer ID"})
		return
	}

	if err := h.svc.DeleteLayer(uint(layerID)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Layer not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete layer: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Layer deleted successfully"})
}
