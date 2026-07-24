package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/gin-gonic/gin"
)

const iamAuthContextKey = "addp.iam.auth_context"

type IAMAuthContextResolver interface {
	ResolveFirstPartyAccessToken(context.Context, string) (*commonauth.AuthContext, error)
}

type iamAuthErrorResponse struct {
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
}

// NewIAMAuthenticationMiddleware resolves a first-party Bearer token into the
// canonical AuthContext. It does not project legacy flat identity keys.
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
		authContext, err := resolver.ResolveFirstPartyAccessToken(c.Request.Context(), accessToken)
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
		c.Set(iamAuthContextKey, cloneIAMAuthContext(*authContext))
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
	value, exists := c.Get(iamAuthContextKey)
	authContext, valid := value.(commonauth.AuthContext)
	if !exists || !valid {
		return commonauth.AuthContext{}, false
	}
	return cloneIAMAuthContext(authContext), true
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

func cloneIAMAuthContext(source commonauth.AuthContext) commonauth.AuthContext {
	clone := source
	clone.Authentication.Methods = append([]string(nil), source.Authentication.Methods...)
	clone.Authentication.StepUpExpiresAt = cloneIAMTime(source.Authentication.StepUpExpiresAt)
	clone.Client.ClientID = cloneIAMString(source.Client.ClientID)
	clone.Client.Audiences = append([]string(nil), source.Client.Audiences...)
	clone.Client.Scopes = append([]string(nil), source.Client.Scopes...)
	clone.Context.TenantID = cloneIAMString(source.Context.TenantID)
	clone.Context.TenantMembershipID = cloneIAMString(source.Context.TenantMembershipID)
	clone.Organization.Departments = append([]commonauth.DepartmentMembership(nil), source.Organization.Departments...)
	for index := range clone.Organization.Departments {
		clone.Organization.Departments[index].AncestorIDs = append(
			[]string(nil),
			source.Organization.Departments[index].AncestorIDs...,
		)
	}
	clone.Organization.ProjectGroups = append([]commonauth.ProjectGroupMembership(nil), source.Organization.ProjectGroups...)
	clone.Authorization.RoleAssignments = append([]commonauth.RoleAssignment(nil), source.Authorization.RoleAssignments...)
	for index := range clone.Authorization.RoleAssignments {
		sourceAssignment := source.Authorization.RoleAssignments[index]
		assignment := &clone.Authorization.RoleAssignments[index]
		assignment.Scope.TenantID = cloneIAMString(sourceAssignment.Scope.TenantID)
		assignment.Scope.DepartmentID = cloneIAMString(sourceAssignment.Scope.DepartmentID)
		assignment.Scope.ProjectGroupID = cloneIAMString(sourceAssignment.Scope.ProjectGroupID)
		assignment.Permissions = append([]string(nil), sourceAssignment.Permissions...)
		assignment.ValidUntil = cloneIAMTime(sourceAssignment.ValidUntil)
	}
	if source.Delegation != nil {
		delegation := *source.Delegation
		clone.Delegation = &delegation
	}
	return clone
}

func cloneIAMString(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneIAMTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
