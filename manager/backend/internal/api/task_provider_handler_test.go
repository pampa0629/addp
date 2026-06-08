package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTaskProviderListTasksRejectsUnsupportedTaskType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewTaskProviderHandler(nil, nil, nil)
	router.GET("/tasks", handler.ListTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?task_type=unknown", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}
