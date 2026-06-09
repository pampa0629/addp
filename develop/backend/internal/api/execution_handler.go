package api

import (
	"net/http"
	"strconv"
	"strings"

	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// ExecutionHandler 执行记录API处理器
type ExecutionHandler struct {
	devExecutor *service.DevExecutor
}

// NewExecutionHandler 创建执行记录处理器
func NewExecutionHandler(devExecutor *service.DevExecutor) *ExecutionHandler {
	return &ExecutionHandler{
		devExecutor: devExecutor,
	}
}

// ExecuteDevTask 执行开发任务（支持参数化）
// @Summary 执行开发任务 | Execute development task
// @Tags Execution
// @Accept json
// @Produce json
// @Param id path int true "开发任务 ID | Development task ID"
// @Param body body map[string]interface{} false "执行参数（可选）| Execution parameters (optional)"
// @Success 200 {object} map[string]string "执行已启动 | Execution started"
// @Router /task-definitions/{id}/execute [post]
func (h *ExecutionHandler) ExecuteDevTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	// 尝试解析参数（可选）
	var params map[string]interface{}
	_ = c.ShouldBindJSON(&params)

	var executionID string
	if params != nil && len(params) > 0 {
		// 参数化执行
		executionID, err = h.devExecutor.ExecuteWithParams(
			c.Request.Context(),
			uint(id),
			params,
			tenantID,
			userID,
		)
	} else {
		// 常规执行
		executionID, err = h.devExecutor.ExecuteDevTask(c.Request.Context(), uint(id), tenantID, userID, "manual")
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      commoni18n.T(c, developi18n.MsgExecutionStarted),
		"execution_id": executionID,
	})
}

// ExecuteContent 执行临时内容（不创建开发任务）
// @Summary 执行临时内容 | Execute temporary content
// @Tags Execution
// @Accept json
// @Produce json
// @Param body body models.CreateExecutionRequest true "执行请求 | Execution request"
// @Success 200 {object} map[string]string "执行已启动 | Execution started"
// @Router /executions [post]
func (h *ExecutionHandler) ExecuteContent(c *gin.Context) {
	var req models.CreateExecutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	// 执行临时内容
	timeout := 300 // 默认5分钟
	executionID, err := h.devExecutor.ExecuteContent(
		c.Request.Context(),
		req.DevType,
		req.Content,
		req.EngineID,
		tenantID,
		userID,
		timeout,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      commoni18n.T(c, developi18n.MsgExecutionStarted),
		"execution_id": executionID,
	})
}

// GetExecution 获取执行详情
// @Summary 获取执行详情 | Get execution details
// @Tags Execution
// @Produce json
// @Param id path string true "执行ID（UUID）| Execution ID (UUID)"
// @Success 200 {object} models.ExecutionWithDevTask "执行详情 | Execution details"
// @Router /executions/{id} [get]
func (h *ExecutionHandler) GetExecution(c *gin.Context) {
	executionID := c.Param("id")
	tenantID := c.GetUint("tenant_id")

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
// @Param dev_type query string false "类型过滤 | Filter by type"
// @Param status query string false "状态过滤 | Filter by status"
// @Param trigger_type query string false "触发类型过滤 | Filter by trigger type"
// @Param start_date query string false "开始日期 YYYY-MM-DD | Start date YYYY-MM-DD"
// @Param end_date query string false "结束日期 YYYY-MM-DD | End date YYYY-MM-DD"
// @Success 200 {object} models.ListExecutionsResponse "执行列表 | Execution list"
// @Router /executions [get]
func (h *ExecutionHandler) ListExecutions(c *gin.Context) {
	var req models.ListExecutionsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")

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

// RetryExecution 重试执行
// @Summary 重试执行 | Retry execution
// @Tags Execution
// @Param id path string true "执行ID（UUID）| Execution ID (UUID)"
// @Success 200 {object} map[string]string "重试已启动 | Retry started"
// @Router /executions/{id}/retry [post]
func (h *ExecutionHandler) RetryExecution(c *gin.Context) {
	executionID := c.Param("id")
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	newExecutionID, err := h.devExecutor.RetryExecution(executionID, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      commoni18n.T(c, developi18n.MsgRetryStarted),
		"execution_id": newExecutionID,
	})
}

// GetExecutionStatistics 获取执行统计
// @Summary 获取执行统计 | Get execution statistics
// @Tags Execution
// @Produce json
// @Param source_task_id query string false "任务定义 ID | Source task ID"
// @Param start_date query string false "开始日期 YYYY-MM-DD | Start date YYYY-MM-DD"
// @Param end_date query string false "结束日期 YYYY-MM-DD | End date YYYY-MM-DD"
// @Success 200 {object} models.ExecutionStatistics "执行统计 | Execution statistics"
// @Router /executions/statistics [get]
func (h *ExecutionHandler) GetExecutionStatistics(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

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
// @Param id path string true "执行ID（UUID）| Execution ID (UUID)"
// @Success 200 {object} map[string]interface{} "执行日志 | Execution logs"
// @Router /executions/{id}/logs [get]
func (h *ExecutionHandler) GetExecutionLogs(c *gin.Context) {
	executionID := c.Param("id")

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

// ProviderExecuteDevTask 按 TaskProvider 标准协议执行开发任务。
// @Summary 执行 TaskProvider 开发任务 | Execute TaskProvider development task
// @Description 按标准 TaskProvider 协议执行开发任务；task_type 支持 query/workflow/script，parameters 会传入本次执行。| Execute a development task through the standard TaskProvider protocol; task_type supports query/workflow/script and parameters are passed to this execution.
// @Tags Execution
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型：query/workflow/script | Task type: query/workflow/script"
// @Param id path int true "开发任务ID | Development task ID"
// @Param request body providerExecuteDevRequest false "TaskProvider 执行请求 | TaskProvider execution request"
// @Success 200 {object} map[string]string "执行已启动 | Execution started"
// @Failure 400 {object} map[string]interface{} "参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器错误 | Server error"
// @Router /tasks/{task_type}/{id}/execute [post]
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
	_ = c.ShouldBindJSON(&req)
	triggerType, err := commonExecution.NormalizeTriggerType(req.TriggerType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = commonExecution.ModuleDevelop
	}
	var parentExecutionID *string
	if strings.TrimSpace(req.ParentExecutionID) != "" {
		parentExecutionID = &req.ParentExecutionID
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	executionID, err := h.devExecutor.ExecuteWithParamsWithContext(
		c.Request.Context(),
		uint(id),
		req.Parameters,
		tenantID,
		userID,
		triggerType,
		source,
		parentExecutionID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "success",
		"execution_id": executionID,
		"message":      commoni18n.T(c, developi18n.MsgParamExecStarted),
	})
}
