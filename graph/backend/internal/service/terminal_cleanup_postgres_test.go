package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Exercise the last SQL statement of the real final transaction, after both
// archive and professional result writes, rather than an earlier result error.
func testGraphCleanupTerminalRollback(t *testing.T) {
	for _, parentState := range []string{"running", "timeout"} {
		t.Run(parentState, func(t *testing.T) {
			budget := time.Minute
			if parentState == "timeout" {
				budget = 4 * time.Second
			}
			f := newReviewCleanupPGFixtureWithBudget(t, budget)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			before := readDeleteCleanupFacts(t, f.db, f.task.TenantID)
			input, err := execution.CleanupRequestFromExecution(f.child)
			if err != nil {
				t.Fatal(err)
			}
			name := "terminal_cleanup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			statement := fmt.Sprintf(`CREATE FUNCTION graph.%[1]s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.execution_id = '%[2]s' AND OLD.status = 'running' AND NEW.status = 'success' THEN
    IF NOT (NEW.metadata ? 'cleanup_receipt') OR NEW.completed_at IS NULL OR NEW.progress <> 100
      OR NOT EXISTS (SELECT 1 FROM graph.knowledge_graphs WHERE id = %[3]d AND tenant_id = NEW.tenant_id
        AND status = 'archived' AND admission_closed AND admission_execution_id = NEW.execution_id)
      OR NOT EXISTS (SELECT 1 FROM graph.cleanup_results WHERE execution_id = NEW.execution_id
        AND tenant_id = NEW.tenant_id AND attempt = NEW.attempt) THEN
      RAISE EXCEPTION 'fixture final stages not reached';
    END IF;
    RAISE EXCEPTION 'fixture terminal update failure';
  END IF;
  RETURN NEW;
END; $$;
CREATE TRIGGER %[1]s BEFORE UPDATE ON common.task_executions FOR EACH ROW EXECUTE FUNCTION graph.%[1]s()`, name, f.child.ExecutionID, f.graph.ID)
			if err := f.db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := f.db.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON common.task_executions; DROP FUNCTION IF EXISTS graph.%s()", name, name)).Error; err != nil {
					t.Error(err)
				}
			})
			failedSQL := make(chan error, 1)
			callback := "fixture:terminal-cleanup-error"
			if err := f.db.Callback().Update().After("gorm:update").Register(callback, func(tx *gorm.DB) {
				fields, ok := tx.Statement.Dest.(map[string]interface{})
				if ok && fields["status"] == execution.ExecutionStatusSuccess && tx.Error != nil {
					select {
					case failedSQL <- tx.Error:
					default:
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = f.db.Callback().Update().Remove(callback) })
			barrier, release := pauseCleanupAfterClosurePG(t, ctx, f)
			defer release()
			f.start(t, ctx)
			select {
			case <-barrier:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var running, parent execution.TaskExecution
			if err := f.db.First(&running, "execution_id = ?", f.child.ExecutionID).Error; err != nil || running.Status != execution.ExecutionStatusRunning || running.Attempt != 1 || running.StartedAt == nil {
				t.Fatalf("child claim did not commit: %v", err)
			}
			if err := f.db.First(&parent, "execution_id = ?", input.ParentExecutionID).Error; err != nil {
				t.Fatal(err)
			}
			if parentState == "timeout" {
				lease, err := execution.LeaseFromExecution(parent)
				if err != nil {
					t.Fatal(err)
				}
				waitReviewCleanupPG(t, ctx, f.db, "SELECT clock_timestamp() >= ?", input.Deadline)
				if err := execution.CompleteWithLease(ctx, f.db, lease, execution.ExecutionStatusTimeout, nil); err != nil {
					t.Fatal(err)
				}
				// Reading SQL NULL into the reused timestamp pointer retains its
				// old value; compare two fresh committed database snapshots.
				parent = execution.TaskExecution{}
				if err := f.db.First(&parent, "execution_id = ?", input.ParentExecutionID).Error; err != nil {
					t.Fatal(err)
				}
				if parent.CompletedAt == nil || parent.LeaseOwner != nil || parent.LeaseToken != nil || parent.LeaseExpiresAt != nil {
					t.Fatal("parent timeout did not clear its committed lease")
				}
			}
			if parent.Status != parentState {
				t.Fatalf("wrong parent state before final transaction: %s", parent.Status)
			}
			var closed models.KnowledgeGraph
			if err := f.db.First(&closed, f.graph.ID).Error; err != nil || !closed.AdmissionClosed || closed.AdmissionExecutionID != running.ExecutionID || closed.Version != f.graph.Version+1 || closed.Status != f.graph.Status {
				t.Fatalf("domain closure did not commit before final transaction: %v", err)
			}
			release()
			select {
			case err := <-failedSQL:
				var state interface{ SQLState() string }
				if !errors.As(err, &state) || state.SQLState() != "P0001" || !strings.Contains(err.Error(), "fixture terminal update failure") {
					t.Fatalf("failure did not follow actual archive/result writes: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			child := f.waitTerminal(t, ctx, execution.ExecutionStatusFailed)
			if child.StartedAt == nil || !child.StartedAt.Equal(*running.StartedAt) || child.CompletedAt == nil || child.LeaseOwner != nil || child.LeaseToken != nil || child.LeaseExpiresAt != nil || child.Progress != running.Progress || !reflect.DeepEqual(child.ExecutionConfig, running.ExecutionConfig) || !reflect.DeepEqual(child.Metadata, running.Metadata) || !reflect.DeepEqual(child.ParentExecutionID, running.ParentExecutionID) {
				t.Fatal("failed convergence changed the admitted attempt/input or retained success fields")
			}
			if child.ErrorDetails["code"] != "cleanup_owner_stopped_without_receipt" {
				t.Fatal("failed convergence did not retain the safe failure classification")
			}
			receipt, err := execution.ReadCleanupReceipt(*child)
			if err != nil || receipt != nil {
				t.Fatalf("failed final update retained a public receipt: %v", err)
			}
			f.assertReceiptCount(t, 0)
			var after models.KnowledgeGraph
			if err := f.db.First(&after, f.graph.ID).Error; err != nil || !reflect.DeepEqual(after, closed) {
				t.Fatalf("archive rollback changed the committed domain closure: %v", err)
			}
			var unchanged execution.TaskExecution
			if err := f.db.First(&unchanged, "execution_id = ?", parent.ExecutionID).Error; err != nil || !reflect.DeepEqual(unchanged, parent) {
				var different []string
				a, b := reflect.ValueOf(unchanged), reflect.ValueOf(parent)
				for i := 0; i < a.NumField(); i++ {
					if !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
						different = append(different, a.Type().Field(i).Name)
					}
				}
				t.Fatalf("failed final transaction changed parent fields %v: %v", different, err)
			}
			if !reflect.DeepEqual(readDeleteCleanupFacts(t, f.db, f.task.TenantID), before) {
				t.Fatal("failed final transaction changed original build/domain evidence")
			}
		})
	}
}
