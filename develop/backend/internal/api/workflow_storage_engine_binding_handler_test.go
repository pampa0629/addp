package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

func TestWorkflowStorageEngineBindingHandlers(t *testing.T) {
	db := newNotebookHandlerTestDB(t)
	if err := db.Exec(`
		INSERT INTO develop.dev_tasks (
			id, tenant_id, name, dev_type, content, execution_config, editor_layout,
			timeout, tags, created_by, updated_at, status
		) VALUES (
			34, 7, 'workflow', 'workflow',
			CAST('{"workflow_definition":{"tasks":[{"params":{"locator":"addp://engine/5/path/public/source?type=table&item_id=8"}}]}}' AS BLOB),
			CAST('{"engine_id":1}' AS BLOB), CAST('{}' AS BLOB), 300, '{}', 1, ?, 'active'
		)
	`, time.Now().UTC().Truncate(time.Millisecond)).Error; err != nil {
		t.Fatalf("seed workflow: %v", err)
	}

	tabularCapabilities := commonModels.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"doris",
		"engine_family":"tabular",
		"storage":{"catalog_model":{"path_version":"catalog.path/v1","root_term":"server","levels":[{"term":"database","kinds":["namespace"],"role":"branch"},{"term":"table","kinds":["table","view"],"role":"leaf"}]},"catalog":{"supported":true}}
	}`)
	objectCapabilities := commonModels.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"minio",
		"engine_family":"object",
		"storage":{"catalog_model":{"path_version":"catalog.path/v1","root_term":"service","levels":[{"term":"bucket","kinds":["bucket"],"role":"branch"},{"term":"prefix","kinds":["prefix"],"role":"branch"},{"term":"object","kinds":["object"],"role":"leaf"}]},"catalog":{"supported":true}}
	}`)
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []commonModels.EngineRuntimeDescriptor{
				{ID: 15, Name: "Replacement Doris", EngineType: "doris", LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionOnline, Capabilities: &tabularCapabilities},
				{ID: 16, Name: "Object Storage", EngineType: "minio", LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionOnline, Capabilities: &objectCapabilities},
			},
			"total": 2, "page": 1, "page_size": 100,
		})
	}))
	defer systemServer.Close()

	tokens := staticDevelopServiceTokens("tenant-token")
	taskService := service.NewDevTaskService(
		repository.NewDevTaskRepository(db),
		commonClient.NewSystemServiceClient(systemServer.URL, tokens, systemServer.Client()),
	)
	handler := NewDevTaskHandler(taskService, nil)
	router := workflowStorageBindingTestRouter(handler)

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/task-definitions/34/storage-engine-bindings", nil)
	router.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
	var bindings models.WorkflowStorageEngineBindingsResponse
	if err := json.Unmarshal(listResponse.Body.Bytes(), &bindings); err != nil {
		t.Fatal(err)
	}
	if len(bindings.Items) != 1 || bindings.Items[0].EngineID != 5 || bindings.Items[0].Available {
		t.Fatalf("bindings = %#v", bindings.Items)
	}

	incompatibleResponse := httptest.NewRecorder()
	incompatibleRequest := httptest.NewRequest(
		http.MethodPut,
		"/task-definitions/34/storage-engine-bindings/5",
		bytes.NewBufferString(`{"target_engine_id":16}`),
	)
	incompatibleRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(incompatibleResponse, incompatibleRequest)
	if incompatibleResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("incompatible status = %d, body = %s", incompatibleResponse.Code, incompatibleResponse.Body.String())
	}

	updateResponse := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(
		http.MethodPut,
		"/task-definitions/34/storage-engine-bindings/5",
		bytes.NewBufferString(`{"target_engine_id":15}`),
	)
	updateRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateResponse.Code, updateResponse.Body.String())
	}
	var result models.RebindWorkflowStorageEngineResponse
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ReplacedLocatorCount != 1 || result.SourceEngineID != 5 || result.TargetEngineID != 15 {
		t.Fatalf("result = %#v", result)
	}
	workflow := result.Task.Content["workflow_definition"].(map[string]interface{})
	tasks := workflow["tasks"].([]interface{})
	params := tasks[0].(map[string]interface{})["params"].(map[string]interface{})
	if params["locator"] != "addp://engine/15/path/public/source?type=table" {
		t.Fatalf("locator = %#v", params["locator"])
	}
}

func workflowStorageBindingTestRouter(handler *DevTaskHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/task-definitions")
	group.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 3)
		c.Next()
	})
	group.GET("/:id/storage-engine-bindings", handler.ListWorkflowStorageEngineBindings)
	group.PUT("/:id/storage-engine-bindings/:source_engine_id", handler.RebindWorkflowStorageEngine)
	return router
}
