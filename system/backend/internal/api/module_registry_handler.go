package api

import (
	"net/http"

	commonauthmiddleware "github.com/addp/common/middleware/auth"
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

// ListModulesService godoc
// @Summary      查询平台注册模块 | List registered platform modules
// @Description  平台 Service Principal 查询运行模块注册表，可按 status=up 过滤存活模块 | A platform service principal lists runtime modules and may filter active modules with status=up
// @Tags         运行时注册 | Runtime Registry
// @Produce      json
// @Security     BearerAuth
// @Param        status query string false "模块状态过滤 | Module status filter"
// @Success      200 {object} object{modules=[]models.ModuleInfo,count=int}
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.read"]
// @Router       /runtime/modules [get]
func (h *ModuleRegistryHandler) ListModulesService(c *gin.Context) {
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
	c.JSON(http.StatusOK, gin.H{"modules": modules, "count": len(modules)})
}

// ListConfigurationManagementEntries godoc
// @Summary      查询当前上下文可见的配置管理入口 | List visible configuration management entries
// @Tags         配置管理 | Configuration Management
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} models.ConfigurationManagementEntryView
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "self"
// @Router       /configuration-management/entries [get]
func (h *ModuleRegistryHandler) ListConfigurationManagementEntries(c *gin.Context) {
	authContext, exists := commonauthmiddleware.AuthContextFromGin(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	permissions := make(map[string]struct{})
	for _, assignment := range authContext.Authorization.RoleAssignments {
		for _, permission := range assignment.Permissions {
			permissions[permission] = struct{}{}
		}
	}
	entries, err := h.service.ListConfigurationManagementEntries(authContext.Context.Type, permissions)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entries)
}

// GetModuleService godoc
// @Summary      查询平台注册模块详情 | Get registered platform module
// @Description  平台 Service Principal 按模块名查询运行模块注册信息 | A platform service principal gets runtime module registration by module name
// @Tags         运行时注册 | Runtime Registry
// @Produce      json
// @Security     BearerAuth
// @Param        module_name path string true "模块名 | Module name"
// @Success      200 {object} models.ModuleInfo
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.read"]
// @Router       /runtime/modules/{module_name} [get]
func (h *ModuleRegistryHandler) GetModuleService(c *gin.Context) {
	module, err := h.service.GetModule(c.Param("module_name"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgModuleNotFound)})
		return
	}
	c.JSON(http.StatusOK, module)
}
