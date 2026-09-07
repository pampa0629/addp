package repository

import (
	"context"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"gorm.io/gorm"
)

func managerExecutionLeaseContextForTest(t *testing.T, db *gorm.DB, executionID string, tenantID int, claimAt time.Time) context.Context {
	t.Helper()
	var execution commonExecution.TaskExecution
	if err := db.Where("execution_id = ? AND tenant_id = ?", executionID, tenantID).First(&execution).Error; err != nil {
		t.Fatalf("load Manager execution %s: %v", executionID, err)
	}
	if execution.Status == commonExecution.ExecutionStatusPending {
		claimed, lease, err := NewBoundedExecutionQueueRepository(db).ClaimNext(
			context.Background(), execution.TaskType, "manager-repository-test", claimAt.UTC(), 100*365*24*time.Hour,
		)
		if err != nil {
			t.Fatalf("claim Manager execution %s: %v", executionID, err)
		}
		if claimed == nil || lease == nil || claimed.ExecutionID != executionID {
			t.Fatalf("claimed Manager execution = %#v, want %s", claimed, executionID)
		}
		return commonExecution.ContextWithLease(context.Background(), *lease)
	}
	lease, err := commonExecution.LeaseFromExecution(execution)
	if err != nil {
		t.Fatalf("load Manager execution lease %s: %v", executionID, err)
	}
	return commonExecution.ContextWithLease(context.Background(), lease)
}
