package middleware

import (
	"net/http"
	"strings"

	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

const AuthorizationContextKey = "authorization_context"

// AuthMiddleware resolves opaque user access tokens through System TokenService.
func AuthMiddleware(tokenService *service.TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgMissingAuthHeader)})
			c.Abort()
			return
		}

		// Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidAuthFormat)})
			c.Abort()
			return
		}

		authorizationContext, err := tokenService.ResolveAccessToken(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidToken)})
			c.Abort()
			return
		}

		c.Set("user_id", authorizationContext.UserID)
		c.Set("username", authorizationContext.Username)
		if authorizationContext.TenantID != nil {
			c.Set("tenant_id", *authorizationContext.TenantID)
		} else {
			c.Set("tenant_id", uint(0))
		}
		c.Set(AuthorizationContextKey, authorizationContext)
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
