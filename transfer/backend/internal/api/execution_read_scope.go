package api

import (
	"github.com/addp/common/execution"
	ownerauthorization "github.com/addp/transfer/internal/authorization"
	"github.com/gin-gonic/gin"
)

var executionReadPermissions = map[string]string{
	"sync":             ownerauthorization.PermissionTransferTaskRead,
	"cleanup_executor": "system.cleanup.read",
}

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
	execution.OwnerReadScopeHandler("transfer", executionReadPermissions)(c)
}
