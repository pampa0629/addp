package repository

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The owner journal is tested against the real PG lease and JSONB replacement,
// independently of a fake object uploader. It is not a source permission proof.
func TestIntegrationPostgresManagerRasterCOGExactInputJournal(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run through make test-manager-postgres")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.RasterCOGTask{}, &models.RasterCOG{}); err != nil {
		t.Fatal(err)
	}
	tenant := uint(time.Now().UnixNano()%100000000 + 980000000)
	t.Cleanup(func() {
		for _, query := range []struct {
			sql   string
			model any
		}{{"", &models.RasterCOG{}}, {"DELETE FROM common.execution_events WHERE tenant_id=?", nil}, {"", &commonExecution.TaskExecution{}}, {"", &models.RasterCOGTask{}}} {
			var err error
			if query.sql != "" {
				err = db.Exec(query.sql, tenant).Error
			} else {
				err = db.Unscoped().Where("tenant_id=?", tenant).Delete(query.model).Error
			}
			if err != nil {
				t.Error(err)
			}
		}
	})
	repo := NewRasterCOGRepository(db)
	task := &models.RasterCOGTask{TenantID: tenant, Name: "exact COG input journal", Enabled: true, Config: commonModels.JSONMap{"target": commonModels.JSONMap{"item_fingerprint": fmt.Sprint(tenant)}}}
	if err := repo.CreateTask(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	execution := newManagerRepositoryTestExecution(uuid.NewString(), int(tenant), commonExecution.TaskTypeRasterCOGGeneration, time.Now())
	snapshot, err := repo.TaskConfigSnapshot(t.Context(), task.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimExecution(t.Context(), task.ID, tenant, execution, true, snapshot); err != nil {
		t.Fatal(err)
	}
	ctx := managerExecutionLeaseContextForTest(t, db, execution.ExecutionID, int(tenant), time.Now())
	lease, _ := commonExecution.LeaseFromContext(ctx)
	result := &models.RasterCOG{TenantID: tenant, ItemFingerprint: fmt.Sprint(tenant), TaskID: &task.ID, LastExecutionID: &execution.ExecutionID, SourceEngineID: 14, TargetKind: models.RasterCOGTargetKindMinIO, StorageRef: "not-written", Status: models.RasterCOGStatusBuilding, Metadata: commonModels.JSONMap{}}
	if err := repo.Create(ctx, result); err != nil {
		t.Fatal(err)
	}
	prep := FileInputPreparation{ExecutionID: execution.ExecutionID, Attempt: lease.Attempt, PreparationID: uuid.NewString()}
	prefix, err := prep.Prefix(tenant, commonExecution.TaskTypeRasterCOGGeneration)
	if err != nil {
		t.Fatal(err)
	}
	prep.Objects = []FileInputObject{{Bucket: "manager", Object: prefix + "grid.img"}, {Bucket: "manager", Object: prefix + "grid.HDR"}}
	if err := repo.PrepareExecutionInputs(ctx, result.ID, prep); err != nil {
		t.Fatal(err)
	}
	if err := repo.PrepareExecutionInputs(ctx, result.ID, prep); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatal("duplicate manifest accepted", err)
	}
	if err := repo.FinishExecutionInputs(t.Context(), result.ID, prep, true, true); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatal("missing lease settled manifest", err)
	}
	if err := repo.FinishExecutionInputs(ctx, result.ID, prep, false, true); err != nil {
		t.Fatal(err)
	}
	merged, err := repo.MergeExecutionInputMetadata(ctx, result.ID, tenant, commonModels.JSONMap{"native": "facts"})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := RasterCOGInputPreparations(merged, tenant)
	if err != nil || len(entries) != 1 || !entries[0].Released || !entries[0].Watch || entries[0].Cleaned || len(entries[0].Objects) != 2 || merged["native"] != "facts" {
		t.Fatalf("lost uncertain exact ownership: %#v %v", entries, err)
	}
	if err := repo.FinishExecutionInputs(ctx, result.ID, prep, true, true); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatal("uncertain preparation silently restated", err)
	}
}
