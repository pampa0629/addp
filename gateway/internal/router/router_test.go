package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/gateway/internal/proxy"
	"github.com/gin-gonic/gin"
)

type fakeModuleProxyLookup struct {
	proxies map[string]*proxy.ServiceProxy
	calls   []string
}

func (f *fakeModuleProxyLookup) GetProxy(moduleName string) (*proxy.ServiceProxy, error) {
	f.calls = append(f.calls, moduleName)
	p, ok := f.proxies[moduleName]
	if !ok {
		return nil, fmt.Errorf("module unavailable")
	}
	return p, nil
}

func TestModuleRouteUsesSystemBootstrapForEverySystemPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	discovery := &fakeModuleProxyLookup{proxies: map[string]*proxy.ServiceProxy{}}
	r := gin.New()
	api := r.Group("/api/v1")
	registerModuleRoutes(api, proxy.NewServiceProxy(upstream.URL), discovery)

	paths := []string{
		"/api/v1/system/login",
		"/api/v1/system/refresh",
		"/api/v1/system/logout",
		"/api/v1/system/auth/context",
		"/api/v1/system/oauth/authorizations",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if res.Code != http.StatusNoContent {
			t.Fatalf("%s returned %d, want %d", path, res.Code, http.StatusNoContent)
		}
	}

	if len(discovery.calls) != 0 {
		t.Fatalf("system routes unexpectedly used module discovery: %v", discovery.calls)
	}
}

func TestModuleRouteReturnsServiceUnavailableWithoutStaticFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	discovery := &fakeModuleProxyLookup{proxies: map[string]*proxy.ServiceProxy{}}
	r := gin.New()
	api := r.Group("/api/v1")
	registerModuleRoutes(api, proxy.NewServiceProxy("http://system.invalid"), discovery)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/manager/engines", nil)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}
	if len(discovery.calls) != 1 || discovery.calls[0] != "manager" {
		t.Fatalf("module discovery calls = %v, want [manager]", discovery.calls)
	}
}
