package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
)

func TestRequirePostgreSQLCatalogTarget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/v1/system/engines/12/catalog/facts" {
			_ = json.NewEncoder(w).Encode(map[string]any{"table": map[string]any{"fields": []map[string]any{{"name": "id"}, {"name": "created_at"}}}})
			return
		}
		if r.URL.Path != "/api/v1/system/engines/12/catalog/children" {
			http.NotFound(w, r)
			return
		}
		var request commonClient.EngineCatalogListChildrenRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		switch len(request.Path.Segments) {
		case 0:
			_ = json.NewEncoder(w).Encode(commonClient.EngineCatalogListChildrenResponse{Nodes: []commonClient.EngineCatalogEntry{{
				Name: "Business PostgreSQL", Role: "branch", Path: commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: 12, Segments: []commonClient.EngineCatalogSegment{{Term: "server", Kind: "server"}}},
			}}})
		case 1:
			_ = json.NewEncoder(w).Encode(commonClient.EngineCatalogListChildrenResponse{Nodes: []commonClient.EngineCatalogEntry{{
				Name: "public", Role: "branch", Path: commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: 12, Segments: []commonClient.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "schema", Kind: "namespace", Name: "public"}}},
			}}})
		case 2:
			entry := commonClient.EngineCatalogEntry{
				Name: "orders", Role: "leaf",
				Path: commonClient.EngineCatalogPath{
					Version: "catalog.path/v1", EngineID: 12,
					Segments: []commonClient.EngineCatalogSegment{{Term: "server"}, {Term: "schema", Name: "public"}, {Term: "table", Name: "orders"}},
				},
			}
			_ = json.NewEncoder(w).Encode(commonClient.EngineCatalogListChildrenResponse{Nodes: []commonClient.EngineCatalogEntry{entry}})
		default:
			t.Fatalf("unexpected catalog path: %#v", request.Path)
		}
	}))
	defer server.Close()

	client := commonClient.NewSystemServiceClient(server.URL, qualityCatalogTokenSource("tenant-token"), server.Client())
	table, err := requirePostgreSQLCatalogTable(context.Background(), client, 7, 12, "public", "orders")
	if err != nil {
		t.Fatalf("valid table error = %v", err)
	}
	if err := requirePostgreSQLCatalogColumn(context.Background(), client, 7, 12, table, "id"); err != nil {
		t.Fatalf("valid target error = %v", err)
	}
	if err := requirePostgreSQLCatalogColumn(context.Background(), client, 7, 12, table, "missing"); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("missing column error = %v, want bad request", err)
	}
	if _, err := requirePostgreSQLCatalogTable(context.Background(), client, 7, 12, "public", "missing"); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("missing table error = %v, want bad request", err)
	}
}

type qualityCatalogTokenSource string

func (s qualityCatalogTokenSource) Token(context.Context, uint) (string, error) {
	return string(s), nil
}

func (s qualityCatalogTokenSource) PlatformToken(context.Context) (string, error) {
	return string(s), nil
}
