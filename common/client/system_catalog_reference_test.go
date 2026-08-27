package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSystemServiceClientResolvesCatalogReferences(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/system/runtime/catalog-references/resolve" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tenant-token" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatalf("unexpected auth headers: %#v", r.Header)
		}
		var request struct {
			References []systemCatalogReferenceWire `json:"references"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.References) != 3 || request.References[0].ID != "7" || request.References[1].SubjectType != "user" || request.References[2].SubjectType != "project_group" {
			t.Fatalf("request = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"subject_type":"department","id":"7","found":true,"referenceable":true,"name":"Sales","code":"sales","status":"active"},{"subject_type":"user","id":"9","found":true,"referenceable":false,"name":"Alice","status":"suspended","principal_status":"active","membership_status":"suspended"},{"subject_type":"project_group","id":"11","found":true,"referenceable":true,"name":"Delivery","code":"delivery","status":"active"}]}`))
	}))
	defer server.Close()

	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("tenant-token"), server.Client()).WithTenantID(3)
	results, err := client.ResolveCatalogReferences(context.Background(), []SystemCatalogReference{
		{SubjectType: "department", ID: 7},
		{SubjectType: "user", ID: 9},
		{SubjectType: "project_group", ID: 11},
	})
	if err != nil {
		t.Fatalf("ResolveCatalogReferences() error = %v", err)
	}
	if len(results) != 3 || !results[0].Referenceable || results[1].Referenceable || results[1].MembershipStatus != "suspended" || results[2].Name != "Delivery" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSystemServiceClientListsCatalogReferenceCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/system/runtime/catalog-references/candidates" ||
			r.URL.Query().Get("subject_type") != "user" || r.URL.Query().Get("search") != "alice" ||
			r.URL.Query().Get("page") != "1" || r.URL.Query().Get("page_size") != "20" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"subject_type":"user","id":"9","name":"Alice","code":"alice","status":"active"}],"total":1,"page":1,"page_size":20,"total_pages":1}`))
	}))
	defer server.Close()
	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("tenant-token"), server.Client()).WithTenantID(3)
	result, err := client.ListCatalogReferenceCandidates(context.Background(), "user", " alice ", 1, 20)
	if err != nil || result.Total != 1 || len(result.Data) != 1 || result.Data[0].ID != "9" {
		t.Fatalf("result = %#v, err=%v", result, err)
	}
}
