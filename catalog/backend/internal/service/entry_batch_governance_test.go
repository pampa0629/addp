package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestBatchGovernanceAssignsDepartmentAtomicallyAndPreservesOtherResponsibilities(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	first, _ := createEditableCatalogEntry(t, db, 7)
	second, _ := createEditableCatalogEntry(t, db, 7)
	now := time.Now().UTC()
	seedResponsibility(t, db, first.ID, models.ResponsibilityRoleAccountableDepartment, models.ResponsibilitySubjectDepartment, 21)
	seedResponsibility(t, db, first.ID, models.ResponsibilityRoleBusinessOwner, models.ResponsibilitySubjectUser, 31)
	seedResponsibility(t, db, second.ID, models.ResponsibilityRoleDataSteward, models.ResponsibilitySubjectUser, 32)
	seedGovernanceTask(t, db, first.ID, 21, now)
	seedGovernanceTask(t, db, second.ID, 44, now)
	system := &fakeSystemReferenceResolver{}
	svc := NewEntryService(db, &fakeStandardReferenceResolver{}, system)

	result, err := svc.BatchGovernance(context.Background(), 7, BatchGovernanceInput{
		Entries:   []BatchGovernanceEntryInput{{ID: second.ID, Version: 1}, {ID: first.ID, Version: 1}},
		Operation: BatchGovernanceAssignAccountableDepartment, ReferenceID: 44,
	}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("BatchGovernance() error = %v", err)
	}
	if result.BatchID == uuid.Nil || len(result.Entries) != 2 || result.Entries[0].ID != second.ID || result.Entries[0].Version != 2 || result.Entries[1].ID != first.ID {
		t.Fatalf("result = %#v", result)
	}
	if system.calls != 1 {
		t.Fatalf("System resolver calls = %d, want 1", system.calls)
	}
	for _, entryID := range []uuid.UUID{first.ID, second.ID} {
		var entry models.Entry
		if err := db.First(&entry, "id = ?", entryID).Error; err != nil || entry.Version != 2 {
			t.Fatalf("entry %s version/error = %d/%v", entryID, entry.Version, err)
		}
		var departments []models.Responsibility
		if err := db.Where("catalog_entry_id = ? AND role = ?", entryID, models.ResponsibilityRoleAccountableDepartment).Find(&departments).Error; err != nil {
			t.Fatal(err)
		}
		if len(departments) != 1 || departments[0].SubjectID != 44 || departments[0].ObservedSnapshot["name"] == "" {
			t.Fatalf("departments for %s = %#v", entryID, departments)
		}
	}
	var preserved int64
	if err := db.Model(&models.Responsibility{}).Where("role IN ?", []string{models.ResponsibilityRoleBusinessOwner, models.ResponsibilityRoleDataSteward}).Count(&preserved).Error; err != nil || preserved != 2 {
		t.Fatalf("preserved responsibilities = %d, error = %v", preserved, err)
	}
	var tasks []models.GovernanceTask
	if err := db.Order("catalog_entry_id").Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].Status != models.GovernanceTaskStatusResolved || tasks[1].Status != models.GovernanceTaskStatusResolved {
		t.Fatalf("tasks = %#v", tasks)
	}
	var audits []models.AuditEvent
	if err := db.Where("event_type = ?", "catalog.entry.batch_governance_applied").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 2 || audits[0].Details["batch_id"] != result.BatchID.String() || audits[1].Details["batch_id"] != result.BatchID.String() {
		t.Fatalf("audits = %#v", audits)
	}
	var projectionCount int64
	if err := db.Model(&models.ProjectionTask{}).Where("catalog_entry_id IN ?", []uuid.UUID{first.ID, second.ID}).Count(&projectionCount).Error; err != nil || projectionCount != 2 {
		t.Fatalf("projection count = %d, error = %v", projectionCount, err)
	}
}

func TestBatchGovernanceVersionConflictRollsBackWholeBatch(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	first, _ := createEditableCatalogEntry(t, db, 7)
	second, _ := createEditableCatalogEntry(t, db, 7)
	seedResponsibility(t, db, first.ID, models.ResponsibilityRoleAccountableDepartment, models.ResponsibilitySubjectDepartment, 21)
	svc := NewEntryService(db, nil, &fakeSystemReferenceResolver{})

	_, err := svc.BatchGovernance(context.Background(), 7, BatchGovernanceInput{
		Entries:   []BatchGovernanceEntryInput{{ID: first.ID, Version: 1}, {ID: second.ID, Version: 2}},
		Operation: BatchGovernanceAssignAccountableDepartment, ReferenceID: 44,
	}, UpdateEntryActor{Type: "user", ID: "99"})
	if !errors.Is(err, ErrEntryVersionConflict) {
		t.Fatalf("BatchGovernance() error = %v", err)
	}
	var responsibility models.Responsibility
	if err := db.Where("catalog_entry_id = ? AND role = ?", first.ID, models.ResponsibilityRoleAccountableDepartment).First(&responsibility).Error; err != nil {
		t.Fatal(err)
	}
	if responsibility.SubjectID != 21 {
		t.Fatalf("responsibility changed after rollback = %#v", responsibility)
	}
	var changedEntries, audits int64
	if err := db.Model(&models.Entry{}).Where("id IN ? AND version <> ?", []uuid.UUID{first.ID, second.ID}, 1).Count(&changedEntries).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AuditEvent{}).Where("event_type = ?", "catalog.entry.batch_governance_applied").Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if changedEntries != 0 || audits != 0 {
		t.Fatalf("rollback side effects entries/audits = %d/%d", changedEntries, audits)
	}
}

