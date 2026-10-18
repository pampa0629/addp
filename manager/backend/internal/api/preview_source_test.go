package api

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/manager/internal/preview"
	"github.com/gin-gonic/gin"
)

func TestPreviewSourceErrorsPauseWithoutPublishingPrivateFacts(t *testing.T) {
	for _, lang := range []string{"zh-cn", "en"} {
		for _, tc := range []struct {
			err    error
			status int
			code   string
		}{
			{preview.ErrPreviewSourceRefreshRequired, 409, "preview_source_refresh_required"},
			{preview.ErrPreviewSourceUnavailable, 503, "preview_source_unavailable"},
		} {
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.GET("/preview", func(c *gin.Context) {
				if !writePreviewSourceError(c, errors.Join(tc.err, errors.New("private-storage-path"))) {
					t.Fatal("source error not handled")
				}
			})
			w := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/preview", nil)
			request.Header.Set("Accept-Language", lang)
			router.ServeHTTP(w, request)
			body := w.Body.String()
			if w.Code != tc.status || !strings.Contains(body, `"error_code":"`+tc.code+`"`) || strings.Contains(body, "private-storage-path") || strings.Contains(body, `"rows"`) || strings.Contains(body, "manager.error.") {
				t.Fatalf("invalid source error: %d %s", w.Code, body)
			}
			if tc.status == 409 && !strings.Contains(body, map[string]string{"zh-cn": "刷新元数据", "en": "Refresh metadata"}[lang]) {
				t.Fatalf("missing localized refresh instruction: %s", body)
			}
		}
	}
}
