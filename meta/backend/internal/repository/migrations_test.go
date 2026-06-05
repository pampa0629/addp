package repository

import (
	"reflect"
	"testing"
	"testing/fstest"
)

func TestMigrationNamesFiltersDownMigrations(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"migrations/011_drop_scan_task_runs.sql":           {Data: []byte("select 1;")},
		"migrations/009_add_tenant_engine_fk_down.sql":     {Data: []byte("select 1;")},
		"migrations/010_align_scan_tasks_basetask.sql":     {Data: []byte("select 1;")},
		"migrations/014_add_meta_node_full_name_unique.md": {Data: []byte("ignored")},
	}

	got, err := migrationNames(fsys, "migrations")
	if err != nil {
		t.Fatalf("migrationNames() error = %v", err)
	}
	want := []string{
		"010_align_scan_tasks_basetask.sql",
		"011_drop_scan_task_runs.sql",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("migration names = %#v, want %#v", got, want)
	}
}
