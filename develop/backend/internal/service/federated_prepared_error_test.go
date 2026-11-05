package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
)

type preparedServiceGateFixture struct{ allowDevelopProtectionGate }

func (preparedServiceGateFixture) BeginFederatedQuery(ctx context.Context, _ uint, prepare func(context.Context) (plugin.PreparedFederatedQuery, error)) (plugin.PreparedFederatedQuery, func(), error) {
	p, err := prepare(ctx)
	if err != nil {
		return nil, nil, err
	}
	return p, func() { _ = p.Close(context.Background()) }, nil
}

func TestFederatedPreparedExecutionErrorPropagatesAndClosesWithoutNilResultPanic(t *testing.T) {
	id := "9a21ab1a-2900-42a5-ae91-821339b3fcdd"
	spark, err := plugin.Get("spark")
	if err != nil {
		t.Fatal(err)
	}
	targetCaps := spark.Capabilities()
	actorCaps := plugin.NewFederatedQueryCapabilities("spark_workflow", "addp.query-runtime/v1", []string{"postgresql", "mysql"}, nil)
	actorCaps.Compute.Query.Federation.ExecutionAudience = "spark"
	actorCaps.Compute.Query.Federation.TargetEngineTypes = []string{"spark"}
	descriptor := func(id uint, kind string, caps plugin.EngineCapabilities) commonModels.EngineRuntimeDescriptor {
		raw, _ := json.Marshal(caps)
		encoded := commonModels.JSONString(raw)
		return commonModels.EngineRuntimeDescriptor{ID: id, Name: kind, EngineType: kind, Capabilities: &encoded, LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionOnline}
	}
	target := descriptor(90, "spark", targetCaps)
	actor := descriptor(99, "spark_workflow", actorCaps)
	actor.IsBuiltin = true
	closes, executes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/runtime/engine-descriptors/90":
			_ = json.NewEncoder(w).Encode(target)
		case "/api/v1/system/runtime/engine-descriptors/99":
			_ = json.NewEncoder(w).Encode(actor)
		case "/api/v1/queries":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"query_id": id, "sources": []map[string]interface{}{{"engine_id": 12, "engine_type": "postgresql"}}, "engine_read_scope": map[string]interface{}{"engine_ids": []uint{12}}, "read_set": &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{plugin.TabularItemPath(12, "schema", "public", "example")}}})
		case "/api/v1/queries/" + id + "/execute":
			executes++
			if executes == 1 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error_code":"query_driver_failed","error":"fixture driver failure"}`))
			} else {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error_code":"query_column_not_found","error":"private candidate columns"}`))
			}
		case "/api/v1/queries/" + id:
			closes++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(endpoint.Port())
	actor.RuntimeEndpoint = &commonModels.EngineRuntimeEndpoint{Protocol: "http", Host: endpoint.Hostname(), Port: port}
	client := commonClient.NewSystemServiceClient(server.URL, staticServiceTokenSource("fixture"), server.Client())
	service := NewFederatedQueryService(client, nil)
	service.SetProtectionGate(preparedServiceGateFixture{})
	frozen := &models.FederatedQueryBinding{SchemaVersion: federatedBindingSchema, SelectedEngineID: 90, RuntimeEngineID: 99, Audience: "spark", SourceEngineIDs: []uint{12}}
	result, err := service.ExecuteQuery(t.Context(), 7, frozen, uuid.New(), 8, "SELECT id FROM source.public.example", 30, 3)
	if result != nil || err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if closes != 1 || executes != 1 {
		t.Fatalf("execute=%d close=%d", executes, closes)
	}
	result, err = service.ExecuteQuery(t.Context(), 7, frozen, uuid.New(), 8, "SELECT missing FROM source.public.example", 30, 3)
	if result != nil || plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeColumnNotFound ||
		strings.Contains(err.Error(), "private") || closes != 2 || executes != 2 {
		t.Fatalf("result=%+v error=%v execute=%d close=%d", result, err, executes, closes)
	}
	diagnostic := queryFailureDiagnostic("develop.query.execution_failed", err.Error(), err)
	if diagnostic == nil || diagnostic.Stage != "execution" || diagnostic.Reason != "column_not_found" || diagnostic.SQL != nil {
		t.Fatalf("unexpected missing column diagnostic: %+v", diagnostic)
	}
}
