package router

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	gateway := httptest.NewServer(r)
	defer gateway.Close()

	paths := []string{
		"/api/v1/system/login",
		"/api/v1/system/refresh",
		"/api/v1/system/logout",
		"/api/v1/system/auth/context",
		"/api/v1/system/oauth/authorizations",
	}
	for _, path := range paths {
		res, err := http.Post(gateway.URL+path, "application/json", nil)
		if err != nil {
			t.Fatalf("%s request failed: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNoContent {
			t.Fatalf("%s returned %d, want %d", path, res.StatusCode, http.StatusNoContent)
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

func TestQueryServiceRouteProxiesCanonicalPostOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var upstreamMethod, upstreamPath, upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamMethod = r.Method
		upstreamPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		upstreamBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	discovery := &fakeModuleProxyLookup{proxies: map[string]*proxy.ServiceProxy{
		"service": proxy.NewServiceProxy(upstream.URL),
	}}
	r := gin.New()
	registerQueryServiceRoute(r, registeredModuleHandler("service", discovery))
	gateway := httptest.NewServer(r)
	defer gateway.Close()

	body := `{"page":{"limit":10}}`
	response, err := http.Post(gateway.URL+"/api/query/f2/query", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST query route: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("POST status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if upstreamMethod != http.MethodPost || upstreamPath != "/api/query/f2/query" || upstreamBody != body {
		t.Fatalf("upstream request = %s %s body=%q", upstreamMethod, upstreamPath, upstreamBody)
	}

	legacyResponse, err := http.Get(gateway.URL + "/api/query/f2")
	if err != nil {
		t.Fatalf("GET legacy query route: %v", err)
	}
	legacyResponse.Body.Close()
	if legacyResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("legacy GET status = %d, want %d", legacyResponse.StatusCode, http.StatusNotFound)
	}
}
