package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/common/authorization/authtest"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"encoding/json"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type cleanupPGFixture struct {
	db          *gorm.DB
	coordinator *cleanupCoordinator
	input       events.CleanupRequest
	parent      execution.TaskExecution
	probe       string
	ctx         context.Context
}

// These fixtures prove real PG transaction composition and locks. The probe
// substitutes only IAM issue/audit persistence; IAM policy is tested separately
// by TestCleanupAuthorizationAgainstPostgres in the isolated IAM gate.
func newCleanupPGFixture(t *testing.T, budget time.Duration) *cleanupPGFixture {
	t.Helper()
	dsn := os.Getenv("ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("System Cleanup PostgreSQL standard gate only")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	var database string
	if err := db.Raw("SELECT current_database()").Scan(&database).Error; err != nil {
		t.Fatal(err)
	}
	if database != "addp_test" && os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Fatalf("Cleanup test requires addp_test, got %q", database)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	if err := execution.EnsureStore(db.WithContext(ctx)); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('cleanup-gate-schema',0))").Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS system").Error; err != nil {
			return err
		}
		var exists bool
		if err := tx.Raw("SELECT to_regclass('system.cleanup_requests') IS NOT NULL").Scan(&exists).Error; err != nil {
			return err
		}
		if exists {
			if !tx.Migrator().HasColumn(&models.CleanupRequestRecord{}, "requested_scope") {
				data, err := migration.EmbeddedSQL.ReadFile("sql/000252_cleanup_requested_scope.up.sql")
				if err != nil {
					return err
				}
				return tx.Exec(string(data)).Error
			}
			return nil
		}
		data, err := migration.EmbeddedSQL.ReadFile("sql/000250_cleanup_durable_requests.up.sql")
		if err != nil {
			return err
		}
		if err := tx.Exec(string(data)).Error; err != nil {
			return err
		}
		data, err = migration.EmbeddedSQL.ReadFile("sql/000252_cleanup_requested_scope.up.sql")
		if err != nil {
			return err
		}
		return tx.Exec(string(data)).Error
	}); err != nil {
		t.Fatal(err)
	}
	probe := "cleanup_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.Exec("CREATE SCHEMA " + probe).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE " + probe + ".facts (kind text NOT NULL, identity text NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	now, _ := execution.DatabaseNow(db)
	actor, member, version, by := int64(7), int64(17), int64(1), 7
	parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 940000007, Module: execution.ModuleSystem, TaskType: execution.TaskTypeCleanup, Source: execution.ModuleSystem,
		Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, TriggeredBy: &by, ActorPrincipalID: &actor, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version,
		ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}, MaxAttempts: 3}
	input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: uint(parent.TenantID), RequestedBy: 7, Action: events.CleanupActionScan,
		TriggerType: execution.TriggerTypeManual, ExpectedModules: []string{"graph", "meta"}, RequestedAt: now, Deadline: now.Add(budget)}
	if err := input.Seal(); err != nil {
		t.Fatal(err)
	}
	s := newCleanupCoordinator(db)
	s.issue = func(ctx context.Context, tx *gorm.DB, e execution.TaskExecution) error {
		return tx.WithContext(ctx).Exec("INSERT INTO "+probe+".facts(kind,identity) VALUES ('authority',?)", e.ExecutionID).Error
	}
	s.audit = func(ctx context.Context, tx *gorm.DB, e iam.AuditEvent) error {
		return tx.WithContext(ctx).Exec("INSERT INTO "+probe+".facts(kind,identity) VALUES (?,?)", e.EventName, e.EntityID).Error
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		owned := db.WithContext(cleanupCtx)
		var ids []string
		if err := owned.Model(&execution.TaskExecution{}).Where("execution_id = ? OR parent_execution_id = ?", parent.ExecutionID, parent.ExecutionID).Pluck("execution_id", &ids).Error; err != nil {
			t.Error(err)
			return
		}
		for _, model := range []any{&execution.Event{}, &execution.TaskExecution{}} {
			if len(ids) > 0 {
				if err := owned.Where("execution_id IN ?", ids).Delete(model).Error; err != nil {
					t.Error(err)
				}
				var count int64
				if err := owned.Model(model).Where("execution_id IN ?", ids).Count(&count).Error; err != nil || count != 0 {
					t.Errorf("execution fixture remains=%d: %v", count, err)
				}
			}
		}
		if err := owned.Where("request_id = ?", input.RequestID).Delete(&models.CleanupRequestRecord{}).Error; err != nil {
			t.Error(err)
		}
		var count int64
		if err := owned.Model(&models.CleanupRequestRecord{}).Where("request_id = ?", input.RequestID).Count(&count).Error; err != nil || count != 0 {
			t.Errorf("request remains=%d: %v", count, err)
		}
		if err := owned.Exec("DROP SCHEMA " + probe + " CASCADE").Error; err != nil {
			t.Error(err)
		}
		var remains bool
		if err := owned.Raw("SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname=?)", probe).Scan(&remains).Error; err != nil || remains {
			t.Errorf("probe remains=%v: %v", remains, err)
		}
	})
	return &cleanupPGFixture{db: db, coordinator: s, input: input, parent: parent, probe: probe, ctx: ctx}
}

