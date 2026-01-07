package api

import (
	"github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

// tenantIDFromContext 从 Gin Context 中提取 TenantID
// 返回 *uint 指针，如果 tenantID 为 0（超级管理员）则返回 nil
func tenantIDFromContext(c *gin.Context) *uint {
	tenantID := auth.GetTenantID(c)
	if tenantID == 0 {
		// 超级管理员返回 nil，表示不限制租户
		return nil
	}
	return &tenantID
}
