package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerGLBIsolatedOutputPublication(t *testing.T) {
	for _, sourceFormat := range []string{"ifc", "3ds", "dae", "obj", "stl"} {
		t.Run(sourceFormat, func(t *testing.T) { exerciseManagerGLBIsolatedOutputPublication(t, sourceFormat) })
	}
}

func exerciseManagerGLBIsolatedOutputPublication(t *testing.T, sourceFormat string) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run through make test-manager-postgres")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Model3DGLBTask{}, &models.Model3DGLB{}, &models.TaskResourceBinding{}); err != nil {
		t.Fatal(err)
	}
	tenantID := uint(time.Now().UnixNano()%100000000 + 980000000)
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.TaskResourceBinding{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.Model3DGLB{}).Error
		_ = db.Exec("DELETE FROM common.execution_events WHERE tenant_id = ?", tenantID).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.Model3DGLBTask{}).Error
	})
	task := createModel3DGLBExecutionRepositoryTestTask(t, db, tenantID)
	repo := NewModel3DGLBRepository(db)
	item := newManagerRepositoryTestExecution(uuid.NewString(), int(tenantID), commonExecution.TaskTypeModel3DGLBGeneration, time.Now())
	ctx := commonExecution.WithUserOrigin(context.Background(), authtest.NewTenantUserAuthContext(fmt.Sprint(tenantID), "31", nil))
	if err := commonExecution.ApplyUserOrigin(ctx, item); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.TaskConfigSnapshot(ctx, task.ID, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimExecution(ctx, task.ID, tenantID, item, true, snapshot); err != nil {
		t.Fatal(err)
	}
	oldID := uuid.NewString()
	old := &models.Model3DGLB{TenantID: tenantID, ItemFingerprint: "glb-fp", TaskID: &task.ID, LastExecutionID: &oldID, SourceEngineID: 33, SourceFormat: sourceFormat, StorageRef: glbCleanupTestRef("published.glb"), Status: models.Model3DGLBStatusReady, Metadata: commonModels.JSONMap{}}
	if err := db.Create(old).Error; err != nil {
		t.Fatal(err)
	}
	ctx = managerExecutionLeaseContextForTest(t, db, item.ExecutionID, int(tenantID), time.Now())
	desired := *old
	desired.LastExecutionID = &item.ExecutionID
	prepared, err := repo.PrepareExecutionOutput(ctx, &desired, glbCleanupTestRef("execution-owned.glb"))
	if err != nil || prepared.StorageRef != glbCleanupTestRef("published.glb") {
		t.Fatalf("prepare=%+v %v", prepared, err)
	}
	if err := repo.CompleteExecutionOutput(ctx, task.ID, tenantID, item.ExecutionID, old.ID, glbCleanupTestRef("execution-owned.glb"), map[string]interface{}{"status": models.Model3DGLBStatusReady, "storage_ref": glbCleanupTestRef("execution-owned.glb"), "last_execution_id": item.ExecutionID}, map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "progress": 100, "completed_at": time.Now()}, time.Now()); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetByID(context.Background(), old.ID, tenantID)
	if err != nil || current.StorageRef != glbCleanupTestRef("execution-owned.glb") {
		t.Fatalf("published=%+v %v", current, err)
	}
	if n, err := repo.CleanupExecutionOutputs(context.Background(), 100, nil, func(_ context.Context, tenant uint, ref string) error {
		if tenant != tenantID || ref != glbCleanupTestRef("published.glb") {
			t.Fatalf("cleanup escaped owner: %d %s", tenant, ref)
		}
		return nil
	}); err != nil || n != 1 {
		t.Fatalf("cleanup=%d %v", n, err)
	}
	// Deleting a finished task retains the current result. A new FTP task must
	// explicitly confirm replacement and keeps old provenance on failure.
	if err := repo.DeleteTask(t.Context(), task.ID, tenantID); err != nil {
		t.Fatal(err)
	}
	replacement := &models.Model3DGLBTask{TenantID: tenantID, Name: "replacement", Enabled: true, Config: commonModels.JSONMap{"source": commonModels.JSONMap{"item_fingerprint": "glb-fp"}}}
	if err := repo.CreateTask(t.Context(), replacement); err != nil {
		t.Fatal(err)
	}
	replacementExecution := newManagerRepositoryTestExecution(uuid.NewString(), int(tenantID), commonExecution.TaskTypeModel3DGLBGeneration, time.Now())
	actorCtx := commonExecution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext(fmt.Sprint(tenantID), "31", nil))
	if err := commonExecution.ApplyUserOrigin(actorCtx, replacementExecution); err != nil {
		t.Fatal(err)
	}
	replacementExecution.ExecutionConfig = commonModels.JSONMap{"config_version": models.Model3DGLBFileConfigVersion, "source": commonModels.JSONMap{"item_fingerprint": "glb-fp"}}
	replacementSnapshot, err := repo.TaskConfigSnapshot(t.Context(), replacement.ID, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimExecution(t.Context(), replacement.ID, tenantID, replacementExecution, false, replacementSnapshot); !errors.Is(err, ErrExistingResultActionRequired) {
		t.Fatalf("replacement skipped confirmation: %v", err)
	}
	if _, err := repo.ClaimExecution(t.Context(), replacement.ID, tenantID, replacementExecution, true, replacementSnapshot); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteTask(t.Context(), replacement.ID, tenantID); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("active FTP task deleted: %v", err)
	}
	// Synthetic reference exercises queue/owner transaction semantics only;
	// no System Allow, source access or FTP conversion is claimed by this T2.
	if err := db.Model(replacementExecution).Updates(map[string]interface{}{"execution_authorization_id": int64(51), "authorization_expires_at": time.Now().Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	replacementCtx := managerExecutionLeaseContextForTest(t, db, replacementExecution.ExecutionID, int(tenantID), time.Now())
	next := *current
	next.TaskID = &replacement.ID
	next.LastExecutionID = &replacementExecution.ExecutionID
	prepared, err = repo.PrepareExecutionOutput(replacementCtx, &next, "failed-replacement.glb")
	if err != nil || *prepared.TaskID != task.ID || prepared.StorageRef != glbCleanupTestRef("execution-owned.glb") {
		t.Fatalf("replacement preparation=%+v %v", prepared, err)
	}
	if err := repo.CompleteExecutionOutput(replacementCtx, replacement.ID, tenantID, replacementExecution.ExecutionID, current.ID, "failed-replacement.glb", map[string]interface{}{"error_message": "conversion failed"}, map[string]interface{}{"status": commonExecution.ExecutionStatusFailed, "progress": 0, "completed_at": time.Now()}, time.Now()); err != nil {
		t.Fatal(err)
	}
	preserved, err := repo.GetByID(t.Context(), current.ID, tenantID)
	if err != nil || *preserved.TaskID != task.ID || preserved.StorageRef != glbCleanupTestRef("execution-owned.glb") || preserved.Status != models.Model3DGLBStatusReady {
		t.Fatalf("failure changed current artifact: %+v %v", preserved, err)
	}
}
