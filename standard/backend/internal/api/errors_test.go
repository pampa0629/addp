package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/repository"
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
		{name: "dependency cycle", err: repository.ErrMetricDependencyCycle, fallback: http.StatusBadRequest, wantStatus: http.StatusBadRequest, wantBody: "Metric dependencies cannot form a cycle"},
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
