package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWorkbenchResourceGrantClientUsesTenantServiceTokenAndStableContract(t *testing.T) {
	applicationID := "1714dcf7-f34e-4996-a8dc-3b88998ebe55"
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	request, err := NewWorkbenchDataApplicationGrantRequest(applicationID, 91, &expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workbench/runtime/resource-grants/73" || r.Header.Get("Authorization") != "Bearer asset-tenant-7" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatalf("request=%s authorization=%q tenant_header=%q", r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Tenant-ID"))
		}
		var body WorkbenchAssetResourceGrantRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ResourceID != applicationID || body.SubjectID != "91" || body.Permission != WorkbenchDataApplicationExecutePermission {
			t.Fatalf("body=%#v", body)
		}
		status := "effective"
		if r.Method == http.MethodDelete {
			status = "revoked"
		}
		_ = json.NewEncoder(w).Encode(WorkbenchAssetResourceGrantResponse{ID: "rule-1", SourceIdentity: "73", Status: status})
	}))
	defer server.Close()

	client := NewWorkbenchResourceGrantClient(server.URL, staticWorkbenchGrantTokenSource("asset-tenant-7"), server.Client()).WithTenantID(7)
	if _, err := client.FulfillAssetGrant(context.Background(), 73, request); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RevokeAssetGrant(context.Background(), 73, request); err != nil {
		t.Fatal(err)
	}
}

type staticWorkbenchGrantTokenSource string

func (source staticWorkbenchGrantTokenSource) Token(_ context.Context, _ uint) (string, error) {
	return string(source), nil
}
