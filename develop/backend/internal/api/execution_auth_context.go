package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

func executionAuthContextMiddleware(c *gin.Context) {
	if facts, ok := commonAuth.AuthContextFromGin(c); ok {
		ctx := service.WithExecutionAuthContext(c.Request.Context(), facts)
		if facts.Token.Type == "resource_access_ticket" && isDevelopExportResourceRequest(c) {
			ctx = service.WithExportResourceRead(ctx)
		}
		c.Request = c.Request.WithContext(ctx)
	}
	c.Next()
}
