package auth

import (
	"net/http"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
)

const AuthTypeDelegatedAccessToken = "delegated_access_token"

type DelegatedRoutePolicy map[string][]string

// DelegatedAccessPolicy denies delegated tokens by default and only allows
// explicitly registered Tool routes with the required owner audience and scopes.
func DelegatedAccessPolicy(audience string, routes DelegatedRoutePolicy) gin.HandlerFunc {
	return func(c *gin.Context) {
		authorizationContext, ok := GetAuthorizationContext(c)
		if !ok || authorizationContext.AuthType != AuthTypeDelegatedAccessToken {
			c.Next()
			return
		}
		requiredScopes, allowed := routes[c.Request.Method+" "+c.FullPath()]
		if !allowed || !ValidateDelegatedAccess(authorizationContext.AuthType, authorizationContext.Audiences, authorizationContext.Scopes, audience, requiredScopes) {
			c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, commoni18n.MsgForbidden)})
			c.Abort()
			return
		}
		c.Next()
	}
}

func ValidateDelegatedAccess(authType string, audiences, scopes []string, audience string, requiredScopes []string) bool {
	if authType != AuthTypeDelegatedAccessToken {
		return true
	}
	if !containsString(audiences, audience) {
		return false
	}
	for _, required := range requiredScopes {
		if !containsString(scopes, required) {
			return false
		}
	}
	return len(requiredScopes) > 0
}
