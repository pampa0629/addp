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

func TestStandardClientRequiresActiveLifecycleForReferenceValidation(t *testing.T) {
	for _, resource := range []struct {
		name     string
		path     string
		validate func(*StandardClient) error
	}{
		{
			name: "domain",
			path: "/api/v1/standard/domains/42",
			validate: func(client *StandardClient) error {
				return client.ValidateDomain(context.Background(), 42)
			},
		},
		{
			name: "element",
			path: "/api/v1/standard/elements/42",
			validate: func(client *StandardClient) error {
				return client.ValidateElement(context.Background(), 42)
			},
		},
	} {
		resource := resource
		t.Run(resource.name, func(t *testing.T) {
			lifecycleState := "active"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != resource.path {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":42,"tenant_id":7,"lifecycle_state":"` + lifecycleState + `"}`))
			}))
			defer server.Close()

			client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
				return "tenant-token", nil
			}), server.Client()).WithTenantID(7)
			if err := resource.validate(client); err != nil {
				t.Fatalf("active validation error = %v", err)
			}
			lifecycleState = "deleting"
			if err := resource.validate(client); !errors.Is(err, ErrStandardReferenceDeleting) {
				t.Fatalf("deleting validation error = %v, want ErrStandardReferenceDeleting", err)
			}
		})
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
		_, _ = w.Write([]byte(`{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","enabled":true,"severity":"error","message":"required","params":{}}]}`))
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

func TestStandardClientListsElementSummariesByCanonicalIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/standard/elements" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("ids"); got != "12,7" {
			t.Fatalf("ids = %q, want %q", got, "12,7")
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("page_size") != "100" {
			t.Fatalf("pagination query = %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":12,"name":"Order ID","code":"order_id"},{"id":7,"name":"Customer ID","code":"customer_id"}],"total":2,"page":1,"page_size":100,"total_pages":1}`))
	}))
	defer server.Close()

	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	elements, err := client.ListElementSummaries(context.Background(), []int64{12, 7})
	if err != nil {
		t.Fatalf("ListElementSummaries() error = %v", err)
	}
	if len(elements) != 2 || elements[0].ID != 12 || elements[0].Code != "order_id" {
		t.Fatalf("elements = %#v", elements)
	}
}

func TestStandardClientListsElementCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/standard/elements" || r.URL.Query().Get("keyword") != "gender" {
			t.Fatalf("unexpected Standard request: %s", r.URL.String())
		}
		if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("page_size") != "20" {
			t.Fatalf("pagination query = %q", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer tenant-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":12,"name":"Gender","code":"gender","quality_rules":{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","enabled":true,"severity":"error","message":"required","params":{}}]}}],"total":1,"page":2,"page_size":20,"total_pages":1}`))
	}))
	defer server.Close()

	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	elements, total, err := client.ListElementCandidates(context.Background(), "gender", 2, 20)
	if err != nil {
		t.Fatalf("ListElementCandidates() error = %v", err)
	}
	if total != 1 || len(elements) != 1 || elements[0].ID != 12 || len(elements[0].QualityRules.EnabledRules()) != 1 {
		t.Fatalf("element candidates = %#v, total=%d", elements, total)
	}
}
