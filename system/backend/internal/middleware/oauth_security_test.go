package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	redisv9 "github.com/redis/go-redis/v9"
)

func TestOAuthUserRateLimitPreservesJSONAndReturnsOAuth429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user_id", uint(42)); c.Next() })
	router.POST("/api/v1/system/oauth/authorizations", OAuthUserRateLimitMiddleware(client, 1), func(c *gin.Context) {
		var request struct {
			RequestID string `json:"request_id"`
		}
		if err := c.ShouldBindJSON(&request); err != nil || request.RequestID != "request-1" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "body_lost"})
			return
		}
		c.Status(http.StatusNoContent)
	})

	body := []byte(`{"request_id":"request-1"}`)
	first := performJSONRateLimitRequest(router, body)
	if first.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	second := performJSONRateLimitRequest(router, body)
	if second.Code != http.StatusTooManyRequests || !strings.Contains(second.Body.String(), `"error":"temporarily_unavailable"`) {
		t.Fatalf("second response = %d %s", second.Code, second.Body.String())
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

func TestOAuthRateLimitFailsClosedWithoutRedis(t *testing.T) {
	router := gin.New()
	router.POST("/api/v1/system/oauth/token", OAuthPublicRateLimitMiddleware(nil, 60), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/token", strings.NewReader("client_id=addp-cli"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"error":"temporarily_unavailable"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func performJSONRateLimitRequest(router http.Handler, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/authorizations", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
