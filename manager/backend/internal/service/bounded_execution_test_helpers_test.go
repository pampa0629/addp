package service

import (
	"context"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/repository"
	"gorm.io/gorm"
)

func managerExecutionLeaseContextForServiceTest(executionID string, tenantID uint) context.Context {
	return commonExecution.ContextWithLease(context.Background(), commonExecution.Lease{
		ExecutionID: executionID,
		TenantID:    int(tenantID),
		Attempt:     1,
		Token:       "lease-" + executionID,
		Owner:       "manager-service-test",
	})
}

func leaseStoredManagerExecutionForServiceTest(t *testing.T, db *gorm.DB, executionID string, tenantID uint) context.Context {
	t.Helper()
	leaseCtx := managerExecutionLeaseContextForServiceTest(executionID, tenantID)
	lease, _ := commonExecution.LeaseFromContext(leaseCtx)
	expiresAt := time.Now().UTC().Add(time.Minute)
	result := db.Model(&commonExecution.TaskExecution{}).
		Where("execution_id = ? AND tenant_id = ?", executionID, int(tenantID)).
		Updates(map[string]interface{}{
			"attempt":          lease.Attempt,
			"lease_owner":      lease.Owner,
			"lease_token":      lease.Token,
			"lease_expires_at": expiresAt,
		})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("seed execution lease: rows=%d error=%v", result.RowsAffected, result.Error)
	}
	return leaseCtx
}

func runManagerBoundedExecutionForTest(t *testing.T, db *gorm.DB, taskType string, dispatcher *BoundedExecutionDispatcher) {
	t.Helper()
	queue := repository.NewBoundedExecutionQueueRepository(db)
	execution, lease, err := queue.ClaimNext(context.Background(), taskType, "manager-test-1", time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatalf("claim %s execution: %v", taskType, err)
	}
	if execution == nil || lease == nil {
		t.Fatalf("claim %s execution returned no work", taskType)
	}
	if err := dispatcher.RunClaimedExecution(context.Background(), execution, *lease); err != nil {
		t.Fatalf("run %s execution: %v", taskType, err)
	}
}

func runManagerBoundedExecutionAsyncForTest(t *testing.T, db *gorm.DB, taskType string, dispatcher *BoundedExecutionDispatcher) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runManagerBoundedExecutionForTest(t, db, taskType, dispatcher)
	}()
	return done
}
