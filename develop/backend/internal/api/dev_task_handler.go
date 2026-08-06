package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/taskprovider"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// DevTaskHandler 开发任务API处理器
type DevTaskHandler struct {
	devTaskService    *service.DevTaskService
	operatorDiscovery *service.OperatorDiscoveryService
}

// NewDevTaskHandler 创建开发任务处理器
func NewDevTaskHandler(devTaskService *service.DevTaskService, operatorDiscovery *service.OperatorDiscoveryService) *DevTaskHandler {
	return &DevTaskHandler{
		devTaskService:    devTaskService,
		operatorDiscovery: operatorDiscovery,
	}
}

// CreateDevTask 创建开发任务
// @Summary 创建开发任务 | Create development task
// @Tags DevTask
// @Accept json
// @Produce json
// @Param body body models.CreateDevTaskSwaggerRequest true "创建请求 | Create request"
// @Success 200 {object} models.DevTaskSwagger "已创建的开发任务 | Created development task"
// @Failure 400 {object} models.ErrorResponse "请求或查询参数定义无效 | Invalid request or query parameter definitions"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.create"]
// @Router /task-definitions [post]
func (h *DevTaskHandler) CreateDevTask(c *gin.Context) {
	var req models.CreateDevTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := tenantIDValue(c)
	userID := userIDValue(c)

	item, err := h.devTaskService.CreateDevTask(&req, tenantID, userID)
	if err != nil {
		var parameterDefinitionsError *service.QueryParameterDefinitionsError
		if errors.As(err, &parameterDefinitionsError) {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgQueryParameterDefinitionsInvalid), "error_code": "invalid_query_parameter_definitions"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// UpdateDevTask 更新开发任务
// @Summary 更新开发任务 | Update development task
// @Tags DevTask
// @Accept json
// @Produce json
// @Param id path int true "开发任务 ID | Development task ID"
// @Param body body models.UpdateDevTaskSwaggerRequest true "更新请求 | Update request"
// @Success 200 {object} models.DevTaskSwagger "已更新的开发任务 | Updated development task"
// @Failure 400 {object} models.ErrorResponse "请求或查询参数定义无效 | Invalid request or query parameter definitions"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.update"]
// @Router /task-definitions/{id} [put]
func (h *DevTaskHandler) UpdateDevTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var req models.UpdateDevTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := tenantIDValue(c)
	userID := userIDValue(c)

	item, err := h.devTaskService.UpdateDevTask(uint(id), &req, tenantID, userID)
	if err != nil {
		var parameterDefinitionsError *service.QueryParameterDefinitionsError
		if errors.As(err, &parameterDefinitionsError) {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgQueryParameterDefinitionsInvalid), "error_code": "invalid_query_parameter_definitions"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// GetDevTask 获取开发任务详情
// @Summary 获取开发任务详情 | Get development task details
// @Tags DevTask
// @Produce json
// @Param id path int true "开发任务ID | Development task ID"
// @Success 200 {object} models.DevTaskDetailSwagger "开发任务详情 | Development task details"
// @Failure 500 {object} models.ErrorResponse "执行契约不可用 | Execution contract unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /task-definitions/{id} [get]
func (h *DevTaskHandler) GetDevTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	tenantID := tenantIDValue(c)

	item, err := h.devTaskService.GetDevTask(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	contract, err := h.executionContract(c, item, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionContractUnavailable), "error_code": "execution_contract_unavailable"})
		return
	}
	item.ExecutionContract = contract

	c.JSON(http.StatusOK, item)
}

