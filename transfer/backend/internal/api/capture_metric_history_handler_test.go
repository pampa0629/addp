package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCaptureMetricHistoryRejectsInvalidQueriesBeforeReading(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewTaskHandler(nil)
	r.GET("/task-definitions/:id/cdc-metric-history", h.GetCaptureMetricHistory)
	for _, query := range []string{"", "generation=0&start=2026-11-15T00:00:00Z&end=2026-11-15T01:00:00Z", "generation=01&start=x&end=x", "generation=1&start=x&end=x", "generation=1&start=2026-11-15T00:00:00Z&end=2099-01-01T00:00:00Z", "generation=1&generation=2&start=x&end=x", "generation=1&start=x&end=x&tenant_id=1"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/task-definitions/7/cdc-metric-history?"+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query=%s code=%d body=%s", query, w.Code, w.Body)
		}
	}
}
