package api

import (
	"github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

// tenantIDFromContext 从 canonical AuthContext 中提取当前租户。
func tenantIDFromContext(c *gin.Context) *uint {
	tenantID := auth.GetTenantID(c)
	if tenantID == 0 {
		return nil
	}
	return &tenantID
}

func tenantIDValue(c *gin.Context) uint {
	return auth.GetTenantID(c)
}

func userIDValue(c *gin.Context) uint {
	return auth.GetUserID(c)
}

// tenantFilterIDFromContext 只使用认证租户，不接受 query 覆盖。
func tenantFilterIDFromContext(c *gin.Context) *uint {
	tenantID := auth.GetTenantID(c)
	if tenantID == 0 {
		return nil
	}
	return &tenantID
}
