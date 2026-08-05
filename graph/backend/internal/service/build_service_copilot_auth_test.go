package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
)

func TestCallCopilotExtractSendsTenantServiceToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != copilotExtractPath {
			t.Errorf("path = %q, want %q", r.URL.Path, copilotExtractPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer service-token" {
			t.Errorf("Authorization = %q, want Bearer service-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entities":[],"relations":[]}`))
	}))
	defer server.Close()

	service := &BuildService{
		copilotURL: server.URL,
		serviceTokenSource: commonClient.ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
			if tenantID != 7 {
				t.Fatalf("tenant ID = %d, want 7", tenantID)
			}
			return "service-token", nil
		}),
		httpClient: server.Client(),
	}
	result, err := service.callCopilotExtract(
		context.Background(),
		7,
		"铁路连接两个城市",
		"测试文档",
		&ontologySchemaDTO{EntityTypes: []entityTypeDTO{{Name: "city"}}},
		0.8,
	)
	if err != nil {
		t.Fatalf("callCopilotExtract() error = %v", err)
	}
	if len(result.Entities) != 0 || len(result.Relations) != 0 {
		t.Fatalf("unexpected extraction result: %#v", result)
	}
}
