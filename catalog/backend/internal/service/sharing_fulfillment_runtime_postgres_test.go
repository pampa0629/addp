package service

import (
	"context"
	"encoding/json"
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

type recoveryRuntimeTestTokens struct{}

func (recoveryRuntimeTestTokens) Token(context.Context, uint) (string, error) {
	return "fixture-catalog-runtime", nil
}

func exerciseCatalogRuntimeRecovery(t *testing.T, db *gorm.DB, original *models.FulfillmentCheck) {
	t.Run("production recovery consumer over HTTP", func(t *testing.T) {
		binding, err := decodeSharingFulfillmentBinding(original.RequestBinding)
		if err != nil {
			t.Fatal(err)
		}
		pending := *original
		pending.RequestID = uuid.New()
		pending.CreatedAt = time.Now().Add(-2 * time.Minute)
		if err := db.Create(&pending).Error; err != nil {
			t.Fatal(err)
		}
		calls, closes := 0, 0
		outage, closed := true, false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Header.Get("Authorization") != "Bearer fixture-catalog-runtime" || r.Header.Get("X-Tenant-ID") != "" || !strings.Contains(r.URL.Path, pending.RequestID.String()) {
				t.Error("invalid tenant runtime call")
			}
			var b sharingFulfillmentBinding
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil || !equalSharingFulfillmentBinding(binding, b) {
				t.Error("request binding changed")
			}
			if outage {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/close") {
				closes++
				closed = true
				// System committed closure, but its response is lost.
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			result := shared.SharingFulfillmentLookup{Found: closed}
			if closed {
				result.Resolution = &shared.SharingFulfillmentResolution{RequestID: pending.RequestID, TenantID: pending.TenantID, Binding: b, Outcome: "closed", RecordedAt: time.Now()}
			}
			_ = json.NewEncoder(w).Encode(result)
		}))
		defer server.Close()
		remote := client.NewSystemFulfillmentClient(server.URL, recoveryRuntimeTestTokens{}, server.Client())
		runner := NewSharingFulfillmentReconciliationRunner(db, nil, remote, time.Second)
		assertPending := func() {
			t.Helper()
			var current models.FulfillmentCheck
			if err := db.First(&current, "request_id = ?", pending.RequestID).Error; err != nil || current.ResolvedAt != nil {
				t.Fatalf("lost protection: %+v err=%v", current, err)
			}
		}
		if err := runner.reconcileTenant(context.Background(), pending.TenantID); err == nil || closes != 0 {
			t.Fatalf("404 treated as miss: %v closes=%d", err, closes)
		}
		assertPending()
		outage = false
		if err := runner.reconcileTenant(context.Background(), pending.TenantID); err == nil || closes != 1 {
			t.Fatalf("lost close response resolved: %v closes=%d", err, closes)
		}
		assertPending()
		// A new runner models process restart, with no memory of the first call.
		runner = NewSharingFulfillmentReconciliationRunner(db, nil, remote, time.Second)
		if err := runner.reconcileTenant(context.Background(), pending.TenantID); err != nil {
			t.Fatal(err)
		}
		var completed models.FulfillmentCheck
		if err := db.First(&completed, "request_id = ?", pending.RequestID).Error; err != nil || completed.ResolvedAt == nil {
			t.Fatalf("recovered=%+v err=%v", completed, err)
		}
		previousCalls := calls
		if err := runner.reconcileTenant(context.Background(), pending.TenantID); err != nil || calls != previousCalls || closes != 1 {
			t.Fatalf("resolved history resent: %v calls=%d closes=%d", err, calls, closes)
		}
		// The just-created foreground request remains pending and unsent by recovery.
		var recent models.FulfillmentCheck
		if err := db.First(&recent, "request_id = ?", original.RequestID).Error; err != nil || recent.ResolvedAt != nil {
			t.Fatalf("foreground request recovered too early: %v", err)
		}
		// A permanently failing first batch must not starve later requests.
		rows := make([]models.FulfillmentCheck, 101)
		ids := make([]uuid.UUID, len(rows))
		for index := range rows {
			rows[index] = *original
			rows[index].RequestID = uuid.New()
			ids[index] = rows[index].RequestID
			rows[index].CreatedAt = time.Now().Add(-3*time.Minute + time.Duration(index)*time.Millisecond)
		}
		if err := db.Create(&rows).Error; err != nil {
			t.Fatal(err)
		}
		lastVisited := false
		batchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, rows[100].RequestID.String()) {
				lastVisited = true
			}
			w.WriteHeader(http.StatusConflict)
		}))
		defer batchServer.Close()
		runner = NewSharingFulfillmentReconciliationRunner(db, nil,
			client.NewSystemFulfillmentClient(batchServer.URL, recoveryRuntimeTestTokens{}, batchServer.Client()), time.Second)
		if err := runner.reconcileTenant(context.Background(), pending.TenantID); err == nil || !lastVisited {
			t.Fatalf("old failures starved the next batch: visited=%v err=%v", lastVisited, err)
		}
		// Keep immutable history; retire test-only blockers for the parent
		// fixture without deleting or rewriting their request bindings.
		if err := db.Model(&models.FulfillmentCheck{}).Where("request_id IN ?", ids).
			Update("resolved_at", gorm.Expr("clock_timestamp()")).Error; err != nil {
			t.Fatal(err)
		}
	})
}
