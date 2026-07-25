package auth

import (
	"fmt"
	"sort"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/gin-gonic/gin"
)

type DelegatedRouteGuardConfig struct {
	Audience            string
	RequiredScopes      []string
	RequiredPermissions []string
	Now                 func() time.Time
}

// NewDelegatedRouteGuard constrains delegated tokens on one explicitly
// mounted Tool route. Non-delegated credentials remain on the route's normal
// permission and owner resource-policy path.
func NewDelegatedRouteGuard(config DelegatedRouteGuardConfig) (gin.HandlerFunc, error) {
	if commonauth.ValidateOwnerModuleName(config.Audience) != nil {
		return nil, fmt.Errorf("%w: invalid delegated audience", commonapi.ErrBadRequest)
	}
	requiredScopes, err := normalizeDelegatedScopes(config.RequiredScopes)
	if err != nil {
		return nil, err
	}
	requiredPermissions, err := normalizeRequiredPermissions(config.RequiredPermissions)
	if err != nil {
		return nil, err
	}
	ownerPrefix := config.Audience + "."
	for _, permission := range requiredPermissions {
		if !strings.HasPrefix(permission, ownerPrefix) {
			return nil, fmt.Errorf("%w: delegated permissions must belong to the audience owner", commonapi.ErrBadRequest)
		}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}

	return func(c *gin.Context) {
		authContext, exists := AuthContextFromGin(c)
		if !exists {
			abortAuthenticationRequired(c)
			return
		}
		if authContext.Token.Type != "delegated_access_token" {
			c.Next()
			return
		}
		if !authContext.Token.ExpiresAt.After(now().UTC()) {
			abortAuthenticationRequired(c)
			return
		}
		if !validDelegatedRouteContext(authContext, config.Audience, requiredScopes) ||
			!hasAllRolePermissions(authContext, requiredPermissions) {
			abortPermissionDenied(c)
			return
		}
		c.Next()
	}, nil
}

func normalizeDelegatedScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, fmt.Errorf("%w: delegated Tool scopes are required", commonapi.ErrBadRequest)
	}
	normalized := append([]string(nil), scopes...)
	sort.Strings(normalized)
	for index, scope := range normalized {
		if commonauth.ValidateToolScope(scope) != nil || (index > 0 && scope == normalized[index-1]) {
			return nil, fmt.Errorf("%w: invalid delegated Tool scopes", commonapi.ErrBadRequest)
		}
	}
	return normalized, nil
}

func validDelegatedRouteContext(
	authContext commonauth.AuthContext,
	audience string,
	requiredScopes []string,
) bool {
	if authContext.Principal.Type != "user" || authContext.Client.ClientID == nil ||
		authContext.Client.ScopeMode != "restricted" || len(authContext.Client.Audiences) != 1 ||
		authContext.Client.Audiences[0] != audience || authContext.Delegation == nil ||
		authContext.Delegation.DelegatedByClientID != *authContext.Client.ClientID ||
		len(authContext.Client.Scopes) != len(requiredScopes) {
		return false
	}
	for index, scope := range requiredScopes {
		if authContext.Client.Scopes[index] != scope {
			return false
		}
	}
	return true
}
