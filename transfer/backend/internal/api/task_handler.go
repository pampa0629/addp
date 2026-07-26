package api

import (
	"errors"
	"net/http"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	transferI18n "github.com/addp/transfer/i18n"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
// @Description 创建 bounded、业务 Kafka continuous 或数据库 CDC 任务。业务 Kafka continuous 必须显式使用 runtime.record_failure.mode=block|dead_letter；dead_letter 只处理确定性记录级数据错误。CDC 第一版支持 PostgreSQL/MySQL 单表 initial_snapshot、block 和 PostgreSQL 新目标表 upsert_delete。旧 mode/write_mode 字段会被拒绝。| Create a bounded, business Kafka continuous, or database CDC task. Business Kafka continuous tasks must explicitly use runtime.record_failure.mode=block|dead_letter; dead_letter only handles deterministic record-level data errors. CDC v1 supports a single PostgreSQL or MySQL table with initial_snapshot, block policy, and a new PostgreSQL upsert_delete target. Legacy mode/write_mode fields are rejected.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param request body models.CreateTaskRequestDoc true "任务创建请求 | Task creation request"
// @Success 201 {object} models.TransferTask "任务创建成功 | Task created successfully"
// @Failure 400 {object} map[string]string "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]string "未授权 | Unauthorized"
// @Failure 500 {object} map[string]string "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.create"]
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.update"]
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
// @Description 删除指定任务。运行中的任务必须先停止；删除前会清理 task-owned capture/DLQ 资源，但保留统一 execution 历史和目标业务数据。| Delete a task. Running tasks must be stopped first; task-owned capture/DLQ resources are cleaned before deletion, while unified execution history and target business data are retained.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]string "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]string "参数错误 | Bad request"
// @Failure 409 {object} map[string]string "任务仍在运行 | Task is still running"
// @Failure 503 {object} map[string]string "任务资源清理不可用或失败 | Task resource cleanup unavailable or failed"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.delete"]
// @Router /task-definitions/{id} [delete]
// @Security BearerAuth
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.taskService.DeleteTask(c.Request.Context(), id, tenantID); err != nil {
		respondTaskServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task deleted successfully"})
}

// StartTask 启动任务
// @Summary 启动任务 | Start task
// @Description 启动任务执行并创建执行记录；数据库 CDC 首次启动会先创建并确认 capture generation，普通中断恢复时复用同一 generation；schema drift blocked 任务必须先完成可用的 additive 审批，否则只能 Stop。| Start task execution and create an execution record. Database CDC provisions and verifies its capture generation before the first runtime session and reuses that generation for normal interruption recovery. A schema-drift-blocked task must first complete an eligible additive approval or be stopped.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} models.TaskExecution "启动成功，返回执行记录 | Started successfully, returns execution record"
// @Failure 400 {object} map[string]string "参数错误或任务已在运行 | Bad request or task already running"
// @Failure 409 {object} map[string]string "CDC 已永久停止或被结构变化阻塞 | CDC permanently stopped or blocked by schema change"
// @Failure 500 {object} map[string]string "服务器错误 | Server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.execute"]
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

// ReplayTask 创建业务 Kafka bounded replay execution。
// @Summary 创建 Kafka bounded replay | Create Kafka bounded replay
// @Description 从 owner task 的原业务 Kafka topic 按显式半开 partition/offset 范围读取，创建独立 bounded execution，并只写入不存在的新 PostgreSQL 隔离表。请求不能覆盖 source、mapping、key、policy 或原目标；replay 不修改主任务状态、主水位或主目标。| Read explicit half-open partition/offset ranges from the owner task's original business Kafka topic, create an independent bounded execution, and write only to a new non-existing isolated PostgreSQL table. The request cannot override source, mapping, key, policy, or the owner target; replay does not change the owner task state, committed position, or target.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param request body models.ReplayTaskRequest true "回放范围与新目标 | Replay ranges and new target"
// @Success 202 {object} models.TaskExecution "已创建独立回放执行 | Independent replay execution created"
// @Failure 400 {object} map[string]string "请求不符合 bounded replay v1 契约 | Request violates the bounded replay v1 contract"
// @Failure 404 {object} map[string]string "任务不存在 | Task not found"
// @Failure 409 {object} map[string]string "范围超出保留边界或目标已存在 | Range is outside retention or target already exists"
// @Failure 503 {object} map[string]string "回放运行时不可用 | Replay runtime unavailable"
// @Failure 500 {object} map[string]string "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.execute"]
// @Router /task-definitions/{id}/replay [post]
// @Security BearerAuth
func (h *TaskHandler) ReplayTask(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}
	var req models.ReplayTaskRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		commonAPI.BadRequestError(c, i18nmiddleware.T(c, transferI18n.MsgReplayInvalid))
		return
	}
	execution, err := h.taskService.ReplayTask(c.Request.Context(), id, c.GetUint("tenant_id"), c.GetUint("user_id"), req)
	if err != nil {
		respondReplayTaskError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, execution)
}

