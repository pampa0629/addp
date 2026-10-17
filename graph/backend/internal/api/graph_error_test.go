package api

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/graph/i18n"
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
