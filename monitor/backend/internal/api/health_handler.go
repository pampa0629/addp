package api

import (
	"errors"
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

// HealthHandler 健康检查 Handler
type HealthHandler struct {
	healthService *service.HealthCheckService
}

// NewHealthHandler 创建 Handler
func NewHealthHandler(healthService *service.HealthCheckService) *HealthHandler {
	return &HealthHandler{
		healthService: healthService,
	}
}

// GetTaskProviders 获取所有任务提供者
// @Summary 获取任务提供者列表 | Get task providers
// @Tags Monitor
// @Produce json
// @Success 200 {array} models.TaskProvider
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.health.read"]
// @Router /task-providers [get]
// @Security BearerAuth
func (h *HealthHandler) GetTaskProviders(c *gin.Context) {
	providers, err := h.healthService.ListTaskProviders(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, providers)
}

// CheckAllProvidersHealth 检查所有任务提供者运行态健康状态
// @Summary 检查所有任务提供者运行态健康状态 | Check all task provider health
// @Description 从 System 读取启用 TaskProvider 及其当前有效 Backend 实例池，逐实例探测模块 /health 与标准 GET /tasks?task_type= endpoint，并聚合 Provider 状态。| Read enabled TaskProviders and their currently valid Backend instance pools from System, probe module /health plus standard GET /tasks?task_type= endpoints per instance, and aggregate Provider status.
// @Tags Monitor
// @Produce json
// @Success 200 {array} service.ProviderHealthStatus
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.health.read"]
// @Router /providers/health [get]
// @Security BearerAuth
func (h *HealthHandler) CheckAllProvidersHealth(c *gin.Context) {
	statuses, err := h.healthService.CheckAllProviderHealth(c.Request.Context(), currentTenantID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, statuses)
}

// CheckProviderHealth 检查单个任务提供者运行态健康状态
// @Summary 检查单个任务提供者运行态健康状态 | Check task provider health
// @Description 从 System 读取指定 TaskProvider 及其当前有效 Backend 实例池，逐实例探测模块 /health 与标准 GET /tasks?task_type= endpoint，并聚合 Provider 状态。| Read the TaskProvider and its currently valid Backend instance pool from System, probe module /health plus standard GET /tasks?task_type= endpoints per instance, and aggregate Provider status.
// @Tags Monitor
// @Produce json
// @Param module path string true "模块名 | Module"
// @Success 200 {object} service.ProviderHealthStatus
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.health.read"]
// @Router /providers/{module}/health [get]
// @Security BearerAuth
func (h *HealthHandler) CheckProviderHealth(c *gin.Context) {
	status, err := h.healthService.CheckProviderHealth(c.Request.Context(), c.Param("module"), currentTenantID(c))
	if err != nil {
		if errors.Is(err, service.ErrTaskProviderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, status)
}

func currentTenantID(c *gin.Context) uint {
	return commonAuth.GetTenantID(c)
}
