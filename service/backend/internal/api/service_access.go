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

	if _, authenticated := authMiddleware.AuthContextFromGin(c); !authenticated {
		return http.StatusUnauthorized
	}
	tenantID, exists := authMiddleware.TenantIDFromGin(c)
	if !exists || uint(tenantID) != serviceTenantID {
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
