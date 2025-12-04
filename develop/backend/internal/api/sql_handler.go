package api

import (
	"net/http"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// SQLHandler SQL执行相关的HTTP处理器
type SQLHandler struct {
	sqlService *service.SQLExecutionService
}

// NewSQLHandler 创建SQL Handler
func NewSQLHandler(sqlService *service.SQLExecutionService) *SQLHandler {
	return &SQLHandler{
		sqlService: sqlService,
	}
}

// Execute 执行SQL
// POST /api/develop/execute
func (h *SQLHandler) Execute(c *gin.Context) {
	var req models.ExecutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	// 从上下文获取用户信息
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "租户信息缺失"})
		return
	}

	// 执行SQL
	result, err := h.sqlService.Execute(
		c.Request.Context(),
		userID.(uint),
		tenantID.(uint),
		req.ResourceID,
		req.SQL,
		req.Timeout,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// TestConnection 测试数据库连接
// GET /api/develop/test/:resource_id
func (h *SQLHandler) TestConnection(c *gin.Context) {
	var req struct {
		ResourceID uint `uri:"resource_id" binding:"required"`
	}

	if err := c.ShouldBindUri(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "资源ID无效"})
		return
	}

	if err := h.sqlService.TestConnection(req.ResourceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "连接测试成功",
	})
}

// Health 健康检查
// GET /api/develop/health
func (h *SQLHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "develop",
		"version": "0.1.0",
	})
}
