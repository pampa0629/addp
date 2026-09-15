package api

import (
	commoni18n "github.com/addp/common/middleware/i18n"
	svc "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParameterOptionErrorsUseRequestLanguage(t *testing.T) {
	for _, tc := range []struct{ lang, text string }{{"zh-cn", "参数选项"}, {"en", "Invalid parameter options"}} {
		router := gin.New()
		router.Use(commoni18n.I18nMiddleware())
		router.GET("/", func(c *gin.Context) { writeQueryExecutionError(c, svc.ErrInvalidParameterOptions) })
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Accept-Language", tc.lang)
		router.ServeHTTP(recorder, request)
		if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), tc.text) {
			t.Fatalf("%s: %d %s", tc.lang, recorder.Code, recorder.Body.String())
		}
	}
}
