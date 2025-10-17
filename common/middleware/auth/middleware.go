package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Context keys used to store authenticated user data.
const (
	ContextUserIDKey   = "user_id"
	ContextUsernameKey = "username"
	ContextTenantIDKey = "tenant_id"
	ContextUserInfoKey = "user_info"
)

// UserInfo mirrors the payload returned by the System service /api/users/me endpoint.
type UserInfo struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	TenantID *uint  `json:"tenant_id"`
}

// SystemAuthMiddleware validates the incoming request token by delegating to the System service.
// On success it stores the resolved user information in the Gin context using the constants defined above.
func SystemAuthMiddleware(systemURL string) gin.HandlerFunc {
	baseURL := strings.TrimSuffix(systemURL, "/")
	meEndpoint := baseURL + "/api/users/me"
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if strings.TrimSpace(authHeader) == "" {
			if tokenParam := strings.TrimSpace(c.Query("token")); tokenParam != "" {
				authHeader = "Bearer " + tokenParam
				c.Request.Header.Set("Authorization", authHeader)
			}
		}
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

		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, meEndpoint, nil)
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
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10)) // cap to 4KB
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "invalid token",
				"details": string(body),
			})
			c.Abort()
			return
		}

		var userInfo UserInfo
		if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decode system user response"})
			c.Abort()
			return
		}

		c.Set(ContextUserIDKey, userInfo.ID)
		c.Set(ContextUsernameKey, userInfo.Username)
		if userInfo.TenantID != nil {
			c.Set(ContextTenantIDKey, *userInfo.TenantID)
		} else {
			c.Set(ContextTenantIDKey, uint(0))
		}
		c.Set(ContextUserInfoKey, userInfo)

		c.Next()
	}
}
