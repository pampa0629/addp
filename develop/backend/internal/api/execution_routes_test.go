package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
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

func TestExecuteContentRejectsPublicParameterTypesBeforeAdmission(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	engineType := "contract_test_workflow"
	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewWorkflowCapabilities(engineType, plugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatal(err)
	}
	capabilitiesJSON := commonModels.JSONString(capabilities)
	var rejectDiscovery atomic.Bool
	var unexpectedRequests atomic.Int64
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/operators" {
			unexpectedRequests.Add(1)
			http.NotFound(w, r)
			return
		}
		if rejectDiscovery.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"operators": []plugin.OperatorDescriptor{{
			ID: "weighted_focal", Name: "weighted_focal", DisplayName: "Weighted focal", EngineType: engineType,
			Category: "raster", CategoryPath: []string{"Raster"}, Description: "Weighted focal declaration",
			ExecutionModes: []string{"workflow"}, Effects: []string{"read"},
			OutputPorts: []plugin.OutputPortDescriptor{{Name: "default", Type: "raster", IsDefault: true}},
			Parameters: []plugin.ParameterDescriptor{
				{Name: "weights", Type: "array", ItemType: "array"},
				{Name: "window_size", Type: "array", ItemType: "integer"},
				{Name: "radius_metres", Type: "number"},
				{Name: "band", Type: "integer"},
			},
		}}})
	}))
	defer runtime.Close()
	host, portText, err := net.SplitHostPort(runtime.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/system/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "addp_at_test", "token_type": "Bearer", "expires_in": 300, "scope": "addp.api",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/runtime/engine-descriptors/12":
			_ = json.NewEncoder(w).Encode(commonModels.EngineRuntimeDescriptor{
				ID: 12, Name: "Fixture runtime", EngineType: engineType,
				ConnectionStatus: commonModels.EngineConnectionOnline, LifecycleState: "active",
				Capabilities:    &capabilitiesJSON,
				RuntimeEndpoint: &commonModels.EngineRuntimeEndpoint{Protocol: "http", Host: host, Port: port},
			})
		default:
			unexpectedRequests.Add(1)
			http.NotFound(w, r)
		}
	}))
	defer system.Close()
	source, err := commonClient.NewOAuthServiceTokenSource(system.URL, "addp-develop", strings.Repeat("a", 48), system.Client())
	if err != nil {
		t.Fatal(err)
	}
	discovery := service.NewOperatorDiscoveryService(commonClient.NewSystemServiceClient(system.URL, source, system.Client()))
	executor := service.NewDevExecutor(nil, commonExecution.NewTaskExecutionRepository(db), nil, discovery, nil, nil, nil, nil)
	handler := NewExecutionHandler(executor, nil, nil)
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 7, 3); c.Next() })
	const route = "/api/v1/develop/executions"
	router.POST(route, handler.ExecuteContent)

	submit := func(lang, field, value string) *httptest.ResponseRecorder {
		body := `{"dev_type":"workflow","trigger_type":"manual","execution_config":{"engine_id":12},"content":{"workflow_definition":{"tasks":[{"id":"focal","operator":"weighted_focal","params":{` +
			strconv.Quote(field) + ":" + value + `},"depends_on":[]}]}}}`
		req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", lang)
		req.Header.Set("Authorization", "Bearer addp_at_user")
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	for _, locale := range []struct{ lang, summary, messagePrefix string }{
		{"zh-cn", "工作流参数无效，请检查节点参数", "参数类型与算子声明不符："},
		{"en", "Invalid workflow parameters. Review the node parameters", "Parameter type differs from the operator declaration: "},
	} {
		for _, input := range []struct{ name, field, value string }{
			{"weight_null", "weights", "null"}, {"weight_scalar", "weights", "5"}, {"weight_flat", "weights", "[1,2]"},
			{"circle_window_null", "window_size", "null"}, {"radius_null", "radius_metres", "null"},
			{"radius_bool", "radius_metres", "false"}, {"band_fraction", "band", "1.5"},
		} {
			t.Run(locale.lang+"/"+input.name, func(t *testing.T) {
				out := submit(locale.lang, input.field, input.value)
				if out.Code != http.StatusBadRequest {
					t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
				}
				path := "workflow_definition.tasks[0].params." + input.field
				var envelope WorkflowParametersErrorResponse
				if err := json.Unmarshal(out.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				want := service.WorkflowValidationIssue{Code: "parameter_type_invalid", Path: path, Message: locale.messagePrefix + path}
				if envelope.Error != locale.summary || envelope.ErrorCode != "invalid_workflow_parameters" ||
					len(envelope.Errors) != 1 || envelope.Errors[0] != want || strings.Contains(out.Body.String(), "execution_id") {
					t.Fatalf("unexpected error: %s", out.Body.String())
				}
			})
		}
	}
	rejectDiscovery.Store(true)
	out := submit("en", "weights", "null")
	if out.Code != http.StatusInternalServerError || strings.Contains(out.Body.String(), "invalid_workflow_parameters") {
		t.Fatalf("Runtime discovery failure classified as user input: %d %s", out.Code, out.Body.String())
	}
	var count int64
	if err := db.Table("common.task_executions").Count(&count).Error; err != nil || count != 0 || unexpectedRequests.Load() != 0 {
		t.Fatalf("rejected requests acquired effects: execution count=%d unexpected HTTP=%d error=%v", count, unexpectedRequests.Load(), err)
	}
}
