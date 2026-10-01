package api

import (
	"net/http"

	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	moni18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

func executionReadMiddleware(query *service.ExecutionQueryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := commonauth.AuthContextFromGin(c)
		tenant, tenantOK := commonauth.TenantIDFromGin(c)
		principal, principalOK := commonauth.PrincipalIDFromGin(c)
		if !ok || !tenantOK || !principalOK || identity.Principal.Type != "user" || identity.Token.Type != "first_party_access_token" || identity.Client.ScopeMode != "unrestricted" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error_code": "permission_denied", "error": commoni18n.T(c, commoni18n.MsgForbidden)})
			return
		}
		if query == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error_code": "execution_owner_unavailable", "error": commoni18n.T(c, moni18n.MsgExecutionOwnerUnavailable)})
			return
		}
		ctx, err := query.AuthorizeReadRequest(c.Request.Context(), int(tenant), principal, c.GetHeader("Authorization"), c.Query("module"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error_code": "execution_owner_unavailable", "error": commoni18n.T(c, moni18n.MsgExecutionOwnerUnavailable)})
			return
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
