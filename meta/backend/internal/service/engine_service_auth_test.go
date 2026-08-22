package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	supermapworkflow "github.com/addp/common/engine/plugins/supermap_workflow"
	commonModels "github.com/addp/common/models"
)

func TestEngineServiceUsesTenantBearerAndIsolatesConnectionCache(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	tokenRequests := map[string]int{}
	engineRequests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			tenantID := r.Form.Get("tenant_id")
			if r.Form.Get("context_type") != "" || (tenantID != "7" && tenantID != "8") {
				t.Errorf("unexpected token context form: %#v", r.Form)
			}
			mu.Lock()
			tokenRequests[tenantID]++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_meta_" + tenantID,
				"token_type":   "bearer",
				"expires_in":   300,
				"scope":        "addp.api",
			})
			return
		}

		if r.URL.Path != "/api/v1/system/engines/9" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("engine detail request sent a legacy authentication header")
		}
		tenantID := ""
		switch r.Header.Get("Authorization") {
		case "Bearer addp_at_meta_7":
			tenantID = "7"
		case "Bearer addp_at_meta_8":
			tenantID = "8"
		default:
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		engineRequests[tenantID]++
		mu.Unlock()
		parsedTenantID := uint(7)
		if tenantID == "8" {
			parsedTenantID = 8
		}
		_ = json.NewEncoder(w).Encode(commonModels.Engine{
			ID: 9, TenantID: &parsedTenantID, Name: "tenant-" + tenantID,
			EngineType: "postgresql", LifecycleState: "active",
			ConnectionInfo: commonModels.ConnectionInfo{"password": "plain-" + tenantID},
		})
	}))
	defer server.Close()

	engineService := newEngineServiceAuthTestClient(t, server)
	for _, tenantID := range []uint{7, 8, 7, 8} {
		engine, err := engineService.GetResourceByID(9, tenantID)
		if err != nil {
			t.Fatalf("GetResourceByID(9, %d) error = %v", tenantID, err)
		}
		if engine.TenantID == nil || *engine.TenantID != tenantID ||
			engine.ConnectionInfo["password"] != fmt.Sprintf("plain-%d", tenantID) {
			t.Fatalf("tenant %d engine = %#v", tenantID, engine)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if tokenRequests["7"] != 1 || tokenRequests["8"] != 1 || len(tokenRequests) != 2 {
		t.Fatalf("token requests = %#v, want one per tenant", tokenRequests)
	}
	if engineRequests["7"] != 1 || engineRequests["8"] != 1 || len(engineRequests) != 2 {
		t.Fatalf("engine requests = %#v, want one per tenant cache key", engineRequests)
	}
}

func TestEngineServiceDoesNotFallbackToOtherTenantOrExpiredConnectionCache(t *testing.T) {
	t.Parallel()

	engineRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_meta_7", "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}
		engineRequests++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	engineService := newEngineServiceAuthTestClient(t, server)
	tenant7 := uint(7)
	tenant8 := uint(8)
	engineService.engineCache[engineCacheKey{tenantID: tenant7, engineID: 9}] = &engineCacheEntry{
		resource: &commonModels.Engine{
			ID: 9, TenantID: &tenant7, ConnectionInfo: commonModels.ConnectionInfo{"password": "expired-tenant-7"},
		},
		expiresAt: time.Now().Add(-time.Minute),
	}
	engineService.engineCache[engineCacheKey{tenantID: tenant8, engineID: 9}] = &engineCacheEntry{
		resource: &commonModels.Engine{
			ID: 9, TenantID: &tenant8, ConnectionInfo: commonModels.ConnectionInfo{"password": "tenant-8"},
		},
		expiresAt: time.Now().Add(time.Minute),
	}

	if engine, err := engineService.GetResourceByID(9, tenant7); err == nil || engine != nil {
		t.Fatalf("GetResourceByID() = (%#v, %v), want remote error without cache fallback", engine, err)
	}
	if engineRequests != 1 {
		t.Fatalf("engine requests = %d, want 1", engineRequests)
	}
}

