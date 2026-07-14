package api

import (
	"errors"
	"net/http"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
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
// @Description 创建 bounded、业务 Kafka continuous 或 PostgreSQL CDC 任务。配置必须使用 runtime.boundary、load.mode、load.change_detection 和 target.policy.apply_mode；CDC 第一版固定为 PostgreSQL 单表 initial_snapshot 和 PostgreSQL 新目标表 upsert_delete。旧 mode/write_mode 字段会被拒绝。| Create a bounded, business Kafka continuous, or PostgreSQL CDC task using runtime.boundary, load.mode, load.change_detection, and target.policy.apply_mode. CDC v1 is limited to a single PostgreSQL table with initial_snapshot and a new PostgreSQL upsert_delete target. Legacy mode/write_mode fields are rejected.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param request body models.CreateTaskRequestDoc true "任务创建请求 | Task creation request"
// @Success 201 {object} models.TransferTask "任务创建成功 | Task created successfully"
// @Failure 400 {object} map[string]string "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]string "未授权 | Unauthorized"
// @Failure 500 {object} map[string]string "服务器内部错误 | Internal server error"
// @Router /task-definitions [post]
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
// @Router /task-definitions/{id} [get]
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
// @Param task_type query string false "任务类型，当前固定为 sync | Task type, currently fixed to sync"
// @Param status query string false "任务定义状态: idle, running, blocked | Task definition status: idle, running, blocked"
// @Param runtime_boundary query string false "执行边界: bounded, continuous | Runtime boundary: bounded, continuous"
// @Success 200 {object} models.ListProviderTasksResponse "获取成功 | Retrieved successfully"
// @Failure 400 {object} map[string]string "不支持的任务类型 | Unsupported task type"
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
	req.TaskType = strings.TrimSpace(req.TaskType)
	req.RuntimeBoundary = strings.TrimSpace(req.RuntimeBoundary)
	if req.TaskType != "" && req.TaskType != commonExecution.TaskTypeSync {
		commonAPI.BadRequestError(c, "unsupported task_type: "+req.TaskType)
		return
	}
	// 带 task_type 的调用遵循标准 TaskProvider 发现语义，只暴露 Orchestrator v1 可编排的 bounded task。
	if req.TaskType == commonExecution.TaskTypeSync {
		req.RuntimeBoundary = "bounded"
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
		respondTaskServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, models.ListProviderTasksResponse{
		Items:    tasks,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
}

// UpdateTask 更新任务
// @Summary 更新任务 | Update task
// @Description 更新任务的配置信息。source 指向已入库 Meta item 时使用 config.source.locator 的 item_id；target 使用 parent_locator + name；不支持在任务配置中直接传递 endpoint attributes。| Update task configuration. Use config.source.locator item_id for persisted Meta items; target uses parent_locator + name; endpoint attributes are not accepted in task config.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param request body models.UpdateTaskRequestDoc true "任务更新请求 | Task update request"
// @Success 200 {object} models.TransferTask "更新成功 | Updated successfully"
// @Failure 400 {object} map[string]string "参数错误 | Bad request"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /task-definitions/{id} [put]
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
// @Router /task-definitions/{id} [delete]
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
// @Description 启动任务执行并创建执行记录；PostgreSQL CDC 首次启动会先创建并确认 capture generation，普通中断恢复时复用同一 generation；schema drift blocked 任务不得再次启动。| Start task execution and create an execution record. PostgreSQL CDC provisions and verifies its capture generation before the first runtime session and reuses that generation for normal interruption recovery; schema-drift-blocked tasks cannot be started again.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} models.TaskExecution "启动成功，返回执行记录 | Started successfully, returns execution record"
// @Failure 400 {object} map[string]string "参数错误或任务已在运行 | Bad request or task already running"
// @Failure 409 {object} map[string]string "CDC 已永久停止或被结构变化阻塞 | CDC permanently stopped or blocked by schema change"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /task-definitions/{id}/start [post]
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

// ProviderExecuteRequest 是 TaskProvider 标准执行请求。
type ProviderExecuteRequest struct {
	TriggerType       string                 `json:"trigger_type"`
	Source            string                 `json:"source"`
	ParentExecutionID string                 `json:"parent_execution_id"`
	Parameters        map[string]interface{} `json:"parameters"`
}

type ProviderExecuteResponse struct {
	Status      string `json:"status"`
	ExecutionID string `json:"execution_id"`
}

// ProviderGetTask 获取标准 TaskProvider 任务详情。
// @Summary 获取 TaskProvider 任务详情 | Get TaskProvider task detail
// @Description 按标准 TaskProvider 路径获取 Transfer 任务详情；task_type 仅支持 sync。| Get Transfer task detail through the standard TaskProvider path; task_type only supports sync.
// @Tags         任务管理 | Task Management
// @Produce json
// @Param task_type path string true "任务类型，固定为 sync | Task type, fixed to sync"
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} models.TransferTask "任务详情 | Task detail"
// @Failure 400 {object} map[string]string "参数错误 | Bad request"
// @Failure 404 {object} map[string]string "任务不存在 | Task not found"
// @Router /tasks/{task_type}/{id} [get]
// @Security BearerAuth
func (h *TaskHandler) ProviderGetTask(c *gin.Context) {
	taskType := c.Param("task_type")
	if taskType != commonExecution.TaskTypeSync {
		commonAPI.BadRequestError(c, "unsupported task_type: "+taskType)
		return
	}
	h.GetTask(c)
}

// ProviderExecuteTask 使用 TaskProvider 标准协议启动 Transfer 任务。
// @Summary 执行 TaskProvider Transfer 任务 | Execute TaskProvider Transfer task
// @Description 按标准 TaskProvider 协议启动 Transfer 任务；task_type 仅支持 sync，parameters 当前不支持覆盖。| Start a Transfer task through the standard TaskProvider protocol; task_type only supports sync and parameters overrides are not supported.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型，固定为 sync | Task type, fixed to sync"
// @Param id path int true "任务ID | Task ID"
// @Param request body ProviderExecuteRequest false "TaskProvider 执行请求 | TaskProvider execution request"
// @Success 202 {object} ProviderExecuteResponse "执行记录 | Execution"
// @Failure 400 {object} map[string]string "参数错误 | Bad request"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *TaskHandler) ProviderExecuteTask(c *gin.Context) {
	taskType := c.Param("task_type")
	if taskType != commonExecution.TaskTypeSync {
		commonAPI.BadRequestError(c, "unsupported task_type: "+taskType)
		return
	}

	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	var req ProviderExecuteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	if len(req.Parameters) > 0 {
		commonAPI.BadRequestError(c, "Transfer task provider does not support execution parameter overrides")
		return
	}

	triggerType, err := commonExecution.NormalizeTriggerType(req.TriggerType)
	if err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = commonExecution.ModuleTransfer
	}
	var parentExecutionID *string
	if strings.TrimSpace(req.ParentExecutionID) != "" {
		parentExecutionID = &req.ParentExecutionID
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")
	task, err := h.taskService.GetTask(c.Request.Context(), id, tenantID)
	if err != nil {
		respondTaskServiceError(c, err)
		return
	}
	boundary, err := planner.TaskRuntimeBoundary(task.Config)
	if err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	if boundary == planner.RuntimeBoundaryContinuous {
		commonAPI.BadRequestError(c, "continuous Transfer tasks cannot be executed through TaskProvider")
		return
	}

	execution, err := h.taskService.StartTaskWithContext(c.Request.Context(), id, tenantID, userID, triggerType, source, parentExecutionID)
	if err != nil {
		respondTaskServiceError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, ProviderExecuteResponse{
		Status:      string(execution.Status),
		ExecutionID: execution.ExecutionID,
	})
}

