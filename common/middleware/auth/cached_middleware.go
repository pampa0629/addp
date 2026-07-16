package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// CachedSystemAuthMiddleware is similar to SystemAuthMiddleware but caches authorization contexts in Redis.
// This significantly reduces load on the System service by avoiding repeated /api/v1/system/auth/context calls
// for the same token within the TTL window.
//
// Architecture:
// 1. Extract token from Authorization header or query parameter
// 2. Check Redis cache using hash(token) as key
// 3. If cache hit → use cached user info (no System call)
// 4. If cache miss → call System service, cache result for future requests
//
// Performance improvement: 90%+ reduction in System service calls (when cache hit rate > 90%)
//
// Parameters:
// - systemURL: System service base URL (e.g., "http://localhost:8180")
// - redisClient: Initialized Redis client
// - cacheTTL: Time-to-live for cached user info (recommended: 5 minutes)
//
// Usage:
//
//	redisClient := redis.NewClient(&redis.Options{...})
//	router.Use(auth.CachedSystemAuthMiddleware("http://localhost:8180", redisClient, 5*time.Minute))
func CachedSystemAuthMiddleware(systemURL string, redisClient *redis.Client, cacheTTL time.Duration) gin.HandlerFunc {
	baseURL := strings.TrimSuffix(systemURL, "/")
	authContextEndpoint := baseURL + "/api/v1/system/auth/context"
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	return func(c *gin.Context) {
		// 0. Check for internal API key (for service-to-service calls)
		if internalKey := c.GetHeader("X-Internal-API-Key"); internalKey != "" {
			if !validInternalAPIKey(internalKey) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid internal api key"})
				c.Abort()
				return
			}

			// 从请求头读取 tenant_id（如果提供）
			tenantID := uint(0)
			var tenantIDPtr *uint
			if tenantIDStr := c.GetHeader("X-Tenant-ID"); tenantIDStr != "" {
				if tid, err := strconv.ParseUint(tenantIDStr, 10, 32); err == nil {
					tenantID = uint(tid)
					tenantIDPtr = &tenantID
				}
			}

			// Set a system user context for internal API calls
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

		// 1. Extract token (same logic as SystemAuthMiddleware)
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

		token := parts[1]

		// 2. Generate cache key from token hash (avoid storing plaintext tokens in Redis)
		cacheKey := generateTokenCacheKey(token)

		// 3. Try to get cached user info from Redis
		ctx := context.Background()
		cachedData, err := redisClient.Get(ctx, cacheKey).Result()

		if err == nil && cachedData != "" {
			// Cache hit! Decode and use cached user info
			var authorizationContext AuthorizationContext
			if err := json.Unmarshal([]byte(cachedData), &authorizationContext); err == nil &&
				authorizationContext.ExpiresAt.After(time.Now()) {
				setAuthorizationContext(c, authorizationContext)
				c.Next()
				return
			}
			_ = redisClient.Del(ctx, cacheKey).Err()
			// If unmarshal failed, fall through to System service call
		}

		// 4. Cache miss or Redis error → call System service
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
		if authorizationContext.UserID == 0 || authorizationContext.SubjectType != "user" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization context"})
			c.Abort()
			return
		}

		// 5. Cache the context no longer than the access token remains valid.
		authorizationContextJSON, err := json.Marshal(authorizationContext)
		remainingTTL := time.Until(authorizationContext.ExpiresAt)
		if err == nil && remainingTTL > 0 {
			effectiveTTL := cacheTTL
			if remainingTTL < effectiveTTL {
				effectiveTTL = remainingTTL
			}
			// Best-effort caching (don't fail request if Redis write fails)
			_ = redisClient.Set(ctx, cacheKey, authorizationContextJSON, effectiveTTL).Err()
		}

		// 6. Inject the canonical context.
		setAuthorizationContext(c, authorizationContext)

		c.Next()
	}
}

// generateTokenCacheKey creates a deterministic cache key from a token using SHA-256 hash.
// This avoids storing plaintext tokens in Redis while ensuring uniqueness.
//
// Key format: "auth:context:<sha256_hash>"
//
// Example:
//
//	token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
//	cache key: "auth:context:3a4f8c2b1e7d9f5a..."
func generateTokenCacheKey(token string) string {
	hash := sha256.Sum256([]byte(token))
	hashHex := hex.EncodeToString(hash[:])
	return fmt.Sprintf("auth:context:%s", hashHex)
}
