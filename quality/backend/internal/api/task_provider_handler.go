package api

import (
	"net/http"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/taskprovider"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

// TaskProviderHandler 标准 TaskProvider API 处理器。
type TaskProviderHandler struct {
	checkTaskSvc *service.CheckTaskService
	gateTaskSvc  *service.MaterializationGateService
	executor     *service.CheckExecutor
}

func NewTaskProviderHandler(checkTaskSvc *service.CheckTaskService, gateTaskSvc *service.MaterializationGateService, executor *service.CheckExecutor) *TaskProviderHandler {
	return &TaskProviderHandler{checkTaskSvc: checkTaskSvc, gateTaskSvc: gateTaskSvc, executor: executor}
}

type taskProviderTaskListItem struct {
	ID                  int64                          `json:"id"`
	TenantID            int64                          `json:"tenant_id"`
	TaskType            string                         `json:"task_type"`
	Name                string                         `json:"name"`
	Description         string                         `json:"description,omitempty"`
	Status              string                         `json:"status"`
	LastRunAt           string                         `json:"last_run_at,omitempty"`
	LastExecutionID     string                         `json:"last_execution_id,omitempty"`
	LastExecutionStatus string                         `json:"last_execution_status,omitempty"`
	ExecutionContract   taskprovider.ExecutionContract `json:"execution_contract"`
}

type taskProviderTaskListResponse struct {
	Items    []taskProviderTaskListItem `json:"items"`
	Total    int                        `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"page_size"`
}

type qualityTaskProviderExecuteRequest struct {
	TriggerType       string                 `json:"trigger_type"`
	Source            string                 `json:"source"`
	ParentExecutionID string                 `json:"parent_execution_id"`
	Parameters        map[string]interface{} `json:"parameters"`
}

type qualityTaskProviderExecuteResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status" enums:"pending" example:"pending"`
}

// ListTasks 列出 Quality 检查任务。
// @Summary 列出 TaskProvider 质量检查任务 | List TaskProvider quality check tasks
// @Description 按标准 TaskProvider 协议列出 Quality 任务；task_type 支持 check|materialization_gate。| List Quality tasks through the standard TaskProvider protocol; task_type supports check or materialization_gate.
// @Tags CheckTask
// @Produce json
// @Param task_type query string false "任务类型：check|materialization_gate | Task type: check or materialization_gate"
// @Success 200 {object} taskProviderTaskListResponse "任务列表 | Task list"
// @Failure 400 {object} qualityErrorResponse "请求参数错误 | Bad request"
// @Failure 500 {object} qualityErrorResponse "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.task_provider.read"]
// @Router /tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTasks(c *gin.Context) {
	taskType := strings.TrimSpace(c.Query("task_type"))
	if taskType != "" && taskType != commonExecution.TaskTypeQualityCheck && taskType != commonExecution.TaskTypeMaterializationGate {
		respondInvalidRequest(c, "")
		return
	}
	page, pageSize := pageParams(c.Query("page"), c.Query("page_size"))
	if strings.TrimSpace(c.Query("page_size")) == "" {
		pageSize = 100
	}

	tenantID := getTenantID(c)
	items := make([]taskProviderTaskListItem, 0)
	var total int64
	if taskType == "" || taskType == commonExecution.TaskTypeQualityCheck {
		tasks, count, err := h.checkTaskSvc.List(tenantID, page, pageSize)
		if err != nil {
			respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
			return
		}
		total += count
		for _, task := range tasks {
			items = append(items, qualityTaskListItem(task))
		}
	}
	if (taskType == "" || taskType == commonExecution.TaskTypeMaterializationGate) && h.gateTaskSvc != nil {
		tasks, count, err := h.gateTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
		if err != nil {
			respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
			return
		}
		total += count
		for _, task := range tasks {
			items = append(items, qualityGateTaskListItem(task))
		}
	}
	c.JSON(http.StatusOK, taskProviderTaskListResponse{
		Items:    items,
		Total:    int(total),
		Page:     page,
		PageSize: pageSize,
	})
}

// TaskDetail 获取 Quality 检查任务详情。
// @Summary 获取 TaskProvider 质量检查任务详情 | Get TaskProvider quality check task detail
// @Description 按标准 TaskProvider 协议获取 Quality 任务详情；task_type 支持 check|materialization_gate。| Get Quality task detail through the standard TaskProvider protocol; task_type supports check or materialization_gate.
// @Tags CheckTask
// @Produce json
// @Param task_type path string true "任务类型：check|materialization_gate | Task type: check or materialization_gate"
// @Param id path int true "检查任务ID | Check task ID"
// @Success 200 {object} taskProviderTaskListItem "任务详情 | Task detail"
// @Failure 400 {object} qualityErrorResponse "请求参数错误 | Bad request"
// @Failure 404 {object} qualityErrorResponse "任务不存在 | Task not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.task_provider.read"]
// @Router /tasks/{task_type}/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) TaskDetail(c *gin.Context) {
	taskType := c.Param("task_type")
	if taskType != commonExecution.TaskTypeQualityCheck && taskType != commonExecution.TaskTypeMaterializationGate {
		respondInvalidRequest(c, "")
		return
	}

	taskID, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}

	if taskType == commonExecution.TaskTypeMaterializationGate {
		if h.gateTaskSvc == nil {
			respondInvalidRequest(c, "")
			return
		}
		task, err := h.gateTaskSvc.Get(c.Request.Context(), getTenantID(c), taskID)
		if err != nil {
			respondQualityServiceError(c, err, qualityi18n.MsgCheckTaskNotFound, qualityi18n.MsgInternal)
			return
		}
		c.JSON(http.StatusOK, qualityGateTaskListItem(*task))
		return
	}
	task, err := h.checkTaskSvc.Get(taskID, getTenantID(c))
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgCheckTaskNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, qualityTaskListItem(*task))
}

