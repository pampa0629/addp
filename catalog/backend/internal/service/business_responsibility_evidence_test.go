package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestBusinessResponsibilityEstablishment(t *testing.T) {
	assertBusinessResponsibilityEstablishment(t, openCatalogServiceTestDB(t))
}

// Run the same write/rollback cases through T1 and the registered PostgreSQL gate.
func assertBusinessResponsibilityEstablishment(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	entries := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	actor := UpdateEntryActor{Type: "user", ID: "99"}
	entry, _ := createEditableCatalogEntry(t, db, 7)
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(false))
	input := UpdateEntryInput{
		Version: entry.Version, GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory,
		Domains: []DomainLinkInput{{ID: 10, Role: models.SemanticRolePrimary}},
		Responsibilities: []ResponsibilityInput{
			{Role: models.ResponsibilityRoleAccountableDepartment, SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 30},
			{Role: models.ResponsibilityRoleDataSteward, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 41},
			{Role: models.ResponsibilityRoleTechnicalOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 44},
		},
	}
	draft, err := entries.Update(ctx, 7, entry.ID, input, actor)
	if err != nil {
		t.Fatal(err)
	}
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(false))
	input.Version = draft.Version
	input.Responsibilities = append(input.Responsibilities, ResponsibilityInput{
		Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 40,
	})
	owned, err := entries.Update(ctx, 7, entry.ID, input, actor)
	if err != nil {
		t.Fatal(err)
	}
	if owned.GovernanceStatus != models.GovernanceStatusDiscovered || owned.Version != draft.Version+1 {
		t.Fatalf("owner assignment changed curation state or incremented twice: %#v", owned.Entry)
	}
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(true))
	var audit models.AuditEvent
	if err := db.Where("catalog_entry_id = ? AND event_type = ?", entry.ID, "catalog.entry.updated").Order("created_at DESC").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(audit.Details["responsibilities"])
	if err != nil || !strings.Contains(string(encoded), `"role":"business_owner"`) || !strings.Contains(string(encoded), `"subject_id":"40"`) {
		t.Fatalf("audit lacks precise responsibility evidence: %s / %v", encoded, err)
	}
	encoded, err = json.Marshal(owned)
	if err != nil || strings.Contains(string(encoded), "business_responsibility_established") {
		t.Fatalf("internal evidence leaked into response: %s / %v", encoded, err)
	}
	cleared, err := entries.Update(ctx, 7, entry.ID, UpdateEntryInput{
		Version: owned.Version, GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory,
	}, actor)
	if err != nil || len(cleared.Responsibilities) != 0 {
		t.Fatalf("clear draft owner: %#v / %v", cleared, err)
	}
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(true))
	curated := curateCompleteEntry(t, entries, cleared.Entry, models.Component{})
	if _, err := entries.Update(ctx, 7, entry.ID, UpdateEntryInput{
		Version: curated.Version, GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory,
	}, actor); err != nil {
		t.Fatal(err)
	}
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(true))
	if err := applyMetaDataItemChange(db, 7, commonClient.MetaDataItemChange{
		Operation: "missing", SourceIdentity: "fingerprint-" + entry.ID.String(), SourceVersion: "00000000000000000002",
		ObservedAt: time.Now().UTC(), Snapshot: map[string]interface{}{"name": "Orders"},
	}); err != nil {
		t.Fatal(err)
	}
	assertBusinessResponsibilityEvidence(t, db, entry.ID, boolEvidence(true))

	// Legacy fixtures intentionally have NULL evidence. Department-only edits
	// cannot turn missing history into proof of never having had an owner.
	legacy := createModelCatalogEntry(t, db, 7, "10")
	assertBusinessResponsibilityEvidence(t, db, legacy.ID, nil)
	legacyInput := UpdateEntryInput{
		Version: legacy.Version, GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory,
		Responsibilities: input.Responsibilities[:1],
	}
	legacyDraft, err := entries.Update(ctx, 7, legacy.ID, legacyInput, actor)
	if err != nil {
		t.Fatal(err)
	}
	assertBusinessResponsibilityEvidence(t, db, legacy.ID, nil)
	legacyInput.Version = legacyDraft.Version
	legacyInput.Responsibilities = input.Responsibilities
	if _, err := entries.Update(ctx, 7, legacy.ID, legacyInput, actor); err != nil {
		t.Fatal(err)
	}
	assertBusinessResponsibilityEvidence(t, db, legacy.ID, boolEvidence(true))

	// Neither a rejected reference nor a transaction failing after the owner
	// write may leave positive evidence, replacement roles or outbox/audit rows.
	fresh, _ := createEditableCatalogEntry(t, db, 7)
	input.Version = fresh.Version
	invalid := NewEntryService(db, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{rejectedUserID: 40})
	if _, err := invalid.Update(ctx, 7, fresh.ID, input, actor); !errors.Is(err, ErrReferenceNotReferenceable) {
		t.Fatalf("invalid current reference error = %v", err)
	}
	assertBusinessResponsibilityEvidence(t, db, fresh.ID, boolEvidence(false))
	stale := input
	stale.Version++
	if _, err := entries.Update(ctx, 7, fresh.ID, stale, actor); !errors.Is(err, ErrEntryVersionConflict) {
		t.Fatalf("stale owner write error = %v", err)
	}
	failure := errors.New("injected establishment audit failure")
	callback := "test:business_responsibility_evidence_audit_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == (models.AuditEvent{}).TableName() {
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	_, err = entries.Update(ctx, 7, fresh.ID, input, actor)
	_ = db.Callback().Create().Remove(callback)
	if !errors.Is(err, failure) {
		t.Fatalf("injected failure error = %v", err)
	}
	assertBusinessResponsibilityEvidence(t, db, fresh.ID, boolEvidence(false))
	assertEntryVersionAndGovernance(t, db, fresh.ID, fresh.Version, models.GovernanceStatusDiscovered)
	for _, model := range []any{&models.Responsibility{}, &models.AuditEvent{}, &models.ProjectionTask{}} {
		var count int64
		if err := db.Model(model).Where("catalog_entry_id = ?", fresh.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rollback left %T count=%d error=%v", model, count, err)
		}
	}
}

func boolEvidence(value bool) *bool { return &value }

func assertBusinessResponsibilityEvidence(t *testing.T, db *gorm.DB, id uuid.UUID, want *bool) {
	t.Helper()
	var entry models.Entry
	if err := db.First(&entry, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	got := entry.BusinessResponsibilityEstablished
	if (got == nil) != (want == nil) || (got != nil && want != nil && *got != *want) {
		t.Fatalf("entry %s establishment evidence=%v, want=%v", id, got, want)
	}
}
