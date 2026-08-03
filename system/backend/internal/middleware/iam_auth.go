package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/gin-gonic/gin"
)

const (
	IAMTokenTypeFirstPartyAccess = "first_party_access_token"
	IAMTokenTypeOAuthAccess      = "oauth_access_token"
	IAMTokenTypeServiceAccess    = "service_access_token"
	IAMTokenTypeDelegatedAccess  = "delegated_access_token"
	IAMTokenTypeResourceTicket   = "resource_access_ticket"
)

type IAMAuthContextResolver interface {
	ResolveAuthContext(context.Context, string) (*commonauth.AuthContext, error)
}

type iamAuthErrorResponse struct {
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
}

// NewIAMAuthenticationMiddleware resolves an ADDP credential into the
// canonical AuthContext. Route guards decide which credential types are valid.
// It does not project legacy flat identity keys.
func NewIAMAuthenticationMiddleware(resolver IAMAuthContextResolver) (gin.HandlerFunc, error) {
	if resolver == nil {
		return nil, fmt.Errorf("%w: IAM AuthContext resolver is required", commonapi.ErrBadRequest)
	}
	return func(c *gin.Context) {
		accessToken := iamAccessToken(c.GetHeader("Authorization"))
		if accessToken == "" {
			abortIAMAuthenticationRequired(c)
			return
		}
		authContext, err := resolver.ResolveAuthContext(c.Request.Context(), accessToken)
		if err != nil {
			if errors.Is(err, commonapi.ErrUnauthorized) {
				abortIAMAuthenticationRequired(c)
				return
			}
			abortIAMInternalError(c)
			return
		}
		if authContext == nil || commonauth.ValidateAuthContext(*authContext) != nil {
			abortIAMInternalError(c)
			return
		}
		if err := sharedauth.SetAuthContextForGin(c, *authContext); err != nil {
			abortIAMInternalError(c)
			return
		}
		c.Next()
	}, nil
}

// NewIAMCredentialGuard restricts a route to explicitly listed AuthContext
// credential token types.
func NewIAMCredentialGuard(allowedTokenTypes ...string) (gin.HandlerFunc, error) {
	allowed := append([]string(nil), allowedTokenTypes...)
	if len(allowed) == 0 {
		return nil, fmt.Errorf("%w: at least one credential token type is required", commonapi.ErrBadRequest)
	}
	sort.Strings(allowed)
	allowedSet := make(map[string]struct{}, len(allowed))
	for index, tokenType := range allowed {
		if tokenType != strings.TrimSpace(tokenType) || tokenType == "" {
			return nil, fmt.Errorf("%w: invalid credential token type", commonapi.ErrBadRequest)
		}
		if index > 0 && allowed[index-1] == tokenType {
			return nil, fmt.Errorf("%w: duplicate credential token type %q", commonapi.ErrBadRequest, tokenType)
		}
		allowedSet[tokenType] = struct{}{}
	}

	return func(c *gin.Context) {
		authContext, exists := IAMAuthContextFromGin(c)
		if !exists {
			abortIAMAuthenticationRequired(c)
			return
		}
		if _, exists := allowedSet[authContext.Token.Type]; !exists {
			abortIAMPermissionDenied(c)
			return
		}
		c.Next()
	}, nil
}

// RequireIAMAuthenticated enforces the authenticated route mode.
func RequireIAMAuthenticated() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, exists := IAMAuthContextFromGin(c); !exists {
			abortIAMAuthenticationRequired(c)
			return
		}
		c.Next()
	}
}

// RequireIAMSelf enforces the self route mode for the current User.
func RequireIAMSelf() gin.HandlerFunc {
	return func(c *gin.Context) {
		authContext, exists := IAMAuthContextFromGin(c)
		if !exists {
			abortIAMAuthenticationRequired(c)
			return
		}
		if authContext.Principal.Type != "user" {
			abortIAMPermissionDenied(c)
			return
		}
		c.Next()
	}
}

