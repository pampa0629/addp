package migration

import (
	"testing"
	"testing/fstest"
)

func TestMigrationNamesReturnsOnlyOrderedUpMigrations(t *testing.T) {
	names, err := migrationNames(fstest.MapFS{
		"sql/002_second.up.sql":   {Data: []byte("select 2")},
		"sql/001_first.up.sql":    {Data: []byte("select 1")},
		"sql/002_second.down.sql": {Data: []byte("select 2")},
		"sql/readme.md":           {Data: []byte("ignored")},
	}, "sql")
	if err != nil {
		t.Fatalf("migrationNames() error = %v", err)
	}
	if len(names) != 2 || names[0] != "001_first.up.sql" || names[1] != "002_second.up.sql" {
		t.Fatalf("migration names = %#v", names)
	}
}
