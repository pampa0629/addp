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
	GetMetaScanCancellationScope(context.Context, engineaccess.Actor, int64) (*shared.EngineAccessHandlingScope, error)
}
type MetaScanCancellationScopeHandler struct {
	service metaScanCancellationScopeService
}

func RegisterMetaScanCancellationScopeRoute(api *gin.RouterGroup, runtime *IAMRuntime, h *MetaScanCancellationScopeHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || h == nil || h.service == nil {
		return errors.New("collection management dependencies required")
	}
	api.GET("/engines/:id/scan_cancellation_scope", runtime.Authentication, runtime.UserAccessCredential, sharedauth.MustNewContextGuard("tenant"), sharedauth.MustNewPermissionGuard("meta.scan_task.cancel"), h.Check)
	return nil
}

// Check godoc
// @Summary 核验当前用户的扫描取消范围 | Check current User scan cancellation scope
// @Description 核对当前取消扫描功能权限及 PG 引擎管理范围，只返回本次观察，不批准任务或授予数据权限 | Checks the current scan cancellation permission and PG engine management scope. Returns only a current observation, not task approval or data access
// @Tags 元数据集中采集 | Centralized Metadata Collection
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Success 200 {object} shared.EngineAccessHandlingScope "当前范围观察 | Current scope observation"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.cancel"]
// @Router /engines/{id}/scan_cancellation_scope [get]
func (h *MetaScanCancellationScopeHandler) Check(c *gin.Context) {
	actor, engineID, ok := engineAccessActor(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.service.GetMetaScanCancellationScope(c.Request.Context(), actor, engineID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}
