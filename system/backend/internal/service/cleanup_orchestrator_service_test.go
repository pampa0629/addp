package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
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

func TestCleanupExecutionErrorDetails(t *testing.T) {
	t.Parallel()

	failed := cleanupExecutionErrorDetails("completed_with_errors", events.CleanupResultSummary{ErrorCount: 3})
	if failed["message"] != "cleanup completed with errors" || failed["error_count"] != 3 {
		t.Fatalf("failed error details = %#v", failed)
	}
	timedOut := cleanupExecutionErrorDetails("timeout", events.CleanupResultSummary{ErrorCount: 1})
	if timedOut["message"] != "cleanup timed out" || timedOut["cleanup_status"] != "timeout" {
		t.Fatalf("timeout error details = %#v", timedOut)
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
			name: "enabled resource reclaim executor",
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
			name: "disabled resource reclaim executor",
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
	if _, err := svc.CreateExecuteTask(context.Background(), "scan-1", "soft_delete", 99, CleanupExecuteConfirmation{Confirmed: true}); err == nil {
		t.Fatal("CreateExecuteTask should reject invalid cleanup mode")
	}
}

func TestValidateCleanupExecuteConfirmation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cleanupMode  string
		summary      events.CleanupResultSummary
		confirmation CleanupExecuteConfirmation
		wantErr      error
	}{
		{
			name:        "unconfirmed request is rejected",
			cleanupMode: events.CleanupModeLogical,
			confirmation: CleanupExecuteConfirmation{
				Confirmed: false,
			},
			wantErr: ErrCleanupExecuteConfirmRequired,
		},
		{
			name:        "physical cleanup requires token",
			cleanupMode: events.CleanupModePhysical,
			confirmation: CleanupExecuteConfirmation{
				Confirmed: true,
			},
			wantErr: ErrCleanupExecuteConfirmTokenRequired,
		},
		{
			name:        "high risk logical cleanup requires token",
			cleanupMode: events.CleanupModeLogical,
			summary: events.CleanupResultSummary{
				RiskLevel: "high",
			},
			confirmation: CleanupExecuteConfirmation{
				Confirmed: true,
			},
			wantErr: ErrCleanupExecuteConfirmTokenRequired,
		},
		{
			name:        "low risk logical cleanup accepts confirmation",
			cleanupMode: events.CleanupModeLogical,
			summary: events.CleanupResultSummary{
				RiskLevel: "low",
			},
			confirmation: CleanupExecuteConfirmation{
				Confirmed: true,
			},
		},
		{
			name:        "physical cleanup accepts token",
			cleanupMode: events.CleanupModePhysical,
			confirmation: CleanupExecuteConfirmation{
				Confirmed:         true,
				ConfirmationToken: "CONFIRM",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateCleanupExecuteConfirmation(tt.cleanupMode, tt.summary, tt.confirmation)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("validateCleanupExecuteConfirmation() error = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateCleanupExecuteConfirmation() error = %v, want %v", err, tt.wantErr)
			}
		})
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

func TestCleanupOverallStatusTreatsPartialSuccessAsError(t *testing.T) {
	t.Parallel()

	svc := &CleanupOrchestratorService{}
	task := &models.CleanupTask{}
	progress := &models.TaskProgress{
		Total:     2,
		Completed: 2,
		Modules: map[string]string{
			"meta":    events.CleanupResultSuccess,
			"manager": events.CleanupResultPartialSuccess,
		},
	}
	if got := svc.calculateOverallStatus(task, progress, false); got != "completed_with_errors" {
		t.Fatalf("overall status = %q, want completed_with_errors", got)
	}
}

func TestCleanupSummaryDoesNotDoubleCountReportedErrors(t *testing.T) {
	t.Parallel()

	results := map[string]interface{}{
		"manager": events.CleanupResultData{
			Status: events.CleanupResultPartialSuccess,
			Summary: events.CleanupResultSummary{
				ErrorCount: 2,
			},
			Errors: []string{"first", "second"},
		},
	}
	if got := summaryFromResults(results).ErrorCount; got != 2 {
		t.Fatalf("error count = %d, want 2", got)
	}
}

type cleanupAuditWriterStub struct {
	events []iam.AuditEvent
}

func (w *cleanupAuditWriterStub) Write(_ context.Context, event iam.AuditEvent) error {
	w.events = append(w.events, event)
	return nil
}

func TestCleanupAuditEventKeepsTaskIdentityForAuditLookup(t *testing.T) {
	t.Parallel()

	tenantID := uint(12)
	for _, action := range []string{
		"cleanup.execute.confirmed",
		"cleanup.execute.created",
		"cleanup.completed",
		"cleanup.failed",
	} {
		action := action
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			writer := &cleanupAuditWriterStub{}
			service := &CleanupOrchestratorService{auditWriter: writer, log: slog.Default()}
			service.writeAuditLog(context.Background(), 99, &tenantID, action, "cleanup-exec-1", map[string]interface{}{
				"confirmation_token": true,
			})
			if len(writer.events) != 1 {
				t.Fatalf("audit event count = %d, want 1", len(writer.events))
			}
			event := writer.events[0]
			if event.EventName != action || event.EntityType != "cleanup_task" || event.EntityID != "cleanup-exec-1" ||
				event.ModuleName != "system" || event.Metadata.TenantID == nil || *event.Metadata.TenantID != int64(tenantID) ||
				event.Details["confirmation_token"] != true {
				t.Fatalf("cleanup audit event = %#v", event)
			}
		})
	}
}
