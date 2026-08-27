package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	standardauthorization "github.com/addp/standard/internal/authorization"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDimensionHierarchyRoutesEnforceTenantScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach standard schema: %v", err)
	}
	createDimensionHierarchyTestSchema(t, db)

	authServer := newTenantScopeAuthServer(t, map[string]string{
		"Bearer tenant-one": "1",
		"Bearer tenant-two": "2",
	})
	defer authServer.Close()

	references := repository.NewTenantReferenceRepository(db)
	dimensionService := service.NewDimensionHierarchyService(
		repository.NewDimensionHierarchyRepository(db),
		references,
		nil,
	)
	router := SetupRouter(db, nil, nil, nil, nil, nil, nil, nil, nil, dimensionService, nil, nil, authServer.URL, modulelifecycle.NewStandalone("standard"))

	t.Run("list only returns current tenant", func(t *testing.T) {
		response := performTenantRequest(router, http.MethodGet, "/api/v1/standard/dimension-hierarchies", "tenant-one", "")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
		}
		var items []models.DimensionHierarchy
		if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(items) != 1 || items[0].TenantID != 1 {
			t.Fatalf("items = %#v, want only tenant 1", items)
		}
	})

	t.Run("detail from another tenant is hidden", func(t *testing.T) {
		response := performTenantRequest(router, http.MethodGet, "/api/v1/standard/dimension-hierarchies/2", "tenant-one", "")
		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNotFound, response.Body.String())
		}
	})

	t.Run("cross tenant domain reference is rejected", func(t *testing.T) {
		body := `{"domain_id":2,"name":"Invalid hierarchy","code":"invalid-hierarchy"}`
		response := performTenantRequest(router, http.MethodPost, "/api/v1/standard/dimension-hierarchies", "tenant-one", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
		}
	})

	t.Run("cross tenant element reference is rejected", func(t *testing.T) {
		body := `{"level_num":1,"name":"Invalid level","element_id":2}`
		response := performTenantRequest(router, http.MethodPost, "/api/v1/standard/dimension-hierarchies/1/levels", "tenant-one", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
		}
	})
}

func createDimensionHierarchyTestSchema(t testing.TB, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE standard.domains (
			id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL, code TEXT NOT NULL,
			lifecycle_state TEXT NOT NULL DEFAULT 'active'
		)`,
		`CREATE TABLE standard.elements (
			id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL, code TEXT NOT NULL,
			lifecycle_state TEXT NOT NULL DEFAULT 'active'
		)`,
		`CREATE TABLE standard.dimension_hierarchies (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			domain_id INTEGER, name TEXT NOT NULL, code TEXT NOT NULL,
			description TEXT, created_by INTEGER NOT NULL, updated_by INTEGER,
			created_at DATETIME, updated_at DATETIME,
			version INTEGER NOT NULL DEFAULT 1,
			lifecycle_state TEXT NOT NULL DEFAULT 'active'
		)`,
		`CREATE TABLE standard.dimension_hierarchy_levels (
			id INTEGER PRIMARY KEY AUTOINCREMENT, hierarchy_id INTEGER NOT NULL,
			level_num INTEGER NOT NULL, name TEXT NOT NULL, element_id INTEGER,
			description TEXT, sort_order INTEGER DEFAULT 0
		)`,
		`INSERT INTO standard.domains (id, tenant_id, name, code) VALUES
			(1, 1, 'Domain one', 'domain-one'),
			(2, 2, 'Domain two', 'domain-two')`,
		`INSERT INTO standard.elements (id, tenant_id, name, code) VALUES
			(1, 1, 'Element one', 'element-one'),
			(2, 2, 'Element two', 'element-two')`,
		`INSERT INTO standard.dimension_hierarchies
			(id, tenant_id, domain_id, name, code, description, created_by) VALUES
			(1, 1, 1, 'Tenant one', 'tenant-one', '', 9),
			(2, 2, 2, 'Tenant two', 'tenant-two', '', 9)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create dimension hierarchy test schema: %v", err)
		}
	}
}

func newTenantScopeAuthServer(t testing.TB, tokens map[string]string) *httptest.Server {
	t.Helper()
	permissions := []string{
		standardauthorization.PermissionStandardDimensionHierarchyRead,
		standardauthorization.PermissionStandardDimensionHierarchyCreate,
		standardauthorization.PermissionStandardDimensionHierarchyUpdate,
		standardauthorization.PermissionStandardDimensionHierarchyDelete,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			http.NotFound(w, r)
			return
		}
		tenantID, ok := tokens[r.Header.Get("Authorization")]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(authtest.NewTenantUserAuthContext(tenantID, "9", permissions)); err != nil {
			t.Errorf("encode auth context: %v", err)
		}
	}))
}

func performTenantRequest(router http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
