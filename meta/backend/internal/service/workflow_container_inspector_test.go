package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
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

func TestWorkflowContainerInspectorDetectsPGeoFromAccessFilePlan(t *testing.T) {
	const tenantID = uint(7)
	const operatorName = "vector_dataset.detect"

	var invokeRequest struct {
		Params map[string]interface{} `json:"params"`
	}
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/operators":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"operators": []map[string]interface{}{testEngineServiceWorkflowOperator("geopython", operatorName)},
			})
		case "/api/operators/vector_dataset.detect/invoke":
			if err := json.NewDecoder(r.Body).Decode(&invokeRequest); err != nil {
				t.Fatalf("decode invoke request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"result": map[string]interface{}{
					"schema_version":   "gdal.vector-dataset.detect/v1",
					"candidate_format": "access",
					"format":           "pgeo",
				},
			})
		default:
			http.NotFound(w, r)
		}
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
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []commonModels.EngineRuntimeDescriptor{{
					ID: 42, Name: "GeoPython", EngineType: "geopython",
					LifecycleState: commonModels.EngineLifecycleActive, IsBuiltin: true,
					Capabilities:    &encodedCapabilities,
					RuntimeEndpoint: testEngineServiceRuntimeEndpoint(t, runtimeServer.URL),
				}},
				"total": 1, "page": 1, "page_size": 100,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer systemServer.Close()

	source := &commonModels.Engine{
		ID: 14, EngineType: "nfs",
		ConnectionInfo: commonModels.ConnectionInfo{
			"server": "nfs", "export_path": "/business/nfs/data", "nfs_version": "4",
		},
	}
	inspector := NewWorkflowContainerInspector(newEngineServiceAuthTestClient(t, systemServer))
	detected, err := inspector.DetectFormat(
		context.Background(), source, tenantID,
		"arcgis/AggDB_1.2015.1_Data/AggDB_v1.2015.1.mdb", "access", format.LayoutSingle,
	)
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if detected != format.FormatPGeo {
		t.Fatalf("detected format = %q, want pgeo", detected)
	}

	accessPlan, ok := invokeRequest.Params["access_plan"].(map[string]interface{})
	if !ok {
		t.Fatalf("access_plan = %#v", invokeRequest.Params["access_plan"])
	}
	workflowSource, ok := accessPlan["source"].(map[string]interface{})
	if !ok {
		t.Fatalf("access_plan.source = %#v", accessPlan["source"])
	}
	if workflowSource["kind"] != "file" || workflowSource["format"] != "access" {
		t.Fatalf("access_plan.source = %#v, want access file", workflowSource)
	}
	access, ok := workflowSource["access"].(map[string]interface{})
	if !ok {
		t.Fatalf("access_plan.source.access = %#v", workflowSource["access"])
	}
	if access["method"] != "mounted_path" || access["path"] != "/business/nfs/data/arcgis/AggDB_1.2015.1_Data/AggDB_v1.2015.1.mdb" {
		t.Fatalf("access_plan.source.access = %#v", access)
	}
}
