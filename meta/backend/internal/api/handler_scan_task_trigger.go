package api

import (
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
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

type taskProviderExecuteRequest struct {
	TriggerType       string                 `json:"trigger_type"`
	Source            string                 `json:"source"`
	ParentExecutionID string                 `json:"parent_execution_id"`
	Parameters        map[string]interface{} `json:"parameters"`
}

type taskProviderExecuteResponse struct {
	Status      string `json:"status"`
	ExecutionID string `json:"execution_id"`
}

// ProviderExecuteScanTask 按 TaskProvider 标准协议触发 ScanTask。
// @Summary 执行 TaskProvider 扫描任务 | Execute TaskProvider scan task
// @Description 按标准 TaskProvider 协议触发 Meta ScanTask；task_type 仅支持 scan，parameters 当前不支持覆盖。| Trigger a Meta ScanTask through the standard TaskProvider protocol; task_type only supports scan and parameters overrides are not supported.
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型，固定为 scan | Task type, fixed to scan"
// @Param id path int true "扫描任务ID | Scan task ID"
// @Param request body taskProviderExecuteRequest false "TaskProvider 执行请求 | TaskProvider execution request"
// @Success 202 {object} taskProviderExecuteResponse "执行记录 | Execution"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *Handler) ProviderExecuteScanTask(c *gin.Context) {
	taskType := c.Param("task_type")
	if taskType != commonExecution.TaskTypeScan {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}

	var req taskProviderExecuteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Parameters) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Meta scan task provider does not support execution parameter overrides"})
		return
	}
	if h.taskService == nil || h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scan task service not available"})
		return
	}
	triggerType, err := commonExecution.NormalizeTriggerType(req.TriggerType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = commonExecution.ModuleMeta
	}
	var parentExecutionID *string
	if strings.TrimSpace(req.ParentExecutionID) != "" {
		parentExecutionID = &req.ParentExecutionID
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)

	taskIDStr := c.Param("id")
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

	run, err := h.executionService.CreateTaskRunWithContext(c.Request.Context(), task, userID, triggerType, source, parentExecutionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, taskProviderExecuteResponse{
		Status:      run.Status,
		ExecutionID: run.ExecutionID,
	})
}
