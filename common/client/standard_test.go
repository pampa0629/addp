package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStandardClientResolvesExactReferencesInRequestOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/standard/references/resolve" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tenant-token" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatalf("unexpected auth headers: %#v", r.Header)
		}
		var request standardReferenceResolutionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.References) != 2 || request.References[0].ObjectType != "domain" || request.References[1].ID != 9 {
			t.Fatalf("request = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"object_type":"domain","id":7,"found":true,"referenceable":true,"name":"Sales","code":"sales","status":"active","version":3},{"object_type":"glossary","id":9,"found":false,"referenceable":false}]}`))
	}))
	defer server.Close()

	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	results, err := client.ResolveReferences(context.Background(), []StandardReference{
		{ObjectType: "domain", ID: 7},
		{ObjectType: "glossary", ID: 9},
	})
	if err != nil {
		t.Fatalf("ResolveReferences() error = %v", err)
	}
	if len(results) != 2 || !results[0].Referenceable || results[1].Found {
		t.Fatalf("results = %#v", results)
	}
}

func TestStandardClientRejectsMisorderedReferenceResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"object_type":"element","id":2,"found":true,"referenceable":true},{"object_type":"domain","id":1,"found":true,"referenceable":true}]}`))
	}))
	defer server.Close()

	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	_, err := client.ResolveReferences(context.Background(), []StandardReference{{ObjectType: "domain", ID: 1}, {ObjectType: "element", ID: 2}})
	if err == nil || !strings.Contains(err.Error(), "out of request order") {
		t.Fatalf("ResolveReferences() error = %v", err)
	}
}

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
				body := `{"id":42,"tenant_id":7,"lifecycle_state":"` + lifecycleState + `"}`
				if resource.name == "element" {
					body = `{"id":42,"tenant_id":7,"lifecycle_state":"` + lifecycleState + `","current_revision":{"id":101,"revision_no":2,"status":"published","name":"Customer ID","data_type":"string"}}`
				}
				_, _ = w.Write([]byte(body))
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

func TestStandardClientReadsPublishedRevisionQualityRuleSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/standard/elements/12/quality-rules" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tenant-token" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatalf("unexpected auth headers: Authorization=%q X-Tenant-ID=%q", r.Header.Get("Authorization"), r.Header.Get("X-Tenant-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"element_id":12,"element_revision_id":1201,"revision_no":3,"quality_rules":{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","enabled":true,"severity":"error","message":"required","params":{}}]}}`))
	}))
	defer server.Close()

	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	document, err := client.GetElementQualityRules(context.Background(), 12)
	if err != nil {
		t.Fatalf("GetElementQualityRules() error = %v", err)
	}
	if document.ElementID != 12 || document.ElementRevisionID != 1201 || document.RevisionNo != 3 || document.QualityRules.SchemaVersion != "addp.quality.rules/v1" || len(document.QualityRules.Rules) != 1 || document.QualityRules.Rules[0].Type != "not_null" {
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
	if _, err := client.GetElementQualityRules(context.Background(), 12); err == nil || !strings.Contains(err.Error(), "invalid element revision identity") {
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
		_, _ = w.Write([]byte(`{"data":[{"id":12,"code":"order_id","current_revision":{"id":1201,"revision_no":1,"status":"published","name":"Order ID","data_type":"string"}},{"id":7,"code":"customer_id","current_revision":{"id":701,"revision_no":2,"status":"published","name":"Customer ID","data_type":"string"}}],"total":2,"page":1,"page_size":100,"total_pages":1}`))
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

func TestStandardClientResolvesElementRevisionsAtOnePointInTime(t *testing.T) {
	asOf := time.Date(2026, 8, 28, 9, 30, 0, 123000000, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/standard/runtime/element-revisions/resolve" {
			t.Fatalf("unexpected Standard request: %s %s", r.Method, r.URL.String())
		}
		var request elementRevisionResolutionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.ElementIDs) != 2 || request.ElementIDs[0] != "12" || request.ElementIDs[1] != "7" || !request.AsOf.Equal(asOf) {
			t.Fatalf("request = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"element_id":"12","found":true,"snapshot":{"element_id":"12","element_revision_id":"1201","revision_no":3,"code":"order_id","name":"Order ID","definition":"Order identifier","data_type":"bigint","nullable":false,"value_domain_kind":"unrestricted","example_values":[],"effective_from":"2026-01-01T00:00:00Z"}},{"element_id":"7","found":true,"snapshot":{"element_id":"7","element_revision_id":"701","revision_no":2,"code":"customer_id","name":"Customer ID","definition":"Customer identifier","data_type":"bigint","nullable":false,"value_domain_kind":"unrestricted","example_values":[],"effective_from":"2026-01-01T00:00:00Z"}}]}`))
	}))
	defer server.Close()

	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	bindings, err := client.ResolveElementRevisions(context.Background(), []int64{12, 7, 12}, asOf)
	if err != nil {
		t.Fatalf("ResolveElementRevisions() error = %v", err)
	}
	if len(bindings) != 2 || bindings[12].RevisionID != 1201 || bindings[7].RevisionNo != 2 {
		t.Fatalf("bindings = %#v", bindings)
	}
}

func TestStandardClientRejectsMissingEffectiveElementRevision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"element_id":"12","found":false}]}`))
	}))
	defer server.Close()
	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	_, err := client.ResolveElementRevisions(context.Background(), []int64{12}, time.Now().UTC())
	if !errors.Is(err, ErrTenantReferenceNotFound) {
		t.Fatalf("ResolveElementRevisions() error = %v, want ErrTenantReferenceNotFound", err)
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
		_, _ = w.Write([]byte(`{"data":[{"id":12,"code":"gender","current_revision":{"id":1201,"revision_no":3,"status":"published","name":"Gender","data_type":"string","compiled_quality_rules":{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","enabled":true,"severity":"error","message":"required","params":{}}]}}}],"total":1,"page":2,"page_size":20,"total_pages":1}`))
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

func TestStandardClientListsReferenceCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/standard/references/candidates" ||
			r.URL.Query().Get("object_type") != "domain" || r.URL.Query().Get("search") != "sales" ||
			r.URL.Query().Get("page") != "2" || r.URL.Query().Get("page_size") != "20" {
			t.Fatalf("unexpected Standard request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"object_type":"domain","id":7,"name":"Sales","code":"sales","status":"active"}],"total":21,"page":2,"page_size":20,"total_pages":2}`))
	}))
	defer server.Close()
	client := NewStandardClient(server.URL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client()).WithTenantID(7)
	result, err := client.ListReferenceCandidates(context.Background(), "domain", " sales ", 2, 20)
	if err != nil || result.Total != 21 || len(result.Data) != 1 || result.Data[0].Code != "sales" {
		t.Fatalf("result = %#v, err=%v", result, err)
	}
}
