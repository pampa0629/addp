package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
)

func TestEntryDetailResolvesPinnedMappingLabelsWithoutBlockingOnStandard(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, component := createEditableCatalogEntry(t, db, 7)
	resolvedRevisionID, missingRevisionID := int64(501), int64(601)
	mappings := []models.StandardMapping{
		{
			ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, ComponentID: component.ID,
			ElementID: 50, ElementRevisionID: &resolvedRevisionID, Source: models.StandardMappingSourceManual,
			ReviewStatus: models.StandardMappingApproved, Version: 2, ProposedByType: "user", ProposedByID: "99",
			Evidence: commonModels.JSONMap{"reason": "verified"},
		},
		{
			ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, ComponentID: component.ID,
			ElementID: 60, ElementRevisionID: &missingRevisionID, Source: models.StandardMappingSourceManual,
			ReviewStatus: models.StandardMappingProposed, Version: 1, ProposedByType: "user", ProposedByID: "99",
			Evidence: commonModels.JSONMap{"reason": "candidate"},
		},
		{
			ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, ComponentID: component.ID,
			ElementID: 70, Source: models.StandardMappingSourceLegacy, ReviewStatus: models.StandardMappingProposed,
			Version: 1, ProposedByType: "migration", ProposedByID: "legacy",
			Evidence: commonModels.JSONMap{"legacy_observed_snapshot": map[string]any{"name": "Legacy name"}},
		},
	}
	for _, mapping := range mappings {
		if err := db.Create(&mapping).Error; err != nil {
			t.Fatal(err)
		}
	}
	resolver := &fakeElementRevisionResolver{snapshots: map[int64]*commonClient.ElementRevisionBinding{
		501: {ElementID: 50, RevisionID: 501, RevisionNo: 3, Status: "withdrawn", Name: "Person ID", Code: "person_id"},
	}}
	svc := NewEntryService(db, nil, nil).WithDataDictionaryResolvers(nil, resolver)
	detail, err := svc.Get(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[uuid.UUID]StandardMappingDetail, len(detail.StandardMappings))
	for _, mapping := range detail.StandardMappings {
		byID[mapping.ID] = mapping
	}
	if len(byID) != 3 || byID[mappings[0].ID].ElementReference != (StandardMappingElementReference{Status: "resolved", Name: "Person ID", Code: "person_id", RevisionNo: 3}) ||
		byID[mappings[1].ID].ElementReference.Status != "missing" || byID[mappings[2].ID].ElementReference.Status != "unfixed" || resolver.tenantID != 7 || len(resolver.revisionIDs) != 2 {
		t.Fatalf("mapping references = %#v, resolver = %#v", byID, resolver)
	}
	encoded, err := json.Marshal(byID[mappings[0].ID])
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	if response["id"] != mappings[0].ID.String() || response["element_revision_id"] != "501" || response["element_reference"].(map[string]any)["revision_no"] != float64(3) {
		t.Fatalf("mapping response = %#v", response)
	}

	resolver.err = errors.New("Standard unavailable")
	detail, err = svc.Get(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID)
	if err != nil {
		t.Fatalf("Catalog detail must remain readable: %v", err)
	}
	for _, mapping := range detail.StandardMappings {
		if mapping.ElementRevisionID != nil && mapping.ElementReference.Status != "unavailable" {
			t.Fatalf("unavailable reference = %#v", mapping.ElementReference)
		}
	}
}

