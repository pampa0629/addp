package middleware

import (
	"fmt"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/infra/redistest"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestOAuthRateLimitAgainstRedis(t *testing.T) {
	cfg := redistest.Config(t)
	admin := redistest.Open(t, cfg)
	proxy := redistest.NewProxy(t, net.JoinHostPort(cfg.Host, cfg.Port))
	client := redistest.Open(t, proxy.Config(cfg))
	calls := 0
	router := func(client *redis.Client) *gin.Engine {
		r := gin.New()
		r.Use(OAuthPublicRateLimitMiddleware(client, 2))
		r.POST("/token", func(c *gin.Context) { calls++; c.JSON(200, gin.H{"access_token": "fixture-success"}) })
		return r
	}
	request := func(r *gin.Engine, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/token", nil))
		if w.Code != want {
			t.Fatalf("%d %s want %d", w.Code, w.Body.String(), want)
		}
		if want == 503 && calls > 2 {
			t.Fatal("unavailable token handler called")
		}
	}
	r := router(client)
	request(r, 200)
	proxy.SetAvailable(false)
	request(r, 503)
	if calls != 1 {
		t.Fatal("dependency failure issued token")
	}
	proxy.SetAvailable(true)
	request(r, 200)
	request(r, 429)
	if calls != 2 {
		t.Fatal("OAuth counter did not survive reconnect")
	}
	user := fmt.Sprintf("r1_oauth_%d", time.Now().UnixNano())
	if err := admin.Do(t.Context(), "ACL", "SETUSER", user, "reset", "on", ">probe-password", "+ping").Err(); err != nil {
		t.Fatal(err)
	}
	defer admin.Do(t.Context(), "ACL", "DELUSER", user)
	restricted := redis.NewClient(&redis.Options{Addr: admin.Options().Addr, Username: user, Password: "probe-password", MaxRetries: -1})
	defer restricted.Close()
	if err := restricted.Ping(t.Context()).Err(); err != nil {
		t.Fatal(err)
	}
	request(router(restricted), 503)
	if calls != 2 {
		t.Fatal("ACL failure issued token")
	}
}
