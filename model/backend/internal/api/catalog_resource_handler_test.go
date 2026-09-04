package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	modelauthorization "github.com/addp/model/internal/authorization"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"github.com/addp/model/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCatalogResourceRoutesRequireCatalogServiceAndResolveCurrentOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:model-catalog-api?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS model`,
		`CREATE TABLE model.catalog_resource_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_type TEXT NOT NULL,
			source_identity INTEGER NOT NULL, operation TEXT NOT NULL, resource_version INTEGER NOT NULL,
			snapshot JSON NOT NULL, observed_at DATETIME NOT NULL)`,
		`CREATE TABLE model.entities (
			id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, domain_id INTEGER, name TEXT NOT NULL, code TEXT NOT NULL,
			description TEXT NOT NULL, status TEXT NOT NULL, version INTEGER NOT NULL, created_by INTEGER NOT NULL,
			updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE model.logical_tables (
			id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, domain_id INTEGER, entity_id INTEGER, name TEXT NOT NULL,
			code TEXT NOT NULL, description TEXT NOT NULL, table_type TEXT NOT NULL, layer TEXT, status TEXT NOT NULL,
			grain_description TEXT NOT NULL, scd_type INTEGER NOT NULL, materialization JSON, version INTEGER NOT NULL,
			created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare Model test schema: %v", err)
		}
	}
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO model.catalog_resource_changes
		(tenant_id, source_type, source_identity, operation, resource_version, snapshot, observed_at)
		VALUES (7, 'entity', 9, 'upsert', 2, '{"name":"Orders"}', ?),
		       (8, 'entity', 10, 'upsert', 1, '{"name":"Other tenant"}', ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO model.entities
		(id, tenant_id, domain_id, name, code, description, status, version, created_by, created_at, updated_at)
		VALUES (9, 7, 31, 'Current Orders', 'orders', '', 'approved', 3, 1, ?, ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}

	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer catalog-token": {ClientID: "addp-catalog", Permissions: []string{modelauthorization.PermissionModelCatalogRead}},
		"Bearer wrong-client":  {ClientID: "addp-asset", Permissions: []string{modelauthorization.PermissionModelCatalogRead}},
		"Bearer no-permission": {ClientID: "addp-catalog", Permissions: []string{modelauthorization.PermissionModelEntityRead}},
	})
	defer authServer.Close()
	catalogService := service.NewCatalogResourceService(repository.NewCatalogResourceRepository(db))
	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, catalogService, nil, authServer.URL, nil, modulelifecycle.NewStandalone("model"))

	changesResponse := performModelCatalogRequest(router, http.MethodGet, "/api/v1/model/catalog-resources/changes", "catalog-token", nil)
	if changesResponse.Code != http.StatusOK {
		t.Fatalf("changes status = %d; body=%s", changesResponse.Code, changesResponse.Body.String())
	}
	var changes models.CatalogResourceChangesResponse
	if err := json.Unmarshal(changesResponse.Body.Bytes(), &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 1 || changes.Changes[0].SourceIdentity != "9" {
		t.Fatalf("tenant-scoped changes = %#v", changes.Changes)
	}

	body := []byte(`{"references":[{"source_type":"entity","source_identity":"9"},{"source_type":"entity","source_identity":"10"}]}`)
	resolveResponse := performModelCatalogRequest(router, http.MethodPost, "/api/v1/model/runtime/catalog-references/resolve", "catalog-token", body)
	if resolveResponse.Code != http.StatusOK {
		t.Fatalf("resolve status = %d; body=%s", resolveResponse.Code, resolveResponse.Body.String())
	}
	var resolved models.ResolveCatalogReferencesResponse
	if err := json.Unmarshal(resolveResponse.Body.Bytes(), &resolved); err != nil {
		t.Fatal(err)
	}
	if len(resolved.Results) != 2 || !resolved.Results[0].Found || resolved.Results[0].Summary["name"] != "Current Orders" || resolved.Results[1].Found {
		t.Fatalf("resolved references = %#v", resolved.Results)
	}

	for _, testCase := range []struct {
		token string
		want  int
	}{
		{token: "wrong-client", want: http.StatusForbidden},
		{token: "no-permission", want: http.StatusForbidden},
		{token: "", want: http.StatusUnauthorized},
	} {
		response := performModelCatalogRequest(router, http.MethodGet, "/api/v1/model/catalog-resources/changes", testCase.token, nil)
		if response.Code != testCase.want {
			t.Fatalf("token %q status = %d, want %d; body=%s", testCase.token, response.Code, testCase.want, response.Body.String())
		}
	}
}

func performModelCatalogRequest(handler http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
