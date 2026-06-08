package api

import (
	"net/http"

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
