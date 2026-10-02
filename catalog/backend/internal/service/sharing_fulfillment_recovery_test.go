package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fakeSharingFulfillmentAuthority struct {
	readCalls, closeCalls int
	read                  func(int64, uuid.UUID, sharingFulfillmentBinding) (*sharingFulfillmentResolution, error)
	close                 func(int64, uuid.UUID, sharingFulfillmentBinding) (*sharingFulfillmentResolution, error)
}

func (a *fakeSharingFulfillmentAuthority) Read(_ context.Context, tenant int64, request uuid.UUID, binding sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
	a.readCalls++
	return a.read(tenant, request, binding)
}
func (a *fakeSharingFulfillmentAuthority) Close(_ context.Context, tenant int64, request uuid.UUID, binding sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
	a.closeCalls++
	return a.close(tenant, request, binding)
}

func seedSharingRecovery(t *testing.T) (*gorm.DB, models.Entry, *models.FulfillmentCheck, sharingFulfillmentBinding) {
	t.Helper()
	db := openSharingPreparationTestDB(t)
	entry, input := seedSharingEntry(t, db)
	input.ExpiryMode, input.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
	decision, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
	if err != nil {
		t.Fatal(err)
	}
	request := preparationFixture(t, decision)
	// Exercise IDs larger than JavaScript's exact integer range.
	request.Operator.AuthorizationVersion = 9007199254740993
	var check *models.FulfillmentCheck
	err = db.Transaction(func(tx *gorm.DB) error {
		var err error
		check, _, err = prepareSharingFulfillment(context.Background(), tx, 7, entry.ID, decision.ID, request)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := decodeSharingFulfillmentBinding(check.RequestBinding)
	if err != nil {
		t.Fatal(err)
	}
	return db, entry, check, binding
}

func TestSharingFulfillmentRecoveryUsesOriginalResultAndPreservesResolution(t *testing.T) {
	for _, fixture := range []struct {
		name, outcome string
		missing       bool
	}{
		{"read accepted", "accepted", false}, {"read closed", "closed", false},
		{"close before delayed accept", "closed", true}, {"accept before close", "accepted", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			db, entry, check, binding := seedSharingRecovery(t)
			authority := &fakeSharingFulfillmentAuthority{}
			result := func(tenant int64, request uuid.UUID, actual sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
				if !equalSharingFulfillmentBinding(binding, actual) {
					t.Fatal("recovery changed original binding")
				}
				// A second connection/transaction can write while the remote call
				// runs. Local history has been committed; no owner lock is held.
				if err := db.Transaction(func(tx *gorm.DB) error {
					return tx.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("business_name", "Concurrent prose").Error
				}); err != nil {
					t.Fatal(err)
				}
				return &sharingFulfillmentResolution{request, tenant, actual, fixture.outcome}, nil
			}
			authority.read, authority.close = result, result
			if fixture.missing {
				authority.read = func(_ int64, _ uuid.UUID, actual sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
					// Adapter mutation must not change the parameters later closed.
					actual.Path.Segments[0].Name = "mutated"
					return nil, errSharingFulfillmentNotFound
				}
			}
			resolved, err := reconcileSharingFulfillment(context.Background(), db, 7, entry.ID, check.RequestID, authority)
			if err != nil || resolved.ResolvedAt == nil || resolved.ResolvedAt.Before(check.CreatedAt) ||
				authority.readCalls != 1 || authority.closeCalls != boolCount(fixture.missing) {
				t.Fatalf("resolved=%+v calls=%d/%d err=%v", resolved, authority.readCalls, authority.closeCalls, err)
			}
			// Historical completion does not require current approval identity or
			// source validity. SQLite changes below are only negative fixtures.
			if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("governance_status", models.GovernanceStatusDeprecated).Error; err != nil {
				t.Fatal(err)
			}
			retry, err := reconcileSharingFulfillment(context.Background(), db, 7, entry.ID, check.RequestID, authority)
			if err != nil || !retry.ResolvedAt.Equal(*resolved.ResolvedAt) || authority.readCalls != 1 ||
				string(retry.RequestBinding) != string(check.RequestBinding) || !retry.CreatedAt.Equal(check.CreatedAt) {
				t.Fatalf("retry changed history or repeated remote call: %+v err=%v", retry, err)
			}
		})
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestSharingFulfillmentBindingUsesExactExpiryNotTimezoneEncoding(t *testing.T) {
	_, _, _, binding := seedSharingRecovery(t)
	instant := time.Date(2030, 1, 2, 3, 4, 5, 123456000, time.UTC)
	binding.ExpiryMode, binding.ExpiresAt = authorization.SharingExpiryAtTime, &instant
	other := binding
	local := instant.In(time.FixedZone("test offset", 8*60*60))
	other.ExpiresAt = &local
	if !equalSharingFulfillmentBinding(binding, other) {
		t.Fatal("timezone encoding changed the same instant")
	}
	raw, err := json.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := decodeSharingFulfillmentBinding(raw)
	if err != nil || !equalSharingFulfillmentBinding(stored, other) {
		t.Fatalf("decoded history changed expiry: %+v err=%v", stored, err)
	}
	changed := instant.Add(time.Microsecond)
	other.ExpiresAt = &changed
	if equalSharingFulfillmentBinding(binding, other) {
		t.Fatal("different absolute expiry compared equal")
	}
}

func TestSharingFulfillmentRecoveryUnknownOrMismatchedResultStaysPending(t *testing.T) {
	remoteErr := errors.New("authority response lost")
	for _, fixture := range []struct {
		name      string
		missing   bool
		remoteErr error
		mutate    func(*sharingFulfillmentResolution)
	}{
		{name: "read unavailable", remoteErr: remoteErr},
		{name: "close response lost", missing: true, remoteErr: remoteErr},
		{name: "close has no durable result", missing: true, remoteErr: errSharingFulfillmentNotFound},
		{name: "nil result", mutate: func(r *sharingFulfillmentResolution) { r.RequestID = uuid.Nil }},
		{name: "request mismatch", mutate: func(r *sharingFulfillmentResolution) { r.RequestID = uuid.New() }},
		{name: "tenant mismatch", mutate: func(r *sharingFulfillmentResolution) { r.TenantID++ }},
		{name: "binding mismatch", mutate: func(r *sharingFulfillmentResolution) { r.Binding.Operator.AuthorizationVersion-- }},
		{name: "caller mismatch", mutate: func(r *sharingFulfillmentResolution) { r.Binding.CallerPrincipalID++ }},
		{name: "operator mismatch", mutate: func(r *sharingFulfillmentResolution) { r.Binding.Operator.PrincipalID++ }},
		{name: "membership mismatch", mutate: func(r *sharingFulfillmentResolution) { r.Binding.Operator.MembershipID++ }},
		{name: "requirement mismatch", mutate: func(r *sharingFulfillmentResolution) { r.Binding.RequirementVersion++ }},
		{name: "decision mismatch", mutate: func(r *sharingFulfillmentResolution) { r.Binding.DecisionID = uuid.New() }},
		{name: "engine mismatch", mutate: func(r *sharingFulfillmentResolution) { r.Binding.Path.EngineID++ }},
		{name: "path mismatch", mutate: func(r *sharingFulfillmentResolution) {
			r.Binding.Path.Segments[len(r.Binding.Path.Segments)-1].Name = "Orders"
		}},
		{name: "recipient mismatch", mutate: func(r *sharingFulfillmentResolution) { r.Binding.RecipientID++ }},
		{name: "recipient type mismatch", mutate: func(r *sharingFulfillmentResolution) { r.Binding.RecipientType = "project_group" }},
		{name: "action mismatch", mutate: func(r *sharingFulfillmentResolution) { r.Binding.Action = "write" }},
		{name: "expiry mismatch", mutate: func(r *sharingFulfillmentResolution) {
			future := time.Now().UTC().Add(time.Hour)
			r.Binding.ExpiryMode, r.Binding.ExpiresAt = authorization.SharingExpiryAtTime, &future
		}},
		{name: "unknown outcome", mutate: func(r *sharingFulfillmentResolution) { r.Outcome = "granted" }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			db, entry, check, binding := seedSharingRecovery(t)
			authority := &fakeSharingFulfillmentAuthority{}
			response := func(_ int64, _ uuid.UUID, actual sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
				if fixture.remoteErr != nil {
					return nil, fixture.remoteErr
				}
				if !equalSharingFulfillmentBinding(binding, actual) {
					t.Fatal("adapter received changed request")
				}
				r := &sharingFulfillmentResolution{check.RequestID, 7, actual, "accepted"}
				fixture.mutate(r)
				if r.RequestID == uuid.Nil {
					return nil, nil
				}
				return r, nil
			}
			authority.read, authority.close = response, response
			if fixture.missing {
				authority.read = func(int64, uuid.UUID, sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
					return nil, errSharingFulfillmentNotFound
				}
			}
			resolved, err := reconcileSharingFulfillment(context.Background(), db, 7, entry.ID, check.RequestID, authority)
			if err == nil || resolved != nil || authority.closeCalls != boolCount(fixture.missing) {
				t.Fatalf("unsafe recovery: %+v err=%v calls=%d", resolved, err, authority.closeCalls)
			}
			var pending models.FulfillmentCheck
			if err := db.First(&pending, "request_id = ?", check.RequestID).Error; err != nil || pending.ResolvedAt != nil {
				t.Fatalf("unverified result released protection: %+v err=%v", pending, err)
			}
		})
	}
}

func TestSharingFulfillmentRecoveryRejectsTransactionAndForeignHistory(t *testing.T) {
	db, entry, check, _ := seedSharingRecovery(t)
	authority := &fakeSharingFulfillmentAuthority{} // Calls would panic: none are allowed.
	for _, reference := range []struct {
		tenant int64
		entry  uuid.UUID
	}{
		{8, entry.ID}, {7, uuid.New()},
	} {
		if _, err := reconcileSharingFulfillment(context.Background(), db, reference.tenant, reference.entry, check.RequestID, authority); !errors.Is(err, ErrEntryNotFound) {
			t.Fatalf("foreign reference: %v", err)
		}
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := reconcileSharingFulfillment(context.Background(), tx, 7, entry.ID, check.RequestID, authority)
		return err
	})
	if !errors.Is(err, ErrInvalidEntryUpdate) || authority.readCalls != 0 || authority.closeCalls != 0 {
		t.Fatalf("network attempted inside transaction: %v", err)
	}
	for _, raw := range []string{`{}`, string(check.RequestBinding) + ` {}`, `{"unknown":true}`} {
		if _, err := decodeSharingFulfillmentBinding(json.RawMessage(raw)); err == nil {
			t.Fatalf("malformed/incomplete binding decoded: %s", raw)
		}
	}
	// An expired finite decision is still recoverable; the operator and original
	// responsibility are not requalified, and no access is granted by resolution.
	var binding sharingFulfillmentBinding
	if err := json.Unmarshal(check.RequestBinding, &binding); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Hour).UTC()
	binding.ExpiryMode, binding.ExpiresAt = authorization.SharingExpiryAtTime, &expired
	raw, _ := json.Marshal(binding)
	if err := db.Model(&models.FulfillmentCheck{}).Where("request_id = ?", check.RequestID).Update("request_binding", raw).Error; err != nil {
		t.Fatal(err)
	}
	authority.read = func(tenant int64, request uuid.UUID, actual sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
		return &sharingFulfillmentResolution{request, tenant, actual, "closed"}, nil
	}
	if resolved, err := reconcileSharingFulfillment(context.Background(), db, 7, entry.ID, check.RequestID, authority); err != nil || resolved.ResolvedAt == nil {
		t.Fatalf("expiry blocked historical recovery: %+v err=%v", resolved, err)
	}
}

