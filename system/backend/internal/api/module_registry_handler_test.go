package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpdateModulePlatformUsesOptimisticVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}); err != nil {
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
