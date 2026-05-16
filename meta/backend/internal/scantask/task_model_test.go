package scantask

import (
	"testing"
	"time"

	"github.com/addp/meta/internal/models"
)

func TestNewTaskFromUpsertRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	next := now.Add(time.Hour)
	task := NewTaskFromUpsertRequest(3, 9, &models.ScanTaskUpsertRequest{
		Name:         "daily",
		Description:  "scan",
		EngineID:     7,
		CatalogPaths: []string{"public"},
		ScanDepth:    "deep",
		Schedule:     "0 1 * * *",
		Enabled:      true,
	}, now, &next)

	if task.TenantID != 3 || task.EngineID != 7 || task.CreatedBy != 9 || task.UpdatedBy != 9 {
		t.Fatalf("task ids = %#v", task)
	}
	if task.Parameters["scan_depth"] != "deep" {
		t.Fatalf("parameters = %#v", task.Parameters)
	}
	if task.NextRunAt == nil || !task.NextRunAt.Equal(next) {
		t.Fatalf("next run = %#v", task.NextRunAt)
	}
}

func TestTaskParametersUseCatalogPaths(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	task := NewTaskFromUpsertRequest(3, 9, &models.ScanTaskUpsertRequest{
		Name:         "daily",
		EngineID:     7,
		CatalogPaths: []string{"catalog/path"},
		ScanDepth:    "deep",
	}, now, nil)

	got, ok := task.Parameters["catalog_paths"].([]string)
	if !ok || len(got) != 1 || got[0] != "catalog/path" {
		t.Fatalf("catalog_paths = %#v", task.Parameters["catalog_paths"])
	}
}

func TestAutomaticTaskName(t *testing.T) {
	t.Parallel()

	if got := AutomaticTaskName("PostGIS"); got != "自动扫描 - PostGIS" {
		t.Fatalf("name = %q", got)
	}
	if got := AutomaticTaskPattern(); got != "自动扫描%" {
		t.Fatalf("pattern = %q", got)
	}
}
