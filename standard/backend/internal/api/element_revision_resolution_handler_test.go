package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	standardauthorization "github.com/addp/standard/internal/authorization"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestElementRevisionResolutionRouteRequiresCatalogOrModelService(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:standard-element-resolution-api?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS standard`,
		`CREATE TABLE standard.elements (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, lifecycle_state TEXT NOT NULL)`,
		`CREATE TABLE standard.element_revisions (
			id INTEGER PRIMARY KEY, element_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL,
			name TEXT NOT NULL, definition TEXT NOT NULL, data_type TEXT NOT NULL, nullable NUMERIC NOT NULL,
			default_value TEXT, format TEXT, value_domain_kind TEXT NOT NULL,
			effective_from DATETIME, effective_to DATETIME)`,
		`INSERT INTO standard.elements (id, tenant_id, code, lifecycle_state) VALUES (10, 7, 'order_id', 'active'), (20, 8, 'foreign', 'active')`,
		`INSERT INTO standard.element_revisions
			(id, element_id, revision_no, status, name, definition, data_type, nullable, default_value, format, value_domain_kind, effective_from)
		 VALUES (101, 10, 2, 'published', 'Order ID', 'Order identifier', 'bigint', 0, '', '', 'unrestricted', '2026-01-01T00:00:00Z'),
		        (201, 20, 1, 'published', 'Foreign', 'Foreign identifier', 'bigint', 0, '', '', 'unrestricted', '2026-01-01T00:00:00Z')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer catalog-token": {ClientID: "addp-catalog", Permissions: []string{standardauthorization.PermissionStandardElementRead}},
		"Bearer model-token":   {ClientID: "addp-model", Permissions: []string{standardauthorization.PermissionStandardElementRead}},
		"Bearer asset-token":   {ClientID: "addp-asset", Permissions: []string{standardauthorization.PermissionStandardElementRead}},
		"Bearer no-permission": {ClientID: "addp-catalog", Permissions: []string{standardauthorization.PermissionStandardCodeSetRead}},
	})
	defer authServer.Close()
	resolutionService := service.NewElementRevisionResolutionService(repository.NewElementRepository(db), repository.NewCodeSetRepository(db))
	router := SetupRouter(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, resolutionService, nil, authServer.URL, modulelifecycle.NewStandalone("standard"))
	body := `{"element_ids":["10","20","999"],"as_of":"2026-08-28T10:00:00Z"}`
	for _, token := range []string{"catalog-token", "model-token"} {
		response := performTenantRequest(router, http.MethodPost, "/api/v1/standard/runtime/element-revisions/resolve", token, body)
		if response.Code != http.StatusOK {
			t.Fatalf("token %q status = %d; body=%s", token, response.Code, response.Body.String())
		}
		var resolved elementRevisionResolutionResponse
		if err := json.Unmarshal(response.Body.Bytes(), &resolved); err != nil {
			t.Fatal(err)
		}
		if len(resolved.Results) != 3 || !resolved.Results[0].Found || resolved.Results[0].Snapshot == nil || resolved.Results[0].Snapshot.ElementRevisionID != 101 || resolved.Results[1].Found || resolved.Results[2].Found {
			t.Fatalf("resolved = %#v", resolved.Results)
		}
		if !resolved.Results[0].Snapshot.EffectiveFrom.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("effective_from = %s", resolved.Results[0].Snapshot.EffectiveFrom)
		}
	}
	for _, testCase := range []struct {
		token string
		want  int
	}{{"asset-token", http.StatusForbidden}, {"no-permission", http.StatusForbidden}, {"", http.StatusUnauthorized}} {
		response := performTenantRequest(router, http.MethodPost, "/api/v1/standard/runtime/element-revisions/resolve", testCase.token, body)
		if response.Code != testCase.want {
			t.Fatalf("token %q status = %d, want %d; body=%s", testCase.token, response.Code, testCase.want, response.Body.String())
		}
	}
}
