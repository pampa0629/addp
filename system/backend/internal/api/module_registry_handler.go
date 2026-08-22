package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commonauthmiddleware "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
// @Description  平台 Service Principal 只能注册与自身 OAuth Client 对应的模块定义和当前进程实例；注册不覆盖管理员 enabled 状态 | A platform service principal can only register its matching module definition and current process instance; registration never overrides administrator enabled state
// @Tags         运行时注册 | Runtime Registry
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.ModuleRegistrationRequest true "模块注册信息 | Module registration"
// @Success      200 {object} object{message=string,module=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
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
		if errors.Is(err, service.ErrInvalidModuleRegistration) {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgModuleRegistrationInvalid)})
			return
		}
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
// @Failure      404 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
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
	if err := h.service.SendHeartbeat(req.ModuleName, req.InstanceID); err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgModuleRuntimeInstanceMissing)})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgModuleHeartbeat), "module": req.ModuleName})
}

// ListModulesService godoc
// @Summary      查询平台注册模块 | List registered platform modules
// @Description  平台 Service Principal 查询持久模块定义及运行实例；status=up 仅返回已启用且存在有效 Backend 租约的模块 | A platform service principal lists persistent module definitions and runtime instances; status=up returns only enabled modules with a valid Backend lease
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

// ListModulesPlatform godoc
// @Summary      查询平台模块定义与运行实例 | List platform module definitions and runtime instances
// @Description  平台系统管理员读取持久模块定义及 Backend、Worker、Scheduler 当前租约投影 | A platform system administrator reads persistent module definitions and current Backend, Worker, and Scheduler lease projections
// @Tags         平台模块管理 | Platform Module Management
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} object{modules=[]models.ModuleInfo,count=int}
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.module.read"]
// @Router       /platform/modules [get]
func (h *ModuleRegistryHandler) ListModulesPlatform(c *gin.Context) {
	modules, err := h.service.ListModules()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"modules": modules, "count": len(modules)})
}

// GetModulePlatform godoc
// @Summary      查询平台模块详情 | Get platform module details
// @Tags         平台模块管理 | Platform Module Management
// @Produce      json
// @Security     BearerAuth
// @Param        module_name path string true "模块名 | Module name"
// @Success      200 {object} models.ModuleInfo
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.module.read"]
// @Router       /platform/modules/{module_name} [get]
func (h *ModuleRegistryHandler) GetModulePlatform(c *gin.Context) {
	module, err := h.service.GetModule(c.Param("module_name"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgModuleNotFound)})
		return
	}
	c.JSON(http.StatusOK, module)
}

// UpdateModulePlatform godoc
// @Summary      更新平台模块启用状态 | Update platform module enabled state
// @Description  只更新管理员 enabled 意图；运行实例健康仍由注册、心跳和租约决定 | Updates only the administrator enabled intent; runtime instance health remains determined by registration, heartbeat, and lease
// @Tags         平台模块管理 | Platform Module Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        module_name path string true "模块名 | Module name"
// @Param        request body models.ModuleDefinitionUpdateRequest true "模块定义更新 | Module definition update"
// @Success      200 {object} models.ModuleInfo
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.module.update"]
// @Router       /platform/modules/{module_name} [put]
func (h *ModuleRegistryHandler) UpdateModulePlatform(c *gin.Context) {
	var req models.ModuleDefinitionUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	module, err := h.service.UpdateModuleDefinition(c.Param("module_name"), &req)
	switch {
	case errors.Is(err, service.ErrModuleDefinitionVersionConflict):
		c.JSON(http.StatusConflict, gin.H{
			"error": commoni18n.T(c, sysi18n.MsgModuleVersionConflict), "error_code": "resource_version_conflict",
		})
		return
	case errors.Is(err, service.ErrInvalidModuleRegistration):
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgModuleRegistrationInvalid)})
		return
	case err != nil:
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgModuleNotFound)})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, module)
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
