package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/common/authorization/authtest"
	commoni18n "github.com/addp/common/middleware/i18n"
	"os"
	"reflect"
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
	done := make(chan error, 1)
	go func() {
		item, l, err := execution.ClaimCleanupChild(claimCtx, f.db, cleanupPending(other), cleanupClaimOptions(other), nil)
		if err == nil && (l != nil || item == nil || item.Status != "failed" || item.Attempt != 0 || item.StartedAt != nil) {
			err = fmt.Errorf("claimed after closure")
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("claim bypassed held parent: %v", err)
	case <-time.After(50 * time.Millisecond):
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
