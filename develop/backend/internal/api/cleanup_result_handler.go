package api

import (
	"github.com/addp/common/cleanup"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type CleanupResultHandler struct{ read gin.HandlerFunc }

func NewCleanupResultHandler(db *gorm.DB) *CleanupResultHandler {
	return &CleanupResultHandler{read: cleanup.ReadResultHandler(db, "develop")}
}

// Get returns the current User's own Owner-local professional Cleanup result.
// @Summary 获取本模块资源回收专业结果 | Get module cleanup professional result
// @Description 仅第一方 unrestricted 租户用户；同时要求本模块专业结果权限、Cleanup公共读取权限及本人子执行归属。不返回SQL、凭据、对象路径或原始错误。 | First-party unrestricted tenant Users only; requires both professional and public Cleanup read permissions and ownership of the child execution. No SQL, credentials, object paths or raw errors.
// @Tags Cleanup
// @Produce json
// @Security BearerAuth
// @Param execution_id path string true "子执行UUID | Child execution UUID"
// @Success 200 {object} cleanup.Result
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.cleanup_result.read","system.cleanup.read"]
// @Router /cleanup-executions/{execution_id}/result [get]
func (h *CleanupResultHandler) Get(c *gin.Context) { h.read(c) }
