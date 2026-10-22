package api

import (
	"context"
	"encoding/json"
	"errors"
	commonexecution "github.com/addp/common/execution"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/jsonmap"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type iamManagerModel3DAuthorizationService interface {
	Issue(context.Context, iam.IssueManagerModel3DAuthorizationInput) (*iam.IssuedManagerModel3DAuthorization, error)
	Authorize(context.Context, iam.AuthorizeManagerModel3DInput) (*iam.ManagerModel3DAccessObservation, error)
}

type IAMManagerModel3DAuthorizationHandler struct {
	service iamManagerModel3DAuthorizationService
}

func RegisterIAMManagerModel3DAuthorizationRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *IAMManagerModel3DAuthorizationHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || runtime.ServiceCredential == nil || handler == nil || handler.service == nil {
		return errors.New("Manager model3d execution authorization dependencies required")
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
	issue, err := middleware.NewIAMPermissionGuard("manager.derived_artifact.create", "manager.data_item.read")
	if err != nil {
		return err
	}
	consume, err := middleware.NewIAMPermissionGuard("system.execution_authorization.execute")
	if err != nil {
		return err
	}
	noStore := func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() }
	api.POST("/auth/execution-authorizations/manager-model3d-glb", noStore, runtime.Authentication, runtime.UserAccessCredential, tenant, issue, handler.Issue)
	api.POST("/execution-authorizations/:id/manager-model3d-glb-accesses", noStore, runtime.Authentication, runtime.ServiceCredential, service, manager, consume, handler.Authorize)
	return nil
}

// Keep the canonical SDK DTOs while rejecting duplicate members and casing
// aliases before encoding/json can silently select a value. No query scope.
func bindManagerModel3DRequest(c *gin.Context, dst any, allowed ...string) error {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.Body == nil {
		return commonapi.ErrBadRequest
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return commonapi.ErrBadRequest
	}
	fields, err := jsonmap.DecodeObjectStrict(raw, 512<<10, 64)
	if err != nil {
		return commonapi.ErrBadRequest
	}
	for key, value := range fields {
		if value == nil || !slices.Contains(allowed, key) {
			return commonapi.ErrBadRequest
		}
	}
	if json.Unmarshal(raw, dst) != nil {
		return commonapi.ErrBadRequest
	}
	return nil
}

// Issue godoc
// @Summary 签发 Manager 精确组成模型 GLB 执行授权 | Issue Manager exact-composition model GLB execution authorization
// @Description 仅当前普通第一方或 OAuth API User，按精确 pending execution 反查任务版本、完整配置和 Meta 文件组成，并核现行源规则。不接受自报身份或来源，不返回连接或凭据 | Current ordinary first-party or OAuth API User only. Resolve task version, complete configuration and Meta file composition from the exact pending execution and check current source rules. No caller-supplied identity or source, connections or credentials
// @Tags 执行授权 | Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body commonclient.IssueManagerModel3DAuthorizationRequest true "执行 UUID 与有效期秒数，默认900、最大3600；正文上限512 KiB，无query，拒绝未知或重复字段 | Execution UUID and TTL seconds, default 900, maximum 3600; 512 KiB body limit, no query, unknown or duplicate fields"
// @Success 201 {object} commonclient.IssuedManagerModel3DAuthorization "固定完整配置和文件组成范围 | Frozen complete configuration and file composition scope"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "签发失败 | Issuance failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create","manager.data_item.read"]
// @Router /auth/execution-authorizations/manager-model3d-glb [post]
func (h *IAMManagerModel3DAuthorizationHandler) Issue(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var request commonclient.IssueManagerModel3DAuthorizationRequest
	if bindManagerModel3DRequest(c, &request, "execution_id", "expires_in") != nil || request.ExpiresIn < 0 || request.ExpiresIn > 3600 {
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
	current, ok := middleware.IAMAuthContextFromGin(c)
	ordinary := ok && current.Delegation == nil && len(current.Client.Audiences) == 1 && current.Client.Audiences[0] == "addp.api" &&
		((current.Token.Type == middleware.IAMTokenTypeFirstPartyAccess && current.Client.ScopeMode == "unrestricted" && len(current.Client.Scopes) == 0) ||
			(current.Token.Type == middleware.IAMTokenTypeOAuthAccess && current.Client.ScopeMode == "restricted" && slices.Contains(current.Client.Scopes, "addp.api")))
	if !ordinary {
		respondExecutionAuthorizationError(c, commonapi.ErrForbidden)
		return
	}
	result, err := h.service.Issue(c.Request.Context(), iam.IssueManagerModel3DAuthorizationInput{SourceAccessToken: sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), ExecutionID: executionID, ExpiresIn: time.Duration(request.ExpiresIn) * time.Second, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated)})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	if result == nil || result.ID <= 0 || result.ExecutionID != executionID || result.TenantID != int64(tenantID) || !result.ExpiresAt.After(time.Now()) || result.ExpiresAt.After(time.Now().Add(time.Hour+5*time.Second)) || result.SourceReadScope.Validate() != nil {
		respondExecutionAuthorizationError(c, errors.New("incomplete Manager model3d execution authorization"))
		return
	}
	c.JSON(http.StatusCreated, commonclient.IssuedManagerModel3DAuthorization{ID: strconv.FormatInt(result.ID, 10), ExecutionID: result.ExecutionID.String(), TenantID: strconv.FormatInt(result.TenantID, 10), Audience: "manager", SourceReadScope: *result.SourceReadScope.Clone(), ExpiresAt: result.ExpiresAt.UTC()})
}

