package api

import (
	"context"
	"errors"
	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"

	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/engineaccess"
	"github.com/gin-gonic/gin"
	"net/http"
)

type metaScanCancellationScopeService interface {
	GetMetaScanManagementScope(context.Context, engineaccess.Actor, int64, string) (*shared.EngineAccessHandlingScope, error)
}
type MetaScanManagementScopeHandler struct {
	service metaScanCancellationScopeService
}

func RegisterMetaScanManagementScopeRoute(api *gin.RouterGroup, runtime *IAMRuntime, h *MetaScanManagementScopeHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || h == nil || h.service == nil {
		return errors.New("collection management dependencies required")
	}
	api.GET("/engines/:id/scan_create_scope", runtime.Authentication, runtime.UserAccessCredential, sharedauth.MustNewContextGuard("tenant"), sharedauth.MustNewPermissionGuard("meta.scan_task.create"), h.Create)
	api.GET("/engines/:id/scan_update_scope", runtime.Authentication, runtime.UserAccessCredential, sharedauth.MustNewContextGuard("tenant"), sharedauth.MustNewPermissionGuard("meta.scan_task.update"), h.Update)
	api.GET("/engines/:id/scan_delete_scope", runtime.Authentication, runtime.UserAccessCredential, sharedauth.MustNewContextGuard("tenant"), sharedauth.MustNewPermissionGuard("meta.scan_task.delete"), h.Delete)
	api.GET("/engines/:id/scan_execute_scope", runtime.Authentication, runtime.UserAccessCredential, sharedauth.MustNewContextGuard("tenant"), sharedauth.MustNewPermissionGuard("meta.scan_task.execute"), h.Execute)
	api.GET("/engines/:id/scan_cancel_scope", runtime.Authentication, runtime.UserAccessCredential, sharedauth.MustNewContextGuard("tenant"), sharedauth.MustNewPermissionGuard("meta.scan_task.cancel"), h.Cancel)
	return nil
}

// Create godoc
// @Summary 核验当前用户的扫描创建范围 | Check current User scan create scope
// @Description 核对当次功能权限与 PG 引擎管理范围，不批准任务或授予数据权限 | Checks current function permission and PG engine management scope; does not approve tasks or grant data access
// @Tags 元数据集中采集 | Centralized Metadata Collection
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Success 200 {object} shared.EngineAccessHandlingScope "当前范围观察 | Current scope observation"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.create"]
// @Router /engines/{id}/scan_create_scope [get]
func (h *MetaScanManagementScopeHandler) Create(c *gin.Context) { h.check(c, "create") }

// Update godoc
// @Summary 核验当前用户的扫描修改范围 | Check current User scan update scope
// @Description 核对当次功能权限与 PG 引擎管理范围，不批准任务或授予数据权限 | Checks current function permission and PG engine management scope; does not approve tasks or grant data access
// @Tags 元数据集中采集 | Centralized Metadata Collection
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Success 200 {object} shared.EngineAccessHandlingScope "当前范围观察 | Current scope observation"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.update"]
// @Router /engines/{id}/scan_update_scope [get]
func (h *MetaScanManagementScopeHandler) Update(c *gin.Context) { h.check(c, "update") }

// Delete godoc
// @Summary 核验当前用户的扫描删除范围 | Check current User scan delete scope
// @Description 核对当次功能权限与 PG 引擎管理范围，不批准任务或授予数据权限 | Checks current function permission and PG engine management scope; does not approve tasks or grant data access
// @Tags 元数据集中采集 | Centralized Metadata Collection
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Success 200 {object} shared.EngineAccessHandlingScope "当前范围观察 | Current scope observation"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.delete"]
// @Router /engines/{id}/scan_delete_scope [get]
func (h *MetaScanManagementScopeHandler) Delete(c *gin.Context) { h.check(c, "delete") }

// Execute godoc
// @Summary 核验当前用户的扫描执行范围 | Check current User scan execute scope
// @Description 核对当次功能权限与 PG 引擎管理范围，不批准任务或授予数据权限 | Checks current function permission and PG engine management scope; does not approve tasks or grant data access
// @Tags 元数据集中采集 | Centralized Metadata Collection
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Success 200 {object} shared.EngineAccessHandlingScope "当前范围观察 | Current scope observation"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /engines/{id}/scan_execute_scope [get]
func (h *MetaScanManagementScopeHandler) Execute(c *gin.Context) { h.check(c, "execute") }

// Cancel godoc
// @Summary 核验当前用户的扫描取消范围 | Check current User scan cancel scope
// @Description 核对当次功能权限与 PG 引擎管理范围，不批准任务或授予数据权限 | Checks current function permission and PG engine management scope; does not approve tasks or grant data access
// @Tags 元数据集中采集 | Centralized Metadata Collection
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Success 200 {object} shared.EngineAccessHandlingScope "当前范围观察 | Current scope observation"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.cancel"]
// @Router /engines/{id}/scan_cancel_scope [get]
func (h *MetaScanManagementScopeHandler) Cancel(c *gin.Context) { h.check(c, "cancel") }

func (h *MetaScanManagementScopeHandler) check(c *gin.Context, operation string) {
	actor, engineID, ok := engineAccessActor(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.service.GetMetaScanManagementScope(c.Request.Context(), actor, engineID, operation)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}
