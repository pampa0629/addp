package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceClientUsesTenantBearerWithoutLegacyHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/service/endpoints" || r.URL.Query().Get("ref") != "query:12" {
			t.Errorf("request target = %s", r.URL.RequestURI())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer portal-tenant-token" {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Errorf("legacy headers were sent: %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"service_type":"query","title":"Query","endpoints":{"http":"/api/query/q"}}`))
	}))
	defer server.Close()

	client := NewServiceClient(server.URL, ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		if tenantID != 7 {
			t.Fatalf("tenantID = %d", tenantID)
		}
		return "portal-tenant-token", nil
	}), server.Client())
	result, err := client.GetEndpointsByRef(context.Background(), 7, "query:12")
	if err != nil {
		t.Fatalf("GetEndpointsByRef: %v", err)
	}
	if result.ServiceType != "query" || result.Title != "Query" {
		t.Fatalf("result = %#v", result)
	}
}
