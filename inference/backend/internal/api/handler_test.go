package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/inference/internal/service"
	"github.com/gin-gonic/gin"
)

func TestProviderInputRejectsCredentialField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/provider-connections", strings.NewReader(`{
		"name":"provider",
		"scope_type":"platform",
		"adapter_type":"openai_compatible",
		"endpoint":"https://example.test/v1",
		"credential":"must-use-dedicated-operation"
	}`))
	context.Request.Header.Set("Content-Type", "application/json")

	var input service.ProviderInput
	if bind(context, &input) {
		t.Fatal("ordinary provider input must reject credential fields")
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
