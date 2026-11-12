package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/gorm"
)

// Graph configuration deletion is a PG operation. Known native effects below
// are represented by the durable operation protocol, not by a graph engine.
func testGraphDefinitionDeleteCleanupConcurrency(t *testing.T) {
	for _, scope := range []string{"no_remaining_graph", "remaining_graph"} {
		for _, outcome := range []string{"closure_first", "delete_first", "delete_rollback", "cleanup_receipt_rollback"} {
			t.Run(scope+"/"+outcome, func(t *testing.T) {
				f := newReviewCleanupPGFixture(t)
				ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
				defer cancel()
				// A settled operation must remain history after its configuration
				// and review item disappear; it cannot become deletion proof.
				buildRepo := repository.NewBuildRepository(f.db)
				item, err := buildRepo.BeginReview(ctx, f.task.GraphID, f.item.ID, f.task.TenantID, 7)
				if err != nil {
					t.Fatal(err)
				}
				op := reviewCompetitionOperation(t, f, item.ActiveCommandID)
				if err := buildRepo.BeginCommandOperation(ctx, &op); err != nil {
					t.Fatal(err)
				}
				if err := buildRepo.SettleCommandOperation(ctx, op, strings.Repeat("b", 64), true); err != nil {
					t.Fatal(err)
				}
				if err := buildRepo.EndReview(ctx, item, models.ReviewStatusApproved, "known-fixture-id", nil, nil); err != nil {
					t.Fatal(err)
				}
				if scope == "remaining_graph" {
					addGraphDeleteCleanupSibling(t, f)
				}
				before := readDeleteCleanupFacts(t, f.db, f.task.TenantID)
				var graphs []models.KnowledgeGraph
				if err := f.db.Where("tenant_id = ?", f.task.TenantID).Order("id").Find(&graphs).Error; err != nil {
					t.Fatal(err)
				}
				deleteGraph := func(db *gorm.DB) error {
					return repository.NewKnowledgeGraphRepository(db).DeleteDefinition(ctx, f.graph.ID, f.graph.TenantID, f.graph.Version)
				}
				if outcome == "cleanup_receipt_rollback" {
					f.rejectWrite(t, "cleanup_results", "INSERT", "NEW.execution_id = '"+f.child.ExecutionID+"'")
				}
				deleted := outcome == "delete_first" || outcome == "cleanup_receipt_rollback"
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
							return deleteGraph(conn)
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
						if !errors.Is(err, repository.ErrGraphVersionConflict) {
							t.Fatalf("stale version deleted a closed graph: %v", err)
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				} else {
					if outcome == "delete_rollback" {
						rejectDeleteCleanupPG(t, f.db, "knowledge_graphs", f.graph.ID, f.graph.TenantID)
					}
					barrier, release := holdDeleteCleanupMutationPG(t, ctx, f, "knowledge_graphs")
					defer release()
					result := make(chan error, 1)
					go func() { result <- deleteGraph(f.db) }()
					var blocked deleteCleanupBarrier
					select {
					case blocked = <-barrier:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					if blocked.err != nil || blocked.tasks != 0 || blocked.materials != 0 || blocked.reviews != 0 {
						t.Fatalf("graph deletion barrier did not follow actual child deletions: %v", blocked.err)
					}
					f.start(t, ctx)
					waitReviewCleanupPG(t, ctx, f.db, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid)))", blocked.pid)
					release()
					select {
					case err := <-result:
						if outcome == "delete_rollback" {
							if err == nil || !strings.Contains(err.Error(), "fixture deletion rollback") {
								t.Fatalf("missing PG deletion failure: %v", err)
							}
						} else if err != nil {
							t.Fatal(err)
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				wantStatus := execution.ExecutionStatusSuccess
				if outcome == "cleanup_receipt_rollback" {
					wantStatus = execution.ExecutionStatusFailed
				}
				child := f.waitTerminal(t, ctx, wantStatus)
				expected := before
				remaining := graphs
				if deleted {
					expected.tasks = slices.DeleteFunc(slices.Clone(before.tasks), func(v models.BuildTask) bool { return v.GraphID == f.graph.ID })
					expected.materials = slices.DeleteFunc(slices.Clone(before.materials), func(v models.BuildMaterial) bool { return v.GraphID == f.graph.ID })
					expected.reviews = slices.DeleteFunc(slices.Clone(before.reviews), func(v models.ReviewItem) bool { return v.GraphID == f.graph.ID })
					remaining = slices.DeleteFunc(slices.Clone(graphs), func(v models.KnowledgeGraph) bool { return v.ID == f.graph.ID })
				}
				if !reflect.DeepEqual(readDeleteCleanupFacts(t, f.db, f.task.TenantID), expected) {
					t.Fatal("graph deletion/Cleanup changed remaining rows, execution history, frozen input or settled operations")
				}
				var actual []models.KnowledgeGraph
				if err := f.db.Where("tenant_id = ?", f.task.TenantID).Order("id").Find(&actual).Error; err != nil || len(actual) != len(remaining) {
					t.Fatalf("incorrect remaining graph set: %v", err)
				}
				for i, kg := range actual {
					prior := remaining[i]
					if kg.ID != prior.ID || !kg.AdmissionClosed || kg.AdmissionCause != "cleanup" || kg.AdmissionExecutionID != child.ExecutionID || kg.Version != prior.Version+1 || (kg.Status == "archived") != (wantStatus == execution.ExecutionStatusSuccess) || kg.EngineID != prior.EngineID || kg.OntologyID != prior.OntologyID || kg.IndexManagement != prior.IndexManagement || !reflect.DeepEqual(kg.CatalogPath, prior.CatalogPath) {
						t.Fatal("Cleanup closed/archived the wrong graph or changed its target")
					}
					if outcome == "cleanup_receipt_rollback" && kg.Status != prior.Status {
						t.Fatal("failed final transaction retained an archive")
					}
				}
				if outcome == "closure_first" {
					// Check admission independently of the version CAS rejection.
					if err := repository.NewKnowledgeGraphRepository(f.db).DeleteDefinition(ctx, f.graph.ID, f.graph.TenantID, f.graph.Version+1); !errors.Is(err, repository.ErrGraphAdmissionClosed) {
						t.Fatalf("current version deleted a closed graph: %v", err)
					}
					if !reflect.DeepEqual(readDeleteCleanupFacts(t, f.db, f.task.TenantID), expected) {
						t.Fatal("refused current-version deletion changed history/domain rows")
					}
				}
				receipt, err := execution.ReadCleanupReceipt(*child)
				if err != nil {
					t.Fatal(err)
				}
				if outcome == "cleanup_receipt_rollback" {
					if receipt != nil {
						t.Fatal("failed final transaction retained a public receipt")
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
					if err := json.Unmarshal([]byte(payload), &result); err != nil || result.Statistics["knowledge_graphs"] != int64(len(remaining)) || result.Statistics["archived_graphs"] != int64(len(remaining)) || result.Statistics["build_tasks"] != int64(len(expected.tasks)) || result.Statistics["build_materials"] != int64(len(expected.materials)) || result.Statistics["review_items"] != int64(len(expected.reviews)) || result.Statistics["cancelled_build_tasks"] != 0 || result.Statistics["deleted_records"] != 0 {
						t.Fatalf("Cleanup reported stale scope or invented deletion/cancellation: %v", err)
					}
				}
			})
		}
	}
}

func addGraphDeleteCleanupSibling(t *testing.T, f *reviewCleanupPGFixture) {
	t.Helper()
	kg := f.graph
	kg.ID = 0
	kg.Name = "remaining-graph"
	kg.CatalogPath.Segments = slices.Clone(f.graph.CatalogPath.Segments)
	kg.CatalogPath.Segments[len(kg.CatalogPath.Segments)-1].Name = "remaining-graph"
	if err := f.db.Create(&kg).Error; err != nil {
		t.Fatal(err)
	}
	task := models.BuildTask{TenantID: kg.TenantID, GraphID: kg.ID, Name: "remaining-task", Status: models.BuildStatusPending, Stats: []byte(`{}`)}
	if err := f.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	material := models.BuildMaterial{TenantID: kg.TenantID, GraphID: kg.ID, TaskID: task.ID, Type: "document", FileName: "remaining.txt", FilePath: "build/remaining.txt", Status: models.BuildMaterialStatusPending, Stats: []byte(`{}`)}
	if err := f.db.Create(&material).Error; err != nil {
		t.Fatal(err)
	}
	item := models.ReviewItem{TenantID: kg.TenantID, GraphID: kg.ID, TaskID: task.ID, MaterialID: material.ID, ItemType: models.ReviewItemEntity, Content: []byte(`{"name":"remaining"}`), Status: models.ReviewStatusPending}
	if err := f.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
}
