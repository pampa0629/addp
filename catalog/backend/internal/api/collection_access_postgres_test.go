package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	"github.com/addp/catalog/internal/service"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresCollectionRoutesSeparateFunctionsFromOrganizationResources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires the disposable Catalog PostgreSQL gate")
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
	entryID := uuid.New()
	for _, value := range []any{
		&models.Entry{ID: entryID, TenantID: 7, EntryType: models.EntryTypeDataItem, EntryStatus: models.EntryStatusActive,
			GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityDepartment, Version: 1},
		&models.SourceBinding{ID: uuid.New(), TenantID: 7, CatalogEntryID: entryID, SourceModule: models.SourceModuleMeta,
			SourceType: models.SourceTypeDataItem, SourceIdentity: uuid.NewString(), SourceStatus: models.SourceStatusActive,
			SourceVersion: "00000000000000000001", IsCurrent: true, ObservedSnapshot: commonModels.JSONMap{"name": "Department resource"}},
		&models.Responsibility{ID: uuid.New(), TenantID: 7, CatalogEntryID: entryID, Role: models.ResponsibilityRoleAccountableDepartment,
			SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 31, Status: "active",
			ObservedSnapshot: commonModels.JSONMap{}, VerifiedAt: time.Now().UTC()},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	permissions := []string{"catalog.entry.read", "catalog.collection.read", "catalog.collection.update"}
	tenant := "7"
	departmentMember, projectMember := true, true
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			t.Errorf("unexpected System request %s", r.URL.Path)
		}
		auth := authtest.NewTenantUserAuthContext(tenant, "40", permissions)
		if departmentMember {
			auth.Organization.Departments = []authorization.DepartmentMembership{{MembershipID: "71", DepartmentID: "31",
				MembershipType: "primary", RelationRole: "member", AncestorIDs: []string{}}}
		}
		if projectMember {
			auth.Organization.ProjectGroups = []authorization.ProjectGroupMembership{{MembershipID: "72", ProjectGroupID: "9", RelationRole: "member"}}
		}
		if err := authorization.ValidateAuthContext(auth); err != nil {
			t.Errorf("invalid fixture AuthContext: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(auth)
	}))
	defer system.Close()
	entries := service.NewEntryService(db, nil, nil)
	collections := service.NewCollectionService(db, entries).WithSystemReferenceResolver(sharingRouteReferences{})
	router := SetupRouter(system.URL, modulelifecycle.NewStandalone("catalog"), entries, nil, nil, collections, nil, nil)
	request := func(method, path, body string, want int) []byte {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/catalog"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer addp_at_test")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s %s status=%d, want=%d, body=%s", method, path, response.Code, want, response.Body.String())
		}
		return response.Body.Bytes()
	}
	assertGroups := func(total int, canUpdate bool) {
		t.Helper()
		var groups service.CollectionProjectGroupList
		if err := json.Unmarshal(request(http.MethodGet, "/me/project-groups", "", http.StatusOK), &groups); err != nil || len(groups.Data) != total {
			t.Fatalf("project group options=%+v, error=%v", groups, err)
		}
		if total == 1 && (groups.Data[0].ProjectGroupID != 9 || !groups.Data[0].CanRead || groups.Data[0].CanUpdate != canUpdate) {
			t.Fatalf("project group resource eligibility=%+v", groups.Data)
		}
	}
	assertGroups(1, true)
	createBody := fmt.Sprintf(`{"project_group_id":"9","name":"Isolated collection","entry_ids":[%q]}`, entryID)
	var created service.CollectionDetail
	if err := json.Unmarshal(request(http.MethodPost, "/collections", createBody, http.StatusCreated), &created); err != nil || len(created.Entries) != 1 {
		t.Fatalf("create result=%+v, error=%v", created, err)
	}
	path := "/collections/" + created.ID.String()
	departmentMember = false
	assertGroups(1, true)
	var detail service.CollectionDetail
	if err := json.Unmarshal(request(http.MethodGet, path, "", http.StatusOK), &detail); err != nil || len(detail.Entries) != 0 {
		t.Fatalf("department exit exposed hidden entry: %+v, %v", detail, err)
	}
	request(http.MethodGet, "/entries/"+entryID.String(), "", http.StatusNotFound)
	projectMember = false
	assertGroups(0, false)
	request(http.MethodGet, path, "", http.StatusNotFound)
	var listed service.CollectionListResult
	if err := json.Unmarshal(request(http.MethodGet, "/collections", "", http.StatusOK), &listed); err != nil || listed.Total != 0 {
		t.Fatalf("nonmember collection list=%+v, %v", listed, err)
	}
	request(http.MethodPut, path, `{"version":1,"name":"Not a member","entry_ids":[]}`, http.StatusNotFound)
	request(http.MethodPost, "/collections", createBody, http.StatusBadRequest)
	departmentMember, projectMember = true, true
	if err := json.Unmarshal(request(http.MethodGet, path, "", http.StatusOK), &detail); err != nil || len(detail.Entries) != 1 {
		t.Fatalf("preserved collection/entry membership=%+v, %v", detail, err)
	}
	permissions = []string{"catalog.entry.read"}
	request(http.MethodGet, path, "", http.StatusForbidden)
	request(http.MethodPost, "/collections", createBody, http.StatusForbidden)
	permissions = []string{"catalog.entry.read", "catalog.collection.read"}
	assertGroups(1, false)
	request(http.MethodGet, path, "", http.StatusOK)
	request(http.MethodPut, path, `{"version":1,"name":"Read only","entry_ids":[]}`, http.StatusForbidden)
	tenant = "8"
	request(http.MethodGet, path, "", http.StatusNotFound)
	var count int64
	if err := db.Model(&models.CollectionEntry{}).Where("collection_id = ?", created.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("authorization observation changed collection entries: count=%d, error=%v", count, err)
	}
	if err := db.Model(&models.CollectionAuditEvent{}).Where("collection_id = ?", created.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("denied operations changed collection audit: count=%d, error=%v", count, err)
	}
}
