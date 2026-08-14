package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

func TestWorkflowContainerInspectorResolvesBuiltinRuntimeDescriptor(t *testing.T) {
	const tenantID = uint(7)
	const runtimeID = uint(42)
	const operatorName = "vector_dataset.inspect"

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/operators" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"operators": []map[string]interface{}{testEngineServiceWorkflowOperator("geopython", operatorName)},
		})
	}))
	defer runtimeServer.Close()

	capabilities := engineplugin.NewWorkflowCapabilities("geopython", engineplugin.WorkflowRuntimeAPIAddpV1)
	capabilitiesJSON, err := engineplugin.MarshalEngineCapabilities(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	encodedCapabilities := commonModels.JSONString(capabilitiesJSON)

	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_meta_7", "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
		case "/api/v1/system/runtime/engine-descriptors":
			if r.Header.Get("Authorization") != "Bearer addp_at_meta_7" {
				t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []commonModels.EngineRuntimeDescriptor{{
					ID: runtimeID, Name: "GeoPython", EngineType: "geopython",
					LifecycleState: commonModels.EngineLifecycleActive, IsBuiltin: true,
					Capabilities:    &encodedCapabilities,
					RuntimeEndpoint: testEngineServiceRuntimeEndpoint(t, runtimeServer.URL),
				}},
				"total": 1, "page": 1, "page_size": 100,
			})
		case "/api/v1/system/engines/42":
			t.Error("workflow runtime must not be loaded through the storage engine resource API")
			http.Error(w, "unexpected storage resource request", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer systemServer.Close()

	inspector := NewWorkflowContainerInspector(newEngineServiceAuthTestClient(t, systemServer))
	runtime, provider, connection, err := inspector.resolveRuntime(context.Background(), tenantID, []string{operatorName})
	if err != nil {
		t.Fatalf("resolveRuntime() error = %v", err)
	}
	if runtime.ID != runtimeID || !runtime.IsBuiltin {
		t.Fatalf("runtime = %#v, want builtin runtime %d", runtime, runtimeID)
	}
	if provider == nil || connection["host"] == "" || connection["port"] == nil {
		t.Fatalf("resolved provider=%T connection=%#v", provider, connection)
	}
}