func TestBatchGovernanceRejectsOwnerManagedPrimaryDomainWithoutPartialWrite(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	dataItem, _ := createEditableCatalogEntry(t, db, 7)
	modelEntry := createModelCatalogEntry(t, db, 7, "10")
	seedSemantic(t, db, dataItem.ID, models.SemanticTypeDomain, models.SemanticRolePrimary, 9)
	standard := &fakeStandardReferenceResolver{}
	svc := NewEntryService(db, standard, nil)

	_, err := svc.BatchGovernance(context.Background(), 7, BatchGovernanceInput{
		Entries:   []BatchGovernanceEntryInput{{ID: dataItem.ID, Version: 1}, {ID: modelEntry.ID, Version: 1}},
		Operation: BatchGovernanceAssignPrimaryDomain, ReferenceID: 44,
	}, UpdateEntryActor{Type: "user", ID: "99"})
	if !errors.Is(err, ErrBatchGovernanceUnsupportedEntry) {
		t.Fatalf("BatchGovernance() error = %v", err)
	}
	if standard.calls != 1 {
		t.Fatalf("Standard resolver calls = %d, want 1", standard.calls)
	}
	var association models.SemanticAssociation
	if err := db.Where("catalog_entry_id = ? AND semantic_type = ? AND relation_role = ?", dataItem.ID, models.SemanticTypeDomain, models.SemanticRolePrimary).First(&association).Error; err != nil {
		t.Fatal(err)
	}
	if association.SemanticID != 9 {
		t.Fatalf("primary domain changed after rollback = %#v", association)
	}
}

func TestBatchGovernancePrimaryDomainPreservesSecondaryAndGlossaryLinks(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	seedSemantic(t, db, entry.ID, models.SemanticTypeDomain, models.SemanticRolePrimary, 9)
	seedSemantic(t, db, entry.ID, models.SemanticTypeDomain, models.SemanticRoleSecondary, 10)
	seedSemantic(t, db, entry.ID, models.SemanticTypeGlossary, models.SemanticRoleApplies, 11)
	svc := NewEntryService(db, &fakeStandardReferenceResolver{}, nil)

	_, err := svc.BatchGovernance(context.Background(), 7, BatchGovernanceInput{
		Entries:   []BatchGovernanceEntryInput{{ID: entry.ID, Version: 1}},
		Operation: BatchGovernanceAssignPrimaryDomain, ReferenceID: 44,
	}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("BatchGovernance() error = %v", err)
	}
	var associations []models.SemanticAssociation
	if err := db.Where("catalog_entry_id = ?", entry.ID).Order("semantic_id").Find(&associations).Error; err != nil {
		t.Fatal(err)
	}
	if len(associations) != 3 {
		t.Fatalf("associations = %#v", associations)
	}
	seen := map[int64]string{}
	for _, association := range associations {
		seen[association.SemanticID] = association.RelationRole
	}
	if seen[44] != models.SemanticRolePrimary || seen[10] != models.SemanticRoleSecondary || seen[11] != models.SemanticRoleApplies {
		t.Fatalf("semantic roles = %#v", seen)
	}
}

func TestBatchGovernanceRejectsInvalidMemberSet(t *testing.T) {
	id := uuid.New()
	svc := NewEntryService(openCatalogServiceTestDB(t), nil, nil)
	for name, entries := range map[string][]BatchGovernanceEntryInput{
		"empty":     {},
		"duplicate": {{ID: id, Version: 1}, {ID: id, Version: 1}},
		"too_many":  make([]BatchGovernanceEntryInput, BatchGovernanceMaxEntries+1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.BatchGovernance(context.Background(), 7, BatchGovernanceInput{
				Entries: entries, Operation: BatchGovernanceAssignPrimaryDomain, ReferenceID: 1,
			}, UpdateEntryActor{Type: "user", ID: "99"})
			if !errors.Is(err, ErrInvalidBatchGovernance) {
				t.Fatalf("BatchGovernance() error = %v", err)
			}
		})
	}
}

func seedResponsibility(t *testing.T, db *gorm.DB, entryID uuid.UUID, role, subjectType string, subjectID int64) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Create(&models.Responsibility{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entryID, Role: role, SubjectType: subjectType,
		SubjectID: subjectID, Status: models.ResponsibilityStatusActive,
		ObservedSnapshot: commonModels.JSONMap{"name": "existing"}, VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func seedSemantic(t *testing.T, db *gorm.DB, entryID uuid.UUID, semanticType, role string, semanticID int64) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Create(&models.SemanticAssociation{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entryID, SemanticType: semanticType,
		SemanticID: semanticID, RelationRole: role, ObservedVersion: 1,
		ObservedSnapshot: commonModels.JSONMap{"name": "existing"}, VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func seedGovernanceTask(t *testing.T, db *gorm.DB, entryID uuid.UUID, subjectID int64, now time.Time) {
	t.Helper()
	if err := db.Create(&models.GovernanceTask{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entryID,
		TaskType:           models.GovernanceTaskTypeResponsibilityTransfer,
		ResponsibilityRole: models.ResponsibilityRoleAccountableDepartment,
		SubjectType:        models.ResponsibilitySubjectDepartment, SubjectID: subjectID,
		Status: models.GovernanceTaskStatusOpen, Reason: models.GovernanceTaskReasonSubjectNotReferenceable,
		ObservedSnapshot: commonModels.JSONMap{"name": "existing"}, OpenedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}
