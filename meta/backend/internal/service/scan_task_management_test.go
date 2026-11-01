package service

import (
	"context"
	"errors"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestTaskManagementChecksBothOldAndNewTargetAndRollsBack(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)
	svc := NewScanTaskService(db)
	ctx := context.Background()
	req := &models.ScanTaskUpsertRequest{Name: "central", EngineID: 9, ScanDepth: "basic", Scope: models.JSONMap{"type": "engine"}}
	if _, err := svc.CreateTask(ctx, 7, 33, req); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("missing current authorization accepted", err)
	}
	task, err := svc.CreateTask(allowScanManagement(ctx), 7, 33, req)
	if err != nil {
		t.Fatal(err)
	}
	updated := *req
	updated.EngineID = 10
	var checked []uint
	denied := WithScanManagementCheck(ctx, func(_ context.Context, task *models.ScanTask, op string) error {
		checked = append(checked, task.EngineID)
		if task.EngineID == 10 {
			return commonapi.ErrForbidden
		}
		return nil
	})
	if _, err := svc.UpdateTask(denied, 7, task.ID, 33, &updated); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal(err)
	}
	stored, err := svc.GetTask(7, task.ID)
	if err != nil || stored.EngineID != 9 || !reflect.DeepEqual(checked, []uint{9, 10}) {
		t.Fatal("target not checked or changed after denial", stored, checked, err)
	}
	if err := svc.DeleteTask(ctx, 7, task.ID); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("missing delete scope accepted", err)
	}
	if _, err := svc.GetTask(7, task.ID); err != nil {
		t.Fatal("denied deletion changed task", err)
	}
}

