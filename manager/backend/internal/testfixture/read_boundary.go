package testfixture

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ReadDatabase gives each test its own Manager namespace, including the durable read boundary.
func ReadDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	for _, statement := range []string{
		"ATTACH DATABASE ':memory:' AS manager",
		"CREATE TABLE manager.protection_reads (read_id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, process_instance_id TEXT NOT NULL, started_at DATETIME NOT NULL)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
