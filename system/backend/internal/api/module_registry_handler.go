package api

import (
	"net/http"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	sysi18n "github.com/addp/system/i18n"
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
// POST /api/v1/internal/modules/register
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
		"message": commoni18n.T(c, sysi18n.MsgModuleRegistered),
		"module":  req.ModuleName,
	})
}

// Heartbeat 心跳更新
// POST /api/v1/internal/modules/heartbeat
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
		"message": commoni18n.T(c, sysi18n.MsgModuleHeartbeat),
		"module":  req.ModuleName,
	})
}

// ListModules 查询模块列表
// GET /api/v1/internal/modules
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
// GET /api/v1/internal/modules/:name
func (h *ModuleRegistryHandler) GetModule(c *gin.Context) {
	moduleName := c.Param("name")

	module, err := h.service.GetModule(moduleName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgModuleNotFound)})
		return
	}

	c.JSON(http.StatusOK, module)
}

// DeleteModule 模块注销
// DELETE /api/v1/internal/modules/:name
func (h *ModuleRegistryHandler) DeleteModule(c *gin.Context) {
	moduleName := c.Param("name")

	if err := h.service.DeleteModule(moduleName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": commoni18n.T(c, sysi18n.MsgModuleDeleted),
		"module":  moduleName,
	})
}