// GetSchemaChange 获取当前数据库 CDC schema change request。
// @Summary 获取 CDC 结构变更请求 | Get CDC schema change request
// @Description 只读返回当前 task/generation 最新的 schema change request。只有当前阻塞消息实际包含的新增 nullable 非 geometry 字段可审批；Meta scan claim 状态只观测、不在 GET 中触发。| Read the latest schema change request for the current task/generation without side effects. Only nullable non-geometry fields present in the blocked record are approvable. Meta scan claim state is observed but never triggered by GET.
// @Tags         任务管理 | Task Management
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} models.SchemaChangeRequestView
// @Failure 404 {object} map[string]string "任务或结构变更请求不存在 | Task or schema change request not found"
// @Failure 500 {object} map[string]string "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /task-definitions/{id}/schema-change [get]
// @Security BearerAuth
func (h *TaskHandler) GetSchemaChange(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}
	request, err := h.taskService.GetSchemaChange(c.Request.Context(), id, c.GetUint("tenant_id"))
	if err != nil {
		respondSchemaChangeError(c, err)
		return
	}
	c.JSON(http.StatusOK, request)
}

// ApproveSchemaChange 审批数据库 CDC additive schema migration。
// @Summary 审批 CDC additive 结构变更 | Approve CDC additive schema change
// @Description 显式确认全部新增源字段的目标映射，幂等新增 PostgreSQL nullable 列并提交新的 mapping revision。成功后任务进入 paused，必须使用既有 Resume API 从原 committed offset 恢复；响应同时返回 Meta scan 的 pending/running/success/failed 状态和 attempt。| Explicitly approve target mappings for every added source field, idempotently add nullable PostgreSQL columns, and commit a new mapping revision. The task becomes paused and must be resumed through the existing Resume API from the original committed offset. The response also includes the pending/running/success/failed Meta scan status and attempt.
// @Tags         任务管理 | Task Management
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param request body models.ApproveSchemaChangeRequest true "新增字段映射 | Added field mappings"
// @Success 200 {object} models.SchemaChangeRequestView
// @Failure 400 {object} map[string]string "请求格式无效 | Invalid request"
// @Failure 404 {object} map[string]string "任务或结构变更请求不存在 | Task or schema change request not found"
// @Failure 409 {object} map[string]string "变化不可 additive 或审批与当前事实冲突 | Change is not additive or approval conflicts with current facts"
// @Failure 503 {object} map[string]string "结构变更控制面不可用 | Schema change control unavailable"
// @Failure 500 {object} map[string]string "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.update"]
// @Router /task-definitions/{id}/schema-change/approve [post]
// @Security BearerAuth
func (h *TaskHandler) ApproveSchemaChange(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}
	var request models.ApproveSchemaChangeRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil || len(request.Fields) == 0 {
		commonAPI.BadRequestError(c, i18nmiddleware.T(c, transferI18n.MsgSchemaChangeInvalid))
		return
	}
	result, err := h.taskService.ApproveSchemaChange(c.Request.Context(), id, c.GetUint("tenant_id"), c.GetUint("user_id"), request)
	if err != nil {
		respondSchemaChangeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListDeadLetters 获取 owner task 的 DLQ 控制索引。
// @Summary 获取任务死信记录 | List task dead-letter records
// @Description 分页查询当前租户 owner task 的安全 DLQ 控制索引。只返回源位置、稳定错误、execution 与观测审计事实，不返回 Infra Kafka payload reference 或原始 key/value/headers。| List safe DLQ control-index records for the current tenant's owner task. Only source position, stable error, execution, and observation audit facts are returned; Infra Kafka payload references and raw key/value/headers are never exposed.
// @Tags         死信管理 | Dead-letter Management
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param page query int false "页码 | Page number" default(1)
// @Param page_size query int false "每页大小，最大100 | Page size, max 100" default(20)
// @Param source_partition query string false "源分区精确过滤 | Exact source partition filter"
// @Param error_category query string false "错误分类精确过滤 | Exact error category filter"
// @Param error_code query string false "错误码精确过滤 | Exact error code filter"
// @Param payload_available query bool false "payload 可用状态精确过滤 | Exact payload availability filter"
// @Success 200 {object} models.DeadLetterListResponse "死信记录分页结果 | Paginated dead-letter records"
// @Failure 400 {object} map[string]string "查询参数无效 | Invalid query parameters"
// @Failure 404 {object} map[string]string "任务不存在 | Task not found"
// @Failure 500 {object} map[string]string "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /task-definitions/{id}/dead-letters [get]
// @Security BearerAuth
func (h *TaskHandler) ListDeadLetters(c *gin.Context) {
	taskID, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}
	var request models.DeadLetterListRequest
	if err := c.ShouldBindQuery(&request); err != nil {
		commonAPI.BadRequestError(c, i18nmiddleware.T(c, transferI18n.MsgDeadLetterInvalidQuery))
		return
	}
	if _, exists := c.GetQuery("page"); !exists {
		request.Page = 1
	}
	if _, exists := c.GetQuery("page_size"); !exists {
		request.PageSize = 20
	}
	if request.Page < 1 || request.PageSize < 1 || request.PageSize > 100 {
		commonAPI.BadRequestError(c, i18nmiddleware.T(c, transferI18n.MsgDeadLetterInvalidQuery))
		return
	}
	request.SourcePartition = strings.TrimSpace(request.SourcePartition)
	request.ErrorCategory = strings.TrimSpace(request.ErrorCategory)
	request.ErrorCode = strings.TrimSpace(request.ErrorCode)

	deadLetters, total, err := h.taskService.ListDeadLetters(c.Request.Context(), taskID, c.GetUint("tenant_id"), request)
	if err != nil {
		respondDeadLetterError(c, err)
		return
	}
	commonAPI.RespondPaginated(c, deadLetters, total, request.Page, request.PageSize)
}

