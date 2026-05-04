package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
	"github.com/addp/service/internal/models"
	svc "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RegisteredServiceHandler 处理注册服务相关的 HTTP 请求
type RegisteredServiceHandler struct {
	svc *svc.RegisteredServiceService
}

// NewRegisteredServiceHandler 创建新的注册服务处理器
func NewRegisteredServiceHandler(s *svc.RegisteredServiceService) *RegisteredServiceHandler {
	return &RegisteredServiceHandler{
		svc: s,
	}
}

// ===== 服务管理 API =====

// CreateService 创建新的注册服务
// @Summary 创建注册服务 | Create registered service
// @Tags RegisteredService
// @Accept json
// @Produce json
// @Param request body models.CreateRegisteredServiceRequest true "创建请求 | Create request"
// @Success 201 {object} models.RegisteredServiceDTO
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /registered [post]
// @Security BearerAuth
func (h *RegisteredServiceHandler) CreateService(c *gin.Context) {
	var req models.CreateRegisteredServiceRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		// 记录详细错误日志
		logger.L().Error("CreateService validation error", "error", err)
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
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusCreated, result)
}

// ListServices 列出租户下的所有注册服务
// @Summary 注册服务列表 | List registered services
// @Tags RegisteredService
// @Produce json
// @Param page query int false "页码 | Page" default(1)
// @Param limit query int false "每页数量 | Limit" default(20)
// @Param search query string false "搜索词 | Search"
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /registered [get]
// @Security BearerAuth
func (h *RegisteredServiceHandler) ListServices(c *gin.Context) {
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

	var results []models.RegisteredServiceDTO
	var total int64
	var err error

	if search != "" {
		// 如果有搜索词，使用 SearchServices
		results, total, err = h.svc.SearchServices(tenantID, search, offset, limit)
	} else {
		// 否则使用 ListServices
		results, total, err = h.svc.ListServices(tenantID, offset, limit)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list services: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  results,
		"total": total,
		"page":  page,
		"limit": limit,
		"pages": (total + int64(limit) - 1) / int64(limit),
	})
}

// GetService 获取服务详情
// @Summary 获取注册服务详情 | Get registered service
// @Tags RegisteredService
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Success 200 {object} models.RegisteredServiceDTO
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /registered/{id} [get]
// @Security BearerAuth
func (h *RegisteredServiceHandler) GetService(c *gin.Context) {
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
// @Summary 更新注册服务 | Update registered service
// @Tags RegisteredService
// @Accept json
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Param request body models.UpdateRegisteredServiceRequest true "更新请求 | Update request"
// @Success 200 {object} models.RegisteredServiceDTO
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /registered/{id} [put]
// @Security BearerAuth
func (h *RegisteredServiceHandler) UpdateService(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	var req models.UpdateRegisteredServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	result, err := h.svc.UpdateService(uint(id), &req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// DeleteService 删除服务
// @Summary 删除注册服务 | Delete registered service
// @Tags RegisteredService
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /registered/{id} [delete]
// @Security BearerAuth
func (h *RegisteredServiceHandler) DeleteService(c *gin.Context) {
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

// RefreshMetadata 刷新服务元数据
// @Summary 刷新注册服务元数据 | Refresh registered service metadata
// @Tags RegisteredService
// @Accept json
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Param request body models.RefreshMetadataRequest false "刷新请求 | Refresh request"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /registered/{id}/refresh [post]
// @Security BearerAuth
func (h *RegisteredServiceHandler) RefreshMetadata(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	var req models.RefreshMetadataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 如果没有请求体，使用默认值
		req.Force = false
	}

	if err := h.svc.RefreshMetadata(uint(id), req.Force); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else if err.Error() == "metadata refresh is only supported for OGC services" {
			// 业务逻辑错误：不支持的服务类型
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refresh metadata: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Metadata refreshed successfully"})
}

// HealthCheck 健康检查
// @Summary 检查注册服务健康状态 | Check registered service health
// @Tags RegisteredService
// @Produce json
// @Param id path int true "服务ID | Service ID"
// @Success 200 {object} models.HealthCheckResult
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /registered/{id}/health [post]
// @Security BearerAuth
func (h *RegisteredServiceHandler) HealthCheck(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	result, err := h.svc.HealthCheck(uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to perform health check: " + err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// ProxyService 代理转发外部服务请求
// ANY /api/service/proxy/:id/*path
func (h *RegisteredServiceHandler) ProxyService(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// 获取租户 ID 和用户 ID（用于审计，如果没有认证则为 0）
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	// 代理请求到外部服务
	err = h.svc.ProxyServiceRequest(uint(id), tenantID, userID, c)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Proxy request failed: " + err.Error()})
		}
		return
	}
}
