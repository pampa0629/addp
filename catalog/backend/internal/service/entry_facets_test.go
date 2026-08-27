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

func TestEntryListSeparatesDefaultGovernanceAndExplicitInventoryViews(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	discovered, _ := createEditableCatalogEntry(t, db, 7)
	curated, _ := createEditableCatalogEntry(t, db, 7)
	if err := db.Model(&models.Entry{}).Where("id = ?", curated.ID).Updates(map[string]any{
		"governance_status": models.GovernanceStatusCurated,
		"visibility":        models.VisibilityTenant,
	}).Error; err != nil {
		t.Fatal(err)
	}

	service := NewEntryService(db, nil, nil)
	governance, err := service.List(context.Background(), 7, EntryAccess{Inventory: true}, EntryListFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list default governance view: %v", err)
	}
	if len(governance.Data) != 1 || governance.Data[0].ID != curated.ID {
		t.Fatalf("governance result = %#v", governance.Data)
	}
	inventory, err := service.List(context.Background(), 7, EntryAccess{Inventory: true}, EntryListFilter{View: EntryViewInventory, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list inventory view: %v", err)
	}
	if len(inventory.Data) != 2 || inventory.Total != 2 {
		t.Fatalf("inventory result = %#v", inventory)
	}
	if _, err := service.List(context.Background(), 7, EntryAccess{}, EntryListFilter{View: EntryViewInventory, Page: 1, PageSize: 20}); !errors.Is(err, ErrInventoryPermissionRequired) {
		t.Fatalf("inventory without permission error = %v", err)
	}
	if discovered.ID == curated.ID {
		t.Fatal("test entries unexpectedly share an ID")
	}
}

type fakeEngineReferenceResolver struct {
	err error
}

func (r *fakeEngineReferenceResolver) ResolveEngineReferences(_ context.Context, _ int64, ids []int64) ([]EngineReferenceResolution, error) {
	if r.err != nil {
		return nil, r.err
	}
	results := make([]EngineReferenceResolution, 0, len(ids))
	for _, id := range ids {
		results = append(results, EngineReferenceResolution{
			ID: id, Found: true, Referenceable: true, Name: "Production PostgreSQL",
			EngineType: "postgresql", LifecycleState: "active",
		})
	}
	return results, nil
}

func TestEntryFacetsResolveOnlyReferencesUsedByVisibleView(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	now := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Updates(map[string]any{
		"governance_status": models.GovernanceStatusCurated,
		"visibility":        models.VisibilityTenant,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", entry.ID).Update(
		"observed_snapshot", commonModels.JSONMap{"name": "orders", "engine_id": 14},
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SemanticAssociation{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, SemanticType: models.SemanticTypeDomain,
		SemanticID: 10, RelationRole: models.SemanticRolePrimary, ObservedVersion: 1,
		ObservedSnapshot: commonModels.JSONMap{"name": "Sales"}, VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Responsibility{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, Role: models.ResponsibilityRoleAccountableDepartment,
		SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30, Status: models.ResponsibilityStatusActive,
		ObservedSnapshot: commonModels.JSONMap{"name": "Data Office"}, VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{}).
		WithEngineReferenceResolver(&fakeEngineReferenceResolver{})
	result, err := service.ListFacets(context.Background(), 7, EntryAccess{Inventory: true}, EntryFacetFilter{View: EntryViewGovernance})
	if err != nil {
		t.Fatalf("list facets: %v", err)
	}
	if result.View != EntryViewGovernance || len(result.PrimaryDomains.Options) != 1 || result.PrimaryDomains.Options[0].ID != "10" || result.PrimaryDomains.Options[0].Count != 1 {
		t.Fatalf("domain facet = %#v", result.PrimaryDomains)
	}
	if len(result.AccountableDepartments.Options) != 1 || result.AccountableDepartments.Options[0].ID != "30" || result.AccountableDepartments.Options[0].Count != 1 {
		t.Fatalf("department facet = %#v", result.AccountableDepartments)
	}
	if len(result.SourceEngines.Options) != 1 || result.SourceEngines.Options[0].ID != "14" || result.SourceEngines.Options[0].Name != "Production PostgreSQL" || result.SourceEngines.Options[0].EngineType != "postgresql" {
		t.Fatalf("engine facet = %#v", result.SourceEngines)
	}
	if len(result.EntryTypes) != 1 || result.EntryTypes[0].EntryType != models.EntryTypeDataItem || result.EntryTypes[0].Count != 1 {
		t.Fatalf("entry type facets = %#v", result.EntryTypes)
	}
}

func TestEntryFacetsKeepOtherFacetsWhenEngineOwnerIsUnavailable(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Updates(map[string]any{
		"governance_status": models.GovernanceStatusCurated,
		"visibility":        models.VisibilityTenant,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", entry.ID).Update(
		"observed_snapshot", commonModels.JSONMap{"name": "orders", "engine_id": 14},
	).Error; err != nil {
		t.Fatal(err)
	}
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{}).
		WithEngineReferenceResolver(&fakeEngineReferenceResolver{err: errors.New("System unavailable")})
	result, err := service.ListFacets(context.Background(), 7, EntryAccess{Inventory: true}, EntryFacetFilter{View: EntryViewGovernance})
	if err != nil {
		t.Fatalf("list partial facets: %v", err)
	}
	if result.SourceEngines.Status != FacetStatusUnavailable || len(result.SourceEngines.Options) != 0 ||
		result.PrimaryDomains.Status != FacetStatusCurrent || result.AccountableDepartments.Status != FacetStatusCurrent {
		t.Fatalf("facets = %#v", result)
	}
}

func TestEntryFacetsProvideContextualDomainDepartmentAndTypeNavigation(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	first, _ := createEditableCatalogEntry(t, db, 7)
	second, _ := createEditableCatalogEntry(t, db, 7)
	third, _ := createEditableCatalogEntry(t, db, 7)
	for _, item := range []struct {
		entry      models.Entry
		domainID   int64
		department int64
		entryType  string
	}{
		{entry: first, domainID: 10, department: 30, entryType: models.EntryTypeDataItem},
		{entry: second, domainID: 20, department: 40, entryType: models.EntryTypeLogicalModel},
		{entry: third, domainID: 10, department: 40, entryType: models.EntryTypeMetric},
	} {
		if err := db.Model(&models.Entry{}).Where("id = ?", item.entry.ID).Updates(map[string]any{
			"governance_status": models.GovernanceStatusCurated,
			"visibility":        models.VisibilityTenant,
			"entry_type":        item.entryType,
		}).Error; err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
		if err := db.Create(&models.SemanticAssociation{
			ID: uuid.New(), TenantID: 7, CatalogEntryID: item.entry.ID,
			SemanticType: models.SemanticTypeDomain, SemanticID: item.domainID,
			RelationRole: models.SemanticRolePrimary, ObservedVersion: 1,
			ObservedSnapshot: commonModels.JSONMap{"name": "Domain"}, VerifiedAt: now,
			CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.Responsibility{
			ID: uuid.New(), TenantID: 7, CatalogEntryID: item.entry.ID,
			Role:        models.ResponsibilityRoleAccountableDepartment,
			SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: item.department,
			Status:           models.ResponsibilityStatusActive,
			ObservedSnapshot: commonModels.JSONMap{"name": "Department"}, VerifiedAt: now,
			CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	result, err := service.ListFacets(context.Background(), 7, EntryAccess{Inventory: true}, EntryFacetFilter{
		View: EntryViewGovernance, PrimaryDomainID: 10, DepartmentID: 40,
	})
	if err != nil {
		t.Fatalf("list contextual facets: %v", err)
	}
	if len(result.PrimaryDomains.Options) != 2 {
		t.Fatalf("primary Domain facets = %#v", result.PrimaryDomains)
	}
	if len(result.AccountableDepartments.Options) != 2 || result.AccountableDepartments.Options[0].Count+result.AccountableDepartments.Options[1].Count != 2 {
		t.Fatalf("contextual Department facets = %#v", result.AccountableDepartments)
	}
	if len(result.EntryTypes) != 1 || result.EntryTypes[0].EntryType != models.EntryTypeMetric || result.EntryTypes[0].Count != 1 {
		t.Fatalf("contextual entry type facets = %#v", result.EntryTypes)
	}
}