// GetDeadLetter 获取 owner task 下单条 DLQ 控制索引。
// @Summary 获取死信记录详情 | Get dead-letter record detail
// @Description 按 identity 获取当前租户 owner task 下的安全 DLQ 控制索引详情。DLQ 不是 replay source，本接口不读取原始 payload。| Get a safe DLQ control-index detail by identity under the current tenant's owner task. DLQ is not a replay source and this endpoint does not read raw payload.
// @Tags         死信管理 | Dead-letter Management
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param identity path string true "死信记录 UUID | Dead-letter record UUID"
// @Success 200 {object} models.DeadLetterView "死信记录详情 | Dead-letter record detail"
// @Failure 400 {object} map[string]string "标识无效 | Invalid identity"
// @Failure 404 {object} map[string]string "任务或死信记录不存在 | Task or dead-letter record not found"
// @Failure 500 {object} map[string]string "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /task-definitions/{id}/dead-letters/{identity} [get]
// @Security BearerAuth
func (h *TaskHandler) GetDeadLetter(c *gin.Context) {
	taskID, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}
	identity := strings.TrimSpace(c.Param("identity"))
	if _, err := uuid.Parse(identity); err != nil {
		commonAPI.BadRequestError(c, i18nmiddleware.T(c, transferI18n.MsgDeadLetterInvalidID))
		return
	}
	deadLetter, err := h.taskService.GetDeadLetter(c.Request.Context(), taskID, c.GetUint("tenant_id"), identity)
	if err != nil {
		respondDeadLetterError(c, err)
		return
	}
	c.JSON(http.StatusOK, deadLetter)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.execute"]
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
// @Description bounded 任务关闭自身定时调度；continuous 任务将 desired_state 置为 paused 并通知 runtime 取消当前 execution；schema drift blocked CDC 需先检查专用结构变更请求。| Disable a bounded task schedule, or set a continuous task desired_state to paused and request runtime cancellation. A schema-drift-blocked CDC task must use the dedicated schema change request first.
// @Tags         任务管理 | Task Management
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]string
// @Failure 409 {object} map[string]string "CDC 已永久停止或被结构变化阻塞 | CDC permanently stopped or blocked by schema change"
// @Failure 503 {object} map[string]string "捕获控制面不可用 | Capture control unavailable"
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.update"]
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
// @Description bounded 任务恢复自身定时调度；continuous 任务创建新的 pending execution 并从 committed position 恢复；schema drift blocked CDC 必须先完成可用的 additive 审批，其他变化只能 Stop 后新建任务和目标表。| Re-enable a bounded task schedule, or create a new pending continuous execution that resumes from committed position. A schema-drift-blocked CDC task must first complete an eligible additive approval; other changes require stopping and recreating the task with a new target table.
// @Tags         任务管理 | Task Management
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]string
// @Failure 409 {object} map[string]string "CDC 已永久停止或被结构变化阻塞 | CDC permanently stopped or blocked by schema change"
// @Failure 503 {object} map[string]string "捕获控制面不可用 | Capture control unavailable"
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.update"]
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
// @Description 普通业务 Kafka continuous stop 保留 committed position；数据库 CDC stop 是不可逆终态，必须提交 confirmed=true 且 confirmation_text 与任务名称完全一致，并删除 ADDP-owned connector、provider 专属捕获资源、内部 topic、group 和 ACL。| Business Kafka continuous stop keeps committed positions. Database CDC stop is irreversible and requires confirmed=true plus confirmation_text exactly matching the task name; ADDP-owned connector, provider-specific capture resources, internal topics, group, and ACLs are deleted.
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.update"]
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
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
	if errors.Is(err, commonAPI.ErrNotFound) {
		commonAPI.NotFoundError(c, i18nmiddleware.T(c, transferI18n.MsgTaskNotFound))
		return
	}
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
	if errors.Is(err, service.ErrTaskDeleteRequiresStopped) {
		c.JSON(http.StatusConflict, gin.H{"error": i18nmiddleware.T(c, transferI18n.MsgTaskDeleteRequiresStopped)})
		return
	}
	if errors.Is(err, service.ErrTaskDeleteCleanupFailed) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": i18nmiddleware.T(c, transferI18n.MsgTaskDeleteCleanupFailed)})
		return
	}
	commonAPI.InternalServerError(c, err.Error())
}

