package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStandardClientListCatalogResourceChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/standard/catalog-resources/changes" || r.URL.Query().Get("after_cursor") != "NDE" || r.URL.Query().Get("limit") != "200" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(StandardCatalogResourceChangesResponse{
			SchemaVersion: StandardCatalogResourceChangesSchemaVersion, NextCursor: "NDI",
			Changes: []StandardCatalogResourceChange{{ChangeID: "NDI", SourceType: "metric", SourceIdentity: "9", Operation: "upsert", SourceVersion: "00000000000000000042", ObservedAt: time.Now().UTC(), Snapshot: map[string]any{"name": "Order amount"}}},
		})
	}))
	defer server.Close()
	client := NewStandardClient(server.URL, staticTenantTokenSource{}, server.Client()).WithTenantID(7)
	result, err := client.ListCatalogResourceChanges(context.Background(), "NDE", 200)
	if err != nil || len(result.Changes) != 1 || result.Changes[0].SourceIdentity != "9" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestStandardClientResolveCatalogReferencesPreservesOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/standard/runtime/catalog-references/resolve" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ResolveStandardCatalogReferencesResponse{Results: []StandardCatalogReferenceResolution{
			{SourceType: "metric", SourceIdentity: "12", Found: true, Status: "published", Version: 3, Summary: map[string]any{"name": "Order amount"}, DetailPath: "/standard/metrics/12"},
			{SourceType: "metric", SourceIdentity: "8", Found: false},
		}})
	}))
	defer server.Close()
	client := NewStandardClient(server.URL, staticTenantTokenSource{}, server.Client()).WithTenantID(7)
	result, err := client.ResolveCatalogReferences(context.Background(), []StandardCatalogReference{{SourceType: "metric", SourceIdentity: "12"}, {SourceType: "metric", SourceIdentity: "8"}})
	if err != nil || len(result.Results) != 2 || !result.Results[0].Found || result.Results[1].Found {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestStandardClientCatalogReferenceUsesCurrentRevisionLifecycle(t *testing.T) {
	for _, status := range []string{"draft", "in_review", "published", "withdrawn", "approved", "deprecated", "active", "unknown", ""} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(ResolveStandardCatalogReferencesResponse{Results: []StandardCatalogReferenceResolution{{
					SourceType: "metric", SourceIdentity: "12", Found: true, Status: status, Version: 3,
					Summary: map[string]any{"name": "Order amount", "category_id": "41", "unit_id": "51", "lifecycle_state": "active"}, DetailPath: "/standard/metrics/12",
				}}})
			}))
			defer server.Close()
			client := NewStandardClient(server.URL, staticTenantTokenSource{}, server.Client()).WithTenantID(7)
			result, err := client.ResolveCatalogReferences(context.Background(), []StandardCatalogReference{{SourceType: "metric", SourceIdentity: "12"}})
			valid := status == "draft" || status == "in_review" || status == "published" || status == "withdrawn"
			if valid {
				if err != nil || result.Results[0].Status != status || result.Results[0].Summary["category_id"] != "41" || result.Results[0].Summary["unit_id"] != "51" {
					t.Fatalf("current revision rejected or references lost: result=%+v error=%v", result, err)
				}
			} else if err == nil {
				t.Fatalf("invalid revision status %q accepted", status)
			}
		})
	}
}
