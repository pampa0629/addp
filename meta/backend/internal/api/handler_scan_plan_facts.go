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
	h.readScanFacts(c, false)
}

// GetScanTaskFacts godoc
// @Summary 查询当前采集任务事实 | Read current collection task facts
// @Description 仅 addp-system Tenant Service；只读当前任务功能配置，不创建执行、访问 System 或读取源。首期仅支持整个引擎的显式任务范围；其他 selector 返回 409，不扩大范围。引擎版本由 System 独立冻结；200 不表示采集已批准 | Restricted to addp-system Tenant Service. Reads current task configuration without creating an execution, calling System or opening a source. Initially supports explicit entire-engine tasks only; other selectors return 409 without scope expansion. System independently freezes the Engine version. HTTP 200 is not approval
// @Tags Meta Runtime
// @Produce json
// @Security BearerAuth
// @Param task_id path string true "任务 ID | Task ID"
// @Success 200 {object} execution.MetaScanTaskFacts "当前任务最小事实 | Minimal current task facts"
// @Failure 400,401,403,404,409,500,503 {object} map[string]interface{} "查询失败 | Query failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.task_provider.read"]
// @Router /runtime/scan-tasks/{task_id}/authorization-facts [get]
func (h *Handler) GetScanTaskFacts(c *gin.Context) {
	h.readScanFacts(c, true)
}

func (h *Handler) readScanFacts(c *gin.Context, task bool) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, metai18n.MsgAuthorizationFactsInvalid), "error_code": "meta_scan_plan_facts_invalid"})
		return
	}
	if h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, metai18n.MsgScanPlanFactsUnavailable), "error_code": "meta_scan_plan_facts_unavailable"})
		return
	}
	var facts any
	var err error
	if task {
		facts, err = h.executionService.GetScanTaskFacts(c.Request.Context(), commonAuth.GetTenantID(c), c.Param("task_id"))
	} else {
		facts, err = h.executionService.GetScanPlanFacts(c.Request.Context(), commonAuth.GetTenantID(c), c.Param("execution_id"))
	}
	if err != nil {
		status, message, code := http.StatusInternalServerError, metai18n.MsgScanPlanFactsReadFailed, "meta_scan_plan_facts_unavailable"
		switch {
		case errors.Is(err, commonapi.ErrBadRequest):
			status, message, code = http.StatusBadRequest, metai18n.MsgAuthorizationFactsInvalid, "meta_scan_plan_facts_invalid"
		case errors.Is(err, gorm.ErrRecordNotFound):
			status, message, code = http.StatusNotFound, metai18n.MsgScanExecutionNotFound, "meta_scan_execution_not_found"
			if task {
				message, code = metai18n.MsgScanTaskNotFound, "meta_scan_task_not_found"
			}
		case errors.Is(err, service.ErrScanPlanFactsUnavailable):
			status, message = http.StatusConflict, metai18n.MsgScanPlanFactsUnavailable
		}
		c.JSON(status, gin.H{"error": commoni18n.T(c, message), "error_code": code})
		return
	}
	c.JSON(http.StatusOK, facts)
}
