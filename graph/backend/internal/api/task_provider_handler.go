package api

import (
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
)

// TaskProviderHandler 标准 TaskProvider API 处理器。
type TaskProviderHandler struct {
	buildSvc      *service.BuildService
	executionRepo *commonExecution.TaskExecutionRepository
}

func NewTaskProviderHandler(buildSvc *service.BuildService, executionRepo *commonExecution.TaskExecutionRepository) *TaskProviderHandler {
	return &TaskProviderHandler{buildSvc: buildSvc, executionRepo: executionRepo}
}

type graphTaskProviderExecuteRequest struct {
	TriggerType       string                 `json:"trigger_type"`
	Source            string                 `json:"source"`
	ParentExecutionID string                 `json:"parent_execution_id"`
	Parameters        map[string]interface{} `json:"parameters"`
}

type graphTaskProviderExecuteResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
}

type graphTaskListItem struct {
	ID          uint   `json:"id"`
	TenantID    uint   `json:"tenant_id"`
	GraphID     uint   `json:"graph_id"`
	TaskType    string `json:"task_type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
	Status      string `json:"status,omitempty"`
	ExecutionID string `json:"last_execution_id,omitempty"`
}

type graphTaskListResponse struct {
	Items    []graphTaskListItem `json:"items"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

// ListProviderTasks 列出 Graph 构建任务。
// @Summary 列出 TaskProvider 图谱构建任务 | List TaskProvider graph build tasks
// @Description 按标准 TaskProvider 协议列出 Graph 构建任务；task_type 仅支持 kg_build。| List Graph build tasks through the standard TaskProvider protocol; task_type only supports kg_build.
// @Tags 图谱构建 | Graph Build
// @Produce json
// @Param task_type query string false "任务类型，固定为 kg_build | Task type, fixed to kg_build"
// @Success 200 {object} graphTaskListResponse "任务列表 | Task list"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} models.ErrorResponse
// @Router /tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListProviderTasks(c *gin.Context) {
	taskType := strings.TrimSpace(c.Query("task_type"))
	if taskType != "" && taskType != commonExecution.TaskTypeKGBuild {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}

	tasks, err := h.buildSvc.ListAllTasks(commonAuth.GetTenantID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	items := make([]graphTaskListItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, graphTaskProviderListItem(task))
	}
	c.JSON(http.StatusOK, graphTaskListResponse{
		Items:    items,
		Total:    len(items),
		Page:     page,
		PageSize: pageSize,
	})
}

// GetProviderTask 获取 Graph 构建任务详情。
// @Summary 获取 TaskProvider 图谱构建任务详情 | Get TaskProvider graph build task detail
// @Description 按标准 TaskProvider 协议获取 Graph 构建任务详情；task_type 仅支持 kg_build。| Get Graph build task detail through the standard TaskProvider protocol; task_type only supports kg_build.
// @Tags 图谱构建 | Graph Build
// @Produce json
// @Param task_type path string true "任务类型，固定为 kg_build | Task type, fixed to kg_build"
// @Param id path int true "构建任务ID | Build task ID"
// @Success 200 {object} graphTaskListItem
// @Failure 400 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /tasks/{task_type}/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetProviderTask(c *gin.Context) {
	taskType := c.Param("task_type")
	if taskType != commonExecution.TaskTypeKGBuild {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}

	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	task, err := h.buildSvc.GetTask(uint(taskID), commonAuth.GetTenantID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, graphTaskProviderListItem(*task))
}

// ExecuteProviderTask 执行 Graph 构建任务。
// @Summary 执行 TaskProvider 图谱构建任务 | Execute TaskProvider graph build task
// @Description 按标准 TaskProvider 协议执行 Graph 构建任务；task_type 仅支持 kg_build，parameters 当前不支持覆盖。| Execute a Graph build task through the standard TaskProvider protocol; task_type only supports kg_build and parameters overrides are not supported.
// @Tags 图谱构建 | Graph Build
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型，固定为 kg_build | Task type, fixed to kg_build"
// @Param id path int true "构建任务ID | Build task ID"
// @Param request body graphTaskProviderExecuteRequest false "TaskProvider 执行请求 | TaskProvider execution request"
// @Success 202 {object} graphTaskProviderExecuteResponse "执行ID | Execution ID"
// @Failure 400 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *TaskProviderHandler) ExecuteProviderTask(c *gin.Context) {
	taskType := c.Param("task_type")
	if taskType != commonExecution.TaskTypeKGBuild {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}

	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req graphTaskProviderExecuteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Parameters) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Graph task provider does not support execution parameter overrides"})
		return
	}
	triggerType, err := commonExecution.NormalizeTriggerType(req.TriggerType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = commonExecution.ModuleGraph
	}
	var parentExecutionID *string
	if strings.TrimSpace(req.ParentExecutionID) != "" {
		parentExecutionID = &req.ParentExecutionID
	}

	executionID, err := h.buildSvc.RunTaskByIDWithContext(
		c.Request.Context(),
		uint(taskID),
		commonAuth.GetTenantID(c),
		commonAuth.GetUserID(c),
		triggerType,
		source,
		parentExecutionID,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, graphTaskProviderExecuteResponse{
		ExecutionID: executionID,
		Status:      commonExecution.ExecutionStatusRunning,
	})
}

// GetProviderExecution 获取 Graph 构建执行状态。
// @Summary 获取 Graph 构建执行状态 | Get graph build execution status
// @Description 按 execution_id 查询 Graph 构建任务统一执行记录。| Get a Graph build task execution record by execution_id.
// @Tags 图谱构建 | Graph Build
// @Produce json
// @Param execution_id path string true "执行UUID | Execution UUID"
// @Success 200 {object} map[string]interface{} "执行记录 | Execution"
// @Failure 404 {object} models.ErrorResponse
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetProviderExecution(c *gin.Context) {
	execution, err := h.executionRepo.GetByExecutionID(c.Request.Context(), c.Param("execution_id"), int(commonAuth.GetTenantID(c)))
	if err != nil || execution.Module != commonExecution.ModuleGraph {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution not found"})
		return
	}
	c.JSON(http.StatusOK, execution)
}

func graphTaskProviderListItem(task models.BuildTask) graphTaskListItem {
	return graphTaskListItem{
		ID:          task.ID,
		TenantID:    task.TenantID,
		GraphID:     task.GraphID,
		TaskType:    commonExecution.TaskTypeKGBuild,
		Name:        task.Name,
		Description: task.Description,
		Enabled:     task.Status != models.BuildStatusRunning,
		Status:      task.Status,
		ExecutionID: task.ExecutionID,
	}
}
