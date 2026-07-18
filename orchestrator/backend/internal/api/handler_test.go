package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateLocalizesExecutionSchemaValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := taskProviderForAPITest("meta", "http://owner.invalid")
	capabilities := commonModels.JSONString(`{
		"schema_version":"task.capabilities/v1",
		"task_capabilities":[{
			"type":"scan",
			"display_name":"scan",
			"description":"scan task",
			"definition_schema":{"type":"object"},
			"execution_schema":{"type":"object","properties":{"limit":{"type":"integer"}},"additionalProperties":false},
			"supports_schedule":false,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/meta/scan",
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":false
		}]
	}`)
	provider.Capabilities = &capabilities
	system := newTaskProviderSystemServerWithProvider(t, provider)
	defer system.Close()

	handler := &OrchestrationHandler{
		taskProviderRegistry: service.NewTaskProviderRegistry(system.URL, "", time.Hour),
	}
	tests := []struct {
		name     string
		language string
		want     string
	}{
		{name: "Chinese", language: "zh-CN", want: "第 1 个步骤的执行参数 parameters.limit 必须是整数"},
		{name: "English", language: "en-US", want: "Execution parameter parameters.limit for step 1 must be an integer"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.Use(testTenantAuthorizationContext(7, 9))
			router.POST("/orchestrations", handler.Create)

			body := `{"name":"invalid parameters","steps":[{"id":"scan","name":"Scan","provider":"meta","task_type":"scan","task_id":1,"parameters":{"limit":"100"},"depends_on":[],"timeout":300}]}`
			req := httptest.NewRequest(http.MethodPost, "/orchestrations", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept-Language", testCase.language)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			var response map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response["error"] != testCase.want {
				t.Fatalf("error = %q, want %q", response["error"], testCase.want)
			}
		})
	}
}

func TestCreateLocalizesJSONAndStepDefinitionErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &OrchestrationHandler{}
	tests := []struct {
		name     string
		language string
		body     string
		want     string
	}{
		{
			name: "malformed JSON", language: "zh-CN", body: `{"name":`,
			want: "请求体不是有效的编排定义",
		},
		{
			name: "unknown top-level field", language: "en-US", body: `{"name":"x","steps":[],"legacy":true}`,
			want: "Request body is not a valid orchestration definition",
		},
		{
			name: "client tenant field", language: "zh-CN", body: `{"name":"x","steps":[],"tenant_id":999}`,
			want: "请求体不是有效的编排定义",
		},
		{
			name: "unsupported step field", language: "zh-CN",
			body: `{"name":"x","steps":[{"id":"scan","name":"Scan","provider":"meta","task_type":"scan","task_id":1,"parameters":{},"depends_on":[],"timeout":300,"legacy":true}]}`,
			want: "编排步骤不支持字段 legacy",
		},
		{
			name: "steps required", language: "en-US", body: `{"name":"x","steps":[]}`,
			want: "An orchestration must contain at least one step",
		},
		{
			name: "provider required", language: "zh-CN",
			body: `{"name":"x","steps":[{"id":"scan","name":"Scan","task_type":"scan","task_id":1,"parameters":{},"depends_on":[],"timeout":300}]}`,
			want: "第 1 个步骤缺少任务提供者",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			status, message := executeCreateValidationRequest(t, handler, testCase.language, testCase.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", status, http.StatusBadRequest)
			}
			if message != testCase.want {
				t.Fatalf("error = %q, want %q", message, testCase.want)
			}
		})
	}
}

func TestLocalizeReferenceAndScheduleValidationErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		language string
		err      error
		want     string
	}{
		{
			name: "reference cycle", language: "zh-CN",
			err:  &service.OrchestrationReferenceValidationError{Code: service.OrchestrationReferenceCycle, Path: []uint{7, 8, 7}},
			want: "编排引用形成循环：7 -> 8 -> 7",
		},
		{
			name: "reference not found", language: "en-US",
			err:  &service.OrchestrationReferenceValidationError{Code: service.OrchestrationReferenceNotFound, OrchestrationID: 99},
			want: "Referenced orchestration 99 does not exist",
		},
		{
			name: "invalid schedule", language: "zh-CN",
			err:  &service.ScheduleValidationError{Code: service.ScheduleExpressionInvalid, Expression: "not-a-cron"},
			want: "调度表达式 not-a-cron 无效",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.GET("/", func(c *gin.Context) {
				c.String(http.StatusOK, localizeOrchestrationValidationError(c, testCase.err))
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept-Language", testCase.language)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Body.String() != testCase.want {
				t.Fatalf("message = %q, want %q", rec.Body.String(), testCase.want)
			}
		})
	}
}

