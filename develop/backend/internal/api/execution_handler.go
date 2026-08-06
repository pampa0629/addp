package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	developi18n "github.com/addp/develop/backend/i18n"
	developauthorization "github.com/addp/develop/backend/internal/authorization"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// ExecutionHandler 执行记录API处理器
type ExecutionHandler struct {
	devExecutor     *service.DevExecutor
	approvalService *service.ToolApprovalService
}

type executeDevTaskRequest struct {
	Parameters map[string]interface{} `json:"parameters"`
}

// NewExecutionHandler 创建执行记录处理器
func NewExecutionHandler(devExecutor *service.DevExecutor, approvalService *service.ToolApprovalService) *ExecutionHandler {
	return &ExecutionHandler{
		devExecutor:     devExecutor,
		approvalService: approvalService,
	}
}

// ExecuteDevTask 执行开发任务（支持参数化）
// @Summary 执行开发任务 | Execute development task
// @Tags Execution
// @Accept json
// @Produce json
// @Param id path int true "开发任务 ID | Development task ID"
// @Param body body executeDevTaskRequest false "本次执行参数覆盖（可选）| Execution parameter overrides (optional)"
// @Success 202 {object} map[string]string "执行已启动 | Execution started"
// @Failure 400 {object} map[string]interface{} "请求或执行参数无效 | Invalid request or execution parameters"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute"]
// @x-addp-conditional-permissions ["develop.data_read.execute","develop.data_write.execute","develop.data_ddl.execute","develop.data_external_effect.execute","develop.notebook.execute"]
// @Router /task-definitions/{id}/execute [post]
func (h *ExecutionHandler) ExecuteDevTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgInvalidTaskID)})
		return
	}

	tenantID := tenantIDValue(c)
	userID := userIDValue(c)
	taskType, err := h.devExecutor.GetDevTaskType(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if !requireNotebookExecutionPermission(c, taskType) {
		return
	}
	userAccessToken, err := requestUserAccessToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, developi18n.MsgAuthenticationRequired), "error_code": "authentication_required"})
		return
	}

	var req executeDevTaskRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionParametersInvalid), "error_code": "invalid_execution_parameters"})
		return
	}
	executionID, err := h.devExecutor.ExecuteWithParams(
		c.Request.Context(),
		uint(id),
		req.Parameters,
		tenantID,
		userID,
		userAccessToken,
	)

	if err != nil {
		if writeExecutionAuthorizationError(c, err) {
			return
		}
		var parameterDefinitionsError *service.QueryParameterDefinitionsError
		if errors.As(err, &parameterDefinitionsError) {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgQueryParameterDefinitionsInvalid), "error_code": "invalid_query_parameter_definitions"})
			return
		}
		var parameterError *service.ExecutionParametersError
		if errors.As(err, &parameterError) {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionParametersInvalid), "error_code": "invalid_execution_parameters"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionStartFailed), "error_code": "execution_start_failed"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":      commoni18n.T(c, developi18n.MsgExecutionStarted),
		"execution_id": executionID,
	})
}

