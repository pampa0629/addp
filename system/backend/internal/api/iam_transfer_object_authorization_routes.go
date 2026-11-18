package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type transferObjectAuthorizationService interface {
	Issue(context.Context, iam.IssueTransferObjectAuthorizationInput) (*iam.IssuedTransferObjectAuthorization, error)
	Authorize(context.Context, iam.AuthorizeTransferObjectInput) (*iam.ExecutionSourceReadObservation, error)
}

type transferObjectAuthorizationHandler struct {
	service transferObjectAuthorizationService
}

func registerTransferObjectAuthorizationRoutes(api *gin.RouterGroup, runtime *IAMRuntime, service transferObjectAuthorizationService) error {
	if service == nil {
		return errors.New("Transfer object import authorization service required")
	}
	handler := &transferObjectAuthorizationHandler{service: service}
	issue, consume, err := exactSourceAuthorizationHandlers(api, runtime, "addp-transfer", handler.Issue, handler.Authorize, "transfer.task.execute", "manager.data_item.read", "develop.data_write.execute")
	if err != nil {
		return err
	}
	api.POST("/auth/execution-authorizations/transfer-objects", issue...)
	api.POST("/execution-authorizations/:id/transfer-object-accesses", consume...)
	return nil
}

// Issue godoc
// @Summary 签发 Transfer 单对象导入执行授权 | Issue Transfer single-object import execution authorization
// @Description 从当前用户的待执行 Transfer 单对象导入读取冻结配置，核对精确对象 Grant 与目标写入权限 | Read the current user frozen pending Transfer import and check exact object Grant and target write permission
// @Tags 执行授权 | Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body commonclient.IssueManagerFileAuthorizationRequest true "执行 UUID 和可选有效期秒数 | Execution UUID and optional lifetime in seconds"
// @Success 201 {object} commonclient.IssuedTransferObjectAuthorization "精确源对象执行授权 | Exact source object execution authorization"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "签发失败 | Issuance failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.execute","manager.data_item.read","develop.data_write.execute"]
// @Router /auth/execution-authorizations/transfer-objects [post]
func (h *transferObjectAuthorizationHandler) Issue(c *gin.Context) {
	var request commonclient.IssueManagerFileAuthorizationRequest
	if bindManagerFileRequest(c, &request, "execution_id", "expires_in") != nil || request.ExpiresIn < 0 || request.ExpiresIn > 3600 {
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
	result, err := h.service.Issue(c.Request.Context(), iam.IssueTransferObjectAuthorizationInput{
		SourceAccessToken: sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), ExecutionID: executionID,
		ExpiresIn: time.Duration(request.ExpiresIn) * time.Second, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	if result == nil || result.ID <= 0 || result.ExecutionID != executionID || result.TenantID != int64(tenantID) ||
		result.SourceReadScope.Validate() != nil || !result.ExpiresAt.After(time.Now()) {
		respondExecutionAuthorizationError(c, errors.New("incomplete Transfer object import authorization"))
		return
	}
	c.JSON(http.StatusCreated, commonclient.IssuedTransferObjectAuthorization{
		ID: strconv.FormatInt(result.ID, 10), ExecutionID: result.ExecutionID.String(), TenantID: strconv.FormatInt(result.TenantID, 10),
		Audience: execution.AudienceTransfer, SourceReadScope: *result.SourceReadScope.Clone(), ExpiresAt: result.ExpiresAt.UTC(),
	})
}

// Authorize godoc
// @Summary 复核 Transfer 单对象导入当前源权限 | Recheck current Transfer single-object import source permission
// @Description 仅固定 addp-transfer Tenant Service，核对实际运行租约、冻结配置、引擎版本和当前源 Grant。只返回当次受控读取所需连接 | Fixed addp-transfer Tenant Service only. Check the active lease, frozen configuration, engine version and current source Grant. Return the connection only for this guarded read
// @Tags 执行授权 | Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "规范正整数授权 ID | Canonical positive authorization ID"
// @Param request body commonclient.TransferObjectAccessRequest true "真实执行租约及冻结源范围 | Actual execution lease and frozen source scope"
// @Success 200 {object} commonclient.ManagerFileAccessObservation "不可缓存的当前源引擎观察 | Non-cacheable current source engine observation"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "复核失败 | Recheck failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router /execution-authorizations/{id}/transfer-object-accesses [post]
func (h *transferObjectAuthorizationHandler) Authorize(c *gin.Context) {
	id, idErr := parseCanonicalIAMInt64(c.Param("id"))
	var request commonclient.TransferObjectAccessRequest
	if idErr != nil || bindManagerFileRequest(c, &request, "execution_id", "attempt", "lease_token", "source_read_scope") != nil ||
		request.Attempt <= 0 || request.SourceReadScope.Validate() != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	executionID, executionErr := parseCanonicalExecutionUUID(request.ExecutionID)
	lease, leaseErr := parseCanonicalExecutionUUID(request.LeaseToken)
	if executionErr != nil || leaseErr != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	principal, tenant, principalType, err := iamTenantActor(c)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	current, ok := middleware.IAMAuthContextFromGin(c)
	if !ok || principalType != string(iam.PrincipalTypeServicePrincipal) || current.Token.Type != middleware.IAMTokenTypeServiceAccess ||
		current.Delegation != nil || current.Client.ClientID == nil || *current.Client.ClientID != "addp-transfer" {
		respondExecutionAuthorizationError(c, commonapi.ErrForbidden)
		return
	}
	observation, err := h.service.Authorize(c.Request.Context(), iam.AuthorizeTransferObjectInput{
		AuthorizationID: id, ExecutionID: executionID, Attempt: request.Attempt, LeaseToken: lease, SourceReadScope: *request.SourceReadScope.Clone(),
		ServicePrincipalID: int64(principal), ServiceClientID: *current.Client.ClientID, TenantID: int64(tenant), Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	if observation == nil {
		respondExecutionAuthorizationError(c, errors.New("missing Transfer object import source observation"))
		return
	}
	response := commonclient.ManagerFileAccessObservation{ObservedAt: observation.ObservedAt.UTC(), Engine: observation.Engine}
	if !response.MatchesTransferObjectSource(tenant, request.SourceReadScope.ObjectSource) {
		respondExecutionAuthorizationError(c, errors.New("inconsistent Transfer object import source observation"))
		return
	}
	c.JSON(http.StatusOK, response)
}
