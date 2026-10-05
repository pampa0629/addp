package api

import (
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/gin-gonic/gin"
)

// protectionReadBoundary must follow authentication and Tenant context guards.
// c.Next includes handler serialization and streaming; release must not occur
// when a Provider has merely finished fetching rows.
func protectionReadBoundary(boundary *managerprotection.ReadBoundary) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := tenantIDFromContext(c)
		if tenantID == nil {
			protectionRequired(c)
			c.Abort()
			return
		}
		end, err := boundary.BeginRead(c.Request.Context(), int64(*tenantID))
		if err != nil {
			protectionRequired(c)
			c.Abort()
			return
		}
		defer end()
		c.Next()
	}
}
