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
	service := NewGovernanceTaskService(db, NewEntryService(db, nil, nil), resolver)
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

	result, err := service.List(context.Background(), 7, EntryAccess{Inventory: true}, GovernanceTaskListFilter{
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
	governance := NewGovernanceTaskService(db, NewEntryService(db, nil, nil), &governanceSystemResolver{found: false})
	governance.now = func() time.Time { return now }
	if err := governance.ReconcileTenant(context.Background(), 7); err != nil {
		t.Fatal(err)
	}

	entries := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	if _, err := entries.Update(context.Background(), 7, entry.ID, UpdateEntryInput{
		Version: 2, GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory,
	}, UpdateEntryActor{Type: "user", ID: "99"}); err != nil {
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
	service := NewGovernanceTaskService(db, NewEntryService(db, nil, nil), &governanceSystemResolver{found: true, referenceable: false, mismatch: true})
	if err := service.ReconcileTenant(context.Background(), 7); !errors.Is(err, ErrReferenceValidationUnavailable) {
		t.Fatalf("mismatched resolution error = %v", err)
	}
	assertResponsibilityGovernanceState(t, db, responsibility.ID, entry.ID, models.ResponsibilityStatusActive, "", 1, 0)
}

func TestResponsibilityRecoveryUsesCurrentEntryVisibility(t *testing.T) {
	assertResponsibilityRecoveryUsesCurrentEntryVisibility(t, openCatalogServiceTestDB(t))
}

// This scenario runs in T1 and in the registered PostgreSQL gate, exercising
// the same visibility predicates, reconciliation and aggregate repair path.
func assertResponsibilityRecoveryUsesCurrentEntryVisibility(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	entries := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	curate := func(tenantID, departmentID int64, visibility string) (*EntryDetail, UpdateEntryInput) {
		t.Helper()
		entry, _ := createEditableCatalogEntry(t, db, tenantID)
		name, description := "Orders", "Enterprise order facts"
		input := UpdateEntryInput{
			Version: entry.Version, BusinessName: &name, BusinessDescription: &description,
			GovernanceStatus: models.GovernanceStatusCurated, Visibility: visibility,
			Domains: []DomainLinkInput{{ID: 10, Role: models.SemanticRolePrimary}},
			Responsibilities: []ResponsibilityInput{
				{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: departmentID},
				{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40},
				{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
			},
		}
		result, err := entries.Update(ctx, tenantID, entry.ID, input, UpdateEntryActor{Type: "user", ID: "99"})
		if err != nil {
			t.Fatal(err)
		}
		return result, input
	}
	orphan, repairInput := curate(7, 30, models.VisibilityDepartment)
	departmentEntry, certifiedRepairInput := curate(7, 31, models.VisibilityDepartment)
	certified, err := entries.UpdateGovernance(ctx, 7, departmentEntry.ID, UpdateEntryGovernanceInput{
		Version: departmentEntry.Version, GovernanceStatus: models.GovernanceStatusCertified,
	}, UpdateEntryGovernanceAuthorization{CanCertify: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatal(err)
	}
	tenantEntry, _ := curate(7, 32, models.VisibilityTenant)
	crossTenantEntry, _ := curate(8, 30, models.VisibilityDepartment)
	inventoryEntry, _ := createEditableCatalogEntry(t, db, 7)
	if err := db.Create(&models.Responsibility{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: inventoryEntry.ID,
		Role: models.ResponsibilityRoleTechnicalOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40,
		Status: models.ResponsibilityStatusActive, ObservedSnapshot: map[string]interface{}{"name": "Former Maintainer"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceTaskService(db, entries, &invalidGovernanceSubjectsResolver{})
	for _, tenantID := range []int64{7, 8} {
		if err := governance.ReconcileTenant(ctx, tenantID); err != nil {
			t.Fatal(err)
		}
	}
	checks := []struct {
		name   string
		access EntryAccess
		ids    map[uuid.UUID]int
	}{
		{"tenant reader", EntryAccess{}, map[uuid.UUID]int{tenantEntry.ID: 1}},
		{"active department", EntryAccess{DepartmentIDs: []int64{31}}, map[uuid.UUID]int{tenantEntry.ID: 1, departmentEntry.ID: 1}},
		{"former department", EntryAccess{DepartmentIDs: []int64{30}}, map[uuid.UUID]int{tenantEntry.ID: 1}},
		{"tenant governance", EntryAccess{Inventory: true}, map[uuid.UUID]int{orphan.ID: 2, departmentEntry.ID: 1, tenantEntry.ID: 1, inventoryEntry.ID: 1}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			result, err := governance.List(ctx, 7, check.access, GovernanceTaskListFilter{Status: models.GovernanceTaskStatusOpen, Page: 1, PageSize: 20})
			if err != nil {
				t.Fatal(err)
			}
			expectedTotal := 0
			actual := make(map[uuid.UUID]int)
			for _, count := range check.ids {
				expectedTotal += count
			}
			for _, task := range result.Data {
				actual[task.CatalogEntryID]++
				if _, err := entries.Get(ctx, 7, check.access, task.CatalogEntryID); err != nil {
					t.Fatalf("queue returned an entry that cannot be opened: %v", err)
				}
			}
			if result.Total != int64(expectedTotal) || len(result.Data) != expectedTotal || len(actual) != len(check.ids) {
				t.Fatalf("visibility mismatch: result=%#v actual=%v expected=%v", result, actual, check.ids)
			}
			for id, count := range check.ids {
				if actual[id] != count {
					t.Fatalf("entry %s: tasks=%d, want %d", id, actual[id], count)
				}
			}
		})
	}
	for _, status := range []string{models.GovernanceTaskStatusOpen, models.GovernanceTaskStatusResolved} {
		for _, id := range []uuid.UUID{orphan.ID, inventoryEntry.ID, crossTenantEntry.ID} {
			result, err := governance.List(ctx, 7, EntryAccess{DepartmentIDs: []int64{31}}, GovernanceTaskListFilter{Status: status, EntryID: id, Page: 1, PageSize: 20})
			if err != nil || result.Total != 0 || len(result.Data) != 0 || result.TotalPages != 0 {
				t.Fatalf("exact filter leaked hidden entry %s: %#v, error=%v", id, result, err)
			}
		}
	}
	result, err := governance.List(ctx, 7, EntryAccess{Inventory: true}, GovernanceTaskListFilter{Status: models.GovernanceTaskStatusOpen, EntryID: crossTenantEntry.ID, Page: 1, PageSize: 20})
	if err != nil || result.Total != 0 {
		t.Fatalf("inventory crossed tenant boundary: %#v, error=%v", result, err)
	}
	page, err := governance.List(ctx, 7, EntryAccess{Inventory: true}, GovernanceTaskListFilter{Status: models.GovernanceTaskStatusOpen, Page: 2, PageSize: 2})
	if err != nil || page.Total != 5 || len(page.Data) != 2 || page.TotalPages != 3 {
		t.Fatalf("filtered pagination = %#v, error=%v", page, err)
	}
	current, err := entries.Get(ctx, 7, EntryAccess{Inventory: true}, orphan.ID)
	if err != nil {
		t.Fatalf("tenant governance cannot open orphaned entry: %v", err)
	}
	repairInput.Responsibilities[0].SubjectID = 33
	repairInput.Responsibilities[1].SubjectID = 43
	actor := UpdateEntryActor{Type: "user", ID: "99"}
	if _, err := entries.Update(ctx, 7, orphan.ID, repairInput, actor); !errors.Is(err, ErrEntryVersionConflict) {
		t.Fatalf("stale repair must fail without closing tasks: %v", err)
	}
	open, err := governance.List(ctx, 7, EntryAccess{Inventory: true}, GovernanceTaskListFilter{Status: models.GovernanceTaskStatusOpen, EntryID: orphan.ID, Page: 1, PageSize: 20})
	if err != nil || open.Total != 2 {
		t.Fatalf("stale repair mutated tasks: %#v, error=%v", open, err)
	}
	repairInput.Version = current.Version
	repaired, err := entries.Update(ctx, 7, orphan.ID, repairInput, actor)
	if err != nil || repaired.Version != current.Version+1 || repaired.Source.ID != orphan.Source.ID ||
		repaired.Source.SourceIdentity != orphan.Source.SourceIdentity || repaired.GovernanceStatus != models.GovernanceStatusCurated {
		t.Fatalf("repair changed source or failed to advance the aggregate: %#v, error=%v", repaired, err)
	}
	if _, err := entries.Get(ctx, 7, EntryAccess{DepartmentIDs: []int64{33}}, orphan.ID); err != nil {
		t.Fatalf("replacement department cannot open repaired entry: %v", err)
	}
	for _, status := range []string{models.GovernanceTaskStatusOpen, models.GovernanceTaskStatusResolved} {
		result, err := governance.List(ctx, 7, EntryAccess{Inventory: true}, GovernanceTaskListFilter{Status: status, EntryID: orphan.ID, Page: 1, PageSize: 20})
		expected := int64(0)
		if status == models.GovernanceTaskStatusResolved {
			expected = 2
		}
		if err != nil || result.Total != expected {
			t.Fatalf("task lifecycle after repair: %#v, error=%v", result, err)
		}
		for _, task := range result.Data {
			if task.Resolution == nil || *task.Resolution != models.GovernanceTaskResolutionResponsibilityReplaced {
				t.Fatalf("task did not resolve through aggregate repair: %#v", task)
			}
		}
	}
	hiddenResolved, err := governance.List(ctx, 7, EntryAccess{DepartmentIDs: []int64{31}}, GovernanceTaskListFilter{
		Status: models.GovernanceTaskStatusResolved, EntryID: orphan.ID, Page: 1, PageSize: 20,
	})
	if err != nil || hiddenResolved.Total != 0 || len(hiddenResolved.Data) != 0 {
		t.Fatalf("resolved task exposed hidden entry: %#v, error=%v", hiddenResolved, err)
	}
	if _, err := entries.Get(ctx, 8, EntryAccess{Inventory: true}, orphan.ID); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("repair visibility crossed tenant boundary: %v", err)
	}
	var audits []models.AuditEvent
	if err := db.Where("catalog_entry_id = ? AND event_type = ?", orphan.ID, "catalog.entry.updated").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 2 {
		t.Fatalf("initial curation and one successful repair must be audited: %#v", audits)
	}
	for _, audit := range audits {
		if audit.ActorType != actor.Type || audit.ActorID != actor.ID {
			t.Fatalf("repair audit lost the explicit actor: %#v", audit)
		}
	}
	currentCertified, err := entries.Get(ctx, 7, EntryAccess{Inventory: true}, certified.ID)
	if err != nil {
		t.Fatal(err)
	}
	certifiedRepairInput.Version = currentCertified.Version
	certifiedRepairInput.Responsibilities[1].SubjectID = 43
	if _, err := entries.Update(ctx, 7, certified.ID, certifiedRepairInput, actor); !errors.Is(err, ErrEntryNotEditable) {
		t.Fatalf("responsibility repair bypassed certification freeze: %v", err)
	}
	reason := "Withdraw certification to transfer the invalid owner"
	withdrawal := UpdateEntryGovernanceInput{
		Version: currentCertified.Version, GovernanceStatus: models.GovernanceStatusCurated, Reason: &reason,
	}
	if _, err := entries.UpdateGovernance(ctx, 7, certified.ID, withdrawal, UpdateEntryGovernanceAuthorization{}, actor); !errors.Is(err, ErrCertificationPermissionRequired) {
		t.Fatalf("repair implicitly granted certification authority: %v", err)
	}
	withdrawn, err := entries.UpdateGovernance(ctx, 7, certified.ID, withdrawal, UpdateEntryGovernanceAuthorization{CanCertify: true}, actor)
	if err != nil {
		t.Fatal(err)
	}
	certifiedRepairInput.Version = withdrawn.Version
	if _, err := entries.Update(ctx, 7, certified.ID, certifiedRepairInput, actor); err != nil {
		t.Fatalf("repair after authorized certification withdrawal: %v", err)
	}
	resolvedCertified, err := governance.List(ctx, 7, EntryAccess{Inventory: true}, GovernanceTaskListFilter{
		Status: models.GovernanceTaskStatusResolved, EntryID: certified.ID, Page: 1, PageSize: 20,
	})
	if err != nil || resolvedCertified.Total != 1 {
		t.Fatalf("certified responsibility transfer did not resolve task: %#v, error=%v", resolvedCertified, err)
	}
}

type invalidGovernanceSubjectsResolver struct{}

func (*invalidGovernanceSubjectsResolver) ResolveSystemReferences(ctx context.Context, tenantID int64, references []commonClient.SystemCatalogReference) ([]commonClient.SystemCatalogReferenceResolution, error) {
	results, err := (&fakeSystemReferenceResolver{}).ResolveSystemReferences(ctx, tenantID, references)
	for index, result := range results {
		if (result.SubjectType == models.ResponsibilitySubjectDepartment && result.ID == 30) ||
			(result.SubjectType == models.ResponsibilitySubjectUser && result.ID == 40) {
			results[index].Referenceable = false
		}
	}
	return results, err
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
