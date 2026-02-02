package api

import (
	"net/http"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

// ModuleRegistryHandler 模块注册API处理器
type ModuleRegistryHandler struct {
	service *service.ModuleRegistryService
}

// NewModuleRegistryHandler 创建模块注册Handler
func NewModuleRegistryHandler(service *service.ModuleRegistryService) *ModuleRegistryHandler {
	return &ModuleRegistryHandler{service: service}
}

// Register 模块注册(幂等)
// POST /api/internal/modules/register
func (h *ModuleRegistryHandler) Register(c *gin.Context) {
	var req models.ModuleRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.service.Register(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "模块注册成功",
		"module":  req.ModuleName,
	})
}

// Heartbeat 心跳更新
// POST /api/internal/modules/heartbeat
func (h *ModuleRegistryHandler) Heartbeat(c *gin.Context) {
	var req models.HeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.service.SendHeartbeat(req.ModuleName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "心跳更新成功",
		"module":  req.ModuleName,
	})
}

// ListModules 查询模块列表
// GET /api/internal/modules
func (h *ModuleRegistryHandler) ListModules(c *gin.Context) {
	// 支持查询参数: ?status=up 只返回活跃模块
	status := c.Query("status")

	var modules []*models.ModuleInfo
	var err error

	if status == "up" {
		modules, err = h.service.ListActiveModules()
	} else {
		modules, err = h.service.ListModules()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"modules": modules,
		"count":   len(modules),
	})
}

// GetModule 查询单个模块
// GET /api/internal/modules/:name
func (h *ModuleRegistryHandler) GetModule(c *gin.Context) {
	moduleName := c.Param("name")

	module, err := h.service.GetModule(moduleName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "模块不存在"})
		return
	}

	c.JSON(http.StatusOK, module)
}

// DeleteModule 模块注销
// DELETE /api/internal/modules/:name
func (h *ModuleRegistryHandler) DeleteModule(c *gin.Context) {
	moduleName := c.Param("name")

	if err := h.service.DeleteModule(moduleName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "模块注销成功",
		"module":  moduleName,
	})
}
