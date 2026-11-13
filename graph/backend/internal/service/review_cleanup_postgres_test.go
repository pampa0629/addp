package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Called by the mandatory GraphCleanupRuntime PostgreSQL entry. Native calls
// are represented by the real durable command/operation protocol, not a graph
// engine; these cases prove PG ordering and rollback, not native recovery.
func testGraphReviewCleanupConcurrency(t *testing.T) {
	for _, outcome := range []string{"complete", "closure_rollback", "cleanup_receipt_rollback"} {
		t.Run("closure_first/"+outcome, func(t *testing.T) {
			f := newReviewCleanupPGFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			if outcome == "cleanup_receipt_rollback" {
				f.rejectWrite(t, "cleanup_results", "INSERT", "NEW.execution_id = '"+f.child.ExecutionID+"'")
			}
			closed := make(chan int, 1)
			callbackErrors := make(chan error, 1)
			release := make(chan struct{})
			var once, releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			callback := "fixture:review-cleanup-closure"
			if err := f.db.Callback().Update().After("gorm:update").Register(callback, func(tx *gorm.DB) {
				fields, ok := tx.Statement.Dest.(map[string]interface{})
				if !ok || fields["admission_execution_id"] != f.child.ExecutionID {
					return
				}
				once.Do(func() {
					var pid int
					if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						callbackErrors <- err
						tx.AddError(err)
						return
					}
					closed <- pid
					select {
					case <-release:
					case <-ctx.Done():
						tx.AddError(ctx.Err())
					}
					if outcome == "closure_rollback" {
						tx.AddError(errors.New("fixture closure rollback"))
					}
				})
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = f.db.Callback().Update().Remove(callback) })
			f.start(t, ctx)
			var blocker int
			select {
			case blocker = <-closed:
			case err := <-callbackErrors:
				t.Fatalf("closure barrier: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			pids, result := make(chan int, 1), make(chan error, 1)
			go func() {
				result <- f.db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
					var pid int
					if err := conn.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						return err
					}
					pids <- pid
					_, err := repository.NewBuildRepository(conn).BeginReview(ctx, f.task.GraphID, f.item.ID, f.task.TenantID, 9)
					return err
				})
			}()
			var waiter int
			select {
			case waiter = <-pids:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			waitReviewCleanupPG(t, ctx, f.db, "SELECT ? = ANY(pg_blocking_pids(?))", blocker, waiter)
			releaseOnce.Do(func() { close(release) })
			var err error
			select {
			case err = <-result:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if outcome == "closure_rollback" {
				if err != nil {
					t.Fatalf("rolled-back closure rejected review: %v", err)
				}
			} else if !errors.Is(err, repository.ErrGraphAdmissionClosed) {
				t.Fatalf("committed closure admitted review: %v", err)
			}
			want := execution.ExecutionStatusSuccess
			if outcome != "complete" {
				want = execution.ExecutionStatusFailed
			}
			child := f.waitTerminal(t, ctx, want)
			receipt, err := execution.ReadCleanupReceipt(*child)
			if err != nil || (receipt != nil) != (outcome == "complete") {
				t.Fatalf("public cleanup receipt did not roll back: %v", err)
			}
			var kg models.KnowledgeGraph
			var item models.ReviewItem
			if err := f.db.First(&kg, f.task.GraphID).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.First(&item, f.item.ID).Error; err != nil {
				t.Fatal(err)
			}
			if kg.AdmissionClosed != (outcome != "closure_rollback") || (kg.Status == "archived") != (outcome == "complete") {
				t.Fatalf("closure/final transaction leaked: closed=%v status=%s", kg.AdmissionClosed, kg.Status)
			}
			if outcome == "closure_rollback" && (kg.AdmissionExecutionID != f.graph.AdmissionExecutionID || kg.AdmissionCause != f.graph.AdmissionCause || kg.Version != f.graph.Version) {
				t.Fatal("rolled-back closure retained its owner, cause or version")
			}
			if (item.ActiveCommandID != "") != (outcome == "closure_rollback") || item.Status != models.ReviewStatusPending || item.ReviewedAt != nil {
				t.Fatalf("unexpected review state: %+v", item)
			}
			var receipts int64
			if outcome == "complete" {
				receipts = 1
			}
			f.assertReceiptCount(t, receipts)
			f.assertBuildUnchanged(t)
		})
	}
	for _, outcome := range []string{"known", "uncertain", "review_receipt_rollback"} {
		t.Run("review_first/"+outcome, func(t *testing.T) {
			f := newReviewCleanupPGFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			tx := f.db.WithContext(ctx).Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			var blocker int
			if err := tx.Raw("SELECT pg_backend_pid()").Scan(&blocker).Error; err != nil {
				t.Fatal(err)
			}
			repo := repository.NewBuildRepository(tx)
			item, err := repo.BeginReview(ctx, f.task.GraphID, f.item.ID, f.task.TenantID, 9)
			if err != nil {
				t.Fatal(err)
			}
			var kg models.KnowledgeGraph
			if err := tx.First(&kg, f.task.GraphID).Error; err != nil {
				t.Fatal(err)
			}
			descriptor, err := json.Marshal(models.GraphOperationTarget{EngineID: kg.EngineID, EngineVersion: 1, CatalogPath: kg.CatalogPath, IndexManagement: kg.IndexManagement})
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(descriptor)
			op := models.GraphOperation{ID: uuid.NewString(), TenantID: f.task.TenantID, GraphID: f.task.GraphID, ReviewItemID: item.ID, CommandID: item.ActiveCommandID, RunnerID: "review-pg", Kind: "graph", TargetDescriptor: string(descriptor), TargetDigest: hex.EncodeToString(hash[:]), RequestDigest: strings.Repeat("a", 64)}
			if err := repo.BeginCommandOperation(ctx, &op); err != nil {
				t.Fatal(err)
			}
			f.start(t, ctx)
			waitReviewCleanupPG(t, ctx, f.db, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid)))", blocker)
			if err := tx.Commit().Error; err != nil {
				t.Fatal(err)
			}
			waitReviewCleanupPG(t, ctx, f.db, "SELECT admission_closed FROM graph.knowledge_graphs WHERE id = ?", f.task.GraphID)
			f.assertBlocked(t, ctx)
			repo = repository.NewBuildRepository(f.db)
			if _, err := repo.BeginReview(ctx, f.task.GraphID, f.other.ID, f.task.TenantID, 10); !errors.Is(err, repository.ErrGraphAdmissionClosed) {
				t.Fatalf("closed admission accepted new review: %v", err)
			}
			if err := repo.EndReview(ctx, item, models.ReviewStatusModified, "native-result", nil, nil); !errors.Is(err, repository.ErrGraphAdmissionClosed) {
				t.Fatalf("in-flight operation accepted final review: %v", err)
			}
			if err := repo.SettleCommandOperation(ctx, op, strings.Repeat("b", 64), outcome != "uncertain"); err != nil {
				t.Fatal(err)
			}
			if outcome == "uncertain" {
				if err := repo.EndReview(ctx, item, models.ReviewStatusModified, "native-result", nil, nil); !errors.Is(err, repository.ErrGraphAdmissionClosed) {
					t.Fatalf("uncertain operation accepted final review: %v", err)
				}
				child := f.waitTerminal(t, ctx, execution.ExecutionStatusFailed)
				receipt, err := execution.ReadCleanupReceipt(*child)
				if err != nil || receipt == nil || receipt.Reason != "external_effect_uncertain" {
					t.Fatalf("lost uncertain cleanup outcome: %+v %v", receipt, err)
				}
				f.assertReceiptCount(t, 1)
			} else if outcome == "review_receipt_rollback" {
				f.rejectWrite(t, "review_items", "UPDATE", fmt.Sprintf("NEW.id = %d", item.ID))
				if err := repo.EndReview(ctx, item, models.ReviewStatusModified, "native-result", []byte(`{"reviewed":true}`), nil); err == nil {
					t.Fatal("review receipt failure was ignored")
				}
				f.assertBlocked(t, ctx)
				f.assertReceiptCount(t, 0)
			} else {
				if err := repo.EndReview(ctx, item, models.ReviewStatusModified, "native-result", []byte(`{"reviewed":true}`), nil); err != nil {
					t.Fatalf("closed admission prevented original command settlement: %v", err)
				}
				f.waitTerminal(t, ctx, execution.ExecutionStatusSuccess)
				f.assertReceiptCount(t, 1)
			}
			var stored models.ReviewItem
			if err := f.db.First(&stored, item.ID).Error; err != nil {
				t.Fatal(err)
			}
			approved := outcome == "known"
			if (stored.Status == models.ReviewStatusModified) != approved || (stored.ActiveCommandID == "") != approved || (stored.ReviewedAt != nil) != approved || (len(stored.FinalContent) > 0) != approved {
				t.Fatalf("review commit/rollback lost: %+v", stored)
			}
			if approved && (stored.ReviewedBy == nil || *stored.ReviewedBy != 9 || stored.ElementID != "native-result") {
				t.Fatal("original review settlement lost its actor or result")
			}
			if !approved && (stored.ActiveCommandID != item.ActiveCommandID || stored.CommandActor != 9 || stored.ElementID != "") {
				t.Fatal("unsettled review ownership or result changed")
			}
			var storedOp models.GraphOperation
			if err := f.db.First(&storedOp, "id = ?", op.ID).Error; err != nil || storedOp.CommandID != item.ActiveCommandID || (storedOp.State == models.GraphOperationKnown) != (outcome != "uncertain") {
				t.Fatalf("operation evidence was lost: %+v %v", storedOp, err)
			}
			if err := f.db.First(&kg, f.task.GraphID).Error; err != nil || !kg.AdmissionClosed || (kg.Status == "archived") != approved {
				t.Fatalf("cleanup crossed unsettled review: %+v %v", kg, err)
			}
			f.assertBuildUnchanged(t)
		})
	}
}

