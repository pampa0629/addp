package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newServiceCatalogTestClient(server *httptest.Server) *ServiceClient {
	return NewServiceClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
}

func TestServiceClientReadsQueryServiceCatalogChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/service/catalog-resources/changes" || r.URL.Query().Get("limit") != "200" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"schema_version":"service.catalog_resource_changes/v1","changes":[{"change_id":"NDI","source_type":"query_service","source_identity":"9","operation":"upsert","source_version":"00000000000000000042","observed_at":"2026-08-26T01:02:03Z","snapshot":{"name":"Orders"}}],"next_cursor":"NDI","has_more":false}`)
	}))
	defer server.Close()

	result, err := newServiceCatalogTestClient(server).ListCatalogResourceChanges(context.Background(), "", 200)
	if err != nil || len(result.Changes) != 1 || result.Changes[0].SourceIdentity != "9" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestServiceClientResolvesCurrentQueryService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/service/runtime/catalog-references/resolve" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"source_type":"query_service","source_identity":"9","found":true,"status":"active","version":42,"summary":{"name":"Orders"},"detail_path":"/service/published-services/9"}]}`)
	}))
	defer server.Close()

	result, err := newServiceCatalogTestClient(server).ResolveCatalogReferences(context.Background(), []ServiceCatalogReference{{SourceType: "query_service", SourceIdentity: "9"}})
	if err != nil || !result.Results[0].Found || result.Results[0].Version != 42 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestServiceClientRejectsNonCanonicalQueryServiceIdentity(t *testing.T) {
	client := &ServiceClient{tenantHTTPClient: tenantHTTPClient{tenantID: func() *uint { value := uint(7); return &value }()}}
	for _, identity := range []string{"0", "01", " 1", "-1", "service-1"} {
		_, err := client.ResolveCatalogReferences(context.Background(), []ServiceCatalogReference{{SourceType: "query_service", SourceIdentity: identity}})
		if err == nil || !strings.Contains(err.Error(), "invalid reference") {
			t.Fatalf("identity %q error=%v", identity, err)
		}
	}
}
