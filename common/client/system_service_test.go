package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func TestRegisterRuntimeEngineUsesPlatformBearer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/system/runtime/engines" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer platform-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var request models.CapabilityRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.EngineType != "duckdb" || !request.IsBuiltin || request.ConnectionInfo["port"] != float64(8104) {
			t.Fatalf("runtime registration = %#v", request)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("platform-token"), server.Client())
	if err := client.RegisterRuntimeEngine(context.Background(), &models.CapabilityRegistrationRequest{
		Name: "DuckDB", EngineType: "duckdb", IsBuiltin: true,
		ConnectionInfo: map[string]interface{}{"protocol": "http", "port": 8104},
	}); err != nil {
		t.Fatalf("RegisterRuntimeEngine() error = %v", err)
	}
}

func TestRegisterRuntimeEngineWithRetryDoesNotBlockAndRecovers(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	registered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(w, "system not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		select {
		case <-registered:
		default:
			close(registered)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("platform-token"), server.Client())
	started := time.Now()
	client.RegisterRuntimeEngineWithRetry(ctx, &models.CapabilityRegistrationRequest{
		Name: "Inference", EngineType: "inference_runtime", IsBuiltin: true,
		ConnectionInfo: map[string]interface{}{"protocol": "http", "port": 8191},
	}, time.Millisecond, 2*time.Millisecond)
	if time.Since(started) > 20*time.Millisecond {
		t.Fatal("RegisterRuntimeEngineWithRetry blocked the caller")
	}
	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatalf("runtime was not registered; attempts=%d", attempts.Load())
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
}

func TestRegisterAndHeartbeatGeneratesBackendInstanceDeclaration(t *testing.T) {
	t.Parallel()

	registered := make(chan ModuleRegistrationRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/system/runtime/modules" {
			http.NotFound(w, r)
			return
		}
		var request ModuleRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		registered <- request
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("platform-token"), server.Client())
	client.RegisterAndHeartbeat(ctx, &ModuleRegistrationRequest{
		ModuleName: "manager", ModuleURL: "http://manager:8080", RoutePrefix: "/manager",
		Metadata: map[string]interface{}{"version": "test"},
	})
	select {
	case request := <-registered:
		cancel()
		if request.InstanceID == "" || request.Role != ModuleRuntimeRoleBackend {
			t.Fatalf("runtime identity = %#v", request)
		}
		if request.Metadata["module"] != "manager" || request.Metadata["version"] != "test" {
			t.Fatalf("runtime metadata = %#v", request.Metadata)
		}
	case <-time.After(time.Second):
		t.Fatal("module registration was not sent")
	}
}

func TestListActiveModulesRequestsRoutableRegistryProjection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/system/runtime/modules" || r.URL.Query().Get("status") != "up" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"modules": []ModuleInfo{{ModuleName: "manager", Enabled: true}},
			"count":   1,
		})
	}))
	defer server.Close()

	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("platform-token"), server.Client())
	modules, err := client.ListActiveModules(context.Background())
	if err != nil || len(modules) != 1 || modules[0].ModuleName != "manager" {
		t.Fatalf("ListActiveModules() modules=%#v error=%v", modules, err)
	}
}

func TestWatchActiveModulesSendsRevisionAndWait(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/system/runtime/modules/watch" ||
			r.URL.Query().Get("revision") != "7" || r.URL.Query().Get("wait_seconds") != "3" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(ModuleRoutingSnapshot{
			Revision: 8, Modules: []*ModuleInfo{{ModuleName: "manager", Enabled: true}},
		})
	}))
	defer server.Close()

	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("platform-token"), server.Client())
	snapshot, err := client.WatchActiveModules(context.Background(), 7, 3*time.Second)
	if err != nil || snapshot.Revision != 8 || len(snapshot.Modules) != 1 {
		t.Fatalf("WatchActiveModules() snapshot=%#v error=%v", snapshot, err)
	}
}

