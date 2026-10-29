package api

import (
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"strconv"
	"time"
)

type IAMCleanupAccessRequest struct {
	ExecutionID string                              `json:"execution_id"`
	Stage       string                              `json:"stage"`
	Attempt     int                                 `json:"attempt"`
	LeaseToken  string                              `json:"lease_token,omitempty"`
	Scope       execution.CleanupAuthorizationScope `json:"scope"`
}
type IAMCleanupAccessResponse struct {
	AuthorizationID string                              `json:"authorization_id"`
	ExecutionID     string                              `json:"execution_id"`
	TenantID        string                              `json:"tenant_id"`
	Stage           string                              `json:"stage"`
	Attempt         int                                 `json:"attempt"`
	Scope           execution.CleanupAuthorizationScope `json:"scope"`
	ExpiresAt       time.Time                           `json:"expires_at"`
}

// AuthorizeCleanup godoc
// @Summary 复核清理子执行授权 | Recheck cleanup child authorization
// @Description 仅对应 Owner 的 Tenant Runtime 可复核原用户当前清理权限、冻结请求及当前领取或效果边界，不返回引擎凭据 | Only the corresponding Owner tenant runtime may recheck the original user's current cleanup permission, frozen request and claim or effect boundary; no engine credentials are returned
// @Tags Runtime 执行授权 | Runtime Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "执行授权 ID | Execution authorization ID"
// @Param request body IAMCleanupAccessRequest true "清理与租约边界 | Cleanup and lease boundary"
// @Success 200 {object} IAMCleanupAccessResponse
// @Failure 400 {object} IAMErrorResponse
// @Failure 401 {object} IAMErrorResponse
// @Failure 403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.cleanup_authorization.execute"]
// @Router /execution-authorizations/{id}/cleanup-accesses [post]
func (h *IAMExecutionAuthorizationHandler) AuthorizeCleanup(c *gin.Context) {
	id, err := parseCanonicalIAMInt64(c.Param("id"))
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	var request IAMCleanupAccessRequest
	if commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil || request.Scope.Validate() != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	var token uuid.UUID
	if request.Stage == "effect" {
		token, err = parseCanonicalExecutionUUID(request.LeaseToken)
		if err != nil || request.Attempt <= 0 {
			respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
			return
		}
	} else if request.Stage != "claim" || request.Attempt != 0 || request.LeaseToken != "" {
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
	result, err := h.service.AuthorizeCleanup(c.Request.Context(), iam.AuthorizeCleanupInput{AuthorizationID: id, ExecutionID: executionID, Stage: request.Stage, Attempt: request.Attempt, LeaseToken: token, Scope: request.Scope, ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID, TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(http.StatusOK, IAMCleanupAccessResponse{AuthorizationID: strconv.FormatInt(result.AuthorizationID, 10), ExecutionID: result.ExecutionID.String(), TenantID: strconv.FormatInt(result.TenantID, 10), Stage: result.Stage, Attempt: result.Attempt, Scope: result.Scope, ExpiresAt: result.ExpiresAt.UTC()})
}
