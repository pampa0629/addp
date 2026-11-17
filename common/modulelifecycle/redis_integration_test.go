package modulelifecycle

import (
	"github.com/addp/common/infra/redistest"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedisReadinessAgainstRedis(t *testing.T) {
	cfg := redistest.Config(t)
	proxy := redistest.NewProxy(t, net.JoinHostPort(cfg.Host, cfg.Port))
	client := redistest.Open(t, proxy.Config(cfg))
	controller := NewStandalone("redis-gate", RedisCheck(client))
	router := NewRouter()
	controller.RegisterHealthRoutes(router)
	router.Use(controller.RequireReady())
	calls := 0
	router.GET("/business", func(c *gin.Context) { calls++; c.Status(204) })
	request := func(path string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	request("/health/ready", 200)
	request("/business", 204)
	proxy.SetAvailable(false)
	request("/health/live", 200)
	request("/health/ready", 503)
	request("/business", 503)
	if calls != 1 {
		t.Fatal("unready process served business")
	}
	proxy.SetAvailable(true)
	request("/health/ready", 200)
	request("/business", 204)
	if calls != 2 {
		t.Fatal("same client did not recover")
	}
}
