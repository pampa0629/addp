package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/taskprovider"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type materializationExecuteResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
}
type materializationTaskRequest struct {
	TriggerType       string                 `json:"trigger_type"`
	Source            string                 `json:"source"`
	ParentExecutionID string                 `json:"parent_execution_id"`
	Parameters        map[string]interface{} `json:"parameters"`
}
type materializationTaskList struct {
	Items    []service.MaterializationTask `json:"items"`
	Total    int64                         `json:"total"`
	Page     int                           `json:"page"`
	PageSize int                           `json:"page_size"`
}
type MaterializationTaskHandler struct {
	svc  *service.MaterializationService
	repo *execution.TaskExecutionRepository
}

func NewMaterializationTaskHandler(svc *service.MaterializationService, repo *execution.TaskExecutionRepository) *MaterializationTaskHandler {
	return &MaterializationTaskHandler{svc: svc, repo: repo}
}

// List lists saved approved logical tables as materialization tasks.
// @Summary 列出逻辑表物化任务 | List logical table materialization tasks
// @Description 仅返回已审批且配置物化目标的逻辑表。| Return approved logical tables with a configured target.
// @Tags Model
// @Produce json
// @Param task_type query string false "任务类型 | Task type"
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页条数 | Page size"
// @Success 200 {object} materializationTaskList
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.task_provider.read"]
// @Router /task-provider/tasks [get]
// @Security BearerAuth
func (h *MaterializationTaskHandler) List(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("task_type"))
	if kind != "" && kind != service.MaterializationTaskType {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	page, size := 1, 100
	if value := c.Query("page"); value != "" {
		v, e := strconv.Atoi(value)
		if e != nil || v < 1 {
			c.JSON(400, invalidParamsResponse(c))
			return
		}
		page = v
	}
	if value := c.Query("page_size"); value != "" {
		v, e := strconv.Atoi(value)
		if e != nil || v < 1 || v > 100 {
			c.JSON(400, invalidParamsResponse(c))
			return
		}
		size = v
	}
	items, total, err := h.svc.ListMaterializationTasks(c.Request.Context(), getTenantID(c), page, size)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(200, materializationTaskList{items, total, page, size})
}
func materializationTaskID(c *gin.Context) (int64, bool) {
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id <= 0 || c.Param("task_type") != service.MaterializationTaskType {
		c.JSON(400, invalidParamsResponse(c))
		return 0, false
	}
	return id, true
}

// Detail returns the task execution contract from its logical table.
// @Summary 获取逻辑表物化任务 | Get logical table materialization task
// @Tags Model
// @Produce json
// @Param task_type path string true "任务类型 | Task type"
// @Param id path int true "逻辑表 ID | Logical table ID"
// @Success 200 {object} service.MaterializationTask
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.task_provider.read"]
// @Router /task-provider/tasks/{task_type}/{id} [get]
// @Security BearerAuth
func (h *MaterializationTaskHandler) Detail(c *gin.Context) {
	id, ok := materializationTaskID(c)
	if !ok {
		return
	}
	item, err := h.svc.MaterializationTaskDetail(id, getTenantID(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(200, item)
}

// Execute enqueues a materialization child with verified orchestration lineage.
// @Summary 编排执行逻辑表物化 | Execute orchestrated logical table materialization
// @Description 仅接受 addp-orchestrator 和有效父执行；参数只允许模型版本。| Only addp-orchestrator with a valid parent execution; parameters only allow model version.
// @Tags Model
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型 | Task type"
// @Param id path int true "逻辑表 ID | Logical table ID"
// @Param request body materializationTaskRequest true "执行请求 | Execution request"
// @Success 202 {object} materializationExecuteResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 503 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.task_provider.execute"]
// @Router /task-provider/tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *MaterializationTaskHandler) Execute(c *gin.Context) {
	id, ok := materializationTaskID(c)
	if !ok {
		return
	}
	var req materializationTaskRequest
	if commonAPI.BindOptionalJSONStrict(c, &req) != nil {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	parent, err := execution.NormalizeOrchestratorChildContext(req.Source, req.ParentExecutionID)
	if err != nil {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	trigger, err := execution.NormalizeTriggerType(req.TriggerType)
	if err != nil {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	task, err := h.svc.MaterializationTaskDetail(id, getTenantID(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if taskprovider.ValidateExecutionParameters(task.ExecutionContract.InputSchema, req.Parameters, taskprovider.ParameterValidationOptions{AllowMissingRequired: true}) != nil {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	version := models.VersionRequest{Version: task.ExecutionContract.InputDefaults["version"].(int64)}
	raw, _ := json.Marshal(req.Parameters)
	if json.Unmarshal(raw, &version) != nil || version.Version <= 0 {
		c.JSON(400, invalidParamsResponse(c))
		return
	}
	item, err := h.svc.EnqueueMaterialization(c.Request.Context(), id, getTenantID(c), version.Version, "", parent, trigger)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, materializationExecuteResponse{item.ExecutionID, item.Status})
}

// Status returns the unified execution and its stable outputs.
// @Failure 400 {object} models.ErrorResponse
// @Summary 获取物化执行状态 | Get materialization execution status
// @Tags Model
// @Produce json
// @Param execution_id path string true "执行 UUID | Execution UUID"
// @Success 200 {object} taskprovider.ExecutionStatusResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.task_provider.read"]
// @Router /task-provider/executions/{execution_id} [get]
// @Security BearerAuth
func (h *MaterializationTaskHandler) Status(c *gin.Context) {
	if id, err := uuid.Parse(c.Param("execution_id")); err != nil || id == uuid.Nil || id.String() != c.Param("execution_id") {
		c.JSON(400, invalidParamsResponse(c))
		return
	}

	item, err := h.repo.GetByExecutionID(c.Request.Context(), c.Param("execution_id"), int(getTenantID(c)))
	if err != nil || item.Module != execution.ModuleModel || item.TaskType != service.MaterializationTaskType {
		c.JSON(404, localizedErrorResponse(c, "model.common.resource_not_found", "execution_not_found"))
		return
	}
	c.JSON(200, taskprovider.NewExecutionStatusResponse(item))
}
