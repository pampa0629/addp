package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/pdf"
	"github.com/addp/common/taskprovider"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateNewTaskConfigAcceptsRawCopy(t *testing.T) {
	err := validateNewTaskConfig(map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator":        "addp://engine/1/path/docs/a.pdf?type=object",
			"data_type":      "document",
			"representation": "encoded",
			"format":         string(format.FormatPDF),
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/2/path/backup?type=directory",
			"name":           "a.pdf",
			"representation": "encoded",
			"policy":         map[string]interface{}{"apply_mode": "replace"},
		},
	}, 1000)
	if err != nil {
		t.Fatalf("validateNewTaskConfig() error = %v", err)
	}
}

func TestValidateNewTaskConfigAcceptsEncodedRecordExport(t *testing.T) {
	err := validateNewTaskConfig(map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator": "addp://engine/11/path/Outdoor/Persons?type=collection", "data_type": "unknown", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/2/path/exports?type=directory", "name": "Persons.ejsonl",
			"data_type": "unknown", "representation": "encoded", "format": "mongodb_extended_jsonl",
			"policy": map[string]interface{}{"apply_mode": "replace"},
		},
	}, 1000)
	if err != nil {
		t.Fatalf("validateNewTaskConfig() error = %v", err)
	}
}

func TestValidateNewTaskConfigStillAcceptsTableTransfer(t *testing.T) {
	err := validateNewTaskConfig(map[string]interface{}{
		"runtime":    map[string]interface{}{"boundary": "bounded"},
		"load":       map[string]interface{}{"mode": "snapshot"},
		"batch_size": 100,
		"source": map[string]interface{}{
			"locator":        "addp://engine/1/path/public/roads?type=table",
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/2/path/exports?type=directory",
			"name":           "roads.csv",
			"data_type":      "table",
			"representation": "encoded",
			"format":         string(format.FormatCSV),
			"policy":         map[string]interface{}{"apply_mode": "replace"},
		},
	}, 1000)
	if err != nil {
		t.Fatalf("validateNewTaskConfig() error = %v", err)
	}
}

func TestValidateNewTaskConfigAcceptsQueryCSVExportWithIdentityProjection(t *testing.T) {
	err := validateNewTaskConfig(map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator": "addp://engine/1/path/public/orders?type=table", "data_type": "table", "representation": "native",
			"query": map[string]interface{}{"language": "sql", "statement": "SELECT id, total FROM public.orders"},
		},
		"target": map[string]interface{}{
			"parent_locator": "addp-infra://minio/manager/tenant_7/export/develop/run-1?type=prefix", "name": "orders.csv",
			"data_type": "table", "representation": "encoded", "format": string(format.FormatCSV),
			"policy": map[string]interface{}{"apply_mode": "replace"},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{
				map[string]interface{}{"source": "id", "target": "id", "target_type": "unknown", "nullable": true},
				map[string]interface{}{"source": "total", "target": "total", "target_type": "unknown", "nullable": true},
			},
		}},
	}, 1000)
	if err != nil {
		t.Fatalf("validateNewTaskConfig() error = %v", err)
	}
}

