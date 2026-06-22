package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

func TestTaskProviderRegisterRejectsUnknownTopLevelFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.POST("/task-providers/register", NewTaskProviderHandler(service.NewTaskProviderService(nil)).RegisterOrUpdate)

	body := `{
		"module_name":"meta",
		"display_name":"Meta",
		"description":"Meta provider",
		"base_url":"http://localhost:8082",
		"task_list_endpoint":"/api/v1/meta/tasks",
		"task_detail_endpoint":"/api/v1/meta/tasks/{task_type}/{id}",
		"task_execute_endpoint":"/api/v1/meta/tasks/{task_type}/{id}/execute",
		"task_status_endpoint":"/api/v1/meta/executions/{execution_id}",
		"create_task_url":"/meta/scan",
		"capabilities":{
			"schema_version":"task.capabilities/v1",
			"task_capabilities":[{
				"type":"scan",
				"display_name":"扫描任务",
				"description":"执行元数据扫描",
				"definition_schema":{"type":"object"},
				"execution_schema":{"type":"object"},
				"supports_schedule":true,
				"supports_cancel":false,
				"supports_inline_execution":false,
				"create_url":"/meta/scan",
				"edit_url":"/meta/scan?task_id=:id",
				"deprecated":false
			}]
		},
		"is_enabled":true
	}`
	req := httptest.NewRequest(http.MethodPost, "/task-providers/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown field") || !strings.Contains(rec.Body.String(), "create_task_url") {
		t.Fatalf("body = %s, want unknown create_task_url error", rec.Body.String())
	}
}
