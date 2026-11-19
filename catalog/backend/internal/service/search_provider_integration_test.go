package service

import (
	"os"
	"slices"
	"testing"

	"github.com/addp/catalog/internal/models"
	"github.com/google/uuid"
)

type catalogTestIndex interface {
	CatalogSearchProjection
	CatalogSearchResolver
}

func TestPlatformCatalogSearchProviders(t *testing.T) {
	if os.Getenv("ADDP_PLATFORM_SEARCH_TEST") != "1" {
		t.Skip("run make test-common-elasticsearch")
	}
	t.Run("elasticsearch", func(t *testing.T) {
		indexName := "catalog_entries_test_" + uuid.NewString()
		testCatalogSearchProvider(t, func() (catalogTestIndex, error) {
			return NewElasticsearchCatalogIndex(os.Getenv("ELASTICSEARCH_ENDPOINT"), "elastic", os.Getenv("ELASTICSEARCH_PASSWORD"), indexName)
		})
	})
	t.Run("meilisearch", func(t *testing.T) {
		indexName := "catalog_entries_test_" + uuid.NewString()
		testCatalogSearchProvider(t, func() (catalogTestIndex, error) {
			return NewMeilisearchCatalogIndex(os.Getenv("MEILISEARCH_ENDPOINT"), os.Getenv("MEILISEARCH_TEST_KEY"), indexName)
		})
	})
}

func testCatalogSearchProvider(t *testing.T, openIndex func() (catalogTestIndex, error)) {
	t.Helper()
	index, err := openIndex()
	if err != nil {
		t.Fatal(err)
	}
	base := CatalogSearchDocument{
		TenantID: 7, EntryStatus: models.EntryStatusActive, EntryType: models.EntryTypeDataItem, BusinessName: "订单 Order catalog",
		SourceStatus: models.SourceStatusActive, GovernanceStatus: models.GovernanceStatusCertified, Visibility: models.VisibilityTenant,
		SourceEngineID: 9, PrimaryDomainID: "12", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	fixtures := []CatalogSearchDocument{base, base, base, base, base, base}
	fixtures[1].Visibility = models.VisibilityDepartment
	fixtures[1].AccountableDepartmentID = "15"
	fixtures[2].TenantID = 8
	fixtures[3].EntryStatus = models.EntryStatusMerged
	fixtures[4].GovernanceStatus = models.GovernanceStatusDiscovered
	fixtures[5].Visibility = models.VisibilityInventory
	ids := make([]uuid.UUID, len(fixtures))
	for position, fixture := range fixtures {
		ids[position] = uuid.New()
		fixture.ID = ids[position].String()
		if err := index.Upsert(t.Context(), fixture); err != nil {
			t.Fatal(err)
		}
	}
	// A fresh process must be able to reuse the existing index.
	index, err = openIndex()
	if err != nil {
		t.Fatal(err)
	}
	filter := EntryListFilter{
		View: EntryViewGovernance, Search: "订单", EntryType: models.EntryTypeDataItem, SourceEngineID: 9,
		PrimaryDomainID: 12, Page: 1, PageSize: 20,
	}
	search := func(access EntryAccess, criteria EntryListFilter, total int64, want ...uuid.UUID) {
		t.Helper()
		got, count, err := index.SearchCatalogEntries(t.Context(), 7, access, criteria)
		if err != nil || count != total || len(got) != len(want) {
			t.Fatalf("hits %v, total %d, error %v, expected %v and %d", got, count, err, want, total)
		}
		for _, id := range want {
			if !slices.Contains(got, id) {
				t.Fatalf("missing %s from %v", id, got)
			}
		}
	}
	search(EntryAccess{DepartmentIDs: []int64{0, 21}}, filter, 1, ids[0])
	search(EntryAccess{DepartmentIDs: []int64{15}}, filter, 2, ids[0], ids[1])
	search(EntryAccess{Inventory: true}, filter, 3, ids[0], ids[1], ids[5])
	filter.Page, filter.PageSize = 2, 1
	search(EntryAccess{}, filter, 1)
	filter.Page, filter.PageSize = 1, 20
	updated := base
	updated.ID = ids[0].String()
	updated.BusinessName = "Inventory"
	if err := index.Upsert(t.Context(), updated); err != nil {
		t.Fatal(err)
	}
	search(EntryAccess{DepartmentIDs: []int64{15}}, filter, 1, ids[1])
	if err := index.Delete(t.Context(), ids[1].String()); err != nil {
		t.Fatal(err)
	}
	if err := index.Delete(t.Context(), ids[1].String()); err != nil {
		t.Fatal(err)
	}
	search(EntryAccess{DepartmentIDs: []int64{15}}, filter, 0)
}
