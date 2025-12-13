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

	// 提取 JWT token（去除 "Bearer " 前缀）
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少认证令牌"})
		return
	}

	// 解析 Bearer token
	token := authHeader
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}

	// 调用 Service 层方法（传递 token）
	if err := h.sqlService.TestConnectionWithToken(req.ResourceID, token); err != nil {
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

// ListResources 获取可用的数据库资源列表
// GET /api/develop/resources
func (h *SQLHandler) ListResources(c *gin.Context) {
	// 从上下文获取租户信息
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "租户信息缺失"})
		return
	}

	tenantIDUint, ok := tenantID.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "租户ID类型错误"})
		return
	}

	// 提取 JWT token（去除 "Bearer " 前缀）
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少认证令牌"})
		return
	}

	// 解析 Bearer token
	token := authHeader
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}

	// 调用 Service 层方法
	resources, err := h.sqlService.ListDatabaseResourcesWithToken(c.Request.Context(), tenantIDUint, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取资源列表失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, resources)
}
