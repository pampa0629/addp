package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	standardauthorization "github.com/addp/standard/internal/authorization"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCatalogResourceRoutesRequireCatalogServiceAndResolveCurrentMetric(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:standard-catalog-api?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS standard`,
		`CREATE TABLE standard.catalog_resource_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_type TEXT NOT NULL,
			source_identity INTEGER NOT NULL, operation TEXT NOT NULL, resource_version INTEGER NOT NULL,
			snapshot JSON NOT NULL, observed_at DATETIME NOT NULL)`,
		`CREATE TABLE standard.metrics (
			id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, category_id INTEGER, domain_id INTEGER,
			name TEXT NOT NULL, code TEXT NOT NULL, type TEXT NOT NULL, definition TEXT NOT NULL, formula TEXT NOT NULL,
			unit_id INTEGER, base_metric_id INTEGER, derivation_config JSON, status TEXT NOT NULL, steward_id INTEGER,
			tags JSON, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME,
			version INTEGER NOT NULL, lifecycle_state TEXT NOT NULL)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO standard.catalog_resource_changes
		(tenant_id, source_type, source_identity, operation, resource_version, snapshot, observed_at)
		VALUES (7, 'metric', 9, 'upsert', 2, '{"name":"Order amount"}', ?),
		       (8, 'metric', 10, 'upsert', 1, '{"name":"Other tenant"}', ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO standard.metrics
		(id, tenant_id, domain_id, name, code, type, definition, formula, derivation_config, status, tags, created_by, version, lifecycle_state)
		VALUES (9, 7, 31, 'Current order amount', 'order_amount', 'atomic', '', '', '{}', 'approved', '[]', 1, 3, 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer catalog-token": {ClientID: "addp-catalog", Permissions: []string{standardauthorization.PermissionStandardCatalogRead}},
		"Bearer wrong-client":  {ClientID: "addp-asset", Permissions: []string{standardauthorization.PermissionStandardCatalogRead}},
		"Bearer no-permission": {ClientID: "addp-catalog", Permissions: []string{standardauthorization.PermissionStandardMetricRead}},
	})
	defer authServer.Close()
	catalogService := service.NewCatalogResourceService(repository.NewCatalogResourceRepository(db))
	router := SetupRouter(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, catalogService, authServer.URL, modulelifecycle.NewStandalone("standard"))

	changesResponse := performTenantRequest(router, http.MethodGet, "/api/v1/standard/catalog-resources/changes", "catalog-token", "")
	if changesResponse.Code != http.StatusOK {
		t.Fatalf("changes status = %d; body=%s", changesResponse.Code, changesResponse.Body.String())
	}
	var changes models.CatalogResourceChangesResponse
	if err := json.Unmarshal(changesResponse.Body.Bytes(), &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 1 || changes.Changes[0].SourceIdentity != "9" {
		t.Fatalf("changes = %#v", changes.Changes)
	}

	body := `{"references":[{"source_type":"metric","source_identity":"9"},{"source_type":"metric","source_identity":"10"}]}`
	resolveResponse := performTenantRequest(router, http.MethodPost, "/api/v1/standard/runtime/catalog-references/resolve", "catalog-token", body)
	if resolveResponse.Code != http.StatusOK {
		t.Fatalf("resolve status = %d; body=%s", resolveResponse.Code, resolveResponse.Body.String())
	}
	var resolved models.ResolveCatalogReferencesResponse
	if err := json.Unmarshal(resolveResponse.Body.Bytes(), &resolved); err != nil {
		t.Fatal(err)
	}
	if len(resolved.Results) != 2 || !resolved.Results[0].Found || resolved.Results[0].Summary["name"] != "Current order amount" || resolved.Results[1].Found {
		t.Fatalf("resolved = %#v", resolved.Results)
	}

	for _, testCase := range []struct {
		token string
		want  int
	}{{"wrong-client", http.StatusForbidden}, {"no-permission", http.StatusForbidden}, {"", http.StatusUnauthorized}} {
		response := performTenantRequest(router, http.MethodGet, "/api/v1/standard/catalog-resources/changes", testCase.token, "")
		if response.Code != testCase.want {
			t.Fatalf("token %q status = %d, want %d; body=%s", testCase.token, response.Code, testCase.want, response.Body.String())
		}
	}
}
