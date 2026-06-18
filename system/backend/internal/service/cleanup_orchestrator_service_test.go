package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/system/internal/models"
)

func TestCreateEventScanTaskBuildsEventContext(t *testing.T) {
	t.Parallel()

	svc := &CleanupOrchestratorService{}
	_, err := svc.createParentExecution(
		context.Background(),
		12,
		"task-1",
		events.CleanupActionScan,
		"",
		commonExecution.TriggerTypeEvent,
		events.CleanupCauseEngineDeleted,
		"",
		[]string{events.ModuleMeta, events.ModuleManager},
		map[string]interface{}{"engine_id": 7},
		99,
		30,
	)
	if err == nil {
		t.Fatal("expected createParentExecution to fail without repo")
	}

	taskID, err := svc.CreateEventScanTask(
		context.Background(),
		12,
		[]string{events.ModuleMeta, events.ModuleManager},
		99,
		events.CleanupCauseEngineDeleted,
		map[string]interface{}{"engine_id": 7},
	)
	if err == nil {
		t.Fatalf("CreateEventScanTask() task_id=%q, expected repo missing error", taskID)
	}
}

func TestCleanupExecutorEnabledReadsModuleRegistryCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     bool
	}{
		{
			name: "enabled cleanup executor",
			metadata: map[string]interface{}{
				"module": "meta",
				"capabilities": map[string]interface{}{
					"cleanup_executor": map[string]interface{}{
						"enabled": true,
					},
				},
			},
			want: true,
		},
		{
			name: "disabled cleanup executor",
			metadata: map[string]interface{}{
				"capabilities": map[string]interface{}{
					"cleanup_executor": map[string]interface{}{
						"enabled": false,
					},
				},
			},
			want: false,
		},
		{
			name: "missing capability",
			metadata: map[string]interface{}{
				"module": "transfer",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cleanupExecutorEnabled(tt.metadata); got != tt.want {
				t.Fatalf("cleanupExecutorEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateExecutableScanTask(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		task    *models.TaskStatusResponse
		wantErr bool
	}{
		{
			name: "completed scan",
			task: &models.TaskStatusResponse{
				Status: "completed",
				Task: models.CleanupTask{
					Action: events.CleanupActionScan,
				},
			},
			wantErr: false,
		},
		{
			name: "execute task is rejected",
			task: &models.TaskStatusResponse{
				Status: "completed",
				Task: models.CleanupTask{
					Action: events.CleanupActionExecute,
				},
			},
			wantErr: true,
		},
		{
			name: "pending scan is rejected",
			task: &models.TaskStatusResponse{
				Status: "pending",
				Task: models.CleanupTask{
					Action: events.CleanupActionScan,
				},
			},
			wantErr: true,
		},
		{
			name:    "nil task is rejected",
			task:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateExecutableScanTask(tt.task)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExecutableScanTask() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCreateExecuteTaskRejectsInvalidCleanupModeBeforeLoadingScan(t *testing.T) {
	t.Parallel()

	svc := &CleanupOrchestratorService{}
	if _, err := svc.CreateExecuteTask(context.Background(), "scan-1", "soft_delete", 99); err == nil {
		t.Fatal("CreateExecuteTask should reject invalid cleanup mode")
	}
}

func TestBuildCleanupScanTaskKeepsLifecycleContext(t *testing.T) {
	t.Parallel()

	task := buildCleanupScanTask(
		"scan-1",
		12,
		[]string{events.ModuleMeta, events.ModuleManager},
		99,
		commonExecution.TriggerTypeEvent,
		events.CleanupCauseEngineDeleted,
		map[string]interface{}{"engine_id": 7},
		"execution-1",
		"2026-06-17T10:00:00Z",
		"2026-06-17T10:00:30Z",
	)

	if task.CauseEvent != events.CleanupCauseEngineDeleted {
		t.Fatalf("CauseEvent = %q, want %s", task.CauseEvent, events.CleanupCauseEngineDeleted)
	}
	if task.Context["engine_id"].(int) != 7 {
		t.Fatalf("Context = %#v", task.Context)
	}
	if task.TriggerType != commonExecution.TriggerTypeEvent {
		t.Fatalf("TriggerType = %q, want event", task.TriggerType)
	}
}

func TestCleanupLifecycleContexts(t *testing.T) {
	t.Parallel()

	if ctx := cleanupEngineContext(7); ctx["engine_id"].(uint) != 7 {
		t.Fatalf("engine context = %#v", ctx)
	}
	if ctx := cleanupTenantContext(12); ctx["tenant_id"].(uint) != 12 {
		t.Fatalf("tenant context = %#v", ctx)
	}
}

func TestCleanupWatchDeadlineIncludesGraceWindow(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	got := cleanupWatchDeadline(startedAt, 30*time.Second)
	want := startedAt.Add(32 * time.Second)
	if !got.Equal(want) {
		t.Fatalf("deadline = %s, want %s", got, want)
	}
}

func TestCleanupTaskTerminalStatuses(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"completed", "completed_with_errors", "timeout", "failed"} {
		if !isTaskTerminal(status) {
			t.Fatalf("status %q should be terminal", status)
		}
	}
	for _, status := range []string{"pending", "running"} {
		if isTaskTerminal(status) {
			t.Fatalf("status %q should not be terminal", status)
		}
	}
}
