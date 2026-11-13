package api

import (
	"net/http"

	commoni18n "github.com/addp/common/middleware/i18n"
	moni18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

func alertReadMiddleware(query *service.ExecutionQueryService, alerts *service.AlertService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenant, principal, ok := executionReadIdentity(c)
		if !ok {
			return
		}
		if query == nil || alerts == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error_code": "execution_owner_unavailable", "error": commoni18n.T(c, moni18n.MsgExecutionOwnerUnavailable)})
			return
		}
		ctx, err := alerts.AuthorizeReadRequest(c.Request.Context(), query, int(tenant), principal, c.GetHeader("Authorization"), c.Query("module"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error_code": "execution_owner_unavailable", "error": commoni18n.T(c, moni18n.MsgExecutionOwnerUnavailable)})
			return
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
