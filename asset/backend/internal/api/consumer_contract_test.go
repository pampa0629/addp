package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	root := models.AssetCategory{TenantID: 7, Name: "Government"}
	if err := db.Create(&root).Error; err != nil {
		t.Fatalf("create root category: %v", err)
	}
	child := models.AssetCategory{TenantID: 7, Name: "Education", ParentID: &root.ID}
	if err := db.Create(&child).Error; err != nil {
		t.Fatalf("create child category: %v", err)
	}
	otherRoot := models.AssetCategory{TenantID: 7, Name: "Healthcare"}
	if err := db.Create(&otherRoot).Error; err != nil {
		t.Fatalf("create other root category: %v", err)
	}
	emptyRoot := models.AssetCategory{TenantID: 7, Name: "Empty"}
	if err := db.Create(&emptyRoot).Error; err != nil {
		t.Fatalf("create empty category: %v", err)
	}
	published := models.Asset{TenantID: 7, Name: "published-root", TypeID: typeDefinition.ID, CategoryID: &root.ID, Status: "published", OwnerID: 1, CreatedBy: 1}
	publishedChild := models.Asset{TenantID: 7, Name: "published-child", TypeID: typeDefinition.ID, CategoryID: &child.ID, Status: "published", OwnerID: 1, CreatedBy: 1}
	publishedOther := models.Asset{TenantID: 7, Name: "published-healthcare", TypeID: typeDefinition.ID, CategoryID: &otherRoot.ID, Status: "published", OwnerID: 1, CreatedBy: 1}
	draft := models.Asset{TenantID: 7, Name: "draft", TypeID: typeDefinition.ID, Status: "draft", OwnerID: 1, CreatedBy: 1}
	otherTenant := models.Asset{TenantID: 8, Name: "other", TypeID: typeDefinition.ID, Status: "published", OwnerID: 1, CreatedBy: 1}
	for _, asset := range []*models.Asset{&published, &publishedChild, &publishedOther, &draft, &otherTenant} {
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
	subtree := consumerRequest(t, router, http.MethodGet, "/api/v1/asset/consumer/assets?category_id="+int64String(root.ID), "")
	if subtree.Code != http.StatusOK || !strings.Contains(subtree.Body.String(), "published-root") || !strings.Contains(subtree.Body.String(), "published-child") || strings.Contains(subtree.Body.String(), "published-healthcare") {
		t.Fatalf("consumer subtree mismatch: status=%d body=%s", subtree.Code, subtree.Body.String())
	}
	invalidCategory := consumerRequest(t, router, http.MethodGet, "/api/v1/asset/consumer/assets?category_id=invalid", "")
	if invalidCategory.Code != http.StatusBadRequest {
		t.Fatalf("invalid category status=%d, want 400 body=%s", invalidCategory.Code, invalidCategory.Body.String())
	}
	missingCategory := consumerRequest(t, router, http.MethodGet, "/api/v1/asset/consumer/assets?category_id=999999", "")
	if missingCategory.Code != http.StatusNotFound {
		t.Fatalf("missing category status=%d, want 404 body=%s", missingCategory.Code, missingCategory.Body.String())
	}
	categories := consumerRequest(t, router, http.MethodGet, "/api/v1/asset/consumer/categories", "")
	if categories.Code != http.StatusOK || !strings.Contains(categories.Body.String(), "Government") || !strings.Contains(categories.Body.String(), "Education") || strings.Contains(categories.Body.String(), "Empty") {
		t.Fatalf("consumer category tree mismatch: status=%d body=%s", categories.Code, categories.Body.String())
	}
	var categoryTree []assetservice.AssetCategoryTreeNode
	if err := json.Unmarshal(categories.Body.Bytes(), &categoryTree); err != nil {
		t.Fatalf("decode consumer category tree: %v", err)
	}
	if len(categoryTree) != 2 || categoryTree[0].ID != root.ID || categoryTree[0].Count != 2 || len(categoryTree[0].Children) != 1 || categoryTree[0].Children[0].Count != 1 {
		t.Fatalf("consumer category subtree counts = %#v", categoryTree)
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

	applicationID := savedApplication.ID
	grant := models.Authorization{TenantID: 7, AssetID: published.ID, ApplicationID: &applicationID, UserID: 9, Status: models.AuthorizationStatusEffective}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatalf("create effective grant: %v", err)
	}
	rating := consumerRequest(t, router, http.MethodPost,
		"/api/v1/asset/consumer/assets/"+int64String(published.ID)+"/ratings",
		`{"score":5,"comment":"useful","user_id":999,"tenant_id":999,"asset_id":999}`,
	)
	if rating.Code != http.StatusCreated {
		t.Fatalf("create rating status=%d body=%s", rating.Code, rating.Body.String())
	}
	var savedRating models.Rating
	if err := db.First(&savedRating).Error; err != nil {
		t.Fatalf("read rating: %v", err)
	}
	if savedRating.UserID != 9 || savedRating.TenantID != 7 || savedRating.AssetID != published.ID {
		t.Fatalf("rating identity = tenant:%d user:%d asset:%d", savedRating.TenantID, savedRating.UserID, savedRating.AssetID)
	}
	newerRating := models.Rating{TenantID: 7, AssetID: published.ID, UserID: 10, Score: 2, CreatedAt: savedRating.CreatedAt.Add(time.Minute)}
	if err := db.Create(&newerRating).Error; err != nil {
		t.Fatalf("create newer rating: %v", err)
	}
	firstPage := consumerRequest(t, router, http.MethodGet,
		"/api/v1/asset/consumer/assets/"+int64String(published.ID)+"/ratings?page_size=1", "",
	)
	if firstPage.Code != http.StatusOK {
		t.Fatalf("list ratings: status=%d body=%s", firstPage.Code, firstPage.Body.String())
	}
	var projection struct {
		Data     []models.Rating `json:"data"`
		MyRating *models.Rating  `json:"my_rating"`
		AvgScore float64         `json:"avg_score"`
		Total    int64           `json:"total"`
		PageSize int             `json:"page_size"`
	}
	if err := json.Unmarshal(firstPage.Body.Bytes(), &projection); err != nil {
		t.Fatalf("decode rating projection: %v", err)
	}
	if len(projection.Data) != 1 || projection.Data[0].UserID != 10 || projection.MyRating == nil || projection.MyRating.ID != savedRating.ID || projection.AvgScore != 3.5 || projection.Total != 2 || projection.PageSize != 1 {
		t.Fatalf("rating projection omitted paginated own rating or average: %#v", projection)
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

func TestConsumerRatingSeparatesCreateUpdateAndChecksEffectiveGrant(t *testing.T) {
	db := consumerTestDB(t)
	asset := models.Asset{TenantID: 7, Name: "published", Status: "published", TypeID: 1, OwnerID: 1, CreatedBy: 1}
	foreign := models.Asset{TenantID: 8, Name: "foreign", Status: "published", TypeID: 1, OwnerID: 1, CreatedBy: 1}
	for _, item := range []*models.Asset{&asset, &foreign} {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("create asset: %v", err)
		}
	}
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer create": {"asset.rating.create"},
		"Bearer update": {"asset.rating.update"},
		"Bearer reader": {"asset.rating.read"},
	})
	defer authServer.Close()
	router := SetupRouter(db, authServer.URL, nil, assetservice.NewAssetService(db, nil, nil), modulelifecycle.NewStandalone("asset"))
	path := "/api/v1/asset/consumer/assets/" + int64String(asset.ID) + "/ratings"
	ownPath := "/api/v1/asset/consumer/assets/" + int64String(asset.ID) + "/my-rating"
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	assertStatus := func(response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("status=%d, want %d; body=%s", response.Code, want, response.Body.String())
		}
	}
	assertStatus(request(http.MethodPost, path, "", `{"score":5}`), http.StatusUnauthorized)
	assertStatus(request(http.MethodPost, path, "reader", `{"score":5}`), http.StatusForbidden)
	assertStatus(request(http.MethodGet, ownPath, "reader", ""), http.StatusForbidden)
	assertStatus(request(http.MethodGet, ownPath, "create", ""), http.StatusForbidden)
	assertStatus(request(http.MethodGet, ownPath, "update", ""), http.StatusOK)
	assertStatus(request(http.MethodPut, path, "create", `{"score":4}`), http.StatusForbidden)
	assertStatus(request(http.MethodPost, path, "create", `{"score":5}`), http.StatusForbidden)
	assertStatus(request(http.MethodPost, "/api/v1/asset/consumer/assets/"+int64String(foreign.ID)+"/ratings", "create", `{"score":5}`), http.StatusNotFound)

	application := models.Application{TenantID: 7, AssetID: asset.ID, ApplicantID: 9, Status: "approved"}
	if err := db.Create(&application).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}
	applicationID := application.ID
	grant := models.Authorization{TenantID: 7, AssetID: asset.ID, ApplicationID: &applicationID, UserID: 9, Status: models.AuthorizationStatusEffective}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatalf("create grant: %v", err)
	}
	assertStatus(request(http.MethodPut, path, "update", `{"score":4}`), http.StatusNotFound)
	assertStatus(request(http.MethodPost, path, "update", `{"score":5}`), http.StatusForbidden)
	assertStatus(request(http.MethodPost, path, "create", `{"score":5,"comment":"first"}`), http.StatusCreated)
	own := request(http.MethodGet, ownPath, "update", "")
	assertStatus(own, http.StatusOK)
	if !strings.Contains(own.Body.String(), `"user_id":9`) || strings.Contains(own.Body.String(), `"user_id":10`) {
		t.Fatalf("own rating projection leaked another user: %s", own.Body.String())
	}
	assertStatus(request(http.MethodPost, path, "create", `{"score":1}`), http.StatusConflict)
	if err := db.Model(&models.Rating{}).Where("asset_id = ? AND user_id = ?", asset.ID, 9).Update("is_handled", true).Error; err != nil {
		t.Fatalf("mark rating handled: %v", err)
	}
	assertStatus(request(http.MethodPut, path, "update", `{"score":4,"comment":"revised"}`), http.StatusOK)
	var saved models.Rating
	if err := db.Where("asset_id = ? AND user_id = ?", asset.ID, 9).First(&saved).Error; err != nil {
		t.Fatalf("read saved rating: %v", err)
	}
	if saved.Score != 4 || saved.Comment != "revised" || !saved.IsHandled {
		t.Fatalf("rating update changed wrong fields: %#v", saved)
	}
	if err := db.Model(&grant).Update("status", models.AuthorizationStatusRevoked).Error; err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	assertStatus(request(http.MethodPut, path, "update", `{"score":2}`), http.StatusForbidden)
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
			sort_order INTEGER, description TEXT, version INTEGER NOT NULL DEFAULT 1, created_at DATETIME, updated_at DATETIME
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