type reviewCleanupPGFixture struct {
	db    *gorm.DB
	task  models.BuildTask
	graph models.KnowledgeGraph
	build execution.TaskExecution
	item  models.ReviewItem
	other models.ReviewItem
	child execution.TaskExecution
	svc   *CleanupService
}

func newReviewPGFixture(t *testing.T) *reviewCleanupPGFixture {
	t.Helper()
	db, task, build := cleanupRuntimeDomainFixture(t, true)
	ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext(strconv.Itoa(int(task.TenantID)), "7", []string{"system.cleanup.execute"}))
	repo := repository.NewBuildRepository(db)
	_, lease, runtime, _, err := repo.ClaimBuild(ctx, *build, "review-fixture", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteBuild(execution.ContextWithLease(ctx, *lease), runtime, execution.ExecutionStatusSuccess, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("execution_id = ?", build.ExecutionID).First(build).Error; err != nil {
		t.Fatal(err)
	}
	f := &reviewCleanupPGFixture{db: db, task: task, build: *build}
	if err := db.First(&f.task, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&f.graph, task.GraphID).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range []*models.ReviewItem{&f.item, &f.other} {
		*item = models.ReviewItem{TenantID: task.TenantID, GraphID: task.GraphID, TaskID: task.ID, ItemType: models.ReviewItemEntity, Content: []byte(`{"name":"pending"}`), Status: models.ReviewStatusPending}
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanup.MigrateResults(db, "graph"); err != nil {
		t.Fatal(err)
	}
	return f
}

func newReviewCleanupPGFixture(t *testing.T) *reviewCleanupPGFixture {
	t.Helper()
	return newReviewCleanupPGFixtureWithBudget(t, time.Minute)
}

func newReviewCleanupPGFixtureWithBudget(t *testing.T, budget time.Duration) *reviewCleanupPGFixture {
	t.Helper()
	f := newReviewPGFixture(t)
	prepareReviewCleanupPGFixture(t, f, budget)
	return f
}

func prepareReviewCleanupPGFixture(t *testing.T, f *reviewCleanupPGFixture, budget time.Duration) {
	t.Helper()
	db, task := f.db, f.task
	ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext(strconv.Itoa(int(task.TenantID)), "7", []string{"system.cleanup.execute"}))
	parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(task.TenantID), Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
	if err := execution.ApplyUserOrigin(ctx, &parent); err != nil {
		t.Fatal(err)
	}
	now, err := execution.DatabaseNow(db)
	if err != nil {
		t.Fatal(err)
	}
	input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: task.TenantID, RequestedBy: 7, Action: "execute", CleanupMode: events.CleanupModeLogical, TriggerType: "manual", ExpectedModules: []string{"graph"}, BasedOnScan: uuid.NewString(), Context: events.CleanupContext{EngineID: 1}, Confirmation: events.CleanupConfirmation{Confirmed: true, RiskLevel: "low", ImpactDigest: strings.Repeat("a", 64)}, RequestedAt: now, Deadline: now.Add(budget)}
	input.BasedOnScan, input.Confirmation.ImpactDigest = graphCleanupScanFixture(t, db, input)
	now, err = execution.DatabaseNow(db)
	if err != nil {
		t.Fatal(err)
	}
	input.RequestedAt, input.Deadline = now, now.Add(budget)
	if err := input.Seal(); err != nil {
		t.Fatal(err)
	}
	children, err := execution.CreateCleanupExecutions(ctx, db, &parent, input)
	if err != nil {
		t.Fatal(err)
	}
	f.child = children[0]
	identity := execution.PendingIdentity{ExecutionID: f.child.ExecutionID, TenantID: f.child.TenantID, Module: "graph", TaskType: "cleanup_executor", Source: "system"}
	if err := execution.AttachPendingAuthorization(ctx, db, identity, execution.PendingAuthorization{ID: 1, ExpiresAt: now.Add(time.Hour), ActorPrincipalID: *f.child.ActorPrincipalID, ActorTenantMembershipID: *f.child.ActorTenantMembershipID, IssuedAuthorizationVersion: *f.child.IssuedAuthorizationVersion}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: parent.ExecutionID, TenantID: parent.TenantID, Module: "system", TaskType: "cleanup", Source: "system"}, execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "review-parent", LeaseDuration: time.Minute}, nil); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request client.CleanupAccessRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.ExecutionID != f.child.ExecutionID || r.Header.Get("Authorization") != "Bearer fixture" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(client.CleanupAccess{AuthorizationID: "1", ExecutionID: f.child.ExecutionID, TenantID: strconv.Itoa(f.child.TenantID), Stage: request.Stage, Attempt: request.Attempt, Scope: request.Scope, ExpiresAt: time.Now().Add(time.Minute)})
	}))
	t.Cleanup(server.Close)
	f.svc = NewCleanupService(db, client.NewSystemServiceClient(server.URL, cleanupRuntimeToken{}, nil))
}

