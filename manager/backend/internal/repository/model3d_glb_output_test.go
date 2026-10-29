package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
)

func TestGLBIsolatedOutputSwitchesOnlyWithAtomicSuccess(t *testing.T) {
	for _, sourceFormat := range []string{"ifc", "3ds", "dae", "obj", "stl"} {
		for _, outcome := range []string{"success", "failed", "lost lease"} {
			t.Run(sourceFormat+"/"+outcome, func(t *testing.T) {
				db := newTileCacheExecutionRepositoryTestDB(t)
				repo, item := pendingGLBAuthorizationFixture(t, db, time.Now())
				taskID, _ := commonExecution.ParseSourceTaskIDUint(item.SourceTaskID)
				oldExecution := "previous-render-version"
				old := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "glb-fp", TaskID: &taskID, LastExecutionID: &oldExecution, SourceEngineID: 33, SourceFormat: sourceFormat, StorageRef: glbCleanupTestRef("old.glb"), FileName: glbCleanupTestRef("old.glb"), Status: models.Model3DGLBStatusReady, Metadata: commonModels.JSONMap{"old_fact": "retained"}}
				if err := db.Create(old).Error; err != nil {
					t.Fatal(err)
				}
				ctx := managerExecutionLeaseContextForTest(t, db, item.ExecutionID, 7, time.Now())
				desired := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "glb-fp", TaskID: &taskID, LastExecutionID: &item.ExecutionID, SourceEngineID: 33, SourceFormat: sourceFormat, StorageRef: glbCleanupTestRef("new.glb"), FileName: glbCleanupTestRef("new.glb")}
				prepared, err := repo.PrepareExecutionOutput(ctx, desired, glbCleanupTestRef("new.glb"))
				if err != nil {
					t.Fatal(err)
				}
				if prepared.StorageRef != glbCleanupTestRef("old.glb") || prepared.Status != models.Model3DGLBStatusReady || *prepared.LastExecutionID != oldExecution {
					t.Fatalf("prepare changed visible result: %+v", prepared)
				}
				var deleted []string
				clean := func(_ context.Context, tenant uint, ref string) error {
					if tenant != 7 || ref == glbCleanupTestRef("old.glb") {
						t.Fatal("cleanup touched current result")
					}
					deleted = append(deleted, ref)
					return nil
				}
				if n, err := repo.CleanupExecutionOutputs(ctx, 100, nil, clean); err != nil || n != 0 {
					t.Fatalf("live pending cleaned: %d %v", n, err)
				}
				status := commonExecution.ExecutionStatusSuccess
				if outcome == "failed" {
					status = commonExecution.ExecutionStatusFailed
				}
				if outcome == "lost lease" {
					if err := db.Model(item).Update("lease_token", "replacement").Error; err != nil {
						t.Fatal(err)
					}
				}
				fields := map[string]interface{}{"status": models.Model3DGLBStatusReady, "storage_ref": glbCleanupTestRef("new.glb"), "file_name": glbCleanupTestRef("new.glb"), "metadata": commonModels.JSONMap{"built": true}, "last_execution_id": item.ExecutionID}
				err = repo.CompleteExecutionOutput(ctx, taskID, 7, item.ExecutionID, prepared.ID, glbCleanupTestRef("new.glb"), fields, map[string]interface{}{"status": status, "progress": 100, "completed_at": time.Now()}, time.Now())
				current, readErr := repo.GetByID(context.Background(), old.ID, 7)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if outcome == "lost lease" {
					if !errors.Is(err, commonAPI.ErrConflict) || current.StorageRef != glbCleanupTestRef("old.glb") {
						t.Fatalf("unfenced publish err=%v current=%+v", err, current)
					}
					state, _ := readModel3DGLBOutputState(current.Metadata)
					if state.Pending == nil {
						t.Fatal("failed transaction lost cleanup ownership")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if outcome == "success" {
					if current.StorageRef != glbCleanupTestRef("new.glb") || current.Status != models.Model3DGLBStatusReady || *current.LastExecutionID != item.ExecutionID {
						t.Fatalf("publish=%+v", current)
					}
					clean = func(_ context.Context, tenant uint, ref string) error {
						if tenant != 7 || ref != glbCleanupTestRef("old.glb") {
							t.Fatal("retired cleanup escaped exact old target")
						}
						deleted = append(deleted, ref)
						return nil
					}
				} else if current.StorageRef != glbCleanupTestRef("old.glb") || current.Status != models.Model3DGLBStatusReady || *current.LastExecutionID != oldExecution || current.Metadata["old_fact"] != "retained" {
					t.Fatalf("failure changed old result: %+v", current)
				}
				if outcome == "failed" {
					calls := 0
					for i := 0; i < 2; i++ {
						if n, err := repo.CleanupExecutionOutputs(t.Context(), 100, nil, func(context.Context, uint, string) error { calls++; return nil }); err != nil || n != 0 {
							t.Fatal("uncertain writer was reclaimed", n, err)
						}
					}
					if calls != 0 {
						t.Fatal("failed execution was treated as stopped", calls)
					}
					return
				}

				if n, err := repo.CleanupExecutionOutputs(context.Background(), 100, nil, clean); err != nil || n != 1 {
					t.Fatalf("cleanup=%d %v", n, err)
				}
				if n, err := repo.CleanupExecutionOutputs(t.Context(), 100, nil, clean); err != nil || n != 0 {
					t.Fatal("retired object retained", n, err)
				}

			})
		}
	}
}

