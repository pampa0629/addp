package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	commonAPI "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	i18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

// GetCaptureMetricHistory reads retained observations under the original task permission.
// @Summary 读取 CDC 任务指标历史 | Read CDC task metric history
// @Description 当前租户按任务及明确代次读取最近七天的安全指标。窗口按整分钟归一，每区间最后一次历史采样，最多1000点；缺样为空，不用于告警重算。| Read safe metrics for a task generation in the current tenant over the last seven days. Minute-aligned intervals contain their last retained sample, at most 1000 points; gaps are null and do not re-evaluate alerts.
// @Tags 任务管理 | Task Management
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Param generation query int true "捕获代次 | Capture generation" minimum(1)
// @Param start query string true "RFC3339 开始时间 | RFC3339 start time"
// @Param end query string true "RFC3339 结束时间 | RFC3339 end time"
// @Success 200 {object} models.CaptureMetricHistoryResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /task-definitions/{id}/cdc-metric-history [get]
// @Security BearerAuth
func (h *TaskHandler) GetCaptureMetricHistory(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}
	q := c.Request.URL.Query()
	bad := func() {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.T(c, i18n.MsgInvalidParams), "error_code": "invalid_params"})
	}
	if len(q) != 3 || len(q["generation"]) != 1 || len(q["start"]) != 1 || len(q["end"]) != 1 {
		bad()
		return
	}
	generation, err := strconv.ParseUint(q.Get("generation"), 10, 63)
	if err != nil || generation == 0 || strconv.FormatUint(generation, 10) != q.Get("generation") {
		bad()
		return
	}
	start, err := time.Parse(time.RFC3339, q.Get("start"))
	if err != nil {
		bad()
		return
	}
	end, err := time.Parse(time.RFC3339, q.Get("end"))
	if err != nil {
		bad()
		return
	}
	now := time.Now().UTC()
	window, err := service.NewCaptureMetricWindow(start, end, now)
	if err != nil {
		bad()
		return
	}
	var result *models.CaptureMetricHistoryResponse
	result, err = h.taskService.CaptureMetricHistory(c.Request.Context(), id, commonAuth.GetTenantID(c), generation, window, now)
	if errors.Is(err, commonAPI.ErrNotFound) {
		commonAPI.NotFoundError(c, i18n.T(c, "transfer.task.not_found"))
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": i18n.T(c, "transfer.cdc.capture_unavailable"), "error_code": "cdc_metric_history_unavailable"})
		return
	}
	c.JSON(http.StatusOK, result)
}
