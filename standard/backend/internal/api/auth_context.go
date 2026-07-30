package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func getTenantID(c *gin.Context) int64 {
	tenantID, _ := commonAuth.TenantIDFromGin(c)
	return tenantID
}

func getUserID(c *gin.Context) int64 {
	userID, _ := commonAuth.PrincipalIDFromGin(c)
	return userID
}