func TestGLBOutputCleanupStorageFailureDoesNotStarveNextOwnedObject(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewModel3DGLBRepository(db)
	for _, name := range []string{glbCleanupTestRef("unavailable.glb"), glbCleanupTestRef("collectable.glb")} {
		item := &models.Model3DGLB{TenantID: 7, ItemFingerprint: name, SourceEngineID: 33, SourceFormat: "ifc", StorageRef: "current-" + name, Status: models.Model3DGLBStatusReady, Metadata: model3DGLBOutputMetadata(nil, model3DGLBOutputState{Garbage: []model3DGLBOutputRef{{StorageRef: name}}})}
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	storageErr := errors.New("storage unavailable for exact object")
	deleteExact := func(_ context.Context, tenant uint, ref string) error {
		if tenant != 7 {
			t.Fatal("cleanup changed tenant")
		}
		if ref == glbCleanupTestRef("unavailable.glb") {
			return storageErr
		}
		if ref != glbCleanupTestRef("collectable.glb") {
			t.Fatalf("unexpected object %s", ref)
		}
		return nil
	}
	if n, err := repo.CleanupExecutionOutputs(t.Context(), 1, nil, deleteExact); n != 0 || !errors.Is(err, storageErr) {
		t.Fatalf("first cleanup=%d %v", n, err)
	}
	if n, err := repo.CleanupExecutionOutputs(t.Context(), 1, nil, deleteExact); n != 1 || err != nil {
		t.Fatalf("failed object starved next row: %d %v", n, err)
	}
}

func TestGLBRecreatedTaskDoesNotTakeOwnershipOfPreviousArtifactBeforeSuccess(t *testing.T) {
	for _, outcome := range []string{"success", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			db := newTileCacheExecutionRepositoryTestDB(t)
			if err := db.Exec("CREATE TABLE manager.task_resource_bindings (id INTEGER PRIMARY KEY, tenant_id INTEGER, task_definition_id INTEGER)").Error; err != nil {
				t.Fatal(err)
			}
			repo := NewModel3DGLBRepository(db)
			oldTask := createModel3DGLBExecutionRepositoryTestTask(t, db, 7)
			oldExecution := uuid.NewString()
			old := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "glb-fp", TaskID: &oldTask.ID, LastExecutionID: &oldExecution, SourceEngineID: 33, SourceFormat: "ifc", StorageRef: glbCleanupTestRef("old.glb"), Status: models.Model3DGLBStatusReady, Metadata: model3DGLBOutputMetadata(nil, model3DGLBOutputState{})}
			if err := db.Create(old).Error; err != nil {
				t.Fatal(err)
			}
			if err := repo.DeleteTask(t.Context(), oldTask.ID, 7); err != nil {
				t.Fatal(err)
			}
			newTask := createModel3DGLBExecutionRepositoryTestTask(t, db, 7)
			item := newManagerRepositoryTestExecution(uuid.NewString(), 7, commonExecution.TaskTypeModel3DGLBGeneration, time.Now())
			if err := commonExecution.ApplyUserOrigin(commonExecution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "31", nil)), item); err != nil {
				t.Fatal(err)
			}
			snapshot, err := repo.TaskConfigSnapshot(t.Context(), newTask.ID, 7)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.ClaimExecution(t.Context(), newTask.ID, 7, item, true, snapshot); err != nil {
				t.Fatal(err)
			}
			ctx := managerExecutionLeaseContextForTest(t, db, item.ExecutionID, 7, time.Now())
			desired := *old
			desired.TaskID = &newTask.ID
			desired.LastExecutionID = &item.ExecutionID
			result, err := repo.PrepareExecutionOutput(ctx, &desired, glbCleanupTestRef("new.glb"))
			if err != nil || *result.TaskID != oldTask.ID || result.StorageRef != glbCleanupTestRef("old.glb") {
				t.Fatalf("prepare stole provenance: %+v %v", result, err)
			}
			status := commonExecution.ExecutionStatusSuccess
			if outcome == "failed" {
				status = commonExecution.ExecutionStatusFailed
			}
			err = repo.CompleteExecutionOutput(ctx, newTask.ID, 7, item.ExecutionID, result.ID, glbCleanupTestRef("new.glb"), map[string]interface{}{"status": models.Model3DGLBStatusReady, "storage_ref": glbCleanupTestRef("new.glb")}, map[string]interface{}{"status": status, "progress": 100, "completed_at": time.Now()}, time.Now())
			if err != nil {
				t.Fatalf("recreated task could not complete: %v", err)
			}
			current, err := repo.GetByID(t.Context(), result.ID, 7)
			if err != nil {
				t.Fatal(err)
			}
			wantTask, wantRef := newTask.ID, glbCleanupTestRef("new.glb")
			if outcome == "failed" {
				wantTask, wantRef = oldTask.ID, glbCleanupTestRef("old.glb")
			}
			if *current.TaskID != wantTask || current.StorageRef != wantRef {
				t.Fatalf("result provenance=%+v", current)
			}
		})
	}
}

