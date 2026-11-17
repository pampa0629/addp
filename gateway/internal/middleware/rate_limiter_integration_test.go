package middleware

import (
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/infra/redistest"
	gatewayclient "github.com/addp/gateway/pkg/client"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestConsumerRateLimitAgainstRedis(t *testing.T) {
	cfg := redistest.Config(t)
	admin := redistest.Open(t, cfg)
	proxy := redistest.NewProxy(t, net.JoinHostPort(cfg.Host, cfg.Port))
	connection := redistest.Open(t, proxy.Config(cfg))
	id := uint(time.Now().UnixNano() & 0x7fffffff)
	key := fmt.Sprintf("ratelimit:api-consumer:%d", id)
	t.Cleanup(func() { _ = admin.Del(t.Context(), key).Err() })
	calls := 0
	route := func(client *redis.Client) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("api_consumer_info", &gatewayclient.APIConsumerCredentialValidationResponse{APIConsumerID: id, RateLimitPerMinute: 2})
		})
		r.Use(NewRateLimiterMiddleware(client).Handler())
		r.GET("/query", func(c *gin.Context) { calls++; c.Status(204) })
		return r
	}
	request := func(r *gin.Engine, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/query", nil))
		if w.Code != want {
			t.Fatalf("got %d %s want %d", w.Code, w.Body.String(), want)
		}
	}
	r := route(connection)
	request(r, 204)
	ttl, err := admin.TTL(t.Context(), key).Result()
	if err != nil || ttl <= 0 || ttl > time.Minute {
		t.Fatal("fixed 60 second window lost", ttl, err)
	}
	proxy.SetAvailable(false)
	request(r, 503)
	if calls != 1 {
		t.Fatal("failed limiter reached downstream")
	}
	proxy.SetAvailable(true)
	request(r, 204)
	request(r, 429)
	if count, _ := admin.Get(t.Context(), key).Int(); count != 3 || calls != 2 {
		t.Fatal("counter was reset during network recovery", count, calls)
	}
	// Ping alone is not a proof of Eval/ACL availability.
	user := fmt.Sprintf("r1_probe_%d", id)
	if err := admin.Do(t.Context(), "ACL", "SETUSER", user, "reset", "on", ">probe-password", "+ping").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Do(t.Context(), "ACL", "DELUSER", user).Err() })
	restricted := redis.NewClient(&redis.Options{Addr: admin.Options().Addr, Username: user, Password: "probe-password", MaxRetries: -1})
	defer restricted.Close()
	if err := restricted.Ping(t.Context()).Err(); err != nil {
		t.Fatal(err)
	}
	request(route(restricted), 503)
	if calls != 2 {
		t.Fatal("ACL failure bypassed limiter")
	}
	// Transport errors must not expose addresses, credentials or raw server errors.
	w := httptest.NewRecorder()
	route(restricted).ServeHTTP(w, httptest.NewRequest("GET", "/query", nil))
	if strings.Contains(w.Body.String(), "NOPERM") {
		t.Fatal("raw Redis error leaked")
	}
}
