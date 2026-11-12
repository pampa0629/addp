package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

func TestCDCTaskListRejectsUnsupportedAndProviderFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewTaskHandler(service.NewTaskService(newTransferTaskHandlerTestDB(t), nil, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) { setTransferTestAuthContext(t, c, 7, 9); c.Next() })
	router.GET("/task-definitions", h.ListTasks)
	router.GET("/task-provider/tasks", h.ProviderListTasks)
	for _, path := range []string{"/task-definitions?change_detection=unknown", "/task-definitions?change_detection=cdc&runtime_boundary=bounded", "/task-provider/tasks?change_detection=cdc"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}