func TestGLBOutputPreparationUsesTheCurrentResultSelection(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo, item := pendingGLBAuthorizationFixture(t, db, time.Now())
	taskID, _ := commonExecution.ParseSourceTaskIDUint(item.SourceTaskID)
	older := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "glb-fp", TaskID: &taskID, SourceEngineID: 33, SourceFormat: "ifc", StorageRef: glbCleanupTestRef("older.glb"), Status: models.Model3DGLBStatusReady, Metadata: commonModels.JSONMap{}, UpdatedAt: time.Now().Add(-time.Hour)}
	if err := db.Create(older).Error; err != nil {
		t.Fatal(err)
	}
	newer := *older
	newer.ID = 0
	newer.StorageRef = glbCleanupTestRef("current.glb")
	newer.UpdatedAt = time.Now()
	if err := db.Create(&newer).Error; err != nil {
		t.Fatal(err)
	}
	ctx := managerExecutionLeaseContextForTest(t, db, item.ExecutionID, 7, time.Now())
	desired := newer
	desired.LastExecutionID = &item.ExecutionID
	prepared, err := repo.PrepareExecutionOutput(ctx, &desired, glbCleanupTestRef("new.glb"))
	if err != nil || prepared.ID != newer.ID || prepared.StorageRef != glbCleanupTestRef("current.glb") {
		t.Fatalf("non-current result selected: %+v %v", prepared, err)
	}
}

func TestGLBFTPRecreatedTaskRequiresOverwriteAndProtectsThePreviousResult(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	if err := db.Exec("CREATE TABLE manager.task_resource_bindings (id INTEGER PRIMARY KEY, tenant_id INTEGER, task_definition_id INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	repo := NewModel3DGLBRepository(db)
	oldTask := createModel3DGLBExecutionRepositoryTestTask(t, db, 7)
	if err := db.Model(&oldTask).Update("semantic_key", "glb-fp").Error; err != nil {
		t.Fatal(err)
	}
	old := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "glb-fp", TaskID: &oldTask.ID, SourceEngineID: 33, SourceFormat: "ifc", StorageRef: glbCleanupTestRef("published.glb"), Status: models.Model3DGLBStatusReady, Metadata: model3DGLBOutputMetadata(nil, model3DGLBOutputState{})}
	if err := db.Create(old).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteTask(t.Context(), oldTask.ID, 7); err != nil {
		t.Fatal(err)
	}
	newTask := createModel3DGLBExecutionRepositoryTestTask(t, db, 7)
	if err := db.Model(&newTask).Update("semantic_key", "glb-fp").Error; err != nil {
		t.Fatal(err)
	}
	item := newManagerRepositoryTestExecution(uuid.NewString(), 7, commonExecution.TaskTypeModel3DGLBGeneration, time.Now())
	if err := commonExecution.ApplyUserOrigin(commonExecution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "31", nil)), item); err != nil {
		t.Fatal(err)
	}
	item.ExecutionConfig = commonModels.JSONMap{"config_version": models.Model3DGLBFileConfigVersion, "source": commonModels.JSONMap{"item_fingerprint": "glb-fp"}}
	snapshot, err := repo.TaskConfigSnapshot(t.Context(), newTask.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimExecution(t.Context(), newTask.ID, 7, item, false, snapshot); !errors.Is(err, ErrExistingResultActionRequired) {
		t.Fatalf("missing overwrite accepted: %v", err)
	}
	if _, err := repo.ClaimExecution(t.Context(), newTask.ID, 7, item, true, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteTask(t.Context(), newTask.ID, 7); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("active FTP task deleted: %v", err)
	}
	if _, err := ObserveCleanupTarget(t.Context(), db, CleanupTargetKey{TenantID: old.TenantID, Kind: CleanupTargetGeneration, Identity: commonExecution.TaskTypeModel3DGLBGeneration + ":glb-fp"}); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatal("active replacement did not retain shared generation hold", err)
	}
}

