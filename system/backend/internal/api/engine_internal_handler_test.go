package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRegisterRuntimeEngineRejectsPhysicalEndpointChange(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate engine: %v", err)
	}

	repo := repository.NewEngineRepository(db)
	engine := &models.Engine{
		Name:           "GeoPython Runtime",
		EngineType:     "geopython_workflow",
		EngineOrigin:   "extension",
		IsBuiltin:      true,
		LifecycleState: models.EngineLifecycleActive,
		ConnectionInfo: models.ConnectionInfo{
			"protocol": "http",
			"host":     "runtime.internal",
			"port":     8080,
		},
	}
	if err := repo.Create(engine); err != nil {
		t.Fatalf("create engine: %v", err)
	}

	router := gin.New()
	handler := NewEngineHandler(service.NewEngineService(repo, nil, nil))
	router.POST("/api/v1/system/runtime/engines", handler.RegisterRuntimeEngine)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/system/runtime/engines",
		bytes.NewBufferString(`{"engine_type":"geopython_workflow","name":"GeoPython Runtime","connection_info":{"protocol":"http","port":8080},"capabilities":{"schema_version":"engine.capabilities/v1","engine_type":"geopython_workflow","engine_family":"workflow","compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}}},"is_builtin":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("register status = %d body=%s, want 409", response.Code, response.Body.String())
	}
	stored, err := repo.GetByID(engine.ID)
	if err != nil {
		t.Fatalf("get stored engine: %v", err)
	}
	if stored.ConnectionInfo["host"] != "runtime.internal" {
		t.Fatalf("stored host = %#v, want unchanged runtime.internal", stored.ConnectionInfo["host"])
	}
}

func TestRegisterRuntimeEngineKeepsSubmittedCapabilitiesForBuiltinCustomRuntime(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate engine: %v", err)
	}

	repo := repository.NewEngineRepository(db)
	router := gin.New()
	handler := NewEngineHandler(service.NewEngineService(repo, nil, nil))
	router.POST("/api/v1/system/runtime/engines", handler.RegisterRuntimeEngine)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/system/runtime/engines",
		bytes.NewBufferString(`{
			"engine_type":"tenant_workflow_runtime",
			"name":"Tenant Workflow Runtime",
			"connection_info":{"protocol":"http","port":18103},
			"capabilities":{
				"schema_version":"engine.capabilities/v1",
				"engine_type":"tenant_workflow_runtime",
				"engine_family":"workflow",
				"compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}}
			},
			"is_builtin":true
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("register status = %d body=%s, want 202", response.Code, response.Body.String())
	}

	stored, err := repo.GetByEngineTypeAndTenant("tenant_workflow_runtime", nil)
	if err != nil {
		t.Fatalf("get registered runtime: %v", err)
	}
	if stored.Capabilities == nil || !bytes.Contains([]byte(*stored.Capabilities), []byte(`"runtime_api":"addp.workflow/v1"`)) {
		t.Fatalf("stored capabilities = %v, want submitted workflow declaration", stored.Capabilities)
	}
}

func TestRegisterRuntimeEnginePreservesStableAdvertisedHost(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}

	repo := repository.NewEngineRepository(db)
	router := gin.New()
	handler := NewEngineHandler(service.NewEngineService(repo, nil, nil))
	router.POST("/api/v1/system/runtime/engines", handler.RegisterRuntimeEngine)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/runtime/engines", bytes.NewBufferString(`{
		"engine_type":"custom_runtime",
		"name":"Stable Runtime",
		"connection_info":{"protocol":"http","host":"stable-runtime","port":18080},
		"capabilities":{"schema_version":"engine.capabilities/v1","engine_type":"custom_runtime","engine_family":"custom"},
		"is_builtin":true
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("register status = %d body=%s, want 202", response.Code, response.Body.String())
	}
	stored, err := repo.GetByEngineTypeAndTenant("custom_runtime", nil)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConnectionInfo["host"] != "stable-runtime" {
		t.Fatalf("stored host = %#v, want stable-runtime", stored.ConnectionInfo["host"])
	}
}
