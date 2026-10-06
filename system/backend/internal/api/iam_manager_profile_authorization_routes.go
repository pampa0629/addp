package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type iamManagerProfileAuthorizationService interface {
	Issue(context.Context, iam.IssueManagerProfileAuthorizationInput) (*iam.IssuedManagerProfileAuthorization, error)
	Authorize(context.Context, iam.AuthorizeManagerProfileInput) (time.Time, error)
}

type IAMManagerProfileAuthorizationHandler struct {
	service iamManagerProfileAuthorizationService
}

type IAMIssueManagerProfileRequest struct {
	ExecutionID string `json:"execution_id"`
	ExpiresIn   int64  `json:"expires_in,omitempty"`
}

type IAMIssuedManagerProfileResponse struct {
	ID              string                            `json:"id"`
	ExecutionID     string                            `json:"execution_id"`
	TenantID        string                            `json:"tenant_id"`
	Audience        string                            `json:"audience"`
	SourceReadScope execution.ManagerProfileReadScope `json:"source_read_scope"`
	ExpiresAt       time.Time                         `json:"expires_at"`
}

type IAMManagerProfileAccessRequest struct {
	ExecutionID     string                            `json:"execution_id"`
	Attempt         int                               `json:"attempt"`
	LeaseToken      string                            `json:"lease_token"`
	SourceReadScope execution.ManagerProfileReadScope `json:"source_read_scope"`
}

type IAMManagerProfileAccessResponse struct {
	ObservedAt time.Time `json:"observed_at"`
}

func RegisterIAMManagerProfileAuthorizationRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *IAMManagerProfileAuthorizationHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || runtime.ServiceCredential == nil || handler == nil || handler.service == nil {
		return errors.New("Manager execution authorization dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	service, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	manager, err := middleware.NewIAMClientGuard("addp-manager")
	if err != nil {
		return err
	}
	issue, err := middleware.NewIAMPermissionGuard("manager.data_profile.execute", "manager.data_item.read")
	if err != nil {
		return err
	}
	consume, err := middleware.NewIAMPermissionGuard("system.execution_authorization.execute")
	if err != nil {
		return err
	}
	api.POST("/auth/execution-authorizations/manager-profiles", runtime.Authentication, runtime.UserAccessCredential, tenant, issue, handler.Issue)
	api.POST("/execution-authorizations/:id/manager-profile-accesses", runtime.Authentication, runtime.ServiceCredential, service, manager, consume, handler.Authorize)
	return nil
}

func bindManagerProfileRequest(c *gin.Context, dst any) error {
	if c.Request.URL.RawQuery != "" {
		return commonapi.ErrBadRequest
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	if commonapi.BindOptionalJSONStrict(c, dst) != nil {
		return commonapi.ErrBadRequest
	}
	return nil
}

// Issue godoc
// @Summary 签发 Manager 剖析执行授权 | Issue Manager profiling execution authorization
// @Description 从当前普通 API User 和精确 pending execution 反查完整来源、配置及当前权限，不接受自报身份或范围；不返回连接或凭据 | Resolve complete sources, configuration and current permissions from an ordinary API User and exact pending execution; no caller-supplied identity or scope, connections or credentials
// @Tags 执行授权 | Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body IAMIssueManagerProfileRequest true "执行 UUID 与有效期秒数，默认 900、最大 3600；正文上限 512 KiB，无 query | Execution UUID and TTL seconds, default 900, maximum 3600; 512 KiB body limit, no query"
// @Success 201 {object} IAMIssuedManagerProfileResponse "固定只读来源范围 | Fixed read-only source boundary"
// @Failure 400,401,403,409,500 {object} IAMErrorResponse "签发失败 | Issuance failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_profile.execute","manager.data_item.read"]
// @Router /auth/execution-authorizations/manager-profiles [post]
func (h *IAMManagerProfileAuthorizationHandler) Issue(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var request IAMIssueManagerProfileRequest
	if bindManagerProfileRequest(c, &request) != nil || request.ExpiresIn < 0 || request.ExpiresIn > 3600 {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	result, err := h.service.Issue(c.Request.Context(), iam.IssueManagerProfileAuthorizationInput{
		SourceAccessToken: sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), ExecutionID: executionID,
		ExpiresIn: time.Duration(request.ExpiresIn) * time.Second, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	if result == nil || result.ID <= 0 || result.TenantID != int64(tenantID) || result.ExecutionID != executionID || result.ExpiresAt.IsZero() || result.SourceReadScope.Validate() != nil {
		respondExecutionAuthorizationError(c, errors.New("incomplete Manager execution authorization"))
		return
	}
	c.JSON(http.StatusCreated, IAMIssuedManagerProfileResponse{ID: strconv.FormatInt(result.ID, 10), ExecutionID: result.ExecutionID.String(),
		TenantID: strconv.FormatInt(result.TenantID, 10), Audience: "manager", SourceReadScope: *result.SourceReadScope.Clone(), ExpiresAt: result.ExpiresAt.UTC()})
}

// Authorize godoc
// @Summary 复核 Manager 剖析执行的当前完整来源权限 | Recheck current complete source permissions for Manager profiling
// @Description 仅固定 addp-manager Tenant Service 凭据；核验真实 running attempt、租约、完整配置和全部现行源规则。仅返回当次观察时刻，不是访问租约或源连接 | Only the fixed addp-manager Tenant Service credential; checks the actual running attempt, lease, complete configuration and all current source rules. Returns only a point-in-time observation, not an access lease or source connection
// @Tags 执行授权 | Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "规范正整数授权 ID | Canonical positive authorization ID"
// @Param request body IAMManagerProfileAccessRequest true "精确执行租约与相同来源范围；正文上限 512 KiB，无 query | Exact execution lease and identical source boundary; 512 KiB body limit, no query"
// @Success 200 {object} IAMManagerProfileAccessResponse "不可缓存的当次观察 | Non-cacheable current observation"
// @Failure 400,401,403,500 {object} IAMErrorResponse "复核失败 | Recheck failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router /execution-authorizations/{id}/manager-profile-accesses [post]
func (h *IAMManagerProfileAuthorizationHandler) Authorize(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := parseCanonicalIAMInt64(c.Param("id"))
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	var request IAMManagerProfileAccessRequest
	if bindManagerProfileRequest(c, &request) != nil || request.Attempt <= 0 || request.SourceReadScope.Validate() != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	lease, err := parseCanonicalExecutionUUID(request.LeaseToken)
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	principal, tenant, principalType, err := iamTenantActor(c)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	current, ok := middleware.IAMAuthContextFromGin(c)
	if !ok || principalType != string(iam.PrincipalTypeServicePrincipal) || current.Token.Type != middleware.IAMTokenTypeServiceAccess || current.Client.ClientID == nil || *current.Client.ClientID != "addp-manager" {
		respondExecutionAuthorizationError(c, commonapi.ErrForbidden)
		return
	}
	observed, err := h.service.Authorize(c.Request.Context(), iam.AuthorizeManagerProfileInput{
		AuthorizationID: id, ExecutionID: executionID, Attempt: request.Attempt, LeaseToken: lease, SourceReadScope: request.SourceReadScope,
		ServicePrincipalID: int64(principal), ServiceClientID: *current.Client.ClientID, TenantID: int64(tenant), Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	if observed.IsZero() {
		respondExecutionAuthorizationError(c, errors.New("incomplete Manager execution observation"))
		return
	}
	c.JSON(http.StatusOK, IAMManagerProfileAccessResponse{ObservedAt: observed.UTC()})
}