// NewIAMContextGuard restricts a route to one explicit AuthContext mode.
func NewIAMContextGuard(contextType string) (gin.HandlerFunc, error) {
	if contextType != "platform" && contextType != "tenant" {
		return nil, fmt.Errorf("%w: unsupported IAM context type", commonapi.ErrBadRequest)
	}
	return func(c *gin.Context) {
		authContext, exists := IAMAuthContextFromGin(c)
		if !exists {
			abortIAMAuthenticationRequired(c)
			return
		}
		if authContext.Principal.Type != "user" || authContext.Context.Type != contextType {
			abortIAMPermissionDenied(c)
			return
		}
		c.Next()
	}, nil
}

func NewIAMServiceContextGuard(contextType string) (gin.HandlerFunc, error) {
	if contextType != "platform" && contextType != "tenant" {
		return nil, fmt.Errorf("%w: unsupported IAM context type", commonapi.ErrBadRequest)
	}
	return func(c *gin.Context) {
		authContext, exists := IAMAuthContextFromGin(c)
		if !exists {
			abortIAMAuthenticationRequired(c)
			return
		}
		if authContext.Principal.Type != "service_principal" || authContext.Context.Type != contextType {
			abortIAMPermissionDenied(c)
			return
		}
		c.Next()
	}, nil
}

// NewIAMClientGuard restricts a service route to one exact OAuth Client.
func NewIAMClientGuard(clientID string) (gin.HandlerFunc, error) {
	if clientID == "" || strings.TrimSpace(clientID) != clientID {
		return nil, fmt.Errorf("%w: invalid IAM OAuth client ID", commonapi.ErrBadRequest)
	}
	return func(c *gin.Context) {
		authContext, exists := IAMAuthContextFromGin(c)
		if !exists {
			abortIAMAuthenticationRequired(c)
			return
		}
		if authContext.Principal.Type != "service_principal" || authContext.Client.ClientID == nil ||
			*authContext.Client.ClientID != clientID {
			abortIAMPermissionDenied(c)
			return
		}
		c.Next()
	}, nil
}

// NewIAMPermissionGuard enforces all required Permission keys. Resource and
// assignment-scope policy remains the responsibility of the owner handler.
func NewIAMPermissionGuard(requiredPermissions ...string) (gin.HandlerFunc, error) {
	required := append([]string(nil), requiredPermissions...)
	if len(required) == 0 {
		return nil, fmt.Errorf("%w: at least one Permission is required", commonapi.ErrBadRequest)
	}
	sort.Strings(required)
	for index, permission := range required {
		if err := commonauth.ValidatePermissionKey(permission); err != nil {
			return nil, fmt.Errorf("%w: %v", commonapi.ErrBadRequest, err)
		}
		if index > 0 && required[index-1] == permission {
			return nil, fmt.Errorf("%w: duplicate Permission %q", commonapi.ErrBadRequest, permission)
		}
	}

	return func(c *gin.Context) {
		authContext, exists := IAMAuthContextFromGin(c)
		if !exists {
			abortIAMAuthenticationRequired(c)
			return
		}
		granted := make(map[string]struct{})
		for _, assignment := range authContext.Authorization.RoleAssignments {
			for _, permission := range assignment.Permissions {
				granted[permission] = struct{}{}
			}
		}
		for _, permission := range required {
			if _, exists := granted[permission]; !exists {
				abortIAMPermissionDenied(c)
				return
			}
		}
		c.Next()
	}, nil
}

// IAMAuthContextFromGin returns a detached copy so downstream handlers cannot
// mutate the context shared by later middleware in the same request.
func IAMAuthContextFromGin(c *gin.Context) (commonauth.AuthContext, bool) {
	return sharedauth.AuthContextFromGin(c)
}

func iamAccessToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func abortIAMAuthenticationRequired(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, iamAuthErrorResponse{
		Error:     commoni18n.T(c, commoni18n.MsgUnauthorized),
		ErrorCode: "authentication_required",
	})
}

func abortIAMPermissionDenied(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusForbidden, iamAuthErrorResponse{
		Error:     commoni18n.T(c, commoni18n.MsgForbidden),
		ErrorCode: "permission_denied",
	})
}

func abortIAMInternalError(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusInternalServerError, iamAuthErrorResponse{
		Error:     commoni18n.T(c, sysi18n.MsgInternalError),
		ErrorCode: "internal_error",
	})
}
