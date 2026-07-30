package scantask

import (
	commonExecution "github.com/addp/common/execution"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func TestNewManualExecution(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	exec := NewManualExecution(
		3,
		9,
		7,
		1831,
		"postgres",
		[]string{"public"},
		[]models.ScanRefGroup{{Primary: "bucket/path/roads.shp"}},
		"basic",
		false,
		"transfer",
		now,
	)

	if exec.TenantID != 3 || exec.Module != commonExecution.ModuleMeta || exec.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("execution basics = %#v", exec)
	}
	if exec.StartedAt != nil {
		t.Fatalf("pending execution started_at = %v, want nil", exec.StartedAt)
	}
	if exec.TriggeredBy == nil || *exec.TriggeredBy != 9 {
		t.Fatalf("triggered_by = %#v", exec.TriggeredBy)
	}
	if exec.ExecutionConfig["engine_id"] != uint(7) || exec.ExecutionConfig["item_id"] != uint(1831) || exec.ExecutionConfig["scan_depth"] != "basic" {
		t.Fatalf("execution config = %#v", exec.ExecutionConfig)
	}
	if exec.ExecutionConfig["source"] != "transfer" {
		t.Fatalf("execution source = %#v", exec.ExecutionConfig)
	}
	if _, exists := exec.ExecutionConfig["token"]; exists {
		t.Fatal("manual execution persisted a user token")
	}
	refGroups, ok := exec.ExecutionConfig["ref_groups"].([]models.ScanRefGroup)
	if !ok || len(refGroups) != 1 || refGroups[0].Primary != "bucket/path/roads.shp" {
		t.Fatalf("execution ref_groups = %#v", exec.ExecutionConfig["ref_groups"])
	}
}

func TestNewScheduledExecutionUsesTargets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	task := &models.ScanTask{ID: 11, TenantID: 3, EngineID: 7, Name: "daily"}
	exec := NewScheduledExecution(task, "s3", scanflow.TargetSet{ScopeType: "catalog_path", CatalogPaths: []string{"bucket/prefix"}}, now, now)

	if exec.TriggerType != models.TriggerTypeScheduled {
		t.Fatalf("trigger_type = %q", exec.TriggerType)
	}
	if exec.StartedAt != nil {
		t.Fatalf("scheduled pending started_at = %v, want nil", exec.StartedAt)
	}
	if exec.SourceTaskID == nil || *exec.SourceTaskID != "11" {
		t.Fatalf("source_task_id = %#v", exec.SourceTaskID)
	}
	if got := exec.ExecutionConfig["catalog_paths"]; len(got.([]string)) != 1 {
		t.Fatalf("catalog_paths = %#v", got)
	}
	if exec.ExecutionConfig["source"] != "meta" {
		t.Fatalf("source = %#v", exec.ExecutionConfig["source"])
	}
	if exec.ExecutionConfig["planned_run_at"] != now.Format(time.RFC3339Nano) {
		t.Fatalf("planned_run_at = %#v", exec.ExecutionConfig["planned_run_at"])
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

	backfill := TaskStatusBackfillFields("exec-1", commonExecution.ExecutionStatusSuccess, now, now)
	if _, ok := backfill["next_run_at"]; ok {
		t.Fatalf("backfill fields = %#v", backfill)
	}
}
