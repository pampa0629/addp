package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDWLayerRejectsQualityPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewDWLayerHandler(nil)
	router := gin.New()
	router.POST("/dw-layers", handler.CreateDWLayer)
	router.PUT("/dw-layers/:id", handler.UpdateDWLayer)
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		path := "/dw-layers"
		body := `{"layer_code":"dwd","layer_name":"DWD","sort_order":0,"quality_sla":{"pass_rate":99}}`
		if method == http.MethodPut {
			path += "/1"
			body = `{"version":1,"layer_name":"DWD","sort_order":0,"quality_sla":{"pass_rate":99}}`
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", method, response.Code, response.Body.String())
		}
	}
}
