package api

import (
	"net/http"
	"strconv"
	"strings"

	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
)

var _ = models.ScanTaskUpsertRequest{}

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

// ProviderGetScanTask 按 TaskProvider 标准路径获取扫描任务详情。
// @Summary 获取 TaskProvider 扫描任务详情 | Get TaskProvider scan task detail
// @Description 按标准 TaskProvider 路径获取 Meta ScanTask 详情；task_type 仅支持 scan。| Get Meta ScanTask detail through the standard TaskProvider path; task_type only supports scan.
// @Tags Meta Scan
// @Produce json
// @Param task_type path string true "任务类型，固定为 scan | Task type, fixed to scan"
// @Param id path int true "扫描任务ID | Scan task ID"
// @Success 200 {object} models.ProviderScanTask "扫描任务详情 | Scan task detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Router /tasks/{task_type}/{id} [get]
// @Security BearerAuth
func (h *Handler) ProviderGetScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}
	taskType := c.Param("task_type")
	if taskType != commonExecution.TaskTypeScan {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	taskIDStr := c.Param("id")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
		return
	}

	task, err := h.taskService.GetTask(tenantID, uint(taskID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	c.JSON(http.StatusOK, models.NewProviderScanTask(*task))
}

// ListScanTasks 列出扫描任务
// @Summary 列出扫描任务 | List scan tasks
// @Description 列出当前租户的扫描任务 | List scan tasks for current tenant
// @Tags Meta Scan
// @Produce json
// @Param task_type query string false "任务类型，固定为 scan | Task type, fixed to scan"
// @Success 200 {array} map[string]interface{} "扫描任务列表 | Scan tasks"
// @Failure 400 {object} map[string]interface{} "不支持的任务类型 | Unsupported task type"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks [get]
// @Security BearerAuth
func (h *Handler) ListScanTasks(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}
	taskType := strings.TrimSpace(c.Query("task_type"))
	if taskType != "" && taskType != commonExecution.TaskTypeScan {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
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

// ListProviderScanTasks 列出可供 TaskProvider 编排复用的扫描任务。
// @Summary 列出 TaskProvider 扫描任务 | List TaskProvider scan tasks
// @Description 返回可供 Orchestrator 编排复用的 Meta ScanTask，task_type 固定为 scan。| List Meta ScanTasks exposed through TaskProvider; task_type is fixed to scan.
// @Tags Meta Scan
// @Produce json
// @Param task_type query string false "任务类型，固定为 scan | Task type, fixed to scan"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(100)
// @Success 200 {object} models.ListProviderScanTasksResponse "扫描任务列表 | Scan tasks"
// @Failure 400 {object} map[string]interface{} "不支持的任务类型 | Unsupported task type"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /tasks [get]
// @Security BearerAuth
func (h *Handler) ListProviderScanTasks(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}
	taskType := strings.TrimSpace(c.Query("task_type"))
	if taskType != "" && taskType != commonExecution.TaskTypeScan {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))

	tasks, err := h.taskService.ListTasks(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.ListProviderScanTasksResponse{
		Items:    models.NewProviderScanTasks(tasks),
		Total:    int64(len(tasks)),
		Page:     page,
		PageSize: pageSize,
	})
}
