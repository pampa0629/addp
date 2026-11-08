package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSearchRejectsLegacyAndAmbiguousParametersAtBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewSearchHandler(nil, nil, nil)
	for _, query := range []string{"q=roads&page=1", "q=roads&tenant_id=7", "q=roads&q=hidden", "q=roads&page_size=0", "q=roads&page_size=51", "q=roads&cursor=untrusted", "q=roads&engine_id=-1", "q="} {
		t.Run(query, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 7, 1); c.Next() })
			router.GET("/search", handler.Search)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/search?"+query, nil))
			if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
