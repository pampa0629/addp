package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestCurrentSharingDecisionBasisDiffersFromHistoryAndHasNoSideEffects(t *testing.T) {
	for _, name := range []string{"current", "prose_edit", "long_term", "same_user_new_relation", "different_owner", "inactive_owner", "no_owner", "source_version", "rebound_source", "missing_source", "not_current_source", "other_module", "deprecated", "merged", "other_type", "version_regression", "expired", "foreign_tenant", "other_entry"} {
		t.Run(name, func(t *testing.T) {
			db := openCatalogServiceTestDB(t)
			entry, input := seedSharingEntry(t, db)
			if name == "long_term" {
				input.ExpiryMode, input.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil
			}
			resolver := &fakeSharingTargetResolver{}
			s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(resolver)
			original, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
			if err != nil {
				t.Fatal(err)
			}
			mustExec := func(sql string, args ...any) {
				t.Helper()
				if err := db.Exec(sql, args...).Error; err != nil {
					t.Fatal(err)
				}
			}
			tenant, target := int64(7), entry.ID
			var want error
			switch name {
			case "prose_edit":
				mustExec("UPDATE catalog.entries SET business_name = ?, business_description = ?, version = version + 1 WHERE id = ?", "业务名称", "补充说明", entry.ID)
			case "same_user_new_relation":
				// Delete/re-add simulates a transfer away and back. The new UUID
				// must not revive the earlier responsibility relation's decision.
				mustExec("UPDATE catalog.responsibilities SET id = ? WHERE id = ?", uuid.New(), original.ResponsibilityID)
				want = ErrSharingConfirmationForbidden
			case "different_owner":
				mustExec("UPDATE catalog.responsibilities SET subject_id = 41 WHERE id = ?", original.ResponsibilityID)
				want = ErrSharingConfirmationForbidden
			case "inactive_owner":
				mustExec("UPDATE catalog.responsibilities SET status = 'needs_transfer' WHERE id = ?", original.ResponsibilityID)
				want = ErrSharingConfirmationForbidden
			case "no_owner":
				mustExec("DELETE FROM catalog.responsibilities WHERE id = ?", original.ResponsibilityID)
				want = ErrSharingConfirmationForbidden
			case "source_version":
				mustExec("UPDATE catalog.source_bindings SET source_version = '00000000000000000002' WHERE id = ?", original.SourceBindingID)
				want = ErrSharingTargetUnsupported
			case "rebound_source":
				mustExec("UPDATE catalog.source_bindings SET id = ? WHERE id = ?", uuid.New(), original.SourceBindingID)
				want = ErrSharingTargetUnsupported
			case "missing_source":
				mustExec("UPDATE catalog.source_bindings SET source_status = 'missing' WHERE id = ?", original.SourceBindingID)
				want = ErrSharingTargetUnsupported
			case "not_current_source":
				mustExec("UPDATE catalog.source_bindings SET is_current = false WHERE id = ?", original.SourceBindingID)
				want = ErrSharingTargetUnsupported
			case "other_module":
				mustExec("UPDATE catalog.source_bindings SET source_module = 'model' WHERE id = ?", original.SourceBindingID)
				want = ErrSharingTargetUnsupported
			case "deprecated":
				mustExec("UPDATE catalog.entries SET governance_status = 'deprecated' WHERE id = ?", entry.ID)
				want = ErrSharingTargetUnsupported
			case "merged":
				mustExec("UPDATE catalog.entries SET entry_status = 'merged' WHERE id = ?", entry.ID)
				want = ErrSharingTargetUnsupported
			case "other_type":
				mustExec("UPDATE catalog.entries SET entry_type = 'business_entity' WHERE id = ?", entry.ID)
				want = ErrSharingTargetUnsupported
			case "version_regression":
				mustExec("UPDATE catalog.entries SET version = 1 WHERE id = ?", entry.ID)
				want = ErrSharingTargetUnsupported
			case "expired":
				// Invalid historical fixture only; production history is immutable.
				mustExec("UPDATE catalog.sharing_decisions SET expires_at = ? WHERE id = ?", time.Now().Add(-time.Hour), original.ID)
				want = ErrInvalidEntryUpdate
			case "foreign_tenant":
				tenant, want = 8, ErrEntryNotFound
			case "other_entry":
				target, want = uuid.New(), ErrEntryNotFound
			}
			var before models.Entry
			if err := db.First(&before, "id = ?", entry.ID).Error; err != nil {
				t.Fatal(err)
			}
			err = db.Transaction(func(tx *gorm.DB) error {
				row, err := lockCurrentSharingDecisionBasis(context.Background(), tx, tenant, target, original.ID)
				if !errors.Is(err, want) || (want != nil && row != nil) {
					t.Fatalf("basis=%+v err=%v want=%v", row, err, want)
				}
				if want == nil && (row.ID != original.ID || row.EntryVersion != original.EntryVersion || !row.CreatedAt.Equal(original.CreatedAt)) {
					t.Fatalf("basis rewrote history: %+v", row)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			var after models.Entry
			if err := db.First(&after, "id = ?", entry.ID).Error; err != nil || before.Version != after.Version || !before.UpdatedAt.Equal(after.UpdatedAt) {
				t.Fatalf("basis changed aggregate: %+v err=%v", after, err)
			}
			for _, table := range []any{&models.SharingDecision{}, &models.AuditEvent{}, &models.ProjectionTask{}} {
				var count int64
				if err := db.Model(table).Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("basis created history/side effects: %T count=%d err=%v", table, count, err)
				}
			}
			if resolver.calls != 1 {
				t.Fatalf("local basis performed remote IO: calls=%d", resolver.calls)
			}
		})
	}
}

func TestCurrentSharingDecisionBasisRequiresOwnerTransaction(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, input := seedSharingEntry(t, db)
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
	row, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range []*gorm.DB{nil, db} {
		if _, err := lockCurrentSharingDecisionBasis(context.Background(), connection, 7, entry.ID, row.ID); !errors.Is(err, ErrInvalidEntryUpdate) {
			t.Fatalf("no transaction accepted: %v", err)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, id := range []uuid.UUID{uuid.Nil, uuid.New()} {
			_, err := lockCurrentSharingDecisionBasis(context.Background(), tx, 7, entry.ID, id)
			want := ErrEntryNotFound
			if id == uuid.Nil {
				want = ErrInvalidEntryUpdate
			}
			if !errors.Is(err, want) {
				t.Fatalf("invalid/missing decision: %v", err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
