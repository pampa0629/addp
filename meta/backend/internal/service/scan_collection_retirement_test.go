package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
)

func exerciseCollectionRetirement(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	var tasks []models.ScanTask
	for _, id := range []uint{9001, 9002} {
		task := models.ScanTask{TenantID: 7001, EngineID: id, Name: "retirement", Enabled: true, Schedule: "* * * * *", NextRunAt: &now, Scope: models.JSONMap{"type": "engine"}, Parameters: models.JSONMap{"scan_depth": "basic"}}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	pending := scantask.NewTaskExecution(&tasks[0], 0, "postgresql", "scheduled", "meta", nil, now)
	pending.ExecutionConfig["engine_type"] = "postgresql"
	if err := db.Create(pending).Error; err != nil {
		t.Fatal(err)
	}
	running := scantask.NewTaskExecution(&tasks[0], 0, "postgresql", "scheduled", "meta", nil, now)
	running.ExecutionConfig["engine_type"] = "postgresql"
	running.Status = "running"
	running.Attempt = 1
	if err := db.Create(running).Error; err != nil {
		t.Fatal(err)
	}
	reader := func(_ context.Context, tenant, id uint) (*commonmodels.Engine, error) {
		kind := "s3"
		if id == 9001 {
			kind = "postgresql"
		}
		return &commonmodels.Engine{ID: id, TenantID: &tenant, EngineType: kind}, nil
	}
	boom := errors.New("System unavailable")
	err := db.Transaction(func(tx *gorm.DB) error {
		return RetireCollectionApproval(ctx, tx, func(ctx context.Context, t, id uint) (*commonmodels.Engine, error) {
			if id == 9002 {
				return nil, boom
			}
			return reader(ctx, t, id)
		})
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	var original models.ScanTask
	if err := db.First(&original, tasks[0].ID).Error; err != nil || !original.Enabled || original.NextRunAt == nil {
		t.Fatal("failed migration partially disabled schedule", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return RetireCollectionApproval(ctx, tx, reader) }); err != nil {
		t.Fatal(err)
	}
	for i, task := range tasks {
		var got models.ScanTask
		if err := db.First(&got, task.ID).Error; err != nil {
			t.Fatal(err)
		}
		if got.Enabled != (i == 1) || got.Schedule != task.Schedule || (i == 0 && got.NextRunAt != nil) {
			t.Fatal("migration changed wrong schedule", got)
		}
	}
	for _, r := range []*execution.TaskExecution{pending, running} {
		var got execution.TaskExecution
		if err := db.Where("execution_id=?", r.ExecutionID).Take(&got).Error; err != nil {
			t.Fatal(err)
		}
		want := "running"
		if r == pending {
			want = "cancelled"
		}
		if got.Status != want || got.Metadata[scanCancellationKey] == nil {
			t.Fatal("incorrect cancellation boundary", got.Status, got.Metadata)
		}
		if err := (&ScanExecutionService{db: db}).checkScanCancellation(ctx, got.TenantID, got.ExecutionID); !errors.Is(err, errScanCancellationRequested) {
			t.Fatal("next enumeration was not stopped", err)
		}
	}
	var count int64
	if err := db.Model(&execution.TaskExecution{}).Where("tenant_id=?", 7001).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("migration created execution", count, err)
	}
}
func TestCollectionRetirementDisablesOnlyOldPGAndStopsAtEnumerationBoundary(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)
	createTaskExecutionTable(t, db)
	exerciseCollectionRetirement(t, db)
}