// A tenant schedule never borrows the creator's current User credential. A
// missing User management callback still rejects that person's manual action.
func TestTenantScheduleDoesNotBorrowCreatorAuthority(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)
	createTaskExecutionTable(t, db)
	tasks := NewScanTaskService(db)
	now := time.Now()
	past := now.Add(-time.Minute)
	creator := uint(33)
	task := models.ScanTask{TenantID: 7, EngineID: 9, CreatedBy: creator, Name: "tenant plan", Enabled: true, Schedule: "* * * * *", NextRunAt: &past, Scope: models.JSONMap{"type": "engine"}, Parameters: models.JSONMap{"scan_depth": "basic"}}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	runner := NewScanExecutionService(db, nil, leaseTestEngine("postgresql"), nil)
	scheduler := NewScanTaskScheduler(tasks, runner)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := scheduler.claimDueScheduledTask(ctx, task.ID, now)
	if err != nil || id == "" {
		t.Fatal("tenant scheduler needed human authority", err)
	}
	var run execution.TaskExecution
	if err := db.Where("execution_id=?", id).Take(&run).Error; err != nil {
		t.Fatal(err)
	}
	if run.TriggeredBy != nil || run.ActorPrincipalID != nil || run.Source != "meta" || run.TriggerType != "scheduled" || run.ParentExecutionID != nil {
		t.Fatal("scheduler impersonated creator", run)
	}
	if err := tasks.DeleteTask(context.Background(), 7, task.ID); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("lost human authority still managed plan", err)
	}
	if err := db.Model(&task).Updates(map[string]any{"enabled": false, "next_run_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	id, err = scheduler.claimDueScheduledTask(context.Background(), task.ID, now.Add(time.Hour))
	if err != nil || id != "" {
		t.Fatal("disabled schedule dispatched", id, err)
	}
}

func TestScheduledScopeLookupFailureDoesNotDispatchOrAdvance(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)
	createTaskExecutionTable(t, db)
	now := time.Now()
	past := now.Add(-time.Minute)
	task := models.ScanTask{TenantID: 7, EngineID: 9, CreatedBy: 33, Name: "tenant plan", Enabled: true, Schedule: "* * * * *", NextRunAt: &past, Scope: models.JSONMap{"type": "engine"}, Parameters: models.JSONMap{"scan_depth": "basic"}}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	lookupFailure := errors.New("independent scopes unavailable")
	queries := 0
	const callback = "test:scheduled_scope_lookup_failure"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "ScanTask" {
			queries++
			if queries == 2 {
				tx.AddError(lookupFailure)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(callback) })
	scheduler := NewScanTaskScheduler(NewScanTaskService(db), NewScanExecutionService(db, nil, leaseTestEngine("postgresql"), nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := scheduler.claimDueScheduledTask(ctx, task.ID, now)
	if !errors.Is(err, lookupFailure) || id != "" || queries != 2 {
		t.Fatalf("scope query failure dispatched or was hidden: id=%q queries=%d err=%v", id, queries, err)
	}
	var stored models.ScanTask
	if err := db.First(&stored, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.Enabled || stored.NextRunAt == nil || !stored.NextRunAt.Equal(past) || stored.LastExecutionID != nil {
		t.Fatalf("failed scope lookup changed schedule: %+v", stored)
	}
	var count int64
	if err := db.Model(&execution.TaskExecution{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed scope lookup created executions: count=%d err=%v", count, err)
	}
}

func TestDisablePGEngineSchedulePreservesPlanAndInFlightExecution(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)
	createTaskExecutionTable(t, db)
	exerciseDisablePGEngineSchedule(t, db)
}

func exerciseDisablePGEngineSchedule(t *testing.T, db *gorm.DB) {
	t.Helper()
	tasks := NewScanTaskService(db)
	now := time.Now()
	past := now.Add(-time.Minute)
	task := scantask.NewEngineScanTask(7, 33, 9, "PG", "* * * * *", "basic", now, &past)
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	scheduler := NewScanTaskScheduler(tasks, NewScanExecutionService(db, nil, leaseTestEngine("postgresql"), nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := scheduler.claimDueScheduledTask(ctx, task.ID, now)
	if err != nil || id == "" {
		t.Fatal("create in-flight execution", id, err)
	}
	before, err := tasks.GetTask(7, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.UpsertEngineScanTaskFromPolicy(ctx, 7, 33, 9, "postgresql", "PG", nil); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("closing a schedule accepted without current authority", err)
	}
	stored, err := tasks.GetTask(7, task.ID)
	if err != nil || !stored.Enabled {
		t.Fatal("denied disable changed plan", err)
	}
	disabled, err := tasks.UpsertEngineScanTaskFromPolicy(allowScanManagement(ctx), 7, 33, 9, "postgresql", "ignored on disable", nil)
	if err != nil || disabled == nil || disabled.ID != before.ID || disabled.Enabled || disabled.NextRunAt != nil {
		t.Fatal("did not preserve disabled plan", disabled, err)
	}
	if disabled.Name != before.Name || disabled.Schedule != before.Schedule || !reflect.DeepEqual(disabled.Scope, before.Scope) || !reflect.DeepEqual(disabled.Parameters, before.Parameters) || !reflect.DeepEqual(disabled.LastExecutionID, before.LastExecutionID) || !reflect.DeepEqual(disabled.LastRunAt, before.LastRunAt) {
		t.Fatal("disable replaced plan configuration or history", before, disabled)
	}
	var run execution.TaskExecution
	if err := db.Where("execution_id=?", id).Take(&run).Error; err != nil || run.Status != execution.ExecutionStatusPending {
		t.Fatal("disable cancelled or deleted in-flight execution", run.Status, err)
	}
	if next, err := scheduler.claimDueScheduledTask(ctx, task.ID, now.Add(time.Hour)); err != nil || next != "" {
		t.Fatal("disabled plan dispatched", next, err)
	}
	absent, err := tasks.UpsertEngineScanTaskFromPolicy(allowScanManagement(ctx), 7, 33, 10, "postgresql", "no plan", nil)
	if err != nil || absent != nil {
		t.Fatal("closing absent schedule created a plan", absent, err)
	}
	var count int64
	if err := db.Model(&models.ScanTask{}).Where("tenant_id=? AND engine_id IN ?", 7, []uint{9, 10}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("unexpected plan mutation", count, err)
	}
}
