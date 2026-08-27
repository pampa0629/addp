package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const workbenchCatalogTestApplicationID = "d6c30859-15c8-4b88-964b-f2dd315fb923"

func newWorkbenchCatalogTestClient(server *httptest.Server) *WorkbenchClient {
	return NewWorkbenchClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
}

func TestWorkbenchClientReadsDataApplicationCatalogChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/workbench/catalog-resources/changes" || r.URL.Query().Get("limit") != "200" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"schema_version":"workbench.catalog_resource_changes/v1","changes":[{"change_id":"NDI","source_type":"data_application","source_identity":"%s","operation":"upsert","source_version":"00000000000000000042","observed_at":"2026-08-27T01:02:03Z","snapshot":{"name":"Application"}}],"next_cursor":"NDI","has_more":false}`, workbenchCatalogTestApplicationID)
	}))
	defer server.Close()

	result, err := newWorkbenchCatalogTestClient(server).ListCatalogResourceChanges(context.Background(), "", 200)
	if err != nil || len(result.Changes) != 1 || result.Changes[0].SourceIdentity != workbenchCatalogTestApplicationID {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestWorkbenchClientResolvesCurrentDataApplication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/workbench/runtime/catalog-references/resolve" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"results":[{"source_type":"data_application","source_identity":"%s","found":true,"status":"published","version":42,"summary":{"name":"Application"},"detail_path":"/data-apps/%s"}]}`, workbenchCatalogTestApplicationID, workbenchCatalogTestApplicationID)
	}))
	defer server.Close()

	result, err := newWorkbenchCatalogTestClient(server).ResolveCatalogReferences(context.Background(), []WorkbenchCatalogReference{{SourceType: "data_application", SourceIdentity: workbenchCatalogTestApplicationID}})
	if err != nil || !result.Results[0].Found || result.Results[0].Version != 42 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestWorkbenchClientRejectsNonCanonicalDataApplicationIdentity(t *testing.T) {
	client := &WorkbenchClient{tenantHTTPClient: tenantHTTPClient{tenantID: func() *uint { value := uint(7); return &value }()}}
	for _, identity := range []string{"", "D6C30859-15C8-4B88-964B-F2DD315FB923", " " + workbenchCatalogTestApplicationID, "not-a-uuid"} {
		_, err := client.ResolveCatalogReferences(context.Background(), []WorkbenchCatalogReference{{SourceType: "data_application", SourceIdentity: identity}})
		if err == nil || !strings.Contains(err.Error(), "invalid reference") {
			t.Fatalf("identity %q error=%v", identity, err)
		}
	}
}
