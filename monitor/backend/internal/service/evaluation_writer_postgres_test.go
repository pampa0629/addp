package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
)

func TestIntegrationPostgresEvaluationWriterTakeover(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	exerciseEvaluationWriterTakeover(t, f.db, f.incident.ID, func(ctx context.Context, old *evaluationWriter) error {
		// Disconnect only the backend that owns the fixture's write transaction.
		var pid int
		if err := old.transaction(ctx, f.db, func(tx *gorm.DB) error { return tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error }); err != nil {
			return err
		}
		var killed bool
		if err := f.db.WithContext(ctx).Raw("SELECT pg_terminate_backend(?, 5000)", pid).Scan(&killed).Error; err != nil {
			return err
		}
		if !killed {
			return fmt.Errorf("test writer %d did not disconnect", pid)
		}
		return nil
	})
}

func exerciseEvaluationWriterTakeover(t *testing.T, db *gorm.DB, incidentID uint, disconnect func(context.Context, *evaluationWriter) error) {
	t.Helper()
	ctx, stop := context.WithTimeout(t.Context(), 15*time.Second)
	defer stop()
	run := func(ctx context.Context, acquired chan<- *evaluationWriter, done chan<- struct{}, drain <-chan struct{}) {
		defer close(done)
		runEvaluationElection(ctx, db, 100*time.Millisecond, func(active context.Context, w *evaluationWriter) error {
			select {
			case acquired <- w:
			case <-active.Done():
				return active.Err()
			}
			<-active.Done()
			if drain != nil {
				<-drain
			}
			return active.Err()
		})
	}
	take := func(ch <-chan *evaluationWriter) *evaluationWriter {
		t.Helper()
		select {
		case writer := <-ch:
			return writer
		case <-ctx.Done():
			t.Fatal("no evaluation writer acquired")
			return nil
		}
	}
	first, second := make(chan *evaluationWriter, 1), make(chan *evaluationWriter, 1)
	doneA, doneB := make(chan struct{}), make(chan struct{})
	a, cancelA := context.WithCancel(ctx)
	b, cancelB := context.WithCancel(ctx)
	releaseFirst := make(chan struct{})
	go run(a, first, doneA, releaseFirst)
	defer func() { cancelA(); close(releaseFirst); <-doneA }()
	old := take(first)
	go run(b, second, doneB, nil)
	defer func() { cancelB(); <-doneB }()
	select {
	case <-second:
		t.Fatal("two active evaluators")
	case <-time.After(350 * time.Millisecond):
	}
	if err := disconnect(ctx, old); err != nil {
		t.Fatalf("disconnect test writer: %v", err)
	}
	// The old evaluator is still draining when the standby acquires the lock.
	next := take(second)
	update := func(w *evaluationWriter, label string) error {
		return w.transaction(ctx, db, func(tx *gorm.DB) error {
			return tx.Model(&models.PlatformAlertIncident{}).Where("id=?", incidentID).UpdateColumn("severity", label).Error
		})
	}
	if err := update(next, "warning"); err != nil {
		t.Fatalf("standby write: %v", err)
	}
	if err := update(old, "critical"); err == nil {
		t.Fatal("disconnected writer committed stale result")
	}
	var stored models.PlatformAlertIncident
	if err := db.First(&stored, incidentID).Error; err != nil || stored.Severity != "warning" {
		t.Fatalf("takeover state: %q, %v", stored.Severity, err)
	}
}
