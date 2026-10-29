package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/events"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func testCleanupPostgresLockOrder(t *testing.T, db *gorm.DB) {
	wait := func(t *testing.T, query string, args ...interface{}) {
		t.Helper()
		limit := time.Now().Add(5 * time.Second)
		for time.Now().Before(limit) {
			var ready bool
			if err := db.Raw(query, args...).Scan(&ready).Error; err != nil {
				t.Fatal(err)
			}
			if ready {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("database condition did not become observable")
	}
	t.Run("parent_closure_commits_before_waiting_child", func(t *testing.T) {
		p, c, _ := cleanupProtocolFixture(t, db)
		_, pl, err := ClaimPending(t.Context(), db, cleanupIdentity(p), cleanupOptions(p), nil)
		if err != nil {
			t.Fatal(err)
		}
		holder := db.Begin()
		defer holder.Rollback()
		if _, err = LockActiveLease(t.Context(), holder, *pl); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
		defer cancel()
		pids := make(chan int, 1)
		done := make(chan error, 1)
		var child *TaskExecution
		var lease *Lease
		go func() {
			done <- db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
				var pid int
				if err := conn.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				pids <- pid
				var err error
				child, lease, err = ClaimCleanupChild(ctx, conn, cleanupIdentity(c), cleanupOptions(c), nil)
				return err
			})
		}()

		var pid int
		select {
		case pid = <-pids:
		case err := <-done:
			t.Fatalf("claim ended before reaching lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		wait(t, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid=? AND wait_event_type='Lock')", pid)
		if err = CompleteWithLease(t.Context(), holder, *pl, ExecutionStatusTimeout, nil); err != nil {
			t.Fatal(err)
		}
		if err = holder.Commit().Error; err != nil {
			t.Fatal(err)
		}
		if err = <-done; err != nil || child == nil || child.Status != ExecutionStatusFailed || lease != nil {
			t.Fatalf("closure race admitted child: %+v %+v %v", child, lease, err)
		}
	})
	t.Run("deadline_rechecked_after_owner_lock", func(t *testing.T) {
		p, c, input := cleanupProtocolFixture(t, db, 2*time.Second)
		if _, _, err := ClaimPending(t.Context(), db, cleanupIdentity(p), cleanupOptions(p), nil); err != nil {
			t.Fatal(err)
		}
		probe := "common.cleanup_probe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if err := db.Exec("CREATE TABLE " + probe + " (id integer PRIMARY KEY)").Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Exec("DROP TABLE " + probe).Error; err != nil {
				t.Error(err)
			}
		})
		if err := db.Exec("INSERT INTO " + probe + " VALUES (1)").Error; err != nil {
			t.Fatal(err)
		}
		holder := db.Begin()
		defer holder.Rollback()
		if err := holder.Exec("SELECT id FROM " + probe + " WHERE id=1 FOR UPDATE").Error; err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
		defer cancel()
		pids := make(chan int, 1)
		done := make(chan error, 1)
		var child *TaskExecution
		var lease *Lease
		go func() {
			var err error
			child, lease, err = ClaimCleanupChild(ctx, db, cleanupIdentity(c), cleanupOptions(c), func(tx *gorm.DB, _ TaskExecution, _ events.CleanupRequest) error {
				var pid int
				if err := tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				pids <- pid
				return tx.Exec("SELECT id FROM " + probe + " WHERE id=1 FOR UPDATE").Error
			})
			done <- err
		}()

		var pid int
		select {
		case pid = <-pids:
		case err := <-done:
			t.Fatalf("claim ended before reaching lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		wait(t, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid=? AND wait_event_type='Lock')", pid)
		wait(t, "SELECT clock_timestamp() >= ?", input.Deadline)
		if err := holder.Commit().Error; err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil || child == nil || child.Status != ExecutionStatusFailed || child.Attempt != 0 || lease != nil {
			t.Fatalf("deadline wait admitted runtime: %+v %+v %v", child, lease, err)
		}
		parent := readCleanupExecution(t, db, p)
		now, _ := DatabaseNow(db)
		view := ProjectCleanupAdmission(*child, &parent, now)
		if view == nil || view.Basis != "deadline_reached" || view.State != "closed_rejected" {
			t.Fatalf("committed deadline refusal not projected: %+v", view)
		}
	})
}
