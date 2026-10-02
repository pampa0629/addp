package api

import (
	"context"
	"errors"
	"net/http"

	commonauthorization "github.com/addp/common/authorization"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type engineAccessHandlingScopeService interface {
	GetHandlingScope(context.Context, engineaccess.Actor, int64) (*commonauthorization.EngineAccessHandlingScope, error)
}
type EngineAccessHandlingScopeHandler struct {
	service engineAccessHandlingScopeService
}

func RegisterEngineAccessHandlingScopeRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessHandlingScopeHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || handler == nil || handler.service == nil {
		return errors.New("engine handling scope dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessFulfillmentCreate)
	if err != nil {
		return err
	}
	routes := api.Group("/engines/:id/access_handling_scope")
	routes.Use(runtime.Authentication, runtime.UserAccessCredential, tenant)
	routes.GET("", permission, handler.Get)
	return nil
}

// Get godoc
// @Summary 核验当前源读取授权办理范围 | Check current source-read authorization handling scope
// @Description 当前 Tenant User 需独立办理 Permission 和有效引擎管理委派，等待锁后重新检查资格与到期；只返回当次观察，不授予读取、业务确认、委派、受理或 Grant，不是可复用凭据 | The current Tenant User needs independent handling permission and an effective engine management delegation. Eligibility and expiry are rechecked after lock waits. This is an observation, not read access, business confirmation, delegation, acceptance, a Grant or a reusable credential
// @Tags 源授权办理 | Source Access Fulfillment
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Success 200 {object} commonauthorization.EngineAccessHandlingScope "当前办理范围观察 | Current handling scope observation"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_fulfillment.create"]
// @Router /engines/{id}/access_handling_scope [get]
func (h *EngineAccessHandlingScopeHandler) Get(c *gin.Context) {
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	result, err := h.service.GetHandlingScope(c.Request.Context(), actor, engineID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