// ListWorkflowStorageEngineBindings 查询算子工作流存储引擎绑定。
// @Summary 查询工作流存储引擎绑定 | List workflow storage engine bindings
// @Description 返回任务 content 中标准 ResourceLocator 的当前 Engine 引用、可用状态和兼容候选；不改变工作流运行引擎绑定。| Return current Engine references, availability, and compatible candidates for standard ResourceLocators in task content without changing the workflow runtime binding.
// @Tags DevTask
// @Produce json
// @Param id path int true "开发任务 ID | Development task ID"
// @Success 200 {object} models.WorkflowStorageEngineBindingsSwaggerResponse "存储引擎绑定 | Storage engine bindings"
// @Failure 400 {object} models.ErrorResponse "参数错误 | Invalid request"
// @Failure 404 {object} models.ErrorResponse "任务不存在 | Task not found"
// @Failure 422 {object} models.ErrorResponse "任务不是工作流 | Task is not a workflow"
// @Failure 503 {object} models.ErrorResponse "System 引擎发现不可用 | System engine discovery unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /task-definitions/{id}/storage-engine-bindings [get]
func (h *DevTaskHandler) ListWorkflowStorageEngineBindings(c *gin.Context) {
	var uri struct {
		ID uint `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgInvalidTaskID)})
		return
	}

	result, err := h.devTaskService.ListWorkflowStorageEngineBindings(c.Request.Context(), uri.ID, tenantIDValue(c))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrDevTaskNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, developi18n.MsgTaskNotFound)})
		case errors.Is(err, service.ErrTaskNotWorkflow):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": commoni18n.T(c, developi18n.MsgTaskNotWorkflow)})
		case errors.Is(err, service.ErrStorageEngineDiscovery):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, developi18n.MsgStorageEngineDiscoveryFailed)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, developi18n.MsgStorageBindingListFailed)})
		}
		return
	}
	c.JSON(http.StatusOK, result)
}

// RebindWorkflowStorageEngine 显式替换算子工作流中的一个存储引擎绑定。
// @Summary 重绑定工作流存储引擎 | Rebind workflow storage engine
// @Description 原子替换任务 content 中指向旧 Engine 的全部标准 ResourceLocator，保留 path/type、清除旧 Meta ID，不改变 execution_config.engine_id。| Atomically replace all standard ResourceLocators in task content that reference the old Engine, preserve path/type, clear stale Meta IDs, and keep execution_config.engine_id unchanged.
// @Tags DevTask
// @Accept json
// @Produce json
// @Param id path int true "开发任务 ID | Development task ID"
// @Param source_engine_id path int true "原存储引擎 ID | Source storage engine ID"
// @Param body body models.RebindWorkflowStorageEngineSwaggerRequest true "目标存储引擎 | Target storage engine"
// @Success 200 {object} models.RebindWorkflowStorageEngineSwaggerResponse "重绑定结果 | Rebind result"
// @Failure 400 {object} models.ErrorResponse "参数错误 | Invalid request"
// @Failure 404 {object} models.ErrorResponse "任务或绑定不存在 | Task or binding not found"
// @Failure 409 {object} models.ErrorResponse "任务发生并发修改 | Task changed concurrently"
// @Failure 422 {object} models.ErrorResponse "任务类型、目标引擎状态或能力不合法 | Invalid task type, target engine state, or capability"
// @Failure 503 {object} models.ErrorResponse "System 引擎发现不可用 | System engine discovery unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.update"]
// @Router /task-definitions/{id}/storage-engine-bindings/{source_engine_id} [put]
func (h *DevTaskHandler) RebindWorkflowStorageEngine(c *gin.Context) {
	var uri struct {
		ID             uint `uri:"id" binding:"required"`
		SourceEngineID uint `uri:"source_engine_id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgInvalidStorageBinding)})
		return
	}
	var req models.RebindWorkflowStorageEngineRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TargetEngineID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgInvalidStorageBinding)})
		return
	}

	result, err := h.devTaskService.RebindWorkflowStorageEngine(
		c.Request.Context(), uri.ID, tenantIDValue(c), userIDValue(c), uri.SourceEngineID, req.TargetEngineID,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrDevTaskNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, developi18n.MsgTaskNotFound)})
		case errors.Is(err, service.ErrStorageBindingNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, developi18n.MsgStorageBindingNotFound)})
		case errors.Is(err, service.ErrStorageBindingConflict):
			c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, developi18n.MsgStorageBindingConflict)})
		case errors.Is(err, service.ErrTaskNotWorkflow):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": commoni18n.T(c, developi18n.MsgTaskNotWorkflow)})
		case errors.Is(err, service.ErrStorageEngineUnavailable):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": commoni18n.T(c, developi18n.MsgStorageEngineUnavailable)})
		case errors.Is(err, service.ErrStorageEngineIncompatible):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": commoni18n.T(c, developi18n.MsgStorageEngineIncompatible)})
		case errors.Is(err, service.ErrStorageEngineDiscovery):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, developi18n.MsgStorageEngineDiscoveryFailed)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, developi18n.MsgStorageBindingUpdateFailed)})
		}
		return
	}
	c.JSON(http.StatusOK, result)
}

