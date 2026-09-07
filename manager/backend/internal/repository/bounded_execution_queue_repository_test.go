package repository

import (
	"context"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
)

func TestManagerBoundedTaskTypesHaveSingleOwnershipDefinition(t *testing.T) {
	taskTypes := ManagerBoundedTaskTypes()
	if len(taskTypes) != len(managerExecutionOwnerships) {
		t.Fatalf("Manager bounded task type count = %d, ownership count = %d", len(taskTypes), len(managerExecutionOwnerships))
	}
	seen := make(map[string]struct{}, len(taskTypes))
	for _, taskType := range taskTypes {
		if _, duplicate := seen[taskType]; duplicate {
			t.Fatalf("duplicate Manager bounded task type %q", taskType)
		}
		seen[taskType] = struct{}{}
		if _, ok := managerExecutionOwnerships[taskType]; !ok {
			t.Fatalf("Manager bounded task type %q has no ownership definition", taskType)
		}
	}
}

func TestManagerBoundedQueueClaimsAdHocEmbeddingWithoutTaskOwner(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	queue := NewBoundedExecutionQueueRepository(db)
	now := time.Now().UTC()
	execution := &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "manager-ad-hoc-embedding", Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeEmbedding, Source: commonExecution.ModuleManager,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status:            commonExecution.ExecutionStatusPending,
		TriggerType:       commonExecution.TriggerTypeManual,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(execution).Error; err != nil {
		t.Fatalf("create ad-hoc embedding execution: %v", err)
	}
	claimed, lease, err := queue.ClaimNext(context.Background(), commonExecution.TaskTypeEmbedding, "manager-repository-test", now, time.Minute)
	if err != nil || claimed == nil || lease == nil {
		t.Fatalf("claim ad-hoc embedding = %#v/%#v, error = %v", claimed, lease, err)
	}
	if claimed.ExecutionID != execution.ExecutionID || claimed.Status != commonExecution.ExecutionStatusRunning {
		t.Fatalf("claimed ad-hoc embedding = %#v", claimed)
	}
}

func TestManagerBoundedFailureConvergesAfterOwnerTaskWasDeleted(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	taskRepo := NewTileCacheRepository(db)
	queue := NewBoundedExecutionQueueRepository(db)
	task := createTileCacheExecutionRepositoryTestTask(t, db, 7)
	now := time.Now().UTC()
	execution := newTileCacheRepositoryTestExecution("manager-owner-deleted", int(task.TenantID), now)
	if _, err := taskRepo.ClaimExecution(context.Background(), task.ID, task.TenantID, execution, false); err != nil {
		t.Fatalf("enqueue execution: %v", err)
	}
	claimed, lease, err := queue.ClaimNext(
		context.Background(), commonExecution.TaskTypeVectorTileCacheGeneration,
		"manager-repository-test", now, time.Minute,
	)
	if err != nil || claimed == nil || lease == nil {
		t.Fatalf("claim execution = %#v/%#v, error = %v", claimed, lease, err)
	}
	if err := db.Unscoped().Delete(&models.TileCacheTask{}, task.ID).Error; err != nil {
		t.Fatalf("delete owner task: %v", err)
	}
	if err := queue.FailClaimed(context.Background(), claimed, *lease, "manager.execution.dispatch_failed", "task deleted", now.Add(time.Second)); err != nil {
		t.Fatalf("fail claimed execution after owner deletion: %v", err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load failed execution: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusFailed || stored.ErrorDetails["code"] != "manager.execution.dispatch_failed" {
		t.Fatalf("failed execution = %#v", stored)
	}
}

func TestManagerBoundedPendingExecutionFailsWhenOwnerTaskWasDeleted(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	taskRepo := NewTileCacheRepository(db)
	queue := NewBoundedExecutionQueueRepository(db)
	task := createTileCacheExecutionRepositoryTestTask(t, db, 7)
	now := time.Now().UTC()
	execution := newTileCacheRepositoryTestExecution("manager-pending-owner-deleted", int(task.TenantID), now)
	if _, err := taskRepo.ClaimExecution(context.Background(), task.ID, task.TenantID, execution, false); err != nil {
		t.Fatalf("enqueue execution: %v", err)
	}
	if err := db.Unscoped().Delete(&models.TileCacheTask{}, task.ID).Error; err != nil {
		t.Fatalf("delete owner task: %v", err)
	}
	claimed, lease, err := queue.ClaimNext(
		context.Background(), commonExecution.TaskTypeVectorTileCacheGeneration,
		"manager-repository-test", now, time.Minute,
	)
	if err != nil || claimed != nil || lease != nil {
		t.Fatalf("claim orphaned execution = %#v/%#v, error = %v", claimed, lease, err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load failed execution: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusFailed || stored.ErrorDetails["code"] != managerOwnerUnavailableCode {
		t.Fatalf("failed orphaned execution = %#v", stored)
	}
}
