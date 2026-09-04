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
		`CREATE TABLE standard.metric_definitions (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, category_id INTEGER, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, steward_id INTEGER, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL DEFAULT 'active')`,
		`CREATE TABLE standard.metric_definition_revisions (id INTEGER PRIMARY KEY, metric_definition_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, metric_type TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, statistical_caliber TEXT NOT NULL, semantic_formula TEXT, unit_id INTEGER, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.metric_definition_revision_dependencies (id INTEGER PRIMARY KEY, metric_definition_revision_id INTEGER NOT NULL, dependency_definition_id INTEGER NOT NULL, dependency_revision_id INTEGER, relation_kind TEXT NOT NULL, coefficient REAL, note TEXT, created_at DATETIME)`,
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
	if err := db.Exec(`INSERT INTO standard.metric_definitions
		(id, tenant_id, owner_domain_id, scope_type, code, tags, created_by, version, lifecycle_state)
		VALUES (9, 7, 31, 'domain', 'order_amount', '[]', 1, 3, 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO standard.metric_definition_revisions
		(id, metric_definition_id, revision_no, status, metric_type, name, definition, statistical_caliber, change_summary, effective_from, created_by)
		VALUES (19, 9, 1, 'published', 'atomic', 'Current order amount', 'Order amount', 'All completed orders', 'Initial', ?, 1)`, now.Add(-time.Hour)).Error; err != nil {
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
