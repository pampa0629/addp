package api

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/graph/i18n"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
)

func TestGraphIndexConflictUsesLocalizedConflictStatus(t *testing.T) {
	for _, language := range []string{"en", "zh-cn"} {
		t.Run(language, func(t *testing.T) {
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.GET("/search", func(c *gin.Context) {
				graphOperationError(c, fmt.Errorf("wrapped: %w", plugin.ErrGraphSearchIndexConflict))
			})
			request := httptest.NewRequest("GET", "/search", nil)
			request.Header.Set("Accept-Language", language)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 409 {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			expected := "fulltext"
			if language == "zh-cn" {
				expected = "全文索引"
			}
			if !strings.Contains(response.Body.String(), expected) {
				t.Fatalf("untranslated conflict: %s", response.Body.String())
			}
		})
	}
}

func TestGraphProtectionErrorDoesNotExposeSourceDetails(t *testing.T) {
	for _, language := range []string{"en", "zh-cn"} {
		router := gin.New()
		router.Use(commoni18n.I18nMiddleware())
		router.GET("/read", func(c *gin.Context) {
			graphOperationError(c, fmt.Errorf("private graph/password: %w", service.ErrGraphProtectionRequired))
		})
		request := httptest.NewRequest("GET", "/read", nil)
		request.Header.Set("Accept-Language", language)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 403 || strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), "password") {
			t.Fatalf("unsafe response: %d %s", response.Code, response.Body.String())
		}
		expected := "Data protection"
		if language == "zh-cn" {
			expected = "数据保护"
		}
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatal(response.Body.String())
		}
	}
}
