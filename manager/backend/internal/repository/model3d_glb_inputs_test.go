package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func glbInputOwnershipFixture(t *testing.T, db *gorm.DB) (*Model3DGLBRepository, *models.Model3DGLB, context.Context, Model3DGLBInputPreparation) {
	t.Helper()
	repo, execution := pendingGLBAuthorizationFixture(t, db, time.Now())
	taskID, _ := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
	ctx := managerExecutionLeaseContextForTest(t, db, execution.ExecutionID, 7, time.Now())
	lease, _ := commonExecution.LeaseFromContext(ctx)
	item, err := repo.PrepareExecutionOutput(ctx, &models.Model3DGLB{TenantID: 7, TaskID: &taskID, ItemFingerprint: "glb-fp", SourceEngineID: 33, SourceFormat: "3ds", LastExecutionID: &execution.ExecutionID}, glbCleanupTestRef("published.glb"))
	if err != nil {
		t.Fatal(err)
	}
	p := Model3DGLBInputPreparation{ExecutionID: execution.ExecutionID, Attempt: lease.Attempt, PreparationID: uuid.NewString()}
	prefix := fmt.Sprintf("temp/model3d-input/tenant_7/%s/attempt_%d/%s/", p.ExecutionID, p.Attempt, p.PreparationID)
	p.Objects = []Model3DGLBInputObject{{Bucket: "manager", Object: prefix + "Model.3DS"}, {Bucket: "manager", Object: prefix + "textures/Color.PNG "}}
	return repo, item, ctx, p
}

func readInputPreparation(t *testing.T, db *gorm.DB, id uint) Model3DGLBInputPreparation {
	t.Helper()
	var item models.Model3DGLB
	if err := db.Unscoped().First(&item, id).Error; err != nil {
		t.Fatal(err)
	}
	state, err := readModel3DGLBOutputState(item.Metadata)
	if err != nil || len(state.InputPreparations) != 1 {
		t.Fatalf("input state=%+v err=%v", state, err)
	}
	return state.InputPreparations[0]
}

