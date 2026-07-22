package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	redisv9 "github.com/redis/go-redis/v9"
)

func TestRedisFixedWindowMiddlewareSharesLimitAcrossInstances(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	newRouter := func() *gin.Engine {
		middleware, err := RedisFixedWindowMiddleware(client, RedisFixedWindowOptions{
			Prefix: "test:shared", Period: time.Minute, Limit: 2,
			KeyGetter: func(c *gin.Context) string { return c.GetHeader("X-Test-Key") },
			OnLimitReached: func(c *gin.Context, _ int64) {
				c.JSON(http.StatusTooManyRequests, gin.H{"error": "limited"})
				c.Abort()
			},
		})
		if err != nil {
			t.Fatalf("create middleware: %v", err)
		}
		router := gin.New()
		router.GET("/", middleware, func(c *gin.Context) { c.Status(http.StatusNoContent) })
		return router
	}

	for index, router := range []*gin.Engine{newRouter(), newRouter(), newRouter()} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("X-Test-Key", "same-client")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		want := http.StatusNoContent
		if index == 2 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("request %d status = %d, want %d", index+1, response.Code, want)
		}
	}
}

func TestRedisFixedWindowMiddlewareFailsClosedWhenRedisUnavailable(t *testing.T) {
	middleware, err := RedisFixedWindowMiddleware(nil, RedisFixedWindowOptions{
		Prefix: "test:unavailable", Period: time.Minute, Limit: 1,
		KeyGetter: func(*gin.Context) string { return "client" },
		OnUnavailable: func(c *gin.Context, err error) {
			if err == nil {
				t.Fatal("expected redis error")
			}
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
			c.Abort()
		},
	})
	if err != nil {
		t.Fatalf("create middleware: %v", err)
	}
	router := gin.New()
	router.GET("/", middleware, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func TestRedisFixedWindowMiddlewareRejectsInvalidOptions(t *testing.T) {
	_, err := RedisFixedWindowMiddleware(nil, RedisFixedWindowOptions{})
	if err == nil {
		t.Fatal("expected invalid options error")
	}
}