func TestEngineServiceResolvesBoundSuperMapSDXPostgreSQLScanProvider(t *testing.T) {
	const tenantID = uint(7)
	const runtimeID = uint(42)
	runtimeEngineType := "tenant_supermap_runtime"
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/operators" {
			http.NotFound(w, r)
			return
		}
		operators := make([]map[string]interface{}, 0)
		for _, name := range supermapworkflow.RequiredTableReadOperators() {
			operators = append(operators, testEngineServiceWorkflowOperator(runtimeEngineType, name))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"operators": operators})
	}))
	defer runtimeServer.Close()
	runtimeEndpoint := testEngineServiceRuntimeEndpoint(t, runtimeServer.URL)
	runtimeCapabilities := engineplugin.NewWorkflowCapabilities(runtimeEngineType, engineplugin.WorkflowRuntimeAPIAddpV1)
	runtimeCapabilitiesJSON, err := engineplugin.MarshalEngineCapabilities(runtimeCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	encodedRuntimeCapabilities := commonModels.JSONString(runtimeCapabilitiesJSON)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_meta_7", "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
		case "/api/v1/system/runtime/engine-descriptors/42":
			if r.Header.Get("Authorization") != "Bearer addp_at_meta_7" {
				t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(commonModels.EngineRuntimeDescriptor{
				ID: runtimeID, Name: "Tenant SuperMap Runtime", EngineType: runtimeEngineType,
				LifecycleState:   commonModels.EngineLifecycleActive,
				ConnectionStatus: commonModels.EngineConnectionOnline,
				Capabilities:     &encodedRuntimeCapabilities,
				RuntimeEndpoint:  runtimeEndpoint,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	capabilities := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	engineplugin.SetSpatialWorkspacesExtension(&capabilities, []engineplugin.SpatialWorkspaceFact{{
		Ecosystem:            "supermap",
		Kind:                 engineplugin.SpatialWorkspaceSuperMapSDXPostgreSQL,
		State:                engineplugin.SpatialWorkspaceStateEnabled,
		BoundRuntimeEngineID: uintPointer(runtimeID),
	}})
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	encodedCapabilities := commonModels.JSONString(capabilitiesJSON)
	resource := &commonModels.Engine{
		ID: 9, TenantID: uintPointer(tenantID), EngineType: "postgresql",
		Capabilities: &encodedCapabilities,
	}

	engineService := newEngineServiceAuthTestClient(t, server)
	resolved, err := engineService.ResolveScanPlugin(context.Background(), resource, tenantID)
	if err != nil {
		t.Fatalf("ResolveScanPlugin() error = %v", err)
	}
	if _, ok := resolved.(*supermapworkflow.SDXPostgreSQLTableProvider); !ok {
		t.Fatalf("ResolveScanPlugin() = %T, want SDXPostgreSQLTableProvider", resolved)
	}
}

func testEngineServiceWorkflowOperator(engineType, name string) map[string]interface{} {
	return map[string]interface{}{
		"id": name, "name": name, "display_name": name, "engine_type": engineType,
		"category": "table", "category_path": []string{"table"}, "description": name,
		"execution_modes": []string{"direct"}, "parameters": []interface{}{},
		"output_ports": []map[string]interface{}{{"name": "result", "data_type": "object", "default": true}},
		"effects":      []string{"read"},
	}
}

func testEngineServiceRuntimeEndpoint(t *testing.T, rawURL string) *commonModels.EngineRuntimeEndpoint {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return &commonModels.EngineRuntimeEndpoint{
		Protocol: parsed.Scheme,
		Host:     parsed.Hostname(),
		Port:     port,
	}
}

func TestEngineServiceKeepsPostgreSQLProviderForOtherSpatialWorkspaces(t *testing.T) {
	capabilities := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	engineplugin.SetSpatialWorkspacesExtension(&capabilities, []engineplugin.SpatialWorkspaceFact{{
		Ecosystem: "supermap", Kind: engineplugin.SpatialWorkspaceSuperMapSDXPostGIS,
		State: engineplugin.SpatialWorkspaceStateDetected,
	}})
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	encodedCapabilities := commonModels.JSONString(capabilitiesJSON)
	resolved, err := (&EngineService{}).ResolveScanPlugin(context.Background(), &commonModels.Engine{
		ID: 9, EngineType: "postgresql", Capabilities: &encodedCapabilities,
	}, 7)
	if err != nil {
		t.Fatalf("ResolveScanPlugin() error = %v", err)
	}
	if _, ok := resolved.(*postgresql.PostgreSQLPlugin); !ok {
		t.Fatalf("ResolveScanPlugin() = %T, want PostgreSQLPlugin", resolved)
	}
}

func uintPointer(value uint) *uint {
	return &value
}

func newEngineServiceAuthTestClient(t *testing.T, server *httptest.Server) *EngineService {
	t.Helper()
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		server.URL, "addp-meta", "meta-engine-service-test-secret-32-bytes", server.Client(),
	)
	if err != nil {
		t.Fatalf("create Meta service token source: %v", err)
	}
	return NewEngineService(nil, commonClient.NewSystemServiceClient(server.URL, tokenSource, server.Client()))
}
