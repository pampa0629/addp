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
// @Param source query string false "触发来源模块 | Source module"
// @Param status query string false "执行状态 | Status"
// @Param trigger_type query string false "触发类型 | Trigger type"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} map[string]interface{}
// @Router /executions [get]
// @Security BearerAuth
func (h *ExecutionHandler) ListExecutions(c *gin.Context) {
	// 从 context 获取 tenant_id（由认证中间件注入）
	tenantIDRaw, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, moni18n.MsgTenantNotFound)})
		return
	}

	// 转换类型：中间件设置的是 uint，需要转换为 int
	tenantID := int(tenantIDRaw.(uint))

	// 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	req := &service.ListExecutionsRequest{
		TenantID:    tenantID,
		Module:      c.Query("module"),
		TaskType:    c.Query("task_type"),
		Source:      c.Query("source"),
		Status:      c.Query("status"),
		TriggerType: c.Query("trigger_type"),
		Page:        page,
		PageSize:    pageSize,
	}

	// 查询执行记录
	resp, err := h.queryService.ListExecutions(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetExecution 获取单条执行记录
// @Summary 获取执行记录详情 | Get execution record detail
// @Tags Monitor
// @Produce json
// @Param id path int true "执行ID | Execution ID"
// @Success 200 {object} map[string]interface{}
// @Router /executions/{id} [get]
// @Security BearerAuth
func (h *ExecutionHandler) GetExecution(c *gin.Context) {
	// 从 context 获取 tenant_id
	tenantIDRaw, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, moni18n.MsgTenantNotFound)})
		return
	}

	// 转换类型：中间件设置的是 uint，需要转换为 int
	tenantID := int(tenantIDRaw.(uint))

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
