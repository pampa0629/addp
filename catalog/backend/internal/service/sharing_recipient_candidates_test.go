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
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type recipientCandidateResolver struct {
	calls  int
	kind   string
	before func()
	result *ReferenceCandidateList
	err    error
}

func (r *recipientCandidateResolver) ListReferenceCandidates(_ context.Context, tenant int64, kind, search string, page, size int) (*ReferenceCandidateList, error) {
	r.calls++
	expectedKind := r.kind
	if expectedKind == "" {
		expectedKind = "project_group"
	}
	if tenant != 7 || kind != expectedKind || search != "delivery" || page != 1 || size != 20 {
		return nil, errors.New("incorrect scoped request")
	}
	if r.before != nil {
		r.before()
	}
	return r.result, r.err
}

func exerciseSharingRecipients(t *testing.T, db *gorm.DB) {
	t.Helper()
	entry, _ := seedSharingEntry(t, db)
	resolver := &recipientCandidateResolver{result: &ReferenceCandidateList{Data: []ReferenceCandidate{{ReferenceType: "project_group", ID: "50", Name: "Delivery", Code: "delivery", Status: "active"}}, Total: 1, Page: 1, PageSize: 20, TotalPages: 1}}
	s := NewEntryService(db, nil, nil).WithReferenceCandidateResolvers(nil, resolver)
	auth := authtest.NewTenantUserAuthContext("7", "40", []string{"catalog.entry.read", "catalog.inventory.read", "catalog.sharing_decision.create"})
	list := func(a authorization.AuthContext, access EntryAccess) ([]SharingRecipientCandidate, int64, error) {
		return s.ListSharingRecipientCandidates(context.Background(), 7, access, entry.ID, a, "project_group", " delivery ", 1, 20)
	}
	rows, total, err := list(auth, EntryAccess{Inventory: true})
	if err != nil || len(rows) != 1 || total != 1 || rows[0].ID != "50" || len(auth.Organization.ProjectGroups) != 0 {
		t.Fatalf("nonmember recipient=%+v total=%d err=%v", rows, total, err)
	}
	body, _ := json.Marshal(rows[0])
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	if len(fields) != 5 {
		t.Fatalf("nonminimal recipient: %s", body)
	}
	groupResult := resolver.result
	resolver.kind = "user"
	resolver.result = &ReferenceCandidateList{Data: []ReferenceCandidate{{ReferenceType: "user", ID: "41", Name: "Delivery User", Code: "delivery_user", Status: "active"}}, Total: 1, Page: 1, PageSize: 20, TotalPages: 1}
	rows, total, err = s.ListSharingRecipientCandidates(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "user", "delivery", 1, 20)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].RecipientType != "user" || rows[0].ID != "41" {
		t.Fatalf("user recipients=%+v total=%d err=%v", rows, total, err)
	}
	resolver.kind, resolver.result = "", groupResult
	if _, err := s.ListReferenceCandidates(context.Background(), 7, "project_group", "", 1, 20); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("generic read enumerated groups: %v", err)
	}
	for _, test := range []struct {
		name   string
		auth   authorization.AuthContext
		access EntryAccess
		want   error
	}{
		{"not owner", authtest.NewTenantUserAuthContext("7", "41", []string{"catalog.entry.read", "catalog.inventory.read", "catalog.sharing_decision.create"}), EntryAccess{Inventory: true}, ErrSharingConfirmationForbidden},
		{"maintenance only", authtest.NewTenantUserAuthContext("7", "40", []string{"catalog.entry.read", "catalog.inventory.read", "catalog.entry.update"}), EntryAccess{Inventory: true}, ErrSharingConfirmationForbidden},
		{"confirmation without read", authtest.NewTenantUserAuthContext("7", "40", []string{"catalog.inventory.read", "catalog.sharing_decision.create"}), EntryAccess{Inventory: true}, ErrSharingConfirmationForbidden},
		{"wrong tenant", authtest.NewTenantUserAuthContext("8", "40", []string{"catalog.entry.read", "catalog.inventory.read", "catalog.sharing_decision.create"}), EntryAccess{Inventory: true}, ErrSharingConfirmationForbidden},
		{"invisible", auth, EntryAccess{}, ErrEntryNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := resolver.calls
			if _, _, err := list(test.auth, test.access); !errors.Is(err, test.want) || resolver.calls != before {
				t.Fatalf("unqualified query reached System: %v", err)
			}
		})
	}
	for _, mutate := range []func(*ReferenceCandidateList){
		func(r *ReferenceCandidateList) { r.Data[0].ID = "050" },
		func(r *ReferenceCandidateList) { r.Data[0].Status = "closed" },
		func(r *ReferenceCandidateList) { r.Data[0].ReferenceType = "user" },
		func(r *ReferenceCandidateList) { r.Data = append(r.Data, r.Data[0]) },
		func(r *ReferenceCandidateList) { r.TotalPages = 2 },
		func(r *ReferenceCandidateList) { r.Total = 0; r.TotalPages = 0 },
	} {
		original := *resolver.result
		original.Data = append([]ReferenceCandidate(nil), resolver.result.Data...)
		mutate(resolver.result)
		if _, _, err := list(auth, EntryAccess{Inventory: true}); !errors.Is(err, ErrReferenceValidationUnavailable) {
			t.Fatalf("malformed upstream accepted: %v", err)
		}
		resolver.result = &original
	}
	resolver.err = errors.New("offline")
	if _, _, err := list(auth, EntryAccess{Inventory: true}); !errors.Is(err, ErrReferenceValidationUnavailable) {
		t.Fatalf("offline fallback: %v", err)
	}
	resolver.err = nil
	for _, test := range []struct {
		name, statement string
		want            error
	}{
		{"transfer", "UPDATE catalog.responsibilities SET status='needs_transfer' WHERE catalog_entry_id=?", ErrSharingConfirmationForbidden},
		{"source changed", "UPDATE catalog.source_bindings SET source_version='00000000000000000002' WHERE catalog_entry_id=?", ErrSharingDecisionConflict},
		{"source missing", "UPDATE catalog.source_bindings SET source_status='missing' WHERE catalog_entry_id=?", ErrSharingTargetUnsupported},
		{"deprecated", "UPDATE catalog.entries SET governance_status='deprecated' WHERE id=?", ErrSharingTargetUnsupported},
		{"same account reappointed", "DELETE FROM catalog.responsibilities WHERE catalog_entry_id=?", ErrSharingDecisionConflict},
	} {
		t.Run(test.name+" during query", func(t *testing.T) {
			rollback := errors.New("rollback recipient fixture")
			err := db.Transaction(func(tx *gorm.DB) error {
				local := *s
				local.db = tx
				resolver.before = func() {
					if err := tx.Exec(test.statement, entry.ID).Error; err != nil {
						t.Fatal(err)
					}
					if test.name == "same account reappointed" {
						if err := tx.Create(&models.Responsibility{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, Role: models.ResponsibilityRoleBusinessOwner, SubjectType: "user", SubjectID: 40, Status: models.ResponsibilityStatusActive, ObservedSnapshot: map[string]interface{}{}, VerifiedAt: time.Now()}).Error; err != nil {
							t.Fatal(err)
						}
					}
				}
				defer func() { resolver.before = nil }()
				if _, _, err := local.ListSharingRecipientCandidates(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "project_group", "delivery", 1, 20); !errors.Is(err, test.want) {
					t.Fatalf("stale recipients returned: %v", err)
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal(err)
			}
		})
	}
	t.Run("token expires during query", func(t *testing.T) {
		expiring := auth
		expiring.Token.ExpiresAt = time.Now().UTC().Add(100 * time.Millisecond)
		resolver.before = func() { waitSharingTestExpiry(t, db, expiring.Token.ExpiresAt) }
		defer func() { resolver.before = nil }()
		if _, _, err := list(expiring, EntryAccess{Inventory: true}); !errors.Is(err, ErrSharingConfirmationForbidden) {
			t.Fatalf("expired token accepted: %v", err)
		}
	})
	t.Run("inventory permission expires during query", func(t *testing.T) {
		expiring := auth
		expires := time.Now().UTC().Add(200 * time.Millisecond)
		expiring.Authorization.RoleAssignments = append([]authorization.RoleAssignment(nil), auth.Authorization.RoleAssignments...)
		inventory := expiring.Authorization.RoleAssignments[0]
		inventory.AssignmentID, inventory.RoleKey = "2", "custom.inventory"
		inventory.Permissions, inventory.ValidUntil = []string{"catalog.inventory.read"}, &expires
		expiring.Authorization.RoleAssignments[0].Permissions = []string{"catalog.entry.read", "catalog.sharing_decision.create"}
		expiring.Authorization.RoleAssignments = append(expiring.Authorization.RoleAssignments, inventory)
		resolver.before = func() { waitSharingTestExpiry(t, db, expires) }
		defer func() { resolver.before = nil }()
		if _, _, err := list(expiring, EntryAccess{Inventory: true}); !errors.Is(err, ErrEntryNotFound) {
			t.Fatalf("expired inventory permission kept visibility: %v", err)
		}
	})
	var count int64
	for _, model := range []any{&models.SharingDecision{}, &models.FulfillmentCheck{}} {
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("read created decision or protection: %d %v", count, err)
		}
	}
}

// PostgreSQL and the macOS host have independent clocks. Exercise an actually
// expired basis instead of assuming their deadlines differ by less than 1 ms.
func waitSharingTestExpiry(t *testing.T, db *gorm.DB, expires time.Time) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		now := time.Now().UTC()
		if db.Dialector.Name() == "postgres" {
			if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
				t.Fatal(err)
			}
		}
		if now.After(expires) && time.Now().UTC().After(expires) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expiry not reached: database=%s host=%s expires=%s", now, time.Now().UTC(), expires)
		}
		<-time.After(5 * time.Millisecond)
	}
}

func TestSharingRecipientsRequireCurrentOwnerAndIndependentPermission(t *testing.T) {
	exerciseSharingRecipients(t, openSharingPreparationTestDB(t))
}
