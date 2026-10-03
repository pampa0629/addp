package api

import (
	"context"
	"net/http"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/engineaccess"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fulfillmentRecoveryService interface {
	ResolveFulfillment(context.Context, engineaccess.FulfillmentRuntimeActor, uuid.UUID, shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentLookup, error)
	CloseFulfillment(context.Context, engineaccess.FulfillmentRuntimeActor, uuid.UUID, shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentResolution, error)
}
type EngineAccessFulfillmentRecoveryHandler struct{ service fulfillmentRecoveryService }

type fulfillmentAcceptanceService interface {
	AcceptFulfillment(context.Context, engineaccess.FulfillmentRuntimeActor, uuid.UUID, shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentResolution, error)
}

type EngineAccessFulfillmentAcceptanceHandler struct{ service fulfillmentAcceptanceService }

// Accept godoc
// @Summary 首次受理源读取授权办理 | Accept source-read access fulfillment
// @Description 仅 Catalog Tenant Service；锁外可信反查已提交请求，事务内核验当前人类、目标批准要求和期限；回执不是 Grant | Catalog Tenant Service only; trusted committed-basis lookup outside locks and current eligibility/target/expiry verification inside the transaction; receipt is not a Grant
// @Tags 源授权办理 | Source Access Fulfillment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request_id path string true "原请求 UUID | Original request UUID"
// @Param request body authorization.SharingFulfillmentBinding true "完整绑定 | Complete binding"
// @Success 200 {object} authorization.SharingFulfillmentResolution "不可变回执，不是授权 | Immutable receipt, not granted access"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_fulfillment.execute"]
// @Router /runtime/engine-access-fulfillments/{request_id}/accept [post]
func (h *EngineAccessFulfillmentAcceptanceHandler) Accept(c *gin.Context) {
	a, id, binding, ok := fulfillmentRecoveryInput(c)
	if !ok {
		return
	}
	result, err := h.service.AcceptFulfillment(c.Request.Context(), a, id, binding)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func fulfillmentRecoveryInput(c *gin.Context) (engineaccess.FulfillmentRuntimeActor, uuid.UUID, shared.SharingFulfillmentBinding, bool) {
	var actor engineaccess.FulfillmentRuntimeActor
	var binding shared.SharingFulfillmentBinding
	if rejectTenantIDQuery(c) {
		return actor, uuid.Nil, binding, false
	}
	ac, ok := sharedauth.AuthContextFromGin(c)
	if !ok || ac.Principal.Type != "service_principal" || ac.Context.Type != "tenant" || ac.Context.TenantID == nil || ac.Context.TenantMembershipID == nil || ac.Client.ClientID == nil {
		respondIAMError(c, commonapi.ErrForbidden)
		return actor, uuid.Nil, binding, false
	}
	values := []*int64{&actor.TenantID, &actor.PrincipalID, &actor.MembershipID, &actor.AuthorizationVersion}
	for index, text := range []string{*ac.Context.TenantID, ac.Principal.ID, *ac.Context.TenantMembershipID, ac.Authorization.AuthorizationVersion} {
		id, err := parseIAMDecimalID(text)
		if err != nil {
			respondIAMError(c, commonapi.ErrForbidden)
			return actor, uuid.Nil, binding, false
		}
		*values[index] = id
	}
	actor.ClientID, actor.TokenExpiresAt = *ac.Client.ClientID, ac.Token.ExpiresAt
	id, err := uuid.Parse(c.Param("request_id"))
	if err != nil || id == uuid.Nil || id.String() != c.Param("request_id") || commonapi.BindOptionalJSONStrict(c, &binding) != nil || binding.Validate() != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return actor, uuid.Nil, binding, false
	}
	if binding.CallerPrincipalID != actor.PrincipalID {
		respondIAMError(c, commonapi.ErrForbidden)
		return actor, uuid.Nil, binding, false
	}
	return actor, id, binding, true
}

// Resolve godoc
// @Summary 核清原办理请求结果 | Resolve the original fulfillment outcome
// @Description 仅 Catalog 租户服务身份；精确匹配原绑定，只读未找到不表示关闭 | Catalog tenant service only; exact original binding; a read-only miss is not closure
// @Tags 源授权办理核清 | Source Access Fulfillment Recovery
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request_id path string true "原请求 UUID | Original request UUID"
// @Param request body authorization.SharingFulfillmentBinding true "原完整绑定 | Original complete binding"
// @Success 200 {object} authorization.SharingFulfillmentLookup "权威查询结果 | Authoritative lookup"
// @Failure 400,401,403,409,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_fulfillment.execute"]
// @Router /runtime/engine-access-fulfillments/{request_id}/resolve [post]
func (h *EngineAccessFulfillmentRecoveryHandler) Resolve(c *gin.Context) {
	a, id, binding, ok := fulfillmentRecoveryInput(c)
	if !ok {
		return
	}
	result, err := h.service.ResolveFulfillment(c.Request.Context(), a, id, binding)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Close godoc
// @Summary 核清并关闭未受理的原办理请求 | Settle and close an unaccepted original fulfillment request
// @Description 关闭与受理串行；若受理已胜出返回原回执，不重新办理或写 Grant | Serializes with acceptance; returns the winning original receipt, without new acceptance or Grant
// @Tags 源授权办理核清 | Source Access Fulfillment Recovery
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request_id path string true "原请求 UUID | Original request UUID"
// @Param request body authorization.SharingFulfillmentBinding true "原完整绑定 | Original complete binding"
// @Success 200 {object} authorization.SharingFulfillmentResolution "原不可变结果 | Original immutable outcome"
// @Failure 400,401,403,409,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_fulfillment.execute"]
// @Router /runtime/engine-access-fulfillments/{request_id}/close [post]
func (h *EngineAccessFulfillmentRecoveryHandler) Close(c *gin.Context) {
	a, id, binding, ok := fulfillmentRecoveryInput(c)
	if !ok {
		return
	}
	result, err := h.service.CloseFulfillment(c.Request.Context(), a, id, binding)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