func TestCreateAdHocExecutionPersistsNoTransferTaskDefinition(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	executionRepository := commonExecution.NewTaskExecutionRepository(db)
	taskService := NewTaskService(db, nil, nil)
	taskService.SetExecutionService(NewExecutionService(db, executionRepository))

	result, err := taskService.CreateAdHocExecution(context.Background(), &models.CreateAdHocExecutionRequest{
		Name: "query export", BatchSize: 1000,
		Config: models.TableTransferTaskConfigDoc{
			Runtime: models.TransferRuntimeDoc{Boundary: commonExecution.ExecutionBoundaryBounded},
			Load:    models.TransferLoadDoc{Mode: "snapshot"},
			Source: models.TransferSourceEndpointDoc{
				Locator: "addp://engine/1/path/public/orders?type=table", DataType: "table", Representation: "native",
			},
			Target: models.TransferTargetEndpointDoc{
				ParentLocator: "addp://engine/2/path/exports?type=directory", Name: "orders.csv",
				DataType: "table", Representation: "encoded", Format: "csv",
				Policy: models.TransferTargetPolicyDoc{ApplyMode: "replace"},
			},
		},
	}, "asset", 7, 9)
	if err != nil {
		t.Fatalf("CreateAdHocExecution() error = %v", err)
	}
	var taskCount int64
	if err := db.Model(&models.TransferTask{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 {
		t.Fatalf("transfer task definition count = %d, want 0", taskCount)
	}
	execution, err := executionRepository.GetByExecutionID(context.Background(), result.ExecutionID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if execution.SourceTaskID != nil || execution.Source != "asset" || execution.MaxAttempts != 1 {
		t.Fatalf("ad-hoc execution = %#v", execution)
	}
	claimed, lease, err := taskService.taskRepo.ClaimNextBoundedExecution(context.Background(), "transfer-test-worker", time.Now(), time.Minute)
	if err != nil || claimed == nil || lease == nil || claimed.ExecutionID != result.ExecutionID {
		t.Fatalf("claim = %#v lease = %#v error = %v", claimed, lease, err)
	}
}

func TestRuntimeTargetTaskRequiresOrchestratorInputs(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskService := NewTaskService(db, nil, nil)
	task, err := taskService.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name: "managed-dim", TaskType: commonExecution.TaskTypeSync,
		Config: validRuntimeTargetTransferTaskConfig(),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Enabled || task.Schedule != "" || task.AutoScanMetadata {
		t.Fatalf("managed task owns scheduling or scanning: %#v", task)
	}
	if _, err := taskService.StartTask(context.Background(), task.ID, 7, 9); err == nil || !strings.Contains(err.Error(), "target_locator") {
		t.Fatalf("StartTask() error = %v, want runtime input rejection", err)
	}
	parentExecutionID := uuid.NewString()
	actorPrincipalID, actorMembershipID, authorizationVersion := int64(91), int64(92), int64(3)
	if err := db.Create(&commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: parentExecutionID, Module: commonExecution.ModuleOrchestrator,
		TaskType: commonExecution.TaskTypeOrchestration, Source: commonExecution.ModuleOrchestrator,
		Status: commonExecution.ExecutionStatusRunning, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		TriggerType:      commonExecution.TriggerTypeManual,
		ActorPrincipalID: &actorPrincipalID, ActorTenantMembershipID: &actorMembershipID,
		IssuedAuthorizationVersion: &authorizationVersion,
	}).Error; err != nil {
		t.Fatalf("create Orchestrator parent execution: %v", err)
	}
	execution, err := taskService.StartTaskWithExecutionParameters(
		context.Background(), task.ID, 7, 9, commonExecution.TriggerTypeManual,
		commonExecution.ModuleOrchestrator, &parentExecutionID,
		map[string]interface{}{"target_locator": "addp://engine/9/path/public/staging?type=table"},
	)
	if err != nil {
		t.Fatalf("StartTaskWithContext() error = %v", err)
	}
	if execution.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("managed execution = %#v", execution)
	}
	var persistedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&persistedExecution).Error; err != nil {
		t.Fatalf("query managed execution parent: %v", err)
	}
	if persistedExecution.ParentExecutionID == nil || *persistedExecution.ParentExecutionID != parentExecutionID {
		t.Fatalf("parent_execution_id = %v, want %s", persistedExecution.ParentExecutionID, parentExecutionID)
	}
}

func TestTransferTaskExecutionContractDeclaresStableOutputsForFixedTarget(t *testing.T) {
	contract := TransferTaskExecutionContract(validTableTransferTaskConfig())
	inputProperties, ok := contract.InputSchema["properties"].(map[string]interface{})
	if !ok || len(inputProperties) != 0 {
		t.Fatalf("fixed-target input properties = %#v, want closed empty object", contract.InputSchema["properties"])
	}
	if _, exists := contract.InputSchema["required"]; exists {
		t.Fatalf("fixed-target input schema unexpectedly requires parameters: %#v", contract.InputSchema)
	}
	assertTransferStableOutputContract(t, contract)
}

