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

func TestSystemAuthMiddlewareRejectsDelegatedContextWithoutAuditBinding(t *testing.T) {
	tenantID := uint(3)
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(AuthorizationContext{
			SubjectType: "user",
			UserID:      12,
			Username:    "alice",
			UserType:    "user",
			TenantID:    &tenantID,
			AuthType:    AuthTypeDelegatedAccessToken,
			Audiences:   []string{"develop"},
			Scopes:      []string{"workflow.validate"},
			IssuedAt:    time.Now().Add(-time.Minute),
			ExpiresAt:   time.Now().Add(time.Minute),
		})
	}))
	defer systemServer.Close()

	router := gin.New()
	router.Use(SystemAuthMiddleware(systemServer.URL))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusOK) })
	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	request.Header.Set("Authorization", "Bearer addp_dat_test")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestSystemAuthMiddlewareRejectsQueryAccessToken(t *testing.T) {
	systemCalls := 0
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		systemCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer systemServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SystemAuthMiddleware(systemServer.URL))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/resource?token=user-token", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || systemCalls != 0 {
		t.Fatalf("status = %d, system calls = %d", response.Code, systemCalls)
	}
}

func TestBrowserResourceAccessMiddlewareAcceptsOnlyAllowedOwnerRoute(t *testing.T) {
	tenantID := uint(3)
	systemCalls := 0
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		systemCalls++
		if r.Header.Get("Authorization") != "Bearer addp_rat_manager" {
			t.Fatalf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(AuthorizationContext{
			SubjectType: "user",
			UserID:      12,
			Username:    "alice",
			TenantID:    &tenantID,
			AuthType:    AuthTypeResourceAccessTicket,
			Audiences:   []string{"manager"},
			Scopes:      []string{BrowserResourceAccessScope},
			IssuedAt:    time.Now().Add(-time.Minute),
			ExpiresAt:   time.Now().Add(time.Minute),
		})
	}))
	defer systemServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(BrowserResourceAccessMiddleware(systemServer.URL, nil, "manager", func(c *gin.Context) bool {
		return c.Request.URL.Path == "/resource"
	}))
	router.Use(SystemAuthMiddleware(systemServer.URL))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/ordinary", func(c *gin.Context) { c.Status(http.StatusOK) })

	allowedRequest := httptest.NewRequest(http.MethodGet, "/resource", nil)
	allowedRequest.AddCookie(&http.Cookie{Name: BrowserResourceAccessTicketCookieName, Value: "addp_rat_manager"})
	allowedResponse := httptest.NewRecorder()
	router.ServeHTTP(allowedResponse, allowedRequest)
	if allowedResponse.Code != http.StatusOK || systemCalls != 1 {
		t.Fatalf("allowed status = %d, system calls = %d", allowedResponse.Code, systemCalls)
	}

	disallowedRequest := httptest.NewRequest(http.MethodGet, "/ordinary", nil)
	disallowedRequest.AddCookie(&http.Cookie{Name: BrowserResourceAccessTicketCookieName, Value: "addp_rat_manager"})
	disallowedResponse := httptest.NewRecorder()
	router.ServeHTTP(disallowedResponse, disallowedRequest)
	if disallowedResponse.Code != http.StatusUnauthorized || systemCalls != 1 {
		t.Fatalf("disallowed status = %d, system calls = %d", disallowedResponse.Code, systemCalls)
	}

	disallowedMethodRequest := httptest.NewRequest(http.MethodPost, "/resource", nil)
	disallowedMethodRequest.AddCookie(&http.Cookie{Name: BrowserResourceAccessTicketCookieName, Value: "addp_rat_manager"})
	disallowedMethodResponse := httptest.NewRecorder()
	router.ServeHTTP(disallowedMethodResponse, disallowedMethodRequest)
	if disallowedMethodResponse.Code != http.StatusUnauthorized || systemCalls != 1 {
		t.Fatalf("disallowed method status = %d, system calls = %d", disallowedMethodResponse.Code, systemCalls)
	}
}

func TestBrowserResourceAccessMiddlewareRejectsWrongOwnerContext(t *testing.T) {
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(AuthorizationContext{
			SubjectType: "user",
			UserID:      12,
			AuthType:    AuthTypeResourceAccessTicket,
			Audiences:   []string{"standard"},
			Scopes:      []string{BrowserResourceAccessScope},
			IssuedAt:    time.Now().Add(-time.Minute),
			ExpiresAt:   time.Now().Add(time.Minute),
		})
	}))
	defer systemServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(BrowserResourceAccessMiddleware(systemServer.URL, nil, "manager", func(*gin.Context) bool { return true }))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	request.AddCookie(&http.Cookie{Name: BrowserResourceAccessTicketCookieName, Value: "addp_rat_standard"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
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
