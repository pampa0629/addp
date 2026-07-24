package api

import (
	"net/http"

	authMiddleware "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func serviceAccessStatus(c *gin.Context, publicAccess bool, serviceTenantID uint) int {
	if publicAccess {
		return 0
	}

	authContext, authenticated := authMiddleware.GetAuthorizationContext(c)
	if !authenticated {
		return http.StatusUnauthorized
	}
	if authContext.TenantID == nil || *authContext.TenantID != serviceTenantID {
		return http.StatusForbidden
	}
	return 0
}

func requireJSONServiceAccess(c *gin.Context, publicAccess bool, serviceTenantID uint) bool {
	status := serviceAccessStatus(c, publicAccess, serviceTenantID)
	if status == 0 {
		return true
	}
	if status == http.StatusUnauthorized {
		c.JSON(status, gin.H{"error": "Authentication required"})
	} else {
		c.JSON(status, gin.H{"error": "Access denied"})
	}
	return false
}
