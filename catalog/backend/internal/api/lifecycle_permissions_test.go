package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	"github.com/addp/catalog/internal/service"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresLifecycleRoutesUseCurrentVisibilityAndIndependentPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("CATALOG_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db = db.Begin()
	if db.Error != nil {
		t.Fatal(db.Error)
	}
	defer db.Rollback()
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := db.Create(&models.Entry{ID: id, TenantID: 7, EntryType: models.EntryTypeDataItem, EntryStatus: models.EntryStatusActive,
		GovernanceStatus: models.GovernanceStatusDeprecated, Visibility: models.VisibilityTenant, Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SourceBinding{ID: uuid.New(), TenantID: 7, CatalogEntryID: id, SourceModule: models.SourceModuleMeta,
		SourceType: models.SourceTypeDataItem, SourceIdentity: uuid.NewString(), SourceStatus: models.SourceStatusActive,
		SourceVersion: "00000000000000000001", IsCurrent: true, ObservedSnapshot: map[string]interface{}{"name": "C"}}).Error; err != nil {
		t.Fatal(err)
	}
	permissions := []string{}
	tenant := "7"
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			t.Errorf("unexpected System request %s", r.URL.Path)
		}
		authContext := authtest.NewTenantUserAuthContext(tenant, "100", permissions)
		if len(permissions) == 0 {
			authContext.Authorization.RoleAssignments = []commonauth.RoleAssignment{}
		}
		if err := commonauth.ValidateAuthContext(authContext); err != nil {
			t.Errorf("invalid test AuthContext: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(authContext)
	}))
	defer system.Close()
	router := SetupRouter(system.URL, modulelifecycle.NewStandalone("catalog"), service.NewEntryService(db, nil, nil), nil, nil, nil, nil, nil)
	request := func(path, body string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/catalog/entries/"+id.String()+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer addp_at_test")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s status=%d want=%d body=%s", path, response.Code, want, response.Body.String())
		}
	}
	withdraw := `{"version":1,"governance_status":"curated","reason":"Restore use"}`
	transfer := `{"version":1,"reason":"Transfer","responsibilities":[{"role":"accountable_department","subject_type":"department","subject_id":"30"},{"role":"business_owner","subject_type":"user","subject_id":"40"},{"role":"data_steward","subject_type":"user","subject_id":"41"}]}`
	for _, granted := range [][]string{{}, {"catalog.entry.read"}, {"catalog.entry.update"}} {
		permissions = granted
		request("/governance", withdraw, http.StatusForbidden)
		request("/responsibilities", transfer, http.StatusForbidden)
	}
	permissions = []string{"catalog.entry.read", "catalog.entry.update"}
	request("/governance", withdraw, http.StatusForbidden)
	// Transfer passes the same read/update guards without deprecation permission.
	// System responsibility validation is unavailable in this fixture, so it fails closed.
	request("/responsibilities", transfer, http.StatusServiceUnavailable)
	request("/responsibilities", strings.TrimSuffix(transfer, "}")+`,"visibility":"tenant"}`, http.StatusBadRequest)
	request("/responsibilities", strings.ReplaceAll(transfer, `"subject_id":"30"`, `"subject_id":30`), http.StatusBadRequest)
	permissions = append(permissions, "catalog.entry.deprecate")
	if err := db.Model(&models.Entry{}).Where("id = ?", id).Update("visibility", models.VisibilityDepartment).Error; err != nil {
		t.Fatal(err)
	}
	request("/governance", withdraw, http.StatusNotFound)
	request("/responsibilities", transfer, http.StatusNotFound)
	permissions = append(permissions, "catalog.inventory.read")
	tenant = "8"
	request("/governance", withdraw, http.StatusNotFound)
	tenant = "7"
	request("/governance", withdraw, http.StatusOK)
	request("/governance", withdraw, http.StatusConflict)
	var audit models.AuditEvent
	if err := db.Where("catalog_entry_id = ? AND event_type = ?", id, "catalog.entry.deprecation_withdrawn").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.ActorID != "100" {
		t.Fatalf("audit actor=%s", audit.ActorID)
	}
}
