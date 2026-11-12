package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The mandatory Graph PG entry exercises production deletion and Cleanup
// transactions. Barriers observe real PG blocking, not inferred timing.
func testGraphDeleteCleanupConcurrency(t *testing.T) {
	for _, action := range []string{"task", "material"} {
		for _, outcome := range []string{"closure_first", "delete_first", "delete_rollback", "cleanup_receipt_rollback"} {
			t.Run(action+"/"+outcome, func(t *testing.T) {
				f := newReviewCleanupPGFixture(t)
				ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
				defer cancel()
				// A separate definition/material/review must survive every path.
				sibling := models.BuildTask{TenantID: f.task.TenantID, GraphID: f.task.GraphID, Name: "retained-sibling", Status: models.BuildStatusPending, Stats: []byte(`{}`)}
				if err := f.db.Create(&sibling).Error; err != nil {
					t.Fatal(err)
				}
				material := models.BuildMaterial{TenantID: f.task.TenantID, GraphID: f.task.GraphID, TaskID: sibling.ID, Type: "document", FileName: "sibling.txt", FilePath: "build/sibling.txt", Status: models.BuildMaterialStatusCompleted, Stats: []byte(`{}`)}
				if err := f.db.Create(&material).Error; err != nil {
					t.Fatal(err)
				}
				item := models.ReviewItem{TenantID: f.task.TenantID, GraphID: f.task.GraphID, TaskID: sibling.ID, MaterialID: material.ID, ItemType: models.ReviewItemEntity, Content: []byte(`{"name":"retained"}`), Status: models.ReviewStatusPending}
				if err := f.db.Create(&item).Error; err != nil {
					t.Fatal(err)
				}
				before := readDeleteCleanupFacts(t, f.db, f.task.TenantID)
				var target models.BuildMaterial
				if err := f.db.Where("task_id = ? AND tenant_id = ?", f.task.ID, f.task.TenantID).First(&target).Error; err != nil {
					t.Fatal(err)
				}
				deleteDomain := func(db *gorm.DB) error {
					repo := repository.NewBuildRepository(db)
					if action == "task" {
						return repo.DeleteTask(ctx, f.task.ID, f.task.GraphID, f.task.TenantID)
					}
					return repo.DeleteMaterial(ctx, target.ID, f.task.ID, f.task.GraphID, f.task.TenantID)
				}
				if outcome == "cleanup_receipt_rollback" {
					f.rejectWrite(t, "cleanup_results", "INSERT", "NEW.execution_id = '"+f.child.ExecutionID+"'")
				}
				expected := before
				if outcome == "closure_first" {
					barrier, release := holdDeleteCleanupClosurePG(t, ctx, f)
					defer release()
					f.start(t, ctx)
					var blocker int
					select {
					case blocker = <-barrier:
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
							return deleteDomain(conn)
						})
					}()
					var waiter int
					select {
					case waiter = <-pids:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					waitReviewCleanupPG(t, ctx, f.db, "SELECT ? = ANY(pg_blocking_pids(?))", blocker, waiter)
					release()
					select {
					case err := <-result:
						if !errors.Is(err, repository.ErrGraphAdmissionClosed) {
							t.Fatalf("closed admission allowed %s deletion: %v", action, err)
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				} else {
					table, id := "build_materials", target.ID
					if action == "task" {
						table, id = "build_tasks", f.task.ID
					}
					if outcome == "delete_rollback" {
						rejectDeleteCleanupPG(t, f.db, table, id, f.task.TenantID)
					}
					barrier, release := holdDeleteCleanupMutationPG(t, ctx, f, table)
					defer release()
					result := make(chan error, 1)
					go func() { result <- deleteDomain(f.db) }()
					var blocked deleteCleanupBarrier
					select {
					case blocked = <-barrier:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					if blocked.err != nil {
						t.Fatal(blocked.err)
					}
					if action == "task" && (blocked.materials != 0 || blocked.reviews != 0) {
						t.Fatal("task deletion barrier did not follow actual child deletions")
					}
					if action == "material" && (blocked.materials != 1 || blocked.reviews != 2) {
						t.Fatal("material deletion barrier lost original facts")
					}
					f.start(t, ctx)
					waitReviewCleanupPG(t, ctx, f.db, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid)))", blocked.pid)
					release()
					select {
					case err := <-result:
						if outcome == "delete_rollback" {
							if err == nil || !strings.Contains(err.Error(), "fixture deletion rollback") {
								t.Fatalf("expected PG statement failure: %v", err)
							}
						} else if err != nil {
							t.Fatal(err)
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					if outcome != "delete_rollback" {
						if action == "task" {
							expected.tasks = slices.DeleteFunc(slices.Clone(before.tasks), func(v models.BuildTask) bool { return v.ID == f.task.ID })
							expected.materials = slices.DeleteFunc(slices.Clone(before.materials), func(v models.BuildMaterial) bool { return v.TaskID == f.task.ID })
							expected.reviews = slices.DeleteFunc(slices.Clone(before.reviews), func(v models.ReviewItem) bool { return v.TaskID == f.task.ID })
						} else {
							expected.materials = slices.DeleteFunc(slices.Clone(before.materials), func(v models.BuildMaterial) bool { return v.ID == target.ID })
						}
					}
				}
				wantStatus := execution.ExecutionStatusSuccess
				if outcome == "cleanup_receipt_rollback" {
					wantStatus = execution.ExecutionStatusFailed
				}
				child := f.waitTerminal(t, ctx, wantStatus)
				after := readDeleteCleanupFacts(t, f.db, f.task.TenantID)
				if !reflect.DeepEqual(after, expected) {
					t.Fatal("deletion/Cleanup changed facts outside the committed exact deletion, including history or frozen input")
				}
				var kg models.KnowledgeGraph
				if err := f.db.First(&kg, f.task.GraphID).Error; err != nil || !kg.AdmissionClosed || kg.AdmissionExecutionID != child.ExecutionID || kg.Version != f.graph.Version+1 || (kg.Status == "archived") != (wantStatus == execution.ExecutionStatusSuccess) {
					t.Fatalf("closure or archive rollback incorrect: %v", err)
				}
				receipt, err := execution.ReadCleanupReceipt(*child)
				if err != nil {
					t.Fatal(err)
				}
				if outcome == "cleanup_receipt_rollback" {
					if receipt != nil || kg.Status != f.graph.Status {
						t.Fatal("failed final transaction retained receipt/archive")
					}
					f.assertReceiptCount(t, 0)
				} else {
					if receipt == nil || receipt.Status != "success" {
						t.Fatal("successful Cleanup lost its receipt")
					}
					f.assertReceiptCount(t, 1)
					var payload string
					if err := f.db.Table("graph.cleanup_results").Select("payload").Where("execution_id = ?", child.ExecutionID).Scan(&payload).Error; err != nil {
						t.Fatal(err)
					}
					var result cleanup.Result
					if err := json.Unmarshal([]byte(payload), &result); err != nil || result.Statistics["build_tasks"] != int64(len(expected.tasks)) || result.Statistics["build_materials"] != int64(len(expected.materials)) || result.Statistics["review_items"] != int64(len(expected.reviews)) || result.Statistics["cancelled_build_tasks"] != 0 || result.Statistics["deleted_records"] != 0 || result.Statistics["archived_graphs"] != 1 {
						t.Fatalf("Cleanup result did not reflect exact remaining facts: %v", err)
					}
				}
			})
		}
	}
}

type deleteCleanupFacts struct {
	tasks      []models.BuildTask
	materials  []models.BuildMaterial
	reviews    []models.ReviewItem
	controls   []models.BuildExecutionControl
	executions []execution.TaskExecution
	operations []models.GraphOperation
}

func readDeleteCleanupFacts(t *testing.T, db *gorm.DB, tenant uint) deleteCleanupFacts {
	t.Helper()
	var facts deleteCleanupFacts
	for _, query := range []struct {
		dest  any
		order string
	}{{&facts.tasks, "id"}, {&facts.materials, "id"}, {&facts.reviews, "id"}, {&facts.controls, "execution_id"}, {&facts.operations, "id"}} {
		if err := db.Where("tenant_id = ?", tenant).Order(query.order).Find(query.dest).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Where("tenant_id = ? AND module = ? AND task_type = ?", tenant, "graph", "kg_build").Order("id").Find(&facts.executions).Error; err != nil {
		t.Fatal(err)
	}
	return facts
}

func holdDeleteCleanupClosurePG(t *testing.T, ctx context.Context, f *reviewCleanupPGFixture) (<-chan int, func()) {
	t.Helper()
	barrier, release := make(chan int, 1), make(chan struct{})
	var once, releaseOnce sync.Once
	name := "fixture:delete-cleanup-closure"
	if err := f.db.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
		fields, ok := tx.Statement.Dest.(map[string]interface{})
		if !ok || fields["admission_execution_id"] != f.child.ExecutionID {
			return
		}
		once.Do(func() {
			var pid int
			if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				tx.AddError(err)
				return
			}
			barrier <- pid
			select {
			case <-release:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Update().Remove(name) })
	return barrier, func() { releaseOnce.Do(func() { close(release) }) }
}

type deleteCleanupBarrier struct {
	pid                int
	materials, reviews int64
	tasks              int64
	err                error
}

func holdDeleteCleanupMutationPG(t *testing.T, ctx context.Context, f *reviewCleanupPGFixture, table string) (<-chan deleteCleanupBarrier, func()) {
	t.Helper()
	barrier, release := make(chan deleteCleanupBarrier, 1), make(chan struct{})
	var once, releaseOnce sync.Once
	name := "fixture:delete-cleanup-mutation"
	if err := f.db.Callback().Delete().Before("gorm:delete").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table != table {
			return
		}
		once.Do(func() {
			var state deleteCleanupBarrier
			fresh := tx.Session(&gorm.Session{NewDB: true})
			state.err = fresh.Raw("SELECT pg_backend_pid()").Scan(&state.pid).Error
			if state.err == nil {
				state.err = fresh.Model(&models.BuildMaterial{}).Where("task_id = ? AND tenant_id = ?", f.task.ID, f.task.TenantID).Count(&state.materials).Error
			}
			if state.err == nil {
				state.err = fresh.Model(&models.ReviewItem{}).Where("task_id = ? AND tenant_id = ?", f.task.ID, f.task.TenantID).Count(&state.reviews).Error
			}
			if state.err == nil {
				state.err = fresh.Model(&models.BuildTask{}).Where("id = ? AND tenant_id = ?", f.task.ID, f.task.TenantID).Count(&state.tasks).Error
			}
			barrier <- state
			select {
			case <-release:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Delete().Remove(name) })
	return barrier, func() { releaseOnce.Do(func() { close(release) }) }
}

func rejectDeleteCleanupPG(t *testing.T, db *gorm.DB, table string, id, tenant uint) {
	t.Helper()
	name := "delete_cleanup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	statement := fmt.Sprintf("CREATE FUNCTION graph.%s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.id = %d AND OLD.tenant_id = %d THEN RAISE EXCEPTION 'fixture deletion rollback'; END IF; RETURN OLD; END; $$; CREATE TRIGGER %s AFTER DELETE ON graph.%s FOR EACH ROW EXECUTE FUNCTION graph.%s()", name, id, tenant, name, table, name)
	if err := db.Exec(statement).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON graph.%s; DROP FUNCTION IF EXISTS graph.%s()", name, table, name)).Error; err != nil {
			t.Error(err)
		}
	})
}
