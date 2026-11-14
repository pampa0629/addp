package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/develop/backend/internal/repository"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

func TestTaskLatestExecutionReadFailureIsNotTaskAbsence(t *testing.T) {
	db := newNotebookHandlerTestDB(t)
	if err := db.Exec(`INSERT INTO develop.dev_tasks (id, tenant_id, name, dev_type, content, execution_config, editor_layout, timeout, tags, created_by, status) VALUES (14, 7, 'latest', 'script', CAST('{"notebook_path":"latest.ipynb"}' AS BLOB), CAST('{}' AS BLOB), CAST('{}' AS BLOB), 600, '{}', 1, 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	taskService := service.NewDevTaskService(repository.NewDevTaskRepository(db), nil)
	tasks := NewDevTaskHandler(taskService, nil)
	notebooks := NewNotebookHandler(nil, nil, taskService, nil, "", "")
	router := gin.New()
	router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 7, 3); c.Next() })
	router.GET("/task-definitions/:id", tasks.GetDevTask)
	router.GET("/task-definitions", tasks.ListDevTasks)
	router.GET("/task-provider/tasks/:task_type/:id", tasks.ProviderGetDevTask)
	router.GET("/notebooks", notebooks.ListNotebooks)
	read := func(path string) *httptest.ResponseRecorder {
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, path, nil))
		return out
	}
	paths := []string{"/task-definitions/14", "/task-provider/tasks/script/14", "/task-definitions?page=1&page_size=20", "/notebooks?page=1&page_size=20"}
	for _, path := range paths {
		if out := read(path); out.Code != 200 {
			t.Fatalf("baseline %s = %d %s", path, out.Code, out.Body.String())
		}
	}
	if err := db.Exec("DROP TABLE common.task_executions").Error; err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			out := read(path)
			if out.Code != 500 || !strings.Contains(out.Body.String(), `"error_code":"execution_read_failed"`) || strings.Contains(out.Body.String(), "task_executions") {
				t.Fatalf("read failure = %d %s", out.Code, out.Body.String())
			}
		})
	}
	for _, path := range []string{"/task-definitions/99", "/task-provider/tasks/script/99"} {
		if out := read(path); out.Code != 404 {
			t.Fatalf("absent task %s = %d %s", path, out.Code, out.Body.String())
		}
	}
}
