package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/gin-gonic/gin"
)

const (
	canonicalAuthContextKey               = "addp.auth_context/v1"
	maxAuthContextResponseBytes           = 1 << 20
	defaultAuthContextHTTPTimeout         = 10 * time.Second
	BrowserResourceAccessTicketCookieName = "addp_resource_access_ticket"
)

type MiddlewareConfig struct {
	SystemURL  string
	HTTPClient *http.Client
}

type authContextErrorResponse struct {
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
}

var (
	errAuthContextCredentialRejected = errors.New("auth context credential rejected")
	errAuthContextUnavailable        = errors.New("auth context unavailable")
)

type authContextHTTPResolver struct {
	endpoint   string
	httpClient *http.Client
}

// NewMiddleware resolves a user Bearer token through System's canonical
// AuthContext endpoint. It does not accept internal API keys or cache context.
func NewMiddleware(config MiddlewareConfig) (gin.HandlerFunc, error) {
	resolver, err := newAuthContextHTTPResolver(config.SystemURL, config.HTTPClient)
	if err != nil {
		return nil, err
	}

	return func(c *gin.Context) {
		if _, exists := AuthContextFromGin(c); exists {
			c.Next()
			return
		}
		accessToken := canonicalBearerToken(c.GetHeader("Authorization"))
		if accessToken == "" {
			abortAuthenticationRequired(c)
			return
		}

		authContext, err := resolver.resolve(c.Request.Context(), accessToken, authContextForwardHeaders(c))
		if errors.Is(err, errAuthContextCredentialRejected) {
			abortAuthenticationRequired(c)
			return
		}
		if err != nil {
			abortAuthorizationServiceUnavailable(c)
			return
		}
		if err := SetAuthContextForGin(c, authContext); err != nil {
			abortAuthorizationServiceUnavailable(c)
			return
		}
		c.Next()
	}, nil
}

// MustNewMiddleware creates the canonical AuthContext middleware for router
// composition and fails fast when the module configuration is invalid.
func MustNewMiddleware(config MiddlewareConfig) gin.HandlerFunc {
	middleware, err := NewMiddleware(config)
	if err != nil {
		panic(err)
	}
	return middleware
}

type authContextRequestHeaders struct {
	AcceptLanguage string
	RequestID      string
}

func newAuthContextHTTPResolver(systemURL string, configuredClient *http.Client) (*authContextHTTPResolver, error) {
	endpoint, err := canonicalAuthContextEndpoint(systemURL)
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	if configuredClient != nil {
		client.Transport = configuredClient.Transport
		client.Timeout = configuredClient.Timeout
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if client.Timeout <= 0 {
		client.Timeout = defaultAuthContextHTTPTimeout
	}
	return &authContextHTTPResolver{endpoint: endpoint, httpClient: client}, nil
}

func (r *authContextHTTPResolver) resolve(
	ctx context.Context,
	credential string,
	headers authContextRequestHeaders,
) (commonauth.AuthContext, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint, nil)
	if err != nil {
		return commonauth.AuthContext{}, errAuthContextUnavailable
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	if headers.AcceptLanguage != "" {
		request.Header.Set("Accept-Language", headers.AcceptLanguage)
	}
	if headers.RequestID != "" {
		request.Header.Set(requestidmiddleware.RequestIDHeader, headers.RequestID)
	}

	response, err := r.httpClient.Do(request)
	if err != nil {
		return commonauth.AuthContext{}, errAuthContextUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		discardAuthContextResponse(response.Body)
		return commonauth.AuthContext{}, errAuthContextCredentialRejected
	}
	if response.StatusCode != http.StatusOK {
		discardAuthContextResponse(response.Body)
		return commonauth.AuthContext{}, errAuthContextUnavailable
	}

	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxAuthContextResponseBytes+1))
	if err != nil || len(encoded) > maxAuthContextResponseBytes {
		return commonauth.AuthContext{}, errAuthContextUnavailable
	}
	authContext, err := commonauth.DecodeAuthContext(bytes.NewReader(encoded))
	if err != nil {
		return commonauth.AuthContext{}, errAuthContextUnavailable
	}
	return authContext, nil
}

func authContextForwardHeaders(c *gin.Context) authContextRequestHeaders {
	requestID := requestidmiddleware.FromGinContext(c)
	if requestID == "" {
		requestID = c.GetHeader(requestidmiddleware.RequestIDHeader)
	}
	return authContextRequestHeaders{
		AcceptLanguage: c.GetHeader("Accept-Language"),
		RequestID:      requestID,
	}
}