// PauseTask 暂停任务
// @Summary 暂停任务 | Pause task
// @Description bounded 任务关闭自身定时调度；continuous 任务将 desired_state 置为 paused 并通知 runtime 取消当前 execution；schema drift blocked CDC 只能 Stop。| Disable a bounded task schedule, or set a continuous task desired_state to paused and request runtime cancellation. Schema-drift-blocked CDC can only be stopped.
// @Tags         任务管理 | Task Management
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]string
// @Failure 409 {object} map[string]string "CDC 已永久停止或被结构变化阻塞 | CDC permanently stopped or blocked by schema change"
// @Failure 503 {object} map[string]string "捕获控制面不可用 | Capture control unavailable"
// @Failure 500 {object} map[string]string
// @Router /task-definitions/{id}/pause [post]
// @Security BearerAuth
func (h *TaskHandler) PauseTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.taskService.PauseTask(c.Request.Context(), id, tenantID); err != nil {
		respondTaskServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18nmiddleware.T(c, "transfer.task.paused")})
}

// ResumeTask 恢复任务
// @Summary 恢复任务 | Resume task
// @Description bounded 任务恢复自身定时调度；continuous 任务创建新的 pending execution 并从 committed position 恢复；schema drift blocked CDC 不支持恢复，必须 Stop 后新建任务和目标表。| Re-enable a bounded task schedule, or create a new pending continuous execution that resumes from committed position. Schema-drift-blocked CDC cannot resume and must be stopped and recreated with a new target table.
// @Tags         任务管理 | Task Management
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]string
// @Failure 409 {object} map[string]string "CDC 已永久停止或被结构变化阻塞 | CDC permanently stopped or blocked by schema change"
// @Failure 503 {object} map[string]string "捕获控制面不可用 | Capture control unavailable"
// @Failure 500 {object} map[string]string
// @Router /task-definitions/{id}/resume [post]
// @Security BearerAuth
func (h *TaskHandler) ResumeTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	execution, err := h.taskService.ResumeTask(c.Request.Context(), id, tenantID, userID)
	if err != nil {
		respondTaskServiceError(c, err)
		return
	}
	response := gin.H{"message": i18nmiddleware.T(c, "transfer.task.resumed")}
	if execution != nil {
		response["execution"] = execution
	}
	c.JSON(http.StatusOK, response)
}

