package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

func TestFederatedQueryServiceResolvesOnlyReferencedSupportedSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer addp_at_service" {
			t.Fatalf("unexpected authorization: %s", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/api/v1/system/runtime/engine-descriptors/90":
			_ = json.NewEncoder(w).Encode(commonModels.EngineRuntimeDescriptor{
				ID: 90, Name: "DuckDB", EngineType: "duckdb", LifecycleState: commonModels.EngineLifecycleActive,
				RuntimeEndpoint: &commonModels.EngineRuntimeEndpoint{Protocol: "http", Host: "duckdb", Port: 8096},
			})
		case "/api/v1/system/runtime/engine-descriptors":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []commonModels.EngineRuntimeDescriptor{
					{ID: 12, Name: "Business PostgreSQL", EngineType: "postgresql", LifecycleState: commonModels.EngineLifecycleActive},
					{ID: 13, Name: "Business MySQL", EngineType: "mysql", LifecycleState: commonModels.EngineLifecycleActive},
					{ID: 14, Name: "Business MinIO", EngineType: "minio", LifecycleState: "disabled"},
					{ID: 15, Name: "Business Neo4j", EngineType: "neo4j", LifecycleState: commonModels.EngineLifecycleActive},
				},
				"total": 4, "page": 1, "page_size": 100,
			})
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	systemClient := commonClient.NewSystemServiceClient(server.URL, staticServiceTokenSource("addp_at_service"), server.Client())
	svc := NewFederatedQueryService(systemClient, nil)

	ids, err := svc.ReferencedEngineIDs(
		context.Background(), 7, 90,
		"SELECT t.id FROM Business_PostgreSQL.public.cities AS t JOIN Business_MySQL.sales.orders AS o ON o.id = t.id",
	)
	if err != nil {
		t.Fatalf("ReferencedEngineIDs() error = %v", err)
	}
	if !reflect.DeepEqual(ids, []uint{12, 13}) {
		t.Fatalf("engine IDs = %#v, want [12 13]", ids)
	}
}

func TestFederatedQueryCandidatesComeFromCurrentCatalogSources(t *testing.T) {
	svc := &FederatedQueryService{}
	candidates := svc.CandidateQueries([]DataSource{{
		EngineName: "Business MinIO", EngineID: 12, EngineType: "minio",
		Tables: []TableRef{{Table: "orders", PhysicalPath: "manager/orders"}},
	}})
	if len(candidates) != 1 || candidates[0].EngineID != 12 ||
		candidates[0].Query != "SELECT *\nFROM Business_MinIO.orders\nLIMIT 10" {
		t.Fatalf("candidates = %#v", candidates)
	}
}
