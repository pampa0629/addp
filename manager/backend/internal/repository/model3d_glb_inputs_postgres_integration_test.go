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
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerGLBInputOwnership(t *testing.T) {
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
	execution := newManagerRepositoryTestExecution(uuid.NewString(), int(tenantID), commonExecution.TaskTypeModel3DGLBGeneration, time.Now())
	ctx := commonExecution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext(fmt.Sprint(tenantID), "31", nil))
	if err := commonExecution.ApplyUserOrigin(ctx, execution); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.TaskConfigSnapshot(ctx, task.ID, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimExecution(ctx, task.ID, tenantID, execution, true, snapshot); err != nil {
		t.Fatal(err)
	}
	ctx = managerExecutionLeaseContextForTest(t, db, execution.ExecutionID, int(tenantID), time.Now())
	lease, _ := commonExecution.LeaseFromContext(ctx)
	result, err := repo.PrepareExecutionOutput(ctx, &models.Model3DGLB{TenantID: tenantID, TaskID: &task.ID, ItemFingerprint: "glb-fp", SourceEngineID: 33, SourceFormat: "ifc", LastExecutionID: &execution.ExecutionID}, "owned-output.glb")
	if err != nil {
		t.Fatal(err)
	}
	p := Model3DGLBInputPreparation{ExecutionID: execution.ExecutionID, Attempt: lease.Attempt, PreparationID: uuid.NewString()}
	p.Objects = []Model3DGLBInputObject{{Bucket: "manager", Object: fmt.Sprintf("temp/model3d-input/tenant_%d/%s/attempt_%d/%s/model.ifc", tenantID, p.ExecutionID, p.Attempt, p.PreparationID)}}
	if err := repo.PrepareExecutionInputs(ctx, result.ID, p); err != nil {
		t.Fatal(err)
	}
	before, err := repo.GetByID(t.Context(), result.ID, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := readModel3DGLBOutputState(before.Metadata)
	if err != nil || len(state.InputPreparations) != 1 || state.InputPreparations[0].Objects[0] != p.Objects[0] {
		t.Fatalf("owner read lost exact durable manifest: %+v %v", state, err)
	}
	removed := 0
	deleteInput := func(_ context.Context, tenant uint, object Model3DGLBInputObject) error {
		if tenant != tenantID || object != p.Objects[0] {
			t.Fatal("input cleanup escaped exact ownership")
		}
		removed++
		return nil
	}
	deleteOutput := func(context.Context, uint, string) error { return nil }
	if n, err := repo.CleanupExecutionOutputs(t.Context(), 100, deleteInput, deleteOutput); err != nil || n != 0 {
		t.Fatalf("live inputs cleaned %d %v", n, err)
	}
	// Simulate the persisted terminal fact left by crash recovery. The owner
	// manifest remains durable even when the result itself has been soft-deleted.
	if err := db.Model(execution).Updates(map[string]interface{}{"status": commonExecution.ExecutionStatusFailed, "completed_at": time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishExecutionInputs(ctx, result.ID, p, true, true); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("terminal worker settled crash: %v", err)
	}
	if err := db.Delete(result).Error; err != nil {
		t.Fatal(err)
	}
	closed := p
	closed.PreparationID = uuid.NewString()
	closed.Objects = []Model3DGLBInputObject{{Bucket: "manager", Object: fmt.Sprintf("temp/model3d-input/tenant_%d/%s/attempt_%d/%s/closed.ifc", tenantID, closed.ExecutionID, closed.Attempt, closed.PreparationID)}}
	closed.Released, closed.Cleaned = true, true
	history := &models.Model3DGLB{TenantID: tenantID, ItemFingerprint: "closed-input-history", SourceEngineID: 33, SourceFormat: "ifc", StorageRef: "closed-published.glb", Status: models.Model3DGLBStatusReady, Metadata: model3DGLBOutputMetadata(nil, model3DGLBOutputState{InputPreparations: []Model3DGLBInputPreparation{closed}})}
	if err := db.Create(history).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := repo.CleanupExecutionOutputs(t.Context(), 1, deleteInput, deleteOutput); err != nil {
			t.Fatal(err)
		}
	}
	if removed != 0 {
		t.Fatalf("unknown writer was deleted without stop proof: %d", removed)
	}
	if err := db.Unscoped().First(result, result.ID).Error; err != nil {
		t.Fatal(err)
	}
	state, err = readModel3DGLBOutputState(result.Metadata)
	if err != nil || len(state.InputPreparations) != 1 || state.InputPreparations[0].Released || state.InputPreparations[0].Watch || state.InputPreparations[0].Cleaned {
		t.Fatalf("lost durable crash evidence: %+v %v", state, err)
	}
}