func (f *reviewCleanupPGFixture) start(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := f.svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.svc.Stop)
}

func waitReviewCleanupPG(t *testing.T, ctx context.Context, db *gorm.DB, query string, args ...interface{}) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		if err := db.WithContext(ctx).Raw(query, args...).Scan(&ready).Error; err != nil {
			t.Fatal(err)
		}
		if ready {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func (f *reviewCleanupPGFixture) waitTerminal(t *testing.T, ctx context.Context, status string) *execution.TaskExecution {
	t.Helper()
	waitReviewCleanupPG(t, ctx, f.db, "SELECT status IN ('success', 'failed', 'timeout', 'cancelled') FROM common.task_executions WHERE execution_id = ?", f.child.ExecutionID)
	var child execution.TaskExecution
	if err := f.db.Where("execution_id = ?", f.child.ExecutionID).First(&child).Error; err != nil || child.Status != status || child.Attempt != 1 {
		t.Fatalf("unexpected Cleanup terminal: %+v %v", child, err)
	}
	return &child
}

func (f *reviewCleanupPGFixture) assertBlocked(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := f.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := repository.LockAdmission(tx, f.task.TenantID, true); err != nil {
			return err
		}
		var kg models.KnowledgeGraph
		if err := tx.First(&kg, f.task.GraphID).Error; err != nil {
			return err
		}
		return repository.CheckCleanupQuiescence(tx, &kg, f.child.ExecutionID)
	}); !errors.Is(err, repository.ErrGraphReconciliationIncomplete) {
		t.Fatalf("in-flight review did not block quiescence: %v", err)
	}
	var child execution.TaskExecution
	if err := f.db.Where("execution_id = ?", f.child.ExecutionID).First(&child).Error; err != nil || child.Status != execution.ExecutionStatusRunning {
		t.Fatalf("cleanup finished before review settled: %+v %v", child, err)
	}
	f.assertReceiptCount(t, 0)
}

