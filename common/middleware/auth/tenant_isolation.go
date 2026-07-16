package auth

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Context key for tenant isolation (ContextTenantIDKey is defined in middleware.go)
const (
	ContextTenantIsolationEnabledKey = "tenant_isolation_enabled"
)

// TenantIsolationMiddleware 租户隔离中间件
// 必须在 SystemAuthMiddleware 之后使用，依赖其注入的 tenant_id
func TenantIsolationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 Context 读取 tenant_id（由 SystemAuthMiddleware 或其他认证中间件注入）
		tenantIDRaw, exists := c.Get(ContextTenantIDKey)
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant information missing"})
			c.Abort()
			return
		}

		tenantID, ok := tenantIDRaw.(uint)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid tenant ID format"})
			c.Abort()
			return
		}

		// 超级管理员（tenant_id = 0）不需要强制隔离
		// 可以通过查询参数 ?tenant_id=X 访问特定租户数据
		if tenantID == 0 {
			c.Set(ContextTenantIsolationEnabledKey, false)
		} else {
			c.Set(ContextTenantIsolationEnabledKey, true)
		}

		c.Next()
	}
}

// GetTenantID 从 Gin Context 中获取租户 ID
func GetTenantID(c *gin.Context) uint {
	tenantIDRaw, exists := c.Get(ContextTenantIDKey)
	if !exists {
		return 0
	}

	tenantID, ok := tenantIDRaw.(uint)
	if !ok {
		return 0
	}

	return tenantID
}

// GetUserID 从 Gin Context 中获取用户 ID
func GetUserID(c *gin.Context) uint {
	userIDRaw, exists := c.Get(ContextUserIDKey)
	if !exists {
		return 0
	}

	userID, ok := userIDRaw.(uint)
	if !ok {
		return 0
	}

	return userID
}

// GetUsername 从 Gin Context 中获取用户名
func GetUsername(c *gin.Context) string {
	usernameRaw, exists := c.Get(ContextUsernameKey)
	if !exists {
		return ""
	}

	username, ok := usernameRaw.(string)
	if !ok {
		return ""
	}

	return username
}

// GetAuthorizationContext returns the canonical context produced by System.
func GetAuthorizationContext(c *gin.Context) (AuthorizationContext, bool) {
	value, exists := c.Get(ContextAuthorizationContextKey)
	if !exists {
		return AuthorizationContext{}, false
	}
	context, ok := value.(AuthorizationContext)
	return context, ok
}

func GetUserType(c *gin.Context) string {
	context, ok := GetAuthorizationContext(c)
	if !ok {
		return ""
	}
	return context.UserType
}

func GetClientID(c *gin.Context) string {
	context, ok := GetAuthorizationContext(c)
	if !ok || context.ClientID == nil {
		return ""
	}
	return *context.ClientID
}

func GetScopes(c *gin.Context) []string {
	context, ok := GetAuthorizationContext(c)
	if !ok {
		return nil
	}
	return append([]string(nil), context.Scopes...)
}

func GetAudiences(c *gin.Context) []string {
	context, ok := GetAuthorizationContext(c)
	if !ok {
		return nil
	}
	return append([]string(nil), context.Audiences...)
}

// IsTenantIsolationEnabled 检查是否启用租户隔离
func IsTenantIsolationEnabled(c *gin.Context) bool {
	enabled, exists := c.Get(ContextTenantIsolationEnabledKey)
	if !exists {
		return true // 默认启用隔离
	}

	result, ok := enabled.(bool)
	if !ok {
		return true
	}

	return result
}

// GetTenantFilterWithQuery 获取租户过滤 ID，支持超级管理员通过查询参数指定租户
// 普通用户强制使用 JWT 中的 tenant_id
func GetTenantFilterWithQuery(c *gin.Context) uint {
	tenantID := GetTenantID(c)

	// 超级管理员可通过查询参数指定租户
	if tenantID == 0 {
		if queryTenantID := c.Query("tenant_id"); queryTenantID != "" {
			// 尝试解析查询参数
			var parsedID uint
			if _, err := fmt.Sscanf(queryTenantID, "%d", &parsedID); err == nil {
				return parsedID
			}
		}
		return 0 // 不指定时返回 0（查询所有数据）
	}

	// 普通用户强制使用 JWT 中的 tenant_id
	return tenantID
}
