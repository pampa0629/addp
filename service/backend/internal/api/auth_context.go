package api

import (
	authMiddleware "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func tenantIDValue(c *gin.Context) uint {
	return authMiddleware.GetTenantID(c)
}

func userIDValue(c *gin.Context) uint {
	return authMiddleware.GetUserID(c)
}
