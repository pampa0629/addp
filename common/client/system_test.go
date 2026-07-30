package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSystemClientListEnginesDecodesInternalCapabilitiesView(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotHeader string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Internal-API-Key")
		gotQuery = r.URL.RawQuery
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

	client := NewSystemClientWithInternalKey(server.URL, "internal-key")
	engines, err := client.ListEngines("geopython_workflow", 7)
	if err != nil {
		t.Fatalf("ListEngines() error = %v", err)
	}

	if gotPath != "/api/v1/internal/engines" {
		t.Fatalf("path = %q, want /api/v1/internal/engines", gotPath)
	}
	if gotHeader != "internal-key" {
		t.Fatalf("internal key header = %q, want internal-key", gotHeader)
	}
	if gotQuery != "engine_type=geopython_workflow&tenant_id=7" {
		t.Fatalf("query = %q, want engine_type=geopython_workflow&tenant_id=7", gotQuery)
	}
	if len(engines) != 1 {
		t.Fatalf("engines length = %d, want 1", len(engines))
	}
	view := engines[0].CapabilitiesView
	if view == nil {
		t.Fatal("CapabilitiesView is nil")
	}
	if got := view.Summary[0].ID; got != "workflow" {
		t.Fatalf("summary id = %q, want workflow", got)
	}
	if got := view.Sections[0].ID; got != "compute" {
		t.Fatalf("section id = %q, want compute", got)
	}
}

