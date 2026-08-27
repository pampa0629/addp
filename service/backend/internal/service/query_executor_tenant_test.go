package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/service/internal/models"
)

func TestDirectQueryLoadsEngineInServiceTenantContext(t *testing.T) {
	t.Parallel()

	var gotTenantID uint
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		http.Error(w, "stop after tenant-scoped engine lookup", http.StatusInternalServerError)
	}))
	defer server.Close()

	systemClient := client.NewSystemClient(server.URL, client.ServiceTokenProviderFunc(
		func(_ context.Context, tenantID uint) (string, error) {
			gotTenantID = tenantID
			return "addp_at_tenant_service", nil
		},
	))
	executor := NewQueryExecutorService(systemClient, nil, []byte("0123456789abcdef0123456789abcdef"))
	engineID := uint(26)
	_, err := executor.ExecuteQuery(context.Background(), &models.QueryService{
		TenantID: 7,
		EngineID: &engineID,
	}, &models.QueryExecutionRequest{})
	if err == nil {
		t.Fatal("ExecuteQuery() error = nil, want engine lookup failure")
	}
	if gotTenantID != 7 {
		t.Fatalf("engine lookup tenant ID = %d, want 7", gotTenantID)
	}
	if gotPath != "/api/v1/system/engines/26" {
		t.Fatalf("engine lookup path = %q, want /api/v1/system/engines/26", gotPath)
	}
}
