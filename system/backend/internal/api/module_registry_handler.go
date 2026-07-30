package api

import (
	"net/http"

	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
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

// RegisterService godoc
// @Summary      注册当前运行模块 | Register current runtime module
// @Description  平台 Service Principal 只能注册与自身 OAuth Client 对应的模块 | A platform service principal can only register the module matching its OAuth client
// @Tags         运行时注册 | Runtime Registry
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.ModuleRegistrationRequest true "模块注册信息 | Module registration"
// @Success      200 {object} object{message=string,module=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.update"]
// @Router       /runtime/modules [post]
func (h *ModuleRegistryHandler) RegisterService(c *gin.Context) {
	var req models.ModuleRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := iamServiceOwnsModule(c, req.ModuleName); err != nil {
		respondIAMError(c, err)
		return
	}
	if err := h.service.Register(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgModuleRegistered), "module": req.ModuleName})
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

// HeartbeatService godoc
// @Summary      更新当前运行模块心跳 | Update current runtime module heartbeat
// @Tags         运行时注册 | Runtime Registry
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.HeartbeatRequest true "模块心跳 | Module heartbeat"
// @Success      200 {object} object{message=string,module=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.update"]
// @Router       /runtime/modules/heartbeat [post]
func (h *ModuleRegistryHandler) HeartbeatService(c *gin.Context) {
	var req models.HeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := iamServiceOwnsModule(c, req.ModuleName); err != nil {
		respondIAMError(c, err)
		return
	}
	if err := h.service.SendHeartbeat(req.ModuleName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgModuleHeartbeat), "module": req.ModuleName})
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