func TestSharingFulfillmentRecoveryLostLocalCompletionKeepsOriginalRequest(t *testing.T) {
	db, entry, check, binding := seedSharingRecovery(t)
	ctx, cancel := context.WithCancel(context.Background())
	authority := &fakeSharingFulfillmentAuthority{
		read: func(tenant int64, request uuid.UUID, actual sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
			cancel() // Authority result obtained, local commit cannot complete.
			return &sharingFulfillmentResolution{request, tenant, actual, "accepted"}, nil
		},
	}
	if row, err := reconcileSharingFulfillment(ctx, db, 7, entry.ID, check.RequestID, authority); !errors.Is(err, context.Canceled) || row != nil {
		t.Fatalf("local failure incorrectly resolved: %+v err=%v", row, err)
	}
	var pending models.FulfillmentCheck
	if err := db.First(&pending, "request_id = ?", check.RequestID).Error; err != nil || pending.ResolvedAt != nil ||
		string(pending.RequestBinding) != string(check.RequestBinding) || !pending.CreatedAt.Equal(check.CreatedAt) {
		t.Fatalf("local failure changed original request: %+v err=%v", pending, err)
	}
	authority.read = func(tenant int64, request uuid.UUID, actual sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
		if !equalSharingFulfillmentBinding(binding, actual) {
			t.Fatal("retry changed binding")
		}
		return &sharingFulfillmentResolution{request, tenant, actual, "accepted"}, nil
	}
	if row, err := reconcileSharingFulfillment(context.Background(), db, 7, entry.ID, check.RequestID, authority); err != nil || row.ResolvedAt == nil || authority.closeCalls != 0 {
		t.Fatalf("original result could not be recovered: %+v err=%v", row, err)
	}
}
