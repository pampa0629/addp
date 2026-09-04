package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/google/uuid"
)

type allowTransferSourceProtectionGate struct{}

func (allowTransferSourceProtectionGate) RequireSourceConfig(context.Context, uint, map[string]interface{}) error {
	return nil
}

func (allowTransferSourceProtectionGate) PrepareBoundedTableProtection(context.Context, uint, map[string]interface{}) (executor.TableSourceProtector, error) {
	return nil, nil
}

func (allowTransferSourceProtectionGate) PrepareBoundedEncodedRecordProtection(context.Context, uint, map[string]interface{}, []datatype.FieldInfo) (engineplugin.EncodedRecordTransform, error) {
	return nil, nil
}

func TestExecutionEngineRunsReplayWithoutUpdatingOwnerTask(t *testing.T) {
	db := newExecutionServiceTestDB(t)
	lastExecutionID := "main-continuous-execution"
	lastExecutionStatus := commonExecution.ExecutionStatusRunning
	task := models.TransferTask{
		TenantID: 7, Name: "orders continuous", TaskType: commonExecution.TaskTypeSync,
		Config: validContinuousTaskConfig(), BatchSize: 1000, Status: models.TaskStatusRunning,
		DesiredState: models.TaskDesiredStateRunning, Progress: 37,
		LastExecutionID: &lastExecutionID, LastExecutionStatus: &lastExecutionStatus,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	applyIdentity := uuid.NewString()
	executionConfig, err := replayStructMap(replayExecutionConfig{
		TaskConfig: task.Config,
		Replay: replayExecutionSpec{
			Version: ReplayExecutionVersion,
			Ranges:  []planner.ReplayOffsetRange{{Partition: "0", StartOffset: 10, EndOffset: 12}},
			Target: planner.ReplayTargetSpec{
				ParentLocator: "addp://engine/8/path/replay?type=schema&node_id=12", Name: "orders_replay",
			},
			ApplyIdentity: applyIdentity,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskName := task.Name
	execution := commonExecution.TaskExecution{
		TenantID: int(task.TenantID), ExecutionID: uuid.NewString(), Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleTransfer,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), SourceTaskName: &taskName,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		ExecutionConfig:   executionConfig,
		Metadata: commonModels.JSONMap{"replay": map[string]interface{}{
			"version": ReplayExecutionVersion, "status": "pending", "positions": map[string]interface{}{"0": float64(10)},
		}},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}

	server := replaySystemEngineServer(t)
	defer server.Close()
	executionService := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	claimed, lease, err := repository.NewTaskRepository(db).ClaimNextBoundedExecution(context.Background(), "replay-test-worker", time.Now(), time.Minute)
	if err != nil || claimed == nil || lease == nil {
		t.Fatalf("claim replay execution = %#v %#v, %v", claimed, lease, err)
	}
	executionService.BindBoundedLease(uint(execution.ID), *lease)
	runtime := &fakeBoundedReplayRuntime{}
	engineService := NewExecutionEngineService(
		repository.NewTaskRepository(db), nil, executionService,
		commonClient.NewSystemClient(server.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
			return "addp_at_transfer_service_token", nil
		})), nil, nil,
	)
	engineService.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	engineService.SetReplayRuntime(runtime)
	engineService.SetProtectionGate(allowTransferSourceProtectionGate{})

	execCtx := commonExecution.ContextWithLease(context.Background(), *lease)
	if err := engineService.ExecuteExecution(execCtx, uint(execution.ID)); err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	if runtime.applyIdentity != applyIdentity || runtime.ranges[0].StartOffset != 10 {
		t.Fatalf("runtime apply_identity=%q ranges=%#v", runtime.applyIdentity, runtime.ranges)
	}
	var reloadedTask models.TransferTask
	if err := db.First(&reloadedTask, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloadedTask.Status != models.TaskStatusRunning || reloadedTask.DesiredState != models.TaskDesiredStateRunning ||
		reloadedTask.Progress != 37 || reloadedTask.LastExecutionID == nil || *reloadedTask.LastExecutionID != lastExecutionID ||
		reloadedTask.LastExecutionStatus == nil || *reloadedTask.LastExecutionStatus != lastExecutionStatus {
		t.Fatalf("owner task changed after replay: %#v", reloadedTask)
	}
	var reloadedExecution commonExecution.TaskExecution
	if err := db.First(&reloadedExecution, execution.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloadedExecution.Status != commonExecution.ExecutionStatusSuccess || reloadedExecution.RecordsRead == nil || *reloadedExecution.RecordsRead != 2 ||
		reloadedExecution.RecordsWritten == nil || *reloadedExecution.RecordsWritten != 2 {
		t.Fatalf("replay execution = %#v", reloadedExecution)
	}
}

type fakeBoundedReplayRuntime struct {
	applyIdentity string
	ranges        []planner.ReplayOffsetRange
}

func (*fakeBoundedReplayRuntime) Prepare(context.Context, *planner.ContinuousPlan, []planner.ReplayOffsetRange, string) ([]planner.ReplayRetentionSnapshot, error) {
	return nil, nil
}

func (r *fakeBoundedReplayRuntime) Run(_ context.Context, _ *planner.ContinuousPlan, ranges []planner.ReplayOffsetRange, executionApplyIdentity string, recordProgress func(context.Context, planner.ReplayProgress) error) (*planner.ReplayResult, error) {
	r.applyIdentity = executionApplyIdentity
	r.ranges = append([]planner.ReplayOffsetRange(nil), ranges...)
	progress := planner.ReplayProgress{Positions: map[string]int64{"0": 12}, RecordsRead: 2, RecordsWritten: 2}
	if recordProgress != nil {
		if err := recordProgress(context.Background(), progress); err != nil {
			return nil, err
		}
	}
	return &planner.ReplayResult{Positions: progress.Positions, RecordsRead: 2, RecordsWritten: 2}, nil
}

func replaySystemEngineServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		engineID := uint(0)
		engineType := ""
		switch parts[len(parts)-1] {
		case "30":
			engineID, engineType = 30, "kafka"
		case "8":
			engineID, engineType = 8, "postgresql"
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonModels.Engine{
			ID: engineID, EngineType: engineType, LifecycleState: "active", ConnectionStatus: commonModels.EngineConnectionOnline,
		})
	}))
}
