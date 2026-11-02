package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/addp/common/engine/plugins/postgresql"
	execution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Standard gate only: this persists control-plane rows in the allowed test DB.
// No Worker is started and no Business source or target is opened.
func TestIntegrationPostgresRunApprovalAtomicity(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal("open run approval fixture failed")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err = db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err = execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	recoverRunApprovalFixture(t, db)
	if err = db.AutoMigrate(&models.TransferTask{}); err != nil {
		t.Fatal(err)
	}
	svc, task := seedRunApprovalTask(t, db)
	t.Cleanup(func() {
		for _, query := range []func() error{
			func() error {
				return db.Where("source_task_id = ? AND tenant_id = ? AND module = ?", *execution.NewSourceTaskIDFromUint(task.ID), task.TenantID, "transfer").Delete(&execution.TaskExecution{}).Error
			},
			func() error {
				return db.Where("task_id = ? AND tenant_id = ?", task.ID, task.TenantID).Delete(&models.RunApproval{}).Error
			},
			func() error { return db.Unscoped().Delete(task).Error },
		} {
			if err := query(); err != nil {
				t.Error("clean exact run approval fixture failed", err)
			}
		}
	})
	// Unique names avoid interfering with other test records; no global deletion.
	if err = db.Model(task).Update("name", "run_approval_"+uuid.NewString()).Error; err != nil {
		t.Fatal(err)
	}
	user, delegated := runApprovalAuth()
	ctx := execution.WithUserOrigin(context.Background(), delegated)
	approval, err := svc.CreateRunApproval(ctx, delegated, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.DecideRunApproval(ctx, user, approval.ID.String(), models.RunApprovalDecision{Decision: "approved", RequestFingerprint: approval.RequestFingerprint}); err != nil {
		t.Fatal(err)
	}
	req := models.StartTaskRequest{ApprovalID: approval.ID.String(), RequestFingerprint: approval.RequestFingerprint}
	start := make(chan struct{})
	out := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.StartApprovedTask(ctx, delegated, task.ID, req)
			out <- err
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	successes := 0
	for err := range out {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent consumers succeeded %d times", successes)
	}
	stored, err := svc.GetRunApproval(ctx, user, approval.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "consumed" || stored.ExecutionID == nil {
		t.Fatalf("approval not consumed with execution: %#v", stored)
	}
	var rows []execution.TaskExecution
	if err = db.Where("source_task_id = ? AND tenant_id = ? AND module = ?", *execution.NewSourceTaskIDFromUint(task.ID), task.TenantID, "transfer").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ExecutionID != *stored.ExecutionID || rows[0].Status != execution.ExecutionStatusPending {
		t.Fatalf("want exactly one linked pending execution, got %#v", rows)
	}
	if rows[0].ActorPrincipalID == nil || *rows[0].ActorPrincipalID != 42 || rows[0].ActorTenantMembershipID == nil {
		t.Fatal("execution lost current user origin")
	}
}

// Explicit recovery is limited to an orphan from this fixture's UUID namespace.
// No age-based cleanup, wildcard deletion or task-owned execution is accepted.
func recoverRunApprovalFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	id := os.Getenv("ADDP_TRANSFER_TEST_RECOVER_EXECUTION")
	if id == "" {
		return
	}
	if _, err := uuid.Parse(id); err != nil {
		t.Fatal("invalid explicit recovery execution ID")
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		var row execution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND source = ?", id, 7, "transfer", "sync", "transfer").First(&row).Error; err != nil {
			return err
		}
		if row.SourceTaskID == nil || row.SourceTaskName == nil || !strings.HasPrefix(*row.SourceTaskName, "run_approval_") {
			return errors.New("recovery does not identify a run approval fixture")
		}
		if _, err := uuid.Parse(strings.TrimPrefix(*row.SourceTaskName, "run_approval_")); err != nil {
			return errors.New("invalid fixture UUID marker")
		}
		if row.Status != execution.ExecutionStatusPending || row.Attempt != 0 || row.LeaseOwner != nil || row.LeaseToken != nil || row.ParentExecutionID != nil || row.ExecutionAuthorizationID != nil || (row.RecordsRead != nil && *row.RecordsRead != 0) || (row.RecordsWritten != nil && *row.RecordsWritten != 0) {
			return errors.New("recovery refuses any attempted or effectful execution")
		}
		var count int64
		if err := tx.Unscoped().Model(&models.TransferTask{}).Where("id = ? AND tenant_id = ?", *row.SourceTaskID, row.TenantID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errors.New("recovery refuses an execution with a surviving task")
		}
		if err := tx.Model(&execution.Event{}).Where("execution_id = ? AND tenant_id = ?", id, 7).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errors.New("recovery refuses an execution with process events")
		}
		return tx.Where("execution_id = ? AND tenant_id = ?", id, 7).Delete(&execution.TaskExecution{}).Error
	})
	if err != nil {
		t.Fatal("explicit run approval fixture recovery refused", err)
	}
}
