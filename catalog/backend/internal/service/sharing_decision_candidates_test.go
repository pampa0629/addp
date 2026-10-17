package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

type candidateLabelResolver struct {
	refs              []commonClient.SystemCatalogReference
	before            func()
	err               error
	missing, mismatch bool
}

func (r *candidateLabelResolver) ResolveSystemReferences(_ context.Context, tenantID int64, refs []commonClient.SystemCatalogReference) ([]commonClient.SystemCatalogReferenceResolution, error) {
	if tenantID != 7 {
		return nil, errors.New("incorrect label tenant")
	}
	r.refs = append([]commonClient.SystemCatalogReference(nil), refs...)
	if r.before != nil {
		r.before()
	}
	if r.err != nil {
		return nil, r.err
	}
	rows := make([]commonClient.SystemCatalogReferenceResolution, len(refs))
	for i, ref := range refs {
		rows[i] = commonClient.SystemCatalogReferenceResolution{SubjectType: ref.SubjectType, ID: ref.ID, Found: !r.missing, Referenceable: !r.missing, Name: "Recipient", Code: "recipient", Status: "active"}
		if r.mismatch {
			rows[i].ID++
		}
	}
	return rows, nil
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
	labels := &candidateLabelResolver{}
	s.system = labels
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
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != decision.ID || rows[0].ConfirmedBy == p || rows[0].RecipientName != "Recipient" || rows[0].RecipientLabelStatus != "resolved" || len(labels.refs) != 1 || labels.refs[0].ID != input.RecipientID {
		t.Fatalf("distinct handler list=%+v total=%d err=%v", rows, total, err)
	}
	t.Run("new command cannot duplicate a current approval or mutate its history", func(t *testing.T) {
		rollback := errors.New("rollback new approval fixture")
		if err := db.Transaction(func(tx *gorm.DB) error {
			local := *s
			local.db = tx
			local.system = &fakeSystemReferenceResolver{}
			duplicate := input
			duplicate.DecisionID, duplicate.Version, duplicate.Reason = uuid.New(), decision.EntryVersion, "Another click"
			if _, created, err := local.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, duplicate, sharingAuth()); !errors.Is(err, ErrSharingDecisionExists) || created {
				t.Fatalf("duplicate created=%v err=%v", created, err)
			}
			duplicate.Version = input.Version
			if _, created, err := local.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, duplicate, sharingAuth()); !errors.Is(err, ErrSharingDecisionExists) || created {
				t.Fatalf("concurrent stale duplicate created=%v err=%v", created, err)
			}
			if restored, created, err := local.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth()); err != nil || created || restored.ID != decision.ID {
				t.Fatalf("same ID retry changed: %v %v", created, err)
			}
			var count int64
			if err := tx.Model(&models.SharingDecision{}).Where("catalog_entry_id = ?", entry.ID).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("history mutated=%d %v", count, err)
			}
			changedExpiry := duplicate
			expires := input.ExpiresAt.Add(time.Hour)
			changedExpiry.DecisionID, changedExpiry.Version, changedExpiry.ExpiresAt = uuid.New(), decision.EntryVersion, &expires
			if _, created, err := local.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, changedExpiry, sharingAuth()); err != nil || !created {
				t.Fatalf("different expiry incorrectly blocked: %v %v", created, err)
			}
			rejoined := sharingAuth()
			membershipID := "2"
			rejoined.Context.TenantMembershipID = &membershipID
			newMembership := input
			newMembership.DecisionID, newMembership.Version = uuid.New(), decision.EntryVersion+1
			if _, created, err := local.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, newMembership, rejoined); err != nil || !created {
				t.Fatalf("old membership blocked a fresh approval: %v %v", created, err)
			}
			return rollback
		}); !errors.Is(err, rollback) {
			t.Fatal(err)
		}
	})
	t.Run("equivalent approvals collapse before pagination but different expiry remains", func(t *testing.T) {
		rollback := errors.New("rollback duplicate approval fixture")
		if err := db.Transaction(func(tx *gorm.DB) error {
			local := *s
			local.db = tx
			var original models.SharingDecision
			if err := tx.Where("id = ?", decision.ID).Take(&original).Error; err != nil {
				t.Fatal(err)
			}
			latest := original
			latest.ID, latest.EntryVersion, latest.CreatedAt = uuid.New(), original.EntryVersion+1, time.Now().UTC()
			if tx.Dialector.Name() == "postgres" {
				if err := tx.Raw("SELECT clock_timestamp()").Scan(&latest.CreatedAt).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Create(&latest).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("version", latest.EntryVersion).Error; err != nil {
				t.Fatal(err)
			}
			page, count, err := local.ListSharingDecisionCandidates(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "addp_at_handler", 1, 1)
			if err != nil || count != 1 || len(page) != 1 || page[0].ID != latest.ID {
				t.Fatalf("deduplicated page=%+v total=%d err=%v", page, count, err)
			}
			different := latest
			expiry := latest.ExpiresAt.Add(time.Hour)
			different.ID, different.EntryVersion, different.CreatedAt, different.ExpiresAt = uuid.New(), latest.EntryVersion+1, time.Now().UTC(), &expiry
			if tx.Dialector.Name() == "postgres" {
				if err := tx.Raw("SELECT clock_timestamp()").Scan(&different.CreatedAt).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Create(&different).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("version", different.EntryVersion).Error; err != nil {
				t.Fatal(err)
			}
			page, count, err = local.ListSharingDecisionCandidates(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "addp_at_handler", 2, 1)
			if err != nil || count != 2 || len(page) != 1 || page[0].ID != latest.ID {
				t.Fatalf("distinct expiry lost=%+v total=%d err=%v", page, count, err)
			}
			otherBoundary := different
			otherBoundary.ID, otherBoundary.EntryVersion, otherBoundary.BoundaryHash, otherBoundary.ExpiresAt = uuid.New(), different.EntryVersion+1, strings.Repeat("c", 64), latest.ExpiresAt
			if tx.Dialector.Name() == "postgres" {
				if err := tx.Raw("SELECT clock_timestamp()").Scan(&otherBoundary.CreatedAt).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Create(&otherBoundary).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("version", otherBoundary.EntryVersion).Error; err != nil {
				t.Fatal(err)
			}
			_, count, err = local.ListSharingDecisionCandidates(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "addp_at_handler", 1, 10)
			if err != nil || count != 3 {
				t.Fatalf("different boundary collapsed: %d %v", count, err)
			}
			return rollback
		}); !errors.Is(err, rollback) {
			t.Fatal(err)
		}
	})
	body, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	if fields["item_fingerprint"] != decision.ItemFingerprint || fields["boundary_hash"] != decision.BoundaryHash {
		t.Fatalf("candidate lost the immutable approved DataItem boundary: %s", body)
	}
	for _, field := range []string{"reason", "responsibility_id", "source_binding_id", "authorization_version", "confirmer_membership_id", "tenant_id"} {
		if _, present := fields[field]; present {
			t.Fatalf("history field %s leaked", field)
		}
	}
	labels.missing = true
	rows, _, err = list(EntryAccess{Inventory: true})
	if err != nil || len(rows) != 1 || rows[0].RecipientLabelStatus != "not_found" || rows[0].RecipientName != "" || rows[0].RecipientCode != "" {
		t.Fatalf("missing recipient invented a label: %+v %v", rows, err)
	}
	labels.missing, labels.mismatch = false, true
	if _, _, err := list(EntryAccess{Inventory: true}); !errors.Is(err, ErrReferenceValidationUnavailable) {
		t.Fatalf("misbound label accepted: %v", err)
	}
	labels.mismatch, labels.err = false, errors.New("offline")
	if _, _, err := list(EntryAccess{Inventory: true}); !errors.Is(err, ErrReferenceValidationUnavailable) {
		t.Fatalf("offline label fallback: %v", err)
	}
	labels.err = nil
	t.Run("source changes during label resolution", func(t *testing.T) {
		rollback := errors.New("rollback label fixture")
		err := db.Transaction(func(tx *gorm.DB) error {
			local := *s
			local.db = tx
			labels.before = func() {
				if err := tx.Exec("UPDATE catalog.source_bindings SET source_version='00000000000000000002' WHERE catalog_entry_id=?", entry.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			defer func() { labels.before = nil }()
			if _, _, err := local.ListSharingDecisionCandidates(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "addp_at_handler", 1, 10); !errors.Is(err, ErrSharingDecisionConflict) {
				t.Fatalf("stale label returned: %v", err)
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
	})
	t.Run("token expires during label resolution", func(t *testing.T) {
		expiring := auth
		expiring.Token.ExpiresAt = time.Now().UTC().Add(200 * time.Millisecond)
		labels.before = func() { waitSharingTestExpiry(t, db, expiring.Token.ExpiresAt) }
		defer func() { labels.before = nil }()
		if _, _, err := s.ListSharingDecisionCandidates(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, expiring, "addp_at_handler", 1, 10); !errors.Is(err, ErrSharingHandlingForbidden) {
			t.Fatalf("expired token leaked labels: %v", err)
		}
	})
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
		reader.before = func() { waitSharingTestExpiry(t, db, expires) }
		defer func() { reader.before = nil }()
		rows, total, err := s.ListSharingDecisionCandidates(context.Background(), 7, EntryAccess{Inventory: true}, shortEntry.ID, auth, "addp_at_handler", 1, 10)
		if err != nil || total != 0 || len(rows) != 0 {
			t.Fatalf("expired decision leaked: %+v/%d %v", rows, total, err)
		}
	})
	t.Run("human token expires during remote qualification", func(t *testing.T) {
		expiring := auth
		expiring.Token.ExpiresAt = time.Now().UTC().Add(200 * time.Millisecond)
		reader.before = func() { waitSharingTestExpiry(t, db, expiring.Token.ExpiresAt) }
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
		reader.before = func() { waitSharingTestExpiry(t, db, expires) }
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
