package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSharingFulfillmentFormalPreparation(t *testing.T) {
	exerciseSharingFulfillmentPreparation(t, openSharingPreparationTestDB(t))
}

func exerciseSharingFulfillmentPreparation(t *testing.T, db *gorm.DB) {
	t.Run("commit before send and trusted pending basis", func(t *testing.T) {
		ctx := context.Background()
		entry, input := seedSharingEntry(t, db)
		input.ExpiryMode = shared.SharingExpiryUntilRevoked
		input.ExpiresAt = nil
		s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
		decision, _, err := s.CreateSharingDecision(ctx, 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
		if err != nil {
			t.Fatal(err)
		}
		auth := authtest.NewTenantUserAuthContext("7", "41", []string{"catalog.entry.read", "catalog.inventory.read", sourceHandlingPermission})
		p, m, v, err := sharingUserProvenance(auth, 7, "catalog.entry.read", sourceHandlingPermission)
		if err != nil {
			t.Fatal(err)
		}
		s.WithSharingHandlingScopeReader(&candidateScopeReader{scope: &shared.EngineAccessHandlingScope{TenantID: 7, EngineID: 12, Operator: shared.SharingFulfillmentOperator{PrincipalID: p, MembershipID: m, AuthorizationVersion: v}, VerifiedAt: time.Now()}})
		calls := 0
		unavailable := true
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer fixture-catalog-runtime" {
				t.Error("wrong service credential")
			}
			if r.URL.Path == "/api/v1/system/auth/context" {
				ac := auth
				ac.Principal.Type = "service_principal"
				ac.Principal.ID = "9007199254740993"
				id := "addp-catalog"
				ac.Client.ClientID = &id
				_ = json.NewEncoder(w).Encode(ac)
				return
			}
			calls++
			var binding shared.SharingFulfillmentBinding
			if err := json.NewDecoder(r.Body).Decode(&binding); err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			parts := strings.Split(r.URL.Path, "/")
			requestID, err := uuid.Parse(parts[6])
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			var persisted models.FulfillmentCheck
			if err := db.Where("request_id=?", requestID).Take(&persisted).Error; err != nil {
				t.Errorf("send before commit: %+v %v", persisted, err)
				w.WriteHeader(500)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/grant/resolve") {
				_ = json.NewEncoder(w).Encode(shared.SharingFulfillmentGrantLookup{Found: false})
				return
			}
			if strings.HasSuffix(r.URL.Path, "/grant") {
				if persisted.ResolvedAt == nil {
					t.Error("issuance precedes acceptance reconciliation")
				}
				_ = json.NewEncoder(w).Encode(shared.SharingFulfillmentGrant{RequestID: requestID, GrantedAt: time.Now()})
				return
			}
			if strings.HasSuffix(r.URL.Path, "/resolve") {
				now := time.Now().UTC()
				deadline := now.Add(5 * time.Minute)
				_ = json.NewEncoder(w).Encode(shared.SharingFulfillmentLookup{Found: true, Resolution: &shared.SharingFulfillmentResolution{RequestID: requestID, TenantID: 7, Binding: binding, Outcome: "accepted", RecordedAt: now, Deadline: &deadline}})
				return
			}
			if persisted.ResolvedAt != nil {
				t.Error("resolved request presented as new basis")
				w.WriteHeader(500)
				return
			}
			proof, err := s.ReadSharingFulfillmentBasis(ctx, 7, requestID, binding)
			if err != nil || proof.Confirmation.PrincipalID != 40 || proof.Binding.Operator.PrincipalID != 41 || proof.Binding.CallerPrincipalID != 9007199254740993 {
				t.Errorf("trusted basis=%+v %v", proof, err)
				w.WriteHeader(500)
				return
			}
			if db.Dialector.Name() == "postgres" {
				if err := db.Transaction(func(tx *gorm.DB) error {
					return tx.Exec("SELECT id FROM catalog.entries WHERE id=? FOR UPDATE NOWAIT", entry.ID).Error
				}); err != nil {
					t.Errorf("send holds entry lock: %v", err)
				}
			}
			if unavailable {
				w.WriteHeader(503)
				return
			}
			now := time.Now().UTC()
			deadline := now.Add(5 * time.Minute)
			_ = json.NewEncoder(w).Encode(shared.SharingFulfillmentResolution{RequestID: requestID, TenantID: 7, Binding: binding, Outcome: "accepted", RecordedAt: now, Deadline: &deadline})
		}))
		defer server.Close()
		s.WithSharingFulfillmentClient(client.NewSystemFulfillmentClient(server.URL, recoveryRuntimeTestTokens{}, server.Client()))
		prepare := SharingFulfillmentInput{RequestID: uuid.New(), DecisionID: decision.ID, RequirementVersion: 1}
		result, err := s.PrepareSharingFulfillment(ctx, 7, EntryAccess{Inventory: true}, entry.ID, prepare, auth, "addp_at_handler")
		if err != nil || result.State != "pending" || calls != 1 {
			t.Fatalf("uncertain send=%+v calls=%d %v", result, calls, err)
		}
		var pending models.FulfillmentCheck
		if err := db.Where("request_id=?", prepare.RequestID).Take(&pending).Error; err != nil || pending.ResolvedAt != nil {
			t.Fatalf("lost pending=%+v %v", pending, err)
		}
		binding, err := decodeSharingFulfillmentBinding(pending.RequestBinding)
		if err != nil {
			t.Fatal(err)
		}
		bad := binding
		bad.Operator.PrincipalID++
		if _, err := s.ReadSharingFulfillmentBasis(ctx, 7, prepare.RequestID, bad); !errors.Is(err, ErrSharingDecisionConflict) {
			t.Fatalf("forged binding=%v", err)
		}
		if _, err := s.ReadSharingFulfillmentBasis(ctx, 8, prepare.RequestID, binding); err == nil {
			t.Fatal("cross-tenant basis exposed")
		}
		denied := authtest.NewTenantUserAuthContext("7", "41", []string{"catalog.entry.read", "catalog.entry.update"})
		if _, err := s.PrepareSharingFulfillment(ctx, 7, EntryAccess{Inventory: true}, entry.ID, prepare, denied, "addp_at_handler"); !errors.Is(err, ErrSharingHandlingForbidden) {
			t.Fatalf("curation grants handling=%v", err)
		}
		unavailable = false
		result, err = s.PrepareSharingFulfillment(ctx, 7, EntryAccess{Inventory: true}, entry.ID, prepare, auth, "addp_at_handler")
		if err != nil || result.State != "accepted" || result.Resolution == nil {
			t.Fatalf("retry=%+v %v", result, err)
		}
		if err := db.Where("request_id=?", prepare.RequestID).Take(&pending).Error; err != nil || pending.GrantReconciledAt == nil {
			t.Fatalf("foreground issuance was not continued: %+v %v", pending, err)
		}
		if _, err := s.ReadSharingFulfillmentBasis(ctx, 7, prepare.RequestID, binding); err == nil {
			t.Fatal("resolved request presented as new basis")
		}
		var n int64
		if err := db.Model(&models.FulfillmentCheck{}).Where("request_id=?", prepare.RequestID).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("duplicate preparation=%d %v", n, err)
		}
		// This helper must not leave a pending protection in the owning PG suite.
	})
}