func (f *cleanupPGFixture) accept(t *testing.T) {
	t.Helper()
	if _, err := f.coordinator.accept(f.ctx, f.input, f.parent, "", f.input.ExpectedModules); err != nil {
		t.Fatal(err)
	}
}
func (f *cleanupPGFixture) snapshot(t *testing.T) *cleanupSnapshot {
	t.Helper()
	value, err := f.coordinator.snapshot(f.ctx, f.input.RequestID, f.input.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func (f *cleanupPGFixture) advance(t *testing.T, worker string) {
	t.Helper()
	if err := f.coordinator.advance(f.ctx, f.parent.ExecutionID, worker); err != nil {
		t.Fatal(err)
	}
}
func cleanupPending(e execution.TaskExecution) execution.PendingIdentity {
	return execution.PendingIdentity{ExecutionID: e.ExecutionID, TenantID: e.TenantID, Module: e.Module, TaskType: e.TaskType, Source: e.Source}
}
func cleanupClaimOptions(e execution.TaskExecution) execution.ClaimOptions {
	return execution.ClaimOptions{Module: e.Module, TaskType: e.TaskType, Source: e.Source, WorkerID: "owner-fixture", LeaseDuration: time.Minute}
}

func waitCleanupPGCondition(t *testing.T, f *cleanupPGFixture, query string, args ...interface{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		var ready bool
		if err := f.db.WithContext(ctx).Raw(query, args...).Scan(&ready).Error; err != nil {
			t.Fatal(err)
		}
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Cleanup PG condition did not become observable")
}

func TestIntegrationPostgresCleanupAcceptanceAtomicity(t *testing.T) {
	t.Run("engine_workflow", testCleanupEngineWorkflowPostgres)
	t.Run("meta_pair_required", testCleanupMetaPairPostgres)
	f := newCleanupPGFixture(t, time.Minute)
	audit := f.coordinator.audit
	f.coordinator.audit = func(context.Context, *gorm.DB, iam.AuditEvent) error { return errors.New("audit unavailable") }
	if _, err := f.coordinator.accept(f.ctx, f.input, f.parent, "", f.input.ExpectedModules); err == nil {
		t.Fatal("accepted without audit")
	}
	for _, query := range []string{"SELECT count(*) FROM system.cleanup_requests WHERE request_id='" + f.input.RequestID + "'", "SELECT count(*) FROM common.task_executions WHERE execution_id='" + f.parent.ExecutionID + "' OR parent_execution_id='" + f.parent.ExecutionID + "'", "SELECT count(*) FROM " + f.probe + ".facts"} {
		var n int64
		if err := f.db.Raw(query).Scan(&n).Error; err != nil || n != 0 {
			t.Fatalf("non-atomic acceptance n=%d err=%v", n, err)
		}
	}
	f.coordinator.audit = audit
	f.accept(t)
	got := f.snapshot(t)
	if len(got.Children) != 2 || got.Parent.Status != "pending" || got.Parent.StartedAt != nil {
		t.Fatalf("bad accepted state: %+v", got.Parent)
	}
	var n int64
	f.db.Table(f.probe + ".facts").Count(&n)
	if n != 3 {
		t.Fatalf("authority and audit count=%d", n)
	}
	if err := f.db.Model(&models.CleanupRequestRecord{}).Where("request_id=?", f.input.RequestID).Update("protocol", "{}").Error; err == nil {
		t.Fatal("immutable request was modified")
	}
}

func TestIntegrationPostgresCleanupIdempotence(t *testing.T) {
	f := newCleanupPGFixture(t, time.Minute)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.coordinator.accept(f.ctx, f.input, f.parent, "", f.input.ExpectedModules)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	first := f.snapshot(t)
	retry := f.input
	retry.ParentExecutionID = uuid.NewString()
	retry.RequestedAt = retry.RequestedAt.Add(time.Second)
	retry.Deadline = retry.Deadline.Add(time.Second)
	if err := retry.Seal(); err != nil {
		t.Fatal(err)
	}
	record, err := f.coordinator.accept(f.ctx, retry, f.parent, "", f.input.ExpectedModules)
	if err != nil || record.ParentExecutionID != f.parent.ExecutionID {
		t.Fatalf("retry recreated: %v", err)
	}
	second := f.snapshot(t)
	if first.Record.Protocol != second.Record.Protocol || len(second.Children) != 2 || first.Parent.ExecutionID != second.Parent.ExecutionID {
		t.Fatal("retry changed accepted facts")
	}
	wrong := f.input
	wrong.ExpectedModules = []string{"meta"}
	_ = wrong.Seal()
	if _, err := f.coordinator.accept(f.ctx, wrong, f.parent, "", wrong.ExpectedModules); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("different intent accepted: %v", err)
	}
	wrong = f.input
	wrong.RequestedBy = 8
	_ = wrong.Seal()
	if _, err := f.coordinator.accept(f.ctx, wrong, f.parent, "", wrong.ExpectedModules); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("different actor accepted: %v", err)
	}

	// Identical submitted scope reuses the accepted discovery even when the
	// registry is unavailable or its current expected module set has changed.
	userCtx := execution.WithUserOrigin(f.ctx, authtest.NewTenantUserAuthContext(strconv.Itoa(int(f.input.TenantID)), "7", []string{"system.cleanup.execute"}))
	userCtx, err = commoni18n.WithLanguage(userCtx, "en")
	if err != nil {
		t.Fatal(err)
	}
	submission := cleanupSubmission{RequestID: f.input.RequestID, TenantID: f.input.TenantID, RequestedBy: 7, Action: "scan", Scope: f.input.ExpectedModules, TriggerType: "manual"}
	accepted, err := f.coordinator.acceptUser(userCtx, submission, func([]string, string) ([]string, error) { t.Fatal("retry re-ran discovery"); return nil, nil })
	if err != nil || accepted.ParentExecutionID != f.parent.ExecutionID {
		t.Fatal("retry did not reuse frozen discovery", err)
	}
	submission.Scope = nil
	if _, err := f.coordinator.acceptUser(userCtx, submission, nil); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatal("auto scope replaced explicit original scope", err)
	}
	changedDiscovery := f.input
	changedDiscovery.ExpectedModules = []string{"meta"}
	if err := changedDiscovery.Seal(); err != nil {
		t.Fatal(err)
	}
	if accepted, err := f.coordinator.accept(f.ctx, changedDiscovery, f.parent, "", f.input.ExpectedModules); err != nil || accepted.Protocol != first.Record.Protocol {
		t.Fatal("concurrent discovery changed accepted facts", err)
	}
	var n int64
	f.db.Table(f.probe + ".facts").Count(&n)
	if n != 3 {
		t.Fatalf("retry reissued authority/audit=%d", n)
	}
}

