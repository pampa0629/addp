package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallCopilotExtractSendsInternalAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != copilotExtractPath {
			t.Errorf("path = %q, want %q", r.URL.Path, copilotExtractPath)
		}
		if got := r.Header.Get("X-Internal-API-Key"); got != "shared-secret" {
			t.Errorf("X-Internal-API-Key = %q, want shared-secret", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entities":[],"relations":[]}`))
	}))
	defer server.Close()

	service := &BuildService{
		copilotURL:     server.URL,
		internalAPIKey: "shared-secret",
		httpClient:     server.Client(),
	}
	result, err := service.callCopilotExtract(
		context.Background(),
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
