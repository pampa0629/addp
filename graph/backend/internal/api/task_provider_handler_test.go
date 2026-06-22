package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"github.com/gin-gonic/gin"
)

func TestGraphTaskListResponseUsesStandardItemsShape(t *testing.T) {
	body, err := json.Marshal(graphTaskListResponse{
		Items: []graphTaskListItem{{
			ID:       1,
			TenantID: 7,
			TaskType: commonExecution.TaskTypeKGBuild,
			Name:     "kg build",
			Enabled:  true,
		}},
		Total:    1,
		Page:     1,
		PageSize: 100,
	})
	if err != nil {
		t.Fatalf("marshal graphTaskListResponse: %v", err)
	}

	assertStandardTaskProviderListShape(t, body)
}

func TestGraphTaskDetailResponseUsesStandardTaskShape(t *testing.T) {
	body, err := json.Marshal(graphTaskProviderListItem(models.BuildTask{
		ID:       1,
		TenantID: 7,
		GraphID:  9,
		Name:     "kg build",
		Status:   "pending",
	}))
	if err != nil {
		t.Fatalf("marshal graph task detail: %v", err)
	}

	assertStandardTaskProviderDetailShape(t, body)
}

func TestExecuteProviderTaskRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/tasks/:task_type/:id/execute", NewTaskProviderHandler(nil, nil).ExecuteProviderTask)

	req := httptest.NewRequest(http.MethodPost, "/tasks/kg_build/1/execute", strings.NewReader(`{"legacy":true}`))
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
	for _, field := range []string{"id", "task_type", "name", "enabled"} {
		if _, ok := resp[field]; !ok {
			t.Fatalf("response missing %q: %s", field, body)
		}
	}
	for _, legacyField := range []string{"data", "message"} {
		if _, ok := resp[legacyField]; ok {
			t.Fatalf("response contains non-standard field %q: %s", legacyField, body)
		}
	}
}
