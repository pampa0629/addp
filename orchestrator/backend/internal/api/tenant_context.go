package api

import (
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	orchestratori18n "github.com/addp/orchestrator/i18n"
	"github.com/gin-gonic/gin"
)

func requireTenantID(c *gin.Context) (uint, bool) {
	if _, ok := commonAuth.AuthContextFromGin(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, commoni18n.MsgUnauthorized)})
		return 0, false
	}
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, orchestratori18n.MsgTenantContextRequired)})
		return 0, false
	}
	return uint(tenantID), true
}
