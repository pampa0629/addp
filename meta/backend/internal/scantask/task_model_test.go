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
	if task.Parameters["catalog_paths"] != nil {
		t.Fatalf("parameters should not contain catalog paths: %#v", task.Parameters)
	}
	if task.Scope["type"] != "catalog_path" {
		t.Fatalf("scope = %#v", task.Scope)
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

	got, ok := task.Scope["catalog_paths"].([]string)
	if !ok || len(got) != 1 || got[0] != "catalog/path" {
		t.Fatalf("catalog_paths = %#v", task.Scope["catalog_paths"])
	}
}

func TestAutomaticTaskOwner(t *testing.T) {
	t.Parallel()

	if got := AutomaticTaskName("PostGIS"); got != "自动扫描 - PostGIS" {
		t.Fatalf("name = %q", got)
	}
	if got := AutomaticTaskOwnerRef(7); got != "engine:7" {
		t.Fatalf("owner_ref = %q", got)
	}
}