func TestGLBInputOwnershipKeepsExactManifestAfterCleanup(t *testing.T) {
	for _, scenario := range []string{"settled", "delete failure", "unknown upload", "crash", "superseded attempt"} {
		t.Run(scenario, func(t *testing.T) {
			db := newTileCacheExecutionRepositoryTestDB(t)
			repo, item, ctx, p := glbInputOwnershipFixture(t, db)
			if err := repo.PrepareExecutionInputs(ctx, item.ID, p); err != nil {
				t.Fatal(err)
			}
			removed := 0
			var outputDeletes []string
			deleteOutput := func(_ context.Context, _ uint, ref string) error {
				outputDeletes = append(outputDeletes, ref)
				return nil
			}
			deleteInput := func(_ context.Context, tenant uint, object Model3DGLBInputObject) error {
				if tenant != 7 || (object != p.Objects[0] && object != p.Objects[1]) {
					t.Fatal("cleanup changed exact input identity")
				}
				removed++
				return nil
			}
			if n, err := repo.CleanupExecutionOutputs(t.Context(), 100, deleteInput, deleteOutput); err != nil || n != 0 {
				t.Fatalf("live input cleaned %d %v", n, err)
			}
			if scenario == "crash" || scenario == "superseded attempt" {
				updates := map[string]interface{}{"status": commonExecution.ExecutionStatusFailed}
				if scenario == "superseded attempt" {
					updates = map[string]interface{}{"attempt": p.Attempt + 1, "lease_token": "replacement-token", "status": commonExecution.ExecutionStatusRunning}
				}
				if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", p.ExecutionID).Updates(updates).Error; err != nil {
					t.Fatal(err)
				}
				if err := repo.FinishExecutionInputs(ctx, item.ID, p, true, true); !errors.Is(err, commonAPI.ErrConflict) {
					t.Fatalf("stale worker settled manifest: %v", err)
				}
				if err := db.Delete(item).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := repo.FinishExecutionInputs(ctx, item.ID, p, scenario != "unknown upload", false); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "settled" || scenario == "delete failure" {
				if err := repo.CompleteExecutionOutput(ctx, *item.TaskID, 7, p.ExecutionID, item.ID, glbCleanupTestRef("published.glb"), map[string]interface{}{"status": models.Model3DGLBStatusReady, "storage_ref": glbCleanupTestRef("published.glb"), "metadata": commonModels.JSONMap{"new_render_fact": true}}, map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "progress": 100, "completed_at": time.Now()}, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "delete failure" {
				if _, err := repo.CleanupExecutionOutputs(t.Context(), 100, func(context.Context, uint, Model3DGLBInputObject) error { return errors.New("store offline") }, deleteOutput); err == nil {
					t.Fatal("storage failure hidden")
				}
			}
			if _, err := repo.CleanupExecutionOutputs(t.Context(), 100, deleteInput, deleteOutput); err != nil {
				t.Fatal(err)
			}
			current := readInputPreparation(t, db, item.ID)
			if len(current.Objects) != 2 || !strings.HasSuffix(current.Objects[1].Object, "Color.PNG ") {
				t.Fatal("cleanup lost raw manifest")
			}
			switch scenario {
			case "settled":
				if !current.Released || current.Watch || !current.Cleaned || removed != 2 {
					t.Fatal("known input cleanup missing", current, removed)
				}
			case "delete failure":
				if !current.Released || current.Watch || current.Cleaned || removed != 0 {
					t.Fatal("unknown deletion was replayed", current, removed)
				}
			case "unknown upload":
				if !current.Released || !current.Watch || current.Cleaned || removed != 0 {
					t.Fatal("unknown upload was reclaimed", current, removed)
				}
			default:
				if current.Released || current.Cleaned || removed != 0 {
					t.Fatal("lease/terminal fabricated a stopped writer", current, removed)
				}
			}
			if _, err := repo.CleanupExecutionOutputs(t.Context(), 100, deleteInput, deleteOutput); err != nil {
				t.Fatal(err)
			}
			if scenario != "settled" && removed != 0 {
				t.Fatal("unresolved input cleanup replayed", removed)
			}

		})
	}
}

func TestGLBInputOwnershipRejectsChangedOrUnboundManifest(t *testing.T) {
	for _, scenario := range []string{"wrong execution", "wrong attempt", "wrong tenant", "duplicate", "escape", "bad nonce", "missing output owner", "duplicate prepare", "changed finish"} {
		t.Run(scenario, func(t *testing.T) {
			db := newTileCacheExecutionRepositoryTestDB(t)
			repo, item, ctx, p := glbInputOwnershipFixture(t, db)
			switch scenario {
			case "wrong execution":
				p.ExecutionID = uuid.NewString()
			case "wrong attempt":
				p.Attempt++
			case "wrong tenant":
				p.Objects[0].Object = strings.Replace(p.Objects[0].Object, "tenant_7", "tenant_8", 1)
			case "duplicate":
				p.Objects = append(p.Objects, p.Objects[0])
			case "escape":
				p.Objects[0].Object += "/../secret"
			case "bad nonce":
				p.PreparationID = "not-a-uuid"
			case "missing output owner":
				if err := db.Model(item).Update("metadata", `{}`).Error; err != nil {
					t.Fatal(err)
				}
			case "duplicate prepare", "changed finish":
				if err := repo.PrepareExecutionInputs(ctx, item.ID, p); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if scenario == "changed finish" {
				p.Objects[0].Object += "-changed"
				err = repo.FinishExecutionInputs(ctx, item.ID, p, true, true)
			} else {
				err = repo.PrepareExecutionInputs(ctx, item.ID, p)
			}
			if !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("unbound manifest accepted: %v", err)
			}
		})
	}
}

func TestGLBInputClosedHistoryDoesNotConsumeCleanupLimit(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo, item, ctx, p := glbInputOwnershipFixture(t, db)
	if err := repo.PrepareExecutionInputs(ctx, item.ID, p); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishExecutionInputs(ctx, item.ID, p, true, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteExecutionOutput(ctx, *item.TaskID, 7, p.ExecutionID, item.ID, glbCleanupTestRef("published.glb"), map[string]interface{}{"status": models.Model3DGLBStatusReady, "storage_ref": glbCleanupTestRef("published.glb")}, map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "progress": 100, "completed_at": time.Now()}, time.Now()); err != nil {
		t.Fatal(err)
	}
	closed := readInputPreparation(t, db, item.ID)
	if !closed.Cleaned || len(closed.Objects) != 2 {
		t.Fatal("closed history missing")
	}
	waiting := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "waiting-for-cleanup", SourceEngineID: 33, SourceFormat: "ifc", StorageRef: glbCleanupTestRef("another-current.glb"), Status: models.Model3DGLBStatusReady, Metadata: model3DGLBOutputMetadata(nil, model3DGLBOutputState{Garbage: []model3DGLBOutputRef{{StorageRef: glbCleanupTestRef("waiting.glb")}}})}
	if err := db.Create(waiting).Error; err != nil {
		t.Fatal(err)
	}
	removed := 0
	if n, err := repo.CleanupExecutionOutputs(t.Context(), 1, func(context.Context, uint, Model3DGLBInputObject) error {
		t.Fatal("cleaned history retried")
		return nil
	}, func(_ context.Context, _ uint, ref string) error {
		if ref != glbCleanupTestRef("waiting.glb") {
			t.Fatal("wrong output")
		}
		removed++
		return nil
	}); err != nil || n != 1 || removed != 1 {
		t.Fatalf("closed audit consumed pending-work limit n=%d err=%v removed=%d", n, err, removed)
	}
	if !readInputPreparation(t, db, item.ID).Cleaned {
		t.Fatal("cleanup erased closed history")
	}
}
