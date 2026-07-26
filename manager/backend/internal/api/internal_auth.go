package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/manager/internal/config"
	"github.com/gin-gonic/gin"
)

const managerInternalTenantIDKey = "manager.internal_tenant_id"

func managerInternalAPIKeyMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		expectedKey := ""
		if cfg != nil {
			expectedKey = strings.TrimSpace(cfg.InternalAPIKey)
		}
		if expectedKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: internal API key not configured"})
			c.Abort()
			return
		}
		if c.GetHeader("X-Internal-API-Key") != expectedKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: invalid internal API key"})
			c.Abort()
			return
		}

		tenantIDHeader := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
		tenantIDValue, err := strconv.ParseUint(tenantIDHeader, 10, 32)
		if err != nil || tenantIDValue == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-ID is required"})
			c.Abort()
			return
		}
		tenantID := uint(tenantIDValue)
		c.Set(managerInternalTenantIDKey, tenantID)
		c.Next()
	}
}

func managerInternalTenantID(c *gin.Context) uint {
	return c.GetUint(managerInternalTenantIDKey)
}