// StopTask 停止 continuous task。
// @Summary 停止连续任务 | Stop continuous task
// @Description 普通业务 Kafka continuous stop 保留 committed position；PostgreSQL CDC stop 是不可逆终态，必须提交 confirmed=true 且 confirmation_text 与任务名称完全一致，并删除 ADDP-owned connector、slot、publication 和 CDC topic。| Business Kafka continuous stop keeps committed positions. PostgreSQL CDC stop is irreversible and requires confirmed=true plus confirmation_text exactly matching the task name; ADDP-owned connector, slot, publication, and CDC topic are deleted.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param request body models.StopTaskRequest false "CDC 不可逆停止确认；普通 Kafka continuous 可省略 | Irreversible CDC stop confirmation; optional for business Kafka continuous"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string "CDC 已永久停止 | CDC permanently stopped"
// @Failure 503 {object} map[string]string "捕获控制面不可用 | Capture control unavailable"
// @Failure 500 {object} map[string]string
// @Router /task-definitions/{id}/stop [post]
// @Security BearerAuth
func (h *TaskHandler) StopTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}
	var req models.StopTaskRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	if err := h.taskService.StopTask(c.Request.Context(), id, c.GetUint("tenant_id"), req); err != nil {
		respondTaskServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": i18nmiddleware.T(c, "transfer.task.stopped")})
}

// GetTaskStatistics 获取任务统计
// @Summary 获取任务统计信息 | Get task statistics
// @Description 获取当前租户的任务统计数据（各状态任务数量等）| Get task statistics for the current tenant (task counts by status, etc.)
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Success 200 {object} models.TaskStatistics "统计信息 | Statistics"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @Router /task-definitions/statistics [get]
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

func respondTaskServiceError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrCDCStopConfirmationRequired) {
		commonAPI.BadRequestError(c, i18nmiddleware.T(c, "transfer.cdc.stop_confirmation_required"))
		return
	}
	if errors.Is(err, repository.ErrCaptureTerminal) {
		c.JSON(http.StatusConflict, gin.H{"error": i18nmiddleware.T(c, "transfer.cdc.terminal")})
		return
	}
	if errors.Is(err, service.ErrCDCSchemaChangeBlocked) {
		c.JSON(http.StatusConflict, gin.H{"error": i18nmiddleware.T(c, "transfer.cdc.schema_change_blocked")})
		return
	}
	if errors.Is(err, service.ErrCDCCaptureControlUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": i18nmiddleware.T(c, "transfer.cdc.capture_unavailable")})
		return
	}
	if errors.Is(err, service.ErrInvalidTaskConfig) {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	if errors.Is(err, service.ErrUnsupportedTaskType) {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	commonAPI.InternalServerError(c, err.Error())
}
