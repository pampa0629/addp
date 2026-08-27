package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQualityClientResolveCatalogSummaries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/quality/runtime/catalog-summaries/resolve" || r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("request = %s authorization=%s", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"reference":{"engine_id":7,"schema_name":"public","table_name":"orders"},"configured":true,"check_task_id":31,"last_execution_status":"success","quality_score":97.5,"open_issue_count":2}]}`))
	}))
	defer server.Close()
	client := NewQualityClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "token", nil }), nil).WithTenantID(8)
	result, err := client.ResolveCatalogSummaries(context.Background(), []QualityCatalogSummaryReference{{EngineID: 7, SchemaName: "public", TableName: "orders"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].QualityScore == nil || *result.Results[0].QualityScore != 97.5 {
		t.Fatalf("result = %#v", result)
	}
}