func TestTransferTaskExecutionContractRequiresTargetOnlyForRuntimeBinding(t *testing.T) {
	contract := TransferTaskExecutionContract(validRuntimeTargetTransferTaskConfig())
	inputProperties, ok := contract.InputSchema["properties"].(map[string]interface{})
	if !ok || inputProperties["target_locator"] == nil {
		t.Fatalf("runtime-target input properties = %#v, want target_locator", contract.InputSchema["properties"])
	}
	required, ok := contract.InputSchema["required"].([]interface{})
	if !ok || len(required) != 1 || required[0] != "target_locator" {
		t.Fatalf("runtime-target required = %#v, want target_locator", contract.InputSchema["required"])
	}
	assertTransferStableOutputContract(t, contract)
}

func assertTransferStableOutputContract(t *testing.T, contract taskprovider.ExecutionContract) {
	t.Helper()
	properties, ok := contract.OutputSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("output properties = %#v, want object", contract.OutputSchema["properties"])
	}
	for _, name := range []string{"execution_id", "target_locator", "row_count"} {
		if properties[name] == nil {
			t.Fatalf("output properties missing %s: %#v", name, properties)
		}
	}
	required, ok := contract.OutputSchema["required"].([]interface{})
	if !ok || len(required) != 3 {
		t.Fatalf("output required = %#v, want three stable outputs", contract.OutputSchema["required"])
	}
}

func TestRuntimeTargetTaskRejectsTaskOwnedScheduleAndScan(t *testing.T) {
	for name, request := range map[string]*models.CreateTaskRequest{
		"schedule": {
			Name: "scheduled-managed", TaskType: commonExecution.TaskTypeSync,
			Config: validRuntimeTargetTransferTaskConfig(), Schedule: "0 0 * * * *", Enabled: boolPtr(true),
		},
		"scan": {
			Name: "scanning-managed", TaskType: commonExecution.TaskTypeSync,
			Config: validRuntimeTargetTransferTaskConfig(), AutoScanMetadata: boolPtr(true),
		},
	} {
		t.Run(name, func(t *testing.T) {
			taskService := NewTaskService(newTransferTaskServiceTestDB(t), nil, nil)
			if _, err := taskService.CreateTask(context.Background(), request, 7, 9); !errors.Is(err, ErrInvalidTaskConfig) {
				t.Fatalf("CreateTask() error = %v, want invalid task config", err)
			}
		})
	}
}

func TestValidateNewTaskConfigAcceptsPostgreSQLCDCAfterDataPlaneIsAvailable(t *testing.T) {
	if err := validateNewTaskConfig(validPostgreSQLCDCTaskConfig(), 1000); err != nil {
		t.Fatalf("PostgreSQL CDC task config rejected: %v", err)
	}
}

func TestTaskServiceAcceptsMySQLCDCThroughDatabaseCDCParser(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil)
	taskSvc.SetEngineResolver(planner.StaticEngineResolver{
		12: {Type: "mysql", EngineID: 12},
		20: {Type: "postgresql", EngineID: 20},
	})
	config := validPostgreSQLCDCTaskConfig()
	config["source"].(map[string]interface{})["locator"] = "addp://engine/12/path/business/orders?type=table"
	task, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name: "mysql-cdc", TaskType: commonExecution.TaskTypeSync, Config: config,
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.ID == 0 || task.DesiredState != models.TaskDesiredStateStopped {
		t.Fatalf("created MySQL CDC task = %#v", task)
	}
}

