package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
)

func TestFederatedAuthorizationSeparatesSelectedTargetActorAndSources(t *testing.T) {
	descriptor := func(id uint, engineType string, fed plugin.QueryFederationCapability) commonModels.EngineRuntimeDescriptor {
		caps := plugin.NewFederatedQueryCapabilities(engineType, "addp.query-runtime/v1", nil, nil)
		caps.Compute.Query.Federation = &fed
		value, _ := json.Marshal(caps)
		encoded := commonModels.JSONString(value)
		return commonModels.EngineRuntimeDescriptor{ID: id, EngineType: engineType, Capabilities: &encoded,
			LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionOnline}
	}
	// A registered provider supplies SQL behavior; the descriptor owns routing.
	target := descriptor(90, "duckdb", plugin.QueryFederationCapability{Supported: true, RuntimeAPI: "addp.query-runtime/v1", ExecutionAudience: "spark", RuntimeEngineType: "builtin_compute"})
	actor := descriptor(99, "builtin_compute", plugin.QueryFederationCapability{Supported: true, RuntimeAPI: "addp.query-runtime/v1", ExecutionAudience: "spark", TargetEngineTypes: []string{"duckdb"}})
	actor.IsBuiltin = true
	actor.RuntimeEndpoint = &commonModels.EngineRuntimeEndpoint{Protocol: "http", Host: "private-runtime", Port: 8098}
	var dispatched *plugin.FederatedQueryRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer addp_at_service" {
			t.Error("missing service identity")
		}
		switch r.URL.Path {
		case "/api/v1/system/runtime/engine-descriptors/90":
			json.NewEncoder(w).Encode(target)
		case "/api/v1/system/runtime/engine-descriptors":
			json.NewEncoder(w).Encode(map[string]interface{}{"data": []commonModels.EngineRuntimeDescriptor{actor}, "total": 1, "page": 1, "page_size": 100})
		case "/api/v1/queries":
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				t.Error(readErr)
			}
			if strings.Contains(string(body), "addp_at_service") || strings.Contains(string(body), "connection_info") {
				t.Error("runtime wire body contains caller token or source connection")
			}
			var request plugin.FederatedQueryRequest
			if err := json.Unmarshal(body, &request); err != nil {
				t.Error(err)
			}
			dispatched = &request
			json.NewEncoder(w).Encode(map[string]interface{}{"columns": []string{"id"}, "rows": []map[string]interface{}{{"id": "9007199254740993"}}})
		default:
			t.Errorf("unexpected credential or catalog request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	actor.RuntimeEndpoint.Host = endpoint.Hostname()
	actor.RuntimeEndpoint.Port, _ = strconv.Atoi(endpoint.Port())
	client := commonClient.NewSystemServiceClient(server.URL, staticServiceTokenSource("addp_at_service"), server.Client())
	binding, _, err := resolveFederatedRuntime(context.Background(), client, 7, 90)
	if err != nil || binding.Target.ID != 90 || binding.Runtime.ID != 99 || binding.Audience != "spark" {
		t.Fatalf("binding=%+v error=%v", binding, err)
	}
	svc := &SQLEngineService{systemService: client}
	sources := []uint{12, 13}
	ids, audience, err := svc.federatedAuthorizationBoundary(context.Background(), 7, 90, sources)
	if err != nil || audience != "spark" || !reflect.DeepEqual(ids, []uint{12, 13, 90}) || !reflect.DeepEqual(sources, []uint{12, 13}) {
		t.Fatalf("authorization ids=%v audience=%s sources=%v error=%v", ids, audience, sources, err)
	}
	if _, _, err := svc.federatedAuthorizationBoundary(context.Background(), 7, 90, []uint{90}); err == nil {
		t.Fatal("compute target was admitted as a source")
	}
	federated := NewFederatedQueryService(client, nil)
	federated.SetProtectionGate(allowDevelopProtectionGate{})
	executionID := uuid.New()
	result, err := federated.ExecuteQuery(context.Background(), 7, 90, executionID, 8, "SELECT 1", 30, 10, sources)
	if err != nil || result.RowCount != 1 || dispatched == nil {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if dispatched.TargetEngineID != 90 || !reflect.DeepEqual(dispatched.SourceEngineIDs, []uint{12, 13}) ||
		dispatched.ExecutionID != executionID.String() || dispatched.ExecutionAuthorizationID != "8" || dispatched.CallerAccessToken != "" {
		t.Fatalf("runtime request mixed identities or serialized the caller token: %+v", dispatched)
	}
}
