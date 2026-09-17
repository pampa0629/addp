package api

import (
	"encoding/json"
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
	planSvc *service.PlanService
}

func NewTaskProviderHandler(planSvc *service.PlanService) *TaskProviderHandler {
	return &TaskProviderHandler{planSvc: planSvc}
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
	TriggerType       string                `json:"trigger_type"`
	Source            string                `json:"source"`
	ParentExecutionID string                `json:"parent_execution_id"`
	Parameters        models.PlanRunRequest `json:"parameters"`
}

type qualityTaskProviderExecuteResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status" enums:"pending" example:"pending"`
}

// ListTasks 列出 Quality 检查任务。
// @Summary 列出 TaskProvider 质量检查任务 | List TaskProvider quality check tasks
// @Description 按标准 TaskProvider 协议列出 Quality 任务；task_type 支持 quality_plan。| List Quality tasks through the standard TaskProvider protocol; task_type supports quality_plan.
// @Tags QualityPlan
// @Produce json
// @Param task_type query string false "任务类型：quality_plan | Task type: quality_plan"
// @Success 200 {object} taskProviderTaskListResponse "任务列表 | Task list"
// @Failure 400 {object} qualityErrorResponse "请求参数错误 | Bad request"
// @Failure 500 {object} qualityErrorResponse "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.task_provider.read"]
// @Router /task-provider/tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTasks(c *gin.Context) {
	taskType := strings.TrimSpace(c.Query("task_type"))
	if taskType != "" && taskType != commonExecution.TaskTypeQualityPlan {
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
	if (taskType == "" || taskType == commonExecution.TaskTypeQualityPlan) && h.planSvc != nil {
		tasks, count, err := h.planSvc.List(c.Request.Context(), tenantID, nil, page, pageSize)
		if err != nil {
			respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
			return
		}
		total += count
		for _, task := range tasks {
			items = append(items, qualityPlanListItem(task))
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
// @Description 按标准 TaskProvider 协议获取 Quality 任务详情；task_type 支持 quality_plan。| Get Quality task detail through the standard TaskProvider protocol; task_type supports quality_plan.
// @Tags QualityPlan
// @Produce json
// @Param task_type path string true "任务类型：quality_plan | Task type: quality_plan"
// @Param id path int true "检查任务ID | Check task ID"
// @Success 200 {object} taskProviderTaskListItem "任务详情 | Task detail"
// @Failure 400 {object} qualityErrorResponse "请求参数错误 | Bad request"
// @Failure 404 {object} qualityErrorResponse "任务不存在 | Task not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.task_provider.read"]
// @Router /task-provider/tasks/{task_type}/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) TaskDetail(c *gin.Context) {
	taskType := c.Param("task_type")
	if taskType != commonExecution.TaskTypeQualityPlan {
		respondInvalidRequest(c, "")
		return
	}

	taskID, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}

	task, err := h.planSvc.Get(c.Request.Context(), getTenantID(c), taskID)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgPlanNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, qualityPlanListItem(*task))
}

// TaskExecute 执行 Quality 检查任务。
// @Summary 执行 TaskProvider 质量检查任务 | Execute TaskProvider quality check task
// @Description 仅接受编排父执行血缘；parameters.table_bindings 按别名指定实际数据表，不覆盖规则。| Requires orchestrator parent lineage; parameters.table_bindings supplies actual tables by alias without overriding rules.
// @Tags QualityPlan
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型：quality_plan | Task type: quality_plan"
// @Param id path int true "检查任务ID | Check task ID"
// @Param request body qualityTaskProviderExecuteRequest true "TaskProvider 执行请求 | TaskProvider execution request"
// @Success 202 {object} qualityTaskProviderExecuteResponse "执行ID | Execution ID"
// @Failure 400 {object} qualityErrorResponse "请求参数错误 | Bad request"
// @Failure 404 {object} qualityErrorResponse "任务不存在 | Task not found"
// @Failure 409 {object} qualityErrorResponse "任务已有活动 execution | Task already has an active execution"
// @Failure 500 {object} qualityErrorResponse "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.task_provider.execute"]
// @Router /task-provider/tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *TaskProviderHandler) TaskExecute(c *gin.Context) {
	taskType := c.Param("task_type")
	if taskType != commonExecution.TaskTypeQualityPlan {
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

	triggerType, err := commonExecution.NormalizeTriggerType(req.TriggerType)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	parentID, err := commonExecution.NormalizeOrchestratorChildContext(req.Source, req.ParentExecutionID)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	executionID, err := h.planSvc.Execute(c.Request.Context(), getTenantID(c), taskID, triggerType, commonExecution.ModuleOrchestrator, parentID, req.Parameters)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgPlanNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusAccepted, qualityTaskProviderExecuteResponse{
		ExecutionID: executionID,
		Status:      commonExecution.ExecutionStatusPending,
	})
}

func qualityPlanListItem(task models.QualityPlan) taskProviderTaskListItem {
	item := taskProviderTaskListItem{
		ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeQualityPlan,
		Name: task.Name, Description: task.Description, Status: qualityPlanStatus(task),
		ExecutionContract: planExecutionContract(task),
		LastExecutionID:   task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
	}
	if task.LastRunAt != nil {
		item.LastRunAt = task.LastRunAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return item
}

func planExecutionContract(task models.QualityPlan) taskprovider.ExecutionContract {
	var bindings []models.PlanTableBinding
	_ = json.Unmarshal(task.TableBindings, &bindings)
	properties, defaults, fields := map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}
	for _, b := range bindings {
		properties[b.Alias] = map[string]interface{}{"type": "string", "format": "resource-locator", "minLength": float64(1)}
		fields[b.Alias] = map[string]interface{}{"control": "resource_tree_picker", "display_name": b.Alias, "engine_families": []string{"tabular"}, "selectable_node_types": []string{"table"}}
		if b.Locator != "" {
			defaults[b.Alias] = b.Locator
		}
	}
	required := make([]interface{}, 0, len(bindings))
	for _, b := range bindings {
		if b.Locator == "" {
			required = append(required, b.Alias)
		}
	}
	targetSchema := taskprovider.ClosedObjectSchema()
	targetSchema["properties"], targetSchema["required"] = properties, required
	inputSchema := taskprovider.ClosedObjectSchema()
	inputSchema["properties"] = map[string]interface{}{"table_bindings": targetSchema}
	if len(required) > 0 {
		inputSchema["required"] = []interface{}{"table_bindings"}
	}
	return taskprovider.ExecutionContract{
		InputSchema: inputSchema, InputDefaults: map[string]interface{}{"table_bindings": defaults}, InputUISchema: map[string]interface{}{"table_bindings": map[string]interface{}{"control": "group", "fields": fields}},
		OutputSchema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"passed": map[string]interface{}{"type": "boolean"}}, "required": []interface{}{"passed"}, "additionalProperties": false,
		},
	}
}

func qualityPlanStatus(task models.QualityPlan) string {
	switch task.LastExecutionStatus {
	case commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning:
		return task.LastExecutionStatus
	default:
		return "idle"
	}
}