func TestValidateNewTaskConfigRequiresExplicitRecordFailurePolicy(t *testing.T) {
	if err := validateNewTaskConfig(validContinuousTaskConfig(), 1000); err != nil {
		t.Fatalf("explicit block policy rejected: %v", err)
	}
	missing := validContinuousTaskConfig()
	delete(missing["runtime"].(map[string]interface{}), "record_failure")
	if err := validateNewTaskConfig(missing, 1000); err == nil {
		t.Fatal("missing record_failure policy was accepted")
	}
	deadLetter := validContinuousTaskConfig()
	deadLetter["runtime"].(map[string]interface{})["record_failure"] = map[string]interface{}{"mode": "dead_letter"}
	if err := validateNewTaskConfig(deadLetter, 1000); err != nil {
		t.Fatalf("explicit dead_letter policy rejected: %v", err)
	}
}

func TestReplayTaskCreatesIndependentExecutionWithoutChangingOwnerTask(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	lastExecutionID := "owner-continuous-execution"
	lastExecutionStatus := commonExecution.ExecutionStatusRunning
	task := &models.TransferTask{
		TenantID: 7, Name: "orders continuous", TaskType: commonExecution.TaskTypeSync,
		Config: validContinuousTaskConfig(), BatchSize: 1000, Status: models.TaskStatusRunning,
		DesiredState: models.TaskDesiredStateRunning, Progress: 42,
		LastExecutionID: &lastExecutionID, LastExecutionStatus: &lastExecutionStatus,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	engine := &fakeReplayTaskExecutionEngine{}
	executionService := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	taskService := NewTaskService(db, engine, nil)
	taskService.SetExecutionService(executionService)

	execution, err := taskService.ReplayTask(context.Background(), task.ID, task.TenantID, 9, models.ReplayTaskRequest{
		Ranges: []models.ReplayOffsetRangeRequest{{Partition: "0", StartOffset: 10, EndOffset: 20}},
		Target: models.ReplayTargetRequest{ParentLocator: "addp://engine/8/path/replay?type=schema&node_id=12", Name: "orders_replay"},
	})
	if err != nil {
		t.Fatalf("ReplayTask() error = %v", err)
	}
	if execution.Status != models.ExecutionStatusPending {
		t.Fatalf("execution=%#v", execution)
	}
	if engine.applyIdentity == "" || engine.applyIdentity == task.ApplyIdentity {
		t.Fatalf("replay apply identity = %q, owner = %q", engine.applyIdentity, task.ApplyIdentity)
	}

	var reloaded models.TransferTask
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != models.TaskStatusRunning || reloaded.DesiredState != models.TaskDesiredStateRunning ||
		reloaded.Progress != 42 || reloaded.LastExecutionID == nil || *reloaded.LastExecutionID != lastExecutionID ||
		reloaded.LastExecutionStatus == nil || *reloaded.LastExecutionStatus != lastExecutionStatus {
		t.Fatalf("owner task was changed by replay: %#v", reloaded)
	}
}

type fakeReplayTaskExecutionEngine struct {
	applyIdentity string
}

func (e *fakeReplayTaskExecutionEngine) ExecuteExecution(context.Context, uint) error { return nil }

func (e *fakeReplayTaskExecutionEngine) PrepareReplayExecution(_ context.Context, _ uint, taskConfig map[string]interface{}, request ReplayExecutionRequest, executionApplyIdentity string) (*ReplayExecutionPreparation, error) {
	e.applyIdentity = executionApplyIdentity
	return &ReplayExecutionPreparation{
		ExecutionConfig: models.JSONMap{
			"task_config": taskConfig,
			"replay": map[string]interface{}{
				"version": ReplayExecutionVersion, "ranges": request.Ranges,
				"target": request.Target, "apply_identity": executionApplyIdentity,
			},
		},
		Metadata: models.JSONMap{"replay": map[string]interface{}{"status": "pending"}},
	}, nil
}

func TestCreateTaskPersistsNextRunAtWhenEnabled(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil)
	task, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:     "scheduled",
		TaskType: commonExecution.TaskTypeSync,
		Config:   validTableTransferTaskConfig(),
		Schedule: "0 */5 * * * *",
		Enabled:  boolPtr(true),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if !task.Enabled || task.NextRunAt == nil {
		t.Fatalf("task enabled/next_run_at = %v/%v, want true/non-nil", task.Enabled, task.NextRunAt)
	}
	if _, err := uuid.Parse(task.ApplyIdentity); err != nil {
		t.Fatalf("task apply_identity = %q, want generated UUID", task.ApplyIdentity)
	}
}

