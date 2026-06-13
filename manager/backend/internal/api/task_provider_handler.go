package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// TaskProviderHandler 标准 TaskProvider API 处理器
// 实现: GET /api/manager/tasks, POST /api/manager/tasks/:task_type/:id/execute
//
//	GET /api/manager/tasks/:task_type/:id, GET /api/manager/executions/:execution_id
type TaskProviderHandler struct {
	embeddingTaskSvc *service.EmbeddingTaskService
	tileCacheTaskSvc *service.TileCacheTaskService
	taskExecRepo     *commonExecution.TaskExecutionRepository
}

// NewTaskProviderHandler 创建处理器
func NewTaskProviderHandler(
	embeddingTaskSvc *service.EmbeddingTaskService,
	tileCacheTaskSvc *service.TileCacheTaskService,
	taskExecRepo *commonExecution.TaskExecutionRepository,
) *TaskProviderHandler {
	return &TaskProviderHandler{
		embeddingTaskSvc: embeddingTaskSvc,
		tileCacheTaskSvc: tileCacheTaskSvc,
		taskExecRepo:     taskExecRepo,
	}
}

// TaskListResponse 任务列表响应（统一包装 tile_cache_generation 和 embedding 任务）
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

// EmbeddingTaskRequest 是私有向量化任务 CRUD 的显式契约。
type EmbeddingTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type EmbeddingTaskTargetResponse struct {
	Scope     string `json:"scope"`
	EngineID  uint   `json:"engine_id,omitempty"`
	ItemID    uint   `json:"item_id,omitempty"`
	NodeID    uint   `json:"node_id,omitempty"`
	Locator   string `json:"locator,omitempty"`
	Recursive bool   `json:"recursive"`
}

type EmbeddingTaskResponse struct {
	ID                  uint                         `json:"id"`
	TenantID            uint                         `json:"tenant_id"`
	TaskType            string                       `json:"task_type"`
	Name                string                       `json:"name"`
	Description         string                       `json:"description,omitempty"`
	Enabled             bool                         `json:"enabled"`
	Schedule            string                       `json:"schedule,omitempty"`
	NextRunAt           *time.Time                   `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                   `json:"last_run_at,omitempty"`
	LastExecutionID     *string                      `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                      `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                        `json:"created_by,omitempty"`
	Config              commonModels.JSONMap         `json:"config"`
	Target              *EmbeddingTaskTargetResponse `json:"target,omitempty"`
	CreatedAt           time.Time                    `json:"created_at"`
	UpdatedAt           time.Time                    `json:"updated_at"`
}

type TileCacheTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type TileCacheTaskTargetResponse struct {
	ItemID          uint   `json:"item_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	Locator         string `json:"locator,omitempty"`
	SourceEngineID  uint   `json:"source_engine_id,omitempty"`
	Schema          string `json:"schema,omitempty"`
	Table           string `json:"table,omitempty"`
}

type TileCacheTaskTileResponse struct {
	Format         string `json:"format"`
	MinZoom        int    `json:"min_zoom"`
	MaxZoom        int    `json:"max_zoom"`
	TargetSRID     int    `json:"target_srid,omitempty"`
	GeometryColumn string `json:"geometry_column,omitempty"`
}

