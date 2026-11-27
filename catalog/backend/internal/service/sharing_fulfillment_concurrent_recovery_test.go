package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"gorm.io/gorm"
)

func exerciseConcurrentReceiptRecovery(t *testing.T, db *gorm.DB) {
	_, pending, binding := prepareRecoveryCheck(t, db)
	// Compare against the committed preparation, as a newly started worker does.
	if err := db.Where("request_id = ?", pending.RequestID).Take(pending).Error; err != nil {
		t.Fatal(err)
	}
	recorded := time.Now().UTC().Truncate(time.Microsecond)
	deadline := recorded.Add(5 * time.Minute)
	receipt := shared.SharingFulfillmentResolution{
		RequestID: pending.RequestID, TenantID: pending.TenantID, Binding: binding,
		Outcome: "accepted", RecordedAt: recorded, Deadline: &deadline,
	}
	var accepted atomic.Bool
	var reads atomic.Int32
	release := make(chan struct{})
	ready := make(chan struct{}, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// Hold both original receipt reads until both workers have observed the
	// pending Catalog row. Neither may issue a new acceptance or close it.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var actual sharingFulfillmentBinding
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&actual) != nil || !equalSharingFulfillmentBinding(binding, actual) {
			http.Error(w, "unexpected recovery request", http.StatusConflict)
			return
		}
		path := "/api/v1/system/runtime/engine-access-fulfillments/" + pending.RequestID.String()
		if r.URL.Path == path+"/accept" && accepted.CompareAndSwap(false, true) {
			panic(http.ErrAbortHandler) // Drop the response after recording acceptance.
		}
		if r.URL.Path != path+"/resolve" || !accepted.Load() || reads.Add(1) > 2 {
			http.Error(w, "repeated authority lookup", http.StatusConflict)
			return
		}
		ready <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return
		}
		_ = json.NewEncoder(w).Encode(shared.SharingFulfillmentLookup{Found: true, Resolution: &receipt})
	}))
	defer server.Close()
	remote := client.NewSystemFulfillmentClient(server.URL, recoveryRuntimeTestTokens{}, server.Client())
	result, err := remote.WithTenantID(uint(pending.TenantID)).Accept(ctx, pending.RequestID, binding)
	if result != nil || err == nil || !accepted.Load() {
		t.Fatalf("expected lost acceptance response: received=%v err=%v", result, err)
	}
	authority := &systemSharingFulfillmentAuthority{client: remote}

	type completion struct {
		row *models.FulfillmentCheck
		err error
	}
	done := make(chan completion, 2)
	for range 2 {
		go func() {
			var row *models.FulfillmentCheck
			err := db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
				var err error
				row, err = reconcileSharingFulfillment(ctx, conn, pending.TenantID, pending.CatalogEntryID, pending.RequestID, authority)
				return err
			})
			done <- completion{row, err}
		}()
	}
	for range 2 {
		select {
		case <-ready:
		case early := <-done:
			t.Fatalf("worker exited before both read the receipt: %v", early.err)
		case <-ctx.Done():
			t.Fatal("workers did not reach concurrent receipt lookup")
		}
	}
	close(release)
	var finished time.Time
	for range 2 {
		result := <-done
		if result.err != nil || result.row == nil || result.row.RequestID != pending.RequestID || result.row.ResolvedAt == nil {
			t.Fatalf("recovery result=%+v err=%v", result.row, result.err)
		}
		if !finished.IsZero() && !finished.Equal(*result.row.ResolvedAt) {
			t.Fatalf("workers returned different completion times: %s and %s", finished, result.row.ResolvedAt)
		}
		finished = *result.row.ResolvedAt
	}
	var rows []models.FulfillmentCheck
	if err := db.Where("request_id = ?", pending.RequestID).Find(&rows).Error; err != nil || len(rows) != 1 {
		t.Fatalf("persisted results=%d err=%v", len(rows), err)
	}
	stored := rows[0]
	if stored.ResolvedAt == nil || !stored.ResolvedAt.Equal(finished) || !stored.CreatedAt.Equal(pending.CreatedAt) {
		t.Fatalf("unexpected persisted times: created=%s resolved=%v", stored.CreatedAt, stored.ResolvedAt)
	}
	retry, err := reconcileSharingFulfillment(ctx, db, pending.TenantID, pending.CatalogEntryID, pending.RequestID, authority)
	if err != nil || retry == nil || retry.ResolvedAt == nil || !retry.ResolvedAt.Equal(finished) || reads.Load() != 2 {
		t.Fatalf("retry=%+v authority reads=%d err=%v", retry, reads.Load(), err)
	}
	t.Logf("request=%s workers=2 receipts_read=2 catalog_rows=1 resolved_at=%s", pending.RequestID, finished.UTC().Format(time.RFC3339Nano))
}