func TestRegisterAndHeartbeatReregistersImmediatelyAndDeregistersOnShutdown(t *testing.T) {
	t.Parallel()
	var registrations atomic.Int32
	reregistered := make(chan struct{})
	deregistered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/system/runtime/modules":
			count := registrations.Add(1)
			w.WriteHeader(http.StatusOK)
			if count == 2 {
				close(reregistered)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/system/runtime/modules/heartbeat":
			http.Error(w, "instance missing", http.StatusNotFound)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/system/runtime/modules/manager/instances/"):
			close(deregistered)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("platform-token"), server.Client())
	client.registerAndHeartbeat(ctx, &ModuleRegistrationRequest{
		ModuleName: "manager", ModuleURL: "http://manager:8081", RoutePrefix: "/manager",
	}, time.Millisecond, 2*time.Millisecond, time.Millisecond)
	select {
	case <-reregistered:
	case <-time.After(time.Second):
		t.Fatal("heartbeat failure did not trigger immediate re-registration")
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-deregistered:
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not deregister the runtime instance")
	}
}

func TestSystemServiceClientRefreshesRejectedContextTokenOnce(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	tokenRequests := map[string]int{}
	apiRequests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			contextKey := "tenant:" + r.Form.Get("tenant_id")
			if r.Form.Get("context_type") == "platform" {
				contextKey = "platform"
			}
			mu.Lock()
			tokenRequests[contextKey]++
			sequence := tokenRequests[contextKey]
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "addp_at_" + strings.ReplaceAll(contextKey, ":", "_") + "_" + strconv.Itoa(sequence),
				"token_type":   "Bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}

		mu.Lock()
		apiRequests[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/system/engines/12":
			if r.Header.Get("Authorization") == "Bearer addp_at_tenant_7_1" {
				http.Error(w, "authorization version changed", http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer addp_at_tenant_7_2" {
				t.Fatalf("tenant Authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(models.Engine{ID: 12})
		case "/api/v1/system/engines/13":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/api/v1/system/runtime/modules":
			if r.Header.Get("Authorization") == "Bearer addp_at_platform_1" {
				http.Error(w, "authorization version changed", http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer addp_at_platform_2" {
				t.Fatalf("platform Authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-meta", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := NewSystemServiceClient(server.URL, source, server.Client())
	engine, err := client.WithTenantID(7).GetEngine(context.Background(), 12)
	if err != nil || engine.ID != 12 {
		t.Fatalf("GetEngine() engine=%#v error=%v", engine, err)
	}
	if err := client.RegisterModule(context.Background(), &ModuleRegistrationRequest{ModuleName: "meta"}); err != nil {
		t.Fatalf("RegisterModule() error = %v", err)
	}
	if _, err := client.WithTenantID(7).GetEngine(context.Background(), 13); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("forbidden GetEngine() error = %v", err)
	}
	if tokenRequests["tenant:7"] != 2 || tokenRequests["platform"] != 2 ||
		apiRequests["/api/v1/system/engines/12"] != 2 || apiRequests["/api/v1/system/runtime/modules"] != 2 ||
		apiRequests["/api/v1/system/engines/13"] != 1 {
		t.Fatalf("token requests=%v API requests=%v", tokenRequests, apiRequests)
	}
}

func TestSystemServiceClientUsesTenantAndPlatformBearerWithoutLegacyHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			suffix := r.Form.Get("tenant_id")
			if r.Form.Get("context_type") == "platform" {
				suffix = "platform"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "addp_at_" + suffix, "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("System service request sent legacy authentication headers")
		}
		switch r.URL.Path {
		case "/api/v1/system/engines/12":
			if r.Header.Get("Authorization") != "Bearer addp_at_7" {
				t.Fatalf("tenant Authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(models.Engine{ID: 12})
		case "/api/v1/system/runtime/modules":
			if r.Header.Get("Authorization") != "Bearer addp_at_platform" {
				t.Fatalf("platform Authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-meta", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := NewSystemServiceClient(server.URL, source, server.Client())
	if _, err := client.WithTenantID(7).GetEngine(context.Background(), 12); err != nil {
		t.Fatalf("GetEngine() error = %v", err)
	}
	if err := client.RegisterModule(context.Background(), &ModuleRegistrationRequest{ModuleName: "meta"}); err != nil {
		t.Fatalf("RegisterModule() error = %v", err)
	}
}

func TestSystemServiceClientListsCatalogChildrenWithTenantBearer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/engines/12/catalog/children" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tenant-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var request EngineCatalogListChildrenRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Path.EngineID != 12 || request.Path.Version != "catalog.path/v1" || len(request.Path.Segments) != 1 || request.Path.Segments[0].Name != "public" {
			t.Fatalf("catalog request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(EngineCatalogListChildrenResponse{Nodes: []EngineCatalogEntry{{Name: "orders", Role: "leaf"}}})
	}))
	defer server.Close()

	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("tenant-token"), server.Client()).WithTenantID(7)
	nodes, err := client.ListCatalogChildren(context.Background(), 12, EngineCatalogListChildrenRequest{Path: EngineCatalogPath{
		Version: "catalog.path/v1", EngineID: 12, Segments: []EngineCatalogSegment{{Term: "schema", Kind: "namespace", Name: "public"}},
	}})
	if err != nil || len(nodes) != 1 || nodes[0].Name != "orders" {
		t.Fatalf("ListCatalogChildren() nodes=%#v error=%v", nodes, err)
	}
}

type staticSystemServiceTokenSource string

func (s staticSystemServiceTokenSource) Token(context.Context, uint) (string, error) {
	return string(s), nil
}

func (s staticSystemServiceTokenSource) PlatformToken(context.Context) (string, error) {
	return string(s), nil
}

func TestSystemServiceClientListsEveryEngine(t *testing.T) {
	t.Parallel()

	engineRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "addp_at_tenant_7", "token_type": "Bearer", "expires_in": 300, "scope": "addp.api",
			})
		case "/api/v1/system/engines":
			if r.Header.Get("Authorization") != "Bearer addp_at_tenant_7" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			if r.URL.RawQuery != "" {
				t.Fatalf("unexpected engine list query: %s", r.URL.RawQuery)
			}
			engineRequests++
			data := make([]models.Engine, 0, 205)
			for index := 0; index < 205; index++ {
				data = append(data, models.Engine{ID: uint(index + 1)})
			}
			_ = json.NewEncoder(w).Encode(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-meta", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := NewSystemServiceClient(server.URL, source, server.Client())
	engines, err := client.WithTenantID(7).ListEngines(context.Background())
	if err != nil {
		t.Fatalf("ListEngines() error = %v", err)
	}
	if len(engines) != 205 || engines[0].ID != 1 || engines[204].ID != 205 {
		t.Fatalf("engines length=%d first=%d last=%d", len(engines), engines[0].ID, engines[len(engines)-1].ID)
	}
	if engineRequests != 1 {
		t.Fatalf("engine requests = %d, want 1", engineRequests)
	}
}

func TestSystemServiceClientReadsRuntimeDescriptorsWithoutLegacyHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "addp_at_tenant_7", "token_type": "Bearer", "expires_in": 300, "scope": "addp.api",
			})
		case "/api/v1/system/runtime/engine-descriptors/12":
			if r.Header.Get("Authorization") != "Bearer addp_at_tenant_7" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
				t.Fatal("descriptor request sent legacy authentication headers")
			}
			_ = json.NewEncoder(w).Encode(models.EngineRuntimeDescriptor{
				ID:              12,
				RuntimeEndpoint: &models.EngineRuntimeEndpoint{Protocol: "http", Host: "workflow", Port: 8099},
			})
		case "/api/v1/system/runtime/engine-descriptors":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":  []models.EngineRuntimeDescriptor{{ID: 12}},
				"total": 1, "page": 1, "page_size": 100,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-develop", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := NewSystemServiceClient(server.URL, source, server.Client()).WithTenantID(7)
	descriptor, err := client.GetEngineRuntimeDescriptor(context.Background(), 12)
	if err != nil || descriptor.RuntimeEndpoint == nil || descriptor.RuntimeEndpoint.Port != 8099 {
		t.Fatalf("GetEngineRuntimeDescriptor() descriptor=%#v error=%v", descriptor, err)
	}
	descriptors, err := client.ListEngineRuntimeDescriptors(context.Background())
	if err != nil || len(descriptors) != 1 || descriptors[0].ID != 12 {
		t.Fatalf("ListEngineRuntimeDescriptors() descriptors=%#v error=%v", descriptors, err)
	}
}
