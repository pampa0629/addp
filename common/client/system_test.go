package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func staticTenantToken(token string) ServiceTokenProvider {
	return ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		return token, nil
	})
}

func TestSystemClientListEnginesUsesTenantServiceToken(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuth string
	var gotQuery string
	var gotLegacyHeaders bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		gotLegacyHeaders = r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != ""
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"id":1,
			"name":"GeoPython Workflow",
			"engine_type":"geopython_workflow",
			"connection_info":{},
			"capabilities_view":{
				"summary":[{"id":"workflow","label_key":"system.engine.capabilityView.summary.workflow"}],
				"sections":[{"id":"compute","title_key":"system.engine.capabilityView.sections.compute"}],
				"json_view":[{"key":"schema_version","value":"engine.capabilities/v1"}]
			}
		}]`))
	}))
	defer server.Close()

	client := NewSystemClient(server.URL, staticTenantToken("addp_at_test_service_token"))
	engines, err := client.ListEngines("python workflow", 9)
	if err != nil {
		t.Fatalf("ListEngines() error = %v", err)
	}

	if gotPath != "/api/v1/system/engines" {
		t.Fatalf("path = %q, want /api/v1/system/engines", gotPath)
	}
	if gotAuth != "Bearer addp_at_test_service_token" {
		t.Fatalf("authorization header = %q", gotAuth)
	}
	if gotQuery != "engine_type=python+workflow" {
		t.Fatalf("query = %q, want escaped engine_type only", gotQuery)
	}
	if gotLegacyHeaders {
		t.Fatal("request contained legacy internal authentication headers")
	}
	if len(engines) != 1 || engines[0].CapabilitiesView == nil {
		t.Fatalf("engines = %#v, want one engine with capabilities view", engines)
	}
}

func TestSystemClientGetEngineForTenantUsesRequestedTenantToken(t *testing.T) {
	t.Parallel()

	var gotTenantID uint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/engines/26" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":26,"name":"Runtime","engine_type":"geopython_workflow","connection_info":{}}`))
	}))
	defer server.Close()

	client := NewSystemClient(server.URL, ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		gotTenantID = tenantID
		return "addp_at_tenant_token", nil
	}))
	engine, err := client.GetEngineForTenant(context.Background(), 7, 26)
	if err != nil {
		t.Fatalf("GetEngineForTenant() error = %v", err)
	}
	if gotTenantID != 7 {
		t.Fatalf("token tenant ID = %d, want 7", gotTenantID)
	}
	if engine.ID != 26 {
		t.Fatalf("engine = %#v", engine)
	}
}

func TestSystemClientGetEngineRequiresTenantContext(t *testing.T) {
	t.Parallel()

	client := NewSystemClient("http://system", staticTenantToken("addp_at_test_service_token"))
	if _, err := client.GetEngine(1); err == nil {
		t.Fatal("GetEngine() error = nil, want tenant context error")
	}
}