// ExecuteContent 执行临时内容（不创建开发任务）
// @Summary 执行临时内容 | Execute temporary content
// @Description 第一方或 OAuth 用户提交 dev_type、trigger_type、content 和 execution_config 后直接执行。委托 workflow.run 首次提交相同执行内容并返回审批；批准后恢复请求只提交 approval_id 和 request_fingerprint。| First-party or OAuth users submit dev_type, trigger_type, content, and execution_config for direct execution. A delegated workflow.run first submits the same execution content and receives an approval; after approval, the resume request contains only approval_id and request_fingerprint.
// @Tags Execution
// @Accept json
// @Produce json
// @Param body body models.CreateExecutionSwaggerRequest true "执行请求 | Execution request"
// @Success 200 {object} models.ExecutionStartedResponse "执行已启动 | Execution started"
// @Success 202 {object} models.ApprovalRequiredResponse "需要在 Develop 完成审批 | Develop approval required"
// @Failure 400 {object} models.ToolApprovalErrorResponse "请求无效 | Invalid request"
// @Failure 403 {object} models.ToolApprovalErrorResponse "审批身份无效 | Invalid approval identity"
// @Failure 409 {object} models.ToolApprovalErrorResponse "审批状态冲突 | Approval state conflict"
// @Failure 410 {object} models.ToolApprovalErrorResponse "审批已过期 | Approval expired"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute"]
// @x-addp-conditional-permissions ["develop.data_read.execute","develop.data_write.execute","develop.data_ddl.execute","develop.data_external_effect.execute","develop.notebook.execute"]
// @Router /executions [post]
func (h *ExecutionHandler) ExecuteContent(c *gin.Context) {
	var req models.CreateExecutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	authContext, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		writeToolApprovalError(c, serviceError("approval_forbidden", "缺少认证上下文"))
		return
	}
	if authContext.Token.Type == "delegated_access_token" {
		if h.approvalService == nil {
			writeToolApprovalError(c, serviceError("approval_unavailable", "审批服务不可用"))
			return
		}
		if req.ApprovalID == "" {
			approval, err := h.approvalService.CreateWorkflowRunApproval(c.Request.Context(), authContext, req)
			if err != nil {
				writeToolApprovalError(c, err)
				return
			}
			c.JSON(http.StatusAccepted, models.ApprovalRequiredResponse{
				Status:             "approval_required",
				InteractionID:      approval.ID.String(),
				OpenURL:            "/develop/approvals/" + approval.ID.String(),
				RequestFingerprint: approval.RequestFingerprint,
				RequestSummary:     approval.RequestSummary,
				ExpiresAt:          approval.ExpiresAt,
			})
			return
		}
		if req.DevType != "" || req.TriggerType != "" || req.Content != nil || req.ExecutionConfig != nil || req.Parameters != nil || req.Timeout != 0 {
			writeToolApprovalError(c, serviceError("approval_invalid_request", "恢复 workflow.run 只允许 approval_id 和 request_fingerprint"))
			return
		}
		executionID, err := h.approvalService.ConsumeWorkflowRunApproval(
			c.Request.Context(),
			authContext,
			req.ApprovalID,
			req.RequestFingerprint,
		)
		if err != nil {
			writeToolApprovalError(c, err)
			return
		}
		c.JSON(http.StatusOK, models.ExecutionStartedResponse{
			Message:     commoni18n.T(c, developi18n.MsgExecutionStarted),
			ExecutionID: executionID,
		})
		return
	}
	if req.ApprovalID != "" || req.RequestFingerprint != "" {
		writeToolApprovalError(c, serviceError("approval_invalid_request", "approval_id 只用于受委托 workflow.run 恢复"))
		return
	}
	if req.DevType == "" || req.TriggerType == "" || req.ExecutionConfig == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dev_type、trigger_type 和 execution_config 为必填字段"})
		return
	}
	if !requireNotebookExecutionPermission(c, req.DevType) {
		return
	}

	tenantID := tenantIDValue(c)
	userID := userIDValue(c)
	userAccessToken, err := requestUserAccessToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, developi18n.MsgAuthenticationRequired), "error_code": "authentication_required"})
		return
	}

	executionID, err := h.devExecutor.ExecuteContent(
		c.Request.Context(),
		req.DevType,
		req.Content,
		req.ExecutionConfig,
		req.Parameters,
		tenantID,
		userID,
		userAccessToken,
		req.TriggerType,
		req.Timeout,
	)
	if err != nil {
		if writeExecutionAuthorizationError(c, err) {
			return
		}
		var parameterDefinitionsError *service.QueryParameterDefinitionsError
		if errors.As(err, &parameterDefinitionsError) {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgQueryParameterDefinitionsInvalid), "error_code": "invalid_query_parameter_definitions"})
			return
		}
		var parameterError *service.ExecutionParametersError
		if errors.As(err, &parameterError) {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionParametersInvalid), "error_code": "invalid_execution_parameters"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.ExecutionStartedResponse{
		Message:     commoni18n.T(c, developi18n.MsgExecutionStarted),
		ExecutionID: executionID,
	})
}

func serviceError(code, message string) error {
	return &service.ToolApprovalError{Code: code, Message: message}
}

func writeToolApprovalError(c *gin.Context, err error) {
	var approvalErr *service.ToolApprovalError
	if !errors.As(err, &approvalErr) {
		c.JSON(http.StatusInternalServerError, models.ToolApprovalErrorResponse{
			Error: models.ToolApprovalErrorBody{Code: "approval_internal_error", Message: err.Error()},
		})
		return
	}
	statusCode := http.StatusConflict
	switch approvalErr.Code {
	case "approval_invalid_request", "approval_invalid_decision":
		statusCode = http.StatusBadRequest
	case "approval_forbidden":
		statusCode = http.StatusForbidden
	case "approval_not_found":
		statusCode = http.StatusNotFound
	case "approval_expired":
		statusCode = http.StatusGone
	case "approval_unavailable":
		statusCode = http.StatusServiceUnavailable
	}
	message := approvalErr.Message
	if messageID := developi18n.ToolApprovalErrorMessageID(approvalErr.Code); messageID != "" {
		message = commoni18n.T(c, messageID)
	}
	c.JSON(statusCode, models.ToolApprovalErrorResponse{
		Error: models.ToolApprovalErrorBody{Code: approvalErr.Code, Message: message},
	})
}

