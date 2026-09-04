package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonAuthorization "github.com/addp/common/authorization"
	commonAuth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/taskprovider"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

// ExecutionHandler 执行记录管理 API Handler
type ExecutionHandler struct {
	executionService *service.ExecutionService
	taskService      *service.TaskService
}

// NewExecutionHandler 创建 ExecutionHandler
func NewExecutionHandler(executionService *service.ExecutionService, taskService *service.TaskService) *ExecutionHandler {
	return &ExecutionHandler{
		executionService: executionService,
		taskService:      taskService,
	}
}

// CreateExecution 创建一次性 bounded sync execution。
// @Summary 创建一次性同步执行 | Create one-off sync execution
// @Description 模块 Service Client 创建不带 Transfer 任务定义的一次性 bounded sync execution；调用来源由认证 Client ID 推导，响应只返回统一 execution_id。| A module Service Client creates a one-off bounded sync execution without a Transfer task definition; the source module is derived from the authenticated Client ID and the response contains only the unified execution_id.
// @Tags 执行管理 | Execution Management
// @Accept json
// @Produce json
// @Param request body models.CreateAdHocExecutionRequest true "一次性同步执行 | One-off sync execution"
// @Success 202 {object} models.CreateAdHocExecutionResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.execution.create"]
// @Router /executions [post]
// @Security BearerAuth
func (h *ExecutionHandler) CreateExecution(c *gin.Context) {
	if h == nil || h.taskService == nil {
		commonAPI.InternalServerError(c, "transfer execution service is unavailable")
		return
	}
	var req models.CreateAdHocExecutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	sourceModule, ok := transferExecutionSourceModule(c)
	if !ok {
		commonAPI.ForbiddenError(c, "one-off transfer execution requires a module service client")
		return
	}
	result, err := h.taskService.CreateAdHocExecution(
		c.Request.Context(), &req, sourceModule, commonAuth.GetTenantID(c), commonAuth.GetUserID(c),
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidTaskConfig) {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	c.JSON(http.StatusAccepted, result)
}

func transferExecutionSourceModule(c *gin.Context) (string, bool) {
	authContext, exists := commonAuth.AuthContextFromGin(c)
	clientID := strings.TrimSpace(commonAuth.GetClientID(c))
	if !exists || authContext.Principal.Type != "service_principal" || !strings.HasPrefix(clientID, "addp-") {
		return "", false
	}
	module := strings.TrimPrefix(clientID, "addp-")
	if commonAuthorization.ValidateOwnerModuleName(module) != nil {
		return "", false
	}
	return module, true
}

// GetExecution 获取执行记录详情
// @Summary 获取执行详情 | Get execution detail
// @Tags 执行管理 | Execution Management
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} models.TaskExecution
// @Failure 404 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetExecution(c *gin.Context) {
	h.getExecution(c)
}

// GetOwnedExecutionResult 获取当前模块自己创建的一次性执行结果。
// @Summary 获取模块一次性执行结果 | Get module-owned one-off execution result
// @Description 仅返回 source 与当前模块 Service Client 一致、且没有 Transfer 任务定义的一次性 execution。| Returns only a one-off execution whose source matches the authenticated module Service Client and which has no Transfer task definition.
// @Tags 执行管理 | Execution Management
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} models.TaskExecution
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.execution.read"]
// @Router /executions/{execution_id}/result [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetOwnedExecutionResult(c *gin.Context) {
	if h == nil || h.executionService == nil {
		commonAPI.InternalServerError(c, "transfer execution service is unavailable")
		return
	}
	sourceModule, ok := transferExecutionSourceModule(c)
	if !ok {
		commonAPI.ForbiddenError(c, "one-off transfer execution result requires a module service client")
		return
	}
	execution, err := h.executionService.GetOwnedExecutionByExecutionID(
		c.Request.Context(), c.Param("execution_id"), commonAuth.GetTenantID(c), sourceModule,
	)
	if err != nil {
		commonAPI.NotFoundError(c, "Execution not found")
		return
	}
	c.JSON(http.StatusOK, execution)
}

// ProviderGetExecution 获取 TaskProvider 执行状态。
// @Summary 获取 TaskProvider 执行状态 | Get TaskProvider execution status
// @Tags TaskProvider
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} taskprovider.ExecutionStatusResponse
// @Failure 404 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task_provider.read"]
// @Router /task-provider/executions/{execution_id} [get]
// @Security BearerAuth
func (h *ExecutionHandler) ProviderGetExecution(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)
	executionID := c.Param("execution_id")

	execution, err := h.executionService.GetTaskProviderExecutionByExecutionID(c.Request.Context(), executionID, tenantID)
	if err != nil {
		commonAPI.NotFoundError(c, "Execution not found")
		return
	}

	c.JSON(http.StatusOK, taskprovider.NewExecutionStatusResponse(execution))
}

func (h *ExecutionHandler) getExecution(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)
	executionID := c.Param("execution_id")

	execution, err := h.executionService.GetExecutionByExecutionID(c.Request.Context(), executionID, tenantID)
	if err != nil {
		commonAPI.NotFoundError(c, "Execution not found")
		return
	}

	c.JSON(http.StatusOK, execution)
}

