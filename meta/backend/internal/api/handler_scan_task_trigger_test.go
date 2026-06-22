package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
)

func TestProviderExecuteScanTaskRejectsUnknownFieldsBeforeServiceCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/tasks/:task_type/:id/execute", NewHandler(nil, nil, nil, nil, nil, nil).ProviderExecuteScanTask)

	req := httptest.NewRequest(http.MethodPost, "/tasks/scan/1/execute", strings.NewReader(`{"legacy":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unknown field") {
		t.Fatalf("body = %s, want unknown field error", w.Body.String())
	}
}

func TestProviderScanTaskListResponseUsesStandardItemsShape(t *testing.T) {
	body, err := json.Marshal(models.ListProviderScanTasksResponse{
		Items: []models.ProviderScanTask{{
			ID:       1,
			TenantID: 7,
			Name:     "scan task",
			TaskType: "scan",
			Enabled:  true,
		}},
		Total:    1,
		Page:     1,
		PageSize: 100,
	})
	if err != nil {
		t.Fatalf("marshal ListProviderScanTasksResponse: %v", err)
	}

	assertStandardTaskProviderListShape(t, body)
}

func assertStandardTaskProviderListShape(t *testing.T, body []byte) {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
	for _, field := range []string{"items", "total", "page", "page_size"} {
		if _, ok := resp[field]; !ok {
			t.Fatalf("response missing %q: %s", field, body)
		}
	}
	for _, legacyField := range []string{"data", "status", "message", "total_pages", "tasks"} {
		if _, ok := resp[legacyField]; ok {
			t.Fatalf("response contains non-standard field %q: %s", legacyField, body)
		}
	}
}
