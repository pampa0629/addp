package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpdateNotebookRuntimeBindingValidatesAndPersistsSelection(t *testing.T) {
	handler, taskService := newNotebookBindingHandlerForTest(t)
	response := updateNotebookBindingRequestForTest(t, handler, `{"engine_id":10,"kernel":"python3"}`)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var updated models.DevTask
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if updated.ID != 14 || updated.GetEngineID() == nil || *updated.GetEngineID() != 10 {
		t.Fatalf("updated task = %#v", updated)
	}
	if updated.Content["kernel"] != "python3" || updated.Content["minio_path"] != "tenant_7/notebooks/analysis.ipynb" {
		t.Fatalf("updated content = %#v", updated.Content)
	}

	stored, err := taskService.GetDevTask(14, 7)
	if err != nil || stored.GetEngineID() == nil || *stored.GetEngineID() != 10 || stored.Content["kernel"] != "python3" {
		t.Fatalf("stored task = %#v, error = %v", stored, err)
	}
}

func TestUpdateNotebookRuntimeBindingRejectsUnavailableKernelWithoutMutation(t *testing.T) {
	handler, taskService := newNotebookBindingHandlerForTest(t)
	response := updateNotebookBindingRequestForTest(t, handler, `{"engine_id":10,"kernel":"missing"}`)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	stored, err := taskService.GetDevTask(14, 7)
	if err != nil {
		t.Fatalf("GetDevTask() error = %v", err)
	}
	if got := stored.GetEngineID(); got == nil || *got != 8 {
		t.Fatalf("stored engine_id = %v, want 8", got)
	}
	if stored.Content["kernel"] != "old-kernel" {
		t.Fatalf("stored kernel = %#v, want old-kernel", stored.Content["kernel"])
	}
}

func newNotebookBindingHandlerForTest(t *testing.T) (*NotebookHandler, *service.DevTaskService) {
	t.Helper()
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/kernels" {
			t.Fatalf("runtime path = %q", request.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "success",
			"kernels": []map[string]string{{"name": "python3", "display_name": "Python 3", "language": "python"}},
		})
	}))
	t.Cleanup(runtimeServer.Close)
	runtimeURL, err := url.Parse(runtimeServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	runtimePort, err := strconv.Atoi(runtimeURL.Port())
	if err != nil {
		t.Fatal(err)
	}
	capabilitiesJSON, err := dbbridge.GenerateCapabilities("jupyter")
	if err != nil {
		t.Fatal(err)
	}
	capabilities := commonModels.JSONString(capabilitiesJSON)
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/system/runtime/engine-descriptors/10" {
			t.Fatalf("System path = %q", request.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(commonModels.EngineRuntimeDescriptor{
			ID: 10, Name: "Jupyter Engine", EngineType: "jupyter",
			LifecycleState: commonModels.EngineLifecycleActive, Capabilities: &capabilities,
			RuntimeEndpoint: &commonModels.EngineRuntimeEndpoint{
				Protocol: runtimeURL.Scheme, Host: runtimeURL.Hostname(), Port: runtimePort,
			},
		})
	}))
	t.Cleanup(systemServer.Close)

	db := newNotebookHandlerTestDB(t)
	if err := db.Exec(`
		INSERT INTO develop.dev_tasks (
			id, tenant_id, name, dev_type, content, execution_config, editor_layout,
			timeout, tags, created_by, status
		) VALUES (
			14, 7, 'analysis', 'script',
			CAST('{"notebook_path":"analysis.ipynb","minio_path":"tenant_7/notebooks/analysis.ipynb","kernel":"old-kernel"}' AS BLOB),
			CAST('{"engine_id":8}' AS BLOB), CAST('{}' AS BLOB), 600, '{}', 1, 'active'
		)
	`).Error; err != nil {
		t.Fatalf("seed notebook: %v", err)
	}

	tokens := staticDevelopServiceTokens("addp_at_service")
	jupyterService := service.NewJupyterService(
		commonClient.NewSystemServiceClient(systemServer.URL, tokens, systemServer.Client()),
		tokens,
	)
	taskService := service.NewDevTaskService(repository.NewDevTaskRepository(db), nil)
	return NewNotebookHandler(jupyterService, nil, taskService), taskService
}

func updateNotebookBindingRequestForTest(t *testing.T, handler *NotebookHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PUT("/notebooks/:id/runtime-binding", func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 3)
		handler.UpdateRuntimeBinding(c)
	})
	request := httptest.NewRequest(http.MethodPut, "/notebooks/14/runtime-binding", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func newNotebookHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatalf("attach develop schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE develop.dev_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL, display_name TEXT, dev_type TEXT NOT NULL,
			content JSON NOT NULL, execution_config JSON, editor_layout JSON NOT NULL,
			timeout INTEGER, description TEXT, tags TEXT, created_by INTEGER,
			updated_by INTEGER, created_at DATETIME, updated_at DATETIME,
			deleted_at DATETIME, status TEXT, last_execution_id TEXT,
			last_execution_status TEXT, last_run_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create develop.dev_tasks: %v", err)
	}
	return db
}
