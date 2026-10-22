package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetScanPlanFacts godoc
// @Summary 查询冻结扫描计划事实 | Read frozen scan plan facts
// @Description 仅 addp-system Tenant Service 可读；200 仅表示计划事实，不是采集许可。不扫描、不读取源、不返回 Actor、连接、配置或 Grant。缺少真实冻结引擎版本时返回 409，不用当前版本补齐 | Restricted to addp-system Tenant Service. HTTP 200 contains facts, never an Allow. No scan, source I/O, Actor, connection, raw configuration or Grant. Returns 409 when the frozen engine version is missing; never substitutes the current version
// @Description 有任务的执行还须逐项匹配真实任务的引擎、深度、读取参数和范围；保留摘要却更换执行配置返回 409。任务已修改返回 needs_refresh，不签发许可 | Task-backed executions must also match the actual task engine, depth, read options and scope. Retaining the digest while changing execution configuration returns 409. A changed task returns needs_refresh, never a permit
// @Tags Meta Runtime
// @Produce json
// @Security BearerAuth
// @Param execution_id path string true "执行 UUID | Execution UUID"
// @Success 200 {object} execution.MetaScanPlanFacts "冻结计划的最小事实 | Minimal frozen plan facts"
// @Failure 400,401,403,404,409,500,503 {object} map[string]interface{} "查询失败 | Query failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.task_provider.read"]
// @Router /runtime/scan-executions/{execution_id}/authorization-facts [get]
func (h *Handler) GetScanPlanFacts(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid), "error_code": "meta_scan_plan_facts_invalid"})
		return
	}
	if h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, metai18n.MsgScanPlanFactsUnavailable), "error_code": "meta_scan_plan_facts_unavailable"})
		return
	}
	facts, err := h.executionService.GetScanPlanFacts(c.Request.Context(), commonAuth.GetTenantID(c), c.Param("execution_id"))
	if err != nil {
		status, message, code := http.StatusInternalServerError, metai18n.MsgScanPlanFactsReadFailed, "meta_scan_plan_facts_unavailable"
		switch {
		case errors.Is(err, commonapi.ErrBadRequest):
			status, message, code = http.StatusBadRequest, metai18n.MsgAuthorizationFactsInvalid, "meta_scan_plan_facts_invalid"
		case errors.Is(err, gorm.ErrRecordNotFound):
			status, message, code = http.StatusNotFound, metai18n.MsgScanExecutionNotFound, "meta_scan_execution_not_found"
		case errors.Is(err, service.ErrScanPlanFactsUnavailable):
			status, message = http.StatusConflict, metai18n.MsgScanPlanFactsUnavailable
		}
		c.JSON(status, gin.H{"error": commoni18n.T(c, message), "error_code": code})
		return
	}
	c.JSON(http.StatusOK, facts)
}
