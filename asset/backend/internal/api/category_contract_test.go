package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	assetauthorization "github.com/addp/asset/internal/authorization"
	"github.com/addp/asset/internal/models"
	assetservice "github.com/addp/asset/internal/service"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
)

func TestAssetCategoryFullUpdateMovesHierarchy(t *testing.T) {
	db := consumerTestDB(t)
	root := models.AssetCategory{TenantID: 7, Name: "Government"}
	target := models.AssetCategory{TenantID: 7, Name: "Healthcare"}
	otherTenant := models.AssetCategory{TenantID: 8, Name: "External"}
	for _, category := range []*models.AssetCategory{&root, &target, &otherTenant} {
		if err := db.Create(category).Error; err != nil {
			t.Fatalf("create category %q: %v", category.Name, err)
		}
	}
	child := models.AssetCategory{TenantID: 7, Name: "Education", ParentID: &root.ID}
	if err := db.Create(&child).Error; err != nil {
		t.Fatalf("create child category: %v", err)
	}
	grandchild := models.AssetCategory{TenantID: 7, Name: "Schools", ParentID: &child.ID}
	if err := db.Create(&grandchild).Error; err != nil {
		t.Fatalf("create grandchild category: %v", err)
	}

	permissions := []string{
		assetauthorization.PermissionAssetManagementRead,
		assetauthorization.PermissionAssetCategoryUpdate,
	}
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer consumer": permissions})
	defer authServer.Close()
	router := SetupRouter(db, authServer.URL, nil, assetservice.NewAssetService(db, nil, nil), modulelifecycle.NewStandalone("asset"))

	incompleteRequests := []string{
		`{"version":1,"name":"Education","description":"education","sort_order":10}`,
		`{"version":1,"name":"Education","parent_id":null,"sort_order":10}`,
		`{"version":1,"name":"Education","parent_id":null,"description":"education"}`,
	}
	for _, body := range incompleteRequests {
		response := consumerRequest(t, router, http.MethodPut, "/api/v1/asset/categories/"+int64String(child.ID), body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("incomplete update status=%d, want 400 body=%s", response.Code, response.Body.String())
		}
	}

	move := consumerRequest(t, router, http.MethodPut, "/api/v1/asset/categories/"+int64String(child.ID),
		`{"version":1,"name":"Education","parent_id":`+int64String(target.ID)+`,"description":"education","sort_order":10}`)
	if move.Code != http.StatusOK {
		t.Fatalf("move category status=%d body=%s", move.Code, move.Body.String())
	}
	var moved models.AssetCategory
	if err := json.Unmarshal(move.Body.Bytes(), &moved); err != nil {
		t.Fatalf("decode moved category: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != target.ID || moved.Version != 2 || moved.SortOrder != 10 {
		t.Fatalf("moved category = %#v", moved)
	}

	stale := consumerRequest(t, router, http.MethodPut, "/api/v1/asset/categories/"+int64String(child.ID),
		`{"version":1,"name":"Education","parent_id":null,"description":"education","sort_order":10}`)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"error_code":"asset_category_version_conflict"`) {
		t.Fatalf("stale update status=%d, want 409 conflict body=%s", stale.Code, stale.Body.String())
	}

	cycle := consumerRequest(t, router, http.MethodPut, "/api/v1/asset/categories/"+int64String(target.ID),
		`{"version":1,"name":"Healthcare","parent_id":`+int64String(grandchild.ID)+`,"description":"","sort_order":0}`)
	if cycle.Code != http.StatusBadRequest {
		t.Fatalf("cycle update status=%d, want 400 body=%s", cycle.Code, cycle.Body.String())
	}

	crossTenant := consumerRequest(t, router, http.MethodPut, "/api/v1/asset/categories/"+int64String(target.ID),
		`{"version":1,"name":"Healthcare","parent_id":`+int64String(otherTenant.ID)+`,"description":"","sort_order":0}`)
	if crossTenant.Code != http.StatusBadRequest {
		t.Fatalf("cross-tenant update status=%d, want 400 body=%s", crossTenant.Code, crossTenant.Body.String())
	}

	moveToRoot := consumerRequest(t, router, http.MethodPut, "/api/v1/asset/categories/"+int64String(child.ID),
		`{"version":2,"name":"Education","parent_id":null,"description":"education","sort_order":10}`)
	if moveToRoot.Code != http.StatusOK {
		t.Fatalf("move category to root status=%d body=%s", moveToRoot.Code, moveToRoot.Body.String())
	}
	var rooted models.AssetCategory
	if err := json.Unmarshal(moveToRoot.Body.Bytes(), &rooted); err != nil {
		t.Fatalf("decode root category: %v", err)
	}
	if rooted.ParentID != nil || rooted.Version != 3 {
		t.Fatalf("root category = %#v", rooted)
	}
}
