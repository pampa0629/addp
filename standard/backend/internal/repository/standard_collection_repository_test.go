package repository

import (
	"testing"

	"github.com/addp/standard/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStandardCollectionRepositoryWorkflowAndAssignments(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach schema: %v", err)
	}
	statements := []string{
		`CREATE TABLE standard.standard_collections (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, UNIQUE(tenant_id, code))`,
		`CREATE TABLE standard.standard_collection_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, collection_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL, change_summary TEXT NOT NULL, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, UNIQUE(collection_id, revision_no))`,
		`CREATE TABLE standard.standard_collection_members (id INTEGER PRIMARY KEY AUTOINCREMENT, collection_revision_id INTEGER NOT NULL, member_type TEXT NOT NULL, member_id INTEGER NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME, UNIQUE(collection_revision_id, member_type, member_id))`,
		`CREATE TABLE standard.standard_collection_assignments (id INTEGER PRIMARY KEY AUTOINCREMENT, collection_id INTEGER NOT NULL, principal_id INTEGER NOT NULL, role TEXT NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME, UNIQUE(collection_id, principal_id, role))`,
		`CREATE TABLE standard.standard_collection_events (id INTEGER PRIMARY KEY AUTOINCREMENT, collection_id INTEGER NOT NULL, revision_id INTEGER, event_type TEXT NOT NULL, actor_id INTEGER NOT NULL, detail TEXT NOT NULL, created_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}

	repo := NewStandardCollectionRepository(db)
	collection := &models.StandardCollection{TenantID: 7, Code: "core", CreatedBy: 11}
	revision := &models.StandardCollectionRevision{Name: "Core", Description: "Core standards", ChangeSummary: "initial", CreatedBy: 11}
	if err := repo.Create(collection, revision, []models.StandardCollectionMember{{MemberType: models.CollectionMemberElement, MemberID: 31}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if collection.DraftRevisionID == nil || *collection.DraftRevisionID != revision.ID {
		t.Fatalf("draft pointer = %#v", collection.DraftRevisionID)
	}
	if ok, err := repo.HasRole(collection.ID, 7, 11, models.CollectionAssignmentOwner); err != nil || !ok {
		t.Fatalf("creator owner role: ok=%v err=%v", ok, err)
	}

	assignments := []models.StandardCollectionAssignment{
		{PrincipalID: 11, Role: models.CollectionAssignmentOwner},
		{PrincipalID: 12, Role: models.CollectionAssignmentReviewer},
	}
	if err := repo.ReplaceAssignments(collection.ID, 7, 11, 1, assignments); err != nil {
		t.Fatalf("replace assignments: %v", err)
	}
	if err := repo.Transition(collection.ID, revision.ID, 7, 11, 2, models.RevisionStatusDraft, models.RevisionStatusInReview); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := repo.Publish(collection.ID, revision.ID, 7, 12, 3); err != nil {
		t.Fatalf("publish: %v", err)
	}

	aggregate, err := repo.GetAggregate(collection.ID, 7, 12)
	if err != nil {
		t.Fatalf("get aggregate: %v", err)
	}
	if aggregate.DraftRevision != nil || aggregate.CurrentRevision == nil || aggregate.CurrentRevision.Status != models.RevisionStatusPublished || aggregate.Version != 4 {
		t.Fatalf("published aggregate = %#v", aggregate)
	}
	if len(aggregate.CurrentRevision.Members) != 1 || len(aggregate.MyRoles) != 1 || aggregate.MyRoles[0] != models.CollectionAssignmentReviewer {
		t.Fatalf("members/roles = %#v / %#v", aggregate.CurrentRevision.Members, aggregate.MyRoles)
	}
	if err := repo.Delete(collection.ID, 7, 11, 4); err == nil {
		t.Fatal("published collection should not be deleted")
	}
	events, total, err := repo.ListEvents(collection.ID, 7, 1, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if total != 4 || len(events) != 4 || events[0].EventType != models.CollectionEventPublished || events[3].EventType != models.CollectionEventCreated {
		t.Fatalf("events = %#v", events)
	}
}
