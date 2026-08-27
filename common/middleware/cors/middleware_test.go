package cors

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSAllowsServiceQueryIntentHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORS())
	router.POST("/query", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := httptest.NewRequest(http.MethodOptions, "/query", nil)
	request.Header.Set("Access-Control-Request-Headers", "X-ADDP-Query-Intent")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if allowed := response.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(allowed), strings.ToLower("X-ADDP-Query-Intent")) {
		t.Fatalf("allowed headers = %q", allowed)
	}
	if exposed := response.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(strings.ToLower(exposed), strings.ToLower("X-ADDP-Has-More")) ||
		!strings.Contains(strings.ToLower(exposed), strings.ToLower("X-ADDP-Service-Version")) {
		t.Fatalf("exposed headers = %q", exposed)
	}
}
