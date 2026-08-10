package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStandardClientReadsDirectVersionedQualityRuleDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/standard/elements/12/quality-rules" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tenant-token" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatalf("unexpected auth headers: Authorization=%q X-Tenant-ID=%q", r.Header.Get("Authorization"), r.Header.Get("X-Tenant-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema_version":"addp.quality.rules/v1","rules":[{"type":"not_null","enabled":true,"severity":"error","message":"required","params":{}}]}`))
	}))
	defer server.Close()

	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	document, err := client.GetElementQualityRules(context.Background(), 12)
	if err != nil {
		t.Fatalf("GetElementQualityRules() error = %v", err)
	}
	if document.SchemaVersion != "addp.quality.rules/v1" || len(document.Rules) != 1 || document.Rules[0].Type != "not_null" {
		t.Fatalf("quality rule document = %#v", document)
	}
}

func TestStandardClientRejectsLegacyQualityRuleEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"schema_version":"addp.quality.rules/v1","rules":[]}}`))
	}))
	defer server.Close()

	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	if _, err := client.GetElementQualityRules(context.Background(), 12); err == nil || !strings.Contains(err.Error(), "unknown field \"data\"") {
		t.Fatalf("GetElementQualityRules() error = %v, want legacy envelope rejection", err)
	}
}
