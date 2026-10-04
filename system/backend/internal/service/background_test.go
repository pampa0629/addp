package service

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// A private database avoids sharing state with another invocation of a test.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// Keep one idle connection alive and serialize background writes with reads.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
	return db
}

// observeBackgroundUpdate waits for a specific final database operation, including
// its error, before fixture cleanup. It never retries a failed write.
func observeBackgroundUpdate(t *testing.T, db *gorm.DB, name string, matches func(*gorm.DB) bool) func() {
	t.Helper()
	done := make(chan error, 1)
	var signalOnce, waitOnce sync.Once
	if err := db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test:"+name, func(tx *gorm.DB) {
		if matches(tx) {
			signalOnce.Do(func() {
				err := tx.Error
				if err == nil && tx.RowsAffected != 1 {
					err = fmt.Errorf("updated %d rows, want 1", tx.RowsAffected)
				}
				done <- err
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	wait := func() {
		t.Helper()
		waitOnce.Do(func() {
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("background %s: %v", name, err)
				}
			case <-time.After(5 * time.Second):
				t.Errorf("background %s did not finish", name)
			}
		})
	}
	t.Cleanup(wait)
	return wait
}
