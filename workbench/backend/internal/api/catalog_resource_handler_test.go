package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	commonAuth "github.com/addp/common/middleware/auth"
	workbenchauthorization "github.com/addp/workbench/internal/authorization"
	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/repository"
	"github.com/addp/workbench/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCatalogResourceRoutesRequireCatalogServiceAndResolvePublishedApplication(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:workbench-catalog-api?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS workbench`,
		`CREATE TABLE workbench.catalog_resource_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_type TEXT NOT NULL,
			source_identity TEXT NOT NULL, operation TEXT NOT NULL, snapshot JSON NOT NULL, observed_at DATETIME NOT NULL)`,
		`CREATE TABLE workbench.data_applications (
			id TEXT NOT NULL, tenant_id INTEGER NOT NULL, publication_status TEXT NOT NULL,
			current_revision_number INTEGER, PRIMARY KEY (id, tenant_id))`,
		`CREATE TABLE workbench.data_application_revisions (
			application_id TEXT NOT NULL, tenant_id INTEGER NOT NULL, revision_number INTEGER NOT NULL,
			name TEXT NOT NULL, description TEXT NOT NULL, published_at DATETIME NOT NULL,
			PRIMARY KEY (application_id, tenant_id, revision_number))`,
		`INSERT INTO workbench.catalog_resource_changes
			(id, tenant_id, source_type, source_identity, operation, snapshot, observed_at)
			VALUES (42, 7, 'data_application', '1714dcf7-f34e-4996-a8dc-3b88998ebe55', 'upsert', '{"name":"Observed application"}', CURRENT_TIMESTAMP),
			       (43, 8, 'data_application', '1d611f8a-1442-4d6c-bdbb-c86e5d2855cc', 'upsert', '{"name":"Other tenant"}', CURRENT_TIMESTAMP)`,
		`INSERT INTO workbench.data_applications
			(id, tenant_id, publication_status, current_revision_number)
			VALUES ('1714dcf7-f34e-4996-a8dc-3b88998ebe55', 7, 'published', 1)`,
		`INSERT INTO workbench.data_application_revisions
			(application_id, tenant_id, revision_number, name, description, published_at)
			VALUES ('1714dcf7-f34e-4996-a8dc-3b88998ebe55', 7, 1, 'Current application', 'Current description', CURRENT_TIMESTAMP)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}

	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer catalog-token": {ClientID: "addp-catalog", Permissions: []string{workbenchauthorization.PermissionWorkbenchCatalogRead}},
		"Bearer wrong-client":  {ClientID: "addp-asset", Permissions: []string{workbenchauthorization.PermissionWorkbenchCatalogRead}},
		"Bearer no-permission": {ClientID: "addp-catalog", Permissions: []string{workbenchauthorization.PermissionWorkbenchDataApplicationRead}},
	})
	defer authServer.Close()

	handler := NewCatalogResourceHandler(service.NewCatalogResourceService(repository.NewCatalogResourceRepository(db)))
	router := gin.New()
	api := router.Group("/api/v1/workbench")
	api.Use(commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: authServer.URL}), commonAuth.MustNewContextGuard("tenant"))
	catalogRoutes := api.Group("")
	catalogRoutes.Use(commonAuth.MustNewServiceClientGuard("addp-catalog"), commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchCatalogRead))
	catalogRoutes.GET("/catalog-resources/changes", handler.ListChanges)
	catalogRoutes.POST("/runtime/catalog-references/resolve", handler.ResolveReferences)

	body := `{"references":[{"source_type":"data_application","source_identity":"1714dcf7-f34e-4996-a8dc-3b88998ebe55"},{"source_type":"data_application","source_identity":"1d611f8a-1442-4d6c-bdbb-c86e5d2855cc"}]}`
	response := performWorkbenchCatalogRequest(router, http.MethodPost, "/api/v1/workbench/runtime/catalog-references/resolve", "catalog-token", body)
	if response.Code != http.StatusOK {
		t.Fatalf("resolve status = %d; body=%s", response.Code, response.Body.String())
	}
	var resolved models.ResolveCatalogReferencesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &resolved); err != nil {
		t.Fatal(err)
	}
	if len(resolved.Results) != 2 || !resolved.Results[0].Found || resolved.Results[0].Summary["name"] != "Current application" || resolved.Results[0].Version != 42 || resolved.Results[1].Found {
		t.Fatalf("resolved = %#v", resolved.Results)
	}

	for _, testCase := range []struct {
		token string
		want  int
	}{{"wrong-client", http.StatusForbidden}, {"no-permission", http.StatusForbidden}, {"", http.StatusUnauthorized}} {
		response := performWorkbenchCatalogRequest(router, http.MethodGet, "/api/v1/workbench/catalog-resources/changes", testCase.token, "")
		if response.Code != testCase.want {
			t.Fatalf("token %q status = %d, want %d; body=%s", testCase.token, response.Code, testCase.want, response.Body.String())
		}
	}
}

func performWorkbenchCatalogRequest(router http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
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
