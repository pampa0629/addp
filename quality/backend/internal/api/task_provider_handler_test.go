package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/taskprovider"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
	router.POST("/tasks/:task_type/:id/execute", NewTaskProviderHandler(nil, nil, nil).TaskExecute)

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

func TestTaskProviderListRejectsUnsupportedTypeAndScopesTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	createTaskProviderHandlerTask(t, db, models.CheckTask{TenantID: 7, Name: "tenant-7", EngineID: 2, SchemaName: "public", Table: "orders", CreatedBy: 1})
	createTaskProviderHandlerTask(t, db, models.CheckTask{TenantID: 8, Name: "tenant-8", EngineID: 2, SchemaName: "public", Table: "orders", CreatedBy: 1})
	handler := NewTaskProviderHandler(service.NewCheckTaskService(repository.NewCheckTaskRepository(db), nil), nil, nil)

	invalidRouter := gin.New()
	invalidRouter.GET("/tasks", withIssueHandlerAuth(7, 11), handler.ListTasks)
	invalidResponse := httptest.NewRecorder()
	invalidRouter.ServeHTTP(invalidResponse, httptest.NewRequest(http.MethodGet, "/tasks?task_type=sync", nil))
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid task type status = %d, want %d, body=%s", invalidResponse.Code, http.StatusBadRequest, invalidResponse.Body.String())
	}

	listResponse := httptest.NewRecorder()
	listRouter := gin.New()
	listRouter.GET("/tasks", withIssueHandlerAuth(7, 11), handler.ListTasks)
	listRouter.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/tasks?task_type=check&page=0&page_size=999", nil))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body=%s", listResponse.Code, http.StatusOK, listResponse.Body.String())
	}
	var body taskProviderTaskListResponse
	if err := json.Unmarshal(listResponse.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if body.Total != 1 || len(body.Items) != 1 || body.Items[0].TenantID != 7 || body.Items[0].TaskType != commonExecution.TaskTypeQualityCheck {
		t.Fatalf("tenant-scoped task list = %#v", body)
	}
	if body.Page != 1 || body.PageSize != 100 {
		t.Fatalf("normalized task list pagination = %#v", body)
	}
}

func TestTaskProviderDetailRejectsInvalidRouteAndCrossTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	task := createTaskProviderHandlerTask(t, db, models.CheckTask{TenantID: 7, Name: "tenant-7", EngineID: 2, SchemaName: "public", Table: "orders", CreatedBy: 1})
	handler := NewTaskProviderHandler(service.NewCheckTaskService(repository.NewCheckTaskRepository(db), nil), nil, nil)

	invalidTypeRouter := gin.New()
	invalidTypeRouter.GET("/tasks/:task_type/:id", withIssueHandlerAuth(7, 11), handler.TaskDetail)
	invalidTypeResponse := httptest.NewRecorder()
	invalidTypeRouter.ServeHTTP(invalidTypeResponse, httptest.NewRequest(http.MethodGet, "/tasks/sync/1", nil))
	if invalidTypeResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid task type status = %d, want %d, body=%s", invalidTypeResponse.Code, http.StatusBadRequest, invalidTypeResponse.Body.String())
	}

	invalidIDResponse := httptest.NewRecorder()
	invalidTypeRouter.ServeHTTP(invalidIDResponse, httptest.NewRequest(http.MethodGet, "/tasks/check/not-an-id", nil))
	if invalidIDResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid task id status = %d, want %d, body=%s", invalidIDResponse.Code, http.StatusBadRequest, invalidIDResponse.Body.String())
	}

	crossTenantResponse := httptest.NewRecorder()
	crossTenantRouter := gin.New()
	crossTenantRouter.GET("/tasks/:task_type/:id", withIssueHandlerAuth(8, 22), handler.TaskDetail)
	crossTenantRouter.ServeHTTP(crossTenantResponse, httptest.NewRequest(http.MethodGet, "/tasks/check/"+strconv.FormatInt(task.ID, 10), nil))
	if crossTenantResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant status = %d, want %d, body=%s", crossTenantResponse.Code, http.StatusNotFound, crossTenantResponse.Body.String())
	}
}

func TestTaskProviderExecuteRejectsUnsupportedRequestBeforeExecutor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/tasks/:task_type/:id/execute", withIssueHandlerAuth(7, 11), NewTaskProviderHandler(nil, nil, nil).TaskExecute)

	cases := []struct {
		name string
		path string
		body string
	}{
		{name: "invalid type", path: "/tasks/sync/1/execute", body: `{}`},
		{name: "invalid id", path: "/tasks/check/not-an-id/execute", body: `{}`},
		{name: "parameters not supported", path: "/tasks/check/1/execute", body: `{"parameters":{"mode":"full"}}`},
		{name: "invalid trigger", path: "/tasks/check/1/execute", body: `{"trigger_type":"retry"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
}

func TestMaterializationGateExecutionContractDeclaresVersionHandoff(t *testing.T) {
	contract := materializationGateExecutionContract()
	raw := map[string]interface{}{
		"input_schema": contract.InputSchema, "input_defaults": contract.InputDefaults,
		"input_ui_schema": contract.InputUISchema, "output_schema": contract.OutputSchema,
	}
	if err := taskprovider.ValidateExecutionContract(raw); err != nil {
		t.Fatal(err)
	}
	properties := contract.OutputSchema["properties"].(map[string]interface{})
	if properties["materialization_group_id"] == nil || properties["materialization_group_version"] == nil {
		t.Fatalf("output schema = %#v", contract.OutputSchema)
	}
}

func newTaskProviderHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS quality").Error; err != nil {
		t.Fatalf("attach quality schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.check_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		engine_id INTEGER NOT NULL,
		schema_name TEXT NOT NULL,
		table_name TEXT NOT NULL,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		last_run_at DATETIME,
		last_execution_id TEXT,
		last_execution_status TEXT
	)`).Error; err != nil {
		t.Fatalf("create check tasks table: %v", err)
	}
	return db
}

func createTaskProviderHandlerTask(t *testing.T, db *gorm.DB, task models.CheckTask) models.CheckTask {
	t.Helper()
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create check task: %v", err)
	}
	return task
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
