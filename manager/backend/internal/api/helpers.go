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

func tenantIDValue(c *gin.Context) uint {
	return auth.GetTenantID(c)
}

// tenantFilterIDFromContext 从 Gin Context 中提取检索用租户过滤。
// 普通用户强制使用认证租户；超级管理员可通过 query.tenant_id 显式指定租户。
func tenantFilterIDFromContext(c *gin.Context) *uint {
	tenantID := auth.GetTenantFilterWithQuery(c)
	if tenantID == 0 {
		return nil
	}
	return &tenantID
}
