package api

import (
	"context"
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	modulei18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type initialProductionGrantService interface {
	IssueInitialTransferGrant(context.Context, engineaccess.ProductionRuntimeActor, string) (*shared.InitialProductionResult, error)
	ResolveInitialTransferGrant(context.Context, engineaccess.ProductionRuntimeActor, string) (*shared.InitialProductionLookup, error)
}
type EngineAccessInitialGrantHandler struct{ service initialProductionGrantService }

func RegisterEngineAccessInitialGrantRoutes(api *gin.RouterGroup, runtime *IAMRuntime, h *EngineAccessInitialGrantHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.ServiceCredential == nil || h == nil || h.service == nil {
		return errors.New("initial production grant dependencies required")
	}
	tenant, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessInitialGrantExecute)
	if err != nil {
		return err
	}
	routes := api.Group("/runtime/initial_production_grants/transfer/:execution_id")
	routes.Use(runtime.Authentication, runtime.ServiceCredential, tenant, sharedauth.MustNewServiceClientGuard("addp-transfer"), permission)
	routes.GET("", h.Resolve)
	routes.POST("", h.Issue)
	return nil
}

func initialGrantRuntimeInput(c *gin.Context) (engineaccess.ProductionRuntimeActor, bool) {
	var actor engineaccess.ProductionRuntimeActor
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return actor, false
	}
	id, err := uuid.Parse(c.Param("execution_id"))
	if err != nil || id == uuid.Nil || id.String() != c.Param("execution_id") {
		respondIAMError(c, commonapi.ErrBadRequest)
		return actor, false
	}
	ac, ok := sharedauth.AuthContextFromGin(c)
	if !ok || ac.Principal.Type != "service_principal" || ac.Context.Type != "tenant" || ac.Context.TenantID == nil || ac.Context.TenantMembershipID == nil || ac.Client.ClientID == nil || *ac.Client.ClientID != "addp-transfer" {
		respondIAMError(c, commonapi.ErrForbidden)
		return actor, false
	}
	for index, text := range []string{*ac.Context.TenantID, ac.Principal.ID, *ac.Context.TenantMembershipID, ac.Authorization.AuthorizationVersion} {
		id, err := parseIAMDecimalID(text)
		if err != nil {
			respondIAMError(c, commonapi.ErrForbidden)
			return actor, false
		}
		values := []*int64{&actor.TenantID, &actor.PrincipalID, &actor.MembershipID, &actor.AuthorizationVersion}
		*values[index] = id
	}
	actor.ClientID, actor.TokenExpiresAt = *ac.Client.ClientID, ac.Token.ExpiresAt
	return actor, true
}

// Issue godoc
// @Summary 续办 PG 产出初始只读授权 | Continue initial read authorization for PG production
// @Description 仅固定 addp-transfer 当前租户机器；无请求体，按原执行反查证据。返回不可变签发或拒绝终态；暂时故障不生成终态。同执行重试不续期或恢复撤权 | Fixed current Tenant addp-transfer only; no request body, evidence is resolved by original execution. Returns immutable issued or denied history; temporary failures create no terminal outcome. Replay never renews expiry or revives a revoked Grant
// @Tags 源授权办理 | Source Access Fulfillment
// @Produce json
// @Security BearerAuth
// @Param execution_id path string true "原执行 UUID | Original execution UUID"
// @Success 200 {object} shared.InitialProductionResult
// @Failure 400,401,403,503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_initial_grant.execute"]
// @Router /runtime/initial_production_grants/transfer/{execution_id} [post]
func (h *EngineAccessInitialGrantHandler) Issue(c *gin.Context) {
	actor, ok := initialGrantRuntimeInput(c)
	if !ok {
		return
	}
	result, err := h.service.IssueInitialTransferGrant(c.Request.Context(), actor, c.Param("execution_id"))
	if err != nil {
		respondInitialGrantError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Resolve godoc
// @Summary 核清 PG 原产出的初始授权终态 | Resolve initial authorization history for original PG production
// @Description 仅固定 addp-transfer 当前租户机器；只读、不依赖 Meta 或原人类当前资格，不签发或撤销。found=false 仅表示尚无终态，不等于拒绝或允许 | Fixed current Tenant addp-transfer only; read-only, independent of Meta and historical human qualification; no issuance or withdrawal. found=false means no terminal result, never denial or Allow
// @Tags 源授权办理 | Source Access Fulfillment
// @Produce json
// @Security BearerAuth
// @Param execution_id path string true "原执行 UUID | Original execution UUID"
// @Success 200 {object} shared.InitialProductionLookup
// @Failure 400,401,403,503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_initial_grant.execute"]
// @Router /runtime/initial_production_grants/transfer/{execution_id} [get]
func (h *EngineAccessInitialGrantHandler) Resolve(c *gin.Context) {
	actor, ok := initialGrantRuntimeInput(c)
	if !ok {
		return
	}
	result, err := h.service.ResolveInitialTransferGrant(c.Request.Context(), actor, c.Param("execution_id"))
	if err != nil {
		respondInitialGrantError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func respondInitialGrantError(c *gin.Context, err error) {
	if errors.Is(err, commonapi.ErrBadRequest) || errors.Is(err, commonapi.ErrUnauthorized) || errors.Is(err, commonapi.ErrForbidden) {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, modulei18n.MsgInitialProductionPending), "error_code": "initial_production_pending"})
}
