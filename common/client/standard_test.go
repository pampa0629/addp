package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStandardClientPreservesTenantAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/standard/domains/42" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"domain not found","error_code":"domain_not_found"}`))
	}))
	defer server.Close()

	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	err := client.ValidateDomain(context.Background(), 42)

	var apiError *TenantAPIError
	if !errors.As(err, &apiError) {
		t.Fatalf("ValidateDomain() error type = %T, want *TenantAPIError", err)
	}
	if apiError.StatusCode != http.StatusNotFound || apiError.ErrorCode != "domain_not_found" {
		t.Fatalf("TenantAPIError = %#v", apiError)
	}
	if apiError.Method != http.MethodGet || apiError.Path != "/api/v1/standard/domains/42" {
		t.Fatalf("TenantAPIError request = %s %s", apiError.Method, apiError.Path)
	}
}

func TestTenantAPIStatusCodeRejectsTransportFailure(t *testing.T) {
	err := &TenantTransportError{Method: http.MethodGet, Path: "/api/v1/standard/domains/42", Cause: errors.New("connection refused")}
	if status, ok := TenantAPIStatusCode(err); ok || status != 0 {
		t.Fatalf("TenantAPIStatusCode() = %d, %t, want 0, false", status, ok)
	}
	var transportError *TenantTransportError
	if !errors.As(err, &transportError) || transportError.Path != "/api/v1/standard/domains/42" {
		t.Fatalf("transport error = %#v", transportError)
	}
}

func TestStandardClientHidesCrossTenantReference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"tenant_id":8}`))
	}))
	defer server.Close()

	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	err := client.ValidateDomain(context.Background(), 42)
	if !errors.Is(err, ErrTenantReferenceNotFound) {
		t.Fatalf("ValidateDomain() error = %v, want ErrTenantReferenceNotFound", err)
	}
	if strings.Contains(err.Error(), "tenant 8") {
		t.Fatalf("ValidateDomain() leaked owner tenant: %v", err)
	}
}

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
