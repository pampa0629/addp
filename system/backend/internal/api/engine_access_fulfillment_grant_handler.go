package api

import (
	"context"
	"errors"
	"net/http"

	shared "github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	modulei18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fulfillmentGrantService interface {
	IssueFulfillmentGrant(context.Context, engineaccess.FulfillmentRuntimeActor, uuid.UUID, shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentGrant, error)
	ResolveFulfillmentGrant(context.Context, engineaccess.FulfillmentRuntimeActor, uuid.UUID, shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentGrantLookup, error)
}

type EngineAccessFulfillmentGrantHandler struct{ service fulfillmentGrantService }

// Issue godoc
// @Summary 签发已正式受理请求的源数据 Grant | Issue a source-data Grant for an accepted fulfillment
// @Description 仅当前 Catalog Tenant Service；完整匹配原受理绑定，在原窗口内核验人类权限与委派；同参重试只返回原签发历史，不代表当前访问允许 | Current Catalog Tenant Service only; exact accepted binding and current human Permission/delegation within the original window; identical retries return issuance history, not current access Allow
// @Tags 源授权办理 | Source Access Fulfillment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request_id path string true "原办理 UUID | Original fulfillment UUID"
// @Param request body authorization.SharingFulfillmentBinding true "完整原绑定 | Complete original binding"
// @Success 200 {object} authorization.SharingFulfillmentGrant "不可变签发历史，不是当前访问许可 | Immutable issuance history, not current access Allow"
// @Failure 400,401,403,409,500 {object} IAMErrorResponse "窗口到期、关闭与绑定冲突含稳定错误码 | Window expiry, closure and binding conflict include stable error codes"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_fulfillment.execute"]
// @Router /runtime/engine-access-fulfillments/{request_id}/grant [post]
func (h *EngineAccessFulfillmentGrantHandler) Issue(c *gin.Context) {
	actor, id, binding, ok := fulfillmentRecoveryInput(c)
	if !ok {
		return
	}
	result, err := h.service.IssueFulfillmentGrant(c.Request.Context(), actor, id, binding)
	if err != nil {
		respondFulfillmentGrantError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Resolve godoc
// @Summary 只读查询原请求的签发历史 | Resolve original Grant issuance history without writes
// @Description 仅当前 Catalog Tenant Service，严格匹配原绑定；未找到不等于关闭，不签发、不续期、不恢复撤销；历史不证明当前数据访问允许 | Current Catalog Tenant Service and exact original binding only; a miss is not closure, never issues, renews or restores a revoked Grant; history is not current access Allow
// @Tags 源授权办理 | Source Access Fulfillment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request_id path string true "原办理 UUID | Original fulfillment UUID"
// @Param request body authorization.SharingFulfillmentBinding true "完整原绑定 | Complete original binding"
// @Success 200 {object} authorization.SharingFulfillmentGrantLookup "只读历史；未找到时省略 grant | Read-only history; grant omitted on a miss"
// @Failure 400,401,403,409,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_fulfillment.execute"]
// @Router /runtime/engine-access-fulfillments/{request_id}/grant/resolve [post]
func (h *EngineAccessFulfillmentGrantHandler) Resolve(c *gin.Context) {
	actor, id, binding, ok := fulfillmentRecoveryInput(c)
	if !ok {
		return
	}
	result, err := h.service.ResolveFulfillmentGrant(c.Request.Context(), actor, id, binding)
	if err != nil {
		respondFulfillmentGrantError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func respondFulfillmentGrantError(c *gin.Context, err error) {
	var code, message string
	switch {
	case errors.Is(err, engineaccess.ErrGrantRelationExists):
		code, message = "engine_access_grant_relation_exists", modulei18n.MsgGrantRelationExists
	case errors.Is(err, engineaccess.ErrGrantWindowExpired):
		code, message = "engine_access_grant_window_expired", modulei18n.MsgGrantWindowExpired
	case errors.Is(err, engineaccess.ErrFulfillmentAlreadyClosed):
		code, message = "engine_access_fulfillment_closed", modulei18n.MsgFulfillmentClosed
	case errors.Is(err, engineaccess.ErrFulfillmentBindingConflict):
		code, message = "engine_access_fulfillment_binding_conflict", modulei18n.MsgFulfillmentBindingConflict
	default:
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusConflict, IAMErrorResponse{Error: commoni18n.T(c, message), ErrorCode: &code})
}
