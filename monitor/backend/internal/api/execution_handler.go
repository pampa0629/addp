package api

import (
	"net/http"
	"strconv"

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
func (h *ExecutionHandler) ListExecutions(c *gin.Context) {
	// 从 context 获取 tenant_id（由认证中间件注入）
	tenantIDRaw, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id not found"})
		return
	}

	// 转换类型：中间件设置的是 uint，需要转换为 int
	tenantID := int(tenantIDRaw.(uint))

	// 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	req := &service.ListExecutionsRequest{
		TenantID:      tenantID,
		Module:        c.Query("module"),
		TaskType:      c.Query("task_type"),
		Status:        c.Query("status"),
		TriggerType:   c.Query("trigger_type"),
		Page:          page,
		PageSize:      pageSize,
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
func (h *ExecutionHandler) GetExecution(c *gin.Context) {
	// 从 context 获取 tenant_id
	tenantIDRaw, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id not found"})
		return
	}

	// 转换类型：中间件设置的是 uint，需要转换为 int
	tenantID := int(tenantIDRaw.(uint))

	// 解析 ID
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid execution id"})
		return
	}

	// 查询执行记录
	execution, err := h.queryService.GetExecution(c.Request.Context(), id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution not found"})
		return
	}

	c.JSON(http.StatusOK, execution)
}
