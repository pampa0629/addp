package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func tenantIDValue(c *gin.Context) uint {
	return commonAuth.GetTenantID(c)
}

func userIDValue(c *gin.Context) uint {
	return commonAuth.GetUserID(c)
}
