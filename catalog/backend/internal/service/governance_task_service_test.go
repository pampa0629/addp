package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestResponsibilityReconciliationOpensAndResolvesOneGovernanceTask(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	responsibility := models.Responsibility{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID,
		Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40,
		Status: models.ResponsibilityStatusActive, ObservedSnapshot: map[string]interface{}{"name": "Former Owner"},
		VerifiedAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	if err := db.Create(&responsibility).Error; err != nil {
		t.Fatal(err)
	}
	resolver := &governanceSystemResolver{found: true, referenceable: false, name: "Former Owner"}
	service := NewGovernanceTaskService(db, resolver)
	service.now = func() time.Time { return now }

	if err := service.ReconcileTenant(context.Background(), 7); err != nil {
		t.Fatalf("ReconcileTenant() invalidation error = %v", err)
	}
	assertResponsibilityGovernanceState(t, db, responsibility.ID, entry.ID, models.ResponsibilityStatusNeedsTransfer, models.GovernanceTaskStatusOpen, 2, 1)

	service.now = func() time.Time { return now.Add(time.Minute) }
	if err := service.ReconcileTenant(context.Background(), 7); err != nil {
		t.Fatalf("repeat ReconcileTenant() error = %v", err)
	}
	assertResponsibilityGovernanceState(t, db, responsibility.ID, entry.ID, models.ResponsibilityStatusNeedsTransfer, models.GovernanceTaskStatusOpen, 2, 1)

	resolver.referenceable = true
	resolver.name = "Returned Owner"
	service.now = func() time.Time { return now.Add(2 * time.Minute) }
	if err := service.ReconcileTenant(context.Background(), 7); err != nil {
		t.Fatalf("ReconcileTenant() restoration error = %v", err)
	}
	assertResponsibilityGovernanceState(t, db, responsibility.ID, entry.ID, models.ResponsibilityStatusActive, models.GovernanceTaskStatusResolved, 3, 2)

	result, err := service.List(context.Background(), 7, GovernanceTaskListFilter{
		Status: models.GovernanceTaskStatusResolved, Page: 1, PageSize: 20,
	})
	if err != nil || result.Total != 1 || len(result.Data) != 1 || result.Data[0].EntryDisplayName != "orders" ||
		result.Data[0].Resolution == nil || *result.Data[0].Resolution != models.GovernanceTaskResolutionReferenceRestored {
		t.Fatalf("resolved governance task list = %#v error=%v", result, err)
	}
}

func TestEntryUpdateResolvesSupersededResponsibilityTask(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	responsibility := models.Responsibility{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID,
		Role: models.ResponsibilityRoleTechnicalOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41,
		Status: models.ResponsibilityStatusActive, ObservedSnapshot: map[string]interface{}{"name": "Former Maintainer"},
		VerifiedAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	if err := db.Create(&responsibility).Error; err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceTaskService(db, &governanceSystemResolver{found: false})
	governance.now = func() time.Time { return now }
	if err := governance.ReconcileTenant(context.Background(), 7); err != nil {
		t.Fatal(err)
	}

	entries := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	if _, err := entries.Update(context.Background(), 7, entry.ID, UpdateEntryInput{
		Version: 2, GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory,
	}, UpdateEntryAuthorization{}, UpdateEntryActor{Type: "user", ID: "99"}); err != nil {
		t.Fatalf("repair responsibility aggregate: %v", err)
	}
	var task models.GovernanceTask
	if err := db.Where("catalog_entry_id = ?", entry.ID).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != models.GovernanceTaskStatusResolved || task.Resolution == nil ||
		*task.Resolution != models.GovernanceTaskResolutionResponsibilityReplaced {
		t.Fatalf("governance task after aggregate repair = %#v", task)
	}
}

func TestResponsibilityReconciliationRejectsMismatchedResolutionWithoutMutation(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	responsibility := models.Responsibility{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID,
		Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40,
		Status: models.ResponsibilityStatusActive, ObservedSnapshot: map[string]interface{}{"name": "Owner"},
		VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&responsibility).Error; err != nil {
		t.Fatal(err)
	}
	service := NewGovernanceTaskService(db, &governanceSystemResolver{found: true, referenceable: false, mismatch: true})
	if err := service.ReconcileTenant(context.Background(), 7); !errors.Is(err, ErrReferenceValidationUnavailable) {
		t.Fatalf("mismatched resolution error = %v", err)
	}
	assertResponsibilityGovernanceState(t, db, responsibility.ID, entry.ID, models.ResponsibilityStatusActive, "", 1, 0)
}

func assertResponsibilityGovernanceState(
	t *testing.T,
	db *gorm.DB,
	responsibilityID, entryID uuid.UUID,
	responsibilityStatus, taskStatus string,
	entryVersion int64,
	auditCount int64,
) {
	t.Helper()
	var responsibility models.Responsibility
	if err := db.First(&responsibility, "id = ?", responsibilityID).Error; err != nil {
		t.Fatal(err)
	}
	var entry models.Entry
	if err := db.First(&entry, "id = ?", entryID).Error; err != nil {
		t.Fatal(err)
	}
	var taskCount int64
	query := db.Model(&models.GovernanceTask{}).Where("catalog_entry_id = ?", entryID)
	if taskStatus != "" {
		query = query.Where("status = ?", taskStatus)
	}
	if err := query.Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	var actualAuditCount int64
	if err := db.Model(&models.AuditEvent{}).Where("catalog_entry_id = ? AND event_type = ?", entryID, "catalog.responsibility.reference_state_changed").Count(&actualAuditCount).Error; err != nil {
		t.Fatal(err)
	}
	expectedTaskCount := int64(1)
	if taskStatus == "" {
		expectedTaskCount = 0
	}
	if responsibility.Status != responsibilityStatus || taskCount != expectedTaskCount || entry.Version != entryVersion || actualAuditCount != auditCount {
		t.Fatalf("responsibility=%#v task_count=%d entry_version=%d audit_count=%d", responsibility, taskCount, entry.Version, actualAuditCount)
	}
}

type governanceSystemResolver struct {
	found, referenceable, mismatch bool
	name                           string
}

func (r *governanceSystemResolver) ResolveSystemReferences(_ context.Context, _ int64, references []commonClient.SystemCatalogReference) ([]commonClient.SystemCatalogReferenceResolution, error) {
	results := make([]commonClient.SystemCatalogReferenceResolution, 0, len(references))
	for _, reference := range references {
		result := commonClient.SystemCatalogReferenceResolution{
			SubjectType: reference.SubjectType, ID: reference.ID, Found: r.found,
			Referenceable: r.referenceable, Name: r.name,
		}
		if r.mismatch {
			result.ID++
		}
		results = append(results, result)
	}
	return results, nil
}
