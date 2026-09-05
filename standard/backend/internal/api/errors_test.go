package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

func TestRespondErrorMapsStructuredErrorsAndLanguage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		err        error
		fallback   int
		wantStatus int
		wantBody   string
	}{
		{name: "not found", err: commonapi.ErrNotFound, fallback: http.StatusInternalServerError, wantStatus: http.StatusNotFound, wantBody: "Resource not found"},
		{name: "conflict", err: commonapi.ErrConflict, fallback: http.StatusBadRequest, wantStatus: http.StatusConflict, wantBody: "Resource code or relation already exists"},
		{name: "domain referenced", err: service.ErrDomainReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The domain is still referenced by a child domain, glossary, element, metric, or dimension hierarchy"},
		{name: "metric category referenced", err: service.ErrMetricCategoryReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The metric category is still referenced by a child category or metric"},
		{name: "measurement category referenced", err: service.ErrMeasurementCategoryReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The measurement category is still referenced by a unit"},
		{name: "unit referenced", err: service.ErrUnitReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The unit is still referenced by an element or metric"},
		{name: "code set referenced", err: service.ErrCodeSetReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The code set is still referenced by an element"},
		{name: "code item referenced", err: service.ErrCodeItemReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The code item is still referenced by a child code item"},
		{name: "metric referenced", err: service.ErrMetricReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The metric is still referenced by a derived metric or dependency"},
		{name: "glossary publication history", err: service.ErrGlossaryPublicationHistory, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "A glossary with publication history cannot be deleted"},
		{name: "dependency cycle", err: repository.ErrMetricDependencyCycle, fallback: http.StatusBadRequest, wantStatus: http.StatusBadRequest, wantBody: "Metric dependencies cannot form a cycle"},
		{name: "domain parent cycle", err: service.ErrDomainParentCycle, fallback: http.StatusBadRequest, wantStatus: http.StatusBadRequest, wantBody: "A domain parent cannot be itself or its descendant"},
		{name: "system unit", err: service.ErrSystemUnitImmutable, fallback: http.StatusBadRequest, wantStatus: http.StatusConflict, wantBody: "System units cannot be updated or deleted"},
		{name: "invalid tenant reference", err: repository.ErrInvalidTenantReference, fallback: http.StatusInternalServerError, wantStatus: http.StatusBadRequest, wantBody: "A referenced resource does not exist or belongs to another tenant"},
		{name: "invalid standard scope", err: service.ErrInvalidStandardScope, fallback: http.StatusInternalServerError, wantStatus: http.StatusBadRequest, wantBody: "The scope and owning domain are inconsistent"},
		{name: "unknown bad request", err: fmt.Errorf("binding internals"), fallback: http.StatusBadRequest, wantStatus: http.StatusBadRequest, wantBody: "Invalid request parameters"},
		{name: "wrapped document not found", err: fmt.Errorf("link document: %w", commonapi.ErrNotFound), fallback: http.StatusBadRequest, wantStatus: http.StatusNotFound, wantBody: "Resource not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Set("addp_lang", "en")

			respondError(context, tt.fallback, tt.err)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if !strings.Contains(response.Body.String(), tt.wantBody) {
				t.Fatalf("body = %s, want %q", response.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestRespondErrorReturnsInvalidStandardScopeCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("addp_lang", "en")

	respondError(context, http.StatusInternalServerError, service.ErrInvalidStandardScope)

	var response struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ErrorCode != "invalid_standard_scope" {
		t.Fatalf("error_code = %q", response.ErrorCode)
	}
}

func TestRespondErrorReturnsGlossaryPublicationHistoryCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("addp_lang", "en")

	respondError(context, http.StatusInternalServerError, service.ErrGlossaryPublicationHistory)

	var response struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusConflict || response.ErrorCode != "glossary_publication_history" {
		t.Fatalf("status=%d error_code=%q", recorder.Code, response.ErrorCode)
	}
}

func TestRespondErrorReturnsStandardReferenceConflictContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/standard/domains/42", nil)

	respondError(context, http.StatusInternalServerError, &service.StandardResourceReferencedError{
		Impact: &commonclient.StandardReferenceGuardResponse{
			ResourceType:   "domain",
			ResourceID:     42,
			State:          commonclient.StandardReferenceGuardFrozen,
			ReferenceCount: 2,
			Summary: []commonclient.StandardReferenceImpactSummary{
				{OwnerType: "entity", Field: "domain_id", Count: 2},
			},
			Sample: []commonclient.StandardReferenceImpact{
				{OwnerType: "entity", OwnerID: 7, Field: "domain_id"},
			},
		},
	})

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var response struct {
		Error                    string                                        `json:"error"`
		ErrorCode                string                                        `json:"error_code"`
		ReferenceCount           int64                                         `json:"reference_count"`
		ReferenceSummary         []commonclient.StandardReferenceImpactSummary `json:"reference_summary"`
		ReferenceSample          []commonclient.StandardReferenceImpact        `json:"reference_sample"`
		ReferenceSampleTruncated bool                                          `json:"reference_sample_truncated"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error == "" || response.ErrorCode != "standard_resource_referenced" || response.ReferenceCount != 2 {
		t.Fatalf("response = %+v", response)
	}
	if len(response.ReferenceSummary) != 1 || response.ReferenceSummary[0].Count != 2 {
		t.Fatalf("reference summary = %+v", response.ReferenceSummary)
	}
	if len(response.ReferenceSample) != 1 || response.ReferenceSample[0].OwnerID != 7 {
		t.Fatalf("reference sample = %+v", response.ReferenceSample)
	}
}
