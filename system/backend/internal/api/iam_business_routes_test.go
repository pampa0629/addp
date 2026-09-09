package api

import (
	"sort"
	"testing"

	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRegisterIAMMigratedBusinessRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:iam-business-routes?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig(), testIAMSecurityPolicy())
	if err != nil {
		t.Fatal(err)
	}
	engineHandler := NewEngineHandler(service.NewEngineService(repository.NewEngineRepository(db), nil, nil))
	apiConsumerHandler := NewAPIConsumerHandler(service.NewAPIConsumerService(repository.NewAPIConsumerRepository(db)))
	cleanupHandler := NewCleanupHandler(nil)
	router := gin.New()
	api := router.Group("/api/v1/system")
	if err := RegisterIAMMigratedBusinessRoutes(api, runtime, engineHandler, apiConsumerHandler, cleanupHandler); err != nil {
		t.Fatalf("RegisterIAMMigratedBusinessRoutes() error = %v", err)
	}

	actual := make([]string, 0, len(router.Routes()))
	for _, route := range router.Routes() {
		actual = append(actual, route.Method+" "+route.Path)
	}
	sort.Strings(actual)
	want := []string{
		"DELETE /api/v1/system/tenant/api-consumers/:id",
		"DELETE /api/v1/system/tenant/api-consumers/:id/credentials/:credential_id",
		"DELETE /api/v1/system/engines/:id",
		"GET /api/v1/system/tenant/api-consumers",
		"GET /api/v1/system/tenant/api-consumers/:id",
		"GET /api/v1/system/tenant/api-consumers/:id/credentials",
		"GET /api/v1/system/admin/cleanup/history",
		"GET /api/v1/system/admin/cleanup/tasks/:task_id",
		"GET /api/v1/system/engine-types",
		"GET /api/v1/system/engines",
		"GET /api/v1/system/engines/:id",
		"GET /api/v1/system/engines/:id/deletion-assessments/:assessment_id",
		"POST /api/v1/system/tenant/api-consumers",
		"POST /api/v1/system/tenant/api-consumers/:id/credentials",
		"POST /api/v1/system/admin/cleanup/execute",
		"POST /api/v1/system/admin/cleanup/scan",
		"POST /api/v1/system/engines",
		"POST /api/v1/system/engines/:id/catalog/children",
		"POST /api/v1/system/engines/:id/catalog/facts",
		"POST /api/v1/system/engines/:id/deletion-assessments",
		"POST /api/v1/system/engines/:id/restore",
		"POST /api/v1/system/engines/:id/spatial-workspaces/:ecosystem/:kind/enable",
		"POST /api/v1/system/engines/:id/test",
		"POST /api/v1/system/engines/test-connection",
		"PUT /api/v1/system/tenant/api-consumers/:id",
		"PUT /api/v1/system/engines/:id",
	}
	sort.Strings(want)
	if len(actual) != len(want) {
		t.Fatalf("routes = %#v, want %#v", actual, want)
	}
	for index := range want {
		if actual[index] != want[index] {
			t.Fatalf("routes = %#v, want %#v", actual, want)
		}
	}
}
