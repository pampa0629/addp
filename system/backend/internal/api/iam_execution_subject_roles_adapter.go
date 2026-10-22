package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type IAMExecutionSubjectRolesRequest struct {
	ExecutionID string `json:"execution_id"`
	Attempt     int    `json:"attempt"`
	LeaseToken  string `json:"lease_token"`
}
type IAMExecutionSubjectRolesResponse = execution.SubjectRoles

// ResolveSubjectRoles godoc
// @Summary 核验执行发起人的当前角色 | Observe current execution subject roles
// @Description 仅所属 Develop 或 Transfer Tenant Service 可按真实运行租约核验，不授予数据读取或原值权限 | Only the owning Develop or Transfer Tenant Service may observe roles using the current running lease; grants no data or plaintext access
// @Tags Runtime 执行授权 | Runtime Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "执行授权 ID | Execution authorization ID"
// @Param request body IAMExecutionSubjectRolesRequest true "当前执行租约 | Current execution lease"
// @Success 200 {object} IAMExecutionSubjectRolesResponse
// @Failure 400 {object} IAMErrorResponse
// @Failure 401 {object} IAMErrorResponse
// @Failure 403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router /execution-authorizations/{id}/subject-roles [post]
func (h *IAMExecutionAuthorizationHandler) ResolveSubjectRoles(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := parseCanonicalIAMInt64(c.Param("id"))
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	var request IAMExecutionSubjectRolesRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || request.Attempt <= 0 {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	leaseToken, err := parseCanonicalExecutionUUID(request.LeaseToken)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	if principalType != string(iam.PrincipalTypeServicePrincipal) {
		respondExecutionAuthorizationError(c, commonapi.ErrForbidden)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondExecutionAuthorizationError(c, commonapi.ErrUnauthorized)
		return
	}
	result, err := h.service.ResolveSubjectRoles(c.Request.Context(), iam.ResolveExecutionSubjectRolesInput{AuthorizationID: id, ExecutionID: executionID, Attempt: request.Attempt, LeaseToken: leaseToken, ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID, TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
