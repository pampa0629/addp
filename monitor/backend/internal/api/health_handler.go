package api

import (
	"net/http"

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
func (h *HealthHandler) GetModules(c *gin.Context) {
	modules, err := h.healthService.GetModules(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"modules": modules,
	})
}

// CheckModuleHealth 检查单个模块健康状态
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
		c.JSON(http.StatusNotFound, gin.H{"error": "module not found"})
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
func (h *HealthHandler) CheckAllModules(c *gin.Context) {
	statuses, err := h.healthService.CheckAllModules(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"modules": statuses,
	})
}
