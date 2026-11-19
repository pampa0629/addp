package search

import (
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestPlatformAssetSearchAgainstElasticsearch(t *testing.T) {
	if os.Getenv("ADDP_PLATFORM_ASSET_SEARCH_TEST") != "1" {
		t.Skip("run the disposable platform-asset Elasticsearch gate")
	}
	index, err := NewElasticsearchIndexer(os.Getenv("ELASTICSEARCH_ENDPOINT"), "elastic",
		os.Getenv("ELASTICSEARCH_PASSWORD"), "asset_published_test_"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	category := int64(17)
	asset := AssetIndexDoc{ID: 1, TenantID: 7, Name: "订单 Orders", TypeCode: "data", CategoryID: &category, Status: "published"}
	uncategorized := asset
	uncategorized.ID, uncategorized.CategoryID = 2, nil
	foreign := asset
	foreign.ID, foreign.TenantID = 3, 8
	offline := asset
	offline.ID, offline.Status = 4, "offline"
	if err := index.ReplaceAssets([]AssetIndexDoc{asset, uncategorized, foreign, offline}); err != nil {
		t.Fatal(err)
	}
	check := func(categories []int64, limit, offset int64, total int64, want ...int64) {
		t.Helper()
		got, err := index.Search(7, "订单", "data", categories, limit, offset)
		if err != nil || got.Total != total || !slices.Equal(got.IDs, want) {
			t.Fatalf("hits=%+v err=%v, expected ids=%v total=%d", got, err, want, total)
		}
	}
	check(nil, 10, 0, 2, 1, 2)
	check(nil, 1, 1, 2, 2)
	check([]int64{17}, 10, 0, 1, 1)
	check([]int64{-1}, 10, 0, 1, 2)
	index.UpdateStatus(1, "offline")
	check(nil, 10, 0, 1, 2)
	index.DeleteAsset(2)
	check(nil, 10, 0, 0)
	index.UpsertAsset(&asset)
	check(nil, 10, 0, 1, 1)
}