func respondReplayTaskError(c *gin.Context, err error) {
	if errors.Is(err, commonAPI.ErrNotFound) {
		commonAPI.NotFoundError(c, i18nmiddleware.T(c, transferI18n.MsgTaskNotFound))
		return
	}
	if errors.Is(err, service.ErrInvalidReplayRequest) {
		commonAPI.BadRequestError(c, i18nmiddleware.T(c, transferI18n.MsgReplayInvalid))
		return
	}
	if errors.Is(err, service.ErrReplayRangeUnavailable) {
		c.JSON(http.StatusConflict, gin.H{"error": i18nmiddleware.T(c, transferI18n.MsgReplayRangeUnavailable)})
		return
	}
	if errors.Is(err, service.ErrReplayTargetExists) {
		c.JSON(http.StatusConflict, gin.H{"error": i18nmiddleware.T(c, transferI18n.MsgReplayTargetExists)})
		return
	}
	if errors.Is(err, service.ErrReplayRuntimeUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": i18nmiddleware.T(c, transferI18n.MsgReplayUnavailable)})
		return
	}
	commonAPI.InternalServerError(c, i18nmiddleware.T(c, transferI18n.MsgReplayInternalError))
}

func respondDeadLetterError(c *gin.Context, err error) {
	if errors.Is(err, commonAPI.ErrNotFound) {
		commonAPI.NotFoundError(c, i18nmiddleware.T(c, transferI18n.MsgTaskNotFound))
		return
	}
	if errors.Is(err, service.ErrDeadLetterNotFound) {
		commonAPI.NotFoundError(c, i18nmiddleware.T(c, transferI18n.MsgDeadLetterNotFound))
		return
	}
	commonAPI.InternalServerError(c, i18nmiddleware.T(c, transferI18n.MsgDeadLetterInternal))
}

func respondSchemaChangeError(c *gin.Context, err error) {
	if errors.Is(err, commonAPI.ErrNotFound) {
		commonAPI.NotFoundError(c, i18nmiddleware.T(c, transferI18n.MsgTaskNotFound))
		return
	}
	if errors.Is(err, service.ErrSchemaChangeNotFound) || errors.Is(err, repository.ErrSchemaChangeRequestNotFound) {
		commonAPI.NotFoundError(c, i18nmiddleware.T(c, transferI18n.MsgSchemaChangeNotFound))
		return
	}
	if errors.Is(err, service.ErrSchemaChangeNotAdditive) {
		c.JSON(http.StatusConflict, gin.H{"error": i18nmiddleware.T(c, transferI18n.MsgSchemaChangeNotAdditive)})
		return
	}
	if errors.Is(err, service.ErrSchemaChangeApprovalConflict) || errors.Is(err, repository.ErrSchemaChangeRequestConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": i18nmiddleware.T(c, transferI18n.MsgSchemaChangeConflict)})
		return
	}
	if errors.Is(err, service.ErrSchemaChangeControlUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": i18nmiddleware.T(c, transferI18n.MsgSchemaChangeUnavailable)})
		return
	}
	commonAPI.InternalServerError(c, i18nmiddleware.T(c, transferI18n.MsgSchemaChangeInternal))
}
