package api

import (
	"github.com/addp/common/execution"
	systemauthorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

// GetExecutionReadScope returns the tenant cleanup history read scope.
// @Summary 获取清理执行读取范围 | Get cleanup execution read scope
// @Tags Execution diagnostics
// @Produce json
// @Success 200 {object} execution.ReadScope
// @Failure 403 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /execution-read-scope [get]
// @Security BearerAuth
func GetExecutionReadScope(c *gin.Context) {
	execution.OwnerReadScopeHandler("system", map[string]string{
		"cleanup": systemauthorization.PermissionSystemCleanupRead,
	})(c)
}

func registerExecutionReadScopeRoute(api *gin.RouterGroup, runtime *IAMRuntime) error {
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	api.GET("/execution-read-scope", runtime.Authentication, runtime.FirstPartyCredential, tenant, GetExecutionReadScope)
	return nil
}
