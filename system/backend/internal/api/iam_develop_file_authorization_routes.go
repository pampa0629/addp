package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/jsonmap"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type iamDevelopFileAuthorizationService interface {
	Issue(context.Context, iam.IssueDevelopFileAuthorizationInput) (*client.IssuedDevelopFileAuthorization, error)
	Check(context.Context, iam.CheckDevelopFileInput) (*client.DevelopFileAdmissionObservation, *client.DevelopFileSourceAccessObservation, error)
}
type IAMDevelopFileAuthorizationHandler struct {
	service iamDevelopFileAuthorizationService
}
type IAMIssueDevelopFileRequest struct {
	ExecutionID  string `json:"execution_id"`
	IntentDigest string `json:"intent_digest"`
}

func RegisterIAMDevelopFileAuthorizationRoutes(api *gin.RouterGroup, runtime *IAMRuntime, h *IAMDevelopFileAuthorizationHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || runtime.ServiceCredential == nil || h == nil || h.service == nil {
		return errors.New("Develop file authorization dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	service, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	develop, err := middleware.NewIAMClientGuard("addp-develop")
	if err != nil {
		return err
	}
	issue, err := middleware.NewIAMPermissionGuard("develop.task.execute", "develop.data_read.execute")
	if err != nil {
		return err
	}
	consume, err := middleware.NewIAMPermissionGuard("system.execution_authorization.execute")
	if err != nil {
		return err
	}
	api.POST("/auth/execution-authorizations/develop-geopackage-workflows", runtime.Authentication, runtime.UserAccessCredential, tenant, issue, h.Issue)
	api.POST("/execution-authorizations/:id/develop-geopackage-admission-checks", runtime.Authentication, runtime.ServiceCredential, service, develop, consume, h.Admission)
	api.POST("/execution-authorizations/:id/develop-geopackage-source-accesses", runtime.Authentication, runtime.ServiceCredential, service, develop, consume, h.Source)
	return nil
}

// Issue godoc
// @Summary 签发 Develop 精确文件执行授权 | Issue Develop exact file execution authorization
// @Description 当前普通 API User；独立读取 Owner 冻结 Intent 和 Meta 完整内容边界，核当前人员、精确源规则和原 pending，封存同一 EA；不新增源 Grant 或目标写权 | Ordinary API User only; independently reads frozen Owner intent and complete Meta content facts, checks current actor, exact source rules and original pending execution, and seals one EA without source Grants or target write access
// @Tags 执行授权 | Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body IAMIssueDevelopFileRequest true "原执行 UUID 与预期 Intent 摘要；正文 512 KiB，无 query | Original execution UUID and expected Intent digest; 512 KiB body, no query"
// @Success 201 {object} client.IssuedDevelopFileAuthorization "封存的来源读取范围 | Sealed source read scope"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "拒绝或依赖不可用 | Rejected or dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute","develop.data_read.execute"]
// @Router /auth/execution-authorizations/develop-geopackage-workflows [post]
func (h *IAMDevelopFileAuthorizationHandler) Issue(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var request IAMIssueDevelopFileRequest
	if bindDevelopFileRequest(c, &request) != nil || len(request.IntentDigest) != 64 {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	id, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	actor, tenant, err := iamTenantUserActor(c)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	result, err := h.service.Issue(c.Request.Context(), iam.IssueDevelopFileAuthorizationInput{SourceAccessToken: sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), ExecutionID: id, IntentDigest: request.IntentDigest, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated)})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	current, ok := middleware.IAMAuthContextFromGin(c)
	if !ok || current.Context.TenantMembershipID == nil {
		respondExecutionAuthorizationError(c, commonapi.ErrUnauthorized)
		return
	}
	if result.Validate() != nil || result.Authorization.ExecutionID != request.ExecutionID || result.Authorization.TenantID != strconv.FormatUint(uint64(tenant), 10) || result.Authorization.ActorPrincipalID != strconv.FormatUint(uint64(actor), 10) || result.Authorization.TenantMembershipID != *current.Context.TenantMembershipID || result.Authorization.IssuedAuthorizationVersion != current.Authorization.AuthorizationVersion {
		respondExecutionAuthorizationError(c, errors.New("invalid Develop file authorization response"))
		return
	}
	c.JSON(http.StatusCreated, result)
}

// Admission godoc
// @Summary 观察 Develop 文件执行当前准入资格 | Observe current Develop file execution eligibility
// @Description 固定 addp-develop Tenant Service；复核原 EA、Scope、pending、主体和当前源规则；无连接、不放行发布。未实现正式目标批准时保存范围关闭 | Fixed addp-develop Tenant Service; rechecks original EA, scope, pending execution, actor and current source rules; no connection or publication release. Save scopes remain closed until formal approval consumption is implemented
// @Tags 执行授权 | Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "规范正整数 EA ID | Canonical positive EA ID"
// @Param request body client.DevelopFileAdmissionRequest true "原执行、预期 attempt 与同一范围摘要；512 KiB，无 query | Original execution, expected attempt and identical scope digest; 512 KiB, no query"
// @Success 200 {object} client.DevelopFileAdmissionObservation "当次资格观察，非缓存许可 | Current eligibility observation, not cached permission"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "拒绝或依赖不可用 | Rejected or dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router /execution-authorizations/{id}/develop-geopackage-admission-checks [post]
func (h *IAMDevelopFileAuthorizationHandler) Admission(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var request client.DevelopFileAdmissionRequest
	if bindDevelopFileRequest(c, &request) != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	input, err := developFileCheckInput(c, request.ExecutionID, request.ExpectedAttempt, request.ScopeDigest)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	observation, access, err := h.service.Check(c.Request.Context(), input)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	if observation == nil || access != nil {
		respondExecutionAuthorizationError(c, errors.New("invalid Develop admission observation"))
		return
	}
	c.JSON(http.StatusOK, observation)
}

// Source godoc
// @Summary 复核 Develop 原执行的精确文件读取 | Recheck exact file read for original Develop execution
// @Description 固定 addp-develop 当前机器凭据；核同一 Scope、当前 User、完整源规则、真实 running attempt/lease、StartedAt 固定截止及同版本 NFS；只返回指定源连接、不可续期的 business_deadline 与受当前租约／机器凭据限制的 expires_at。元数据边界不能用作内容许可 | Fixed current addp-develop machine credential; checks identical scope, current User, complete source rules, actual running attempt and lease, original StartedAt deadline and unchanged NFS version. Returns only the selected source connection, a sealed business_deadline independent of renewal, and expires_at bounded by the current lease and machine credential; metadata boundaries never grant content access
// @Tags 执行授权 | Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "规范正整数 EA ID | Canonical positive EA ID"
// @Param request body client.DevelopFileSourceAccessRequest true "同一执行租约、范围摘要与精确源；512 KiB，无 query | Identical execution lease, scope digest and exact source; 512 KiB, no query"
// @Success 200 {object} client.DevelopFileSourceAccessObservation "当次精确读取观察，非引擎级授权 | Current exact read observation, not engine-wide authorization"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "拒绝或依赖不可用 | Rejected or dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router /execution-authorizations/{id}/develop-geopackage-source-accesses [post]
func (h *IAMDevelopFileAuthorizationHandler) Source(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var request client.DevelopFileSourceAccessRequest
	if bindDevelopFileRequest(c, &request) != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	input, err := developFileCheckInput(c, request.ExecutionID, request.Attempt, request.ScopeDigest)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	lease, err := parseCanonicalExecutionUUID(request.LeaseToken)
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	input.LeaseToken = lease
	input.Source = &request.Source
	admission, observation, err := h.service.Check(c.Request.Context(), input)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	if observation == nil || admission != nil || observation.Engine == nil {
		respondExecutionAuthorizationError(c, errors.New("invalid Develop source observation"))
		return
	}
	c.JSON(http.StatusOK, observation)
}

func developFileCheckInput(c *gin.Context, executionID string, attempt int, digest string) (iam.CheckDevelopFileInput, error) {
	id, err := parseCanonicalIAMInt64(c.Param("id"))
	if err != nil || attempt <= 0 || len(digest) != 64 {
		return iam.CheckDevelopFileInput{}, commonapi.ErrBadRequest
	}
	executionIDParsed, err := parseCanonicalExecutionUUID(executionID)
	if err != nil {
		return iam.CheckDevelopFileInput{}, commonapi.ErrBadRequest
	}
	_, tenant, kind, err := iamTenantActor(c)
	if err != nil || kind != string(iam.PrincipalTypeServicePrincipal) {
		return iam.CheckDevelopFileInput{}, commonapi.ErrForbidden
	}
	return iam.CheckDevelopFileInput{ServiceAccessToken: sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), AuthorizationID: id, ExecutionID: executionIDParsed, TenantID: int64(tenant), ExpectedAttempt: attempt, ScopeDigest: digest, Audit: iamAuditMetadataWithStatus(c, http.StatusOK)}, nil
}

// This purpose requires a single closed object with no duplicate keys at any
// depth; the general optional-JSON binder alone does not enforce uniqueness.
func bindDevelopFileRequest(c *gin.Context, dst any) error {
	if c.Request.URL.RawQuery != "" {
		return commonapi.ErrBadRequest
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 512<<10+1))
	if err != nil || len(body) > 512<<10 || !utf8.Valid(body) {
		return commonapi.ErrBadRequest
	}
	if _, err := jsonmap.DecodeObjectStrict(body, 512<<10, 64); err != nil {
		return commonapi.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil {
		return commonapi.ErrBadRequest
	}
	return nil
}
