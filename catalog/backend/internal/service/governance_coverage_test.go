package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
)

func TestGovernanceCoverageUsesApplicabilityAwareCatalogFacts(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	metaEntry, component := createEditableCatalogEntry(t, db, 7)
	modelEntry := createModelCatalogEntry(t, db, 7, "20")
	now := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	name, description := "Customer orders", "Orders curated for enterprise reuse"
	if err := db.Model(&models.Entry{}).Where("id = ?", metaEntry.ID).Updates(map[string]any{
		"business_name": name, "business_description": description,
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, semantic := range []models.SemanticAssociation{
		{ID: uuid.New(), TenantID: 7, CatalogEntryID: metaEntry.ID, SemanticType: models.SemanticTypeDomain, SemanticID: 20, RelationRole: models.SemanticRolePrimary, ObservedVersion: 1, ObservedSnapshot: commonModels.JSONMap{"name": "Sales"}, VerifiedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), TenantID: 7, CatalogEntryID: metaEntry.ID, SemanticType: models.SemanticTypeGlossary, SemanticID: 21, RelationRole: models.SemanticRoleApplies, ObservedVersion: 1, ObservedSnapshot: commonModels.JSONMap{"name": "Order"}, VerifiedAt: now, CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&semantic).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index, responsibility := range []models.Responsibility{
		{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30},
		{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40},
		{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
	} {
		responsibility.ID = uuid.New()
		responsibility.TenantID = 7
		responsibility.CatalogEntryID = metaEntry.ID
		responsibility.Status = models.ResponsibilityStatusActive
		responsibility.ObservedSnapshot = commonModels.JSONMap{"name": "responsibility"}
		responsibility.VerifiedAt = now
		responsibility.CreatedAt = now.Add(time.Duration(index) * time.Second)
		responsibility.UpdatedAt = responsibility.CreatedAt
		if err := db.Create(&responsibility).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.ComponentElementAssociation{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: metaEntry.ID, ComponentID: component.ID,
		ElementID: 50, ObservedVersion: 1, ObservedSnapshot: commonModels.JSONMap{"name": "Order ID"},
		VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	result, err := NewEntryService(db, nil, nil).GetGovernanceCoverage(context.Background(), 7, EntryAccess{Inventory: true})
	if err != nil {
		t.Fatalf("GetGovernanceCoverage() error = %v", err)
	}
	if result.TotalEntries != 2 || result.View != EntryViewInventory {
		t.Fatalf("coverage scope = %#v", result)
	}
	if result.GovernanceStatuses[0].Status != models.GovernanceStatusDiscovered || result.GovernanceStatuses[0].Count != 2 {
		t.Fatalf("governance statuses = %#v", result.GovernanceStatuses)
	}
	dimensions := make(map[string]GovernanceCoverageDimension, len(result.Dimensions))
	for _, dimension := range result.Dimensions {
		dimensions[dimension.Key] = dimension
	}
	assertCoverageDimension(t, dimensions[CoverageDimensionBusinessDefinition], 1, 2, 50)
	assertCoverageDimension(t, dimensions[CoverageDimensionPrimaryDomain], 2, 2, 100)
	assertCoverageDimension(t, dimensions[CoverageDimensionAccountability], 1, 2, 50)
	assertCoverageDimension(t, dimensions[CoverageDimensionGlossary], 1, 2, 50)
	componentCoverage := dimensions[CoverageDimensionComponentElement]
	assertCoverageDimension(t, componentCoverage, 1, 1, 100)
	if componentCoverage.NotApplicable != 1 || modelEntry.ID == metaEntry.ID {
		t.Fatalf("component coverage = %#v", componentCoverage)
	}
}

func TestGovernanceCoverageRequiresInventoryPermission(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	if _, err := NewEntryService(db, nil, nil).GetGovernanceCoverage(context.Background(), 7, EntryAccess{}); !errors.Is(err, ErrInventoryPermissionRequired) {
		t.Fatalf("GetGovernanceCoverage() error = %v", err)
	}
}

func assertCoverageDimension(t *testing.T, actual GovernanceCoverageDimension, covered, applicable int64, rate float64) {
	t.Helper()
	if actual.Covered != covered || actual.Applicable != applicable || actual.NotCovered != applicable-covered || actual.CoverageRate != rate {
		t.Fatalf("coverage dimension = %#v, want covered=%d applicable=%d rate=%v", actual, covered, applicable, rate)
	}
}

func TestResolveSourceEntriesUsesExactCurrentBindingAndPreservesOrder(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	metaEntry, _ := createEditableCatalogEntry(t, db, 7)
	modelEntry := createModelCatalogEntry(t, db, 7, "20")
	createModelCatalogEntry(t, db, 8, "20")
	metaIdentity := "fingerprint-" + metaEntry.ID.String()
	references := []CatalogSourceReference{
		{SourceModule: models.SourceModuleModel, SourceType: models.SourceTypeLogicalTable, SourceIdentity: "12"},
		{SourceModule: models.SourceModuleMeta, SourceType: models.SourceTypeDataItem, SourceIdentity: metaIdentity},
		{SourceModule: models.SourceModuleStandard, SourceType: models.SourceTypeMetric, SourceIdentity: "99"},
	}
	service := NewEntryService(db, nil, nil)
	governanceOnly, err := service.ResolveSourceEntries(context.Background(), 7, EntryAccess{}, references)
	if err != nil {
		t.Fatalf("ResolveSourceEntries() governance-only error = %v", err)
	}
	if governanceOnly.Results[0].Found || governanceOnly.Results[1].Found {
		t.Fatalf("inventory entries leaked without inventory permission: %#v", governanceOnly.Results)
	}
	if err := db.Model(&models.Entry{}).Where("id = ?", modelEntry.ID).Updates(map[string]any{
		"governance_status": models.GovernanceStatusCurated, "visibility": models.VisibilityTenant,
	}).Error; err != nil {
		t.Fatal(err)
	}
	governanceOnly, err = service.ResolveSourceEntries(context.Background(), 7, EntryAccess{}, references)
	if err != nil || !governanceOnly.Results[0].Found || governanceOnly.Results[1].Found {
		t.Fatalf("governance visibility resolution = %#v, error=%v", governanceOnly, err)
	}

	result, err := service.ResolveSourceEntries(context.Background(), 7, EntryAccess{Inventory: true}, references)
	if err != nil {
		t.Fatalf("ResolveSourceEntries() error = %v", err)
	}
	if len(result.Results) != 3 || !result.Results[0].Found || result.Results[0].Entry == nil || result.Results[0].Entry.ID != modelEntry.ID {
		t.Fatalf("model resolution = %#v", result.Results)
	}
	if !result.Results[1].Found || result.Results[1].Entry == nil || result.Results[1].Entry.ID != metaEntry.ID || result.Results[2].Found {
		t.Fatalf("source resolutions = %#v", result.Results)
	}
}

func TestResolveSourceEntriesRejectsInvalidReferences(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	service := NewEntryService(db, nil, nil)
	for _, reference := range []CatalogSourceReference{
		{SourceModule: models.SourceModuleModel, SourceType: models.SourceTypeMetric, SourceIdentity: "1"},
		{SourceModule: models.SourceModuleModel, SourceType: models.SourceTypeEntity, SourceIdentity: "01"},
		{SourceModule: models.SourceModuleMeta, SourceType: models.SourceTypeDataItem, SourceIdentity: ""},
	} {
		if _, err := service.ResolveSourceEntries(context.Background(), 7, EntryAccess{Inventory: true}, []CatalogSourceReference{reference}); !errors.Is(err, ErrInvalidSourceReference) {
			t.Fatalf("reference %#v error = %v", reference, err)
		}
	}
}
