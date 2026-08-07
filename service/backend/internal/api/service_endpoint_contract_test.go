package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authtest"
	"github.com/addp/service/internal/authorization"
	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	services "github.com/addp/service/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestServiceEndpointRouteRequiresPortalServicePrincipalAndTenantPermission(t *testing.T) {
	db := serviceEndpointTestDB(t)
	seedServiceEndpointQuery(t, db, 1, 7, "tenant-seven")
	seedServiceEndpointQuery(t, db, 2, 8, "tenant-eight")

	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer portal":        {ClientID: "addp-portal", Permissions: []string{authorization.PermissionServiceEndpointRead}},
		"Bearer wrong-client":  {ClientID: "addp-meta", Permissions: []string{authorization.PermissionServiceEndpointRead}},
		"Bearer no-permission": {ClientID: "addp-portal", Permissions: []string{authorization.PermissionServiceDefinitionRead}},
	})
	defer authServer.Close()
	router := serviceEndpointTestRouter(t, db, authServer.URL)

	tests := []struct {
		name       string
		token      string
		ref        string
		legacy     bool
		wantStatus int
		wantTitle  string
	}{
		{name: "portal service", token: "portal", ref: "query:1", wantStatus: http.StatusOK, wantTitle: "tenant-seven"},
		{name: "wrong service client", token: "wrong-client", ref: "query:1", wantStatus: http.StatusForbidden},
		{name: "missing permission", token: "no-permission", ref: "query:1", wantStatus: http.StatusForbidden},
		{name: "tenant isolation", token: "portal", ref: "query:2", wantStatus: http.StatusNotFound},
		{name: "legacy headers", legacy: true, ref: "query:1", wantStatus: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/service/endpoints?ref="+tt.ref, nil)
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			if tt.legacy {
				request.Header.Set("X-Internal-API-Key", "legacy")
				request.Header.Set("X-Tenant-ID", "7")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status=%d, want=%d body=%s", response.Code, tt.wantStatus, response.Body.String())
			}
			if tt.wantTitle != "" {
				var body map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if body["title"] != tt.wantTitle {
					t.Fatalf("title=%v, want=%q", body["title"], tt.wantTitle)
				}
			}
		})
	}
}

func TestServiceEndpointRouteRejectsUserActor(t *testing.T) {
	db := serviceEndpointTestDB(t)
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer user": {authorization.PermissionServiceEndpointRead},
	})
	defer authServer.Close()
	router := serviceEndpointTestRouter(t, db, authServer.URL)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/service/endpoints?ref=query:1", nil)
	request.Header.Set("Authorization", "Bearer user")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want=403 body=%s", response.Code, response.Body.String())
	}
}

func serviceEndpointTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS service").Error; err != nil {
		t.Fatalf("attach service schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE service.query_services (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, service_name TEXT NOT NULL,
		title TEXT NOT NULL, description TEXT, keywords TEXT, config_type TEXT, engine_id INTEGER, runtime_engine_id INTEGER,
		schema_name TEXT, table_name TEXT, sql_query TEXT, data_config TEXT, protocols TEXT,
		public_access BOOLEAN, max_features INTEGER, status TEXT, error_message TEXT, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create service.query_services: %v", err)
	}
	return db
}

func seedServiceEndpointQuery(t *testing.T, db *gorm.DB, id, tenantID uint, title string) {
	t.Helper()
	query := &models.QueryService{
		ID: id, TenantID: tenantID, ServiceName: "query-" + title, Title: title,
		ConfigType: "sql", SqlQuery: "SELECT 1", DataConfig: models.JSONB{}, Protocols: models.JSONB{},
		Status: "active", CreatedBy: 1,
	}
	if err := db.Create(query).Error; err != nil {
		t.Fatalf("seed query service: %v", err)
	}
}

func serviceEndpointTestRouter(t *testing.T, db *gorm.DB, systemURL string) http.Handler {
	t.Helper()
	querySvc := services.NewQueryServiceService(repository.NewQueryServiceRepository(db), nil, nil, "http://gateway")
	registeredSvc := services.NewRegisteredServiceService(repository.NewRegisteredServiceRepository(db), "http://gateway")
	tileSvc := services.NewTileServiceService(repository.NewTileServiceRepository(db), nil, "http://gateway")
	endpointHandler := NewServiceEndpointHandler(querySvc, registeredSvc, tileSvc)
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemURL
	return SetupRouter(cfg, db, nil, nil, nil, nil, nil, nil, nil, nil, nil, endpointHandler, nil, nil, nil)
}
