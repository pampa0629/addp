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
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "窗口到期、关闭、绑定冲突与 Meta 核验不可用含稳定错误码 | Stable errors for window expiry, closure, binding conflict and unavailable Meta validation"
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
// @Summary 只读查询原请求的签发历史及当前阻碍 | Resolve original issuance history and current blockers without writes
// @Description 仅当前 Catalog Tenant Service，严格匹配原绑定；同一已提交快照优先返回本次签发历史，未签发时可返回 unissued_reason=relation_exists/window_expired/closed；原窗口按 System 数据库时间观察。阻碍不是失败历史或访问许可，不返回其他授权身份，不签发、撤销、续期或关闭请求 | Current Catalog Tenant Service and exact original binding only; one committed snapshot returns original issuance history first, or optional unissued_reason=relation_exists/window_expired/closed using System database time. Blockers are observations, not failure history or access Allow; no other Grant identity or mutation
// @Tags 源授权办理 | Source Access Fulfillment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request_id path string true "原办理 UUID | Original fulfillment UUID"
// @Param request body authorization.SharingFulfillmentBinding true "完整原绑定 | Complete original binding"
// @Success 200 {object} authorization.SharingFulfillmentGrantLookup "原签发优先；未签发可含当前阻碍，无阻碍省略原因 | Original issuance first; an unissued request may include a current blocker, omitted when none"
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