func TestGLBOutputCleanupDoesNotPromoteHistoryOrDeleteReferencedObjects(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewModel3DGLBRepository(db)
	older := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "same-source", SourceEngineID: 33, SourceFormat: "ifc", StorageRef: glbCleanupTestRef("old.glb"), Status: models.Model3DGLBStatusReady, UpdatedAt: time.Now().Add(-time.Hour), Metadata: model3DGLBOutputMetadata(commonModels.JSONMap{"scope_engine": json.Number("9007199254740993")}, model3DGLBOutputState{Garbage: []model3DGLBOutputRef{{StorageRef: glbCleanupTestRef("shared.glb")}}})}
	if err := db.Create(older).Error; err != nil {
		t.Fatal(err)
	}
	current := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "same-source", SourceEngineID: 33, SourceFormat: "ifc", StorageRef: glbCleanupTestRef("shared.glb"), Status: models.Model3DGLBStatusReady, UpdatedAt: time.Now(), Metadata: commonModels.JSONMap{}}
	if err := db.Create(current).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := repo.CleanupExecutionOutputs(t.Context(), 100, nil, func(context.Context, uint, string) error { t.Fatal("deleted a referenced object"); return nil }); err != nil || n != 0 {
		t.Fatalf("referenced cleanup=%d %v", n, err)
	}
	selected, err := repo.GetCurrentByFingerprint(t.Context(), 7, "same-source")
	if err != nil || selected.ID != current.ID {
		t.Fatalf("cleanup promoted history: %+v %v", selected, err)
	}
	var raw []byte
	if err := db.Model(older).Select("metadata").Where("id = ?", older.ID).Row().Scan(&raw); err != nil || !strings.Contains(string(raw), "9007199254740993") {
		t.Fatalf("metadata rounded scope: %s %v", raw, err)
	}
	if err := db.Delete(current).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := repo.CleanupExecutionOutputs(t.Context(), 100, nil, func(_ context.Context, tenant uint, ref string) error {
		if tenant != 7 || ref != glbCleanupTestRef("shared.glb") {
			t.Fatalf("unexpected cleanup %d %s", tenant, ref)
		}
		return nil
	}); err != nil || n != 1 {
		t.Fatalf("unreferenced cleanup=%d %v", n, err)
	}
}

func TestGLBOutputCleanupRecoversCrashWithoutDeletingPublishedArtifact(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo, item := pendingGLBAuthorizationFixture(t, db, time.Now())
	taskID, _ := commonExecution.ParseSourceTaskIDUint(item.SourceTaskID)
	ctx := managerExecutionLeaseContextForTest(t, db, item.ExecutionID, 7, time.Now())
	desired := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "glb-fp", TaskID: &taskID, LastExecutionID: &item.ExecutionID, SourceEngineID: 33, SourceFormat: "ifc"}
	result, err := repo.PrepareExecutionOutput(ctx, desired, glbCleanupTestRef("isolated.glb"))
	if err != nil || result.StorageRef != "" {
		t.Fatalf("prepare=%+v %v", result, err)
	}
	if err := db.Model(item).Updates(map[string]interface{}{"status": commonExecution.ExecutionStatusFailed, "completed_at": time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	// Source cleanup may soft-delete the result before this retry. The marker
	// must still own the orphaned object.
	if err := db.Delete(result).Error; err != nil {
		t.Fatal(err)
	}
	count := 0
	if n, err := repo.CleanupExecutionOutputs(context.Background(), 100, nil, func(_ context.Context, tenant uint, ref string) error {
		count++
		if tenant != 7 || ref != glbCleanupTestRef("isolated.glb") {
			t.Fatal("unexpected cleanup")
		}
		return nil
	}); err != nil || n != 0 || count != 0 {
		t.Fatalf("uncertain crash output was deleted: %d %v", n, err)
	}
}

func TestGLBOutputPreparationRejectsDifferentTaskOwner(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo, item := pendingGLBAuthorizationFixture(t, db, time.Now())
	taskID, _ := commonExecution.ParseSourceTaskIDUint(item.SourceTaskID)
	ctx := managerExecutionLeaseContextForTest(t, db, item.ExecutionID, 7, time.Now())
	wrongTaskID := taskID + 1
	desired := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "glb-fp", TaskID: &wrongTaskID, LastExecutionID: &item.ExecutionID, SourceEngineID: 33, SourceFormat: "ifc"}
	if _, err := repo.PrepareExecutionOutput(ctx, desired, glbCleanupTestRef("isolated.glb")); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("different owner accepted: %v", err)
	}
	var count int64
	if err := db.Model(&models.Model3DGLB{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected preparation persisted result: %d %v", count, err)
	}
}

func glbCleanupTestRef(name string) string {
	return cog.ObjectStorageRef("manager", "tenant_7/model3d-quick-view/test/"+name)
}
