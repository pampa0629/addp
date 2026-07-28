package api

import (
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	moni18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

// ExecutionHandler 执行记录 Handler
type ExecutionHandler struct {
	queryService *service.ExecutionQueryService
}

// NewExecutionHandler 创建 Handler
func NewExecutionHandler(queryService *service.ExecutionQueryService) *ExecutionHandler {
	return &ExecutionHandler{
		queryService: queryService,
	}
}

// ListExecutions 分页查询执行记录
// @Summary 查询执行记录列表 | List execution records
// @Tags Monitor
// @Produce json
// @Param module query string false "模块名 | Module"
// @Param task_type query string false "任务类型 | Task type"
// @Param source_task_id query string false "任务定义 ID，字符串软引用 | Source task ID as string soft reference"
// @Param source query string false "触发来源模块 | Source module"
// @Param status query string false "执行状态 | Status"
// @Param trigger_type query string false "触发类型 | Trigger type"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.execution.read"]
// @Router /executions [get]
// @Security BearerAuth
func (h *ExecutionHandler) ListExecutions(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	// 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	req := &service.ListExecutionsRequest{
		TenantID:     tenantID,
		Module:       c.Query("module"),
		TaskType:     c.Query("task_type"),
		SourceTaskID: stringPtrFromQuery(c.Query("source_task_id")),
		Source:       c.Query("source"),
		Status:       c.Query("status"),
		TriggerType:  c.Query("trigger_type"),
		Page:         page,
		PageSize:     pageSize,
	}

	// 查询执行记录
	resp, err := h.queryService.ListExecutions(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func stringPtrFromQuery(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// GetExecution 获取单条执行记录
// @Summary 获取执行记录详情 | Get execution record detail
// @Tags Monitor
// @Produce json
// @Param id path int true "执行ID | Execution ID"
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.execution.read"]
// @Router /executions/{id} [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetExecution(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	// 解析 ID
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidExecutionID)})
		return
	}

	// 查询执行记录
	execution, err := h.queryService.GetExecution(c.Request.Context(), id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgExecutionNotFound)})
		return
	}

	c.JSON(http.StatusOK, execution)
}

// GetExecutionByExecutionID 获取单条执行记录
// @Summary 按 execution_id 获取执行记录详情 | Get execution record detail by execution_id
// @Tags Monitor
// @Produce json
// @Param execution_id path string true "执行 UUID | Execution UUID"
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.execution.read"]
// @Router /executions/by-execution-id/{execution_id} [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetExecutionByExecutionID(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	executionID := c.Param("execution_id")
	if executionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidExecutionID)})
		return
	}

	execution, err := h.queryService.GetExecutionByExecutionID(c.Request.Context(), executionID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgExecutionNotFound)})
		return
	}

	c.JSON(http.StatusOK, execution)
}

// GetExecutionTree 获取执行记录树
// @Summary 获取执行记录树 | Get execution tree
// @Tags Monitor
// @Produce json
// @Param id path int true "执行ID | Execution ID"
// @Success 200 {object} service.ExecutionTreeNode
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.execution.read"]
// @Router /executions/{id}/tree [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetExecutionTree(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidExecutionID)})
		return
	}

	tree, err := h.queryService.GetExecutionTree(c.Request.Context(), id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgExecutionNotFound)})
		return
	}

	c.JSON(http.StatusOK, tree)
}

// GetExecutionTreeByExecutionID 获取执行记录树
// @Summary 按 execution_id 获取执行记录树 | Get execution tree by execution_id
// @Tags Monitor
// @Produce json
// @Param execution_id path string true "执行 UUID | Execution UUID"
// @Success 200 {object} service.ExecutionTreeNode
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.execution.read"]
// @Router /executions/by-execution-id/{execution_id}/tree [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetExecutionTreeByExecutionID(c *gin.Context) {
	tenantID, ok := requireTenantID(c)
	if !ok {
		return
	}

	executionID := c.Param("execution_id")
	if executionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, moni18n.MsgInvalidExecutionID)})
		return
	}

	tree, err := h.queryService.GetExecutionTreeByExecutionID(c.Request.Context(), executionID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, moni18n.MsgExecutionNotFound)})
		return
	}

	c.JSON(http.StatusOK, tree)
}
