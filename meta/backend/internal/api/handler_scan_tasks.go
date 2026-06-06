package api

import (
	"net/http"
	"strconv"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
)

// CreateScanTask 创建扫描任务
// @Summary 创建扫描任务 | Create scan task
// @Description 创建一个定时或手动扫描任务 | Create a scheduled or manual scan task
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param request body models.ScanTaskUpsertRequest true "扫描任务请求 | Scan task request"
// @Success 201 {object} map[string]interface{} "扫描任务 | Scan task"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks [post]
// @Security BearerAuth
func (h *Handler) CreateScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)

	var req models.ScanTaskUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.taskService.CreateTask(c.Request.Context(), tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, task)
}

// UpdateScanTask 更新任务
// @Summary 更新扫描任务 | Update scan task
// @Description 更新扫描任务配置 | Update scan task configuration
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param task_id path int true "任务ID | Task ID"
// @Param request body models.ScanTaskUpsertRequest true "扫描任务请求 | Scan task request"
// @Success 200 {object} map[string]interface{} "扫描任务 | Scan task"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks/{task_id} [put]
// @Security BearerAuth
func (h *Handler) UpdateScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
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

	var req models.ScanTaskUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.taskService.UpdateTask(c.Request.Context(), tenantID, uint(taskID), userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, task)
}

// DeleteScanTask 删除任务
// @Summary 删除扫描任务 | Delete scan task
// @Description 删除指定扫描任务 | Delete scan task by ID
// @Tags Meta Scan
// @Produce json
// @Param task_id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除结果 | Delete result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks/{task_id} [delete]
// @Security BearerAuth
func (h *Handler) DeleteScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)

	taskIDStr := c.Param("task_id")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
		return
	}

	if err := h.taskService.DeleteTask(c.Request.Context(), tenantID, uint(taskID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "task deleted"})
}

// UpsertEngineScanTask 维护指定 engine 绑定的扫描计划
// @Summary 维护 engine 扫描计划 | Upsert engine scan task
// @Description 为指定 engine 创建、更新或关闭绑定的 Meta 扫描计划 | Create, update or disable the Meta scan task bound to an engine
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param engine_id path int true "引擎ID | Engine ID"
// @Param request body models.EngineScanTaskPolicyRequest true "engine 扫描计划 | Engine scan policy"
// @Success 200 {object} models.ScanTask "扫描任务 | Scan task"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks/engines/{engine_id} [put]
// @Security BearerAuth
func (h *Handler) UpsertEngineScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)

	engineID64, err := strconv.ParseUint(c.Param("engine_id"), 10, 32)
	if err != nil || engineID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	var req models.EngineScanTaskPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.taskService.UpsertEngineScanTaskFromPolicy(tenantID, userID, uint(engineID64), req.EngineName, req.ScanPolicy.ToCommonScanConfig())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(http.StatusOK, gin.H{"status": "deleted"})
		return
	}

	c.JSON(http.StatusOK, task)
}

// DeleteEngineScanTask 删除指定 engine 绑定的扫描计划
// @Summary 删除 engine 扫描计划 | Delete engine scan task
// @Description 删除指定 engine 绑定的 Meta 扫描计划 | Delete the Meta scan task bound to an engine
// @Tags Meta Scan
// @Produce json
// @Param engine_id path int true "引擎ID | Engine ID"
// @Success 200 {object} map[string]interface{} "删除结果 | Delete result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks/engines/{engine_id} [delete]
// @Security BearerAuth
func (h *Handler) DeleteEngineScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	engineID64, err := strconv.ParseUint(c.Param("engine_id"), 10, 32)
	if err != nil || engineID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	if err := h.taskService.DeleteEngineTaskBinding(tenantID, uint(engineID64)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "task deleted"})
}

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

// ListScanTasks 列出扫描任务
// @Summary 列出扫描任务 | List scan tasks
// @Description 列出当前租户的扫描任务 | List scan tasks for current tenant
// @Tags Meta Scan
// @Produce json
// @Success 200 {array} map[string]interface{} "扫描任务列表 | Scan tasks"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks [get]
// @Security BearerAuth
func (h *Handler) ListScanTasks(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)

	tasks, err := h.taskService.ListTasks(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tasks)
}
