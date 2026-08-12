package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
	"github.com/gin-gonic/gin"
)

func TestQualityTaskListResponseUsesStandardItemsShape(t *testing.T) {
	body, err := json.Marshal(taskProviderTaskListResponse{
		Items: []taskProviderTaskListItem{{
			ID:       1,
			TenantID: 7,
			TaskType: commonExecution.TaskTypeQualityCheck,
			Name:     "quality check",
		}},
		Total:    1,
		Page:     1,
		PageSize: 100,
	})
	if err != nil {
		t.Fatalf("marshal taskProviderTaskListResponse: %v", err)
	}

	assertStandardTaskProviderListShape(t, body)
}

func TestQualityTaskDetailResponseUsesStandardTaskShape(t *testing.T) {
	body, err := json.Marshal(qualityTaskListItem(models.CheckTask{
		ID:       1,
		TenantID: 7,
		Name:     "quality check",
	}))
	if err != nil {
		t.Fatalf("marshal quality task detail: %v", err)
	}

	assertStandardTaskProviderDetailShape(t, body)
}

func TestTaskExecuteRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/tasks/:task_type/:id/execute", NewTaskProviderHandler(nil, nil).TaskExecute)

	req := httptest.NewRequest(http.MethodPost, "/tasks/check/1/execute", strings.NewReader(`{"legacy":true}`))
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

func assertStandardTaskProviderDetailShape(t *testing.T, body []byte) {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
	for _, field := range []string{"id", "task_type", "name", "status", "execution_contract"} {
		if _, ok := resp[field]; !ok {
			t.Fatalf("response missing %q: %s", field, body)
		}
	}
	if _, ok := resp["enabled"]; ok {
		t.Fatalf("response contains unsupported scheduling field enabled: %s", body)
	}
	for _, legacyField := range []string{"data", "message"} {
		if _, ok := resp[legacyField]; ok {
			t.Fatalf("response contains non-standard field %q: %s", legacyField, body)
		}
	}
}