// ProviderGetDevTask 按 TaskProvider 标准路径获取开发任务详情。
// @Summary 获取 TaskProvider 开发任务详情 | Get TaskProvider development task detail
// @Description 按标准 TaskProvider 路径获取开发任务详情；task_type 是对外任务类型契约，映射到 Develop 内部 dev_type。| Get development task detail through the standard TaskProvider path; task_type is the external task contract mapped to Develop internal dev_type.
// @Tags DevTask
// @Produce json
// @Param task_type path string true "TaskProvider 任务类型：query/workflow/script | TaskProvider task type: query/workflow/script"
// @Param id path int true "开发任务ID | Development task ID"
// @Success 200 {object} models.ProviderDevTaskSwagger "开发任务详情 | Development task detail"
// @Failure 400 {object} map[string]interface{} "参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task_provider.read"]
// @Router /task-provider/tasks/{task_type}/{id} [get]
func (h *DevTaskHandler) ProviderGetDevTask(c *gin.Context) {
	taskType := c.Param("task_type")
	if !isDevelopTaskType(taskType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported task_type: " + taskType})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	tenantID := tenantIDValue(c)
	item, err := h.devTaskService.GetDevTask(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if item.DevType != taskType {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found for task_type: " + taskType})
		return
	}

	contract, err := h.executionContract(c, item, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionContractUnavailable), "error_code": "execution_contract_unavailable"})
		return
	}
	c.JSON(http.StatusOK, models.NewProviderDevTask(*item, contract))
}

func (h *DevTaskHandler) executionContract(c *gin.Context, item *models.DevTask, tenantID uint) (*taskprovider.ExecutionContract, error) {
	empty := taskprovider.EmptyExecutionContract()
	if item == nil {
		return &empty, nil
	}
	if item.DevType == commonExecution.TaskTypeQuery {
		return service.BuildQueryExecutionContract(item.Content)
	}
	if item.DevType != commonExecution.TaskTypeWorkflow {
		return &empty, nil
	}
	if h.operatorDiscovery == nil {
		return nil, fmt.Errorf("operator discovery is unavailable")
	}
	engineID := item.GetEngineID()
	workflow, ok := item.Content["workflow_definition"].(map[string]interface{})
	if engineID == nil || !ok {
		return nil, fmt.Errorf("workflow execution contract is unavailable")
	}
	return h.operatorDiscovery.WorkflowExecutionContractForTenant(c.Request.Context(), *engineID, workflow, tenantID)
}

func isDevelopTaskType(taskType string) bool {
	switch taskType {
	case commonExecution.TaskTypeQuery, commonExecution.TaskTypeWorkflow, commonExecution.TaskTypeScript:
		return true
	default:
		return false
	}
}

// ListDevTasks 查询开发任务列表
// @Summary 查询开发任务列表 | List development tasks
// @Tags DevTask
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Param dev_type query string false "Develop 内部类型过滤：query/workflow/script | Develop internal type filter: query/workflow/script"
// @Param status query string false "状态过滤 | Filter by status"
// @Param engine_id query int false "资源ID过滤 | Filter by engine ID"
// @Param tag query string false "标签过滤 | Filter by tag"
// @Param keyword query string false "关键词搜索 | Keyword search"
// @Success 200 {object} models.ListDevTasksSwaggerResponse "开发任务列表 | Development task list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /task-definitions [get]
func (h *DevTaskHandler) ListDevTasks(c *gin.Context) {
	var req models.ListDevTasksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := tenantIDValue(c)

	items, total, err := h.devTaskService.ListDevTasks(&req, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 设置默认分页参数
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 20
	}

	c.JSON(http.StatusOK, models.ListDevTasksResponse{
		Items:    items,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
}

// DeleteDevTask 删除开发任务
// @Summary 删除开发任务 | Delete development task
// @Tags DevTask
// @Param id path int true "开发任务ID | Development task ID"
// @Success 200 {object} map[string]string "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.delete"]
// @Router /task-definitions/{id} [delete]
func (h *DevTaskHandler) DeleteDevTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	tenantID := tenantIDValue(c)

	if err := h.devTaskService.DeleteDevTask(uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, developi18n.MsgDeleteSuccess)})
}

// ExecuteDevTask 执行开发任务
// @Summary 执行开发任务 | Execute development task
// @Tags DevTask
// @Accept json
// @Produce json
// @Param id path int true "开发任务ID | Development task ID"
// @Param body body map[string]interface{} false "执行参数 | Execution parameters"
// @Success 200 {object} map[string]string "执行已启动 | Execution started"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute"]
// @Router /task-definitions/{id}/execute [post]
func (h *DevTaskHandler) ExecuteDevTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	// 实际执行逻辑在 execution_handler 中
	// 这里返回提示信息
	c.JSON(http.StatusOK, gin.H{
		"message": commoni18n.T(c, developi18n.MsgUseExecuteEndpoint),
		"task_id": id,
	})
}

// GetDevTaskStatistics 获取开发任务统计
// @Summary 获取开发任务统计 | Get development task statistics
// @Tags DevTask
// @Produce json
// @Success 200 {object} map[string]int64 "统计数据 | Statistics"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /task-definitions/statistics [get]
func (h *DevTaskHandler) GetDevTaskStatistics(c *gin.Context) {
	tenantID := tenantIDValue(c)

	stats, err := h.devTaskService.CountByType(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}
