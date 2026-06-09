package api

import (
	"errors"
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	moni18n "github.com/addp/monitor/i18n"
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

// GetModules 获取所有模块列表
// @Summary 获取模块列表 | Get module list
// @Tags Monitor
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /modules [get]
// @Security BearerAuth
func (h *HealthHandler) GetModules(c *gin.Context) {
	modules, err := h.healthService.GetModules(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, modules)
}

// GetTaskProviders 获取所有任务提供者
// @Summary 获取任务提供者列表 | Get task providers
// @Tags Monitor
// @Produce json
// @Success 200 {array} models.TaskProvider
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
// @Description 从 System 读取启用 TaskProvider，并探测模块 /health 与标准 GET /tasks?task_type= endpoint。| Read enabled TaskProviders from System and probe module /health plus standard GET /tasks?task_type= endpoints.
// @Tags Monitor
// @Produce json
// @Success 200 {array} service.ProviderHealthStatus
// @Failure 500 {object} models.ErrorResponse
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
// @Description 从 System 读取指定 TaskProvider，并探测模块 /health 与标准 GET /tasks?task_type= endpoint。| Read a TaskProvider from System and probe module /health plus standard GET /tasks?task_type= endpoints.
// @Tags Monitor
// @Produce json
// @Param module path string true "模块名 | Module"
// @Success 200 {object} service.ProviderHealthStatus
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
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

// CheckModuleHealth 检查单个模块健康状态
// @Summary 检查模块健康状态 | Check module health
// @Tags Monitor
// @Produce json
// @Param module path string true "模块名 | Module"
// @Success 200 {object} map[string]interface{}
// @Router /modules/{module}/health [get]
// @Security BearerAuth
func (h *HealthHandler) CheckModuleHealth(c *gin.Context) {
	moduleName := c.Param("module")

	// 获取模块信息
	modules, err := h.healthService.GetModules(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 查找模块
	var moduleInfo *service.ModuleInfo
	for _, m := range modules {
		if m.Name == moduleName {
			moduleInfo = m
			break
		}
	}

	if moduleInfo == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgModuleNotFound)})
		return
	}

	// 检查健康状态
	status, err := h.healthService.CheckModuleHealth(c.Request.Context(), moduleInfo.Name, moduleInfo.BaseURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, status)
}

// CheckAllModules 检查所有模块健康状态
// @Summary 检查所有模块健康状态 | Check all modules health
// @Tags Monitor
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /modules/health/all [get]
// @Security BearerAuth
func (h *HealthHandler) CheckAllModules(c *gin.Context) {
	statuses, err := h.healthService.CheckAllModules(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, statuses)
}

func currentTenantID(c *gin.Context) uint {
	tenantID, _ := c.Get(commonAuth.ContextTenantIDKey)
	switch typed := tenantID.(type) {
	case uint:
		return typed
	case int:
		if typed > 0 {
			return uint(typed)
		}
	case int64:
		if typed > 0 {
			return uint(typed)
		}
	}
	return 0
}