func executeCreateValidationRequest(t *testing.T, handler *OrchestrationHandler, language, body string) (int, string) {
	t.Helper()
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.Use(testTenantAuthorizationContext(7, 9))
	router.POST("/orchestrations", handler.Create)
	req := httptest.NewRequest(http.MethodPost, "/orchestrations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", language)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, rec.Body.String())
	}
	return rec.Code, response["error"]
}

func TestRequireTenantIDRejectsTenantlessIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set(commonAuth.ContextAuthorizationContextKey, commonAuth.AuthorizationContext{SubjectType: "user", UserID: 1})
		c.Next()
	})
	router.GET("/", func(c *gin.Context) {
		if _, ok := requireTenantID(c); ok {
			c.Status(http.StatusNoContent)
		}
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "en-US")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "not bound to a tenant") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOrchestrationHandlersHideCrossTenantResources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS orchestrator").Error; err != nil {
		t.Fatalf("attach orchestrator schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE orchestrator.orchestrations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		steps JSON NOT NULL,
		enabled BOOLEAN,
		schedule TEXT,
		last_run_at DATETIME,
		next_run_at DATETIME,
		last_execution_id TEXT,
		last_execution_status TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create orchestration table: %v", err)
	}
	repo := repository.NewOrchestrationRepository(db)
	orch := models.Orchestration{
		TenantID: 7,
		Name:     "tenant-seven",
		Steps:    models.Steps{{ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1}},
	}
	if err := repo.Create(&orch); err != nil {
		t.Fatalf("create orchestration: %v", err)
	}
	handler := &OrchestrationHandler{orchRepo: repo}

	crossGet := executeTenantHandlerRequest(t, handler.Get, 8, http.MethodGet, fmt.Sprintf("/orchestrations/%d", orch.ID))
	if crossGet.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant get status=%d body=%s", crossGet.Code, crossGet.Body.String())
	}
	crossDelete := executeTenantHandlerRequest(t, handler.Delete, 8, http.MethodDelete, fmt.Sprintf("/orchestrations/%d", orch.ID))
	if crossDelete.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant delete status=%d body=%s", crossDelete.Code, crossDelete.Body.String())
	}
	if _, err := repo.GetByIDAndTenant(orch.ID, 7); err != nil {
		t.Fatalf("orchestration was changed by cross-tenant request: %v", err)
	}
	ownerGet := executeTenantHandlerRequest(t, handler.Get, 7, http.MethodGet, fmt.Sprintf("/orchestrations/%d", orch.ID))
	if ownerGet.Code != http.StatusOK {
		t.Fatalf("owner get status=%d body=%s", ownerGet.Code, ownerGet.Body.String())
	}
}

func executeTenantHandlerRequest(t *testing.T, handler gin.HandlerFunc, tenantID uint, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.Use(testTenantAuthorizationContext(tenantID, 9))
	router.Handle(method, "/orchestrations/:id", handler)
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func testTenantAuthorizationContext(tenantID, userID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(commonAuth.ContextAuthorizationContextKey, commonAuth.AuthorizationContext{
			SubjectType: "user",
			UserID:      userID,
			TenantID:    &tenantID,
		})
		c.Set(commonAuth.ContextTenantIDKey, tenantID)
		c.Set(commonAuth.ContextUserIDKey, userID)
		c.Next()
	}
}

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
	router.Use(testTenantAuthorizationContext(7, 9))
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
		if r.URL.Query().Has("tenant_id") {
			t.Fatalf("client tenant_id was forwarded: %s", r.URL.RawQuery)
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
	router.Use(testTenantAuthorizationContext(7, 9))
	router.GET("/tasks", handler.ListModuleTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?module_name=meta&task_type=scan&tenant_id=999", nil)
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
	router.Use(testTenantAuthorizationContext(7, 9))
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
	return newTaskProviderSystemServerWithProvider(t, provider)
}

func newTaskProviderSystemServerWithProvider(t *testing.T, provider *commonModels.TaskProvider) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := fmt.Sprintf("/api/v1/internal/task-providers/%s", provider.ModuleName)
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
