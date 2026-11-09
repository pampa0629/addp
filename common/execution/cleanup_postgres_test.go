package execution

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func testCleanupPostgresAdmissionEvidence(t *testing.T, db *gorm.DB) {
	t.Run("invalid_binding", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			columns map[string]any
			request func(*events.CleanupRequest)
		}{
			{name: "tenant", request: func(r *events.CleanupRequest) { r.TenantID++ }},
			{name: "module", columns: map[string]any{"module": ModuleMeta}},
			{name: "type", columns: map[string]any{"task_type": TaskTypeScan}},
			{name: "source", columns: map[string]any{"source": ModuleMeta}},
			{name: "absent_parent", columns: map[string]any{"parent_execution_id": nil}},
			{name: "missing_parent_row", columns: map[string]any{"parent_execution_id": uuid.NewString()}},
			{name: "different_parent_digest", request: func(r *events.CleanupRequest) { r.RequestID = uuid.NewString() }},
			{name: "different_actor_membership", columns: map[string]any{"actor_tenant_membership_id": 18}},
			{name: "different_authorization_version", columns: map[string]any{"issued_authorization_version": 2}},
			{name: "deadline_not_after_request", request: func(r *events.CleanupRequest) { r.Deadline = r.RequestedAt }},
			{name: "deadline_exceeds_contract", request: func(r *events.CleanupRequest) { r.Deadline = r.RequestedAt.Add(25 * time.Hour) }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p, c, input := cleanupProtocolFixture(t, db)
				_, pl, err := ClaimPending(t.Context(), db, cleanupIdentity(p), cleanupOptions(p), nil)
				if err != nil {
					t.Fatal(err)
				}
				fields := tc.columns
				if tc.request != nil {
					tc.request(&input)
					// Recompute the digest even for invalid deadlines, so those
					// cases test time validation rather than a stale digest.
					err := input.Seal()
					if (err != nil) != strings.HasPrefix(tc.name, "deadline_") {
						t.Fatalf("unexpected fixture validation: %v", err)
					}
					raw, err := json.Marshal(input)
					if err != nil {
						t.Fatal(err)
					}
					fields = map[string]any{"execution_config": models.JSONMap{cleanupInputKey: string(raw), DisplayLanguageKey: "en"}}
				}
				if err := db.Model(&c).Updates(fields).Error; err != nil {
					t.Fatal(err)
				}
				stored := readCleanupExecution(t, db, c)
				// Use a recognized Owner for the wrong-module case, but keep
				// the original executor identity for wrong type and source.
				c.Module = stored.Module
				for _, phase := range []string{"active", "closed"} {
					t.Run(phase, func(t *testing.T) {
						if phase == "closed" {
							if err := CompleteWithLease(t.Context(), db, *pl, ExecutionStatusTimeout, nil); err != nil {
								t.Fatal(err)
							}
						}
						parent := readCleanupExecution(t, db, p)
						prepared := false
						item, lease, err := ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), cleanupOptions(c), func(*gorm.DB, TaskExecution, events.CleanupRequest) error {
							prepared = true
							return nil
						})
						if err == nil || item != nil || lease != nil || prepared {
							t.Fatalf("invalid binding reached claim or rejection: item=%t lease=%t prepare=%t err=%v", item != nil, lease != nil, prepared, err)
						}
						if !reflect.DeepEqual(stored, readCleanupExecution(t, db, c)) || !reflect.DeepEqual(parent, readCleanupExecution(t, db, p)) {
							t.Fatal("invalid binding changed persistent parent or child facts")
						}
					})
				}
			})
		}
	})
	t.Run("pending_with_prior_operation_facts", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			fields func(time.Time) map[string]any
		}{
			{"recovered_attempt", nil},
			{"started_at", func(now time.Time) map[string]any { return map[string]any{"started_at": now} }},
			{"elapsed", func(time.Time) map[string]any { return map[string]any{"execution_time_ms": 0} }},
			{"lease_owner", func(time.Time) map[string]any { return map[string]any{"lease_owner": "old-owner"} }},
			{"lease_token", func(time.Time) map[string]any { return map[string]any{"lease_token": uuid.NewString()} }},
			{"lease_expiry", func(now time.Time) map[string]any { return map[string]any{"lease_expires_at": now} }},
			{"progress", func(time.Time) map[string]any { return map[string]any{"progress": 1} }},
			{"incomplete_receipt", func(time.Time) map[string]any {
				return map[string]any{"metadata": models.JSONMap{"cleanup_receipt": map[string]any{}}}
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p, c, _ := cleanupProtocolFixture(t, db)
				_, pl, err := ClaimPending(t.Context(), db, cleanupIdentity(p), cleanupOptions(p), nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.fields == nil {
					options := cleanupOptions(c)
					options.LeaseDuration = 100 * time.Millisecond
					claimed, lease, err := ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), options, nil)
					if err != nil || claimed == nil || lease == nil {
						t.Fatalf("initial claim failed: %v", err)
					}
					waitCleanupPostgres(t, db, "SELECT clock_timestamp() >= ?", *claimed.LeaseExpiresAt)
					if err := RetryExpired(t.Context(), db, *lease, "recovered"); err != nil {
						t.Fatal(err)
					}
				} else {
					now, err := DatabaseNow(db)
					if err != nil {
						t.Fatal(err)
					}
					if err := db.Model(&c).Updates(tc.fields(now)).Error; err != nil {
						t.Fatal(err)
					}
				}
				if err := CompleteWithLease(t.Context(), db, *pl, ExecutionStatusTimeout, nil); err != nil {
					t.Fatal(err)
				}
				stored := readCleanupExecution(t, db, c)
				parent := readCleanupExecution(t, db, p)
				if stored.Status != ExecutionStatusPending {
					t.Fatal("fixture is not pending")
				}
				item, lease, err := ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), cleanupOptions(c), nil)
				if !errors.Is(err, commonapi.ErrConflict) || item != nil || lease != nil {
					t.Fatalf("prior operation facts treated as unstarted: %v", err)
				}
				if err := RejectUnstarted(t.Context(), db, cleanupIdentity(c), nil); !errors.Is(err, commonapi.ErrConflict) {
					t.Fatalf("unstarted primitive discarded prior operation facts: %v", err)
				}
				if ProjectCleanupAdmission(stored, &parent, *parent.CompletedAt) != nil {
					t.Fatal("prior operation facts projected as never started")
				}
				if !reflect.DeepEqual(stored, readCleanupExecution(t, db, c)) || !reflect.DeepEqual(parent, readCleanupExecution(t, db, p)) {
					t.Fatal("unstarted rejection changed persistent parent or child facts")
				}
			})
		}
	})
}

