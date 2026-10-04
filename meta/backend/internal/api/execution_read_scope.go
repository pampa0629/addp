package api

import (
	"net/http"

	"github.com/addp/common/execution"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	ownerauthorization "github.com/addp/meta/internal/authorization"
	"github.com/gin-gonic/gin"
)

// GetExecutionReadScope returns the current User's Owner-defined execution scope.
// @Summary 获取任务执行读取范围 | Get execution read scope
// @Description 仅第一方租户用户；范围由 Owner 的现有读取 Permission 裁决，任务历史与本人一次性执行使用不同条件。| First-party tenant users only; Owner permissions decide task history and own ad-hoc visibility.
// @Tags Execution diagnostics
// @Produce json
// @Success 200 {object} execution.ReadScope
// @Failure 403 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /execution-read-scope [get]
// @Security BearerAuth
func GetExecutionReadScope(c *gin.Context) {
	execution.OwnerReadScopeHandler("meta", map[string]string{
		"scan":             ownerauthorization.PermissionMetaScanTaskRead,
		"cleanup_executor": "system.cleanup.read",
	})(c)
}

// installScanUserReadScope preserves the route's canonical authentication and
// Client Scope guards, including OAuth Users. Service callers use TaskProvider.
func installScanUserReadScope(c *gin.Context) bool {
	identity, ok := commonauth.AuthContextFromGin(c)
	tenantID, tenantOK := commonauth.TenantIDFromGin(c)
	principalID, principalOK := commonauth.PrincipalIDFromGin(c)
	if !ok || !tenantOK || !principalOK || identity.Principal.Type != "user" || !commonauth.HasRolePermission(c, ownerauthorization.PermissionMetaScanTaskRead) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error_code": "permission_denied", "error": commoni18n.T(c, commoni18n.MsgForbidden)})
		return false
	}
	scope := execution.ReadScope{Module: execution.ModuleMeta, TenantID: int(tenantID), PrincipalID: principalID,
		Grants: []execution.ReadGrant{{TaskType: execution.TaskTypeScan, TaskHistory: true, OwnAdHoc: true}}}
	c.Request = c.Request.WithContext(execution.WithReadScopes(c.Request.Context(), []execution.ReadScope{scope}))
	return true
}
