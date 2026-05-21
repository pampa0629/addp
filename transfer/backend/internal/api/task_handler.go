package api

import (
	"errors"
	"net/http"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/logger"
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
// @Summary 创建数据传输任务 | Create data transfer task
// @Description 创建一个新的数据导入/导出/同步任务。新任务 config 使用 source/target endpoint；source 指向已入库 Meta item 时可携带 source.attributes，Transfer planner 将消费其中的 item.layout、item.format、item.refs、type_info.table.fields 和 capabilities.spatial。| Create a new data import/export/sync task. New config uses source/target endpoints; source.attributes may carry Meta item attributes consumed by the planner.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param request body models.CreateTaskRequestDoc true "任务创建请求 | Task creation request"
// @Success 201 {object} models.TransferTask "任务创建成功 | Task created successfully"
// @Failure 400 {object} map[string]string "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]string "未授权 | Unauthorized"
// @Failure 500 {object} map[string]string "服务器内部错误 | Internal server error"
// @Router /tasks [post]
// @Security BearerAuth
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
		respondTaskServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, task)
}

// GetTask 获取任务详情
// @Summary 获取任务详情 | Get task detail
// @Description 根据任务ID获取任务的详细信息 | Get detailed task information by task ID
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} models.TransferTask "获取成功 | Retrieved successfully"
// @Failure 400 {object} map[string]string "参数错误 | Bad request"
// @Failure 404 {object} map[string]string "任务不存在 | Task not found"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /tasks/{id} [get]
// @Security BearerAuth
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
// @Summary 获取任务列表 | List tasks
// @Description 分页获取任务列表，支持按类型、状态过滤 | Get paginated task list with type and status filtering
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param page query int false "页码 | Page number" default(1)
// @Param page_size query int false "每页大小 | Page size" default(20)
// @Param type query string false "任务类型: import, export, sync | Task type: import, export, sync"
// @Param status query string false "任务状态: pending, running, completed, failed | Task status"
// @Success 200 {object} commonAPI.PaginatedResponse{data=[]models.TransferTask} "获取成功 | Retrieved successfully"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /tasks [get]
// @Security BearerAuth
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
// @Summary 更新任务 | Update task
// @Description 更新任务的配置信息。config.source.attributes 会按 JSONB 原样保存，用于 planner 消费已入库 Meta item 标准 attributes。| Update task configuration. config.source.attributes is preserved as JSONB for planner consumption.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param request body models.UpdateTaskRequestDoc true "任务更新请求 | Task update request"
// @Success 200 {object} models.TransferTask "更新成功 | Updated successfully"
// @Failure 400 {object} map[string]string "参数错误 | Bad request"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /tasks/{id} [put]
// @Security BearerAuth
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
		respondTaskServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, task)
}

// DeleteTask 删除任务
// @Summary 删除任务 | Delete task
// @Description 删除指定的任务及相关执行记录 | Delete a specific task and its execution records
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]string "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]string "参数错误 | Bad request"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /tasks/{id} [delete]
// @Security BearerAuth
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
// @Summary 启动任务 | Start task
// @Description 启动任务执行，创建新的执行记录 | Start task execution and create a new execution record
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} models.TaskExecution "启动成功，返回执行记录 | Started successfully, returns execution record"
// @Failure 400 {object} map[string]string "参数错误或任务已在运行 | Bad request or task already running"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /tasks/{id}/start [post]
// @Security BearerAuth
func (h *TaskHandler) StartTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	execution, err := h.taskService.StartTask(c.Request.Context(), id, tenantID, userID)
	if err != nil {
		respondTaskServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, execution)
}

// StopTask 停止任务
// @Summary 停止任务 | Stop task
// @Description 停止正在执行的任务 | Stop a running task
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]string "停止成功 | Stopped successfully"
// @Failure 400 {object} map[string]string "参数错误 | Bad request"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /tasks/{id}/stop [post]
// @Security BearerAuth
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
// @Summary 暂停任务 | Pause task
// @Tags         任务管理 | Task Management
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tasks/{id}/pause [post]
// @Security BearerAuth
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
// @Summary 恢复任务 | Resume task
// @Tags         任务管理 | Task Management
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tasks/{id}/resume [post]
// @Security BearerAuth
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
// @Summary 获取任务统计信息 | Get task statistics
// @Description 获取当前租户的任务统计数据（各状态任务数量等）| Get task statistics for the current tenant (task counts by status, etc.)
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Success 200 {object} models.TaskStatistics "统计信息 | Statistics"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /tasks/statistics [get]
// @Security BearerAuth
func (h *TaskHandler) GetTaskStatistics(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 添加调试日志
	logger.L().Info("getting task statistics", "tenant_id", tenantID)

	stats, err := h.taskService.GetStatistics(c.Request.Context(), tenantID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	// 记录统计结果
	logger.L().Info("task statistics retrieved", "tenant_id", tenantID, "total_tasks", stats.TotalTasks)

	c.JSON(http.StatusOK, stats)
}

// CreateFieldMapping 创建字段映射
// @Summary 创建字段映射 | Create field mapping
// @Tags         字段映射 | Field Mapping
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param request body models.CreateFieldMappingRequest true "字段映射请求 | Field mapping request"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tasks/{id}/mappings [post]
// @Security BearerAuth
func (h *TaskHandler) CreateFieldMapping(c *gin.Context) {
	taskID, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	var req models.CreateFieldMappingRequest
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
// @Summary 获取任务字段映射 | Get task mappings
// @Tags         字段映射 | Field Mapping
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {array} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /tasks/{id}/mappings [get]
// @Security BearerAuth
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

// DeleteFieldMapping 删除字段映射
// @Summary 删除字段映射 | Delete field mapping
// @Tags         字段映射 | Field Mapping
// @Produce json
// @Param id path int true "字段映射ID | Mapping ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /mappings/{id} [delete]
// @Security BearerAuth
func (h *TaskHandler) DeleteFieldMapping(c *gin.Context) {
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

func respondTaskServiceError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrInvalidTaskConfig) {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.InternalServerError(c, err.Error())
}
