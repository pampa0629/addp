package api

import (
	"context"

	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func scanUserOriginContext(c *gin.Context) context.Context {
	facts, _ := commonAuth.AuthContextFromGin(c)
	return commonExecution.WithUserOrigin(c.Request.Context(), facts)
}
