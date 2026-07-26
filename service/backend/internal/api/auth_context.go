package api

import (
	"net/http"
	"strconv"
	"strings"

	authMiddleware "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

const internalTenantIDKey = "service.internal_tenant_id"

func tenantIDValue(c *gin.Context) uint {
	return authMiddleware.GetTenantID(c)
}

func userIDValue(c *gin.Context) uint {
	return authMiddleware.GetUserID(c)
}

func internalTenantIDValue(c *gin.Context) uint {
	return c.GetUint(internalTenantIDKey)
}

func internalAPIKeyMiddleware(expectedKey string) gin.HandlerFunc {
	expectedKey = strings.TrimSpace(expectedKey)
	return func(c *gin.Context) {
		if expectedKey == "" || c.GetHeader("X-Internal-API-Key") != expectedKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized internal request"})
			return
		}
		tenantID, err := strconv.ParseUint(strings.TrimSpace(c.GetHeader("X-Tenant-ID")), 10, 32)
		if err != nil || tenantID == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-ID is required"})
			return
		}
		c.Set(internalTenantIDKey, uint(tenantID))
		c.Next()
	}
}
