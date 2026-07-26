package auth

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
)

type ResourceTicketMiddlewareConfig struct {
	SystemURL           string
	HTTPClient          *http.Client
	Owner               string
	RequiredPermissions []string
	Now                 func() time.Time
}

type ResourceTicketRequestMatcher func(*gin.Context) bool

// NewOptionalResourceTicketMiddleware resolves an owner ticket only for an
// explicit native-resource route matcher. Other requests continue to the
// normal Bearer AuthContext middleware.
func NewOptionalResourceTicketMiddleware(
	config ResourceTicketMiddlewareConfig,
	matcher ResourceTicketRequestMatcher,
) (gin.HandlerFunc, error) {
	if matcher == nil {
		return nil, fmt.Errorf("%w: Resource Ticket route matcher is required", commonapi.ErrBadRequest)
	}
	resourceTicket, err := NewResourceTicketMiddleware(config)
	if err != nil {
		return nil, err
	}
	return func(c *gin.Context) {
		if len(c.Request.Header.Values("Authorization")) != 0 || !matcher(c) {
			c.Next()
			return
		}
		resourceTicket(c)
	}, nil
}

func MustNewOptionalResourceTicketMiddleware(
	config ResourceTicketMiddlewareConfig,
	matcher ResourceTicketRequestMatcher,
) gin.HandlerFunc {
	middleware, err := NewOptionalResourceTicketMiddleware(config, matcher)
	if err != nil {
		panic(err)
	}
	return middleware
}

// NewResourceTicketMiddleware authenticates one explicitly mounted GET/HEAD
// native resource route with its owner-scoped browser ticket cookie.
func NewResourceTicketMiddleware(config ResourceTicketMiddlewareConfig) (gin.HandlerFunc, error) {
	if commonauth.ValidateOwnerModuleName(config.Owner) != nil {
		return nil, fmt.Errorf("%w: invalid Resource Ticket owner", commonapi.ErrBadRequest)
	}
	requiredPermissions, err := normalizeRequiredPermissions(config.RequiredPermissions)
	if err != nil {
		return nil, err
	}
	resolver, err := newAuthContextHTTPResolver(config.SystemURL, config.HTTPClient)
	if err != nil {
		return nil, err
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}

	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			abortResourceTicketMethodNotAllowed(c)
			return
		}
		if len(c.Request.Header.Values("Authorization")) != 0 {
			abortAuthenticationRequired(c)
			return
		}
		ticket, valid := uniqueResourceAccessTicketCookie(c.Request)
		if !valid {
			abortAuthenticationRequired(c)
			return
		}

		authContext, err := resolver.resolve(c.Request.Context(), ticket, authContextForwardHeaders(c))
		if errors.Is(err, errAuthContextCredentialRejected) {
			abortAuthenticationRequired(c)
			return
		}
		if err != nil {
			abortAuthorizationServiceUnavailable(c)
			return
		}
		if !validCanonicalResourceTicketContext(authContext, config.Owner, now().UTC()) {
			abortAuthenticationRequired(c)
			return
		}
		if !hasAllRolePermissions(authContext, requiredPermissions) {
			abortPermissionDenied(c)
			return
		}

		setCanonicalAuthContext(c, authContext)
		c.Next()
	}, nil
}

func normalizeRequiredPermissions(permissions []string) ([]string, error) {
	if len(permissions) == 0 {
		return nil, fmt.Errorf("%w: Resource Ticket permissions are required", commonapi.ErrBadRequest)
	}
	normalized := append([]string(nil), permissions...)
	sort.Strings(normalized)
	for index, permission := range normalized {
		if commonauth.ValidatePermissionKey(permission) != nil ||
			(index > 0 && permission == normalized[index-1]) {
			return nil, fmt.Errorf("%w: invalid Resource Ticket permissions", commonapi.ErrBadRequest)
		}
	}
	return normalized, nil
}

func uniqueResourceAccessTicketCookie(request *http.Request) (string, bool) {
	var ticket string
	count := 0
	for _, cookie := range request.Cookies() {
		if cookie.Name != BrowserResourceAccessTicketCookieName {
			continue
		}
		count++
		ticket = cookie.Value
	}
	return ticket, count == 1 && ticket != "" && strings.TrimSpace(ticket) == ticket
}

func validCanonicalResourceTicketContext(
	authContext commonauth.AuthContext,
	owner string,
	now time.Time,
) bool {
	return authContext.Principal.Type == "user" &&
		authContext.Token.Type == "resource_access_ticket" &&
		authContext.Client.ClientID != nil && *authContext.Client.ClientID == "addp-web" &&
		authContext.Client.ScopeMode == "restricted" &&
		len(authContext.Client.Audiences) == 1 && authContext.Client.Audiences[0] == owner &&
		len(authContext.Client.Scopes) == 1 &&
		authContext.Client.Scopes[0] == commonauth.BrowserResourceAccessScope &&
		authContext.Delegation == nil && authContext.Token.ExpiresAt.After(now)
}

func abortPermissionDenied(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusForbidden, authContextErrorResponse{
		Error:     commoni18n.T(c, commoni18n.MsgForbidden),
		ErrorCode: "permission_denied",
	})
}

func abortResourceTicketMethodNotAllowed(c *gin.Context) {
	c.Header("Allow", "GET, HEAD")
	c.AbortWithStatusJSON(http.StatusMethodNotAllowed, authContextErrorResponse{
		Error:     commoni18n.T(c, commoni18n.MsgMethodNotAllowed),
		ErrorCode: "method_not_allowed",
	})
}
