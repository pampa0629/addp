package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newDevelopCatalogTestClient(server *httptest.Server) *DevelopClient {
	return NewDevelopClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
}

func TestDevelopClientReadsCatalogChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/develop/catalog-resources/changes" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"schema_version":"develop.catalog_resource_changes/v1","changes":[{"change_id":"NDI","source_type":"dev_task","source_identity":"9","operation":"upsert","source_version":"00000000000000000042","observed_at":"2026-08-26T01:02:03Z","snapshot":{"name":"Orders workflow"}}],"next_cursor":"NDI","has_more":false}`)
	}))
	defer server.Close()
	result, err := newDevelopCatalogTestClient(server).ListCatalogResourceChanges(context.Background(), "", 200)
	if err != nil || len(result.Changes) != 1 || result.Changes[0].SourceIdentity != "9" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestDevelopClientResolvesCurrentArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/develop/runtime/catalog-references/resolve" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"source_type":"dev_task","source_identity":"9","found":true,"status":"active","version":42,"summary":{"name":"Orders workflow"},"detail_path":"/develop/workflow?action=edit&id=9"}]}`)
	}))
	defer server.Close()
	result, err := newDevelopCatalogTestClient(server).ResolveCatalogReferences(context.Background(), []DevelopCatalogReference{{SourceType: "dev_task", SourceIdentity: "9"}})
	if err != nil || !result.Results[0].Found {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}
