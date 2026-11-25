package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
)

func TestIntegrationPostgresEvaluationWriterTakeover(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	db := f.db
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
	// Use the same transaction path as a periodic rule. Only terminate the
	// backend whose identity we read from this test's actual write transaction.
	var pid int
	if err := old.transaction(ctx, db, func(tx *gorm.DB) error { return tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error }); err != nil {
		t.Fatal(err)
	}
	var killed bool
	if err := db.WithContext(ctx).Raw("SELECT pg_terminate_backend(?, 5000)", pid).Scan(&killed).Error; err != nil || !killed {
		t.Fatalf("disconnect test writer: %v %v", killed, err)
	}
	// The old evaluator is still draining when the standby acquires the lock.
	next := take(second)
	update := func(w *evaluationWriter, label string) error {
		return w.transaction(ctx, db, func(tx *gorm.DB) error {
			return tx.Model(&models.PlatformAlertIncident{}).Where("id=?", f.incident.ID).UpdateColumn("severity", label).Error
		})
	}
	if err := update(next, "warning"); err != nil {
		t.Fatalf("standby write: %v", err)
	}
	if err := update(old, "critical"); err == nil {
		t.Fatal("disconnected writer committed stale result")
	}
	var stored models.PlatformAlertIncident
	if err := db.First(&stored, f.incident.ID).Error; err != nil || stored.Severity != "warning" {
		t.Fatalf("takeover state: %q, %v", stored.Severity, err)
	}
}
