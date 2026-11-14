package execution

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// leaseClockValue is evaluated inside the conditional write on PostgreSQL.
// SQLite unit fixtures use the time sampled by DatabaseNow.
func leaseClockValue(db *gorm.DB, sampled time.Time) any {
	if db.Dialector.Name() == "postgres" {
		return gorm.Expr("clock_timestamp()")
	}
	return sampled.UTC()
}

// DatabaseNow is the shared lifecycle clock. PostgreSQL never trusts worker time.
func DatabaseNow(db *gorm.DB) (time.Time, error) {
	if db == nil {
		return time.Time{}, fmt.Errorf("execution database is required")
	}
	if db.Dialector.Name() != "postgres" {
		return time.Now().UTC(), nil
	}
	var now time.Time
	err := db.Session(&gorm.Session{NewDB: true}).Raw("SELECT clock_timestamp()").Scan(&now).Error
	return now.UTC(), err
}

func ownedActiveExecution(db *gorm.DB, lease Lease, now time.Time) *gorm.DB {
	query := ownedExecution(db, lease)
	return query.Where("lease_expires_at > ?", leaseClockValue(db, now))
}

func ownedExpiredExecution(db *gorm.DB, lease Lease, now time.Time) *gorm.DB {
	query := ownedExecution(db, lease)
	return query.Where("lease_expires_at <= ?", leaseClockValue(db, now))
}
