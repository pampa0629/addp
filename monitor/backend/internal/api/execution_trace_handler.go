package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/monitor/internal/service"
	"github.com/addp/monitor/internal/tracequery"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GetExecutionTraceStages godoc
// @Summary 查看已授权执行的阶段追踪 | Read authorized execution stage observations
// @Description 第一方Tenant User，当前Owner逐次裁决；最多两个精确阶段锚点，不返回技术ID、完整Trace或日志。整个Owner读取与Trace查询共用5秒及原预算 | First-party Tenant User with a fresh Owner decision. At most two exact stage anchors, without technical IDs, full traces or logs. Owner and trace reads share five seconds and existing budgets.
// @Tags Monitor
// @Produce json
// @Security BearerAuth
// @Param execution_id path string true "规范执行UUID | Canonical execution UUID"
// @Success 200 {object} service.ExecutionTraceStagesResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.execution.read"]
// @Router /executions/by-execution-id/{execution_id}/trace-stages [get]
func (h *ExecutionHandler) GetExecutionTraceStages(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	tenant, principal, ok := executionReadIdentity(c)
	if !ok {
		return
	}
	id := c.Param("execution_id")
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id || c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		traceError(c, tracequery.ErrInvalid)
		return
	}
	if h.queryService == nil || h.traceService == nil {
		respondExecutionReadError(c, client.ErrExecutionOwnerUnavailable)
		return
	}
	var value service.ExecutionTraceStagesResponse
	value, err = h.traceService.ExecutionStages(c.Request.Context(), strconv.FormatInt(principal, 10), func(ctx context.Context) (*execution.TaskExecution, error) {
		return h.queryService.ReadExecutionForTrace(ctx, id, int(tenant), principal, c.GetHeader("Authorization"))
	})
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) || errors.Is(err, client.ErrExecutionOwnerUnavailable) {
			respondExecutionReadError(c, err)
		} else {
			traceError(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, value)
}
