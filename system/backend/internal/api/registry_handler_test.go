package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

func TestRegisterCapabilityRejectsMissingNonBuiltinCapabilities(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	handler := NewRegistryHandler(service.NewRegistryService(&repository.EngineRepository{}), nil)
	router.POST("/registry/capabilities", handler.RegisterCapability)

	body := `{
		"name":"Custom Runtime",
		"engine_type":"custom_runtime",
		"is_builtin":false
	}`
	req := httptest.NewRequest(http.MethodPost, "/registry/capabilities", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "capabilities is required") {
		t.Fatalf("body = %s, want capabilities is required error", rec.Body.String())
	}
}
