package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/inference/internal/config"
	"github.com/addp/inference/internal/repository"
	"github.com/addp/inference/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModelLabelRouteEnforcesNarrowTenantPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokenPermissions := map[string][]string{
		"Bearer no-permission": {},
		"Bearer labels-only":   {"inference.model_label.read"},
		"Bearer provider-read": {"inference.provider.read"},
	}
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			http.NotFound(w, r)
			return
		}
		permissions, exists := tokenPermissions[r.Header.Get("Authorization")]
		if !exists {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authContext := authtest.NewTenantUserAuthContext("7", "9", permissions)
		if len(permissions) == 0 {
			authContext.Authorization.RoleAssignments = authContext.Authorization.RoleAssignments[:0]
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(authContext); err != nil {
			t.Errorf("encode AuthContext: %v", err)
		}
	}))
	defer authServer.Close()

	db, err := gorm.Open(sqlite.Open("file:inference_model_label_route?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS inference").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE inference.provider_connections (id TEXT PRIMARY KEY, scope_type TEXT, tenant_id INTEGER, allow_all_tenants INTEGER)`,
		`CREATE TABLE inference.model_deployments (id TEXT PRIMARY KEY, provider_connection_id TEXT, upstream_model TEXT)`,
		`CREATE TABLE inference.model_profiles (id TEXT PRIMARY KEY, name TEXT, scope_type TEXT, tenant_id INTEGER, model_deployment_id TEXT, created_at DATETIME)`,
		`INSERT INTO inference.provider_connections VALUES ('provider-7', 'tenant', 7, 0), ('provider-8', 'tenant', 8, 0)`,
		`INSERT INTO inference.model_deployments VALUES ('deployment-7', 'provider-7', 'model-7'), ('deployment-8', 'provider-8', 'model-8')`,
		`INSERT INTO inference.model_profiles VALUES ('profile-7', 'visible', 'tenant', 7, 'deployment-7', CURRENT_TIMESTAMP), ('profile-8', 'hidden', 'tenant', 8, 'deployment-8', CURRENT_TIMESTAMP)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}

	control := service.NewControlPlane(repository.NewStore(db), nil)
	router := SetupRouter(&config.Config{SystemURL: authServer.URL}, NewHandler(control, nil), modulelifecycle.NewStandalone("inference"))
	request := func(method, path, token string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		httpRequest := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		if token != "" {
			httpRequest.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(response, httpRequest)
		return response
	}

	for _, test := range []struct {
		name, method, path, token string
		want                      int
	}{
		{"unauthenticated", http.MethodGet, "/api/v1/inference/model-labels", "", http.StatusUnauthorized},
		{"zero permission", http.MethodGet, "/api/v1/inference/model-labels", "no-permission", http.StatusForbidden},
		{"management read cannot replace label read", http.MethodGet, "/api/v1/inference/model-labels", "provider-read", http.StatusForbidden},
		{"label read cannot list management resources", http.MethodGet, "/api/v1/inference/provider-templates", "labels-only", http.StatusForbidden},
		{"label read cannot create management resources", http.MethodPost, "/api/v1/inference/provider-connections", "labels-only", http.StatusForbidden},
		{"management read can list provider templates", http.MethodGet, "/api/v1/inference/provider-templates", "provider-read", http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := request(test.method, test.path, test.token)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}

	response := request(http.MethodGet, "/api/v1/inference/model-labels", "labels-only")
	if response.Code != http.StatusOK {
		t.Fatalf("model labels status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var labels []service.ModelLabel
	if err := json.Unmarshal(response.Body.Bytes(), &labels); err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || labels[0].ID != "profile-7" || labels[0].Name != "visible" || labels[0].UpstreamModel != "model-7" {
		t.Fatalf("tenant model labels = %+v, want only profile-7", labels)
	}
	if strings.Contains(response.Body.String(), "profile-8") || strings.Contains(response.Body.String(), "provider_connection_id") {
		t.Fatalf("model label projection leaked hidden or management fields: %s", response.Body.String())
	}
}