// GetExecution 获取执行详情
// @Summary 获取执行详情 | Get execution details
// @Tags Execution
// @Produce json
// @Param execution_id path string true "执行ID（UUID）| Execution ID (UUID)"
// @Success 200 {object} models.ExecutionWithDevTaskSwagger "执行详情 | Execution details"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /executions/{execution_id} [get]
func (h *ExecutionHandler) GetExecution(c *gin.Context) {
	h.getExecution(c, tenantIDValue(c))
}

// ProviderGetExecution 返回 TaskProvider 内部调用的执行状态。
// @Summary 获取 TaskProvider 执行状态 | Get TaskProvider execution status
// @Tags Execution
// @Produce json
// @Param execution_id path string true "执行ID（UUID）| Execution ID (UUID)"
// @Success 200 {object} models.ExecutionWithDevTaskSwagger "执行详情 | Execution details"
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task_provider.read"]
// @Router /task-provider/executions/{execution_id} [get]
func (h *ExecutionHandler) ProviderGetExecution(c *gin.Context) {
	h.getExecution(c, tenantIDValue(c))
}

func (h *ExecutionHandler) getExecution(c *gin.Context, tenantID uint) {
	executionID := c.Param("execution_id")

	execution, err := h.devExecutor.GetExecution(executionID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, execution)
}

// ListExecutions 查询执行列表
// @Summary 查询执行列表 | List executions
// @Tags Execution
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Param source_task_id query string false "任务定义 ID 过滤 | Filter by source task ID"
// @Param dev_type query string false "Develop 内部类型过滤：query/workflow/script | Develop internal type filter: query/workflow/script"
// @Param status query string false "状态过滤 | Filter by status"
// @Param trigger_type query string false "触发类型过滤 | Filter by trigger type"
// @Param start_date query string false "开始日期 YYYY-MM-DD | Start date YYYY-MM-DD"
// @Param end_date query string false "结束日期 YYYY-MM-DD | End date YYYY-MM-DD"
// @Success 200 {object} models.ListExecutionsSwaggerResponse "执行列表 | Execution list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /executions [get]
func (h *ExecutionHandler) ListExecutions(c *gin.Context) {
	var req models.ListExecutionsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := tenantIDValue(c)

	executions, total, err := h.devExecutor.ListExecutions(&req, tenantID)
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

	c.JSON(http.StatusOK, models.ListExecutionsResponse{
		Executions: executions,
		Total:      total,
		Page:       req.Page,
		PageSize:   req.PageSize,
	})
}

func writeExecutionAuthorizationError(c *gin.Context, err error) bool {
	status, ok := commonClient.SystemAPIStatusCode(err)
	if !ok {
		return false
	}
	switch status {
	case http.StatusUnauthorized:
		c.JSON(status, gin.H{"error": commoni18n.T(c, developi18n.MsgAuthenticationRequired), "error_code": "authentication_required"})
	case http.StatusForbidden:
		c.JSON(status, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionEffectForbidden), "error_code": "execution_effect_permission_denied"})
	case http.StatusConflict:
		c.JSON(status, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionConflict), "error_code": "execution_authorization_conflict"})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionAuthorizationUnavailable), "error_code": "execution_authorization_unavailable"})
	}
	return true
}

// RetryExecution 重试执行
// @Summary 重试执行 | Retry execution
// @Tags Execution
// @Param execution_id path string true "执行ID（UUID）| Execution ID (UUID)"
// @Success 200 {object} map[string]string "重试已启动 | Retry started"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute"]
// @x-addp-conditional-permissions ["develop.data_read.execute","develop.data_write.execute","develop.data_ddl.execute","develop.data_external_effect.execute","develop.notebook.execute"]
// @Router /executions/{execution_id}/retry [post]
func (h *ExecutionHandler) RetryExecution(c *gin.Context) {
	executionID := c.Param("execution_id")
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)
	taskType, err := h.devExecutor.GetExecutionTaskType(executionID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if !requireNotebookExecutionPermission(c, taskType) {
		return
	}
	userAccessToken, err := requestUserAccessToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, developi18n.MsgAuthenticationRequired), "error_code": "authentication_required"})
		return
	}

	newExecutionID, err := h.devExecutor.RetryExecution(executionID, tenantID, userID, userAccessToken)
	if err != nil {
		if writeExecutionAuthorizationError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      commoni18n.T(c, developi18n.MsgRetryStarted),
		"execution_id": newExecutionID,
	})
}