// AuthContextFromGin returns a detached canonical AuthContext.
func AuthContextFromGin(c *gin.Context) (commonauth.AuthContext, bool) {
	value, exists := c.Get(canonicalAuthContextKey)
	authContext, valid := value.(commonauth.AuthContext)
	if !exists || !valid {
		return commonauth.AuthContext{}, false
	}
	return commonauth.CloneAuthContext(authContext), true
}

func PrincipalFromGin(c *gin.Context) (commonauth.AuthPrincipal, bool) {
	authContext, exists := AuthContextFromGin(c)
	if !exists {
		return commonauth.AuthPrincipal{}, false
	}
	return authContext.Principal, true
}

func AuthSessionContextFromGin(c *gin.Context) (commonauth.AuthSessionContext, bool) {
	authContext, exists := AuthContextFromGin(c)
	if !exists {
		return commonauth.AuthSessionContext{}, false
	}
	return authContext.Context, true
}

func PrincipalIDFromGin(c *gin.Context) (int64, bool) {
	principal, exists := PrincipalFromGin(c)
	if !exists {
		return 0, false
	}
	principalID, err := strconv.ParseInt(principal.ID, 10, 64)
	return principalID, err == nil && principalID > 0
}

func TenantIDFromGin(c *gin.Context) (int64, bool) {
	sessionContext, exists := AuthSessionContextFromGin(c)
	if !exists || sessionContext.Type != "tenant" || sessionContext.TenantID == nil {
		return 0, false
	}
	tenantID, err := strconv.ParseInt(*sessionContext.TenantID, 10, 64)
	return tenantID, err == nil && tenantID > 0
}

func GetUserID(c *gin.Context) uint {
	principal, exists := PrincipalFromGin(c)
	if !exists || principal.Type != "user" {
		return 0
	}
	principalID, err := strconv.ParseUint(principal.ID, 10, 64)
	if err != nil {
		return 0
	}
	return uint(principalID)
}

func GetTenantID(c *gin.Context) uint {
	tenantID, exists := TenantIDFromGin(c)
	if !exists {
		return 0
	}
	return uint(tenantID)
}

func GetClientID(c *gin.Context) string {
	authContext, exists := AuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		return ""
	}
	return *authContext.Client.ClientID
}

func GetScopes(c *gin.Context) []string {
	authContext, exists := AuthContextFromGin(c)
	if !exists {
		return nil
	}
	return append([]string(nil), authContext.Client.Scopes...)
}

func GetAudiences(c *gin.Context) []string {
	authContext, exists := AuthContextFromGin(c)
	if !exists {
		return nil
	}
	return append([]string(nil), authContext.Client.Audiences...)
}

func NewContextGuard(contextType string) (gin.HandlerFunc, error) {
	if contextType != "platform" && contextType != "tenant" {
		return nil, fmt.Errorf("%w: invalid AuthContext type", commonapi.ErrBadRequest)
	}
	return func(c *gin.Context) {
		authContext, exists := AuthContextFromGin(c)
		if !exists {
			abortAuthenticationRequired(c)
			return
		}
		if authContext.Context.Type != contextType {
			abortPermissionDenied(c)
			return
		}
		c.Next()
	}, nil
}

func MustNewContextGuard(contextType string) gin.HandlerFunc {
	guard, err := NewContextGuard(contextType)
	if err != nil {
		panic(err)
	}
	return guard
}

func NewServiceClientGuard(expectedClientIDs ...string) (gin.HandlerFunc, error) {
	allowed := make(map[string]struct{}, len(expectedClientIDs))
	for _, expectedClientID := range expectedClientIDs {
		expectedClientID = strings.TrimSpace(expectedClientID)
		if expectedClientID == "" {
			return nil, fmt.Errorf("%w: expected service client ID is required", commonapi.ErrBadRequest)
		}
		allowed[expectedClientID] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("%w: expected service client ID is required", commonapi.ErrBadRequest)
	}
	return func(c *gin.Context) {
		authContext, exists := AuthContextFromGin(c)
		if !exists {
			abortAuthenticationRequired(c)
			return
		}
		_, clientAllowed := allowed[GetClientID(c)]
		if authContext.Principal.Type != "service_principal" || !clientAllowed {
			abortPermissionDenied(c)
			return
		}
		c.Next()
	}, nil
}

func MustNewServiceClientGuard(expectedClientIDs ...string) gin.HandlerFunc {
	guard, err := NewServiceClientGuard(expectedClientIDs...)
	if err != nil {
		panic(err)
	}
	return guard
}

