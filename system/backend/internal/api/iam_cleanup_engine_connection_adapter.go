package api

import (
	"context"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type IAMCleanupEngineConnectionRequest struct {
	ExecutionID string                              `json:"execution_id"`
	Attempt     int                                 `json:"attempt"`
	LeaseToken  string                              `json:"lease_token"`
	Scope       execution.CleanupAuthorizationScope `json:"scope"`
	EngineID    string                              `json:"engine_id"`
}

// AuthorizeCleanupEngineConnection godoc
// @Summary 获取原 Engine 清理连接 | Resolve the original Engine cleanup connection
// @Description 仅固定 Transfer Owner 可为当前 Engine 删除子执行取得内存内短期连接；核验当前用户删除权、父子请求与有效租约，不授予普通数据访问 | Only the fixed Transfer Owner may obtain an ephemeral connection for its current Engine deletion child; rechecks current user deletion permission, parent/child request and live leases, without granting ordinary data access
// @Tags Runtime 执行授权 | Runtime Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "执行授权 ID | Execution authorization ID"
// @Param request body IAMCleanupEngineConnectionRequest true "准确清理与连接边界 | Exact cleanup and connection boundary"
// @Success 200 {object} client.CleanupEngineConnection
// @Failure 400 {object} IAMErrorResponse
// @Failure 401 {object} IAMErrorResponse
// @Failure 403 {object} IAMErrorResponse
// @Failure 404 {object} IAMErrorResponse
// @Failure 409 {object} IAMErrorResponse
// @Failure 500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.cleanup_authorization.execute"]
// @Router /execution-authorizations/{id}/cleanup-engine-connection [post]
func (h *IAMExecutionAuthorizationHandler) AuthorizeCleanupEngineConnection(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := parseCanonicalIAMInt64(c.Param("id"))
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	var request IAMCleanupEngineConnectionRequest
	if commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil || request.Attempt <= 0 || request.Scope.Validate() != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	token, err := parseCanonicalExecutionUUID(request.LeaseToken)
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	engineID, err := parseCanonicalIAMInt64(request.EngineID)
	if err != nil {
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
	if request.Scope.Module != events.ModuleTransfer || *authContext.Client.ClientID != "addp-transfer" {
		respondExecutionAuthorizationError(c, commonapi.ErrForbidden)
		return
	}
	var engine *models.Engine
	result, err := h.service.AuthorizeCleanupEngineConnection(c.Request.Context(), iam.AuthorizeCleanupInput{
		AuthorizationID: id, ExecutionID: executionID, Stage: "effect", Attempt: request.Attempt, LeaseToken: token, Scope: request.Scope,
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID, TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	}, engineID, func(ctx context.Context, tx *gorm.DB, id, tenant uint) error {
		var readErr error
		engine, readErr = h.engines.GetForCleanupConnection(ctx, tx, id, tenant)
		return readErr
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	if result == nil || result.Access == nil || engine == nil || engine.ID != uint(engineID) || engine.TenantID == nil || *engine.TenantID != uint(tenantID) || engine.LifecycleState != "deleting" {
		respondExecutionAuthorizationError(c, commonapi.ErrForbidden)
		return
	}
	access := result.Access
	c.JSON(http.StatusOK, client.CleanupEngineConnection{
		AuthorizationID: strconv.FormatInt(access.AuthorizationID, 10), ExecutionID: access.ExecutionID.String(), TenantID: strconv.FormatInt(access.TenantID, 10),
		Attempt: access.Attempt, Scope: access.Scope, Purpose: execution.CleanupEngineConnectionPurpose, EngineID: strconv.FormatInt(result.EngineID, 10), ExpiresAt: access.ExpiresAt.UTC(),
		Engine: &client.CleanupConnectionEngine{ID: engine.ID, TenantID: *engine.TenantID, EngineType: engine.EngineType, LifecycleState: engine.LifecycleState, Capabilities: engine.Capabilities, ConnectionInfo: engine.ConnectionInfo},
	})
}
