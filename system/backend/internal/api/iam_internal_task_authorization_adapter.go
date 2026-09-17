package api

import (
	"net/http"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type IAMInternalTaskAccessRequest struct {
	ExecutionID  string                      `json:"execution_id"`
	Attempt      int                         `json:"attempt"`
	LeaseToken   string                      `json:"lease_token"`
	InternalTask execution.InternalTaskScope `json:"internal_task"`
}

type IAMInternalTaskAccessResponse struct {
	AuthorizationID string                      `json:"authorization_id"`
	ExecutionID     string                      `json:"execution_id"`
	TenantID        string                      `json:"tenant_id"`
	Audience        string                      `json:"audience"`
	Attempt         int                         `json:"attempt"`
	InternalTask    execution.InternalTaskScope `json:"internal_task"`
	ExpiresAt       time.Time                   `json:"expires_at"`
}

// AuthorizeInternalTask godoc
// @Summary 消费内部任务执行授权 | Consume internal task execution authorization
// @Description 仅 addp-ontology Tenant Runtime 可复核当前用户授权与精确运行租约，不返回引擎或 Infra 凭据 | Only the addp-ontology tenant runtime may recheck current user authorization and the exact running lease; no engine or Infra credentials are returned
// @Tags Runtime 执行授权 | Runtime Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "执行授权 ID | Execution authorization ID"
// @Param request body IAMInternalTaskAccessRequest true "内部任务与租约边界 | Internal task and lease boundary"
// @Success 200 {object} IAMInternalTaskAccessResponse
// @Failure 400 {object} IAMErrorResponse
// @Failure 401 {object} IAMErrorResponse
// @Failure 403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router /execution-authorizations/{id}/internal-task-accesses [post]
func (h *IAMExecutionAuthorizationHandler) AuthorizeInternalTask(c *gin.Context) {
	id, err := parseCanonicalIAMInt64(c.Param("id"))
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	var request IAMInternalTaskAccessRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	leaseToken, err := parseCanonicalExecutionUUID(request.LeaseToken)
	if err != nil || request.Attempt <= 0 || request.InternalTask.Validate(execution.AudienceOntology) != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		respondExecutionAuthorizationError(c, commonapi.ErrForbidden)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondExecutionAuthorizationError(c, commonapi.ErrUnauthorized)
		return
	}
	result, err := h.service.AuthorizeInternalTask(c.Request.Context(), iam.AuthorizeInternalTaskInput{
		AuthorizationID: int64(id), ExecutionID: executionID, Attempt: request.Attempt, LeaseToken: leaseToken,
		InternalTask: request.InternalTask, ServicePrincipalID: int64(principalID), TenantID: int64(tenantID),
		ServiceClientID: *authContext.Client.ClientID, Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(http.StatusOK, IAMInternalTaskAccessResponse{
		AuthorizationID: strconv.FormatInt(result.AuthorizationID, 10), ExecutionID: result.ExecutionID.String(),
		TenantID: strconv.FormatInt(result.TenantID, 10), Audience: result.Audience, Attempt: result.Attempt,
		InternalTask: result.InternalTask, ExpiresAt: result.ExpiresAt.UTC(),
	})
}
