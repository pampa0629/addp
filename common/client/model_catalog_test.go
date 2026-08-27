package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestModelClientListCatalogResourceChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/model/catalog-resources/changes" || r.URL.Query().Get("after_cursor") != "NDE" || r.URL.Query().Get("limit") != "200" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(ModelCatalogResourceChangesResponse{
			SchemaVersion: ModelCatalogResourceChangesSchemaVersion, NextCursor: "NDI",
			Changes: []ModelCatalogResourceChange{{ChangeID: "NDI", SourceType: "entity", SourceIdentity: "9", Operation: "upsert", SourceVersion: "00000000000000000042", ObservedAt: time.Now().UTC(), Snapshot: map[string]any{"name": "Order"}}},
		})
	}))
	defer server.Close()
	client := NewModelClient(server.URL, staticTenantTokenSource{}, server.Client()).WithTenantID(7)
	result, err := client.ListCatalogResourceChanges(context.Background(), "NDE", 200)
	if err != nil || len(result.Changes) != 1 || result.Changes[0].SourceIdentity != "9" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestModelClientResolveCatalogReferencesPreservesOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/model/runtime/catalog-references/resolve" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ResolveModelCatalogReferencesResponse{Results: []ModelCatalogReferenceResolution{
			{SourceType: "logical_table", SourceIdentity: "12", Found: true, Status: "approved", Version: 3, Summary: map[string]any{"name": "Orders"}, DetailPath: "/modeling/logical-tables/12"},
			{SourceType: "entity", SourceIdentity: "8", Found: false},
		}})
	}))
	defer server.Close()
	client := NewModelClient(server.URL, staticTenantTokenSource{}, server.Client()).WithTenantID(7)
	result, err := client.ResolveCatalogReferences(context.Background(), []ModelCatalogReference{{SourceType: "logical_table", SourceIdentity: "12"}, {SourceType: "entity", SourceIdentity: "8"}})
	if err != nil || len(result.Results) != 2 || !result.Results[0].Found || result.Results[1].Found {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}