func waitCleanupPostgres(t *testing.T, db *gorm.DB, query string, args ...interface{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		var ready bool
		if err := db.WithContext(ctx).Raw(query, args...).Scan(&ready).Error; err != nil {
			t.Fatal(err)
		}
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("database condition did not become observable")
}

type cleanupPGClaim struct {
	item  *TaskExecution
	lease *Lease
	err   error
}

// Pin the claimant to one real PG connection so a barrier observes this exact
// backend waiting on its blocker, rather than inferring order from elapsed time.
func startCleanupPostgresClaim(t *testing.T, db *gorm.DB, child TaskExecution, prepare func(*gorm.DB, TaskExecution, events.CleanupRequest) error) (int, <-chan cleanupPGClaim) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	pids := make(chan int, 1)
	done := make(chan cleanupPGClaim, 1)
	go func() {
		var result cleanupPGClaim
		result.err = db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
			var pid int
			if err := conn.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				return err
			}
			pids <- pid
			options := cleanupOptions(child)
			options.WorkerID = uuid.NewString()
			var err error
			result.item, result.lease, err = ClaimCleanupChild(ctx, conn, cleanupIdentity(child), options, prepare)
			return err
		})
		done <- result
	}()
	select {
	case pid := <-pids:
		return pid, done
	case result := <-done:
		t.Fatalf("claim ended before publishing its backend: %v", result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return 0, done
}

func testCleanupPostgresLockOrder(t *testing.T, db *gorm.DB) {
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
		waitCleanupPostgres(t, db, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid=? AND wait_event_type='Lock')", pid)
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
	t.Run("clock_rechecked_after_lock", func(t *testing.T) {
		for _, lock := range []string{"parent", "child", "owner"} {
			for _, boundary := range []string{"deadline", "lease"} {
				t.Run(lock+"/"+boundary, func(t *testing.T) {
					budget := time.Minute
					if boundary == "deadline" {
						budget = 2 * time.Second
					}
					p, c, input := cleanupProtocolFixture(t, db, budget)
					probe := "common.cleanup_probe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
					if lock == "owner" {
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
					}
					options := cleanupOptions(p)
					if boundary == "lease" {
						options.LeaseDuration = 2 * time.Second
					}
					if _, _, err := ClaimPending(t.Context(), db, cleanupIdentity(p), options, nil); err != nil {
						t.Fatal(err)
					}
					parent, child := readCleanupExecution(t, db, p), readCleanupExecution(t, db, c)
					crossAt := input.Deadline
					if boundary == "lease" {
						crossAt = *parent.LeaseExpiresAt
					}
					holder := db.Begin()
					defer holder.Rollback()
					var blocker int
					if err := holder.Raw("SELECT pg_backend_pid()").Scan(&blocker).Error; err != nil {
						t.Fatal(err)
					}
					var prepare func(*gorm.DB, TaskExecution, events.CleanupRequest) error
					switch lock {
					case "parent", "child":
						id := p.ExecutionID
						if lock == "child" {
							id = c.ExecutionID
						}
						if err := holder.Exec("SELECT id FROM common.task_executions WHERE execution_id=? FOR UPDATE", id).Error; err != nil {
							t.Fatal(err)
						}
						prepare = func(*gorm.DB, TaskExecution, events.CleanupRequest) error {
							return errors.New("prepare called after admission boundary")
						}
					case "owner":
						lockRow := "SELECT id FROM " + probe + " WHERE id=1 FOR UPDATE"
						if err := holder.Exec(lockRow).Error; err != nil {
							t.Fatal(err)
						}
						prepare = func(tx *gorm.DB, _ TaskExecution, _ events.CleanupRequest) error {
							return tx.Exec(lockRow).Error
						}
					}
					pid, done := startCleanupPostgresClaim(t, db, c, prepare)
					waitCleanupPostgres(t, db, "SELECT ? = ANY(pg_blocking_pids(?))", blocker, pid)
					now, err := DatabaseNow(db)
					if err != nil || !now.Before(crossAt) {
						t.Fatalf("claim did not reach the lock before %s: %v", boundary, err)
					}
					waitCleanupPostgres(t, db, "SELECT clock_timestamp() >= ?", crossAt)
					if err := holder.Commit().Error; err != nil {
						t.Fatal(err)
					}
					result := <-done
					if result.err != nil || result.lease != nil {
						t.Fatalf("lock wait granted ownership or errored: lease=%t err=%v", result.lease != nil, result.err)
					}
					if !reflect.DeepEqual(parent, readCleanupExecution(t, db, p)) {
						t.Fatal("child admission changed the parent")
					}
					stored := readCleanupExecution(t, db, c)
					if boundary == "lease" {
						if result.item != nil || !reflect.DeepEqual(child, stored) {
							t.Fatal("temporary parent lease loss changed the child")
						}
						return
					}
					if result.item == nil || stored.Status != ExecutionStatusFailed || stored.CompletedAt == nil || !cleanupUnstarted(stored) {
						t.Fatal("deadline crossing did not commit an unstarted rejection")
					}
					view := ProjectCleanupAdmission(stored, &parent, *stored.CompletedAt)
					if view == nil || view.Basis != "deadline_reached" || view.State != "closed_rejected" {
						t.Fatalf("committed deadline refusal not projected: %+v", view)
					}
				})
			}
		}
	})
	t.Run("two_parents_same_owner_exact_single_claim", func(t *testing.T) {
		p, c, input := cleanupProtocolFixture(t, db)
		// A second independent request in the same Tenant targets the same Owner.
		other := p
		other.ID, other.ExecutionID = 0, uuid.NewString()
		other.ExecutionConfig = models.JSONMap{DisplayLanguageKey: "en"}
		input.RequestID, input.ParentExecutionID = uuid.NewString(), other.ExecutionID
		if err := input.Seal(); err != nil {
			t.Fatal(err)
		}
		children, err := CreateCleanupExecutions(t.Context(), db, &other, input)
		if err != nil {
			t.Fatal(err)
		}
		for _, parent := range []TaskExecution{p, other} {
			if _, _, err := ClaimPending(t.Context(), db, cleanupIdentity(parent), cleanupOptions(parent), nil); err != nil {
				t.Fatal(err)
			}
		}
		holder := db.Begin()
		defer holder.Rollback()
		var blocker int
		if err := holder.Raw("SELECT pg_backend_pid()").Scan(&blocker).Error; err != nil {
			t.Fatal(err)
		}
		if err := holder.Exec("SELECT id FROM common.task_executions WHERE execution_id IN ? ORDER BY id FOR UPDATE", []string{p.ExecutionID, other.ExecutionID}).Error; err != nil {
			t.Fatal(err)
		}
		var preparations atomic.Int32
		prepare := func(*gorm.DB, TaskExecution, events.CleanupRequest) error { preparations.Add(1); return nil }
		candidates := []TaskExecution{c, c, children[0], children[0]}
		results := make([]<-chan cleanupPGClaim, 0, len(candidates))
		for _, candidate := range candidates {
			pid, done := startCleanupPostgresClaim(t, db, candidate, prepare)
			waitCleanupPostgres(t, db, "WITH RECURSIVE blockers(pid) AS (SELECT ?::integer UNION SELECT unnest(pg_blocking_pids(pid)) FROM blockers) SELECT EXISTS (SELECT 1 FROM blockers WHERE pid=?)", pid, blocker)
			results = append(results, done)
		}
		if err := holder.Commit().Error; err != nil {
			t.Fatal(err)
		}
		wins := map[string]int{}
		conflicts := 0
		for i, done := range results {
			result := <-done
			if errors.Is(result.err, commonapi.ErrConflict) {
				conflicts++
				continue
			}
			if result.err != nil || result.item == nil || result.lease == nil || result.item.ExecutionID != candidates[i].ExecutionID || result.item.Attempt != 1 {
				t.Fatalf("claim selected another candidate or failed: %+v", result)
			}
			wins[result.item.ExecutionID]++
		}
		if conflicts != 2 || wins[c.ExecutionID] != 1 || wins[children[0].ExecutionID] != 1 || preparations.Load() != 2 {
			t.Fatalf("wins=%v conflicts=%d preparations=%d", wins, conflicts, preparations.Load())
		}
	})
	t.Run("child_claim_commits_before_parent_closure", func(t *testing.T) {
		p, c, input := cleanupProtocolFixture(t, db)
		_, pl, err := ClaimPending(t.Context(), db, cleanupIdentity(p), cleanupOptions(p), nil)
		if err != nil {
			t.Fatal(err)
		}
		holder := db.Begin()
		defer holder.Rollback()
		claimed, cl, err := ClaimCleanupChild(t.Context(), holder, cleanupIdentity(c), cleanupOptions(c), nil)
		if err != nil || cl == nil {
			t.Fatalf("claim: %v", err)
		}
		if visible := readCleanupExecution(t, db, c); visible.Status != ExecutionStatusPending || visible.Attempt != 0 {
			t.Fatal("uncommitted claim became visible")
		}
		var blocker int
		if err := holder.Raw("SELECT pg_backend_pid()").Scan(&blocker).Error; err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		pids, done := make(chan int, 1), make(chan error, 1)
		go func() {
			done <- db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
				var pid int
				if err := conn.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				pids <- pid
				return CompleteWithLease(ctx, conn, *pl, ExecutionStatusTimeout, nil)
			})
		}()
		select {
		case pid := <-pids:
			waitCleanupPostgres(t, db, "WITH RECURSIVE blockers(pid) AS (SELECT ?::integer UNION SELECT unnest(pg_blocking_pids(pid)) FROM blockers) SELECT EXISTS (SELECT 1 FROM blockers WHERE pid=?)", pid, blocker)
		case err := <-done:
			t.Fatalf("closure ended before its lock barrier: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if err := holder.Commit().Error; err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		closed := readCleanupExecution(t, db, p)
		if closed.Status != ExecutionStatusTimeout || readCleanupExecution(t, db, c).Status != ExecutionStatusRunning {
			t.Fatal("parent closure revoked an admitted child")
		}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := NewCleanupReceipt(*claimed, events.CleanupResultData{Module: c.Module, TenantID: uint(c.TenantID), TaskID: input.RequestID, Action: input.Action, Status: events.CleanupResultSuccess, Summary: events.CleanupResultSummary{RiskLevel: "low"}, Impact: &impact}, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := CompleteCleanupWithReceipt(t.Context(), db, *cl, receipt); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(closed, readCleanupExecution(t, db, p)) {
			t.Fatal("late child changed the parent conclusion")
		}
	})
	t.Run("concurrent_and_duplicate_unstarted_rejection", func(t *testing.T) {
		p, c, _ := cleanupProtocolFixture(t, db)
		_, pl, err := ClaimPending(t.Context(), db, cleanupIdentity(p), cleanupOptions(p), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := CompleteWithLease(t.Context(), db, *pl, ExecutionStatusTimeout, nil); err != nil {
			t.Fatal(err)
		}
		reference := testTraceReference()
		reference.ModuleName = c.Module
		c.Metadata, err = LatestTraceMetadata(nil, reference)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&TaskExecution{}).Where("execution_id = ?", c.ExecutionID).Update("metadata", c.Metadata).Error; err != nil {
			t.Fatal(err)
		}
		before := readCleanupExecution(t, db, c)
		holder := db.Begin()
		defer holder.Rollback()
		var blocker int
		if err := holder.Raw("SELECT pg_backend_pid()").Scan(&blocker).Error; err != nil {
			t.Fatal(err)
		}
		if err := holder.Exec("SELECT id FROM common.task_executions WHERE execution_id=? FOR UPDATE", p.ExecutionID).Error; err != nil {
			t.Fatal(err)
		}
		var preparations atomic.Int32
		prepare := func(*gorm.DB, TaskExecution, events.CleanupRequest) error { preparations.Add(1); return nil }
		var results []<-chan cleanupPGClaim
		for range 4 {
			pid, done := startCleanupPostgresClaim(t, db, c, prepare)
			waitCleanupPostgres(t, db, "WITH RECURSIVE blockers(pid) AS (SELECT ?::integer UNION SELECT unnest(pg_blocking_pids(pid)) FROM blockers) SELECT EXISTS (SELECT 1 FROM blockers WHERE pid=?)", pid, blocker)
			results = append(results, done)
		}
		if err := holder.Commit().Error; err != nil {
			t.Fatal(err)
		}
		rejections, conflicts := 0, 0
		for _, done := range results {
			result := <-done
			if errors.Is(result.err, commonapi.ErrConflict) {
				conflicts++
			} else if result.err == nil && result.item != nil && result.item.Status == ExecutionStatusFailed && result.lease == nil {
				rejections++
			} else {
				t.Fatalf("invalid rejection result: %+v", result)
			}
		}
		closed := readCleanupExecution(t, db, c)
		if rejections != 1 || conflicts != 3 || preparations.Load() != 0 || closed.Attempt != 0 || closed.CompletedAt == nil || closed.StartedAt != nil || closed.ExecutionTimeMs != nil || closed.LeaseToken != nil || closed.LeaseOwner != nil || closed.LeaseExpiresAt != nil || closed.Progress != 0 || !reflect.DeepEqual(closed.Metadata, before.Metadata) {
			t.Fatalf("rejection manufactured runtime/results: rejections=%d conflicts=%d child=%+v", rejections, conflicts, closed)
		}
		parent := readCleanupExecution(t, db, p)
		now, err := DatabaseNow(db)
		if err != nil {
			t.Fatal(err)
		}
		if view := ProjectCleanupAdmission(closed, &parent, now); view == nil || view.State != "closed_rejected" {
			t.Fatal("rejection did not commit the safe reason atomically")
		}
		if _, _, err := ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), cleanupOptions(c), prepare); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("duplicate rejection: %v", err)
		}
		if !reflect.DeepEqual(closed, readCleanupExecution(t, db, c)) {
			t.Fatal("duplicate rejection changed completed_at or other stored facts")
		}
		var n int64
		if err := db.Model(&Event{}).Where("execution_id = ?", c.ExecutionID).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("unstarted rejection created attempt events: count=%d err=%v", n, err)
		}
	})
	for _, mode := range []string{"prepare_failure", "claim_outer_rollback", "rejection_outer_rollback", "rejection_sql_failure"} {
		t.Run(mode, func(t *testing.T) {
			p, c, _ := cleanupProtocolFixture(t, db)
			_, pl, err := ClaimPending(t.Context(), db, cleanupIdentity(p), cleanupOptions(p), nil)
			if err != nil {
				t.Fatal(err)
			}
			rejection := strings.HasPrefix(mode, "rejection_")
			if rejection {
				if err := CompleteWithLease(t.Context(), db, *pl, ExecutionStatusTimeout, nil); err != nil {
					t.Fatal(err)
				}
			}
			before, parent := readCleanupExecution(t, db, c), readCleanupExecution(t, db, p)
			probe := "common.cleanup_rollback_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if err := db.Exec("CREATE TABLE " + probe + " (id integer PRIMARY KEY)").Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Exec("DROP TABLE " + probe).Error; err != nil {
					t.Error(err)
				}
			})
			if mode == "rejection_sql_failure" {
				function := probe + "_fault"
				trigger := "cleanup_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
				// This AFTER trigger proves the UPDATE and an actual PG write were
				// reached before PostgreSQL rejects the statement. Both must roll back.
				if err := db.Exec("CREATE FUNCTION " + function + "() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO " + probe + " VALUES (1); RAISE EXCEPTION 'cleanup test write failure' USING ERRCODE='23514'; RETURN NEW; END $$").Error; err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Exec("DROP TRIGGER IF EXISTS " + trigger + " ON common.task_executions; DROP FUNCTION " + function + "()").Error; err != nil {
						t.Error(err)
					}
				})
				if err := db.Exec("CREATE TRIGGER " + trigger + " AFTER UPDATE ON common.task_executions FOR EACH ROW WHEN (NEW.execution_id='" + c.ExecutionID + "' AND NEW.status='failed') EXECUTE FUNCTION " + function + "()").Error; err != nil {
					t.Fatal(err)
				}
				_, _, err = ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), cleanupOptions(c), nil)
				var sqlError interface{ SQLState() string }
				if !errors.As(err, &sqlError) || sqlError.SQLState() != "23514" {
					t.Fatalf("expected the injected PostgreSQL statement failure: %v", err)
				}
				if err := db.Exec("DROP TRIGGER " + trigger + " ON common.task_executions").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				injected := errors.New("owner transaction aborted")
				prepare := func(tx *gorm.DB, _ TaskExecution, _ events.CleanupRequest) error {
					if err := tx.Exec("INSERT INTO " + probe + " VALUES (1)").Error; err != nil {
						return err
					}
					if mode == "prepare_failure" {
						return injected
					}
					return nil
				}
				if mode == "prepare_failure" {
					_, _, err = ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), cleanupOptions(c), prepare)
				} else {
					err = db.Transaction(func(tx *gorm.DB) error {
						item, lease, err := ClaimCleanupChild(t.Context(), tx, cleanupIdentity(c), cleanupOptions(c), prepare)
						if err != nil {
							return err
						}
						if item == nil || (lease == nil) != rejection {
							return errors.New("inner transition did not reach its intended state")
						}
						if rejection {
							if err := tx.Exec("INSERT INTO " + probe + " VALUES (1)").Error; err != nil {
								return err
							}
						}
						return injected
					})
				}
				if !errors.Is(err, injected) {
					t.Fatalf("transaction did not reach the injected failure: %v", err)
				}
			}
			if !reflect.DeepEqual(before, readCleanupExecution(t, db, c)) || !reflect.DeepEqual(parent, readCleanupExecution(t, db, p)) {
				t.Fatal("rollback left partial lease, rejection reason, timestamp, or parent facts")
			}
			var n int64
			if err := db.Table(probe).Count(&n).Error; err != nil || n != 0 {
				t.Fatalf("owner write survived rollback: count=%d err=%v", n, err)
			}
			item, lease, err := ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), cleanupOptions(c), nil)
			if err != nil || item == nil || (lease == nil) != rejection || (!rejection && item.Attempt != 1) {
				t.Fatalf("rollback consumed admission or an attempt: %+v %+v %v", item, lease, err)
			}
		})
	}
}
