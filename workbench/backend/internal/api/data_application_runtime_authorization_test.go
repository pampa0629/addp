package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonAuth "github.com/addp/common/middleware/auth"
	workbenchauthorization "github.com/addp/workbench/internal/authorization"
	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/repository"
	"github.com/addp/workbench/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDataApplicationRuntimeFollowsAssetGrantLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:workbench-runtime-authorization-"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("ATTACH DATABASE ':memory:' AS workbench").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE workbench.data_applications (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, owner_user_id INTEGER NOT NULL,
			name TEXT NOT NULL, description TEXT NOT NULL, draft_snapshot TEXT NOT NULL,
			draft_content_hash TEXT NOT NULL, publication_status TEXT NOT NULL,
			current_revision_number INTEGER, current_revision_hash TEXT NOT NULL, version INTEGER NOT NULL,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE workbench.data_application_revisions (
			id TEXT PRIMARY KEY, application_id TEXT NOT NULL, tenant_id INTEGER NOT NULL,
			revision_number INTEGER NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL,
			snapshot TEXT NOT NULL, content_hash TEXT NOT NULL, published_by INTEGER NOT NULL,
			published_at DATETIME
		)`,
		`CREATE TABLE workbench.resource_access_rules (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, resource_type TEXT NOT NULL,
			resource_id TEXT NOT NULL, subject_type TEXT NOT NULL, subject_id INTEGER NOT NULL,
			permission TEXT NOT NULL, effect TEXT NOT NULL, source_module TEXT NOT NULL,
			source_identity TEXT NOT NULL, expires_at DATETIME, revoked_at DATETIME,
			created_at DATETIME, updated_at DATETIME,
			UNIQUE(tenant_id, source_module, source_identity)
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare Workbench authorization schema: %v", err)
		}
	}

	applicationID := uuid.NewString()
	revisionNumber := int64(1)
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := datatypes.JSON(`{
		"schema_version":"addp.workbench_data_application/v1",
		"page":{"id":"69e435ef-5f56-456e-b495-791b42e74247","title":"Orders","display_mode":"desktop","refresh_interval_seconds":0,"visible_sections":["title","parameters","query_actions"],"placements":[]},
		"components":[],"parameters":[],"parameter_presets":[],"parameter_bindings":[],"selection_bindings":[]
	}`)
	application := models.DataApplication{
		ID: applicationID, TenantID: 7, OwnerUserID: 11, Name: "Orders application", Description: "",
		DraftSnapshot: snapshot, DraftContentHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PublicationStatus: models.PublicationStatusPublished, CurrentRevisionNumber: &revisionNumber,
		CurrentRevisionHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Version: 2,
		CreatedAt: now, UpdatedAt: now,
	}
	revision := models.DataApplicationRevision{
		ID: uuid.NewString(), ApplicationID: applicationID, TenantID: 7, RevisionNumber: revisionNumber,
		Name: application.Name, Description: application.Description, Snapshot: snapshot,
		ContentHash: application.CurrentRevisionHash, PublishedBy: 11, PublishedAt: now,
	}
	if err := db.Create(&application).Error; err != nil {
		t.Fatalf("create published application: %v", err)
	}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatalf("create published revision: %v", err)
	}

	accessRules := repository.NewResourceAccessRuleRepository(db)
	applicationService := service.NewDataApplicationService(repository.NewDataApplicationRepository(db), nil, accessRules)
	grantService := service.NewResourceGrantService(accessRules)
	router := runtimeAuthorizationRouter(t, applicationService)
	runtimePath := "/api/v1/workbench/data_applications/" + applicationID + "/runtime"

	assertRuntimeAccessDenied(t, performRuntimeAuthorizationRequest(router, runtimePath))
	grantRequest := models.AssetResourceGrantRequest{
		ResourceType: models.ResourceTypeDataApplication, ResourceID: applicationID,
		SubjectType: models.ResourceAccessSubjectUser, SubjectID: "91",
		Permission: models.DataApplicationExecutePermission,
	}
	if _, err := grantService.FulfillAssetGrant(7, "73", grantRequest); err != nil {
		t.Fatalf("fulfill Asset grant: %v", err)
	}

	allowed := performRuntimeAuthorizationRequest(router, runtimePath)
	if allowed.Code != http.StatusOK {
		t.Fatalf("granted runtime status=%d body=%s", allowed.Code, allowed.Body.String())
	}
	var runtime models.DataApplicationRuntimeResponse
	if err := json.Unmarshal(allowed.Body.Bytes(), &runtime); err != nil {
		t.Fatalf("decode granted runtime: %v", err)
	}
	if runtime.ID != applicationID || runtime.RevisionNumber != revisionNumber || runtime.Name != application.Name {
		t.Fatalf("granted runtime=%#v", runtime)
	}

	if _, err := grantService.RevokeAssetGrant(7, "73", grantRequest); err != nil {
		t.Fatalf("revoke Asset grant: %v", err)
	}
	assertRuntimeAccessDenied(t, performRuntimeAuthorizationRequest(router, runtimePath))
}

func runtimeAuthorizationRouter(t *testing.T, applications *service.DataApplicationService) *gin.Engine {
	t.Helper()
	authContext := authtest.NewTenantUserAuthContext("7", "91", []string{
		workbenchauthorization.PermissionWorkbenchDataApplicationExecute,
	})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if err := commonAuth.SetAuthContextForGin(c, authContext); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Next()
	})
	handler := NewHandler(applications)
	router.GET(
		"/api/v1/workbench/data_applications/:id/runtime",
		commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchDataApplicationExecute),
		handler.GetDataApplicationRuntime,
	)
	return router
}

func performRuntimeAuthorizationRequest(router http.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertRuntimeAccessDenied(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusForbidden {
		t.Fatalf("denied runtime status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode denied runtime: %v", err)
	}
	if payload["error_code"] != "workbench_data_application_access_denied" {
		t.Fatalf("denied runtime payload=%#v", payload)
	}
}
