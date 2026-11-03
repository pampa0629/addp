package api

import (
	"context"

	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

func scanUserOriginContext(c *gin.Context) context.Context {
	facts, _ := commonAuth.AuthContextFromGin(c)
	return service.WithScanRefreshCredential(commonExecution.WithUserOrigin(c.Request.Context(), facts), commonAuth.CanonicalBearerToken(c.GetHeader("Authorization")))
}
