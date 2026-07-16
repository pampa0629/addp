package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestSystemAuthMiddlewareUsesAuthorizationContextEndpoint(t *testing.T) {
	tenantID := uint(3)
	var requestedPath string
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer user-token" {
			t.Fatalf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(AuthorizationContext{
			SubjectType: "user",
			UserID:      12,
			Username:    "alice",
			UserType:    "tenant_admin",
			TenantID:    &tenantID,
			AuthType:    "first_party_access_token",
			Audiences:   []string{},
			Scopes:      []string{},
			IssuedAt:    time.Now().Add(-time.Minute),
			ExpiresAt:   time.Now().Add(time.Minute),
		})
	}))
	defer systemServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SystemAuthMiddleware(systemServer.URL))
	router.GET("/resource", func(c *gin.Context) {
		context, ok := GetAuthorizationContext(c)
		if !ok {
			t.Fatal("authorization context missing")
		}
		c.JSON(http.StatusOK, gin.H{
			"user_id":   GetUserID(c),
			"tenant_id": GetTenantID(c),
			"user_type": context.UserType,
		})
	})

	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if requestedPath != "/api/v1/system/auth/context" {
		t.Fatalf("requested path = %q", requestedPath)
	}
}

func TestSystemAuthMiddlewareRejectsInvalidContext(t *testing.T) {
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(AuthorizationContext{SubjectType: "user"})
	}))
	defer systemServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SystemAuthMiddleware(systemServer.URL))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestSystemAuthMiddlewareValidatesInternalAPIKey(t *testing.T) {
	t.Setenv("INTERNAL_API_KEY", "expected-internal-key")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SystemAuthMiddleware("http://system.invalid"))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusOK) })

	invalidRequest := httptest.NewRequest(http.MethodGet, "/resource", nil)
	invalidRequest.Header.Set("X-Internal-API-Key", "wrong-key")
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusUnauthorized {
		t.Fatalf("invalid internal key status = %d", invalidResponse.Code)
	}

	validRequest := httptest.NewRequest(http.MethodGet, "/resource", nil)
	validRequest.Header.Set("X-Internal-API-Key", "expected-internal-key")
	validResponse := httptest.NewRecorder()
	router.ServeHTTP(validResponse, validRequest)
	if validResponse.Code != http.StatusOK {
		t.Fatalf("valid internal key status = %d, body = %s", validResponse.Code, validResponse.Body.String())
	}
}

func TestCachedSystemAuthMiddlewareCapsTTLAtTokenExpiry(t *testing.T) {
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	tenantID := uint(3)
	expiresAt := time.Now().Add(30 * time.Second)
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(AuthorizationContext{
			SubjectType: "user",
			UserID:      12,
			Username:    "alice",
			UserType:    "tenant_admin",
			TenantID:    &tenantID,
			AuthType:    "first_party_access_token",
			Audiences:   []string{},
			Scopes:      []string{},
			IssuedAt:    time.Now().Add(-time.Minute),
			ExpiresAt:   expiresAt,
		})
	}))
	defer systemServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CachedSystemAuthMiddleware(systemServer.URL, redisClient, 5*time.Minute))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cacheTTL := mini.TTL(generateTokenCacheKey("user-token"))
	if cacheTTL <= 0 || cacheTTL > 30*time.Second {
		t.Fatalf("cache TTL = %s, want no more than token lifetime", cacheTTL)
	}
}

func TestCachedSystemAuthMiddlewareDeletesExpiredContext(t *testing.T) {
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	expiredContext, err := json.Marshal(AuthorizationContext{
		SubjectType: "user",
		UserID:      12,
		Username:    "alice",
		ExpiresAt:   time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("marshal expired context: %v", err)
	}
	cacheKey := generateTokenCacheKey("expired-token")
	mini.Set(cacheKey, string(expiredContext))
	mini.SetTTL(cacheKey, 5*time.Minute)

	systemCalls := 0
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		systemCalls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer systemServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CachedSystemAuthMiddleware(systemServer.URL, redisClient, 5*time.Minute))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	request.Header.Set("Authorization", "Bearer expired-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if systemCalls != 1 {
		t.Fatalf("system calls = %d, want 1", systemCalls)
	}
	if mini.Exists(cacheKey) {
		t.Fatal("expired authorization context remains cached")
	}
}
