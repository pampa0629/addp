package api

import (
	"errors"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	moni18n "github.com/addp/monitor/i18n"
	"github.com/gin-gonic/gin"
)

// ExecutionEventPage is the canonical event page exposed by this API.
type ExecutionEventPage = execution.EventPage

// GetExecutionEvents returns bounded process events after the Owner scope check.
// @Summary 获取执行过程事件 | Get execution process events
// @Description 复用执行读取范围，按 ID 游标读取最近 30 天安全事件；每页最多 100 条。| Uses execution read scope; ID cursor over safe events retained for 30 days, up to 100 per page.
// @Tags Monitor
// @Produce json
// @Param execution_id path string true "执行 UUID | Execution UUID"
// @Param after query int false "上一页末尾事件 ID | Last event ID from previous page" minimum(0)
// @Param limit query int false "每页事件数 | Page size" default(100) minimum(1) maximum(100)
// @Success 200 {object} ExecutionEventPage
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.execution.read"]
// @Router /executions/by-execution-id/{execution_id}/events [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetExecutionEvents(c *gin.Context) {
	tenant, ok := requireTenantID(c)
	if !ok {
		return
	}
	after, afterErr := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if afterErr != nil || limitErr != nil || after < 0 || limit < 1 || limit > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error_code": "invalid_params", "error": commoni18n.T(c, commoni18n.MsgInvalidParams)})
		return
	}
	page, err := h.queryService.GetExecutionEvents(c.Request.Context(), c.Param("execution_id"), tenant, after, limit)
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error_code": "execution_not_found", "error": commoni18n.T(c, moni18n.MsgExecutionNotFound)})
		} else {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error_code": "execution_events_unavailable", "error": commoni18n.T(c, moni18n.MsgDiagnosticQueryFailed)})
		}
		return
	}
	c.JSON(http.StatusOK, page)
}
