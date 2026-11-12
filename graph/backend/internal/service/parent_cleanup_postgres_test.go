package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Parent timeout uses the production Common lease transition; System's private
// cutoff/audit and actual authorization are covered by their own gates.
func testGraphCleanupParentClosureConcurrency(t *testing.T) {
	for _, phase := range []string{"admission", "after_closure"} {
		for _, outcome := range []string{"complete", "result_rollback"} {
			t.Run(phase+"/"+outcome, func(t *testing.T) {
				f := newReviewCleanupPGFixtureWithBudget(t, 4*time.Second)
				ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
				defer cancel()
				input, err := execution.CleanupRequestFromExecution(f.child)
				if err != nil {
					t.Fatal(err)
				}
				before := readDeleteCleanupFacts(t, f.db, f.task.TenantID)
				var parent execution.TaskExecution
				if err := f.db.First(&parent, "execution_id = ?", input.ParentExecutionID).Error; err != nil {
					t.Fatal(err)
				}
				parentLease, err := execution.LeaseFromExecution(parent)
				if err != nil {
					t.Fatal(err)
				}
				if outcome == "result_rollback" {
					f.rejectWrite(t, "cleanup_results", "INSERT", "NEW.execution_id = '"+f.child.ExecutionID+"'")
				}
				var holder *gorm.DB
				var blocker int
				if phase == "admission" {
					holder, blocker = lockCleanupAdmissionPG(t, ctx, f)
					defer holder.Rollback()
					f.start(t, ctx)
				} else {
					barrier, release := pauseCleanupAfterClosurePG(t, ctx, f)
					defer release()
					f.start(t, ctx)
					select {
					case <-barrier:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					holder, blocker = lockCleanupAdmissionPG(t, ctx, f)
					defer holder.Rollback()
					release()
				}
				waitReviewCleanupPG(t, ctx, f.db, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid)))", blocker)
				var running execution.TaskExecution
				if err := f.db.First(&running, "execution_id = ?", f.child.ExecutionID).Error; err != nil || running.Status != execution.ExecutionStatusRunning || running.Attempt != 1 || running.StartedAt == nil || running.LeaseExpiresAt == nil || !running.LeaseExpiresAt.After(input.Deadline) {
					t.Fatalf("child claim was not committed with an independent lease: %v", err)
				}
				// A real NOWAIT probe proves C is already held while G is busy.
				var probe execution.TaskExecution
				err = f.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).First(&probe, "execution_id = ?", f.child.ExecutionID).Error
				var state interface{ SQLState() string }
				if !errors.As(err, &state) || state.SQLState() != "55P03" {
					t.Fatalf("expected actual C lock while waiting for G: %v", err)
				}
				var observed models.KnowledgeGraph
				if err := f.db.First(&observed, f.graph.ID).Error; err != nil || observed.AdmissionClosed != (phase == "after_closure") {
					t.Fatalf("wrong domain phase before parent closure: %v", err)
				}
				waitReviewCleanupPG(t, ctx, f.db, "SELECT clock_timestamp() >= ?", input.Deadline)
				// P closes while C is locked by the child and G by this fixture.
				if err := execution.CompleteWithLease(ctx, f.db, parentLease, execution.ExecutionStatusTimeout, nil); err != nil {
					t.Fatalf("parent closure waited on its admitted child/domain: %v", err)
				}
				parentGuard := f.db.WithContext(ctx).Begin()
				if parentGuard.Error != nil {
					t.Fatal(parentGuard.Error)
				}
				defer parentGuard.Rollback()
				var closed execution.TaskExecution
				if err := parentGuard.Clauses(clause.Locking{Strength: "UPDATE"}).First(&closed, "execution_id = ?", parent.ExecutionID).Error; err != nil || closed.Status != execution.ExecutionStatusTimeout || closed.CompletedAt == nil {
					t.Fatalf("parent timeout did not commit independently: %v", err)
				}
				var stillRunning execution.TaskExecution
				if err := f.db.First(&stillRunning, "execution_id = ?", f.child.ExecutionID).Error; err != nil || !reflect.DeepEqual(stillRunning, running) {
					t.Fatalf("parent closure changed the admitted child: %v", err)
				}
				var afterClose models.KnowledgeGraph
				if err := f.db.First(&afterClose, f.graph.ID).Error; err != nil || !reflect.DeepEqual(afterClose, observed) {
					t.Fatalf("parent closure changed domain evidence: %v", err)
				}
				if err := holder.Commit().Error; err != nil {
					t.Fatal(err)
				}
				// Keep P locked until after C finishes: any reverse C -> P read
				// lock would block and fail this bounded case.
				want := execution.ExecutionStatusSuccess
				if outcome == "result_rollback" {
					want = execution.ExecutionStatusFailed
				}
				child := f.waitTerminal(t, ctx, want)
				if child.StartedAt == nil || !child.StartedAt.Equal(*running.StartedAt) || child.ParentExecutionID == nil || *child.ParentExecutionID != parent.ExecutionID || !reflect.DeepEqual(child.ExecutionConfig, running.ExecutionConfig) {
					t.Fatal("child replaced its admitted attempt, start, parent or frozen input")
				}
				var unchanged execution.TaskExecution
				if err := f.db.First(&unchanged, "execution_id = ?", parent.ExecutionID).Error; err != nil || !reflect.DeepEqual(unchanged, closed) {
					t.Fatalf("late child completion rewrote parent terminal/observation: %v", err)
				}
				if !reflect.DeepEqual(readDeleteCleanupFacts(t, f.db, f.task.TenantID), before) {
					t.Fatal("parent closure or child receipt changed original build/domain evidence")
				}
				var kg models.KnowledgeGraph
				if err := f.db.First(&kg, f.graph.ID).Error; err != nil || !kg.AdmissionClosed || kg.AdmissionExecutionID != child.ExecutionID || kg.Version != f.graph.Version+1 || (kg.Status == "archived") != (outcome == "complete") {
					t.Fatalf("child lost its committed domain closure or archive rollback: %v", err)
				}
				receipt, err := execution.ReadCleanupReceipt(*child)
				if err != nil {
					t.Fatal(err)
				}
				if outcome == "result_rollback" {
					if receipt != nil || kg.Status != f.graph.Status {
						t.Fatal("failed child transaction left an archive/public receipt")
					}
					f.assertReceiptCount(t, 0)
				} else {
					if receipt == nil || receipt.Status != "success" {
						t.Fatal("admitted child could not finish after parent timeout")
					}
					f.assertReceiptCount(t, 1)
					var payload string
					if err := f.db.Table("graph.cleanup_results").Select("payload").Where("execution_id = ?", child.ExecutionID).Scan(&payload).Error; err != nil {
						t.Fatal(err)
					}
					var result cleanup.Result
					if err := json.Unmarshal([]byte(payload), &result); err != nil || result.Status != "success" || result.Attempt != 1 || result.ExecutionID != child.ExecutionID || result.Statistics["archived_graphs"] != 1 || result.Statistics["cancelled_build_tasks"] != 0 {
						t.Fatalf("child professional receipt changed identity or statistics: %v", err)
					}
				}
			})
		}
	}
}

func lockCleanupAdmissionPG(t *testing.T, ctx context.Context, f *reviewCleanupPGFixture) (*gorm.DB, int) {
	t.Helper()
	tx := f.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	var pid int
	if err := tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.LockAdmission(tx, f.task.TenantID, true); err != nil {
		t.Fatal(err)
	}
	return tx, pid
}

func pauseCleanupAfterClosurePG(t *testing.T, ctx context.Context, f *reviewCleanupPGFixture) (<-chan struct{}, func()) {
	t.Helper()
	barrier, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	name := "fixture:parent-closure-after-graph-closure"
	if err := f.db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]models.BuildExecutionControl); !ok {
			return
		}
		once.Do(func() {
			close(barrier)
			select {
			case <-release:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Query().Remove(name) })
	return barrier, func() { releaseOnce.Do(func() { close(release) }) }
}