// ListExecutions 获取执行记录列表
// @Summary 获取执行列表 | List executions
// @Tags 执行管理 | Execution Management
// @Produce json
// @Param task_id query int false "任务ID | Task ID"
// @Param status query string false "执行状态 | Status"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /executions [get]
// @Security BearerAuth
func (h *ExecutionHandler) ListExecutions(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	// 分页参数
	page, pageSize := commonAPI.GetPaginationParams(c)

	// 如果指定了 task_id，调用单任务执行记录查询
	if taskIDStr := c.Query("task_id"); taskIDStr != "" {
		if taskID, err := strconv.ParseUint(taskIDStr, 10, 32); err == nil {
			executions, total, err := h.executionService.ListExecutions(c.Request.Context(), uint(taskID), tenantID, page, pageSize)
			if err != nil {
				commonAPI.InternalServerError(c, err.Error())
				return
			}

			commonAPI.SendPaginatedResponse(c, executions, total, page, pageSize)
			return
		}
	}

	// 全局执行列表（所有任务的执行记录）
	filters := make(map[string]interface{})
	if status := c.Query("status"); status != "" {
		filters["status"] = status
	}

	executions, total, err := h.executionService.ListAllExecutions(c.Request.Context(), tenantID, filters, page, pageSize)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	commonAPI.SendPaginatedResponse(c, executions, total, page, pageSize)
}

// GetTaskExecutions 获取指定任务的执行记录
// @Summary 获取任务执行列表 | List task executions
// @Tags 执行管理 | Execution Management
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /task-definitions/{id}/executions [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetTaskExecutions(c *gin.Context) {
	taskID, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	tenantID := commonAuth.GetTenantID(c)

	// 分页参数
	page, pageSize := commonAPI.GetPaginationParams(c)

	// 使用 ListExecutions 方法
	executions, total, err := h.executionService.ListExecutions(c.Request.Context(), taskID, tenantID, page, pageSize)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	commonAPI.SendPaginatedResponse(c, executions, total, page, pageSize)
}

// RetryExecution 重试失败的执行
// @Summary 重试执行 | Retry execution
// @Description 为允许重试的失败 bounded execution 创建 restartable retry；continuous execution 和 schema drift blocked CDC 不支持 retry。| Create a restartable retry for an eligible failed bounded execution. Continuous executions and schema-drift-blocked CDC do not support retry.
// @Tags 执行管理 | Execution Management
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} map[string]interface{}
// @Failure 409 {object} map[string]string "CDC 被结构变化阻塞 | CDC blocked by schema change"
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.execute"]
// @Router /executions/{execution_id}/retry [post]
// @Security BearerAuth
func (h *ExecutionHandler) RetryExecution(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)
	execution, ok := h.getExecutionByExecutionID(c, tenantID)
	if !ok {
		return
	}

	newExecution, err := h.executionService.RetryExecution(c.Request.Context(), execution.ID, tenantID, userID)
	if err != nil {
		if errors.Is(err, service.ErrCDCSchemaChangeBlocked) {
			c.JSON(http.StatusConflict, gin.H{"error": i18nmiddleware.T(c, "transfer.cdc.schema_change_blocked")})
			return
		}
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, newExecution)
}

// GetExecutionProgress 获取执行进度
// @Summary 获取执行进度 | Get execution progress
// @Tags 执行管理 | Execution Management
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /executions/{execution_id}/progress [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetExecutionProgress(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)
	execution, ok := h.getExecutionByExecutionID(c, tenantID)
	if !ok {
		return
	}

	progress, err := h.executionService.GetExecutionProgress(c.Request.Context(), execution.ID, tenantID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, progress)
}

// GetExecutionLogs 获取执行日志
// @Summary 获取执行日志 | Get execution logs
// @Tags 执行管理 | Execution Management
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Param limit query int false "最多返回行数 | Line limit"
// @Success 200 {array} string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /executions/{execution_id}/logs [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetExecutionLogs(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)
	execution, ok := h.getExecutionByExecutionID(c, tenantID)
	if !ok {
		return
	}

	// 获取完整日志字符串
	logs, err := h.executionService.GetExecutionLogs(c.Request.Context(), execution.ID, tenantID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	// 拆分为行，并根据可选的 limit 参数进行裁剪
	lines := []string{}
	if logs != "" {
		// 使用 \n 拆分，并去掉可能的末尾空行
		raw := strings.Split(logs, "\n")
		for _, line := range raw {
			if line != "" {
				lines = append(lines, line)
			}
		}
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && len(lines) > limit {
			lines = lines[len(lines)-limit:]
		}
	}

	// 返回数组，前端按行渲染
	c.JSON(http.StatusOK, lines)
}

func (h *ExecutionHandler) getExecutionByExecutionID(c *gin.Context, tenantID uint) (*models.TaskExecution, bool) {
	executionID := c.Param("execution_id")
	execution, err := h.executionService.GetExecutionByExecutionID(c.Request.Context(), executionID, tenantID)
	if err != nil {
		commonAPI.NotFoundError(c, "Execution not found")
		return nil, false
	}
	return execution, true
}

// GetExecutionStatistics 获取执行统计
// @Summary 获取执行统计 | Get execution statistics
// @Tags 执行管理 | Execution Management
// @Produce json
// @Param task_id query int false "任务ID | Task ID"
// @Param start_time query string false "开始时间 | Start time"
// @Param end_time query string false "结束时间 | End time"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /executions/statistics [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetExecutionStatistics(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	filters := make(map[string]interface{})
	if taskID := c.Query("task_id"); taskID != "" {
		if id, err := strconv.ParseUint(taskID, 10, 32); err == nil {
			filters["task_id"] = uint(id)
		}
	}
	if startTime := c.Query("start_time"); startTime != "" {
		filters["start_time"] = startTime
	}
	if endTime := c.Query("end_time"); endTime != "" {
		filters["end_time"] = endTime
	}

	stats, err := h.executionService.GetExecutionStatistics(c.Request.Context(), tenantID, filters)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, stats)
}
