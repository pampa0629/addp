package api

import (
	"errors"
	commonExecution "github.com/addp/common/execution"
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// TaskProviderHandler 标准 TaskProvider API 处理器
// 实现: GET /api/manager/tasks, POST /api/manager/tasks/:task_type/:id/execute
//
//	GET /api/manager/tasks/:task_type/:id, GET /api/manager/executions/:execution_id
type TaskProviderHandler struct {
	embeddingTaskSvc *service.EmbeddingTaskService
	mvtTaskSvc       *service.MvtTaskService
	taskExecRepo     *commonExecution.TaskExecutionRepository
}

// NewTaskProviderHandler 创建处理器
func NewTaskProviderHandler(
	embeddingTaskSvc *service.EmbeddingTaskService,
	mvtTaskSvc *service.MvtTaskService,
	taskExecRepo *commonExecution.TaskExecutionRepository,
) *TaskProviderHandler {
	return &TaskProviderHandler{
		embeddingTaskSvc: embeddingTaskSvc,
		mvtTaskSvc:       mvtTaskSvc,
		taskExecRepo:     taskExecRepo,
	}
}

// TaskListResponse 任务列表响应（统一包装 mvt_generation 和 embedding 任务）
type TaskListItem struct {
	ID                  uint    `json:"id"`
	TenantID            uint    `json:"tenant_id"`
	TaskType            string  `json:"task_type"`
	Name                string  `json:"name"`
	Description         string  `json:"description,omitempty"`
	Enabled             bool    `json:"enabled"`
	LastExecutionID     *string `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string `json:"last_execution_status,omitempty"`
}

// ListTasks GET /api/manager/tasks
// 查询参数：?task_type=mvt_generation|embedding
// @Summary 列出任务 | List tasks
// @Description 列出Manager模块的任务（MVT生成任务和向量化任务）| List Manager module tasks (MVT generation and embedding tasks)
// @Tags Manager
// @Produce json
// @Param task_type query string false "任务类型过滤：mvt_generation|embedding | Task type filter: mvt_generation|embedding"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 400 {object} map[string]interface{} "不支持的任务类型 | Unsupported task type"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /tasks [get]
// @Router /mvt_tasks [get]
// @Router /embedding_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	taskType := strings.TrimSpace(c.Query("task_type"))

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	ctx := c.Request.Context()
	var items []TaskListItem
	var total int64

	switch taskType {
	case commonExecution.TaskTypeMvtGeneration:
		tasks, t, err := h.mvtTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeMvtGeneration,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case commonExecution.TaskTypeEmbedding:
		tasks, t, err := h.embeddingTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeEmbedding,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case "":
		// 返回所有类型
		mvtTasks, mvtTotal, err := h.mvtTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		embTasks, embTotal, err := h.embeddingTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		total = mvtTotal + embTotal
		for _, task := range mvtTasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeMvtGeneration,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
		for _, task := range embTasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeEmbedding,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "不支持的任务类型: " + taskType})
		return
	}

	if items == nil {
		items = []TaskListItem{}
	}
	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"data":      items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// TaskDetail GET /api/manager/tasks/:task_type/:id
// @Summary 获取任务详情 | Get task detail
// @Description 获取指定类型和ID的任务详细信息 | Get detailed information of a task by type and ID
// @Tags Manager
// @Produce json
// @Param task_type path string true "任务类型：mvt_generation|embedding | Task type: mvt_generation|embedding"
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "任务详情 | Task detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /tasks/{task_type}/{id} [get]
// @Router /mvt_tasks/{id} [get]
// @Router /embedding_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) TaskDetail(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	taskType := c.Param("task_type")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "无效的任务ID"})
		return
	}

	ctx := c.Request.Context()
	switch taskType {
	case "mvt_generation":
		task, err := h.mvtTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "任务不存在"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": task})
	case "embedding":
		task, err := h.embeddingTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "任务不存在"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": task})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "不支持的任务类型: " + taskType})
	}
}

// TaskExecuteRequest 触发执行请求
type TaskExecuteRequest struct {
	TriggerType       string                 `json:"trigger_type"`        // manual|scheduled，默认 manual
	Source            string                 `json:"source"`              // 触发来源模块
	ParentExecutionID string                 `json:"parent_execution_id"` // 父执行ID（Orchestrator 调用时传入）
	Parameters        map[string]interface{} `json:"parameters"`          // 执行参数覆盖；当前 Manager provider 不支持
}

// TaskExecute POST /api/manager/tasks/:task_type/:id/execute
// @Summary 执行任务 | Execute task
// @Description 触发指定任务立即执行 | Trigger immediate execution of a specific task
// @Tags Manager
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型：mvt_generation|embedding | Task type: mvt_generation|embedding"
// @Param id path int true "任务ID | Task ID"
// @Param body body TaskExecuteRequest false "执行配置 | Execution configuration"
// @Success 200 {object} map[string]interface{} "执行ID | Execution ID"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *TaskProviderHandler) TaskExecute(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	taskType := c.Param("task_type")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "无效的任务ID"})
		return
	}

	var req TaskExecuteRequest
	_ = c.ShouldBindJSON(&req)
	triggerType, err := commonExecution.NormalizeTriggerType(req.TriggerType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = commonExecution.ModuleManager
	}
	if len(req.Parameters) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Manager task provider does not support execution parameter overrides"})
		return
	}
	var parentExecID *string
	if req.ParentExecutionID != "" {
		parentExecID = &req.ParentExecutionID
	}

	ctx := c.Request.Context()
	var executionID string

	switch taskType {
	case "mvt_generation":
		executionID, err = h.mvtTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
	case "embedding":
		executionID, err = h.embeddingTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "不支持的任务类型: " + taskType})
		return
	}

	if err != nil {
		if errors.Is(err, service.ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "任务不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "success",
		"execution_id": executionID,
		"message":      "任务已触发执行",
	})
}

// ExecutionStatus GET /api/manager/executions/:execution_id
// @Summary 获取执行状态 | Get execution status
// @Description 获取任务执行记录的状态信息 | Get status information of a task execution record
// @Tags Manager
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} map[string]interface{} "执行状态 | Execution status"
// @Failure 404 {object} map[string]interface{} "执行记录不存在 | Execution not found"
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ExecutionStatus(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	executionID := c.Param("execution_id")

	exec, err := h.taskExecRepo.GetByExecutionID(c.Request.Context(), executionID, int(tenantID))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "执行记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "success", "data": exec})
}

// ===== EmbeddingTask CRUD =====

// CreateEmbeddingTask POST /api/manager/embedding-tasks
// @Summary 创建向量化任务配置 | Create embedding task configuration
// @Description 创建新的向量化任务配置（定时或手动触发）| Create a new embedding task configuration (scheduled or manual)
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body models.EmbeddingTask true "向量化任务配置 | Embedding task configuration"
// @Success 201 {object} map[string]interface{} "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /embedding_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateEmbeddingTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	var task models.EmbeddingTask
	if err := c.ShouldBindJSON(&task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	task.TenantID = tenantID
	task.CreatedBy = &userID

	if err := h.embeddingTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "data": task})
}

// UpdateEmbeddingTask PUT /api/manager/embedding-tasks/:id
// @Summary 更新向量化任务配置 | Update embedding task configuration
// @Description 更新指定的向量化任务配置 | Update a specific embedding task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body models.EmbeddingTask true "向量化任务配置 | Embedding task configuration"
// @Success 200 {object} map[string]interface{} "更新后的任务配置 | Updated task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /embedding_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateEmbeddingTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "无效的任务ID"})
		return
	}

	existing, err := h.embeddingTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "任务不存在"})
		return
	}

	if err := c.ShouldBindJSON(existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	existing.ID = uint(id)
	existing.TenantID = tenantID

	if err := h.embeddingTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": existing})
}

// DeleteEmbeddingTask DELETE /api/manager/embedding-tasks/:id
// @Summary 删除向量化任务配置 | Delete embedding task configuration
// @Description 删除指定的向量化任务配置 | Delete a specific embedding task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /embedding_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteEmbeddingTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "无效的任务ID"})
		return
	}

	if err := h.embeddingTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "已删除"})
}

// ===== MvtTask CRUD =====

// CreateMvtTask POST /api/manager/mvt-tasks
// @Summary 创建MVT生成任务配置 | Create MVT generation task configuration
// @Description 创建新的MVT瓦片生成任务配置 | Create a new MVT tile generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body models.MvtTask true "MVT任务配置 | MVT task configuration"
// @Success 201 {object} map[string]interface{} "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /mvt_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateMvtTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	var task models.MvtTask
	if err := c.ShouldBindJSON(&task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	task.TenantID = tenantID
	task.CreatedBy = &userID

	if err := h.mvtTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "data": task})
}

// UpdateMvtTask PUT /api/manager/mvt-tasks/:id
// @Summary 更新MVT生成任务配置 | Update MVT generation task configuration
// @Description 更新指定的MVT瓦片生成任务配置 | Update a specific MVT tile generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body models.MvtTask true "MVT任务配置 | MVT task configuration"
// @Success 200 {object} map[string]interface{} "更新后的任务配置 | Updated task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /mvt_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateMvtTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "无效的任务ID"})
		return
	}

	existing, err := h.mvtTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "任务不存在"})
		return
	}

	if err := c.ShouldBindJSON(existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	existing.ID = uint(id)
	existing.TenantID = tenantID

	if err := h.mvtTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": existing})
}

// DeleteMvtTask DELETE /api/manager/mvt-tasks/:id
// @Summary 删除MVT生成任务配置 | Delete MVT generation task configuration
// @Description 删除指定的MVT瓦片生成任务配置 | Delete a specific MVT tile generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /mvt_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteMvtTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "无效的任务ID"})
		return
	}

	if err := h.mvtTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "已删除"})
}
