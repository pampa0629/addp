package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestExportEarlyRefusalsAreNotCacheable(t *testing.T) {
	handler := NewExportHandler(nil)
	for _, item := range []struct {
		name   string
		handle gin.HandlerFunc
		status int
	}{
		{"create_unavailable", handler.CreateExport, http.StatusInternalServerError},
		{"status_invalid_id", handler.GetExport, http.StatusBadRequest},
		{"download_invalid_id", handler.DownloadExportFile, http.StatusBadRequest},
	} {
		t.Run(item.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/test/:id", item.handle)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test/invalid", nil))
			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != item.status || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("export refusal must prohibit caching: status=%d headers=%v", response.StatusCode, response.Header)
			}
		})
	}
}

func TestPreviewMissingLocatorIsNotCacheable(t *testing.T) {
	router := gin.New()
	router.GET("/preview", NewExplorerHandler(nil, nil, nil, nil, nil).Preview)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/preview", nil))
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("preview validation refusal may be cached: status=%d headers=%v", response.StatusCode, response.Header)
	}
}
