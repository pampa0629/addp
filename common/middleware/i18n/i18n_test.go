package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestLanguageUsesStandardContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ header, want string }{{"en-US,en;q=0.9", "en"}, {"zh-CN,zh;q=0.9", "zh-cn"}, {"fr", "zh-cn"}, {"", "zh-cn"}} {
		t.Run(tc.header, func(t *testing.T) {
			router := gin.New()
			router.Use(I18nMiddleware())
			router.GET("/", func(c *gin.Context) {
				if Language(c.Request.Context()) != tc.want || GetLang(c) != tc.want {
					t.Fatal("handler and service language differ")
				}
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept-Language", tc.header)
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != http.StatusNoContent {
				t.Fatal(out.Code)
			}
		})
	}
}

func TestRestoredLanguageRejectsHeaderSyntax(t *testing.T) {
	for _, lang := range []string{"", "en-US", "EN", "en,zh-cn", "fr", " en"} {
		if _, err := WithLanguage(context.Background(), lang); err == nil {
			t.Fatalf("accepted %q", lang)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restored, err := WithLanguage(ctx, LangEn)
	if err != nil || Language(restored) != LangEn {
		t.Fatal(err)
	}
	cancel()
	if restored.Err() != context.Canceled {
		t.Fatal("language context lost cancellation")
	}
}
