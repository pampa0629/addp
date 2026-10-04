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
	"github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSharingFulfillmentAutomaticIssuance(t *testing.T) {
	exerciseCatalogAutomaticIssuance(t, openSharingPreparationTestDB(t))
}

func exerciseCatalogAutomaticIssuance(t *testing.T, db *gorm.DB) {
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	previousLimit := pool.Stats().MaxOpenConnections
	pool.SetMaxOpenConns(1) // Remote callbacks must run without a leased DB connection.
	defer pool.SetMaxOpenConns(previousLimit)
	for _, scenario := range []string{"restart_after_acceptance", "lost_issuance_response", "local_commit_failure", "issued_history", "closed", "window_expired", "closed_at_issue", "forbidden", "lookup_404", "binding_conflict", "malformed_history"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			entry, input := seedSharingEntry(t, db)
			input.ExpiryMode, input.ExpiresAt = shared.SharingExpiryUntilRevoked, nil
			s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
			decision, _, err := s.CreateSharingDecision(ctx, 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
			if err != nil {
				t.Fatal(err)
			}
			var prepared *models.FulfillmentCheck
			if err := db.Transaction(func(tx *gorm.DB) error {
				var err error
				prepared, _, err = prepareSharingFulfillment(ctx, tx, 7, entry.ID, decision.ID, preparationFixture(t, decision))
				return err
			}); err != nil {
				t.Fatal(err)
			}
			// A committed aged row models the original process's durable record.
			pending := *prepared
			pending.RequestID, pending.CreatedAt = uuid.New(), time.Now().UTC().Add(-2*time.Minute)
			if err := db.Create(&pending).Error; err != nil {
				t.Fatal(err)
			}
			binding, err := decodeSharingFulfillmentBinding(pending.RequestBinding)
			if err != nil {
				t.Fatal(err)
			}
			// The foreground had already reconciled acceptance, then exited.
			for _, row := range []*models.FulfillmentCheck{prepared, &pending} {
				if err := db.Transaction(func(tx *gorm.DB) error {
					_, err := resolveSharingFulfillment(ctx, tx, 7, entry.ID, row.RequestID, binding)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			// Retire the fixture's recent preparation; only the aged original is
			// tested below. These timestamps are fixtures, not authority outcomes.
			if err := db.Model(prepared).Update("grant_reconciled_at", gorm.Expr("CURRENT_TIMESTAMP")).Error; err != nil {
				t.Fatal(err)
			}
			var before models.FulfillmentCheck
			if err := db.First(&before, "request_id=?", pending.RequestID).Error; err != nil {
				t.Fatal(err)
			}
			if scenario == "restart_after_acceptance" || scenario == "lost_issuance_response" {
				if err := db.Model(&models.Responsibility{}).Where("id=?", decision.ResponsibilityID).Update("status", models.ResponsibilityStatusNeedsTransfer).Error; err != nil {
					t.Fatalf("unprocessed issuance froze reconciled responsibility: %v", err)
				}
			}
			issued, issues, reads := scenario == "issued_history", 0, 0
			historyTime := time.Date(2020, 1, 2, 3, 4, 5, 123456000, time.UTC)
			recordedAt := time.Now().UTC()
			deadline := recordedAt.Add(5 * time.Minute)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads++
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-catalog-runtime" || r.Header.Get("X-Tenant-ID") != "" || !strings.Contains(r.URL.Path, pending.RequestID.String()) {
					t.Error("changed original identity")
				}
				var actual sharingFulfillmentBinding
				if err := json.NewDecoder(r.Body).Decode(&actual); err != nil || !equalSharingFulfillmentBinding(binding, actual) {
					t.Error("changed original binding")
				}
				if db.Dialector.Name() == "postgres" {
					if err := db.Transaction(func(tx *gorm.DB) error {
						return tx.Exec("SELECT id FROM catalog.entries WHERE id=? FOR UPDATE NOWAIT", entry.ID).Error
					}); err != nil {
						t.Errorf("network call holds owner lock: %v", err)
					}
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/grant/resolve"):
					if scenario == "lookup_404" {
						w.WriteHeader(404)
						return
					}
					if scenario == "malformed_history" {
						_, _ = w.Write([]byte(`{"found":true,"grant":{}}`))
						return
					}
					lookup := shared.SharingFulfillmentGrantLookup{Found: issued}
					if issued {
						lookup.Grant = &shared.SharingFulfillmentGrant{RequestID: pending.RequestID, GrantedAt: historyTime}
					}
					_ = json.NewEncoder(w).Encode(lookup)
				case strings.HasSuffix(r.URL.Path, "/grant"):
					issues++
					codes := map[string]string{"window_expired": "engine_access_grant_window_expired", "closed_at_issue": "engine_access_fulfillment_closed", "binding_conflict": "engine_access_fulfillment_binding_conflict"}
					if code := codes[scenario]; code != "" {
						w.WriteHeader(409)
						_ = json.NewEncoder(w).Encode(map[string]string{"error_code": code})
						return
					}
					if scenario == "forbidden" {
						w.WriteHeader(403)
						return
					}
					issued = true
					if scenario == "lost_issuance_response" {
						w.WriteHeader(503) // System committed, response lost.
						return
					}
					_ = json.NewEncoder(w).Encode(shared.SharingFulfillmentGrant{RequestID: pending.RequestID, GrantedAt: historyTime})
				case strings.HasSuffix(r.URL.Path, "/resolve"):
					outcome := "accepted"
					var end *time.Time = &deadline
					if scenario == "closed" {
						outcome, end = "closed", nil
					}
					_ = json.NewEncoder(w).Encode(shared.SharingFulfillmentLookup{Found: true, Resolution: &shared.SharingFulfillmentResolution{RequestID: pending.RequestID, TenantID: 7, Binding: actual, Outcome: outcome, RecordedAt: recordedAt, Deadline: end}})
				default:
					t.Error("recovery accepted or closed a resolved request")
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			remote := client.NewSystemFulfillmentClient(server.URL, recoveryRuntimeTestTokens{}, server.Client())
			authority := &systemSharingFulfillmentAuthority{client: remote}
			callbackName := "test:issuance_commit_failure"
			if scenario == "local_commit_failure" {
				if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
					if values, ok := tx.Statement.Dest.(map[string]interface{}); ok {
						if _, updatingMarker := values["grant_reconciled_at"]; updatingMarker {
							tx.AddError(errors.New("injected local completion failure"))
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
				defer db.Callback().Update().Remove(callbackName)
			}
			expectsError := scenario == "lost_issuance_response" || scenario == "local_commit_failure" || scenario == "forbidden" || scenario == "lookup_404" || scenario == "binding_conflict" || scenario == "malformed_history"
			if scenario == "restart_after_acceptance" {
				err = NewSharingFulfillmentReconciliationRunner(db, nil, remote, time.Second).reconcileTenant(ctx, 7)
			} else {
				_, err = continueSharingFulfillment(ctx, db, 7, entry.ID, pending.RequestID, authority)
			}
			if (err != nil) != expectsError {
				t.Fatalf("unexpected continuation result: %v", err)
			}
			if scenario == "local_commit_failure" {
				if err := db.Callback().Update().Remove(callbackName); err != nil {
					t.Fatal(err)
				}
			}
			var after models.FulfillmentCheck
			if err := db.First(&after, "request_id=?", pending.RequestID).Error; err != nil || (after.GrantReconciledAt == nil) != expectsError || !after.ResolvedAt.Equal(*before.ResolvedAt) || !after.CreatedAt.Equal(before.CreatedAt) || string(after.RequestBinding) != string(before.RequestBinding) {
				t.Fatalf("recovery marker or original record invalid: %+v %v", after, err)
			}
			if scenario == "closed" || scenario == "issued_history" || scenario == "lookup_404" || scenario == "malformed_history" {
				if issues != 0 {
					t.Fatal("history/error triggered issuance")
				}
			} else if issues != 1 {
				t.Fatalf("issuance count=%d", issues)
			}
			if scenario == "restart_after_acceptance" || scenario == "lost_issuance_response" || scenario == "local_commit_failure" {
				// A fresh production runner must scan a resolved but unprocessed
				// row, recover committed issuance, and never issue it twice.
				runner := NewSharingFulfillmentReconciliationRunner(db, nil, remote, time.Second)
				if err := runner.reconcileTenant(ctx, 7); err != nil {
					t.Fatal(err)
				}
				if err := db.First(&after, "request_id=?", pending.RequestID).Error; err != nil || after.GrantReconciledAt == nil || issues != 1 {
					t.Fatalf("restart lost issuance recovery: %+v issues=%d %v", after, issues, err)
				}
			}
			if after.GrantReconciledAt != nil {
				previous := reads
				repeated, err := continueSharingFulfillment(ctx, db, 7, entry.ID, pending.RequestID, authority)
				if err != nil || reads != previous || !repeated.GrantReconciledAt.Equal(*after.GrantReconciledAt) {
					t.Fatalf("terminal local history replayed: %v", err)
				}
			} else {
				// Retire failure fixtures only so later runner cases remain isolated.
				if err := db.Model(&after).Update("grant_reconciled_at", gorm.Expr("CURRENT_TIMESTAMP")).Error; err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