// TaskExecute 执行 Quality 检查任务。
// @Summary 执行 TaskProvider 质量检查任务 | Execute TaskProvider quality check task
// @Description 按标准 TaskProvider 协议执行 Quality 任务；materialization_gate 仅允许 Orchestrator 且 parameters 不支持覆盖。| Execute a Quality task through the standard TaskProvider protocol; materialization_gate only accepts Orchestrator and parameters overrides are not supported.
// @Tags CheckTask
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型：check|materialization_gate | Task type: check or materialization_gate"
// @Param id path int true "检查任务ID | Check task ID"
// @Param request body qualityTaskProviderExecuteRequest false "TaskProvider 执行请求 | TaskProvider execution request"
// @Success 202 {object} qualityTaskProviderExecuteResponse "执行ID | Execution ID"
// @Failure 400 {object} qualityErrorResponse "请求参数错误 | Bad request"
// @Failure 404 {object} qualityErrorResponse "任务不存在 | Task not found"
// @Failure 409 {object} qualityErrorResponse "任务已有活动 execution | Task already has an active execution"
// @Failure 500 {object} qualityErrorResponse "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.task_provider.execute"]
// @Router /tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *TaskProviderHandler) TaskExecute(c *gin.Context) {
	taskType := c.Param("task_type")
	if taskType != commonExecution.TaskTypeQualityCheck && taskType != commonExecution.TaskTypeMaterializationGate {
		respondInvalidRequest(c, "")
		return
	}

	taskID, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}

	var req qualityTaskProviderExecuteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	if len(req.Parameters) > 0 {
		respondInvalidRequest(c, "")
		return
	}

	triggerType, err := commonExecution.NormalizeTriggerType(req.TriggerType)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = commonExecution.ModuleQuality
	}
	var parentExecutionID *string
	if strings.TrimSpace(req.ParentExecutionID) != "" {
		parentExecutionID = &req.ParentExecutionID
	}

	var executionID string
	if taskType == commonExecution.TaskTypeMaterializationGate {
		if h.gateTaskSvc == nil {
			respondInvalidRequest(c, "")
			return
		}
		if parentExecutionID == nil {
			respondInvalidRequest(c, "")
			return
		}
		executionID, err = h.gateTaskSvc.Execute(c.Request.Context(), getTenantID(c), taskID, triggerType, source, *parentExecutionID)
	} else {
		executionID, err = h.executor.RunCheckWithContext(c.Request.Context(), taskID, getTenantID(c), getUserID(c), bearerToken(c), triggerType, source, parentExecutionID)
	}
	if err != nil {
		respondCheckRunError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, qualityTaskProviderExecuteResponse{
		ExecutionID: executionID,
		Status:      commonExecution.ExecutionStatusPending,
	})
}

func qualityGateTaskListItem(task models.MaterializationGateTask) taskProviderTaskListItem {
	item := taskProviderTaskListItem{
		ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeMaterializationGate,
		Name: task.Name, Description: task.Description, Status: qualityGateTaskStatus(task),
		ExecutionContract: materializationGateExecutionContract(),
		LastExecutionID:   task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
	}
	if task.LastRunAt != nil {
		item.LastRunAt = task.LastRunAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return item
}

func materializationGateExecutionContract() taskprovider.ExecutionContract {
	return taskprovider.ExecutionContract{
		InputSchema: taskprovider.ClosedObjectSchema(), InputDefaults: map[string]interface{}{}, InputUISchema: map[string]interface{}{},
		OutputSchema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{
				"materialization_group_id":      map[string]interface{}{"type": "integer", "minimum": float64(1)},
				"materialization_group_version": map[string]interface{}{"type": "integer", "minimum": float64(1)},
			}, "required": []interface{}{"materialization_group_id", "materialization_group_version"}, "additionalProperties": false,
		},
	}
}

func qualityGateTaskStatus(task models.MaterializationGateTask) string {
	switch task.LastExecutionStatus {
	case commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning:
		return task.LastExecutionStatus
	default:
		return "idle"
	}
}

func qualityTaskListItem(task models.CheckTask) taskProviderTaskListItem {
	item := taskProviderTaskListItem{
		ID:                task.ID,
		TenantID:          task.TenantID,
		TaskType:          commonExecution.TaskTypeQualityCheck,
		Name:              task.Name,
		Description:       task.Description,
		Status:            qualityTaskStatus(task),
		ExecutionContract: taskprovider.EmptyExecutionContract(),
	}
	if task.LastRunAt != nil {
		item.LastRunAt = task.LastRunAt.Format("2006-01-02T15:04:05Z07:00")
	}
	item.LastExecutionID = task.LastExecutionID
	item.LastExecutionStatus = task.LastExecutionStatus
	return item
}

func qualityTaskStatus(task models.CheckTask) string {
	switch task.LastExecutionStatus {
	case commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning:
		return task.LastExecutionStatus
	default:
		return "idle"
	}
}
