package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/gateway/pkg/client"
	"github.com/gin-gonic/gin"
)

func TestConsumerRateLimiterRejectsMissingRedis(t *testing.T) {
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("api_consumer_info", &client.APIConsumerCredentialValidationResponse{APIConsumerID: 7, RateLimitPerMinute: 2})
	})
	router.Use(NewRateLimiterMiddleware(nil).Handler())
	router.GET("/query", func(c *gin.Context) { t.Fatal("unlimited request reached downstream") })
	for locale, message := range map[string]string{"zh-cn": "限流服务暂时不可用", "en": "Rate limit service is temporarily unavailable"} {
		w := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/query", nil)
		request.Header.Set("Accept-Language", locale)
		router.ServeHTTP(w, request)
		if w.Code != 503 || !strings.Contains(w.Body.String(), "rate_limit_unavailable") || !strings.Contains(w.Body.String(), message) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
