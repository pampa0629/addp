package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sharedauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModuleRegistryRuntimeErrorsExposeStableCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("invalid registration", func(t *testing.T) {
		router := newModuleRegistryRuntimeTestRouter(t, "addp-manager")
		response := performModuleRegistryRequest(router, http.MethodPost, "/runtime/modules", `{}`)
		assertModuleRegistryErrorCode(t, response, http.StatusBadRequest, moduleRegistrationInvalidErrorCode)
	})

	t.Run("service principal does not own module", func(t *testing.T) {
		router := newModuleRegistryRuntimeTestRouter(t, "addp-meta")
		response := performModuleRegistryRequest(router, http.MethodPost, "/runtime/modules", `{
			"module_name":"manager","instance_id":"manager-1","role":"backend",
			"module_url":"http://manager:8080","route_prefix":"/manager"
		}`)
		assertModuleRegistryErrorCode(t, response, http.StatusForbidden, moduleRegistryForbiddenErrorCode)
	})

	t.Run("heartbeat instance is missing", func(t *testing.T) {
		router := newModuleRegistryRuntimeTestRouter(t, "addp-manager")
		response := performModuleRegistryRequest(router, http.MethodPost, "/runtime/modules/heartbeat", `{
			"module_name":"manager","instance_id":"missing-instance"
		}`)
		assertModuleRegistryErrorCode(t, response, http.StatusNotFound, moduleRuntimeInstanceMissingErrorCode)
	})
}

func newModuleRegistryRuntimeTestRouter(t *testing.T, clientID string) *gin.Engine {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name()+clientID)+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	registry := service.NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
	handler := NewModuleRegistryHandler(registry)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, testIAMServiceActorContext("platform", clientID)); err != nil {
			panic(err)
		}
		c.Next()
	})
	router.POST("/runtime/modules", handler.RegisterService)
	router.POST("/runtime/modules/heartbeat", handler.HeartbeatService)
	return router
}

func performModuleRegistryRequest(router http.Handler, method, path, payload string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertModuleRegistryErrorCode(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, wantStatus, response.Body.String())
	}
	var payload models.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error == "" || payload.ErrorCode != wantCode {
		t.Fatalf("error response = %#v, want error_code %q", payload, wantCode)
	}
}

func TestUpdateModulePlatformUsesOptimisticVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	registry := service.NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
	if err := registry.Register(&models.ModuleRegistrationRequest{
		ModuleName: "manager", InstanceID: "manager-backend-1", Role: models.ModuleRuntimeRoleBackend,
		ModuleURL: "http://manager:8080", RoutePrefix: "/manager",
	}); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.PUT("/platform/modules/:module_name", NewModuleRegistryHandler(registry).UpdateModulePlatform)

	response := updatePlatformModule(t, router, `{"enabled":false,"version":1}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var updated models.ModuleInfo
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated module: %v", err)
	}
	if updated.Enabled || updated.Version != 2 {
		t.Fatalf("updated module = %#v, want disabled version 2", updated)
	}

	response = updatePlatformModule(t, router, `{"enabled":true,"version":1}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	var conflict struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.ErrorCode != "resource_version_conflict" {
		t.Fatalf("error_code = %q, want resource_version_conflict", conflict.ErrorCode)
	}
}

func updatePlatformModule(t *testing.T, router http.Handler, payload string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, "/platform/modules/manager", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
