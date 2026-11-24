package router

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/addp/gateway/internal/proxy"
	"github.com/gin-gonic/gin"
)

type fakeModuleProxyLookup struct {
	proxies   map[string]*proxy.ServiceProxy
	calls     []string
	alternate *proxy.ServiceProxy
}

func (f *fakeModuleProxyLookup) AlternateProxy(moduleName, excludeURL string) *proxy.ServiceProxy {
	return f.alternate
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
	r.POST("/api/query/:serviceName/query", registeredModuleHandler("service", discovery))
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

func TestCatalogReadSwitchesOnlyBeforeDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const entries = "/api/v1/catalog/entries"
	var calls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.RequestURI() != entries+"?page=1" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Errorf("alternate received changed request")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	var received atomic.Int32
	accepted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		if r.URL.Query().Has("disconnect") {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer accepted.Close()
	for _, tc := range []struct {
		name, primary, method, uri, body string
		want                             int
		switched                         bool
	}{
		{"dial failure", closed.URL, "GET", entries + "?page=1", "", 200, true},
		{"business failure", accepted.URL, "GET", entries, "", 503, false},
		{"disconnect after dispatch", accepted.URL, "GET", entries + "?disconnect=1", "", 502, false},
		{"write", closed.URL, "POST", entries, "{}", 502, false},
		{"body", closed.URL, "GET", entries, "{}", 502, false},
		{"other read", closed.URL, "GET", entries + "/audit", "", 502, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			discovery := &fakeModuleProxyLookup{proxies: map[string]*proxy.ServiceProxy{"catalog": proxy.NewServiceProxy(tc.primary)}, alternate: proxy.NewServiceProxy(second.URL)}
			router := gin.New()
			registerModuleRoutes(router.Group("/api/v1"), nil, discovery)
			server := httptest.NewServer(router)
			defer server.Close()
			req, err := http.NewRequest(tc.method, server.URL+tc.uri, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer fixture")
			before := calls.Load()
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			wantCalls := int32(0)
			if tc.switched {
				wantCalls = 1
			}
			if response.StatusCode != tc.want || calls.Load()-before != wantCalls {
				t.Fatalf("HTTP %d; alternate calls %d", response.StatusCode, calls.Load()-before)
			}
		})
	}
	if received.Load() != 2 {
		t.Fatalf("requests delivered to accepting primary = %d", received.Load())
	}
}
