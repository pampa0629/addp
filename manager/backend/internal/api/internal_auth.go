package api

import (
	"net/http"
	"strconv"
	"strings"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/config"
	"github.com/gin-gonic/gin"
)

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
		c.Set(commonAuth.ContextUserIDKey, uint(1))
		c.Set(commonAuth.ContextUsernameKey, "internal-api-call")
		c.Set(commonAuth.ContextTenantIDKey, tenantID)
		c.Next()
	}
}