func TestCreateTaskKeepsScheduledTaskDisabledWithoutNextRunAt(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil)

	task, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:     "scheduled-disabled",
		TaskType: commonExecution.TaskTypeSync,
		Config:   validTableTransferTaskConfig(),
		Schedule: "0 */5 * * * *",
		Enabled:  boolPtr(false),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Enabled || task.NextRunAt != nil {
		t.Fatalf("task enabled/next_run_at = %v/%v, want false/nil", task.Enabled, task.NextRunAt)
	}
}

func TestUpdateTaskClearsNextRunAtWhenScheduleRemoved(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil)

	task, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:     "scheduled",
		TaskType: commonExecution.TaskTypeSync,
		Config:   validTableTransferTaskConfig(),
		Schedule: "0 */5 * * * *",
		Enabled:  boolPtr(true),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	updated, err := taskSvc.UpdateTask(context.Background(), task.ID, 7, &models.UpdateTaskRequest{
		Schedule: strPtr(""),
	})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if updated.Enabled || updated.NextRunAt != nil {
		t.Fatalf("updated enabled/next_run_at = %v/%v, want false/nil", updated.Enabled, updated.NextRunAt)
	}
}

func TestCreateTaskRejectsMissingTaskType(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil)

	_, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:   "missing-task-type",
		Config: validTableTransferTaskConfig(),
	}, 7, 9)
	if err == nil {
		t.Fatal("CreateTask() error = nil, want unsupported task_type")
	}
	if !errors.Is(err, ErrUnsupportedTaskType) {
		t.Fatalf("CreateTask() error = %v, want ErrUnsupportedTaskType", err)
	}
}

func TestStartTaskRejectsPersistedNonSyncTaskType(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil)

	task := models.TransferTask{
		TenantID:  7,
		Name:      "legacy",
		TaskType:  "import",
		Config:    validTableTransferTaskConfig(),
		BatchSize: 100,
		Status:    models.TaskStatusIdle,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create legacy task: %v", err)
	}

	_, err := taskSvc.StartTask(context.Background(), task.ID, 7, 9)
	if err == nil {
		t.Fatal("StartTask() error = nil, want unsupported task_type")
	}
	if !errors.Is(err, ErrUnsupportedTaskType) {
		t.Fatalf("StartTask() error = %v, want ErrUnsupportedTaskType", err)
	}
}

func TestContinuousTaskLifecycleIsAtomicWithoutAsynqQueue(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil)
	taskSvc.SetExecutionService(NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db)))
	task, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name: "continuous", TaskType: commonExecution.TaskTypeSync, Config: validContinuousTaskConfig(),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.DesiredState != models.TaskDesiredStateStopped {
		t.Fatalf("initial desired_state = %q", task.DesiredState)
	}
	first, err := taskSvc.StartTask(context.Background(), task.ID, 7, 9)
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if first.Status != models.ExecutionStatusPending {
		t.Fatalf("start execution status = %q", first.Status)
	}
	if err := taskSvc.PauseTask(context.Background(), task.ID, 7); err != nil {
		t.Fatalf("PauseTask() error = %v", err)
	}
	var cancelled commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", first.ExecutionID).First(&cancelled).Error; err != nil {
		t.Fatalf("load cancelled execution: %v", err)
	}
	if cancelled.Status != commonExecution.ExecutionStatusCancelled || cancelled.Metadata["stop_reason"] != "paused" {
		t.Fatalf("cancelled execution = status=%q metadata=%#v", cancelled.Status, cancelled.Metadata)
	}
	second, err := taskSvc.ResumeTask(context.Background(), task.ID, 7, 9)
	if err != nil {
		t.Fatalf("ResumeTask() error = %v", err)
	}
	if second == nil || second.ExecutionID == first.ExecutionID {
		t.Fatalf("resume execution = %#v, first=%s", second, first.ExecutionID)
	}
	if err := taskSvc.StopTask(context.Background(), task.ID, 7, models.StopTaskRequest{}); err != nil {
		t.Fatalf("StopTask() error = %v", err)
	}
	stored, err := taskSvc.GetTask(context.Background(), task.ID, 7)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if stored.DesiredState != models.TaskDesiredStateStopped {
		t.Fatalf("final desired_state = %q", stored.DesiredState)
	}
}

