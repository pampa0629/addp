package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestEntryUpdateCuratesCompleteAggregateAtomically(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, component := createEditableCatalogEntry(t, db, 7)
	standard := &fakeStandardReferenceResolver{}
	system := &fakeSystemReferenceResolver{}
	service := NewEntryService(db, standard, system)
	name := "Orders"
	description := "Canonical customer orders"

	result, err := service.Update(context.Background(), 7, entry.ID, UpdateEntryInput{
		Version: entry.Version, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityDepartment,
		Domains:     []DomainLinkInput{{ID: 10, Role: models.SemanticRolePrimary}, {ID: 11, Role: models.SemanticRoleSecondary}},
		GlossaryIDs: []int64{20},
		Responsibilities: []ResponsibilityInput{
			{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30},
			{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40},
			{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
		},
		ComponentElements: []ComponentElementInput{{ComponentID: component.ID, ElementID: 50}},
	}, UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Version != 2 || result.GovernanceStatus != models.GovernanceStatusCurated || result.Visibility != models.VisibilityDepartment {
		t.Fatalf("entry = %#v", result.Entry)
	}
	if len(result.SemanticLinks) != 3 || len(result.Responsibilities) != 3 || len(result.ComponentElements) != 1 {
		t.Fatalf("aggregate counts = semantic:%d responsibility:%d component:%d", len(result.SemanticLinks), len(result.Responsibilities), len(result.ComponentElements))
	}
	if result.SemanticLinks[0].ObservedVersion != 7 || result.Responsibilities[0].ObservedSnapshot["name"] == "" {
		t.Fatalf("observed snapshots = semantic:%#v responsibility:%#v", result.SemanticLinks[0], result.Responsibilities[0])
	}
	var auditCount, projectionCount int64
	if err := db.Model(&models.AuditEvent{}).Where("catalog_entry_id = ? AND event_type = ?", entry.ID, "catalog.entry.updated").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProjectionTask{}).Where("catalog_entry_id = ? AND projection = ?", entry.ID, "search").Count(&projectionCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 || projectionCount != 1 {
		t.Fatalf("audit/projection = %d/%d", auditCount, projectionCount)
	}
	if standard.calls != 1 || system.calls != 1 {
		t.Fatalf("resolver calls = standard:%d system:%d", standard.calls, system.calls)
	}
}

func TestEntryUpdateDeprecatesWithRecommendedSuccessor(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	successor, _ := createEditableCatalogEntry(t, db, 7)
	name, description := "Legacy orders", "Legacy order facts"
	successorName := "Current orders"
	for id, currentName := range map[uuid.UUID]string{entry.ID: name, successor.ID: successorName} {
		if err := db.Model(&models.Entry{}).Where("id = ?", id).Updates(map[string]any{
			"business_name": currentName, "business_description": description,
			"governance_status": models.GovernanceStatusCurated, "visibility": models.VisibilityTenant,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	reason := "Replaced by the current order dataset"
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	result, err := service.Update(context.Background(), 7, entry.ID, governedUpdateInput(
		1, name, description, models.GovernanceStatusDeprecated, &reason, &successor.ID,
	), UpdateEntryAuthorization{CanDeprecate: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.GovernanceStatus != models.GovernanceStatusDeprecated || result.RecommendedSuccessorEntryID == nil ||
		*result.RecommendedSuccessorEntryID != successor.ID || result.RecommendedSuccessor == nil ||
		result.RecommendedSuccessor.ID != successor.ID || result.RecommendedSuccessor.DisplayName != successorName {
		t.Fatalf("result = %#v", result)
	}
	var audit models.AuditEvent
	if err := db.Where("catalog_entry_id = ? AND event_type = ?", entry.ID, "catalog.entry.updated").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.Details["recommended_successor_entry_id"] != successor.ID.String() || audit.Details["deprecation_reason"] != reason {
		t.Fatalf("audit details = %#v", audit.Details)
	}
}

func TestEntryUpdateRejectsInvalidRecommendedSuccessorsWithoutSideEffects(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, *gorm.DB, models.Entry) uuid.UUID
	}{
		{name: "same entry", prepare: func(_ *testing.T, _ *gorm.DB, entry models.Entry) uuid.UUID { return entry.ID }},
		{name: "other tenant", prepare: func(t *testing.T, db *gorm.DB, _ models.Entry) uuid.UUID {
			candidate, _ := createEditableCatalogEntry(t, db, 8)
			makeSuccessorEligible(t, db, candidate.ID)
			return candidate.ID
		}},
		{name: "not curated", prepare: func(t *testing.T, db *gorm.DB, _ models.Entry) uuid.UUID {
			candidate, _ := createEditableCatalogEntry(t, db, 7)
			return candidate.ID
		}},
		{name: "missing source", prepare: func(t *testing.T, db *gorm.DB, _ models.Entry) uuid.UUID {
			candidate, _ := createEditableCatalogEntry(t, db, 7)
			makeSuccessorEligible(t, db, candidate.ID)
			if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", candidate.ID).Update("source_status", models.SourceStatusMissing).Error; err != nil {
				t.Fatal(err)
			}
			return candidate.ID
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := openCatalogServiceTestDB(t)
			entry, _ := createEditableCatalogEntry(t, db, 7)
			makeSuccessorEligible(t, db, entry.ID)
			candidateID := test.prepare(t, db, entry)
			name, description, reason := "Legacy orders", "Legacy order facts", "Retired"
			service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
			_, err := service.Update(context.Background(), 7, entry.ID, governedUpdateInput(
				1, name, description, models.GovernanceStatusDeprecated, &reason, &candidateID,
			), UpdateEntryAuthorization{CanDeprecate: true}, UpdateEntryActor{Type: "user", ID: "99"})
			if !errors.Is(err, ErrInvalidRecommendedSuccessor) {
				t.Fatalf("Update() error = %v", err)
			}
			var reloaded models.Entry
			if err := db.First(&reloaded, "id = ?", entry.ID).Error; err != nil {
				t.Fatal(err)
			}
			if reloaded.Version != 1 || reloaded.GovernanceStatus != models.GovernanceStatusCurated || reloaded.RecommendedSuccessorEntryID != nil {
				t.Fatalf("entry changed after rejected successor: %#v", reloaded)
			}
		})
	}
}

func TestEntryUpdateRequiresDeprecationPermissionToChangeSuccessor(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	successor, _ := createEditableCatalogEntry(t, db, 7)
	makeSuccessorEligible(t, db, successor.ID)
	if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Updates(map[string]any{
		"governance_status":              models.GovernanceStatusDeprecated,
		"visibility":                     models.VisibilityTenant,
		"recommended_successor_entry_id": successor.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	name, description := "Legacy orders", "Legacy order facts"
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	_, err := service.Update(context.Background(), 7, entry.ID, governedUpdateInput(
		1, name, description, models.GovernanceStatusDeprecated, nil, nil,
	), UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"})
	if !errors.Is(err, ErrDeprecationPermissionRequired) {
		t.Fatalf("Update() error = %v", err)
	}
}

func makeSuccessorEligible(t *testing.T, db *gorm.DB, id uuid.UUID) {
	t.Helper()
	name, description := "Current orders", "Current order facts"
	if err := db.Model(&models.Entry{}).Where("id = ?", id).Updates(map[string]any{
		"business_name": name, "business_description": description,
		"governance_status": models.GovernanceStatusCurated, "visibility": models.VisibilityTenant,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func governedUpdateInput(version int64, name, description, status string, reason *string, successorID *uuid.UUID) UpdateEntryInput {
	return UpdateEntryInput{
		Version: version, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: status, Visibility: models.VisibilityTenant,
		Domains: []DomainLinkInput{{ID: 10, Role: models.SemanticRolePrimary}},
		Responsibilities: []ResponsibilityInput{
			{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30},
			{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40},
			{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
		},
		DeprecationReason: reason, RecommendedSuccessorEntryID: successorID,
	}
}

func TestModelEntryUsesOwnerPrimaryDomainWithoutCatalogCopy(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry := createModelCatalogEntry(t, db, 7, "30")
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	name, description := "Orders", "Logical order model"
	input := UpdateEntryInput{
		Version: 1, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityTenant,
		Responsibilities: []ResponsibilityInput{
			{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30},
			{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40},
			{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
		},
	}
	result, err := service.Update(context.Background(), 7, entry.ID, input, UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatal(err)
	}
	if result.GovernanceStatus != models.GovernanceStatusCurated || len(result.SemanticLinks) != 0 {
		t.Fatalf("result = %#v", result)
	}
	input.Version = result.Version
	input.Domains = []DomainLinkInput{{ID: 30, Role: models.SemanticRolePrimary}}
	if _, err := service.Update(context.Background(), 7, entry.ID, input, UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"}); !errors.Is(err, ErrInvalidEntryUpdate) {
		t.Fatalf("Catalog primary Domain copy accepted: %v", err)
	}
}

func TestModelEntryDetailResolvesCurrentOwnerSummaryDynamically(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry := createModelCatalogEntry(t, db, 7, "30")
	resolver := &fakeProfessionalSourceResolver{module: models.SourceModuleModel, result: ProfessionalSourceResult{
		Found: true, Status: "approved", Version: 4,
		Summary: map[string]any{"name": "Current Orders", "domain_id": "30"}, DetailPath: "/modeling/logical-tables/12",
	}}
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{}).WithProfessionalSourceResolvers(resolver)
	result, err := service.Get(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 || result.SourceResolution == nil || result.SourceResolution.Status != "current" ||
		result.SourceResolution.OwnerVersion != 4 || result.SourceResolution.Summary["name"] != "Current Orders" {
		t.Fatalf("resolution = %#v", result.SourceResolution)
	}
}

func TestModelEntryDetailLabelsUnavailableOwnerProjection(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry := createModelCatalogEntry(t, db, 7, "30")
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})

	result, err := service.Get(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceResolution == nil || result.SourceResolution.Status != "unavailable" ||
		result.SourceResolution.Summary["domain_id"] != "30" {
		t.Fatalf("resolution = %#v", result.SourceResolution)
	}
}

func TestMetaDataItemDetailResolvesQualitySummaryDynamically(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ? AND is_current = ?", entry.ID, true).Update("observed_snapshot", commonModels.JSONMap{
		"name": "orders", "item_type": "table", "engine_id": float64(9), "schema_name": "public", "table_name": "orders",
	}).Error; err != nil {
		t.Fatal(err)
	}
	score := 98.5
	resolver := &fakeQualitySummaryResolver{result: commonClient.QualityCatalogSummaryResolution{
		Reference:  commonClient.QualityCatalogSummaryReference{EngineID: 9, SchemaName: "public", TableName: "orders"},
		Configured: true, CheckTaskID: 31, LastExecutionID: "execution-1", LastExecutionStatus: "success", QualityScore: &score, OpenIssueCount: 2,
	}}
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{}).WithQualitySummaryResolver(resolver)
	result, err := service.Get(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 || result.QualitySummary == nil || result.QualitySummary.Status != "current" || result.QualitySummary.QualityScore == nil || *result.QualitySummary.QualityScore != 98.5 || result.QualitySummary.OpenIssueCount != 2 {
		t.Fatalf("quality summary = %#v", result.QualitySummary)
	}
}

func TestStandardMetricUsesOwnerDomainAndDynamicCurrentSummary(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry := createMetricCatalogEntry(t, db, 7, "41")
	resolver := &fakeProfessionalSourceResolver{module: models.SourceModuleStandard, result: ProfessionalSourceResult{
		Found: true, Status: "approved", Version: 5,
		Summary: map[string]any{"name": "Current order amount", "domain_id": "41", "metric_type": "atomic"}, DetailPath: "/standard/metrics/21",
	}}
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{}).WithProfessionalSourceResolvers(resolver)
	name, description := "Order amount", "Canonical order amount metric"
	responsibilities := []ResponsibilityInput{
		{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30},
		{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40},
		{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
	}
	result, err := service.Update(context.Background(), 7, entry.ID, UpdateEntryInput{
		Version: 1, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityTenant,
		Responsibilities: responsibilities,
	}, UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SemanticLinks) != 0 || result.SourceResolution == nil || result.SourceResolution.Status != "current" || result.SourceResolution.OwnerVersion != 5 {
		t.Fatalf("result = %#v", result)
	}
	input := UpdateEntryInput{Version: result.Version, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityTenant,
		Domains: []DomainLinkInput{{ID: 41, Role: models.SemanticRolePrimary}}, Responsibilities: responsibilities}
	if _, err := service.Update(context.Background(), 7, entry.ID, input, UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"}); !errors.Is(err, ErrInvalidEntryUpdate) {
		t.Fatalf("Catalog Metric primary Domain copy accepted: %v", err)
	}
}

func TestServiceQueryServiceUsesDynamicFactsAndCatalogOwnedPrimaryDomain(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	now := time.Now().UTC()
	entry := models.Entry{ID: uuid.New(), TenantID: 7, EntryType: models.EntryTypeDataService, EntryStatus: models.EntryStatusActive,
		GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	binding := models.SourceBinding{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, SourceModule: models.SourceModuleService,
		SourceType: models.SourceTypeQueryService, SourceIdentity: "31", SourceStatus: models.SourceStatusActive,
		SourceVersion: "00000000000000000001", IsCurrent: true, BoundAt: now,
		ObservedSnapshot: map[string]any{"name": "Observed orders", "service_status": "active"}, ObservedAt: now}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	resolver := &fakeProfessionalSourceResolver{module: models.SourceModuleService, result: ProfessionalSourceResult{
		Found: true, Status: "inactive", Version: 5,
		Summary: map[string]any{"name": "Current orders", "service_status": "inactive", "config_type": "sql"}, DetailPath: "/service/published-services/31",
	}}
	svc := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{}).WithProfessionalSourceResolvers(resolver)
	detail, err := svc.Get(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.SourceResolution == nil || detail.SourceResolution.Status != "current" || detail.SourceResolution.OwnerStatus != "inactive" || detail.SourceResolution.Summary["name"] != "Current orders" {
		t.Fatalf("detail = %#v", detail)
	}
	name, description := "Order service", "Curated order query service"
	responsibilities := []ResponsibilityInput{
		{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30},
		{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40},
		{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
	}
	updated, err := svc.Update(context.Background(), 7, entry.ID, UpdateEntryInput{
		Version: 1, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityTenant,
		Domains: []DomainLinkInput{{ID: 41, Role: models.SemanticRolePrimary}}, Responsibilities: responsibilities,
	}, UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.SemanticLinks) != 1 || updated.SemanticLinks[0].SemanticID != 41 {
		t.Fatalf("updated semantics = %#v", updated.SemanticLinks)
	}
	invalid := UpdateEntryInput{Version: updated.Version, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityTenant,
		Domains: []DomainLinkInput{{ID: 41, Role: models.SemanticRolePrimary}}, Responsibilities: responsibilities,
		ComponentElements: []ComponentElementInput{{ComponentID: uuid.New(), ElementID: 51}},
	}
	if _, err := svc.Update(context.Background(), 7, entry.ID, invalid, UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"}); !errors.Is(err, ErrInvalidEntryUpdate) {
		t.Fatalf("Catalog QueryService component copy accepted: %v", err)
	}
}

func TestEntryUpdateRejectsStaleVersionWithoutReplacingAggregate(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	name := "Existing"
	if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Updates(map[string]interface{}{
		"business_name": name, "version": 2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	_, err := service.Update(context.Background(), 7, entry.ID, UpdateEntryInput{
		Version: 1, GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory,
	}, UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"})
	if !errors.Is(err, ErrEntryVersionConflict) {
		t.Fatalf("Update() error = %v, want version conflict", err)
	}
	var reloaded models.Entry
	if err := db.First(&reloaded, "id = ?", entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != 2 || reloaded.BusinessName == nil || *reloaded.BusinessName != name {
		t.Fatalf("entry changed after conflict: %#v", reloaded)
	}
}

func TestEntryUpdateRequiresReferenceabilityAndConditionalPermissions(t *testing.T) {
	t.Run("reference is not referenceable", func(t *testing.T) {
		db := openCatalogServiceTestDB(t)
		entry, _ := createEditableCatalogEntry(t, db, 7)
		standard := &fakeStandardReferenceResolver{notReferenceable: true}
		service := NewEntryService(db, standard, &fakeSystemReferenceResolver{})
		_, err := service.Update(context.Background(), 7, entry.ID, UpdateEntryInput{
			Version: 1, GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory,
			Domains: []DomainLinkInput{{ID: 10, Role: models.SemanticRoleSecondary}},
		}, UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"})
		if !errors.Is(err, ErrReferenceNotReferenceable) {
			t.Fatalf("Update() error = %v", err)
		}
	})

	t.Run("certification requires permission", func(t *testing.T) {
		db := openCatalogServiceTestDB(t)
		entry, _ := createEditableCatalogEntry(t, db, 7)
		entry.GovernanceStatus = models.GovernanceStatusCurated
		name, description := "Orders", "Orders"
		if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Updates(map[string]interface{}{
			"governance_status": models.GovernanceStatusCurated, "business_name": name, "business_description": description,
		}).Error; err != nil {
			t.Fatal(err)
		}
		service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
		_, err := service.Update(context.Background(), 7, entry.ID, completeGovernedUpdateInput(name, description), UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"})
		if !errors.Is(err, ErrCertificationPermissionRequired) {
			t.Fatalf("Update() error = %v", err)
		}
	})
}

func completeGovernedUpdateInput(name, description string) UpdateEntryInput {
	return UpdateEntryInput{
		Version: 1, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: models.GovernanceStatusCertified, Visibility: models.VisibilityTenant,
		Domains: []DomainLinkInput{{ID: 10, Role: models.SemanticRolePrimary}},
		Responsibilities: []ResponsibilityInput{
			{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30},
			{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40},
			{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
		},
	}
}

func createEditableCatalogEntry(t *testing.T, db *gorm.DB, tenantID int64) (models.Entry, models.Component) {
	t.Helper()
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	entry := models.Entry{
		ID: uuid.New(), TenantID: tenantID, EntryType: models.EntryTypeDataItem,
		EntryStatus: models.EntryStatusActive, GovernanceStatus: models.GovernanceStatusDiscovered,
		Visibility: models.VisibilityInventory, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SourceBinding{
		ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entry.ID,
		SourceModule: models.SourceModuleMeta, SourceType: models.SourceTypeDataItem,
		SourceIdentity: "fingerprint-" + entry.ID.String(), SourceStatus: models.SourceStatusActive,
		SourceVersion: "00000000000000000001", IsCurrent: true, BoundAt: now,
		ObservedSnapshot: map[string]interface{}{"name": "orders"}, ObservedAt: now,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	component := models.Component{
		ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entry.ID, ComponentKey: "id",
		DisplayName: "id", DataType: "int64", ComponentStatus: models.SourceStatusActive,
		Ordinal: 1, ObservedSnapshot: map[string]interface{}{"name": "id"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&component).Error; err != nil {
		t.Fatal(err)
	}
	return entry, component
}

func createModelCatalogEntry(t *testing.T, db *gorm.DB, tenantID int64, domainID string) models.Entry {
	t.Helper()
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	entry := models.Entry{ID: uuid.New(), TenantID: tenantID, EntryType: models.EntryTypeLogicalModel,
		EntryStatus: models.EntryStatusActive, GovernanceStatus: models.GovernanceStatusDiscovered,
		Visibility: models.VisibilityInventory, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SourceBinding{ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entry.ID,
		SourceModule: models.SourceModuleModel, SourceType: models.SourceTypeLogicalTable, SourceIdentity: "12",
		SourceStatus: models.SourceStatusActive, SourceVersion: "00000000000000000001", IsCurrent: true,
		BoundAt: now, ObservedSnapshot: map[string]interface{}{"name": "Orders", "domain_id": domainID}, ObservedAt: now,
		CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	return entry
}

func createMetricCatalogEntry(t *testing.T, db *gorm.DB, tenantID int64, domainID string) models.Entry {
	t.Helper()
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	entry := models.Entry{ID: uuid.New(), TenantID: tenantID, EntryType: models.EntryTypeMetric,
		EntryStatus: models.EntryStatusActive, GovernanceStatus: models.GovernanceStatusDiscovered,
		Visibility: models.VisibilityInventory, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SourceBinding{ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entry.ID,
		SourceModule: models.SourceModuleStandard, SourceType: models.SourceTypeMetric, SourceIdentity: "21",
		SourceStatus: models.SourceStatusActive, SourceVersion: "00000000000000000001", IsCurrent: true,
		BoundAt: now, ObservedSnapshot: map[string]interface{}{"name": "Order amount", "domain_id": domainID}, ObservedAt: now,
		CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	return entry
}

type fakeStandardReferenceResolver struct {
	calls            int
	notReferenceable bool
}

type fakeProfessionalSourceResolver struct {
	module string
	calls  int
	result ProfessionalSourceResult
	err    error
}

type fakeQualitySummaryResolver struct {
	calls  int
	result commonClient.QualityCatalogSummaryResolution
	err    error
}

func (r *fakeQualitySummaryResolver) ResolveCatalogSummaries(_ context.Context, _ int64, _ []commonClient.QualityCatalogSummaryReference) ([]commonClient.QualityCatalogSummaryResolution, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return []commonClient.QualityCatalogSummaryResolution{r.result}, nil
}

func (r *fakeProfessionalSourceResolver) SourceModule() string { return r.module }

func (r *fakeProfessionalSourceResolver) ResolveSources(_ context.Context, _ int64, _ []ProfessionalSourceReference) ([]ProfessionalSourceResult, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return []ProfessionalSourceResult{r.result}, nil
}

func (r *fakeStandardReferenceResolver) ResolveStandardReferences(_ context.Context, _ int64, references []commonClient.StandardReference) ([]commonClient.StandardReferenceResolution, error) {
	r.calls++
	results := make([]commonClient.StandardReferenceResolution, 0, len(references))
	for _, reference := range references {
		results = append(results, commonClient.StandardReferenceResolution{
			ObjectType: reference.ObjectType, ID: reference.ID, Found: true,
			Referenceable: !r.notReferenceable, Name: reference.ObjectType + " name",
			Code: reference.ObjectType + "_code", Status: "approved", LifecycleState: "active", Version: 7,
		})
	}
	return results, nil
}

type fakeSystemReferenceResolver struct{ calls int }

func (r *fakeSystemReferenceResolver) ResolveSystemReferences(_ context.Context, _ int64, references []commonClient.SystemCatalogReference) ([]commonClient.SystemCatalogReferenceResolution, error) {
	r.calls++
	results := make([]commonClient.SystemCatalogReferenceResolution, 0, len(references))
	for _, reference := range references {
		results = append(results, commonClient.SystemCatalogReferenceResolution{
			SubjectType: reference.SubjectType, ID: reference.ID, Found: true, Referenceable: true,
			Name: reference.SubjectType + " name", Code: reference.SubjectType + "_code",
			Status: "active", PrincipalStatus: "active", MembershipStatus: "active",
		})
	}
	return results, nil
}
