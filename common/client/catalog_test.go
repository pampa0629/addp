package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestCatalogClientResolveReferencesUsesTenantTokenAndPreservesOrder(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/catalog/runtime/references/resolve" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tenant-7" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.IDs) != 2 || request.IDs[0] != first.String() || request.IDs[1] != second.String() {
			t.Fatalf("request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"id": first.String(), "found": true, "selectable": true, "publishable": true, "version": "9"},
			{"id": second.String(), "found": true, "selectable": true, "publishable": false, "version": "2"},
		}})
	}))
	defer server.Close()

	client := NewCatalogClient(server.URL, staticTenantTokenSource{}, server.Client()).WithTenantID(7)
	results, err := client.ResolveReferences(context.Background(), []uuid.UUID{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].ID != first || results[0].Version != 9 || results[1].Publishable {
		t.Fatalf("results = %#v", results)
	}
}

type staticTenantTokenSource struct{}

func (staticTenantTokenSource) Token(_ context.Context, tenantID uint) (string, error) {
	return fmt.Sprintf("tenant-%d", tenantID), nil
}