func TestStandardMappingApprovalSupersedesAtomicallyAndUsesOwnVersions(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, component := createEditableCatalogEntry(t, db, 7)
	resolver := &fakeElementRevisionResolver{snapshots: map[int64]*commonClient.ElementRevisionBinding{
		501: {ElementID: 50, RevisionID: 501, Status: "published"},
		601: {ElementID: 60, RevisionID: 601, Status: "published"},
	}}
	svc := NewEntryService(db, nil, nil).WithDataDictionaryResolvers(nil, resolver)
	actor := UpdateEntryActor{Type: "user", ID: "99"}
	first, err := svc.CreateStandardMapping(context.Background(), 7, StandardMappingInput{
		CatalogEntryID: entry.ID, ComponentID: component.ID, ElementID: 50, ElementRevisionID: 501,
		Evidence: commonModels.JSONMap{"reason": "Order key"},
	}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReviewStatus != models.StandardMappingProposed || first.Version != 1 {
		t.Fatalf("first candidate = %#v", first)
	}
	approved, err := svc.ReviewStandardMapping(context.Background(), 7, first.ID, "approve", StandardMappingDecision{Version: 1}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if approved.ReviewStatus != models.StandardMappingApproved || approved.Version != 2 {
		t.Fatalf("first approval = %#v", approved)
	}
	second, err := svc.CreateStandardMapping(context.Background(), 7, StandardMappingInput{
		CatalogEntryID: entry.ID, ComponentID: component.ID, ElementID: 60, ElementRevisionID: 601,
		Evidence: commonModels.JSONMap{"reason": "Corrected definition"},
	}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReviewStandardMapping(context.Background(), 7, second.ID, "approve", StandardMappingDecision{Version: 1}, actor); err != nil {
		t.Fatal(err)
	}
	var old, current models.StandardMapping
	if err := db.First(&old, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&current, "id = ?", second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if old.ReviewStatus != models.StandardMappingWithdrawn || old.Version != 3 || current.ReviewStatus != models.StandardMappingApproved || current.Version != 2 {
		t.Fatalf("replacement = old %#v, current %#v", old, current)
	}
	if _, err := svc.ReviewStandardMapping(context.Background(), 7, second.ID, "withdraw", StandardMappingDecision{Version: 1, Opinion: "stale version"}, actor); !errors.Is(err, ErrStandardMappingVersionConflict) {
		t.Fatalf("stale review = %v", err)
	}
	if _, err := svc.ReviewStandardMapping(context.Background(), 7, second.ID, "withdraw", StandardMappingDecision{Version: 2, Opinion: "No longer valid"}, actor); err != nil {
		t.Fatal(err)
	}
	var approvedCount int64
	if err := db.Model(&models.StandardMapping{}).Where("tenant_id = ? AND component_id = ? AND review_status = ?", 7, component.ID, models.StandardMappingApproved).Count(&approvedCount).Error; err != nil {
		t.Fatal(err)
	}
	if approvedCount != 0 {
		t.Fatalf("approved mappings = %d", approvedCount)
	}
	var auditCount int64
	if err := db.Model(&models.AuditEvent{}).Where("catalog_entry_id = ? AND event_type LIKE ?", entry.ID, "catalog.standard_mapping.%").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 6 {
		t.Fatalf("mapping audit events = %d, want 6", auditCount)
	}
}

func TestStandardMappingRejectsUnpinnedLegacyAndCertifiedWrites(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, component := createEditableCatalogEntry(t, db, 7)
	resolver := &fakeElementRevisionResolver{snapshots: map[int64]*commonClient.ElementRevisionBinding{
		501: {ElementID: 50, RevisionID: 501, Status: "published"},
	}}
	svc := NewEntryService(db, nil, nil).WithDataDictionaryResolvers(nil, resolver)
	actor := UpdateEntryActor{Type: "user", ID: "99"}
	legacy := models.StandardMapping{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, ComponentID: component.ID, ElementID: 50,
		Source: models.StandardMappingSourceLegacy, ReviewStatus: models.StandardMappingProposed, Version: 1,
		ProposedByType: "migration", ProposedByID: "legacy", Evidence: commonModels.JSONMap{},
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReviewStandardMapping(context.Background(), 7, legacy.ID, "approve", StandardMappingDecision{Version: 1}, actor); !errors.Is(err, ErrReferenceNotReferenceable) {
		t.Fatalf("unpinned legacy approved: %v", err)
	}
	if _, err := svc.ReviewStandardMapping(context.Background(), 7, legacy.ID, "reject", StandardMappingDecision{Version: 1}, actor); !errors.Is(err, ErrInvalidStandardMapping) {
		t.Fatalf("reject without opinion = %v", err)
	}
	resolver.validateErr = commonClient.ErrStandardReferenceDeleting
	if _, err := svc.CreateStandardMapping(context.Background(), 7, StandardMappingInput{
		CatalogEntryID: entry.ID, ComponentID: component.ID, ElementID: 50, ElementRevisionID: 501,
	}, actor); !errors.Is(err, ErrReferenceNotReferenceable) {
		t.Fatalf("deleting Standard element accepted: %v", err)
	}
	resolver.validateErr = nil
	if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Updates(map[string]any{"governance_status": models.GovernanceStatusCertified, "visibility": models.VisibilityTenant}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateStandardMapping(context.Background(), 7, StandardMappingInput{
		CatalogEntryID: entry.ID, ComponentID: component.ID, ElementID: 50, ElementRevisionID: 501,
	}, actor); !errors.Is(err, ErrEntryNotEditable) {
		t.Fatalf("certified entry accepted candidate: %v", err)
	}
}

func TestStandardMappingCandidateUpdateDeleteUsesOwnVersion(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, component := createEditableCatalogEntry(t, db, 7)
	resolver := &fakeElementRevisionResolver{snapshots: map[int64]*commonClient.ElementRevisionBinding{
		501: {ElementID: 50, RevisionID: 501, Status: "published"},
	}}
	svc := NewEntryService(db, nil, nil).WithDataDictionaryResolvers(nil, resolver)
	actor := UpdateEntryActor{Type: "user", ID: "99"}
	input := StandardMappingInput{
		CatalogEntryID: entry.ID, ComponentID: component.ID, ElementID: 50, ElementRevisionID: 501,
		Evidence: commonModels.JSONMap{"reason": "original"},
	}
	candidate, err := svc.CreateStandardMapping(context.Background(), 7, input, actor)
	if err != nil {
		t.Fatal(err)
	}
	input.Version = 1
	input.Evidence = commonModels.JSONMap{"reason": "corrected"}
	updated, err := svc.UpdateStandardMapping(context.Background(), 7, candidate.ID, input, actor)
	if err != nil || updated.Version != 2 || updated.Evidence["reason"] != "corrected" {
		t.Fatalf("updated candidate = %#v, error = %v", updated, err)
	}
	if _, err := svc.UpdateStandardMapping(context.Background(), 7, candidate.ID, input, actor); !errors.Is(err, ErrStandardMappingVersionConflict) {
		t.Fatalf("stale candidate update = %v", err)
	}
	if err := svc.DeleteStandardMapping(context.Background(), 7, candidate.ID, 1, actor); !errors.Is(err, ErrStandardMappingVersionConflict) {
		t.Fatalf("stale candidate delete = %v", err)
	}
	if err := svc.DeleteStandardMapping(context.Background(), 7, candidate.ID, 2, actor); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.StandardMapping{}).Where("id = ?", candidate.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("candidate remained after delete: count = %d, error = %v", count, err)
	}
}
