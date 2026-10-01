package execution

import (
	"net/http"
	"sort"

	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
)

// OwnerReadScopeHandler consumes only a module-local Permission declaration.
// The caller must be a first-party User, not a service or delegated credential.
func OwnerReadScopeHandler(module string, permissions map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := commonauth.AuthContextFromGin(c)
		tenantID, tenantOK := commonauth.TenantIDFromGin(c)
		principalID, principalOK := commonauth.PrincipalIDFromGin(c)
		if !ok || !tenantOK || !principalOK || identity.Principal.Type != "user" || identity.Token.Type != "first_party_access_token" || identity.Client.ScopeMode != "unrestricted" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error_code": "permission_denied", "error": commoni18n.T(c, commoni18n.MsgForbidden)})
			return
		}
		scope := ReadScope{Module: module, TenantID: int(tenantID), PrincipalID: principalID, Grants: []ReadGrant{}}
		types := make([]string, 0, len(permissions))
		for taskType := range permissions {
			types = append(types, taskType)
		}
		sort.Strings(types)
		for _, taskType := range types {
			if commonauth.HasRolePermission(c, permissions[taskType]) {
				scope.Grants = append(scope.Grants, ReadGrant{TaskType: taskType, TaskHistory: true, OwnAdHoc: true})
			}
		}
		c.JSON(http.StatusOK, scope)
	}
}
