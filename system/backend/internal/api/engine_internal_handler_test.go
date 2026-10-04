package api

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newRuntimeRegistrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite connection pool: %v", err)
	}
	// Registration starts an asynchronous observation. SQLite shared-cache table
	// locks must not make its writes race with assertions on another connection.
	sqlDB.SetMaxOpenConns(1)
	observationDone := make(chan error, 1)
	if err := db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test:runtime_observation_done", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]interface{})
		if tx.Statement.Table != "engines" || !ok || values["last_check_at"] == nil {
			return
		}
		err := tx.Error
		if err == nil && (tx.RowsAffected != 1 || values["connection_status"] != models.EngineConnectionOffline) {
			err = fmt.Errorf("observation rows=%d status=%v, want one offline runtime", tx.RowsAffected, values["connection_status"])
		}
		observationDone <- err
	}); err != nil {
		_ = sqlDB.Close()
		t.Fatalf("observe connection check: %v", err)
	}
	t.Cleanup(func() {
		// These fixtures fail the connection check deliberately. Its offline
		// observation is the last database operation before the worker returns.
		select {
		case err := <-observationDone:
			if err != nil {
				t.Errorf("background connection observation: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("background connection observation did not finish")
		}
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate engine: %v", err)
	}
	return db
}

func newOfflineRuntimeTestServer(t *testing.T) (string, int) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	address := server.Listener.Addr().String()
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return address, port
}

func TestRegisterRuntimeEngineCreatesNewIDForDifferentPhysicalEndpoint(t *testing.T) {
	address, port := newOfflineRuntimeTestServer(t)
	db := newRuntimeRegistrationTestDB(t)

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
		bytes.NewBufferString(fmt.Sprintf(`{"engine_type":"geopython_workflow","name":"GeoPython Runtime","connection_info":{"protocol":"http","port":%d},"capabilities":{"schema_version":"engine.capabilities/v1","engine_type":"geopython_workflow","engine_family":"workflow","compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}}},"is_builtin":true}`, port)),
	)
	request.RemoteAddr = address
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("register status = %d body=%s, want 202", response.Code, response.Body.String())
	}
	stored, err := repo.GetByID(engine.ID)
	if err != nil {
		t.Fatalf("get stored engine: %v", err)
	}
	if stored.ConnectionInfo["host"] != "runtime.internal" {
		t.Fatalf("stored host = %#v, want unchanged runtime.internal", stored.ConnectionInfo["host"])
	}
	var count int64
	if err := db.Model(&models.Engine{}).Count(&count).Error; err != nil {
		t.Fatalf("count engines: %v", err)
	}
	if count != 2 {
		t.Fatalf("engine count = %d, want 2 permanent instances", count)
	}
}

func TestRegisterRuntimeEngineKeepsSubmittedCapabilitiesForBuiltinCustomRuntime(t *testing.T) {
	address, port := newOfflineRuntimeTestServer(t)
	db := newRuntimeRegistrationTestDB(t)

	repo := repository.NewEngineRepository(db)
	router := gin.New()
	handler := NewEngineHandler(service.NewEngineService(repo, nil, nil))
	router.POST("/api/v1/system/runtime/engines", handler.RegisterRuntimeEngine)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/system/runtime/engines",
		bytes.NewBufferString(fmt.Sprintf(`{
			"engine_type":"tenant_workflow_runtime",
			"name":"Tenant Workflow Runtime",
			"connection_info":{"protocol":"http","port":%d},
			"capabilities":{
				"schema_version":"engine.capabilities/v1",
				"engine_type":"tenant_workflow_runtime",
				"engine_family":"workflow",
				"compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}}
			},
			"is_builtin":true
		}`, port)),
	)
	request.RemoteAddr = address
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("register status = %d body=%s, want 202", response.Code, response.Body.String())
	}

	var stored models.Engine
	err := db.Where("engine_type = ?", "tenant_workflow_runtime").First(&stored).Error
	if err != nil {
		t.Fatalf("get registered runtime: %v", err)
	}
	if stored.Capabilities == nil || !bytes.Contains([]byte(*stored.Capabilities), []byte(`"runtime_api":"addp.workflow/v1"`)) {
		t.Fatalf("stored capabilities = %v, want submitted workflow declaration", stored.Capabilities)
	}
}

func TestRegisterRuntimeEnginePreservesStableAdvertisedHost(t *testing.T) {
	db := newRuntimeRegistrationTestDB(t)

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
	var stored models.Engine
	err := db.Where("engine_type = ?", "custom_runtime").First(&stored).Error
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConnectionInfo["host"] != "stable-runtime" {
		t.Fatalf("stored host = %#v, want stable-runtime", stored.ConnectionInfo["host"])
	}
}

func TestRegisterRuntimeEngineReadDuringConnectionObservation(t *testing.T) {
	db := newRuntimeRegistrationTestDB(t)
	writeHeld := make(chan struct{})
	releaseWrite := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseWrite) }) }
	t.Cleanup(release)
	if err := db.Callback().Update().After("gorm:update").Before("gorm:commit_or_rollback_transaction").Register("test:hold_runtime_observation", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]interface{})
		if tx.Statement.Table == "engines" && ok && values["last_check_at"] != nil && tx.Error == nil {
			close(writeHeld)
			<-releaseWrite
		}
	}); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.POST("/api/v1/system/runtime/engines", NewEngineHandler(service.NewEngineService(repository.NewEngineRepository(db), nil, nil)).RegisterRuntimeEngine)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/runtime/engines", bytes.NewBufferString(`{
		"engine_type":"custom_runtime",
		"name":"Concurrent Runtime",
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
	select {
	case <-writeHeld:
	case <-time.After(5 * time.Second):
		t.Fatal("connection observation did not reach the uncommitted write")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	readDone := make(chan error, 1)
	var stored models.Engine
	go func() {
		readDone <- db.WithContext(ctx).Where("engine_type = ?", "custom_runtime").First(&stored).Error
	}()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-readDone:
			release()
			if err != nil {
				t.Fatalf("read concurrent with connection observation: %v", err)
			}
			if stored.ConnectionStatus != models.EngineConnectionOffline || stored.LastCheckAt == nil {
				t.Fatalf("read did not observe the committed connection check: %#v", stored)
			}
			return
		case <-ticker.C:
			// Release only once the reader is contending for the held connection.
			// With the old pool it instead obtains a second connection and fails
			// with SQLITE_LOCKED while the observation owns the table write lock.
			if sqlDB.Stats().WaitCount > 0 {
				release()
			}
		case <-ctx.Done():
			release()
			t.Fatal("concurrent runtime read did not finish")
		}
	}
}
