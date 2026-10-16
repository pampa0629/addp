package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/addp/common/dataprotection"
	commoni18n "github.com/addp/common/middleware/i18n"
	protection "github.com/addp/service/internal/protection"
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

func TestQueryProtectionRefusalUsesSafeLocalizedContract(t *testing.T) {
	for _, cause := range []error{protection.ErrRequired, dataprotection.ErrDenied, fmt.Errorf("private-sentinel password=credential: %w", protection.ErrRequired), fmt.Errorf("result: %w", dataprotection.ErrDenied)} {
		for _, language := range []string{"zh-cn", "en"} {
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.GET("/", func(c *gin.Context) { writeQueryExecutionError(c, cause) })
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("Accept-Language", language)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			var payload map[string]interface{}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusForbidden || payload["error_code"] != "data_protection_required" || len(payload) != 2 {
				t.Fatalf("unexpected protection refusal: %d %v", recorder.Code, payload)
			}
			text, _ := payload["error"].(string)
			want := "数据保护"
			if language == "en" {
				want = "data protection"
			}
			if !strings.Contains(text, want) || strings.Contains(text, "private-sentinel") || strings.Contains(text, "credential") {
				t.Fatalf("unsafe or untranslated refusal: %q", text)
			}
			if queryExecutionErrorCode(cause) != "data_protection_required" {
				t.Fatal("audit error code differs")
			}
		}
	}
	for _, cause := range []error{errors.New(protection.ErrRequired.Error()), errors.New("database connection failed")} {
		if queryExecutionErrorCode(cause) != "query_execution_failed" {
			t.Fatal("ordinary error classified as protection refusal")
		}
	}
}
