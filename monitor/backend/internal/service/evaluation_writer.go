package service

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/addp/common/repository"
	"gorm.io/gorm"
)

// Reads and upstream calls keep their separate budgets. Only database mutations
// share the lock-owning connection; its transactions must not interleave.
type evaluationWriter struct {
	conn *sql.Conn
	mu   sync.Mutex
}

func (w *evaluationWriter) transaction(ctx context.Context, db *gorm.DB, fn func(*gorm.DB) error) error {
	tx := db.WithContext(ctx)
	if w != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		tx.Statement.ConnPool = w.conn
	}
	return tx.Transaction(fn)
}

// PeriodicEvaluators holds independent copies of the HTTP services for each
// elected tenure. Request handlers and notification dispatchers use the pool.
type PeriodicEvaluators struct {
	Tenant       AlertService
	Logs         LogPipelineService
	Resources    ResourceAlertRuleService
	HTTP         HTTPAlertRuleService
	Processes    ProcessAlertRuleService
	Availability ServiceAvailabilityRuleService
}

func (e PeriodicEvaluators) Run(ctx context.Context, interval time.Duration) {
	runEvaluationElection(ctx, e.Tenant.db, 5*time.Second, func(active context.Context, w *evaluationWriter) error {
		e.Tenant.writer = w
		e.Logs.writer = w
		start := time.Now().UTC()
		e.Resources.writer, e.Resources.startedAt = w, start
		e.HTTP.writer, e.HTTP.startedAt = w, start
		e.Processes.writer, e.Processes.startedAt = w, start
		e.Availability.writer, e.Availability.startedAt = w, start
		if err := e.Logs.resetEvaluationWindow(active); err != nil {
			return err
		}
		log.Print("Monitor periodic evaluation started")
		var group sync.WaitGroup
		for _, run := range []func(context.Context){e.Logs.Run, e.Resources.Run, e.HTTP.Run, e.Processes.Run, e.Availability.Run} {
			group.Add(1)
			go func() { defer group.Done(); run(active) }()
		}
		defer group.Wait()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for active.Err() == nil {
			if err := e.Tenant.Evaluate(active, time.Now()); err != nil && active.Err() == nil {
				log.Printf("Alert evaluation failed: %v", err)
			}
			select {
			case <-active.Done():
				return active.Err()
			case <-ticker.C:
			}
		}
		return active.Err()
	})
}

func runEvaluationElection(ctx context.Context, db *gorm.DB, interval time.Duration, run func(context.Context, *evaluationWriter) error) {
	for ctx.Err() == nil {
		err := repository.WithSessionLock(ctx, db, "monitor.periodic-alert-evaluation", func(conn *sql.Conn) error {
			active, cancel := context.WithCancel(ctx)
			defer cancel()
			done := make(chan error, 1)
			writer := &evaluationWriter{conn: conn}
			go func() { done <- run(active, writer) }()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case err := <-done:
					return err
				case <-ctx.Done():
					cancel()
					<-done
					return ctx.Err()
				case <-ticker.C:
					// Do not send a driver ping inside an active rule transaction.
					if !writer.mu.TryLock() {
						continue
					}
					probe, stop := context.WithTimeout(ctx, interval)
					err := conn.PingContext(probe)
					writer.mu.Unlock()
					stop()
					if err != nil {
						cancel()
						<-done
						return err
					}
				}
			}
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("Monitor periodic evaluation session ended: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