func TestSystemClientListEnginesDecodesPaginatedCapabilitiesView(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuth string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":[{
				"id":1,
				"name":"GeoPython Workflow",
				"engine_type":"geopython_workflow",
				"connection_info":{},
				"capabilities_view":{
					"summary":[{"id":"workflow","label_key":"system.engine.capabilityView.summary.workflow"}],
					"sections":[{"id":"compute","title_key":"system.engine.capabilityView.sections.compute"}],
					"json_view":[{"key":"schema_version","value":"engine.capabilities/v1"}]
				}
			}],
			"total":1,
			"page":1,
			"page_size":10,
			"total_pages":1
		}`))
	}))
	defer server.Close()

	client := NewSystemClient(server.URL, "token")
	engines, err := client.ListEngines("python workflow", 9)
	if err != nil {
		t.Fatalf("ListEngines() error = %v", err)
	}

	if gotPath != "/api/v1/system/engines" {
		t.Fatalf("path = %q, want /api/v1/system/engines", gotPath)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("authorization header = %q, want Bearer token", gotAuth)
	}
	if gotQuery != "engine_type=python+workflow&tenant_id=9" {
		t.Fatalf("query = %q, want escaped engine_type and tenant_id", gotQuery)
	}
	if len(engines) != 1 {
		t.Fatalf("engines length = %d, want 1", len(engines))
	}
	if engines[0].CapabilitiesView == nil {
		t.Fatal("CapabilitiesView is nil")
	}
	if got := engines[0].CapabilitiesView.Summary[0].ID; got != "workflow" {
		t.Fatalf("summary id = %q, want workflow", got)
	}
}

func TestSystemClientListCapabilitiesEncodesFilters(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotHeader string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Internal-API-Key")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewSystemClientWithInternalKey(server.URL, "internal-key")
	_, err := client.ListCapabilities(map[string]string{
		"engine_type": "python workflow",
		"name":        "A&B",
	})
	if err != nil {
		t.Fatalf("ListCapabilities() error = %v", err)
	}

	if gotPath != "/api/v1/internal/registry/capabilities" {
		t.Fatalf("path = %q, want /api/v1/internal/registry/capabilities", gotPath)
	}
	if gotHeader != "internal-key" {
		t.Fatalf("internal key header = %q, want internal-key", gotHeader)
	}
	if gotQuery != "engine_type=python+workflow&name=A%26B" {
		t.Fatalf("query = %q, want escaped filters", gotQuery)
	}
}

func TestSystemClientListComputeEnginesEncodesFilters(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewSystemClientWithInternalKey(server.URL, "internal-key")
	_, err := client.ListComputeEngines(map[string]string{
		"runtime_api": "addp.workflow/v1",
	})
	if err != nil {
		t.Fatalf("ListComputeEngines() error = %v", err)
	}

	if gotPath != "/api/v1/internal/registry/compute-engines" {
		t.Fatalf("path = %q, want /api/v1/internal/registry/compute-engines", gotPath)
	}
	if gotQuery != "runtime_api=addp.workflow%2Fv1" {
		t.Fatalf("query = %q, want escaped runtime_api filter", gotQuery)
	}
}

func TestSystemClientListEnginesByCapabilityEncodesFilters(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotHeader string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Internal-API-Key")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewSystemClientWithInternalKey(server.URL, "internal-key")
	_, err := client.ListEnginesByCapability(5, []string{"tabular", "dynamic_schema"})
	if err != nil {
		t.Fatalf("ListEnginesByCapability() error = %v", err)
	}

	if gotPath != "/api/v1/internal/engines" {
		t.Fatalf("path = %q, want /api/v1/internal/engines", gotPath)
	}
	if gotHeader != "internal-key" {
		t.Fatalf("internal key header = %q, want internal-key", gotHeader)
	}
	if gotQuery != "storage_type=tabular%2Cdynamic_schema&tenant_id=5" {
		t.Fatalf("query = %q, want escaped storage_type and tenant_id", gotQuery)
	}
}

func TestSystemClientListEnginesByCapabilityRequiresInternalKey(t *testing.T) {
	t.Parallel()

	client := NewSystemClient("http://system", "token")
	if _, err := client.ListEnginesByCapability(1, []string{"tabular"}); err == nil {
		t.Fatal("ListEnginesByCapability() error = nil, want internal key requirement error")
	}
}

func TestSystemClientListWorkflowEnginesFiltersByV1WorkflowCapability(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotHeader string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Internal-API-Key")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"id":1,
				"name":"GeoPython Workflow",
				"engine_type":"geopython_workflow",
				"lifecycle_state":"active",
				"connection_info":{},
				"capabilities":{
					"schema_version":"engine.capabilities/v1",
					"engine_type":"geopython_workflow",
					"engine_family":"workflow",
					"compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}}
				}
			},
			{
				"id":2,
				"name":"Inactive Workflow",
				"engine_type":"math_workflow",
				"lifecycle_state":"disabled",
				"connection_info":{},
				"capabilities":{
					"schema_version":"engine.capabilities/v1",
					"engine_type":"math_workflow",
					"engine_family":"workflow",
					"compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}}
				}
			},
			{
				"id":3,
				"name":"Legacy Workflow",
				"engine_type":"legacy_workflow",
				"lifecycle_state":"active",
				"connection_info":{},
				"capabilities":{"schema_version":"legacy","compute":{"workflow":{"supported":true}}}
			},
			{
				"id":4,
				"name":"Spark General",
				"engine_type":"spark",
				"lifecycle_state":"active",
				"connection_info":{},
				"capabilities":{
					"schema_version":"engine.capabilities/v1",
					"engine_type":"spark",
					"engine_family":"tabular",
					"compute":{"query":{"supported":true,"languages":["sql"]}}
				}
			}
		]`))
	}))
	defer server.Close()

	client := NewSystemClientWithInternalKey(server.URL, "internal-key")
	engines, err := client.ListWorkflowEngines(9)
	if err != nil {
		t.Fatalf("ListWorkflowEngines() error = %v", err)
	}

	if gotPath != "/api/v1/internal/engines" {
		t.Fatalf("path = %q, want /api/v1/internal/engines", gotPath)
	}
	if gotHeader != "internal-key" {
		t.Fatalf("internal key header = %q, want internal-key", gotHeader)
	}
	if gotQuery != "tenant_id=9" {
		t.Fatalf("query = %q, want tenant_id=9", gotQuery)
	}
	if len(engines) != 1 {
		t.Fatalf("engines length = %d, want 1", len(engines))
	}
	if engines[0].EngineType != "geopython_workflow" {
		t.Fatalf("engine_type = %q, want geopython_workflow", engines[0].EngineType)
	}
}

func TestSystemClientListSparkRuntimesKeepsSparkGeneralEngineBinding(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotHeader string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Internal-API-Key")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":11,"name":"Spark Runtime A","engine_type":"spark","lifecycle_state":"active","connection_info":{}},
			{"id":12,"name":"Spark Runtime B","engine_type":"spark","lifecycle_state":"disabled","connection_info":{}}
		]`))
	}))
	defer server.Close()

	client := NewSystemClientWithInternalKey(server.URL, "internal-key")
	runtimes, err := client.ListSparkRuntimes(3)
	if err != nil {
		t.Fatalf("ListSparkRuntimes() error = %v", err)
	}

	if gotPath != "/api/v1/internal/engines" {
		t.Fatalf("path = %q, want /api/v1/internal/engines", gotPath)
	}
	if gotHeader != "internal-key" {
		t.Fatalf("internal key header = %q, want internal-key", gotHeader)
	}
	if gotQuery != "engine_type=spark&tenant_id=3" {
		t.Fatalf("query = %q, want engine_type=spark&tenant_id=3", gotQuery)
	}
	if len(runtimes) != 1 {
		t.Fatalf("runtimes length = %d, want 1", len(runtimes))
	}
	if runtimes[0].ID != 11 || runtimes[0].EngineType != "spark" {
		t.Fatalf("runtime = %#v, want active spark general engine ID 11", runtimes[0])
	}
}
