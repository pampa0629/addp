package api

import (
	"net/http"
	"strconv"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	developi18n "github.com/addp/develop/backend/i18n"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
)

// DevExecutionHandler 执行记录API处理器
type DevExecutionHandler struct {
	devExecutor *service.DevExecutor
}

// NewDevExecutionHandler 创建执行记录处理器
func NewDevExecutionHandler(devExecutor *service.DevExecutor) *DevExecutionHandler {
	return &DevExecutionHandler{
		devExecutor: devExecutor,
	}
}

// ExecuteDevItem 执行开发项（支持参数化）
// @Summary 执行开发项 | Execute development item
// @Tags Execution
// @Accept json
// @Produce json
// @Param id path int true "开发项ID | Development item ID"
// @Param body body map[string]interface{} false "执行参数（可选）| Execution parameters (optional)"
// @Success 200 {object} map[string]string "执行已启动 | Execution started"
// @Router /items/{id}/execute [post]
func (h *DevExecutionHandler) ExecuteDevItem(c *gin.Context) {
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
		executionID, err = h.devExecutor.ExecuteDevItem(c.Request.Context(), uint(id), tenantID, userID, "manual")
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

// ExecuteContent 执行临时内容（不创建开发项）
// @Summary 执行临时内容 | Execute temporary content
// @Tags Execution
// @Accept json
// @Produce json
// @Param body body models.CreateExecutionRequest true "执行请求 | Execution request"
// @Success 200 {object} map[string]string "执行已启动 | Execution started"
// @Router /executions [post]
func (h *DevExecutionHandler) ExecuteContent(c *gin.Context) {
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
// @Success 200 {object} models.ExecutionWithDevItem "执行详情 | Execution details"
// @Router /executions/{id} [get]
func (h *DevExecutionHandler) GetExecution(c *gin.Context) {
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
// @Param dev_item_id query int false "开发项ID过滤 | Filter by development item ID"
// @Param dev_type query string false "类型过滤 | Filter by type"
// @Param status query string false "状态过滤 | Filter by status"
// @Param trigger_type query string false "触发类型过滤 | Filter by trigger type"
// @Param start_date query string false "开始日期 YYYY-MM-DD | Start date YYYY-MM-DD"
// @Param end_date query string false "结束日期 YYYY-MM-DD | End date YYYY-MM-DD"
// @Success 200 {object} models.ListExecutionsResponse "执行列表 | Execution list"
// @Router /executions [get]
func (h *DevExecutionHandler) ListExecutions(c *gin.Context) {
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

// CancelExecution 取消执行
// @Summary 取消执行 | Cancel execution
// @Tags Execution
// @Param id path string true "执行ID（UUID）| Execution ID (UUID)"
// @Success 200 {object} map[string]string "取消成功 | Cancelled successfully"
// @Router /executions/{id}/cancel [post]
func (h *DevExecutionHandler) CancelExecution(c *gin.Context) {
	executionID := c.Param("id")
	tenantID := c.GetUint("tenant_id")

	if err := h.devExecutor.CancelExecution(executionID, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, developi18n.MsgExecutionCancelled)})
}

// RetryExecution 重试执行
// @Summary 重试执行 | Retry execution
// @Tags Execution
// @Param id path string true "执行ID（UUID）| Execution ID (UUID)"
// @Success 200 {object} map[string]string "重试已启动 | Retry started"
// @Router /executions/{id}/retry [post]
func (h *DevExecutionHandler) RetryExecution(c *gin.Context) {
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
// @Param dev_item_id query int false "开发项ID | Development item ID"
// @Param start_date query string false "开始日期 YYYY-MM-DD | Start date YYYY-MM-DD"
// @Param end_date query string false "结束日期 YYYY-MM-DD | End date YYYY-MM-DD"
// @Success 200 {object} models.ExecutionStatistics "执行统计 | Execution statistics"
// @Router /executions/statistics [get]
func (h *DevExecutionHandler) GetExecutionStatistics(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 解析查询参数
	var devItemID *uint
	if idStr := c.Query("dev_item_id"); idStr != "" {
		id, err := strconv.ParseUint(idStr, 10, 32)
		if err == nil {
			uintID := uint(id)
			devItemID = &uintID
		}
	}

	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	stats, err := h.devExecutor.GetStatistics(tenantID, devItemID, startDate, endDate)
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
func (h *DevExecutionHandler) GetExecutionLogs(c *gin.Context) {
	executionID := c.Param("id")

	// 暂时返回占位响应
	c.JSON(http.StatusOK, gin.H{
		"execution_id": executionID,
		"logs":         []string{commoni18n.T(c, developi18n.MsgLogsNotReady)},
	})
}

// ExecuteWithParams 参数化执行开发项（供 Orchestrator 调用）
// @Summary 参数化执行开发项 | Execute development item with parameters
// @Tags Execution
// @Accept json
// @Produce json
// @Param id path int true "开发项ID | Development item ID"
// @Param body body map[string]interface{} true "执行参数 | Execution parameters"
// @Success 200 {object} map[string]string "执行已启动 | Execution started"
// @Router /items/{id}/execute-with-params [post]
func (h *DevExecutionHandler) ExecuteWithParams(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	// 解析参数
	var params map[string]interface{}
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parameters: " + err.Error()})
		return
	}

	// 参数化执行
	executionID, err := h.devExecutor.ExecuteWithParams(
		c.Request.Context(),
		uint(id),
		params,
		tenantID,
		userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"execution_id": executionID,
		"message":      commoni18n.T(c, developi18n.MsgParamExecStarted),
	})
}
