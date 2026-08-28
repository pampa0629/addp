package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/asset/internal/models"
	assetservice "github.com/addp/asset/internal/service"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestConsumerProjectionFiltersVisibilityAndDerivesCurrentUser(t *testing.T) {
	db := consumerTestDB(t)
	typeDefinition := models.TypeDefinition{TenantID: 0, Name: "Dataset", Code: "dataset", Enabled: true}
	if err := db.Create(&typeDefinition).Error; err != nil {
		t.Fatalf("create type: %v", err)
	}
	published := models.Asset{TenantID: 7, Name: "published", TypeID: typeDefinition.ID, Status: "published", OwnerID: 1, CreatedBy: 1}
	draft := models.Asset{TenantID: 7, Name: "draft", TypeID: typeDefinition.ID, Status: "draft", OwnerID: 1, CreatedBy: 1}
	otherTenant := models.Asset{TenantID: 8, Name: "other", TypeID: typeDefinition.ID, Status: "published", OwnerID: 1, CreatedBy: 1}
	for _, asset := range []*models.Asset{&published, &draft, &otherTenant} {
		if err := db.Create(asset).Error; err != nil {
			t.Fatalf("create asset: %v", err)
		}
	}

	permissions := []string{
		"asset.entry.read", "asset.category.read", "asset.application.create", "asset.application.read",
		"asset.authorization.read", "asset.rating.create", "asset.rating.read", "asset.rating.update",
	}
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer consumer": permissions})
	defer authServer.Close()
	assetSvc := assetservice.NewAssetService(db, nil, nil)
	router := SetupRouter(db, authServer.URL, nil, assetSvc, modulelifecycle.NewStandalone("asset"))

	list := consumerRequest(t, router, http.MethodGet, "/api/v1/asset/consumer/assets", "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "draft") || strings.Contains(list.Body.String(), "other") || !strings.Contains(list.Body.String(), "published") {
		t.Fatalf("consumer list leaked hidden assets: status=%d body=%s", list.Code, list.Body.String())
	}
	detail := consumerRequest(t, router, http.MethodGet, "/api/v1/asset/consumer/assets/"+int64String(draft.ID), "")
	if detail.Code != http.StatusNotFound {
		t.Fatalf("draft detail status=%d, want 404 body=%s", detail.Code, detail.Body.String())
	}

	application := consumerRequest(t, router, http.MethodPost,
		"/api/v1/asset/consumer/assets/"+int64String(published.ID)+"/applications",
		`{"reason":"research","duration_day":7,"applicant_id":999,"user_id":999,"tenant_id":999}`,
	)
	if application.Code != http.StatusCreated {
		t.Fatalf("create application status=%d body=%s", application.Code, application.Body.String())
	}
	var savedApplication models.Application
	if err := db.First(&savedApplication).Error; err != nil {
		t.Fatalf("read application: %v", err)
	}
	if savedApplication.ApplicantID != 9 || savedApplication.TenantID != 7 {
		t.Fatalf("application identity = tenant:%d applicant:%d", savedApplication.TenantID, savedApplication.ApplicantID)
	}

	rating := consumerRequest(t, router, http.MethodPost,
		"/api/v1/asset/consumer/assets/"+int64String(published.ID)+"/ratings",
		`{"score":5,"comment":"useful","user_id":999,"tenant_id":999,"asset_id":999}`,
	)
	if rating.Code != http.StatusOK {
		t.Fatalf("upsert rating status=%d body=%s", rating.Code, rating.Body.String())
	}
	var savedRating models.Rating
	if err := db.First(&savedRating).Error; err != nil {
		t.Fatalf("read rating: %v", err)
	}
	if savedRating.UserID != 9 || savedRating.TenantID != 7 || savedRating.AssetID != published.ID {
		t.Fatalf("rating identity = tenant:%d user:%d asset:%d", savedRating.TenantID, savedRating.UserID, savedRating.AssetID)
	}
	ratings := consumerRequest(t, router, http.MethodGet,
		"/api/v1/asset/consumer/assets/"+int64String(published.ID)+"/ratings", "",
	)
	if ratings.Code != http.StatusOK || !strings.Contains(ratings.Body.String(), `"user_name":"Consumer User"`) {
		t.Fatalf("consumer ratings do not use the IAM display name: status=%d body=%s", ratings.Code, ratings.Body.String())
	}

	applications := consumerRequest(t, router, http.MethodGet, "/api/v1/asset/consumer/applications?applicant_id=999", "")
	if applications.Code != http.StatusOK || !strings.Contains(applications.Body.String(), `"applicant_id":9`) {
		t.Fatalf("my applications not scoped to current user: status=%d body=%s", applications.Code, applications.Body.String())
	}
}

func consumerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS asset").Error; err != nil {
		t.Fatalf("attach asset schema: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS system").Error; err != nil {
		t.Fatalf("attach system schema: %v", err)
	}
	if err := db.Exec("CREATE TABLE system.users (id INTEGER PRIMARY KEY, display_name TEXT NOT NULL)").Error; err != nil {
		t.Fatalf("create system users: %v", err)
	}
	if err := db.Exec("INSERT INTO system.users (id, display_name) VALUES (9, 'Consumer User')").Error; err != nil {
		t.Fatalf("seed system user: %v", err)
	}
	statements := []string{
		`CREATE TABLE asset.type_definitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, code TEXT NOT NULL,
			icon_url TEXT,
			description TEXT, enabled BOOLEAN, sort_order INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE asset.categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, parent_id INTEGER,
			sort_order INTEGER, description TEXT, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE asset.assets (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT,
			type_id INTEGER NOT NULL, category_id INTEGER, tags TEXT, status TEXT, owner_id INTEGER,
			version INTEGER NOT NULL DEFAULT 1, published_at DATETIME,
			created_by INTEGER, updated_by INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE asset.asset_components (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			catalog_entry_id TEXT NOT NULL, role TEXT NOT NULL, sort_order INTEGER NOT NULL,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE asset.asset_ext_fields (
			id INTEGER PRIMARY KEY AUTOINCREMENT, asset_id INTEGER NOT NULL, field_key TEXT, value TEXT,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE asset.applications (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			applicant_id INTEGER NOT NULL, reason TEXT, duration_day INTEGER, status TEXT, reviewer_id INTEGER,
			review_note TEXT, reviewed_at DATETIME, expires_at DATETIME, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE asset.authorizations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			application_id INTEGER NOT NULL, user_id INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
			target_module TEXT NOT NULL DEFAULT '', target_resource_type TEXT NOT NULL DEFAULT '', target_resource_id TEXT NOT NULL DEFAULT '',
			expires_at DATETIME, fulfillment_attempt INTEGER NOT NULL DEFAULT 0, fulfillment_last_error TEXT NOT NULL DEFAULT '',
			next_attempt_at DATETIME, fulfilled_at DATETIME, revoked_at DATETIME, revoked_by INTEGER,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE asset.ratings (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL, score REAL NOT NULL, comment TEXT, tags TEXT, is_handled BOOLEAN,
			created_at DATETIME, updated_at DATETIME, UNIQUE(asset_id, user_id)
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create asset test table: %v", err)
		}
	}
	return db
}

func consumerRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer consumer")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func int64String(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
