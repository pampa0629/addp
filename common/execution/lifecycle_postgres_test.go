package execution

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func TestExecutionLifecycleAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_EXECUTION_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_EXECUTION_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	fixture := func(t *testing.T) TaskExecution {
		t.Helper()
		item := TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: ModuleMeta, TaskType: TaskTypeScan, Source: uuid.NewString(), Status: ExecutionStatusPending, ExecutionBoundary: ExecutionBoundaryBounded, TriggerType: TriggerTypeManual}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			for _, model := range []any{&Event{}, &TaskExecution{}} {
				if err := db.Where("execution_id = ?", item.ExecutionID).Delete(model).Error; err != nil {
					t.Error(err)
				}
				var n int64
				if err := db.Model(model).Where("execution_id = ?", item.ExecutionID).Count(&n).Error; err != nil || n != 0 {
					t.Errorf("fixture remaining=%d err=%v", n, err)
				}
			}
		})
		return item
	}
	options := func(item TaskExecution) ClaimOptions {
		return ClaimOptions{Module: item.Module, TaskType: item.TaskType, Source: item.Source, WorkerID: "lifecycle-test", LeaseDuration: time.Minute}
	}
	read := func(t *testing.T, id string) TaskExecution {
		t.Helper()
		var item TaskExecution
		if err := db.Where("execution_id = ?", id).First(&item).Error; err != nil {
			t.Fatal(err)
		}
		return item
	}
	t.Run("single_claim_and_terminal_clock", func(t *testing.T) {
		item := fixture(t)
		var wg sync.WaitGroup
		leases := make(chan *Lease, 8)
		errs := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, lease, err := ClaimNext(t.Context(), db, options(item))
				leases <- lease
				errs <- err
			}()
		}
		wg.Wait()
		close(leases)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var owner *Lease
		count := 0
		for lease := range leases {
			if lease != nil {
				owner = lease
				count++
			}
		}
		if count != 1 {
			t.Fatalf("claims=%d", count)
		}
		claimed := read(t, item.ExecutionID)
		if claimed.Attempt != 1 || claimed.StartedAt == nil || !claimed.CreatedAt.Equal(item.CreatedAt) {
			t.Fatalf("invalid claim: %+v", claimed)
		}
		protected := []string{"id", "execution_id", "tenant_id", "module", "task_type", "source", "source_task_id", "parent_execution_id", "execution_config", "execution_boundary", "retry_of_execution_id", "created_at", "started_at", "completed_at", "updated_at", "execution_time_ms", "attempt", "max_attempts", "lease_owner", "lease_token", "lease_expires_at", "status", "execution_authorization_id", "authorization_expires_at"}
		for _, key := range protected {
			if err := UpdateWithLease(t.Context(), db, *owner, map[string]any{key: nil}); err == nil {
				t.Fatalf("accepted protected %s", key)
			}
		}
		fields := map[string]any{"progress": 100, "metadata": models.JSONMap{"result": "ok"}}
		before := map[string]any{"progress": 100, "metadata": models.JSONMap{"result": "ok"}}
		if err := CompleteWithLease(t.Context(), db, *owner, ExecutionStatusSuccess, fields); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(fields, before) {
			t.Fatal("mutated caller fields")
		}
		done := read(t, item.ExecutionID)
		if done.CompletedAt == nil || done.ExecutionTimeMs == nil || *done.ExecutionTimeMs != done.CompletedAt.Sub(*done.StartedAt).Milliseconds() || !done.StartedAt.Equal(*claimed.StartedAt) || done.LeaseToken != nil {
			t.Fatalf("terminal timing/lease=%+v", done)
		}
		if err := CompleteWithLease(t.Context(), db, *owner, ExecutionStatusFailed, nil); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("terminal overwrite=%v", err)
		}
	})
	t.Run("wait_for_row_lock_past_expiry", func(t *testing.T) {
		item := fixture(t)
		_, lease, err := ClaimNext(t.Context(), db, options(item))
		if err != nil {
			t.Fatal(err)
		}
		holder := db.Begin()
		if holder.Error != nil {
			t.Fatal(holder.Error)
		}
		defer holder.Rollback()
		var locked TaskExecution
		if err := holder.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ?", item.ExecutionID).First(&locked).Error; err != nil {
			t.Fatal(err)
		}
		if err := holder.Model(&locked).Update("lease_expires_at", gorm.Expr("clock_timestamp() + interval '600 milliseconds'")).Error; err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		pids := make(chan int, 1)
		result := make(chan error, 1)
		go func() {
			result <- db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
				var pid int
				if err := conn.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				pids <- pid
				return CompleteWithEvent(ctx, conn, *lease, ExecutionStatusSuccess, nil)
			})
		}()
		var pid int
		select {
		case pid = <-pids:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		waitFor := func(query string, args ...any) {
			t.Helper()
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				var ok bool
				if err := holder.Raw(query, args...).Scan(&ok).Error; err != nil {
					t.Fatal(err)
				}
				if ok {
					return
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-ticker.C:
				}
			}
		}
		waitFor("SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = ? AND wait_event_type = 'Lock')", pid)
		waitFor("SELECT clock_timestamp() >= lease_expires_at FROM common.task_executions WHERE execution_id = ?", item.ExecutionID)
		if err := holder.Commit().Error; err != nil {
			t.Fatal(err)
		}
		if err := <-result; !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("expired lock waiter=%v", err)
		}
		stored := read(t, item.ExecutionID)
		var n int64
		if err := db.Model(&Event{}).Where("execution_id = ?", item.ExecutionID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Status != ExecutionStatusRunning || stored.CompletedAt != nil || n != 0 {
			t.Fatalf("stale commit or event: status=%s events=%d", stored.Status, n)
		}
		if err := RetryExpired(t.Context(), db, *lease, "retry pending"); err != nil {
			t.Fatal(err)
		}
		pending := read(t, item.ExecutionID)
		if pending.StartedAt != nil || pending.CompletedAt != nil || pending.ExecutionTimeMs != nil || pending.LeaseToken != nil {
			t.Fatal("retry retained attempt facts")
		}
		again, newLease, err := ClaimNext(t.Context(), db, options(item))
		if err != nil {
			t.Fatal(err)
		}
		if again == nil || newLease.Attempt != 2 || newLease.Token == lease.Token || !again.StartedAt.After(*locked.StartedAt) {
			t.Fatal("retry did not create new attempt")
		}
		if err := UpdateWithLease(t.Context(), db, *lease, map[string]any{"progress": 99}); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("stale progress=%v", err)
		}
	})
	t.Run("identity_boundary_and_transaction_rollback", func(t *testing.T) {
		item := fixture(t)
		_, lease, err := ClaimNext(t.Context(), db, options(item))
		if err != nil {
			t.Fatal(err)
		}
		for _, mutate := range []func(*Lease){func(l *Lease) { l.TenantID++ }, func(l *Lease) { l.Attempt++ }, func(l *Lease) { l.Owner = "another" }, func(l *Lease) { l.Token = uuid.NewString() }} {
			wrong := *lease
			mutate(&wrong)
			if err := UpdateWithLease(t.Context(), db, wrong, map[string]any{"progress": 90}); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("wrong lease=%v", err)
			}
		}
		if err := db.Model(&item).Update("execution_boundary", ExecutionBoundaryContinuous).Error; err != nil {
			t.Fatal(err)
		}
		if err := RenewLease(t.Context(), db, *lease, time.Minute); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("bounded write into continuous=%v", err)
		}
		if err := db.Model(&item).Update("execution_boundary", ExecutionBoundaryBounded).Error; err != nil {
			t.Fatal(err)
		}
		rejected := errors.New("owner transaction rejected")
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := UpdateWithEvent(t.Context(), tx, *lease, map[string]any{"progress": 50}, EventInput{Kind: "progress"}); err != nil {
				return err
			}
			if err := CompleteWithEvent(t.Context(), tx, *lease, ExecutionStatusSuccess, nil); err != nil {
				return err
			}
			return rejected
		})
		if !errors.Is(err, rejected) {
			t.Fatal(err)
		}
		stored := read(t, item.ExecutionID)
		var n int64
		if err := db.Model(&Event{}).Where("execution_id = ?", item.ExecutionID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Status != ExecutionStatusRunning || stored.Progress != 0 || stored.CompletedAt != nil || n != 0 {
			t.Fatalf("partial transaction: status=%s progress=%d events=%d", stored.Status, stored.Progress, n)
		}
		if err := CompleteWithLease(t.Context(), db, *lease, ExecutionStatusSuccess, map[string]any{"metadata": models.JSONMap{"cleanup_receipt": map[string]any{}}}); err == nil {
			t.Fatal("untyped receipt accepted")
		}
	})
	t.Run("exact_pending_and_unstarted_termination", func(t *testing.T) {
		item := fixture(t)
		identity := PendingIdentity{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Module: item.Module, TaskType: item.TaskType, Source: item.Source}
		past := time.Now().UTC().Add(-time.Second)
		if _, _, err := ClaimPending(t.Context(), db, identity, options(item), &past); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("closed claim=%v", err)
		}
		if err := RejectUnstarted(t.Context(), db, identity, map[string]any{"error_details": models.JSONMap{"code": "admission_closed"}}); err != nil {
			t.Fatal(err)
		}
		stored := read(t, item.ExecutionID)
		if stored.Status != ExecutionStatusFailed || stored.Attempt != 0 || stored.StartedAt != nil || stored.ExecutionTimeMs != nil || stored.CompletedAt == nil || stored.LeaseToken != nil {
			t.Fatalf("unstarted facts=%+v", stored)
		}
		if err := CancelUnstarted(t.Context(), db, identity); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("terminal rewrite=%v", err)
		}
		next := fixture(t)
		identity = PendingIdentity{ExecutionID: next.ExecutionID, TenantID: next.TenantID, Module: next.Module, TaskType: next.TaskType, Source: next.Source}
		if err := db.Model(&next).Update("attempt", 1).Error; err != nil {
			t.Fatal(err)
		}
		if err := RejectUnstarted(t.Context(), db, identity, nil); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("recovered pending treated as unstarted=%v", err)
		}
	})
	t.Run("owner_lock_wait_rolls_back_domain_and_terminal", func(t *testing.T) {
		item := fixture(t)
		_, lease, err := ClaimNext(t.Context(), db, options(item))
		if err != nil {
			t.Fatal(err)
		}
		probe := "common.lifecycle_probe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if err := db.Exec("CREATE TABLE " + probe + " (id integer PRIMARY KEY, value integer NOT NULL)").Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Exec("DROP TABLE " + probe).Error; err != nil {
				t.Error(err)
			}
		})
		if err := db.Exec("INSERT INTO " + probe + " VALUES (1, 0)").Error; err != nil {
			t.Fatal(err)
		}
		holder := db.Begin()
		defer holder.Rollback()
		if err := holder.Exec("SELECT id FROM " + probe + " WHERE id = 1 FOR UPDATE").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&item).Update("lease_expires_at", gorm.Expr("clock_timestamp() + interval '800 milliseconds'")).Error; err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		pids, result := make(chan int, 1), make(chan error, 1)
		go func() {
			result <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if _, err := LockActiveLease(ctx, tx, *lease); err != nil {
					return err
				}
				var pid int
				if err := tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				pids <- pid
				if err := tx.Exec("UPDATE " + probe + " SET value = 1 WHERE id = 1").Error; err != nil {
					return err
				}
				return CompleteWithEvent(ctx, tx, *lease, ExecutionStatusSuccess, nil)
			})
		}()
		var pid int
		select {
		case pid = <-pids:
		case err := <-result:
			t.Fatalf("owner lock preparation: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			var ready bool
			if err := db.Raw("SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = ? AND wait_event_type = 'Lock') AND clock_timestamp() >= lease_expires_at FROM common.task_executions WHERE execution_id = ?", pid, item.ExecutionID).Scan(&ready).Error; err != nil {
				t.Fatal(err)
			}
			if ready {
				break
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		if err := holder.Commit().Error; err != nil {
			t.Fatal(err)
		}
		if err := <-result; !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("owner-lock waiter completion: %v", err)
		}
		var value int
		if err := db.Raw("SELECT value FROM " + probe + " WHERE id = 1").Scan(&value).Error; err != nil {
			t.Fatal(err)
		}
		var events int64
		if err := db.Model(&Event{}).Where("execution_id = ?", item.ExecutionID).Count(&events).Error; err != nil {
			t.Fatal(err)
		}
		stored := read(t, item.ExecutionID)
		if value != 0 || events != 0 || stored.Status != ExecutionStatusRunning || stored.CompletedAt != nil {
			t.Fatalf("partial owner commit: value=%d events=%d status=%s", value, events, stored.Status)
		}
	})
	t.Run("attempt_authorization_and_recovered_pending_rejection", func(t *testing.T) {
		item := fixture(t)
		actor := int64(31)
		parent := uuid.NewString()
		if err := db.Model(&item).Updates(map[string]any{"actor_principal_id": actor, "actor_tenant_membership_id": actor, "issued_authorization_version": actor, "source": ModuleOrchestrator, "parent_execution_id": parent}).Error; err != nil {
			t.Fatal(err)
		}
		item.Source = ModuleOrchestrator
		_, lease, err := ClaimPending(t.Context(), db, PendingIdentity{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Module: item.Module, TaskType: item.TaskType, Source: item.Source}, options(item), nil)
		if err != nil {
			t.Fatal(err)
		}
		now, err := DatabaseNow(db)
		if err != nil {
			t.Fatal(err)
		}
		if err := AttachAuthorizationWithLease(t.Context(), db, *lease, 51, now); err == nil {
			t.Fatal("expired authorization accepted")
		}
		if err := AttachAuthorizationWithLease(t.Context(), db, *lease, 51, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := AttachAuthorizationWithLease(t.Context(), db, *lease, 52, now.Add(time.Hour)); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("authorization replacement=%v", err)
		}
		if err := db.Model(&item).Update("lease_expires_at", gorm.Expr("clock_timestamp()")).Error; err != nil {
			t.Fatal(err)
		}
		if err := RetryExpired(t.Context(), db, *lease, "retry"); err != nil {
			t.Fatal(err)
		}
		stored := read(t, item.ExecutionID)
		if stored.ExecutionAuthorizationID != nil || stored.AuthorizationExpiresAt != nil || stored.ActorPrincipalID == nil || *stored.ActorPrincipalID != actor {
			t.Fatal("retry authorization lineage was not preserved correctly")
		}
		id := PendingIdentity{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Module: item.Module, TaskType: item.TaskType, Source: item.Source}
		if err := FailPending(t.Context(), db, id, map[string]any{"error_details": models.JSONMap{"code": "authorization_denied"}}); err != nil {
			t.Fatal(err)
		}
		stored = read(t, item.ExecutionID)
		if stored.Status != ExecutionStatusFailed || stored.Attempt != 1 || stored.StartedAt != nil || stored.ExecutionTimeMs != nil {
			t.Fatal("pending denial manufactured attempt facts")
		}
	})

}
