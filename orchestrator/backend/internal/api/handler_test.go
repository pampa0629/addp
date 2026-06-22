package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
)

func TestStandardizeTaskListResponseRejectsTopLevelArray(t *testing.T) {
	handler := &OrchestrationHandler{}

	_, err := handler.standardizeTaskListResponse([]interface{}{
		map[string]interface{}{
			"id":   float64(1),
			"name": "Meta scan",
		},
	})
	if err == nil {
		t.Fatal("standardizeTaskListResponse error is nil, want top-level array rejected")
	}
}

func TestStandardizeTaskListResponseAcceptsStandardItemsShape(t *testing.T) {
	handler := &OrchestrationHandler{}

	result, err := handler.standardizeTaskListResponse(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"id":        float64(1),
				"name":      "Meta scan",
				"task_type": "scan",
			},
		},
		"total":     float64(1),
		"page":      float64(1),
		"page_size": float64(20),
	})
	if err != nil {
		t.Fatalf("standardizeTaskListResponse returned error: %v", err)
	}

	if result["total"] != 1 {
		t.Fatalf("total = %#v, want 1", result["total"])
	}
	if result["page"] != 1 {
		t.Fatalf("page = %#v, want 1", result["page"])
	}
	if result["page_size"] != 20 {
		t.Fatalf("page_size = %#v, want 20", result["page_size"])
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

func TestStandardizeTaskListResponseAcceptsEmptyStandardItemsShape(t *testing.T) {
	handler := &OrchestrationHandler{}

	result, err := handler.standardizeTaskListResponse(map[string]interface{}{
		"items":     []interface{}{},
		"total":     float64(0),
		"page":      float64(1),
		"page_size": float64(100),
	})
	if err != nil {
		t.Fatalf("standardizeTaskListResponse returned error: %v", err)
	}
	items, ok := result["items"].([]interface{})
	if !ok || len(items) != 0 {
		t.Fatalf("items = %#v, want empty array", result["items"])
	}
	if result["total"] != 0 || result["page"] != 1 || result["page_size"] != 100 {
		t.Fatalf("result = %#v, want empty standard pagination", result)
	}
}

func TestStandardizeTaskListResponseRejectsStatusDataWrapper(t *testing.T) {
	handler := &OrchestrationHandler{}

	_, err := handler.standardizeTaskListResponse(map[string]interface{}{
		"status": "success",
		"data": []interface{}{
			map[string]interface{}{
				"id":        float64(1),
				"name":      "Legacy scan",
				"task_type": "scan",
			},
		},
	})

	if err == nil {
		t.Fatal("standardizeTaskListResponse error is nil, want status/data wrapper rejected")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Fatalf("error = %q, want status field rejection", err.Error())
	}
}

func TestStandardizeTaskListResponseRejectsLegacyTasksShape(t *testing.T) {
	handler := &OrchestrationHandler{}

	_, err := handler.standardizeTaskListResponse(map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":        float64(1),
				"name":      "Legacy query",
				"task_type": "query",
			},
		},
		"total":     float64(1),
		"page":      float64(1),
		"page_size": float64(20),
	})

	if err == nil {
		t.Fatal("standardizeTaskListResponse error is nil, want legacy tasks shape rejected")
	}
}

func TestStandardizeTaskListResponseRejectsMissingPaginationFields(t *testing.T) {
	handler := &OrchestrationHandler{}

	_, err := handler.standardizeTaskListResponse(map[string]interface{}{
		"items": []interface{}{},
		"total": float64(0),
		"page":  float64(1),
	})
	if err == nil {
		t.Fatal("standardizeTaskListResponse error is nil, want missing page_size rejected")
	}
	if !strings.Contains(err.Error(), "page_size") {
		t.Fatalf("error = %q, want page_size", err.Error())
	}
}

func TestStandardizeTaskListResponseRejectsNonArrayItems(t *testing.T) {
	handler := &OrchestrationHandler{}

	_, err := handler.standardizeTaskListResponse(map[string]interface{}{
		"items":     map[string]interface{}{},
		"total":     float64(0),
		"page":      float64(1),
		"page_size": float64(20),
	})
	if err == nil {
		t.Fatal("standardizeTaskListResponse error is nil, want non-array items rejected")
	}
	if !strings.Contains(err.Error(), "items must be an array") {
		t.Fatalf("error = %q, want items array", err.Error())
	}
}

