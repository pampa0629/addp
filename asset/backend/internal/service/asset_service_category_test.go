package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/asset/internal/models"
	"github.com/addp/asset/internal/search"
	"gorm.io/gorm"
)

func TestBatchCategoryEnforcesTenantOwnedCategory(t *testing.T) {
	db := openAssetAggregateTestDB(t)
	typeDefinition := models.TypeDefinition{TenantID: 0, Name: "Dataset", Code: "dataset", Enabled: true}
	if err := db.Create(&typeDefinition).Error; err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{TenantID: 7, Name: "Orders", TypeID: typeDefinition.ID, Status: "draft", OwnerID: 11, CreatedBy: 11}
	owned := models.AssetCategory{TenantID: 7, Name: "Education"}
	foreign := models.AssetCategory{TenantID: 8, Name: "Finance"}
	for _, value := range []any{&asset, &owned, &foreign} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}

	service := NewAssetService(db, nil, nil)
	if _, err := service.BatchCategory(7, []int64{asset.ID}, &foreign.ID); !errors.Is(err, ErrInvalidAssetAggregate) {
		t.Fatalf("foreign BatchCategory() error = %v", err)
	}
	if _, err := service.BatchCategory(7, []int64{asset.ID, asset.ID}, &owned.ID); !errors.Is(err, ErrInvalidAssetAggregate) {
		t.Fatalf("duplicate BatchCategory() error = %v", err)
	}
	affected, err := service.BatchCategory(7, []int64{asset.ID}, &owned.ID)
	if err != nil || affected != 1 {
		t.Fatalf("BatchCategory() affected=%d error=%v", affected, err)
	}
	if err := db.First(&asset, asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	if asset.CategoryID == nil || *asset.CategoryID != owned.ID {
		t.Fatalf("categorized asset = %#v", asset)
	}
}

func TestAssetKeywordSearchRechecksCurrentFacts(t *testing.T) {
	testAssetKeywordSearchRechecksCurrentFacts(t, openAssetAggregateTestDB(t))
}

// The same stale-index fixture runs with SQLite at T1 and PostgreSQL at T2.
// Only the SDK HTTP endpoint is simulated; this is not a real Meilisearch T4 test.
func testAssetKeywordSearchRechecksCurrentFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	typeDefinition := models.TypeDefinition{TenantID: 7, Name: "Dataset", Code: "search_dataset", Enabled: true}
	otherType := models.TypeDefinition{TenantID: 7, Name: "Application", Code: "search_application", Enabled: true}
	category := models.AssetCategory{TenantID: 7, Name: "Search category"}
	otherCategory := models.AssetCategory{TenantID: 7, Name: "Changed category"}
	for _, value := range []any{&typeDefinition, &otherType, &category, &otherCategory} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	assets := []models.Asset{
		{TenantID: 7, Name: "first", TypeID: typeDefinition.ID, CategoryID: &category.ID, Status: "published", OwnerID: 11, CreatedBy: 11},
		{TenantID: 7, Name: "second", TypeID: typeDefinition.ID, CategoryID: &category.ID, Status: "published", OwnerID: 11, CreatedBy: 11},
		{TenantID: 7, Name: "offlined", TypeID: typeDefinition.ID, CategoryID: &category.ID, Status: "offline", OwnerID: 11, CreatedBy: 11},
		{TenantID: 7, Name: "draft", TypeID: typeDefinition.ID, CategoryID: &category.ID, Status: "draft", OwnerID: 11, CreatedBy: 11},
		{TenantID: 8, Name: "foreign", TypeID: typeDefinition.ID, Status: "published", OwnerID: 11, CreatedBy: 11},
		{TenantID: 7, Name: "changed type", TypeID: otherType.ID, CategoryID: &category.ID, Status: "published", OwnerID: 11, CreatedBy: 11},
		{TenantID: 7, Name: "changed category", TypeID: typeDefinition.ID, CategoryID: &otherCategory.ID, Status: "published", OwnerID: 11, CreatedBy: 11},
		{TenantID: 7, Name: "uncategorized", TypeID: typeDefinition.ID, Status: "published", OwnerID: 11, CreatedBy: 11},
	}
	if err := db.Create(&assets).Error; err != nil {
		t.Fatal(err)
	}
	// Return stale/foreign/deleted candidates and reverse the two valid hits.
	hits := []map[string]int64{{"id": assets[1].ID}, {"id": assets[0].ID}, {"id": assets[2].ID}, {"id": assets[3].ID}, {"id": assets[4].ID}, {"id": assets[5].ID}, {"id": assets[6].ID}, {"id": assets[7].ID}, {"id": 999999}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/indexes/assets":
			fmt.Fprint(w, `{"uid":"assets","primaryKey":"id"}`)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/indexes/assets/settings/"):
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"taskUid":0,"status":"enqueued"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/0":
			fmt.Fprint(w, `{"uid":0,"status":"succeeded"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/indexes/assets/search":
			var request struct {
				Filter string `json:"filter"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if !strings.Contains(request.Filter, "tenant_id = 7") || !strings.Contains(request.Filter, `status = "published"`) {
				t.Errorf("search missing tenant/published filter: %s", request.Filter)
			}
			if err := json.NewEncoder(w).Encode(map[string]any{"hits": hits, "estimatedTotalHits": len(hits)}); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	indexer, err := search.NewIndexer(server.URL, "", "assets")
	if err != nil {
		t.Fatal(err)
	}
	service := NewAssetService(db, nil, indexer)
	for _, test := range []struct {
		name       string
		status     string
		categories []int64
		want       []int64
	}{
		{name: "published category", status: "published", categories: []int64{category.ID}, want: []int64{assets[1].ID, assets[0].ID}},
		{name: "search always published", categories: []int64{category.ID}, want: []int64{assets[1].ID, assets[0].ID}},
		{name: "nonpublished status", status: "offline", categories: []int64{category.ID}, want: []int64{}},
		{name: "uncategorized", status: "published", categories: []int64{-1}, want: []int64{assets[7].ID}},
		{name: "category subtree", status: "published", categories: []int64{category.ID, otherCategory.ID}, want: []int64{assets[1].ID, assets[0].ID, assets[6].ID}},
		{name: "empty category set", status: "published", categories: []int64{}, want: []int64{}},
		{name: "all categories", status: "published", want: []int64{assets[1].ID, assets[0].ID, assets[6].ID, assets[7].ID}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, _, err := service.List(7, &AssetListParams{Page: 1, PageSize: 20, Keyword: "orders", Status: test.status, TypeID: typeDefinition.ID, CategoryIDs: test.categories})
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]int64, 0, len(result))
			for _, asset := range result {
				ids = append(ids, asset.ID)
			}
			if !reflect.DeepEqual(ids, test.want) {
				t.Fatalf("returned candidates %v; want current matching assets %v", ids, test.want)
			}
		})
	}
}
