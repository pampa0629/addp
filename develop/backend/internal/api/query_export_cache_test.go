package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestQueryExportEarlyRefusalsAreNotCacheable(t *testing.T) {
	handler := NewExecutionHandler(nil, nil, nil)
	for _, item := range []struct {
		name   string
		handle gin.HandlerFunc
		status int
	}{
		{"create_unavailable", handler.CreateQueryExport, http.StatusServiceUnavailable},
		{"status_invalid_id", handler.GetQueryExport, http.StatusBadRequest},
		{"download_invalid_id", handler.DownloadQueryExport, http.StatusBadRequest},
	} {
		t.Run(item.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/test/:id", item.handle)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test/invalid", nil))
			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != item.status || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("query export refusal must prohibit caching: status=%d headers=%v", response.StatusCode, response.Header)
			}
		})
	}
}