func (f *reviewCleanupPGFixture) assertReceiptCount(t *testing.T, want int64) {
	t.Helper()
	var count int64
	if err := f.db.Table("graph.cleanup_results").Where("execution_id = ?", f.child.ExecutionID).Count(&count).Error; err != nil || count != want {
		t.Fatalf("cleanup results=%d want=%d err=%v", count, want, err)
	}
}

func (f *reviewCleanupPGFixture) assertBuildUnchanged(t *testing.T) {
	t.Helper()
	var build execution.TaskExecution
	if err := f.db.Where("execution_id = ?", f.build.ExecutionID).First(&build).Error; err != nil || build.Status != f.build.Status || build.Attempt != f.build.Attempt || build.Progress != f.build.Progress || build.CompletedAt == nil || f.build.CompletedAt == nil || !build.CompletedAt.Equal(*f.build.CompletedAt) {
		t.Fatalf("review/Cleanup rewrote build terminal: %+v %v", build, err)
	}
	var count int64
	if err := f.db.Model(&models.ReviewItem{}).Where("tenant_id = ? AND graph_id = ?", f.task.TenantID, f.task.GraphID).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("Engine cleanup erased review history: %d %v", count, err)
	}
	var other models.ReviewItem
	if err := f.db.First(&other, f.other.ID).Error; err != nil || other.Status != models.ReviewStatusPending || other.ActiveCommandID != "" || other.CommandActor != 0 || other.ReviewedAt != nil || other.ReviewedBy != nil || len(other.FinalContent) != 0 {
		t.Fatalf("new review refusal changed another item: %v", err)
	}
	var task models.BuildTask
	if err := f.db.First(&task, f.task.ID).Error; err != nil || task.Status != f.task.Status || string(task.Stats) != string(f.task.Stats) {
		t.Fatalf("review/Cleanup changed completed build summary: %v", err)
	}
}

func (f *reviewCleanupPGFixture) rejectWrite(t *testing.T, table, operation, predicate string) {
	t.Helper()
	name := "review_cleanup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	statement := fmt.Sprintf("CREATE FUNCTION graph.%s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF %s THEN RAISE EXCEPTION 'fixture review cleanup write failure'; END IF; RETURN NEW; END; $$; CREATE TRIGGER %s BEFORE %s ON graph.%s FOR EACH ROW EXECUTE FUNCTION graph.%s()", name, predicate, name, operation, table, name)
	if err := f.db.Exec(statement).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.db.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON graph.%s; DROP FUNCTION IF EXISTS graph.%s()", name, table, name)).Error; err != nil {
			t.Error(err)
		}
	})
}
