package api

import (
	"net/http"
	"strconv"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

// TriggerScanTask 手动触发任务
// @Summary 触发扫描任务 | Trigger scan task
// @Description 立即触发指定扫描任务 | Trigger scan task immediately
// @Tags Meta Scan
// @Produce json
// @Param task_id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "执行记录 | Execution"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks/{task_id}/trigger [post]
// @Security BearerAuth
func (h *Handler) TriggerScanTask(c *gin.Context) {
	if h.taskService == nil || h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scan task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)

	taskIDStr := c.Param("task_id")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
		return
	}

	task, err := h.taskService.GetTask(tenantID, uint(taskID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	run, err := h.executionService.CreateTaskManualRun(c.Request.Context(), task, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, run)
}
