package api

import (
	"net/http"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

// TaskHandler 任务管理 API Handler
type TaskHandler struct {
	taskService *service.TaskService
}

// NewTaskHandler 创建 TaskHandler
func NewTaskHandler(taskService *service.TaskService) *TaskHandler {
	return &TaskHandler{
		taskService: taskService,
	}
}

// CreateTask 创建任务
// POST /api/tasks
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req models.CreateTaskRequest
	if !commonAPI.BindJSON(c, &req) {
		return
	}

	// 从上下文获取租户和用户信息（由 AuthMiddleware 注入）
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	task, err := h.taskService.CreateTask(c.Request.Context(), &req, tenantID, userID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusCreated, task)
}

// GetTask 获取任务详情
// GET /api/tasks/:id
func (h *TaskHandler) GetTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	task, err := h.taskService.GetTask(c.Request.Context(), id, tenantID)
	if err != nil {
		commonAPI.NotFoundError(c, "Task not found")
		return
	}

	c.JSON(http.StatusOK, task)
}

// ListTasks 获取任务列表
// GET /api/tasks?page=1&page_size=20&type=import&status=running
func (h *TaskHandler) ListTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 构建请求参数
	var req models.ListTasksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		// 使用默认值
		req.Page = 1
		req.PageSize = 20
	}

	// 确保分页参数有效
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 || req.PageSize > 100 {
		req.PageSize = 20
	}

	tasks, total, err := h.taskService.ListTasks(c.Request.Context(), tenantID, &req)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	commonAPI.SendPaginatedResponse(c, tasks, total, req.Page, req.PageSize)
}

// UpdateTask 更新任务
// PUT /api/tasks/:id
func (h *TaskHandler) UpdateTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	var req models.UpdateTaskRequest
	if !commonAPI.BindJSON(c, &req) {
		return
	}

	tenantID := c.GetUint("tenant_id")

	// 修正参数顺序：id, tenantID, req
	task, err := h.taskService.UpdateTask(c.Request.Context(), id, tenantID, &req)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, task)
}

// DeleteTask 删除任务
// DELETE /api/tasks/:id
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.taskService.DeleteTask(c.Request.Context(), id, tenantID); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task deleted successfully"})
}

// StartTask 启动任务
// POST /api/tasks/:id/start
func (h *TaskHandler) StartTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	execution, err := h.taskService.StartTask(c.Request.Context(), id, tenantID, userID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, execution)
}

// StopTask 停止任务
// POST /api/tasks/:id/stop
func (h *TaskHandler) StopTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.taskService.StopTask(c.Request.Context(), id, tenantID); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task stopped successfully"})
}

// PauseTask 暂停任务
// POST /api/tasks/:id/pause
func (h *TaskHandler) PauseTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.taskService.PauseTask(c.Request.Context(), id, tenantID); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task paused successfully"})
}

// ResumeTask 恢复任务
// POST /api/tasks/:id/resume
func (h *TaskHandler) ResumeTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.taskService.ResumeTask(c.Request.Context(), id, tenantID); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task resumed successfully"})
}

// GetTaskStatistics 获取任务统计
// GET /api/tasks/statistics
func (h *TaskHandler) GetTaskStatistics(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	stats, err := h.taskService.GetStatistics(c.Request.Context(), tenantID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, stats)
}

// CreateDataMapping 创建字段映射
// POST /api/tasks/:id/mappings
func (h *TaskHandler) CreateDataMapping(c *gin.Context) {
	taskID, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	var req models.CreateDataMappingRequest
	if !commonAPI.BindJSON(c, &req) {
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	mapping, err := h.taskService.CreateMapping(c.Request.Context(), taskID, &req, tenantID, userID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusCreated, mapping)
}

// GetTaskMappings 获取任务的字段映射列表
// GET /api/tasks/:id/mappings
func (h *TaskHandler) GetTaskMappings(c *gin.Context) {
	taskID, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	mappings, err := h.taskService.GetTaskMappings(c.Request.Context(), taskID, tenantID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, mappings)
}

// DeleteDataMapping 删除字段映射
// DELETE /api/mappings/:id
func (h *TaskHandler) DeleteDataMapping(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.taskService.DeleteMapping(c.Request.Context(), id, tenantID); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Mapping deleted successfully"})
}
