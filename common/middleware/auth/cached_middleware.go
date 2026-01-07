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

// CachedSystemAuthMiddleware is similar to SystemAuthMiddleware but caches user info in Redis.
// This significantly reduces load on the System service by avoiding repeated /api/users/me calls
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
// - systemURL: System service base URL (e.g., "http://localhost:8080")
// - redisClient: Initialized Redis client
// - cacheTTL: Time-to-live for cached user info (recommended: 5 minutes)
//
// Usage:
//   redisClient := redis.NewClient(&redis.Options{...})
//   router.Use(auth.CachedSystemAuthMiddleware("http://localhost:8080", redisClient, 5*time.Minute))
func CachedSystemAuthMiddleware(systemURL string, redisClient *redis.Client, cacheTTL time.Duration) gin.HandlerFunc {
	baseURL := strings.TrimSuffix(systemURL, "/")
	meEndpoint := baseURL + "/api/users/me"
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	return func(c *gin.Context) {
		// 0. Check for internal API key (for service-to-service calls)
		if internalKey := c.GetHeader("X-Internal-API-Key"); internalKey != "" {
			// TODO: Validate internal key against environment variable
			// For now, trust any internal key (in production, add validation)

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
			c.Set(ContextUserIDKey, uint(1))        // System user ID
			c.Set(ContextUsernameKey, "internal-api-call")
			c.Set(ContextTenantIDKey, tenantID)     // Use tenant_id from header if provided
			c.Set(ContextUserInfoKey, UserInfo{
				ID:       1,
				Username: "internal-api-call",
				TenantID: tenantIDPtr,
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
			var userInfo UserInfo
			if err := json.Unmarshal([]byte(cachedData), &userInfo); err == nil {
				// Inject cached user info into Gin context
				c.Set(ContextUserIDKey, userInfo.ID)
				c.Set(ContextUsernameKey, userInfo.Username)
				if userInfo.TenantID != nil {
					c.Set(ContextTenantIDKey, *userInfo.TenantID)
				} else {
					c.Set(ContextTenantIDKey, uint(0))
				}
				c.Set(ContextUserInfoKey, userInfo)
				c.Next()
				return
			}
			// If unmarshal failed, fall through to System service call
		}

		// 4. Cache miss or Redis error → call System service
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

		// 5. Cache user info in Redis for future requests
		userInfoJSON, err := json.Marshal(userInfo)
		if err == nil {
			// Best-effort caching (don't fail request if Redis write fails)
			_ = redisClient.Set(ctx, cacheKey, userInfoJSON, cacheTTL).Err()
		}

		// 6. Inject user info into Gin context
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

// generateTokenCacheKey creates a deterministic cache key from a token using SHA-256 hash.
// This avoids storing plaintext tokens in Redis while ensuring uniqueness.
//
// Key format: "auth:token_cache:<sha256_hash>"
//
// Example:
//   token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
//   cache key: "auth:token_cache:3a4f8c2b1e7d9f5a..."
func generateTokenCacheKey(token string) string {
	hash := sha256.Sum256([]byte(token))
	hashHex := hex.EncodeToString(hash[:])
	return fmt.Sprintf("auth:token_cache:%s", hashHex)
}
