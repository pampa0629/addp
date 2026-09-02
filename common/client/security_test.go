package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityClientUsesTenantBearerChangesAndAcknowledgementContracts(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer tenant-7" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatalf("auth headers = %#v", r.Header)
		}
		switch requests {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/api/v1/security/runtime/protection-projections/changes" || r.URL.Query().Get("after_cursor") != "cursor-1" || r.URL.Query().Get("limit") != "200" {
				t.Fatalf("changes request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"schema_version":"addp.protection_projection_changes/v1","changes":[],"next_cursor":"cursor-1","has_more":false}`))
		case 2:
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/security/runtime/protection-projection-acknowledgements" {
				t.Fatalf("ack request = %s %s", r.Method, r.URL.Path)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["applied_cursor"] != "cursor-1" {
				t.Fatalf("ack body = %#v, err = %v", body, err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request count %d", requests)
		}
	}))
	defer server.Close()

	tokens := ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		if tenantID != 7 {
			t.Fatalf("tenant = %d", tenantID)
		}
		return "tenant-7", nil
	})
	client := NewSecurityClient(server.URL, tokens, server.Client()).WithTenantID(7)
	changes, err := client.ListProtectionProjectionChanges(context.Background(), "cursor-1", 200)
	if err != nil || changes.SchemaVersion != "addp.protection_projection_changes/v1" {
		t.Fatalf("changes = %#v, err = %v", changes, err)
	}
	if err := client.AcknowledgeProtectionProjectionCursor(context.Background(), "cursor-1"); err != nil {
		t.Fatal(err)
	}
}