// Authorize godoc
// @Summary 复核 Manager 精确组成模型执行的当前完整来源权限 | Recheck current complete source permissions for Manager exact-composition model execution
// @Description 仅固定 addp-manager Tenant Service 与机器执行 Permission；核验真实 running attempt、租约、配置、任务和引擎版本、完整模型组成及现行源规则。返回当次同快照 FTP/NFS/MinIO 连接，只供本次受控源 I/O，不缓存或落库 | Only fixed addp-manager Tenant Service with machine execution permission. Verify the actual running attempt, lease, configuration, task and engine versions, complete model composition and current source rules. Return the same-snapshot FTP/NFS/MinIO connection for this guarded source I/O only; never cache or persist
// @Tags 执行授权 | Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "规范正整数授权ID | Canonical positive authorization ID"
// @Param request body commonclient.ManagerModel3DAccessRequest true "精确执行租约与相同来源范围；正文上限512 KiB，无query，拒绝未知或重复字段 | Exact execution lease and identical source scope; 512 KiB body limit, no query, unknown or duplicate fields"
// @Success 200 {object} commonclient.ManagerModel3DAccessObservation "不可缓存的同次来源观察与连接 | Non-cacheable same-decision source observation and connection"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "复核失败 | Recheck failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router /execution-authorizations/{id}/manager-model3d-glb-accesses [post]
func (h *IAMManagerModel3DAuthorizationHandler) Authorize(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := parseCanonicalIAMInt64(c.Param("id"))
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	var request commonclient.ManagerModel3DAccessRequest
	if bindManagerModel3DRequest(c, &request, "execution_id", "attempt", "lease_token", "source_read_scope") != nil || request.Attempt <= 0 || request.SourceReadScope.Validate() != nil {
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
	if !ok || principalType != string(iam.PrincipalTypeServicePrincipal) || current.Token.Type != middleware.IAMTokenTypeServiceAccess || current.Delegation != nil || current.Client.ClientID == nil || *current.Client.ClientID != "addp-manager" {
		respondExecutionAuthorizationError(c, commonapi.ErrForbidden)
		return
	}
	expected := request.SourceReadScope.Clone()
	observed, err := h.service.Authorize(c.Request.Context(), iam.AuthorizeManagerModel3DInput{AuthorizationID: id, ExecutionID: executionID, Attempt: request.Attempt, LeaseToken: lease, SourceReadScope: *expected.Clone(), ServicePrincipalID: int64(principal), ServiceClientID: *current.Client.ClientID, TenantID: int64(tenant), Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	if observed == nil || observed.ObservedAt.IsZero() || observed.Engine == nil {
		respondExecutionAuthorizationError(c, errors.New("incomplete Manager model3d observation"))
		return
	}
	engine := observed.Engine
	primary, _ := expected.FileComposition.PrimaryTarget()
	version, _ := strconv.ParseInt(expected.EngineVersion, 10, 64)
	if engine.ID != primary.EngineID || engine.TenantID == nil || *engine.TenantID != tenant || engine.Version != version || !commonexecution.ManagerModel3DSourceComposition(engine.EngineType, &expected.FileComposition) || engine.LifecycleState != "active" || engine.DeletedAt != nil || len(engine.ConnectionInfo) == 0 {
		respondExecutionAuthorizationError(c, errors.New("inconsistent Manager model3d observation"))
		return
	}
	c.JSON(http.StatusOK, commonclient.ManagerModel3DAccessObservation{ObservedAt: observed.ObservedAt.UTC(), Engine: engine})
}
