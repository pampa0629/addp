package middleware

import (
	"net/http"
	"strings"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/pkg/utils"
	"github.com/gin-gonic/gin"
)

// AuthMiddleware JWT 认证中间件（用于用户请求）
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少认证令牌"})
			c.Abort()
			return
		}

		// Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "认证令牌格式错误"})
			c.Abort()
			return
		}

		claims, err := utils.ParseToken(parts[1], cfg.JWTSecret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的认证令牌"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}

// InternalAPIMiddleware 内部 API 认证中间件（用于服务间调用）
func InternalAPIMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-Internal-API-Key")
		expectedKey := cfg.InternalAPIKey

		if expectedKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: internal API key not configured"})
			c.Abort()
			return
		}

		if apiKey != expectedKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: invalid internal API key"})
			c.Abort()
			return
		}

		c.Next()
	}
}
