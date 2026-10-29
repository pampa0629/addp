package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Real PG contention and exact effect callbacks; not Hosted storage acceptance.
func TestIntegrationPostgresManagerGLBCleanupLifecycle(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PG gate")
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("MANAGER_POSTGRES_TEST_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	pool.SetMaxOpenConns(5)
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := repository.MigrateCleanupArtifactSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Model3DGLBTask{}, &models.TaskResourceBinding{}, &models.Model3DGLB{}, &models.RasterCOG{}, &models.Model3DTiles{}, &models.GaussianSplatKSplat{}, &models.PointCloudCOPC{}, &models.PPTXPDF{}, &models.TileCache{}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cleaned", "unreleased", "watch", "uncleaned", "invalid_manifest", "unknown_hold", "prepare_vs_complete", "prepare_vs_inputs", "cleanup_vs_complete"} {
		t.Run(mode, func(t *testing.T) {
			fp := uuid.NewString()
			task := models.Model3DGLBTask{TenantID: 7, TaskType: execution.TaskTypeModel3DGLBGeneration, Name: "cleanup-glb", Enabled: true, Config: commonmodels.JSONMap{"version": 1, "source": commonmodels.JSONMap{"item_fingerprint": fp}}}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			actorCtx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "7", nil))
			item := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "manager", TaskType: execution.TaskTypeModel3DGLBGeneration, Source: "manager", TriggerType: "manual", Status: "pending", ExecutionBoundary: "bounded", MaxAttempts: 1, ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
			var row models.Model3DGLB
			var cleanupLease execution.Lease
			t.Cleanup(func() {
				ids := []string{item.ExecutionID, cleanupLease.ExecutionID}
				object, _ := json.Marshal([]string{"manager", "tenant_7/glb/" + fp + ".glb"})
				identities := []string{item.TaskType + ":" + fp, fmt.Sprintf("%s:task:%d", item.TaskType, task.ID), fmt.Sprintf("%s:%d", item.TaskType, row.ID), string(object)}
				for _, action := range []func() error{
					func() error {
						return db.Where("tenant_id = ? AND identity IN ?", 7, identities).Delete(&models.CleanupTarget{}).Error
					},
					func() error {
						return db.Where("execution_id IN ?", ids).Delete(&models.CleanupArtifactOperation{}).Error
					},
					func() error { return db.Exec("DELETE FROM common.execution_events WHERE execution_id IN ?", ids).Error },
					func() error { return db.Where("execution_id IN ?", ids).Delete(&execution.TaskExecution{}).Error },
					func() error { return db.Unscoped().Delete(&models.Model3DGLB{}, row.ID).Error },
					func() error { return db.Unscoped().Delete(&models.Model3DGLBTask{}, task.ID).Error },
				} {
					if err := action(); err != nil {
						t.Error(err)
					}
				}
			})
			if err := execution.ApplyUserOrigin(actorCtx, &item); err != nil {
				t.Fatal(err)
			}
			ref := cog.ObjectStorageRef("manager", "tenant_7/glb/"+fp+".glb")
			row = models.Model3DGLB{TenantID: 7, TaskID: &task.ID, ItemFingerprint: fp, SourceEngineID: 8, SourceFormat: "ifc", Status: models.Model3DGLBStatusReady, Metadata: commonmodels.JSONMap{}}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			cleanupCtx, createdCleanupLease := artifactTestLease(t, db)
			cleanupLease = createdCleanupLease
			calls := 0
			svc := NewCleanupArtifactService(db, CleanupArtifactEffects{Bucket: "manager", Objects: artifactCleanerFunc(func(c context.Context, got string) error {
				if got != ref {
					return errors.New("wrong physical target")
				}
				calls++
				return nil
			})})
			freeze := func() ([]CleanupArtifactTarget, error) {
				return svc.Freeze(cleanupCtx, 7, nil, nil, []repository.CleanupManagedArtifact{{ID: row.ID, TenantID: 7, TaskType: item.TaskType}})
			}
			// Freeze before a pending output exists; racing Cleanup must recheck it.
			initial, err := freeze()
			if err != nil {
				t.Fatal(err)
			}
			repo := repository.NewModel3DGLBRepository(db)
			snapshot, err := repo.TaskConfigSnapshot(actorCtx, task.ID, 7)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.ClaimExecution(actorCtx, task.ID, 7, &item, true, snapshot); err != nil {
				t.Fatal(err)
			}
			claimed, lease, err := repository.NewBoundedExecutionQueueRepository(db).ClaimNext(t.Context(), []string{item.TaskType}, "glb-race", time.Now(), time.Minute)
			if err != nil || claimed == nil || claimed.ExecutionID != item.ExecutionID {
				t.Fatalf("claim: %+v %v", claimed, err)
			}
			ctx := execution.ContextWithLease(t.Context(), *lease)
			row.LastExecutionID = &item.ExecutionID
			if _, err := repo.PrepareExecutionOutput(ctx, &row, ref); err != nil {
				t.Fatal(err)
			}
			prep := repository.Model3DGLBInputPreparation{ExecutionID: item.ExecutionID, Attempt: lease.Attempt, PreparationID: uuid.NewString()}
			prep.Objects = []repository.Model3DGLBInputObject{{Bucket: "manager", Object: fmt.Sprintf("temp/model3d-input/tenant_7/%s/attempt_%d/%s/input.ifc", item.ExecutionID, lease.Attempt, prep.PreparationID)}}
			if err := repo.PrepareExecutionInputs(ctx, row.ID, prep); err != nil {
				t.Fatal(err)
			}

			complete := func(r *repository.Model3DGLBRepository) error {
				return r.CompleteExecutionOutput(ctx, task.ID, 7, item.ExecutionID, row.ID, ref, map[string]interface{}{"storage_ref": ref, "status": models.Model3DGLBStatusReady}, map[string]interface{}{"status": execution.ExecutionStatusSuccess}, time.Now())
			}
			if strings.Contains(mode, "_vs_") {
				a := db.Begin()
				defer a.Rollback()
				var blockerPID int
				if err := a.Raw("SELECT pg_backend_pid()").Scan(&blockerPID).Error; err != nil {
					t.Fatal(err)
				}
				if mode == "cleanup_vs_complete" {
					var locked models.Model3DGLBTask
					if err := a.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, task.ID).Error; err != nil {
						t.Fatal(err)
					}
				} else if _, err := execution.LockActiveLease(ctx, a, *lease); err != nil {
					t.Fatal(err)
				}
				b := db.Begin()
				defer b.Rollback()
				if err := b.Exec("SET LOCAL statement_timeout = '5s'").Error; err != nil {
					t.Fatal(err)
				}
				var waiterPID int
				if err := b.Raw("SELECT pg_backend_pid()").Scan(&waiterPID).Error; err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() {
					r := repository.NewModel3DGLBRepository(b)
					var err error
					if mode == "prepare_vs_inputs" {
						err = r.FinishExecutionInputs(ctx, row.ID, prep, true, true)
					} else {
						err = complete(r)
					}
					if err == nil {
						err = b.Commit().Error
					} else {
						_ = b.Rollback().Error
					}
					done <- err
				}()
				deadline := time.Now().Add(3 * time.Second)
				for {
					var blocked bool
					if err := db.Raw("SELECT ? = ANY(pg_blocking_pids(?))", blockerPID, waiterPID).Scan(&blocked).Error; err != nil {
						t.Fatal(err)
					}
					if blocked {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("operation never reached controlled row-lock wait")
					}
					time.Sleep(5 * time.Millisecond)
				}
				if err := a.Exec("SET LOCAL statement_timeout = '5s'").Error; err != nil {
					t.Fatal(err)
				}
				var competing error
				if mode == "cleanup_vs_complete" {
					competing = a.Transaction(func(tx *gorm.DB) error {
						_, err := svc.Prepare(cleanupCtx, tx, cleanupLease, initial[0], events.CleanupModePhysical, "delete")
						return err
					})
				} else {
					_, competing = repository.NewModel3DGLBRepository(a).PrepareExecutionOutput(ctx, &row, ref)
				}
				if !errors.Is(competing, commonapi.ErrConflict) {
					t.Fatalf("expected safety rejection, never 40P01: %v", competing)
				}
				if err := a.Commit().Error; err != nil {
					t.Fatal(err)
				}
				if err := <-done; err != nil {
					t.Fatalf("live owner failed during contention (including 40P01): %v", err)
				}
				if calls != 0 {
					t.Fatal("racing Cleanup dispatched effect")
				}
				if mode == "prepare_vs_inputs" {
					if err := complete(repo); err != nil {
						t.Fatal(err)
					}
				}
				var final execution.TaskExecution
				var finalTask models.Model3DGLBTask
				var finalRow models.Model3DGLB
				if err := db.Where("execution_id = ?", item.ExecutionID).First(&final).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.First(&finalTask, task.ID).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.First(&finalRow, row.ID).Error; err != nil {
					t.Fatal(err)
				}
				if final.Status != "success" || (finalTask.LastExecutionStatus == nil || *finalTask.LastExecutionStatus != "success") || finalRow.StorageRef != ref {
					t.Fatal("terminal/result transaction did not commit together")
				}
				if err := repo.FinishExecutionInputs(ctx, row.ID, prep, true, true); err == nil {
					t.Fatal("terminal owner wrote late input facts")
				}
				return
			}
			if mode != "unreleased" {
				if err := repo.FinishExecutionInputs(ctx, row.ID, prep, mode != "watch", mode != "uncleaned"); err != nil {
					t.Fatal(err)
				}
			}
			if err := complete(repo); err != nil {
				t.Fatal(err)
			}
			if mode == "invalid_manifest" {
				if err := db.Model(&models.Model3DGLB{}).Where("id = ?", row.ID).UpdateColumn("metadata", gorm.Expr("jsonb_set(metadata, '{model3d_output_lifecycle,input_preparations,0,objects,0,object}', '\"wrong-tenant/input.ifc\"'::jsonb)")).Error; err != nil {
					t.Fatal(err)
				}
			}
			targets, err := freeze()
			if mode != "cleaned" && mode != "unknown_hold" {
				if !errors.Is(err, commonapi.ErrConflict) {
					t.Fatalf("unsettled/invalid inputs allowed: %v", err)
				}
				return
			}
			if err != nil || len(targets) != 1 {
				t.Fatalf("cleaned audit manifest blocked: %v", err)
			}
			id := prepareArtifactTest(t, db, svc, cleanupCtx, cleanupLease, targets[0], events.CleanupModePhysical, "delete")
			if mode == "unknown_hold" {
				svc.effects.Objects = artifactCleanerFunc(func(context.Context, string) error { calls++; return context.DeadlineExceeded })
			}
			err = svc.Effect(cleanupCtx, id)
			if mode == "unknown_hold" {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
				blocked, err := freeze()
				if err != nil || len(blocked) != 1 || len(blocked[0].UnsettledOperationIDs) == 0 {
					t.Fatal("unknown hold missing from scan", err)
				}
				if err := db.Transaction(func(tx *gorm.DB) error {
					_, err := svc.Prepare(cleanupCtx, tx, cleanupLease, blocked[0], events.CleanupModePhysical, "delete")
					return err
				}); !errors.Is(err, commonapi.ErrConflict) {
					t.Fatal("cleaned audit bypassed unknown hold", err)
				}
				if err := svc.Effect(cleanupCtx, id); !errors.Is(err, commonapi.ErrConflict) || calls != 1 {
					t.Fatal("unknown effect replayed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error {
				stats, err := svc.Finalize(cleanupCtx, tx, cleanupLease, id)
				if stats.DeletedPhysicalArtifacts != 1 {
					t.Error(stats)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			var final models.Model3DGLB
			if err := db.Unscoped().First(&final, row.ID).Error; err != nil || !final.DeletedAt.Valid || calls != 1 {
				t.Fatalf("exact deletion: %+v %d %v", final, calls, err)
			}
			if err := repository.ValidateModel3DGLBCleanupLifecycle(&final); err != nil {
				t.Fatal("cleaned audit manifest was lost or corrupted", err)
			}
			var audit struct {
				Inputs []repository.Model3DGLBInputPreparation `json:"input_preparations"`
			}
			raw, err := json.Marshal(final.Metadata["model3d_output_lifecycle"])
			if err != nil || json.Unmarshal(raw, &audit) != nil || len(audit.Inputs) != 1 || audit.Inputs[0].PreparationID != prep.PreparationID || !audit.Inputs[0].Cleaned || len(audit.Inputs[0].Objects) != 1 || audit.Inputs[0].Objects[0] != prep.Objects[0] {
				t.Fatal("physical cleanup erased or changed the retained input audit")
			}
			for _, token := range targets[0].Tokens {
				if err := db.Transaction(func(tx *gorm.DB) error { return repository.GuardCleanupTargetWrite(cleanupCtx, tx, token) }); !errors.Is(err, commonapi.ErrConflict) {
					t.Fatal("late writer admitted", err)
				}
			}
		})
	}
}
