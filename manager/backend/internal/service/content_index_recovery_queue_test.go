package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

func TestContentRecoveryDrainsMaintenanceBehindFullQueue(t *testing.T) {
	const at = "2026-10-14T00:00:00Z"
	correlations := map[int]string{}
	polls := map[int]int{}
	submissions := 0
	svc, db := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/indexes/content/documents/delete":
			submissions++
			correlations[submissions] = r.URL.Query().Get("customMetadata")
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"taskUid":%d,"indexUid":"content","type":"documentDeletion","enqueuedAt":%q}`, submissions, at)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tasks/"):
			uid, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/tasks/"))
			if err != nil {
				t.Errorf("unexpected task path: %s", r.URL.Path)
			}
			polls[uid]++
			status := "succeeded"
			if uid == 1 && polls[uid] == 1 {
				status = "processing"
			}
			fmt.Fprintf(w, `{"uid":%d,"indexUid":"content","type":"documentDeletion","status":%q,"enqueuedAt":%q,"customMetadata":%q}`, uid, status, at, correlations[uid])
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	var last *models.ContentIndexDelivery
	for i := 0; i < 101; i++ {
		var err error
		last, err = svc.deliveries.QueueDelete(t.Context(), svc.epoch, 7, fmt.Sprintf("tenant_id = 7 AND engine_id = %d", i+1))
		if err != nil {
			t.Fatal(err)
		}
	}
	// A newer in-flight cleanup blocks more than one full page of older queued
	// work. Its external task already succeeded before this Manager restarted.
	if claimed, err := svc.deliveries.ClaimMaintenance(t.Context(), svc.epoch, last); err != nil || !claimed {
		t.Fatalf("claim existing delivery: claimed=%v error=%v", claimed, err)
	}
	enqueued, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.deliveries.Receipt(t.Context(), last, 0, enqueued); err != nil {
		t.Fatal(err)
	}
	correlations[0] = last.ID
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for cycle := 0; cycle < 2; cycle++ {
		err := svc.reconcileContentDeliveries(ctx)
		if cycle == 0 {
			if err != repository.ErrContentIndexIsolated {
				t.Fatalf("first page must retain isolation for remaining work: %v", err)
			}
		} else if err != nil {
			t.Fatalf("recovery did not drain after two pages: %v", err)
		}
	}
	var succeeded int64
	if err := db.Model(&models.ContentIndexDelivery{}).Where("status = ?", repository.IndexDeliverySucceeded).Count(&succeeded).Error; err != nil {
		t.Fatal(err)
	}
	if succeeded != 101 || submissions != 100 || polls[0] == 0 {
		t.Fatalf("recovery starved or resent work: succeeded=%d submissions=%d original_polls=%d", succeeded, submissions, polls[0])
	}
	if err := svc.deliveries.RequireActive(t.Context(), svc.epoch); err != nil {
		t.Fatalf("settled outlet remained isolated: %v", err)
	}
}
