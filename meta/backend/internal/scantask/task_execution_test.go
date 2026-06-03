package scantask

import (
	commonExecution "github.com/addp/common/execution"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

func TestNewManualExecution(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	exec := NewManualExecution(3, 9, 7, "postgres", []string{"public"}, "basic", false, "token", now)

	if exec.TenantID != 3 || exec.Module != commonExecution.ModuleMeta || exec.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("execution basics = %#v", exec)
	}
	if exec.TriggeredBy == nil || *exec.TriggeredBy != 9 {
		t.Fatalf("triggered_by = %#v", exec.TriggeredBy)
	}
	if exec.ExecutionConfig["engine_id"] != uint(7) || exec.ExecutionConfig["scan_depth"] != "basic" {
		t.Fatalf("execution config = %#v", exec.ExecutionConfig)
	}
}

func TestNewScheduledExecutionUsesTargets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	task := &models.ScanTask{ID: 11, TenantID: 3, EngineID: 7, Name: "daily"}
	exec := NewScheduledExecution(task, "s3", TargetSet{CatalogPaths: []string{"bucket/prefix"}}, now)

	if exec.TriggerType != commonExecution.TriggerTypeSchedule {
		t.Fatalf("trigger_type = %q", exec.TriggerType)
	}
	if exec.SourceTaskID == nil || *exec.SourceTaskID != 11 {
		t.Fatalf("source_task_id = %#v", exec.SourceTaskID)
	}
	if got := exec.ExecutionConfig["catalog_paths"]; len(got.([]string)) != 1 {
		t.Fatalf("catalog_paths = %#v", got)
	}
}

func TestExecutionStatusFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	resp := &models.ScanResponse{
		CatalogNodesScanned: 1,
		ItemsScanned:        2,
		FieldsScanned:       3,
		Extraction:          &models.ExtractionScanStats{Documents: 1, Extracted: 1, Indexed: 1},
	}

	success := SuccessfulExecutionFields(resp, "postgres", now, 123, now)
	if success["status"] != commonExecution.ExecutionStatusSuccess || success["progress"] != 100 {
		t.Fatalf("success fields = %#v", success)
	}
	metadata := success["metadata"].(commonModels.JSONMap)
	if metadata["items_scanned"] != 2 || metadata["storage_type"] != "postgres" {
		t.Fatalf("metadata = %#v", metadata)
	}
	extraction := metadata["extraction"].(commonModels.JSONMap)
	if extraction["documents"] != 1 || extraction["indexed"] != 1 {
		t.Fatalf("extraction metadata = %#v", extraction)
	}

	next := now.Add(time.Hour)
	backfill := TaskStatusBackfillFields("exec-1", commonExecution.ExecutionStatusSuccess, now, &next, now)
	if backfill["next_run_at"] != next {
		t.Fatalf("backfill fields = %#v", backfill)
	}
}