func TestIntegrationPostgresCleanupCoordinatorRecovery(t *testing.T) {
	f := newCleanupPGFixture(t, 400*time.Millisecond)
	f.accept(t)
	f.advance(t, "first")
	before := f.snapshot(t)
	oldLease, err := execution.LeaseFromExecution(before.Parent)
	if err != nil {
		t.Fatal(err)
	}
	// Use the real expired-attempt transition after a bounded lease expires.
	if err := execution.RenewLease(f.ctx, f.db, oldLease, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("SELECT pg_sleep(0.15)").Error; err != nil {
		t.Fatal(err)
	}
	f.advance(t, "recovered")
	after := f.snapshot(t)
	if after.Parent.Attempt != 2 || after.Parent.LeaseToken == nil || *after.Parent.LeaseToken == oldLease.Token || after.Record.Protocol != before.Record.Protocol || len(after.Children) != 2 {
		t.Fatal("recovery changed input or failed to acquire a fresh attempt")
	}
	if err := execution.CompleteWithLease(f.ctx, f.db, oldLease, "success", nil); err == nil {
		t.Fatal("old coordinator terminal accepted")
	}
	if err := f.db.Exec("SELECT pg_sleep(0.45)").Error; err != nil {
		t.Fatal(err)
	}
	f.advance(t, "recovered")
	closed := f.snapshot(t)
	cutoff, err := closed.Record.FrozenCutoff()
	if err != nil || cutoff == nil || closed.Parent.Status != "timeout" || len(cutoff.Missing) != 2 || len(cutoff.Receipts) != 0 {
		t.Fatalf("cutoff %+v err=%v", cutoff, err)
	}
	for _, c := range closed.Children {
		if c.Status != "pending" || c.Attempt != 0 || c.StartedAt != nil {
			t.Fatal("parent fabricated child state")
		}
	}
	f.advance(t, "recovered-again")
	if *f.snapshot(t).Record.Cutoff != *closed.Record.Cutoff {
		t.Fatal("parent cutoff changed")
	}
}

func TestIntegrationPostgresCleanupParentRecoveryAdmission(t *testing.T) {
	f := newCleanupPGFixture(t, time.Minute)
	f.accept(t)
	f.advance(t, "first")
	before := f.snapshot(t)
	old, err := execution.LeaseFromExecution(before.Parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.RenewLease(f.ctx, f.db, old, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	var expiry time.Time
	if err := f.db.Model(&execution.TaskExecution{}).Select("lease_expires_at").Where("execution_id=?", old.ExecutionID).Scan(&expiry).Error; err != nil {
		t.Fatal(err)
	}
	waitCleanupPGCondition(t, f, "SELECT clock_timestamp() >= ?", expiry)
	child := before.Children[0]
	item, lease, err := execution.ClaimCleanupChild(f.ctx, f.db, cleanupPending(child), cleanupClaimOptions(child), nil)
	if err != nil || item != nil || lease != nil || !reflect.DeepEqual(before.Children, f.snapshot(t).Children) {
		t.Fatalf("temporary parent lease loss rejected or claimed the child: %v", err)
	}
	f.advance(t, "recovered")
	after := f.snapshot(t)
	if after.Parent.Attempt != old.Attempt+1 || after.Parent.LeaseToken == nil || *after.Parent.LeaseToken == old.Token || after.Record.Protocol != before.Record.Protocol || after.Record.Cutoff != nil || !reflect.DeepEqual(before.Children, after.Children) {
		t.Fatal("parent recovery changed deadline/children or failed to acquire a new attempt")
	}
	if err := execution.RenewLease(f.ctx, f.db, old, time.Minute); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("stale parent regained ownership: %v", err)
	}
	item, lease, err = execution.ClaimCleanupChild(f.ctx, f.db, cleanupPending(child), cleanupClaimOptions(child), nil)
	if err != nil || item == nil || lease == nil || item.ExecutionID != child.ExecutionID || item.Attempt != 1 || item.StartedAt == nil {
		t.Fatalf("same child was not admitted after recovery: %+v %+v %v", item, lease, err)
	}
	if !reflect.DeepEqual(after.Parent, f.snapshot(t).Parent) {
		t.Fatal("child admission changed the recovered coordinator")
	}
}

func TestIntegrationPostgresCleanupInvalidBinding(t *testing.T) {
	for _, tc := range []struct {
		name, snapshotError, advanceError string
	}{
		{"parent_digest", "invalid cleanup parent binding", "cleanup request/parent mismatch"},
		{"child_digest", "invalid cleanup child binding", "invalid cleanup child binding"},
		{"missing_child", "accepted cleanup child is missing", "accepted cleanup child is missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCleanupPGFixture(t, time.Minute)
			f.accept(t)
			before := f.snapshot(t)
			child := before.Children[0]
			switch tc.name {
			case "parent_digest", "child_digest":
				row := before.Parent
				if tc.name == "child_digest" {
					row = child
				}
				// Each individual protocol is valid. Only the frozen binding
				// between it and the accepted System request is incorrect.
				input := before.Input
				input.Deadline = input.Deadline.Add(time.Second)
				if err := input.Seal(); err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				row.ExecutionConfig["cleanup_request"] = string(raw)
				if err := f.db.Model(&row).Update("execution_config", row.ExecutionConfig).Error; err != nil {
					t.Fatal(err)
				}
			case "missing_child":
				if err := f.db.Delete(&child).Error; err != nil {
					t.Fatal(err)
				}
			}
			var parent execution.TaskExecution
			if err := f.db.Where("execution_id = ?", before.Parent.ExecutionID).First(&parent).Error; err != nil {
				t.Fatal(err)
			}
			var children []execution.TaskExecution
			readChildren := func(rows *[]execution.TaskExecution) {
				t.Helper()
				if err := f.db.Where("parent_execution_id = ?", parent.ExecutionID).Order("module").Find(rows).Error; err != nil {
					t.Fatal(err)
				}
			}
			readChildren(&children)
			if _, err := f.coordinator.snapshot(f.ctx, f.input.RequestID, f.input.TenantID); err == nil || err.Error() != tc.snapshotError {
				t.Fatalf("snapshot did not reject the expected binding: %v", err)
			}
			if err := f.coordinator.advance(f.ctx, parent.ExecutionID, "invalid-binding"); err == nil || err.Error() != tc.advanceError {
				t.Fatalf("coordinator did not reach the expected binding check: %v", err)
			}
			var afterParent execution.TaskExecution
			var afterRecord models.CleanupRequestRecord
			var afterChildren []execution.TaskExecution
			if err := f.db.Where("execution_id = ?", parent.ExecutionID).First(&afterParent).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.Where("request_id = ?", f.input.RequestID).First(&afterRecord).Error; err != nil {
				t.Fatal(err)
			}
			readChildren(&afterChildren)
			var terminalAudits int64
			if err := f.db.Table(f.probe+".facts").Where("kind IN ?", []string{"cleanup.completed", "cleanup.failed"}).Count(&terminalAudits).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(parent, afterParent) || !reflect.DeepEqual(children, afterChildren) || !reflect.DeepEqual(before.Record, afterRecord) || terminalAudits != 0 {
				t.Fatal("invalid binding left a parent attempt, child update, cutoff or terminal audit")
			}
		})
	}
}

func TestIntegrationPostgresCleanupClosureRollback(t *testing.T) {
	f := newCleanupPGFixture(t, 500*time.Millisecond)
	f.accept(t)
	f.advance(t, "parent")
	waitCleanupPGCondition(t, f, "SELECT clock_timestamp() >= ?", f.input.Deadline)
	before := f.snapshot(t)
	var facts int64
	if err := f.db.Table(f.probe + ".facts").Count(&facts).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("closure audit persistence failed")
	audit := f.coordinator.audit
	f.coordinator.audit = func(ctx context.Context, tx *gorm.DB, event iam.AuditEvent) error {
		if err := audit(ctx, tx, event); err != nil {
			return err
		}
		return injected
	}
	if err := f.coordinator.advance(f.ctx, f.parent.ExecutionID, "parent"); !errors.Is(err, injected) {
		t.Fatalf("did not reach audit after cutoff and parent writes: %v", err)
	}
	after := f.snapshot(t)
	if !reflect.DeepEqual(before.Record, after.Record) || !reflect.DeepEqual(before.Parent, after.Parent) || !reflect.DeepEqual(before.Children, after.Children) || !reflect.DeepEqual(before.Input, after.Input) {
		t.Fatal("audit failure partially committed cutoff, parent terminal, or child state")
	}
	var afterFacts int64
	if err := f.db.Table(f.probe + ".facts").Count(&afterFacts).Error; err != nil || afterFacts != facts {
		t.Fatalf("failed audit write survived rollback: before=%d after=%d err=%v", facts, afterFacts, err)
	}
	f.coordinator.audit = audit
	f.advance(t, "parent")
	closed := f.snapshot(t)
	cutoff, err := closed.Record.FrozenCutoff()
	if err != nil || cutoff == nil || closed.Parent.Status != execution.ExecutionStatusTimeout || len(cutoff.Missing) != len(before.Children) || !reflect.DeepEqual(before.Children, closed.Children) {
		t.Fatalf("coordinator did not close the original request after rollback: %v", err)
	}
	if err := f.db.Table(f.probe + ".facts").Count(&afterFacts).Error; err != nil || afterFacts != facts+1 {
		t.Fatalf("closure audit count=%d err=%v", afterFacts, err)
	}
	f.advance(t, "parent")
	duplicate := f.snapshot(t)
	if !reflect.DeepEqual(closed.Record, duplicate.Record) || !reflect.DeepEqual(closed.Parent, duplicate.Parent) || !reflect.DeepEqual(closed.Children, duplicate.Children) || !reflect.DeepEqual(closed.Input, duplicate.Input) {
		t.Fatal("duplicate closure changed the frozen parent conclusion")
	}
}

func TestIntegrationPostgresCleanupAdmissionRace(t *testing.T) {
	f := newCleanupPGFixture(t, 900*time.Millisecond)
	f.accept(t)
	f.advance(t, "parent")
	snapshot := f.snapshot(t)
	child := snapshot.Children[0]
	claimed, lease, err := execution.ClaimCleanupChild(f.ctx, f.db, cleanupPending(child), cleanupClaimOptions(child), nil)
	if err != nil || lease == nil {
		t.Fatalf("child admission: %v", err)
	}
	// A valid child can finish after parent cutoff; it must not rewrite the
	// parent's missing list. Keep its own lease alive beyond the parent's budget.
	if err := execution.RenewLease(f.ctx, f.db, *lease, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("SELECT pg_sleep(1)").Error; err != nil {
		t.Fatal(err)
	}
	f.advance(t, "parent")
	closed := f.snapshot(t)
	impact, err := events.BuildCleanupImpactData(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	result := events.CleanupResultData{TaskID: f.input.RequestID, TenantID: f.input.TenantID, Module: child.Module, Action: "scan", Status: "success", Summary: events.CleanupResultSummary{RiskLevel: "low"}, Impact: &impact}
	receipt, err := execution.NewCleanupReceipt(*claimed, result, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.CompleteCleanupWithReceipt(f.ctx, f.db, *lease, receipt); err != nil {
		t.Fatal(err)
	}
	f.advance(t, "parent")
	if *closed.Record.Cutoff != *f.snapshot(t).Record.Cutoff {
		t.Fatal("late child rewrote parent cutoff")
	}
	other := snapshot.Children[1]
	// Hold P and observe that claim waits behind it, then sees committed closure.
	tx := f.db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	var p execution.TaskExecution
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id=?", f.parent.ExecutionID).First(&p).Error; err != nil {
		t.Fatal(err)
	}
	claimCtx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	var blocker int
	if err := tx.Raw("SELECT pg_backend_pid()").Scan(&blocker).Error; err != nil {
		t.Fatal(err)
	}
	pids := make(chan int, 1)
	done := make(chan error, 1)
	go func() {
		done <- f.db.WithContext(claimCtx).Connection(func(conn *gorm.DB) error {
			var pid int
			if err := conn.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				return err
			}
			pids <- pid
			item, l, err := execution.ClaimCleanupChild(claimCtx, conn, cleanupPending(other), cleanupClaimOptions(other), nil)
			if err == nil && (l != nil || item == nil || item.Status != "failed" || item.Attempt != 0 || item.StartedAt != nil) {
				return fmt.Errorf("claimed after closure")
			}
			return err
		})
	}()
	select {
	case pid := <-pids:
		waitCleanupPGCondition(t, f, "SELECT ? = ANY(pg_blocking_pids(?))", blocker, pid)
	case err := <-done:
		t.Fatalf("claim bypassed held parent: %v", err)
	case <-claimCtx.Done():
		t.Fatal(claimCtx.Err())
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationPostgresCleanupReadOnlySnapshot(t *testing.T) {
	f := newCleanupPGFixture(t, 100*time.Millisecond)
	f.accept(t)
	before := f.snapshot(t)
	if err := f.db.Exec("SELECT pg_sleep(0.15)").Error; err != nil {
		t.Fatal(err)
	}
	after := f.snapshot(t)
	if after.Record.Cutoff != nil || after.Parent.Status != "pending" || !reflect.DeepEqual(before.Parent, after.Parent) || !reflect.DeepEqual(before.Children, after.Children) {
		t.Fatal("read advanced state after deadline")
	}
	if _, err := f.coordinator.snapshot(f.ctx, f.input.RequestID, f.input.TenantID+1); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant read: %v", err)
	}
	for _, child := range after.Children {
		if view := execution.ProjectCleanupAdmission(child, &after.Parent, after.ObservedAt); view != nil {
			t.Fatal("deadline-only read asserted parent closure")
		}
	}
	resolver := &cleanupReadResolverStub{scopes: []execution.ReadScope{{Module: "graph", TenantID: int(f.input.TenantID), PrincipalID: int64(f.input.RequestedBy), Grants: []execution.ReadGrant{{TaskType: execution.TaskTypeCleanupExecutor, OwnAdHoc: true}}}, {Module: "meta", TenantID: int(f.input.TenantID), PrincipalID: int64(f.input.RequestedBy)}}}
	api := &CleanupOrchestratorService{coordinator: f.coordinator, reads: resolver}
	reader := CleanupReader{TenantID: f.input.TenantID, UserID: f.input.RequestedBy, OwnerProtocol: true, Bearer: "Bearer test-request-only"}
	pending, err := api.GetTaskStatus(f.ctx, f.input.RequestID, reader)
	if err != nil || pending.Status != "pending" || pending.Task.StartedAt != nil || pending.ModuleExecutions == nil || len(*pending.ModuleExecutions) != 1 || (*pending.ModuleExecutions)[0].CleanupAdmission != nil {
		t.Fatalf("pending view=%+v err=%v", pending, err)
	}
	calls := resolver.calls
	for _, bad := range []CleanupReader{{TenantID: reader.TenantID + 1, UserID: reader.UserID, OwnerProtocol: true}} {
		if _, err := api.GetTaskStatus(f.ctx, f.input.RequestID, bad); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatalf("foreign read: %v", err)
		}
	}
	if resolver.calls != calls {
		t.Fatal("foreign parent invoked Owner")
	}
	other := reader
	other.UserID++
	resolver.scopes = []execution.ReadScope{{Module: "graph", TenantID: int(other.TenantID), PrincipalID: int64(other.UserID), Grants: []execution.ReadGrant{{TaskType: execution.TaskTypeCleanupExecutor, OwnAdHoc: true}}}}
	otherView, err := api.GetTaskStatus(f.ctx, f.input.RequestID, other)
	if err != nil || otherView.ModuleExecutions == nil || len(*otherView.ModuleExecutions) != 0 || otherView.Summary != nil {
		t.Fatalf("same-Tenant parent/foreign child scope: %+v %v", otherView, err)
	}
	otherHistory, _, err := api.GetTaskHistory(f.ctx, other.TenantID, other.UserID, 1, 20)
	if err != nil || len(otherHistory) != 1 {
		t.Fatalf("same-Tenant history was narrowed: %+v %v", otherHistory, err)
	}
	resolver.scopes = []execution.ReadScope{{Module: "graph", TenantID: int(reader.TenantID), PrincipalID: int64(reader.UserID), Grants: []execution.ReadGrant{{TaskType: execution.TaskTypeCleanupExecutor, OwnAdHoc: true}}}}
	calls = resolver.calls
	restricted := reader
	restricted.OwnerProtocol = false
	parentOnly, err := api.GetTaskStatus(f.ctx, f.input.RequestID, restricted)
	if err != nil || parentOnly.ModuleExecutions != nil || parentOnly.Summary != nil || resolver.calls != calls {
		t.Fatalf("non-protocol parent=%+v err=%v", parentOnly, err)
	}
	resolver.err = client.ErrExecutionOwnerUnavailable
	if got, err := api.GetTaskStatus(f.ctx, f.input.RequestID, reader); !errors.Is(err, client.ErrExecutionOwnerUnavailable) || got != nil {
		t.Fatalf("Owner failure downgraded: %+v %v", got, err)
	}
	resolver.err = nil
	readAgain := f.snapshot(t)
	if !reflect.DeepEqual(before.Parent, readAgain.Parent) || !reflect.DeepEqual(before.Children, readAgain.Children) {
		t.Fatal("public GET wrote execution facts")
	}
	tasks, total, err := api.GetTaskHistory(f.ctx, reader.TenantID, reader.UserID, 1, 1)
	if err != nil || len(tasks) != 1 || total < 1 || tasks[0].StartedAt != nil {
		t.Fatalf("history=%+v total=%d err=%v", tasks, total, err)
	}
	f.advance(t, "observer")
	closed := f.snapshot(t)
	projected, err := api.GetTaskStatus(f.ctx, f.input.RequestID, reader)
	if err != nil || projected.Status != "timeout" || projected.ModuleExecutions == nil || len(*projected.ModuleExecutions) != 1 || (*projected.ModuleExecutions)[0].Status != "pending" || (*projected.ModuleExecutions)[0].CleanupAdmission == nil {
		t.Fatalf("closed view=%+v err=%v", projected, err)
	}
	data, _ := json.Marshal(projected)
	for _, forbidden := range []string{"lease_token", "execution_config", "Bearer test", "missing_modules", "results"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("forbidden field %s", forbidden)
		}
	}
	if projected.Summary != nil {
		t.Fatal("missing authorized receipt invented summary")
	}
	for _, child := range closed.Children {
		view := execution.ProjectCleanupAdmission(child, &closed.Parent, closed.ObservedAt)
		if view == nil || view.State != "closed_unclaimed" {
			t.Fatalf("committed closure missing %+v", view)
		}
		if execution.ProjectCleanupAdmission(child, nil, closed.ObservedAt) != nil {
			t.Fatal("hidden parent revealed")
		}
	}
}

type cleanupReadResolverStub struct {
	scopes []execution.ReadScope
	err    error
	calls  int
}

func (s *cleanupReadResolverStub) Resolve(context.Context, []string, int, int64, string) ([]execution.ReadScope, error) {
	s.calls++
	return s.scopes, s.err
}

func testCleanupMetaPairPostgres(t *testing.T) {
	f := newCleanupPGFixture(t, time.Minute)
	f.accept(t)
	f.advance(t, "parent")
	for _, child := range f.snapshot(t).Children {
		claimed, lease, err := execution.ClaimCleanupChild(f.ctx, f.db, cleanupPending(child), cleanupClaimOptions(child), nil)
		if err != nil || lease == nil {
			t.Fatal("claim", err)
		}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := execution.NewCleanupReceipt(*claimed, events.CleanupResultData{TaskID: f.input.RequestID, TenantID: f.input.TenantID, Module: child.Module, Action: "scan", Status: "success", Summary: events.CleanupResultSummary{RiskLevel: "low"}, Impact: &impact}, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := execution.CompleteCleanupWithReceipt(f.ctx, f.db, *lease, receipt); err != nil {
			t.Fatal(err)
		}
	}
	f.advance(t, "parent")
	if f.snapshot(t).Parent.Status != "success" {
		t.Fatal("assessment did not complete")
	}
	var before, executionsBefore int64
	f.db.Table(f.probe + ".facts").Count(&before)
	f.db.Model(&execution.TaskExecution{}).Where("tenant_id=?", f.input.TenantID).Count(&executionsBefore)
	svc := &CleanupOrchestratorService{coordinator: f.coordinator}
	id := uuid.NewString()
	if _, err := svc.CreateExecuteTask(f.ctx, id, f.input.RequestID, events.CleanupModeLogical, f.input.TenantID, f.input.RequestedBy, CleanupExecuteConfirmation{Confirmed: true}); !errors.Is(err, ErrCleanupManagerScopeRequired) {
		t.Fatalf("Meta scope accepted: %v", err)
	}
	var after, requests, children int64
	f.db.Table(f.probe + ".facts").Count(&after)
	f.db.Model(&models.CleanupRequestRecord{}).Where("request_id=?", id).Count(&requests)
	f.db.Model(&execution.TaskExecution{}).Where("tenant_id=?", f.input.TenantID).Count(&children)
	if after != before || requests != 0 || children != executionsBefore {
		t.Fatalf("denial produced effects: facts=%d/%d requests=%d children=%d", before, after, requests, children)
	}
}

// The native runner consumes this public DTO, not the professional-result ACL.
// IAM issuance is covered separately; the fixture retains the six approved
// permissions and uses the real shared Owner decision and PG read projection.
func TestIntegrationPostgresCleanupPublicScopeSerialization(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../../../scripts/test/execution-online-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		NativePermissions map[string][]string `json:"native_permissions"`
		Owners            map[string][]string `json:"cleanup_owners_by_cause"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if len(contract.NativePermissions["engine"]) != 6 || len(contract.Owners["engine.deleting"]) != 7 {
		t.Fatal("native contract scope changed")
	}
	f := newCleanupPGFixture(t, 500*time.Millisecond)
	f.input.ExpectedModules = contract.Owners["engine.deleting"]
	f.input.CauseEvent, f.input.Context.EngineID = events.CleanupCauseEngineDeleting, 9
	if err := f.input.Seal(); err != nil {
		t.Fatal(err)
	}
	f.accept(t)
	if err := f.db.Exec("SELECT pg_sleep(0.55)").Error; err != nil {
		t.Fatal(err)
	}
	f.advance(t, "public-contract")
	facts := authtest.NewTenantUserAuthContext(strconv.FormatUint(uint64(f.input.TenantID), 10), strconv.FormatUint(uint64(f.input.RequestedBy), 10), contract.NativePermissions["engine"])
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if err := commonauth.SetAuthContextForGin(c, facts); err != nil {
		t.Fatal(err)
	}
	scopes := []execution.ReadScope{}
	for _, owner := range f.input.ExpectedModules {
		scope, ok := execution.OwnerReadScopeFromGin(c, owner, map[string]string{execution.TaskTypeCleanupExecutor: "system.cleanup.read"})
		if !ok || len(scope.Grants) != 1 || !scope.Grants[0].OwnAdHoc {
			t.Fatalf("public Owner scope denied: %s", owner)
		}
		scopes = append(scopes, *scope)
		if owner != "manager" && owner != "transfer" && commonauth.HasRolePermission(c, owner+".cleanup_result.read") {
			t.Fatal("professional permission expanded")
		}
	}
	api := &CleanupOrchestratorService{coordinator: f.coordinator, reads: &cleanupReadResolverStub{scopes: scopes}}
	response, err := api.GetTaskStatus(f.ctx, f.input.RequestID, CleanupReader{TenantID: f.input.TenantID, UserID: f.input.RequestedBy, OwnerProtocol: true, Bearer: "Bearer test-request-only"})
	if err != nil || response == nil {
		t.Fatalf("public projection: %v", err)
	}
	if response.Status != "timeout" || response.ModuleExecutions == nil || len(*response.ModuleExecutions) != 7 {
		t.Fatal("public projection was incorrectly narrowed to professional permissions")
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Task struct {
			ExecutionID string `json:"execution_id"`
		} `json:"task"`
		Children []struct {
			ExecutionID       string `json:"execution_id"`
			ParentExecutionID string `json:"parent_execution_id"`
			Module            string `json:"module"`
			TaskType          string `json:"task_type"`
		} `json:"module_executions"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, child := range wire.Children {
		if _, err := uuid.Parse(child.ExecutionID); err != nil {
			t.Fatal(err)
		}
		if child.ParentExecutionID != wire.Task.ExecutionID || child.TaskType != "cleanup_executor" || seen[child.Module] {
			t.Fatal("invalid serialized child binding")
		}
		seen[child.Module] = true
	}
	if len(seen) != 7 || !seen["manager"] || !seen["transfer"] {
		t.Fatal("serialized public Owner set differs")
	}
	// The standard PG gate already requires python3. Pass only dummy public
	// fixture facts through stdin, using the real Hosted consumer function.
	command := exec.CommandContext(f.ctx, "python3", "-c", `
import importlib, json, sys
value = json.load(sys.stdin)
runner = importlib.import_module('scripts.test.cleanup-native-effects-online')
observed = runner.engine_task_observation(value, value['task_id'], 9, '7', action='scan')
assert observed['status'] == 'timeout' and all(observed['bindings'].values())
assert observed['children_observation'] == 'observed'
assert [row['module'] for row in observed['children']] == ['manager', 'transfer']
assert all(row['status'] == 'pending' and row['attempt'] == 0 and not row['started']
           and row['admission_state'] == 'closed_unclaimed' for row in observed['children'])
print(json.dumps(observed, sort_keys=True))
`)
	command.Dir = filepath.Clean(filepath.Join(filepath.Dir(source), "../../../.."))
	command.Stdin = strings.NewReader(string(encoded))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("real public DTO rejected by Hosted consumer: %v: %s", err, output)
	}
	t.Logf("cleanup_native_public_contract_projection=%s", output)
}