func NewPermissionGuard(requiredPermissions ...string) (gin.HandlerFunc, error) {
	permissions, err := normalizeRequiredPermissions(requiredPermissions)
	if err != nil {
		return nil, err
	}
	return func(c *gin.Context) {
		if _, exists := AuthContextFromGin(c); !exists {
			abortAuthenticationRequired(c)
			return
		}
		if !HasAllRolePermissions(c, permissions...) {
			abortPermissionDenied(c)
			return
		}
		c.Next()
	}, nil
}

func MustNewPermissionGuard(requiredPermissions ...string) gin.HandlerFunc {
	guard, err := NewPermissionGuard(requiredPermissions...)
	if err != nil {
		panic(err)
	}
	return guard
}

// HasRolePermission reports only the functional Allow candidate projected by
// Role Assignment. Owner resource policy and restricted client scopes remain
// separate mandatory checks.
func HasRolePermission(c *gin.Context, permission string) bool {
	return len(RolePermissionScopes(c, permission)) > 0
}

func HasAllRolePermissions(c *gin.Context, requiredPermissions ...string) bool {
	if len(requiredPermissions) == 0 {
		return false
	}
	authContext, exists := AuthContextFromGin(c)
	if !exists {
		return false
	}
	return hasAllRolePermissions(authContext, requiredPermissions)
}

func hasAllRolePermissions(authContext commonauth.AuthContext, requiredPermissions []string) bool {
	if len(requiredPermissions) == 0 {
		return false
	}
	required := make(map[string]struct{}, len(requiredPermissions))
	for _, permission := range requiredPermissions {
		if commonauth.ValidatePermissionKey(permission) != nil {
			return false
		}
		required[permission] = struct{}{}
	}
	for _, assignment := range authContext.Authorization.RoleAssignments {
		for _, permission := range assignment.Permissions {
			delete(required, permission)
		}
	}
	return len(required) == 0
}

// RolePermissionScopes returns the effective Assignment Scopes that contain a
// Permission candidate. Duplicate scopes are collapsed in stable order.
func RolePermissionScopes(c *gin.Context, permission string) []commonauth.AssignmentScope {
	if commonauth.ValidatePermissionKey(permission) != nil {
		return nil
	}
	authContext, exists := AuthContextFromGin(c)
	if !exists {
		return nil
	}
	result := make([]commonauth.AssignmentScope, 0)
	seen := make(map[string]struct{})
	for _, assignment := range authContext.Authorization.RoleAssignments {
		if !containsCanonicalString(assignment.Permissions, permission) {
			continue
		}
		key := assignmentScopeKey(assignment.Scope)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, assignment.Scope)
	}
	return result
}

// SetAuthContextForGin validates and stores a detached canonical AuthContext.
// It is used by System's in-process resolver and by remote consumer middleware.
func SetAuthContextForGin(c *gin.Context, authContext commonauth.AuthContext) error {
	if c == nil {
		return fmt.Errorf("%w: Gin Context is required", commonapi.ErrBadRequest)
	}
	if err := commonauth.ValidateAuthContext(authContext); err != nil {
		return fmt.Errorf("%w: invalid AuthContext: %v", commonapi.ErrBadRequest, err)
	}
	c.Set(canonicalAuthContextKey, commonauth.CloneAuthContext(authContext))
	return nil
}

func setCanonicalAuthContext(c *gin.Context, authContext commonauth.AuthContext) {
	c.Set(canonicalAuthContextKey, commonauth.CloneAuthContext(authContext))
}

func canonicalAuthContextEndpoint(systemURL string) (string, error) {
	if strings.TrimSpace(systemURL) != systemURL || systemURL == "" {
		return "", fmt.Errorf("%w: System URL is required", commonapi.ErrBadRequest)
	}
	parsed, err := url.Parse(systemURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: invalid System URL", commonapi.ErrBadRequest)
	}
	return parsed.JoinPath("api/v1/system/auth/context").String(), nil
}

func canonicalBearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func abortAuthenticationRequired(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, authContextErrorResponse{
		Error:     commoni18n.T(c, commoni18n.MsgUnauthorized),
		ErrorCode: "authentication_required",
	})
}

func abortAuthorizationServiceUnavailable(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, authContextErrorResponse{
		Error:     commoni18n.T(c, commoni18n.MsgAuthorizationServiceUnavailable),
		ErrorCode: "authorization_service_unavailable",
	})
}

func discardAuthContextResponse(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4<<10))
}

func containsCanonicalString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func assignmentScopeKey(scope commonauth.AssignmentScope) string {
	return strings.Join([]string{
		scope.Type,
		canonicalStringValue(scope.TenantID),
		canonicalStringValue(scope.DepartmentID),
		canonicalStringValue(scope.ProjectGroupID),
	}, "|")
}

func canonicalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
