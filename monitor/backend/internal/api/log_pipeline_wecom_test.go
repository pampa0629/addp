package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

func TestPlatformLogWeComTestFailureReturnsSafeBadGateway(t *testing.T) {
	for _, err := range []error{service.ErrWeComRejected, service.ErrWeComRateLimited, service.ErrWeComResponse, service.ErrWeComNetwork, service.ErrWeComHTTP} {
		router := gin.New()
		router.GET("/test", func(c *gin.Context) { logRespond(c, nil, err) })
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/test", nil))
		if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"error_code":"wecom_test_failed"`) || strings.Contains(response.Body.String(), "https://") {
			t.Fatalf("unsafe or false success response: %d %s", response.Code, response.Body.String())
		}
	}
}
