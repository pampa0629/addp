package api

import (
	"errors"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	commonModels "github.com/addp/common/models"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// TaskProviderHandler 标准 TaskProvider API 处理器
// 实现: GET /api/manager/tasks, POST /api/manager/tasks/:task_type/:id/execute
//       GET /api/manager/tasks/:task_type/:id, GET /api/manager/executions/:execution_id
//       POST /api/manager/executions/:execution_id/cancel
type TaskProviderHandler struct {
	embeddingTaskSvc *service.EmbeddingTaskService
	mvtTaskSvc       *service.MvtTaskService
	taskExecRepo     *commonRepo.TaskExecutionRepository
}

// NewTaskProviderHandler 创建处理器
func NewTaskProviderHandler(
	embeddingTaskSvc *service.EmbeddingTaskService,
	mvtTaskSvc *service.MvtTaskService,
	taskExecRepo *commonRepo.TaskExecutionRepository,
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
// @Summary ListTasks
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /listtasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	taskType := c.Query("task_type")

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
	case "mvt_generation":
		tasks, t, err := h.mvtTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: "mvt_generation",
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case "embedding":
		tasks, t, err := h.embeddingTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: "embedding",
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	default:
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
				ID: task.ID, TenantID: task.TenantID, TaskType: "mvt_generation",
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
		for _, task := range embTasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: "embedding",
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
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
// @Summary TaskDetail
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /taskdetail [get]
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
	TriggerType       string `json:"trigger_type"`        // manual|schedule|api|orchestrator，默认 manual
	ParentExecutionID string `json:"parent_execution_id"` // 父执行ID（Orchestrator 调用时传入）
}

// TaskExecute POST /api/manager/tasks/:task_type/:id/execute
// @Summary TaskExecute
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /taskexecute [get]
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
	if req.TriggerType == "" {
		req.TriggerType = commonModels.TriggerTypeManual
	}
	var parentExecID *string
	if req.ParentExecutionID != "" {
		parentExecID = &req.ParentExecutionID
	}

	ctx := c.Request.Context()
	var executionID string

	switch taskType {
	case "mvt_generation":
		executionID, err = h.mvtTaskSvc.Execute(ctx, uint(id), tenantID, req.TriggerType, parentExecID)
	case "embedding":
		executionID, err = h.embeddingTaskSvc.Execute(ctx, uint(id), tenantID, req.TriggerType, parentExecID)
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
// @Summary ExecutionStatus
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /executionstatus [get]
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

// ExecutionCancel POST /api/manager/executions/:execution_id/cancel
// @Summary ExecutionCancel
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /executioncancel [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ExecutionCancel(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	executionID := c.Param("execution_id")

	err := h.taskExecRepo.UpdateStatus(
		c.Request.Context(), executionID, int(tenantID),
		commonModels.ExecutionStatusRunning, commonModels.ExecutionStatusCancelled,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "取消失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "已取消"})
}

// ===== EmbeddingTask CRUD =====

// CreateEmbeddingTask POST /api/manager/embedding-tasks
// @Summary CreateEmbeddingTask
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /createembeddingtask [get]
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
// @Summary UpdateEmbeddingTask
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /updateembeddingtask [get]
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
// @Summary DeleteEmbeddingTask
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /deleteembeddingtask [get]
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
// @Summary CreateMvtTask
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /createmvttask [get]
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
// @Summary UpdateMvtTask
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /updatemvttask [get]
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
// @Summary DeleteMvtTask
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /deletemvttask [get]
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
