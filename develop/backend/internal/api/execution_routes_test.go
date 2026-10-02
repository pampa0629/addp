package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExecutionRoutesUseExecutionIDWildcard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	executions := router.Group("/api/v1/develop/executions")

	executions.GET("/:execution_id", func(c *gin.Context) {})
	executions.GET("/:execution_id/logs", func(c *gin.Context) {})
	executions.POST("/:execution_id/retry", func(c *gin.Context) {})
}

func TestDevelopExportResourceRequestMatcher(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{
		"/api/v1/develop/exports/7/file",
		"/api/v1/develop/exports/123/file",
	} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodGet, path, nil)
		if !isDevelopExportResourceRequest(context) {
			t.Fatalf("export file path was not classified as a Resource Ticket route: %s", path)
		}
	}
	for _, path := range []string{
		"/api/v1/develop/exports/7",
		"/api/v1/develop/executions/7/exports",
		"/api/v1/develop/exports/7/file/extra",
	} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodGet, path, nil)
		if isDevelopExportResourceRequest(context) {
			t.Fatalf("ordinary API path was classified as a Resource Ticket route: %s", path)
		}
	}
}

func TestProviderDevTaskListResponseUsesStandardItemsShape(t *testing.T) {
	now := time.Now()
	body, err := json.Marshal(models.ListProviderDevTasksResponse{
		Items: []models.ProviderDevTask{{
			ID:        1,
			TenantID:  7,
			Name:      "query task",
			TaskType:  commonExecution.TaskTypeQuery,
			CreatedAt: now,
			UpdatedAt: now,
			Status:    "active",
		}},
		Total:    1,
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("marshal ListProviderDevTasksResponse: %v", err)
	}

	assertStandardTaskProviderListShape(t, body)
}

func TestProviderExecuteDevResponseUsesStandardExecutionShape(t *testing.T) {
	body, err := json.Marshal(providerExecuteDevResponse{
		ExecutionID: "develop-exec-1",
		Status:      commonExecution.ExecutionStatusRunning,
	})
	if err != nil {
		t.Fatalf("marshal providerExecuteDevResponse: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
	if resp["execution_id"] != "develop-exec-1" || resp["status"] != commonExecution.ExecutionStatusRunning {
		t.Fatalf("response = %#v, want execution_id and status", resp)
	}
	for _, legacyField := range []string{"message", "data", "id"} {
		if _, ok := resp[legacyField]; ok {
			t.Fatalf("response contains non-standard field %q: %s", legacyField, body)
		}
	}
}

func TestProviderExecutionUsesOnlyStableMetadataOutputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	repo := commonExecution.NewTaskExecutionRepository(db)
	parent := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: "orchestrator-parent", Module: "orchestrator", TaskType: "orchestration", Source: "orchestrator", Status: "running", TriggerType: "manual"}
	if err := repo.Create(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	foreignParent := *parent
	foreignParent.ID = 0
	foreignParent.ExecutionID = "foreign-orchestrator-parent"
	foreignParent.TenantID = 8
	wrongModuleParent := *parent
	wrongModuleParent.ID = 0
	wrongModuleParent.ExecutionID = "develop-parent"
	wrongModuleParent.Module = commonExecution.ModuleDevelop
	for _, record := range []*commonExecution.TaskExecution{&foreignParent, &wrongModuleParent} {
		if err := repo.Create(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}
	execution := &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "develop-output-execution", Module: commonExecution.ModuleDevelop,
		TaskType: commonExecution.TaskTypeWorkflow, Source: commonExecution.ModuleOrchestrator, ParentExecutionID: &parent.ExecutionID,
		Status: commonExecution.ExecutionStatusSuccess, TriggerType: commonExecution.TriggerTypeManual,
		Metadata: map[string]interface{}{
			"outputs": map[string]interface{}{"target_locator": "addp://engine/2/path/public/result?type=table"},
			"result":  map[string]interface{}{"outputs": map[string]interface{}{"legacy": "ignored"}},
		},
	}
	if err := repo.Create(t.Context(), execution); err != nil {
		t.Fatal(err)
	}
	executor := service.NewDevExecutor(nil, repo, nil, nil, nil, nil, nil, nil)
	handler := NewExecutionHandler(executor, nil, nil)
	router := gin.New()
	system := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer provider":      {ClientID: "addp-orchestrator", Permissions: []string{"develop.task_provider.read"}},
		"Bearer other-service": {ClientID: "addp-transfer", Permissions: []string{"develop.task_provider.read"}},
	})
	defer system.Close()
	router.Use(commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: system.URL}))
	router.GET("/task-provider/executions/:execution_id", handler.ProviderGetExecution)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/task-provider/executions/develop-output-execution", nil)
	request.Header.Set("Authorization", "Bearer provider")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	outputs, ok := body["outputs"].(map[string]interface{})
	if !ok || outputs["target_locator"] == nil || outputs["legacy"] != nil {
		t.Fatalf("outputs=%#v", body["outputs"])
	}

	if strings.Contains(response.Body.String(), "legacy") || strings.Contains(response.Body.String(), "execution_config") {
		t.Fatalf("provider exposed professional payload: %s", response.Body.String())
	}
	for _, test := range []struct {
		id, source string
		parent     *string
	}{
		{id: "manual-source", source: "develop", parent: &parent.ExecutionID},
		{id: "missing-parent", source: "orchestrator"},
		{id: "nonexistent-parent", source: "orchestrator", parent: func() *string { v := "nonexistent"; return &v }()},
		{id: "foreign-parent", source: "orchestrator", parent: &foreignParent.ExecutionID},
		{id: "wrong-parent-module", source: "orchestrator", parent: &wrongModuleParent.ExecutionID},
	} {
		child := *execution
		child.ID = 0
		child.ExecutionID = test.id
		child.Source = test.source
		child.ParentExecutionID = test.parent
		if err := repo.Create(t.Context(), &child); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("GET", "/task-provider/executions/"+test.id, nil)
		request.Header.Set("Authorization", "Bearer provider")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 404 {
			t.Fatalf("provider read %s: %d %s", test.id, response.Code, response.Body.String())
		}
	}
	request = httptest.NewRequest(http.MethodGet, "/task-provider/executions/develop-output-execution", nil)
	request.Header.Set("Authorization", "Bearer other-service")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "target_locator") {
		t.Fatalf("other service read provider outputs: %d %s", response.Code, response.Body.String())
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