type TileCacheTaskResponse struct {
	ID                  uint                         `json:"id"`
	TenantID            uint                         `json:"tenant_id"`
	TaskType            string                       `json:"task_type"`
	Name                string                       `json:"name"`
	Description         string                       `json:"description,omitempty"`
	Enabled             bool                         `json:"enabled"`
	Schedule            string                       `json:"schedule,omitempty"`
	NextRunAt           *time.Time                   `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                   `json:"last_run_at,omitempty"`
	LastExecutionID     *string                      `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                      `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                        `json:"created_by,omitempty"`
	Config              commonModels.JSONMap         `json:"config"`
	Target              *TileCacheTaskTargetResponse `json:"target,omitempty"`
	Tile                *TileCacheTaskTileResponse   `json:"tile,omitempty"`
	CreatedAt           time.Time                    `json:"created_at"`
	UpdatedAt           time.Time                    `json:"updated_at"`
}

// ListTasks GET /api/manager/tasks
// 查询参数：?task_type=tile_cache_generation|embedding
// @Summary 列出任务 | List tasks
// @Description 列出Manager模块的任务（瓦片缓存生成任务和向量化任务）| List Manager module tasks (tile cache generation and embedding tasks)
// @Tags Manager
// @Produce json
// @Param task_type query string false "任务类型过滤：tile_cache_generation|embedding | Task type filter: tile_cache_generation|embedding"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 400 {object} map[string]interface{} "不支持的任务类型 | Unsupported task type"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTasks(c *gin.Context) {
	h.listTasks(c, strings.TrimSpace(c.Query("task_type")))
}

func (h *TaskProviderHandler) listTasks(c *gin.Context, taskType string) {
	tenantID := c.GetUint("tenant_id")

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
	case commonExecution.TaskTypeTileCacheGeneration:
		tasks, t, err := h.tileCacheTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeTileCacheGeneration,
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
		tileCacheTasks, tileCacheTotal, err := h.tileCacheTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		embTasks, embTotal, err := h.embeddingTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		total = tileCacheTotal + embTotal
		for _, task := range tileCacheTasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeTileCacheGeneration,
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

// ListTileCacheTasks GET /api/manager/tile_cache_tasks
// @Summary 列出瓦片缓存生成任务配置 | List tile cache generation task configurations
// @Description 列出 Manager 模块的瓦片缓存生成任务配置。该私有入口固定返回 task_type=tile_cache_generation；编排模块应使用标准 /tasks 入口。| List Manager tile cache generation task configurations. This private endpoint always returns task_type=tile_cache_generation; orchestrator should use the standard /tasks endpoint.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /tile_cache_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTileCacheTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	tasks, total, err := h.tileCacheTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]TileCacheTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, tileCacheTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ListEmbeddingTasks GET /api/manager/embedding_tasks
// @Summary 列出向量化任务配置 | List embedding task configurations
// @Description 列出 Manager 模块的向量化任务配置。该私有入口固定返回 task_type=embedding；编排模块应使用标准 /tasks 入口。| List Manager embedding task configurations. This private endpoint always returns task_type=embedding; orchestrator should use the standard /tasks endpoint.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /embedding_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListEmbeddingTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	tasks, total, err := h.embeddingTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	items := make([]EmbeddingTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, embeddingTaskResponse(task))
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
// @Param task_type path string true "任务类型：tile_cache_generation|embedding | Task type: tile_cache_generation|embedding"
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "任务详情 | Task detail"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /tasks/{task_type}/{id} [get]
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
	case commonExecution.TaskTypeTileCacheGeneration:
		task, err := h.tileCacheTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "任务不存在"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": tileCacheTaskResponse(task)})
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
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": embeddingTaskResponse(task)})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "不支持的任务类型: " + taskType})
	}
}

// GetTileCacheTask GET /api/manager/tile_cache_tasks/:id
// @Summary 获取瓦片缓存生成任务配置 | Get tile cache generation task configuration
// @Description 获取指定瓦片缓存生成任务配置 | Get a specific tile cache generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} TileCacheTaskResponse "任务配置 | Task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /tile_cache_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetTileCacheTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	task, err := h.tileCacheTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, tileCacheTaskResponse(task))
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
// @Param task_type path string true "任务类型：tile_cache_generation|embedding | Task type: tile_cache_generation|embedding"
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

	req, err := decodeTaskExecuteRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
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
	case commonExecution.TaskTypeTileCacheGeneration:
		executionID, err = h.tileCacheTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
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

func decodeTaskExecuteRequest(c *gin.Context) (TaskExecuteRequest, error) {
	var req TaskExecuteRequest
	if c.Request.Body == nil {
		return req, nil
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return req, nil
		}
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	return req, nil
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

// CreateEmbeddingTask POST /api/manager/embedding_tasks
// @Summary 创建向量化任务配置 | Create embedding task configuration
// @Description 创建新的向量化任务配置（定时或手动触发）| Create a new embedding task configuration (scheduled or manual)
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body EmbeddingTaskRequest true "向量化任务配置 | Embedding task configuration"
// @Success 201 {object} TileCacheTaskResponse "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /embedding_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateEmbeddingTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	req, err := decodeEmbeddingTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.EmbeddingTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}

	if err := h.embeddingTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "data": embeddingTaskResponse(&task)})
}

// UpdateEmbeddingTask PUT /api/manager/embedding_tasks/:id
// @Summary 更新向量化任务配置 | Update embedding task configuration
// @Description 更新指定的向量化任务配置 | Update a specific embedding task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body EmbeddingTaskRequest true "向量化任务配置 | Embedding task configuration"
// @Success 200 {object} TileCacheTaskResponse "更新后的任务配置 | Updated task configuration"
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

	req, err := decodeEmbeddingTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	existing.ID = uint(id)
	existing.TenantID = tenantID
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = strings.TrimSpace(req.Description)
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.Schedule = strings.TrimSpace(req.Schedule)
	existing.NextRunAt = req.NextRunAt
	existing.Config = req.Config

	if err := h.embeddingTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": embeddingTaskResponse(existing)})
}

// DeleteEmbeddingTask DELETE /api/manager/embedding_tasks/:id
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

func decodeEmbeddingTaskRequest(c *gin.Context) (EmbeddingTaskRequest, error) {
	var req EmbeddingTaskRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	if req.Config == nil {
		req.Config = commonModels.JSONMap{}
	}
	return req, nil
}

func embeddingTaskResponse(task *models.EmbeddingTask) EmbeddingTaskResponse {
	resp := EmbeddingTaskResponse{}
	if task == nil {
		return resp
	}
	resp = EmbeddingTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeEmbedding,
		Name:                task.Name,
		Description:         task.Description,
		Enabled:             task.Enabled,
		Schedule:            task.Schedule,
		NextRunAt:           task.NextRunAt,
		LastRunAt:           task.LastRunAt,
		LastExecutionID:     task.LastExecutionID,
		LastExecutionStatus: task.LastExecutionStatus,
		CreatedBy:           task.CreatedBy,
		Config:              task.Config,
		CreatedAt:           task.CreatedAt,
		UpdatedAt:           task.UpdatedAt,
	}
	if target, ok := asJSONMap(task.Config["target"]); ok {
		resp.Target = &EmbeddingTaskTargetResponse{
			Scope:     stringFromConfig(target["scope"]),
			EngineID:  uintFromConfig(target["engine_id"]),
			ItemID:    uintFromConfig(target["item_id"]),
			NodeID:    uintFromConfig(target["node_id"]),
			Locator:   stringFromConfig(target["locator"]),
			Recursive: boolFromConfig(target["recursive"], true),
		}
	}
	return resp
}

func asJSONMap(value interface{}) (commonModels.JSONMap, bool) {
	switch v := value.(type) {
	case commonModels.JSONMap:
		return v, true
	case map[string]interface{}:
		return commonModels.JSONMap(v), true
	default:
		return nil, false
	}
}

func uintFromConfig(value interface{}) uint {
	switch v := value.(type) {
	case uint:
		return v
	case int:
		if v > 0 {
			return uint(v)
		}
	case int64:
		if v > 0 {
			return uint(v)
		}
	case float64:
		if v > 0 {
			return uint(v)
		}
	}
	return 0
}

func stringFromConfig(value interface{}) string {
	if v, ok := value.(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func boolFromConfig(value interface{}, defaultValue bool) bool {
	if v, ok := value.(bool); ok {
		return v
	}
	return defaultValue
}

// ===== TileCacheTask CRUD =====

// CreateTileCacheTask POST /api/manager/tile_cache_tasks
// @Summary 创建瓦片缓存生成任务配置 | Create tile cache generation task configuration
// @Description 创建新的瓦片缓存生成任务配置 | Create a new tile cache generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body TileCacheTaskRequest true "瓦片缓存任务配置 | Tile cache task configuration"
// @Success 201 {object} map[string]interface{} "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /tile_cache_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateTileCacheTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	req, err := decodeTileCacheTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.TileCacheTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}

	if err := h.tileCacheTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, tileCacheTaskResponse(&task))
}

// UpdateTileCacheTask PUT /api/manager/tile_cache_tasks/:id
// @Summary 更新瓦片缓存生成任务配置 | Update tile cache generation task configuration
// @Description 更新指定的瓦片缓存生成任务配置 | Update a specific tile cache generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body TileCacheTaskRequest true "瓦片缓存任务配置 | Tile cache task configuration"
// @Success 200 {object} map[string]interface{} "更新后的任务配置 | Updated task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /tile_cache_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateTileCacheTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	existing, err := h.tileCacheTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}

	req, err := decodeTileCacheTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.ID = uint(id)
	existing.TenantID = tenantID
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = strings.TrimSpace(req.Description)
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.Schedule = strings.TrimSpace(req.Schedule)
	existing.NextRunAt = req.NextRunAt
	existing.Config = req.Config

	if err := h.tileCacheTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tileCacheTaskResponse(existing))
}

// DeleteTileCacheTask DELETE /api/manager/tile_cache_tasks/:id
// @Summary 删除瓦片缓存生成任务配置 | Delete tile cache generation task configuration
// @Description 删除指定的瓦片缓存生成任务配置 | Delete a specific tile cache generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /tile_cache_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteTileCacheTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	if err := h.tileCacheTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListTileCaches GET /api/manager/tile_cache
// @Summary 列出瓦片缓存结果 | List tile cache results
// @Description 查询瓦片缓存结果状态 | Query tile cache result states
// @Tags Manager
// @Produce json
// @Param item_id query int false "数据项ID | Item ID"
// @Param item_fingerprint query string false "数据项指纹 | Item fingerprint"
// @Param task_id query int false "任务ID | Task ID"
// @Param status query string false "状态 | Status"
// @Param q query string false "关键词 | Keyword"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "结果列表 | Result list"
// @Router /tile_cache [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTileCaches(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	itemID64, _ := strconv.ParseUint(c.Query("item_id"), 10, 32)
	taskID64, _ := strconv.ParseUint(c.Query("task_id"), 10, 32)
	results, total, err := h.tileCacheTaskSvc.ListTileCache(c.Request.Context(), repository.TileCacheFilter{
		TenantID:        tenantID,
		ItemID:          uint(itemID64),
		ItemFingerprint: c.Query("item_fingerprint"),
		TaskID:          uint(taskID64),
		Status:          c.Query("status"),
		Q:               c.Query("q"),
		Page:            page,
		PageSize:        pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      results,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetTileCache GET /api/manager/tile_cache/:id
// @Summary 获取瓦片缓存结果详情 | Get tile cache result detail
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} models.TileCache "结果详情 | Result detail"
// @Router /tile_cache/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetTileCache(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	result, err := h.tileCacheTaskSvc.GetTileCache(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "结果不存在"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteTileCache DELETE /api/manager/tile_cache/:id
// @Summary 删除瓦片缓存结果 | Delete tile cache result
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Router /tile_cache/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteTileCache(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	if err := h.tileCacheTaskSvc.DeleteTileCache(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

func decodeTileCacheTaskRequest(c *gin.Context) (TileCacheTaskRequest, error) {
	var req TileCacheTaskRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	if req.Config == nil {
		req.Config = commonModels.JSONMap{}
	}
	return req, nil
}

func tileCacheTaskResponse(task *models.TileCacheTask) TileCacheTaskResponse {
	resp := TileCacheTaskResponse{}
	if task == nil {
		return resp
	}
	resp = TileCacheTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeTileCacheGeneration,
		Name:                task.Name,
		Description:         task.Description,
		Enabled:             task.Enabled,
		Schedule:            task.Schedule,
		NextRunAt:           task.NextRunAt,
		LastRunAt:           task.LastRunAt,
		LastExecutionID:     task.LastExecutionID,
		LastExecutionStatus: task.LastExecutionStatus,
		CreatedBy:           task.CreatedBy,
		Config:              task.Config,
		CreatedAt:           task.CreatedAt,
		UpdatedAt:           task.UpdatedAt,
	}
	if target, ok := asJSONMap(task.Config["target"]); ok {
		resp.Target = &TileCacheTaskTargetResponse{
			ItemID:          uintFromConfig(target["item_id"]),
			ItemFingerprint: stringFromConfig(target["item_fingerprint"]),
			Locator:         stringFromConfig(target["locator"]),
			SourceEngineID:  uintFromConfig(target["source_engine_id"]),
			Schema:          stringFromConfig(target["schema"]),
			Table:           stringFromConfig(target["table"]),
		}
	}
	if tile, ok := asJSONMap(task.Config["tile"]); ok {
		options, _ := asJSONMap(task.Config["options"])
		resp.Tile = &TileCacheTaskTileResponse{
			Format:         stringFromConfig(tile["format"]),
			MinZoom:        intFromAPIConfig(tile["min_zoom"], 0),
			MaxZoom:        intFromAPIConfig(tile["max_zoom"], 0),
			TargetSRID:     intFromAPIConfig(tile["target_srid"], 0),
			GeometryColumn: stringFromConfig(options["geometry_column"]),
		}
	}
	return resp
}

func intFromAPIConfig(value interface{}, defaultValue int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint:
		return int(v)
	case float64:
		return int(v)
	}
	return defaultValue
}