func TestPostgreSQLCDCTaskStartsCaptureBeforeRuntimeAndResumesGeneration(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	control := &fakeCaptureControl{}
	taskSvc := NewTaskService(db, nil, nil)
	taskSvc.SetEngineResolver(planner.StaticEngineResolver{
		12: {Type: "postgresql", EngineID: 12},
		20: {Type: "postgresql", EngineID: 20},
	})
	taskSvc.SetCaptureControl(control)
	taskSvc.SetExecutionService(NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db)))
	task, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name: "cdc", TaskType: commonExecution.TaskTypeSync, Config: validPostgreSQLCDCTaskConfig(), BatchSize: 100,
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	first, err := taskSvc.StartTask(context.Background(), task.ID, 7, 9)
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if first == nil || control.startCalls != 1 || control.resumeCalls != 0 {
		t.Fatalf("first start execution=%#v capture=%+v", first, control)
	}
	if err := taskSvc.PauseTask(context.Background(), task.ID, 7); err != nil {
		t.Fatalf("PauseTask() error = %v", err)
	}
	second, err := taskSvc.ResumeTask(context.Background(), task.ID, 7, 9)
	if err != nil {
		t.Fatalf("ResumeTask() error = %v", err)
	}
	if second == nil || control.startCalls != 1 || control.resumeCalls != 1 {
		t.Fatalf("resume execution=%#v capture=%+v", second, control)
	}
}

func TestPostgreSQLCDCSchemaBlockedTaskCannotStartOrResume(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	control := &fakeCaptureControl{resource: &models.CaptureResource{Status: models.CaptureStatusRunning}}
	task := &models.TransferTask{
		TenantID: 7, Name: "blocked cdc", TaskType: commonExecution.TaskTypeSync,
		Config: validPostgreSQLCDCTaskConfig(), BatchSize: 100,
		Status: models.TaskStatusBlocked, DesiredState: models.TaskDesiredStateRunning,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	taskSvc := NewTaskService(db, nil, nil)
	taskSvc.SetCaptureControl(control)
	for name, start := range map[string]func() error{
		"start": func() error {
			_, err := taskSvc.StartTask(context.Background(), task.ID, 7, 9)
			return err
		},
		"resume": func() error {
			_, err := taskSvc.ResumeTask(context.Background(), task.ID, 7, 9)
			return err
		},
		"pause": func() error {
			return taskSvc.PauseTask(context.Background(), task.ID, 7)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := start(); !errors.Is(err, ErrCDCSchemaChangeBlocked) {
				t.Fatalf("error = %v, want ErrCDCSchemaChangeBlocked", err)
			}
		})
	}
	if control.startCalls != 0 || control.resumeCalls != 0 {
		t.Fatalf("blocked task touched capture control: start=%d resume=%d", control.startCalls, control.resumeCalls)
	}
}

