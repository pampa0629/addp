package service

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

func TestMetaBoundedClaimAndExpiredRecoveryAreLeaseFenced(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createTaskExecutionTable(t, db)
	createScanTaskTable(t, db)
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	task := &models.ScanTask{TenantID: 7, EngineID: 9, Name: "lease scan", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create scan task: %v", err)
	}
	execution := scantask.NewTaskExecution(task, 1, "s3", commonExecution.TriggerTypeManual, commonExecution.ModuleMeta, nil, now)
	if err := db.Create(execution).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
	if err := db.Model(task).Updates(map[string]interface{}{
		"last_execution_id": execution.ExecutionID, "last_execution_status": commonExecution.ExecutionStatusPending,
	}).Error; err != nil {
		t.Fatalf("initialize task summary: %v", err)
	}

	service := NewScanExecutionService(db, nil, nil, nil)
	claimed, lease, err := service.ClaimNextBoundedExecution(context.Background(), "meta-worker-test", now, time.Minute)
	if err != nil || claimed == nil || lease == nil {
		t.Fatalf("claim = %#v %#v, %v", claimed, lease, err)
	}
	if claimed.Status != commonExecution.ExecutionStatusRunning || claimed.Attempt != 1 || lease.Token == "" {
		t.Fatalf("claimed execution = %#v, lease = %#v", claimed, lease)
	}

	recovered, err := service.FailExpiredBoundedExecutions(context.Background(), now.Add(2*time.Minute), 100)
	if err != nil || recovered != 1 {
		t.Fatalf("expired recovery = %d, %v", recovered, err)
	}
	got, err := service.GetExecution(context.Background(), execution.ExecutionID, 7)
	if err != nil {
		t.Fatalf("get recovered execution: %v", err)
	}
	if got.Status != commonExecution.ExecutionStatusFailed || got.LeaseToken != nil || got.LeaseOwner != nil {
		t.Fatalf("recovered execution = %#v", got)
	}
	var gotTask models.ScanTask
	if err := db.First(&gotTask, task.ID).Error; err != nil {
		t.Fatalf("get task summary: %v", err)
	}
	if gotTask.LastExecutionID == nil || *gotTask.LastExecutionID != execution.ExecutionID || gotTask.LastExecutionStatus == nil || *gotTask.LastExecutionStatus != commonExecution.ExecutionStatusFailed {
		t.Fatalf("task summary = %#v", gotTask)
	}

	service.BindBoundedLease(execution.ExecutionID, *lease)
	defer service.UnbindBoundedLease(execution.ExecutionID)
	err = service.completeBoundedExecution(context.Background(), claimed, commonExecution.ExecutionStatusSuccess, now.Add(3*time.Minute), map[string]interface{}{"progress": 100})
	if !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("late completion error = %v, want conflict", err)
	}
}

func TestCreateTaskRunRejectsSecondActiveExecution(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createTaskExecutionTable(t, db)
	createScanTaskTable(t, db)
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	task := &models.ScanTask{TenantID: 7, EngineID: 9, Name: "single active scan", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create scan task: %v", err)
	}

	service := NewScanExecutionService(db, nil, nil, nil)
	first, err := service.CreateTaskManualRun(context.Background(), task, 1)
	if err != nil || first == nil {
		t.Fatalf("first execution = %#v, %v", first, err)
	}
	second, err := service.CreateTaskManualRun(context.Background(), task, 1)
	if !errors.Is(err, commonapi.ErrConflict) || second != nil {
		t.Fatalf("second execution = %#v, %v; want conflict", second, err)
	}
	var count int64
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("execution count = %d, want 1", count)
	}
}
