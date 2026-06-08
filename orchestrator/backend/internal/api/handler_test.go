package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStandardizeTaskListResponseSupportsTopLevelArray(t *testing.T) {
	handler := &OrchestrationHandler{}

	result := handler.standardizeTaskListResponse([]interface{}{
		map[string]interface{}{
			"id":   float64(1),
			"name": "Meta scan",
		},
	})

	if result["total"] != 1 {
		t.Fatalf("total = %#v, want 1", result["total"])
	}
	items, ok := result["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one item", result["items"])
	}
	item, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("item = %#v, want object", items[0])
	}
	if item["display_name"] != "Meta scan" {
		t.Fatalf("display_name = %#v, want Meta scan", item["display_name"])
	}
}

func TestListProviderOrchestrationTasksRejectsUnsupportedTaskType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &OrchestrationHandler{}
	router := gin.New()
	router.GET("/tasks", handler.ListProviderOrchestrationTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?task_type=query", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
