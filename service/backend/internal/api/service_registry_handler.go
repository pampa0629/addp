package api

import (
	"net/http"
	"strconv"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/service/registry"
	"github.com/gin-gonic/gin"
)

type ServiceRegistryHandler struct {
	service *registry.ExternalServiceService
}

func NewServiceRegistryHandler(service *registry.ExternalServiceService) *ServiceRegistryHandler {
	return &ServiceRegistryHandler{service: service}
}

// CreateService 创建外部服务
// @Summary 注册外部服务
// @Tags ServiceRegistry
// @Accept json
// @Produce json
// @Param request body models.CreateExternalServiceRequest true "创建请求"
// @Success 201 {object} models.ExternalServiceDTO
// @Router /api/service/registry/services [post]
func (h *ServiceRegistryHandler) CreateService(c *gin.Context) {
	var req models.CreateExternalServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 从上下文获取租户 ID 和用户 ID（由认证中间件注入）
	tenantID, _ := c.Get("tenant_id")
	userID, _ := c.Get("user_id")

	service, err := h.service.CreateService(c.Request.Context(), &req, tenantID.(uint), userID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, h.service.ToDTO(service))
}

// GetService 获取服务详情
// @Summary 获取服务详情
// @Tags ServiceRegistry
// @Produce json
// @Param id path int true "服务ID"
// @Success 200 {object} models.ExternalServiceDTO
// @Router /api/service/registry/services/{id} [get]
func (h *ServiceRegistryHandler) GetService(c *gin.Context) {
	serviceID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service id"})
		return
	}

	tenantID, _ := c.Get("tenant_id")

	service, err := h.service.GetService(c.Request.Context(), uint(serviceID), tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, h.service.ToDTO(service))
}

// ListServices 列出服务
// @Summary 列出外部服务
// @Tags ServiceRegistry
// @Produce json
// @Param service_type query string false "服务类型过滤"
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页大小" default(20)
// @Success 200 {object} map[string]interface{}
// @Router /api/service/registry/services [get]
func (h *ServiceRegistryHandler) ListServices(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")

	serviceType := c.Query("service_type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	services, total, err := h.service.ListServices(c.Request.Context(), tenantID.(uint), serviceType, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为 DTO
	dtos := make([]models.ExternalServiceDTO, len(services))
	for i, svc := range services {
		dtos[i] = *h.service.ToDTO(&svc)
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      dtos,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// UpdateService 更新服务
// @Summary 更新外部服务
// @Tags ServiceRegistry
// @Accept json
// @Produce json
// @Param id path int true "服务ID"
// @Param request body models.UpdateExternalServiceRequest true "更新请求"
// @Success 200 {object} models.ExternalServiceDTO
// @Router /api/service/registry/services/{id} [put]
func (h *ServiceRegistryHandler) UpdateService(c *gin.Context) {
	serviceID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service id"})
		return
	}

	var req models.UpdateExternalServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID, _ := c.Get("tenant_id")

	service, err := h.service.UpdateService(c.Request.Context(), uint(serviceID), tenantID.(uint), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, h.service.ToDTO(service))
}

// DeleteService 删除服务
// @Summary 删除外部服务
// @Tags ServiceRegistry
// @Param id path int true "服务ID"
// @Success 204
// @Router /api/service/registry/services/{id} [delete]
func (h *ServiceRegistryHandler) DeleteService(c *gin.Context) {
	serviceID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service id"})
		return
	}

	tenantID, _ := c.Get("tenant_id")

	if err := h.service.DeleteService(c.Request.Context(), uint(serviceID), tenantID.(uint)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// RefreshMetadata 刷新服务元数据
// @Summary 刷新服务元数据
// @Tags ServiceRegistry
// @Param id path int true "服务ID"
// @Success 200 {object} models.ExternalServiceDTO
// @Router /api/service/registry/services/{id}/refresh [post]
func (h *ServiceRegistryHandler) RefreshMetadata(c *gin.Context) {
	serviceID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service id"})
		return
	}

	tenantID, _ := c.Get("tenant_id")

	service, err := h.service.RefreshMetadata(c.Request.Context(), uint(serviceID), tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, h.service.ToDTO(service))
}

// ProxyService 代理访问外部服务
// @Summary 代理访问外部服务
// @Tags ServiceRegistry
// @Param id path int true "服务ID"
// @Param path path string true "目标路径"
// @Success 200
// @Router /api/service/proxy/{id}/*path [get]
func (h *ServiceRegistryHandler) ProxyService(c *gin.Context) {
	serviceID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service id"})
		return
	}

	tenantID, _ := c.Get("tenant_id")

	// 获取服务信息
	service, err := h.service.GetService(c.Request.Context(), uint(serviceID), tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	// 获取目标路径和查询参数
	path := c.Param("path")
	query := c.Request.URL.RawQuery

	// 代理请求
	body, contentType, statusCode, err := h.service.ProxyRequest(c.Request.Context(), service, path, query)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	// 返回响应
	c.Data(statusCode, contentType, body)
}

// GetServiceCatalog 获取服务目录
// @Summary 获取服务目录（按类型分类）
// @Tags ServiceRegistry
// @Produce json
// @Success 200 {object} map[string][]models.ExternalServiceDTO
// @Router /api/service/catalog [get]
func (h *ServiceRegistryHandler) GetServiceCatalog(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")

	catalog, err := h.service.GetServiceCatalog(c.Request.Context(), tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为 DTO
	dtoCatalog := make(map[string][]models.ExternalServiceDTO)
	for serviceType, services := range catalog {
		dtos := make([]models.ExternalServiceDTO, len(services))
		for i, svc := range services {
			dtos[i] = *h.service.ToDTO(&svc)
		}
		dtoCatalog[serviceType] = dtos
	}

	c.JSON(http.StatusOK, dtoCatalog)
}

// SearchServices 搜索服务
// @Summary 搜索服务
// @Tags ServiceRegistry
// @Produce json
// @Param keyword query string true "搜索关键词"
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页大小" default(20)
// @Success 200 {object} map[string]interface{}
// @Router /api/service/registry/search [get]
func (h *ServiceRegistryHandler) SearchServices(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")

	keyword := c.Query("keyword")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	services, total, err := h.service.SearchServices(c.Request.Context(), tenantID.(uint), keyword, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为 DTO
	dtos := make([]models.ExternalServiceDTO, len(services))
	for i, svc := range services {
		dtos[i] = *h.service.ToDTO(&svc)
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      dtos,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// HealthCheck 健康检查
// @Summary 执行服务健康检查
// @Tags ServiceRegistry
// @Param id path int true "服务ID"
// @Success 200
// @Router /api/service/registry/services/{id}/health [post]
func (h *ServiceRegistryHandler) HealthCheck(c *gin.Context) {
	serviceID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service id"})
		return
	}

	tenantID, _ := c.Get("tenant_id")

	if err := h.service.HealthCheck(c.Request.Context(), uint(serviceID), tenantID.(uint)); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"message": "Service is accessible",
	})
}

// ExportConfig 导出服务配置
// @Summary 导出服务配置（备份）
// @Tags ServiceRegistry
// @Produce json
// @Success 200
// @Router /api/service/registry/export [get]
func (h *ServiceRegistryHandler) ExportConfig(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")

	data, err := h.service.ExportServiceConfig(c.Request.Context(), tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Disposition", "attachment; filename=services_export.json")
	c.Data(http.StatusOK, "application/json", data)
}
