package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/addp/catalog/internal/models"
	"gorm.io/gorm"
)

func TestDeprecatedResponsibilityTransferAndWithdrawal(t *testing.T) {
	assertDeprecatedResponsibilityTransferAndWithdrawal(t, openCatalogServiceTestDB(t))
}

// Shared by the unit suite and the standard PostgreSQL gate.
func assertDeprecatedResponsibilityTransferAndWithdrawal(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	entry, component := createEditableCatalogEntry(t, db, 7)
	entries := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	curated := curateCompleteEntry(t, entries, entry, component)
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(true))
	if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("visibility", models.VisibilityDepartment).Error; err != nil {
		t.Fatal(err)
	}
	certified, err := entries.UpdateGovernance(ctx, 7, entry.ID, UpdateEntryGovernanceInput{
		Version: curated.Version, GovernanceStatus: models.GovernanceStatusCertified,
	}, UpdateEntryGovernanceAuthorization{CanCertify: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatal(err)
	}
	successor, _ := createEditableCatalogEntry(t, db, 7)
	makeSuccessorEligible(t, db, successor.ID)
	reason := "Replaced resource"
	deprecated, err := entries.UpdateGovernance(ctx, 7, entry.ID, UpdateEntryGovernanceInput{
		Version: certified.Version, GovernanceStatus: models.GovernanceStatusDeprecated,
		Reason: &reason, RecommendedSuccessorEntryID: &successor.ID,
	}, UpdateEntryGovernanceAuthorization{CanDeprecate: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceTaskService(db, entries, &invalidGovernanceSubjectsResolver{})
	if err := governance.ReconcileTenant(ctx, 7); err != nil {
		t.Fatal(err)
	}
	before, err := entries.Get(ctx, 7, EntryAccess{Inventory: true}, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Version <= deprecated.Version {
		t.Fatal("invalid references did not advance the aggregate version")
	}
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(true))
	// Standard is deliberately absent: frozen semantic facts are not revalidated.
	entries = NewEntryService(db, nil, &fakeSystemReferenceResolver{})
	input := TransferEntryResponsibilitiesInput{
		Version: before.Version, Reason: " Transfer to current stewards ",
		Responsibilities: []ResponsibilityInput{
			{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 33},
			{Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 43},
			{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
			{Role: models.ResponsibilityRoleTechnicalOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 44},
		},
	}
	actor := UpdateEntryActor{Type: "user", ID: "100"} // Not the original deprecating user.
	for _, tc := range []struct {
		name    string
		tenant  int64
		access  EntryAccess
		version int64
		want    error
	}{
		{"hidden former department", 7, EntryAccess{DepartmentIDs: []int64{30}}, before.Version, ErrEntryNotFound},
		{"cross tenant", 8, EntryAccess{Inventory: true}, before.Version, ErrEntryNotFound},
		{"stale version", 7, EntryAccess{Inventory: true}, before.Version - 1, ErrEntryVersionConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := input
			candidate.Version = tc.version
			_, err := entries.TransferResponsibilities(ctx, tc.tenant, entry.ID, tc.access, candidate, actor)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			assertEntryVersionAndGovernance(t, db, entry.ID, before.Version, models.GovernanceStatusDeprecated)
		})
	}
	for _, candidate := range []TransferEntryResponsibilitiesInput{
		{Version: before.Version, Reason: "", Responsibilities: input.Responsibilities},
		{Version: before.Version, Reason: "Missing owner", Responsibilities: input.Responsibilities[:1]},
		{Version: before.Version, Reason: "Duplicate", Responsibilities: append(append([]ResponsibilityInput{}, input.Responsibilities...), input.Responsibilities[0])},
	} {
		if _, err := entries.TransferResponsibilities(ctx, 7, entry.ID, EntryAccess{Inventory: true}, candidate, actor); !errors.Is(err, ErrInvalidEntryUpdate) {
			t.Fatalf("invalid transfer error = %v", err)
		}
	}
	invalidReference := input
	invalidReference.Responsibilities = append([]ResponsibilityInput{}, input.Responsibilities...)
	invalidReference.Responsibilities[0].SubjectID = 30
	if _, err := NewEntryService(db, nil, &invalidGovernanceSubjectsResolver{}).TransferResponsibilities(ctx, 7, entry.ID,
		EntryAccess{Inventory: true}, invalidReference, actor); !errors.Is(err, ErrReferenceNotReferenceable) {
		t.Fatalf("invalid current System subject error=%v", err)
	}
	assertEntryVersionAndGovernance(t, db, entry.ID, before.Version, models.GovernanceStatusDeprecated)
	// A failure after replacing responsibilities must roll back replacements,
	// task resolution and aggregate version together.
	failure := errors.New("injected responsibility audit failure")
	callback := "test:responsibility_transfer_audit_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == (models.AuditEvent{}).TableName() {
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	_, failed := entries.TransferResponsibilities(ctx, 7, entry.ID, EntryAccess{Inventory: true}, input, actor)
	_ = db.Callback().Create().Remove(callback)
	if !errors.Is(failed, failure) {
		t.Fatalf("audit failure error=%v", failed)
	}
	assertEntryVersionAndGovernance(t, db, entry.ID, before.Version, models.GovernanceStatusDeprecated)
	unchanged, err := entries.Get(ctx, 7, EntryAccess{Inventory: true}, entry.ID)
	if err != nil || !reflect.DeepEqual(unchanged.Responsibilities, before.Responsibilities) {
		t.Fatalf("rollback responsibilities=%#v error=%v", unchanged, err)
	}
	var openTasks int64
	if err := db.Model(&models.GovernanceTask{}).Where("catalog_entry_id = ? AND status = ?", entry.ID, models.GovernanceTaskStatusOpen).Count(&openTasks).Error; err != nil {
		t.Fatal(err)
	}
	if openTasks != 2 {
		t.Fatalf("tasks resolved despite rollback: %d", openTasks)
	}
	after, err := entries.TransferResponsibilities(ctx, 7, entry.ID, EntryAccess{Inventory: true}, input, actor)
	if err != nil {
		t.Fatal(err)
	}
	assertEntryVersionAndGovernance(t, db, entry.ID, before.Version+1, models.GovernanceStatusDeprecated)
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(true))
	if !reflect.DeepEqual(before.BusinessName, after.BusinessName) || !reflect.DeepEqual(before.BusinessDescription, after.BusinessDescription) ||
		before.Visibility != after.Visibility || !reflect.DeepEqual(before.RecommendedSuccessorEntryID, after.RecommendedSuccessorEntryID) ||
		!reflect.DeepEqual(before.SemanticLinks, after.SemanticLinks) || !reflect.DeepEqual(before.Source, after.Source) ||
		!reflect.DeepEqual(before.StandardMappings, after.StandardMappings) || len(after.Responsibilities) != 4 {
		t.Fatalf("transfer changed frozen facts: before=%#v after=%#v", before, after)
	}
	for _, department := range []int64{30, 33} {
		_, err := entries.Get(ctx, 7, EntryAccess{DepartmentIDs: []int64{department}}, entry.ID)
		if (department == 30 && !errors.Is(err, ErrEntryNotFound)) || (department == 33 && err != nil) {
			t.Fatalf("department %d error=%v", department, err)
		}
	}
	var tasks []models.GovernanceTask
	if err := db.Where("catalog_entry_id = ?", entry.ID).Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %#v", tasks)
	}
	for _, task := range tasks {
		if task.Status != models.GovernanceTaskStatusResolved || task.Resolution == nil || *task.Resolution != models.GovernanceTaskResolutionResponsibilityReplaced {
			t.Fatalf("unresolved task = %#v", task)
		}
	}
	var audit models.AuditEvent
	if err := db.Where("catalog_entry_id = ? AND event_type = ?", entry.ID, "catalog.entry.responsibilities_transferred").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.ActorID != actor.ID || audit.Details["reason"] != "Transfer to current stewards" || audit.Details["previous_responsibilities"] == nil {
		t.Fatalf("transfer audit = %#v", audit)
	}
	// Withdrawal is a separate privileged operation; transfer does not imply it.
	reason = "Business use is reinstated"
	withdraw := UpdateEntryGovernanceInput{Version: after.Version, GovernanceStatus: models.GovernanceStatusCurated, Reason: &reason}
	for _, tc := range []struct {
		name  string
		input UpdateEntryGovernanceInput
		auth  UpdateEntryGovernanceAuthorization
		want  error
	}{
		{"no deprecation permission", withdraw, UpdateEntryGovernanceAuthorization{}, ErrDeprecationPermissionRequired},
		{"missing reason", UpdateEntryGovernanceInput{Version: after.Version, GovernanceStatus: models.GovernanceStatusCurated}, UpdateEntryGovernanceAuthorization{CanDeprecate: true}, ErrDeprecationReasonRequired},
		{"stale withdrawal", UpdateEntryGovernanceInput{Version: after.Version - 1, GovernanceStatus: models.GovernanceStatusCurated, Reason: &reason}, UpdateEntryGovernanceAuthorization{CanDeprecate: true}, ErrEntryVersionConflict},
		{"direct certification", UpdateEntryGovernanceInput{Version: after.Version, GovernanceStatus: models.GovernanceStatusCertified, Reason: &reason}, UpdateEntryGovernanceAuthorization{CanCertify: true, CanDeprecate: true}, ErrInvalidGovernanceTransition},
		{"successor on withdrawal", UpdateEntryGovernanceInput{Version: after.Version, GovernanceStatus: models.GovernanceStatusCurated, Reason: &reason, RecommendedSuccessorEntryID: &successor.ID}, UpdateEntryGovernanceAuthorization{CanDeprecate: true}, ErrInvalidGovernanceUpdate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := entries.UpdateGovernance(ctx, 7, entry.ID, tc.input, tc.auth, actor); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			assertEntryVersionAndGovernance(t, db, entry.ID, after.Version, models.GovernanceStatusDeprecated)
		})
	}
	// Neither Standard nor System needs to be online to withdraw deprecation.
	restored, err := NewEntryService(db, nil, nil).UpdateGovernance(ctx, 7, entry.ID, withdraw, UpdateEntryGovernanceAuthorization{CanDeprecate: true}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if restored.GovernanceStatus != models.GovernanceStatusCurated || restored.RecommendedSuccessorEntryID != nil || !reflect.DeepEqual(restored.Responsibilities, after.Responsibilities) || !reflect.DeepEqual(restored.SemanticLinks, after.SemanticLinks) {
		t.Fatalf("restored aggregate=%#v", restored)
	}
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(true))
	audit = models.AuditEvent{}
	if err := db.Where("catalog_entry_id = ? AND event_type = ?", entry.ID, "catalog.entry.deprecation_withdrawn").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.Details["previous_recommended_successor_entry_id"] != successor.ID.String() || audit.Details["reason"] != reason {
		t.Fatalf("withdrawal audit=%#v", audit)
	}
	input.Version = restored.Version
	if _, err := entries.TransferResponsibilities(ctx, 7, entry.ID, EntryAccess{Inventory: true}, input, actor); !errors.Is(err, ErrEntryNotEditable) {
		t.Fatalf("transfer on curated error=%v", err)
	}
}
