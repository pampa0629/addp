package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// These cases exercise the actual enqueue and three-phase Cleanup transactions.
// No build supervisor or native graph engine is started.
func testGraphEnqueueCleanupConcurrency(t *testing.T) {
	for _, order := range []string{"closure_first", "enqueue_first"} {
		for _, mode := range []repository.BuildExecutionClaimMode{repository.BuildExecutionClaimRun, repository.BuildExecutionClaimRerun} {
			t.Run(order+"/"+string(mode), func(t *testing.T) {
				f := newReviewCleanupPGFixture(t)
				ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
				defer cancel()
				ctx = execution.WithUserOrigin(ctx, authtest.NewTenantUserAuthContext(strconv.Itoa(int(f.task.TenantID)), "7", nil))
				for _, item := range []*models.ReviewItem{&f.item, &f.other} {
					if err := f.db.First(item, item.ID).Error; err != nil {
						t.Fatal(err)
					}
				}
				if err := f.db.Model(&models.BuildMaterial{}).Where("task_id = ? AND tenant_id = ?", f.task.ID, f.task.TenantID).Updates(map[string]interface{}{"status": models.BuildMaterialStatusCompleted, "processed_chunks": 9, "total_chunks": 9, "processed_at": time.Now().UTC()}).Error; err != nil {
					t.Fatal(err)
				}
				var materials []models.BuildMaterial
				if err := f.db.Where("task_id = ? AND tenant_id = ?", f.task.ID, f.task.TenantID).Order("created_at ASC, id ASC").Find(&materials).Error; err != nil {
					t.Fatal(err)
				}
				var oldControl models.BuildExecutionControl
				if err := f.db.First(&oldControl, "execution_id = ?", f.build.ExecutionID).Error; err != nil {
					t.Fatal(err)
				}
				input, err := repository.DecodeBuildInput(f.build, oldControl, f.task)
				if err != nil {
					t.Fatal(err)
				}
				next := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(f.task.TenantID), Module: "graph", TaskType: "kg_build", Source: "graph", Status: "pending", TriggerType: "manual"}
				enqueue := func(db *gorm.DB) error {
					_, _, err := repository.NewBuildRepository(db).EnqueueExecution(ctx, f.task.ID, f.task.GraphID, f.task.TenantID, &next, mode, input.Materials, input.EngineVersion, f.graph)
					return err
				}
				if order == "closure_first" {
					closed, result := make(chan int, 1), make(chan error, 1)
					releaseClosure, phaseTwo, releasePhaseTwo := make(chan struct{}), make(chan struct{}), make(chan struct{})
					var closureOnce, phaseOnce, releaseOnce, phaseReleaseOnce sync.Once
					defer releaseOnce.Do(func() { close(releaseClosure) })
					defer phaseReleaseOnce.Do(func() { close(releasePhaseTwo) })
					const closureCallback = "fixture:enqueue-cleanup-closure"
					if err := f.db.Callback().Update().After("gorm:update").Register(closureCallback, func(tx *gorm.DB) {
						fields, ok := tx.Statement.Dest.(map[string]interface{})
						if !ok || fields["admission_execution_id"] != f.child.ExecutionID {
							return
						}
						closureOnce.Do(func() {
							var pid int
							if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
								tx.AddError(err)
								return
							}
							closed <- pid
							select {
							case <-releaseClosure:
							case <-ctx.Done():
								tx.AddError(ctx.Err())
							}
						})
					}); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = f.db.Callback().Update().Remove(closureCallback) })
					// Pause after the closure commit, before cancellation/final cleanup,
					// so later task updates cannot hide stale admission reads.
					const phaseCallback = "fixture:enqueue-cleanup-phase-two"
					if err := f.db.Callback().Query().Before("gorm:query").Register(phaseCallback, func(tx *gorm.DB) {
						if _, ok := tx.Statement.Dest.(*[]models.BuildExecutionControl); !ok {
							return
						}
						phaseOnce.Do(func() {
							close(phaseTwo)
							select {
							case <-releasePhaseTwo:
							case <-ctx.Done():
								tx.AddError(ctx.Err())
							}
						})
					}); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = f.db.Callback().Query().Remove(phaseCallback) })
					f.start(t, ctx)
					var blocker int
					select {
					case blocker = <-closed:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					pids := make(chan int, 1)
					go func() {
						result <- f.db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
							var pid int
							if err := conn.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
								return err
							}
							pids <- pid
							return enqueue(conn)
						})
					}()
					var waiter int
					select {
					case waiter = <-pids:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					waitReviewCleanupPG(t, ctx, f.db, "SELECT ? = ANY(pg_blocking_pids(?))", blocker, waiter)
					releaseOnce.Do(func() { close(releaseClosure) })
					select {
					case <-phaseTwo:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					select {
					case err = <-result:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					if !errors.Is(err, commonapi.ErrConflict) {
						t.Fatalf("committed closure admitted %s: %v", mode, err)
					}
					for _, model := range []any{&execution.TaskExecution{}, &models.BuildExecutionControl{}} {
						var count int64
						if err := f.db.Model(model).Where("execution_id = ?", next.ExecutionID).Count(&count).Error; err != nil || count != 0 {
							t.Fatalf("rejected enqueue left %T: count=%d err=%v", model, count, err)
						}
					}
					assertEnqueueCleanupTask(t, f.db, f.task)
					assertReviewCompetitionRow(t, f.db, f.item)
					assertReviewCompetitionRow(t, f.db, f.other)
					assertEnqueueCleanupMaterials(t, f.db, f.task, materials)
					phaseReleaseOnce.Do(func() { close(releasePhaseTwo) })
				} else {
					tx := f.db.WithContext(ctx).Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
					if tx.Error != nil {
						t.Fatal(tx.Error)
					}
					defer tx.Rollback()
					var blocker int
					if err := tx.Raw("SELECT pg_backend_pid()").Scan(&blocker).Error; err != nil {
						t.Fatal(err)
					}
					if err := enqueue(tx); err != nil {
						t.Fatal(err)
					}
					var queuedControl models.BuildExecutionControl
					if err := tx.First(&queuedControl, "execution_id = ?", next.ExecutionID).Error; err != nil {
						t.Fatal(err)
					}
					var queued execution.TaskExecution
					if err := tx.First(&queued, "execution_id = ?", next.ExecutionID).Error; err != nil || queued.Status != execution.ExecutionStatusPending || queued.Attempt != 0 || queued.StartedAt != nil {
						t.Fatalf("enqueue invented execution start: %v", err)
					}
					var queuedMaterials []models.BuildMaterial
					if err := tx.Where("task_id = ? AND tenant_id = ?", f.task.ID, f.task.TenantID).Order("created_at ASC, id ASC").Find(&queuedMaterials).Error; err != nil {
						t.Fatal(err)
					}
					f.start(t, ctx)
					waitReviewCleanupPG(t, ctx, f.db, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid)))", blocker)
					if err := tx.Commit().Error; err != nil {
						t.Fatal(err)
					}
					f.waitTerminal(t, ctx, execution.ExecutionStatusSuccess)
					var cancelled execution.TaskExecution
					if err := f.db.First(&cancelled, "execution_id = ?", next.ExecutionID).Error; err != nil {
						t.Fatal(err)
					}
					if cancelled.Status != execution.ExecutionStatusCancelled || cancelled.Attempt != 0 || cancelled.StartedAt != nil || cancelled.CompletedAt == nil || cancelled.LeaseOwner != nil || cancelled.LeaseToken != nil || cancelled.LeaseExpiresAt != nil || !reflect.DeepEqual(cancelled.ExecutionConfig, queued.ExecutionConfig) {
						t.Fatal("Cleanup did not cancel the exact unclaimed execution without inventing a start or changing frozen config")
					}
					var stored models.BuildTask
					if err := f.db.First(&stored, f.task.ID).Error; err != nil || stored.ExecutionID != next.ExecutionID || stored.Status != models.BuildStatusCancelled || stored.StartedAt != nil || stored.CompletedAt == nil {
						t.Fatalf("cancelled task summary lost exact execution or invented a start: %v", err)
					}
					var control models.BuildExecutionControl
					if err := f.db.First(&control, "execution_id = ?", next.ExecutionID).Error; err != nil || control.Snapshot != queuedControl.Snapshot || !control.CancelRequested || control.CancelReason != "cleanup_requested" || control.CancelRequestedBy != 0 || control.CancelRequestedAt == nil {
						t.Fatalf("frozen input or cancellation identity changed: %v", err)
					}
					if _, err := repository.DecodeBuildInput(cancelled, control, stored); err != nil {
						t.Fatalf("cancelled input no longer decodes: %v", err)
					}
					if mode == repository.BuildExecutionClaimRerun && cancelled.ExecutionConfig["predecessor_execution_id"] != f.build.ExecutionID {
						t.Fatal("rerun lost predecessor")
					}
					assertEnqueueCleanupMaterials(t, f.db, f.task, queuedMaterials)
					if mode == repository.BuildExecutionClaimRun {
						assertReviewCompetitionRow(t, f.db, f.item)
						assertReviewCompetitionRow(t, f.db, f.other)
					} else {
						var count int64
						if err := f.db.Model(&models.ReviewItem{}).Where("task_id = ? AND tenant_id = ?", f.task.ID, f.task.TenantID).Count(&count).Error; err != nil || count != 0 {
							t.Fatalf("rerun retained old pending reviews: count=%d err=%v", count, err)
						}
					}
				}
				child := f.waitTerminal(t, ctx, execution.ExecutionStatusSuccess)
				receipt, err := execution.ReadCleanupReceipt(*child)
				if err != nil || receipt == nil || receipt.Status != "success" {
					t.Fatalf("Cleanup receipt: %v", err)
				}
				var payload string
				if err := f.db.Table("graph.cleanup_results").Select("payload").Where("execution_id = ?", child.ExecutionID).Scan(&payload).Error; err != nil {
					t.Fatal(err)
				}
				var professional cleanup.Result
				if err := json.Unmarshal([]byte(payload), &professional); err != nil {
					t.Fatal(err)
				}
				var wantCancelled int64
				if order == "enqueue_first" {
					wantCancelled = 1
				}
				if professional.Statistics["cancelled_build_tasks"] != wantCancelled {
					t.Fatalf("Cleanup cancellation count=%d want=%d", professional.Statistics["cancelled_build_tasks"], wantCancelled)
				}
				f.assertReceiptCount(t, 1)
				assertReviewCompetitionOperations(t, f, 0)
				var old execution.TaskExecution
				if err := f.db.First(&old, "execution_id = ?", f.build.ExecutionID).Error; err != nil || !reflect.DeepEqual(old, f.build) {
					t.Fatalf("Cleanup rewrote predecessor execution: %v", err)
				}
				var control models.BuildExecutionControl
				if err := f.db.First(&control, "execution_id = ?", f.build.ExecutionID).Error; err != nil || !reflect.DeepEqual(control, oldControl) {
					t.Fatalf("Cleanup rewrote predecessor frozen input: %v", err)
				}
				var kg models.KnowledgeGraph
				if err := f.db.First(&kg, f.task.GraphID).Error; err != nil || !kg.AdmissionClosed || kg.AdmissionExecutionID != child.ExecutionID || kg.Status != "archived" {
					t.Fatalf("Cleanup failed to retain its exact closed scope: %v", err)
				}
			})
		}
	}
}

func assertEnqueueCleanupTask(t *testing.T, db *gorm.DB, expected models.BuildTask) {
	t.Helper()
	var actual models.BuildTask
	if err := db.First(&actual, expected.ID).Error; err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("rejected enqueue changed task including timestamps: %v", err)
	}
}

func assertEnqueueCleanupMaterials(t *testing.T, db *gorm.DB, task models.BuildTask, expected []models.BuildMaterial) {
	t.Helper()
	var actual []models.BuildMaterial
	if err := db.Where("task_id = ? AND tenant_id = ?", task.ID, task.TenantID).Order("created_at ASC, id ASC").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("rejected enqueue changed material checkpoints: %v", err)
	}
}