func TestPostgreSQLCDCConfigIsImmutableAfterCaptureGenerationCreation(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	task := &models.TransferTask{
		TenantID: 7, Name: "cdc", TaskType: commonExecution.TaskTypeSync,
		Config: validPostgreSQLCDCTaskConfig(), BatchSize: 100, Status: models.TaskStatusIdle,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	control := &fakeCaptureControl{resource: &models.CaptureResource{TaskID: task.ID, TenantID: 7, Status: models.CaptureStatusRunning}}
	taskSvc := NewTaskService(db, nil, nil)
	taskSvc.SetCaptureControl(control)
	config := validPostgreSQLCDCTaskConfig()
	config["target"].(map[string]interface{})["name"] = "other_target"
	if _, err := taskSvc.UpdateTask(context.Background(), task.ID, 7, &models.UpdateTaskRequest{Config: config}); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("UpdateTask() error = %v, want immutable CDC config", err)
	}
}

func TestListTasksCanRestrictProviderDiscoveryToBounded(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil)
	for _, req := range []*models.CreateTaskRequest{
		{Name: "bounded", TaskType: commonExecution.TaskTypeSync, Config: validTableTransferTaskConfig()},
		{Name: "continuous", TaskType: commonExecution.TaskTypeSync, Config: validContinuousTaskConfig()},
	} {
		if _, err := taskSvc.CreateTask(context.Background(), req, 7, 9); err != nil {
			t.Fatalf("CreateTask(%s) error = %v", req.Name, err)
		}
	}
	tasks, total, err := taskSvc.ListTasks(context.Background(), 7, &models.ListTasksRequest{
		RuntimeBoundary: "bounded", Page: 1, PageSize: 100,
	})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if total != 1 || len(tasks) != 1 || tasks[0].Name != "bounded" {
		t.Fatalf("bounded tasks total=%d items=%#v", total, tasks)
	}
}

func TestStopPostgreSQLCDCRequiresExactTaskNameAndCleansCapture(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	task := &models.TransferTask{
		TenantID: 7, Name: "订单数据库变更同步", TaskType: commonExecution.TaskTypeSync,
		Config: validPostgreSQLCDCTaskConfig(), Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStatePaused,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	control := &fakeCaptureControl{}
	taskSvc := NewTaskService(db, nil, nil)
	taskSvc.SetCaptureControl(control)
	if err := taskSvc.StopTask(context.Background(), task.ID, 7, models.StopTaskRequest{Confirmed: true, ConfirmationText: "错误名称"}); !errors.Is(err, ErrCDCStopConfirmationRequired) {
		t.Fatalf("StopTask() error = %v", err)
	}
	if control.stopCalls != 0 {
		t.Fatalf("capture cleanup called before confirmation: %d", control.stopCalls)
	}
	if err := taskSvc.StopTask(context.Background(), task.ID, 7, models.StopTaskRequest{Confirmed: true, ConfirmationText: task.Name}); err != nil {
		t.Fatal(err)
	}
	if control.stopCalls != 1 {
		t.Fatalf("capture cleanup calls = %d", control.stopCalls)
	}
}

func newTransferTaskServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
		t.Fatalf("attach transfer schema: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE transfer.transfer_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			apply_identity TEXT NOT NULL UNIQUE,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			task_type TEXT NOT NULL,
			config JSON,
			schedule TEXT,
			batch_size INTEGER,
			enabled BOOLEAN,
			auto_scan_metadata BOOLEAN,
			initial_metadata_scan_status TEXT NOT NULL DEFAULT '',
			initial_metadata_scan_claim_token TEXT NOT NULL DEFAULT '',
			initial_metadata_scan_lease_until DATETIME,
			initial_metadata_scan_attempt INTEGER NOT NULL DEFAULT 0,
			initial_metadata_scan_execution_id TEXT NOT NULL DEFAULT '',
			initial_metadata_scan_error TEXT NOT NULL DEFAULT '',
			status TEXT,
			desired_state TEXT NOT NULL DEFAULT 'stopped',
			progress REAL,
			created_by INTEGER,
			last_execution_id TEXT,
			last_execution_status TEXT,
			last_run_at DATETIME,
			next_run_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create transfer_tasks table: %v", err)
	}
	if err := db.Exec(`
			CREATE TABLE transfer.schema_change_requests (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id INTEGER NOT NULL,
				tenant_id INTEGER NOT NULL,
				status TEXT NOT NULL,
				detected_at DATETIME NOT NULL
			)
	`).Error; err != nil {
		t.Fatalf("create schema_change_requests table: %v", err)
	}
	if err := db.Exec(`
			CREATE TABLE transfer.runtime_leases (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id INTEGER NOT NULL,
				execution_id TEXT NOT NULL,
				owner_instance_id TEXT NOT NULL DEFAULT '',
				lease_until DATETIME NOT NULL
			)
	`).Error; err != nil {
		t.Fatalf("create runtime_leases table: %v", err)
	}
	return db
}

