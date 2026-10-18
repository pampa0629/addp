package service

import (
	"context"
	"os"
	"testing"
	"time"

	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	execution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresProductionMetadataSerialization(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	open := func() *gorm.DB {
		t.Helper()
		db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pool.Close() })
		return db
	}
	db, waitingDB := open(), open()
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	pool, _ := waitingDB.DB()
	pool.SetMaxOpenConns(1)
	appName := "production_merge_" + uuid.NewString()
	if err := waitingDB.Exec("SELECT set_config('application_name',?,false)", appName).Error; err != nil {
		t.Fatal(err)
	}
	owner, token := "production-worker", uuid.NewString()
	expires := time.Now().UTC().Add(time.Hour)
	item := execution.TaskExecution{TenantID: 999999, ExecutionID: uuid.NewString(), Module: execution.ModuleTransfer, TaskType: execution.TaskTypeSync, Source: execution.ModuleTransfer, TriggerType: execution.TriggerTypeManual, Status: execution.ExecutionStatusRunning, ExecutionBoundary: execution.ExecutionBoundaryBounded, Attempt: 1, LeaseOwner: &owner, LeaseToken: &token, LeaseExpiresAt: &expires}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Where("execution_id=? AND tenant_id=?", item.ExecutionID, item.TenantID).Delete(&execution.Event{}).Error; err != nil {
			t.Error(err)
		}
		if err := db.Where("id=? AND tenant_id=?", item.ID, item.TenantID).Delete(&execution.TaskExecution{}).Error; err != nil {
			t.Error(err)
		}
		var remaining int64
		if err := db.Model(&execution.TaskExecution{}).Where("id=?", item.ID).Count(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("production cleanup not proven: count=%d error=%v", remaining, err)
		}
	})
	lease, err := execution.LeaseFromExecution(item)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(execution.ContextWithLease(t.Context(), lease), 10*time.Second)
	defer cancel()
	locator := "addp://engine/2/path/outdoor/persons?type=table"
	if err := repository.NewTaskRepository(db).BeginTargetProduction(ctx, lease, locator); err != nil {
		t.Fatal(err)
	}
	svc := NewExecutionService(waitingDB, execution.NewTaskExecutionRepository(waitingDB))
	// A generic writer starts while another transaction owns the execution row.
	// The owner then records a fact before releasing the lock. The writer must
	// merge the newly committed metadata, not overwrite it with its earlier view.
	writeWhileLocked := func(write func() error, record func(*gorm.DB) error) {
		t.Helper()
		blocker := db.WithContext(ctx).Begin()
		if blocker.Error != nil {
			t.Fatal(blocker.Error)
		}
		defer blocker.Rollback()
		if err := blocker.Exec("SELECT id FROM common.task_executions WHERE id=? FOR UPDATE", item.ID).Error; err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() { result <- write() }()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var waiting bool
			if err := db.Raw("SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=? AND wait_event_type='Lock')", appName).Scan(&waiting).Error; err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("metadata writer never waited for the row lock")
			case <-ticker.C:
			}
		}
		if err := record(blocker); err != nil {
			t.Fatal(err)
		}
		if err := blocker.Commit().Error; err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("metadata writer did not finish")
		}
	}
	writeWhileLocked(func() error {
		return svc.UpdateExecution(ctx, uint(item.ID), map[string]interface{}{"metadata": commonmodels.JSONMap{"outputs": map[string]interface{}{"row_count": 0}}})
	}, func(tx *gorm.DB) error {
		return repository.NewTaskRepository(tx).RecordTargetPreparation(ctx, lease, locator, plugin.TableTargetPreparation{Disposition: plugin.TableTargetRecreated})
	})
	writeWhileLocked(func() error {
		return svc.FinishExecution(ctx, uint(item.ID), models.ExecutionStatusSuccess, nil, commonmodels.JSONMap{"lineage_facts": map[string]interface{}{"test": true}})
	}, func(tx *gorm.DB) error {
		return repository.NewTaskRepository(tx).RecordTargetCommit(ctx, lease, locator)
	})
	var stored execution.TaskExecution
	if err := db.First(&stored, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	record, err := models.DecodeTargetProduction(stored.Metadata[models.TargetProductionMetadataKey])
	if err != nil || record.Phase != models.TargetProductionCommitted || record.Preparation != plugin.TableTargetRecreated || stored.Status != execution.ExecutionStatusSuccess || stored.Metadata["outputs"] == nil || stored.Metadata["lineage_facts"] == nil {
		t.Fatalf("serialized completion lost facts: record=%+v status=%s error=%v", record, stored.Status, err)
	}
	if execution.Observe(&stored).Metadata[models.TargetProductionMetadataKey] != nil {
		t.Fatal("private production receipt leaked into observation")
	}
	receipt, err := svc.ReadProductionReceipt(ctx, uint(item.TenantID), item.ExecutionID)
	if err != nil || receipt.Attempt != item.Attempt || receipt.Preparation != plugin.TableTargetRecreated || receipt.UserOrigin != nil {
		t.Fatalf("persisted production receipt not readable as owner evidence: %+v %v", receipt, err)
	}
}
