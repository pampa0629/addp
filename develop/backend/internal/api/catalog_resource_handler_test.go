package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	commonAuth "github.com/addp/common/middleware/auth"
	developauthorization "github.com/addp/develop/backend/internal/authorization"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCatalogResourceRoutesRequireCatalogServiceAndResolveReusableTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:develop-catalog-api?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS develop`,
		`CREATE TABLE develop.catalog_resource_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_type TEXT NOT NULL,
			source_identity INTEGER NOT NULL, operation TEXT NOT NULL, snapshot JSON NOT NULL, observed_at DATETIME NOT NULL)`,
		`CREATE TABLE develop.dev_tasks (
			id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, display_name TEXT,
			dev_type TEXT NOT NULL, content JSON, execution_config JSON, status TEXT NOT NULL, deleted_at DATETIME)`,
		`INSERT INTO develop.catalog_resource_changes
			(id, tenant_id, source_type, source_identity, operation, snapshot, observed_at)
			VALUES (42, 7, 'dev_task', 9, 'upsert', '{"name":"Observed workflow"}', CURRENT_TIMESTAMP),
			       (43, 8, 'dev_task', 10, 'upsert', '{"name":"Other tenant"}', CURRENT_TIMESTAMP)`,
		`INSERT INTO develop.dev_tasks
			(id, tenant_id, name, display_name, dev_type, content, execution_config, status)
			VALUES (9, 7, 'orders', 'Current orders workflow', 'workflow', '{}', '{}', 'active')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer catalog-token": {ClientID: "addp-catalog", Permissions: []string{developauthorization.PermissionDevelopCatalogRead}},
		"Bearer wrong-client":  {ClientID: "addp-asset", Permissions: []string{developauthorization.PermissionDevelopCatalogRead}},
		"Bearer no-permission": {ClientID: "addp-catalog", Permissions: []string{developauthorization.PermissionDevelopTaskRead}},
	})
	defer authServer.Close()
	handler := NewCatalogResourceHandler(service.NewCatalogResourceService(repository.NewCatalogResourceRepository(db)))
	router := gin.New()
	api := router.Group("/api/v1/develop")
	api.Use(commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: authServer.URL}), commonAuth.MustNewContextGuard("tenant"))
	catalogRoutes := api.Group("")
	catalogRoutes.Use(commonAuth.MustNewServiceClientGuard("addp-catalog"), commonAuth.MustNewPermissionGuard(developauthorization.PermissionDevelopCatalogRead))
	catalogRoutes.GET("/catalog-resources/changes", handler.ListChanges)
	catalogRoutes.POST("/runtime/catalog-references/resolve", handler.ResolveReferences)

	body := `{"references":[{"source_type":"dev_task","source_identity":"9"},{"source_type":"dev_task","source_identity":"10"}]}`
	response := performDevelopCatalogRequest(router, http.MethodPost, "/api/v1/develop/runtime/catalog-references/resolve", "catalog-token", body)
	if response.Code != http.StatusOK {
		t.Fatalf("resolve status = %d; body=%s", response.Code, response.Body.String())
	}
	var resolved models.ResolveCatalogReferencesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &resolved); err != nil {
		t.Fatal(err)
	}
	if len(resolved.Results) != 2 || !resolved.Results[0].Found || resolved.Results[0].Summary["name"] != "Current orders workflow" || resolved.Results[0].Version != 42 || resolved.Results[1].Found {
		t.Fatalf("resolved = %#v", resolved.Results)
	}
	for _, testCase := range []struct {
		token string
		want  int
	}{{"wrong-client", http.StatusForbidden}, {"no-permission", http.StatusForbidden}, {"", http.StatusUnauthorized}} {
		response := performDevelopCatalogRequest(router, http.MethodGet, "/api/v1/develop/catalog-resources/changes", testCase.token, "")
		if response.Code != testCase.want {
			t.Fatalf("token %q status = %d, want %d; body=%s", testCase.token, response.Code, testCase.want, response.Body.String())
		}
	}
}

func performDevelopCatalogRequest(router http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
