package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
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
	execution := &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "develop-output-execution", Module: commonExecution.ModuleDevelop,
		TaskType: commonExecution.TaskTypeWorkflow, Source: commonExecution.ModuleOrchestrator,
		Status: commonExecution.ExecutionStatusSuccess, TriggerType: commonExecution.TriggerTypeManual,
		Metadata: map[string]interface{}{
			"outputs": map[string]interface{}{"target_locator": "addp://engine/2/path/public/result?type=table"},
			"result":  map[string]interface{}{"outputs": map[string]interface{}{"legacy": "ignored"}},
		},
	}
	if err := repo.Create(t.Context(), execution); err != nil {
		t.Fatal(err)
	}
	executor := service.NewDevExecutor(nil, repo, nil, nil, nil, nil, nil, nil, 1000)
	handler := NewExecutionHandler(executor, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		c.Next()
	})
	router.GET("/task-provider/executions/:execution_id", handler.ProviderGetExecution)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/task-provider/executions/develop-output-execution", nil))
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
