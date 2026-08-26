package api

import (
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type MaterializationTaskProviderHandler struct {
	materialization *service.MaterializationService
	executions      *commonExecution.TaskExecutionRepository
}

func NewMaterializationTaskProviderHandler(materialization *service.MaterializationService, executions *commonExecution.TaskExecutionRepository) *MaterializationTaskProviderHandler {
	return &MaterializationTaskProviderHandler{materialization: materialization, executions: executions}
}

type materializationTaskItem struct {
	ID                int64                          `json:"id"`
	TenantID          int64                          `json:"tenant_id"`
	TaskType          string                         `json:"task_type"`
	Name              string                         `json:"name"`
	Description       string                         `json:"description,omitempty"`
	Status            string                         `json:"status"`
	ExecutionContract taskprovider.ExecutionContract `json:"execution_contract"`
}

type materializationTaskListResponse struct {
	Items    []materializationTaskItem `json:"items"`
	Total    int                       `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
}

type materializationExecuteRequest struct {
	TriggerType       string                 `json:"trigger_type"`
	Source            string                 `json:"source"`
	ParentExecutionID string                 `json:"parent_execution_id"`
	Parameters        map[string]interface{} `json:"parameters"`
}

type materializationExecuteResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status" enums:"pending" example:"pending"`
}

type materializationExecutionResponse struct {
	*commonExecution.TaskExecution
	Outputs commonModels.JSONMap `json:"outputs"`
}

// ListTasks godoc
// @Summary 列出 Model 物化任务 | List Model materialization tasks
// @Description 列出由已审批逻辑表派生的物化准备和物化发布任务。| List materialization prepare and publish tasks derived from approved logical tables.
// @Tags Materialization
// @Produce json
// @Param task_type query string false "任务类型 | Task type"
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} materializationTaskListResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.task_provider.read"]
// @Router /task-provider/tasks [get]
// @Security BearerAuth
func (h *MaterializationTaskProviderHandler) ListTasks(c *gin.Context) {
	taskType := strings.TrimSpace(c.Query("task_type"))
	page := boundedPositiveInt(c.Query("page"), 1, 1_000_000)
	pageSize := boundedPositiveInt(c.Query("page_size"), 20, 100)
	items, total, err := h.materialization.ListTaskDefinitions(getTenantID(c), taskType, page, pageSize)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	responseItems := make([]materializationTaskItem, 0, len(items))
	for _, item := range items {
		responseItems = append(responseItems, toMaterializationTaskItem(item))
	}
	c.JSON(http.StatusOK, materializationTaskListResponse{Items: responseItems, Total: total, Page: page, PageSize: pageSize})
}

// TaskDetail godoc
// @Summary 获取 Model 物化任务 | Get Model materialization task
// @Description 获取由已审批逻辑表派生的物化任务详情。| Get materialization task detail derived from an approved logical table.
// @Tags Materialization
// @Produce json
// @Param task_type path string true "任务类型 | Task type"
// @Param id path int true "逻辑表 ID | Logical table ID"
// @Success 200 {object} materializationTaskItem
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.task_provider.read"]
// @Router /task-provider/tasks/{task_type}/{id} [get]
// @Security BearerAuth
func (h *MaterializationTaskProviderHandler) TaskDetail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	item, err := h.materialization.GetTaskDefinition(id, getTenantID(c), c.Param("task_type"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, toMaterializationTaskItem(*item))
}

// TaskExecute godoc
// @Summary 执行 Model 物化任务 | Execute Model materialization task
// @Description 仅接受 Orchestrator 以父 execution 血缘触发；调用方不能提交物理名称或 DDL。| Only accepts Orchestrator execution-lineage invocation; callers cannot submit physical names or DDL.
// @Tags Materialization
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型 | Task type"
// @Param id path int true "逻辑表 ID | Logical table ID"
// @Param request body materializationExecuteRequest false "执行请求 | Execution request"
// @Success 202 {object} materializationExecuteResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.task_provider.execute"]
// @Router /task-provider/tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *MaterializationTaskProviderHandler) TaskExecute(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	var req materializationExecuteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	if len(req.Parameters) != 0 || strings.TrimSpace(req.Source) != commonExecution.ModuleOrchestrator {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	parent := strings.TrimSpace(req.ParentExecutionID)
	if _, err := uuid.Parse(parent); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	trigger, err := commonExecution.NormalizeTriggerType(req.TriggerType)
	if err != nil || (trigger != commonExecution.TriggerTypeManual && trigger != commonExecution.TriggerTypeScheduled) {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	parentExecutionID := parent
	var executionID string
	switch c.Param("task_type") {
	case commonExecution.TaskTypeMaterializationPrepare:
		executionID, _, err = h.materialization.Prepare(c.Request.Context(), id, getTenantID(c), getUserID(c), trigger, commonExecution.ModuleOrchestrator, &parentExecutionID)
	case commonExecution.TaskTypeMaterializationPublish:
		executionID, err = h.materialization.Publish(c.Request.Context(), id, getTenantID(c), getUserID(c), trigger, commonExecution.ModuleOrchestrator, &parentExecutionID)
	default:
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, materializationExecuteResponse{ExecutionID: executionID, Status: commonExecution.ExecutionStatusPending})
}

// ExecutionStatus godoc
// @Summary 获取 Model 物化执行状态 | Get Model materialization execution status
// @Tags Materialization
// @Produce json
// @Param execution_id path string true "执行 ID | Execution ID"
// @Success 200 {object} materializationExecutionResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.task_provider.read"]
// @Router /task-provider/executions/{execution_id} [get]
// @Security BearerAuth
func (h *MaterializationTaskProviderHandler) ExecutionStatus(c *gin.Context) {
	executionID := strings.TrimSpace(c.Param("execution_id"))
	if _, err := uuid.Parse(executionID); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	execution, err := h.executions.GetByExecutionID(c.Request.Context(), executionID, int(getTenantID(c)))
	if err != nil || execution.Module != commonExecution.ModuleModel || !isMaterializationExecutionTaskType(execution.TaskType) {
		c.JSON(http.StatusNotFound, localizedErrorResponse(c, "model.materialization.not_found", "materialization_execution_not_found"))
		return
	}
	outputs := commonModels.JSONMap{}
	if execution.Metadata != nil {
		if raw, ok := execution.Metadata["outputs"].(map[string]interface{}); ok {
			outputs = commonModels.JSONMap(raw)
		}
	}
	c.JSON(http.StatusOK, materializationExecutionResponse{TaskExecution: execution, Outputs: outputs})
}

func toMaterializationTaskItem(item service.MaterializationTaskDefinition) materializationTaskItem {
	return materializationTaskItem{ID: item.ID, TenantID: item.TenantID, TaskType: item.TaskType, Name: item.Name, Description: item.Description, Status: "idle", ExecutionContract: item.ExecutionContract}
}

func boundedPositiveInt(raw string, fallback, maximum int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func isMaterializationExecutionTaskType(taskType string) bool {
	return taskType == commonExecution.TaskTypeMaterializationPrepare || taskType == commonExecution.TaskTypeMaterializationPublish
}
