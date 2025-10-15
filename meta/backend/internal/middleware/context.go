package middleware

import (
	"github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

// GetTenantID returns the tenant ID from context if present.
func GetTenantID(c *gin.Context) uint {
	if value, exists := c.Get(auth.ContextTenantIDKey); exists {
		if tenantID, ok := value.(uint); ok {
			return tenantID
		}
	}
	return 0
}

// GetUserID returns the user ID from context if present.
func GetUserID(c *gin.Context) uint {
	if value, exists := c.Get(auth.ContextUserIDKey); exists {
		if userID, ok := value.(uint); ok {
			return userID
		}
	}
	return 0
}

// GetUsername returns the username from context if present.
func GetUsername(c *gin.Context) string {
	if value, exists := c.Get(auth.ContextUsernameKey); exists {
		if username, ok := value.(string); ok {
			return username
		}
	}
	return ""
}
