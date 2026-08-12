package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
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
		{name: "classification referenced", err: service.ErrClassificationReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The classification is still referenced by a child classification or element"},
		{name: "metric category referenced", err: service.ErrMetricCategoryReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The metric category is still referenced by a child category or metric"},
		{name: "measurement category referenced", err: service.ErrMeasurementCategoryReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The measurement category is still referenced by a unit"},
		{name: "unit referenced", err: service.ErrUnitReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The unit is still referenced by an element or metric"},
		{name: "code set referenced", err: service.ErrCodeSetReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The code set is still referenced by an element"},
		{name: "code item referenced", err: service.ErrCodeItemReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The code item is still referenced by a child code item"},
		{name: "metric referenced", err: service.ErrMetricReferenced, fallback: http.StatusInternalServerError, wantStatus: http.StatusConflict, wantBody: "The metric is still referenced by a derived metric or dependency"},
		{name: "dependency cycle", err: repository.ErrMetricDependencyCycle, fallback: http.StatusBadRequest, wantStatus: http.StatusBadRequest, wantBody: "Metric dependencies cannot form a cycle"},
		{name: "domain parent cycle", err: service.ErrDomainParentCycle, fallback: http.StatusBadRequest, wantStatus: http.StatusBadRequest, wantBody: "A domain parent cannot be itself or its descendant"},
		{name: "system unit", err: service.ErrSystemUnitImmutable, fallback: http.StatusBadRequest, wantStatus: http.StatusConflict, wantBody: "System units cannot be updated or deleted"},
		{name: "invalid tenant reference", err: repository.ErrInvalidTenantReference, fallback: http.StatusBadRequest, wantStatus: http.StatusBadRequest, wantBody: "A referenced resource does not exist or belongs to another tenant"},
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
