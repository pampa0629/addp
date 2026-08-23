package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

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

const (
	moduleRegistrationInvalidErrorCode    = "module_registration_invalid"
	moduleRuntimeInstanceMissingErrorCode = "module_runtime_instance_not_found"
	moduleRegistryUnauthorizedErrorCode   = "module_registry_unauthorized"
	moduleRegistryForbiddenErrorCode      = "module_registry_forbidden"
	moduleRegistrationFailedErrorCode     = "module_registration_failed"
	moduleHeartbeatFailedErrorCode        = "module_heartbeat_failed"
	moduleDeregistrationFailedErrorCode   = "module_deregistration_failed"
)

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
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.update"]
// @Router       /runtime/modules [post]
func (h *ModuleRegistryHandler) RegisterService(c *gin.Context) {
	var req models.ModuleRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondModuleRegistryError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgModuleRegistrationInvalid), moduleRegistrationInvalidErrorCode)
		return
	}
	if err := iamServiceOwnsModule(c, req.ModuleName); err != nil {
		respondModuleRegistryIAMError(c, err)
		return
	}
	if err := h.service.Register(&req); err != nil {
		if errors.Is(err, service.ErrInvalidModuleRegistration) {
			respondModuleRegistryError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgModuleRegistrationInvalid), moduleRegistrationInvalidErrorCode)
			return
		}
		respondModuleRegistryError(c, http.StatusInternalServerError, commoni18n.T(c, sysi18n.MsgInternalError), moduleRegistrationFailedErrorCode)
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
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.update"]
// @Router       /runtime/modules/heartbeat [post]
func (h *ModuleRegistryHandler) HeartbeatService(c *gin.Context) {
	var req models.HeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondModuleRegistryError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgModuleRegistrationInvalid), moduleRegistrationInvalidErrorCode)
		return
	}
	if err := iamServiceOwnsModule(c, req.ModuleName); err != nil {
		respondModuleRegistryIAMError(c, err)
		return
	}
	if err := h.service.SendHeartbeat(req.ModuleName, req.InstanceID); err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			respondModuleRegistryError(c, http.StatusNotFound, commoni18n.T(c, sysi18n.MsgModuleRuntimeInstanceMissing), moduleRuntimeInstanceMissingErrorCode)
			return
		}
		respondModuleRegistryError(c, http.StatusInternalServerError, commoni18n.T(c, sysi18n.MsgInternalError), moduleHeartbeatFailedErrorCode)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgModuleHeartbeat), "module": req.ModuleName})
}

// DeregisterService godoc
// @Summary      注销当前模块运行实例 | Deregister current module runtime instance
// @Description  正常退出的模块进程将自身实例立即标记为 down；持久模块定义和实例历史保留 | A normally exiting module process immediately marks its own instance down while preserving the persistent definition and instance history
// @Tags         运行时注册 | Runtime Registry
// @Produce      json
// @Security     BearerAuth
// @Param        module_name path string true "模块名 | Module name"
// @Param        instance_id path string true "运行实例 ID | Runtime instance ID"
// @Success      204
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.update"]
// @Router       /runtime/modules/{module_name}/instances/{instance_id} [delete]
func (h *ModuleRegistryHandler) DeregisterService(c *gin.Context) {
	moduleName := c.Param("module_name")
	if err := iamServiceOwnsModule(c, moduleName); err != nil {
		respondModuleRegistryIAMError(c, err)
		return
	}
	if err := h.service.Deregister(moduleName, c.Param("instance_id")); err != nil {
		if errors.Is(err, service.ErrInvalidModuleRegistration) {
			respondModuleRegistryError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgModuleRegistrationInvalid), moduleRegistrationInvalidErrorCode)
			return
		}
		respondModuleRegistryError(c, http.StatusInternalServerError, commoni18n.T(c, sysi18n.MsgInternalError), moduleDeregistrationFailedErrorCode)
		return
	}
	c.Status(http.StatusNoContent)
}

func respondModuleRegistryIAMError(c *gin.Context, err error) {
	status := commonapi.MapErrorToHTTPStatus(err)
	switch status {
	case http.StatusUnauthorized:
		respondModuleRegistryError(c, status, commoni18n.T(c, commoni18n.MsgUnauthorized), moduleRegistryUnauthorizedErrorCode)
	case http.StatusForbidden:
		respondModuleRegistryError(c, status, commoni18n.T(c, commoni18n.MsgForbidden), moduleRegistryForbiddenErrorCode)
	default:
		respondModuleRegistryError(c, http.StatusInternalServerError, commoni18n.T(c, sysi18n.MsgInternalError), moduleRegistrationFailedErrorCode)
	}
}

func respondModuleRegistryError(c *gin.Context, status int, message, errorCode string) {
	c.JSON(status, gin.H{"error": message, "error_code": errorCode})
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

// WatchModulesService godoc
// @Summary      等待模块路由快照 | Watch module routing snapshot
// @Description  revision 变化时立即返回完整可路由 Backend 快照；等待超时也返回同 revision 的新鲜快照以更新租约投影 | Returns the complete routable Backend snapshot immediately when the revision changes; a timeout also returns a fresh snapshot at the same revision to renew lease projections
// @Tags         运行时注册 | Runtime Registry
// @Produce      json
// @Security     BearerAuth
// @Param        revision query int false "客户端当前修订号 | Client current revision" default(0)
// @Param        wait_seconds query int false "最长等待秒数，范围 0-30 | Maximum wait in seconds, range 0-30" default(10)
// @Success      200 {object} models.ModuleRoutingSnapshot
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.read"]
// @Router       /runtime/modules/watch [get]
func (h *ModuleRegistryHandler) WatchModulesService(c *gin.Context) {
	revision, err := strconv.ParseInt(c.DefaultQuery("revision", "0"), 10, 64)
	if err != nil || revision < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "revision must be a non-negative integer"})
		return
	}
	waitSeconds, err := strconv.Atoi(c.DefaultQuery("wait_seconds", "10"))
	if err != nil || waitSeconds < 0 || waitSeconds > 30 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wait_seconds must be between 0 and 30"})
		return
	}
	snapshot, err := h.service.WatchRoutingSnapshot(c.Request.Context(), revision, time.Duration(waitSeconds)*time.Second)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, snapshot)
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
		if errors.Is(err, commonapi.ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
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