func TestStandardizeTaskListResponseRejectsExtraFields(t *testing.T) {
	handler := &OrchestrationHandler{}

	_, err := handler.standardizeTaskListResponse(map[string]interface{}{
		"items":       []interface{}{},
		"total":       float64(0),
		"page":        float64(1),
		"page_size":   float64(20),
		"total_pages": float64(0),
	})
	if err == nil {
		t.Fatal("standardizeTaskListResponse error is nil, want extra field rejected")
	}
	if !strings.Contains(err.Error(), "total_pages") {
		t.Fatalf("error = %q, want total_pages", err.Error())
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

func TestListModuleTasksRejectsUndeclaredTaskTypeBeforeOwnerCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := false
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer owner.Close()
	system := newTaskProviderSystemServer(t, "meta", owner.URL)
	defer system.Close()

	handler := &OrchestrationHandler{
		taskProviderRegistry: service.NewTaskProviderRegistry(system.URL, "", time.Hour),
		httpClient:           owner.Client(),
	}
	router := gin.New()
	router.GET("/tasks", handler.ListModuleTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?module_name=meta&task_type=query", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if called {
		t.Fatal("owner task list endpoint was called for undeclared task_type")
	}
	if !strings.Contains(rec.Body.String(), "not declared") {
		t.Fatalf("body = %s, want not declared error", rec.Body.String())
	}
}

func TestListModuleTasksReturnsBadGatewayForOwnerError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/tasks" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unsupported task_type"}`))
	}))
	defer owner.Close()
	system := newTaskProviderSystemServer(t, "meta", owner.URL)
	defer system.Close()

	handler := &OrchestrationHandler{
		taskProviderRegistry: service.NewTaskProviderRegistry(system.URL, "", time.Hour),
		httpClient:           owner.Client(),
	}
	router := gin.New()
	router.GET("/tasks", handler.ListModuleTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?module_name=meta&task_type=scan", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status_code":400`) {
		t.Fatalf("body = %s, want upstream status_code 400", rec.Body.String())
	}
}

func TestListModuleTasksReturnsBadGatewayForNonStandardOwnerResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	}))
	defer owner.Close()
	system := newTaskProviderSystemServer(t, "meta", owner.URL)
	defer system.Close()

	handler := &OrchestrationHandler{
		taskProviderRegistry: service.NewTaskProviderRegistry(system.URL, "", time.Hour),
		httpClient:           owner.Client(),
	}
	router := gin.New()
	router.GET("/tasks", handler.ListModuleTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?module_name=meta&task_type=scan", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "data") {
		t.Fatalf("body = %s, want non-standard data field error", rec.Body.String())
	}
}

func TestExecuteProviderOrchestrationTaskRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &OrchestrationHandler{}
	router := gin.New()
	router.POST("/tasks/:task_type/:id/execute", handler.ExecuteProviderOrchestrationTask)

	req := httptest.NewRequest(http.MethodPost, "/tasks/orchestration/1/execute", strings.NewReader(`{"legacy":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown field") {
		t.Fatalf("body = %s, want unknown field error", rec.Body.String())
	}
}

func newTaskProviderSystemServer(t *testing.T, moduleName, ownerBaseURL string) *httptest.Server {
	t.Helper()

	provider := taskProviderForAPITest(moduleName, ownerBaseURL)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := fmt.Sprintf("/api/v1/internal/task-providers/%s", moduleName)
		if r.URL.Path != expectedPath {
			t.Fatalf("unexpected system path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"id":1,
			"module_name":%q,
			"display_name":"Meta",
			"description":"Meta provider",
			"base_url":%q,
			"task_list_endpoint":%q,
			"task_detail_endpoint":"/api/v1/meta/tasks/{task_type}/{id}",
			"task_execute_endpoint":"/api/v1/meta/tasks/{task_type}/{id}/execute",
			"task_status_endpoint":"/api/v1/meta/executions/{execution_id}",
			"capabilities":%s,
			"is_enabled":true
		}`, provider.ModuleName, provider.BaseURL, provider.TaskListEndpoint, string(*provider.Capabilities))
	}))
}

func taskProviderForAPITest(moduleName, baseURL string) *commonModels.TaskProvider {
	capabilities := commonModels.JSONString(`{
		"schema_version":"task.capabilities/v1",
		"task_capabilities":[{
			"type":"scan",
			"display_name":"scan",
			"description":"scan task",
			"definition_schema":{"type":"object"},
			"execution_schema":{"type":"object","additionalProperties":false},
			"supports_schedule":false,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/meta/scan",
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":false
		}]
	}`)
	return &commonModels.TaskProvider{
		ModuleName:       moduleName,
		BaseURL:          baseURL,
		TaskListEndpoint: "/api/v1/meta/tasks",
		Capabilities:     &capabilities,
		IsEnabled:        true,
	}
}
