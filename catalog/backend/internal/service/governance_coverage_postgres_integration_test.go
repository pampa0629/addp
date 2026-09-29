package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresGovernanceCoverageAndSourceResolution(t *testing.T) {
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("CATALOG_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()
	if err := repository.Migrate(tx); err != nil {
		t.Fatalf("migrate Catalog schema: %v", err)
	}

	metaEntry, component := createEditableCatalogEntry(t, tx, 71)
	modelEntry := createModelCatalogEntry(t, tx, 71, "31")
	sharedMetric := createMetricCatalogEntry(t, tx, 71, "")
	secondComponent := models.Component{
		ID: uuid.New(), TenantID: 71, CatalogEntryID: metaEntry.ID, ComponentKey: "activity_name",
		DisplayName: "activity_name", DataType: "string", ComponentStatus: models.SourceStatusActive,
		Ordinal: 2, ObservedSnapshot: commonModels.JSONMap{"name": "activity_name"},
	}
	inactiveComponent := models.Component{
		ID: uuid.New(), TenantID: 71, CatalogEntryID: metaEntry.ID, ComponentKey: "old_name",
		DisplayName: "old_name", DataType: "string", ComponentStatus: models.SourceStatusMissing,
		Ordinal: 3, ObservedSnapshot: commonModels.JSONMap{"name": "old_name"},
	}
	for _, extra := range []*models.Component{&secondComponent, &inactiveComponent} {
		if err := tx.Create(extra).Error; err != nil {
			t.Fatal(err)
		}
	}
	var sharedBinding models.SourceBinding
	if err := tx.Where("catalog_entry_id = ? AND is_current = ?", sharedMetric.ID, true).First(&sharedBinding).Error; err != nil {
		t.Fatal(err)
	}
	sharedBinding.ObservedSnapshot["scope_type"] = "tenant_common"
	if err := tx.Save(&sharedBinding).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	if err := tx.Model(&models.Entry{}).Where("id = ?", metaEntry.ID).Updates(map[string]any{
		"business_name": "Orders", "business_description": "Enterprise order facts",
	}).Error; err != nil {
		t.Fatal(err)
	}
	revisionID := int64(511)
	if err := tx.Create(&models.StandardMapping{
		ID: uuid.New(), TenantID: 71, CatalogEntryID: metaEntry.ID, ComponentID: component.ID,
		ElementID: 51, ElementRevisionID: &revisionID, Source: models.StandardMappingSourceManual,
		ReviewStatus: models.StandardMappingApproved, Version: 2, ProposedByType: "user", ProposedByID: "1",
		Evidence: commonModels.JSONMap{"name": "Order ID"}, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	secondRevisionID := int64(512)
	proposedMapping := models.StandardMapping{
		ID: uuid.New(), TenantID: 71, CatalogEntryID: metaEntry.ID, ComponentID: secondComponent.ID,
		ElementID: 52, ElementRevisionID: &secondRevisionID, Source: models.StandardMappingSourceManual,
		ReviewStatus: models.StandardMappingProposed, Version: 1, ProposedByType: "user", ProposedByID: "1",
		Evidence: commonModels.JSONMap{"name": "Activity name"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&proposedMapping).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewEntryService(tx, nil, nil)
	coverage, err := svc.GetGovernanceCoverage(context.Background(), 71, EntryAccess{Inventory: true})
	if err != nil {
		t.Fatalf("GetGovernanceCoverage() error = %v", err)
	}
	if coverage.TotalEntries != 3 || len(coverage.Dimensions) != 7 || coverage.Dimensions[1].Key != CoverageDimensionPrimaryDomain ||
		coverage.Dimensions[1].Covered != 1 || coverage.Dimensions[1].Applicable != 2 || coverage.Dimensions[1].NotApplicable != 1 {
		t.Fatalf("coverage = %#v", coverage)
	}
	for _, dimension := range coverage.Dimensions {
		if dimension.Key == CoverageDimensionComponentStandardMapping &&
			(dimension.Covered != 0 || dimension.Applicable != 1 || dimension.NotCovered != 1 || dimension.NotApplicable != 2) {
			t.Fatalf("proposed mapping must not cover the second active component: %#v", dimension)
		}
	}
	for _, dimension := range coverage.Dimensions {
		listed, listErr := svc.List(context.Background(), 71, EntryAccess{Inventory: true}, EntryListFilter{
			View: EntryViewInventory, CoverageDimension: dimension.Key, CoverageState: CoverageStateMissing,
			Page: 1, PageSize: 20,
		})
		if listErr != nil {
			t.Fatalf("List(%s missing) error = %v", dimension.Key, listErr)
		}
		if listed.Total != dimension.NotCovered || int64(len(listed.Data)) != dimension.NotCovered {
			t.Fatalf("List(%s missing) = total %d rows %d, coverage not_covered = %d", dimension.Key, listed.Total, len(listed.Data), dimension.NotCovered)
		}
	}
	resolver := &fakeElementRevisionResolver{snapshots: map[int64]*commonClient.ElementRevisionBinding{
		secondRevisionID: {ElementID: 52, RevisionID: secondRevisionID, Status: "published"},
	}}
	approved, err := svc.WithDataDictionaryResolvers(nil, resolver).ReviewStandardMapping(
		context.Background(), 71, proposedMapping.ID, "approve", StandardMappingDecision{Version: 1}, UpdateEntryActor{Type: "user", ID: "1"},
	)
	if err != nil || approved.ReviewStatus != models.StandardMappingApproved {
		t.Fatalf("approve second component mapping = %#v, error = %v", approved, err)
	}
	coverage, err = svc.GetGovernanceCoverage(context.Background(), 71, EntryAccess{Inventory: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, dimension := range coverage.Dimensions {
		if dimension.Key == CoverageDimensionComponentStandardMapping &&
			(dimension.Covered != 1 || dimension.Applicable != 1 || dimension.NotCovered != 0 || dimension.NotApplicable != 2) {
			t.Fatalf("approved mappings must cover every active component: %#v", dimension)
		}
	}
	mappingGap, err := svc.List(context.Background(), 71, EntryAccess{Inventory: true}, EntryListFilter{
		View: EntryViewInventory, CoverageDimension: CoverageDimensionComponentStandardMapping, CoverageState: CoverageStateMissing,
		Page: 1, PageSize: 20,
	})
	if err != nil || mappingGap.Total != 0 {
		t.Fatalf("approved mapping gap = %#v, error = %v", mappingGap, err)
	}
	facets, err := svc.ListFacets(context.Background(), 71, EntryAccess{Inventory: true}, EntryFacetFilter{
		View: EntryViewInventory, PrimaryDomainID: 31,
	})
	if err != nil {
		t.Fatalf("ListFacets(domain 31) error = %v", err)
	}
	if len(facets.EntryTypes) != 1 || facets.EntryTypes[0].EntryType != models.EntryTypeLogicalModel || facets.EntryTypes[0].Count != 1 {
		t.Fatalf("domain 31 entry type facets = %#v", facets.EntryTypes)
	}
	resolved, err := svc.ResolveSourceEntries(context.Background(), 71, EntryAccess{Inventory: true}, []CatalogSourceReference{
		{SourceModule: models.SourceModuleModel, SourceType: models.SourceTypeLogicalTable, SourceIdentity: "12"},
	})
	if err != nil {
		t.Fatalf("ResolveSourceEntries() error = %v", err)
	}
	if len(resolved.Results) != 1 || !resolved.Results[0].Found || resolved.Results[0].Entry == nil || resolved.Results[0].Entry.ID != modelEntry.ID {
		t.Fatalf("source resolution = %#v", resolved)
	}
}
