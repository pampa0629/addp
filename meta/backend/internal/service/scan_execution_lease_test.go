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
	now := time.Now().UTC()
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

func TestCreateTaskRunInheritsOrchestratorParentActor(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createTaskExecutionTable(t, db)
	createScanTaskTable(t, db)
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	task := &models.ScanTask{TenantID: 7, EngineID: 9, Name: "orchestrated scan", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create scan task: %v", err)
	}
	principalID, membershipID, authorizationVersion := int64(41), int64(51), int64(6)
	parent := &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "77777777-7777-4777-8777-777777777777",
		Module: commonExecution.ModuleOrchestrator, TaskType: commonExecution.TaskTypeOrchestration,
		Source: commonExecution.ModuleOrchestrator, Status: commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
		TriggeredBy: func() *int { v := int(principalID); return &v }(), ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &authorizationVersion,
	}
	if err := commonExecution.NewTaskExecutionRepository(db).Create(context.Background(), parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}

	execution, err := NewScanExecutionService(db, nil, nil, nil).CreateTaskRunWithContext(
		context.Background(), task, 0, commonExecution.TriggerTypeManual,
		commonExecution.ModuleOrchestrator, &parent.ExecutionID,
	)
	if err != nil {
		t.Fatalf("create child execution: %v", err)
	}
	if execution.ActorPrincipalID == nil || *execution.ActorPrincipalID != principalID ||
		execution.ActorTenantMembershipID == nil || *execution.ActorTenantMembershipID != membershipID ||
		execution.IssuedAuthorizationVersion == nil || *execution.IssuedAuthorizationVersion != authorizationVersion {
		t.Fatalf("child actor facts were not inherited: %#v", execution)
	}
}

func TestScanCompletionRetainsFinalProgressSnapshot(t *testing.T) {
	for _, status := range []string{commonExecution.ExecutionStatusSuccess, commonExecution.ExecutionStatusFailed} {
		t.Run(status, func(t *testing.T) {
			db := openObjectCatalogScanTestDB(t)
			createTaskExecutionTable(t, db)
			createScanTaskTable(t, db)
			now := time.Now().UTC()
			execution := scantask.NewManualExecution(7, 1, 9, 0, "nfs", nil, []string{"3d"}, nil, "deep", true, "manager", now)
			execution.Metadata = models.JSONMap{"schema_change_source": map[string]interface{}{"execution_id": "blocked", "request_id": float64(61)}}
			if err := db.Create(execution).Error; err != nil {
				t.Fatal(err)
			}
			svc := NewScanExecutionService(db, nil, nil, nil)
			claimed, lease, err := svc.ClaimNextBoundedExecution(context.Background(), "scan-progress-worker", now, time.Minute)
			if err != nil || lease == nil {
				t.Fatalf("claim: %v", err)
			}
			svc.BindBoundedLease(execution.ExecutionID, *lease)
			defer svc.UnbindBoundedLease(execution.ExecutionID)
			reporter := scantask.NewExecProgressReporter(svc, execution.ExecutionID, 7)
			reporter.Report(models.ScanProgress{Phase: "finalizing", FilesDiscovered: 20, FilesTotalKnown: true,
				ItemsPlanned: 2, ItemsTotalKnown: true, ItemsCompleted: 2, ItemsSucceeded: 1, ItemsFailed: 1, LastProgressAt: now})
			response := &models.ScanResponse{ItemsScanned: 1}
			if status == commonExecution.ExecutionStatusSuccess {
				err = svc.completeExecutionWithSuccess(context.Background(), claimed, response, "nfs", now, 100)
			} else {
				err = svc.completeExecutionWithFailure(context.Background(), claimed, response, "nfs", errors.New("invalid source"), now, 100)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := svc.GetExecution(context.Background(), execution.ExecutionID, 7)
			if err != nil {
				t.Fatal(err)
			}
			if link, ok := got.Metadata["schema_change_source"].(map[string]interface{}); !ok || link["execution_id"] != "blocked" || link["request_id"] != float64(61) {
				t.Fatal("worker overwrote applied approval association")
			}
			p := models.ObserveScanProgress(got.Metadata)
			if p == nil || p.ItemsCompleted != 2 || p.FilesDiscovered != 20 || got.Metadata["items_scanned"] == nil {
				t.Fatalf("completion lost progress or summary: %+v", got.Metadata)
			}
		})
	}
}
