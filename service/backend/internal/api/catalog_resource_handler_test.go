package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	serviceauthorization "github.com/addp/service/internal/authorization"
	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"github.com/addp/service/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCatalogResourceRoutesRequireCatalogServiceAndResolveCurrentQueryService(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:service-catalog-api?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS service`,
		`CREATE TABLE service.catalog_resource_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_type TEXT NOT NULL,
			source_identity INTEGER NOT NULL, operation TEXT NOT NULL, snapshot JSON NOT NULL, observed_at DATETIME NOT NULL)`,
		`CREATE TABLE service.query_services (
			id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, service_name TEXT NOT NULL, title TEXT NOT NULL,
			config_type TEXT NOT NULL, public_access BOOLEAN NOT NULL, status TEXT NOT NULL,
			engine_id INTEGER, runtime_engine_id INTEGER)`,
		`INSERT INTO service.catalog_resource_changes
			(id, tenant_id, source_type, source_identity, operation, snapshot, observed_at)
			VALUES (42, 7, 'query_service', 9, 'upsert', '{"name":"Observed orders"}', CURRENT_TIMESTAMP),
			       (43, 8, 'query_service', 10, 'upsert', '{"name":"Other tenant"}', CURRENT_TIMESTAMP)`,
		`INSERT INTO service.query_services
			(id, tenant_id, service_name, title, config_type, public_access, status)
			VALUES (9, 7, 'orders', 'Current orders', 'sql', TRUE, 'active')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer catalog-token": {ClientID: "addp-catalog", Permissions: []string{serviceauthorization.PermissionServiceCatalogRead}},
		"Bearer wrong-client":  {ClientID: "addp-asset", Permissions: []string{serviceauthorization.PermissionServiceCatalogRead}},
		"Bearer no-permission": {ClientID: "addp-catalog", Permissions: []string{serviceauthorization.PermissionServiceDefinitionRead}},
	})
	defer authServer.Close()
	cfg := &config.Config{}
	cfg.SystemServiceURL = authServer.URL
	handler := NewCatalogResourceHandler(service.NewCatalogResourceService(repository.NewCatalogResourceRepository(db)))
	router := SetupRouter(cfg, db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, handler, nil, nil, nil, nil, modulelifecycle.NewStandalone("service"))

	body := `{"references":[{"source_type":"query_service","source_identity":"9"},{"source_type":"query_service","source_identity":"10"}]}`
	resolvedResponse := performServiceTenantRequest(router, http.MethodPost, "/api/v1/service/runtime/catalog-references/resolve", "catalog-token", body)
	if resolvedResponse.Code != http.StatusOK {
		t.Fatalf("resolve status = %d; body=%s", resolvedResponse.Code, resolvedResponse.Body.String())
	}
	var resolved models.ResolveCatalogReferencesResponse
	if err := json.Unmarshal(resolvedResponse.Body.Bytes(), &resolved); err != nil {
		t.Fatal(err)
	}
	if len(resolved.Results) != 2 || !resolved.Results[0].Found || resolved.Results[0].Summary["name"] != "Current orders" || resolved.Results[0].Version != 42 || resolved.Results[1].Found {
		t.Fatalf("resolved = %#v", resolved.Results)
	}

	for _, testCase := range []struct {
		token string
		want  int
	}{{"wrong-client", http.StatusForbidden}, {"no-permission", http.StatusForbidden}, {"", http.StatusUnauthorized}} {
		response := performServiceTenantRequest(router, http.MethodGet, "/api/v1/service/catalog-resources/changes", testCase.token, "")
		if response.Code != testCase.want {
			t.Fatalf("token %q status = %d, want %d; body=%s", testCase.token, response.Code, testCase.want, response.Body.String())
		}
	}
}

func performServiceTenantRequest(router http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
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
