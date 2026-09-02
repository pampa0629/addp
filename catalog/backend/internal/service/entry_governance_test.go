package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/catalog/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestEntryGovernanceCertifiesAndFreezesCuration(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, component := createEditableCatalogEntry(t, db, 7)
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	curated := curateCompleteEntry(t, service, entry, component)

	certified, err := service.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: curated.Version, GovernanceStatus: models.GovernanceStatusCertified,
	}, UpdateEntryGovernanceAuthorization{CanCertify: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("certify entry: %v", err)
	}
	if certified.GovernanceStatus != models.GovernanceStatusCertified || certified.Version != curated.Version+1 ||
		certified.BusinessName == nil || *certified.BusinessName != "Orders" || len(certified.SemanticLinks) != 2 ||
		len(certified.Responsibilities) != 3 || len(certified.ComponentElements) != 1 {
		t.Fatalf("certified aggregate = %#v", certified)
	}

	name, description := "Changed orders", "An edit that must be rejected while certified"
	_, err = service.Update(context.Background(), 7, entry.ID, UpdateEntryInput{
		Version: certified.Version, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityTenant,
		Domains: []DomainLinkInput{{ID: 10, Role: models.SemanticRolePrimary}},
		Responsibilities: []ResponsibilityInput{
			{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30},
			{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40},
			{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
		},
	}, UpdateEntryActor{Type: "user", ID: "99"})
	if !errors.Is(err, ErrEntryNotEditable) {
		t.Fatalf("generic Update() error = %v", err)
	}
	assertEntryVersionAndGovernance(t, db, entry.ID, certified.Version, models.GovernanceStatusCertified)
	assertAuditEvent(t, db, entry.ID, "catalog.entry.certified")
}

func TestEntryGovernanceWithdrawsCertificationPreservingCuration(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, component := createEditableCatalogEntry(t, db, 7)
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	curated := curateCompleteEntry(t, service, entry, component)
	certified, err := service.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: curated.Version, GovernanceStatus: models.GovernanceStatusCertified,
	}, UpdateEntryGovernanceAuthorization{CanCertify: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: certified.Version, GovernanceStatus: models.GovernanceStatusCurated,
	}, UpdateEntryGovernanceAuthorization{CanCertify: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if !errors.Is(err, ErrCertificationWithdrawalReasonRequired) {
		t.Fatalf("missing reason error = %v", err)
	}
	reason := "The business definition must be revised"
	_, err = service.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: certified.Version, GovernanceStatus: models.GovernanceStatusCurated, Reason: &reason,
	}, UpdateEntryGovernanceAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"})
	if !errors.Is(err, ErrCertificationPermissionRequired) {
		t.Fatalf("missing permission error = %v", err)
	}

	withdrawn, err := service.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: certified.Version, GovernanceStatus: models.GovernanceStatusCurated, Reason: &reason,
	}, UpdateEntryGovernanceAuthorization{CanCertify: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("withdraw certification: %v", err)
	}
	if withdrawn.GovernanceStatus != models.GovernanceStatusCurated || withdrawn.Version != certified.Version+1 ||
		withdrawn.BusinessName == nil || *withdrawn.BusinessName != "Orders" || len(withdrawn.SemanticLinks) != 2 ||
		len(withdrawn.Responsibilities) != 3 || len(withdrawn.ComponentElements) != 1 {
		t.Fatalf("withdrawn aggregate = %#v", withdrawn)
	}
	var audit models.AuditEvent
	if err := db.Where("catalog_entry_id = ? AND event_type = ?", entry.ID, "catalog.entry.certification_withdrawn").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.Details["reason"] != reason {
		t.Fatalf("withdrawal audit = %#v", audit.Details)
	}
}

func TestEntryGovernanceRejectsIncompleteCertificationWithoutSideEffects(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, component := createEditableCatalogEntry(t, db, 7)
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	curated := curateCompleteEntry(t, service, entry, component)
	if err := db.Where("catalog_entry_id = ? AND role = ?", entry.ID, models.ResponsibilityRoleDataSteward).
		Delete(&models.Responsibility{}).Error; err != nil {
		t.Fatal(err)
	}

	_, err := service.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: curated.Version, GovernanceStatus: models.GovernanceStatusCertified,
	}, UpdateEntryGovernanceAuthorization{CanCertify: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if !errors.Is(err, ErrCertificationRequirementsNotMet) {
		t.Fatalf("certification error = %v", err)
	}
	assertEntryVersionAndGovernance(t, db, entry.ID, curated.Version, models.GovernanceStatusCurated)
	var auditCount int64
	if err := db.Model(&models.AuditEvent{}).Where("catalog_entry_id = ? AND event_type = ?", entry.ID, "catalog.entry.certified").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 0 {
		t.Fatalf("certification audit count = %d", auditCount)
	}
}

func TestEntryGovernanceMaintainsDeprecatedSuccessor(t *testing.T) {
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
	service := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	reason := "The previous successor is no longer appropriate"

	updated, err := service.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: 1, GovernanceStatus: models.GovernanceStatusDeprecated, Reason: &reason,
	}, UpdateEntryGovernanceAuthorization{CanDeprecate: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("clear successor: %v", err)
	}
	if updated.RecommendedSuccessorEntryID != nil || updated.Version != 2 {
		t.Fatalf("updated entry = %#v", updated.Entry)
	}
	assertAuditEvent(t, db, entry.ID, "catalog.entry.deprecation_updated")

	_, err = service.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: updated.Version, GovernanceStatus: models.GovernanceStatusDeprecated, Reason: &reason,
	}, UpdateEntryGovernanceAuthorization{CanDeprecate: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if !errors.Is(err, ErrInvalidGovernanceUpdate) {
		t.Fatalf("no-op maintenance error = %v", err)
	}
}

func curateCompleteEntry(t *testing.T, service *EntryService, entry models.Entry, component models.Component) *EntryDetail {
	t.Helper()
	name, description := "Orders", "Canonical customer orders"
	result, err := service.Update(context.Background(), entry.TenantID, entry.ID, UpdateEntryInput{
		Version: entry.Version, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityTenant,
		Domains:     []DomainLinkInput{{ID: 10, Role: models.SemanticRolePrimary}},
		GlossaryIDs: []int64{20},
		Responsibilities: []ResponsibilityInput{
			{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30},
			{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40},
			{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
		},
		ComponentElements: []ComponentElementInput{{ComponentID: component.ID, ElementID: 50}},
	}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("curate complete entry: %v", err)
	}
	return result
}

func assertEntryVersionAndGovernance(t *testing.T, db *gorm.DB, id uuid.UUID, version int64, governanceStatus string) {
	t.Helper()
	var entry models.Entry
	if err := db.First(&entry, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if entry.Version != version || entry.GovernanceStatus != governanceStatus {
		t.Fatalf("entry version/status = %d/%s, want %d/%s", entry.Version, entry.GovernanceStatus, version, governanceStatus)
	}
}

func assertAuditEvent(t *testing.T, db *gorm.DB, id uuid.UUID, eventType string) {
	t.Helper()
	var count int64
	if err := db.Model(&models.AuditEvent{}).Where("catalog_entry_id = ? AND event_type = ?", id, eventType).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit %s count = %d", eventType, count)
	}
}
