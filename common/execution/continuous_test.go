package execution_test

import (
	"errors"
	commonapi "github.com/addp/common/api"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/models"
	"testing"
	"time"
)

func TestContinuousLifecycleRequiresExplicitOwnerAdmission(t *testing.T) {
	db := newLeaseTestDB(t)
	task := "17"
	item := execution.TaskExecution{ExecutionID: "continuous-controlled", TenantID: 7, Module: execution.ModuleTransfer, TaskType: execution.TaskTypeSync, Source: execution.ModuleTransfer, SourceTaskID: &task, ExecutionBoundary: execution.ExecutionBoundaryContinuous, Status: execution.ExecutionStatusPending, TriggerType: execution.TriggerTypeManual, Metadata: models.JSONMap{execution.ObservabilityKey: models.JSONMap{"prior": "kept"}}}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := execution.StartContinuous(t.Context(), db, item.ExecutionID, nil, nil); err == nil {
		t.Fatal("missing owner admission accepted")
	}
	admitted := true
	guard := func(current execution.TaskExecution, now time.Time) error {
		if !admitted || current.TenantID != item.TenantID || current.ExecutionID != item.ExecutionID || now.IsZero() {
			return commonapi.ErrConflict
		}
		return nil
	}
	running, err := execution.StartContinuous(t.Context(), db, item.ExecutionID, nil, guard)
	if err != nil {
		t.Fatal(err)
	}
	if running.Attempt != 1 || running.StartedAt == nil || running.LeaseToken != nil || running.LeaseOwner != nil || running.LeaseExpiresAt != nil {
		t.Fatal("continuous start acquired bounded ownership or omitted start")
	}
	if err := execution.UpdateContinuous(t.Context(), db, item.ExecutionID, map[string]interface{}{"started_at": time.Now()}, guard); err == nil {
		t.Fatal("continuous update overwrote start")
	}
	if err := execution.UpdateContinuous(t.Context(), db, item.ExecutionID, map[string]interface{}{"metadata": models.JSONMap{execution.SchemaChangeScanOriginKey: map[string]interface{}{"request_id": 9}}}, guard); err == nil {
		t.Fatal("ordinary progress injected an approval origin")
	}
	admitted = false
	if err := execution.CompleteContinuous(t.Context(), db, item.ExecutionID, execution.ExecutionStatusRunning, execution.ExecutionStatusSuccess, nil, guard); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("revoked owner completion=%v", err)
	}
	admitted = true
	mutatingGuard := func(current execution.TaskExecution, now time.Time) error {
		if err := guard(current, now); err != nil {
			return err
		}
		*current.StartedAt = now.Add(time.Hour)
		current.Metadata[execution.ObservabilityKey].(map[string]interface{})["prior"] = "corrupted"
		*current.SourceTaskID = "changed"
		return nil
	}
	if err := execution.CompleteContinuous(t.Context(), db, item.ExecutionID, execution.ExecutionStatusRunning, execution.ExecutionStatusCancelled, map[string]interface{}{"metadata": models.JSONMap{}}, mutatingGuard); err != nil {
		t.Fatal(err)
	}
	var stored execution.TaskExecution
	if err := db.Where("execution_id = ?", item.ExecutionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SourceTaskID == nil || *stored.SourceTaskID != task || stored.Metadata[execution.ObservabilityKey].(map[string]interface{})["prior"] != "kept" {
		t.Fatal("admission mutated locked identity or metadata")
	}
	if stored.CompletedAt == nil || stored.ExecutionTimeMs == nil || *stored.ExecutionTimeMs != stored.CompletedAt.Sub(*stored.StartedAt).Milliseconds() {
		t.Fatal("continuous duration is not from persisted attempt")
	}
	if err := execution.CompleteContinuous(t.Context(), db, item.ExecutionID, execution.ExecutionStatusCancelled, execution.ExecutionStatusFailed, nil, guard); err == nil {
		t.Fatal("continuous terminal was rewritten")
	}
	if err := execution.UpdateContinuous(t.Context(), db, item.ExecutionID, nil, guard); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("terminal progress=%v", err)
	}
}
