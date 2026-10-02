package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type candidateScopeReader struct {
	calls  int
	before func()
	err    error
	scope  *authorization.EngineAccessHandlingScope
}

func (r *candidateScopeReader) GetEngineAccessHandlingScope(_ context.Context, engineID int64, token string) (*authorization.EngineAccessHandlingScope, error) {
	r.calls++
	if engineID != 12 || token != "addp_at_handler" {
		return nil, errors.New("wrong engine or User token")
	}
	if r.before != nil {
		r.before()
	}
	return r.scope, r.err
}

func exerciseSharingCandidates(t *testing.T, db *gorm.DB) {
	t.Helper()
	entry, input := seedSharingEntry(t, db)
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
	decision, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
	if err != nil {
		t.Fatal(err)
	}
	// A distinct human handler has no business-confirmation Permission.
	auth := authtest.NewTenantUserAuthContext("7", "41", []string{"catalog.entry.read", "catalog.inventory.read", sourceHandlingPermission})
	p, m, v, err := sharingUserProvenance(auth, 7, "catalog.entry.read", sourceHandlingPermission)
	if err != nil {
		t.Fatal(err)
	}
	reader := &candidateScopeReader{scope: &authorization.EngineAccessHandlingScope{TenantID: 7, EngineID: 12,
		Operator: authorization.SharingFulfillmentOperator{PrincipalID: p, MembershipID: m, AuthorizationVersion: v}, VerifiedAt: time.Now().UTC()}}
	s.WithSharingHandlingScopeReader(reader)
	list := func(access EntryAccess) ([]SharingDecisionCandidate, int64, error) {
		return s.ListSharingDecisionCandidates(context.Background(), 7, access, entry.ID, auth, "addp_at_handler", 1, 10)
	}
	rows, total, err := list(EntryAccess{Inventory: true})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != decision.ID || rows[0].ConfirmedBy == p {
		t.Fatalf("distinct handler list=%+v total=%d err=%v", rows, total, err)
	}
	body, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	for _, field := range []string{"reason", "responsibility_id", "source_binding_id", "authorization_version", "confirmer_membership_id", "tenant_id"} {
		if _, present := fields[field]; present {
			t.Fatalf("history field %s leaked", field)
		}
	}
	if _, err := s.GetSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, decision.ID, auth); !errors.Is(err, ErrSharingConfirmationForbidden) {
		t.Fatalf("handler acquired own-history confirmation permission: %v", err)
	}
	if _, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, auth); !errors.Is(err, ErrSharingConfirmationForbidden) {
		t.Fatalf("handler acquired confirmation: %v", err)
	}
	for _, permissions := range [][]string{{"catalog.entry.read", "catalog.entry.update"}, {"catalog.entry.read", "catalog.sharing_decision.create"}, {sourceHandlingPermission}} {
		denied := authtest.NewTenantUserAuthContext("7", "41", permissions)
		before := reader.calls
		if _, _, err := s.ListSharingDecisionCandidates(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, denied, "addp_at_handler", 1, 10); !errors.Is(err, ErrSharingHandlingForbidden) || reader.calls != before {
			t.Fatalf("permission bypass: %v", err)
		}
	}
	before := reader.calls
	if _, _, err := list(EntryAccess{}); !errors.Is(err, ErrEntryNotFound) || reader.calls != before {
		t.Fatalf("invisible entry contacted System: %v", err)
	}
	for _, status := range []int{401, 403, 404, 500} {
		reader.err = &commonClient.SystemAPIError{StatusCode: status}
		want := ErrSharingHandlingForbidden
		if status == 500 {
			want = ErrReferenceValidationUnavailable
		}
		if _, _, err := list(EntryAccess{Inventory: true}); !errors.Is(err, want) {
			t.Fatalf("System %d was not fail-closed: %v", status, err)
		}
	}
	reader.err = nil
	reader.scope.Operator.PrincipalID++
	if _, _, err := list(EntryAccess{Inventory: true}); !errors.Is(err, ErrReferenceValidationUnavailable) {
		t.Fatalf("forged operator scope: %v", err)
	}
	reader.scope.Operator.PrincipalID--
	for _, fixture := range []struct {
		name, statement string
		id              uuid.UUID
	}{
		{"responsibility transfer", "UPDATE catalog.responsibilities SET status='needs_transfer' WHERE id=?", decision.ResponsibilityID},
		{"same account reappointed", "DELETE FROM catalog.responsibilities WHERE id=?", decision.ResponsibilityID},
		{"source change", "UPDATE catalog.source_bindings SET source_version='00000000000000000002' WHERE id=?", decision.SourceBindingID},
		{"deprecation", "UPDATE catalog.entries SET governance_status='deprecated' WHERE id=?", entry.ID},
		{"entry hidden", "UPDATE catalog.entries SET visibility='department' WHERE id=?", entry.ID},
	} {
		t.Run(fixture.name+" during remote qualification", func(t *testing.T) {
			rollback := errors.New("rollback candidate race")
			err := db.Transaction(func(tx *gorm.DB) error {
				local := *s
				local.db = tx
				reader.before = func() {
					if err := tx.Exec(fixture.statement, fixture.id).Error; err != nil {
						t.Fatal(err)
					}
					if fixture.name == "same account reappointed" {
						if err := tx.Create(&models.Responsibility{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID,
							Role: models.ResponsibilityRoleBusinessOwner, SubjectType: "user", SubjectID: 40,
							Status: models.ResponsibilityStatusActive, ObservedSnapshot: map[string]interface{}{}, VerifiedAt: time.Now()}).Error; err != nil {
							t.Fatal(err)
						}
					}
				}
				defer func() { reader.before = nil }()
				access := EntryAccess{Inventory: true}
				if fixture.name == "entry hidden" {
					if err := tx.Exec("UPDATE catalog.entries SET governance_status='curated', visibility='tenant' WHERE id=?", entry.ID).Error; err != nil {
						return err
					}
					access = EntryAccess{}
				}
				rows, total, err := local.ListSharingDecisionCandidates(context.Background(), 7, access, entry.ID, auth, "addp_at_handler", 1, 10)
				if fixture.name == "entry hidden" {
					if !errors.Is(err, ErrEntryNotFound) {
						t.Fatalf("visibility race: %v", err)
					}
				} else if err != nil || total != 0 || len(rows) != 0 {
					t.Fatalf("stale candidate=%+v/%d err=%v", rows, total, err)
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal(err)
			}
		})
	}
	t.Run("decision expires during remote qualification", func(t *testing.T) {
		shortEntry, shortInput := seedSharingEntry(t, db)
		expires := time.Now().UTC().Add(time.Second)
		shortInput.ExpiresAt = &expires
		if _, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, shortEntry.ID, shortInput, sharingAuth()); err != nil {
			t.Fatal(err)
		}
		reader.before = func() { <-time.After(time.Until(expires) + time.Millisecond) }
		defer func() { reader.before = nil }()
		rows, total, err := s.ListSharingDecisionCandidates(context.Background(), 7, EntryAccess{Inventory: true}, shortEntry.ID, auth, "addp_at_handler", 1, 10)
		if err != nil || total != 0 || len(rows) != 0 {
			t.Fatalf("expired decision leaked: %+v/%d %v", rows, total, err)
		}
	})
	t.Run("human token expires during remote qualification", func(t *testing.T) {
		expiring := auth
		expiring.Token.ExpiresAt = time.Now().UTC().Add(200 * time.Millisecond)
		reader.before = func() { <-time.After(time.Until(expiring.Token.ExpiresAt) + time.Millisecond) }
		defer func() { reader.before = nil }()
		if _, _, err := s.ListSharingDecisionCandidates(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, expiring, "addp_at_handler", 1, 10); !errors.Is(err, ErrSharingHandlingForbidden) {
			t.Fatalf("expired handler token accepted: %v", err)
		}
	})
	t.Run("inventory permission expires during remote qualification", func(t *testing.T) {
		expiring := auth
		expires := time.Now().UTC().Add(200 * time.Millisecond)
		expiring.Authorization.RoleAssignments = append([]authorization.RoleAssignment(nil), auth.Authorization.RoleAssignments...)
		inventory := expiring.Authorization.RoleAssignments[0]
		inventory.AssignmentID, inventory.RoleKey = "2", "custom.inventory"
		inventory.Permissions, inventory.ValidUntil = []string{"catalog.inventory.read"}, &expires
		expiring.Authorization.RoleAssignments[0].Permissions = []string{"catalog.entry.read", sourceHandlingPermission}
		expiring.Authorization.RoleAssignments = append(expiring.Authorization.RoleAssignments, inventory)
		reader.before = func() { <-time.After(time.Until(expires) + time.Millisecond) }
		defer func() { reader.before = nil }()
		if _, _, err := s.ListSharingDecisionCandidates(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, expiring, "addp_at_handler", 1, 10); !errors.Is(err, ErrEntryNotFound) {
			t.Fatalf("expired inventory permission kept visibility: %v", err)
		}
	})
	var pending int64
	if err := db.Model(&models.FulfillmentCheck{}).Count(&pending).Error; err != nil || pending != 0 {
		t.Fatalf("candidate froze resource: %d %v", pending, err)
	}
}

func TestSharingCandidatesKeepHumanRolesVisibilityAndCurrentBasisSeparate(t *testing.T) {
	exerciseSharingCandidates(t, openSharingPreparationTestDB(t))
}
