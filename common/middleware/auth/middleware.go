package auth

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var ErrAuthorizationContextUnavailable = errors.New("authorization context unavailable")

// Context keys used to store authenticated user data.
const (
	ContextUserIDKey               = "user_id"
	ContextUsernameKey             = "username"
	ContextTenantIDKey             = "tenant_id"
	ContextAuthorizationContextKey = "authorization_context"
)

// AuthorizationContext mirrors the canonical System /api/v1/system/auth/context response.
type AuthorizationContext struct {
	SubjectType string    `json:"subject_type"`
	UserID      uint      `json:"user_id"`
	Username    string    `json:"username"`
	UserType    string    `json:"user_type"`
	TenantID    *uint     `json:"tenant_id"`
	AuthType    string    `json:"auth_type"`
	ClientID    *string   `json:"client_id"`
	Audiences   []string  `json:"audiences"`
	Scopes      []string  `json:"scopes"`
	DelegatedBy *string   `json:"delegated_by"`
	AgentRunID  *string   `json:"agent_run_id"`
	ToolCallID  *string   `json:"tool_call_id"`
	IssuedAt    time.Time `json:"issued_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// SystemAuthMiddleware validates the incoming request token by delegating to the System service.
// On success it stores the resolved user information in the Gin context using the constants defined above.
func SystemAuthMiddleware(systemURL string) gin.HandlerFunc {
	baseURL := strings.TrimSuffix(systemURL, "/")
	authContextEndpoint := baseURL + "/api/v1/system/auth/context"
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	return func(c *gin.Context) {
		if _, ok := GetAuthorizationContext(c); ok {
			c.Next()
			return
		}
		if internalKey := c.GetHeader("X-Internal-API-Key"); internalKey != "" {
			if !validInternalAPIKey(internalKey) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid internal api key"})
				c.Abort()
				return
			}
			tenantID := uint(0)
			var tenantIDPtr *uint
			if tenantIDStr := c.GetHeader("X-Tenant-ID"); tenantIDStr != "" {
				if tid, err := strconv.ParseUint(tenantIDStr, 10, 32); err == nil {
					tenantID = uint(tid)
					tenantIDPtr = &tenantID
				}
			}

			setAuthorizationContext(c, AuthorizationContext{
				SubjectType: "service",
				UserID:      1,
				Username:    "internal-api-call",
				TenantID:    tenantIDPtr,
				AuthType:    "internal_api_key",
				Audiences:   []string{},
				Scopes:      []string{},
			})
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if strings.TrimSpace(authHeader) == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization token"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			c.Abort()
			return
		}

		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, authContextEndpoint, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create verification request"})
			c.Abort()
			return
		}
		req.Header.Set("Authorization", authHeader)

		resp, err := httpClient.Do(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "failed to verify token with system service",
				"details": err.Error(),
			})
			c.Abort()
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			statusCode := http.StatusUnauthorized
			if resp.StatusCode >= http.StatusInternalServerError {
				statusCode = http.StatusServiceUnavailable
			}
			c.JSON(statusCode, gin.H{"error": "authorization context unavailable"})
			c.Abort()
			return
		}

		var authorizationContext AuthorizationContext
		if err := json.NewDecoder(resp.Body).Decode(&authorizationContext); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to decode authorization context"})
			c.Abort()
			return
		}
		if !validAuthorizationContext(authorizationContext) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization context"})
			c.Abort()
			return
		}
		setAuthorizationContext(c, authorizationContext)

		c.Next()
	}
}

func validInternalAPIKey(provided string) bool {
	expected := strings.TrimSpace(os.Getenv("INTERNAL_API_KEY"))
	provided = strings.TrimSpace(provided)
	if expected == "" || provided == "" || len(expected) != len(provided) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func setAuthorizationContext(c *gin.Context, authorizationContext AuthorizationContext) {
	c.Set(ContextUserIDKey, authorizationContext.UserID)
	c.Set(ContextUsernameKey, authorizationContext.Username)
	if authorizationContext.TenantID != nil {
		c.Set(ContextTenantIDKey, *authorizationContext.TenantID)
	} else {
		c.Set(ContextTenantIDKey, uint(0))
	}
	c.Set(ContextAuthorizationContextKey, authorizationContext)
}

func validAuthorizationContext(authorizationContext AuthorizationContext) bool {
	if authorizationContext.UserID == 0 || authorizationContext.SubjectType != "user" {
		return false
	}
	if authorizationContext.AuthType != AuthTypeDelegatedAccessToken {
		return true
	}
	return len(authorizationContext.Audiences) == 1 &&
		len(authorizationContext.Scopes) > 0 &&
		authorizationContext.DelegatedBy != nil && strings.TrimSpace(*authorizationContext.DelegatedBy) != "" &&
		authorizationContext.AgentRunID != nil && strings.TrimSpace(*authorizationContext.AgentRunID) != "" &&
		authorizationContext.ToolCallID != nil && strings.TrimSpace(*authorizationContext.ToolCallID) != ""
}
