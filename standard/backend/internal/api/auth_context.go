package api

import (
	"net/http"
	"strconv"
	"strings"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

const internalTenantIDKey = "standard.internal_tenant_id"

func getTenantID(c *gin.Context) int64 {
	tenantID, _ := commonAuth.TenantIDFromGin(c)
	return tenantID
}

func getUserID(c *gin.Context) int64 {
	userID, _ := commonAuth.PrincipalIDFromGin(c)
	return userID
}

func getInternalTenantID(c *gin.Context) int64 {
	return c.GetInt64(internalTenantIDKey)
}

func internalAPIKeyMiddleware(expectedKey string) gin.HandlerFunc {
	expectedKey = strings.TrimSpace(expectedKey)
	return func(c *gin.Context) {
		if expectedKey == "" || c.GetHeader("X-Internal-API-Key") != expectedKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized internal request"})
			return
		}
		tenantID, err := strconv.ParseInt(strings.TrimSpace(c.GetHeader("X-Tenant-ID")), 10, 64)
		if err != nil || tenantID <= 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-ID is required"})
			return
		}
		c.Set(internalTenantIDKey, tenantID)
		c.Next()
	}
}