func requireNotebookExecutionPermission(c *gin.Context, taskType string) bool {
	if taskType != commonExecution.TaskTypeScript {
		return true
	}
	if commonAuth.HasRolePermission(c, developauthorization.PermissionDevelopNotebookExecute) {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{
		"error":      commoni18n.T(c, developi18n.MsgNotebookExecutionForbidden),
		"error_code": "notebook_execution_permission_denied",
	})
	return false
}

// GetExecutionStatistics 获取执行统计
// @Summary 获取执行统计 | Get execution statistics
// @Tags Execution
// @Produce json
// @Param source_task_id query string false "任务定义 ID | Source task ID"
// @Param start_date query string false "开始日期 YYYY-MM-DD | Start date YYYY-MM-DD"
// @Param end_date query string false "结束日期 YYYY-MM-DD | End date YYYY-MM-DD"
// @Success 200 {object} models.ExecutionStatistics "执行统计 | Execution statistics"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /executions/statistics [get]
func (h *ExecutionHandler) GetExecutionStatistics(c *gin.Context) {
	tenantID := tenantIDValue(c)

	sourceTaskID := c.Query("source_task_id")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	stats, err := h.devExecutor.GetStatistics(tenantID, sourceTaskID, startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetExecutionLogs 获取执行日志（占位）
// @Summary 获取执行日志 | Get execution logs
// @Tags Execution
// @Produce json
// @Param execution_id path string true "执行ID（UUID）| Execution ID (UUID)"
// @Success 200 {object} map[string]interface{} "执行日志 | Execution logs"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /executions/{execution_id}/logs [get]
func (h *ExecutionHandler) GetExecutionLogs(c *gin.Context) {
	executionID := c.Param("execution_id")

	// 暂时返回占位响应
	c.JSON(http.StatusOK, gin.H{
		"execution_id": executionID,
		"logs":         []string{commoni18n.T(c, developi18n.MsgLogsNotReady)},
	})
}

type providerExecuteDevRequest struct {
	TriggerType       string                 `json:"trigger_type"`
	Source            string                 `json:"source"`
	ParentExecutionID string                 `json:"parent_execution_id"`
	Parameters        map[string]interface{} `json:"parameters"`
}

type providerExecuteDevResponse struct {
	Status      string `json:"status"`
	ExecutionID string `json:"execution_id"`
}

// ProviderExecuteDevTask 按 TaskProvider 标准协议执行开发任务。
// @Summary 执行 TaskProvider 开发任务 | Execute TaskProvider development task
// @Description 按标准 TaskProvider 协议执行开发任务；task_type 是对外任务类型契约，映射到 Develop 内部 dev_type，parameters 会传入本次执行。| Execute a development task through the standard TaskProvider protocol; task_type is the external task contract mapped to Develop internal dev_type, and parameters are passed to this execution.
// @Tags Execution
// @Accept json
// @Produce json
// @Param task_type path string true "TaskProvider 任务类型：query/workflow/script | TaskProvider task type: query/workflow/script"
// @Param id path int true "开发任务ID | Development task ID"
// @Param request body providerExecuteDevRequest false "TaskProvider 执行请求 | TaskProvider execution request"
// @Success 202 {object} providerExecuteDevResponse "执行已启动 | Execution started"
// @Failure 400 {object} map[string]interface{} "参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器错误 | Server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task_provider.execute"]
// @Router /task-provider/tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *ExecutionHandler) ProviderExecuteDevTask(c *gin.Context) {
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

	var req providerExecuteDevRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	triggerType, err := commonExecution.NormalizeTriggerType(req.TriggerType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	source := strings.TrimSpace(req.Source)
	if source != commonExecution.ModuleOrchestrator || strings.TrimSpace(req.ParentExecutionID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "orchestrator source and parent_execution_id are required"})
		return
	}

	tenantID := tenantIDValue(c)

	executionID, err := h.devExecutor.ExecuteWithParamsFromParentExecution(
		c.Request.Context(),
		uint(id),
		req.Parameters,
		tenantID,
		triggerType,
		source,
		req.ParentExecutionID,
		taskType,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, providerExecuteDevResponse{
		Status:      commonExecution.ExecutionStatusPending,
		ExecutionID: executionID,
	})
}
