package api

import (
	"github.com/addp/common/execution"
	"github.com/gin-gonic/gin"
)

// GetExecutionReadScope returns the Owner's tenant cleanup history scope.
// @Summary 获取执行读取范围 | Get execution read scope
// @Tags Execution diagnostics
// @Produce json
// @Success 200 {object} execution.ReadScope
// @Failure 403 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /execution-read-scope [get]
// @Security BearerAuth
func GetExecutionReadScope(c *gin.Context) {
	execution.OwnerReadScopeHandler("asset", map[string]string{"cleanup_executor": "system.cleanup.read"})(c)
}