func boolPtr(v bool) *bool { return &v }

func strPtr(v string) *string { return &v }

func validTableTransferTaskConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator":        "addp://engine/1/path/public/roads?type=table",
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/2/path/exports?type=directory",
			"name":           "roads.csv",
			"data_type":      "table",
			"representation": "encoded",
			"format":         string(format.FormatCSV),
			"policy":         map[string]interface{}{"apply_mode": "replace"},
		},
	}
}

func validRuntimeTargetTransferTaskConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator": "addp://engine/1/path/outdoor/entries?type=table", "data_type": "table", "representation": "native",
			"query": map[string]interface{}{"language": "mql", "statement": `{"aggregate":"entries","pipeline":[{"$project":{"person_id":"$person.id"}}]}`},
		},
		"target": map[string]interface{}{
			"binding": "runtime", "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "append"},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{map[string]interface{}{
				"source": "person_id", "target": "person_id", "target_type": "string", "nullable": false,
			}},
		}},
	}
}

func validContinuousTaskConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load":    map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{"type": "kafka"}},
		"source": map[string]interface{}{
			"locator": "addp://engine/30/path/orders.events?type=topic", "representation": "native",
			"change_stream": map[string]interface{}{
				"envelope": "record", "encoding": "json", "key": map[string]interface{}{"source": "value", "fields": []interface{}{"id"}},
				"start": map[string]interface{}{"mode": "committed", "initial": "earliest"}, "poll_batch_size": 1000,
			},
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/8/path/public?type=schema", "name": "orders", "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{map[string]interface{}{
				"source": "id", "target": "id", "target_type": "int", "nullable": false,
			}},
		}},
	}
}

func validPostgreSQLCDCTaskConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load": map[string]interface{}{
			"mode": "incremental", "change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"},
		},
		"source": map[string]interface{}{
			"locator": "addp://engine/12/path/public/orders?type=table", "data_type": "table", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/20/path/public?type=schema", "name": "orders_cdc", "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{map[string]interface{}{"source": "id", "target": "id", "target_type": "bigint", "nullable": false}},
		}},
	}
}

type fakeCaptureControl struct {
	startCalls  int
	resumeCalls int
	pauseCalls  int
	stopCalls   int
	terminal    bool
	resource    *models.CaptureResource
}

func (f *fakeCaptureControl) Start(_ context.Context, task *models.TransferTask) (*models.CaptureResource, error) {
	f.startCalls++
	f.resource = &models.CaptureResource{TaskID: task.ID, TenantID: task.TenantID, Status: models.CaptureStatusRunning}
	return f.resource, nil
}
func (f *fakeCaptureControl) Pause(context.Context, *models.TransferTask) error {
	f.pauseCalls++
	return nil
}
func (f *fakeCaptureControl) Resume(context.Context, *models.TransferTask) error {
	f.resumeCalls++
	return nil
}
func (f *fakeCaptureControl) Stop(context.Context, *models.TransferTask) error {
	f.stopCalls++
	return nil
}
func (f *fakeCaptureControl) Get(context.Context, uint, uint) (*models.CaptureResource, error) {
	if f.resource == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return f.resource, nil
}
func (f *fakeCaptureControl) HasStopInitiatedGeneration(context.Context, uint, uint) (bool, error) {
	return f.terminal, nil
}
